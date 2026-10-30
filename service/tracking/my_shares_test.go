package tracking

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type myShareQueryStore struct {
	reviewStore
	queries []struct {
		kind string
		q    RecordQuery
	}
}

func (s *myShareQueryStore) Transaction(_ context.Context, _ string, fn func(Repository) error) error {
	return fn(s)
}

func (s *myShareQueryStore) ListScoped(kind string, query RecordQuery) ([]Record, error) {
	s.queries = append(s.queries, struct {
		kind string
		q    RecordQuery
	}{kind, query})
	rows, err := s.reviewStore.List(kind)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		match, err := matchesRecordQuery(s.reviewStore, row, query)
		if err != nil {
			return nil, err
		}
		if match {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			if query.Descending {
				return out[i].ID > out[j].ID
			}
			return out[i].ID < out[j].ID
		}
		if query.Descending {
			return out[i].At.After(out[j].At)
		}
		return out[i].At.Before(out[j].At)
	})
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return out, nil
}

func seedMyShare(m reviewStore, id, owner, sharer string, at time.Time) {
	share := Share{ID: id, Route: "page", Owner: owner, Sharer: sharer, Origin: "miniprogram", Expires: at.Add(30 * 24 * time.Hour)}
	row := record("share", "token-"+id, owner, at, share)
	row.Share, row.Visitor = id, sharer
	_ = m.Put(row)
}

func seedMyShareView(m reviewStore, id, owner, visitor, session, share string, at time.Time, visible int64) {
	visitPV(m, id, owner, visitor, session, "", share, at, visible)
}

func myShareItems(t *testing.T, list List) []MyShare {
	t.Helper()
	items, ok := list.Items.([]MyShare)
	if !ok {
		t.Fatalf("items type = %T, want []MyShare", list.Items)
	}
	return items
}

func myShareVisitItems(t *testing.T, list List) []MyShareVisit {
	t.Helper()
	items, ok := list.Items.([]MyShareVisit)
	if !ok {
		t.Fatalf("items type = %T, want []MyShareVisit", list.Items)
	}
	return items
}

func TestMySharesDoNotTreatOwnerAsSharer(t *testing.T) {
	scope := Scope{Tenant: "tenant", Actor: 11, Owners: []string{"company"}, View: true}
	other := Share{Owner: "company", Sharer: "u:22"}
	if shareVisibleToActor(scope, 11, other) {
		t.Fatal("同企业不是本人分享授权")
	}
	own := Share{Owner: "company", Sharer: "u:11"}
	if !shareVisibleToActor(scope, 11, own) {
		t.Fatal("本人授权分享应可见")
	}
}

