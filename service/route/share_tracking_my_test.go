package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

type myScopeDriver struct{}
type myScopeConn struct{}
type myScopeRows struct {
	owners []string
	index  int
}

func (myScopeDriver) Open(string) (driver.Conn, error)  { return myScopeConn{}, nil }
func (myScopeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (myScopeConn) Close() error                        { return nil }
func (myScopeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (myScopeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	for _, predicate := range []string{"t.can_view=TRUE", "u.user_type='miniprogram'", "COALESCE(u.status,'active')='active'"} {
		if !strings.Contains(query, predicate) {
			return nil, errors.New("missing predicate: " + predicate)
		}
	}
	if strings.Contains(query, "t.can_manage=TRUE OR") || strings.Contains(query, "OR t.can_manage=TRUE") {
		return nil, errors.New("manage grant widened visitor access")
	}
	if args[0].Value.(int64) == 11 {
		return &myScopeRows{owners: []string{"company"}}, nil
	}
	return &myScopeRows{}, nil
}
func (r *myScopeRows) Columns() []string { return []string{"owner_id"} }
func (r *myScopeRows) Close() error      { return nil }
func (r *myScopeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.owners) {
		return io.EOF
	}
	dest[0] = r.owners[r.index]
	r.index++
	return nil
}

func init() { sql.Register("tracking-my-scope", myScopeDriver{}) }

type myRouteStore struct {
	mu   sync.Mutex
	rows map[string]tracking.Record
}

type myRouteRepo struct{ rows map[string]tracking.Record }

func (s *myRouteStore) Transaction(_ context.Context, _ string, fn func(tracking.Repository) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(myRouteRepo{s.rows})
}

func (s *myRouteStore) ReadTransaction(_ context.Context, _ string, fn func(tracking.Repository) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(myRouteRepo{s.rows})
}

func (r myRouteRepo) Get(kind, id string) (tracking.Record, error) {
	row, ok := r.rows[kind+":"+id]
	if !ok {
		return row, tracking.ErrNotFound
	}
	return row, nil
}
func (r myRouteRepo) List(kind string) ([]tracking.Record, error) {
	rows := []tracking.Record{}
	for _, row := range r.rows {
		if row.Kind == kind {
			rows = append(rows, row)
		}
	}
	return rows, nil
}
func (r myRouteRepo) Put(row tracking.Record) error {
	r.rows[row.Kind+":"+row.ID] = row
	return nil
}
func (r myRouteRepo) Insert(row tracking.Record) error {
	if _, ok := r.rows[row.Kind+":"+row.ID]; ok {
		return errors.New("duplicate")
	}
	return r.Put(row)
}
func (r myRouteRepo) Remove(kind, id string) error {
	delete(r.rows, kind+":"+id)
	return nil
}

func myRouteShare(id, sharer string, at time.Time) tracking.Record {
	share := tracking.Share{ID: id, Route: "page", Owner: "company", Sharer: sharer, Origin: "miniprogram", Expires: at.Add(30 * 24 * time.Hour)}
	payload, _ := json.Marshal(share)
	return tracking.Record{Kind: "share", ID: "token-" + id, Owner: "company", Visitor: sharer, Share: id, At: at, Payload: payload}
}

func TestMySharesRejectMetricVersionBeforeServiceRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1/miniprogram/tracking/my-shares")
	group.Use(func(c *gin.Context) {
		c.Set("tracking.my.scope", tracking.Scope{Tenant: "tenant", Actor: 11, Owners: []string{"company"}, View: true})
		c.Next()
	})
	registerTrackingMyHandlers(group, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/tracking/my-shares?metric_version=old", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", response.Code, response.Body.String())
	}
}

