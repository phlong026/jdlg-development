package tracking

import (
	"context"
	"fmt"
	"testing"
)

// An accepted event must carry its entry's route, content and version, so one
// batch asks one question. A 50-event batch on an original business page used to
// query the source tables 50 times inside the tenant lock.
func TestBatchChecksPageAvailabilityOnce(t *testing.T) {
	s, _ := fixture(t)
	route := "pages/inspirationdetail/inspirationdetail"
	s.Config.Routes[route] = Route{Title: "灵感", Public: true}
	lookups := 0
	s.ResolveLegacy = func(_ string, id int64) (LegacyMetadata, error) {
		lookups++
		return LegacyMetadata{Title: "竖向庭院", Type: "inspiration", CoverURL: "https://public.example/one.jpg", Width: 900, Height: 1600, VersionSeed: "body-a"}, nil
	}
	en, err := s.Enter(context.Background(), EntryInput{Route: route, Content: LegacyID(route, 1), Client: "client-legacy", Consent: "v1"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	events := []Event{}
	for i := 0; i < 50; i++ {
		x := ev(s, en, fmt.Sprintf("batch-event-%04d", i), "content_click", s.Now())
		x.View, x.Target = "view-batch-0001", "target-0001"
		if i == 0 {
			x.Type, x.Target = "page_view", ""
		}
		events = append(events, x)
	}
	lookups = 0
	for _, r := range accept(t, s, en, events...) {
		if r.Status != "accepted" {
			t.Fatalf("event rejected: %+v", r)
		}
	}
	if lookups != 1 {
		t.Fatalf("source tables queried %d times for one 50-event batch", lookups)
	}

	// A change of the source between batches is still seen: the answer is only
	// remembered within one batch and one transaction.
	s.ResolveLegacy = func(string, int64) (LegacyMetadata, error) { return LegacyMetadata{}, fail(404, "withdrawn") }
	next := ev(s, en, "batch-event-late1", "content_click", s.Now())
	next.View, next.Target = "view-batch-0001", "target-0001"
	if r := accept(t, s, en, next); r[0].Status != "rejected" {
		t.Fatalf("a withdrawn source page kept accepting events: %+v", r)
	}
}
