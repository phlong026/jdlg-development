package tracking

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestContentCreatorIsServerOwnedAndSurvivesRevisionAndPublish(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()

	created, err := s.SaveContent(ctx, scope, "", Draft{
		Title: "初稿", Body: "正文", BodyFormat: "plain", Rights: "own", Owner: "owner-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.CreatorID != scope.Actor {
		t.Fatalf("creator = %d, want %d", created.CreatorID, scope.Actor)
	}

	// The editor request cannot transfer authorship to another account.
	other := scope
	other.Actor = 99
	updated, err := s.SaveContent(ctx, other, created.ID, Draft{
		Title: "修订稿", Body: "新正文", BodyFormat: "plain", Rights: "own", Owner: "owner-a", Revision: created.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreatorID != scope.Actor {
		t.Fatalf("updated creator = %d, want original %d", updated.CreatorID, scope.Actor)
	}

	preview, err := s.Preview(ctx, other, updated.ID, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(ctx, other, updated.ID, updated.Revision, preview["preview_token"].(string), "content-author-publish")
	if err != nil {
		t.Fatal(err)
	}
	publishedContent := published["content"].(Content)
	if publishedContent.CreatorID != scope.Actor {
		t.Fatalf("published creator = %d, want original %d", publishedContent.CreatorID, scope.Actor)
	}
	read, err := s.ReadContent(ctx, updated.ID, publishedContent.Version)
	if err != nil {
		t.Fatal(err)
	}
	if read.CreatorID != scope.Actor {
		t.Fatalf("version creator = %d, want original %d", read.CreatorID, scope.Actor)
	}
	if err = s.readTransaction(ctx, func(r Repository) error {
		for _, target := range []struct{ kind, id string }{{"content", updated.ID}, {"version", contentKey(updated.ID, publishedContent.Version)}} {
			record, loadErr := r.Get(target.kind, target.id)
			if loadErr != nil {
				return loadErr
			}
			payload := string(record.Payload)
			if !strings.Contains(payload, `"creator_id":7`) || strings.Contains(payload, `"author"`) {
				t.Fatalf("%s payload = %s", target.kind, payload)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestContentCreatorIDIsNotPartOfClientJSON(t *testing.T) {
	var draft Draft
	if err := json.Unmarshal([]byte(`{"title":"初稿","creator_id":999,"author":{"display_name":"伪造作者"}}`), &draft); err != nil {
		t.Fatal(err)
	}
	s, scope := fixture(t)
	created, err := s.SaveContent(context.Background(), scope, "", Draft{
		Title: draft.Title, Body: "正文", BodyFormat: "plain", Rights: "own", Owner: "owner-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "creator_id") || strings.Contains(string(encoded), "伪造作者") {
		t.Fatalf("internal/client author data leaked: %s", encoded)
	}
}
