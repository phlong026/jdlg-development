package tracking

import (
	"context"
	"testing"
)

func TestContentCreateIdempotencyReplaysOnlyTheSameNormalizedDraft(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	draft := Draft{Title: "固定版本副本", Body: "正文", BodyFormat: "plain", Rights: "own", Owner: scope.Owners[0], IdempotencyKey: "copy-content-version-1"}
	first, err := s.SaveContent(ctx, scope, "", draft)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SaveContent(ctx, scope, "", draft)
	if err != nil || second.ID != first.ID {
		t.Fatalf("replay = %#v, %v; first = %#v", second, err, first)
	}

	changed := draft
	changed.Body = "另一个版本"
	if _, err = s.SaveContent(ctx, scope, "", changed); err == nil {
		t.Fatal("same key accepted a different normalized draft")
	}

	withoutKey := draft
	withoutKey.IdempotencyKey = ""
	third, err := s.SaveContent(ctx, scope, "", withoutKey)
	if err != nil || third.ID == first.ID {
		t.Fatalf("legacy create without key = %#v, %v", third, err)
	}
}

func TestContentCreateIdempotencyCannotRecoverDeletedOrUnauthorizedResult(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	draft := Draft{Title: "不可恢复副本", Body: "正文", Rights: "own", Owner: scope.Owners[0], IdempotencyKey: "copy-content-version-2"}
	created, err := s.SaveContent(ctx, scope, "", draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeleteContentDraft(ctx, scope, created.ID, created.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveContent(ctx, scope, "", draft); err == nil {
		t.Fatal("deleted result was recovered by replay")
	}

	other := scope
	other.Actor++
	other.Owners = []string{"owner-b"}
	foreign := draft
	foreign.Owner = "owner-b"
	isolated, err := s.SaveContent(ctx, other, "", foreign)
	if err != nil || isolated.ID == created.ID {
		t.Fatalf("same client key was not isolated by actor and owner: %#v, %v", isolated, err)
	}
}

func TestContentCreateIdempotencyDoesNotBypassDocumentArchiveIdentity(t *testing.T) {
	s, scope := fixture(t)
	_, err := s.SaveContent(context.Background(), scope, "", Draft{Title: "文档副本", Body: `{}`, BodyFormat: "document_v1", Rights: "own", Owner: scope.Owners[0], IdempotencyKey: "document-copy-key"})
	if err == nil {
		t.Fatal("keyed document create bypassed document archive identity")
	}
}
