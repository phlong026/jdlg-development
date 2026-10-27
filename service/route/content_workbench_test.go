package route

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func TestWorkbenchUsesSignedMiniAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{Tenant: "workbench"})
	registerContentWorkbenchRoutes(r.Group("/api/v1"), svc, &model.UserModel{}, nil)
	for _, target := range []struct{ method, path string }{
		{"GET", "/api/v1/miniprogram/workbench/context"},
		{"GET", "/api/v1/miniprogram/workbench/contents"},
		{"POST", "/api/v1/miniprogram/workbench/contents"},
		{"PATCH", "/api/v1/miniprogram/workbench/contents/other"},
		{"POST", "/api/v1/miniprogram/workbench/media"},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(target.method, target.path+"?user_id=1&owner=owner-a", nil)
		req.Header.Set("token", "client-asserted-token")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: %d %s", target.method, target.path, w.Code, w.Body.String())
		}
	}
}

func newWorkbenchHandlerFixture(t *testing.T, scope *tracking.Scope) (*gin.Engine, *tracking.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{
		Tenant: "workbench", Enabled: true, ContentEnabled: true, DefaultOwner: "owner-a", ConsentVersion: "v1",
		Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}},
	})
	r := gin.New()
	g := r.Group("/workbench", func(c *gin.Context) { c.Set("workbench.scope", *scope); c.Next() })
	registerContentWorkbenchHandlers(g, svc)
	return r, svc
}

