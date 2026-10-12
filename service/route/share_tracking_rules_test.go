package route

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"service/tracking"
	"strings"
	"testing"
)

func TestTrackingRuleRoutesRejectUnscopedWritesAndUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		method, path, body string
		rules              bool
		status             int
	}{
		{"POST", "/intent-rules?owner=foreign", `{}`, true, 403},
		{"POST", "/intent-rules", `{"expected_rule_version":"followup-v1","reason":"test","window_days":30,"medium_sessions":2,"medium_favorites":1,"high_business_results":1,"daily_session_cap":10,"daily_action_cap":1}`, false, 403},
		{"POST", "/intent-rules", `{"tenant_id":"other"}`, true, 400},
		{"POST", "/intent-rules/recompute", `{"request_id":"recompute01","reason":"test","rule_version":"followup-v1"}`, false, 403},
		{"GET", "/intent-rules?owner=foreign", "", true, 403},
	}
	for _, tc := range tests {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			router := gin.New()
			group := router.Group("")
			group.Use(func(c *gin.Context) {
				c.Set("tracking.scope", tracking.Scope{Tenant: "tenant", Actor: 7, Owners: []string{"owner"}, View: true, Rules: tc.rules})
			})
			registerTrackingRuleRoutes(group, tracking.New(nil, tracking.Config{Tenant: "tenant", IntentEnabled: true}))
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
