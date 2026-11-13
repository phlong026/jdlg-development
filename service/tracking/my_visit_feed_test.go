package tracking

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func myVisitFeedItems(t *testing.T, list List) []MyVisitFeedItem {
	t.Helper()
	items, ok := list.Items.([]MyVisitFeedItem)
	if !ok {
		t.Fatalf("items type = %T, want []MyVisitFeedItem", list.Items)
	}
	return items
}

func TestMyVisitFeedListsEveryPageViewInsideActorShares(t *testing.T) {
	s, _, scope, now := myVisitorsFixture(t)
	out, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	items := myVisitFeedItems(t, out)
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3: %+v", len(items), items)
	}
	if items[0].View != "view-2" || items[1].View != "view-1b" || items[2].View != "view-1" {
		t.Fatalf("order = %s/%s/%s, want newest page views", items[0].View, items[1].View, items[2].View)
	}
	if items[1].ContentPageViews != 2 || items[2].ContentPageViews != 2 {
		t.Fatalf("u:77 c-1 cumulative views = %d/%d, want 2/2", items[1].ContentPageViews, items[2].ContentPageViews)
	}
	for _, item := range items {
		if item.Visitor.Key == "u:99" || item.Visitor.Key == "u:11" {
			t.Fatalf("other sharer/self visit leaked: %+v", item)
		}
	}
}

func TestMyVisitFeedFiltersBeforePaginationAndBindsCursor(t *testing.T) {
	s, rows, scope, now := myVisitorsFixture(t)
	s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
		return map[int64]VisitorProfile{77: {Nickname: "张先生"}, 88: {Nickname: "李女士"}}, nil
	}
	for _, id := range []string{"share-1", "share-2"} {
		rec := rows["share/token-"+id]
		var share Share
		if err := json.Unmarshal(rec.Payload, &share); err != nil {
			t.Fatal(err)
		}
		share.Channel = "app_message"
		rec.Payload = marshal(share)
		rows["share/token-"+id] = rec
	}
	first, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Search: "", Channel: "app_message", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	items := myVisitFeedItems(t, first)
	if len(items) != 1 || first.Cursor == "" {
		t.Fatalf("first page = %+v cursor=%q", items, first.Cursor)
	}
	second, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Channel: "app_message", Limit: 1, Cursor: first.Cursor})
	secondItems := myVisitFeedItems(t, second)
	if err != nil || len(secondItems) != 1 || secondItems[0].View != "view-1b" {
		t.Fatalf("stable second page: items=%+v err=%v", second.Items, err)
	}
	if _, err := s.MyVisitFeed(context.Background(), scope, 12, Filter{AsOf: now, Channel: "app_message", Limit: 1, Cursor: first.Cursor}); err == nil {
		t.Fatal("cursor must be bound to actor")
	}
	if _, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Channel: "timeline", Limit: 1, Cursor: first.Cursor}); err == nil {
		t.Fatal("cursor must be bound to source filter")
	}
	if _, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Search: "张先生", Channel: "app_message", Limit: 1, Cursor: first.Cursor}); err == nil {
		t.Fatal("cursor must be bound to customer search")
	}
	if _, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Intent: "low", Channel: "app_message", Limit: 1, Cursor: first.Cursor}); err == nil {
		t.Fatal("cursor must be bound to intent filter")
	}
	searched, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Search: "张先生", Intent: "low", Channel: "app_message", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	filtered := myVisitFeedItems(t, searched)
	if len(filtered) != 2 {
		t.Fatalf("filtered page items = %d, want full page of 2: %+v", len(filtered), filtered)
	}
	for _, item := range filtered {
		if item.Visitor.Nickname != "张先生" || item.Visitor.Intent.Level != "low" || item.Source.Channel != "app_message" {
			t.Fatalf("q/intent/channel filtering happened after pagination: %+v", item)
		}
	}
}

func TestMyVisitFeedCursorBindsShareWindow(t *testing.T) {
	s, rows, scope, now := myVisitorsFixture(t)
	for i := 0; i < 401; i++ {
		seedMyShare(rows, fmt.Sprintf("recent-%04d", i), "company", "u:11", now.Add(-time.Duration(i+1)*time.Second))
	}
	visitPV(rows, "recent-view-a", "company", "u:77", "recent-session", "recent-content", "recent-0000", now.Add(-500*time.Millisecond), 0)
	visitPV(rows, "recent-view-b", "company", "u:77", "recent-session", "recent-content", "recent-0000", now.Add(-250*time.Millisecond), 0)

	first, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if first.Cursor == "" || first.ShareWindow == nil || first.ShareWindow.NextCursor == "" {
		t.Fatalf("missing detail/share cursors: %+v", first)
	}
	if _, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Limit: 1, Cursor: first.Cursor, ShareCursor: first.ShareWindow.NextCursor}); err == nil {
		t.Fatal("detail cursor must be bound to share_cursor")
	}
}

