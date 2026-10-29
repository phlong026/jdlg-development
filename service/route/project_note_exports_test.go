package route

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"service/notedocx"
	"service/tracking"
)

type projectNoteExportRecorder struct{ *httptest.ResponseRecorder }

func exportTinyPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func (*projectNoteExportRecorder) SetWriteDeadline(time.Time) error { return nil }
func requestProjectNoteExport(router http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(&projectNoteExportRecorder{rec}, httptest.NewRequest(method, path, nil))
	return rec
}
func exportedDocumentXML(t *testing.T, raw []byte) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range reader.File {
		if part.Name == "word/document.xml" {
			file, err := part.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			body, err := io.ReadAll(file)
			if err != nil {
				t.Fatal(err)
			}
			return string(body)
		}
	}
	t.Fatal("Word document part missing")
	return ""
}
func TestProjectNoteExportPinsVersionAndHasSafeDownloadHeaders(t *testing.T) {
	router, svc, scope, actor := newProjectNoteHandlerFixture(t)
	project, note, v1, share := createProjectNoteShare(t, router)
	_, err := svc.ProjectNoteSaveNote(context.Background(), *scope, note.ID, tracking.ProjectNoteDraft{Title: "方案二", Body: "最新草稿不能混入", BodyFormat: "plain"}, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	root := "/api/v1/miniprogram/project-notes"
	exact := projectNoteRequest(t, router, http.MethodGet, root+"/projects/"+project.ID+"/versions/"+v1.ID, nil)
	if exact.Code != 200 || projectNoteData[tracking.ProjectNoteVersion](t, exact).Body != v1.Body {
		t.Fatalf("exact=%d %s", exact.Code, exact.Body.String())
	}
	paths := []string{root + "/projects/" + project.ID + "/versions/" + v1.ID + "/export.docx", root + "/shared/" + share.Token + "/export.docx"}
	for _, path := range paths {
		if strings.Contains(path, "/shared/") {
			*actor = 101
		}
		get := requestProjectNoteExport(router, http.MethodGet, path)
		if get.Code != 200 {
			t.Fatalf("get=%d %s", get.Code, get.Body.String())
		}
		for key, want := range map[string]string{"Content-Type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "Content-Disposition": `attachment; filename="jiadi-project-note.docx"`, "Cache-Control": "private, no-store", "X-Content-Type-Options": "nosniff", "Content-Security-Policy": "default-src 'none'"} {
			if get.Header().Get(key) != want {
				t.Errorf("header %s=%q", key, get.Header().Get(key))
			}
		}
		if get.Header().Get("Content-Length") != strconv.Itoa(get.Body.Len()) {
			t.Fatal("download length mismatch")
		}
		doc := exportedDocumentXML(t, get.Body.Bytes())
		if !strings.Contains(doc, "固定方案一") || strings.Contains(doc, "最新草稿不能混入") || strings.Contains(doc, share.Token) {
			t.Fatalf("unsafe or wrong version: %s", doc)
		}
		head := requestProjectNoteExport(router, http.MethodHead, path)
		if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
			t.Fatalf("head=%d body=%d", head.Code, head.Body.Len())
		}
	}
}

