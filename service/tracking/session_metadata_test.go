package tracking

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSessionsRedactUnavailableMaterialMetadata(t *testing.T) {
	for _, retained := range []bool{false, true} {
		for _, unavailable := range []string{"revoked", "moved-owner", "expired-version"} {
			name := unavailable + "/raw"
			if retained {
				name = unavailable + "/retained"
			}
			t.Run(name, func(t *testing.T) {
				s, rows, scope, at := reviewFixture()
				catalogSeed(rows, "material-selected", "Selected safe material", "one", 1, at.Add(-3*time.Hour))
				catalogSeed(rows, "material-hidden", "PRIVATE HISTORICAL TITLE", "one", 1, at.Add(-3*time.Hour))
				catalogVisit(rows, "selected-view", "one", "u:1", "same-session", "material-selected", 1, at.Add(-2*time.Hour), 1000)
				catalogVisit(rows, "hidden-view", "one", "u:1", "same-session", "material-hidden", 1, at.Add(-time.Hour), 2000)
				if retained {
					retainLatestVisitFacts(t, s, rows, at)
				}
				switch unavailable {
				case "revoked":
					rows.Put(record("content_revoked", "material-hidden", "one", at, map[string]bool{"revoked": true}))
				case "moved-owner":
					rows.Put(record("content", "material-hidden", "two", at, Content{ID: "material-hidden", Owner: "two", Version: 1, Status: "published", Title: "PRIVATE CURRENT TITLE"}))
				case "expired-version":
					rows.Remove("version", "material-hidden:1")
				}
				material, err := s.VisitorContents(context.Background(), scope, "u:1", Filter{AsOf: at, Content: "material-hidden"})
				if err != nil {
					t.Fatal(err)
				}
				if item := material.Items.([]VisitorContent)[0]; item.Accessible || strings.Contains(item.Title, "PRIVATE") {
					t.Fatalf("summary not redacted: %+v", item)
				}
				trail, err := s.Sessions(context.Background(), scope, "u:1", Filter{AsOf: at, Content: "material-selected", Related: true})
				if err != nil {
					t.Fatal(err)
				}
				for _, session := range trail.Items.([]Session) {
					for _, view := range session.Views {
						if view.ID == "hidden-view" && strings.Contains(view.Title, "PRIVATE") {
							t.Errorf("nested related trail exposes unavailable title: id=%s title=%q", view.ID, view.Title)
						}
					}
				}
			})
		}
	}
}

func TestLatestVisitCrossBusinessDay(t *testing.T) {
	for _, retained := range []bool{false, true} {
		name := "raw"
		if retained {
			name = "retained"
		}
		t.Run(name, func(t *testing.T) {
			s, rows, scope, at := reviewFixture()
			start := businessDay(at).Add(-2 * time.Second).UTC()
			catalogSeed(rows, "material-a", "A", "one", 1, start.Add(-time.Hour))
			catalogVisit(rows, "cross-day", "one", "u:1", "same-session", "material-a", 1, start, 4000)
			if retained {
				retainLatestVisitFacts(t, s, rows, at)
			}
			v, contents := readLatestVisitProjections(t, s, scope, "u:1", Filter{AsOf: at, Start: start, End: start.Add(4 * time.Second)})
			requireLatestVisit(t, v.LatestVisit, &LatestVisit{ID: "cross-day", Started: start, Visible: 4000, Content: "material-a", Version: 1})
			requireLatestVisit(t, contents[0].LatestVisit, v.LatestVisit)
			v, contents = readLatestVisitProjections(t, s, scope, "u:1", Filter{AsOf: at, Start: businessDay(at), End: start.Add(4 * time.Second)})
			requireLatestVisit(t, v.LatestVisit, nil)
			requireLatestVisit(t, contents[0].LatestVisit, nil)
			if v.Views != 0 || v.Visible != 2000 {
				t.Fatalf("carryover after midnight: %+v", v)
			}
		})
	}
}
