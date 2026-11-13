package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type contentCreateRequest struct {
	ContentID   string `json:"content_id"`
	Fingerprint string `json:"fingerprint"`
}

func contentCreateFingerprint(in Draft) string {
	in.IdempotencyKey = ""
	return hash(string(marshal(in)))
}

func (s *Service) SaveContent(ctx context.Context, scope Scope, id string, in Draft) (Content, error) {
	var out Content
	in, err := s.validateDraft(scope, in)
	if err != nil {
		return out, err
	}
	e := s.transaction(ctx, func(r Repository) error {
		if id == "" && in.IdempotencyKey != "" {
			if in.BodyFormat == "document_v1" {
				return fail(400, "文档资料使用原文件去重，不接受创建幂等标识")
			}
			if !validID(in.IdempotencyKey) {
				return fail(400, "幂等标识无效")
			}
			requestID := hash(fmt.Sprintf("content:create:%d:%s", scope.Actor, in.IdempotencyKey))
			fingerprint := contentCreateFingerprint(in)
			requestRow, requestErr := r.Get("content_create", requestID)
			if requestErr == nil {
				var request contentCreateRequest
				if json.Unmarshal(requestRow.Payload, &request) != nil || requestRow.Owner != in.Owner || requestRow.Visitor != "u:"+strconv.FormatInt(scope.Actor, 10) {
					return ErrUnavailable
				}
				if request.Fingerprint != fingerprint {
					return fail(409, "内容创建幂等参数冲突")
				}
				existing, loadErr := loadCatalogContent(r, "content", request.ContentID)
				if loadErr != nil || !scope.Allows(existing.Owner) || existing.Owner != in.Owner || existing.Status == "deleted" || existing.Status == "revoked" {
					return fail(409, "原创建结果已不可用，请使用新的请求标识")
				}
				out = existing
				return nil
			}
			if !errors.Is(requestErr, ErrNotFound) {
				return requestErr
			}
			saved, saveErr := s.saveContentIn(r, scope, id, in, s.Now())
			if saveErr != nil {
				return saveErr
			}
			createdRequest := record("content_create", requestID, saved.Owner, s.Now(), contentCreateRequest{ContentID: saved.ID, Fingerprint: fingerprint})
			createdRequest.Visitor = "u:" + strconv.FormatInt(scope.Actor, 10)
			if insertErr := r.Insert(createdRequest); insertErr != nil {
				return insertErr
			}
			out = saved
			return nil
		}
		if id == "" && in.BodyFormat == "document_v1" {
			key, request, err := documentArchiveIdentity(in)
			if err != nil {
				return err
			}
			stored, err := decode[documentArchiveRecord](r, "document_archive", key)
			if err == nil {
				if stored.MediaID != request.MediaID {
					return ErrUnavailable
				}
				existing, loadErr := loadCatalogContent(r, "content", stored.ContentID)
				if loadErr != nil || existing.Owner != in.Owner || !scope.Allows(existing.Owner) || existing.Type != "document" || len(existing.Media) != 1 || existing.Media[0].ID != request.MediaID {
					return ErrUnavailable
				}
				if existing.Status == "deleted" {
					return fail(409, "原文件对应的文档资料已删除")
				}
				out = existing
				return nil
			}
			if !errors.Is(err, ErrNotFound) {
				return err
			}
			saved, err := s.saveContentIn(r, scope, id, in, s.Now())
			if err != nil {
				return err
			}
			request.ContentID = saved.ID
			row := record("document_archive", key, saved.Owner, s.Now(), request)
			row.Content = saved.ID
			if err = r.Insert(row); err != nil {
				return err
			}
			out = saved
			return nil
		}
		saved, e := s.saveContentIn(r, scope, id, in, s.Now())
		out = saved
		return e
	})
	return out, e
}

