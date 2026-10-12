package tracking

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func seedLatestVisitEvent(t *testing.T, rows reviewStore, owner, visitor string, event Event) {
	t.Helper()
	row := record("event", event.ID, owner, event.Occurred, event)
	row.Visitor, row.Session, row.Content = visitor, event.Session, event.Content
	if err := rows.Put(row); err != nil {
		t.Fatal(err)
	}
}

func retainLatestVisitFacts(t *testing.T, s *Service, rows reviewStore, at time.Time) {
	t.Helper()
	sessions := map[string]string{}
	for _, row := range rows {
		if row.Kind == "event" {
			sessions[row.Session] = row.Owner
		}
	}
	for session, owner := range sessions {
		if err := s.RebuildSessionSummaries(rows, session, at); err != nil {
			t.Fatal(err)
		}
		if err := rows.Put(record("summary_state", session, owner, at, summaryState{Metric: MetricVersion, RawExpired: true})); err != nil {
			t.Fatal(err)
		}
	}
	for id, row := range rows {
		if row.Kind == "event" {
			delete(rows, id)
		}
	}
}

func readLatestVisitProjections(t *testing.T, s *Service, scope Scope, key string, f Filter) (Visitor, []VisitorContent) {
	t.Helper()
	visitors, err := s.Visitors(context.Background(), scope, f)
	if err != nil {
		t.Fatal(err)
	}
	var visitor Visitor
	for _, item := range visitors.Items.([]Visitor) {
		if item.Key == key {
			visitor = item
			break
		}
	}
	if visitor.Key == "" {
		t.Fatalf("visitor %q missing from %+v", key, visitors)
	}
	contents, err := s.VisitorContents(context.Background(), scope, key, f)
	if err != nil {
		t.Fatal(err)
	}
	return visitor, contents.Items.([]VisitorContent)
}

func requireLatestVisit(t *testing.T, got, want *LatestVisit) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("latest visit = %+v, want %+v", got, want)
	}
}

func TestLatestVisitUsesPageStartNotLastActivity(t *testing.T) {
	for _, retained := range []bool{false, true} {
		name := "raw"
		if retained {
			name = "retained"
		}
		t.Run(name, func(t *testing.T) {
			s, rows, scope, at := reviewFixture()
			catalogSeed(rows, "material-a", "素材甲", "one", 1, at.Add(-3*time.Hour))
			catalogVisit(rows, "older-page", "one", "u:1", "older-session", "material-a", 1, at.Add(-2*time.Hour), 1000)
			started := at.Add(-time.Hour)
			catalogVisit(rows, "newer-page", "one", "u:1", "newer-session", "material-a", 1, started, 2000)
			last := at.Add(-time.Minute)
			seedLatestVisitEvent(t, rows, "one", "u:1", Event{ID: "older-late-heartbeat", Type: "visible_segment", View: "older-page", Session: "older-session", Route: publishedReaderRoute, Content: "material-a", Version: 1, Occurred: last, Received: last, Delta: 3000})
			if retained {
				retainLatestVisitFacts(t, s, rows, at)
			}
			visitor, contents := readLatestVisitProjections(t, s, scope, "u:1", Filter{AsOf: at})
			want := &LatestVisit{ID: "newer-page", Started: started, Visible: 2000, Content: "material-a", Version: 1}
			requireLatestVisit(t, visitor.LatestVisit, want)
			if visitor.Views != 2 || visitor.Visible != 6000 || !visitor.Last.Equal(last) {
				t.Fatalf("visitor aggregate changed: %+v", visitor)
			}
			if len(contents) != 1 || contents[0].PageViews != 2 || contents[0].VisibleMS != 6000 || !contents[0].LastVisit.Equal(last) {
				t.Fatalf("material aggregate changed: %+v", contents)
			}
			requireLatestVisit(t, contents[0].LatestVisit, want)
		})
	}
}

func TestLatestVisitStableStartTiePreservesZero(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	catalogSeed(rows, "material-a", "素材甲", "one", 1, at.Add(-3*time.Hour))
	catalogVisit(rows, "older-page", "one", "u:1", "older-session", "material-a", 1, at.Add(-2*time.Hour), 1000)
	started := at.Add(-time.Hour)
	catalogVisit(rows, "latest-z", "one", "u:1", "latest-z-session", "material-a", 1, started, 3000)
	catalogVisit(rows, "latest-a", "one", "u:1", "latest-a-session", "material-a", 1, started, 0)
	want := &LatestVisit{ID: "latest-a", Started: started, Visible: 0, Content: "material-a", Version: 1}
	// Facts are built from maps; neither insertion order nor duration breaks ties.
	for i := 0; i < 8; i++ {
		visitor, contents := readLatestVisitProjections(t, s, scope, "u:1", Filter{AsOf: at})
		requireLatestVisit(t, visitor.LatestVisit, want)
		requireLatestVisit(t, contents[0].LatestVisit, want)
		if visitor.Visible != 4000 || visitor.Views != 3 || contents[0].VisibleMS != 4000 {
			t.Fatalf("zero latest visit lost aggregate: %+v %+v", visitor, contents)
		}
		for _, projection := range []any{visitor, contents[0]} {
			var encoded map[string]json.RawMessage
			if err := json.Unmarshal(marshal(projection), &encoded); err != nil {
				t.Fatal(err)
			}
			var latest map[string]json.RawMessage
			if err := json.Unmarshal(encoded["latest_visit"], &latest); err != nil || string(latest["visible_ms"]) != "0" {
				t.Fatalf("zero duration omitted: %s (%v)", encoded["latest_visit"], err)
			}
		}
	}
}

