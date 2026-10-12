package tracking

import (
	"context"
	"errors"
	"testing"
	"time"
)

func seedRetention(t *testing.T, s *Service) {
	t.Helper()
	err := s.transaction(context.Background(), func(r Repository) error {
		for _, v := range []Record{
			record("event", "old", "owner-a", s.Now().AddDate(0, 0, -91), map[string]string{"private": "do not emit"}),
			record("event", "edge", "owner-a", s.Now().AddDate(0, 0, -90), nil),
			record("event", "other", "owner-b", s.Now().AddDate(0, 0, -91), nil),
			record("session", "old", "owner-a", s.Now().AddDate(0, 0, -181), nil),
			record("session", "recent", "owner-a", s.Now().AddDate(0, 0, -100), nil),
			record("deletion", "keep", "owner-a", s.Now().AddDate(0, 0, -400), nil),
			record("deletion_session", "keep", "owner-a", s.Now().AddDate(0, 0, -400), nil),
			record("audit", "keep", "owner-a", s.Now().AddDate(0, 0, -400), nil),
			record("content", "keep", "owner-a", s.Now().AddDate(0, 0, -400), nil),
		} {
			if e := r.Put(v); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestRetentionDryRunAndReviewedPurge(t *testing.T) {
	s, scope := fixture(t)
	seedRetention(t, s)
	ctx := context.Background()
	p := DefaultRetentionPolicy()
	plan, e := s.PlanRetention(ctx, scope, p, s.Now().AddDate(0, 0, -90), s.Now().AddDate(0, 0, -180))
	if e != nil {
		t.Fatal(e)
	}
	if plan.Counts["event"] != 1 || plan.Counts["session"] != 1 || plan.ReviewDigest == "" {
		t.Fatalf("bad counters: %+v", plan)
	}
	if _, e = s.PurgeExpired(ctx, scope, p, plan); e == nil {
		t.Fatal("disabled purge accepted")
	}
	_ = s.transaction(ctx, func(r Repository) error {
		if _, e := r.Get("event", "old"); e != nil {
			t.Fatal("dry run changed data")
		}
		return nil
	})
	p.Enabled = true
	if _, e = s.PurgeExpired(ctx, scope, p, plan); e != nil {
		t.Fatal(e)
	}
	_ = s.transaction(ctx, func(r Repository) error {
		if _, e := r.Get("event", "old"); !errors.Is(e, ErrNotFound) {
			t.Fatal("expired row retained")
		}
		for _, pair := range [][2]string{{"event", "edge"}, {"event", "other"}, {"session", "recent"}, {"deletion", "keep"}, {"deletion_session", "keep"}, {"audit", "keep"}, {"content", "keep"}} {
			if _, e := r.Get(pair[0], pair[1]); e != nil {
				t.Fatalf("protected row removed: %v", pair)
			}
		}
		return nil
	})
}
func TestRetentionPlanRejectsDrift(t *testing.T) {
	s, scope := fixture(t)
	seedRetention(t, s)
	ctx := context.Background()
	p := DefaultRetentionPolicy()
	p.Enabled = true
	plan, e := s.PlanRetention(ctx, scope, p, s.Now().AddDate(0, 0, -90), s.Now().AddDate(0, 0, -180))
	if e != nil {
		t.Fatal(e)
	}
	_ = s.transaction(ctx, func(r Repository) error {
		return r.Put(record("event", "new-expired", "owner-a", s.Now().AddDate(0, 0, -100), nil))
	})
	if _, e = s.PurgeExpired(ctx, scope, p, plan); e == nil {
		t.Fatal("stale plan accepted")
	}
	_ = s.transaction(ctx, func(r Repository) error {
		if _, e := r.Get("event", "old"); e != nil {
			t.Fatal("failed purge mutated data")
		}
		return nil
	})
}
func TestRetentionGuards(t *testing.T) {
	s, scope := fixture(t)
	p := DefaultRetentionPolicy()
	ctx := context.Background()
	raw, summary := s.Now().AddDate(0, 0, -90), s.Now().AddDate(0, 0, -180)
	for _, bad := range []RetentionPolicy{{RawDays: 0, SummaryDays: 180}, {RawDays: 180, SummaryDays: 90}, {RawDays: 90, SummaryDays: 4000}} {
		if _, e := s.PlanRetention(ctx, scope, bad, raw, summary); e == nil {
			t.Fatal("invalid policy accepted")
		}
	}
	for _, cut := range []time.Time{{}, s.Now(), raw.Add(time.Second)} {
		if _, e := s.PlanRetention(ctx, scope, p, cut, summary); e == nil {
			t.Fatal("invalid cutoff accepted")
		}
	}
	scope.Delete = false
	if _, e := s.PlanRetention(ctx, scope, p, raw, summary); e == nil {
		t.Fatal("unauthorized maintenance")
	}
	scope.Delete = true
	scope.Rules = false
	if _, e := s.PlanRetention(ctx, scope, p, raw, summary); e == nil {
		t.Fatal("missing rules grant accepted")
	}
}
func TestRetentionReviewCannotChangeScope(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	p := DefaultRetentionPolicy()
	p.Enabled = true
	plan, e := s.PlanRetention(ctx, scope, p, s.Now().AddDate(0, 0, -90), s.Now().AddDate(0, 0, -180))
	if e != nil {
		t.Fatal(e)
	}
	scope.Owners = []string{"owner-b"}
	if _, e := s.PurgeExpired(ctx, scope, p, plan); e == nil {
		t.Fatal("review reused for another scope")
	}
	scope.Owners = []string{"owner-a"}
	scope.Actor++
	if _, e := s.PurgeExpired(ctx, scope, p, plan); e == nil {
		t.Fatal("review reused for another actor")
	}
}

func TestDiagnosticsFiniteScopedCounters(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	_ = s.transaction(ctx, func(r Repository) error {
		for _, row := range []Record{record("event", "one", "owner-a", s.Now(), Event{Flags: []string{"late", "late", "queue_loss", "private-secret"}}), record("event", "two", "owner-b", s.Now(), Event{Flags: []string{"late"}})} {
			if e := r.Put(row); e != nil {
				return e
			}
		}
		return nil
	})
	counts, e := s.Diagnostics(ctx, scope)
	if e != nil {
		t.Fatal(e)
	}
	if counts["accepted_events"] != 1 || counts["late"] != 1 || counts["queue_loss"] != 1 || len(counts) != 8 {
		t.Fatalf("unsafe or incorrect diagnostics: %+v", counts)
	}
	scope.View = false
	if _, e = s.Diagnostics(ctx, scope); e == nil {
		t.Fatal("unauthorized diagnostics")
	}
}
