package tracking

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestWorkbenchContentsSortsBeforePaging(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	scope.View = false
	first := catalogSeed(rows, "content-a", "同一主题一", "one", 2, at.Add(-3*time.Hour))
	first.Shares, first.PageViews = 50, 60
	rows.Put(record("content", first.ID, first.Owner, at.Add(-3*time.Hour), first))
	catalogSeed(rows, "content-b", "同一主题二", "one", 1, at.Add(-2*time.Hour))
	catalogSeed(rows, "content-c", "同一主题三", "one", 1, at.Add(-time.Hour))
	catalogSeed(rows, "ignored", "另一个主题", "one", 1, at.Add(-time.Minute))
	catalogSeed(rows, "foreign", "同一主题外部", "two", 1, at.Add(-time.Minute))

	// Two visits by the same person on different versions both count.
	catalogVisit(rows, "page-a1", "one", "u:4", "session-a1", "content-a", 1, at.Add(-30*time.Minute), 2000)
	catalogVisit(rows, "page-a2", "one", "u:4", "session-a2", "content-a", 2, at.Add(-28*time.Minute), 0)
	catalogVisit(rows, "page-b1", "one", "u:5", "session-b", "content-b", 1, at.Add(-30*time.Minute), 0)
	catalogVisit(rows, "page-c1", "one", "u:5", "session-c", "content-c", 1, at.Add(-30*time.Minute), 0)
	catalogVisit(rows, "other-view", "two", "u:6", "session-other", "content-b", 1, at.Add(-30*time.Minute), 0)
	previewAt := at.Add(-20 * time.Minute)
	preview := Event{ID: "preview-visit", Type: "page_view", View: "preview-visit", Session: "session-preview", Content: "content-b", Occurred: previewAt, Received: previewAt, Flags: []string{"preview"}}
	row := record("event", preview.ID, "one", previewAt, preview)
	row.Visitor, row.Session, row.Content = "u:5", preview.Session, preview.Content
	rows.Put(row)
	// Mix retained data with raw traffic and assert that title filtering is
	// pushed into the repositories that supply each source.
	if err := s.RebuildSessionSummaries(rows, "session-a1", at); err != nil {
		t.Fatal(err)
	}
	rows.Put(record("summary_state", "session-a1", "one", at, summaryState{Metric: MetricVersion, RawExpired: true}))
	rows.Remove("event", "page-a1")
	rows.Remove("event", "page-a1-visible")
	indexed := &indexedTestStore{rows: rows, t: t}
	s.Store = indexed

	for _, tt := range []struct {
		sort string
		ids  []string
	}{
		{"", []string{"content-c", "content-b", "content-a"}},
		{"page_views", []string{"content-a", "content-c", "content-b"}},
	} {
		filter := Filter{Sort: tt.sort, Owner: "one", ContentSearch: "同一主题", TypeFilter: "article", AsOf: at, Limit: 1}
		var got []string
		for range tt.ids {
			page, err := s.WorkbenchContents(context.Background(), scope, filter)
			if err != nil {
				t.Fatal(err)
			}
			items := page.Items.([]Content)
			if len(items) != 1 {
				t.Fatalf("got page %+v", page)
			}
			item := items[0]
			if item.CreatedAt == nil {
				t.Fatalf("pre-upgrade content has no creation time: %+v", item)
			}
			if tt.sort == "page_views" {
				want := map[string]int{"content-a": 2, "content-b": 1, "content-c": 1}[item.ID]
				if item.PageViews != want || item.Identified != 0 || item.Anonymous != 0 || item.LastVisit != nil || item.VisibleMS != 0 || item.Shares != 0 {
					t.Fatalf("invalid count or extraneous visitor metrics: %+v", item)
				}
			}
			got = append(got, item.ID)
			filter.Cursor = page.Cursor
		}
		if !slices.Equal(got, tt.ids) || filter.Cursor != "" {
			t.Fatalf("%s order = %v, cursor = %q; want %v", tt.sort, got, filter.Cursor, tt.ids)
		}
	}

	for _, call := range indexed.calls {
		query := call.query
		if call.kind == "daily_summary" || call.kind == "event" && query.Sessions == nil {
			if !slices.Equal(query.ContentIDs, []string{"content-a", "content-b", "content-c"}) {
				t.Fatalf("unbounded %s candidate query: %+v", call.kind, query)
			}
		}
	}

	_, err := s.WorkbenchContents(context.Background(), scope, Filter{Sort: "page_views", VisitorSearch: "some visitor"})
	requireQueryError(t, err, 403)
	_, err = s.Visitors(context.Background(), scope, Filter{})
	requireQueryError(t, err, 403)
}

func TestWorkbenchContentsGroupsByOwnedCollectionAcrossPages(t *testing.T) {
	s, rows, scope, at := reviewFixture()
	scope.View = false
	for i, id := range []string{"ungrouped", "first-old", "first-new", "item-latest", "deleted-group"} {
		catalogSeed(rows, id, id, "one", 1, at.Add(-time.Duration(i+1)*time.Hour))
	}
	seed := func(id, title string, actor int64, status string, created time.Time, ids ...string) ProjectCollection {
		items := make([]ProjectCollectionItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, ProjectCollectionItem{ContentID: id, ContentVersion: 1})
		}
		col := ProjectCollection{ID: id, Title: title, Owner: "one", CreatorActor: actor, Status: status, CreatedAt: created, Items: items}
		rows.Put(projectCollectionRecord(projectCollectionKind, col, created))
		return col
	}
	seed("collection-b", "B", scope.Actor, "published", at.Add(-30*time.Minute), "first-old", "item-latest")
	group := seed("collection-a", "A", scope.Actor, "draft", at.Add(-20*time.Minute), "first-old", "first-new")
	seed("collection-foreign", "Hidden", scope.Actor+1, "published", at.Add(-10*time.Minute), "ungrouped")
	seed("collection-deleted", "Deleted", scope.Actor, "deleted", at.Add(-5*time.Minute), "deleted-group")

	filter := Filter{Sort: "collection", AsOf: at, Limit: 1}
	wantIDs := []string{"first-old", "first-new", "item-latest", "ungrouped", "deleted-group"}
	wantTitles := []string{"A", "A", "B", "", ""}
	var firstCursor string
	for index, id := range wantIDs {
		page, err := s.WorkbenchContents(context.Background(), scope, filter)
		if err != nil {
			t.Fatal(err)
		}
		items := page.Items.([]Content)
		if len(items) != 1 || items[0].ID != id || items[0].CollectionTitle != wantTitles[index] {
			t.Fatalf("page %d = %+v, want id=%s collection=%q", index, items, id, wantTitles[index])
		}
		filter.Cursor = page.Cursor
		if index == 0 {
			firstCursor = page.Cursor
		}
	}
	if filter.Cursor != "" || firstCursor == "" {
		t.Fatalf("invalid cursor after grouping: first=%q last=%q", firstCursor, filter.Cursor)
	}
	group.Title = "C"
	rows.Put(projectCollectionRecord(projectCollectionKind, group, at.Add(time.Minute)))
	filter.Cursor = firstCursor
	_, err := s.WorkbenchContents(context.Background(), scope, filter)
	requireQueryError(t, err, 409)
}
