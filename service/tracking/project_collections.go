package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	projectCollectionKind        = "project_collection"
	projectCollectionVersionKind = "project_collection_version"
)

type ProjectCollectionItemInput struct {
	ContentID      string `json:"content_id"`
	ContentVersion int    `json:"content_version"`
}

type ProjectCollectionDraft struct {
	Title       string                       `json:"title"`
	Description string                       `json:"description,omitempty"`
	Owner       string                       `json:"owner_id"`
	Items       []ProjectCollectionItemInput `json:"items"`
	// CoverItemIndex 指定用第几个条目的封面作为合集封面；nil/越界/该条无封面
	// 一律回退"第一个有封面的条目"（历史行为）。(B12 合集封面自选)
	CoverItemIndex *int `json:"cover_item_index,omitempty"`
	// Collaborators 为只读共享的协作者 actor ID 列表（B10 方案A，特性开关控制）。
	Collaborators    []int64 `json:"collaborators,omitempty"`
	ExpectedRevision int     `json:"expected_revision,omitempty"`
	IdempotencyKey   string  `json:"idempotency_key,omitempty"`
}

type ProjectCollectionItem struct {
	Type           string     `json:"type,omitempty"`
	Presentation   string     `json:"presentation,omitempty"`
	RouteKey       string     `json:"route_key,omitempty"`
	ContentID      string     `json:"content_id,omitempty"`
	ContentVersion int        `json:"content_version,omitempty"`
	Title          string     `json:"title,omitempty"`
	Cover          *Asset     `json:"cover,omitempty"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	Available      bool       `json:"available"`
}

type ProjectCollection struct {
	ID           string                  `json:"collection_id"`
	Title        string                  `json:"title"`
	Description  string                  `json:"description,omitempty"`
	Owner        string                  `json:"owner_id"`
	Cover        *Asset                  `json:"cover,omitempty"`
	Items        []ProjectCollectionItem `json:"items"`
	Status       string                  `json:"status"`
	Revision     int                     `json:"revision"`
	Version      int                     `json:"version"`
	CreatorActor int64                   `json:"-"`
	// Collaborators 只读共享的协作者列表（B10 方案A）。
	Collaborators []int64    `json:"collaborators,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
}

