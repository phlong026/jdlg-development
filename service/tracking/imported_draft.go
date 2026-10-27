package tracking

import (
	"context"
	"errors"
	"strconv"
)

// SaveImportedDraft makes task completion replayable after a worker restart.
// It shares the normal content/media authorization and never publishes.
func (s *Service) SaveImportedDraft(ctx context.Context, scope Scope, importID string, in Draft, provenance map[string]string) (Content, error) {
	var out Content
	if !validID(importID) {
		return out, fail(400, "导入任务标识无效")
	}
	normalized, err := s.validateDraft(scope, in)
	if err != nil {
		return out, err
	}
	key := hash("content-import:" + strconv.FormatInt(scope.Actor, 10) + ":" + importID)
	signature := hash(string(marshal(normalized)) + ":" + string(marshal(provenance)))
	err = s.transaction(ctx, func(r Repository) error {
		old, e := decode[map[string]string](r, "content_import_result", key)
		if e == nil {
			if old["signature"] != signature {
				return fail(409, "导入任务输入已经变化")
			}
			content, e := decode[Content](r, "content", old["content_id"])
			if e != nil {
				return e
			}
			if content.Owner != normalized.Owner || !scope.Allows(content.Owner) {
				return fail(404, "素材不可访问")
			}
			out = content
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		saved, e := s.saveContentIn(r, scope, "", normalized, s.Now())
		if e != nil {
			return e
		}
		row := record("content_import_result", key, saved.Owner, s.Now(), map[string]string{"content_id": saved.ID, "signature": signature})
		row.Content = saved.ID
		if e = r.Insert(row); e != nil {
			return e
		}
		metadata := map[string]any{"import_id": importID, "created_by": scope.Actor, "source": provenance}
		row = record("import_provenance", saved.ID, saved.Owner, s.Now(), metadata)
		row.Content = saved.ID
		if e = r.Insert(row); e != nil {
			return e
		}
		out = saved
		return nil
	})
	return out, err
}
