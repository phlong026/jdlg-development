package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func withDeletionPages(t *testing.T, rows, sessions int) {
	t.Helper()
	oldRows, oldSessions := deletionPageRows, deletionSessionPage
	deletionPageRows, deletionSessionPage = rows, sessions
	t.Cleanup(func() { deletionPageRows, deletionSessionPage = oldRows, oldSessions })
}

func withoutAudit(rows reviewStore) reviewStore {
	out := reviewStore{}
	for key, row := range rows {
		if row.Kind != "audit" {
			out[key] = row
		}
	}
	return out
}

func auditActions(t *testing.T, rows reviewStore) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, row := range rows {
		if row.Kind != "audit" {
			continue
		}
		var detail struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(row.Payload, &detail); err != nil {
			t.Fatal(err)
		}
		counts[detail.Action]++
	}
	return counts
}

func TestStagedDeletionMatchesAtomicDeletion(t *testing.T) {
	s, indexed, scope, now := privacyFixture(t)
	privacySeedGraph(t, indexed.rows, now)
	if err := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); err != nil {
		t.Fatal(err)
	}
	want := withoutAudit(indexed.rows)
	for _, page := range []int{1, 2, 3, 500} {
		t.Run(fmt.Sprintf("page-%d", page), func(t *testing.T) {
			withDeletionPages(t, page, 1+page%2)
			s, indexed, scope, now := privacyFixture(t)
			privacySeedGraph(t, indexed.rows, now)
			s.forceStagedDeletion = true
			if err := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); err != nil {
				t.Fatal(err)
			}
			if got := withoutAudit(indexed.rows); !reflect.DeepEqual(got, want) {
				for key := range want {
					if _, ok := got[key]; !ok {
						t.Errorf("staged deletion removed %s", key)
					}
				}
				for key := range got {
					if _, ok := want[key]; !ok {
						t.Errorf("staged deletion kept %s", key)
					}
				}
				t.Fatal("staged and atomic deletion differ")
			}
			if actions := auditActions(t, indexed.rows); actions["delete_visitor_started"] != 1 || actions["delete_visitor"] != 1 {
				t.Fatalf("audit trail: %v", actions)
			}
		})
	}
}

func TestStagedDeletionBeyondReadBudgetCompletes(t *testing.T) {
	s, indexed, scope, now := privacyFixture(t)
	privacySeedGraph(t, indexed.rows, now)
	for i := 0; i <= maxQueryRecords/2; i++ {
		reviewPV(indexed.rows, fmt.Sprintf("big-%06d", i), "one", "u:1", "big-session", "", now)
	}
	// The atomic plan cannot hold this graph.
	if err := s.deleteVisitorAtomic(context.Background(), scope, "u:1", "request", false); !errors.Is(err, ErrQueryTooLarge) {
		t.Fatalf("fixture too small to exceed the budget: %v", err)
	}
	done, err := s.DeleteVisitorProgress(context.Background(), scope, "u:1", "request", 0)
	if err != nil || !done {
		t.Fatalf("deletion beyond the read budget failed: done=%v err=%v", done, err)
	}
	for id, row := range indexed.rows {
		if row.Kind == "event" && strings.HasPrefix(row.ID, "big-") || row.Kind == "event" && row.ID == "target-view" {
			t.Fatalf("target event retained: %s", id)
		}
	}
	for _, session := range []string{"big-session", "linked-session", "direct-session"} {
		if _, err := indexed.rows.Get("deletion_session", session); err != nil {
			t.Errorf("missing session tombstone %s", session)
		}
	}
	mark, exists, err := readDeletionMark(indexed.rows, "u:1")
	if err != nil || !exists || mark.InProgress {
		t.Fatalf("deletion not marked complete: %+v %v %v", mark, exists, err)
	}
	if _, err := indexed.rows.Get("event", "unrelated-view"); err != nil {
		t.Fatal("unrelated data removed")
	}
	if actions := auditActions(t, indexed.rows); actions["delete_visitor_started"] != 1 || actions["delete_visitor"] != 1 {
		t.Fatalf("audit trail: %v", actions)
	}
	// A finished deletion answers like an unknown visitor and writes nothing, so
	// a caller cannot learn that the account was deleted.
	before := privacyCopy(indexed.rows)
	done, err = s.DeleteVisitorProgress(context.Background(), scope, "u:1", "request", 0)
	requireQueryError(t, err, 404)
	if done || !reflect.DeepEqual(indexed.rows, before) {
		t.Fatal("repeating a completed deletion changed data or reported success")
	}
}

