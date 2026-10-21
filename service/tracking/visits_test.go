package tracking

import (
	"context"
	"testing"
	"time"
)

// visitPV seeds one reader page view entered through share (empty = direct),
// plus a visible segment when visible > 0.
func visitPV(m reviewStore, id, owner, key, session, content, share string, at time.Time, visible int64, flags ...string) {
	ev := Event{ID: id, Type: "page_view", View: id, Session: session, Route: publishedReaderRoute, Content: content, Version: 1, Share: share, Occurred: at, Received: at, Flags: flags}
	rec := record("event", id, owner, at, ev)
	rec.Visitor, rec.Session, rec.Content, rec.Share = key, session, content, share
	_ = m.Put(rec)
	if visible > 0 {
		seg := Event{ID: id + "-visible", Type: "visible_segment", View: id, Session: session, Route: publishedReaderRoute, Content: content, Version: 1, Share: share, Occurred: at.Add(time.Duration(visible) * time.Millisecond), Delta: visible, Flags: flags}
		seg.Received = seg.Occurred
		rec = record("event", seg.ID, owner, seg.Occurred, seg)
		rec.Visitor, rec.Session, rec.Content, rec.Share = key, session, content, share
		_ = m.Put(rec)
	}
}

// visitEvent seeds an engagement event (video/scroll) on an existing view.
func visitEvent(m reviewStore, id, owner, key, session, view, content string, at time.Time, ev Event) {
	ev.ID, ev.View, ev.Session, ev.Route, ev.Content, ev.Version, ev.Occurred, ev.Received = id, view, session, publishedReaderRoute, content, 1, at, at
	rec := record("event", id, owner, at, ev)
	rec.Visitor, rec.Session, rec.Content = key, session, content
	_ = m.Put(rec)
}

func visitsFixture(t *testing.T) (*Service, reviewStore, Scope, time.Time) {
	t.Helper()
	s, m, scope, now := reviewFixture()
	s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
		names := map[int64]string{1: "灿坤", 2: "雪峰行者", 3: "心随我动", 100: "销售小王"}
		out := map[int64]VisitorProfile{}
		for _, id := range ids {
			if name, ok := names[id]; ok {
				out[id] = VisitorProfile{Nickname: name}
			}
		}
		return out, nil
	}
	// u:100 is the salesperson operating owner "one".
	s.IsContentOperator = func(_ context.Context, account int64, owner string) (bool, error) {
		return account == 100 && owner == "one", nil
	}
	catalogSeed(m, "content1", "南安石堤寺规划", "one", 1, now.Add(-48*time.Hour))
	catalogSeed(m, "content2", "闽南古厝改造", "one", 1, now.Add(-48*time.Hour))
	return s, m, scope, now
}

func visitItems(t *testing.T, list List) []Visit {
	t.Helper()
	items, ok := list.Items.([]Visit)
	if !ok {
		t.Fatalf("items type = %T, want []Visit", list.Items)
	}
	return items
}

func findVisit(t *testing.T, items []Visit, visitor, content string) Visit {
	t.Helper()
	for _, item := range items {
		if item.Visitor.Key == visitor && item.Content.ID == content {
			return item
		}
	}
	t.Fatalf("visit %s/%s missing in %+v", visitor, content, items)
	return Visit{}
}