func TestMySharesIsolateSameOwnerAndCrossOwner(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	seedMyShare(rows, "share-b", "company", "u:11", now.Add(-3*time.Hour))
	seedMyShare(rows, "share-d", "company", "u:22", now.Add(-2*time.Hour))
	seedMyShare(rows, "share-other-owner", "other", "u:11", now.Add(-time.Hour))
	seedMyShareView(rows, "view-b", "company", "u:77", "session-b", "share-b", now.Add(-90*time.Minute), 12000)
	seedMyShareView(rows, "view-d", "company", "u:77", "session-d", "share-d", now.Add(-80*time.Minute), 9000)

	got, err := s.MyShares(context.Background(), scope, 11, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareItems(t, got)
	if len(items) != 1 || items[0].ID != "share-b" || items[0].Visitors != 1 || items[0].Views != 1 {
		t.Fatalf("my shares = %+v, want only B's share and its traffic", items)
	}
	if got.Metric != MetricVersion || got.AsOf.IsZero() {
		t.Fatalf("snapshot = %+v", got.Snapshot)
	}
}

func TestMySharesCumulativeTrafficDeduplicatesLinkedVisitor(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	seedMyShare(rows, "share-b", "company", "u:11", now.Add(-3*time.Hour))
	seedMyShareView(rows, "view-a", "company", "a:one", "session-a", "share-b", now.Add(-90*time.Minute), 1000)
	seedMyShareView(rows, "view-b", "company", "a:two", "session-b", "share-b", now.Add(-80*time.Minute), 2000)
	_ = rows.Put(record("identity", "session-a", "company", now.Add(-70*time.Minute), map[string]string{"original_visitor_key": "a:one", "visitor_key": "u:77"}))
	_ = rows.Put(record("identity", "session-b", "company", now.Add(-60*time.Minute), map[string]string{"original_visitor_key": "a:two", "visitor_key": "u:77"}))

	got, err := s.MyShares(context.Background(), scope, 11, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareItems(t, got)
	if len(items) != 1 || items[0].Visitors != 1 || items[0].Views != 2 {
		t.Fatalf("cumulative traffic = %+v, want one linked visitor and two views", items)
	}
}

func TestMyShareVisitsOverlapDoesNotRevealColleagueHistory(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	seedMyShare(rows, "share-b", "company", "u:11", now.Add(-3*time.Hour))
	seedMyShare(rows, "share-d", "company", "u:22", now.Add(-2*time.Hour))
	seedMyShareView(rows, "view-b", "company", "u:77", "session-b", "share-b", now.Add(-90*time.Minute), 12000)
	seedMyShareView(rows, "view-d", "company", "u:77", "session-d", "share-d", now.Add(-80*time.Minute), 9000)
	seedMyShareView(rows, "view-b-2", "company", "a:guest", "session-b-2", "share-b", now.Add(-70*time.Minute), 3000)

	got, err := s.MyShareVisits(context.Background(), scope, 11, "share-b", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareVisitItems(t, got)
	if len(items) != 2 || got.Identified != 1 || got.Anonymous != 1 {
		t.Fatalf("visits = %+v, counts = %d/%d", items, got.Identified, got.Anonymous)
	}
	for _, item := range items {
		if item.Visitor.Key == "u:77" && (item.Views != 1 || item.Visible != 12000) {
			t.Fatalf("overlapping visitor leaked colleague metrics: %+v", item)
		}
	}

	if _, err = s.MyShareVisits(context.Background(), scope, 11, "share-d", Filter{}); err == nil {
		t.Fatal("actor B opened actor D's share")
	} else {
		var denied *Error
		if !errors.As(err, &denied) || denied.Status != 404 {
			t.Fatalf("foreign share error = %v, want opaque 404", err)
		}
	}
}

func TestMyShareVisitsAuthorizeOldShareForCurrentWindow(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	seedMyShare(rows, "old-share", "company", "u:11", now.Add(-200*24*time.Hour))
	seedMyShareView(rows, "recent-view", "company", "u:77", "recent-session", "old-share", now.Add(-time.Hour), 5000)

	got, err := s.MyShareVisits(context.Background(), scope, 11, "old-share", Filter{Start: now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareVisitItems(t, got)
	if len(items) != 1 || items[0].Views != 1 || items[0].Visible != 5000 {
		t.Fatalf("recent visits for old share = %+v", items)
	}
}

func TestMySharesIncludeOldAndBlankScalarShares(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	share := Share{ID: "legacy-share", Route: "page", Owner: "company", Sharer: "u:11", Origin: "miniprogram", Expires: now.Add(30 * 24 * time.Hour)}
	// This fixture predates visitor_key/share_id denormalization and is older
	// than the visit filter window. Payload remains authoritative during backfill.
	_ = rows.Put(record("share", "legacy-token", "company", now.Add(-400*24*time.Hour), share))
	seedMyShareView(rows, "legacy-view", "company", "u:77", "legacy-session", "legacy-share", now.Add(-time.Hour), 4000)

	got, err := s.MyShares(context.Background(), scope, 11, Filter{Start: now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareItems(t, got)
	if len(items) != 1 || items[0].ID != "legacy-share" || items[0].Views != 1 {
		t.Fatalf("legacy share compatibility = %+v", items)
	}
	detail, err := s.MyShareVisits(context.Background(), scope, 11, "legacy-share", Filter{Start: now.Add(-24 * time.Hour)})
	if err != nil || len(myShareVisitItems(t, detail)) != 1 {
		t.Fatalf("legacy share detail = %+v err=%v", detail, err)
	}
}

func TestMySharesRejectActorMismatchRevocationAndTamperedCursor(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	seedMyShare(rows, "share-1", "company", "u:11", now.Add(-2*time.Hour))
	seedMyShare(rows, "share-2", "company", "u:11", now.Add(-time.Hour))

	if _, err := s.MyShares(context.Background(), scope, 22, Filter{}); err == nil {
		t.Fatal("mismatched authenticated actor accepted")
	}
	page, err := s.MyShares(context.Background(), scope, 11, Filter{Limit: 1})
	if err != nil || page.Cursor == "" {
		t.Fatalf("first page = %+v, err=%v", page, err)
	}
	if _, err = s.MyShares(context.Background(), scope, 11, Filter{Limit: 1, Cursor: page.Cursor + "x"}); err == nil {
		t.Fatal("tampered cursor accepted")
	}
	scope.Owners = []string{"revoked"}
	if _, err = s.MyShareVisits(context.Background(), scope, 11, "share-1", Filter{}); err == nil {
		t.Fatal("revoked owner permission still exposed visits")
	}
}

func TestMySharesUseServerSharerAndBatchTrafficQuery(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	store := &myShareQueryStore{reviewStore: rows}
	s.Store = store
	scope.Actor = 11
	scope.Owners = []string{"company"}
	seedMyShare(rows, "share-1", "company", "u:11", now.Add(-2*time.Hour))
	seedMyShare(rows, "share-2", "company", "u:11", now.Add(-time.Hour))
	seedMyShareView(rows, "view-1", "company", "u:77", "session-1", "share-1", now.Add(-50*time.Minute), 1000)
	seedMyShareView(rows, "view-2", "company", "u:88", "session-2", "share-2", now.Add(-40*time.Minute), 1000)

	if _, err := s.MyShares(context.Background(), scope, 11, Filter{}); err != nil {
		t.Fatal(err)
	}
	shareQueries, eventQueries := 0, 0
	for _, query := range store.queries {
		switch query.kind {
		case "share":
			shareQueries++
			if !reflect.DeepEqual(query.q.VisitorKeys, []string{"u:11"}) || !reflect.DeepEqual(query.q.Owners, []string{"company"}) {
				t.Fatalf("share query not server scoped: %+v", query.q)
			}
		case "event":
			eventQueries++
			if len(query.q.Shares) != 2 {
				t.Fatalf("traffic query shares = %v, want one batch", query.q.Shares)
			}
		}
	}
	if shareQueries != 1 || eventQueries != 1 {
		t.Fatalf("queries share/event = %d/%d, want 1/1: %+v", shareQueries, eventQueries, store.queries)
	}
}

func TestMySharesRespectReadBudget(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	store := &myShareQueryStore{reviewStore: rows}
	s.Store = store
	scope.Actor = 11
	scope.Owners = []string{"company"}
	for i := 0; i <= maxQueryRecords; i++ {
		seedMyShare(rows, fmt.Sprintf("share-%08d", i), "company", "u:11", now.Add(-time.Duration(i)*time.Second))
	}
	first, err := s.MyShares(context.Background(), scope, 11, Filter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareItems(t, first)
	if len(items) != 2 || items[0].ID != "share-00000000" || first.Cursor == "" {
		t.Fatalf("first keyset page = %+v cursor=%q", items, first.Cursor)
	}
	second, err := s.MyShares(context.Background(), scope, 11, Filter{Limit: 2, AsOf: first.AsOf, Cursor: first.Cursor})
	if err != nil {
		t.Fatal(err)
	}
	secondItems := myShareItems(t, second)
	if len(secondItems) != 2 || secondItems[0].ID != "share-00000002" {
		t.Fatalf("second keyset page = %+v", secondItems)
	}
}

func TestMySharesSQLUsesExactSharerDescendingKeyset(t *testing.T) {
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	query, args := buildScopedQuery("tenant", "share", RecordQuery{Owners: []string{"company"}, VisitorKeys: []string{"u:11"}, ExactVisitors: true, Descending: true, BeforeAt: at, BeforeID: "token-share-2", AtThrough: at, Limit: 3}, maxQueryRecords+1)
	for _, want := range []string{"r.visitor_key IN (?)", "r.visitor_key=''", "$.sharer_id", "r.occurred_at<?", "ORDER BY r.occurred_at DESC,BINARY r.record_id DESC LIMIT ?"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
	if strings.Contains(query, "identity_link") {
		t.Fatalf("exact sharer query followed identity aliases: %s", query)
	}
	if got := args[len(args)-1]; got != 3 {
		t.Fatalf("limit arg = %v, want 3", got)
	}
}

func TestMyShareVisitsExcludeResolvedSelfAndTombstones(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	seedMyShare(rows, "share-b", "company", "u:11", now.Add(-200*24*time.Hour))
	seedMyShareView(rows, "self-view", "company", "a:self", "self-session", "share-b", now.Add(-4*time.Hour), 1000)
	_ = rows.Put(record("identity", "self-session", "company", now.Add(-3*time.Hour), map[string]string{"original_visitor_key": "a:self", "visitor_key": "u:11"}))
	seedMyShareView(rows, "deleted-visitor", "company", "u:77", "deleted-visitor-session", "share-b", now.Add(-3*time.Hour), 1000)
	_ = rows.Put(record("deletion", "u:77", "company", now.Add(-2*time.Hour), map[string]any{"deleted_at": now.Add(-2 * time.Hour)}))
	seedMyShareView(rows, "deleted-session", "company", "u:88", "deleted-session-id", "share-b", now.Add(-2*time.Hour), 1000)
	_ = rows.Put(record("deletion_session", "deleted-session-id", "company", now.Add(-time.Hour), map[string]bool{"deleted": true}))
	seedMyShareView(rows, "kept-view", "company", "u:99", "kept-session", "share-b", now.Add(-30*time.Minute), 2000)

	got, err := s.MyShareVisits(context.Background(), scope, 11, "share-b", Filter{Start: now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareVisitItems(t, got)
	if len(items) != 1 || items[0].Visitor.Key != "u:99" {
		t.Fatalf("tombstone/self filtering = %+v", items)
	}
}

func TestMySharesBatchMaterialMetadata(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	store := &myShareQueryStore{reviewStore: rows}
	s.Store = store
	scope.Actor = 11
	scope.Owners = []string{"company"}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("content-%02d", i)
		content := Content{ID: id, Version: 1, Title: "标题" + id, Type: "article", Status: "published", Owner: "company"}
		_ = rows.Put(record("content", id, "company", now.Add(-48*time.Hour), content))
		_ = rows.Put(record("version", contentKey(id, 1), "company", now.Add(-47*time.Hour), content))
		share := Share{ID: "share-" + id, Route: publishedReaderRoute, Content: id, Version: 1, Owner: "company", Sharer: "u:11", Origin: "miniprogram", Expires: now.Add(30 * 24 * time.Hour)}
		row := record("share", "token-"+id, "company", now.Add(-time.Duration(i)*time.Minute), share)
		row.Share, row.Visitor = share.ID, share.Sharer
		_ = rows.Put(row)
	}
	got, err := s.MyShares(context.Background(), scope, 11, Filter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(myShareItems(t, got)) != 12 {
		t.Fatal("missing shares")
	}
	counts := map[string]int{}
	for _, query := range store.queries {
		counts[query.kind]++
	}
	if counts["content"] != 1 || counts["version"] != 1 || counts["content_revoked"] != 1 {
		t.Fatalf("metadata queries = %+v, want one batch per kind", counts)
	}
}

func TestMySharesBatchLegacyMaterialMetadata(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	route := "pages/inspirationdetail/inspirationdetail"
	metadata := LegacyMetadata{Title: "当前公开标题", Type: "inspiration", VersionSeed: "current-public-source"}
	version := SourceSnapshotVersion(metadata)
	batchCalls := 0
	s.ResolveLegacyBatchContext = func(ctx context.Context, gotRoute string, contents []string) (map[string]LegacyMetadata, error) {
		batchCalls++
		if gotRoute != route || len(contents) != 12 {
			t.Fatalf("batch route=%q contents=%v", gotRoute, contents)
		}
		out := make(map[string]LegacyMetadata, len(contents))
		for _, content := range contents {
			out[content] = metadata
		}
		return out, nil
	}
	s.ResolveLegacyContext = func(context.Context, string, int64) (LegacyMetadata, error) {
		t.Fatal("legacy list performed a per-item resolver call")
		return LegacyMetadata{}, ErrUnavailable
	}
	for i := 1; i <= 12; i++ {
		contentID := LegacyID(route, int64(i))
		content := Content{ID: contentID, Version: version, Title: fmt.Sprintf("历史标题%02d", i), Type: "inspiration", Status: "published", ReadOnly: true, Route: route}
		_ = rows.Put(record("version", contentKey(contentID, version), "", now.Add(-47*time.Hour), content))
		share := Share{ID: fmt.Sprintf("legacy-share-%02d", i), Route: route, Content: contentID, Version: version, Owner: "company", Sharer: "u:11", Origin: "miniprogram", Expires: now.Add(30 * 24 * time.Hour)}
		row := record("share", "legacy-token-"+contentID, "company", now.Add(-time.Duration(i)*time.Minute), share)
		row.Share, row.Visitor = share.ID, share.Sharer
		_ = rows.Put(row)
	}

	got, err := s.MyShares(context.Background(), scope, 11, Filter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareItems(t, got)
	if len(items) != 12 || batchCalls != 1 {
		t.Fatalf("legacy shares=%d batch calls=%d", len(items), batchCalls)
	}
	for _, item := range items {
		if item.Status != "published" || !strings.HasPrefix(item.Title, "历史标题") {
			t.Fatalf("legacy material = %+v", item)
		}
	}
}

func TestMySharesRejectMalformedShareBeforePagination(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	valid := Share{ID: "valid-older", Route: "page", Owner: "company", Sharer: "u:11", Origin: "miniprogram", Expires: now.Add(30 * 24 * time.Hour)}
	validRow := record("share", "valid-token", "company", now.Add(-time.Hour), valid)
	validRow.Share, validRow.Visitor = valid.ID, valid.Sharer
	_ = rows.Put(validRow)
	malformed := Share{ID: "payload-id", Route: "page", Owner: "company", Sharer: "u:11", Origin: "miniprogram", Expires: now.Add(30 * 24 * time.Hour)}
	malformedRow := record("share", "malformed-token", "company", now, malformed)
	malformedRow.Share, malformedRow.Visitor = "different-scalar-id", malformed.Sharer
	_ = rows.Put(malformedRow)

	_, err := s.MyShares(context.Background(), scope, 11, Filter{Limit: 1})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("malformed newest share error = %v, want ErrUnavailable", err)
	}
}

func TestMySharesMarksMissingMaterialVersionExpired(t *testing.T) {
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	content := Content{ID: "missing-version", Version: 2, Title: "当前素材", Type: "article", Status: "published", Owner: "company"}
	_ = rows.Put(record("content", content.ID, "company", now.Add(-48*time.Hour), content))
	share := Share{ID: "share-missing-version", Route: publishedReaderRoute, Content: content.ID, Version: 1, Owner: "company", Sharer: "u:11", Origin: "miniprogram", Expires: now.Add(30 * 24 * time.Hour)}
	row := record("share", "missing-version-token", "company", now.Add(-time.Hour), share)
	row.Share, row.Visitor = share.ID, share.Sharer
	_ = rows.Put(row)

	got, err := s.MyShares(context.Background(), scope, 11, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareItems(t, got)
	if len(items) != 1 || items[0].Status != "expired" || items[0].Title != "素材版本已失效" {
		t.Fatalf("missing version material = %+v", items)
	}
}
