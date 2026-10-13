package route

import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"service/model"
	"testing"
)

func TestTrackingManagementResponsesNeverCacheAuthorizationFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("test", cookie.NewStore([]byte("test-only-cookie-signing-key-32-bytes"))))
	RegisterTrackingRoutes(router.Group("/api/v1"), &model.UserModel{}, nil)
	for _, tc := range []struct{ method, path string }{{"GET", "/context"}, {"GET", "/visitors"}, {"POST", "/contents"}} {
		req := httptest.NewRequest(tc.method, "/api/v1/management/tracking"+tc.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status %d", tc.method, tc.path, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("%s: cache header %q", tc.path, got)
		}
	}
}
