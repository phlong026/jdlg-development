package tracking

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// A request must fail, rather than compute a plausible partial aggregate, when
// its facts exceed this in-process safety envelope. These are safety limits,
// not a claim of tested production throughput.
const maxQueryRecords = 20000
const maxQueryPayloadBytes = 32 << 20

var ErrQueryTooLarge = fail(422, "查询范围过大，请缩小时间或归属、素材、访客范围")

// RecordQuery is server constructed. Non-nil empty slices select no records;
// nil slices impose no restriction. AtThrough is inclusive, AtBefore exclusive.
// VisitorKeys selects original OR snapshot-linked identities; the fact layer
// still resolves the exact identity before exposing any result.
type RecordQuery struct {
	Owners, IDs, Sessions, VisitorKeys, Shares []string
	Content, View                              string
	AtFrom, AtBefore, AtThrough, IdentityAsOf  time.Time
	CatalogAsOf                                time.Time // Immutable catalog membership; never mutable occurred_at.
	// WithoutExactSummary keeps only rows whose session lacks a complete
	// projection without deferred events, i.e. one that may hold facts in days
	// that have no projected bucket.
	WithoutExactSummary bool
}

type ScopedRecordLister interface {
	ListScoped(kind string, q RecordQuery) ([]Record, error)
}

type readBudget struct {
	rows, bytes int
	err         error
}

func (b *readBudget) reject() error { b.err = ErrQueryTooLarge; return b.err }
func (b *readBudget) consume(row Record) error {
	if b.err != nil {
		return b.err
	}
	b.rows++
	b.bytes += len(row.Payload)
	if b.rows > maxQueryRecords || b.bytes > maxQueryPayloadBytes {
		return b.reject()
	}
	return nil
}

// Query endpoints share one budget over their primary facts, enrichment facts,
// and point lookups. SQL repositories also apply it to non-query callers.
type queryRepository struct {
	Repository
	budget readBudget
}

