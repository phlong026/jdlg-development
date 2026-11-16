package tracking

import (
	"context"
	"errors"
	"time"
)

type ContentDeletion struct {
	ContentID string    `json:"content_id"`
	Revision  int       `json:"revision"`
	Status    string    `json:"status"`
	DeletedAt time.Time `json:"deleted_at"`
}

// DeleteContentDraft keeps the catalog row and every media object. Only a
// never-published draft can be hidden, so published snapshots and references
// cannot be orphaned by a delete action.
func (s *Service) DeleteContentDraft(ctx context.Context, scope Scope, id string, revision int) (ContentDeletion, error) {
	var out ContentDeletion
	if err := s.authorized(scope); err != nil {
		return out, err
	}
	if !scope.Manage {
		return out, fail(403, "无草稿删除权限")
	}
	err := s.transaction(ctx, func(r Repository) error {
		content, err := loadCatalogContent(r, "content", id)
		if err != nil || !scope.Allows(content.Owner) || content.ReadOnly {
			return fail(404, "素材不可访问")
		}
		if content.Revision != revision {
			return fail(409, "草稿已更新，请重新读取")
		}
		if content.Status != "draft" || content.Version != 0 {
			return fail(409, "已发布、已撤回或有历史版本的内容不能删除草稿")
		}
		if _, err = r.Get("version", contentKey(id, 1)); err == nil {
			return fail(409, "内容已存在发布版本，不能删除草稿")
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		for _, kind := range []string{"share", "native_ref"} {
			rows, err := scopedRecords(r, kind, RecordQuery{Owners: []string{content.Owner}, Content: id, Limit: 1})
			if err != nil {
				return err
			}
			if len(rows) > 0 {
				return fail(409, "内容已被引用，不能删除草稿")
			}
		}
		now := s.Now().UTC()
		content.Status = "deleted"
		content.Revision++
		if err = r.Put(record("content", id, content.Owner, now, persistedContent(content))); err != nil {
			return err
		}
		out = ContentDeletion{ContentID: id, Revision: content.Revision, Status: content.Status, DeletedAt: now}
		return audit(r, content.Owner, "delete_content_draft", scope.Actor, id, now, map[string]any{"revision": content.Revision})
	})
	return out, err
}

// DeleteCreatedContent hides an actor-authorized work without removing its
// immutable versions, media, or audit history. Published links are revoked in
// the same transaction so deletion is not merely a list filter.
func (s *Service) DeleteCreatedContent(ctx context.Context, scope Scope, id string, revision int) (ContentDeletion, error) {
	var out ContentDeletion
	if err := s.authorized(scope); err != nil {
		return out, err
	}
	if !scope.Manage {
		return out, fail(403, "无内容删除权限")
	}
	err := s.transaction(ctx, func(r Repository) error {
		content, err := loadCatalogContent(r, "content", id)
		if err != nil || !scope.Allows(content.Owner) || content.ReadOnly {
			return fail(404, "素材不可访问")
		}
		if content.Revision != revision {
			return fail(409, "内容已更新，请重新读取")
		}
		if content.Status == "deleted" {
			return fail(409, "内容已删除")
		}
		now := s.Now().UTC()
		if content.Version > 0 {
			if err = r.Put(record("content_revoked", id, content.Owner, now, map[string]any{"revoked_at": now})); err != nil {
				return err
			}
			if s.OnContentRevoke != nil {
				notices, queryErr := scopedRecords(r, "notice_event", RecordQuery{Content: id, Limit: 1})
				if queryErr != nil {
					return queryErr
				}
				if len(notices) > 0 {
					if err = s.OnContentRevoke(sourceReadContext(ctx, r), id); err != nil {
						return err
					}
				}
			}
		}
		content.Status = "deleted"
		content.Revision++
		if err = r.Put(record("content", id, content.Owner, now, persistedContent(content))); err != nil {
			return err
		}
		out = ContentDeletion{ContentID: id, Revision: content.Revision, Status: content.Status, DeletedAt: now}
		return audit(r, content.Owner, "delete_created_content", scope.Actor, id, now, map[string]any{"revision": content.Revision, "last_version": content.Version})
	})
	return out, err
}
