package tracking

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func aggregateSeed(t *testing.T, r Repository, owner, visitor, session, view, id, kind string, at time.Time, delta int64) {
	t.Helper()
	ev := Event{ID: id, Type: kind, Session: session, View: view, Route: "pages/home/home", Occurred: at, Received: at, Delta: delta}
	rec := record("event", id, owner, at, ev)
	rec.Visitor = visitor
	rec.Session = session
	if e := r.Put(rec); e != nil {
		t.Fatal(e)
	}
}
func aggregateRows(t *testing.T, r Repository) []DailySummary {
	t.Helper()
	rows, e := r.List("daily_summary")
	if e != nil {
		t.Fatal(e)
	}
	out := []DailySummary{}
	for _, row := range rows {
		var x DailySummary
		if e = json.Unmarshal(row.Payload, &x); e != nil {
			t.Fatal(e)
		}
		out = append(out, x)
	}
	return out
}
func TestDailySummaryDuplicateLateCrossMidnight(t *testing.T) {
	s, _ := fixture(t)
	r := &memoryRepo{data: map[string]Record{}}
	at := time.Date(2026, 10, 10, 15, 59, 55, 0, time.UTC)
	aggregateSeed(t, r, "owner-a", "a:alice", "session-a", "view-a", "heartbeat", "visible_segment", at.Add(10*time.Second), 10000)
	if e := s.RebuildSessionSummaries(r, "session-a", s.Now()); e != nil {
		t.Fatal(e)
	}
	if len(aggregateRows(t, r)) != 0 {
		t.Fatal("heartbeat invented a PV")
	}
	aggregateSeed(t, r, "owner-a", "a:alice", "session-a", "view-a", "page-view", "page_view", at, 0)
	aggregateSeed(t, r, "owner-a", "a:alice", "session-a", "view-a", "heartbeat-copy", "visible_segment", at.Add(10*time.Second), 10000)
	for i := 0; i < 2; i++ {
		if e := s.RebuildSessionSummaries(r, "session-a", s.Now()); e != nil {
			t.Fatal(e)
		}
	}
	rows := aggregateRows(t, r)
	if len(rows) != 2 {
		t.Fatalf("days=%d", len(rows))
	}
	count := 0
	for _, x := range rows {
		if x.Count {
			count++
		}
		if x.View.Visible != 5000 || x.Timezone != "Asia/Shanghai" || x.Metric != MetricVersion {
			t.Fatalf("%+v", x)
		}
	}
	if count != 1 {
		t.Fatal("double counted PV")
	}
	aggregateSeed(t, r, "owner-a", "a:alice", "session-a", "view-a", "late-extra", "visible_segment", at.Add(15*time.Second), 5000)
	if e := s.RebuildSessionSummaries(r, "session-a", s.Now()); e != nil {
		t.Fatal(e)
	}
	total := int64(0)
	for _, x := range aggregateRows(t, r) {
		total += x.View.Visible
	}
	if total != 15000 {
		t.Fatalf("late total %d", total)
	}
}
func TestDailySummaryRetentionScopeIdentityAndDeletion(t *testing.T) {
	s, scope := fixture(t)
	r := &memoryRepo{data: map[string]Record{}}
	at := s.Now().AddDate(0, 0, -100)
	for _, owner := range []string{"owner-a", "owner-b"} {
		session := "session-" + owner
		aggregateSeed(t, r, owner, "a:alice", session, "view-"+owner, "pv-"+owner, "page_view", at, 0)
		aggregateSeed(t, r, owner, "a:alice", session, "view-"+owner, "seg-"+owner, "visible_segment", at.Add(10*time.Second), 10000)
	}
	raw, _ := r.List("event")
	if e := s.PrepareSummaryRetention(r, raw, s.Now()); e != nil {
		t.Fatal(e)
	}
	for _, row := range raw {
		r.Remove(row.Kind, row.ID)
	}
	if e := s.RebuildSessionSummaries(r, "session-owner-a", s.Now()); e != nil {
		t.Fatal(e)
	}
	f := Filter{Start: at.Add(-time.Hour), End: s.Now().Add(time.Nanosecond), AsOf: s.Now()}
	facts, expired, e := s.summaryFacts(r, scope, f)
	if e != nil || len(facts) != 1 || len(expired) != 1 || facts[0].Visible != 10000 || !facts[0].Count {
		t.Fatalf("%+v %v %v", facts, expired, e)
	}
	link := record("identity", "session-owner-a", "owner-a", at.Add(time.Hour), map[string]string{"original_visitor_key": "a:alice", "visitor_key": "u:42"})
	r.Put(link)
	facts, _, e = s.summaryFacts(r, scope, f)
	if e != nil || facts[0].Visitor != "u:42" {
		t.Fatalf("identity %+v %v", facts, e)
	}
	clipped := f
	clipped.Start = at.Add(4 * time.Second)
	clipped.End = at.Add(7 * time.Second)
	facts, _, e = s.summaryFacts(r, scope, clipped)
	if e != nil || len(facts) != 1 || facts[0].Count || facts[0].Visible != 3000 {
		t.Fatalf("clipped %+v %v", facts, e)
	}
	r.Put(record("deletion_session", "session-owner-a", "", s.Now(), map[string]bool{"deleted": true}))
	if e = s.RebuildSessionSummaries(r, "session-owner-a", s.Now()); e != nil {
		t.Fatal(e)
	}
	facts, _, e = s.summaryFacts(r, scope, f)
	if e != nil || len(facts) != 0 {
		t.Fatalf("resurrected %+v %v", facts, e)
	}
	for _, x := range aggregateRows(t, r) {
		if x.Owner == "owner-a" {
			t.Fatal("summary not scrubbed")
		}
	}
}
func TestDailySummaryRejectsUnavailableHistoricalSnapshot(t *testing.T) {
	s, scope := fixture(t)
	r := &memoryRepo{data: map[string]Record{}}
	at := s.Now().AddDate(0, 0, -100)
	aggregateSeed(t, r, "owner-a", "u:1", "session-a", "view-a", "pv-a", "page_view", at, 0)
	raw, _ := r.List("event")
	if e := s.PrepareSummaryRetention(r, raw, s.Now()); e != nil {
		t.Fatal(e)
	}
	_, _, e := s.summaryFacts(r, scope, Filter{Start: at.Add(-time.Hour), End: s.Now(), AsOf: s.Now().Add(-time.Minute)})
	if e == nil {
		t.Fatal("pretended frozen summary can reconstruct historical receipt snapshot")
	}
}
func TestDailySummaryDeleteThroughService(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	if e := s.transaction(ctx, func(r Repository) error {
		at := s.Now().AddDate(0, 0, -100)
		aggregateSeed(t, r, "owner-a", "u:99", "session-delete", "view-delete", "pv-delete", "page_view", at, 0)
		raw, _ := r.List("event")
		if e := s.PrepareSummaryRetention(r, raw, s.Now()); e != nil {
			return e
		}
		for _, row := range raw {
			r.Remove(row.Kind, row.ID)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	// This exercises the integration contract: expired summaries alone must make
	// the visitor discoverable to deletion, with all projection kinds scrubbed.
	if e := s.DeleteVisitor(ctx, scope, "u:99", "authorized deletion"); e != nil {
		t.Fatal(e)
	}
	if e := s.transaction(ctx, func(r Repository) error {
		rows, _ := r.List("daily_summary")
		states, _ := r.List("summary_state")
		if len(rows) != 0 || len(states) != 0 {
			t.Fatal("retained deleted projection")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func TestDailySummaryPurgeRetainsMetrics(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	at := s.Now().AddDate(0, 0, -100)
	if e := s.transaction(ctx, func(r Repository) error {
		aggregateSeed(t, r, "owner-a", "u:88", "session-purge", "view-purge", "pv-purge", "page_view", at, 0)
		aggregateSeed(t, r, "owner-a", "u:88", "session-purge", "view-purge", "seg-purge", "visible_segment", at.Add(10*time.Second), 10000)
		return s.RebuildSessionSummaries(r, "session-purge", s.Now())
	}); e != nil {
		t.Fatal(e)
	}
	p := DefaultRetentionPolicy()
	p.Enabled = true
	plan, e := s.PlanRetention(ctx, scope, p, s.Now().AddDate(0, 0, -90), s.Now().AddDate(0, 0, -180))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.PurgeExpired(ctx, scope, p, plan); e != nil {
		t.Fatal(e)
	}
	if e = s.transaction(ctx, func(r Repository) error {
		rows, _ := r.List("event")
		if len(rows) != 0 {
			t.Fatal("raw not purged")
		}
		facts, _, err := s.summaryFacts(r, scope, Filter{Start: at.Add(-time.Hour), End: s.Now().Add(time.Nanosecond), AsOf: s.Now()})
		if err != nil {
			return err
		}
		if len(facts) != 1 || !facts[0].Count || facts[0].Visible != 10000 {
			t.Fatalf("lost retained metrics: %+v", facts)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func TestDailySummaryIdentityBoundarySameSession(t *testing.T) {
	s, _ := fixture(t)
	r := &memoryRepo{data: map[string]Record{}}
	at := s.Now().Add(-time.Hour)
	aggregateSeed(t, r, "owner-a", "a:alice", "session-link", "view-link", "pv-link", "page_view", at, 0)
	aggregateSeed(t, r, "owner-a", "u:42", "session-link", "view-link", "seg-link", "visible_segment", at.Add(10*time.Second), 10000)
	r.Put(record("identity", "session-link", "owner-a", at.Add(5*time.Second), map[string]string{"original_visitor_key": "a:alice", "visitor_key": "u:42"}))
	if e := s.RebuildSessionSummaries(r, "session-link", s.Now()); e != nil {
		t.Fatal(e)
	}
	rows := aggregateRows(t, r)
	if len(rows) != 1 || rows[0].Visitor != "u:42" || rows[0].View.Visible != 10000 {
		t.Fatalf("identity lost visibility: %+v", rows)
	}
}

func TestDailySummaryLegacyPurgeDoesNotExtendRetention(t *testing.T) {
	s, _ := fixture(t)
	r := &memoryRepo{data: map[string]Record{}}
	at := s.Now().AddDate(0, 0, -200)
	aggregateSeed(t, r, "owner-a", "u:22", "session-legacy", "view-legacy", "pv-legacy", "page_view", at, 0)
	raw, _ := r.List("event")
	if e := s.PrepareSummaryRetention(r, raw, s.Now(), s.Now().AddDate(0, 0, -180)); e != nil {
		t.Fatal(e)
	}
	rows, _ := r.List("daily_summary")
	states, _ := r.List("summary_state")
	if len(rows) != 0 || len(states) != 0 {
		t.Fatal("legacy rebuild prolonged retention")
	}
}
