package route

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/contentimport"
	"service/contentstorage"
	"service/tracking"
)

type testPrivateObjects struct {
	data       map[string][]byte
	mime       map[string]string
	puts, gets int
	putErr     error
}

func (s *testPrivateObjects) Put(_ context.Context, tenant, id string, data []byte, mime string) error {
	s.puts++
	if s.putErr != nil {
		return s.putErr
	}
	s.data[tenant+":"+id] = append([]byte{}, data...)
	s.mime[tenant+":"+id] = mime
	return nil
}
func (s *testPrivateObjects) Get(_ context.Context, tenant, id string, max int64) ([]byte, string, error) {
	s.gets++
	data, ok := s.data[tenant+":"+id]
	if !ok {
		return nil, "", contentstorage.ErrNotFound
	}
	if int64(len(data)) > max {
		return nil, "", contentstorage.ErrIntegrity
	}
	return append([]byte{}, data...), s.mime[tenant+":"+id], nil
}
func (s *testPrivateObjects) Head(_ context.Context, tenant, id string) (contentstorage.Metadata, error) {
	data, ok := s.data[tenant+":"+id]
	if !ok {
		return contentstorage.Metadata{}, contentstorage.ErrNotFound
	}
	return contentstorage.Metadata{Size: int64(len(data)), MIME: s.mime[tenant+":"+id]}, nil
}
func (s *testPrivateObjects) Open(_ context.Context, tenant, id, rangeHeader string) (*contentstorage.Stream, error) {
	data, ok := s.data[tenant+":"+id]
	if !ok {
		return nil, contentstorage.ErrNotFound
	}
	w := newMediaDeadlineRecorder()
	req := httptest.NewRequest("GET", "/object", nil)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	http.ServeContent(w, req, id, time.Time{}, bytes.NewReader(data))
	if w.Code == 416 {
		return nil, &contentstorage.RangeError{Size: int64(len(data))}
	}
	length, _ := strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64)
	return &contentstorage.Stream{Body: io.NopCloser(bytes.NewReader(w.Body.Bytes())), Status: w.Code, Size: int64(len(data)), Length: length, MIME: s.mime[tenant+":"+id], ContentRange: w.Header().Get("Content-Range")}, nil
}
func TestCOSImportAndPrivateProxyWithoutFilesystem(t *testing.T) {
	t.Setenv("CONTENT_MEDIA_STORAGE", "cos")
	t.Setenv("TRACKING_MEDIA_DIR", "")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	objects := &testPrivateObjects{data: map[string][]byte{}, mime: map[string]string{}}
	svc.MediaObjects = objects
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	fetches := 0
	fetch := func(context.Context, contentimport.Platform, string, int64) ([]byte, string, error) {
		fetches++
		return picture.Bytes(), "image/png", nil
	}
	job := contentimport.Job{ID: "job-cos-one", Tenant: "workbench", Actor: 11, Owner: "owner-a", Platform: contentimport.WechatArticle}
	source := contentimport.RemoteMedia{ID: "source-picture-one", Kind: "image", URL: "https://mmbiz.qpic.cn/picture.jpg"}
	asset, err := saveImportedMedia(context.Background(), svc, scope, job, 0, source, fetch)
	if err != nil || objects.puts != 1 {
		t.Fatal(asset, err, objects.puts)
	}
	if _, err := saveImportedMedia(context.Background(), svc, scope, job, 0, source, fetch); err != nil || fetches != 1 {
		t.Fatal("resume", err, fetches)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/media/:id", func(c *gin.Context) { serveTrackingMedia(c, svc, &scope) })
	r.GET("/public/:id", func(c *gin.Context) { serveTrackingMedia(c, svc, nil) })
	w := newMediaDeadlineRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/media/"+asset.ID, nil))
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), picture.Bytes()) || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	before := objects.gets
	w = newMediaDeadlineRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/public/"+asset.ID, nil))
	if w.Code < 400 || objects.gets != before {
		t.Fatal("private object read before grant", w.Code)
	}
	scope.Owners = []string{"different-owner"}
	w = newMediaDeadlineRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/media/"+asset.ID, nil))
	if w.Code < 400 || objects.gets != before {
		t.Fatal("revoked grant read object", w.Code)
	}
}
func TestPublicCOSCannotBecomeReadyMedia(t *testing.T) {
	t.Setenv("CONTENT_MEDIA_STORAGE", "cos")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	objects := &testPrivateObjects{data: map[string][]byte{}, mime: map[string]string{}, putErr: contentstorage.ErrPublicObject}
	svc.MediaObjects = objects
	var picture bytes.Buffer
	_ = png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 3)))
	_, err := saveImportedMedia(context.Background(), svc, scope, contentimport.Job{ID: "job-cos", Tenant: scope.Tenant, Actor: scope.Actor, Owner: "owner-a", Platform: contentimport.WechatArticle}, 0, contentimport.RemoteMedia{ID: "source-one", Kind: "image", URL: "https://mmbiz.qpic.cn/picture"}, func(context.Context, contentimport.Platform, string, int64) ([]byte, string, error) {
		return picture.Bytes(), "image/png", nil
	})
	var resolved *contentimport.ResolveError
	if !errors.As(err, &resolved) || resolved.Code != "private_storage_required" {
		t.Fatal(err)
	}
	media, e := svc.ListMedia(context.Background(), scope)
	if e != nil {
		t.Fatal(e)
	}
	if items, ok := media.Items.([]tracking.Asset); !ok || len(items) != 0 {
		t.Fatal("public storage became an authorized asset", media)
	}
}
func TestCOSProxyNeverUsesSameIDLocalFileAndStreamsRanges(t *testing.T) {
	t.Setenv("CONTENT_MEDIA_STORAGE", "cos")
	root := t.TempDir()
	t.Setenv("TRACKING_MEDIA_DIR", root)
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	objects := &testPrivateObjects{data: map[string][]byte{}, mime: map[string]string{}}
	svc.MediaObjects = objects
	id := strings.Repeat("a", 32)
	payload := []byte("0123456789")
	objects.data[scope.Tenant+":"+id] = payload
	objects.mime[scope.Tenant+":"+id] = "video/mp4"
	if err := os.WriteFile(filepath.Join(root, id), []byte("LOCAL-MUST-NOT-WIN"), 0600); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.GET("/media", func(c *gin.Context) {
		serveContentMediaBytes(c, svc, tracking.Asset{ID: id, Kind: "video", MIME: "video/mp4"})
	})
	request := httptest.NewRequest("GET", "/media", nil)
	request.Header.Set("Range", "bytes=2-5")
	w := newMediaDeadlineRecorder()
	r.ServeHTTP(w, request)
	if w.Code != 206 || w.Body.String() != "2345" || w.Header().Get("Content-Range") != "bytes 2-5/10" || objects.gets != 0 {
		t.Fatal(w.Code, w.Body.String(), w.Header(), objects.gets)
	}
	request = httptest.NewRequest("GET", "/media", nil)
	request.Header.Set("Range", "bytes=10-")
	w = newMediaDeadlineRecorder()
	r.ServeHTTP(w, request)
	if w.Code != 416 || w.Header().Get("Content-Range") != "bytes */10" {
		t.Fatal(w.Code, w.Header())
	}
}