func TestProjectNoteExportIncludesOnlyFrozenKnownProjectFacts(t *testing.T) {
	_, svc, _, _ := newProjectNoteHandlerFixture(t)
	version := tracking.ProjectNoteVersion{Title: "方案", Body: "正文", BodyFormat: "plain", Revision: 2,
		ProjectSnapshot: tracking.ProjectNoteProjectSnapshot{Name: "张宅", Location: "苏州 & 湖畔", BuildingArea: 168.5, Floors: 2, Style: "现代"}}
	data, err := buildProjectNoteDOCX(context.Background(), svc, version, projectNoteExportMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	xml := exportedDocumentXML(t, data)
	for _, want := range []string{"项目：张宅", "地点：苏州 &amp; 湖畔", "建筑面积：168.5 平方米", "层数：2", "风格：现代", "正文"} {
		if !strings.Contains(xml, want) {
			t.Fatalf("missing frozen fact %q", want)
		}
	}
	version.ProjectSnapshot = tracking.ProjectNoteProjectSnapshot{}
	data, err = buildProjectNoteDOCX(context.Background(), svc, version, projectNoteExportMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	xml = exportedDocumentXML(t, data)
	if strings.Contains(xml, "建筑面积：") || strings.Contains(xml, "项目：") {
		t.Fatal("legacy unknown facts must remain absent")
	}
}
func TestProjectNoteExportRejectsUninvitedRemovedExpiredRevokedAndForeignDesigner(t *testing.T) {
	for _, scenario := range []string{"uninvited", "removed", "expired", "revoked", "foreign-designer", "foreign-project"} {
		t.Run(scenario, func(t *testing.T) {
			router, svc, scope, actor := newProjectNoteHandlerFixture(t)
			project, _, version, share := createProjectNoteShare(t, router)
			path := "/api/v1/miniprogram/project-notes/shared/" + share.Token + "/export.docx"
			*actor = 101
			switch scenario {
			case "uninvited":
				*actor = 102
			case "removed":
				if err := svc.ProjectNoteRevokeMember(context.Background(), *scope, project.ID, 101); err != nil {
					t.Fatal(err)
				}
			case "expired":
				svc.Now = func() time.Time { return share.ExpiresAt.Add(time.Second) }
			case "revoked":
				if err := svc.ProjectNoteRevokeShare(context.Background(), *scope, share.ID); err != nil {
					t.Fatal(err)
				}
			case "foreign-designer":
				scope.Actor = 8
				path = "/api/v1/miniprogram/project-notes/projects/" + project.ID + "/versions/" + version.ID + "/export.docx"
			case "foreign-project":
				other, err := svc.ProjectNoteCreateProject(context.Background(), *scope, tracking.ProjectNoteProjectInput{Name: "other", Owner: "owner-a"})
				if err != nil {
					t.Fatal(err)
				}
				path = "/api/v1/miniprogram/project-notes/projects/" + other.ID + "/versions/" + version.ID + "/export.docx"
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				response := requestProjectNoteExport(router, method, path)
				if response.Code < 400 || bytes.HasPrefix(response.Body.Bytes(), []byte("PK")) {
					t.Fatalf("%s admitted=%d", method, response.Code)
				}
			}
		})
	}
}

type projectNoteExportMediaStore struct {
	get func(context.Context, string, string, int64) ([]byte, string, error)
}

func (s projectNoteExportMediaStore) Get(ctx context.Context, tenant, id string, max int64) ([]byte, string, error) {
	return s.get(ctx, tenant, id, max)
}
func (projectNoteExportMediaStore) Put(context.Context, string, string, []byte, string) error {
	return errors.New("unexpected export write")
}

func projectNoteMediaVersion(t *testing.T, svc *tracking.Service, scope tracking.Scope) (tracking.ProjectNoteProject, tracking.ProjectNoteVersion, tracking.ProjectNoteShare) {
	t.Helper()
	ctx := context.Background()
	project, err := svc.ProjectNoteCreateProject(ctx, scope, tracking.ProjectNoteProjectInput{Name: "private image", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectNoteInviteMember(ctx, scope, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	id := "abcdefghijklmnopqrstuvwx12345678"
	asset := tracking.Asset{ID: id, Kind: "image", MIME: "image/png", Width: 2, Height: 3, URL: "/api/v1/miniprogram/project-notes/projects/" + project.ID + "/media/" + id}
	op, _, err := svc.ProjectNotePrepareMedia(ctx, scope, project.ID, asset, strings.Repeat("a", 64), "upload")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectNoteFinalizeMedia(ctx, scope, project.ID, id, op); err != nil {
		t.Fatal(err)
	}
	note, err := svc.ProjectNoteCreateNote(ctx, scope, project.ID, tracking.ProjectNoteDraft{Title: "图片方案", Body: "private image", BodyFormat: "plain", MediaIDs: []string{id}})
	if err != nil {
		t.Fatal(err)
	}
	version, err := svc.ProjectNoteFreezeVersion(ctx, scope, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	share, err := svc.ProjectNoteCreateShare(ctx, scope, version.ID, svc.Now().Add(time.Hour), "private-export-media-share")
	if err != nil {
		t.Fatal(err)
	}
	return project, version, share
}
func TestProjectNoteExportFailsClosedOnMediaFailureAndPermissionChange(t *testing.T) {
	for _, scenario := range []string{"missing", "error", "removed-during-read", "revoked-designer-during-read"} {
		t.Run(scenario, func(t *testing.T) {
			router, svc, scope, actor := newProjectNoteHandlerFixture(t)
			project, version, share := projectNoteMediaVersion(t, svc, *scope)
			*actor = 101
			svc.MediaObjects = projectNoteExportMediaStore{get: func(ctx context.Context, _, _ string, _ int64) ([]byte, string, error) {
				switch scenario {
				case "missing":
					return nil, "", nil
				case "error":
					return nil, "", errors.New("storage failed")
				case "removed-during-read":
					if err := svc.ProjectNoteRevokeMember(ctx, *scope, project.ID, 101); err != nil {
						t.Fatal(err)
					}
				case "revoked-designer-during-read":
					svc.IsContentOperator = func(context.Context, int64, string) (bool, error) { return false, nil }
				}
				return exportTinyPNG(t), "image/png", nil
			}}
			path := "/api/v1/miniprogram/project-notes/shared/" + share.Token + "/export.docx"
			if scenario == "revoked-designer-during-read" {
				path = "/api/v1/miniprogram/project-notes/projects/" + project.ID + "/versions/" + version.ID + "/export.docx"
			}
			response := requestProjectNoteExport(router, http.MethodGet, path)
			if response.Code < 400 || bytes.HasPrefix(response.Body.Bytes(), []byte("PK")) {
				t.Fatalf("partial export returned: status=%d body=%d", response.Code, response.Body.Len())
			}
		})
	}
}

func TestProjectNoteExportBoundsAggregationCancellationAndConcurrency(t *testing.T) {
	router, svc, scope, _ := newProjectNoteHandlerFixture(t)
	project, version, _ := projectNoteMediaVersion(t, svc, *scope)
	reads := 0
	svc.MediaObjects = projectNoteExportMediaStore{get: func(ctx context.Context, _ string, _ string, max int64) ([]byte, string, error) {
		reads++
		if max != notedocx.MaxImageBytes {
			t.Fatalf("read limit=%d", max)
		}
		return exportTinyPNG(t), "image/png", nil
	}}
	if _, err := buildProjectNoteDOCX(context.Background(), svc, version, 1); !errors.Is(err, notedocx.ErrLimit) {
		t.Fatalf("aggregate limit err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reads = 0
	if _, err := buildProjectNoteDOCX(ctx, svc, version, projectNoteExportMaxBytes); !errors.Is(err, context.Canceled) || reads != 0 {
		t.Fatalf("cancel err=%v reads=%d", err, reads)
	}
	path := "/api/v1/miniprogram/project-notes/projects/" + project.ID + "/versions/" + version.ID + "/export.docx"
	for i := 0; i < cap(projectNoteExportSlots); i++ {
		projectNoteExportSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(projectNoteExportSlots); i++ {
			<-projectNoteExportSlots
		}
	}()
	response := requestProjectNoteExport(router, http.MethodGet, path)
	if response.Code != 429 || response.Header().Get("Retry-After") != "1" || bytes.HasPrefix(response.Body.Bytes(), []byte("PK")) {
		t.Fatalf("unbounded concurrency: %d", response.Code)
	}
}

func TestProjectNoteExportEmbedsAuthorizedImageWithoutFetchingVideo(t *testing.T) {
	_, svc, scope, _ := newProjectNoteHandlerFixture(t)
	_, version, _ := projectNoteMediaVersion(t, svc, *scope)
	version.Media = append(version.Media, tracking.Asset{ID: "zyxwvutsrqponmlkjihgfedc12345678", Kind: "video", MIME: "video/mp4", Width: 1920, Height: 1080})
	reads := 0
	svc.MediaObjects = projectNoteExportMediaStore{get: func(_ context.Context, _ string, id string, _ int64) ([]byte, string, error) {
		reads++
		if id != version.Media[0].ID {
			t.Fatal("video bytes were read")
		}
		return exportTinyPNG(t), "image/png", nil
	}}
	data, err := buildProjectNoteDOCX(context.Background(), svc, version, projectNoteExportMaxBytes)
	if err != nil || reads != 1 {
		t.Fatalf("build err=%v reads=%d", err, reads)
	}
	xml := exportedDocumentXML(t, data)
	if !strings.Contains(xml, "rIdImage1") || !strings.Contains(xml, "视频请在甲第方案中查看") || strings.Contains(xml, version.Media[0].URL) || strings.Contains(xml, version.Media[1].ID) {
		t.Fatal("image/video export boundary wrong")
	}
}