func TestMyVisitFeedScansShareEventsOnceForDisplayAndIntentWindows(t *testing.T) {
	s, rows, scope, now := myVisitorsFixture(t)
	probe := &myShareQueryStore{reviewStore: rows}
	s.Store = probe
	if _, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Start: now.Add(-time.Hour), Limit: 20}); err != nil {
		t.Fatal(err)
	}
	broadEventScans := 0
	for _, query := range probe.queries {
		if query.kind == "event" && len(query.q.Shares) > 0 && len(query.q.Types) == 0 {
			broadEventScans++
		}
	}
	if broadEventScans != 1 {
		t.Fatalf("broad share event scans = %d, want 1: %+v", broadEventScans, probe.queries)
	}
}

func TestMyVisitFeedReusesOneScanWithinReadBudget(t *testing.T) {
	s, rows, scope, now := myVisitorsFixture(t)
	for i := 0; i < maxQueryRecords/2; i++ {
		visitPV(rows, fmt.Sprintf("budget-view-%05d", i), "company", "u:77", "budget-session", "budget-content", "share-1", now.Add(-30*time.Minute+time.Duration(i)*time.Nanosecond), 0)
	}
	out, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Start: now.Add(-time.Hour), Limit: 1})
	if err != nil {
		t.Fatalf("single scan should remain inside read budget: %v", err)
	}
	if len(myVisitFeedItems(t, out)) != 1 {
		t.Fatalf("budget page = %+v, want one item", out.Items)
	}
}

func TestMyVisitFeedDeduplicatesMaterialReadsWithinOwner(t *testing.T) {
	base := Share{Route: projectCollectionReaderRoute, Content: "collection-a", Version: 1, Owner: "company"}
	shares := []Share{
		base,
		base,
		{Route: base.Route, Content: base.Content, Version: base.Version, Owner: "other-company"},
		{Route: base.Route, Content: "collection-b", Version: base.Version, Owner: base.Owner},
	}
	rows := myVisitFeedMaterialRows(shares)
	if len(rows) != 3 {
		t.Fatalf("material rows = %d, want 3 owner/content scoped reads: %+v", len(rows), rows)
	}
}

func TestMyVisitFeedContentCountUsesSelectedDateWindow(t *testing.T) {
	s, _, scope, now := myVisitorsFixture(t)
	out, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{
		AsOf:  now,
		Start: now.Add(-49*time.Minute - 30*time.Second),
		Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	items := myVisitFeedItems(t, out)
	if len(items) != 2 {
		t.Fatalf("date-window items = %d, want 2: %+v", len(items), items)
	}
	for _, item := range items {
		if item.View == "view-1b" && item.ContentPageViews != 1 {
			t.Fatalf("windowed cumulative views = %d, want 1: %+v", item.ContentPageViews, item)
		}
	}
}

func TestMyVisitFeedAppliesDeletionBeforeCards(t *testing.T) {
	s, rows, scope, now := myVisitorsFixture(t)
	_ = rows.Put(record("deletion", "u:77", "company", now.Add(-time.Minute), map[string]any{"deleted_at": now.Add(-time.Minute)}))
	out, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range myVisitFeedItems(t, out) {
		if item.Visitor.Key == "u:77" {
			t.Fatalf("deleted visitor reappeared: %+v", item)
		}
	}
}

func TestMyVisitFeedMergesAnonymousHistoryIntoIdentifiedCustomer(t *testing.T) {
	s, rows, scope, now := myVisitorsFixture(t)
	visitPV(rows, "anonymous-view", "company", "a:device", "anonymous-session", "c-1", "share-1", now.Add(-45*time.Minute), 0)
	_ = rows.Put(record("identity", "anonymous-session", "company", now.Add(-44*time.Minute), map[string]string{
		"original_visitor_key": "a:device",
		"visitor_key":          "u:77",
	}))

	out, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range myVisitFeedItems(t, out) {
		if item.Visitor.Key == "a:device" {
			t.Fatalf("anonymous identity was not merged: %+v", item)
		}
		if item.View == "anonymous-view" {
			found = true
			if item.Visitor.Key != "u:77" || item.ContentPageViews != 3 {
				t.Fatalf("merged card = %+v, want u:77 with three c-1 views", item)
			}
		}
	}
	if !found {
		t.Fatal("merged anonymous page view missing")
	}
}

func TestMyVisitFeedRejectsActorAndTenantScopeMismatch(t *testing.T) {
	s, _, scope, now := myVisitorsFixture(t)
	if _, err := s.MyVisitFeed(context.Background(), scope, 12, Filter{AsOf: now}); err == nil {
		t.Fatal("scope actor mismatch accepted")
	}
	scope.Tenant = "other-tenant"
	if _, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now}); err == nil {
		t.Fatal("cross-tenant scope accepted")
	}
}

