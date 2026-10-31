package tracking

import (
	"context"
	"strings"
	"testing"
)

func TestMediaCatalogUsesOwnerScopedKeysetPaginationAndServerFilters(t *testing.T) {
	s, scope := fixture(t)
	for _, asset := range []Asset{
		{ID: strings.Repeat("a", 32), Kind: "image", MIME: "image/png", Width: 2, Height: 2, Title: "庭院效果图", URL: "/api/v1/miniprogram/tracking/media/" + strings.Repeat("a", 32)},
		{ID: strings.Repeat("b", 32), Kind: "video", MIME: "video/mp4", Width: 2, Height: 2, Title: "庭院漫游", URL: "/api/v1/miniprogram/tracking/media/" + strings.Repeat("b", 32)},
		{ID: strings.Repeat("c", 32), Kind: "image", MIME: "image/png", Width: 2, Height: 2, Title: "现代立面", URL: "/api/v1/miniprogram/tracking/media/" + strings.Repeat("c", 32)},
	} {
		if err := s.AddMedia(context.Background(), scope, asset, scope.Owners[0]); err != nil {
			t.Fatal(err)
		}
	}
	foreign := scope
	foreign.Owners = []string{"owner-b"}
	if err := s.AddMedia(context.Background(), foreign, Asset{ID: strings.Repeat("d", 32), Kind: "image", MIME: "image/png", Width: 2, Height: 2, Title: "庭院越权图", URL: "/api/v1/miniprogram/tracking/media/" + strings.Repeat("d", 32)}, "owner-b"); err != nil {
		t.Fatal(err)
	}

	first, err := s.ListMediaPage(context.Background(), scope, "", 1, "庭院", []string{"image", "video"})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := s.ListMediaPage(context.Background(), scope, first.NextCursor, 1, "庭院", []string{"image", "video"})
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	for _, item := range append(first.Items, second.Items...) {
		if item.Title == "庭院越权图" || !strings.Contains(item.Title, "庭院") {
			t.Fatalf("unscoped or unfiltered item: %+v", item)
		}
	}
	images, err := s.ListMediaPage(context.Background(), scope, "", 20, "庭院", []string{"image"})
	if err != nil || len(images.Items) != 1 || images.Items[0].Kind != "image" {
		t.Fatalf("images=%+v err=%v", images, err)
	}
}

func TestMediaCatalogRejectsInvalidFilterBeforeReading(t *testing.T) {
	s, scope := fixture(t)
	if _, err := s.ListMediaPage(context.Background(), scope, "", 20, strings.Repeat("x", 101), nil); err == nil {
		t.Fatal("accepted oversized media search")
	}
	if _, err := s.ListMediaPage(context.Background(), scope, "", 20, "", []string{"private-note"}); err == nil {
		t.Fatal("accepted unsupported media kind")
	}
}

func TestMediaCatalogFiltersArePushedIntoBoundedSQL(t *testing.T) {
	query, args := buildScopedQuery("tenant-a", "media", RecordQuery{
		Owners: []string{"owner-a"}, TitleSearch: "庭院", AssetKinds: []string{"image", "video"}, Limit: 21, Descending: true,
	}, 100)
	for _, clause := range []string{"r.owner_id IN", "LOCATE(LOWER(?)", "JSON_EXTRACT(r.payload,'$.kind')", "ORDER BY r.occurred_at DESC", "LIMIT ?"} {
		if !strings.Contains(query, clause) {
			t.Fatalf("missing SQL predicate %q in %s", clause, query)
		}
	}
	if got := args[len(args)-1]; got != 21 {
		t.Fatalf("unbounded SQL limit args=%v", args)
	}
}
