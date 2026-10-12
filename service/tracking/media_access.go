package tracking

import (
	"encoding/json"
	"errors"
)

// publicMediaReferenceReader returns only an existence bit, never a tenant's
// published bodies. The SQL path supports all historical version records, so
// pre-upgrade publications do not require a backfill before their media works.
type publicMediaReferenceReader interface {
	PublicMediaReferenced(id, owner string) (bool, error)
}

func publicMediaReferenced(r Repository, id, owner string) (bool, error) {
	if indexed, ok := r.(publicMediaReferenceReader); ok {
		return indexed.PublicMediaReferenced(id, owner)
	}
	// Small in-memory repositories only. Production SQL uses the bounded scalar
	// query below; adding a repository wrapper must preserve that capability.
	versions, err := r.List("version")
	if err != nil {
		return false, err
	}
	for _, row := range versions {
		if row.Owner != owner {
			continue
		}
		var c Content
		if json.Unmarshal(row.Payload, &c) != nil {
			return false, ErrUnavailable
		}
		if c.Owner != owner || c.Status != "published" {
			continue
		}
		references := c.Cover != nil && c.Cover.ID == id
		for _, a := range c.Media {
			references = references || a.ID == id
		}
		if !references {
			continue
		}
		current, err := r.Get("content", c.ID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		var master Content
		if json.Unmarshal(current.Payload, &master) != nil {
			return false, ErrUnavailable
		}
		if current.Owner != owner || master.Owner != owner || master.ID != c.ID || master.Status == "revoked" {
			continue
		}
		if _, err = r.Get("content_revoked", c.ID); err == nil {
			continue
		} else if !errors.Is(err, ErrNotFound) {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// This compatibility query filters and joins inside MySQL and returns one bool.
// It does not load unrelated (or even matching) article JSON into Go. The owner
// predicates and tenant-qualified current-master/tombstone joins prevent stale
// versions, cross-owner references, or historical snapshots reviving media.
// JSON reference matching may still scan owner versions in the database; an
// indexed reverse-reference migration can optimize it after live DB profiling.
const publicMediaReferenceSQL = `SELECT EXISTS (
 SELECT 1 FROM tracking_records v
 JOIN tracking_records m ON m.tenant_id=v.tenant_id AND m.kind='content'
  AND m.record_id=JSON_UNQUOTE(JSON_EXTRACT(v.payload,'$.content_id'))
  AND m.owner_id=v.owner_id
 WHERE v.tenant_id=? AND v.kind='version' AND v.owner_id=?
  AND JSON_UNQUOTE(JSON_EXTRACT(v.payload,'$.owner_id'))=v.owner_id
  AND JSON_UNQUOTE(JSON_EXTRACT(m.payload,'$.owner_id'))=m.owner_id
  AND JSON_UNQUOTE(JSON_EXTRACT(m.payload,'$.content_id'))=m.record_id
  AND JSON_UNQUOTE(JSON_EXTRACT(v.payload,'$.status'))='published'
  AND JSON_UNQUOTE(JSON_EXTRACT(m.payload,'$.status'))<>'revoked'
  AND (JSON_CONTAINS(JSON_EXTRACT(v.payload,'$.media'), ?, '$')
   OR JSON_UNQUOTE(JSON_EXTRACT(v.payload,'$.cover.media_id'))=?)
  AND NOT EXISTS (SELECT 1 FROM tracking_records revoked
   WHERE revoked.tenant_id=v.tenant_id AND revoked.kind='content_revoked'
    AND revoked.record_id=m.record_id)
)`

func (r *sqlRepo) PublicMediaReferenced(id, owner string) (bool, error) {
	if r.budget.err != nil {
		return false, r.budget.err
	}
	var exists bool
	err := r.tx.QueryRowContext(r.ctx, publicMediaReferenceSQL, r.tenant, owner, string(marshal(map[string]string{"media_id": id})), id).Scan(&exists)
	if err != nil {
		return false, err
	}
	// Keep the same cumulative safety envelope even for scalar reads.
	if err = r.budget.consume(Record{Payload: []byte("false")}); err != nil {
		return false, err
	}
	return exists, nil
}
