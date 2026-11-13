package route

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"service/tracking"
)

func publishContentForWordExport(t *testing.T, svc *tracking.Service, scope tracking.Scope) tracking.Content {
	t.Helper()
	ctx := context.Background()
	draft, err := svc.SaveContent(ctx, scope, "", tracking.Draft{Title: "原木极简住宅", Body: "通顶落地窗把阳光请进家。", BodyFormat: "plain", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), "content-word-publish")
	if err != nil {
		t.Fatal(err)
	}
	return published["content"].(tracking.Content)
}

func TestContentWordExportUsesAuthorizedImmutableVersion(t *testing.T) {
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, svc := newWorkbenchHandlerFixture(t, &scope)
	content := publishContentForWordExport(t, svc, scope)
	path := "/workbench/contents/" + content.ID + "/versions/1/export.docx"

	w := requestProjectNoteExport(router, http.MethodGet, path)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	for key, want := range map[string]string{
		"Content-Type":        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"Content-Disposition": `attachment; filename="jiadi-project-note.docx"`,
		"Cache-Control":       "private, no-store",
	} {
		if got := w.Header().Get(key); got != want {
			t.Fatalf("%s=%q, want %q", key, got, want)
		}
	}
	document := exportedDocumentXML(t, w.Body.Bytes())
	if !strings.Contains(document, "原木极简住宅") || !strings.Contains(document, "通顶落地窗把阳光请进家") {
		t.Fatalf("export omitted frozen content: %s", document)
	}

	scope.Owners = []string{"owner-b"}
	denied := requestProjectNoteExport(router, http.MethodGet, path)
	if denied.Code != http.StatusNotFound || strings.Contains(denied.Body.String(), "原木极简住宅") {
		t.Fatalf("foreign owner export = %d %s", denied.Code, denied.Body.String())
	}
}

func TestContentWordExportRejectsDraftAndSupportsHead(t *testing.T) {
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, svc := newWorkbenchHandlerFixture(t, &scope)
	draft, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "未发布", Body: "不得导出", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	draftPath := "/workbench/contents/" + draft.ID + "/versions/1/export.docx"
	denied := requestProjectNoteExport(router, http.MethodGet, draftPath)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("draft export = %d %s", denied.Code, denied.Body.String())
	}

	content := publishContentForWordExport(t, svc, scope)
	path := "/workbench/contents/" + content.ID + "/versions/1/export.docx"
	head := requestProjectNoteExport(router, http.MethodHead, path)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Fatalf("HEAD export = %d headers=%v bytes=%d", head.Code, head.Header(), head.Body.Len())
	}
}