func TestCOSConfigurationFailureNeverFallsBackToFilesystem(t *testing.T) {
	t.Setenv("CONTENT_MEDIA_STORAGE", "cos")
	root := t.TempDir()
	t.Setenv("TRACKING_MEDIA_DIR", root)
	id := strings.Repeat("f", 32)
	localPath := filepath.Join(root, id)
	if err := os.WriteFile(localPath, []byte("legacy-local-object"), 0600); err != nil {
		t.Fatal(err)
	}
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)

	if data, found, err := readContentMedia(context.Background(), svc, id, 10<<20); data != nil || found || !errors.Is(err, contentstorage.ErrUnavailable) {
		t.Fatal("COS read used filesystem fallback", data, found, err)
	}
	if err := storeContentMedia(context.Background(), svc, strings.Repeat("w", 32), []byte("new-object"), "image/png"); !errors.Is(err, contentstorage.ErrUnavailable) {
		t.Fatal("COS write used filesystem fallback", err)
	}
	if _, err := os.Stat(filepath.Join(root, strings.Repeat("w", 32))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("COS write created a local object", err)
	}

	r := gin.New()
	r.GET("/media", func(c *gin.Context) {
		serveContentMediaBytes(c, svc, tracking.Asset{ID: id, Kind: "image", MIME: "image/png"})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/media", nil))
	if w.Code != http.StatusServiceUnavailable || w.Body.String() == "legacy-local-object" {
		t.Fatal("COS proxy used filesystem fallback", w.Code, w.Body.String())
	}

	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	if err := writer.WriteField("owner_id", "owner-a"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("rights", "own"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "picture.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(imageBytes.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	uploadRouter := gin.New()
	uploadRouter.POST("/media", func(c *gin.Context) { uploadTrackingMedia(c, svc, scope) })
	request := httptest.NewRequest(http.MethodPost, "/media", &upload)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	w = httptest.NewRecorder()
	uploadRouter.ServeHTTP(w, request)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("COS upload used filesystem fallback", w.Code, w.Body.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != id {
		t.Fatal("COS upload changed local storage", entries, err)
	}
}
func TestXHSPictureCacheChangesWhenCDNObjectPathChanges(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	t.Setenv("TRACKING_MEDIA_DIR", root)
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	var picture bytes.Buffer
	_ = png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 3)))
	fetches := 0
	fetch := func(context.Context, contentimport.Platform, string, int64) ([]byte, string, error) {
		fetches++
		return picture.Bytes(), "image/png", nil
	}
	job := contentimport.Job{ID: "xhs-cache-job", Tenant: scope.Tenant, Actor: scope.Actor, Owner: "owner-a", Platform: contentimport.Xiaohongshu}
	source := contentimport.RemoteMedia{ID: "note-first-image", Kind: "image", URL: "https://a.xhscdn.com/old-object.jpg?sign=first"}
	first, err := saveImportedMedia(context.Background(), svc, scope, job, 0, source, fetch)
	if err != nil {
		t.Fatal(err)
	}
	source.URL = "https://a.xhscdn.com/new-object.jpg?sign=second"
	second, err := saveImportedMedia(context.Background(), svc, scope, job, 0, source, fetch)
	if err != nil || first.ID == second.ID || fetches != 2 {
		t.Fatal(first, second, err, fetches)
	}
	source.URL = "https://a.xhscdn.com/new-object.jpg?sign=third"
	again, err := saveImportedMedia(context.Background(), svc, scope, job, 0, source, fetch)
	if err != nil || again.ID != second.ID || fetches != 2 {
		t.Fatal(again, err, fetches)
	}
}
func TestGalleryImportsDecodedWebPWithoutFormatConversion(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACKING_MEDIA_DIR", root)
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	data, err := base64.StdEncoding.DecodeString("UklGRh4AAABXRUJQVlA4TBEAAAAvAAAAAAfQ//73v/+BiOh/AAA=")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := saveImportedMedia(context.Background(), svc, scope, contentimport.Job{ID: "webp-import", Tenant: scope.Tenant, Actor: scope.Actor, Owner: "owner-a", Platform: contentimport.Xiaohongshu}, 0, contentimport.RemoteMedia{ID: "webp-picture", Kind: "image", URL: "https://a.xhscdn.com/image.webp"}, func(context.Context, contentimport.Platform, string, int64) ([]byte, string, error) {
		return data, "image/webp", nil
	})
	if err != nil || asset.MIME != "image/webp" || asset.Width != 1 || asset.Height != 1 {
		t.Fatal(asset, err)
	}
}

type mediaDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines            []time.Time
	failWrite            bool
	wroteWithoutDeadline bool
}

