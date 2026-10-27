package route

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

func shareWebFixture(t *testing.T) (*gin.Engine, *tracking.Service, *governanceTestStore, string) {
	t.Helper()
	t.Setenv("SHARE_WEB_ENABLED", "true")
	t.Setenv("PUBLIC_SHARE_ORIGIN", "https://www.jiadilingguang.com")
	store := &governanceTestStore{records: map[string]tracking.Record{}}
	svc := tracking.New(store, tracking.Config{
		Enabled: true, ContentEnabled: true, VideoEnabled: true, Tenant: "share-web", DefaultOwner: "owner-a", ConsentVersion: "v1",
		Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}},
	})
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }
	scope := tracking.Scope{Tenant: "share-web", Actor: 42, Owners: []string{"owner-a"}, Manage: true}
	content, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "安全文章", Body: "安全正文<script>alert(1)</script>", BodyFormat: "rich_text", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), scope, content.ID, content.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(context.Background(), scope, content.ID, content.Revision, preview["preview_token"].(string), "share-web-publish")
	if err != nil {
		t.Fatal(err)
	}
	version := published["content"].(tracking.Content).Version
	entry, err := svc.Enter(context.Background(), tracking.EntryInput{Route: "pages/contentreader/contentreader", Content: content.ID, Version: version, Client: "share-web-author-client", Consent: "v1"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateShare(context.Background(), tracking.ShareInput{Route: entry.Route, Content: entry.Content, Version: entry.Version, Channel: "app_message", Key: "share-web-entry"}, entry.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerShareWebHandlers(router, router.Group("/api/v1"), svc, tracking.NewWechatAPI())
	return router, svc, store, created["share_token"].(string)
}

func TestShareWebGETEscapesBodyAndCreatesNoTrackingRecord(t *testing.T) {
	router, _, store, code := shareWebFixture(t)
	before := len(store.records)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/share/s/"+code, nil))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "<script>alert(1)</script>") {
		t.Fatalf("GET %d %s", w.Code, w.Body.String())
	}
	if len(store.records) != before {
		t.Fatalf("GET created records: before=%d after=%d", before, len(store.records))
	}
}

func TestShareWebEntryRequiresFixedOrigin(t *testing.T) {
	router, _, store, code := shareWebFixture(t)
	body, _ := json.Marshal(map[string]any{"code": code, "client_instance": "browser-client-1234", "consent_version": "v1"})
	before := len(store.records)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/share-web/entry", strings.NewReader(string(body)))
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("X-Share-Web", "1")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || len(store.records) != before {
		t.Fatalf("cross-origin entry %d %s records=%d/%d", w.Code, w.Body.String(), len(store.records), before)
	}
}

func TestShareWebMediaRequiresTheSharedPublishedVersion(t *testing.T) {
	t.Setenv("SHARE_WEB_ENABLED", "true")
	t.Setenv("PUBLIC_SHARE_ORIGIN", "https://www.jiadilingguang.com")
	dir := t.TempDir()
	t.Setenv("TRACKING_MEDIA_DIR", dir)
	store := &governanceTestStore{records: map[string]tracking.Record{}}
	svc := tracking.New(store, tracking.Config{
		Enabled: true, ContentEnabled: true, VideoEnabled: true, Tenant: "share-media", DefaultOwner: "owner-a", ConsentVersion: "v1",
		Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}},
	})
	scope := tracking.Scope{Tenant: "share-media", Actor: 42, Owners: []string{"owner-a"}, Manage: true}
	publicID, draftID := strings.Repeat("p", 32), strings.Repeat("d", 32)
	for _, id := range []string{publicID, draftID} {
		if err := svc.AddMedia(context.Background(), scope, tracking.Asset{ID: id, Kind: "image", MIME: "image/png", Width: 2, Height: 2, URL: "/api/v1/miniprogram/tracking/media/" + id}, "owner-a"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id), []byte(id+"-bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"schema":1,"blocks":[{"kind":"image","media_id":"` + publicID + `"}]}`
	content, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "媒体文章", Body: body, BodyFormat: "article_v2", Rights: "own", Owner: "owner-a", MediaIDs: []string{publicID}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), scope, content.ID, content.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(context.Background(), scope, content.ID, content.Revision, preview["preview_token"].(string), "share-media-publish")
	if err != nil {
		t.Fatal(err)
	}
	version := published["content"].(tracking.Content).Version
	entry, err := svc.Enter(context.Background(), tracking.EntryInput{Route: trackingReaderRoute, Content: content.ID, Version: version, Client: "share-media-client", Consent: "v1"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateShare(context.Background(), tracking.ShareInput{Route: entry.Route, Content: entry.Content, Version: entry.Version, Channel: "app_message", Key: "share-media-create"}, entry.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	code := created["share_token"].(string)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerShareWebHandlers(router, router.Group("/api/v1"), svc, tracking.NewWechatAPI())

	valid := httptest.NewRecorder()
	router.ServeHTTP(valid, httptest.NewRequest(http.MethodGet, "/api/v1/share-web/media/"+code+"/"+publicID, nil))
	if valid.Code != http.StatusOK || valid.Body.String() != publicID+"-bytes" {
		t.Fatalf("published media = %d %q", valid.Code, valid.Body.String())
	}
	t.Setenv("SHARE_WEB_ENABLED", "false")
	disabled := httptest.NewRecorder()
	router.ServeHTTP(disabled, httptest.NewRequest(http.MethodGet, "/api/v1/share-web/media/"+code+"/"+publicID, nil))
	if disabled.Code != http.StatusNotFound {
		t.Fatalf("disabled share media = %d %q", disabled.Code, disabled.Body.String())
	}
	t.Setenv("SHARE_WEB_ENABLED", "true")
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/api/v1/share-web/media/"+code+"/"+draftID, nil))
	if denied.Code != http.StatusNotFound || strings.Contains(denied.Body.String(), draftID+"-bytes") {
		t.Fatalf("draft media leaked = %d %q", denied.Code, denied.Body.String())
	}
}

func TestShareWebURLLinkUsesTheSameShareAndCachesWechatResult(t *testing.T) {
	t.Setenv("TRACKING_WECHAT_URLLINK_ENABLED", "true")
	t.Setenv("TRACKING_WECHAT_ENV", "release")
	withMiniprogramCredentials(t)
	_, svc, _, code := shareWebFixture(t)
	linkCalls := 0
	var generated map[string]any
	var callsMu sync.Mutex
	wechat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			_, _ = io.WriteString(w, `{"access_token":"share-web-token"}`)
		case "/wxa/generate_urllink":
			time.Sleep(50 * time.Millisecond)
			callsMu.Lock()
			linkCalls++
			_ = json.NewDecoder(r.Body).Decode(&generated)
			callsMu.Unlock()
			_, _ = io.WriteString(w, `{"errcode":0,"url_link":"https://wxaurl.cn/share-web"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer wechat.Close()
	api := tracking.NewWechatAPI()
	api.Base = wechat.URL
	router := gin.New()
	registerShareWebHandlers(router, router.Group("/api/v1"), svc, api)
	body, _ := json.Marshal(map[string]string{"code": code})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/share-web/open-miniprogram", strings.NewReader(string(body)))
			req.Header.Set("Origin", "https://www.jiadilingguang.com")
			req.Header.Set("X-Share-Web", "1")
			router.ServeHTTP(w, req)
			responses <- w
		}()
	}
	for i := 0; i < 2; i++ {
		w := <-responses
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "https://wxaurl.cn/share-web") {
			t.Fatalf("request %d = %d %s", i, w.Code, w.Body.String())
		}
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	query, err := url.ParseQuery(generated["query"].(string))
	if err != nil || generated["path"] != trackingReaderRoute || query.Get("st") != code || linkCalls != 1 {
		t.Fatalf("generated=%v query=%v err=%v calls=%d", generated, query, err, linkCalls)
	}
}

func TestShareURLLinkSingleflightDoesNotBlockDifferentShares(t *testing.T) {
	cache := newShareURLLinkCache(10)
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})
	go func() {
		_, _ = cache.load(context.Background(), "share-a", now, func() (tracking.URLLinkResult, error) {
			close(started)
			<-release
			return tracking.URLLinkResult{URL: "https://wxaurl.cn/a", Expires: now.Add(time.Hour)}, nil
		})
		close(firstDone)
	}()
	<-started
	secondDone := make(chan struct{})
	go func() {
		_, _ = cache.load(context.Background(), "share-b", now, func() (tracking.URLLinkResult, error) {
			return tracking.URLLinkResult{URL: "https://wxaurl.cn/b", Expires: now.Add(time.Hour)}, nil
		})
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("an in-flight share blocked an unrelated share")
	}
	close(release)
	<-firstDone
}
