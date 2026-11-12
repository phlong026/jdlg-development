package route

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"service/contentimport"
	"service/contentstorage"
	"service/tracking"
)

type importFileObjects struct {
	data     map[string][]byte
	filePuts int
	gets     int
	headMIME string
}

func (s *importFileObjects) Put(context.Context, string, string, []byte, string) error {
	return errors.New("byte put must not be used")
}
func (s *importFileObjects) PutFile(_ context.Context, tenant, id, path string, size int64, _ string, digest string) error {
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) != size {
		return contentstorage.ErrIntegrity
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return contentstorage.ErrIntegrity
	}
	s.filePuts++
	s.data[tenant+":"+id] = data
	return nil
}
func (s *importFileObjects) Get(_ context.Context, tenant, id string, max int64) ([]byte, string, error) {
	s.gets++
	data, ok := s.data[tenant+":"+id]
	if !ok {
		return nil, "", contentstorage.ErrNotFound
	}
	if int64(len(data)) > max {
		return nil, "", contentstorage.ErrIntegrity
	}
	return data, "video/mp4", nil
}
func (s *importFileObjects) Head(_ context.Context, tenant, id string) (contentstorage.Metadata, error) {
	data, ok := s.data[tenant+":"+id]
	if !ok {
		return contentstorage.Metadata{}, contentstorage.ErrNotFound
	}
	mime := s.headMIME
	if mime == "" {
		mime = "video/mp4"
	}
	return contentstorage.Metadata{Size: int64(len(data)), MIME: mime}, nil
}

func TestLargeImportVideoUsesFilePipelineAndFiveHundredMiBLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTENT_MEDIA_STORAGE", "filesystem")
	t.Setenv("TRACKING_MEDIA_DIR", root)
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	payload := videoUploadMP4(1280, 720)
	path := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	var gotMax int64
	asset, err := saveImportedMediaFile(context.Background(), svc, scope, contentimport.Job{ID: "large-video-job", Tenant: scope.Tenant, Actor: scope.Actor, Owner: "owner-a", Platform: contentimport.Channels}, 0, contentimport.RemoteMedia{ID: "channel-video", Kind: "video", URL: "https://mpvideo.qpic.cn/v.mp4"}, func(_ context.Context, _ contentimport.Platform, _ string, max int64) (contentimport.MediaFile, error) {
		gotMax = max
		return contentimport.MediaFile{Path: path, Size: int64(len(payload)), MIME: "video/mp4", SHA256: hex.EncodeToString(digest[:])}, nil
	})
	if err != nil || gotMax != 500<<20 || asset.Kind != "video" || asset.Width != 1280 || asset.Height != 720 {
		t.Fatalf("asset=%+v max=%d err=%v", asset, gotMax, err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary source was not cleaned up: %v", err)
	}
}

func TestImportVideoUsesCOSPutFileAndHeadForDedupe(t *testing.T) {
	t.Setenv("CONTENT_MEDIA_STORAGE", "cos")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	objects := &importFileObjects{data: map[string][]byte{}}
	svc.MediaObjects = objects
	payload := videoUploadMP4(640, 360)
	digest := sha256.Sum256(payload)
	fetches := 0
	job := contentimport.Job{ID: "cos-video-job", Tenant: scope.Tenant, Actor: scope.Actor, Owner: "owner-a", Platform: contentimport.Channels}
	source := contentimport.RemoteMedia{ID: "channel-video-cos", Kind: "video", URL: "https://mpvideo.qpic.cn/v.mp4"}
	fetch := func(context.Context, contentimport.Platform, string, int64) (contentimport.MediaFile, error) {
		fetches++
		path := filepath.Join(t.TempDir(), "source.mp4")
		if err := os.WriteFile(path, payload, 0600); err != nil {
			t.Fatal(err)
		}
		return contentimport.MediaFile{Path: path, Size: int64(len(payload)), MIME: "video/mp4", SHA256: hex.EncodeToString(digest[:])}, nil
	}
	first, err := saveImportedMediaFile(context.Background(), svc, scope, job, 0, source, fetch)
	if err != nil {
		t.Fatal(err)
	}
	second, err := saveImportedMediaFile(context.Background(), svc, scope, job, 0, source, fetch)
	if err != nil || first.ID != second.ID || objects.filePuts != 1 || objects.gets != 0 || fetches != 1 {
		t.Fatalf("first=%+v second=%+v puts=%d gets=%d fetches=%d err=%v", first, second, objects.filePuts, objects.gets, fetches, err)
	}
}

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