// validateDraft applies the checks every writer of content shares and returns
// the draft with its body normalised.
func (s *Service) validateDraft(scope Scope, in Draft) (Draft, error) {
	if e := s.authorized(scope); e != nil {
		return in, e
	}
	if !scope.Manage {
		return in, fail(403, "无素材管理权限")
	}
	if (!trim(in.Title, 80) && !(incompleteVideoDraft(in.Body, in.BodyFormat) && strings.TrimSpace(in.Title) == "")) || !trim(in.Body, 100000) || len(in.SourceNote) > 2000 || (in.SourceURL != "" && !safeURL(in.SourceURL)) || (in.Rights != "own" && in.Rights != "authorized") || !scope.Allows(in.Owner) || len(in.MediaIDs) > 50 {
		return in, fail(400, "标题正文权属或归属无效")
	}
	// B11: 标签最多 10 个，每个 ≤ 32 字符，去重去空白。
	if len(in.Tags) > 10 {
		return in, fail(400, "标签最多 10 个")
	}
	seenTags := map[string]bool{}
	tags := make([]string, 0, len(in.Tags))
	for _, tag := range in.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || len([]rune(tag)) > 32 || seenTags[tag] {
			continue
		}
		seenTags[tag] = true
		tags = append(tags, tag)
	}
	in.Tags = tags
	body, format, err := normalizeBody(in.Body, in.BodyFormat)
	if err != nil {
		return in, err
	}
	in.Body, in.BodyFormat = body, format
	if format == "article_v2" {
		refs, e := articleMediaReferences(body)
		if e != nil {
			return in, e
		}
		authorized := map[string]bool{}
		for _, id := range in.MediaIDs {
			authorized[id] = true
		}
		for id := range refs {
			if !authorized[id] {
				return in, fail(400, "原排版正文引用了未授权媒体")
			}
		}
		// Public article media comes from body references, never extra editor selections.
		mediaIDs := make([]string, 0, len(refs))
		seen := map[string]bool{}
		for _, id := range in.MediaIDs {
			if refs[id] != "" && !seen[id] {
				seen[id] = true
				mediaIDs = append(mediaIDs, id)
			}
		}
		in.MediaIDs = mediaIDs
	} else if format == "document_v1" {
		_, mediaID, e := normalizeDocumentBody(body)
		if e != nil || in.CoverID != "" || len(in.MediaIDs) != 1 || in.MediaIDs[0] != mediaID {
			return in, fail(400, "文档资料必须且只能引用一个原文件")
		}
	}
	return in, nil
}

