package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type scopedQueryCall struct {
	kind  string
	query RecordQuery
}
type indexedTestStore struct {
	rows  reviewStore
	calls []scopedQueryCall
	t     *testing.T
}
type indexedTestRepo struct{ *indexedTestStore }

func (s *indexedTestStore) Transaction(_ context.Context, _ string, fn func(Repository) error) error {
	return fn(indexedTestRepo{s})
}
func (r indexedTestRepo) Get(kind, id string) (Record, error) { return r.rows.Get(kind, id) }
func (r indexedTestRepo) Put(row Record) error                { return r.rows.Put(row) }
func (r indexedTestRepo) Insert(row Record) error             { return r.rows.Insert(row) }
func (r indexedTestRepo) Remove(kind, id string) error        { return r.rows.Remove(kind, id) }
func (r indexedTestRepo) List(kind string) ([]Record, error) {
	return nil, fmt.Errorf("unscoped List(%q) used in query endpoint", kind)
}
func (r indexedTestRepo) ListScoped(kind string, q RecordQuery) ([]Record, error) {
	r.calls = append(r.calls, scopedQueryCall{kind, q})
	if q.Owners == nil {
		return nil, fmt.Errorf("missing owner predicate for %s", kind)
	}
	out := []Record{}
	for _, row := range r.rows {
		if row.Kind != kind {
			continue
		}
		match, err := matchesRecordQuery(r.rows, row, q)
		if err != nil {
			return nil, err
		}
		if match {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].ID < out[j].ID
		}
		return out[i].At.Before(out[j].At)
	})
	return out, nil
}
func querySeed(t *testing.T, r Repository, owner, visitor, session, content, view, id, kind string, at, received time.Time, delta int64) {
	t.Helper()
	e := Event{ID: id, Type: kind, Session: session, View: view, Content: content, Version: 1, Route: "page", Occurred: at, Received: received, Delta: delta}
	row := record("event", id, owner, at, e)
	row.Visitor, row.Session, row.Content = visitor, session, content
	if err := r.Put(row); err != nil {
		t.Fatal(err)
	}
}
func requireQueryError(t *testing.T, err error, status int) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Status != status {
		t.Fatalf("want status %d, got %v", status, err)
	}
}
func TestScopedQueryEndpointsMatchFallbackAcrossWindowAndIdentity(t *testing.T) {
	s, rows, scope, _ := reviewFixture()
	boundary := time.Date(2026, 10, 10, 16, 0, 0, 0, time.UTC) // Business midnight.
	asOf := boundary.Add(20 * time.Second)
	s.Now = func() time.Time { return asOf }
	for _, id := range []string{"material-a", "material-b"} {
		rows.Put(record("content", id, "one", boundary, Content{ID: id, Owner: "one", Title: id}))
	}
	// Both PVs precede the window. The B segment arrives after End and overlaps
	// A, so B receives 2 seconds, not 5. Keep all same-session views for dedup.
	querySeed(t, rows, "one", "a:device", "target-session", "material-a", "view-a", "pv-a", "page_view", boundary.Add(-10*time.Second), boundary.Add(-10*time.Second), 0)
	querySeed(t, rows, "one", "a:device", "target-session", "material-b", "view-b", "pv-b", "page_view", boundary.Add(-time.Second), boundary.Add(-time.Second), 0)
	querySeed(t, rows, "one", "a:device", "target-session", "material-a", "view-a", "seg-a", "visible_segment", boundary.Add(3*time.Second), boundary.Add(3*time.Second), 10000)
	querySeed(t, rows, "one", "u:42", "target-session", "material-b", "view-b", "seg-b", "visible_segment", boundary.Add(10*time.Second), boundary.Add(10*time.Second), 10000)
	// Received after as_of: cannot alter this snapshot.
	querySeed(t, rows, "one", "u:42", "target-session", "material-b", "view-b", "late-seg", "visible_segment", boundary.Add(2*time.Second), asOf.Add(time.Second), 2000)
	rows.Put(record("identity", "target-session", "one", boundary.Add(time.Second), map[string]string{"visitor_key": "u:42", "original_visitor_key": "a:device"}))
	querySeed(t, rows, "two", "u:42", "forbidden-session", "material-b", "forbidden-view", "forbidden-pv", "page_view", boundary.Add(time.Second), boundary.Add(time.Second), 0)
	querySeed(t, rows, "one", "u:42", "other-session", "material-a", "other-view", "other-pv", "page_view", boundary.Add(time.Second), boundary.Add(time.Second), 0)
	// Unrelated old corrupt payload must not enter a narrowed query.
	stale := record("event", "unrelated-old", "one", boundary.AddDate(0, 0, -60), nil)
	stale.Payload, stale.Session = []byte("broken"), "old-session"
	rows.Put(stale)
	f := Filter{Start: boundary, End: boundary.Add(5 * time.Second), AsOf: asOf, Content: "material-b", Related: true}
	calls := map[string]func(*Service) (List, error){
		"visitors": func(s *Service) (List, error) { return s.Visitors(context.Background(), scope, f) },
		"contents": func(s *Service) (List, error) { return s.Contents(context.Background(), scope, f, "material-b") },
		"sessions": func(s *Service) (List, error) { return s.Sessions(context.Background(), scope, "u:42", f) },
		"events":   func(s *Service) (List, error) { return s.Events(context.Background(), scope, "view-b", f) },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			s.Store = rows
			want, err := call(s)
			if err != nil {
				t.Fatal(err)
			}
			indexed := &indexedTestStore{rows: rows, t: t}
			s.Store = indexed
			got, err := call(s)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("indexed=%+v fallback=%+v", got, want)
			}
			if len(indexed.calls) == 0 {
				t.Fatal("no pushed query")
			}
			switch name {
			case "visitors":
				visitors := got.Items.([]Visitor)
				if len(visitors) != 1 || visitors[0].Key != "u:42" || visitors[0].Views != 0 || visitors[0].Visible != 2000 {
					t.Fatalf("%+v", visitors)
				}
			case "contents":
				contents := got.Items.([]Content)
				if len(contents) != 1 || contents[0].ID != "material-b" || contents[0].VisibleMS != 2000 || contents[0].PageViews != 0 {
					t.Fatalf("%+v", contents)
				}
			case "sessions":
				sessions := got.Items.([]Session)
				if len(sessions) != 1 || sessions[0].ID != "target-session" || len(sessions[0].Views) != 2 {
					t.Fatalf("%+v", sessions)
				}
			case "events":
				if len(got.Items.([]Event)) != 0 {
					t.Fatal("post-window / post-snapshot events exposed")
				}
			}
		})
	}
	// Identity is applied only after its receipt snapshot; linked anonymous
	// evidence can be found by identified visitor without matching another owner.
	s.Store = &indexedTestStore{rows: rows, t: t}
	_, err := s.Sessions(context.Background(), scope, "a:device", f)
	requireQueryError(t, err, 404)
	historical := Filter{Start: boundary.Add(-20 * time.Second), End: boundary, AsOf: boundary}
	got, err := s.Sessions(context.Background(), scope, "a:device", historical)
	if err != nil || len(got.Items.([]Session)) != 1 {
		t.Fatalf("historical identity %v %+v", err, got)
	}
	_, err = s.Sessions(context.Background(), scope, "u:42", historical)
	requireQueryError(t, err, 404)
}

