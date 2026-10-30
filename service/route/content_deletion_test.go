package route

import (
	"encoding/json"
	"net/http"
	"testing"

	"service/tracking"
)

func TestWorkbenchDraftDeletionUsesRevisionAndHidesSoftDeletedContent(t *testing.T) {
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	r, _ := newWorkbenchHandlerFixture(t, &scope)
	created := workbenchRequest(t, r, http.MethodPost, "/workbench/contents", tracking.Draft{Title: "待删除草稿", Body: "draft", Rights: "own", Owner: "owner-a"})
	var saved struct {
		Data tracking.Content `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &saved); err != nil || created.Code != http.StatusOK {
		t.Fatal(created.Code, created.Body.String(), err)
	}
	scope.Owners = []string{"owner-b"}
	foreign := workbenchRequest(t, r, http.MethodDelete, "/workbench/contents/"+saved.Data.ID, map[string]any{"expected_revision": saved.Data.Revision})
	if foreign.Code != http.StatusNotFound {
		t.Fatal(foreign.Code, foreign.Body.String())
	}
	scope.Owners = []string{"owner-a"}
	stale := workbenchRequest(t, r, http.MethodDelete, "/workbench/contents/"+saved.Data.ID, map[string]any{"expected_revision": saved.Data.Revision + 1})
	if stale.Code != http.StatusConflict {
		t.Fatal(stale.Code, stale.Body.String())
	}
	deleted := workbenchRequest(t, r, http.MethodDelete, "/workbench/contents/"+saved.Data.ID, map[string]any{"expected_revision": saved.Data.Revision})
	var result struct {
		Data tracking.ContentDeletion `json:"data"`
	}
	if err := json.Unmarshal(deleted.Body.Bytes(), &result); err != nil || deleted.Code != http.StatusOK || result.Data.ContentID != saved.Data.ID || result.Data.Revision != saved.Data.Revision+1 || result.Data.Status != "deleted" || result.Data.DeletedAt.IsZero() {
		t.Fatal(deleted.Code, deleted.Body.String())
	}
	if reopened := workbenchRequest(t, r, http.MethodGet, "/workbench/contents/"+saved.Data.ID, nil); reopened.Code != http.StatusNotFound {
		t.Fatal(reopened.Code, reopened.Body.String())
	}
}