// saveContentIn writes one draft revision inside the caller's transaction. The
// draft must already have passed validateDraft.
func (s *Service) saveContentIn(r Repository, scope Scope, id string, in Draft, now time.Time) (Content, error) {
	var out Content
	created := now.UTC()
	revision := 1
	version := 0
	var oldPinned bool
	var oldTags []string
	var oldProjectID string
	// ProjectID 关联必须校验项目归属：项目 DesignerID 必须是当前账号，
	// 防止把他人的私有项目挂到自己的素材上（codex 批次2 P1）。
	if in.ProjectID != "" {
		project, perr := s.projectNoteManaged(r, scope, in.ProjectID)
		if perr != nil || project.DesignerID != scope.Actor {
			return out, fail(403, "项目关联不可访问")
		}
	}
	if id != "" {
		old, e := loadCatalogContent(r, "content", id)
		if e != nil || !scope.Allows(old.Owner) {
			return out, fail(404, "素材不可访问")
		}
		if old.Status == "deleted" {
			return out, fail(409, "已删除草稿不能恢复")
		}
		if old.Status == "revoked" {
			return out, fail(409, "撤回素材不可重新发布，请创建新素材")
		}
		if old.Owner != in.Owner {
			return out, fail(409, "素材归属变更需单独授权迁移")
		}
		if (old.Type == "document" && in.BodyFormat != "document_v1") || (old.Type != "document" && in.BodyFormat == "document_v1") {
			return out, fail(409, "文档资料与图文内容不能相互转换")
		}
		if old.Revision != in.Revision {
			return out, fail(409, "素材已更新，请重新读取")
		}
		revision = old.Revision + 1
		version = old.Version
		created = *old.CreatedAt
		oldPinned, oldTags, oldProjectID = old.Pinned, old.Tags, old.ProjectID
	} else {
		id = opaque()
	}
	media := []Asset{}
	var cover *Asset
	articleRefs := map[string]string{}
	documentRef := ""
	if in.BodyFormat == "article_v2" {
		articleRefs, _ = articleMediaReferences(in.Body)
	} else if in.BodyFormat == "document_v1" {
		_, documentRef, _ = normalizeDocumentBody(in.Body)
	}
	seen := map[string]bool{}
	for _, mid := range append(append([]string{}, in.MediaIDs...), in.CoverID) {
		if mid == "" || seen[mid] {
			continue
		}
		seen[mid] = true
		rec, e := r.Get("media", mid)
		if e != nil || !scope.Allows(rec.Owner) || rec.Owner != in.Owner {
			return out, fail(404, "媒体不可访问")
		}
		var a Asset
		if json.Unmarshal(rec.Payload, &a) != nil {
			return out, ErrUnavailable
		}
		if mid == in.CoverID {
			if a.Kind != "image" {
				return out, fail(400, "封面必须是图片")
			}
			c := a
			cover = &c
		}
		if expected := articleRefs[mid]; expected != "" && a.Kind != expected {
			return out, fail(400, "原排版正文媒体类型与授权文件不符")
		}
		if documentRef != "" && (mid != documentRef || !supportedDocumentAsset(a)) {
			return out, fail(400, "文档原文件类型与资料不符")
		}
		if documentRef == "" && a.Kind == "document" {
			return out, fail(400, "文档原文件仅能由文档资料引用")
		}
		if in.BodyFormat != "article_v2" || articleRefs[mid] != "" {
			media = append(media, a)
		}
	}
	contentType := "article"
	if in.BodyFormat == "document_v1" {
		contentType = "document"
	}
	out = Content{ID: id, CreatedAt: &created, Revision: revision, Version: version, Title: strings.TrimSpace(in.Title), Type: contentType, Status: "draft", Tags: in.Tags, Cover: cover, Media: media, Body: in.Body, BodyFormat: in.BodyFormat, SourceURL: in.SourceURL, SourceNote: in.SourceNote, ProjectID: in.ProjectID, Rights: in.Rights, Owner: in.Owner}
	out.Presentation = ContentPresentation(out)
	// B11 置顶：新内容默认 false；更新时显式传入则覆盖，否则保留原值。
	if id != "" {
		if in.Pinned != nil {
			out.Pinned = *in.Pinned
		} else {
			out.Pinned = oldPinned
		}
		if out.Tags == nil && oldTags != nil {
			out.Tags = oldTags // 未传标签时保留原标签
		}
		if in.ProjectID == "" && oldProjectID != "" {
			out.ProjectID = oldProjectID // 未显式改关联时保留原关联
		}
	}
	if e := r.Put(record("content", id, in.Owner, now, out)); e != nil {
		return out, e
	}
	return out, audit(r, in.Owner, "save_content", scope.Actor, id, now, map[string]any{"revision": revision})
}
func (s *Service) Preview(ctx context.Context, scope Scope, id string, revision int) (map[string]any, error) {
	var out map[string]any
	if e := s.authorized(scope); e != nil {
		return out, e
	}
	if !scope.Manage {
		return out, fail(403, "无预览权限")
	}
	e := s.transaction(ctx, func(r Repository) error {
		c, e := decode[Content](r, "content", id)
		if e != nil || !scope.Allows(c.Owner) {
			return fail(404, "素材不可访问")
		}
		if c.Revision != revision {
			return fail(409, "草稿已更新")
		}
		if c.Status != "draft" {
			return fail(409, "当前内容不是可预览草稿")
		}
		if incompleteVideoDraft(c.Body, c.BodyFormat) {
			return fail(400, "请补齐视频标题和主视频后再预览发布")
		}
		if c.Type == "document" {
			return fail(400, "文档资料仅供私有下载，不支持发布预览")
		}
		token := opaque()
		if e = r.Insert(record("preview", hash(token), c.Owner, s.Now(), map[string]any{"content": id, "revision": revision, "actor": scope.Actor, "expires": s.Now().Add(15 * time.Minute)})); e != nil {
			return e
		}
		out = map[string]any{"content": c, "preview_token": token}
		return nil
	})
	return out, e
}
func (s *Service) Publish(ctx context.Context, scope Scope, id string, revision int, preview, key string) (map[string]any, error) {
	var out map[string]any
	if e := s.authorized(scope); e != nil {
		return out, e
	}
	if !scope.Manage || !s.Config.ContentEnabled {
		return out, fail(403, "素材发布未授权或未启用")
	}
	if !validID(key) {
		return out, fail(400, "缺少幂等标识")
	}
	e := s.transaction(ctx, func(r Repository) error {
		c, e := loadCatalogContent(r, "content", id)
		if e != nil || !scope.Allows(c.Owner) {
			return fail(404, "素材不可访问")
		}
		requestKey := hash(fmt.Sprintf("publish:%d:%s:%s", scope.Actor, id, key))
		if old, e := decode[map[string]any](r, "publication", requestKey); e == nil {
			if old["revision"] != float64(revision) {
				return fail(409, "幂等参数冲突")
			}
			out = old
			delete(out, "revision")
			return nil
		}
		if c.Revision != revision || c.Status != "draft" {
			return fail(409, "草稿版本或状态已改变")
		}
		if incompleteVideoDraft(c.Body, c.BodyFormat) {
			return fail(400, "请补齐视频标题和主视频后再预览发布")
		}
		if c.Type == "document" {
			return fail(400, "文档资料仅供私有下载，不支持发布")
		}
		pv, e := decode[struct {
			Content  string    `json:"content"`
			Revision int       `json:"revision"`
			Actor    int64     `json:"actor"`
			Expires  time.Time `json:"expires"`
		}](r, "preview", hash(preview))
		if e != nil || pv.Content != id || pv.Revision != revision || pv.Actor != scope.Actor || s.Now().After(pv.Expires) {
			return fail(409, "请先预览当前草稿")
		}
		now := s.Now()
		c.Status = "published"
		c.Version++
		c.Revision++
		c.PublishedAt = &now
		if e = r.Insert(record("version", contentKey(id, c.Version), c.Owner, now, c)); e != nil {
			return e
		}
		if e = r.Put(record("content", id, c.Owner, now, c)); e != nil {
			return e
		}
		out = map[string]any{"content": c, "revision": revision, "share_methods": map[string]any{"message": "请在小程序自有素材页打开后使用原生分享。微信码与URL Link须完成服务端权限核验后开放。", "route_key": "pages/contentreader/contentreader", "content_id": id, "content_version": c.Version}}
		if e = r.Insert(record("publication", requestKey, c.Owner, now, out)); e != nil {
			return e
		}
		delete(out, "revision")
		return audit(r, c.Owner, "publish_content", scope.Actor, id, now, map[string]any{"version": c.Version})
	})
	return out, e
}
func (s *Service) Revoke(ctx context.Context, scope Scope, id string, revision int, reason, key string) (Content, error) {
	var c Content
	if e := s.authorized(scope); e != nil {
		return c, e
	}
	if !scope.Manage || !trim(reason, 500) || !validID(key) {
		return c, fail(400, "撤回权限、原因或幂等标识无效")
	}
	e := s.transaction(ctx, func(r Repository) error {
		old, e := loadCatalogContent(r, "content", id)
		if e != nil || !scope.Allows(old.Owner) {
			return fail(404, "素材不可访问")
		}
		c = old
		requestKey := hash(fmt.Sprintf("revoke:%d:%s:%s", scope.Actor, id, key))
		if done, e := decode[Content](r, "revocation", requestKey); e == nil {
			c = done
			return nil
		}
		if c.Revision != revision {
			return fail(409, "素材已更新")
		}
		if c.Status == "deleted" {
			return fail(409, "已删除草稿不能当作已发布内容撤回")
		}
		if e = r.Put(record("content_revoked", id, c.Owner, s.Now(), map[string]any{"revoked_at": s.Now()})); e != nil {
			return e
		}
		c.Status = "revoked"
		c.Revision++
		if e = r.Put(record("content", id, c.Owner, s.Now(), c)); e != nil {
			return e
		}
		if e = r.Insert(record("revocation", requestKey, c.Owner, s.Now(), c)); e != nil {
			return e
		}
		if s.OnContentRevoke != nil {
			notices, e := scopedRecords(r, "notice_event", RecordQuery{Content: id, Limit: 1})
			if e != nil {
				return e
			}
			if len(notices) > 0 {
				if e = s.OnContentRevoke(sourceReadContext(ctx, r), id); e != nil {
					return e
				}
			}
		}
		return audit(r, c.Owner, "revoke_content", scope.Actor, id, s.Now(), map[string]string{"reason_hash": hash(reason)})
	})
	return c, e
}
func (s *Service) ReadContent(ctx context.Context, id string, version int) (Content, error) {
	var out Content
	if !s.Config.ContentEnabled {
		return out, fail(404, "内容不可访问")
	}
	e := s.readTransaction(ctx, func(r Repository) error {
		if e := s.available(ctx, r, "pages/contentreader/contentreader", id, version); e != nil {
			return e
		}
		var e error
		out, e = decode[Content](r, "version", contentKey(id, version))
		if e == nil && out.Presentation == "" {
			out.Presentation = ContentPresentation(out)
		}
		return e
	})
	return out, e
}
func (s *Service) AddMedia(ctx context.Context, scope Scope, asset Asset, owner string) error {
	if e := s.validateMediaWrite(scope, asset, owner); e != nil {
		return e
	}
	return s.transaction(ctx, func(r Repository) error {
		return s.addMediaIn(r, scope, asset, owner, s.Now())
	})
}

