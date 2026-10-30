package route

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

func newProjectNoteLifecycleRouteFixture(t *testing.T) (*gin.Engine, *tracking.Service, tracking.Scope) {
	t.Helper()
	_, svc, scope, _ := newProjectNoteHandlerFixture(t)
	router := gin.New()
	manage := router.Group("/api/v1/miniprogram/project-notes", func(c *gin.Context) {
		c.Set(projectNoteScopeKey, *scope)
		c.Next()
	})
	registerProjectNoteLifecycleHandlers(manage, svc)
	return router, svc, *scope
}

func TestProjectNoteLifecycleRoutesCopyAndSoftDelete(t *testing.T) {
	router, svc, scope := newProjectNoteLifecycleRouteFixture(t)
	project, err := svc.ProjectNoteCreateProject(context.Background(), scope, tracking.ProjectNoteProjectInput{Name: "生命周期方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := svc.ProjectNoteCreateNote(context.Background(), scope, project.ID, tracking.ProjectNoteDraft{Title: "原笔记", Body: "仅复制正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}

	copyRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/notes/"+note.ID+"/copy", map[string]any{"expected_revision": note.Revision})
	if copyRec.Code != http.StatusOK {
		t.Fatal(copyRec.Code, copyRec.Body.String())
	}
	copied := projectNoteData[tracking.ProjectNote](t, copyRec)
	if copied.ID == note.ID || copied.Title != note.Title || copied.Body != note.Body || copied.ProjectID != project.ID {
		t.Fatalf("unexpected copy response: %#v", copied)
	}

	deleteRec := projectNoteRequest(t, router, http.MethodDelete, "/api/v1/miniprogram/project-notes/notes/"+note.ID, map[string]any{"expected_revision": note.Revision})
	if deleteRec.Code != http.StatusOK {
		t.Fatal(deleteRec.Code, deleteRec.Body.String())
	}
	deleted := projectNoteData[tracking.ProjectNoteDeletion](t, deleteRec)
	if deleted.NoteID != note.ID || deleted.DeletedBy != scope.Actor {
		t.Fatalf("unexpected delete response: %#v", deleted)
	}
}

func TestProjectNoteLifecycleRoutesRejectUnknownFieldsAndStaleRevision(t *testing.T) {
	router, svc, scope := newProjectNoteLifecycleRouteFixture(t)
	project, err := svc.ProjectNoteCreateProject(context.Background(), scope, tracking.ProjectNoteProjectInput{Name: "冲突方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := svc.ProjectNoteCreateNote(context.Background(), scope, project.ID, tracking.ProjectNoteDraft{Title: "冲突笔记", Body: "正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}

	unknown := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/notes/"+note.ID+"/copy", map[string]any{"expected_revision": note.Revision, "share_token": "forged"})
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown field accepted: %d %s", unknown.Code, unknown.Body.String())
	}
	stale := projectNoteRequest(t, router, http.MethodDelete, "/api/v1/miniprogram/project-notes/notes/"+note.ID, map[string]any{"expected_revision": note.Revision + 1})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale delete accepted: %d %s", stale.Code, stale.Body.String())
	}
}

func TestProjectNoteLifecycleRoutesAreRegisteredWithProjectNotes(t *testing.T) {
	router, svc, scope, _ := newProjectNoteHandlerFixture(t)
	project, err := svc.ProjectNoteCreateProject(context.Background(), *scope, tracking.ProjectNoteProjectInput{Name: "注册方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := svc.ProjectNoteCreateNote(context.Background(), *scope, project.ID, tracking.ProjectNoteDraft{Title: "注册笔记", Body: "正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/notes/"+note.ID+"/copy", map[string]any{"expected_revision": note.Revision})
	if rec.Code != http.StatusOK {
		t.Fatalf("lifecycle routes not registered: %d %s", rec.Code, rec.Body.String())
	}
}