func TestVisitsOneCardPerSessionAndContent(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	// Session 1: two views of content1 (a refresh) merge into one card.
	visitPV(m, "pv-a1", "one", "u:1", "session1", "content1", "", now.Add(-5*time.Hour), 30000)
	visitPV(m, "pv-a2", "one", "u:1", "session1", "content1", "", now.Add(-5*time.Hour+time.Minute), 20000)
	// A plain page without material never becomes a card.
	reviewPV(m, "pv-home", "one", "u:1", "session1", "", now.Add(-5*time.Hour+2*time.Minute))
	// Session 2: the same person comes back the next day.
	visitPV(m, "pv-a3", "one", "u:1", "session2", "content1", "", now.Add(-time.Hour), 10000)
	visitPV(m, "pv-b1", "one", "u:2", "session3", "content2", "", now.Add(-3*time.Hour), 5000)

	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	items := visitItems(t, list)
	if len(items) != 3 {
		t.Fatalf("visits = %d, want 3 (refresh merged, plain page excluded): %+v", len(items), items)
	}
	if items[0].Visitor.Key != "u:1" || items[1].Visitor.Key != "u:2" || items[2].Visitor.Key != "u:1" {
		t.Fatalf("order = %s,%s,%s, want newest first", items[0].Visitor.Key, items[1].Visitor.Key, items[2].Visitor.Key)
	}
	older := items[2]
	if older.Visible != 50000 {
		t.Fatalf("merged visible = %d, want 50000", older.Visible)
	}
	if !older.Started.Equal(now.Add(-5 * time.Hour)) {
		t.Fatalf("merged start = %s, want first view", older.Started)
	}
	if older.ContentViews != 3 || items[0].ContentViews != 3 {
		t.Fatalf("content_page_views = %d/%d, want 3 for both cards of u:1", older.ContentViews, items[0].ContentViews)
	}
	if older.Content.Title != "南安石堤寺规划" || older.Visitor.Nickname != "灿坤" {
		t.Fatalf("card = %q by %q, want seeded title and nickname", older.Content.Title, older.Visitor.Nickname)
	}
	if older.Source.Kind != "direct" {
		t.Fatalf("source = %q, want direct", older.Source.Kind)
	}
	if list.Identified != 2 || list.Anonymous != 0 {
		t.Fatalf("counts = %d/%d, want 2 identified visitors", list.Identified, list.Anonymous)
	}
}

func TestVisitsSourceChannelAndForwarder(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	at := now.Add(-6 * time.Hour)
	// The salesperson sends privately: no forwarder shown.
	seedShareMetric(m, "sales", "one", "u:100", "session-sales", "content1", 1, at)
	seedShareIntent(m, "one", "u:100", "session-sales", "sid-sales", "app_message", at)
	visitPV(m, "pv-1", "one", "u:3", "session-3", "content1", "sid-sales", at.Add(time.Minute), 1000)
	// 心随我动 (u:3) forwards to Moments; 雪峰行者 (u:2) opens it.
	seedShareMetric(m, "fwd", "one", "u:3", "session-3", "content1", 1, at.Add(2*time.Minute))
	seedShareIntent(m, "one", "u:3", "session-3", "sid-fwd", "timeline", at.Add(2*time.Minute))
	visitPV(m, "pv-2", "one", "u:2", "session-2", "content1", "sid-fwd", at.Add(3*time.Minute), 1000)
	// An anonymous reader forwards without a reported channel.
	visitPV(m, "pv-anon", "one", "a:reader", "session-anon", "content2", "", at.Add(4*time.Minute), 1000)
	seedShareMetric(m, "anon", "one", "a:reader", "session-anon", "content2", 1, at.Add(5*time.Minute))
	visitPV(m, "pv-3", "one", "u:1", "session-1", "content2", "sid-anon", at.Add(6*time.Minute), 1000)
	// A backend-minted code/link has no mini-program session.
	seedShareMetric(m, "code", "one", "u:100", "", "content2", 1, at)
	visitPV(m, "pv-4", "one", "u:2", "session-2b", "content2", "sid-code", at.Add(7*time.Minute), 1000)

	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	items := visitItems(t, list)
	private := findVisit(t, items, "u:3", "content1")
	if private.Source.Kind != "share" || private.Source.Channel != "app_message" || private.Source.Forwarder != nil {
		t.Fatalf("salesperson share = %+v, want share/app_message without forwarder", private.Source)
	}
	forwarded := findVisit(t, items, "u:2", "content1")
	if forwarded.Source.Channel != "timeline" || forwarded.Source.Forwarder == nil || forwarded.Source.Forwarder.Nickname != "心随我动" {
		t.Fatalf("forwarded = %+v, want timeline via 心随我动", forwarded.Source)
	}
	anon := findVisit(t, items, "u:1", "content2")
	if anon.Source.Kind != "share" || anon.Source.Channel != "" || anon.Source.Forwarder == nil || anon.Source.Forwarder.State != "anonymous" {
		t.Fatalf("anonymous forward = %+v, want unreported channel and anonymous forwarder", anon.Source)
	}
	code := findVisit(t, items, "u:2", "content2")
	if code.Source.Kind != "code_link" || code.Source.Forwarder != nil {
		t.Fatalf("code visit = %+v, want code_link", code.Source)
	}

	only, e := s.Visits(context.Background(), scope, Filter{Owner: "one", Channel: "timeline"})
	if e != nil {
		t.Fatal(e)
	}
	if got := visitItems(t, only); len(got) != 1 || got[0].Visitor.Key != "u:2" {
		t.Fatalf("timeline filter = %+v, want only 雪峰行者", got)
	}
	codes, e := s.Visits(context.Background(), scope, Filter{Owner: "one", Channel: "code_link"})
	if e != nil {
		t.Fatal(e)
	}
	if got := visitItems(t, codes); len(got) != 1 || got[0].Source.Kind != "code_link" {
		t.Fatalf("code_link filter = %+v", got)
	}
	if _, e := s.Visits(context.Background(), scope, Filter{Owner: "one", Channel: "fax"}); e == nil {
		t.Fatal("unknown channel accepted")
	}
}