func TestScopedExpiredSummariesDoNotResurrectPartialRawRows(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	at := now.AddDate(0, 0, -100)
	querySeed(t, rows, "one", "a:device", "expired-session", "material-a", "expired-view", "expired-pv", "page_view", at, at, 0)
	querySeed(t, rows, "one", "a:device", "expired-session", "material-a", "expired-view", "expired-seg", "visible_segment", at.Add(10*time.Second), at.Add(10*time.Second), 10000)
	raw, _ := rows.List("event")
	if err := s.PrepareSummaryRetention(rows, raw, now); err != nil {
		t.Fatal(err)
	}
	rows.Remove("event", "expired-pv") // A retained trailing raw segment must be excluded.
	rows.Put(record("identity", "expired-session", "one", at.Add(time.Hour), map[string]string{"visitor_key": "u:42", "original_visitor_key": "a:device"}))
	f := Filter{Start: at.Add(4 * time.Second), End: at.Add(7 * time.Second), AsOf: now, Content: "material-a"}
	s.Store = rows
	want, err := s.Visitors(context.Background(), scope, f)
	if err != nil {
		t.Fatal(err)
	}
	s.Store = &indexedTestStore{rows: rows, t: t}
	got, err := s.Visitors(context.Background(), scope, f)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || len(got.Items.([]Visitor)) != 1 || got.Items.([]Visitor)[0].Visible != 3000 {
		t.Fatalf("retained metrics: %+v %v", got, err)
	}
	_, err = s.Events(context.Background(), scope, "expired-view", f)
	requireQueryError(t, err, 410)
	f.AsOf = now.Add(-time.Minute)
	_, err = s.Visitors(context.Background(), scope, f)
	requireQueryError(t, err, 409)
	// Expiry is checked by session even when no retained bucket intersects.
	for id, row := range rows {
		if row.Kind == "daily_summary" {
			delete(rows, id)
		}
	}
	f.AsOf = now
	// Residual raw rows include a PV in this range: only summary_state can
	// prevent them from being incorrectly treated as a fresh live session.
	querySeed(t, rows, "one", "a:device", "expired-session", "material-a", "residual-view", "residual-pv", "page_view", at.Add(5*time.Second), at.Add(5*time.Second), 0)
	got, err = s.Visitors(context.Background(), scope, f)
	if err != nil || len(got.Items.([]Visitor)) != 0 {
		t.Fatalf("raw resurrection: %+v %v", got, err)
	}
}