type ProjectCollectionPage struct {
	Items      []ProjectCollection `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

type ProjectCollectionPreview struct {
	Collection   ProjectCollection `json:"collection"`
	PreviewToken string            `json:"preview_token"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

type PublishedProjectCollection struct {
	ID          string                  `json:"collection_id"`
	Title       string                  `json:"title"`
	Description string                  `json:"description,omitempty"`
	Cover       *Asset                  `json:"cover,omitempty"`
	Items       []ProjectCollectionItem `json:"items"`
	Status      string                  `json:"status"`
	Revision    int                     `json:"revision"`
	Version     int                     `json:"version"`
	PublishedAt time.Time               `json:"published_at"`
	Owner       string                  `json:"-"`
}

type ProjectCollectionDeletion struct {
	CollectionID string    `json:"collection_id"`
	Revision     int       `json:"revision"`
	Status       string    `json:"status"`
	DeletedAt    time.Time `json:"deleted_at"`
}

type projectCollectionCreateRequest struct {
	CollectionID string `json:"collection_id"`
	Fingerprint  string `json:"fingerprint"`
}

type projectCollectionStored struct {
	ProjectCollection
	CreatorActor int64 `json:"creator_actor"`
}

type projectCollectionPublication struct {
	CollectionID string `json:"collection_id"`
	Revision     int    `json:"revision"`
	Version      int    `json:"version"`
}

type publishedProjectCollectionStored struct {
	PublishedProjectCollection
	Owner string `json:"owner_id"`
}

type projectCollectionRevocation struct {
	ProjectCollection
	RequestedRevision int    `json:"requested_revision"`
	ReasonHash        string `json:"reason_hash"`
}

func projectCollectionVersionKey(id string, version int) string {
	return fmt.Sprintf("%s:%d", id, version)
}

func projectCollectionActorKey(actor int64) string { return actorVisitorKey(actor) }

func projectCollectionRecord(kind string, collection ProjectCollection, at time.Time) Record {
	row := record(kind, collection.ID, collection.Owner, at, projectCollectionStored{ProjectCollection: collection, CreatorActor: collection.CreatorActor})
	row.Visitor = projectCollectionActorKey(collection.CreatorActor)
	row.Content = collection.ID
	return row
}

func projectCollectionFingerprint(in ProjectCollectionDraft) string {
	return hash(string(marshal(struct {
		Title       string                       `json:"title"`
		Description string                       `json:"description"`
		Owner       string                       `json:"owner_id"`
		Items       []ProjectCollectionItemInput `json:"items"`
	}{strings.TrimSpace(in.Title), strings.TrimSpace(in.Description), in.Owner, in.Items})))
}

func (s *Service) projectCollectionAccess(scope Scope) error {
	return s.publishedMaterialAccess(scope)
}

func (s *Service) projectCollectionItems(r Repository, scope Scope, owner string, input []ProjectCollectionItemInput) ([]ProjectCollectionItem, error) {
	if len(input) == 0 || len(input) > 50 {
		return nil, fail(400, "合集作品数量无效")
	}
	targets := make([]NativeReferenceTarget, 0, len(input))
	seen := map[string]bool{}
	for _, item := range input {
		if !validID(item.ContentID) || item.ContentVersion < 1 {
			return nil, fail(400, "合集作品版本无效")
		}
		key := contentKey(item.ContentID, item.ContentVersion)
		if seen[key] {
			return nil, fail(400, "合集中不能重复添加同一作品版本")
		}
		seen[key] = true
		targets = append(targets, NativeReferenceTarget{ContentID: item.ContentID, ContentVersion: item.ContentVersion})
	}
	resolved, owners, err := s.nativeReferenceSources(r, ownerScope(scope, owner), targets)
	if err != nil {
		return nil, err
	}
	items := make([]ProjectCollectionItem, 0, len(targets))
	for _, target := range targets {
		key := contentKey(target.ContentID, target.ContentVersion)
		item, ok := resolved[key]
		if !ok || owners[key] != owner {
			return nil, fail(404, "已发布来源不可访问")
		}
		items = append(items, ProjectCollectionItem{
			Type: item.Type, Presentation: item.Presentation,
			RouteKey: item.RouteKey, ContentID: item.ContentID, ContentVersion: item.ContentVersion,
			Title: item.Title, Cover: item.Cover, PublishedAt: item.PublishedAt, Available: true,
		})
	}
	return items, nil
}

func projectCollectionCover(items []ProjectCollectionItem, preferred *int) *Asset {
	if preferred != nil {
		if index := *preferred; index >= 0 && index < len(items) && items[index].Cover != nil {
			cover := *items[index].Cover
			return &cover
		}
	}
	for _, item := range items {
		if item.Cover != nil {
			cover := *item.Cover
			return &cover
		}
	}
	return nil
}

func projectCollectionOwned(r Repository, scope Scope, id string) (ProjectCollection, error) {
	var collection ProjectCollection
	if !validID(id) {
		return collection, fail(404, "合集不可访问")
	}
	stored, err := decode[projectCollectionStored](r, projectCollectionKind, id)
	if errors.Is(err, ErrNotFound) {
		return ProjectCollection{}, fail(404, "合集不可访问")
	}
	if err != nil {
		return ProjectCollection{}, err
	}
	collection = stored.ProjectCollection
	collection.CreatorActor = stored.CreatorActor
	if collection.ID != id || collection.CreatorActor != scope.Actor || !scope.Allows(collection.Owner) || collection.Status == "deleted" {
		return ProjectCollection{}, fail(404, "合集不可访问")
	}
	return collection, nil
}

// SaveProjectCollection creates or updates one actor-owned collection. Source
// metadata is server-derived from immutable, currently public content versions.
func (s *Service) SaveProjectCollection(ctx context.Context, scope Scope, id string, in ProjectCollectionDraft) (ProjectCollection, error) {
	var out ProjectCollection
	if err := s.projectCollectionAccess(scope); err != nil {
		return out, err
	}
	in.Title, in.Description = strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	if !trim(in.Title, 80) || len(in.Description) > 500 || !scope.Allows(in.Owner) {
		return out, fail(400, "合集名称、简介或归属无效")
	}
	// B10: 协作者上限 20，去重。
	if len(in.Collaborators) > 20 {
		return out, fail(400, "合集协作者最多 20 人")
	}
	seenCollab := map[int64]bool{}
	collabs := make([]int64, 0, len(in.Collaborators))
	for _, id := range in.Collaborators {
		if id > 0 && !seenCollab[id] {
			seenCollab[id] = true
			collabs = append(collabs, id)
		}
	}
	in.Collaborators = collabs
	if id == "" && !validID(in.IdempotencyKey) {
		return out, fail(400, "缺少幂等标识")
	}
	err := s.transaction(ctx, func(r Repository) error {
		now := s.Now().UTC()
		if id == "" {
			requestID := hash(fmt.Sprintf("project-collection:create:%d:%s", scope.Actor, in.IdempotencyKey))
			fingerprint := projectCollectionFingerprint(in)
			request, err := decode[projectCollectionCreateRequest](r, "project_collection_create", requestID)
			if err == nil {
				if request.Fingerprint != fingerprint {
					return fail(409, "合集幂等参数冲突")
				}
				existing, err := projectCollectionOwned(r, scope, request.CollectionID)
				if err != nil {
					var unavailable *Error
					if errors.As(err, &unavailable) && unavailable.Status == 404 {
						return fail(409, "原合集已不可用，请使用新的请求标识")
					}
					return err
				}
				out = existing
				return nil
			}
			if !errors.Is(err, ErrNotFound) {
				return err
			}
			items, err := s.projectCollectionItems(r, scope, in.Owner, in.Items)
			if err != nil {
				return err
			}
			out = ProjectCollection{ID: opaque(), Title: in.Title, Description: in.Description, Owner: in.Owner, Cover: projectCollectionCover(items, in.CoverItemIndex), Items: items, Status: "draft", Revision: 1, CreatorActor: scope.Actor, Collaborators: in.Collaborators, CreatedAt: now, UpdatedAt: now}
			if err = r.Insert(projectCollectionRecord(projectCollectionKind, out, now)); err != nil {
				return err
			}
			requestRow := record("project_collection_create", requestID, in.Owner, now, projectCollectionCreateRequest{CollectionID: out.ID, Fingerprint: fingerprint})
			requestRow.Visitor = projectCollectionActorKey(scope.Actor)
			if err = r.Insert(requestRow); err != nil {
				return err
			}
			return audit(r, out.Owner, "create_project_collection", scope.Actor, out.ID, now, map[string]any{"revision": out.Revision})
		}

		existing, err := projectCollectionOwned(r, scope, id)
		if err != nil {
			return err
		}
		if existing.Status == "revoked" {
			return fail(409, "已撤回合集不能继续编辑")
		}
		if existing.Revision != in.ExpectedRevision {
			return fail(409, "合集已更新，请重新读取")
		}
		if existing.Owner != in.Owner {
			return fail(409, "合集归属变更需单独授权迁移")
		}
		items, err := s.projectCollectionItems(r, scope, in.Owner, in.Items)
		if err != nil {
			return err
		}
		existing.Title, existing.Description = in.Title, in.Description
		existing.Items, existing.Cover = items, projectCollectionCover(items, in.CoverItemIndex)
		existing.Revision++
		existing.UpdatedAt = now
		out = existing
		if err = r.Put(projectCollectionRecord(projectCollectionKind, out, now)); err != nil {
			return err
		}
		return audit(r, out.Owner, "save_project_collection", scope.Actor, out.ID, now, map[string]any{"revision": out.Revision})
	})
	return out, err
}

func (s *Service) ProjectCollectionCandidates(ctx context.Context, scope Scope, cursor string, limit int) (NativeReferenceCandidatePage, error) {
	return s.NativeReferenceCandidates(ctx, scope, cursor, limit)
}

func (s *Service) ProjectCollection(ctx context.Context, scope Scope, id string) (ProjectCollection, error) {
	var out ProjectCollection
	if err := s.projectCollectionAccess(scope); err != nil {
		return out, err
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		var err error
		out, err = projectCollectionOwned(r, scope, id)
		return err
	})
	return out, err
}