func TestVisitsProgressPlaysAndEngagement(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	at := now.Add(-2 * time.Hour)
	// Video: 60s long, 48s unique coverage, watched 150s in total (replays).
	visitPV(m, "pv-video", "one", "u:1", "session1", "content1", "", at, 20000)
	for i := 0; i < 10; i++ {
		visitEvent(m, "video-"+string(rune('a'+i)), "one", "u:1", "session1", "pv-video", "content1", at.Add(time.Duration(i+1)*time.Second), Event{Type: "video_progress", Media: "media1", Duration: 60000, Unique: int64(4800 * (i + 1)), Watch: 15000})
	}
	// Article: scrolled to 80%, a short but repeated reader.
	visitPV(m, "pv-read", "one", "u:2", "session2", "content2", "", at, 5000)
	visitEvent(m, "scroll-1", "one", "u:2", "session2", "pv-read", "content2", at.Add(time.Second), Event{Type: "scroll_depth", Scroll: 40})
	visitEvent(m, "scroll-2", "one", "u:2", "session2", "pv-read", "content2", at.Add(2*time.Second), Event{Type: "scroll_depth", Scroll: 80})
	// A long single visit without progress data.
	visitPV(m, "pv-long", "one", "u:3", "session3", "content2", "", at, 75000)

	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	items := visitItems(t, list)
	video := findVisit(t, items, "u:1", "content1")
	if video.Progress == nil || video.Progress.Kind != "video" || video.Progress.Percent != 80 || video.Progress.Plays != 2 {
		t.Fatalf("video progress = %+v, want 80%% and 2 plays", video.Progress)
	}
	if !video.Engaged {
		t.Fatal("video at 80% should be engaged")
	}
	article := findVisit(t, items, "u:2", "content2")
	if article.Progress == nil || article.Progress.Kind != "article" || article.Progress.Percent != 80 || article.Progress.Plays != 0 {
		t.Fatalf("article progress = %+v, want read to 80%%", article.Progress)
	}
	if article.Engaged {
		t.Fatal("5s single read should not be engaged")
	}
	long := findVisit(t, items, "u:3", "content2")
	if long.Progress != nil {
		t.Fatalf("progress = %+v, want omitted without data", long.Progress)
	}
	if !long.Engaged {
		t.Fatal("75s visit should be engaged")
	}
}

