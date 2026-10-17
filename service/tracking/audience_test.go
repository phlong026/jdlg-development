package tracking

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The shipped routes.json is the authority for audience gating: every page
// that actually collects page_views must carry a valid domain label, and the
// flagship pages must sit on the expected side of the customer/bside line.
func TestRouteAudienceLabelsCollectedPages(t *testing.T) {
	cfg := LoadConfig()
	if len(cfg.Routes) < 50 {
		t.Fatalf("routes.json unexpectedly small: %d", len(cfg.Routes))
	}
	s := New(nil, cfg)
	for route, r := range cfg.Routes {
		if !r.Public {
			continue
		}
		got := s.routeAudience(route)
		if got != RouteAudienceCustomer && got != RouteAudienceBSide && got != RouteAudienceNeutral {
			t.Fatalf("collected route %s has invalid audience %q", route, got)
		}
	}
	for _, tc := range []struct {
		route string
		want  string
	}{
		{"pages/contentreader/contentreader", RouteAudienceCustomer},
		{"pages/aicost/aicost", RouteAudienceCustomer},
		{"pages/template/template", RouteAudienceCustomer},
		{"pages/distribution/distribution", RouteAudienceBSide},
		{"pages/index/index", RouteAudienceNeutral},
		{"pages/does/not/exist", RouteAudienceNeutral},
	} {
		if got := s.routeAudience(tc.route); got != tc.want {
			t.Fatalf("routeAudience(%s) = %q, want %q", tc.route, got, tc.want)
		}
	}
	// An unlabelled config entry must fall back to neutral, never panic.
	minimal := New(nil, Config{Routes: map[string]Route{"page": {Title: "page", Public: true}}})
	if got := minimal.routeAudience("page"); got != RouteAudienceNeutral {
		t.Fatalf("unlabelled route resolved %q", got)
	}
}

// CustomerShare must weight only customer+bside visible time: neutral tooling
// never dilutes the signal, and page views before session dedupe all count.
func TestIntentCustomerShareOverAudiencePages(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	s := New(nil, Config{Tenant: "t", Enabled: true, IntentEnabled: true, Routes: map[string]Route{
		"tpl":   {Title: "tpl", Public: true, Audience: RouteAudienceCustomer},
		"distr": {Title: "distr", Public: true, Audience: RouteAudienceBSide},
		"home":  {Title: "home", Public: true, Audience: RouteAudienceNeutral},
	}})
	s.Now = func() time.Time { return at }
	view := func(id, route string, visible int64, session string, started time.Time) factView {
		ended := started.Add(time.Minute)
		return factView{PageView: PageView{ID: id, Route: route, Started: started, Visible: visible, Ended: &ended}, Visitor: "u:7", Session: session}
	}
	data := intentFacts{rule: defaultRule("one"), views: map[string][]factView{"u:7": {
		view("v1", "tpl", 60_000, "s1", at.Add(-2*time.Hour)),
		view("v2", "tpl", 30_000, "s1", at.Add(-90*time.Minute)),
		view("v3", "distr", 30_000, "s1", at.Add(-time.Hour)),
		view("v4", "home", 500_000, "s1", at.Add(-30*time.Minute)),
	}}, business: map[string][]Record{}, overrides: map[string]Record{}}
	in := s.intent(Scope{Tenant: "t", Owners: []string{"one"}}, "u:7", at, data)
	// customer 90s / (customer 90s + bside 30s) = 0.75; neutral 500s ignored.
	if in.CustomerShare < 0.749 || in.CustomerShare > 0.751 {
		t.Fatalf("CustomerShare = %.4f, want 0.75", in.CustomerShare)
	}
	// No audience-labelled reading leaves the share unset (omitempty → 0).
	data.views["u:7"] = []factView{view("v5", "home", 999_000, "s2", at.Add(-time.Hour))}
	if in2 := s.intent(Scope{Tenant: "t", Owners: []string{"one"}}, "u:7", at, data); in2.CustomerShare != 0 {
		t.Fatalf("neutral-only share = %.4f, want 0", in2.CustomerShare)
	}
}

// A declared designer keeps an audited rule evaluation but the projected
// visitor intent is pinned to low with an explicit exclusion, so channel
// activity never floods the high-intent worklist.
func TestVisitorsDowngradeBSideProjectedIntent(t *testing.T) {
	s, m, scope, now := reviewFixture()
	// Drive the rule above the medium threshold so the downgrade is visible.
	for i := 0; i < 3; i++ {
		reviewPV(m, "share-view-"+string(rune('a'+i)), "one", "u:5", "session-"+string(rune('a'+i)), "", now.Add(-time.Hour))
	}
	m.Put(record("business", "biz-fav-1", "one", now.Add(-time.Hour), map[string]string{"action": "favorite"}))
	s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
		return map[int64]VisitorProfile{5: {Nickname: "设计乙", IdentityType: "设计师"}}, nil
	}
	out, err := s.Visitors(context.Background(), scope, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var designer *Visitor
	for _, v := range out.Items.([]Visitor) {
		if v.Key == "u:5" {
			designer = &v
		}
	}
	if designer == nil {
		t.Fatal("designer visitor missing from projection")
	}
	if designer.Intent.Level != "low" {
		t.Fatalf("designer projected level = %q, want low", designer.Intent.Level)
	}
	found := false
	for _, x := range designer.Intent.Exclusions {
		if x == "bside_identity" {
			found = true
		}
	}
	if !found || len(designer.Intent.Reasons) == 0 || !strings.HasPrefix(designer.Intent.Reasons[0], "B端账号") {
		t.Fatalf("exclusion/reason not applied: %+v", designer.Intent)
	}
}
