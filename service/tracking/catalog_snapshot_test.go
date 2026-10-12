package tracking

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestCatalogSnapshotMembershipAndStablePagination(t *testing.T) {
	for _, kind := range []string{"content", "legacy_catalog"} {
		for _, indexed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/indexed-%v", kind, indexed), func(t *testing.T) {
				s, rows, scope, at := reviewFixture()
				scope.View = false
				key := func(id string) string {
					if kind == "legacy_catalog" {
						return hash(scope.Owners[0] + ":" + id)
					}
					return id
				}
				seed := func(id string, when time.Time) {
					c := Content{ID: id, Owner: scope.Owners[0], Version: 1, Revision: 1, Title: id, Status: "draft", Rights: "own", Body: "original"}
					if kind == "legacy_catalog" {
						c.ReadOnly = true
						c.Status = "published"
					}
					rows.Put(record(kind, key(id), scope.Owners[0], when, c))
				}
				seed("content-b", at.Add(-time.Hour))
				seed("content-c", at.Add(-time.Hour))
				if indexed {
					s.Store = &indexedTestStore{rows: rows, t: t}
				}
				f := Filter{AsOf: at, Limit: 1}
				first, err := s.Contents(context.Background(), scope, f, "")
				if err != nil {
					t.Fatal(err)
				}
				if len(first.Items.([]Content)) != 1 || first.Items.([]Content)[0].ID != "content-b" || first.Cursor == "" {
					t.Fatal(first)
				}
				s.Now = func() time.Time { return at.Add(time.Minute) }
				// Both old catalog rows are edited after as_of; this must not remove their
				// pre-existing membership. New lower-sorting IDs must not shift page two.
				for _, id := range []string{"content-b", "content-c"} {
					if kind == "content" {
						_, err = s.SaveContent(context.Background(), scope, id, Draft{Title: "edited", Body: "edited", Rights: "own", Owner: scope.Owners[0], Revision: 1})
					} else {
						err = s.transaction(context.Background(), func(r Repository) error {
							return saveLegacy(r, Content{ID: id, Version: 2, Title: "edited", Status: "published", ReadOnly: true}, scope.Owners[0], s.Now())
						})
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				seed("content-a", s.Now())
				f.Cursor = first.Cursor
				second, err := s.Contents(context.Background(), scope, f, "")
				if err != nil {
					t.Fatal(err)
				}
				got := second.Items.([]Content)
				if len(got) != 1 || got[0].ID != "content-c" || second.Cursor != "" {
					t.Fatalf("snapshot repeated, dropped, or gained catalog rows: first=%+v second=%+v", first, second)
				}
				if second.Snapshot.AsOf != at {
					t.Fatal("metric as_of changed")
				}
			})
		}
	}
}

func TestCatalogCreateAndUpdatePreserveMembershipTimestamp(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	created := s.Now()
	c, err := s.SaveContent(ctx, scope, "", Draft{Title: "created", Body: "body", Rights: "own", Owner: scope.Owners[0]})
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return created.Add(time.Hour) }
	c, err = s.SaveContent(ctx, scope, c.ID, Draft{Title: "edited", Body: "body", Rights: "own", Owner: scope.Owners[0], Revision: c.Revision})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]json.RawMessage
	json.Unmarshal(marshal(c), &value)
	var stamp time.Time
	if err = json.Unmarshal(value["created_at"], &stamp); err != nil || !stamp.Equal(created) {
		t.Fatalf("creation time changed/missing: %s %v", marshal(c), err)
	}
	old, err := s.Contents(ctx, scope, Filter{AsOf: created}, c.ID)
	if err != nil || len(old.Items.([]Content)) != 1 {
		t.Fatal(old, err)
	}
	before, err := s.Contents(ctx, scope, Filter{AsOf: created.Add(-time.Second)}, c.ID)
	if err != nil || len(before.Items.([]Content)) != 0 {
		t.Fatal("post-snapshot material entered old snapshot", before, err)
	}
}

func TestCatalogLifecycleRetainsPreUpgradeMembership(t *testing.T) {
	for _, action := range []string{"publish", "revoke"} {
		t.Run(action, func(t *testing.T) {
			s, scope := fixture(t)
			ctx := context.Background()
			at := s.Now()
			// Simulate an existing row from before created_at was introduced.
			c := Content{ID: "pre-upgrade-content", Owner: scope.Owners[0], Title: "old", Body: "old", Rights: "own", Status: "draft", Revision: 1}
			if action == "revoke" {
				c.Status = "published"
				c.Version = 1
			}
			if err := s.transaction(ctx, func(r Repository) error { return r.Put(record("content", c.ID, c.Owner, at, c)) }); err != nil {
				t.Fatal(err)
			}
			s.Now = func() time.Time { return at.Add(time.Minute) }
			if action == "publish" {
				p, err := s.Preview(ctx, scope, c.ID, 1)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.Publish(ctx, scope, c.ID, 1, p["preview_token"].(string), "publish-old-content"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.Revoke(ctx, scope, c.ID, 1, "revoke reason", "revoke-old-content"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.Contents(ctx, scope, Filter{AsOf: at}, c.ID)
			if err != nil || len(got.Items.([]Content)) != 1 {
				t.Fatal("lifecycle changed old catalog membership", got, err)
			}
			if created := got.Items.([]Content)[0].CreatedAt; created == nil || !created.Equal(at) {
				t.Fatal("creation timestamp lost", created)
			}
		})
	}
}
func TestCatalogKeysetKeepsMetricOrderAndRejectsChangedFilter(t *testing.T) {
	for _, sortKey := range []string{"page_views", "visible_ms", "last_visit"} {
		for _, direction := range []string{"asc", "desc"} {
			at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
			a, b := at, at.Add(time.Hour)
			low := Content{ID: "content-low", PageViews: 1, VisibleMS: 1, LastVisit: &a}
			high := Content{ID: "content-high", PageViews: 2, VisibleMS: 2, LastVisit: &b}
			items := []Content{high, low}
			if direction == "asc" {
				items = []Content{low, high}
			}
			f := Filter{AsOf: at, Sort: sortKey, Direction: direction, Limit: 1}
			page, cursor, err := paginateContents(f, items)
			if err != nil || len(page) != 1 || cursor == "" {
				t.Fatal(page, cursor, err)
			}
			f.Cursor = cursor
			page, next, err := paginateContents(f, items)
			if err != nil || len(page) != 1 || page[0].ID != items[1].ID || next != "" {
				t.Fatal(page, next, err)
			}
			f.AsOf = at.Add(time.Second)
			_, _, err = paginateContents(f, items)
			requireQueryError(t, err, 400)
		}
	}
}

func TestCatalogCreationCutoffRetainsSubmicrosecondPrecision(t *testing.T) {
	s, scope := fixture(t)
	at := s.Now().Add(123456100 * time.Nanosecond)
	s.Now = func() time.Time { return at.Add(time.Nanosecond) }
	c, err := s.SaveContent(context.Background(), scope, "", Draft{Title: "new", Body: "body", Rights: "own", Owner: scope.Owners[0]})
	if err != nil {
		t.Fatal(err)
	}
	if c.CreatedAt == nil || !c.CreatedAt.Equal(s.Now()) {
		t.Fatal("creation precision lost", c.CreatedAt)
	}
	got, err := s.Contents(context.Background(), scope, Filter{AsOf: at}, "")
	if err != nil || len(got.Items.([]Content)) != 0 {
		t.Fatal("post-snapshot nanosecond material leaked", got, err)
	}
}