func TestLatestVisitKeepsActiveScopeAndSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name    string
		filter  Filter
		wantID  string
		wantMS  int64
		wantPV  int
		wantSum int64
	}{
		{"authorized owners", Filter{}, "owner-two-page", 4000, 4, 10000},
		{"explicit owner", Filter{Owner: "one"}, "other-material-page", 3000, 3, 6000},
		{"content", Filter{Content: "material-a"}, "version-two-page", 2000, 2, 3000},
		{"version", Filter{Content: "material-a", ContentVersion: 1}, "version-one-page", 1000, 1, 1000},
		{"title", Filter{ContentSearch: "素材甲"}, "version-two-page", 2000, 2, 3000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, rows, scope, at := reviewFixture()
			scope.Owners = []string{"one", "two"}
			catalogSeed(rows, "material-a", "素材甲旧版", "one", 1, at.Add(-4*time.Hour))
			catalogSeed(rows, "material-a", "素材甲新版", "one", 2, at.Add(-3*time.Hour))
			catalogSeed(rows, "material-b", "素材乙", "one", 1, at.Add(-3*time.Hour))
			for _, v := range []struct {
				id, owner, content string
				version, minutes   int
				visible            int64
			}{
				{"version-one-page", "one", "material-a", 1, 100, 1000},
				{"version-two-page", "one", "material-a", 2, 80, 2000},
				{"other-material-page", "one", "material-b", 1, 60, 3000},
				{"owner-two-page", "two", "material-b", 1, 40, 4000},
				{"forbidden-page", "three", "material-b", 1, 20, 5000},
			} {
				catalogVisit(rows, v.id, v.owner, "u:1", v.id+"-session", v.content, v.version, at.Add(-time.Duration(v.minutes)*time.Minute), v.visible)
			}
			// Another visitor's newer visit cannot enter this visitor's projection.
			catalogVisit(rows, "other-visitor-page", "one", "u:2", "other-visitor-session", "material-b", 1, at.Add(-10*time.Minute), 7000)
			// Neither post-snapshot receipt nor post-snapshot occurrence may win.
			seedLatestVisitEvent(t, rows, "one", "u:1", Event{ID: "late-receipt-page", Type: "page_view", View: "late-receipt-page", Session: "late-session", Route: publishedReaderRoute, Content: "material-a", Version: 1, Occurred: at.Add(-5 * time.Minute), Received: at.Add(time.Minute)})
			seedLatestVisitEvent(t, rows, "one", "u:1", Event{ID: "future-page", Type: "page_view", View: "future-page", Session: "future-session", Route: publishedReaderRoute, Content: "material-a", Version: 1, Occurred: at.Add(time.Minute), Received: at})
			f := tc.filter
			f.AsOf = at
			visitor, contents := readLatestVisitProjections(t, s, scope, "u:1", f)
			if visitor.LatestVisit == nil || visitor.LatestVisit.ID != tc.wantID || visitor.LatestVisit.Visible != tc.wantMS || visitor.Views != tc.wantPV || visitor.Visible != tc.wantSum {
				t.Fatalf("escaped active scope: %+v", visitor)
			}
			found := false
			for _, item := range contents {
				if item.LatestVisit == nil || item.LatestVisit.Content != item.ID || item.LatestVisit.Version != item.Version {
					t.Fatalf("material/version association lost: %+v", item)
				}
				if item.LatestVisit.ID == tc.wantID {
					found = true
					requireLatestVisit(t, item.LatestVisit, visitor.LatestVisit)
				}
			}
			if !found {
				t.Fatalf("matching material projection missing: %+v", contents)
			}
		})
	}
}

