package tracking

import (
	"context"
	"testing"
)

func TestDeleteContentDraftSoftDeletesUnpublishedDraftAndKeepsMedia(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	asset := Asset{ID: "draft-media-asset-001", Kind: "image", URL: "/api/v1/miniprogram/tracking/media/draft-media-asset-001", MIME: "image/png", Width: 1, Height: 1}
	if err := s.AddMedia(ctx, scope, asset, "owner-a"); err != nil {
		t.Fatal(err)
	}
	draft, err := s.SaveContent(ctx, scope, "", Draft{Title: "可删除草稿", Body: `{"schema":1,"blocks":[{"kind":"image","media_id":"draft-media-asset-001"}]}`, BodyFormat: "article_v2", Rights: "own", Owner: "owner-a", MediaIDs: []string{asset.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeleteContentDraft(ctx, scope, draft.ID, draft.Revision+1); err == nil {
		t.Fatal("过期 revision 删除成功")
	}
	deleted, err := s.DeleteContentDraft(ctx, scope, draft.ID, draft.Revision)
	if err != nil || deleted.ContentID != draft.ID || deleted.Revision != draft.Revision+1 || deleted.Status != "deleted" || deleted.DeletedAt.IsZero() {
		t.Fatalf("deleted=%#v err=%v", deleted, err)
	}
	listed, err := s.Contents(ctx, scope, Filter{}, draft.ID)
	if err != nil || len(listed.Items.([]Content)) != 0 {
		t.Fatalf("软删除草稿仍在内容目录可见: %#v %v", listed, err)
	}
	if kept, err := s.Media(ctx, &scope, asset.ID); err != nil || kept.ID != asset.ID {
		t.Fatalf("删除草稿不得物理删除媒体: %#v %v", kept, err)
	}
	if _, err = s.SaveContent(ctx, scope, draft.ID, Draft{Title: "不得恢复", Body: "x", Rights: "own", Owner: "owner-a", Revision: deleted.Revision}); err == nil {
		t.Fatal("软删除草稿被保存接口恢复")
	}
	if _, err = s.DeleteContentDraft(ctx, scope, draft.ID, deleted.Revision); err == nil {
		t.Fatal("软删除草稿被重复删除")
	}
}

func TestDeleteContentDraftProtectsPublishedAndReferencedVersions(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	draft, err := s.SaveContent(ctx, scope, "", Draft{Title: "已发布保护", Body: "v1", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), "draft-delete-publish")
	if err != nil {
		t.Fatal(err)
	}
	current := published["content"].(Content)
	if _, err = s.DeleteContentDraft(ctx, scope, current.ID, current.Revision); err == nil {
		t.Fatal("已发布内容被当作草稿删除")
	}
	revised, err := s.SaveContent(ctx, scope, current.ID, Draft{Title: "已发布后的新草稿", Body: "v2", Rights: "own", Owner: "owner-a", Revision: current.Revision})
	if err != nil || revised.Status != "draft" || revised.Version != 1 {
		t.Fatalf("revised=%#v err=%v", revised, err)
	}
	if _, err = s.DeleteContentDraft(ctx, scope, revised.ID, revised.Revision); err == nil {
		t.Fatal("拥有已发布版本的新草稿被删除")
	}

	referenced, err := s.SaveContent(ctx, scope, "", Draft{Title: "异常引用保护", Body: "draft", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Store.Transaction(ctx, s.Config.Tenant, func(r Repository) error {
		row := record("native_ref", "draft-reference-001", referenced.Owner, s.Now(), map[string]string{"content_id": referenced.ID})
		row.Content = referenced.ID
		return r.Insert(row)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeleteContentDraft(ctx, scope, referenced.ID, referenced.Revision); err == nil {
		t.Fatal("已被引用的草稿被删除")
	}
}

func TestDeletedImportedDraftCannotReplayAsReadyContent(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	draft := Draft{Title: "导入草稿", Body: "draft", Rights: "own", Owner: "owner-a"}
	provenance := map[string]string{"platform": "wechat_article"}
	saved, err := s.SaveImportedDraft(ctx, scope, "import-delete-001", draft, provenance)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeleteContentDraft(ctx, scope, saved.ID, saved.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveImportedDraft(ctx, scope, "import-delete-001", draft, provenance); err == nil {
		t.Fatal("已删除导入草稿在幂等重放中重现")
	}
}

func TestDeleteCreatedContentSupportsPublishedAndRevokedAndInvalidatesFixedVersion(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	draft, err := s.SaveContent(ctx, scope, "", Draft{Title: "可删除作品", Body: "v1", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), "delete-created-publish")
	if err != nil {
		t.Fatal(err)
	}
	current := published["content"].(Content)
	if _, err = s.ReadContent(ctx, current.ID, current.Version); err != nil {
		t.Fatal(err)
	}
	reference, err := s.CreateNativeReference(ctx, scope, NativeReferenceInput{ContentID: current.ID, ContentVersion: current.Version, IdempotencyKey: "delete-created-reference"})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.Enter(ctx, EntryInput{Route: publishedReaderRoute, Content: current.ID, Version: current.Version, Client: "delete-created-client", Consent: s.Config.ConsentVersion}, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := s.CreateShare(ctx, ShareInput{Route: publishedReaderRoute, Content: current.ID, Version: current.Version, Channel: "app_message", Key: "delete-created-share"}, entry.Token, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	revokeHookCalled := false
	s.OnContentRevoke = func(context.Context, string) error { revokeHookCalled = true; return nil }
	if err = s.Store.Transaction(ctx, s.Config.Tenant, func(r Repository) error {
		row := record("notice_event", "delete-created-notice", current.Owner, s.Now(), map[string]bool{"queued": true})
		row.Content = current.ID
		return r.Insert(row)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeleteCreatedContent(ctx, Scope{Tenant: scope.Tenant, Actor: scope.Actor, Owners: []string{"owner-b"}, Manage: true}, current.ID, current.Revision); err == nil {
		t.Fatal("跨归属删除成功")
	}
	if _, err = s.DeleteCreatedContent(ctx, scope, current.ID, current.Revision+1); err == nil {
		t.Fatal("过期版本删除成功")
	}
	deleted, err := s.DeleteCreatedContent(ctx, scope, current.ID, current.Revision)
	if err != nil || deleted.Status != "deleted" || deleted.Revision != current.Revision+1 {
		t.Fatalf("deleted=%#v err=%v", deleted, err)
	}
	if _, err = s.ReadContent(ctx, current.ID, current.Version); err == nil {
		t.Fatal("删除后固定版本仍可访问")
	}
	if _, err = s.NativeReferenceTarget(ctx, scope, reference.ReferenceID); err == nil {
		t.Fatal("删除后已保存引用仍可打开")
	}
	if _, err = s.ResolveShareRecord(ctx, shared["share_token"].(string)); err == nil {
		t.Fatal("删除后旧分享仍可打开")
	}
	if !revokeHookCalled {
		t.Fatal("删除已发布内容未取消待投递通知")
	}

	revokedDraft, err := s.SaveContent(ctx, scope, "", Draft{Title: "先撤回再删除", Body: "v1", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	revokedPreview, _ := s.Preview(ctx, scope, revokedDraft.ID, revokedDraft.Revision)
	revokedPub, err := s.Publish(ctx, scope, revokedDraft.ID, revokedDraft.Revision, revokedPreview["preview_token"].(string), "delete-revoked-publish")
	if err != nil {
		t.Fatal(err)
	}
	revoked := revokedPub["content"].(Content)
	revoked, err = s.Revoke(ctx, scope, revoked.ID, revoked.Revision, "下架", "delete-revoked-revoke")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeleteCreatedContent(ctx, scope, revoked.ID, revoked.Revision); err != nil {
		t.Fatal(err)
	}
}