func (s *Service) validateMediaWrite(scope Scope, asset Asset, owner string) error {
	if e := s.authorized(scope); e != nil {
		return e
	}
	if !scope.Manage || !scope.Allows(owner) || !validID(asset.ID) || asset.URL != "/api/v1/miniprogram/tracking/media/"+asset.ID {
		return fail(403, "媒体不在授权存储范围")
	}
	return nil
}

// addMediaIn keeps the media row and its audit record in the caller's transaction.
func (s *Service) addMediaIn(r Repository, scope Scope, asset Asset, owner string, now time.Time) error {
	if e := r.Insert(record("media", asset.ID, owner, now, asset)); e != nil {
		return e
	}
	return audit(r, owner, "upload_media", scope.Actor, asset.ID, now, map[string]string{"kind": asset.Kind})
}
func (s *Service) Media(ctx context.Context, scope *Scope, id string) (Asset, error) {
	var a Asset
	e := s.readTransaction(ctx, func(r Repository) error {
		rec, e := r.Get("media", id)
		if e != nil {
			return fail(404, "媒体不可访问")
		}
		if scope != nil {
			if s.authorized(*scope) != nil || (!scope.View && !scope.Manage) || !scope.Allows(rec.Owner) {
				return fail(404, "媒体不可访问")
			}
			return json.Unmarshal(rec.Payload, &a)
		}
		if !s.Config.ContentEnabled {
			return fail(404, "媒体不可访问")
		}
		allowed, e := publicMediaReferenced(r, id, rec.Owner)
		if e != nil {
			return e
		}
		if allowed {
			return json.Unmarshal(rec.Payload, &a)
		}
		return fail(404, "媒体不可访问")
	})
	return a, e
}
func (s *Service) ListMedia(ctx context.Context, scope Scope) (List, error) {
	page, err := s.ListMediaPage(ctx, scope, "", 50, "", nil)
	return List{Items: page.Items, Cursor: page.NextCursor, Snapshot: Snapshot{s.Now(), MetricVersion}}, err
}

