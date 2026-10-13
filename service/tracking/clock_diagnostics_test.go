package tracking

import (
	"context"
	"testing"
	"time"
)

func TestEventClockRejectionClassifiesWindow(t *testing.T) {
	started := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		now, occurred time.Time
		want          string
	}{
		{"zero time is malformed, not skew", started, time.Time{}, ""},
		{"inside tolerance ahead", started, started.Add(2 * time.Minute), ""},
		{"device clock ahead", started, started.Add(2*time.Minute + time.Second), "clock_ahead"},
		{"inside tolerance behind", started, started.Add(-2 * time.Minute), ""},
		{"event predates its own session", started, started.Add(-3 * time.Minute), "clock_behind"},
		{"queue older than 72 hours", started.Add(72*time.Hour - time.Minute), started.Add(-90 * time.Second), "stale_queue"},
	} {
		if got := eventClockRejection(tc.now, started, tc.occurred); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestClockSkewRejectionsAreCountedPerOwnerDay(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	en := enter(t, s, 0)
	ahead := ev(s, en, "event-ahead-0001", "page_view", now.Add(3*time.Minute))
	behind := ev(s, en, "event-behind-0001", "page_view", now.Add(-3*time.Minute))
	zero := ev(s, en, "event-zero-00001", "page_view", time.Time{})
	// An earlier validation failure is not attributed to the clock.
	malformed := ev(s, en, "event-schema-0001", "page_view", now.Add(3*time.Minute))
	malformed.Schema = 2
	valid := ev(s, en, "event-valid-0001", "page_view", now)
	results := accept(t, s, en, ahead, behind, zero, malformed, valid)
	want := []string{"rejected", "rejected", "rejected", "rejected", "accepted"}
	for i, r := range results {
		if r.Status != want[i] {
			t.Fatalf("result %d: %+v", i, r)
		}
	}
	if results[0].Reason != eventWindowMessage || results[1].Reason != eventWindowMessage {
		t.Fatalf("window rejection reason changed: %+v", results)
	}
	again := ev(s, en, "event-ahead-0002", "page_end", now.Add(5*time.Minute))
	accept(t, s, en, again)

	stats, err := s.RejectionStats(context.Background(), scope, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Owner != "owner-a" || stats[0].Day != "2026-10-11" {
		t.Fatalf("unexpected rows: %+v", stats)
	}
	if got := stats[0].Counts; got["clock_ahead"] != 2 || got["clock_behind"] != 1 || len(got) != 2 {
		t.Fatalf("unexpected counts: %+v", got)
	}

	other := scope
	other.Owners = []string{"owner-b"}
	if stats, err = s.RejectionStats(context.Background(), other, 7); err != nil || len(stats) != 0 {
		t.Fatalf("other owner saw counters: %+v %v", stats, err)
	}
	noView := scope
	noView.View = false
	if _, err = s.RejectionStats(context.Background(), noView, 7); err == nil {
		t.Fatal("counters readable without view permission")
	}
	for _, days := range []int{0, 91} {
		if _, err = s.RejectionStats(context.Background(), scope, days); err == nil {
			t.Fatalf("days=%d accepted", days)
		}
	}
	// The window is whole UTC days: a row from 7 days ago is outside days=7.
	s.Now = func() time.Time { return now.AddDate(0, 0, 7) }
	if stats, err = s.RejectionStats(context.Background(), scope, 7); err != nil || len(stats) != 0 {
		t.Fatalf("old day included: %+v %v", stats, err)
	}
	if stats, err = s.RejectionStats(context.Background(), scope, 8); err != nil || len(stats) != 1 {
		t.Fatalf("day boundary excluded: %+v %v", stats, err)
	}
}
