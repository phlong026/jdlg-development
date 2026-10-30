package route

import (
	"net/http"
	"net/url"
	"testing"

	"service/tracking"
)

func TestProjectNoteHistoryRoutesPageManagerAndSharedFacts(t *testing.T) {
	router, _, _, actor := newProjectNoteHandlerFixture(t)
	project, note, version, share := createProjectNoteShare(t, router)

	*actor = 101
	var feedback tracking.ProjectNoteFeedback
	for index, text := range []string{"反馈一", "反馈二"} {
		rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shared/"+url.PathEscape(share.Token)+"/feedback", map[string]any{
			"text": text, "idempotency_key": []string{"history-route-feedback-1", "history-route-feedback-2"}[index],
		})
		if rec.Code != http.StatusOK {
			t.Fatal(rec.Code, rec.Body.String())
		}
		feedback = projectNoteData[tracking.ProjectNoteFeedback](t, rec)
	}

	sharedRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/shared/"+url.PathEscape(share.Token)+"/history/feedbacks?limit=1", nil)
	if sharedRec.Code != http.StatusOK {
		t.Fatal(sharedRec.Code, sharedRec.Body.String())
	}
	type feedbackPage struct {
		Type       string                         `json:"type"`
		Items      []tracking.ProjectNoteFeedback `json:"items"`
		NextCursor string                         `json:"next_cursor"`
	}
	shared := projectNoteData[feedbackPage](t, sharedRec)
	if len(shared.Items) != 1 || shared.Items[0].VersionID != version.ID || shared.NextCursor == "" {
		t.Fatalf("shared history = %#v", shared)
	}

	*actor = 7
	managerRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/history/feedbacks?limit=1&version_id="+version.ID, nil)
	if managerRec.Code != http.StatusOK {
		t.Fatal(managerRec.Code, managerRec.Body.String())
	}
	manager := projectNoteData[feedbackPage](t, managerRec)
	if len(manager.Items) != 1 || manager.Items[0].ProjectID != project.ID {
		t.Fatalf("manager history = %#v", manager)
	}
	exactRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/feedback/"+feedback.ID, nil)
	if exactRec.Code != http.StatusOK || projectNoteData[tracking.ProjectNoteFeedback](t, exactRec).ID != feedback.ID {
		t.Fatal(exactRec.Code, exactRec.Body.String())
	}
	noteRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/notes/"+note.ID, nil)
	if noteRec.Code != http.StatusOK || projectNoteData[tracking.ProjectNote](t, noteRec).Revision != note.Revision {
		t.Fatal(noteRec.Code, noteRec.Body.String())
	}

	invalidRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/history/feedbacks?limit=bad", nil)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status = %d body=%s", invalidRec.Code, invalidRec.Body.String())
	}
}
