package tracking

import (
	"context"
	"testing"
)

func TestDocumentDraftReusesPrivateMediaAndCatalogSearch(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	asset := Asset{ID: "document-asset-0001", Kind: "document", URL: "/api/v1/miniprogram/tracking/media/document-asset-0001", MIME: "application/pdf", Title: "庭院方案.pdf"}
	if err := s.AddMedia(ctx, scope, asset, "owner-a"); err != nil {
		t.Fatal(err)
	}
	body := `{"schema":1,"media_id":"document-asset-0001"}`
	saved, err := s.SaveContent(ctx, scope, "", Draft{Title: "庭院方案 PDF", Body: body, BodyFormat: "document_v1", Rights: "own", Owner: "owner-a", MediaIDs: []string{asset.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Type != "document" || saved.BodyFormat != "document_v1" || saved.Status != "draft" || saved.Version != 0 || len(saved.Media) != 1 || saved.Media[0].ID != asset.ID {
		t.Fatalf("文档草稿未复用内容与私有媒体模型: %+v", saved)
	}
	updated, err := s.SaveContent(ctx, scope, saved.ID, Draft{Title: "庭院方案 PDF 修订", Body: body, BodyFormat: "document_v1", Rights: "own", Owner: "owner-a", MediaIDs: []string{asset.ID}, Revision: saved.Revision})
	if err != nil || updated.Revision != saved.Revision+1 || updated.Version != 0 || updated.Type != "document" {
		t.Fatalf("文档未复用内容草稿修订机制: %+v %v", updated, err)
	}
	if _, err = s.Preview(ctx, scope, updated.ID, updated.Revision); err == nil {
		t.Fatal("私有文档不应进入发布预览")
	}
	if _, err = s.Publish(ctx, scope, updated.ID, updated.Revision, "", "document-publish-key"); err == nil {
		t.Fatal("私有文档不应生成公开版本")
	}
	reopened, err := s.Contents(ctx, scope, Filter{}, saved.ID)
	if err != nil || len(reopened.Items.([]Content)) != 1 || reopened.Items.([]Content)[0].Media[0].ID != asset.ID {
		t.Fatalf("文档无法按原草稿重开: %+v %v", reopened, err)
	}
	searched, err := s.Contents(ctx, scope, Filter{ContentSearch: "PDF"}, "")
	if err != nil || len(searched.Items.([]Content)) != 1 || searched.Items.([]Content)[0].ID != saved.ID {
		t.Fatalf("文档未进入已有标题检索: %+v %v", searched, err)
	}
}

func TestDocumentDraftRejectsWrongOrAdditionalMedia(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	for _, asset := range []Asset{
		{ID: "document-asset-0001", Kind: "document", URL: "/api/v1/miniprogram/tracking/media/document-asset-0001", MIME: "application/pdf"},
		{ID: "image-asset-0000001", Kind: "image", URL: "/api/v1/miniprogram/tracking/media/image-asset-0000001", MIME: "image/png", Width: 1, Height: 1},
	} {
		if err := s.AddMedia(ctx, scope, asset, "owner-a"); err != nil {
			t.Fatal(err)
		}
	}
	tests := []Draft{
		{Title: "错误类型", Body: `{"schema":1,"media_id":"image-asset-0000001"}`, BodyFormat: "document_v1", Rights: "own", Owner: "owner-a", MediaIDs: []string{"image-asset-0000001"}},
		{Title: "多余媒体", Body: `{"schema":1,"media_id":"document-asset-0001"}`, BodyFormat: "document_v1", Rights: "own", Owner: "owner-a", MediaIDs: []string{"document-asset-0001", "image-asset-0000001"}},
		{Title: "伪装图文附件", Body: "正文", BodyFormat: "plain", Rights: "own", Owner: "owner-a", MediaIDs: []string{"document-asset-0001"}},
	}
	for _, draft := range tests {
		if _, err := s.SaveContent(ctx, scope, "", draft); err == nil {
			t.Fatalf("接受了无效文档草稿: %+v", draft)
		}
	}
}

func TestDocumentArchiveByMediaIDIsIdempotentAfterLostResponse(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	asset := Asset{ID: "document-asset-0001", Kind: "document", URL: "/api/v1/miniprogram/tracking/media/document-asset-0001", MIME: "application/pdf", Title: "庭院方案.pdf"}
	if err := s.AddMedia(ctx, scope, asset, "owner-a"); err != nil {
		t.Fatal(err)
	}
	draft := Draft{Title: "庭院方案", Body: `{"schema":1,"media_id":"document-asset-0001"}`, BodyFormat: "document_v1", Rights: "own", Owner: "owner-a", MediaIDs: []string{asset.ID}}
	first, err := s.SaveContent(ctx, scope, "", draft)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.SaveContent(ctx, scope, "", draft)
	if err != nil || replayed.ID != first.ID || replayed.Revision != first.Revision {
		t.Fatalf("建档响应丢失后未收敛到同一资料: first=%#v replayed=%#v err=%v", first, replayed, err)
	}
	draft.Title = "冲突标题"
	retitled, err := s.SaveContent(ctx, scope, "", draft)
	if err != nil || retitled.ID != first.ID || retitled.Title != first.Title {
		t.Fatalf("重进编辑器后不同标题的恢复请求未返回原资料: %#v %v", retitled, err)
	}
	listed, err := s.Contents(ctx, scope, Filter{}, "")
	if err != nil || len(listed.Items.([]Content)) != 1 {
		t.Fatalf("幂等重试创建了重复资料: %#v %v", listed, err)
	}
}