func (s *Service) ProjectCollections(ctx context.Context, scope Scope, cursor string, limit int) (ProjectCollectionPage, error) {
	out := ProjectCollectionPage{Items: []ProjectCollection{}}
	if err := s.projectCollectionAccess(scope); err != nil {
		return out, err
	}
	query, err := nativeReferenceQuery(scope, cursor, limit)
	if err != nil {
		return out, err
	}
	query.VisitorKeys = []string{projectCollectionActorKey(scope.Actor)}
	query.ExactVisitors = true
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		rows, err := pageRecords(r, projectCollectionKind, query)
		if err != nil {
			return err
		}
		if len(rows) == query.Limit {
			out.NextCursor = nativeReferenceCursor(rows[len(rows)-2])
			rows = rows[:len(rows)-1]
		}
		for _, row := range rows {
			var stored projectCollectionStored
			if json.Unmarshal(row.Payload, &stored) != nil || stored.CreatorActor != scope.Actor || stored.Owner != row.Owner {
				return ErrUnavailable
			}
			collection := stored.ProjectCollection
			collection.CreatorActor = stored.CreatorActor
			if collection.Status != "deleted" {
				out.Items = append(out.Items, collection)
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) PreviewProjectCollection(ctx context.Context, scope Scope, id string, revision int) (ProjectCollectionPreview, error) {
	var out ProjectCollectionPreview
	if err := s.projectCollectionAccess(scope); err != nil {
		return out, err
	}
	err := s.transaction(ctx, func(r Repository) error {
		collection, err := projectCollectionOwned(r, scope, id)
		if err != nil {
			return err
		}
		if collection.Status == "revoked" || collection.Revision != revision {
			return fail(409, "合集已更新或不可发布")
		}
		input := make([]ProjectCollectionItemInput, 0, len(collection.Items))
		for _, item := range collection.Items {
			input = append(input, ProjectCollectionItemInput{ContentID: item.ContentID, ContentVersion: item.ContentVersion})
		}
		items, err := s.projectCollectionItems(r, scope, collection.Owner, input)
		if err != nil {
			return err
		}
		collection.Items, collection.Cover = items, projectCollectionCover(items, nil)
		now := s.Now().UTC()
		expires := now.Add(15 * time.Minute)
		token := opaque()
		preview := struct {
			CollectionID string    `json:"collection_id"`
			Revision     int       `json:"revision"`
			Actor        int64     `json:"actor"`
			Expires      time.Time `json:"expires_at"`
		}{collection.ID, collection.Revision, scope.Actor, expires}
		if err = r.Insert(record("project_collection_preview", hash(token), collection.Owner, now, preview)); err != nil {
			return err
		}
		out = ProjectCollectionPreview{Collection: collection, PreviewToken: token, ExpiresAt: expires}
		return nil
	})
	return out, err
}

func (s *Service) PublishProjectCollection(ctx context.Context, scope Scope, id string, revision int, previewToken, idempotencyKey string) (PublishedProjectCollection, error) {
	var out PublishedProjectCollection
	if err := s.projectCollectionAccess(scope); err != nil {
		return out, err
	}
	if !validID(idempotencyKey) {
		return out, fail(400, "缺少幂等标识")
	}
	err := s.transaction(ctx, func(r Repository) error {
		requestID := hash(fmt.Sprintf("project-collection:publish:%d:%s:%s", scope.Actor, id, idempotencyKey))
		if requestRow, err := r.Get("project_collection_publication", requestID); err == nil {
			var request projectCollectionPublication
			if json.Unmarshal(requestRow.Payload, &request) != nil {
				return ErrUnavailable
			}
			if request.CollectionID != id || request.Revision != revision {
				return fail(409, "发布幂等参数冲突")
			}
			if !scope.Allows(requestRow.Owner) {
				return fail(404, "合集不可访问")
			}
			stored, err := decode[publishedProjectCollectionStored](r, projectCollectionVersionKind, projectCollectionVersionKey(id, request.Version))
			if err == nil && stored.Owner != requestRow.Owner {
				return ErrUnavailable
			}
			out = stored.PublishedProjectCollection
			out.Owner = stored.Owner
			return err
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		collection, err := projectCollectionOwned(r, scope, id)
		if err != nil {
			return err
		}
		if collection.Status == "revoked" || collection.Revision != revision {
			return fail(409, "合集草稿已更新")
		}
		preview, err := decode[struct {
			CollectionID string    `json:"collection_id"`
			Revision     int       `json:"revision"`
			Actor        int64     `json:"actor"`
			Expires      time.Time `json:"expires_at"`
		}](r, "project_collection_preview", hash(previewToken))
		if errors.Is(err, ErrNotFound) {
			return fail(409, "请先预览当前合集")
		}
		if err != nil {
			return err
		}
		if preview.CollectionID != id || preview.Revision != revision || preview.Actor != scope.Actor || !s.Now().Before(preview.Expires) {
			return fail(409, "请先预览当前合集")
		}
		input := make([]ProjectCollectionItemInput, 0, len(collection.Items))
		for _, item := range collection.Items {
			input = append(input, ProjectCollectionItemInput{ContentID: item.ContentID, ContentVersion: item.ContentVersion})
		}
		items, err := s.projectCollectionItems(r, scope, collection.Owner, input)
		if err != nil {
			return err
		}
		now := s.Now().UTC()
		collection.Items, collection.Cover = items, projectCollectionCover(items, nil)
		collection.Status = "published"
		collection.Version++
		collection.Revision++
		collection.UpdatedAt, collection.PublishedAt = now, &now
		out = PublishedProjectCollection{ID: collection.ID, Title: collection.Title, Description: collection.Description, Cover: collection.Cover, Items: collection.Items, Status: "published", Revision: collection.Revision, Version: collection.Version, PublishedAt: now, Owner: collection.Owner}
		versionRow := record(projectCollectionVersionKind, projectCollectionVersionKey(collection.ID, collection.Version), collection.Owner, now, publishedProjectCollectionStored{PublishedProjectCollection: out, Owner: collection.Owner})
		versionRow.Content = collection.ID
		if err = r.Insert(versionRow); err != nil {
			return err
		}
		if err = r.Put(projectCollectionRecord(projectCollectionKind, collection, now)); err != nil {
			return err
		}
		requestRow := record("project_collection_publication", requestID, collection.Owner, now, projectCollectionPublication{CollectionID: id, Revision: revision, Version: collection.Version})
		requestRow.Content = collection.ID
		requestRow.Visitor = projectCollectionActorKey(scope.Actor)
		if err = r.Insert(requestRow); err != nil {
			return err
		}
		return audit(r, collection.Owner, "publish_project_collection", scope.Actor, collection.ID, now, map[string]any{"version": collection.Version})
	})
	return out, err
}

// ReadPublishedProjectCollection returns the immutable manifest, but every item
// is checked against current public authorization. Unavailable items retain
// their position without exposing their former title or identifiers.
func (s *Service) ReadPublishedProjectCollection(ctx context.Context, id string, version int) (PublishedProjectCollection, error) {
	var out PublishedProjectCollection
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		published, err := s.projectCollectionVersion(r, id, version)
		if err != nil {
			return err
		}
		out = published.PublishedProjectCollection
		out.Owner = published.Owner
		targets := make([]NativeReferenceTarget, 0, len(out.Items))
		for _, item := range out.Items {
			targets = append(targets, NativeReferenceTarget{ContentID: item.ContentID, ContentVersion: item.ContentVersion})
		}
		publicScope := Scope{Owners: []string{out.Owner}}
		available, _, err := s.nativeReferenceSources(r, publicScope, targets)
		if err != nil {
			return err
		}
		items := make([]ProjectCollectionItem, 0, len(targets))
		for position, target := range targets {
			item, ok := available[contentKey(target.ContentID, target.ContentVersion)]
			if !ok {
				items = append(items, ProjectCollectionItem{Available: false})
				continue
			}
			cover := item.Cover
			if cover != nil {
				copy := *cover
				if copy.URL == "/api/v1/miniprogram/tracking/media/"+copy.ID {
					copy.URL = projectCollectionMediaPath(id, version, position, copy.ID)
				}
				cover = &copy
			}
			items = append(items, ProjectCollectionItem{Type: item.Type, Presentation: item.Presentation, RouteKey: item.RouteKey, ContentID: item.ContentID, ContentVersion: item.ContentVersion, Title: item.Title, Cover: cover, PublishedAt: item.PublishedAt, Available: true})
		}
		out.Items = items
		out.Cover = projectCollectionCover(items, nil)
		return nil
	})
	return out, err
}

// OpenPublishedProjectCollectionItem resolves one directory position only
// after the collection and that fixed source version pass their current checks.
// It deliberately returns no share token: directory navigation preserves the
// original collection share through EntryInput instead of minting a child share.
func (s *Service) OpenPublishedProjectCollectionItem(ctx context.Context, id string, version, position int) (map[string]string, error) {
	collection, err := s.ReadPublishedProjectCollection(ctx, id, version)
	if err != nil {
		return nil, err
	}
	if position < 0 || position >= len(collection.Items) {
		return nil, fail(404, "合集作品不可访问")
	}
	item := collection.Items[position]
	if !item.Available || item.RouteKey != publishedReaderRoute || !validID(item.ContentID) || item.ContentVersion < 1 {
		return nil, fail(404, "合集作品不可访问")
	}
	query := url.Values{
		"content_id": {item.ContentID}, "content_version": {strconv.Itoa(item.ContentVersion)},
		"collection_id": {id}, "collection_version": {strconv.Itoa(version)}, "item_position": {strconv.Itoa(position)},
	}
	return map[string]string{"route_key": item.RouteKey, "path": "/" + item.RouteKey + "?" + query.Encode()}, nil
}

func projectCollectionMediaPath(id string, version, position int, mediaID string) string {
	return fmt.Sprintf("/api/v1/miniprogram/tracking/project-collections/%s/versions/%d/items/%d/media/%s", id, version, position, mediaID)
}

func (s *Service) projectCollectionItemContent(ctx context.Context, r Repository, id string, version, position int) (Content, error) {
	var out Content
	collection, err := s.projectCollectionVersion(r, id, version)
	if err != nil || position < 0 || position >= len(collection.Items) {
		if err != nil {
			return out, err
		}
		return out, fail(404, "合集作品不可访问")
	}
	item := collection.Items[position]
	if item.RouteKey != publishedReaderRoute {
		return out, fail(404, "合集作品不可访问")
	}
	if _, err = s.projectCollectionChild(ctx, r, id, version, item.RouteKey, item.ContentID, item.ContentVersion); err != nil {
		return out, err
	}
	out, err = decode[Content](r, "version", contentKey(item.ContentID, item.ContentVersion))
	if err != nil {
		return Content{}, err
	}
	if out.ID != item.ContentID || out.Version != item.ContentVersion || out.Owner != collection.Owner || out.Status != "published" {
		return Content{}, fail(404, "合集作品不可访问")
	}
	return out, nil
}

// ReadPublishedProjectCollectionItemContent returns the real fixed content
// while replacing internal media URLs with collection-bound, revalidated URLs.
func (s *Service) ReadPublishedProjectCollectionItemContent(ctx context.Context, id string, version, position int) (Content, error) {
	var out Content
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		content, err := s.projectCollectionItemContent(ctx, r, id, version, position)
		if err != nil {
			return err
		}
		rewrite := func(asset *Asset) {
			if asset != nil && asset.URL == "/api/v1/miniprogram/tracking/media/"+asset.ID {
				asset.URL = projectCollectionMediaPath(id, version, position, asset.ID)
			}
		}
		if content.Cover != nil {
			cover := *content.Cover
			content.Cover = &cover
			rewrite(content.Cover)
		}
		content.Media = append([]Asset(nil), content.Media...)
		for index := range content.Media {
			rewrite(&content.Media[index])
		}
		out = content
		return nil
	})
	return out, err
}

