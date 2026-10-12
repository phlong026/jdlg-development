package route

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"service/tracking"
	"strings"
	"testing"
)

func TestTrackingCRMTargetRouteOwnerAndCurrentPermission(t *testing.T) {
	for _, tc := range []struct {
		name, query, key string
		crm              bool
		status           int
	}{
		{"explicit", "?owner=owner-a", "u:42", true, 200}, {"ambiguous", "", "u:42", true, 409}, {"foreign", "?owner=foreign", "u:42", true, 403}, {"duplicate", "?owner=owner-a&owner=owner-b", "u:42", true, 400}, {"empty", "?owner=", "u:42", true, 400}, {"no-crm", "?owner=owner-a", "u:42", false, 403}, {"anonymous", "?owner=owner-a", "a:anon", true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, svc := governanceRouteFixture(t, []string{"owner-a", "owner-b"})
			scope := tracking.Scope{Tenant: svc.Config.Tenant, Actor: 7, Owners: []string{"owner-a", "owner-b"}, View: true, CRM: tc.crm}
			calls := 0
			svc.VerifyCRM = func(_ context.Context, s tracking.Scope, _, customer, number string) error {
				calls++
				if len(s.Owners) != 1 || s.Owners[0] != "owner-a" || customer != "crm-owner-a" || number != "number-owner-a" {
					t.Fatal("wrong source target")
				}
				return nil
			}
			r := gin.New()
			g := r.Group("/tracking")
			g.Use(func(c *gin.Context) { c.Set("tracking.scope", scope); c.Next() })
			registerTrackingCRMTargetRoutes(g, svc)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/tracking/visitors/"+tc.key+"/crm-target"+tc.query, nil))
			if w.Code != tc.status {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			if tc.status == 200 {
				if calls != 1 || !strings.Contains(w.Body.String(), `"target_kind":"customer"`) || w.Header().Get("Cache-Control") != "private, no-store" {
					t.Fatal(w.Body.String())
				}
			} else if calls != 0 {
				t.Fatal("unauthorized source request")
			}
		})
	}
}
