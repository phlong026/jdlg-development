package route

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

// The visit timeline sits inside the auth-protected, no-store management
// group like the customer list it replaces.
func TestTrackingVisitsEndpointRegisteredBehindAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("test", cookie.NewStore([]byte("test-only-cookie-signing-key-32-bytes"))))
	RegisterTrackingRoutes(router.Group("/api/v1"), &model.UserModel{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/management/tracking/visits?channel=timeline", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("cache header %q", got)
	}
}

func TestTrackingFilterReadsVisitChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/visits?channel=code_link&intent=high&group=day", nil)
	f, e := trackingFilter(c)
	if e != nil {
		t.Fatal(e)
	}
	if f.Channel != "code_link" || f.Intent != "high" || f.Group != "day" {
		t.Fatalf("filter = %+v, want channel code_link, intent high and group day", f)
	}
}

func TestAnnotateVisitRiskTagsWithoutUserStoreKeepsList(t *testing.T) {
	list, e := annotateVisitRiskTags(nil, tracking.List{Items: []map[string]any{{"visit_id": "s:c:1", "visitor": map[string]any{"visitor_key": "u:1", "identity_state": "identified"}}}})
	if e != nil {
		t.Fatal(e)
	}
	items, ok := list.Items.([]map[string]any)
	if !ok || len(items) != 1 || items[0]["visit_id"] != "s:c:1" {
		t.Fatalf("items = %#v, want unchanged visit", list.Items)
	}
}
