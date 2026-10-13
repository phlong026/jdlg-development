package tracking

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestVisitorContentsHistoricalVersionsMetricsAndExactVersionDrilldown(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	old := catalogSeed(rows, "material-a", "访问时旧标题", "one", 1, at.Add(-3*time.Hour))
	cover := Asset{ID: "media-cover1", Kind: "image", URL: "/api/v1/miniprogram/tracking/media/media-cover1", Width: 1600, Height: 900}
	old.Cover = &cover
	rows.Put(record("media", cover.ID, "one", at.Add(-4*time.Hour), cover))
	rows.Put(record("version", contentKey(old.ID, 1), "one", at.Add(-3*time.Hour), old))
	catalogSeed(rows, "material-a", "访问时新标题", "one", 2, at.Add(-2*time.Hour))
	catalogSeed(rows, "material-b", "另一素材", "one", 1, at.Add(-2*time.Hour))
	catalogSeed(rows, "material-unvisited", "未访问", "one", 1, at.Add(-2*time.Hour))
	catalogVisit(rows, "visit-old1", "one", "u:1", "shared-session", "material-a", 1, at.Add(-time.Hour), 1000)
	catalogVisit(rows, "visit-old2", "one", "u:1", "other-session", "material-a", 1, at.Add(-50*time.Minute), 2000)
	catalogVisit(rows, "visit-new1", "one", "u:1", "shared-session", "material-a", 2, at.Add(-40*time.Minute), 3000)
	catalogVisit(rows, "visit-b1", "one", "u:1", "third-session", "material-b", 1, at.Add(-30*time.Minute), 4000)
	catalogVisit(rows, "visit-other", "one", "u:2", "fourth-session", "material-a", 1, at.Add(-20*time.Minute), 5000)
	reviewPV(rows, "general-page", "one", "u:1", "general-session", "", at.Add(-10*time.Minute))
	got, err := s.VisitorContents(context.Background(), scope, "u:1", Filter{AsOf: at})
	if err != nil {
		t.Fatal(err)
	}
	items := got.Items.([]VisitorContent)
	if len(items) != 3 || got.OmittedPageCount != 1 {
		t.Fatal(got)
	}
	by := map[string]VisitorContent{}
	for _, v := range items {
		by[contentKey(v.ID, v.Version)] = v
	}
	v1 := by["material-a:1"]
	if !v1.Accessible || v1.Title != "访问时旧标题" || v1.PageViews != 2 || v1.VisibleMS != 3000 || v1.Cover == nil || v1.Cover.Width != 1600 {
		t.Fatal(v1)
	}
	v2 := by["material-a:2"]
	if v2.Title != "访问时新标题" || v2.PageViews != 1 || v2.VisibleMS != 3000 {
		t.Fatal(v2)
	}
	if strings.Contains(string(marshal(got)), "historical body") {
		t.Fatal("body exposed by summary")
	}
	f := Filter{AsOf: at, Content: "material-a", ContentVersion: 1}
	sessions, err := s.Sessions(context.Background(), scope, "u:1", f)
	if err != nil || len(sessions.Items.([]Session)) != 2 {
		t.Fatal(sessions, err)
	}
	for _, ss := range sessions.Items.([]Session) {
		for _, view := range ss.Views {
			if view.Version != 1 {
				t.Fatal("version drift", view)
			}
		}
	}
	_, err = s.Events(context.Background(), scope, "visit-new1", f)
	requireQueryError(t, err, 404)
	scoped, err := s.VisitorContents(context.Background(), scope, "u:1", f)
	if err != nil || len(scoped.Items.([]VisitorContent)) != 1 || scoped.Items.([]VisitorContent)[0].Version != 1 {
		t.Fatal(scoped, err)
	}
}
func TestVisitorContentsRevocationExpirationAndPermissions(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	for _, id := range []string{"material-a", "material-b", "material-c"} {
		catalogSeed(rows, id, "不可泄漏历史标题", "one", 1, at.Add(-2*time.Hour))
		catalogVisit(rows, id+"-visit", "one", "u:1", id+"-session", id, 1, at.Add(-time.Hour), 1000)
	}
	rows.Put(record("content_revoked", "material-a", "one", at, map[string]bool{"revoked": true}))
	rows.Remove("version", "material-b:1")
	// A current object outside the selected owner must not expose an old version.
	moved := Content{ID: "material-c", Owner: "two", Version: 1, Title: "private", Status: "published"}
	rows.Put(record("content", "material-c", "two", at, moved))
	got, err := s.VisitorContents(context.Background(), scope, "u:1", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]VisitorContent{}
	for _, v := range got.Items.([]VisitorContent) {
		by[v.ID] = v
		if v.Accessible || v.Cover != nil || v.Title == "不可泄漏历史标题" || v.PageViews != 1 || v.VisibleMS != 1000 {
			t.Fatal(v)
		}
	}
	if by["material-a"].Status != "revoked" || by["material-b"].Status != "expired" || by["material-c"].Status != "unavailable" {
		t.Fatal(by)
	}
	_, err = s.VisitorContents(context.Background(), scope, "u:9", Filter{})
	requireQueryError(t, err, 404)
	other := scope
	other.Owners = []string{"two"}
	_, err = s.VisitorContents(context.Background(), other, "u:1", Filter{})
	requireQueryError(t, err, 404)
	other = scope
	other.View = false
	_, err = s.VisitorContents(context.Background(), other, "u:1", Filter{})
	requireQueryError(t, err, 403)
	rows.Put(record("deletion", "u:1", "", at, map[string]bool{"deleted": true}))
	_, err = s.VisitorContents(context.Background(), scope, "u:1", Filter{})
	requireQueryError(t, err, 404)
}
func TestVisitorContentPaginationAndNonMaterialPages(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	catalogSeed(rows, "material-a", "材料甲", "one", 1, at.Add(-2*time.Hour))
	catalogSeed(rows, "material-a", "材料乙", "one", 2, at.Add(-2*time.Hour))
	catalogVisit(rows, "view-old1", "one", "u:1", "session1", "material-a", 1, at.Add(-time.Hour), 1000)
	catalogVisit(rows, "view-new1", "one", "u:1", "session2", "material-a", 2, at.Add(-time.Hour), 1000)
	f := Filter{AsOf: at, Limit: 1}
	first, err := s.VisitorContents(context.Background(), scope, "u:1", f)
	if err != nil || first.Cursor == "" || first.FirstCursor == "" {
		t.Fatal(first, err)
	}
	f.Cursor = first.Cursor
	second, err := s.VisitorContents(context.Background(), scope, "u:1", f)
	if err != nil || second.Items.([]VisitorContent)[0].Version == first.Items.([]VisitorContent)[0].Version {
		t.Fatal(second, err)
	}
	f.Cursor = first.FirstCursor
	back, err := s.VisitorContents(context.Background(), scope, "u:1", f)
	if err != nil || back.Items.([]VisitorContent)[0].Version != first.Items.([]VisitorContent)[0].Version {
		t.Fatal(back, err)
	}
	f.Sort = "page_views"
	_, err = s.VisitorContents(context.Background(), scope, "u:1", f)
	requireQueryError(t, err, 400)
	reviewPV(rows, "general-only", "one", "a:random", "general-session", "", at.Add(-time.Hour))
	empty, err := s.VisitorContents(context.Background(), scope, "a:random", Filter{})
	if err != nil || len(empty.Items.([]VisitorContent)) != 0 || empty.OmittedPageCount != 1 {
		t.Fatal(empty, err)
	}
}
func TestVisitorContentsLegacySnapshotsRecheckPublicSource(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	route := "pages/inspirationdetail/inspirationdetail"
	id := LegacyID(route, 1)
	available := true
	sourceError := error(nil)
	s.Config.Routes[route] = Route{Title: "原业务素材", Public: true}
	s.ResolveLegacyContext = func(context.Context, string, int64) (LegacyMetadata, error) {
		if sourceError != nil {
			return LegacyMetadata{}, sourceError
		}
		if !available {
			return LegacyMetadata{}, fail(404, "hidden")
		}
		return LegacyMetadata{Title: "当前标题", VersionSeed: "new", CoverURL: "https://public.example/new.jpg"}, nil
	}
	snapshot := Content{ID: id, Version: 12, Title: "访问版本标题", Owner: "one", Status: "published", ReadOnly: true, Route: route, Cover: &Asset{ID: "legacy-cover", Kind: "image", URL: "https://public.example/old.jpg"}}
	if err := saveLegacy(rows, snapshot, "one", at.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	event := Event{ID: "legacy-visit", Type: "page_view", View: "legacy-visit", Route: route, Content: id, Version: 12, Session: "legacy-session", Occurred: at.Add(-time.Hour), Received: at.Add(-time.Hour)}
	row := record("event", event.ID, "one", event.Occurred, event)
	row.Visitor, row.Session, row.Content = "u:1", event.Session, id
	rows.Put(row)
	got, err := s.VisitorContents(context.Background(), scope, "u:1", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	item := got.Items.([]VisitorContent)[0]
	if !item.Accessible || item.Title != "访问版本标题" || item.Cover == nil || item.Cover.URL != "https://public.example/old.jpg" {
		t.Fatal(item)
	}
	available = false
	got, err = s.VisitorContents(context.Background(), scope, "u:1", Filter{})
	if err != nil || got.Items.([]VisitorContent)[0].Accessible || got.Items.([]VisitorContent)[0].Cover != nil {
		t.Fatal(got, err)
	}
	sourceError = errors.New("isolated source failure")
	_, err = s.VisitorContents(context.Background(), scope, "u:1", Filter{})
	if !errors.Is(err, sourceError) {
		t.Fatal(err)
	}
}

func TestVisitorContentsRetainedSummaryUsesActualVersionAndClippedTime(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	catalogSeed(rows, "retained-material", "保留版本标题", "one", 7, at.Add(-48*time.Hour))
	catalogVisit(rows, "retained-visit", "one", "u:1", "retained-session", "retained-material", 7, at.Add(-time.Hour), 5000)
	if err := s.RebuildSessionSummaries(rows, "retained-session", at); err != nil {
		t.Fatal(err)
	}
	rows.Put(record("summary_state", "retained-session", "one", at, summaryState{Metric: MetricVersion, RawExpired: true}))
	rows.Remove("event", "retained-visit")
	rows.Remove("event", "retained-visit-visible")
	f := Filter{AsOf: at, Content: "retained-material", ContentVersion: 7, Start: at.Add(-time.Hour + 2*time.Second), End: at}
	got, err := s.VisitorContents(context.Background(), scope, "u:1", f)
	if err != nil {
		t.Fatal(err)
	}
	items := got.Items.([]VisitorContent)
	if len(items) != 1 || items[0].Version != 7 || items[0].PageViews != 0 || items[0].VisibleMS != 3000 || items[0].Title != "保留版本标题" {
		t.Fatal(got)
	}
	f.ContentVersion = 8
	_, err = s.VisitorContents(context.Background(), scope, "u:1", f)
	requireQueryError(t, err, 404)
}
