package route

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"

	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func validMPEnv() map[string]string {
	return map[string]string{"TRACKING_MP_SYNC_ENABLED": "true", "TRACKING_MP_APPID": "wx-official", "TRACKING_MP_SECRET": "official-secret", "TRACKING_MP_OWNER": "owner-a", "TRACKING_MP_ACTOR": "7", "TRACKING_MP_RIGHTS": "own"}
}

func TestMPSyncSettingsDefaultOffAndRejectPartialConfiguration(t *testing.T) {
	if s := loadMPSyncSettings(envFrom(nil)); s.Enabled || len(s.Problems) != 0 {
		t.Fatalf("must default off: %+v", s)
	}
	if s := loadMPSyncSettings(envFrom(map[string]string{"TRACKING_MP_APPID": "x", "TRACKING_MP_SECRET": "y"})); s.Enabled {
		t.Fatal("credentials alone must not enable the sync")
	}
	good := loadMPSyncSettings(envFrom(validMPEnv()))
	if !good.Enabled || good.Config.Owner != "owner-a" || good.Config.Rights != "own" || good.Config.MaxArticles != 30 || good.Config.ImportImages || good.Interval != 0 || good.Actor != 7 {
		t.Fatalf("defaults: %+v", good)
	}
	for name, change := range map[string]map[string]string{
		"no app id":     {"TRACKING_MP_APPID": ""},
		"no secret":     {"TRACKING_MP_SECRET": ""},
		"no owner":      {"TRACKING_MP_OWNER": " "},
		"no actor":      {"TRACKING_MP_ACTOR": ""},
		"zero actor":    {"TRACKING_MP_ACTOR": "0"},
		"bad actor":     {"TRACKING_MP_ACTOR": "admin"},
		"no rights":     {"TRACKING_MP_RIGHTS": ""},
		"bad rights":    {"TRACKING_MP_RIGHTS": "public"},
		"too many":      {"TRACKING_MP_MAX_ARTICLES": "5000"},
		"zero articles": {"TRACKING_MP_MAX_ARTICLES": "0"},
		"fast interval": {"TRACKING_MP_SYNC_INTERVAL_MINUTES": "1"},
		"slow interval": {"TRACKING_MP_SYNC_INTERVAL_MINUTES": "100000"},
		"bad interval":  {"TRACKING_MP_SYNC_INTERVAL_MINUTES": "hourly"},
	} {
		env := validMPEnv()
		for k, v := range change {
			env[k] = v
		}
		s := loadMPSyncSettings(envFrom(env))
		if s.Enabled || len(s.Problems) == 0 {
			t.Errorf("%s: enabled with %+v", name, s)
		}
		for _, p := range s.Problems {
			if strings.Contains(p, "official-secret") {
				t.Errorf("%s: a problem message contains the secret", name)
			}
		}
	}
	// An article's rights declaration is never assumed: leaving it unset keeps the
	// sync off and tells the operator which variable to set.
	missing := validMPEnv()
	delete(missing, "TRACKING_MP_RIGHTS")
	if s := loadMPSyncSettings(envFrom(missing)); s.Enabled || len(s.Problems) != 1 || !strings.Contains(s.Problems[0], "TRACKING_MP_RIGHTS") || s.Config.Rights != "" {
		t.Fatalf("an unset rights declaration must keep the sync off and name the variable: %+v", s)
	}
	env := validMPEnv()
	env["TRACKING_MP_SYNC_INTERVAL_MINUTES"], env["TRACKING_MP_IMPORT_IMAGES"], env["TRACKING_MP_RIGHTS"], env["TRACKING_MP_MAX_ARTICLES"] = "60", "true", "authorized", "12"
	if s := loadMPSyncSettings(envFrom(env)); !s.Enabled || s.Interval != time.Hour || !s.Config.ImportImages || s.Config.Rights != "authorized" || s.Config.MaxArticles != 12 {
		t.Fatalf("explicit settings: %+v", s)
	}
}

type routeFakeSource struct {
	mu       sync.Mutex
	articles []tracking.MPArticle
	err      error
	block    chan struct{}
	calls    int
}

func (f *routeFakeSource) Articles(ctx context.Context, limit int) ([]tracking.MPArticle, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.articles, f.err
}

type mpWorld struct {
	svc    *tracking.Service
	scope  tracking.Scope
	source *routeFakeSource
	runner *mpSyncRunner
	router *gin.Engine
}

