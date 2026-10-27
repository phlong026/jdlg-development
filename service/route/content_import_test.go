package route

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"service/model"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/contentimport"
	"service/tracking"
)

type importRouteStore struct {
	contentimport.JobStore
	submitted contentimport.Request
	job       contentimport.Job
	retries   int
}

func (s *importRouteStore) Submit(_ context.Context, in contentimport.Request) (contentimport.Job, error) {
	s.submitted = in
	return contentimport.Job{ID: "job-1", Owner: in.Owner, Status: contentimport.Queued}, nil
}
func (s *importRouteStore) Get(_ context.Context, actor int64, id string) (contentimport.Job, error) {
	if actor != 11 || id != "job-1" {
		return contentimport.Job{}, contentimport.ErrNotFound
	}
	return s.job, nil
}
func (s *importRouteStore) List(_ context.Context, actor int64, owners []string, limit int) ([]contentimport.Job, error) {
	return []contentimport.Job{}, nil
}
func (s *importRouteStore) Retry(_ context.Context, actor int64, id string) (contentimport.Job, error) {
	s.retries++
	return s.job, nil
}
func importRoutesFixture(t *testing.T, s *importRouteStore) (*gin.Engine, *tracking.Scope) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	scope := &tracking.Scope{Tenant: "test", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	r := gin.New()
	g := r.Group("/imports", func(c *gin.Context) { c.Set("workbench.scope", *scope); c.Next() })
	registerContentImportHandlers(g, s, func() importCapabilities {
		return importCapabilities{Enabled: true, Platforms: []importPlatformCapability{{Platform: contentimport.Douyin, Available: true}}}
	})
	return r, scope
}
func callImport(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}
func TestImportRoutesScopeAndStrictBody(t *testing.T) {
	s := &importRouteStore{}
	r, scope := importRoutesFixture(t, s)
	base := `{"source_url":"https://v.douyin.com/demo/","owner_id":"owner-a","rights":"authorized","idempotency_key":"request-123"}`
	w := callImport(r, "POST", "/imports", base)
	if w.Code != 200 || s.submitted.Actor != 11 || s.submitted.Tenant != "test" || s.submitted.Platform != contentimport.Douyin {
		t.Fatalf("submit=%+v %d %s", s.submitted, w.Code, w.Body.String())
	}
	for _, body := range []string{strings.Replace(base, `"owner-a"`, `"owner-b"`, 1), strings.Replace(base, `"rights":"authorized"`, `"rights":"unknown"`, 1), strings.Replace(base, `"source_url"`, `"actor_id":99,"source_url"`, 1)} {
		if w := callImport(r, "POST", "/imports", body); w.Code < 400 {
			t.Fatalf("accepted %s", body)
		}
	}
	scope.Manage = false
	if w := callImport(r, "POST", "/imports", base); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestImportRoutesRevokedOwnerCannotGetOrRetry(t *testing.T) {
	s := &importRouteStore{job: contentimport.Job{ID: "job-1", Owner: "owner-b", Status: contentimport.Failed}}
	r, _ := importRoutesFixture(t, s)
	for _, method := range []string{"GET", "POST"} {
		path := "/imports/job-1"
		if method == "POST" {
			path += "/retry"
		}
		w := callImport(r, method, path, `{}`)
		if w.Code != 404 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if s.retries != 0 {
		t.Fatal("retried revoked owner")
	}
}
func TestImportRoutesUnavailableAndNoSnapshotLeak(t *testing.T) {
	s := &importRouteStore{job: contentimport.Job{ID: "job-1", Owner: "owner-a", Status: contentimport.Ready, ContentID: "draft-1", Document: &contentimport.Document{Source: contentimport.Source{Media: []contentimport.RemoteMedia{{URL: "https://signed-secret.example"}}}}, LeaseToken: "lease-secret"}}
	r, _ := importRoutesFixture(t, s)
	w := callImport(r, "GET", "/imports/job-1", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	r2 := gin.New()
	registerContentImportHandlers(r2.Group("/imports"), nil, func() importCapabilities { return importCapabilities{Enabled: false} })
	if w := callImport(r2, "GET", "/imports/capabilities", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := callImport(r2, "POST", "/imports", `{}`); w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestImportCookieMustBeProtectedServerFile(t *testing.T) {
	root := t.TempDir()
	path := root + "/cookie"
	if err := os.WriteFile(path, []byte("session=test-only"), 0600); err != nil {
		t.Fatal(err)
	}
	if value, err := importCookieFile(path); err != nil || value != "session=test-only" {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := importCookieFile(path); err == nil {
		t.Fatal("public cookie accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := root + "/link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := importCookieFile(link); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := importCookieFile("relative/path"); err == nil {
		t.Fatal("relative cookie accepted")
	}
}
func TestImportRoutesRequireSignedMiniAccount(t *testing.T) {
	t.Setenv("CONTENT_IMPORT_ENABLED", "false")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{Tenant: "test"})
	stop := registerContentImports(context.Background(), r.Group("/api/v1"), svc, &model.UserModel{}, nil)
	defer stop()
	for _, path := range []string{"/capabilities", "", "/job-1", "/job-1/retry"} {
		method := "GET"
		if path == "" || strings.HasSuffix(path, "retry") {
			method = "POST"
		}
		w := callImport(r, method, "/api/v1/miniprogram/workbench/imports"+path+"?actor_id=11", `{}`)
		if w.Code != 401 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}