func boundedQueryRepository(r Repository) Repository {
	if _, ok := r.(*queryRepository); ok {
		return r
	}
	if _, indexed := r.(ScopedRecordLister); !indexed {
		r = &memoryQuerySnapshot{Repository: r, lists: map[string][]Record{}}
	}
	return &queryRepository{Repository: r}
}
func (r *queryRepository) Get(kind, id string) (Record, error) {
	if r.budget.err != nil {
		return Record{}, r.budget.err
	}
	row, err := r.Repository.Get(kind, id)
	if err == nil {
		err = r.budget.consume(row)
	}
	if errors.Is(err, ErrQueryTooLarge) {
		r.budget.reject()
	}
	return row, err
}
func (r *queryRepository) List(kind string) ([]Record, error) {
	return r.ListScoped(kind, RecordQuery{})
}
func (r *queryRepository) ListSession(kind, session string) ([]Record, error) {
	return r.ListScoped(kind, RecordQuery{Sessions: []string{session}})
}
func (r *queryRepository) ListScoped(kind string, q RecordQuery) ([]Record, error) {
	if r.budget.err != nil {
		return nil, r.budget.err
	}
	rows, err := scopedRecords(r.Repository, kind, q)
	if errors.Is(err, ErrQueryTooLarge) {
		r.budget.reject()
	}
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err = r.budget.consume(row); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func includes(values []string, value string) bool {
	if values == nil {
		return true
	}
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func queryHasNoRows(q RecordQuery) bool {
	return q.Owners != nil && len(q.Owners) == 0 || q.IDs != nil && len(q.IDs) == 0 || q.Sessions != nil && len(q.Sessions) == 0 || q.VisitorKeys != nil && len(q.VisitorKeys) == 0 || q.Shares != nil && len(q.Shares) == 0
}

// The fallback is for small in-memory/test repositories. Production SQL uses
// ListScoped, so filtering happens before JSON rows enter Go memory.
func scopedRecords(r Repository, kind string, q RecordQuery) ([]Record, error) {
	if queryHasNoRows(q) {
		return []Record{}, nil
	}
	if indexed, ok := r.(ScopedRecordLister); ok {
		return indexed.ListScoped(kind, q)
	}
	rows, err := r.List(kind)
	if err != nil {
		return nil, err
	}
	out := []Record{}
	budget := readBudget{}
	for _, row := range rows {
		match, err := matchesRecordQuery(r, row, q)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}
		if err = budget.consume(row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}
func matchesRecordQuery(r Repository, row Record, q RecordQuery) (bool, error) {
	if !includes(q.Owners, row.Owner) || !includes(q.IDs, row.ID) {
		return false, nil
	}
	// SQL DATETIME(6) truncates sub-microsecond precision. Use equally broad
	// index bounds in memory; exact event and business-day clipping follows.
	if !q.AtFrom.IsZero() && row.At.Before(q.AtFrom.Truncate(time.Microsecond)) {
		return false, nil
	}
	if !q.AtBefore.IsZero() && !row.At.Before(ceilMicrosecond(q.AtBefore)) {
		return false, nil
	}
	if !q.AtThrough.IsZero() && row.At.Truncate(time.Microsecond).After(q.AtThrough.Truncate(time.Microsecond)) {
		return false, nil
	}
	// Apply authoritative scalar predicates before touching JSON. This keeps
	// unrelated large event payloads out of the fallback budget just as SQL WHERE
	// does. JSON-only legacy relationships remain bounded before decoding.
	if q.Content != "" && row.Kind != "content" && row.Kind != "legacy_catalog" && row.Content != q.Content {
		return false, nil
	}
	switch row.Kind {
	case "entry", "session", "share", "view_binding":
	case "identity":
		if !includes(q.Sessions, row.ID) {
			return false, nil
		}
	default:
		if !includes(q.Sessions, row.Session) {
			return false, nil
		}
	}
	if row.Kind != "share" && row.Kind != "share_request" && !includes(q.Shares, row.Share) {
		return false, nil
	}
	if len(row.Payload) > maxQueryPayloadBytes {
		return false, ErrQueryTooLarge
	}
	if !q.CatalogAsOf.IsZero() && (row.Kind == "content" || row.Kind == "legacy_catalog") {
		var c Content
		if json.Unmarshal(row.Payload, &c) != nil {
			return false, ErrUnavailable
		}
		if catalogCreatedAt(c, row.At).After(q.CatalogAsOf) {
			return false, nil
		}
	}
	original, session, share := row.Visitor, row.Session, row.Share
	// Legacy entry/share records did not populate their denormalized columns.
	// Select their established JSON fields, never silently skip dependencies.
	switch row.Kind {
	case "entry", "session":
		if q.VisitorKeys != nil || q.Sessions != nil {
			var en Entry
			if json.Unmarshal(row.Payload, &en) != nil {
				return false, ErrUnavailable
			}
			original, session = en.Visitor, en.Session
		}
	case "identity":
		session = row.ID
		if q.VisitorKeys != nil {
			var link map[string]string
			if json.Unmarshal(row.Payload, &link) != nil {
				return false, ErrUnavailable
			}
			original = link["visitor_key"]
		}
	case "share":
		if q.VisitorKeys != nil || q.Shares != nil || q.Sessions != nil {
			var sh Share
			if json.Unmarshal(row.Payload, &sh) != nil {
				return false, ErrUnavailable
			}
			original, share, session = sh.Sharer, sh.ID, sh.Session
		}
	case "view_binding":
		if q.Sessions != nil {
			var binding map[string]string
			if json.Unmarshal(row.Payload, &binding) != nil {
				return false, ErrUnavailable
			}
			session = binding["session"]
		}
	case "share_request":
		if q.Shares != nil {
			var request map[string]json.RawMessage
			if json.Unmarshal(row.Payload, &request) != nil {
				return false, ErrUnavailable
			}
			if b, ok := request["share_id"]; ok && json.Unmarshal(b, &share) != nil {
				return false, ErrUnavailable
			}
		}
	}
	if !includes(q.Sessions, session) || !includes(q.Shares, share) {
		return false, nil
	}
	content := row.Content
	view := ""
	if (row.Kind == "content" || row.Kind == "legacy_catalog") && q.Content != "" {
		var c Content
		if json.Unmarshal(row.Payload, &c) != nil {
			return false, ErrUnavailable
		}
		content = c.ID
	}
	if row.Kind == "daily_summary" && (q.View != "" || q.VisitorKeys != nil) {
		var x DailySummary
		if json.Unmarshal(row.Payload, &x) != nil {
			return false, ErrUnavailable
		}
		original, view = x.OriginalVisitor, x.View.ID
	}
	if row.Kind == "event" && q.View != "" {
		var x Event
		if json.Unmarshal(row.Payload, &x) != nil {
			return false, ErrUnavailable
		}
		view = x.View
	}
	if q.Content != "" && content != q.Content || q.View != "" && view != q.View {
		return false, nil
	}
	if q.VisitorKeys != nil && !includes(q.VisitorKeys, original) && !(row.Kind == "daily_summary" && includes(q.VisitorKeys, row.Visitor)) {
		rec, err := r.Get("identity", session)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !q.IdentityAsOf.IsZero() && rec.At.After(q.IdentityAsOf) {
			return false, nil
		}
		var link map[string]string
		if json.Unmarshal(rec.Payload, &link) != nil {
			return false, ErrUnavailable
		}
		if link["original_visitor_key"] != original || !strings.HasPrefix(link["visitor_key"], "u:") || !includes(q.VisitorKeys, link["visitor_key"]) {
			return false, nil
		}
	}
	if q.WithoutExactSummary {
		state, err := decode[summaryState](r, "summary_state", session)
		if err == nil && state.Complete && state.DeferredFrom == nil {
			return false, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return false, err
		}
	}
	return true, nil
}
func ceilMicrosecond(t time.Time) time.Time {
	x := t.Truncate(time.Microsecond)
	if x.Before(t) {
		x = x.Add(time.Microsecond)
	}
	return x
}
func permittedOwners(scope Scope, owner string) []string {
	out := []string{}
	for _, o := range scope.Owners {
		if owner == "" || o == owner {
			out = append(out, o)
		}
	}
	return out
}

// A non-SQL fallback takes one stable in-memory snapshot per kind per request,
// rather than repeatedly invoking an unindexed full List for enrichment.
type memoryQuerySnapshot struct {
	Repository
	lists map[string][]Record
}

func (r *memoryQuerySnapshot) List(kind string) ([]Record, error) {
	if rows, ok := r.lists[kind]; ok {
		return rows, nil
	}
	rows, err := r.Repository.List(kind)
	if err == nil {
		r.lists[kind] = rows
	}
	return rows, err
}

func (r *memoryQuerySnapshot) Put(row Record) error {
	err := r.Repository.Put(row)
	if err == nil {
		delete(r.lists, row.Kind)
	}
	return err
}
func (r *memoryQuerySnapshot) Insert(row Record) error {
	err := r.Repository.Insert(row)
	if err == nil {
		delete(r.lists, row.Kind)
	}
	return err
}
func (r *memoryQuerySnapshot) Remove(kind, id string) error {
	err := r.Repository.Remove(kind, id)
	if err == nil {
		delete(r.lists, kind)
	}
	return err
}
