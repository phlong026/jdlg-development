package tracking

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *Service) SaveContent(ctx context.Context, scope Scope, id string, in Draft) (Content, error) {
	var out Content
	if e := s.authorized(scope); e != nil {
		return out, e
	}
	if !scope.Manage {
		return out, fail(403, "无素材管理权限")
	}
	if !trim(in.Title, 80) || !trim(in.Body, 100000) || len(in.SourceNote) > 2000 || (in.SourceURL != "" && !safeURL(in.SourceURL)) || (in.Rights != "own" && in.Rights != "authorized") || !scope.Allows(in.Owner) || len(in.MediaIDs) > 50 {
		return out, fail(400, "标题正文权属或归属无效")
	}
	body, format, err := normalizeBody(in.Body, in.BodyFormat)
	if err != nil {
		return out, err
	}
	in.Body, in.BodyFormat = body, format
	e := s.transaction(ctx, func(r Repository) error {
		now := s.Now()
		created := now.UTC()
		revision := 1
		version := 0
		if id != "" {
			old, e := loadCatalogContent(r, "content", id)
			if e != nil || !scope.Allows(old.Owner) {
				return fail(404, "素材不可访问")
			}
			if old.Status == "revoked" {
				return fail(409, "撤回素材不可重新发布，请创建新素材")
			}
			if old.Owner != in.Owner {
				return fail(409, "素材归属变更需单独授权迁移")
			}
			if old.Revision != in.Revision {
				return fail(409, "素材已更新，请重新读取")
			}
			revision = old.Revision + 1
			version = old.Version
			created = *old.CreatedAt
		} else {
			id = opaque()
		}
		media := []Asset{}
		var cover *Asset
		seen := map[string]bool{}
		for _, mid := range append(append([]string{}, in.MediaIDs...), in.CoverID) {
			if mid == "" || seen[mid] {
				continue
			}
			seen[mid] = true
			rec, e := r.Get("media", mid)
			if e != nil || !scope.Allows(rec.Owner) || rec.Owner != in.Owner {
				return fail(404, "媒体不可访问")
			}
			var a Asset
			if json.Unmarshal(rec.Payload, &a) != nil {
				return ErrUnavailable
			}
			if mid == in.CoverID {
				if a.Kind != "image" {
					return fail(400, "封面必须是图片")
				}
				c := a
				cover = &c
			}
			media = append(media, a)
		}
		out = Content{ID: id, CreatedAt: &created, Revision: revision, Version: version, Title: strings.TrimSpace(in.Title), Type: "article", Status: "draft", Cover: cover, Media: media, Body: in.Body, BodyFormat: in.BodyFormat, SourceURL: in.SourceURL, SourceNote: in.SourceNote, Rights: in.Rights, Owner: in.Owner}
		if e := r.Put(record("content", id, in.Owner, now, out)); e != nil {
			return e
		}
		return audit(r, in.Owner, "save_content", scope.Actor, id, now, map[string]any{"revision": revision})
	})
	return out, e
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
		return audit(r, c.Owner, "revoke_content", scope.Actor, id, s.Now(), map[string]string{"reason_hash": hash(reason)})
	})
	return c, e
}
func (s *Service) ReadContent(ctx context.Context, id string, version int) (Content, error) {
	var out Content
	if !s.Config.ContentEnabled {
		return out, fail(404, "内容不可访问")
	}
	e := s.transaction(ctx, func(r Repository) error {
		if e := s.available(ctx, r, "pages/contentreader/contentreader", id, version); e != nil {
			return e
		}
		var e error
		out, e = decode[Content](r, "version", contentKey(id, version))
		return e
	})
	return out, e
}
func (s *Service) AddMedia(ctx context.Context, scope Scope, asset Asset, owner string) error {
	if e := s.authorized(scope); e != nil {
		return e
	}
	if !scope.Manage || !scope.Allows(owner) || !validID(asset.ID) || asset.URL != "/api/v1/miniprogram/tracking/media/"+asset.ID {
		return fail(403, "媒体不在授权存储范围")
	}
	return s.transaction(ctx, func(r Repository) error {
		if e := r.Insert(record("media", asset.ID, owner, s.Now(), asset)); e != nil {
			return e
		}
		return audit(r, owner, "upload_media", scope.Actor, asset.ID, s.Now(), map[string]string{"kind": asset.Kind})
	})
}
func (s *Service) Media(ctx context.Context, scope *Scope, id string) (Asset, error) {
	var a Asset
	e := s.transaction(ctx, func(r Repository) error {
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
	out := List{Items: []Asset{}, Snapshot: Snapshot{s.Now(), MetricVersion}}
	if e := s.authorized(scope); e != nil {
		return out, e
	}
	if !scope.Manage {
		return out, fail(403, "无素材管理权限")
	}
	e := s.transaction(ctx, func(r Repository) error {
		rows, e := r.List("media")
		if e != nil {
			return e
		}
		items := []Asset{}
		for _, row := range rows {
			if !scope.Allows(row.Owner) {
				continue
			}
			var a Asset
			if json.Unmarshal(row.Payload, &a) != nil {
				return ErrUnavailable
			}
			items = append(items, a)
		}
		out.Items = items
		return nil
	})
	return out, e
}
