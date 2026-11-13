package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func collectionInput(owner, key string, contents ...Content) ProjectCollectionDraft {
	items := make([]ProjectCollectionItemInput, 0, len(contents))
	for _, content := range contents {
		items = append(items, ProjectCollectionItemInput{ContentID: content.ID, ContentVersion: content.Version})
	}
	return ProjectCollectionDraft{Title: "乡墅项目合集", Description: "已发布案例", Owner: owner, Items: items, IdempotencyKey: key}
}

func TestProjectCollectionDraftIsIdempotentOrderedAndRevisionChecked(t *testing.T) {
	s, scope := fixture(t)
	first := publishNativeReferenceContent(t, s, scope, "案例甲", "collection-source-a")
	second := publishNativeReferenceContent(t, s, scope, "案例乙", "collection-source-b")
	in := collectionInput(scope.Owners[0], "collection-create-one", second, first)

	created, err := s.SaveProjectCollection(context.Background(), scope, "", in)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.SaveProjectCollection(context.Background(), scope, "", in)
	if err != nil || again.ID != created.ID || created.Revision != 1 || len(created.Items) != 2 || created.Items[0].Title != "案例乙" || created.Items[1].Title != "案例甲" {
		t.Fatalf("created=%#v again=%#v err=%v", created, again, err)
	}

	changed := in
	changed.Title = "参数冲突"
	if _, err = s.SaveProjectCollection(context.Background(), scope, "", changed); err == nil || !strings.Contains(err.Error(), "幂等") {
		t.Fatalf("idempotency conflict err=%v", err)
	}

	update := in
	update.IdempotencyKey = ""
	update.ExpectedRevision = created.Revision
	update.Title = "更新后合集"
	updated, err := s.SaveProjectCollection(context.Background(), scope, created.ID, update)
	if err != nil || updated.Revision != 2 || updated.Title != update.Title {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	if _, err = s.SaveProjectCollection(context.Background(), scope, created.ID, update); err == nil || !strings.Contains(err.Error(), "更新") {
		t.Fatalf("stale revision err=%v", err)
	}

	page, err := s.ProjectCollections(context.Background(), scope, "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	foreign := scope
	foreign.Actor++
	if _, err = s.ProjectCollection(context.Background(), foreign, created.ID); err == nil {
		t.Fatal("foreign actor opened collection")
	}
}

func TestProjectCollectionRejectsDraftForeignLegacyAndDuplicateSources(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	published := publishNativeReferenceContent(t, s, scope, "可选作品", "collection-valid-source")
	draft, err := s.SaveContent(ctx, scope, "", Draft{Title: "私密草稿", Body: "private", Owner: scope.Owners[0], Rights: "own"})
	if err != nil {
		t.Fatal(err)
	}
	other := scope
	other.Owners = []string{"owner-b"}
	foreign := publishNativeReferenceContent(t, s, other, "其他归属", "collection-foreign-source")
	legacy := Content{ID: "legacy-collection-source", Version: 1, Revision: 1, Title: "旧摘要", Type: "article", Status: "published", Owner: scope.Owners[0], ReadOnly: true, VersionMode: "source_snapshot"}
	if err = s.Store.Transaction(ctx, s.Config.Tenant, func(r Repository) error {
		if err := r.Insert(record("content", legacy.ID, legacy.Owner, s.Now(), legacy)); err != nil {
			return err
		}
		return r.Insert(record("version", contentKey(legacy.ID, 1), legacy.Owner, s.Now(), legacy))
	}); err != nil {
		t.Fatal(err)
	}

	bad := []ProjectCollectionDraft{
		collectionInput(scope.Owners[0], "collection-draft-source", draft),
		collectionInput(scope.Owners[0], "collection-foreign-owner", foreign),
		collectionInput(scope.Owners[0], "collection-legacy-source", legacy),
		collectionInput(scope.Owners[0], "collection-duplicate-source", published, published),
	}
	for _, in := range bad {
		if _, err = s.SaveProjectCollection(ctx, scope, "", in); err == nil {
			t.Fatalf("invalid collection accepted: %#v", in)
		}
	}

	candidates, err := s.ProjectCollectionCandidates(ctx, scope, "", 20)
	if err != nil || len(candidates.Items) != 1 || candidates.Items[0].ContentID != published.ID {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
}

func TestProjectCollectionPublishKeepsImmutableVersionsAndRemovalDoesNotDeleteSource(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	first := publishNativeReferenceContent(t, s, scope, "V1作品甲", "collection-publish-a")
	second := publishNativeReferenceContent(t, s, scope, "V1作品乙", "collection-publish-b")
	draft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-publish-create", first, second))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewProjectCollection(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.PublishProjectCollection(ctx, scope, draft.ID, draft.Revision, preview.PreviewToken, "collection-publish-v1")
	if err != nil || v1.Version != 1 || len(v1.Items) != 2 {
		t.Fatalf("v1=%#v err=%v", v1, err)
	}
	foreignOwner := scope
	foreignOwner.Owners = []string{"owner-b"}
	if _, err = s.PublishProjectCollection(ctx, foreignOwner, draft.ID, draft.Revision, preview.PreviewToken, "collection-publish-v1"); err == nil {
		t.Fatal("idempotent publish leaked collection after owner grant loss")
	}

	edit := collectionInput(scope.Owners[0], "", second)
	edit.ExpectedRevision = v1.Revision
	updated, err := s.SaveProjectCollection(ctx, scope, draft.ID, edit)
	if err != nil {
		t.Fatal(err)
	}
	preview, err = s.PreviewProjectCollection(ctx, scope, draft.ID, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.PublishProjectCollection(ctx, scope, draft.ID, updated.Revision, preview.PreviewToken, "collection-publish-v2")
	if err != nil || v2.Version != 2 || len(v2.Items) != 1 || v2.Items[0].ContentID != second.ID {
		t.Fatalf("v2=%#v err=%v", v2, err)
	}

	old, err := s.ReadPublishedProjectCollection(ctx, draft.ID, 1)
	if err != nil || len(old.Items) != 2 || old.Items[0].ContentID != first.ID {
		t.Fatalf("old=%#v err=%v", old, err)
	}
	if _, err = s.ReadContent(ctx, first.ID, first.Version); err != nil {
		t.Fatalf("removing collection reference deleted source: %v", err)
	}
}

func TestProjectCollectionPublicReadRechecksRevocationWithoutLeakingSourceTitle(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	source := publishNativeReferenceContent(t, s, scope, "撤回后不可泄露的标题", "collection-revoke-source")
	draft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-revoke-create", source))
	if err != nil {
		t.Fatal(err)
	}
	preview, _ := s.PreviewProjectCollection(ctx, scope, draft.ID, draft.Revision)
	published, err := s.PublishProjectCollection(ctx, scope, draft.ID, draft.Revision, preview.PreviewToken, "collection-revoke-publish")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Revoke(ctx, scope, source.ID, source.Revision, "撤回源", "collection-source-revoke"); err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadPublishedProjectCollection(ctx, draft.ID, published.Version)
	if err != nil || len(read.Items) != 1 || read.Items[0].Available || read.Items[0].Title != "" || read.Items[0].ContentID != "" {
		t.Fatalf("read=%#v err=%v", read, err)
	}
	encoded, _ := json.Marshal(read)
	if strings.Contains(string(encoded), "撤回后不可泄露") {
		t.Fatalf("revoked title leaked: %s", encoded)
	}

	managed, err := s.ProjectCollection(ctx, scope, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := s.RevokeProjectCollection(ctx, scope, draft.ID, managed.Revision, "合集撤回", "collection-revoke-one")
	if err != nil || revoked.Status != "revoked" {
		t.Fatalf("revoked=%#v err=%v", revoked, err)
	}
	if _, err = s.ReadPublishedProjectCollection(ctx, draft.ID, published.Version); err == nil {
		t.Fatal("revoked collection remained public")
	}
}

func TestProjectCollectionItemTargetRequiresCurrentManifestMembership(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	source := publishNativeReferenceContent(t, s, scope, "固定子作品", "collection-target-source")
	draft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-target-create", source))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewProjectCollection(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.PublishProjectCollection(ctx, scope, draft.ID, draft.Revision, preview.PreviewToken, "collection-target-publish")
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.OpenPublishedProjectCollectionItem(ctx, draft.ID, published.Version, 0)
	if err != nil || target["route_key"] != publishedReaderRoute || !strings.Contains(target["path"], "content_id="+source.ID) || strings.Contains(target["path"], "st=") {
		t.Fatalf("target=%#v err=%v", target, err)
	}
	if _, err = s.OpenPublishedProjectCollectionItem(ctx, draft.ID, published.Version, 1); err == nil {
		t.Fatal("out-of-range collection item opened")
	}
	if _, err = s.Revoke(ctx, scope, source.ID, source.Revision, "撤回源", "collection-target-revoke"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OpenPublishedProjectCollectionItem(ctx, draft.ID, published.Version, 0); err == nil {
		t.Fatal("revoked source remained reachable through collection context")
	}
}

func TestProjectCollectionDeleteOnlyUnpublishedDraftAndKeepsSource(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	source := publishNativeReferenceContent(t, s, scope, "原作品", "collection-delete-source")
	draft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-delete-create", source))
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := s.DeleteProjectCollectionDraft(ctx, scope, draft.ID, draft.Revision)
	if err != nil || deleted.Status != "deleted" {
		t.Fatalf("deleted=%#v err=%v", deleted, err)
	}
	if _, err = s.ProjectCollection(ctx, scope, draft.ID); err == nil {
		t.Fatal("deleted draft remained in management")
	}
	if _, err = s.ReadContent(ctx, source.ID, source.Version); err != nil {
		t.Fatalf("draft deletion removed source: %v", err)
	}
}

func TestProjectCollectionDeleteSupportsPublishedAndRevokedAndInvalidatesFixedVersion(t *testing.T) {
	s, scope := fixture(t)
	var revokedCollectionID string
	s.OnCollectionRevoke = func(_ context.Context, collectionID string) error {
		revokedCollectionID = collectionID
		return nil
	}
	s.Config.Routes[projectCollectionReaderRoute] = Route{Public: true}
	ctx := context.Background()
	source := publishNativeReferenceContent(t, s, scope, "原作品", "collection-delete-any-source")
	draft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-delete-any-create", source))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewProjectCollection(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.PublishProjectCollection(ctx, scope, draft.ID, draft.Revision, preview.PreviewToken, "collection-delete-any-publish")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.Enter(ctx, EntryInput{Route: projectCollectionReaderRoute, Content: draft.ID, Version: published.Version, Client: "collection-delete-client", Consent: s.Config.ConsentVersion}, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := s.CreateShare(ctx, ShareInput{Route: projectCollectionReaderRoute, Content: draft.ID, Version: published.Version, Channel: "app_message", Key: "collection-delete-share"}, entry.Token, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeleteProjectCollection(ctx, Scope{Tenant: scope.Tenant, Actor: scope.Actor + 1, Owners: scope.Owners, Manage: true}, draft.ID, published.Revision); err == nil {
		t.Fatal("其他创建人删除成功")
	}
	if _, err = s.DeleteProjectCollection(ctx, scope, draft.ID, published.Revision+1); err == nil {
		t.Fatal("过期版本删除成功")
	}
	deleted, err := s.DeleteProjectCollection(ctx, scope, draft.ID, published.Revision)
	if err != nil || deleted.Status != "deleted" {
		t.Fatalf("deleted=%#v err=%v", deleted, err)
	}
	if revokedCollectionID != draft.ID {
		t.Fatalf("notification collection=%q want=%q", revokedCollectionID, draft.ID)
	}
	if _, err = s.ReadPublishedProjectCollection(ctx, draft.ID, published.Version); err == nil {
		t.Fatal("删除后固定版本仍可访问")
	}
	if _, err = s.ResolveShareRecord(ctx, shared["share_token"].(string)); err == nil {
		t.Fatal("删除后合集旧分享仍可打开")
	}
	if _, err = s.ReadContent(ctx, source.ID, source.Version); err != nil {
		t.Fatalf("删除合集误删原作: %v", err)
	}
}

func TestProjectCollectionDeleteRollsBackWhenNotificationCancellationFails(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	source := publishNativeReferenceContent(t, s, scope, "回滚原作", "collection-delete-rollback-source")
	draft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-delete-rollback-create", source))
	if err != nil {
		t.Fatal(err)
	}
	s.OnCollectionRevoke = func(context.Context, string) error {
		return errors.New("notification cancellation failed")
	}

	if _, err = s.DeleteProjectCollection(ctx, scope, draft.ID, draft.Revision); err == nil {
		t.Fatal("通知取消失败后合集仍被删除")
	}
	kept, err := s.ProjectCollection(ctx, scope, draft.ID)
	if err != nil || kept.Status == "deleted" || kept.Revision != draft.Revision {
		t.Fatalf("collection=%#v err=%v", kept, err)
	}
}

func TestProjectCollectionStorageCorruptionIsNotDisguisedAsClientError(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	source := publishNativeReferenceContent(t, s, scope, "存储检查作品", "collection-corruption-source")
	draft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-corruption-create", source))
	if err != nil {
		t.Fatal(err)
	}
	store := s.Store.(*memoryStore)
	if err = store.Transaction(ctx, s.Config.Tenant, func(r Repository) error {
		row, err := r.Get(projectCollectionKind, draft.ID)
		if err != nil {
			return err
		}
		row.Payload = json.RawMessage(`{"broken"`)
		return r.Put(row)
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.ProjectCollection(ctx, scope, draft.ID)
	var clientError *Error
	if err == nil || errors.As(err, &clientError) {
		t.Fatalf("storage corruption disguised as client error: %T %v", err, err)
	}
}

func TestProjectCollectionPublishDoesNotDisguiseCorruptPreview(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	source := publishNativeReferenceContent(t, s, scope, "预览检查作品", "collection-preview-corruption-source")
	draft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-preview-corruption-create", source))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewProjectCollection(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	store := s.Store.(*memoryStore)
	if err = store.Transaction(ctx, s.Config.Tenant, func(r Repository) error {
		row, err := r.Get("project_collection_preview", hash(preview.PreviewToken))
		if err != nil {
			return err
		}
		row.Payload = json.RawMessage(`{"broken"`)
		return r.Put(row)
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.PublishProjectCollection(ctx, scope, draft.ID, draft.Revision, preview.PreviewToken, "collection-corrupt-preview-publish")
	var clientError *Error
	if err == nil || errors.As(err, &clientError) {
		t.Fatalf("preview corruption disguised as conflict: %T %v", err, err)
	}
}

func TestProjectCollectionRevokeIdempotencyBindsRevisionAndReason(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	source := publishNativeReferenceContent(t, s, scope, "撤回幂等作品", "collection-revoke-idempotency-source")
	draft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-revoke-idempotency-create", source))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewProjectCollection(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.PublishProjectCollection(ctx, scope, draft.ID, draft.Revision, preview.PreviewToken, "collection-revoke-idempotency-publish")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RevokeProjectCollection(ctx, scope, draft.ID, published.Revision, "业务撤回", "collection-revoke-idempotency-key"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RevokeProjectCollection(ctx, scope, draft.ID, published.Revision+1, "业务撤回", "collection-revoke-idempotency-key"); err == nil || !strings.Contains(err.Error(), "幂等") {
		t.Fatalf("changed revision reused revoke key: %v", err)
	}
	if _, err = s.RevokeProjectCollection(ctx, scope, draft.ID, published.Revision, "其他原因", "collection-revoke-idempotency-key"); err == nil || !strings.Contains(err.Error(), "幂等") {
		t.Fatalf("changed reason reused revoke key: %v", err)
	}
}

func TestProjectCollectionDirectoryCoverUsesScopedMediaAndDisappearsWithSource(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	source := publishNativeReferenceContent(t, s, scope, "封面案例", "collection-cover-source")
	source.Cover = &Asset{ID: "collection-cover-media", URL: "/api/v1/miniprogram/tracking/media/collection-cover-media", Kind: "image"}
	if err := s.Store.Transaction(ctx, s.Config.Tenant, func(r Repository) error {
		if err := r.Put(record("content", source.ID, source.Owner, s.Now(), source)); err != nil {
			return err
		}
		return r.Put(record("version", contentKey(source.ID, source.Version), source.Owner, s.Now(), source))
	}); err != nil {
		t.Fatal(err)
	}
	draft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-cover-create", source))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewProjectCollection(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.PublishProjectCollection(ctx, scope, draft.ID, draft.Revision, preview.PreviewToken, "collection-cover-publish")
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.ReadPublishedProjectCollection(ctx, draft.ID, published.Version)
	if err != nil {
		t.Fatal(err)
	}
	expected := projectCollectionMediaPath(draft.ID, published.Version, 0, source.Cover.ID)
	if view.Cover == nil || view.Cover.URL != expected || view.Items[0].Cover.URL != expected {
		t.Fatalf("unscoped cover: %#v", view.Cover)
	}
	if _, err = s.Revoke(ctx, scope, source.ID, source.Revision, "撤回案例", "collection-cover-revoke"); err != nil {
		t.Fatal(err)
	}
	view, err = s.ReadPublishedProjectCollection(ctx, draft.ID, published.Version)
	if err != nil {
		t.Fatal(err)
	}
	if view.Cover != nil || view.Items[0].Cover != nil {
		t.Fatal("withdrawn source leaked directory cover")
	}
}

func TestProjectCollectionCoverItemIndexOverridesFirstCover(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	publishWithCover := func(key string) Content {
		t.Helper()
		mediaID := "cover-media-" + key
		if err := s.AddMedia(ctx, scope, Asset{ID: mediaID, Kind: "image", URL: "/api/v1/miniprogram/tracking/media/" + mediaID, Width: 800, Height: 600, MIME: "image/png"}, scope.Owners[0]); err != nil {
			t.Fatal(err)
		}
		draft, err := s.SaveContent(ctx, scope, "", Draft{Title: "带封面" + key, Body: "正文", BodyFormat: "plain", Owner: scope.Owners[0], Rights: "own", CoverID: mediaID})
		if err != nil {
			t.Fatal(err)
		}
		preview, err := s.Preview(ctx, scope, draft.ID, draft.Revision)
		if err != nil {
			t.Fatal(err)
		}
		published, err := s.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), "cover-publish-"+key)
		if err != nil {
			t.Fatal(err)
		}
		return published["content"].(Content)
	}
	first := publishWithCover("a")
	second := publishWithCover("b")
	in := collectionInput(scope.Owners[0], "collection-cover-index", second, first)
	secondIndex := 0 // second 排在首位
	in.CoverItemIndex = &secondIndex
	created, err := s.SaveProjectCollection(context.Background(), scope, "", in)
	if err != nil {
		t.Fatal(err)
	}
	if created.Cover == nil || created.Cover.ID != second.Cover.ID {
		t.Fatalf("指定封面索引应生效 created.Cover=%v", created.Cover)
	}

	// 无效索引回退第一个有封面条目（历史行为）
	bad := 99
	in.CoverItemIndex = &bad
	in.IdempotencyKey = "collection-cover-bad"
	fallback, err := s.SaveProjectCollection(context.Background(), scope, "", in)
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Cover == nil || fallback.Cover.ID != second.Cover.ID {
		t.Fatalf("越界索引应回退首图 fallback.Cover=%v", fallback.Cover)
	}

	// 不指定保持历史行为
	plain := collectionInput(scope.Owners[0], "collection-cover-plain", second, first)
	legacy, err := s.SaveProjectCollection(context.Background(), scope, "", plain)
	if err != nil || legacy.Cover == nil || legacy.Cover.ID != second.Cover.ID {
		t.Fatalf("不指定封面索引保持首图 legacy=%v err=%v", legacy.Cover, err)
	}

	// 指定第二个条目（first）为封面
	tail := 1
	tailInput := collectionInput(scope.Owners[0], "collection-cover-tail", second, first)
	tailInput.CoverItemIndex = &tail
	tailCollection, err := s.SaveProjectCollection(context.Background(), scope, "", tailInput)
	if err != nil || tailCollection.Cover == nil || tailCollection.Cover.ID != first.Cover.ID {
		t.Fatalf("索引 1 应取第二个条目封面 got=%v err=%v", tailCollection.Cover, err)
	}
	if tailCollection.CoverItemIndex == nil || *tailCollection.CoverItemIndex != 1 {
		t.Fatal("封面位置未保存")
	}
	legacySelected := tailCollection
	legacySelected.CoverItemIndex = nil
	if err := s.Store.Transaction(ctx, s.Config.Tenant, func(r Repository) error {
		return r.Put(projectCollectionRecord(projectCollectionKind, legacySelected, s.Now()))
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewProjectCollection(ctx, scope, tailCollection.ID, tailCollection.Revision)
	if err != nil || preview.Collection.Cover == nil || preview.Collection.Cover.ID != first.Cover.ID {
		t.Fatalf("自选封面必须贯穿预览: cover=%v err=%v", preview.Collection.Cover, err)
	}
	published, err := s.PublishProjectCollection(ctx, scope, tailCollection.ID, tailCollection.Revision, preview.PreviewToken, "chosen-cover-publish")
	if err != nil || published.Cover == nil || published.Cover.ID != first.Cover.ID {
		t.Fatalf("自选封面必须贯穿发布: cover=%v err=%v", published.Cover, err)
	}
	read, err := s.ReadPublishedProjectCollection(ctx, tailCollection.ID, published.Version)
	if err != nil || read.Cover == nil || read.Cover.ID != first.Cover.ID {
		t.Fatalf("自选封面必须贯穿公开读取: cover=%v err=%v", read.Cover, err)
	}
	if _, err = s.Revoke(ctx, scope, first.ID, first.Revision, "来源撤回", "chosen-cover-revoke"); err != nil {
		t.Fatal(err)
	}
	read, err = s.ReadPublishedProjectCollection(ctx, tailCollection.ID, published.Version)
	if err != nil || read.Cover == nil || read.Cover.ID != second.Cover.ID {
		t.Fatalf("已撤回封面应安全回退: cover=%v err=%v", read.Cover, err)
	}
}

// B10 方案A：协作者字段持久化与上限校验。
func TestProjectCollectionCollaboratorsPersistAndValidate(t *testing.T) {
	s, scope := fixture(t)
	content := publishNativeReferenceContent(t, s, scope, "协作案例", "collab-source")

	in := collectionInput(scope.Owners[0], "collection-collab", content)
	in.Collaborators = []int64{42, 42, 0, 99} // 去重+过滤零值
	created, err := s.SaveProjectCollection(context.Background(), scope, "", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Collaborators) != 2 || created.Collaborators[0] != 42 || created.Collaborators[1] != 99 {
		t.Fatalf("协作者应去重并过滤零值 got=%v", created.Collaborators)
	}

	// 超限拒绝
	over := collectionInput(scope.Owners[0], "collection-collab-over", content)
	over.Collaborators = make([]int64, 21)
	for i := range over.Collaborators {
		over.Collaborators[i] = int64(i + 1)
	}
	if _, err = s.SaveProjectCollection(context.Background(), scope, "", over); err == nil {
		t.Fatal("协作者超过 20 人应被拒绝")
	}
}

func TestProjectCollectionDescriptionCountsCharacters(t *testing.T) {
	s, scope := fixture(t)
	source := publishNativeReferenceContent(t, s, scope, "简介案例", "description-source")
	input := collectionInput(scope.Owners[0], "description-create", source)
	input.Description = strings.Repeat("中", 500)
	if _, err := s.SaveProjectCollection(context.Background(), scope, "", input); err != nil {
		t.Fatalf("500汉字应可保存: %v", err)
	}
	input.Description += "中"
	input.IdempotencyKey = "description-too-long"
	if _, err := s.SaveProjectCollection(context.Background(), scope, "", input); err == nil {
		t.Fatal("501汉字不得保存")
	}
}
