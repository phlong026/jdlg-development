package tracking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type NativeReferenceInput struct {
	ContentID      string `json:"content_id"`
	ContentVersion int    `json:"content_version"`
	IdempotencyKey string `json:"idempotency_key"`
}
type NativeReferenceTarget struct {
	RouteKey       string     `json:"route_key"`
	ContentID      string     `json:"content_id"`
	ContentVersion int        `json:"content_version"`
	Title          string     `json:"title"`
	Cover          *Asset     `json:"cover,omitempty"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
}
type NativeReference struct {
	ReferenceID  string                `json:"reference_id"`
	Target       NativeReferenceTarget `json:"target"`
	Available    bool                  `json:"available"`
	CreatedAt    time.Time             `json:"created_at"`
	CreatorActor int64                 `json:"-"`
}
type nativeReferenceRecord struct {
	NativeReference
	CreatorActor int64 `json:"creator_actor"`
	Deleted      bool  `json:"deleted,omitempty"`
}
type NativeReferencePage struct {
	Items      []NativeReference `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}
type NativeReferenceCandidatePage struct {
	Items      []NativeReferenceTarget `json:"items"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

func nativeReferenceTarget(item PublishedMaterial) NativeReferenceTarget {
	return NativeReferenceTarget{RouteKey: item.Route, ContentID: item.ID, ContentVersion: item.Version, Title: item.Title, Cover: item.Cover, PublishedAt: item.PublishedAt}
}
func nativeReferenceQuery(scope Scope, cursor string, limit int) (RecordQuery, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	q := RecordQuery{Owners: scope.Owners, Limit: limit + 1, Descending: true}
	if cursor != "" {
		var c projectNoteCursor
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if len(cursor) > 512 || err != nil || json.Unmarshal(raw, &c) != nil || !validID(c.ID) || c.At.IsZero() {
			return q, fail(400, "分页游标无效")
		}
		q.BeforeAt, q.BeforeID = c.At, c.ID
	}
	return q, nil
}
func nativeReferenceCursor(row Record) string {
	raw, _ := json.Marshal(projectNoteCursor{At: row.At, ID: row.ID})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Enrich a bounded page in three scoped reads, never one SQL lookup per row.
// Both the mutable master and the immutable published snapshot must be valid.
func (s *Service) nativeReferenceSources(r Repository, scope Scope, targets []NativeReferenceTarget) (map[string]NativeReferenceTarget, map[string]string, error) {
	items, owners := map[string]NativeReferenceTarget{}, map[string]string{}
	if err := s.Config.validateRoute(publishedReaderRoute); err != nil {
		return nil, nil, err
	}
	if len(targets) == 0 {
		return items, owners, nil
	}
	ids, versions := []string{}, []string{}
	for _, target := range targets {
		ids = append(ids, target.ContentID)
		versions = append(versions, contentKey(target.ContentID, target.ContentVersion))
	}
	masters, err := scopedRecords(r, "content", RecordQuery{Owners: scope.Owners, IDs: ids})
	if err != nil {
		return nil, nil, err
	}
	masterByID := map[string]Content{}
	for _, row := range masters {
		var c Content
		if json.Unmarshal(row.Payload, &c) != nil || c.ID != row.ID || c.Owner != row.Owner {
			return nil, nil, ErrUnavailable
		}
		masterByID[c.ID] = c
	}
	revoked, err := scopedRecords(r, "content_revoked", RecordQuery{Owners: scope.Owners, IDs: ids})
	if err != nil {
		return nil, nil, err
	}
	for _, row := range revoked {
		delete(masterByID, row.ID)
	}
	snapshots, err := scopedRecords(r, "version", RecordQuery{Owners: scope.Owners, IDs: versions})
	if err != nil {
		return nil, nil, err
	}
	for _, row := range snapshots {
		var c Content
		if json.Unmarshal(row.Payload, &c) != nil || contentKey(c.ID, c.Version) != row.ID || c.Owner != row.Owner {
			return nil, nil, ErrUnavailable
		}
		master, ok := masterByID[c.ID]
		if !ok || master.ReadOnly || master.Status == "revoked" || master.Version < c.Version || c.Version < 1 || c.Status != "published" || c.Owner != master.Owner || !scope.Allows(master.Owner) {
			continue
		}
		items[row.ID] = nativeReferenceTarget(PublishedMaterial{ID: c.ID, Version: c.Version, Title: c.Title, Cover: c.Cover, PublishedAt: c.PublishedAt, Route: publishedReaderRoute})
		owners[row.ID] = master.Owner
	}
	return items, owners, nil
}

func (s *Service) nativeReferenceSource(r Repository, scope Scope, id string, version int) (NativeReferenceTarget, string, error) {
	items, owners, err := s.nativeReferenceSources(r, scope, []NativeReferenceTarget{{ContentID: id, ContentVersion: version}})
	if err != nil {
		return NativeReferenceTarget{}, "", err
	}
	key := contentKey(id, version)
	item, ok := items[key]
	if !ok {
		return item, "", fail(404, "已发布来源不可访问")
	}
	return item, owners[key], nil
}

// The cursor follows scanned catalog rows, including drafts. A page can be
// empty and still have a cursor; skipping those rows must not skip later sources.
func (s *Service) NativeReferenceCandidates(ctx context.Context, scope Scope, cursor string, limit int) (NativeReferenceCandidatePage, error) {
	out := NativeReferenceCandidatePage{Items: []NativeReferenceTarget{}}
	if err := s.publishedMaterialAccess(scope); err != nil {
		return out, err
	}
	q, err := nativeReferenceQuery(scope, cursor, limit)
	if err != nil {
		return out, err
	}
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		rows, err := pageRecords(r, "content", q)
		if err != nil {
			return err
		}
		if len(rows) == q.Limit {
			out.NextCursor = nativeReferenceCursor(rows[len(rows)-2])
			rows = rows[:len(rows)-1]
		}
		targets := []NativeReferenceTarget{}
		for _, row := range rows {
			var master Content
			if json.Unmarshal(row.Payload, &master) != nil {
				return ErrUnavailable
			}
			if master.Version < 1 || master.ReadOnly || master.Status == "revoked" {
				continue
			}
			targets = append(targets, NativeReferenceTarget{ContentID: master.ID, ContentVersion: master.Version})
		}
		items, _, err := s.nativeReferenceSources(r, scope, targets)
		if err != nil {
			return err
		}
		for _, target := range targets {
			if item, ok := items[contentKey(target.ContentID, target.ContentVersion)]; ok {
				out.Items = append(out.Items, item)
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) CreateNativeReference(ctx context.Context, scope Scope, in NativeReferenceInput) (NativeReference, error) {
	var out NativeReference
	if err := s.publishedMaterialAccess(scope); err != nil {
		return out, err
	}
	if !validID(in.ContentID) || in.ContentVersion < 1 || !validID(in.IdempotencyKey) {
		return out, fail(400, "来源版本或幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		id := hash(fmt.Sprintf("native-ref:%d:%s", scope.Actor, in.IdempotencyKey))
		if row, err := r.Get("native_ref", id); err == nil {
			var old nativeReferenceRecord
			if json.Unmarshal(row.Payload, &old) != nil {
				return ErrUnavailable
			}
			if old.CreatorActor != scope.Actor || old.Target.ContentID != in.ContentID || old.Target.ContentVersion != in.ContentVersion {
				return fail(409, "引用幂等参数冲突")
			}
			if old.Deleted || !scope.Allows(row.Owner) {
				return fail(409, "该引用已移除或不可用，请使用新的请求标识")
			}
			if _, _, err = s.nativeReferenceSource(r, scope, in.ContentID, in.ContentVersion); err != nil {
				return err
			}
			out = old.NativeReference
			out.CreatorActor = old.CreatorActor
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		item, owner, err := s.nativeReferenceSource(r, scope, in.ContentID, in.ContentVersion)
		if err != nil {
			return err
		}
		now := s.Now().UTC()
		out = NativeReference{ReferenceID: id, Target: item, Available: true, CreatedAt: now, CreatorActor: scope.Actor}
		row := record("native_ref", id, owner, now, nativeReferenceRecord{NativeReference: out, CreatorActor: scope.Actor})
		row.Visitor, row.Content = actorVisitorKey(scope.Actor), in.ContentID
		return r.Insert(row)
	})
	return out, err
}
func nativeReferenceOwned(r Repository, scope Scope, id string) (Record, nativeReferenceRecord, error) {
	var ref nativeReferenceRecord
	if !validID(id) {
		return Record{}, ref, fail(404, "引用不可访问")
	}
	row, err := r.Get("native_ref", id)
	if errors.Is(err, ErrNotFound) {
		return row, ref, fail(404, "引用不可访问")
	}
	if err != nil {
		return row, ref, err
	}
	if json.Unmarshal(row.Payload, &ref) != nil {
		return row, ref, ErrUnavailable
	}
	if ref.Deleted || ref.CreatorActor != scope.Actor || !scope.Allows(row.Owner) {
		return row, ref, fail(404, "引用不可访问")
	}
	ref.NativeReference.CreatorActor = ref.CreatorActor
	return row, ref, nil
}
func (s *Service) NativeReferences(ctx context.Context, scope Scope, cursor string, limit int) (NativeReferencePage, error) {
	out := NativeReferencePage{Items: []NativeReference{}}
	if err := s.publishedMaterialAccess(scope); err != nil {
		return out, err
	}
	q, err := nativeReferenceQuery(scope, cursor, limit)
	if err != nil {
		return out, err
	}
	q.VisitorKeys = []string{actorVisitorKey(scope.Actor)}
	q.ExactVisitors = true
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		rows, err := pageRecords(r, "native_ref", q)
		if err != nil {
			return err
		}
		if len(rows) == q.Limit {
			out.NextCursor = nativeReferenceCursor(rows[len(rows)-2])
			rows = rows[:len(rows)-1]
		}
		refs := []nativeReferenceRecord{}
		targets := []NativeReferenceTarget{}
		for _, row := range rows {
			var ref nativeReferenceRecord
			if json.Unmarshal(row.Payload, &ref) != nil || ref.CreatorActor != scope.Actor {
				return ErrUnavailable
			}
			if ref.Deleted {
				continue
			}
			refs = append(refs, ref)
			targets = append(targets, ref.Target)
		}
		items, _, err := s.nativeReferenceSources(r, scope, targets)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			item, ok := items[contentKey(ref.Target.ContentID, ref.Target.ContentVersion)]
			ref.Available = ok
			if ok {
				ref.Target = item
			}
			ref.NativeReference.CreatorActor = ref.CreatorActor
			out.Items = append(out.Items, ref.NativeReference)
		}
		return nil
	})
	return out, err
}
func (s *Service) DeleteNativeReference(ctx context.Context, scope Scope, id string) error {
	if err := s.publishedMaterialAccess(scope); err != nil {
		return err
	}
	return s.transaction(ctx, func(r Repository) error {
		row, ref, err := nativeReferenceOwned(r, scope, id)
		if err != nil {
			return err
		}
		ref.Deleted = true
		row.Payload, _ = json.Marshal(ref)
		// Preserve the original ordering timestamp and idempotency tombstone.
		return r.Put(row)
	})
}
func (s *Service) NativeReferenceTarget(ctx context.Context, scope Scope, id string) (map[string]string, error) {
	var out map[string]string
	if err := s.publishedMaterialAccess(scope); err != nil {
		return out, err
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		_, ref, err := nativeReferenceOwned(r, scope, id)
		if err != nil {
			return err
		}
		item, _, err := s.nativeReferenceSource(r, scope, ref.Target.ContentID, ref.Target.ContentVersion)
		if err != nil {
			return err
		}
		query := url.Values{"content_id": {item.ContentID}, "content_version": {strconv.Itoa(item.ContentVersion)}}
		out = map[string]string{"route_key": item.RouteKey, "path": "/" + item.RouteKey + "?" + query.Encode()}
		return nil
	})
	return out, err
}
