package route

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func TestProjectInvitationRoutesKeepScopedLegacyGrantDuringClientTransition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{Tenant: "project-notes", Enabled: true})
	registerProjectNoteRoutes(router.Group("/api/v1"), svc, &model.UserModel{}, nil)
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"POST /api/v1/miniprogram/project-notes/projects/:id/member-invitations",
		"GET /api/v1/miniprogram/project-notes/projects/:id/member-invitations",
		"POST /api/v1/miniprogram/project-notes/member-invitations/:token/apply",
		"POST /api/v1/miniprogram/project-notes/projects/:id/member-requests/:request/approve",
	} {
		if !routes[want] {
			t.Fatalf("missing invitation route %s", want)
		}
	}
	if !routes["POST /api/v1/miniprogram/project-notes/projects/:id/members/:account"] {
		t.Fatal("legacy clients still require the scoped member grant route during transition")
	}
}

func TestProjectInvitationRouteRequiresExplicitDesignerApproval(t *testing.T) {
	router, _, _, actor := newProjectNoteHandlerFixture(t)
	projectRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "张宅方案", "owner_id": "owner-a"})
	project := projectNoteData[map[string]any](t, projectRec)
	projectID, _ := project["project_id"].(string)
	issuedRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+projectID+"/member-invitations", map[string]any{
		"expires_at": "2026-10-27T09:00:00Z", "idempotency_key": "route-invite-create-1",
	})
	if issuedRec.Code != http.StatusOK {
		t.Fatal(issuedRec.Code, issuedRec.Body.String())
	}
	issued := projectNoteData[map[string]any](t, issuedRec)
	token, _ := issued["invitation_token"].(string)
	if token == "" {
		t.Fatal("invitation token missing")
	}

	*actor = 101
	previewRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/member-invitations/"+token, nil)
	if previewRec.Code != http.StatusOK || strings.Contains(previewRec.Body.String(), "project_id") || strings.Contains(previewRec.Body.String(), "fixed") {
		t.Fatalf("unsafe preview = %d %s", previewRec.Code, previewRec.Body.String())
	}
	applyRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/member-invitations/"+token+"/apply", map[string]any{"idempotency_key": "route-invite-apply-1"})
	if applyRec.Code != http.StatusOK {
		t.Fatal(applyRec.Code, applyRec.Body.String())
	}
	request := projectNoteData[map[string]any](t, applyRec)
	requestID, _ := request["request_id"].(string)

	*actor = 7
	requestsRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+projectID+"/member-requests", nil)
	if requestsRec.Code != http.StatusOK || !strings.Contains(requestsRec.Body.String(), "张女士") || !strings.Contains(requestsRec.Body.String(), "member.png") {
		t.Fatalf("requests = %d %s", requestsRec.Code, requestsRec.Body.String())
	}
	approveRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+projectID+"/member-requests/"+requestID+"/approve", map[string]any{"idempotency_key": "route-invite-approve-1"})
	if approveRec.Code != http.StatusOK {
		t.Fatal(approveRec.Code, approveRec.Body.String())
	}
	detailRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+projectID, nil)
	if detailRec.Code != http.StatusOK || !strings.Contains(detailRec.Body.String(), `"actor_id":101`) {
		t.Fatalf("approved member missing = %d %s", detailRec.Code, detailRec.Body.String())
	}
}

func TestProjectInvitationRouteSupportsRevocationAndApplyRetry(t *testing.T) {
	router, _, _, actor := newProjectNoteHandlerFixture(t)
	projectRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "张宅方案", "owner_id": "owner-a"})
	project := projectNoteData[map[string]any](t, projectRec)
	projectID, _ := project["project_id"].(string)
	issuedRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+projectID+"/member-invitations", map[string]any{
		"expires_at": "2026-10-27T09:00:00Z", "idempotency_key": "route-invite-create-2",
	})
	issued := projectNoteData[map[string]any](t, issuedRec)
	token, _ := issued["invitation_token"].(string)
	invitationID, _ := issued["invitation_id"].(string)
	listRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+projectID+"/member-invitations", nil)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), invitationID) || strings.Contains(listRec.Body.String(), token) || !strings.Contains(listRec.Body.String(), `"status":"active"`) {
		t.Fatalf("safe active invitation list = %d %s", listRec.Code, listRec.Body.String())
	}
	*actor = 101
	first := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/member-invitations/"+token+"/apply", map[string]any{"idempotency_key": "route-invite-apply-2"})
	second := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/member-invitations/"+token+"/apply", map[string]any{"idempotency_key": "route-invite-apply-2"})
	if first.Code != http.StatusOK || second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"replayed":true`) {
		t.Fatalf("apply retry = %d/%d %s", first.Code, second.Code, second.Body.String())
	}
	*actor = 7
	revoked := projectNoteRequest(t, router, http.MethodDelete, "/api/v1/miniprogram/project-notes/projects/"+projectID+"/member-invitations/"+invitationID, nil)
	if revoked.Code != http.StatusOK {
		t.Fatal(revoked.Code, revoked.Body.String())
	}
	listRec = projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+projectID+"/member-invitations", nil)
	if listRec.Code != http.StatusOK || strings.Contains(listRec.Body.String(), invitationID) {
		t.Fatalf("revoked invitation still listed = %d %s", listRec.Code, listRec.Body.String())
	}
	*actor = 102
	denied := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/member-invitations/"+token+"/apply", map[string]any{"idempotency_key": "route-invite-apply-3"})
	if denied.Code != http.StatusNotFound {
		t.Fatalf("revoked apply = %d %s", denied.Code, denied.Body.String())
	}
}

func TestProjectInvitationApprovalFailsClosedWithoutAccountVerifier(t *testing.T) {
	base, svc, scope, actor := newProjectNoteHandlerFixture(t)
	_ = base
	project, err := svc.ProjectNoteCreateProject(t.Context(), *scope, tracking.ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.ProjectNoteCreateInvitation(t.Context(), *scope, project.ID, svc.Now().Add(time.Hour), "route-missing-verifier-create")
	if err != nil {
		t.Fatal(err)
	}
	request, err := svc.ProjectNoteApplyInvitation(t.Context(), issued.Token, 101, "route-missing-verifier-apply")
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	root := router.Group("/api/v1/miniprogram/project-notes")
	manage := root.Group("", func(c *gin.Context) { c.Set(projectNoteScopeKey, *scope); c.Next() })
	registerProjectNoteHandlers(root, manage, svc, projectNoteHandlerDeps{actor: func(*gin.Context) int64 { return *actor }})
	rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/member-requests/"+request.ID+"/approve", map[string]any{"idempotency_key": "route-missing-verifier-approve"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing account verifier must fail closed: %d %s", rec.Code, rec.Body.String())
	}
}
