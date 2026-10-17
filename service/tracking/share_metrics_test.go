package tracking

import (
	"context"
	"testing"
	"time"
)

func seedShareMetric(m reviewStore, id, owner, sharer, session, content string, version int, at time.Time) {
	sh := Share{ID: "sid-" + id, Route: "pages/contentreader/contentreader", Content: content, Version: version, Owner: owner, Sharer: sharer, Session: session, Expires: at.Add(30 * 24 * time.Hour)}
	if session == "" {
		sh.Session = ""
	}
	rec := record("share", "tokhash-"+id, owner, at, sh)
	rec.Share, rec.Visitor, rec.Session = sh.ID, sh.Sharer, sh.Session
	_ = m.Put(rec)
}

func seedShareIntent(m reviewStore, owner, sharer, session, shareID, channel string, at time.Time) {
	ev := Event{ID: "intent-" + shareID, Type: "share_intent", Session: session, Route: "pages/contentreader/contentreader", Share: shareID, SourceChannel: channel, Occurred: at, Received: at}
	rec := record("event", ev.ID, owner, at, ev)
	rec.Visitor, rec.Session = sharer, session
	_ = m.Put(rec)
}

func seedDownstreamView(m reviewStore, owner, visitor, session, shareID string, at time.Time, flags ...string) {
	ev := Event{ID: "down-" + visitor + "-" + session + "-" + shareID + "-" + at.Format("150405.000000000"), Type: "page_view", View: evViewID(visitor, session), Session: session, Route: "pages/contentreader/contentreader", Share: shareID, Occurred: at, Received: at, Flags: flags}
	rec := record("event", ev.ID, owner, at, ev)
	rec.Visitor, rec.Session, rec.Share = visitor, session, shareID
	_ = m.Put(rec)
}

func evViewID(visitor, session string) string { return "view-" + visitor + "-" + session }

