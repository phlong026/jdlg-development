package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"service/config"
)

func corsTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORSMiddleware())
	r.GET("/ping", func(c *gin.Context) { c.String(200, "ok") })
	return r
}

func TestCORSProductionReflectsOnlyAllowlistedOrigins(t *testing.T) {
	t.Setenv("ENV", "production")
	config.Init()
	t.Cleanup(func() {
		t.Setenv("ENV", "development")
		config.Init()
		t.Setenv("ADMIN_ALLOWED_ORIGINS", "")
		t.Setenv("CRM_ALLOWED_ORIGINS", "")
	})
	t.Setenv("ADMIN_ALLOWED_ORIGINS", "https://admin.example.test")
	t.Setenv("CRM_ALLOWED_ORIGINS", "https://crm.example.test")
	config.Init()
	r := corsTestRouter(t)

	// 白名单内的来源回显并允许凭据
	req := httptest.NewRequest("GET", "http://localhost/ping", nil)
	req.Header.Set("Origin", "https://admin.example.test")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.test" {
		t.Fatalf("allowlisted origin: got ACAO %q", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allowlisted origin: got credentials %q", got)
	}

	// 陌生来源不得拿到任何 CORS 头，否则已登录管理员会话可被跨源读取
	req = httptest.NewRequest("GET", "http://localhost/ping", nil)
	req.Header.Set("Origin", "https://evil.example")
	res = httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unknown origin must not receive ACAO, got %q", got)
	}

	// 未配置白名单的生产环境同样不下发 CORS 头（同源请求不带 Origin，不受影响）
	t.Setenv("ADMIN_ALLOWED_ORIGINS", "")
	t.Setenv("CRM_ALLOWED_ORIGINS", "")
	_ = config.Init()
	req = httptest.NewRequest("GET", "http://localhost/ping", nil)
	req.Header.Set("Origin", "https://admin.example.test")
	res = httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("production without allowlist must not reflect origin, got %q", got)
	}

	// 预检请求在拒绝来源时也不得泄露允许头
	req = httptest.NewRequest("OPTIONS", "http://localhost/ping", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	res = httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS should short-circuit 204, got %d", res.Code)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("preflight from unknown origin must not receive ACAO, got %q", got)
	}
}
