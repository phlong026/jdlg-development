package tracking

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestDailySummaryUsesEligibleEventWatermark(t *testing.T) {
	s, _ := fixture(t)
	r := &memoryRepo{data: map[string]Record{}}
	now := s.Now()
	future := now.Add(time.Minute)
	farFuture := now.AddDate(1, 0, 0)

	aggregateSeed(t, r, "owner-a", "u:1", "session-watermark", "view-watermark", "pv-watermark", "page_view", now.Add(-time.Minute), 0)
	aggregateSeed(t, r, "owner-a", "u:1", "session-watermark", "view-watermark", "segment-watermark", "visible_segment", future, 1000)
	excluded := Event{ID: "self-future", Type: "visible_segment", Session: "session-watermark", View: "view-watermark", Route: "pages/home/home", Occurred: farFuture, Received: farFuture, Delta: 1000, Flags: []string{"self"}}
	excludedRow := record("event", excluded.ID, "owner-a", excluded.Occurred, excluded)
	excludedRow.Session, excludedRow.Visitor = excluded.Session, "u:1"
	if err := r.Put(excludedRow); err != nil {
		t.Fatal(err)
	}

	if err := s.RebuildSessionSummaries(r, "session-watermark", now); err != nil {
		t.Fatal(err)
	}
	state, err := decode[summaryState](r, "summary_state", "session-watermark")
	if err != nil {
		t.Fatal(err)
	}
	if state.DeferredFrom != nil || !state.BuiltAt.Equal(future) {
		t.Fatalf("state = %+v, want eligible watermark %s without deferred marker", state, future)
	}
	if summaryUsable(state, now) || !summaryUsable(state, future) {
		t.Fatalf("watermark usability is wrong at now=%v and future=%v", now, future)
	}
	for _, summary := range aggregateRows(t, r) {
		if !summary.BuiltAt.Equal(future) {
			t.Fatalf("summary built_at = %s, want %s", summary.BuiltAt, future)
		}
	}
}

func TestDailySummaryFutureOnlyBucketMatchesRawAcrossShanghaiMidnight(t *testing.T) {
	s, scope := fixture(t)
	r := &memoryRepo{data: map[string]Record{}}
	beforeMidnight := time.Date(2026, 10, 11, 15, 59, 59, 0, time.UTC)
	future := beforeMidnight.Add(2 * time.Second)
	aggregateSeed(t, r, "owner-a", "u:1", "session-future-day", "view-future-day", "pv-future-day", "page_view", future, 0)

	if err := s.RebuildSessionSummaries(r, "session-future-day", beforeMidnight); err != nil {
		t.Fatal(err)
	}
	rows := aggregateRows(t, r)
	if len(rows) != 1 || rows[0].Day != "2026-10-12" || !rows[0].BuiltAt.Equal(future) {
		t.Fatalf("future bucket = %+v", rows)
	}

	checks := []Filter{
		{Start: businessDay(beforeMidnight), End: businessDay(beforeMidnight).AddDate(0, 0, 1), AsOf: beforeMidnight},
		{Start: businessDay(future), End: businessDay(future).AddDate(0, 0, 1), AsOf: future},
	}
	for _, f := range checks {
		want, err := s.factsSelected(r, scope, f, factSelection{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.factsSelected(r, scope, f, factSelection{Summaries: true})
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(comparableFacts(got)) != fmt.Sprint(comparableFacts(want)) {
			t.Fatalf("as_of %s raw=%+v summary=%+v", f.AsOf, want, got)
		}
	}
}

func TestDailySummaryFutureSegmentBecomesUsableWithoutSecondRebuild(t *testing.T) {
	s, scope := fixture(t)
	r := &memoryRepo{data: map[string]Record{}}
	beforeMidnight := time.Date(2026, 10, 11, 15, 59, 59, 0, time.UTC)
	pvAt := time.Date(2026, 10, 11, 15, 59, 40, 0, time.UTC)
	segmentAt := time.Date(2026, 10, 11, 16, 0, 5, 0, time.UTC)
	aggregateSeed(t, r, "owner-a", "u:1", "session-midnight", "view-midnight", "pv-midnight", "page_view", pvAt, 0)
	aggregateSeed(t, r, "owner-a", "u:1", "session-midnight", "view-midnight", "segment-midnight", "visible_segment", segmentAt, 15000)

	if err := s.RebuildSessionSummaries(r, "session-midnight", beforeMidnight); err != nil {
		t.Fatal(err)
	}
	state, err := decode[summaryState](r, "summary_state", "session-midnight")
	if err != nil || state.DeferredFrom != nil || !state.BuiltAt.Equal(segmentAt) {
		t.Fatalf("state = %+v, err=%v", state, err)
	}

	wholeWindow := Filter{Start: businessDay(pvAt), End: businessDay(pvAt).AddDate(0, 0, 2)}
	for _, check := range []struct {
		asOf    time.Time
		count   bool
		visible int64
	}{
		{asOf: beforeMidnight, count: true, visible: 0},
		{asOf: segmentAt, count: true, visible: 15000},
	} {
		f := wholeWindow
		f.AsOf = check.asOf
		want, err := s.factsSelected(r, scope, f, factSelection{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.factsSelected(r, scope, f, factSelection{Summaries: true})
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(comparableFacts(got)) != fmt.Sprint(comparableFacts(want)) {
			t.Fatalf("as_of %s raw=%+v summary=%+v", f.AsOf, want, got)
		}
		if len(got) != 1 || got[0].Count != check.count || got[0].Visible != check.visible {
			t.Fatalf("as_of %s fact=%+v", f.AsOf, got)
		}
	}

	rows := aggregateRows(t, r)
	visibleByDay := map[string]int64{}
	for _, row := range rows {
		visibleByDay[row.Day] += row.View.Visible
	}
	if visibleByDay["2026-10-11"] != 10000 || visibleByDay["2026-10-12"] != 5000 {
		t.Fatalf("cross-midnight visibility = %+v", visibleByDay)
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
