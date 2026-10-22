package tracking

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type countedStore struct {
	*memoryStore
	writes int
}

func (s *countedStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	s.writes++
	return s.memoryStore.Transaction(ctx, tenant, fn)
}

func seedDeferredSummary(t *testing.T, s *Service, owner, session string, eventAt, deferredAt time.Time, rawExpired bool) {
	t.Helper()
	if err := s.transaction(context.Background(), func(r Repository) error {
		aggregateSeed(t, r, owner, "u:1", session, "view-"+session, "pv-"+session, "page_view", eventAt, 0)
		state := summaryState{Metric: MetricVersion, Complete: true, BuiltAt: eventAt, DeferredFrom: &deferredAt, RawExpired: rawExpired}
		row := record("summary_state", session, owner, eventAt, state)
		row.Session, row.Visitor = session, "u:1"
		return r.Put(row)
	}); err != nil {
		t.Fatal(err)
	}
}

func readSummaryState(t *testing.T, s *Service, session string) summaryState {
	t.Helper()
	var state summaryState
	if err := s.readTransaction(context.Background(), func(r Repository) error {
		var err error
		state, err = decode[summaryState](r, "summary_state", session)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestSummaryReconcileDryRunAndApplyOnlyDueAuthorizedStates(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	store := &countedStore{memoryStore: s.Store.(*memoryStore)}
	s.Store = store

	seedDeferredSummary(t, s, "owner-a", "session-due", now.Add(-time.Hour), now.Add(-time.Minute), false)
	seedDeferredSummary(t, s, "owner-a", "session-future", now.Add(-time.Hour), now.Add(time.Minute), false)
	seedDeferredSummary(t, s, "owner-a", "session-expired", now.Add(-time.Hour), now.Add(-time.Minute), true)
	seedDeferredSummary(t, s, "owner-b", "session-other-owner", now.Add(-time.Hour), now.Add(-time.Minute), false)
	seedWrites := store.writes

	dry, err := s.ReconcileDeferredSummaries(context.Background(), scope, SummaryReconcileOptions{AsOf: now, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Candidates != 1 || dry.Rebuilt != 0 || len(dry.Sessions) != 1 || dry.Sessions[0] != "session-due" {
		t.Fatalf("dry run = %+v", dry)
	}
	if store.writes != seedWrites {
		t.Fatalf("dry run opened %d write transactions", store.writes-seedWrites)
	}
	if state := readSummaryState(t, s, "session-due"); state.DeferredFrom == nil {
		t.Fatal("dry run changed deferred state")
	}

	applied, err := s.ReconcileDeferredSummaries(context.Background(), scope, SummaryReconcileOptions{AsOf: now, Limit: 20, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if applied.Rebuilt != 1 || applied.Skipped != 0 || applied.Failed != 0 {
		t.Fatalf("apply = %+v", applied)
	}
	if got := store.writes - seedWrites; got != 1 {
		t.Fatalf("write transactions = %d, want one per due session", got)
	}
	if state := readSummaryState(t, s, "session-due"); state.DeferredFrom != nil {
		t.Fatalf("due state remains deferred: %+v", state)
	}
	if state := readSummaryState(t, s, "session-future"); state.DeferredFrom == nil || !state.DeferredFrom.Equal(now.Add(time.Minute)) {
		t.Fatalf("future state changed: %+v", state)
	}
	if state := readSummaryState(t, s, "session-expired"); !state.RawExpired || state.DeferredFrom == nil {
		t.Fatalf("raw-expired state changed: %+v", state)
	}
	if state := readSummaryState(t, s, "session-other-owner"); state.DeferredFrom == nil {
		t.Fatalf("other owner state changed: %+v", state)
	}
}

func TestSummaryReconcileBoundsEachSessionTransaction(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	store := &countedStore{memoryStore: s.Store.(*memoryStore)}
	s.Store = store

	seedDeferredSummary(t, s, "owner-a", "session-large", now.Add(-time.Hour), now.Add(-time.Minute), false)
	seedDeferredSummary(t, s, "owner-a", "session-small", now.Add(-time.Hour), now.Add(-time.Minute), false)
	if err := s.transaction(context.Background(), func(r Repository) error {
		for i := 0; i < maxQueryRecords; i++ {
			at := now.Add(-time.Hour).Add(time.Duration(i+1) * time.Nanosecond)
			ev := Event{ID: fmt.Sprintf("large-%05d", i), Type: "visible_segment", Session: "session-large", View: "view-session-large", Route: "pages/home/home", Occurred: at, Received: at, Delta: 1}
			row := record("event", ev.ID, "owner-a", at, ev)
			row.Session, row.Visitor = ev.Session, "u:1"
			if err := r.Put(row); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	seedWrites := store.writes

	result, err := s.ReconcileDeferredSummaries(context.Background(), scope, SummaryReconcileOptions{AsOf: now, Limit: 20, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Rebuilt != 1 || result.Failed != 1 || len(result.FailedSessions) != 1 || result.FailedSessions[0] != "session-large" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Failures) != 1 || result.Failures[0].Session != "session-large" || result.Failures[0].Code != "query_too_large" || result.Failures[0].Error == "" {
		t.Fatalf("failures = %+v", result.Failures)
	}
	if got := store.writes - seedWrites; got != 2 {
		t.Fatalf("write transactions = %d, want one per session", got)
	}
	if state := readSummaryState(t, s, "session-large"); state.DeferredFrom == nil {
		t.Fatal("failed large session committed")
	}
	if state := readSummaryState(t, s, "session-small"); state.DeferredFrom != nil {
		t.Fatal("small session was blocked by large session failure")
	}
}

func TestSummaryReconcileRequiresManagementRuleScope(t *testing.T) {
	s, scope := fixture(t)
	seedDeferredSummary(t, s, "owner-a", "session-due", s.Now().Add(-time.Hour), s.Now().Add(-time.Minute), false)
	for _, mutate := range []func(*Scope){
		func(scope *Scope) { scope.Manage = false },
		func(scope *Scope) { scope.Rules = false },
		func(scope *Scope) { scope.Owners = []string{"owner-b"} },
	} {
		candidate := scope
		mutate(&candidate)
		result, err := s.ReconcileDeferredSummaries(context.Background(), candidate, SummaryReconcileOptions{AsOf: s.Now(), Limit: 20, Apply: true})
		if !candidate.Manage || !candidate.Rules {
			if err == nil {
				t.Fatalf("unauthorized reconcile succeeded: %+v", result)
			}
			continue
		}
		if err != nil || result.Candidates != 0 || result.Rebuilt != 0 {
			t.Fatalf("other-owner reconcile = %+v, %v", result, err)
		}
	}
	if state := readSummaryState(t, s, "session-due"); state.DeferredFrom == nil {
		t.Fatal("unauthorized scope changed state")
	}
}

func TestSummaryReconcileRejectsInvalidCursorAndFutureSnapshot(t *testing.T) {
	s, scope := fixture(t)
	for _, options := range []SummaryReconcileOptions{
		{AsOf: s.Now().Add(time.Second), Limit: 20},
		{AsOf: s.Now(), Limit: -1},
		{AsOf: s.Now(), Limit: maxSummaryReconcilePage + 1},
		{AsOf: s.Now(), Limit: 20, AfterID: "bad cursor"},
	} {
		if _, err := s.ReconcileDeferredSummaries(context.Background(), scope, options); err == nil {
			t.Fatalf("invalid options accepted: %+v", options)
		}
	}
}