func TestLatestVisitUsesClippedDeduplicatedSingleView(t *testing.T) {
	for _, retained := range []bool{false, true} {
		name := "raw"
		if retained {
			name = "retained"
		}
		t.Run(name, func(t *testing.T) {
			s, rows, scope, at := reviewFixture()
			started := at.Add(-time.Hour)
			catalogSeed(rows, "material-a", "素材甲", "one", 1, at.Add(-2*time.Hour))
			catalogSeed(rows, "material-b", "素材乙", "one", 1, at.Add(-2*time.Hour))
			catalogVisit(rows, "earlier-page", "one", "u:1", "shared-session", "material-a", 1, started.Add(-2*time.Second), 5000)
			catalogVisit(rows, "latest-page", "one", "u:1", "shared-session", "material-b", 1, started, 10000)
			if retained {
				retainLatestVisitFacts(t, s, rows, at)
			}
			f := Filter{AsOf: at, Content: "material-b", Start: started, End: started.Add(5 * time.Second)}
			visitor, contents := readLatestVisitProjections(t, s, scope, "u:1", f)
			want := &LatestVisit{ID: "latest-page", Started: started, Visible: 2000, Content: "material-b", Version: 1}
			requireLatestVisit(t, visitor.LatestVisit, want)
			requireLatestVisit(t, contents[0].LatestVisit, want)
			if visitor.Visible != 2000 || visitor.Views != 1 || contents[0].VisibleMS != 2000 {
				t.Fatalf("duration lost clipping/deduplication: %+v %+v", visitor, contents)
			}
			// The same view contributes carry-over time, but not a new visit.
			f.Start = started.Add(time.Second)
			visitor, contents = readLatestVisitProjections(t, s, scope, "u:1", f)
			if visitor.Views != 0 || visitor.Visible != 2000 || contents[0].PageViews != 0 || contents[0].VisibleMS != 2000 {
				t.Fatalf("carry-over metrics changed: %+v %+v", visitor, contents)
			}
			requireLatestVisit(t, visitor.LatestVisit, nil)
			requireLatestVisit(t, contents[0].LatestVisit, nil)
			for _, projection := range []any{visitor, contents[0]} {
				var encoded map[string]json.RawMessage
				if err := json.Unmarshal(marshal(projection), &encoded); err != nil {
					t.Fatal(err)
				}
				if _, exists := encoded["latest_visit"]; exists {
					t.Fatalf("invented a visit for carry-over duration: %s", marshal(projection))
				}
			}
		})
	}
}

func TestLatestVisitExcludesFlaggedDeletedAndOrphanFacts(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	catalogSeed(rows, "material-a", "素材甲", "one", 1, at.Add(-3*time.Hour))
	started := at.Add(-2 * time.Hour)
	catalogVisit(rows, "valid-page", "one", "u:1", "valid-session", "material-a", 1, started, 1000)
	for _, flag := range []string{"self", "preview", "test", "abnormal", "duplicate"} {
		stamp := at.Add(-time.Hour)
		seedLatestVisitEvent(t, rows, "one", "u:1", Event{ID: flag + "-page", Type: "page_view", View: flag + "-page", Session: flag + "-session", Route: publishedReaderRoute, Content: "material-a", Version: 1, Occurred: stamp, Received: stamp, Flags: []string{flag}})
		seedLatestVisitEvent(t, rows, "one", "u:1", Event{ID: flag + "-segment", Type: "visible_segment", View: "valid-page", Session: "valid-session", Route: publishedReaderRoute, Content: "material-a", Version: 1, Occurred: stamp, Received: stamp, Delta: 2000, Flags: []string{flag}})
	}
	catalogVisit(rows, "deleted-page", "one", "u:1", "deleted-session", "material-a", 1, at.Add(-time.Minute), 9000)
	rows.Put(record("deletion_session", "deleted-session", "one", at, map[string]bool{"deleted": true}))
	seedLatestVisitEvent(t, rows, "one", "u:1", Event{ID: "orphan-segment", Type: "visible_segment", View: "missing-page", Session: "orphan-session", Route: publishedReaderRoute, Content: "material-a", Version: 1, Occurred: at, Received: at, Delta: 8000})
	visitor, contents := readLatestVisitProjections(t, s, scope, "u:1", Filter{AsOf: at})
	want := &LatestVisit{ID: "valid-page", Started: started, Visible: 1000, Content: "material-a", Version: 1}
	requireLatestVisit(t, visitor.LatestVisit, want)
	requireLatestVisit(t, contents[0].LatestVisit, want)
	if visitor.Views != 1 || visitor.Visible != 1000 || contents[0].PageViews != 1 || contents[0].VisibleMS != 1000 {
		t.Fatalf("excluded facts counted: %+v %+v", visitor, contents)
	}
	rows.Put(record("deletion", "u:1", "", at, map[string]bool{"deleted": true}))
	got, err := s.Visitors(context.Background(), scope, Filter{AsOf: at})
	if err != nil || len(got.Items.([]Visitor)) != 0 {
		t.Fatalf("deleted visitor exposed: %+v %v", got, err)
	}
	_, err = s.VisitorContents(context.Background(), scope, "u:1", Filter{AsOf: at})
	requireQueryError(t, err, 404)
}
