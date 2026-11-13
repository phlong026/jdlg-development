package route

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"service/tracking"
	"testing"
)

func customerLinkedProjectRouter(t *testing.T, verify func(context.Context, tracking.Scope, string) error) *gin.Engine {
	t.Helper()
	_, svc, scope, _ := newProjectNoteHandlerFixture(t)
	router := gin.New()
	root := router.Group("/api/v1/miniprogram/project-notes")
	manage := root.Group("", func(c *gin.Context) { c.Set(projectNoteScopeKey, *scope); c.Next() })
	registerProjectNoteHandlers(root, manage, svc, projectNoteHandlerDeps{actor: func(*gin.Context) int64 { return scope.Actor }, verifyCustomerLink: verify})
	return router
}
func TestProjectCustomerLinkRequiresTrustedVerifier(t *testing.T) {
	router := customerLinkedProjectRouter(t, nil)
	denied := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "关联项目", "owner_id": "owner-a", "customer_ref": "u:101"})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("missing verifier must deny customer link: %d %s", denied.Code, denied.Body.String())
	}
	plain := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "普通项目", "owner_id": "owner-a"})
	if plain.Code != http.StatusOK {
		t.Fatalf("unlinked project unavailable: %d %s", plain.Code, plain.Body.String())
	}
}
func TestProjectCustomerLinkChecksActorBeforePersisting(t *testing.T) {
	calls := 0
	router := customerLinkedProjectRouter(t, func(_ context.Context, scope tracking.Scope, ref string) error {
		calls++
		if scope.Actor != 7 || ref != "u:101" {
			return &tracking.Error{Status: 403, Message: "客户关联不可访问"}
		}
		return nil
	})
	accepted := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "授权客户项目", "owner_id": "owner-a", "customer_ref": "u:101"})
	if accepted.Code != 200 {
		t.Fatalf("authorized link failed: %d %s", accepted.Code, accepted.Body.String())
	}
	data := projectNoteData[map[string]any](t, accepted)
	if data["customer_ref"] != "u:101" || calls != 1 {
		t.Fatalf("link not verified/persisted: calls=%d data=%v", calls, data)
	}
	denied := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "他人客户", "owner_id": "owner-a", "customer_ref": "u:102"})
	if denied.Code != 403 || calls != 2 {
		t.Fatalf("foreign link accepted: %d %s", denied.Code, denied.Body.String())
	}
	crm := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "伪造CRM", "owner_id": "owner-a", "customer_ref": "u:101", "crm_id": "another-channel"})
	if crm.Code != 403 || calls != 2 {
		t.Fatalf("CRM boundary changed: %d calls=%d", crm.Code, calls)
	}
}