func TestScopedQueryBudgetsFailClosedAcrossReads(t *testing.T) {
	t.Run("single scan", func(t *testing.T) {
		s, rows, scope, now := reviewFixture()
		for i := 0; i <= maxQueryRecords; i++ {
			id := fmt.Sprintf("content-%06d", i)
			rows.Put(record("content", id, "one", now, Content{ID: id, Owner: "one"}))
		}
		scope.View = false
		_, err := s.Contents(context.Background(), scope, Filter{}, "")
		requireQueryError(t, err, 422)
		// A known content can still be fetched without unrelated cards consuming budget.
		got, err := s.Contents(context.Background(), scope, Filter{}, "content-000001")
		if err != nil || len(got.Items.([]Content)) != 1 {
			t.Fatalf("narrow failed: %v %+v", err, got)
		}
	})
	t.Run("cumulative scans", func(t *testing.T) {
		s, rows, scope, now := reviewFixture()
		for i := 0; i < maxQueryRecords/2+1; i++ {
			id := fmt.Sprintf("content-%06d", i)
			rows.Put(record("content", id, "one", now, Content{ID: id, Owner: "one"}))
			rows.Put(record("legacy_catalog", "legacy-"+id, "one", now, Content{ID: "legacy-" + id, Owner: "one"}))
		}
		scope.View = false
		_, err := s.Contents(context.Background(), scope, Filter{}, "")
		requireQueryError(t, err, 422)
	})
	t.Run("cumulative payload", func(t *testing.T) {
		s, rows, scope, now := reviewFixture()
		body := strings.Repeat("x", maxQueryPayloadBytes/2)
		rows.Put(record("content", "large-a", "one", now, Content{ID: "large-a", Owner: "one", Body: body}))
		rows.Put(record("legacy_catalog", "large-b", "one", now, Content{ID: "large-b", Owner: "one", Body: body}))
		scope.View = false
		_, err := s.Contents(context.Background(), scope, Filter{}, "")
		requireQueryError(t, err, 422)
	})
	t.Run("ignored point lookup cannot hide budget failure", func(t *testing.T) {
		s, rows, _, now := reviewFixture()
		rows.Put(record("identity", "oversized", "one", now, strings.Repeat("x", maxQueryPayloadBytes)))
		err := s.queryTransaction(context.Background(), func(r Repository) error {
			_ = resolvedVisitorAt(r, Record{Session: "oversized", Visitor: "a:1"}, now)
			return nil
		})
		requireQueryError(t, err, 422)
	})
}