func newMPWorld(t *testing.T, callerScope tracking.Scope) *mpWorld {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := &mpWorld{source: &routeFakeSource{articles: []tracking.MPArticle{{ID: "pub-1#0", Title: "测试文章", HTML: "<p>正文</p>", URL: "https://mp.weixin.qq.com/s/a1"}}}}
	cfg := tracking.Config{Enabled: true, ContentEnabled: true, Tenant: "tenant-a", DefaultOwner: "owner-a", ConsentVersion: "v1"}
	w.svc = tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, cfg)
	w.svc.Now = func() time.Time { return time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC) }
	w.scope = tracking.Scope{Tenant: "tenant-a", Actor: 7, Owners: []string{"owner-a"}, View: true, Manage: true}
	settings := loadMPSyncSettings(envFrom(validMPEnv()))
	w.runner = &mpSyncRunner{svc: w.svc, settings: settings, source: w.source, scopeFor: func(context.Context) (tracking.Scope, error) { return w.scope, nil }}
	w.router = gin.New()
	group := w.router.Group("/")
	group.Use(func(c *gin.Context) { c.Set("tracking.scope", callerScope); c.Next() })
	registerMPSyncRoutes(group, w.svc, w.runner, settings, func(c *gin.Context) tracking.Scope { return c.MustGet("tracking.scope").(tracking.Scope) })
	return w
}

