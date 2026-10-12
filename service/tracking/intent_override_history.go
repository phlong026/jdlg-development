package tracking

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// The current row is a compatibility pointer; complete decisions are immutable.
// Reasons live in visitor-scoped history, never the permanent generic audit log.
type intentOverrideDecision struct {
	IntentOverride
	Revision int64  `json:"revision"`
	Previous string `json:"previous_decision_id,omitempty"`
	Legacy   bool   `json:"legacy_imported,omitempty"`
}
type intentOverrideClock struct {
	Revision int64 `json:"revision"`
}
type intentOverrideBoundary struct {
	Before time.Time `json:"unavailable_before"`
}

func overrideDecisionID(owner, visitor string, revision int64) string {
	return privateKey(owner, fmt.Sprintf("%s\x00%d", visitor, revision))
}
func readOverrideDecision(row Record) (intentOverrideDecision, error) {
	var d intentOverrideDecision
	if json.Unmarshal(row.Payload, &d) != nil {
		return d, ErrUnavailable
	}
	if d.Created.IsZero() {
		d.Created = row.At
	}
	if d.Created.IsZero() || d.Expires.IsZero() || d.Actor <= 0 || !trim(d.Reason, 500) || (d.Level != "high" && d.Level != "medium" && d.Level != "low" && d.Level != "unknown") || d.Revision < 0 {
		return d, ErrUnavailable
	}
	return d, nil
}

// Hash a typed canonical encoding, since MySQL's JSON storage may reorder keys.
func overrideDecisionDigest(d intentOverrideDecision) string { return hash(string(marshal(d))) }
func overrideClock(r Repository, owner string) (intentOverrideClock, error) {
	v, e := decode[intentOverrideClock](r, "intent_override_clock", privateKey(owner, "current"))
	if errors.Is(e, ErrNotFound) {
		return intentOverrideClock{}, nil
	}
	if e != nil || v.Revision < 0 {
		return v, ErrUnavailable
	}
	return v, nil
}
func saveOverrideBoundary(r Repository, owner, visitor string, before, now time.Time) error {
	id := privateKey(owner, visitor)
	old, e := decode[intentOverrideBoundary](r, "intent_override_boundary", id)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return e
	}
	if old.Before.After(before) {
		before = old.Before
	}
	rec := record("intent_override_boundary", id, owner, now, intentOverrideBoundary{Before: before})
	rec.Visitor = visitor
	return r.Put(rec)
}
func appendIntentOverride(r Repository, owner, visitor string, actor int64, level, reason string, expires, now time.Time) error {
	clock, e := overrideClock(r, owner)
	if e != nil {
		return e
	}
	id := privateKey(owner, visitor)
	prior, e := r.Get("intent_override", id)
	previous := ""
	if e == nil {
		if prior.Owner != owner || prior.Visitor != visitor {
			return ErrUnavailable
		}
		old, err := readOverrideDecision(prior)
		if err != nil {
			return err
		}
		if now.Before(old.Created) {
			return fail(409, "服务器时间早于已有人工修正，请稍后重试")
		}
		if old.Revision == 0 {
			// Only the last legacy decision survives; do not invent its lost ancestors.
			clock.Revision++
			old.Revision, old.Legacy = clock.Revision, true
			previous = overrideDecisionID(owner, visitor, old.Revision)
			archived := record("intent_override_history", previous, owner, old.Created, old)
			archived.Visitor = visitor
			if e = r.Insert(archived); e != nil {
				return e
			}
			if e = saveOverrideBoundary(r, owner, visitor, old.Created, now); e != nil {
				return e
			}
		} else {
			if old.Revision > clock.Revision {
				return ErrUnavailable
			}
			previous = overrideDecisionID(owner, visitor, old.Revision)
			archived, err := r.Get("intent_override_history", previous)
			if err != nil || string(archived.Payload) != string(prior.Payload) || archived.Owner != owner || archived.Visitor != visitor {
				return ErrUnavailable
			}
		}
	} else if !errors.Is(e, ErrNotFound) {
		return e
	}
	clock.Revision++
	next := intentOverrideDecision{IntentOverride: IntentOverride{Created: now, Level: level, Reason: reason, Actor: actor, Expires: expires}, Revision: clock.Revision, Previous: previous}
	historyID := overrideDecisionID(owner, visitor, next.Revision)
	rec := record("intent_override_history", historyID, owner, now, next)
	rec.Visitor = visitor
	if e = r.Insert(rec); e != nil {
		return e
	}
	rec.Kind, rec.ID = "intent_override", id
	if e = r.Put(rec); e != nil {
		return e
	}
	if e = r.Put(record("intent_override_clock", privateKey(owner, "current"), owner, now, clock)); e != nil {
		return e
	}
	return audit(r, owner, "intent_override", actor, hash(visitor), now, map[string]any{"decision_id": historyID, "previous_decision_id": previous, "revision": next.Revision, "level": level, "decision_hash": overrideDecisionDigest(next)})
}

