package route

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"service/tracking"
)

func TestNativeReferenceWorkbenchRoutesKeepShareCredentialsServerOwned(t *testing.T) {
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	r, svc := newWorkbenchHandlerFixture(t, &scope)
	content, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "原生引用", Body: "正文", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), scope, content.ID, content.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Publish(context.Background(), scope, content.ID, content.Revision, preview["preview_token"].(string), "route-native-publish"); err != nil {
		t.Fatal(err)
	}

	forged := workbenchRequest(t, r, http.MethodPost, "/workbench/native-references", map[string]any{
		"content_id": content.ID, "content_version": 1, "idempotency_key": "route-native-ref", "owner_id": "owner-b", "share_token": "forged", "sharer_id": "u:999",
	})
	if forged.Code != http.StatusBadRequest {
		t.Fatalf("forged fields accepted: %d %s", forged.Code, forged.Body.String())
	}
	created := workbenchRequest(t, r, http.MethodPost, "/workbench/native-references", map[string]any{"content_id": content.ID, "content_version": 1, "idempotency_key": "route-native-ref"})
	if created.Code != http.StatusOK || strings.Contains(created.Body.String(), "owner_id") || strings.Contains(created.Body.String(), "share_token") || strings.Contains(created.Body.String(), "sharer_id") {
		t.Fatalf("created=%d %s", created.Code, created.Body.String())
	}
	var response struct {
		Data tracking.NativeReference `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil || response.Data.ReferenceID == "" {
		t.Fatal(err, created.Body.String())
	}

	candidates := workbenchRequest(t, r, http.MethodGet, "/workbench/native-reference-candidates?limit=10&q=%E5%8E%9F%E7%94%9F", nil)
	listed := workbenchRequest(t, r, http.MethodGet, "/workbench/native-references?limit=10", nil)
	target := workbenchRequest(t, r, http.MethodGet, "/workbench/native-references/"+response.Data.ReferenceID+"/target", nil)
	if candidates.Code != http.StatusOK || listed.Code != http.StatusOK || target.Code != http.StatusOK {
		t.Fatalf("candidates=%d %s listed=%d %s target=%d %s", candidates.Code, candidates.Body.String(), listed.Code, listed.Body.String(), target.Code, target.Body.String())
	}
	if !strings.Contains(target.Body.String(), "pages/contentreader/contentreader?content_id=") || !strings.Contains(target.Body.String(), "content_version=1") || strings.Contains(target.Body.String(), "st=") {
		t.Fatalf("unsafe target: %s", target.Body.String())
	}
	removed := workbenchRequest(t, r, http.MethodDelete, "/workbench/native-references/"+response.Data.ReferenceID, nil)
	if removed.Code != http.StatusOK {
		t.Fatalf("delete=%d %s", removed.Code, removed.Body.String())
	}
}

func TestNativeReferenceRoutesUseWorkbenchFeatureAndManageGrant(t *testing.T) {
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: false}
	r, _ := newWorkbenchHandlerFixture(t, &scope)
	if got := workbenchRequest(t, r, http.MethodGet, "/workbench/native-references", nil); got.Code != http.StatusForbidden {
		t.Fatalf("read without manage=%d %s", got.Code, got.Body.String())
	}
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "false")
	scope.Manage = true
	if got := workbenchRequest(t, r, http.MethodPost, "/workbench/native-references", map[string]any{"content_id": "content-123", "content_version": 1, "idempotency_key": "route-native-disabled"}); got.Code != http.StatusForbidden {
		t.Fatalf("write while disabled=%d %s", got.Code, got.Body.String())
	}
}