func TestImportDraftRewritesLayoutOnlyNestedMediaAndKeepsGrant(t *testing.T) {
	doc := contentimport.Document{
		Source: contentimport.Source{Title: "嵌套视频", SourceURL: "https://mp.weixin.qq.com/s/example", Media: []contentimport.RemoteMedia{{Kind: "video"}}},
		Body:   tracking.ArticleDocument{Schema: 1, LayoutHTML: `<blockquote>引用<jd-video data-media-id="remote-0000"></jd-video></blockquote>`, Blocks: []tracking.ArticleBlock{{Kind: "quote", Text: "引用"}}},
	}
	draft, err := importDraft(contentimport.Job{Owner: "one", Rights: "authorized"}, doc, []tracking.Asset{{ID: "stored-video-one", Kind: "video"}})
	if err != nil || len(draft.MediaIDs) != 1 || draft.MediaIDs[0] != "stored-video-one" || strings.Contains(draft.Body, "remote-0000") || !strings.Contains(draft.Body, "stored-video-one") {
		t.Fatalf("layout-only media not rewritten: %+v err=%v", draft, err)
	}
}

// Exercise the file pipeline past the old 50MiB boundary without allocating
// the video payload in the test process.
func TestImportStoresVideoLargerThanLegacyFiftyMiBLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTENT_MEDIA_STORAGE", "filesystem")
	t.Setenv("TRACKING_MEDIA_DIR", root)
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	sourcePath := filepath.Join(t.TempDir(), "large-import.mp4")
	file, err := os.OpenFile(sourcePath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err = file.Write(videoUploadMP4(1280, 720)); err != nil {
		t.Fatal(err)
	}
	const payload = int64(51 << 20)
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[:4], uint32(payload+8))
	copy(header[4:], "mdat")
	if _, err = file.Write(header); err != nil {
		t.Fatal(err)
	}
	size, err := file.Seek(payload-1, io.SeekCurrent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	size++
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	asset, err := saveImportedMediaFile(context.Background(), svc, scope, contentimport.Job{ID: "over-fifty", Tenant: scope.Tenant, Actor: scope.Actor, Owner: "owner-a", Platform: contentimport.Channels}, 0, contentimport.RemoteMedia{ID: "large", Kind: "video", URL: "https://mpvideo.qpic.cn/v.mp4"}, func(context.Context, contentimport.Platform, string, int64) (contentimport.MediaFile, error) {
		return contentimport.MediaFile{Path: sourcePath, Size: size, MIME: "video/mp4"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.Stat(filepath.Join(root, asset.ID))
	if err != nil || stored.Size() != size || stored.Size() <= 50<<20 {
		t.Fatalf("size=%d stored=%v err=%v", size, stored, err)
	}
}

func TestImportDedupeRejectsStoredObjectMIMEMismatch(t *testing.T) {
	t.Setenv("CONTENT_MEDIA_STORAGE", "cos")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	id := "11111111111111111111111111111111"
	asset := tracking.Asset{ID: id, Kind: "video", URL: "/api/v1/miniprogram/tracking/media/" + id, MIME: "video/mp4", Width: 640, Height: 360}
	if err := svc.AddMedia(context.Background(), scope, asset, "owner-a"); err != nil {
		t.Fatal(err)
	}
	objects := &importFileObjects{data: map[string][]byte{scope.Tenant + ":" + id: videoUploadMP4(640, 360)}, headMIME: "image/jpeg"}
	svc.MediaObjects = objects
	_, found, err := existingImportedMediaFile(context.Background(), svc, scope, id, 500<<20)
	var resolveErr *contentimport.ResolveError
	if found || !errors.As(err, &resolveErr) || resolveErr.Code != "invalid_stored_media" {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if objects.gets != 0 {
		t.Fatal("dedupe buffered the video")
	}
}