func TestScopedPredicatesAreParameterizedAndBounded(t *testing.T) {
	now := time.Date(2026, 10, 11, 0, 0, 0, 123, time.UTC)
	q := RecordQuery{Owners: []string{"owner' OR 1=1 --"}, Sessions: []string{"s"}, Content: "content", View: "pv", VisitorKeys: []string{"u:1"}, AtFrom: now, AtBefore: now.Add(time.Second), AtThrough: now, IdentityAsOf: now}
	sql, args := buildScopedQuery("tenant", "event", q, 23)
	if strings.Contains(sql, q.Owners[0]) || !strings.Contains(sql, "r.tenant_id=?") || !strings.Contains(sql, "r.owner_id IN (?)") || !strings.Contains(sql, "r.session_id IN (?)") || !strings.HasSuffix(sql, "LIMIT ?") || args[len(args)-1] != 23 {
		t.Fatalf("unsafe/unbounded query: %s %v", sql, args)
	}
	if strings.Count(sql, "?") != len(args) {
		t.Fatalf("placeholder mismatch: %s %v", sql, args)
	}
	if !strings.Contains(sql, "identity_link.tenant_id=r.tenant_id") || !strings.Contains(sql, "identity_link.record_id=r.session_id") || !strings.Contains(sql, "identity_link.occurred_at<=?") {
		t.Fatal("unscoped identity join")
	}
	if !strings.Contains(sql, "OCTET_LENGTH(r.payload)") {
		t.Fatal("single payload unbounded")
	}
	q.Owners = []string{}
	sql, _ = buildScopedQuery("tenant", "daily_summary", q, 1)
	if !strings.Contains(sql, "1=0") || !strings.Contains(sql, "$.page_view.page_view_id") || !strings.Contains(sql, "$.original_visitor_key") {
		t.Fatal(sql)
	}
}

func TestScopedEmptyOwnerNeverFallsBackToTenant(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	querySeed(t, rows, "one", "u:1", "s", "content", "pv", "e", "page_view", now, now, 0)
	s.Store = &indexedTestStore{rows: rows, t: t}
	got, err := s.Visitors(context.Background(), scope, Filter{Owner: "outside"})
	if err != nil || len(got.Items.([]Visitor)) != 0 {
		t.Fatalf("scope broadened: %+v %v", got, err)
	}
}

func TestScopedFallbackMicrosecondCandidateBounds(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	precise := now.Add(-time.Second).Add(123 * time.Nanosecond)
	querySeed(t, rows, "one", "u:1", "s", "", "v", "e", "page_view", precise, precise, 0)
	row, _ := rows.Get("event", "e")
	row.At = row.At.Truncate(time.Microsecond)
	rows.Put(row)
	got, err := s.Visitors(context.Background(), scope, Filter{Start: precise, End: precise.Add(time.Nanosecond), AsOf: now})
	if err != nil || len(got.Items.([]Visitor)) != 1 || got.Items.([]Visitor)[0].Views != 1 {
		t.Fatalf("microsecond omission: %+v %v", got, err)
	}
	// Exact received-at filtering stays in the fact layer, not a lexical JSON comparison.
	var e Event
	json.Unmarshal(row.Payload, &e)
	e.Received = now.Add(time.Nanosecond)
	row.Payload = marshal(e)
	rows.Put(row)
	got, err = s.Visitors(context.Background(), scope, Filter{Start: precise, AsOf: now})
	if err != nil || len(got.Items.([]Visitor)) != 0 {
		t.Fatalf("post-snapshot receipt: %+v %v", got, err)
	}
}

func TestScopedNarrowQueriesIgnoreLargeUnrelatedOwnerAndObject(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	for i := 0; i <= maxQueryRecords; i++ {
		id := fmt.Sprintf("noise-%06d", i)
		querySeed(t, rows, "one", "u:noise", id, "unrelated", id, id, "page_view", now.Add(-time.Minute), now.Add(-time.Minute), 0)
		forbidden := record("event", "forbidden-"+id, "two", now.Add(-time.Minute), nil)
		forbidden.Session, forbidden.Content, forbidden.Payload = "forbidden-"+id, "target", []byte("invalid outside scope")
		rows.Put(forbidden)
	}
	querySeed(t, rows, "one", "u:target", "target-session", "target", "target-view", "target-event", "page_view", now.Add(-time.Minute), now.Add(-time.Minute), 0)
	rows.Put(record("content", "target", "one", now, Content{ID: "target", Owner: "one"}))
	for _, indexed := range []bool{false, true} {
		if indexed {
			s.Store = &indexedTestStore{rows: rows, t: t}
		} else {
			s.Store = rows
		}
		f := Filter{Content: "target", AsOf: now}
		got, err := s.Sessions(context.Background(), scope, "u:target", f)
		if err != nil || len(got.Items.([]Session)) != 1 {
			t.Fatalf("indexed=%v session scope failed: %v %+v", indexed, err, got)
		}
		got, err = s.Contents(context.Background(), scope, f, "target")
		if err != nil || len(got.Items.([]Content)) != 1 || got.Items.([]Content)[0].PageViews != 1 {
			t.Fatalf("indexed=%v content scope failed: %v %+v", indexed, err, got)
		}
		got, err = s.Visitors(context.Background(), scope, f)
		if err != nil || len(got.Items.([]Visitor)) != 1 || got.Items.([]Visitor)[0].Key != "u:target" {
			t.Fatalf("indexed=%v visitor scope failed: %v %+v", indexed, err, got)
		}
	}
}