func TestShareCountsExcludeAdminMintedShares(t *testing.T) {
	s, m, scope, now := reviewFixture()
	reviewPV(m, "pv0000001", "one", "u:1", "session1", "content1", now.Add(-time.Hour))
	seedShareMetric(m, "s1", "one", "u:1", "session1", "content1", 1, now.Add(-time.Hour))
	seedShareMetric(m, "admin1", "one", "u:1", "", "content1", 1, now.Add(-2*time.Hour))
	visitors, e := s.Visitors(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	got := findVisitorMetric(t, visitors, "u:1")
	if got.Shares != 1 {
		t.Fatalf("share_count = %d, want 1 (admin-minted row must not count)", got.Shares)
	}
	if got.RecentShares == nil || *got.RecentShares != 1 {
		t.Fatalf("recent_share_count = %v, want 1", got.RecentShares)
	}
	if got.RecentViews == nil {
		t.Fatal("recent_page_views missing although the default window covers 7 days")
	}
}

func TestShareCountsRecentNilWhenWindowNarrow(t *testing.T) {
	s, m, scope, now := reviewFixture()
	reviewPV(m, "pv0000002", "one", "u:1", "session1", "content1", now.Add(-48*time.Hour))
	seedShareMetric(m, "s1", "one", "u:1", "session1", "content1", 1, now.Add(-48*time.Hour))
	visitors, e := s.Visitors(context.Background(), scope, Filter{Owner: "one", Start: now.Add(-3 * 24 * time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	got := findVisitorMetric(t, visitors, "u:1")
	if got.Shares != 1 {
		t.Fatalf("share_count = %d, want 1", got.Shares)
	}
	if got.RecentShares != nil || got.RecentViews != nil {
		t.Fatalf("recent fields = %v/%v, want nil (window does not reach 7 days)", got.RecentShares, got.RecentViews)
	}
}

func TestVisitorsSortByShares(t *testing.T) {
	s, m, scope, now := reviewFixture()
	reviewPV(m, "pv0000001", "one", "u:1", "session1", "content1", now.Add(-time.Hour))
	reviewPV(m, "pv0000002", "one", "u:2", "session2", "content1", now.Add(-2*time.Hour))
	seedShareMetric(m, "s1", "one", "u:2", "session2", "content1", 1, now.Add(-time.Hour))
	seedShareMetric(m, "s2", "one", "u:2", "session2", "content1", 1, now.Add(-2*time.Hour))
	items, e := s.Visitors(context.Background(), scope, Filter{Owner: "one", Sort: "shares"})
	if e != nil {
		t.Fatal(e)
	}
	list := items.Items.([]Visitor)
	if len(list) != 2 || list[0].Key != "u:2" || list[0].Shares != 2 {
		t.Fatalf("sort by shares = %+v, want u:2 first with 2", list)
	}
}

func TestVisitorSharesListChannelAndDownstream(t *testing.T) {
	s, m, scope, now := reviewFixture()
	catalogSeed(m, "content1", "600㎡别墅全案设计", "one", 1, now.Add(-3*time.Hour))
	seedShareMetric(m, "s1", "one", "u:1", "session1", "content1", 1, now.Add(-time.Hour))
	seedShareMetric(m, "admin1", "one", "u:1", "", "content1", 1, now.Add(-2*time.Hour))
	seedShareIntent(m, "one", "u:1", "session1", "sid-s1", "app_message", now.Add(-time.Hour))
	seedDownstreamView(m, "one", "u:9", "session9", "sid-s1", now.Add(-30*time.Minute))
	seedDownstreamView(m, "one", "u:9", "session9", "sid-s1", now.Add(-29*time.Minute))
	seedDownstreamView(m, "one", "u:8", "session8", "sid-s1", now.Add(-28*time.Minute))
	seedDownstreamView(m, "one", "u:1", "session1", "sid-s1", now.Add(-27*time.Minute), "self")

	list, e := s.VisitorShares(context.Background(), scope, "u:1", Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	items := list.Items.([]VisitorShare)
	if len(items) != 1 {
		t.Fatalf("visitor shares = %d items, want 1 (admin-minted excluded)", len(items))
	}
	got := items[0]
	if got.Title != "600㎡别墅全案设计" || got.Type != "article" {
		t.Fatalf("material = %q/%q, want seeded title", got.Title, got.Type)
	}
	if got.Channel != "app_message" {
		t.Fatalf("channel = %q, want app_message", got.Channel)
	}
	if got.Visitors != 2 || got.Views != 3 {
		t.Fatalf("brought = %d visitors/%d views, want 2/3 (self visit excluded)", got.Visitors, got.Views)
	}
	if got.ID != "sid-s1" {
		t.Fatalf("share id = %q, want sid-s1", got.ID)
	}
}

func TestVisitorSharesEmptyAndDeleted(t *testing.T) {
	s, m, scope, now := reviewFixture()
	list, e := s.VisitorShares(context.Background(), scope, "u:404", Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	if items := list.Items.([]VisitorShare); len(items) != 0 {
		t.Fatalf("unknown visitor shares = %d, want 0", len(items))
	}
	seedShareMetric(m, "s1", "one", "u:1", "session1", "content1", 1, now.Add(-time.Hour))
	if _, e := s.VisitorShares(context.Background(), scope, "u:1", Filter{Owner: "one"}); e != nil {
		t.Fatal(e)
	}
	_ = m.Put(record("deletion", "u:1", "one", now, map[string]string{"reason": "客户要求删除"}))
	if _, e := s.VisitorShares(context.Background(), scope, "u:1", Filter{Owner: "one"}); e == nil {
		t.Fatal("deleted visitor shares readable")
	}
}

func seedVideoProgress(m reviewStore, owner, visitor, session, view, media string, unique, duration int64, at time.Time) {
	pv := Event{ID: view, Type: "page_view", View: view, Session: session, Route: "pages/contentreader/contentreader", Content: "content1", Occurred: at, Received: at}
	rec := record("event", pv.ID, owner, at, pv)
	rec.Visitor, rec.Session, rec.Content = visitor, session, "content1"
	_ = m.Put(rec)
	id := "vid-" + view + media
	ev := Event{ID: id, Type: "video_progress", View: view, Session: session, Route: "pages/contentreader/contentreader", Content: "content1", Media: media, Duration: duration, Unique: unique, Occurred: at.Add(time.Minute), Received: at.Add(time.Minute)}
	rec = record("event", id, owner, at.Add(time.Minute), ev)
	rec.Visitor, rec.Session, rec.Content = visitor, session, "content1"
	_ = m.Put(rec)
}

func TestVisitorContentsVideoProgress(t *testing.T) {
	s, m, scope, now := reviewFixture()
	catalogSeed(m, "content1", "别墅电梯科普视频", "one", 1, now.Add(-3*time.Hour))
	seedVideoProgress(m, "one", "u:1", "session1", "view0000001", "media1", 20000, 60000, now.Add(-time.Hour))
	seedVideoProgress(m, "one", "u:1", "session1", "view0000001", "media1", 45000, 60000, now.Add(-30*time.Minute))
	contents, e := s.VisitorContents(context.Background(), scope, "u:1", Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	items := contents.Items.([]VisitorContent)
	if len(items) != 1 {
		t.Fatalf("visitor contents = %d, want 1", len(items))
	}
	if items[0].VideoUniqueMS != 45000 || items[0].VideoDurationMS != 60000 {
		t.Fatalf("video = %d/%d ms, want 45000/60000 (peak unique, not sum)", items[0].VideoUniqueMS, items[0].VideoDurationMS)
	}
}

func TestContentsShareCounts(t *testing.T) {
	s, m, scope, now := reviewFixture()
	catalogSeed(m, "content1", "标题A", "one", 1, now.Add(-3*time.Hour))
	catalogSeed(m, "content2", "标题B", "one", 1, now.Add(-3*time.Hour))
	seedShareMetric(m, "s1", "one", "u:1", "session1", "content1", 1, now.Add(-time.Hour))
	seedShareMetric(m, "s2", "one", "u:2", "session2", "content1", 1, now.Add(-2*time.Hour))
	seedShareMetric(m, "admin1", "one", "u:2", "", "content1", 1, now.Add(-2*time.Hour))
	contents, e := s.Contents(context.Background(), scope, Filter{Owner: "one"}, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range contents.Items.([]Content) {
		want := 2
		if item.ID == "content2" {
			want = 0
		}
		if item.Shares != want {
			t.Fatalf("content %s share_count = %d, want %d", item.ID, item.Shares, want)
		}
	}
}

func findVisitorMetric(t *testing.T, list List, key string) Visitor {
	t.Helper()
	for _, item := range list.Items.([]Visitor) {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("visitor %q missing", key)
	return Visitor{}
}
