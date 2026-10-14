package tracking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Five-minute visits produce 32 raw events each. Raw list reduction reads the
// window's events and then their full sessions, so 340 visits need about
// 21,760 rows, over the 20,000-row budget. Projections need a few rows per visit.
func TestVisitorListUsesProjectionsWithinReadBudget(t *testing.T) {
	s, scope := fixture(t)
	start := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	clock := start
	s.Now = func() time.Time { return clock }
	const visits = 340
	for i := 0; i < visits; i++ {
		clock = start.Add(time.Duration(i) * 20 * time.Minute)
		en, err := s.Enter(context.Background(), EntryInput{Route: "pages/home/home", Client: fmt.Sprintf("client-%06d", i), Anonymous: fmt.Sprintf("anon-%06d", i), Consent: "v1"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		view := fmt.Sprintf("view-%06d", i)
		mk := func(id, typ string, at time.Time) Event {
			return Event{Schema: 1, ID: id, Type: typ, Session: en.Session, View: view, Route: en.Route, Occurred: at, AppState: "foreground", PageState: "visible", Monotonic: 400000}
		}
		events := []Event{mk(fmt.Sprintf("pv-%06d", i), "page_view", clock)}
		for k := 1; k <= 30; k++ {
			x := mk(fmt.Sprintf("seg-%06d-%02d", i, k), "visible_segment", clock.Add(time.Duration(k)*10*time.Second))
			x.Segment, x.Seq, x.Delta = fmt.Sprintf("segment-%06d", i), k, 10000
			events = append(events, x)
		}
		events = append(events, mk(fmt.Sprintf("end-%06d", i), "page_end", clock.Add(301*time.Second)))
		clock = clock.Add(302 * time.Second)
		for len(events) > 0 {
			n := 50
			if n > len(events) {
				n = len(events)
			}
			if _, err := s.Accept(context.Background(), en.Token, events[:n]); err != nil {
				t.Fatal(err)
			}
			events = events[n:]
		}
	}
	clock = clock.Add(time.Minute)
	f := Filter{Start: businessDay(start), End: businessDay(clock).AddDate(0, 0, 1)}

	// The raw reduction alone cannot serve this window.
	normalized, err := s.Normalize(f)
	if err != nil {
		t.Fatal(err)
	}
	err = s.readQueryTransaction(context.Background(), func(r Repository) error {
		_, err := s.factsSelected(r, scope, normalized, factSelection{})
		return err
	})
	if !errors.Is(err, ErrQueryTooLarge) {
		t.Fatalf("raw reduction unexpectedly fit the budget: %v", err)
	}

	list, err := s.Visitors(context.Background(), scope, f)
	if err != nil {
		t.Fatalf("visitor list exceeded the budget: %v", err)
	}
	if got := list.Identified + list.Anonymous; got != visits {
		t.Fatalf("want %d visitors, got %d", visits, got)
	}
	for _, v := range list.Items.([]Visitor) {
		if v.Views != 1 || v.Visible != 300000 {
			t.Fatalf("wrong projection-backed metrics: %+v", v)
		}
	}
	contents, err := s.Contents(context.Background(), scope, f, "")
	if err != nil {
		t.Fatalf("content list exceeded the budget: %v", err)
	}
	_ = contents
}

func TestScopedQueryRescansOnlySessionsWithoutExactProjection(t *testing.T) {
	query, args := buildScopedQuery("tenant", "event", RecordQuery{Owners: []string{"owner-a"}, AtFrom: time.Now(), WithoutExactSummary: true}, 10)
	for _, want := range []string{"NOT EXISTS (SELECT 1 FROM tracking_records summary_state", "summary_state.record_id=r.session_id", "'$.complete'))='true'", "'$.deferred_from') IS NULL"} {
		if !strings.Contains(query, want) {
			t.Fatalf("missing %q in %s", want, query)
		}
	}
	if strings.Count(query, "?") != len(args) {
		t.Fatalf("%d placeholders for %d args", strings.Count(query, "?"), len(args))
	}
	plain, _ := buildScopedQuery("tenant", "event", RecordQuery{Owners: []string{"owner-a"}}, 10)
	if strings.Contains(plain, "summary_state") {
		t.Fatal("predicate added without the option")
	}
}
