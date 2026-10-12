package tracking

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func catalogVisit(rows reviewStore, id, owner, key, session, content string, version int, at time.Time, visible int64) {
	event := Event{ID: id, Type: "page_view", View: id, Session: session, Route: publishedReaderRoute, Content: content, Version: version, Occurred: at, Received: at}
	rec := record("event", id, owner, at, event)
	rec.Visitor, rec.Session, rec.Content = key, session, content
	rows.Put(rec)
	if visible > 0 {
		event.ID = id + "-visible"
		event.Type = "visible_segment"
		event.Occurred = at.Add(time.Duration(visible) * time.Millisecond)
		event.Received = event.Occurred
		event.Delta = visible
		rec = record("event", event.ID, owner, event.Occurred, event)
		rec.Visitor, rec.Session, rec.Content = key, session, content
		rows.Put(rec)
	}
}
func catalogSeed(rows reviewStore, id, title, owner string, version int, at time.Time) Content {
	c := Content{ID: id, Title: title, Owner: owner, Version: version, Status: "published", Type: "article", Body: "historical body"}
	rows.Put(record("content", id, owner, at, c))
	rows.Put(record("version", contentKey(id, version), owner, at, c))
	return c
}
func TestContentTitleVisitorIdentityANDAndScopedUV(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	catalogSeed(rows, "material-a", "庭院甲", "one", 1, at.Add(-2*time.Hour))
	catalogSeed(rows, "material-b", "庭院乙", "one", 1, at.Add(-2*time.Hour))
	catalogSeed(rows, "material-hidden", "庭院私密", "two", 1, at.Add(-2*time.Hour))
	for _, v := range []struct {
		id, key, session, content, owner string
		visible                          int64
	}{
		{"view-a1", "u:1", "session1", "material-a", "one", 1000}, {"view-a2", "u:1", "session2", "material-a", "one", 2000}, {"view-a3", "u:2", "session3", "material-a", "one", 3000}, {"view-a4", "a:random", "session4", "material-a", "one", 4000},
		{"view-b1", "u:1", "session5", "material-b", "one", 5000}, {"view-h1", "u:9", "session6", "material-hidden", "two", 6000},
	} {
		catalogVisit(rows, v.id, v.owner, v.key, v.session, v.content, 1, at.Add(-time.Hour), v.visible)
	}
	s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
		for _, id := range ids {
			if id == 9 {
				t.Fatal("cross owner profile read")
			}
		}
		return map[int64]VisitorProfile{1: {Nickname: "林间访客"}, 2: {Nickname: "石上访客"}}, nil
	}
	got, err := s.Contents(context.Background(), scope, Filter{AsOf: at, ContentSearch: "庭院"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Identified != 2 || got.Anonymous != 1 {
		t.Fatalf("whole-set counts %+v", got)
	}
	by := map[string]Content{}
	for _, c := range got.Items.([]Content) {
		by[c.ID] = c
	}
	a := by["material-a"]
	if a.PageViews != 4 || a.VisibleMS != 10000 || a.Identified != 2 || a.Anonymous != 1 {
		t.Fatal(a)
	}
	f := Filter{AsOf: at, ContentSearch: "甲", VisitorSearch: "林间", Identity: "identified"}
	got, err = s.Contents(context.Background(), scope, f, "")
	if err != nil {
		t.Fatal(err)
	}
	list := got.Items.([]Content)
	if len(list) != 1 || list[0].ID != "material-a" || list[0].PageViews != 2 || list[0].VisibleMS != 3000 || list[0].Identified != 1 || list[0].Anonymous != 0 {
		t.Fatal(got)
	}
	f.Content = "material-a"
	visitors, err := s.Visitors(context.Background(), scope, f)
	if err != nil || len(visitors.Items.([]Visitor)) != 1 || visitors.Items.([]Visitor)[0].Views != 2 || visitors.Items.([]Visitor)[0].Visible != 3000 {
		t.Fatal(visitors, err)
	}
	sessions, err := s.Sessions(context.Background(), scope, "u:1", f)
	if err != nil || len(sessions.Items.([]Session)) != 2 {
		t.Fatal(sessions, err)
	}
	_, err = s.Sessions(context.Background(), scope, "u:2", f)
	requireQueryError(t, err, 404)
	_, err = s.Events(context.Background(), scope, "view-a3", f)
	requireQueryError(t, err, 404)
	f = Filter{Identity: "anonymous"}
	got, err = s.Contents(context.Background(), scope, f, "")
	if err != nil || len(got.Items.([]Content)) != 1 || got.Items.([]Content)[0].PageViews != 1 || got.Items.([]Content)[0].VisibleMS != 4000 {
		t.Fatal(got, err)
	}
	scope.View = false
	_, err = s.Contents(context.Background(), scope, Filter{VisitorSearch: "林间"}, "")
	requireQueryError(t, err, 403)
}
func TestContentFilteredPaginationRenameLateReportAndSearchValidation(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	name := "测试访客"
	s.ResolveVisitorProfiles = func(context.Context, []int64) (map[int64]VisitorProfile, error) {
		return map[int64]VisitorProfile{1: {Nickname: name}}, nil
	}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("material-%d", i)
		catalogSeed(rows, id, "搜索素材", "one", 1, at.Add(-2*time.Hour))
		catalogVisit(rows, fmt.Sprintf("view-%d", i), "one", "u:1", fmt.Sprintf("session-%d", i), id, 1, at.Add(-time.Hour), 1000)
	}
	f := Filter{AsOf: at, Limit: 1, VisitorSearch: "测试", ContentSearch: "搜索"}
	first, err := s.Contents(context.Background(), scope, f, "")
	if err != nil || first.Cursor == "" {
		t.Fatal(first, err)
	}
	// A late upload is excluded by received_at despite its older occurred_at.
	event := Event{ID: "view-late", Type: "page_view", View: "view-late", Session: "session-late", Route: publishedReaderRoute, Content: "material-1", Version: 1, Occurred: at.Add(-time.Hour), Received: at.Add(time.Minute)}
	rec := record("event", event.ID, "one", event.Occurred, event)
	rec.Visitor, rec.Session, rec.Content = "u:1", event.Session, event.Content
	rows.Put(rec)
	f.Cursor = first.Cursor
	second, err := s.Contents(context.Background(), scope, f, "")
	if err != nil || second.Items.([]Content)[0].ID == first.Items.([]Content)[0].ID || second.Identified != 1 {
		t.Fatal(second, err)
	}
	f.Cursor = first.FirstCursor
	back, err := s.Contents(context.Background(), scope, f, "")
	if err != nil || back.Items.([]Content)[0].ID != first.Items.([]Content)[0].ID {
		t.Fatal(back, err)
	}
	name = "改名"
	_, err = s.Contents(context.Background(), scope, f, "")
	requireQueryError(t, err, 409)
	name = "测试访客"
	f.VisitorSearch = "访客"
	_, err = s.Contents(context.Background(), scope, f, "")
	requireQueryError(t, err, 400)
	for _, bad := range []Filter{{ContentVersion: -1}, {ContentVersion: 1}, {ContentSearch: string(make([]byte, 101))}, {VisitorSearch: string(make([]byte, 101))}} {
		_, err = s.Normalize(bad)
		requireQueryError(t, err, 400)
	}
}
