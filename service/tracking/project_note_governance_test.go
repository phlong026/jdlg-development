package tracking

import (
	"context"
	"errors"
	"testing"
)

func TestProjectNoteOpensFollowReviewedRawRetentionWithoutPurgingVersions(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	old := s.Now().AddDate(0, 0, -91)
	if err := s.transaction(ctx, func(r Repository) error {
		for _, row := range []Record{
			record("note_open", "old-note-open", "owner-a", old, map[string]string{"private": "raw view"}),
			record("note_version", "keep-note-version", "owner-a", old, map[string]string{"private": "business version"}),
		} {
			if err := r.Put(row); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	policy := DefaultRetentionPolicy()
	plan, err := s.PlanRetention(ctx, scope, policy, s.Now().AddDate(0, 0, -90), s.Now().AddDate(0, 0, -180))
	if err != nil || plan.Counts["note_open"] != 1 {
		t.Fatal("private opens missing from raw retention", plan, err)
	}
	policy.Enabled = true
	if _, err = s.PurgeExpired(ctx, scope, policy, plan); err != nil {
		t.Fatal(err)
	}
	if err = s.readTransaction(ctx, func(r Repository) error {
		if _, err := r.Get("note_open", "old-note-open"); !errors.Is(err, ErrNotFound) {
			t.Fatal("old open retained", err)
		}
		if _, err := r.Get("note_version", "keep-note-version"); err != nil {
			t.Fatal("business version purged", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectNoteOpenVisitorDeletionPreservesOtherMembersAndBusinessData(t *testing.T) {
	for _, staged := range []bool{false, true} {
		name := "atomic"
		if staged {
			name = "staged"
		}
		t.Run(name, func(t *testing.T) {
			s, indexed, scope, now := privacyFixture(t)
			s.forceStagedDeletion = staged
			for _, pair := range [][2]string{{"first", "u:1"}, {"second", "u:2"}} {
				for _, kind := range []string{"note_open", "notice_event"} {
					row := record(kind, pair[0], "one", now, map[string]string{"project_id": "same-project"})
					row.Visitor = pair[1]
					// Project IDs must never be treated as public tracking sessions.
					if err := indexed.rows.Put(row); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := indexed.rows.Put(record("note_version", "same-project", "one", now, map[string]string{"body": "private version"})); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteVisitor(context.Background(), scope, "u:1", "privacy request"); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"note_open", "notice_event"} {
				if _, err := indexed.rows.Get(kind, "first"); !errors.Is(err, ErrNotFound) {
					t.Fatal("deleted visitor retained", kind, err)
				}
				if _, err := indexed.rows.Get(kind, "second"); err != nil {
					t.Fatal("other member removed", kind, err)
				}
			}
			if _, err := indexed.rows.Get("note_version", "same-project"); err != nil {
				t.Fatal("business version removed", err)
			}
			if _, err := indexed.rows.Get("deletion_session", "same-project"); !errors.Is(err, ErrNotFound) {
				t.Fatal("project became a session tombstone", err)
			}
		})
	}
}