func TestVisitsEngagedByRepeatViewsAndCappedVideo(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	for i, session := range []string{"s1", "s2", "s3"} {
		visitPV(m, "pv-r"+session, "one", "u:1", session, "content1", "", now.Add(-time.Duration(i+1)*time.Hour), 1000)
	}
	// Coverage reported above duration never exceeds 100%.
	visitEvent(m, "video-cap", "one", "u:1", "s1", "pv-rs1", "content1", now.Add(-time.Hour+time.Second), Event{Type: "video_progress", Media: "media1", Duration: 10000, Unique: 12000, Watch: 9000})
	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	items := visitItems(t, list)
	if len(items) != 3 {
		t.Fatalf("visits = %d, want 3", len(items))
	}
	for _, item := range items {
		if !item.Engaged || item.ContentViews != 3 {
			t.Fatalf("repeat reader card = engaged %v views %d, want engaged with 3 views", item.Engaged, item.ContentViews)
		}
	}
	if items[0].Progress == nil || items[0].Progress.Percent != 100 || items[0].Progress.Plays != 0 {
		t.Fatalf("capped progress = %+v, want 100%% and no plays", items[0].Progress)
	}
}

func TestVisitsExcludeSelfAndRequireView(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	visitPV(m, "pv-self", "one", "u:100", "session-self", "content1", "", now.Add(-time.Hour), 1000, "self")
	visitPV(m, "pv-preview", "one", "u:100", "session-preview", "content1", "", now.Add(-time.Hour), 1000, "preview")
	visitPV(m, "pv-real", "one", "u:1", "session1", "content1", "", now.Add(-time.Hour), 1000)
	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	if items := visitItems(t, list); len(items) != 1 || items[0].Visitor.Key != "u:1" {
		t.Fatalf("visits = %+v, want only the real reader", items)
	}
	scope.View = false
	if _, e := s.Visits(context.Background(), scope, Filter{Owner: "one"}); e == nil {
		t.Fatal("visits readable without view permission")
	}
}

