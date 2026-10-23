package tracking

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Exercise the real entry/share/ingestion/projection services as one journey.
// Authenticated account IDs and public-source resolution are explicit fixtures;
// this does not simulate a native WeChat identity provider or production data.
func TestFullJourneyNavigationLoginRevisitAndForward(t *testing.T) {
	s, scope := fixture(t)
	clock := s.Now()
	s.Now = func() time.Time { return clock }
	home, tool, template := "pages/index/index", "pages/aicost/aicost", "pages/templatesquaredetails/templatesquaredetails"
	for _, route := range []string{home, tool, template, CostSnapshotRoute} {
		s.Config.Routes[route] = Route{Title: route, Public: true}
	}
	s.Config.Routes["pages/report/questionnaire"] = Route{Title: "问卷", Public: false}
	s.ResolveLegacy = func(route string, id int64) (LegacyMetadata, error) {
		if route != template || id != 17 {
			return LegacyMetadata{}, fail(404, "not public")
		}
		return LegacyMetadata{Title: "公开模板17", Type: "image", VersionSeed: "fixed-template-v1"}, nil
	}
	s.ResolvePublicSnapshot = func(_ context.Context, id string) (LegacyMetadata, error) {
		if id != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
			return LegacyMetadata{}, fail(404, "snapshot missing")
		}
		return LegacyMetadata{Title: "主动公开的不可变测算快照", Type: "rich_text", VersionSeed: "immutable-snapshot-v1"}, nil
	}
	profileNames := map[int64]string{42: "林间访客", 77: "再访用户", 100: "销售账号"}
	s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
		out := map[int64]VisitorProfile{}
		for _, id := range ids {
			out[id] = VisitorProfile{Nickname: profileNames[id]}
		}
		return out, nil
	}
	s.IsContentOperator = func(_ context.Context, id int64, _ string) (bool, error) { return id == 100, nil }
	entry := func(account int64, client, route, content, previous, share string) Entry {
		t.Helper()
		en, err := s.Enter(context.Background(), EntryInput{Route: route, Content: content, Client: client, Anonymous: "anonymous-journey", Previous: previous, ShareToken: share, Consent: "v1"}, account)
		if err != nil {
			t.Fatal(err)
		}
		return en
	}
	sequence := 0
	view := func(en Entry, previous string) string {
		t.Helper()
		sequence++
		id := fmt.Sprintf("journey-view-%08d", sequence)
		pv := ev(s, en, id+"-pv", "page_view", clock)
		pv.View = id
		pv.Previous = previous
		got := accept(t, s, en, pv, pv)
		if got[0].Status != "accepted" || got[1].Status != "duplicate" {
			t.Fatalf("PV dedupe: %+v", got)
		}
		clock = clock.Add(10 * time.Second)
		segment := ev(s, en, id+"-segment", "visible_segment", clock)
		segment.View = id
		segment.Segment = id + "-visible"
		segment.Seq = 1
		segment.Delta = 10000
		segment.Monotonic = 30000
		got = accept(t, s, en, segment, segment)
		if got[0].Status != "accepted" || got[1].Status != "duplicate" {
			t.Fatalf("segment dedupe: %+v", got)
		}
		clock = clock.Add(time.Second)
		return id
	}
	share := func(en Entry, account int64, key string) map[string]any {
		t.Helper()
		out, err := s.CreateShare(context.Background(), ShareInput{Route: en.Route, Content: en.Content, Version: en.Version, Parent: en.Share, Channel: "timeline", Key: key}, en.Token, account)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	sender := entry(100, "installation-sender", home, "", "", "")
	firstShare := share(sender, 100, "journey-first-share")
	anonymous := entry(0, "installation-reader", home, "", "", firstShare["share_token"].(string))
	firstView := view(anonymous, "")
	next := entry(0, anonymous.Client, template, LegacyID(template, 17), anonymous.Token, "")
	if next.Session != anonymous.Session || next.Share != anonymous.Share || next.Owner != anonymous.Owner {
		t.Fatal("navigation lost its trusted source", next)
	}
	nextView := view(next, firstView)
	logged := entry(42, next.Client, template, next.Content, next.Token, "")
	if logged.Session != anonymous.Session || logged.Visitor != "u:42" || logged.OriginalVisitor != anonymous.Visitor {
		t.Fatal("verified login did not link this session", logged)
	}
	loggedView := view(logged, nextView)
	form := entry(42, logged.Client, tool, "", logged.Token, "")
	if form.Share != anonymous.Share || form.Owner != anonymous.Owner {
		t.Fatal("public form navigation lost attribution")
	}
	view(form, loggedView)
	if _, err := s.Enter(context.Background(), EntryInput{Route: "pages/report/questionnaire", Client: form.Client, Consent: "v1"}, 42); err == nil {
		t.Fatal("private questionnaire was collected")
	}
	// A later cold start has no trusted continuation credential: do not reuse
	// attribution or link another anonymous session just from installation/name.
	clock = clock.Add(31 * time.Minute)
	revisit := entry(42, form.Client, home, "", "", "")
	if revisit.Session == form.Session || revisit.Share != "" {
		t.Fatal("cold revisit invented prior attribution", revisit)
	}
	view(revisit, "")
	snapshot := entry(42, revisit.Client, CostSnapshotRoute, CostSnapshotID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), revisit.Token, "")
	view(snapshot, "")
	forward := share(snapshot, 42, "journey-forward-share")
	recipient := entry(77, "installation-recipient", CostSnapshotRoute, snapshot.Content, "", forward["share_token"].(string))
	view(recipient, "")
	repeat := entry(77, recipient.Client, CostSnapshotRoute, snapshot.Content, "", forward["share_token"].(string))
	if repeat.Session == recipient.Session {
		t.Fatal("separate external open reused a cold session")
	}
	view(repeat, "")
	again := share(repeat, 77, "journey-forward-again")
	third := entry(88, "installation-third", CostSnapshotRoute, snapshot.Content, "", again["share_token"].(string))
	if third.Share != again["share_id"] {
		t.Fatal("new recipient attributed to the wrong forwarding generation")
	}
	view(third, "")
	list, err := s.Visitors(context.Background(), scope, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Visitor{}
	for _, v := range list.Items.([]Visitor) {
		by[v.Key] = v
	}
	if list.Anonymous != 0 || by["u:42"].Views != 6 || by["u:42"].Visible != 60000 || by["u:42"].Nickname != "林间访客" || by["u:77"].Views != 2 || by["u:77"].Visible != 20000 {
		t.Fatalf("journey totals or nickname mismatch: %+v", by)
	}
	visits, err := s.Visits(context.Background(), scope, Filter{Channel: "timeline"})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	thirdSource := false
	for _, v := range visitItems(t, visits) {
		if v.Visitor.Key == "u:77" {
			count++
			if v.Source.Kind != "share" || v.Source.Channel != "timeline" || v.Source.Forwarder == nil || v.Source.Forwarder.Key != "u:42" || v.Source.Forwarder.Nickname != "林间访客" {
				t.Fatalf("forward source without share_intent missing: %+v", v.Source)
			}
		}
		if v.Visitor.Key == "u:88" {
			thirdSource = v.Source.Forwarder != nil && v.Source.Forwarder.Key == "u:77" && v.Source.Forwarder.Nickname == "再访用户"
		}
	}
	if count != 2 {
		t.Fatalf("repeat snapshot visits missing from timeline filter: %d", count)
	}
	if !thirdSource {
		t.Fatal("second forwarding generation lost its immediate sharer")
	}
	profileNames[42] = "回读新昵称"
	renamed, err := s.Visits(context.Background(), scope, Filter{Channel: "timeline"})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range visitItems(t, renamed) {
		if v.Visitor.Key == "u:42" && v.Visitor.Nickname != "回读新昵称" {
			t.Fatal("visitor name remained stale")
		}
		if v.Visitor.Key == "u:77" && (v.Source.Forwarder == nil || v.Source.Forwarder.Nickname != "回读新昵称") {
			t.Fatal("forwarder name remained stale")
		}
	}
	empty, err := s.Visits(context.Background(), scope, Filter{Owner: "other-owner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(visitItems(t, empty)) != 0 {
		t.Fatal("journey crossed owner authorization")
	}
}
