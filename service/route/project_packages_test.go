package route

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func registerProjectPackageTestHandlers(router *gin.Engine, svc *tracking.Service, scope *tracking.Scope, actor *int64) {
	manage := router.Group("/api/v1/miniprogram/project-notes", func(c *gin.Context) {
		c.Set(projectNoteScopeKey, *scope)
		c.Next()
	})
	shared := router.Group("/api/v1/miniprogram/project-packages", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.Next()
	})
	registerProjectPackageHandlers(manage, shared, svc, func(*gin.Context) int64 { return *actor })
}

func createProjectPackage(t *testing.T, router *gin.Engine, project tracking.ProjectNoteProject, version tracking.ProjectNoteVersion) tracking.ProjectPackageDelivery {
	t.Helper()
	draftRec := projectNoteRequest(t, router, http.MethodPut, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/packages/draft", map[string]any{
		"version_ids": []string{version.ID}, "recipient_ids": []int64{101}, "idempotency_key": "package-route-draft-1",
	})
	if draftRec.Code != http.StatusOK {
		t.Fatal(draftRec.Code, draftRec.Body.String())
	}
	draft := projectNoteData[tracking.ProjectPackageDraft](t, draftRec)
	previewRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/packages/"+draft.ID+"/preview", nil)
	if previewRec.Code != http.StatusOK {
		t.Fatal(previewRec.Code, previewRec.Body.String())
	}
	preview := projectNoteData[tracking.ProjectPackagePreview](t, previewRec)
	publishRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/packages/"+draft.ID+"/publish", map[string]any{
		"expected_revision": draft.Revision, "preview_signature": preview.Signature,
		"expires_at": time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC), "idempotency_key": "package-route-publish-1",
	})
	if publishRec.Code != http.StatusOK {
		t.Fatal(publishRec.Code, publishRec.Body.String())
	}
	return projectNoteData[tracking.ProjectPackageDelivery](t, publishRec)
}

func TestProjectPackageRoutesKeepManagerScopeAndInvitedActorSeparate(t *testing.T) {
	router, svc, scope, actor := newProjectNoteHandlerFixture(t)
	registerProjectPackageTestHandlers(router, svc, scope, actor)
	project, _, version, _ := createProjectNoteShare(t, router)
	delivery := createProjectPackage(t, router, project, version)
	if delivery.Token == "" {
		t.Fatal("publish did not return the one-time package token")
	}

	*actor = 101
	sharedRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-packages/shared/"+delivery.Token, nil)
	if sharedRec.Code != http.StatusOK {
		t.Fatal(sharedRec.Code, sharedRec.Body.String())
	}
	view := projectNoteData[tracking.ProjectPackageView](t, sharedRec)
	if len(view.Items) != 1 || view.Items[0].VersionID != version.ID || len(view.Delivery.RecipientIDs) != 0 {
		t.Fatalf("unexpected invited directory: %#v", view)
	}

	*actor = 102
	denied := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-packages/shared/"+delivery.Token, nil)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unselected member status = %d, want 403: %s", denied.Code, denied.Body.String())
	}
}

