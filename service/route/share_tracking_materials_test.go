package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

type materialScopeDriver struct{}
type materialScopeConn struct{}
type materialScopeRows struct {
	values []string
	index  int
}

func (materialScopeDriver) Open(string) (driver.Conn, error)  { return materialScopeConn{}, nil }
func (materialScopeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (materialScopeConn) Close() error                        { return nil }
func (materialScopeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (materialScopeConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	for _, required := range []string{"JOIN users u ON u.id=t.user_id", "t.user_id=?", "t.tenant_id=?", "t.enabled=TRUE", "t.can_manage=TRUE", "u.user_type='miniprogram'", "COALESCE(u.status,'active')='active'"} {
		if !strings.Contains(q, required) {
			return nil, errors.New("missing server predicate: " + required)
		}
	}
	actor := args[0].Value.(int64)
	tenant := args[1].Value.(string)
	if actor == 503 {
		return nil, errors.New("private database failure")
	}
	// Fixtures 2/3/4/5 represent view-only, disabled, management-type and banned users.
	if actor != 1 || tenant != "tenant-a" {
		return &materialScopeRows{}, nil
	}
	return &materialScopeRows{values: []string{"owner-a", ""}}, nil
}
func (r *materialScopeRows) Columns() []string { return []string{"owner_id"} }
func (r *materialScopeRows) Close() error      { return nil }
func (r *materialScopeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	dest[0] = r.values[r.index]
	r.index++
	return nil
}
func init() { sql.Register("tracking-material-scope", materialScopeDriver{}) }
func TestMiniMaterialScopeIsExplicitAndServerOwned(t *testing.T) {
	db, err := sql.Open("tracking-material-scope", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	scope, err := loadMiniTrackingMaterialScope(context.Background(), db, "tenant-a", 1)
	if err != nil || scope.Actor != 1 || !scope.Manage || scope.View || len(scope.Owners) != 1 || !scope.Allows("owner-a") || scope.Allows("owner-b") {
		t.Fatal(scope, err)
	}
	for _, actor := range []int64{0, 2, 3, 4, 5} {
		if _, err := loadMiniTrackingMaterialScope(context.Background(), db, "tenant-a", actor); err == nil {
			t.Fatal("unauthorized actor", actor)
		}
	}
	if _, err := loadMiniTrackingMaterialScope(context.Background(), db, "other-tenant", 1); err == nil {
		t.Fatal("tenant mismatch")
	}
	if _, err := loadMiniTrackingMaterialScope(context.Background(), db, "tenant-a", 503); !errors.Is(err, tracking.ErrUnavailable) {
		t.Fatal("failure not closed", err)
	}
}
func TestMiniMaterialsRoutesRequireRealSignedToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterTrackingRoutes(router.Group("/api/v1"), &model.UserModel{}, nil, nil)
	for _, path := range []string{"/api/v1/miniprogram/tracking/materials?owner=owner-a&user_id=1", "/api/v1/miniprogram/tracking/materials/material-one/versions/1"} {
		for _, token := range []string{"", "client-asserted-token"} {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("token", token)
			router.ServeHTTP(w, req)
			if w.Code != 401 {
				t.Fatal(path, w.Code, w.Body.String())
			}
		}
	}
}
func TestMiniMaterialHandlersOnlyReturnScopedPublishedMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registered := gin.New()
	svc := RegisterTrackingRoutes(registered.Group("/api/v1"), &model.UserModel{}, nil, nil)
	svc.Config.Tenant = "materials-test"
	svc.Config.ContentEnabled = true
	svc.Config.Routes = map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}}
	svc.Store = &governanceTestStore{records: map[string]tracking.Record{}}
	scope := tracking.Scope{Tenant: svc.Config.Tenant, Actor: 7, Owners: []string{"owner-a"}, Manage: true}
	draft, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "发布标题", Body: "正文", Owner: "owner-a", Rights: "own"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(context.Background(), scope, draft.ID, draft.Revision, preview["preview_token"].(string), "material-http-publish"); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	for _, r := range registered.Routes() {
		if strings.Contains(r.Path, "/materials") {
			router.Handle(r.Method, r.Path, func(c *gin.Context) { c.Set("tracking.material.scope", scope); c.Next() }, r.HandlerFunc)
		}
	}
	path := "/api/v1/miniprogram/tracking/materials/" + draft.ID + "/versions/1"
	for _, target := range []string{"/api/v1/miniprogram/tracking/materials?owner=other&user_id=999", path} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "发布标题") || strings.Contains(w.Body.String(), "正文") || strings.Contains(w.Body.String(), "owner_id") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	scope.Owners = []string{"owner-b"}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != 404 || strings.Contains(w.Body.String(), "发布标题") {
		t.Fatal(w.Code, w.Body.String())
	}
}
