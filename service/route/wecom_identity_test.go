package route

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"service/model"
)

type identityTransport func(*http.Request) (*http.Response, error)

func (f identityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func identityTestConfig(t *testing.T) wecomLoginConfig {
	t.Helper()
	for k, v := range map[string]string{"WECOM_LOGIN_ENABLED": "true", "WECOM_LOGIN_CORP_ID": "corp-test", "WECOM_LOGIN_AGENT_ID": "1000001", "WECOM_LOGIN_SECRET": "private-test-secret", "WECOM_LOGIN_CALLBACK_URL": "https://admin.example.test/api/v1/management/wecom/callback", "WECOM_LOGIN_FRONTEND_ORIGIN": "https://admin.example.test"} {
		t.Setenv(k, v)
	}
	cfg, e := loadWecomLoginConfig()
	if e != nil {
		t.Fatal(e)
	}
	return cfg
}
func identityTestProvider() *wecomIdentityProvider {
	p := newWecomIdentityProvider()
	p.client.Transport = identityTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "qyapi.weixin.qq.com" || r.URL.Scheme != "https" {
			return nil, fmt.Errorf("unexpected provider destination")
		}
		data := ""
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			data = `{"access_token":"test-access","expires_in":7200}`
		case "/cgi-bin/auth/getuserinfo":
			if r.URL.Query().Get("code") == "external" {
				data = `{"openid":"external-only"}`
			} else {
				data = fmt.Sprintf(`{"userid":%q}`, r.URL.Query().Get("code"))
			}
		case "/cgi-bin/user/get":
			id := r.URL.Query().Get("userid")
			if id == "missing" {
				data = `{"errcode":46004,"errmsg":"user not exist"}`
			} else {
				status := 1
				if id == "disabled" {
					status = 2
				}
				data = fmt.Sprintf(`{"userid":%q,"name":"核验成员","status":%d}`, id, status)
			}
		default:
			return nil, fmt.Errorf("unexpected endpoint")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(data))}, nil
	})
	return p
}
func TestWecomIdentityProviderAndConfig(t *testing.T) {
	cfg := identityTestConfig(t)
	p := identityTestProvider()
	m, e := p.exchange(context.Background(), cfg, "employee-a")
	if e != nil || m.ID != "employee-a" {
		t.Fatalf("member lookup: %v %v", m, e)
	}
	for _, code := range []string{"external", "disabled", "bad/id"} {
		if _, e = p.exchange(context.Background(), cfg, code); e == nil {
			t.Fatalf("invalid member accepted: %s", code)
		}
	}
	for _, callback := range []string{"https://other.example.test/api/v1/management/wecom/callback", "http://admin.example.test/api/v1/management/wecom/callback", "https://evil@admin.example.test/api/v1/management/wecom/callback", "https://admin.example.test/other", "https://admin.example.test/api/v1/management/wecom/callback?next=evil"} {
		t.Setenv("WECOM_LOGIN_CALLBACK_URL", callback)
		if _, e = loadWecomLoginConfig(); e == nil {
			t.Fatalf("invalid callback accepted: %s", callback)
		}
	}
}
func TestWecomProviderErrorsDoNotExposeCredentials(t *testing.T) {
	cfg := identityTestConfig(t)
	p := newWecomIdentityProvider()
	p.client.Transport = identityTransport(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("sensitive-secret-url") })
	_, e := p.exchange(context.Background(), cfg, "code")
	if e == nil || strings.Contains(e.Error(), "sensitive") {
		t.Fatal("provider error not sanitized")
	}
}