func (r *mediaDeadlineRecorder) SetWriteDeadline(value time.Time) error {
	r.deadlines = append(r.deadlines, value)
	return nil
}
func (r *mediaDeadlineRecorder) Write(data []byte) (int, error) {
	if r.failWrite {
		if len(r.deadlines) == 0 || r.deadlines[len(r.deadlines)-1].IsZero() {
			r.wroteWithoutDeadline = true
		}
		return 0, os.ErrDeadlineExceeded
	}
	return r.ResponseRecorder.Write(data)
}
func newMediaDeadlineRecorder() *mediaDeadlineRecorder {
	return &mediaDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
}
func TestSlowCOSClientHasFiniteDeadlineAndReleasesStreamSlot(t *testing.T) {
	t.Setenv("CONTENT_MEDIA_STORAGE", "cos")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	id := strings.Repeat("z", 32)
	objects := &testPrivateObjects{data: map[string][]byte{scope.Tenant + ":" + id: []byte("stream-data")}, mime: map[string]string{scope.Tenant + ":" + id: "video/mp4"}}
	svc.MediaObjects = objects
	r := gin.New()
	r.GET("/media", func(c *gin.Context) {
		serveContentMediaBytes(c, svc, tracking.Asset{ID: id, Kind: "video", MIME: "video/mp4"})
	})
	before := len(contentMediaReadSlots)
	w := newMediaDeadlineRecorder()
	w.failWrite = true
	r.ServeHTTP(w, httptest.NewRequest("GET", "/media", nil))
	if len(w.deadlines) == 0 || w.deadlines[0].IsZero() || time.Until(w.deadlines[0]) > 31*time.Second || w.wroteWithoutDeadline || len(contentMediaReadSlots) != before {
		t.Fatal("unbounded downstream", w.deadlines, w.wroteWithoutDeadline, len(contentMediaReadSlots))
	}
	if w.deadlines[len(w.deadlines)-1].IsZero() {
		t.Fatal("cleared failed-write deadline before server abort")
	}
}
func TestWorkbenchMediaHEADUsesSameGrantAsGET(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0700)
	t.Setenv("TRACKING_MEDIA_DIR", root)
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	r, svc := newWorkbenchHandlerFixture(t, &scope)
	id := strings.Repeat("h", 32)
	if err := svc.AddMedia(context.Background(), scope, tracking.Asset{ID: id, Kind: "image", MIME: "image/png", Width: 2, Height: 3, URL: "/api/v1/miniprogram/tracking/media/" + id}, "owner-a"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, id), []byte("fixture-image"), 0600); err != nil {
		t.Fatal(err)
	}
	w := newMediaDeadlineRecorder()
	r.ServeHTTP(w, httptest.NewRequest("HEAD", "/workbench/media/"+id, nil))
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	scope.Owners = []string{"revoked-owner"}
	w = newMediaDeadlineRecorder()
	r.ServeHTTP(w, httptest.NewRequest("HEAD", "/workbench/media/"+id, nil))
	if w.Code != 404 {
		t.Fatal("HEAD bypassed grant", w.Code)
	}
}