func TestVisitsIntentMatchesVisitorList(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	visitPV(m, "pv-1", "one", "u:1", "session1", "content1", "", now.Add(-2*time.Hour), 1000)
	visitPV(m, "pv-2", "one", "a:anon", "session2", "content1", "", now.Add(-time.Hour), 1000)
	visitors, e := s.Visitors(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range visitItems(t, list) {
		want := findVisitorMetric(t, visitors, item.Visitor.Key)
		if item.Visitor.Intent.Level != want.Intent.Level || item.Visitor.Views != want.Views || item.Visitor.Sessions != want.Sessions {
			t.Fatalf("visit visitor %s = %s/%d/%d, want list %s/%d/%d", item.Visitor.Key, item.Visitor.Intent.Level, item.Visitor.Views, item.Visitor.Sessions, want.Intent.Level, want.Views, want.Sessions)
		}
	}
	anonymous, e := s.Visits(context.Background(), scope, Filter{Owner: "one", Identity: "anonymous"})
	if e != nil {
		t.Fatal(e)
	}
	if got := visitItems(t, anonymous); len(got) != 1 || got[0].Visitor.Key != "a:anon" {
		t.Fatalf("identity filter = %+v, want anonymous reader only", got)
	}
	searched, e := s.Visits(context.Background(), scope, Filter{Owner: "one", Search: "灿坤"})
	if e != nil {
		t.Fatal(e)
	}
	if got := visitItems(t, searched); len(got) != 1 || got[0].Visitor.Key != "u:1" {
		t.Fatalf("search = %+v, want 灿坤 only", got)
	}
}

func TestVisitsPagination(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	for i, session := range []string{"s1", "s2", "s3"} {
		visitPV(m, "pv-"+session, "one", "u:1", session, "content1", "", now.Add(-time.Duration(i+1)*time.Hour), 1000)
	}
	first, e := s.Visits(context.Background(), scope, Filter{Owner: "one", Limit: 2})
	if e != nil {
		t.Fatal(e)
	}
	if len(visitItems(t, first)) != 2 || first.Cursor == "" {
		t.Fatalf("first page = %d items cursor %q, want 2 and a cursor", len(visitItems(t, first)), first.Cursor)
	}
	second, e := s.Visits(context.Background(), scope, Filter{Owner: "one", Limit: 2, AsOf: first.AsOf, Cursor: first.Cursor})
	if e != nil {
		t.Fatal(e)
	}
	got := visitItems(t, second)
	if len(got) != 1 || second.Cursor != "" || !got[0].Started.Equal(now.Add(-3*time.Hour)) {
		t.Fatalf("second page = %+v cursor %q, want the oldest visit and no cursor", got, second.Cursor)
	}
	if _, e := s.Visits(context.Background(), scope, Filter{Owner: "one", Limit: 2, AsOf: first.AsOf, Cursor: first.Cursor, Channel: "timeline"}); e == nil {
		t.Fatal("cursor reused under a different channel filter")
	}
}

func TestShareOriginMarksConsoleAndMiniProgramShares(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	en := enter(t, s, 0)
	read := func(token string) Share {
		t.Helper()
		var sh Share
		if e := s.transaction(ctx, func(r Repository) error {
			var e error
			sh, e = decode[Share](r, "share", hash(token))
			return e
		}); e != nil {
			t.Fatal(e)
		}
		return sh
	}
	mini, e := s.CreateShare(ctx, ShareInput{Route: en.Route, Key: "share-origin-mini"}, "", 42)
	if e != nil {
		t.Fatal(e)
	}
	if got := read(mini["share_token"].(string)); got.Origin != "miniprogram" || got.Session != "" {
		t.Fatalf("logged-in share without entry = %+v, want miniprogram origin and no session", got)
	}
	console, e := s.CreateConsoleShare(ctx, ShareInput{Route: en.Route, Key: "share-origin-console"}, 7)
	if e != nil {
		t.Fatal(e)
	}
	if got := read(console["share_token"].(string)); got.Origin != "console" {
		t.Fatalf("console share = %+v, want console origin", got)
	}
}

func seedOriginShare(m reviewStore, id, owner, sharer, session, content, origin string, at time.Time) {
	sh := Share{ID: "sid-" + id, Route: publishedReaderRoute, Content: content, Version: 1, Owner: owner, Sharer: sharer, Session: session, Origin: origin, Expires: at.Add(30 * 24 * time.Hour)}
	rec := record("share", "tokhash-"+id, owner, at, sh)
	rec.Share, rec.Visitor, rec.Session = sh.ID, sh.Sharer, sh.Session
	_ = m.Put(rec)
}

func TestVisitsSessionlessMiniProgramShareIsAForward(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	at := now.Add(-3 * time.Hour)
	seedOriginShare(m, "mini", "one", "u:3", "", "content1", "miniprogram", at)
	visitPV(m, "pv-mini", "one", "u:2", "session-2", "content1", "sid-mini", at.Add(time.Minute), 1000)
	seedOriginShare(m, "console", "one", "u:7", "", "content2", "console", at)
	visitPV(m, "pv-console", "one", "u:1", "session-1", "content2", "sid-console", at.Add(2*time.Minute), 1000)
	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	items := visitItems(t, list)
	mini := findVisit(t, items, "u:2", "content1")
	if mini.Source.Kind != "share" || mini.Source.Channel != "" || mini.Source.Forwarder == nil || mini.Source.Forwarder.Nickname != "心随我动" {
		t.Fatalf("sessionless mini share = %+v, want an unreported-channel forward by 心随我动", mini.Source)
	}
	if console := findVisit(t, items, "u:1", "content2"); console.Source.Kind != "code_link" || console.Source.Forwarder != nil {
		t.Fatalf("console share = %+v, want code_link", console.Source)
	}
}

func TestVisitsForwarderIdentityAndDeletion(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	at := now.Add(-4 * time.Hour)
	// An anonymous reader shared and then logged in inside the same session.
	seedShareMetric(m, "anon", "one", "a:reader", "session-a", "content1", 1, at)
	link := record("identity", "session-a", "one", at.Add(time.Minute), map[string]string{"original_visitor_key": "a:reader", "visitor_key": "u:3"})
	link.Session = "session-a"
	_ = m.Put(link)
	visitPV(m, "pv-1", "one", "u:2", "session-2", "content1", "sid-anon", at.Add(2*time.Minute), 1000)
	// The salesperson shared anonymously before logging in: still not a forward.
	seedShareMetric(m, "sales", "one", "a:sales", "session-s", "content2", 1, at)
	salesLink := record("identity", "session-s", "one", at.Add(time.Minute), map[string]string{"original_visitor_key": "a:sales", "visitor_key": "u:100"})
	salesLink.Session = "session-s"
	_ = m.Put(salesLink)
	visitPV(m, "pv-2", "one", "u:2", "session-2b", "content2", "sid-sales", at.Add(3*time.Minute), 1000)
	// Deleted sharers never reappear as forwarders, including mid staged deletion.
	seedShareMetric(m, "gone", "one", "u:1", "session-g", "content1", 1, at)
	_ = m.Put(record("deletion", "u:1", "one", at, map[string]string{"reason": "客户要求删除"}))
	visitPV(m, "pv-3", "one", "u:3", "session-3", "content1", "sid-gone", at.Add(4*time.Minute), 1000)
	seedShareMetric(m, "staged", "one", "a:staged", "session-x", "content2", 1, at)
	_ = m.Put(record("deletion_session", "session-x", "", at, map[string]any{"deleted_at": at}))
	visitPV(m, "pv-4", "one", "u:3", "session-3b", "content2", "sid-staged", at.Add(5*time.Minute), 1000)

	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	items := visitItems(t, list)
	if got := findVisit(t, items, "u:2", "content1").Source.Forwarder; got == nil || got.Key != "u:3" || got.Nickname != "心随我动" {
		t.Fatalf("logged-in forwarder = %+v, want resolved u:3 心随我动", got)
	}
	if got := findVisit(t, items, "u:2", "content2").Source.Forwarder; got != nil {
		t.Fatalf("salesperson forwarder = %+v, want none after identity resolution", got)
	}
	if got := findVisit(t, items, "u:3", "content1").Source.Forwarder; got != nil {
		t.Fatalf("deleted forwarder = %+v, want none", got)
	}
	if got := findVisit(t, items, "u:3", "content2").Source.Forwarder; got != nil {
		t.Fatalf("staged-deletion forwarder = %+v, want none", got)
	}
}

func TestVisitsNeverReadAnotherOwnersShare(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	at := now.Add(-2 * time.Hour)
	seedShareMetric(m, "foreign", "two", "u:3", "session-f", "content1", 1, at)
	seedShareIntent(m, "two", "u:3", "session-f", "sid-foreign", "timeline", at)
	visitPV(m, "pv-own", "one", "u:2", "session-2", "content1", "sid-foreign", at.Add(time.Minute), 1000)
	visitPV(m, "pv-other", "two", "u:1", "session-1", "content1", "sid-foreign", at.Add(time.Minute), 1000)
	list, e := s.Visits(context.Background(), scope, Filter{})
	if e != nil {
		t.Fatal(e)
	}
	items := visitItems(t, list)
	if len(items) != 1 {
		t.Fatalf("visits = %+v, want only owner one's visit", items)
	}
	if src := items[0].Source; src.Kind != "share" || src.Channel != "" || src.Forwarder != nil {
		t.Fatalf("source = %+v, want no channel or forwarder from another owner", src)
	}
}

func TestVisitsContentViewsMatchVisitorMaterialCard(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	for i, session := range []string{"s1", "s2"} {
		visitPV(m, "pv-c1-"+session, "one", "u:1", session, "content1", "", now.Add(-time.Duration(i+1)*time.Hour), 2000)
	}
	visitPV(m, "pv-c1-s1b", "one", "u:1", "s1", "content1", "", now.Add(-time.Hour+time.Minute), 2000)
	visitPV(m, "pv-c2", "one", "u:1", "s3", "content2", "", now.Add(-3*time.Hour), 2000)
	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	cards, e := s.VisitorContents(context.Background(), scope, "u:1", Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	views := map[string]int{}
	for _, c := range cards.Items.([]VisitorContent) {
		views[c.ID] = c.PageViews
	}
	for _, item := range visitItems(t, list) {
		if item.ContentViews != views[item.Content.ID] {
			t.Fatalf("%s content_page_views = %d, material card = %d", item.Content.ID, item.ContentViews, views[item.Content.ID])
		}
	}
}

func TestVisitsPageSizeAndPageOnlyForwarders(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	calls := 0
	s.IsContentOperator = func(context.Context, int64, string) (bool, error) { calls++; return false, nil }
	for i := 0; i < 60; i++ {
		id := "f" + time.Duration(i).String()
		sharer := "u:" + time.Duration(1000+i).String()
		seedShareMetric(m, id, "one", sharer, "share-session-"+id, "content1", 1, now.Add(-48*time.Hour))
		visitPV(m, "pv-"+id, "one", "u:1", "session-"+id, "content1", "sid-"+id, now.Add(-time.Duration(i+1)*time.Minute), 1000)
	}
	page, e := s.Visits(context.Background(), scope, Filter{Owner: "one"})
	if e != nil {
		t.Fatal(e)
	}
	if got := len(visitItems(t, page)); got != 20 {
		t.Fatalf("default page = %d, want 20", got)
	}
	if calls > 20 {
		t.Fatalf("operator checks = %d, want at most one per served card", calls)
	}
	wide, e := s.Visits(context.Background(), scope, Filter{Owner: "one", Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	if got := len(visitItems(t, wide)); got != 50 {
		t.Fatalf("capped page = %d, want 50", got)
	}
}

func seedTimedShareIntent(m reviewStore, owner, sharer, session, shareID, channel string, occurred, received time.Time) {
	ev := Event{ID: "intent-" + shareID, Type: "share_intent", Session: session, Route: publishedReaderRoute, Share: shareID, SourceChannel: channel, Occurred: occurred, Received: received}
	rec := record("event", ev.ID, owner, occurred, ev)
	rec.Visitor, rec.Session = sharer, session
	_ = m.Put(rec)
}

func TestVisitsChannelToleratesSharerClockBehind(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	at := now.Add(-3 * time.Hour)
	seedShareMetric(m, "skew", "one", "u:3", "session-3", "content1", 1, at)
	// The sharer's device runs 30 minutes slow: the accepted intent predates the share row.
	seedTimedShareIntent(m, "one", "u:3", "session-3", "sid-skew", "timeline", at.Add(-30*time.Minute), at)
	visitPV(m, "pv-skew", "one", "u:2", "session-2", "content1", "sid-skew", at.Add(time.Minute), 1000)
	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one", Channel: "timeline"})
	if e != nil {
		t.Fatal(e)
	}
	if got := visitItems(t, list); len(got) != 1 || got[0].Source.Channel != "timeline" {
		t.Fatalf("skewed intent = %+v, want the timeline visit", got)
	}
}

func TestVisitsChannelIgnoresIntentReceivedAfterSnapshot(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	at := now.Add(-3 * time.Hour)
	asOf := now.Add(-20 * time.Minute)
	seedShareMetric(m, "late", "one", "u:3", "session-3", "content1", 1, at)
	// Queued offline: occurred before the snapshot but uploaded after it.
	seedTimedShareIntent(m, "one", "u:3", "session-3", "sid-late", "timeline", at, now.Add(-10*time.Minute))
	visitPV(m, "pv-late", "one", "u:2", "session-2", "content1", "sid-late", at.Add(time.Minute), 1000)
	list, e := s.Visits(context.Background(), scope, Filter{Owner: "one", AsOf: asOf})
	if e != nil {
		t.Fatal(e)
	}
	if got := visitItems(t, list); len(got) != 1 || got[0].Source.Channel != "" {
		t.Fatalf("snapshot source = %+v, want no channel before the intent was received", got)
	}
	filtered, e := s.Visits(context.Background(), scope, Filter{Owner: "one", AsOf: asOf, Channel: "timeline"})
	if e != nil {
		t.Fatal(e)
	}
	if got := visitItems(t, filtered); len(got) != 0 {
		t.Fatalf("timeline filter at snapshot = %+v, want none", got)
	}
}