// Runs only against an explicitly supplied, empty disposable test database.
func TestWecomIdentityMySQLFlow(t *testing.T) {
	dsn := os.Getenv("CRM_IDENTITY_TEST_DSN")
	if dsn == "" {
		t.Skip("set CRM_IDENTITY_TEST_DSN to an isolated jd_wecom_identity_test database")
	}
	parsed, e := mysql.ParseDSN(dsn)
	if e != nil || !strings.HasPrefix(parsed.DBName, "jd_wecom_identity_test") {
		t.Fatal("refusing non-test database")
	}
	parsed.MultiStatements = true
	db, e := sql.Open("mysql", parsed.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	if e = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()`).Scan(&count); e != nil || count != 0 {
		t.Fatalf("test database must be empty: %d %v", count, e)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE TABLE users(id BIGINT PRIMARY KEY,username VARCHAR(64),user_type VARCHAR(24));`)
	for _, f := range []string{"../sql/customer_management_bridge_up.sql", "../sql/management_wecom_identity_up.sql"} {
		b, e := os.ReadFile(f)
		if e != nil {
			t.Fatal(e)
		}
		exec(string(b))
	}
	for i := 1; i <= 5; i++ {
		exec(`INSERT INTO users VALUES (?,?, 'management')`, i, fmt.Sprintf("user-%d", i))
		scope := "crm_only"
		if i == 1 {
			scope = "platform_admin"
		}
		exec(`INSERT INTO management_access VALUES (?,?,TRUE,0,1,NOW(6))`, i, scope)
	}
	cfg := identityTestConfig(t)
	ctx := context.Background()
	t.Run("binding uniqueness version and audited revocation", func(t *testing.T) {
		b, e := saveWecomBinding(ctx, db, wecomBinding{UserID: 2, CorpID: cfg.Corp, EmployeeID: "employee-a", Enabled: true}, 1)
		if e != nil || b.Version != 1 {
			t.Fatalf("save: %+v %v", b, e)
		}
		if _, e = saveWecomBinding(ctx, db, wecomBinding{UserID: 3, CorpID: cfg.Corp, EmployeeID: "employee-a", Enabled: true}, 1); e == nil {
			t.Fatal("duplicate employee accepted")
		}
		if _, e = saveWecomBinding(ctx, db, wecomBinding{UserID: 2, CorpID: cfg.Corp, EmployeeID: "employee-b", Enabled: true}, 1); e == nil {
			t.Fatal("stale binding accepted")
		}
		if validateStaffIdentity(ctx, db, 2, "employee-b") == nil {
			t.Fatal("mismatched business employee accepted")
		}
		off, e := saveWecomBinding(ctx, db, wecomBinding{UserID: 2, Version: 1, Enabled: false}, 1)
		if e != nil || off.Enabled || off.EmployeeID != "employee-a" {
			t.Fatal("revocation failed", e)
		}
		_, e = saveWecomBinding(ctx, db, wecomBinding{UserID: 2, Version: 2, CorpID: cfg.Corp, EmployeeID: "employee-a", Enabled: true}, 1)
		if e != nil {
			t.Fatal(e)
		}
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM management_access_audit WHERE user_id=2 AND action='wecom_identity'`).Scan(&n)
		if n != 3 {
			t.Fatal("audit missing", n)
		}
	})
	t.Run("concurrent claim only one wins", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for _, id := range []int64{3, 4} {
			wg.Add(1)
			go func(id int64) {
				defer wg.Done()
				_, e := saveWecomBinding(ctx, db, wecomBinding{UserID: id, CorpID: cfg.Corp, EmployeeID: "same-member", Enabled: true}, 1)
				results <- e
			}(id)
		}
		wg.Wait()
		close(results)
		success := 0
		for e := range results {
			if e == nil {
				success++
			}
		}
		if success != 1 {
			t.Fatal("non-unique concurrent binding", success)
		}
	})
	t.Run("role revision and incomplete provision refuse activation", func(t *testing.T) {
		exec(`UPDATE management_access SET version=2 WHERE user_id=5`)
		if _, e := saveWecomBinding(ctx, db, wecomBinding{UserID: 5, CorpID: cfg.Corp, EmployeeID: "member-5", Enabled: true}, 1); e == nil {
			t.Fatal("stale role snapshot accepted")
		}
		exec(`INSERT INTO management_crm_link(user_id,crm_username,state,enabled,version,updated_by,updated_at) VALUES (5,'managed-5','pending',FALSE,2,1,NOW(6))`)
		if _, e := saveWecomBinding(ctx, db, wecomBinding{UserID: 5, CorpID: cfg.Corp, EmployeeID: "member-5", Enabled: true, AccessVersion: 2}, 1); e == nil {
			t.Fatal("pending role accepted")
		}
	})

	t.Run("binding routes enforce admin origin and enterprise", func(t *testing.T) {
		mode := "ok"
		remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			corp, employee := cfg.Corp, "employee-a"
			if mode == "corp" {
				corp = "different-corp"
			}
			if mode == "employee" {
				employee = "someone-else"
			}
			fmt.Fprintf(w, `{"corpId":%q,"demo":false,"accounts":[{"managementUserId":2,"employeeId":%q,"role":"staff","enabled":true}]}`, corp, employee)
		}))
		defer remote.Close()
		t.Setenv("CRM_BRIDGE_URL", remote.URL)
		t.Setenv("CRM_BRIDGE_KEY", "0123456789abcdef0123456789abcdef")
		t.Setenv("CRM_BRIDGE_KEY_ID", "v1")
		t.Setenv("CRM_BRIDGE_AUDIENCE", "test")
		t.Setenv("CRM_ALLOWED_ORIGINS", "https://admin.example.test")
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.Use(sessions.Sessions("test", cookie.NewStore([]byte("test-only-cookie-signing-key-32-bytes"))))
		group := router.Group("/api/v1/management/crm")
		group.Use(func(c *gin.Context) {
			c.Set("management.access", managementAccess{Scope: c.GetHeader("Test-Scope")})
			c.Next()
		})
		registerWecomBindingProvider(group, &model.UserModel{DB: db}, identityTestProvider())
		post := func(scope, origin, body string) int {
			req := httptest.NewRequest("POST", "https://admin.example.test/api/v1/management/crm/access/wecom", strings.NewReader(body))
			req.Header.Set("Test-Scope", scope)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CRM-Request", "1")
			req.Header.Set("Origin", origin)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			return rec.Code
		}
		body := `{"userId":2,"version":3,"employeeId":"employee-a","enabled":true}`
		if post("crm_only", "https://admin.example.test", body) != 403 {
			t.Fatal("staff can manage binding")
		}
		if post("platform_admin", "https://evil.example.test", body) != 403 {
			t.Fatal("cross-origin write accepted")
		}
		for _, invalid := range []string{"corp", "employee"} {
			mode = invalid
			if post("platform_admin", "https://admin.example.test", body) != 409 {
				t.Fatal("mismatched CRM metadata accepted", invalid)
			}
		}
		mode = "ok"
		if post("platform_admin", "https://admin.example.test", body) != 200 {
			t.Fatal("verified binding rejected")
		}
		t.Setenv("WECOM_LOGIN_ENABLED", "false")
		t.Setenv("CRM_BRIDGE_URL", "")
		if post("platform_admin", "https://admin.example.test", `{"userId":2,"version":4,"enabled":false}`) != 200 {
			t.Fatal("offline revocation failed")
		}
		exec(`UPDATE management_wecom_identity SET enabled=TRUE WHERE user_id=2`)
	})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("sessionid", cookie.NewStore([]byte("test-only-cookie-signing-key-32-bytes"))))
	users := &model.UserModel{DB: db}
	group := r.Group("/api/v1/management")
	registerWecomLoginProvider(group, users, identityTestProvider())
	auth := group.Group("")
	auth.Use(AuthRequired, ManagementAccessRequired(users))
	auth.GET("/me", func(c *gin.Context) {
		c.JSON(200, gin.H{"id": GetUserID(c), "scope": c.MustGet("management.access").(managementAccess).Scope})
	})
	auth.GET("/orders", func(c *gin.Context) { c.Status(200) })
	auth.POST("/logout", func(c *gin.Context) { ClearUserSession(c); c.Status(200) })
	request := func(method, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		q := httptest.NewRequest(method, "https://admin.example.test"+path, nil)
		for _, cookie := range cookies {
			q.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	start := func() (string, *http.Cookie) {
		w := request("GET", wecomLoginPath+"/start")
		if w.Code != 302 {
			t.Fatalf("start %d %s", w.Code, w.Body.String())
		}
		u, _ := url.Parse(w.Header().Get("Location"))
		return u.Query().Get("state"), w.Result().Cookies()[0]
	}
	callback := func(code, state string, cookie *http.Cookie) *httptest.ResponseRecorder {
		return request("GET", wecomLoginPath+"/callback?"+url.Values{"code": {code}, "state": {state}}.Encode(), cookie)
	}
	t.Run("browser state one use and member scope", func(t *testing.T) {
		state, cookie := start()
		wrong := *cookie
		wrong.Value = crmRandom()
		w := callback("employee-a", state, &wrong)
		if !strings.Contains(w.Header().Get("Location"), "expired") {
			t.Fatal("cross-browser state accepted")
		}
		w = callback("employee-a", state, cookie)
		if w.Header().Get("Location") != cfg.Frontend+"/admin/customer-management" {
			t.Fatal("member login failed", w.Header())
		}
		var loginCookie *http.Cookie
		for _, c := range w.Result().Cookies() {
			if c.Name == "sessionid" {
				loginCookie = c
				if !c.Secure || !c.HttpOnly {
					t.Fatal("insecure session cookie")
				}
			}
		}
		if loginCookie == nil {
			t.Fatal("missing login session")
		}
		if request("GET", "/api/v1/management/me", loginCookie).Code != 200 {
			t.Fatal("valid member session refused")
		}
		if request("GET", "/api/v1/management/orders", loginCookie).Code != 403 {
			t.Fatal("employee gained platform scope")
		}
		if !strings.Contains(callback("employee-a", state, cookie).Header().Get("Location"), "expired") {
			t.Fatal("state replay accepted")
		}
		exec(`UPDATE management_wecom_identity SET enabled=FALSE,version=version+1 WHERE user_id=2`)
		if request("GET", "/api/v1/management/me", loginCookie).Code != 401 {
			t.Fatal("revoked identity session survived")
		}
		if request("POST", "/api/v1/management/logout", loginCookie).Code != 200 {
			t.Fatal("revoked member cannot logout")
		}
		exec(`UPDATE management_wecom_identity SET enabled=TRUE WHERE user_id=2`)
	})
	t.Run("unknown external disabled and expired state", func(t *testing.T) {
		for _, code := range []string{"unknown", "external", "disabled"} {
			state, c := start()
			w := callback(code, state, c)
			if !strings.Contains(w.Header().Get("Location"), "wecom_error=") {
				t.Fatal("unauthorized member logged in", code)
			}
		}
		state, c := start()
		exec(`UPDATE management_wecom_oauth_state SET expires_at=? WHERE state_hash=?`, time.Now().Add(-time.Minute), identityHash(state))
		if !strings.Contains(callback("employee-a", state, c).Header().Get("Location"), "expired") {
			t.Fatal("expired state accepted")
		}
		exec(`UPDATE management_access SET enabled=FALSE WHERE user_id=2`)
		state, c = start()
		if !strings.Contains(callback("employee-a", state, c).Header().Get("Location"), "not_enabled") {
			t.Fatal("disabled platform account logged in")
		}
	})
}

func TestWecomInvalidatedTokenRefreshesOnlyOnce(t *testing.T) {
	cfg := identityTestConfig(t)
	p := identityTestProvider()
	base := p.client.Transport
	tokens, calls := 0, 0
	p.client.Transport = identityTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/gettoken") {
			tokens++
		}
		if strings.HasSuffix(r.URL.Path, "/auth/getuserinfo") {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"errcode":42001}`))}, nil
			}
		}
		return base.RoundTrip(r)
	})
	if _, e := p.exchange(context.Background(), cfg, "employee-a"); e != nil || tokens != 2 || calls != 2 {
		t.Fatalf("refresh result: %v tokens=%d calls=%d", e, tokens, calls)
	}
	p.client.Transport = identityTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/gettoken") {
			return base.RoundTrip(r)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"errcode":40014}`))}, nil
	})
	if _, e := p.exchange(context.Background(), cfg, "employee-a"); e == nil {
		t.Fatal("permanent token error accepted")
	}
}

func resetRosterCache() {
	wecomRosterCache.Lock()
	wecomRosterCache.entries = map[string]wecomRosterEntry{}
	wecomRosterCache.Unlock()
}

func expireRosterEntry(corp, id string) {
	key := corp + "\n" + id
	wecomRosterCache.Lock()
	if e, ok := wecomRosterCache.entries[key]; ok {
		e.until = time.Now().Add(-time.Second)
		wecomRosterCache.entries[key] = e
	}
	wecomRosterCache.Unlock()
}

// B2 在职复核：明确离职拒绝、新鲜缓存抑制外部调用、降级（沿用旧值 / 无缓存放行）。
func TestWecomRosterRecheck(t *testing.T) {
	cfg := identityTestConfig(t)
	p := identityTestProvider()
	ctx := context.Background()
	resetRosterCache()
	defer resetRosterCache()
	t.Run("active cached without repeated calls", func(t *testing.T) {
		calls := 0
		base := p.client.Transport
		p.client.Transport = identityTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return base.RoundTrip(r)
		})
		if ok, _ := p.ensureRosterActive(ctx, cfg, "employee-a"); !ok || calls == 0 {
			t.Fatalf("active member refused calls=%d", calls)
		}
		first := calls
		if ok, _ := p.ensureRosterActive(ctx, cfg, "employee-a"); !ok || calls != first {
			t.Fatal("fresh cache did not suppress provider call")
		}
	})
	t.Run("explicit departure refused", func(t *testing.T) {
		if ok, reason := p.ensureRosterActive(ctx, cfg, "disabled"); ok || reason == "" {
			t.Fatal("disabled member accepted")
		}
		if ok, _ := p.ensureRosterActive(ctx, cfg, "missing"); ok {
			t.Fatal("missing member accepted")
		}
	})
	t.Run("stale value carried during outage", func(t *testing.T) {
		if ok, _ := p.ensureRosterActive(ctx, cfg, "missing"); ok {
			t.Fatal("missing member accepted")
		}
		expireRosterEntry(cfg.Corp, "missing")
		p.client.Transport = identityTransport(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("network down") })
		if ok, _ := p.ensureRosterActive(ctx, cfg, "missing"); ok {
			t.Fatal("stale inactive not carried during outage")
		}
	})
	t.Run("no cache fail-open during outage", func(t *testing.T) {
		p.client.Transport = identityTransport(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("network down") })
		if ok, _ := p.ensureRosterActive(ctx, cfg, "flaky"); !ok {
			t.Fatal("uncached outage not fail-open")
		}
	})
}
