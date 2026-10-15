package route

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

type urlLinkWorld struct {
	t         *testing.T
	svc       *tracking.Service
	scope     tracking.Scope
	api       *tracking.WechatAPI
	dir       string
	now       time.Time
	contentID string
	tokens    int
	links     []map[string]any
	linkOut   string
}

func newURLLinkWorld(t *testing.T) *urlLinkWorld {
	t.Helper()
	w := &urlLinkWorld{t: t, now: time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC), dir: t.TempDir(), linkOut: `{"errcode":0,"url_link":"https://wxaurl.cn/link-1"}`}
	t.Setenv("TRACKING_MEDIA_DIR", w.dir)
	t.Setenv("TRACKING_WECHAT_URLLINK_ENABLED", "true")
	t.Setenv("TRACKING_WECHAT_ENV", "release")
	wechat := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			w.tokens++
			io.WriteString(rw, `{"access_token":"tok"}`)
		case "/wxa/generate_urllink":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.links = append(w.links, body)
			io.WriteString(rw, w.linkOut)
		default:
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(wechat.Close)
	w.api = tracking.NewWechatAPI()
	w.api.Base = wechat.URL
	cfg := tracking.Config{Enabled: true, ContentEnabled: true, Tenant: "tenant-a", DefaultOwner: "owner-a", ConsentVersion: "v1", Routes: map[string]tracking.Route{trackingReaderRoute: {Title: "文章", Public: true}}}
	w.svc = tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, cfg)
	w.svc.Now = func() time.Time { return w.now }
	w.scope = tracking.Scope{Tenant: "tenant-a", Actor: 7, Owners: []string{"owner-a"}, View: true, Manage: true}
	ctx := context.Background()
	saved, err := w.svc.SaveContent(ctx, w.scope, "", tracking.Draft{Title: "测试文章", Body: "正文内容", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := w.svc.Preview(ctx, w.scope, saved.ID, saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.svc.Publish(ctx, w.scope, saved.ID, saved.Revision, preview["preview_token"].(string), "publish-key-0001"); err != nil {
		t.Fatal(err)
	}
	w.contentID = saved.ID
	return w
}

func (w *urlLinkWorld) link(scope tracking.Scope, version int) (tracking.URLLinkResult, error) {
	return trackingURLLink(context.Background(), w.svc, w.api, wechatCredentials{AppID: "app-id", Secret: "app-secret"}, scope, w.contentID, version)
}

func TestURLLinkOpensThePublishedVersionWithAValidShareToken(t *testing.T) {
	w := newURLLinkWorld(t)
	got, err := w.link(w.scope, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://wxaurl.cn/link-1" || !got.Expires.Equal(w.now.Add(29*24*time.Hour)) {
		t.Fatalf("result: %+v", got)
	}
	body := w.links[0]
	if body["path"] != trackingReaderRoute || body["env_version"] != "release" {
		t.Fatalf("request: %v", body)
	}
	query, err := url.ParseQuery(body["query"].(string))
	if err != nil || query.Get("content_id") != w.contentID || query.Get("content_version") != "1" {
		t.Fatalf("query: %v %v", body["query"], err)
	}
	// The token in the link resolves to exactly this content and version, so a
	// visit through the link is attributed to a real, unexpired share.
	entry, err := w.svc.ResolveShare(context.Background(), query.Get("st"))
	if err != nil || entry["route_key"] != trackingReaderRoute || entry["content_id"] != w.contentID || entry["content_version"] != 1 {
		t.Fatalf("share token does not open this content: %v %v", entry, err)
	}
}

func TestURLLinkIsCachedPerDayAndRegeneratedWhenStaleOrNewDay(t *testing.T) {
	w := newURLLinkWorld(t)
	first, err := w.link(w.scope, 1)
	if err != nil {
		t.Fatal(err)
	}
	w.linkOut = `{"errcode":0,"url_link":"https://wxaurl.cn/link-2"}`
	again, err := w.link(w.scope, 1)
	if err != nil || again.URL != first.URL || len(w.links) != 1 {
		t.Fatalf("same-day request minted another link: %+v err=%v requests=%d", again, err, len(w.links))
	}
	info, err := os.Stat(filepath.Join(w.dir, "urllink-"+trackingCodeCacheKey("tenant-a", 7, w.contentID, 1, "release/urllink", w.now)))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cache entry: %v %v", info, err)
	}
	w.now = w.now.Add(24 * time.Hour)
	next, err := w.link(w.scope, 1)
	if err != nil || next.URL != "https://wxaurl.cn/link-2" || len(w.links) != 2 {
		t.Fatalf("a new day must mint a new link: %+v err=%v", next, err)
	}
	entries, _ := os.ReadDir(w.dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestURLLinkRefusesWithoutPermissionPublishedVersionOrFlag(t *testing.T) {
	w := newURLLinkWorld(t)
	status := func(err error) int {
		var known *tracking.Error
		if errors.As(err, &known) {
			return known.Status
		}
		return 0
	}
	viewOnly := w.scope
	viewOnly.Manage = false
	if _, err := w.link(viewOnly, 1); status(err) != 403 {
		t.Fatalf("view-only account got a link: %v", err)
	}
	other := w.scope
	other.Owners = []string{"owner-b"}
	if _, err := w.link(other, 1); status(err) != 404 {
		t.Fatalf("another owner's content was reachable: %v", err)
	}
	if _, err := w.link(w.scope, 2); err == nil {
		t.Fatal("an unpublished version got a link")
	}
	t.Setenv("TRACKING_WECHAT_URLLINK_ENABLED", "")
	if _, err := w.link(w.scope, 1); status(err) != 403 {
		t.Fatalf("a disabled feature produced a link: %v", err)
	}
	if len(w.links) != 0 || w.tokens != 0 {
		t.Fatalf("refused requests reached WeChat (%d link, %d token)", len(w.links), w.tokens)
	}
}

func TestURLLinkFailureIsReportedSafelyAndNotCached(t *testing.T) {
	w := newURLLinkWorld(t)
	w.linkOut = `{"errcode":85079,"errmsg":"miniprogram has no online release app-secret"}`
	_, err := w.link(w.scope, 1)
	var wechat *tracking.WechatError
	if !errors.As(err, &wechat) || wechat.Code != 85079 {
		t.Fatalf("want typed WeChat error, got %v", err)
	}
	message := trackingURLLinkProblem(err)
	if !strings.Contains(message, "85079") || strings.Contains(message, "app-secret") {
		t.Fatalf("operator message: %q", message)
	}
	entries, _ := os.ReadDir(w.dir)
	if len(entries) != 0 {
		t.Fatalf("a failed generation left files: %v", entries)
	}
	w.linkOut = `{"errcode":0,"url_link":"https://wxaurl.cn/after-retry"}`
	if got, err := w.link(w.scope, 1); err != nil || got.URL != "https://wxaurl.cn/after-retry" {
		t.Fatalf("retry after failure: %+v %v", got, err)
	}
}

func TestShareMethodsAfterPublishNeverFailThePublish(t *testing.T) {
	w := newURLLinkWorld(t)
	content := tracking.Content{ID: w.contentID, Version: 1}
	existing := map[string]any{"route_key": trackingReaderRoute, "content_id": w.contentID}
	// The helper reads the configured mini-program credentials; the test process
	// has none, so generation fails. The publish result must still come back.
	methods := trackingShareMethods(context.Background(), w.svc, w.api, w.scope, content, existing)
	if methods["route_key"] != trackingReaderRoute || methods["content_id"] != w.contentID {
		t.Fatalf("existing publish fields were dropped: %v", methods)
	}
	if methods["url_link"] != nil || methods["message"] != "URL Link生成失败，请稍后重试" {
		t.Fatalf("a failed generation must be reported as a message only: %v", methods)
	}
	t.Setenv("TRACKING_WECHAT_URLLINK_ENABLED", "")
	if methods = trackingShareMethods(context.Background(), w.svc, w.api, w.scope, content, nil); methods["url_link"] != nil || methods["message"] == "" {
		t.Fatalf("disabled feature offered a link: %v", methods)
	}
}

func TestWriteFileAtomicallyReplacesInPlace(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "entry")
	if err := writeFileAtomically(name, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomically(name, []byte("two")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(name); string(got) != "two" {
		t.Fatalf("content %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("stray files: %v", entries)
	}
}

func TestURLLinkNearExpiryCacheEntryIsNotServed(t *testing.T) {
	w := newURLLinkWorld(t)
	name := filepath.Join(w.dir, "urllink-"+trackingCodeCacheKey("tenant-a", 7, w.contentID, 1, "release/urllink", w.now))
	stale, _ := json.Marshal(tracking.URLLinkResult{URL: "https://wxaurl.cn/stale", Expires: w.now.Add(12 * time.Hour)})
	if err := os.WriteFile(name, stale, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := w.link(w.scope, 1)
	if err != nil || got.URL != "https://wxaurl.cn/link-1" || len(w.links) != 1 {
		t.Fatalf("a link about to expire was handed out: %+v err=%v", got, err)
	}
	// A corrupt cache file is ignored, not trusted and not fatal.
	if err = os.WriteFile(name, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err = w.link(w.scope, 1); err != nil || got.URL == "" {
		t.Fatalf("corrupt cache: %+v %v", got, err)
	}
}

// urlLinkRoutes serves only the url-link routes, with the management scope taken
// from the X-Test-Actor header instead of a login session.
func urlLinkRoutes(t *testing.T) *gin.Engine {
	t.Helper()
	t.Setenv("TRACKING_WECHAT_URLLINK_ENABLED", "")
	gin.SetMode(gin.TestMode)
	registered := gin.New()
	RegisterTrackingRoutes(registered.Group("/api/v1"), &model.UserModel{}, nil)
	router := gin.New()
	for _, route := range registered.Routes() {
		if strings.HasSuffix(route.Path, "/url-link") {
			router.Handle(route.Method, route.Path, func(c *gin.Context) {
				actor, _ := strconv.ParseInt(c.GetHeader("X-Test-Actor"), 10, 64)
				c.Set("tracking.scope", tracking.Scope{Manage: true, Actor: actor})
				c.Next()
			}, route.HandlerFunc)
		}
	}
	return router
}

func urlLinkRequest(router *gin.Engine, method string, actor int) int {
	request := httptest.NewRequest(method, "/api/v1/management/tracking/contents/content-1/url-link?version=1", nil)
	request.Header.Set("X-Test-Actor", strconv.Itoa(actor))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response.Code
}

// Generating a link creates a share and can spend WeChat quota, so it must not be
// reachable by a GET that a prefetcher, image tag or link scanner can issue. The
// management group applies its origin check to every non-GET method, so a POST
// route inherits it.
func TestURLLinkRouteIsAPostMutationNotAGet(t *testing.T) {
	router := urlLinkRoutes(t)
	if code := urlLinkRequest(router, "GET", 7); code != http.StatusNotFound {
		t.Fatalf("GET must no longer generate a link, got %d", code)
	}
	if code := urlLinkRequest(router, "POST", 7); code != http.StatusForbidden {
		t.Fatalf("POST must reach the handler (disabled here, so refused with 403), got %d", code)
	}
}

func TestURLLinkRouteLimitsEachAccountPerMinute(t *testing.T) {
	router := urlLinkRoutes(t)
	for i := 1; i <= trackingURLLinkPerMinute; i++ {
		if code := urlLinkRequest(router, "POST", 7); code != http.StatusForbidden {
			t.Fatalf("request %d is inside the limit and must reach the handler, got %d", i, code)
		}
	}
	if code := urlLinkRequest(router, "POST", 7); code != http.StatusTooManyRequests {
		t.Fatalf("the request over the limit must be refused with 429, got %d", code)
	}
	if code := urlLinkRequest(router, "POST", 8); code != http.StatusForbidden {
		t.Fatalf("another account has its own allowance, got %d", code)
	}
}

// The mini-program's access_token is cached for payment, profile and QR-code
// calls. Tracking must take that app's token from the same cache.
func TestTrackingWechatAPISharesTheMiniprogramTokenCache(t *testing.T) {
	api := newTrackingWechatAPI(wechatCredentials{AppID: "wx-mini", Secret: "secret"})
	if api.SharedTokens["wx-mini"] == nil || len(api.SharedTokens) != 1 {
		t.Fatalf("the mini-program app must use the shared token source: %v", api.SharedTokens)
	}
	if bare := newTrackingWechatAPI(wechatCredentials{}); len(bare.SharedTokens) != 0 {
		t.Fatalf("no mini-program app configured, nothing to share: %v", bare.SharedTokens)
	}
}