func TestMyVisitFeedUsesUniquePlayedCoverageInsteadOfSeekPosition(t *testing.T) {
	s, rows, scope, now := myVisitorsFixture(t)
	rec := rows["event/video-1"]
	var event Event
	if err := json.Unmarshal(rec.Payload, &event); err != nil {
		t.Fatal(err)
	}
	event.Media = "video-1"
	event.Position = 9000
	event.Duration = 10000
	event.Unique = 2000
	event.Ranges = [][2]int64{{0, 2000}}
	rec.Payload = marshal(event)
	rows["event/video-1"] = rec
	visitEvent(rows, "video-seek", "company", "u:77", "session-1", "view-1", "c-1", now.Add(-47*time.Minute), Event{ID: "video-seek", Type: "video_progress", Share: "share-1", Media: "video-1", Watch: 1000, Position: 9000, Duration: 10000})
	seek := rows["event/video-seek"]
	seek.Share = "share-1"
	rows["event/video-seek"] = seek

	out, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	items := myVisitFeedItems(t, out)
	for _, item := range items {
		if item.View == "view-1" {
			if !item.ProgressAvailable || item.Progress != 20 || item.VideoWatch != 4000 {
				t.Fatalf("video progress = available:%v pct:%d watch:%d, want true/20/4000", item.ProgressAvailable, item.Progress, item.VideoWatch)
			}
			return
		}
	}
	t.Fatal("view-1 missing")
}

func TestMyVisitFeedBackfilledCollectionContentKeysDoNotCrossCount(t *testing.T) {
	s, rows, scope, now := myVisitorsFixture(t)
	collections := map[string]string{"share-1": "collection-a", "share-2": "collection-b"}
	for shareID, collectionID := range collections {
		key := "share/token-" + shareID
		rec := rows[key]
		var share Share
		if err := json.Unmarshal(rec.Payload, &share); err != nil {
			t.Fatal(err)
		}
		share.Route, share.Content, share.Version = projectCollectionReaderRoute, collectionID, 1
		rec.Payload = marshal(share)
		rows[key] = rec
	}
	visitPV(rows, "view-2b", "company", "u:77", "session-3", "", "share-2", now.Add(-38*time.Minute), 0)
	for _, viewID := range []string{"view-1", "view-1b", "view-2", "view-2b"} {
		key := "event/" + viewID
		rec := rows[key]
		var event Event
		if err := json.Unmarshal(rec.Payload, &event); err != nil {
			t.Fatal(err)
		}
		event.Route, event.Content, event.Version = projectCollectionReaderRoute, "", 0
		rec.Content, rec.Payload = "", marshal(event)
		rows[key] = rec
	}

	out, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, item := range myVisitFeedItems(t, out) {
		if item.Visitor.Key != "u:77" {
			continue
		}
		seen++
		want := 2
		if item.ShareID == "share-2" {
			want = 1
		}
		if item.ContentPageViews != want {
			t.Fatalf("share-backfilled collection count = %d, want %d: %+v", item.ContentPageViews, want, item)
		}
	}
	if seen != 3 {
		t.Fatalf("u:77 collection cards = %d, want 3", seen)
	}
}

func TestMyVisitFeedDoesNotInventProgressWithoutCoverage(t *testing.T) {
	s, _, scope, now := myVisitorsFixture(t)
	out, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range myVisitFeedItems(t, out) {
		if item.View == "view-1" && item.ProgressAvailable {
			t.Fatalf("legacy position-only event exposed as coverage: %+v", item)
		}
	}
}

func TestMyVisitFeedEmptyItemsAreConcreteSlice(t *testing.T) {
	s, _, scope, now := myVisitorsFixture(t)
	resolvedIDs := []int64{}
	s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
		resolvedIDs = append(resolvedIDs, ids...)
		return map[int64]VisitorProfile{}, nil
	}
	out, err := s.MyVisitFeed(context.Background(), scope, 11, Filter{AsOf: now, Start: now.Add(-10 * time.Minute), Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if items := myVisitFeedItems(t, out); items == nil || len(items) != 0 {
		t.Fatalf("items = %#v, want non-nil empty slice", items)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"items":[]`) {
		t.Fatalf("empty feed JSON = %s, want items:[]", raw)
	}
	if len(resolvedIDs) != 0 {
		t.Fatalf("rule-window-only visitors reached profile resolver: %v", resolvedIDs)
	}
}
