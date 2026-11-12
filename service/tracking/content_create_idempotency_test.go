package tracking

import (
	"context"
	"strings"
	"sync"
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
	asset := Asset{ID: "document-asset-copy-1", Kind: "document", URL: "/api/v1/miniprogram/tracking/media/document-asset-copy-1", MIME: "application/pdf"}
	if err := s.AddMedia(context.Background(), scope, asset, scope.Owners[0]); err != nil {
		t.Fatal(err)
	}
	_, err := s.SaveContent(context.Background(), scope, "", Draft{Title: "文档副本", Body: `{"schema":1,"media_id":"document-asset-copy-1"}`, BodyFormat: "document_v1", Rights: "own", Owner: scope.Owners[0], MediaIDs: []string{asset.ID}, IdempotencyKey: "document-copy-key"})
	if err == nil || !strings.Contains(err.Error(), "不接受创建幂等标识") {
		t.Fatalf("keyed valid document did not reach the archive guard: %v", err)
	}
}

func TestContentCreateIdempotencySerializesConcurrentReplays(t *testing.T) {
	s, scope := fixture(t)
	draft := Draft{Title: "并发副本", Body: "固定正文", BodyFormat: "plain", Rights: "own", Owner: scope.Owners[0], IdempotencyKey: "copy-content-concurrent-1"}
	const requests = 12
	start := make(chan struct{})
	results := make(chan Content, requests)
	errors := make(chan error, requests)
	var wait sync.WaitGroup
	for range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			created, err := s.SaveContent(context.Background(), scope, "", draft)
			results <- created
			errors <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent replay failed: %v", err)
		}
	}
	var contentID string
	for created := range results {
		if contentID == "" {
			contentID = created.ID
		}
		if created.ID == "" || created.ID != contentID {
			t.Fatalf("concurrent replay returned different content: first=%q got=%q", contentID, created.ID)
		}
	}
	listed, err := s.Contents(context.Background(), scope, Filter{}, "")
	if err != nil || len(listed.Items.([]Content)) != 1 || listed.Items.([]Content)[0].ID != contentID {
		t.Fatalf("concurrent replay created duplicates: %#v, %v", listed, err)
	}
}