func createWorkbenchShare(t *testing.T, svc *tracking.Service, scope tracking.Scope) string {
	t.Helper()
	ctx := context.Background()
	content, err := svc.SaveContent(ctx, scope, "", tracking.Draft{Title: "统一分享", Body: "公开正文", BodyFormat: "plain", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(ctx, scope, content.ID, content.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(ctx, scope, content.ID, content.Revision, preview["preview_token"].(string), "share-entry-publish")
	if err != nil {
		t.Fatal(err)
	}
	version := published["content"].(tracking.Content).Version
	entry, err := svc.Enter(ctx, tracking.EntryInput{Route: "pages/contentreader/contentreader", Content: content.ID, Version: version, Client: "share-entry-client", Consent: "v1"}, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateShare(ctx, tracking.ShareInput{Route: entry.Route, Content: entry.Content, Version: entry.Version, Channel: "app_message", Key: "share-entry-create"}, entry.Token, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	return created["share_token"].(string)
}

func TestShareEntryDestinationUsesFixedOriginAndDoesNotRequireManageGrant(t *testing.T) {
	t.Setenv("SHARE_WEB_ENABLED", "true")
	t.Setenv("PUBLIC_SHARE_ORIGIN", "https://www.jiadilingguang.com")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	_, svc := newWorkbenchHandlerFixture(t, &scope)
	token := createWorkbenchShare(t, svc, scope)

	out, err := shareEntryDestination(context.Background(), svc, tracking.Scope{Tenant: "workbench", Actor: 11}, token)
	if err != nil {
		t.Fatal(err)
	}
	if out["web_url"] != "https://www.jiadilingguang.com/share/s/"+token || !strings.Contains(out["native_query"].(string), "st="+token) || out["handoff_ready"] != false {
		t.Fatalf("destination = %#v", out)
	}
	if _, err = shareEntryDestination(context.Background(), svc, tracking.Scope{Tenant: "workbench", Actor: 12}, token); err == nil {
		t.Fatal("foreign actor exchanged another account's share")
	}
	svc.Config.Enabled = false
	disabled, err := shareEntryDestination(context.Background(), svc, tracking.Scope{Tenant: "workbench", Actor: 11}, token)
	if err != nil || disabled["tracking_ready"] != false {
		t.Fatalf("disabled tracking destination=%#v err=%v", disabled, err)
	}
}

func TestShareEntryRoutesRequireSignedMiniProgramAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{Tenant: "workbench"})
	registerMiniProgramShareEntryRoutes(router.Group("/api/v1"), svc, &model.UserModel{}, nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/miniprogram/tracking/share-entries", strings.NewReader(`{"share_token":"forged-share-token"}`))
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned request = %d %s", w.Code, w.Body.String())
	}
}

func workbenchRequest(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestWorkbenchDraftPreviewPublishAndOwnerIsolation(t *testing.T) {
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	r, svc := newWorkbenchHandlerFixture(t, &scope)
	w := workbenchRequest(t, r, "POST", "/workbench/contents", tracking.Draft{Title: "三期图文", Body: "项目说明", Rights: "own", Owner: "owner-a"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var saved struct {
		Data tracking.Content `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Data.ID == "" || saved.Data.Status != "draft" {
		t.Fatal(w.Body.String())
	}
	id := saved.Data.ID
	// Another grant cannot read or overwrite this owner's draft, even by known ID.
	scope.Owners = []string{"owner-b"}
	if denied := workbenchRequest(t, r, "GET", "/workbench/contents/"+id, nil); denied.Code != 404 || strings.Contains(denied.Body.String(), "项目说明") {
		t.Fatal(denied.Code, denied.Body.String())
	}
	scope.Owners = []string{"owner-a"}
	stale := workbenchRequest(t, r, "PATCH", "/workbench/contents/"+id, tracking.Draft{Title: "误覆盖", Body: "新正文", Rights: "own", Owner: "owner-a", Revision: 999})
	if stale.Code != 409 {
		t.Fatal(stale.Code, stale.Body.String())
	}
	pv := workbenchRequest(t, r, "POST", "/workbench/contents/"+id+"/preview", map[string]any{"expected_revision": saved.Data.Revision})
	var preview struct {
		Data struct {
			Token string `json:"preview_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(pv.Body.Bytes(), &preview); err != nil || pv.Code != 200 || preview.Data.Token == "" {
		t.Fatal(pv.Code, pv.Body.String(), err)
	}
	published := workbenchRequest(t, r, "POST", "/workbench/contents/"+id+"/publish", map[string]any{"expected_revision": saved.Data.Revision, "preview_token": preview.Data.Token, "idempotency_key": "workbench-publish-one"})
	if published.Code != 200 {
		t.Fatal(published.Code, published.Body.String())
	}
	content, err := svc.ReadContent(context.Background(), id, 1)
	if err != nil || content.Title != "三期图文" {
		t.Fatal(content, err)
	}
}

func TestWorkbenchReadinessDoesNotPretendMediaAndURLLinkAreEnabled(t *testing.T) {
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	r, _ := newWorkbenchHandlerFixture(t, &scope)
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "false")
	t.Setenv("TRACKING_MEDIA_DIR", "")
	t.Setenv("TRACKING_WECHAT_URLLINK_ENABLED", "")
	w := workbenchRequest(t, r, "POST", "/workbench/contents", tracking.Draft{Title: "不应保存", Body: "正文", Rights: "own", Owner: "owner-a"})
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = workbenchRequest(t, r, "GET", "/workbench/context", nil)
	var ctx struct {
		Data struct {
			Media   bool `json:"media_upload_configured"`
			URLLink bool `json:"url_link_configured"`
			Enabled bool `json:"enabled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &ctx); err != nil || w.Code != 200 || ctx.Data.Media || ctx.Data.URLLink || ctx.Data.Enabled {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	privateDir := filepath.Join(t.TempDir(), "private-media")
	if err := os.Mkdir(privateDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACKING_MEDIA_DIR", privateDir)
	w = workbenchRequest(t, r, "GET", "/workbench/context", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &ctx); err != nil || !ctx.Data.Media || !ctx.Data.Enabled || ctx.Data.URLLink {
		t.Fatal(w.Body.String(), err)
	}
	if strings.Contains(w.Body.String(), privateDir) {
		t.Fatal("readiness must not expose server storage paths")
	}
}

func TestWorkbenchDraftMediaRequiresAuthorizedOwnerAndIsNotPublic(t *testing.T) {
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	dir := t.TempDir()
	t.Setenv("TRACKING_MEDIA_DIR", dir)
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	r, svc := newWorkbenchHandlerFixture(t, &scope)
	id := strings.Repeat("m", 32)
	asset := tracking.Asset{ID: id, Kind: "image", MIME: "image/png", Width: 2, Height: 2, URL: "/api/v1/miniprogram/tracking/media/" + id}
	if err := svc.AddMedia(context.Background(), scope, asset, "owner-a"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id), []byte("draft-image-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Media(context.Background(), nil, id); err == nil {
		t.Fatal("unpublished media must remain private")
	}
	w := workbenchRequest(t, r, "GET", "/workbench/media/"+id, nil)
	if w.Code != 200 || w.Body.String() != "draft-image-bytes" || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	scope.Owners = []string{"owner-b"}
	denied := workbenchRequest(t, r, "GET", "/workbench/media/"+id, nil)
	if denied.Code != 404 || strings.Contains(denied.Body.String(), "draft-image-bytes") {
		t.Fatal(denied.Code, denied.Body.String())
	}
}
