package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func publishNativeReferenceContent(t *testing.T, s *Service, scope Scope, title, key string) Content {
	t.Helper()
	draft, err := s.SaveContent(context.Background(), scope, "", Draft{Title: title, Body: "只能留在已发布快照中的正文", BodyFormat: "plain", Owner: scope.Owners[0], Rights: "own"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(context.Background(), scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(context.Background(), scope, draft.ID, draft.Revision, preview["preview_token"].(string), key)
	if err != nil {
		t.Fatal(err)
	}
	return published["content"].(Content)
}

func TestNativeReferenceCreationIsServerDerivedAndIdempotent(t *testing.T) {
	s, scope := fixture(t)
	published := publishNativeReferenceContent(t, s, scope, "服务端标题", "native-publish-one")

	created, err := s.CreateNativeReference(context.Background(), scope, NativeReferenceInput{ContentID: published.ID, ContentVersion: published.Version, IdempotencyKey: "native-reference-one"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateNativeReference(context.Background(), scope, NativeReferenceInput{ContentID: published.ID, ContentVersion: published.Version, IdempotencyKey: "native-reference-one"})
	if err != nil || again.ReferenceID != created.ReferenceID || created.Target.Title != "服务端标题" || !created.Available || created.CreatorActor != scope.Actor {
		t.Fatalf("created=%#v again=%#v err=%v", created, again, err)
	}
	if _, err = s.CreateNativeReference(context.Background(), scope, NativeReferenceInput{ContentID: published.ID, ContentVersion: published.Version + 1, IdempotencyKey: "native-reference-one"}); err == nil || !strings.Contains(err.Error(), "幂等") {
		t.Fatalf("semantic collision err=%v", err)
	}

	store := s.Store.(*memoryStore)
	store.mu.Lock()
	defer store.mu.Unlock()
	row := store.data[s.Config.Tenant]["native_ref:"+created.ReferenceID]
	if row.Owner != scope.Owners[0] || row.Visitor != actorVisitorKey(scope.Actor) || row.Content != published.ID || strings.Contains(string(row.Payload), "只能留在") {
		t.Fatalf("unsafe native reference record: %#v %s", row, row.Payload)
	}
	var stored nativeReferenceRecord
	if json.Unmarshal(row.Payload, &stored) != nil || stored.CreatorActor != scope.Actor || stored.Target.RouteKey != publishedReaderRoute || stored.Target.ContentVersion != published.Version {
		t.Fatalf("stored=%#v", stored)
	}
}

func TestNativeReferenceCandidatesUseBoundedCursorWithoutLosingPublishedRows(t *testing.T) {
	s, scope := fixture(t)
	want := map[string]bool{}
	for i, title := range []string{"已发布甲", "已发布乙", "已发布丙"} {
		content := publishNativeReferenceContent(t, s, scope, title, "native-publish-"+string(rune('a'+i)))
		want[content.ID] = true
	}
	if _, err := s.SaveContent(context.Background(), scope, "", Draft{Title: "草稿不能选", Body: "draft", Owner: scope.Owners[0], Rights: "own"}); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		out, err := s.NativeReferenceCandidates(context.Background(), scope, cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Items) > 1 {
			t.Fatalf("unbounded page: %#v", out)
		}
		for _, item := range out.Items {
			if !want[item.ContentID] || item.RouteKey != publishedReaderRoute {
				t.Fatalf("unexpected candidate: %#v", item)
			}
			seen[item.ContentID] = true
		}
		if out.NextCursor == "" {
			break
		}
		if out.NextCursor == cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = out.NextCursor
	}
	if len(seen) != len(want) {
		t.Fatalf("seen=%v want=%v", seen, want)
	}
}

func TestNativeReferenceCandidateSearchRunsBeforePagination(t *testing.T) {
	s, scope := fixture(t)
	wanted := publishNativeReferenceContent(t, s, scope, "唯一庭院方案", "native-search-wanted")
	for index := 0; index < 5; index++ {
		publishNativeReferenceContent(t, s, scope, "无关内容", "native-search-noise-"+string(rune('a'+index)))
	}
	page, err := s.SearchNativeReferenceCandidates(context.Background(), scope, "", 1, "庭院")
	if err != nil || len(page.Items) != 1 || page.Items[0].ContentID != wanted.ID || page.NextCursor != "" {
		t.Fatalf("searched=%+v err=%v", page, err)
	}
	if _, err = s.SearchNativeReferenceCandidates(context.Background(), scope, "", 20, strings.Repeat("x", 101)); err == nil {
		t.Fatal("accepted oversized candidate search")
	}
}

func TestNativeReferenceRevokedSourceStaysVisibleButCannotOpen(t *testing.T) {
	s, scope := fixture(t)
	published := publishNativeReferenceContent(t, s, scope, "会撤回的来源", "native-publish-revoke")
	created, err := s.CreateNativeReference(context.Background(), scope, NativeReferenceInput{ContentID: published.ID, ContentVersion: published.Version, IdempotencyKey: "native-reference-revoke"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Revoke(context.Background(), scope, published.ID, published.Revision, "测试撤回", "native-revoke-one"); err != nil {
		t.Fatal(err)
	}
	listed, err := s.NativeReferences(context.Background(), scope, "", 20)
	if err != nil || len(listed.Items) != 1 || listed.Items[0].ReferenceID != created.ReferenceID || listed.Items[0].Available || listed.Items[0].Target.Title != "会撤回的来源" {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	if _, err = s.NativeReferenceTarget(context.Background(), scope, created.ReferenceID); err == nil {
		t.Fatal("revoked source remained shareable")
	}
}

func TestNativeReferencesRejectDraftLegacyForeignOwnerAndForeignActor(t *testing.T) {
	s, scope := fixture(t)
	draft, err := s.SaveContent(context.Background(), scope, "", Draft{Title: "私有草稿", Body: "draft", Owner: scope.Owners[0], Rights: "own"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateNativeReference(context.Background(), scope, NativeReferenceInput{ContentID: draft.ID, ContentVersion: 1, IdempotencyKey: "native-draft-denied"}); err == nil {
		t.Fatal("draft accepted")
	}

	store := s.Store.(*memoryStore)
	legacy := Content{ID: "legacy-content-1", Version: 1, Title: "旧作品元数据", Type: "article", Status: "published", Owner: scope.Owners[0], ReadOnly: true, VersionMode: "source_snapshot"}
	if err = store.Transaction(context.Background(), s.Config.Tenant, func(r Repository) error {
		if err := r.Insert(record("content", legacy.ID, legacy.Owner, s.Now(), legacy)); err != nil {
			return err
		}
		return r.Insert(record("version", contentKey(legacy.ID, 1), legacy.Owner, s.Now(), legacy))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateNativeReference(context.Background(), scope, NativeReferenceInput{ContentID: legacy.ID, ContentVersion: 1, IdempotencyKey: "native-legacy-denied"}); err == nil {
		t.Fatal("legacy metadata accepted as replayable body")
	}

	published := publishNativeReferenceContent(t, s, scope, "仅当前创建者", "native-publish-actor")
	created, err := s.CreateNativeReference(context.Background(), scope, NativeReferenceInput{ContentID: published.ID, ContentVersion: 1, IdempotencyKey: "native-reference-actor"})
	if err != nil {
		t.Fatal(err)
	}
	foreignActor := scope
	foreignActor.Actor++
	if _, err = s.NativeReferenceTarget(context.Background(), foreignActor, created.ReferenceID); err == nil {
		t.Fatal("foreign actor opened reference")
	}
	foreignOwner := scope
	foreignOwner.Owners = []string{"owner-b"}
	if _, err = s.NativeReferenceTarget(context.Background(), foreignOwner, created.ReferenceID); err == nil {
		t.Fatal("foreign owner opened reference")
	}
	if err = s.DeleteNativeReference(context.Background(), foreignActor, created.ReferenceID); err == nil {
		t.Fatal("foreign actor deleted reference")
	}
	if err = s.DeleteNativeReference(context.Background(), scope, created.ReferenceID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.NativeReferenceTarget(context.Background(), scope, created.ReferenceID); !errors.As(err, new(*Error)) {
		t.Fatalf("deleted reference err=%v", err)
	}
}

type nativeReferenceReadProbe struct {
	Repository
	points   int
	queries  int
	failKind string
}

func (p *nativeReferenceReadProbe) Get(kind, id string) (Record, error) {
	p.points++
	return p.Repository.Get(kind, id)
}
func (p *nativeReferenceReadProbe) ListScoped(kind string, q RecordQuery) ([]Record, error) {
	p.queries++
	if kind == p.failKind {
		return nil, errors.New("injected source database failure")
	}
	return scopedRecords(p.Repository, kind, q)
}
func TestNativeReferenceBatchDoesNotHideStorageFailureOrPerformPointEnrichment(t *testing.T) {
	s, scope := fixture(t)
	a := publishNativeReferenceContent(t, s, scope, "source-a", "native-publish-batch-a")
	b := publishNativeReferenceContent(t, s, scope, "source-b", "native-publish-batch-b")
	err := s.readQueryTransaction(context.Background(), func(r Repository) error {
		probe := &nativeReferenceReadProbe{Repository: r}
		targets := []NativeReferenceTarget{{ContentID: a.ID, ContentVersion: a.Version}, {ContentID: b.ID, ContentVersion: b.Version}}
		items, _, err := s.nativeReferenceSources(probe, scope, targets)
		if err != nil || len(items) != 2 || probe.points != 0 || probe.queries != 3 {
			t.Fatalf("items=%d points=%d queries=%d err=%v", len(items), probe.points, probe.queries, err)
		}
		for _, kind := range []string{"content", "version", "content_revoked"} {
			probe.failKind = kind
			if _, _, err = s.nativeReferenceSources(probe, scope, targets); err == nil || !strings.Contains(err.Error(), "database failure") {
				t.Fatalf("storage failure hidden for %s: %v", kind, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNativeReferenceKeepsV1WhileTheActualReaderCreatesTheirOwnShare(t *testing.T) {
	s, author := fixture(t)
	ctx := context.Background()
	v1 := publishNativeReferenceContent(t, s, author, "固定来源一", "native-publish-fixed")
	sharer := author
	sharer.Actor++
	ref, err := s.CreateNativeReference(ctx, sharer, NativeReferenceInput{ContentID: v1.ID, ContentVersion: 1, IdempotencyKey: "native-fixed-ref"})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.SaveContent(ctx, author, v1.ID, Draft{Title: "来源二", Body: "更新正文", Owner: v1.Owner, Rights: "own", Revision: v1.Revision})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(ctx, author, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(ctx, author, draft.ID, draft.Revision, preview["preview_token"].(string), "native-publish-v2"); err != nil {
		t.Fatal(err)
	}
	listed, err := s.NativeReferences(ctx, sharer, "", 20)
	if err != nil || len(listed.Items) != 1 || listed.Items[0].Target.Title != v1.Title || listed.Items[0].Target.ContentVersion != 1 {
		t.Fatalf("V1 changed: %+v %v", listed, err)
	}
	target, err := s.NativeReferenceTarget(ctx, sharer, ref.ReferenceID)
	if err != nil || !strings.Contains(target["path"], "content_version=1") {
		t.Fatal(target, err)
	}
	entry, err := s.Enter(ctx, EntryInput{Route: publishedReaderRoute, Content: v1.ID, Version: 1, Client: "native-reader-client", Consent: s.Config.ConsentVersion}, sharer.Actor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateShare(ctx, ShareInput{Route: publishedReaderRoute, Content: v1.ID, Version: 1, Channel: "app_message", Key: "native-reader-share"}, entry.Token, sharer.Actor)
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.ResolveShareRecord(ctx, created["share_token"].(string))
	if err != nil || share.Sharer != actorVisitorKey(sharer.Actor) || share.Version != 1 {
		t.Fatalf("actual sharer drift: %+v %v", share, err)
	}
}