// ReadPublishedProjectCollectionItemMedia authorizes one stored asset through
// the same current collection and fixed item checks used for the content body.
func (s *Service) ReadPublishedProjectCollectionItemMedia(ctx context.Context, id string, version, position int, mediaID string) (Asset, error) {
	var out Asset
	if !validID(mediaID) {
		return out, fail(404, "合集媒体不可访问")
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		content, err := s.projectCollectionItemContent(ctx, r, id, version, position)
		if err != nil {
			return err
		}
		assets := append([]Asset(nil), content.Media...)
		if content.Cover != nil {
			assets = append(assets, *content.Cover)
		}
		for _, asset := range assets {
			if asset.ID != mediaID || asset.URL != "/api/v1/miniprogram/tracking/media/"+mediaID {
				continue
			}
			stored, err := decode[Asset](r, "media", mediaID)
			if err != nil || stored != asset {
				if err != nil && !errors.Is(err, ErrNotFound) {
					return err
				}
				return fail(404, "合集媒体不可访问")
			}
			out = stored
			return nil
		}
		return fail(404, "合集媒体不可访问")
	})
	return out, err
}

func (s *Service) RevokeProjectCollection(ctx context.Context, scope Scope, id string, revision int, reason, idempotencyKey string) (ProjectCollection, error) {
	var out ProjectCollection
	if err := s.projectCollectionAccess(scope); err != nil {
		return out, err
	}
	if !trim(reason, 500) || !validID(idempotencyKey) {
		return out, fail(400, "撤回原因或幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		requestID := hash(fmt.Sprintf("project-collection:revoke:%d:%s:%s", scope.Actor, id, idempotencyKey))
		if requestRow, err := r.Get("project_collection_revocation", requestID); err == nil {
			var done projectCollectionRevocation
			if json.Unmarshal(requestRow.Payload, &done) != nil {
				return ErrUnavailable
			}
			if done.ID != id || done.RequestedRevision != revision || done.ReasonHash != hash(reason) {
				return fail(409, "撤回幂等参数冲突")
			}
			if !scope.Allows(requestRow.Owner) || done.Owner != requestRow.Owner {
				return fail(404, "合集不可访问")
			}
			out = done.ProjectCollection
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		collection, err := projectCollectionOwned(r, scope, id)
		if err != nil {
			return err
		}
		if collection.Revision != revision {
			return fail(409, "合集已更新")
		}
		if collection.Version < 1 || collection.Status != "published" {
			return fail(409, "未发布合集请直接删除草稿")
		}
		now := s.Now().UTC()
		collection.Status = "revoked"
		collection.Revision++
		collection.UpdatedAt = now
		out = collection
		if err = r.Put(projectCollectionRecord(projectCollectionKind, collection, now)); err != nil {
			return err
		}
		if err = r.Insert(record("project_collection_revocation", requestID, collection.Owner, now, projectCollectionRevocation{ProjectCollection: collection, RequestedRevision: revision, ReasonHash: hash(reason)})); err != nil {
			return err
		}
		return audit(r, collection.Owner, "revoke_project_collection", scope.Actor, collection.ID, now, map[string]string{"reason_hash": hash(reason)})
	})
	return out, err
}

func (s *Service) DeleteProjectCollectionDraft(ctx context.Context, scope Scope, id string, revision int) (ProjectCollectionDeletion, error) {
	var out ProjectCollectionDeletion
	if err := s.projectCollectionAccess(scope); err != nil {
		return out, err
	}
	err := s.transaction(ctx, func(r Repository) error {
		collection, err := projectCollectionOwned(r, scope, id)
		if err != nil {
			return err
		}
		if collection.Revision != revision {
			return fail(409, "合集已更新，请重新读取")
		}
		if collection.Version != 0 || collection.Status != "draft" {
			return fail(409, "只能删除未发布合集草稿")
		}
		now := s.Now().UTC()
		collection.Status = "deleted"
		collection.Revision++
		collection.UpdatedAt = now
		if err = r.Put(projectCollectionRecord(projectCollectionKind, collection, now)); err != nil {
			return err
		}
		if s.OnCollectionRevoke != nil {
			if err = s.OnCollectionRevoke(sourceReadContext(ctx, r), id); err != nil {
				return err
			}
		}
		out = ProjectCollectionDeletion{CollectionID: id, Revision: collection.Revision, Status: collection.Status, DeletedAt: now}
		return audit(r, collection.Owner, "delete_project_collection_draft", scope.Actor, id, now, map[string]any{"revision": collection.Revision})
	})
	return out, err
}

// DeleteProjectCollection soft-deletes any actor-owned collection lifecycle
// state. Immutable manifests and source works remain stored for audit, while
// current-master validation makes every old public URL unavailable.
func (s *Service) DeleteProjectCollection(ctx context.Context, scope Scope, id string, revision int) (ProjectCollectionDeletion, error) {
	var out ProjectCollectionDeletion
	if err := s.projectCollectionAccess(scope); err != nil {
		return out, err
	}
	err := s.transaction(ctx, func(r Repository) error {
		collection, err := projectCollectionOwned(r, scope, id)
		if err != nil {
			return err
		}
		if collection.Revision != revision {
			return fail(409, "合集已更新，请重新读取")
		}
		now := s.Now().UTC()
		collection.Status = "deleted"
		collection.Revision++
		collection.UpdatedAt = now
		if err = r.Put(projectCollectionRecord(projectCollectionKind, collection, now)); err != nil {
			return err
		}
		if s.OnCollectionRevoke != nil {
			if err = s.OnCollectionRevoke(sourceReadContext(ctx, r), id); err != nil {
				return err
			}
		}
		out = ProjectCollectionDeletion{CollectionID: id, Revision: collection.Revision, Status: collection.Status, DeletedAt: now}
		return audit(r, collection.Owner, "delete_project_collection", scope.Actor, id, now, map[string]any{"revision": collection.Revision, "last_version": collection.Version})
	})
	return out, err
}