func TestMySharesScopeRequiresExplicitViewGrant(t *testing.T) {
	db, err := sql.Open("tracking-my-scope", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	allowed, err := loadMiniTrackingMyScope(context.Background(), db, "tenant", 11)
	if err != nil || !allowed.View || !allowed.Allows("company") {
		t.Fatalf("view grant = %+v err=%v", allowed, err)
	}
	if _, err = loadMiniTrackingMyScope(context.Background(), db, "tenant", 22); err == nil {
		t.Fatal("manage-only account received visitor access")
	} else {
		var denied *tracking.Error
		if !errors.As(err, &denied) || denied.Status != http.StatusForbidden {
			t.Fatalf("manage-only error = %v, want 403", err)
		}
	}
	router := gin.New()
	router.GET("/my-shares", func(c *gin.Context) {
		_, err := loadMiniTrackingMyScope(c.Request.Context(), db, "tenant", 22)
		trackingResponse(c, nil, err)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/my-shares", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("manage-only route status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestMySharesCapabilityDenialDoesNotBecomeDataAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := sql.Open("tracking-my-scope", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, test := range []struct {
		actor  int64
		path   string
		status int
	}{
		{22, "/context", 200},
		{22, "", 403},
		{22, "/someone-elses-share/visits", 403},
		{0, "/context", 401},
		{11, "/context", 200},
	} {
		router := gin.New()
		g := router.Group("/api/v1/miniprogram/tracking/my-shares", func(c *gin.Context) {
			c.Set("user_id", test.actor)
			c.Next()
		}, miniTrackingMyScopeMiddleware(db, "tenant"))
		registerTrackingMyHandlers(g, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/tracking/my-shares"+test.path+"?can_view=true&owner=company&user_id=11", nil))
		if response.Code != test.status {
			t.Fatalf("actor=%d path=%s got=%d body=%s", test.actor, test.path, response.Code, response.Body.String())
		}
		if test.actor == 22 && test.path == "/context" {
			var result struct {
				Data struct {
					CanView bool     `json:"can_view"`
					Owners  []string `json:"owners"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Data.CanView || result.Data.Owners == nil || len(result.Data.Owners) != 0 {
				t.Fatalf("denied capability disclosed a scope: %s", response.Body.String())
			}
		}
		if response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("capability response must not be cached")
		}
	}
	router := gin.New()
	g := router.Group("/api/v1/miniprogram/tracking/my-shares", func(c *gin.Context) { c.Set("user_id", int64(11)); c.Next() }, miniTrackingMyScopeMiddleware(nil, "tenant"))
	registerTrackingMyHandlers(g, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/tracking/my-shares/context", nil))
	if response.Code != 503 {
		t.Fatalf("backend failure became no permission: %d %s", response.Code, response.Body.String())
	}
}

func TestMySharesContextRequiresRealSignedMiniSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := tracking.New(nil, tracking.Config{Tenant: "tenant"})
	registerTrackingMyRoutes(router.Group("/api/v1/miniprogram/tracking"), svc, &model.UserModel{}, nil)
	for _, token := range []string{"", "client-asserted-token"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/tracking/my-shares/context?user_id=11&can_view=true", nil)
		request.Header.Set("token", token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unsigned context status=%d body=%s", response.Code, response.Body.String())
		}
	}
}

func TestMySharesContextReportsOnlyExplicitViewScopeWithoutReadingRecords(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := sql.Open("tracking-my-scope", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, test := range []struct {
		actor  int64
		status int
	}{{0, http.StatusUnauthorized}, {22, http.StatusForbidden}, {11, http.StatusOK}} {
		router := gin.New()
		group := router.Group("/api/v1/miniprogram/tracking/my-shares", func(c *gin.Context) {
			scope, err := loadMiniTrackingMyScope(c.Request.Context(), db, "tenant", test.actor)
			if err != nil {
				trackingResponse(c, nil, err)
				return
			}
			c.Set("tracking.my.scope", scope)
			c.Next()
		})
		// 权限探测不应调用记录服务；nil 可防止它悄悄变成分享列表读取。
		registerTrackingMyHandlers(group, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/tracking/my-shares/context?owner=other&user_id=11&can_manage=true", nil))
		if response.Code != test.status {
			t.Fatalf("actor=%d status=%d body=%s", test.actor, response.Code, response.Body.String())
		}
		if test.status != http.StatusOK {
			continue
		}
		var result struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Data) != 2 || string(result.Data["can_view"]) != "true" || string(result.Data["owners"]) != `["company"]` {
			t.Fatalf("context must contain only server-owned view capability: %s", response.Body.String())
		}
	}
}

func TestMySharesHTTPAlwaysUsesAuthenticatedActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	store := &myRouteStore{rows: map[string]tracking.Record{}}
	for _, row := range []tracking.Record{
		myRouteShare("share-b-0001", "u:11", now.Add(-2*time.Hour)),
		myRouteShare("share-d-0001", "u:22", now.Add(-time.Hour)),
	} {
		store.rows[row.Kind+":"+row.ID] = row
	}
	svc := tracking.New(store, tracking.Config{Tenant: "tenant", Routes: map[string]tracking.Route{"page": {Title: "页面", Public: true}}, RetentionDays: 180})
	svc.Now = func() time.Time { return now }
	router := gin.New()
	group := router.Group("/api/v1/miniprogram/tracking/my-shares")
	group.Use(func(c *gin.Context) {
		c.Set("tracking.my.scope", tracking.Scope{Tenant: "tenant", Actor: 11, Owners: []string{"company"}, View: true})
		c.Next()
	})
	registerTrackingMyHandlers(group, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/tracking/my-shares?sharer_id=u:22", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "share-b-0001") || strings.Contains(body, "share-d-0001") {
		t.Fatalf("authenticated actor isolation failed: %s", body)
	}
}

// 9.6 策略：客户视图路由由 TRACKING_MY_CUSTOMER_VIEW_ENABLED 门控，默认不注册。
func TestMyCustomerViewRoutesGateBehindEnvSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := tracking.New(nil, tracking.Config{Tenant: "tenant"})
	for _, test := range []struct {
		enabled bool
		want    int
	}{
		{false, http.StatusNotFound},
		{true, http.StatusUnauthorized},
	} {
		if test.enabled {
			t.Setenv("TRACKING_MY_CUSTOMER_VIEW_ENABLED", "true")
		} else {
			t.Setenv("TRACKING_MY_CUSTOMER_VIEW_ENABLED", "")
		}
		for _, path := range []string{"/api/v1/miniprogram/tracking/my-visits", "/api/v1/miniprogram/tracking/my-visitors", "/api/v1/miniprogram/tracking/my-content-stats"} {
			router := gin.New()
			registerTrackingMyRoutes(router.Group("/api/v1/miniprogram/tracking"), svc, &model.UserModel{}, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != test.want {
				t.Fatalf("enabled=%v path=%s status=%d body=%s", test.enabled, path, response.Code, response.Body.String())
			}
		}
	}
}