func (w *mpWorld) call(method string) (int, map[string]any) {
	rec := httptest.NewRecorder()
	w.router.ServeHTTP(rec, httptest.NewRequest(method, "/mp-sync", nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestMPSyncEndpointsRunStatusAndPermissions(t *testing.T) {
	manager := tracking.Scope{Tenant: "tenant-a", Actor: 9, Owners: []string{"owner-a"}, View: true, Manage: true}
	w := newMPWorld(t, manager)
	if code, body := w.call("GET"); code != 200 || body["data"].(map[string]any)["enabled"] != true || body["data"].(map[string]any)["publishes"] != false || body["data"].(map[string]any)["last_run"] != nil {
		t.Fatalf("status before any run: %d %v", code, body)
	}
	code, body := w.call("POST")
	counts := body["data"].(map[string]any)["counts"].(map[string]any)
	if code != 200 || counts["created"] != float64(1) {
		t.Fatalf("run: %d %v", code, body)
	}
	if code, body = w.call("GET"); code != 200 || body["data"].(map[string]any)["last_run"].(map[string]any)["ok"] != true {
		t.Fatalf("status after the run: %d %v", code, body)
	}
	for name, scope := range map[string]tracking.Scope{
		"view only":      {Tenant: "tenant-a", Actor: 9, Owners: []string{"owner-a"}, View: true},
		"another owner":  {Tenant: "tenant-a", Actor: 9, Owners: []string{"owner-b"}, View: true, Manage: true},
		"no permissions": {Tenant: "tenant-a", Actor: 9, Owners: []string{}},
	} {
		w2 := newMPWorld(t, scope)
		if code, _ := w2.call("POST"); code != 403 {
			t.Errorf("%s: trigger got %d", name, code)
		}
		if w2.source.calls != 0 {
			t.Errorf("%s: a refused request reached WeChat", name)
		}
	}
	if code, body := newMPWorld(t, tracking.Scope{Tenant: "tenant-a", Actor: 9, Owners: []string{"owner-b"}, View: true, Manage: true}).call("GET"); code != 200 || body["data"].(map[string]any)["enabled"] != false {
		t.Fatalf("another owner's manager must not learn the configuration: %d %v", code, body)
	}
}

func TestMPSyncManualRunsAreRateLimitedAndSingleFlight(t *testing.T) {
	manager := tracking.Scope{Tenant: "tenant-a", Actor: 9, Owners: []string{"owner-a"}, View: true, Manage: true}
	w := newMPWorld(t, manager)
	if code, _ := w.call("POST"); code != 200 {
		t.Fatal(code)
	}
	if code, _ := w.call("POST"); code != 429 {
		t.Fatalf("a second manual run within the gap got %d", code)
	}
	// The scheduler is not subject to the manual gap but never overlaps a run.
	w2 := newMPWorld(t, manager)
	w2.source.block = make(chan struct{})
	started := make(chan struct{})
	go func() {
		close(started)
		_, _ = w2.runner.Run(context.Background(), false)
	}()
	<-started
	deadline := time.Now().Add(2 * time.Second)
	for {
		w2.source.mu.Lock()
		calls := w2.source.calls
		w2.source.mu.Unlock()
		if calls == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first run never reached the source")
		}
		time.Sleep(time.Millisecond)
	}
	if code, _ := w2.call("POST"); code != 409 {
		t.Fatalf("an overlapping run got %d", code)
	}
	close(w2.source.block)
}

func TestMPSyncUpstreamFailureIsSafeForTheOperator(t *testing.T) {
	manager := tracking.Scope{Tenant: "tenant-a", Actor: 9, Owners: []string{"owner-a"}, View: true, Manage: true}
	w := newMPWorld(t, manager)
	w.source.err = &tracking.WechatError{Code: 48001}
	code, body := w.call("POST")
	message, _ := body["msg"].(string)
	if code != 502 || !strings.Contains(message, "48001") || strings.Contains(message, "official-secret") {
		t.Fatalf("%d %v", code, body)
	}
	w2 := newMPWorld(t, manager)
	w2.source.err = errors.New("dial tcp 10.1.2.3:443: refused")
	code, body = w2.call("POST")
	if message, _ = body["msg"].(string); code != 503 || strings.Contains(message, "10.1.2.3") {
		t.Fatalf("internal detail leaked: %d %v", code, body)
	}
}

func TestMPSyncRunnerUsesTheConfiguredAccountsScopeAndFailsClosed(t *testing.T) {
	manager := tracking.Scope{Tenant: "tenant-a", Actor: 9, Owners: []string{"owner-a"}, View: true, Manage: true}
	w := newMPWorld(t, manager)
	w.runner.scopeFor = func(context.Context) (tracking.Scope, error) {
		return tracking.Scope{}, &tracking.Error{Status: 403, Message: "管理账号未授权"}
	}
	if _, err := w.runner.Run(context.Background(), false); err == nil || w.source.calls != 0 {
		t.Fatalf("a sync account without a grant must not call WeChat (calls=%d, err=%v)", w.source.calls, err)
	}
	w.runner.scopeFor = func(context.Context) (tracking.Scope, error) {
		s := w.scope
		s.Manage = false
		return s, nil
	}
	if _, err := w.runner.Run(context.Background(), false); err == nil || w.source.calls != 0 {
		t.Fatalf("view-only sync account ran (calls=%d, err=%v)", w.source.calls, err)
	}
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{1, 2, 3, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestMPMediaStoreAppliesTheUploadChecks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TRACKING_MEDIA_DIR", dir)
	w := newMPWorld(t, tracking.Scope{})
	store := trackingMPMediaStore(w.svc, w.scope)
	ctx := context.Background()
	asset, err := store(ctx, "owner-a", testPNG(t, 30, 20), "公众号配图")
	if err != nil || asset.Kind != "image" || asset.Width != 30 || asset.Height != 20 || asset.MIME != "image/png" || !strings.HasPrefix(asset.URL, "/api/v1/miniprogram/tracking/media/") {
		t.Fatalf("valid picture: %+v %v", asset, err)
	}
	info, err := os.Stat(filepath.Join(dir, asset.ID))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("stored file: %v %v", info, err)
	}
	for name, data := range map[string][]byte{
		"empty":      nil,
		"text":       []byte("<html><script>alert(1)</script></html>"),
		"svg":        []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"truncated":  testPNG(t, 30, 20)[:20],
		"too large":  append(testPNG(t, 4, 4), bytes.Repeat([]byte{0}, 10<<20)...),
		"mp4 header": append([]byte{0, 0, 0, 0x18}, []byte("ftypmp42\x00\x00\x00\x00mp42isom")...),
		"too wide":   testPNG(t, 20001, 1),
		"too tall":   testPNG(t, 1, 20001),
	} {
		before, _ := os.ReadDir(dir)
		if _, err := store(ctx, "owner-a", data, "x"); err == nil {
			t.Errorf("%s accepted", name)
		}
		if after, _ := os.ReadDir(dir); len(after) != len(before) {
			t.Errorf("%s left a file behind", name)
		}
	}
	// An owner the account cannot manage leaves no file either.
	before, _ := os.ReadDir(dir)
	if _, err := store(ctx, "owner-b", testPNG(t, 5, 5), "x"); err == nil {
		t.Fatal("stored media for an owner outside the scope")
	}
	if after, _ := os.ReadDir(dir); len(after) != len(before) {
		t.Fatal("a refused media record left its file behind")
	}
	t.Setenv("TRACKING_MEDIA_DIR", "relative/path")
	if _, err := store(ctx, "owner-a", testPNG(t, 5, 5), "x"); err == nil {
		t.Fatal("stored media without a protected absolute directory")
	}
}