func TestProjectPackageRoutesBindChildActionsToExactVersion(t *testing.T) {
	router, svc, scope, actor := newProjectNoteHandlerFixture(t)
	registerProjectPackageTestHandlers(router, svc, scope, actor)
	project, _, version, _ := createProjectNoteShare(t, router)
	if rec := projectNoteRequest(t, router, http.MethodPut, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/confirmer", map[string]any{"account_id": 101}); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	delivery := createProjectPackage(t, router, project, version)
	*actor = 101

	versionPath := "/api/v1/miniprogram/project-packages/shared/" + delivery.Token + "/versions/" + version.ID
	if rec := projectNoteRequest(t, router, http.MethodGet, versionPath, nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	feedbackRec := projectNoteRequest(t, router, http.MethodPost, versionPath+"/feedback", map[string]any{
		"text": "调整门厅", "satisfaction": tracking.ProjectNoteSatisfactionNeedsAdjustment, "idempotency_key": "package-route-feedback-1",
	})
	if feedbackRec.Code != http.StatusOK {
		t.Fatal(feedbackRec.Code, feedbackRec.Body.String())
	}
	confirmRec := projectNoteRequest(t, router, http.MethodPost, versionPath+"/confirm", map[string]any{"idempotency_key": "package-route-confirm-1"})
	if confirmRec.Code != http.StatusOK {
		t.Fatal(confirmRec.Code, confirmRec.Body.String())
	}

	missingAck := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-packages/shared/"+delivery.Token+"/receipts", map[string]any{"event_id": "package-route-receipt-1", "version_id": version.ID})
	if missingAck.Code != http.StatusBadRequest {
		t.Fatalf("implicit receipt status = %d, want 400", missingAck.Code)
	}
	ack := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-packages/shared/"+delivery.Token+"/receipts", map[string]any{"event_id": "package-route-receipt-1", "version_id": version.ID, "acknowledged": true})
	if ack.Code != http.StatusOK {
		t.Fatal(ack.Code, ack.Body.String())
	}

	outside := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-packages/shared/"+delivery.Token+"/versions/not-in-manifest", nil)
	if outside.Code != http.StatusNotFound {
		t.Fatalf("outside manifest status = %d, want 404", outside.Code)
	}
}

func TestProjectPackageRoutesRejectPathBodyIdentityMismatch(t *testing.T) {
	router, svc, scope, actor := newProjectNoteHandlerFixture(t)
	registerProjectPackageTestHandlers(router, svc, scope, actor)
	project, _, version, _ := createProjectNoteShare(t, router)
	rec := projectNoteRequest(t, router, http.MethodPut, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/packages/draft", map[string]any{
		"project_id": "another-project", "version_ids": []string{version.ID}, "recipient_ids": []int64{101}, "idempotency_key": "package-route-mismatch-1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("path/body mismatch status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestProjectPackageRoutesPreviewAndFreezeSavedNote(t *testing.T) {
	router, svc, scope, actor := newProjectNoteHandlerFixture(t)
	registerProjectPackageTestHandlers(router, svc, scope, actor)
	project, note, _, _ := createProjectNoteShare(t, router)
	draftRec := projectNoteRequest(t, router, http.MethodPut, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/packages/draft", map[string]any{
		"note_selections": []map[string]any{{"note_id": note.ID, "revision": note.Revision}},
		"recipient_ids":   []int64{101}, "idempotency_key": "package-route-note-draft-1",
	})
	if draftRec.Code != http.StatusOK {
		t.Fatal(draftRec.Code, draftRec.Body.String())
	}
	draft := projectNoteData[tracking.ProjectPackageDraft](t, draftRec)
	previewRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/packages/"+draft.ID+"/preview", nil)
	preview := projectNoteData[tracking.ProjectPackagePreview](t, previewRec)
	if previewRec.Code != http.StatusOK || len(preview.Items) != 1 || !preview.Items[0].PendingFreeze {
		t.Fatalf("preview = %#v, status=%d", preview, previewRec.Code)
	}
	publishRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/packages/"+draft.ID+"/publish", map[string]any{
		"expected_revision": draft.Revision, "preview_signature": preview.Signature,
		"expires_at": time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC), "idempotency_key": "package-route-note-publish-1",
	})
	delivery := projectNoteData[tracking.ProjectPackageDelivery](t, publishRec)
	if publishRec.Code != http.StatusOK || len(delivery.Items) != 1 || delivery.Items[0].VersionID == "" || delivery.Items[0].PendingFreeze {
		t.Fatalf("delivery = %#v, status=%d", delivery, publishRec.Code)
	}
}

func TestProjectNoteRouteRegistrationIncludesIndependentPackageRecipientRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{Tenant: "project-notes", Enabled: true})
	registerProjectNoteRoutes(router.Group("/api/v1"), svc, &model.UserModel{}, nil)
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"PUT /api/v1/miniprogram/project-notes/projects/:id/packages/draft",
		"POST /api/v1/miniprogram/project-notes/shared/:token/media/:id/playback",
		"GET /api/v1/miniprogram/project-packages/shared/:token",
		"GET /api/v1/miniprogram/project-packages/shared/:token/versions/:version",
		"POST /api/v1/miniprogram/project-packages/shared/:token/versions/:version/media/:id/playback",
		"GET /api/v1/shared-video/:ticket",
	} {
		if !routes[want] {
			t.Fatalf("missing registered package route %s", want)
		}
	}
}