func TestStagedDeletionRefusesUncoveredOwnerBeforeAnyMutation(t *testing.T) {
	s, indexed, scope, now := privacyFixture(t)
	privacySeedGraph(t, indexed.rows, now)
	for i := 0; i <= maxQueryRecords/2; i++ {
		reviewPV(indexed.rows, fmt.Sprintf("big-%06d", i), "one", "u:1", "big-session", "", now)
	}
	reviewPV(indexed.rows, "foreign-view", "two", "u:1", "foreign-session", "", now)
	before := privacyCopy(indexed.rows)
	done, err := s.DeleteVisitorProgress(context.Background(), scope, "u:1", "request", 0)
	requireQueryError(t, err, 403)
	if done || !reflect.DeepEqual(indexed.rows, before) {
		t.Fatal("owner rejection mutated data or wrote tombstones")
	}
}

func TestStagedDeletionKeepsOtherAccountsSessions(t *testing.T) {
	withDeletionPages(t, 2, 1)
	s, indexed, scope, now := privacyFixture(t)
	reviewPV(indexed.rows, "anonymous-view", "one", "a:device", "anonymous-session", "", now)
	reviewPV(indexed.rows, "identified-view", "one", "a:device", "identified-session", "", now)
	indexed.rows.Put(record("identity", "identified-session", "one", now, map[string]string{"visitor_key": "u:2", "original_visitor_key": "a:device"}))
	s.forceStagedDeletion = true
	if err := s.DeleteVisitor(context.Background(), scope, "a:device", "request"); err != nil {
		t.Fatal(err)
	}
	if _, err := indexed.rows.Get("event", "anonymous-view"); !errors.Is(err, ErrNotFound) {
		t.Fatal("anonymous session retained")
	}
	if _, err := indexed.rows.Get("event", "identified-view"); err != nil {
		t.Fatal("identified account on shared device was deleted")
	}
	if _, err := indexed.rows.Get("identity", "identified-session"); err != nil {
		t.Fatal("other account identity link was deleted")
	}
	if _, err := indexed.rows.Get("deletion_session", "identified-session"); !errors.Is(err, ErrNotFound) {
		t.Fatal("other account session was tombstoned")
	}
}

// A run interrupted after the events are gone must still reach everything: the
// identity link that joins the anonymous session to the account is removed last.
func TestStagedDeletionInterruptedBeforeIdentityResumesToSameResult(t *testing.T) {
	withDeletionPages(t, 2, 1)
	seed := func() (*Service, *privacyScopedStore, Scope) {
		s, indexed, scope, now := privacyFixture(t)
		privacySeedGraph(t, indexed.rows, now)
		for i := 0; i < 5; i++ {
			reviewPV(indexed.rows, fmt.Sprintf("linked-extra-%d", i), "one", "a:device", "linked-session", "", now)
		}
		s.forceStagedDeletion = true
		return s, indexed, scope
	}
	s, reference, scope := seed()
	if err := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); err != nil {
		t.Fatal(err)
	}
	want := withoutAudit(reference.rows)

	s, indexed, scope := seed()
	indexed.failRemove = "identity"
	done, err := s.DeleteVisitorProgress(context.Background(), scope, "u:1", "request", 0)
	if !errors.Is(err, ErrUnavailable) || done {
		t.Fatalf("interruption was hidden: done=%v err=%v", done, err)
	}
	mark, exists, _ := readDeletionMark(indexed.rows, "u:1")
	if !exists || !mark.InProgress {
		t.Fatalf("interrupted deletion must stay marked in progress: %+v", mark)
	}
	if _, err := indexed.rows.Get("identity", "linked-session"); err != nil {
		t.Fatal("identity link was removed before the events it explains")
	}
	for id, row := range indexed.rows {
		if row.Kind == "event" && strings.HasPrefix(row.ID, "linked-extra-") || row.ID == "target-view" && row.Kind == "event" {
			t.Fatalf("linked anonymous event survived before the failure point: %s", id)
		}
	}
	indexed.failRemove = ""
	if done, err = s.DeleteVisitorProgress(context.Background(), scope, "u:1", "request", 0); err != nil || !done {
		t.Fatalf("resume failed: done=%v err=%v", done, err)
	}
	if got := withoutAudit(indexed.rows); !reflect.DeepEqual(got, want) {
		for key, row := range want {
			if other, ok := got[key]; !ok {
				t.Errorf("resumed deletion removed %s", key)
			} else if !reflect.DeepEqual(row, other) {
				t.Errorf("resumed deletion changed %s: %s vs %s", key, row.Payload, other.Payload)
			}
		}
		for key := range got {
			if _, ok := want[key]; !ok {
				t.Errorf("resumed deletion kept %s", key)
			}
		}
		t.Fatal("resumed deletion differs from an uninterrupted one")
	}
}