// A cursor freezes the transaction's owner-wide revision ceiling, so even
// same-clock writes cannot change its membership. It contains no visitor PII.
func loadIntentOverrides(r Repository, scope Scope, asOf time.Time, visitors []string, limit *int64) (map[string]Record, int64, error) {
	selected := map[string]Record{}
	if len(scope.Owners) != 1 {
		return selected, 0, nil
	}
	owner := scope.Owners[0]
	clock, e := overrideClock(r, owner)
	if e != nil {
		return nil, 0, e
	}
	ceiling := clock.Revision
	if limit != nil {
		if *limit < 0 || *limit > ceiling {
			return nil, 0, fail(409, "人工修正快照不可恢复，请刷新")
		}
		ceiling = *limit
	}
	q := RecordQuery{Owners: []string{owner}, VisitorKeys: visitors}
	boundaries, e := scopedRecords(r, "intent_override_boundary", q)
	if e != nil {
		return nil, 0, e
	}
	for _, row := range boundaries {
		var boundary intentOverrideBoundary
		if json.Unmarshal(row.Payload, &boundary) != nil || boundary.Before.IsZero() {
			return nil, 0, ErrUnavailable
		}
		if asOf.Before(boundary.Before) {
			return nil, 0, fail(409, "该访客的早期人工修正历史不可恢复，请刷新快照")
		}
	}
	current, e := scopedRecords(r, "intent_override", q)
	if e != nil {
		return nil, 0, e
	}
	required := map[string]int64{}
	// Legacy rows remain usable now; they cannot establish what preceded them.
	for _, row := range current {
		if row.ID != privateKey(owner, row.Visitor) {
			continue
		}
		d, err := readOverrideDecision(row)
		if err != nil {
			return nil, 0, err
		}
		if d.Revision > clock.Revision {
			return nil, 0, ErrUnavailable
		}
		if d.Revision != 0 {
			if !d.Created.After(asOf) && d.Revision <= ceiling {
				required[row.ID] = d.Revision
			}
			continue
		}
		if asOf.Before(d.Created) {
			return nil, 0, fail(409, "该访客的早期人工修正历史不可恢复，请刷新快照")
		}
		selected[row.ID] = row
	}
	q.AtThrough = asOf
	history, e := scopedRecords(r, "intent_override_history", q)
	if e != nil {
		return nil, 0, e
	}
	for _, row := range history {
		d, err := readOverrideDecision(row)
		if err != nil || d.Revision == 0 || d.Revision > clock.Revision || row.ID != overrideDecisionID(owner, row.Visitor, d.Revision) {
			return nil, 0, ErrUnavailable
		}
		if d.Created.After(asOf) || (!d.Legacy && d.Revision > ceiling) {
			continue
		}
		id := privateKey(owner, row.Visitor)
		if prior, ok := selected[id]; ok {
			old, err := readOverrideDecision(prior)
			if err != nil {
				return nil, 0, err
			}
			if old.Created.After(d.Created) || (old.Created.Equal(d.Created) && old.Revision >= d.Revision) {
				continue
			}
		}
		selected[id] = row
	}
	for id, revision := range required {
		row, ok := selected[id]
		if !ok {
			return nil, 0, ErrUnavailable
		}
		d, err := readOverrideDecision(row)
		if err != nil || d.Revision != revision {
			return nil, 0, ErrUnavailable
		}
	}
	return selected, ceiling, nil
}

type overrideCursor struct {
	Offset    int    `json:"offset"`
	Signature string `json:"signature"`
	Revision  *int64 `json:"intent_override_revision"`
}

func overrideCursorSignature(f Filter, revision int64) string {
	f.Cursor = ""
	return hash(string(marshal(struct {
		Filter   Filter
		Revision int64
	}{f, revision})))
}
func readOverrideCursor(f Filter) (*int64, error) {
	if f.Cursor == "" {
		return nil, nil
	}
	b, e := base64.RawURLEncoding.DecodeString(f.Cursor)
	var c overrideCursor
	if e != nil || json.Unmarshal(b, &c) != nil || c.Offset < 0 || c.Offset > 1000000 {
		return nil, fail(400, "游标无效")
	}
	if c.Revision == nil {
		return nil, fail(409, "人工修正快照游标已升级，请刷新")
	}
	if *c.Revision < 0 || c.Signature != overrideCursorSignature(f, *c.Revision) {
		return nil, fail(400, "游标与筛选不一致")
	}
	return c.Revision, nil
}
func overrideSnapshotCursor(f Filter, offset int, revision int64) string {
	return base64.RawURLEncoding.EncodeToString(marshal(overrideCursor{Offset: offset, Signature: overrideCursorSignature(f, revision), Revision: &revision}))
}
func paginateOverrideSnapshot(f Filter, total int, revision int64) (int, int, string, error) {
	if _, e := readOverrideCursor(f); e != nil {
		return 0, 0, "", e
	}
	start := 0
	if f.Cursor != "" {
		b, _ := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c overrideCursor
		_ = json.Unmarshal(b, &c)
		start = c.Offset
	}
	if start > total {
		start = total
	}
	end := start + f.Limit
	if end > total {
		end = total
	}
	cursor := ""
	if end < total {
		cursor = overrideSnapshotCursor(f, end, revision)
	}
	return start, end, cursor, nil
}

// Affected identities receive a retention boundary rather than fake historical
// no-override results. Boundaries follow the same owner-scoped privacy policy.
func prepareIntentOverrideRetention(r Repository, rows []Record, before, now time.Time) error {
	byID := map[string]Record{}
	for _, row := range rows {
		if (row.Kind == "intent_override" || row.Kind == "intent_override_history") && row.Visitor != "" {
			byID[privateKey(row.Owner, row.Visitor)] = row
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		row := byID[id]
		if e := saveOverrideBoundary(r, row.Owner, row.Visitor, before, now); e != nil {
			return e
		}
	}
	return nil
}
