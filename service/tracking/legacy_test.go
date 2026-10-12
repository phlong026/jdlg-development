package tracking

import (
	"context"
	"testing"
)

func TestLegacyAuthoritativeMetadataAndVersions(t *testing.T) {
	s, scope := fixture(t)
	route := "pages/inspirationdetail/inspirationdetail"
	s.Config.Routes[route] = Route{Title: "灵感", Public: true}
	sources := map[int64]LegacyMetadata{1: {Title: "竖向庭院", Type: "inspiration", CoverURL: "https://public.example/one.jpg", Width: 900, Height: 1600, VersionSeed: "body-a"}, 2: {Title: "横向客厅", Type: "inspiration", CoverURL: "https://public.example/two.jpg", Width: 1600, Height: 900, VersionSeed: "body-b"}}
	s.ResolveLegacy = func(_ string, id int64) (LegacyMetadata, error) {
		v, ok := sources[id]
		if !ok {
			return v, fail(404, "private or missing")
		}
		return v, nil
	}
	input := func(id int64) EntryInput {
		return EntryInput{Route: route, Content: LegacyID(route, id), Client: "client-legacy", Consent: "v1"}
	}
	for _, in := range []EntryInput{input(404), input(3), {Route: route, Content: LegacyID("pages/utilitytooldetail/utilitytooldetail", 1), Client: "client-legacy", Consent: "v1"}} {
		if _, e := s.Enter(context.Background(), in, 0); e == nil {
			t.Fatal("missing/private/wrong namespace accepted")
		}
	}
	first, e := s.Enter(context.Background(), input(1), 0)
	if e != nil {
		t.Fatal(e)
	}
	if first.Version == 0 {
		t.Fatal("missing authoritative version")
	}
	if _, e = s.Enter(context.Background(), input(2), 0); e != nil {
		t.Fatal(e)
	}
	shares, e := s.CreateShare(context.Background(), ShareInput{Route: route, Content: first.Content, Version: first.Version, Key: "share-legacy-one"}, first.Token, 0)
	if e != nil {
		t.Fatal(e)
	}
	list, e := s.Contents(context.Background(), scope, Filter{}, "")
	if e != nil {
		t.Fatal(e)
	}
	cards := list.Items.([]Content)
	if len(cards) != 2 {
		t.Fatalf("cards %d", len(cards))
	}
	for _, c := range cards {
		if !c.ReadOnly || c.Cover == nil || c.Cover.Width == c.Cover.Height || c.Title == "" {
			t.Fatalf("bad card %+v", c)
		}
	}
	other := scope
	other.Owners = []string{"owner-b"}
	list, e = s.Contents(context.Background(), other, Filter{}, "")
	if e != nil || len(list.Items.([]Content)) != 0 {
		t.Fatal("cross owner metadata leak", e)
	}
	v := sources[1]
	v.Title = "新版庭院"
	v.VersionSeed = "body-c"
	sources[1] = v
	in := input(1)
	in.Version = first.Version
	in.ShareToken = shares["share_token"].(string)
	next, e := s.Enter(context.Background(), in, 0)
	if e != nil {
		t.Fatal(e)
	}
	if next.Version == first.Version || next.Share != "" {
		t.Fatal("stale share attributed or old version reused")
	}
	if _, e = s.CreateShare(context.Background(), ShareInput{Route: route, Content: first.Content, Version: first.Version, Key: "share-legacy-two"}, first.Token, 0); e == nil {
		t.Fatal("stale share issued")
	}
	if _, e = s.ResolveShare(context.Background(), in.ShareToken); e == nil {
		t.Fatal("stale share resolved as pinned source")
	}
	if e = s.transaction(context.Background(), func(r Repository) error {
		old, e := decode[Content](r, "version", contentKey(first.Content, first.Version))
		if e != nil {
			return e
		}
		if old.Title != "竖向庭院" {
			t.Fatal("old snapshot overwritten")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	delete(sources, 1)
	if _, e = s.Enter(context.Background(), input(1), 0); e == nil {
		t.Fatal("unpublished item accepted")
	}
}