func TestStagedDeletionPagesAreResumableAndHideDataImmediately(t *testing.T) {
	withDeletionPages(t, 2, 1)
	s, scope := fixture(t)
	s.forceStagedDeletion = true
	var tokens []string
	for i := 0; i < 3; i++ {
		en, err := s.Enter(context.Background(), EntryInput{Route: "pages/home/home", Client: fmt.Sprintf("client-%07d", i), Anonymous: fmt.Sprintf("anonymous-%04d", i), Consent: "v1"}, 1)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, en.Token)
		for k := 0; k < 4; k++ {
			e := ev(s, en, fmt.Sprintf("staged-event-%d-%d", i, k), "page_view", s.Now())
			e.View = fmt.Sprintf("staged-view-%d", i)
			if k > 0 {
				e.Type, e.Segment, e.Seq, e.Delta = "visible_segment", fmt.Sprintf("segment-%07d", i), k, 1000
			}
			accept(t, s, en, e)
		}
	}
	before, err := s.Visitors(context.Background(), scope, Filter{})
	if err != nil || before.Identified != 1 {
		t.Fatalf("fixture: %+v %v", before, err)
	}
	done, err := s.DeleteVisitorProgress(context.Background(), scope, "u:1", "request", 1)
	if err != nil || done {
		t.Fatalf("one page cannot finish this graph: done=%v err=%v", done, err)
	}
	// Mid-way the data is already unreadable and the account cannot add more.
	mid, err := s.Visitors(context.Background(), scope, Filter{})
	if err != nil || mid.Identified != 0 {
		t.Fatalf("partially deleted visitor still listed: %+v %v", mid, err)
	}
	if _, err = s.Enter(context.Background(), EntryInput{Route: "pages/home/home", Client: "client-late-01", Anonymous: "anonymous-late", Consent: "v1"}, 1); err == nil {
		t.Fatal("deleted account created a new session during deletion")
	}
	if _, err = s.Accept(context.Background(), tokens[0], []Event{{Schema: 1, ID: "late-event-0001", Type: "page_end", Session: "x"}}); err == nil {
		t.Fatal("deleted session accepted new events during deletion")
	}
	for round := 0; !done; round++ {
		if round > 100 {
			t.Fatal("deletion never finished")
		}
		if done, err = s.DeleteVisitorProgress(context.Background(), scope, "u:1", "request", 1); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.readTransaction(context.Background(), func(r Repository) error {
		for _, kind := range []string{"event", "event_hash", "sequence", "view_binding", "entry", "session", "daily_summary", "summary_state"} {
			rows, _ := r.List(kind)
			if len(rows) != 0 {
				t.Errorf("%s rows remain: %d", kind, len(rows))
			}
		}
		mark, exists, e := readDeletionMark(r, "u:1")
		if e != nil || !exists || mark.InProgress {
			t.Errorf("not marked complete: %+v %v %v", mark, exists, e)
		}
		return nil
	})
}

func TestPageRecordsOrdersResumesAndTruncatesWithoutRepositoryHelp(t *testing.T) {
	base := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	rows := reviewStore{}
	for i := 0; i < 7; i++ {
		// Duplicate timestamps force the record_id tie-break.
		_ = rows.Put(record("event", fmt.Sprintf("row-%d", 6-i), "one", base.Add(time.Duration(i/2)*time.Second), Event{}))
	}
	seen := []string{}
	q := RecordQuery{Limit: 3}
	for {
		page, err := pageRecords(rows, "event", q)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range page {
			seen = append(seen, row.ID)
		}
		if len(page) < q.Limit {
			break
		}
		q = cursorAfter(page, q)
	}
	if len(seen) != 7 {
		t.Fatalf("paging lost or repeated rows: %v", seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] == seen[i-1] {
			t.Fatalf("repeated row: %v", seen)
		}
	}
}