type MediaPage struct {
	Items      []Asset `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

func (s *Service) ListMediaPage(ctx context.Context, scope Scope, cursor string, limit int, search string, kinds []string) (MediaPage, error) {
	out := MediaPage{Items: []Asset{}}
	if e := s.authorized(scope); e != nil {
		return out, e
	}
	if !scope.Manage {
		return out, fail(403, "无素材管理权限")
	}
	search = strings.TrimSpace(search)
	if len(search) > 100 {
		return out, fail(400, "素材搜索内容过长")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	normalizedKinds := []string(nil)
	if kinds != nil {
		normalizedKinds = make([]string, 0, len(kinds))
		seen := map[string]bool{}
		for _, kind := range kinds {
			kind = strings.TrimSpace(kind)
			if kind != "image" && kind != "video" && kind != "document" {
				return out, fail(400, "素材类型无效")
			}
			if !seen[kind] {
				seen[kind] = true
				normalizedKinds = append(normalizedKinds, kind)
			}
		}
	}
	query, err := nativeReferenceQuery(scope, cursor, limit)
	if err != nil {
		return out, err
	}
	query.TitleSearch, query.AssetKinds = search, normalizedKinds
	e := s.readQueryTransaction(ctx, func(r Repository) error {
		rows, e := pageRecords(r, "media", query)
		if e != nil {
			return e
		}
		if len(rows) == query.Limit {
			out.NextCursor = nativeReferenceCursor(rows[len(rows)-2])
			rows = rows[:len(rows)-1]
		}
		for _, row := range rows {
			var a Asset
			if !scope.Allows(row.Owner) || json.Unmarshal(row.Payload, &a) != nil || a.ID != row.ID {
				return ErrUnavailable
			}
			out.Items = append(out.Items, a)
		}
		return nil
	})
	return out, e
}

type generationContentSave struct {
	Fingerprint string `json:"fingerprint"`
	ContentID   string `json:"content_id"`
}

// SaveContentFromGeneration replays one explicit save, without merging a later
// intentional copy or overwriting edits made after the first response was lost.
func (s *Service) SaveContentFromGeneration(ctx context.Context, scope Scope, taskNo, key string, in Draft) (Content, error) {
	var out Content
	if !validID(taskNo) || !validID(key) {
		return out, fail(400, "生成任务或保存标识无效")
	}
	in, err := s.validateDraft(scope, in)
	if err != nil {
		return out, err
	}
	fingerprint := hash(string(marshal(struct {
		Task  string
		Draft Draft
	}{taskNo, in})))
	requestID := hash(fmt.Sprintf("generation-content:%d:%s", scope.Actor, key))
	err = s.transaction(ctx, func(r Repository) error {
		old, readErr := decode[generationContentSave](r, "generation_content_save", requestID)
		if readErr == nil {
			if old.Fingerprint != fingerprint {
				return fail(409, "保存请求参数已变化，请另存新内容")
			}
			current, loadErr := loadCatalogContent(r, "content", old.ContentID)
			if loadErr != nil && !errors.Is(loadErr, ErrNotFound) {
				return loadErr
			}
			if loadErr != nil || current.Owner != in.Owner || !scope.Allows(current.Owner) || current.ReadOnly {
				return fail(404, "内容草稿不可访问")
			}
			if current.Status == "deleted" || current.Status == "revoked" {
				return fail(409, "原内容已删除或撤回，请另存新内容")
			}
			out = current
			return nil
		}
		if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		out, err = s.saveContentIn(r, scope, "", in, s.Now())
		if err != nil {
			return err
		}
		return r.Insert(record("generation_content_save", requestID, in.Owner, s.Now(), generationContentSave{Fingerprint: fingerprint, ContentID: out.ID}))
	})
	return out, err
}