func TestScopedLegacyRelationshipSelectors(t *testing.T) {
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	rows := reviewStore{}
	rows.Put(record("identity", "linked-session", "one", at, map[string]string{"visitor_key": "u:42", "original_visitor_key": "a:device"}))
	cases := []struct {
		kind                                string
		value                               any
		q                                   RecordQuery
		sessionPath, visitorPath, sharePath string
	}{
		{"entry", Entry{Session: "linked-session", Visitor: "a:device"}, RecordQuery{Sessions: []string{"linked-session"}, VisitorKeys: []string{"u:42"}}, "$.session_id", "$.visitor_key", ""},
		{"session", Entry{Session: "linked-session", Visitor: "a:device"}, RecordQuery{Sessions: []string{"linked-session"}, VisitorKeys: []string{"u:42"}}, "$.session_id", "$.visitor_key", ""},
		{"identity", map[string]string{"visitor_key": "u:42", "original_visitor_key": "a:device"}, RecordQuery{Sessions: []string{"linked-session"}, VisitorKeys: []string{"u:42"}}, "r.record_id", "$.visitor_key", ""},
		{"share", Share{ID: "share-id", Sharer: "u:42"}, RecordQuery{VisitorKeys: []string{"u:42"}, Shares: []string{"share-id"}}, "", "$.sharer_id", "$.share_id"},
		{"share_request", map[string]any{"share_id": "share-id", "expires_at": at}, RecordQuery{Shares: []string{"share-id"}}, "", "", "$.share_id"},
		{"view_binding", map[string]string{"session": "linked-session"}, RecordQuery{Sessions: []string{"linked-session"}}, "$.session", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			row := record(tc.kind, "linked-session", "one", at, tc.value)
			// Deliberately leave all denormalized relationship columns blank.
			matched, err := matchesRecordQuery(rows, row, tc.q)
			if err != nil || !matched {
				t.Fatalf("JSON relationship missed: %v %v", matched, err)
			}
			query, args := buildScopedQuery("tenant", tc.kind, tc.q, 10)
			for _, path := range []string{tc.sessionPath, tc.visitorPath, tc.sharePath} {
				if path != "" && !strings.Contains(query, path) {
					t.Fatalf("missing %s: %s", path, query)
				}
			}
			if strings.Count(query, "?") != len(args) {
				t.Fatalf("placeholder mismatch %s %+v", query, args)
			}
			tc.q.Shares = []string{}
			if !queryHasNoRows(tc.q) {
				t.Fatal("empty share IDs widened")
			}
		})
	}
	// A missing identity link cannot hide a persisted identified projection from
	// deletion. Query endpoints still apply their exact historical resolution.
	summary := record("daily_summary", "summary", "one", at, DailySummary{OriginalVisitor: "a:unlinked", Visitor: "u:42"})
	summary.Visitor = "u:42"
	q := RecordQuery{VisitorKeys: []string{"u:42"}}
	matched, err := matchesRecordQuery(rows, summary, q)
	if err != nil || !matched {
		t.Fatal("persisted summary identity omitted")
	}
	query, args := buildScopedQuery("tenant", "daily_summary", q, 10)
	if !strings.Contains(query, "OR r.visitor_key IN (?)") || strings.Count(query, "?") != len(args) {
		t.Fatalf("summary selector mismatch: %s %+v", query, args)
	}
}

func TestFallbackFiltersUnrelatedLargeScalarObjectsBeforePayloadBudget(t *testing.T) {
	row := Record{Kind: "event", ID: "unrelated", Owner: "one", Session: "other-session", Content: "other-content", Share: "other-share", Payload: make([]byte, maxQueryPayloadBytes+1)}
	for _, query := range []RecordQuery{{Sessions: []string{"target-session"}}, {Content: "target-content"}, {Shares: []string{"target-share"}}} {
		match, e := matchesRecordQuery(nil, row, query)
		if e != nil || match {
			t.Fatalf("unrelated scalar object affected query: %v %v", match, e)
		}
	}
	if _, e := matchesRecordQuery(nil, row, RecordQuery{}); !errors.Is(e, ErrQueryTooLarge) {
		t.Fatalf("matching oversized payload not bounded: %v", e)
	}
}