func TestScopedQuerySQLCursorAndLimit(t *testing.T) {
	at := time.Date(2026, 10, 11, 1, 2, 3, 456789000, time.UTC)
	query, args := buildScopedQuery("tenant", "event", RecordQuery{VisitorKeys: []string{"u:1"}, Limit: 50, AfterAt: at, AfterID: "row-9"}, maxQueryRecords+1)
	for _, want := range []string{"(r.occurred_at>? OR (r.occurred_at=? AND r.record_id>?))", "ORDER BY r.occurred_at,r.record_id LIMIT ?"} {
		if !strings.Contains(query, want) {
			t.Fatalf("missing %q in %s", want, query)
		}
	}
	if strings.Count(query, "?") != len(args) {
		t.Fatalf("%d placeholders for %d args", strings.Count(query, "?"), len(args))
	}
	if got := args[len(args)-1]; got != 50 {
		t.Fatalf("limit not capped to the page size: %v", got)
	}
	if _, a := buildScopedQuery("tenant", "event", RecordQuery{VisitorKeys: []string{"u:1"}}, 123); a[len(a)-1] != 123 {
		t.Fatal("budget limit lost when no page limit is set")
	}
}

// Whatever step is interrupted, identity links must still exist: they are what
// connects a formerly anonymous session's rows to the account.
func TestStagedDeletionKeepsIdentityLinkUntilEveryOtherRowIsGone(t *testing.T) {
	withDeletionPages(t, 2, 1)
	seed := func() (*Service, *privacyScopedStore, Scope) {
		s, indexed, scope, now := privacyFixture(t)
		privacySeedGraph(t, indexed.rows, now)
		for i := 0; i < 5; i++ {
			reviewPV(indexed.rows, fmt.Sprintf("linked-extra-%d", i), "one", "a:device", "linked-session", "", now)
		}
		s.forceStagedDeletion = true
		return s, indexed, scope
	}
	s, reference, scope := seed()
	if err := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); err != nil {
		t.Fatal(err)
	}
	want := withoutAudit(reference.rows)
	interrupted := 0
	for _, kind := range deletionSessionKinds {
		if kind == "identity" {
			continue
		}
		t.Run(kind, func(t *testing.T) {
			s, indexed, scope := seed()
			indexed.failRemove = kind
			done, err := s.DeleteVisitorProgress(context.Background(), scope, "u:1", "request", 0)
			if err == nil {
				if !done {
					t.Fatal("neither finished nor failed")
				}
				return // no row of this kind in the graph
			}
			interrupted++
			if _, e := indexed.rows.Get("identity", "linked-session"); e != nil {
				t.Fatalf("identity link removed while %s rows remain", kind)
			}
			indexed.failRemove = ""
			if done, err = s.DeleteVisitorProgress(context.Background(), scope, "u:1", "request", 0); err != nil || !done {
				t.Fatalf("resume: done=%v err=%v", done, err)
			}
			if got := withoutAudit(indexed.rows); !reflect.DeepEqual(got, want) {
				t.Fatalf("resumed deletion after interrupting %s differs from an uninterrupted one", kind)
			}
		})
	}
	if interrupted < 5 {
		t.Fatalf("only %d kinds were interrupted; the fixture does not exercise the ordering", interrupted)
	}
}

// Evidence that a legacy share belongs to the account is judged against every
// session being deleted, even when its events land on different pages.
func TestHistoricalShareDeletionAcrossLinkedSessionsAtomicAndStaged(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(fmt.Sprintf("staged=%v", staged), func(t *testing.T) {
			withDeletionPages(t, 1, 1)
			s, scope := fixture(t)
			s.forceStagedDeletion = staged
			ctx := context.Background()
			en := enter(t, s, 0)
			other := enter(t, s, 0)
			sh := lineageShare(t, s, en, "share-legacy-002")
			legacyShareWithoutSession(t, s, sh)
			lineageIntent(t, s, en, sh, "share-intent-0a1")
			lineageIntent(t, s, en, sh, "share-intent-0a2")
			// A second session of the same device logs into the same account, so
			// its intent event is consistent evidence, not a conflict.
			second := ev(s, other, "share-intent-0b1", "share_intent", s.Now())
			second.Share = sh["share_id"].(string)
			if res := accept(t, s, other, second); res[0].Status != "accepted" {
				t.Fatalf("legacy share evidence from the second session was not recorded: %v", res)
			}
			lineageLogin(t, s, en)
			lineageLogin(t, s, other)
			if e := s.DeleteVisitor(ctx, scope, "u:42", "requested deletion"); e != nil {
				t.Fatal(e)
			}
			if _, e := s.ResolveShare(ctx, sh["share_token"].(string)); e == nil {
				t.Fatal("legacy share of the deleted account survived")
			}
		})
	}
}
