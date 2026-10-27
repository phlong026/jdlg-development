package route

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"

	"service/contentimport"
	"service/tracking"
)

func TestImportMediaIsPrivateValidatedAndRestartIdempotent(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACKING_MEDIA_DIR", root)
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	fetches := 0
	fetch := func(context.Context, contentimport.Platform, string, int64) ([]byte, string, error) {
		fetches++
		return data.Bytes(), "image/png", nil
	}
	job := contentimport.Job{ID: "job-import-test-one", Tenant: "workbench", Owner: "owner-a", Actor: 11, Platform: contentimport.WechatArticle}
	media := contentimport.RemoteMedia{ID: "source-image-one", Kind: "image", URL: "https://mmbiz.qpic.cn/source"}
	first, err := saveImportedMedia(context.Background(), svc, scope, job, 0, media, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Media(context.Background(), nil, first.ID); err == nil {
		t.Fatal("draft image public")
	}
	again, err := saveImportedMedia(context.Background(), svc, scope, job, 0, media, fetch)
	if err != nil || first.ID != again.ID || fetches != 1 {
		t.Fatal(first, again, fetches, err)
	}
	another := job
	another.ID = "different-import-job"
	reused, reuseErr := saveImportedMedia(context.Background(), svc, scope, another, 0, media, fetch)
	if reuseErr != nil || reused.ID != first.ID || fetches != 1 {
		t.Fatal("source cache not reused", reused, fetches, reuseErr)
	}
	stat, err := os.Stat(os.Getenv("TRACKING_MEDIA_DIR") + "/" + first.ID)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal(stat, err)
	}
	path := os.Getenv("TRACKING_MEDIA_DIR") + "/" + first.ID
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := saveImportedMedia(context.Background(), svc, scope, job, 0, media, fetch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("missing object was not restored", err)
	}
	media.Kind = "video"
	if _, err = saveImportedMedia(context.Background(), svc, scope, job, 1, media, fetch); err == nil {
		t.Fatal("image masqueraded as video")
	}
}
func TestImportDraftReplacesEveryRemoteReference(t *testing.T) {
	doc := contentimport.Document{Source: contentimport.Source{Title: "导入", SourceURL: "https://mp.weixin.qq.com/s/example", Media: []contentimport.RemoteMedia{{Kind: "image"}}}, Body: tracking.ArticleDocument{Schema: 1, Blocks: []tracking.ArticleBlock{{Kind: "image", MediaID: "remote-0000"}}}}
	job := contentimport.Job{ID: "job-test", Owner: "one", Rights: "authorized"}
	draft, err := importDraft(job, doc, []tracking.Asset{{ID: "actual-media-one", Kind: "image"}})
	if err != nil || bytes.Contains([]byte(draft.Body), []byte("remote-")) {
		t.Fatal(draft, err)
	}
	doc.Body.Blocks[0].MediaID = "remote-9999"
	if _, err = importDraft(job, doc, []tracking.Asset{{ID: "actual-media-one", Kind: "image"}}); err == nil {
		t.Fatal("missing reference accepted")
	}
}
func TestImportDraftRepeatedImageUsesOneAssetGrant(t *testing.T) {
	doc := contentimport.Document{Source: contentimport.Source{Title: "重复图片", SourceURL: "https://mp.weixin.qq.com/s/example", Media: []contentimport.RemoteMedia{{Kind: "image"}}}, Body: tracking.ArticleDocument{Schema: 1, Blocks: []tracking.ArticleBlock{{Kind: "image", MediaID: "remote-0000"}, {Kind: "image", MediaID: "remote-0000"}}}}
	draft, err := importDraft(contentimport.Job{Owner: "one", Rights: "authorized"}, doc, []tracking.Asset{{ID: "stored-image-one", Kind: "image"}})
	if err != nil || len(draft.MediaIDs) != 1 || strings.Count(draft.Body, "stored-image-one") != 2 {
		t.Fatal(draft, err)
	}
}
