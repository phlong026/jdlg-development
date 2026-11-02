package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	redisstore "github.com/gin-contrib/sessions/redis"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"service/component"
	"service/config"
	"service/model"
	"service/processor"
	"strings"
	"testing"
	"time"
)

// Password verification and SQL are fixtures; routes, Redis storage, cookies and
// authorization middleware use the production implementation.
type managementSessionAuth struct{}

func (managementSessionAuth) GetStrategyName() string { return "password" }
func (managementSessionAuth) Login(_ context.Context, req interface{}) (*processor.AuthResult, error) {
	in, ok := req.(*processor.PasswordLoginRequest)
	if !ok || in.Username != "fixture-admin" || in.Password != "fixture-password" {
		return nil, errors.New("invalid fixture credentials")
	}
	return &processor.AuthResult{User: &model.User{ID: 7, Username: in.Username, UserType: "management"}}, nil
}

type managementSessionDB struct {
	enabled bool
	scope   string
}

func (d *managementSessionDB) Connect(context.Context) (driver.Conn, error) { return d, nil }
func (d *managementSessionDB) Driver() driver.Driver                        { return d }
func (d *managementSessionDB) Open(string) (driver.Conn, error)             { return d, nil }
func (d *managementSessionDB) Prepare(string) (driver.Stmt, error)          { return nil, driver.ErrSkip }
func (d *managementSessionDB) Close() error                                 { return nil }
func (d *managementSessionDB) Begin() (driver.Tx, error)                    { return nil, driver.ErrSkip }
func (d *managementSessionDB) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(query, "FROM management_access") || len(args) != 1 || args[0].Value != int64(7) {
		return nil, errors.New("unexpected query")
	}
	return &profileGateAuthRows{columns: []string{"scope", "enabled", "version"}, values: [][]driver.Value{{d.scope, d.enabled, int64(1)}}}, nil
}
func managementSessionRouter(t *testing.T, bridge bool) (*gin.Engine, *miniredis.Miniredis, *managementSessionDB) {
	t.Helper()
	t.Setenv("CRM_BRIDGE_URL", "")
	t.Setenv("WECOM_LOGIN_ENABLED", "false")
	if bridge {
		t.Setenv("CRM_BRIDGE_URL", "https://crm.example.test")
	}
	mr := miniredis.RunT(t)
	previous := component.SessionStore
	cfg := &config.Config{Redis: config.RedisConfig{Addr: mr.Addr()}, Server: config.ServerConfig{Env: "development", SessionSecret: "fixture-only-session-signing-key-32-bytes"}}
	store, err := component.InitSessionStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	backing, err := redisstore.GetRedisStore(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backing.Close(); component.SessionStore = previous })
	auth := processor.NewAuthProcessor()
	auth.RegisterStrategy(managementSessionAuth{})
	access := &managementSessionDB{enabled: true, scope: "platform_admin"}
	db := sql.OpenDB(access)
	t.Cleanup(func() { db.Close() })
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("sessionid", store))
	RegisterManagementRoutes(r.Group("/api/v1/management"), auth, &model.UserModel{DB: db}, nil, nil, nil)
	return r, mr, access
}
func managementSessionRequest(r *gin.Engine, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "https://admin.example.test/api/v1/management"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
func managementSessionLogin(t *testing.T, r *gin.Engine, remember string, cookie *http.Cookie) *http.Cookie {
	t.Helper()
	w := managementSessionRequest(r, "POST", "/login", `{"username":"fixture-admin","password":"fixture-password"`+remember+`}`, cookie)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "sessionid" {
			return c
		}
	}
	t.Fatal("login did not set session cookie")
	return nil
}
func TestManagementRememberSessionLifetime(t *testing.T) {
	for _, bridge := range []bool{false, true} {
		for _, tc := range []struct {
			name, field string
			age         int
		}{
			{"remembered", `,"remember_me":true`, 28 * 24 * 60 * 60},
			{"unchecked", `,"remember_me":false`, 8 * 60 * 60},
			{"legacy-client", "", 8 * 60 * 60},
		} {
			t.Run(fmt.Sprintf("%s/bridge=%v", tc.name, bridge), func(t *testing.T) {
				r, mr, _ := managementSessionRouter(t, bridge)
				cookie := managementSessionLogin(t, r, tc.field, nil)
				if cookie.MaxAge != tc.age {
					t.Fatalf("cookie max-age=%d, want %d", cookie.MaxAge, tc.age)
				}
				if cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
					t.Fatalf("unsafe cookie flags: path=%s httpOnly=%v secure=%v sameSite=%v", cookie.Path, cookie.HttpOnly, cookie.Secure, cookie.SameSite)
				}
				if len(mr.Keys()) != 1 {
					t.Fatalf("session keys=%d", len(mr.Keys()))
				}
				key := mr.Keys()[0]
				if mr.TTL(key) != time.Duration(tc.age)*time.Second {
					t.Fatalf("Redis TTL=%s", mr.TTL(key))
				}
				mr.FastForward(time.Duration(tc.age)*time.Second - time.Second)
				if w := managementSessionRequest(r, "GET", "/me", "", cookie); w.Code != 200 {
					t.Fatalf("session expired early: %d %s", w.Code, w.Body.String())
				}
				if mr.TTL(key) != time.Second {
					t.Fatal("reading /me must not extend the fixed lifetime")
				}
				mr.FastForward(time.Second)
				if w := managementSessionRequest(r, "GET", "/me", "", cookie); w.Code != 401 {
					t.Fatalf("expired session accepted: %d", w.Code)
				}
			})
		}
	}
}
func TestManagementRememberSessionLogoutAndRecheckAccess(t *testing.T) {
	r, mr, access := managementSessionRouter(t, true)
	cookie := managementSessionLogin(t, r, `,"remember_me":true`, nil)
	access.scope = "crm_only"
	if w := managementSessionRequest(r, "GET", "/me", "", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"scope":"crm_only"`) {
		t.Fatalf("scope not rechecked: %d %s", w.Code, w.Body.String())
	}
	access.enabled = false
	if w := managementSessionRequest(r, "GET", "/me", "", cookie); w.Code != 403 {
		t.Fatalf("disabled user accepted: %d", w.Code)
	}
	w := managementSessionRequest(r, "POST", "/logout", "", cookie)
	if w.Code != 200 {
		t.Fatalf("logout blocked for disabled user: %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 || cookies[0].Value != "" {
		t.Fatal("logout must expire browser cookie")
	}
	if len(mr.Keys()) != 0 {
		t.Fatal("logout must delete Redis session")
	}
	access.enabled = true
	if w := managementSessionRequest(r, "GET", "/me", "", cookie); w.Code != 401 {
		t.Fatalf("old cookie replayed after logout: %d", w.Code)
	}
}
func TestManagementRememberSessionCanReturnToShortLifetime(t *testing.T) {
	r, mr, _ := managementSessionRouter(t, true)
	cookie := managementSessionLogin(t, r, `,"remember_me":true`, nil)
	cookie = managementSessionLogin(t, r, `,"remember_me":false`, cookie)
	if cookie.MaxAge != 8*60*60 {
		t.Fatalf("unchecked lifetime=%d", cookie.MaxAge)
	}
	mr.FastForward(8 * time.Hour)
	if w := managementSessionRequest(r, "GET", "/me", "", cookie); w.Code != 401 {
		t.Fatalf("unchecked session inherited 28 days: %d", w.Code)
	}
}
func TestManagementRememberSessionRejectsInvalidLogin(t *testing.T) {
	for _, body := range []string{
		`{"username":"fixture-admin","password":"wrong","remember_me":true}`,
		`{"username":"fixture-admin","password":"fixture-password","remember_me":"true"}`,
	} {
		r, mr, _ := managementSessionRouter(t, true)
		w := managementSessionRequest(r, "POST", "/login", body, nil)
		if w.Code != 400 || len(w.Result().Cookies()) != 0 || len(mr.Keys()) != 0 {
			t.Fatalf("invalid login created session: status=%d", w.Code)
		}
	}
}
