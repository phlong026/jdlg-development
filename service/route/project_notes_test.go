package route

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/notification"
	"service/tracking"
)

type projectNoteMemberDriver struct{}
type projectNoteMemberConn struct{}
type projectNoteMemberRows struct{ active bool }
type failingProjectNoteMediaStore struct{}

func (failingProjectNoteMediaStore) Put(context.Context, string, string, []byte, string) error {
	return errors.New("private object store unavailable")
}
func (failingProjectNoteMediaStore) Get(context.Context, string, string, int64) ([]byte, string, error) {
	return nil, "", errors.New("private object store unavailable")
}

func (projectNoteMemberDriver) Open(string) (driver.Conn, error)  { return projectNoteMemberConn{}, nil }
func (projectNoteMemberConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (projectNoteMemberConn) Close() error                        { return nil }
func (projectNoteMemberConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (projectNoteMemberConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	for _, predicate := range []string{"id=?", "user_type='miniprogram'", "COALESCE(status,'active')='active'"} {
		if !strings.Contains(query, predicate) {
			return nil, errors.New("missing active mini account predicate: " + predicate)
		}
	}
	account := args[0].Value.(int64)
	if account == 503 {
		return nil, errors.New("private database unavailable")
	}
	return &projectNoteMemberRows{active: account == 101}, nil
}
func (*projectNoteMemberRows) Columns() []string { return []string{"1"} }
func (*projectNoteMemberRows) Close() error      { return nil }
func (r *projectNoteMemberRows) Next(values []driver.Value) error {
	if !r.active {
		return io.EOF
	}
	r.active = false
	values[0] = int64(1)
	return nil
}

func init() { sql.Register("project-note-member", projectNoteMemberDriver{}) }

func newProjectNoteHandlerFixture(t *testing.T) (*gin.Engine, *tracking.Service, *tracking.Scope, *int64) {
	return newProjectNoteHandlerFixtureWithProfiles(t, func(_ context.Context, ids []int64) (map[int64]model.UserProfilePresentation, error) {
		out := map[int64]model.UserProfilePresentation{}
		for _, id := range ids {
			if id == 7 {
				out[id] = model.UserProfilePresentation{UserID: id, Nickname: "李设计师"}
			} else if id == 101 {
				out[id] = model.UserProfilePresentation{UserID: id, Nickname: "张女士", Avatar: "https://public.example/member.png"}
			}
		}
		return out, nil
	})
}

func newProjectNoteHandlerFixtureWithProfiles(t *testing.T, profiles projectNoteProfileResolver) (*gin.Engine, *tracking.Service, *tracking.Scope, *int64) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{
		Tenant: "project-notes", Enabled: true, ContentEnabled: true, DefaultOwner: "owner-a", ConsentVersion: "v1",
		Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}},
	})
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }
	svc.IsContentOperator = func(_ context.Context, actor int64, owner string) (bool, error) {
		return actor == 7 && owner == "owner-a", nil
	}
	scope := &tracking.Scope{Tenant: "project-notes", Actor: 7, Owners: []string{"owner-a"}, Manage: true}
	actor := int64(7)
	router := gin.New()
	root := router.Group("/api/v1/miniprogram/project-notes", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.Next()
	})
	manage := root.Group("", func(c *gin.Context) { c.Set(projectNoteScopeKey, *scope); c.Next() })
	registerProjectNoteHandlers(root, manage, svc, projectNoteHandlerDeps{
		actor:    func(*gin.Context) int64 { return actor },
		profiles: profiles,
		verifyMember: func(_ context.Context, account int64) error {
			if account == 101 || account == 102 {
				return nil
			}
			return &tracking.Error{Status: http.StatusBadRequest, Message: "受邀账号不存在或已停用"}
		},
	})
	router.GET("/test/generic-media/:id", func(c *gin.Context) { serveTrackingMedia(c, svc, scope) })
	return router, svc, scope, &actor
}

func projectNoteRequest(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw string
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = string(encoded)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func projectNoteMediaUpload(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "private-project.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(imageData.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(rec, req)
	return rec
}

func projectNoteData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func createProjectNoteShare(t *testing.T, router *gin.Engine) (tracking.ProjectNoteProject, tracking.ProjectNote, tracking.ProjectNoteVersion, tracking.ProjectNoteShare) {
	t.Helper()
	projectRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "张宅方案", "owner_id": "owner-a"})
	if projectRec.Code != http.StatusOK {
		t.Fatal(projectRec.Code, projectRec.Body.String())
	}
	project := projectNoteData[tracking.ProjectNoteProject](t, projectRec)
	if project.CreatorDisplayName != "李设计师" {
		t.Fatalf("creator display name = %q", project.CreatorDisplayName)
	}
	if rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/members/101", nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	noteRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/notes", map[string]any{"title": "首版", "body": "固定方案一", "body_format": "plain"})
	if noteRec.Code != http.StatusOK {
		t.Fatal(noteRec.Code, noteRec.Body.String())
	}
	note := projectNoteData[tracking.ProjectNote](t, noteRec)
	versionRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/notes/"+note.ID+"/versions", map[string]any{"expected_revision": note.Revision})
	if versionRec.Code != http.StatusOK {
		t.Fatal(versionRec.Code, versionRec.Body.String())
	}
	version := projectNoteData[tracking.ProjectNoteVersion](t, versionRec)
	shareRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/versions/"+version.ID+"/shares", map[string]any{"expires_at": "2026-10-28T08:00:00Z", "idempotency_key": "share-route-key-1"})
	if shareRec.Code != http.StatusOK {
		t.Fatal(shareRec.Code, shareRec.Body.String())
	}
	share := projectNoteData[tracking.ProjectNoteShare](t, shareRec)
	return project, note, version, share
}

func TestProjectNoteRoutesSeparateManagerAndInvitedAccount(t *testing.T) {
	router, _, scope, actor := newProjectNoteHandlerFixture(t)
	project, note, version, share := createProjectNoteShare(t, router)
	if share.Token == "" {
		t.Fatal("create share did not return its one-time token")
	}
	detailRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+project.ID, nil)
	detail := projectNoteData[tracking.ProjectNoteProjectDetail](t, detailRec)
	if detailRec.Code != http.StatusOK || len(detail.Members) != 1 || len(detail.Notes) != 1 || len(detail.Versions) != 1 || len(detail.Shares) != 1 || detail.Shares[0].Token != "" {
		t.Fatalf("detail = %d %s", detailRec.Code, detailRec.Body.String())
	}
	confirmer := projectNoteRequest(t, router, http.MethodPut, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/confirmer", map[string]any{"account_id": 101})
	if confirmer.Code != http.StatusOK {
		t.Fatal(confirmer.Code, confirmer.Body.String())
	}

	*actor = 101
	viewRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/shared/"+share.Token, nil)
	view := projectNoteData[tracking.ProjectNoteShareView](t, viewRec)
	if viewRec.Code != http.StatusOK || view.Version.ID != version.ID || view.Version.Body != "固定方案一" {
		t.Fatalf("shared view = %d %s", viewRec.Code, viewRec.Body.String())
	}
	feedback := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/feedback", map[string]any{"text": "请调整门厅", "idempotency_key": "feedback-route-1"})
	if feedback.Code != http.StatusOK {
		t.Fatal(feedback.Code, feedback.Body.String())
	}
	feedbackValue := projectNoteData[tracking.ProjectNoteFeedback](t, feedback)
	confirmation := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/confirm", map[string]any{"idempotency_key": "confirm-route-1"})
	if confirmation.Code != http.StatusOK {
		t.Fatal(confirmation.Code, confirmation.Body.String())
	}
	opened := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/open", map[string]any{"event_id": "open-route-event-1", "acknowledged": true})
	openedFact := projectNoteData[tracking.ProjectNoteOpenFact](t, opened)
	if opened.Code != http.StatusOK || !openedFact.Notify || openedFact.ActualSharerID != 7 {
		t.Fatal(opened.Code, opened.Body.String())
	}
	resolved := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/feedback/"+feedbackValue.ID+"/resolve", map[string]any{"reply": "已调整", "idempotency_key": "resolve-route-1"})
	if resolved.Code != http.StatusOK {
		t.Fatal(resolved.Code, resolved.Body.String())
	}

	*actor = 102
	forwarded := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/shared/"+share.Token, nil)
	if forwarded.Code != http.StatusForbidden || strings.Contains(forwarded.Body.String(), "固定方案一") {
		t.Fatal(forwarded.Code, forwarded.Body.String())
	}

	*scope = tracking.Scope{Tenant: "project-notes", Actor: 8, Owners: []string{"owner-a"}, Manage: true}
	otherDesigner := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+project.ID, nil)
	if otherDesigner.Code != http.StatusNotFound {
		t.Fatal(otherDesigner.Code, otherDesigner.Body.String())
	}

	*scope = tracking.Scope{Tenant: "project-notes", Actor: 7, Owners: []string{"owner-a"}, Manage: true}
	updated := projectNoteRequest(t, router, http.MethodPatch, "/api/v1/miniprogram/project-notes/notes/"+note.ID, map[string]any{"expected_revision": note.Revision, "title": "二版", "body": "方案二", "body_format": "plain"})
	if updated.Code != http.StatusOK {
		t.Fatal(updated.Code, updated.Body.String())
	}
	*actor = 101
	pinned := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/shared/"+share.Token, nil)
	if pinned.Code != http.StatusOK || !strings.Contains(pinned.Body.String(), "固定方案一") || strings.Contains(pinned.Body.String(), "方案二") {
		t.Fatal(pinned.Code, pinned.Body.String())
	}

	*scope = tracking.Scope{Tenant: "project-notes", Actor: 7, Owners: []string{"owner-a"}, Manage: true}
	if rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shares/"+share.ID+"/revoke", nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	*actor = 101
	revoked := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/shared/"+share.Token, nil)
	if revoked.Code != http.StatusNotFound {
		t.Fatal(revoked.Code, revoked.Body.String())
	}
}

func TestProjectNoteDetailAddsPresentationOnlyForAuthorizedProjectRecords(t *testing.T) {
	router, svc, scope, _ := newProjectNoteHandlerFixture(t)
	project, _, version, share := createProjectNoteShare(t, router)
	if err := svc.ProjectNoteSetConfirmer(context.Background(), *scope, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProjectNoteConfirmShare(context.Background(), share.Token, 101, "presentation-confirm"); err != nil {
		t.Fatal(err)
	}
	rec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+project.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Project struct {
				CreatorDisplayName string `json:"creator_display_name"`
			} `json:"project"`
			Members []struct {
				ActorID     int64  `json:"actor_id"`
				DisplayName string `json:"display_name"`
				Avatar      string `json:"avatar"`
			} `json:"members"`
			Confirmations []struct {
				VersionID   string `json:"version_id"`
				ConfirmerID int64  `json:"confirmer_id"`
				DisplayName string `json:"display_name"`
			} `json:"confirmations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Project.CreatorDisplayName != "李设计师" {
		t.Fatalf("project creator = %s", rec.Body.String())
	}
	if len(envelope.Data.Members) != 1 || envelope.Data.Members[0].ActorID != 101 || envelope.Data.Members[0].DisplayName != "张女士" || envelope.Data.Members[0].Avatar != "https://public.example/member.png" {
		t.Fatalf("members = %s", rec.Body.String())
	}
	if len(envelope.Data.Confirmations) != 1 || envelope.Data.Confirmations[0].VersionID != version.ID || envelope.Data.Confirmations[0].ConfirmerID != 101 || envelope.Data.Confirmations[0].DisplayName != "张女士" {
		t.Fatalf("confirmations = %s", rec.Body.String())
	}
}

func TestProjectNoteListAndUpdateKeepCreatorPresentation(t *testing.T) {
	router, _, _, _ := newProjectNoteHandlerFixture(t)
	projectRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "创建人展示", "owner_id": "owner-a"})
	project := projectNoteData[tracking.ProjectNoteProject](t, projectRec)
	listRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects", nil)
	page := projectNoteData[tracking.ProjectNoteProjectPage](t, listRec)
	if listRec.Code != http.StatusOK || len(page.Items) != 1 || page.Items[0].CreatorDisplayName != "李设计师" {
		t.Fatalf("list = %d %s", listRec.Code, listRec.Body.String())
	}
	updateRec := projectNoteRequest(t, router, http.MethodPatch, "/api/v1/miniprogram/project-notes/projects/"+project.ID, map[string]any{"expected_revision": project.Revision, "name": "创建人展示二版"})
	updated := projectNoteData[tracking.ProjectNoteProject](t, updateRec)
	if updateRec.Code != http.StatusOK || updated.CreatorDisplayName != "李设计师" {
		t.Fatalf("update = %d %s", updateRec.Code, updateRec.Body.String())
	}
}

func TestProjectNoteListKeepsProjectsWhenCreatorProfilesFail(t *testing.T) {
	router, _, _, _ := newProjectNoteHandlerFixtureWithProfiles(t, func(context.Context, []int64) (map[int64]model.UserProfilePresentation, error) {
		return nil, errors.New("profile store unavailable")
	})
	projectRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "降级展示", "owner_id": "owner-a"})
	if projectRec.Code != http.StatusOK {
		t.Fatal(projectRec.Code, projectRec.Body.String())
	}
	listRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects", nil)
	page := projectNoteData[tracking.ProjectNoteProjectPage](t, listRec)
	if listRec.Code != http.StatusOK || len(page.Items) != 1 || page.Items[0].CreatorDisplayName != "项目创建人" {
		t.Fatalf("degraded list = %d %s", listRec.Code, listRec.Body.String())
	}
}

func TestProjectNoteSharedMediaUsesOnlyTokenAuthorizedProxy(t *testing.T) {
	router, svc, scope, actor := newProjectNoteHandlerFixture(t)
	mediaDir := t.TempDir()
	if err := os.Chmod(mediaDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACKING_MEDIA_DIR", mediaDir)
	projectRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "带图方案", "owner_id": "owner-a"})
	project := projectNoteData[tracking.ProjectNoteProject](t, projectRec)
	projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/members/101", nil)
	uploadRec := projectNoteMediaUpload(t, router, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/media")
	media := projectNoteData[tracking.ProjectNoteMedia](t, uploadRec)
	mediaID := media.Asset.ID
	if uploadRec.Code != http.StatusOK || mediaID == "" || media.Asset.URL != "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/media/"+mediaID {
		t.Fatal(uploadRec.Code, uploadRec.Body.String())
	}
	if listed, err := svc.ListMedia(context.Background(), *scope); err != nil || len(listed.Items.([]tracking.Asset)) != 0 {
		t.Fatalf("private project media leaked into generic list: %#v %v", listed, err)
	}
	if _, err := svc.Media(context.Background(), scope, mediaID); err == nil {
		t.Fatal("known private project media id was readable through generic media")
	}
	genericMedia := projectNoteRequest(t, router, http.MethodGet, "/test/generic-media/"+mediaID, nil)
	if genericMedia.Code != http.StatusNotFound {
		t.Fatal(genericMedia.Code, genericMedia.Body.String())
	}
	projectMediaList := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/media", nil)
	listedPrivate := projectNoteData[tracking.ProjectNoteMediaPage](t, projectMediaList)
	if projectMediaList.Code != http.StatusOK || len(listedPrivate.Items) != 1 || listedPrivate.Items[0].Asset.ID != mediaID {
		t.Fatal(projectMediaList.Code, projectMediaList.Body.String())
	}
	managerMedia := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/media/"+mediaID, nil)
	if managerMedia.Code != http.StatusOK || !bytes.Equal(managerMedia.Body.Bytes(), mustReadFile(t, filepath.Join(mediaDir, mediaID))) {
		t.Fatal(managerMedia.Code, managerMedia.Body.String())
	}
	managerHead := projectNoteRequest(t, router, http.MethodHead, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/media/"+mediaID, nil)
	if managerHead.Code != http.StatusOK || managerHead.Body.Len() != 0 {
		t.Fatal(managerHead.Code, managerHead.Body.String())
	}
	*scope = tracking.Scope{Tenant: "project-notes", Actor: 8, Owners: []string{"owner-a"}, Manage: true}
	otherDesigner := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/media/"+mediaID, nil)
	if otherDesigner.Code != http.StatusNotFound {
		t.Fatal(otherDesigner.Code, otherDesigner.Body.String())
	}
	*scope = tracking.Scope{Tenant: "project-notes", Actor: 7, Owners: []string{"owner-a"}, Manage: true}
	noteRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/notes", map[string]any{"title": "带图首版", "body": "图片", "body_format": "plain", "cover_media_id": mediaID, "media_ids": []string{mediaID}})
	note := projectNoteData[tracking.ProjectNote](t, noteRec)
	versionRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/notes/"+note.ID+"/versions", map[string]any{"expected_revision": note.Revision})
	version := projectNoteData[tracking.ProjectNoteVersion](t, versionRec)
	shareRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/versions/"+version.ID+"/shares", map[string]any{"expires_at": "2026-10-28T08:00:00Z", "idempotency_key": "share-media-key-1"})
	share := projectNoteData[tracking.ProjectNoteShare](t, shareRec)

	*actor = 101
	view := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/shared/"+share.Token, nil)
	if view.Code != http.StatusOK || strings.Contains(view.Body.String(), "/tracking/media/") || !strings.Contains(view.Body.String(), "/shared/"+share.Token+"/media/"+mediaID) {
		t.Fatal(view.Code, view.Body.String())
	}
	sharedMediaRec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/media/"+mediaID, nil)
	if sharedMediaRec.Code != http.StatusOK || sharedMediaRec.Body.Len() == 0 || sharedMediaRec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(sharedMediaRec.Code, sharedMediaRec.Body.String(), sharedMediaRec.Header())
	}
	*actor = 102
	denied := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/media/"+mediaID, nil)
	if denied.Code != http.StatusForbidden || strings.Contains(denied.Body.String(), "private-note-image") {
		t.Fatal(denied.Code, denied.Body.String())
	}
	*scope = tracking.Scope{Tenant: "project-notes", Actor: 7, Owners: []string{"owner-a"}, Manage: true}
	if rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shares/"+share.ID+"/revoke", nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	*actor = 101
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		revoked := projectNoteRequest(t, router, method, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/media/"+mediaID, nil)
		if revoked.Code != http.StatusNotFound {
			t.Fatal(method, revoked.Code, revoked.Body.String())
		}
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestProjectNoteInviteRequiresActiveMiniProgramAccount(t *testing.T) {
	router, _, _, _ := newProjectNoteHandlerFixture(t)
	projectRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "验证邀请", "owner_id": "owner-a"})
	project := projectNoteData[tracking.ProjectNoteProject](t, projectRec)
	rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/members/999", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func TestProjectNoteMemberLookupRequiresCurrentActiveMiniProgramAccount(t *testing.T) {
	db, err := sql.Open("project-note-member", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = verifyActiveMiniProgramAccount(context.Background(), db, 101); err != nil {
		t.Fatal(err)
	}
	if err = verifyActiveMiniProgramAccount(context.Background(), db, 102); err == nil {
		t.Fatal("inactive or non-mini account was accepted")
	}
	if err = verifyActiveMiniProgramAccount(context.Background(), db, 503); !errors.Is(err, tracking.ErrUnavailable) {
		t.Fatalf("database failure = %v", err)
	}
}

func TestProjectNoteUploadKeepsPendingEvidenceWhenObjectWriteFails(t *testing.T) {
	router, svc, _, _ := newProjectNoteHandlerFixture(t)
	t.Setenv("CONTENT_MEDIA_STORAGE", "cos")
	svc.MediaObjects = failingProjectNoteMediaStore{}
	projectRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "存储失败项目", "owner_id": "owner-a"})
	project := projectNoteData[tracking.ProjectNoteProject](t, projectRec)
	upload := projectNoteMediaUpload(t, router, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/media")
	if upload.Code == http.StatusOK {
		t.Fatal("failed object write reported success")
	}
	rows := svc.Store.(*governanceTestStore).records
	pending, visible := 0, 0
	for _, row := range rows {
		switch row.Kind {
		case "note_media_pending":
			pending++
		case "note_media":
			visible++
		}
	}
	if pending != 1 || visible != 0 {
		t.Fatalf("pending=%d visible=%d", pending, visible)
	}
}

func TestProjectNoteProjectInputFailsClosedForUnverifiedCRMAndUnknownFields(t *testing.T) {
	router, _, _, _ := newProjectNoteHandlerFixture(t)
	crm := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "CRM项目", "owner_id": "owner-a", "crm_id": "customer-1"})
	if crm.Code != http.StatusForbidden {
		t.Fatal(crm.Code, crm.Body.String())
	}
	unknown := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "未知字段", "owner_id": "owner-a", "identity_type": "设计师"})
	if unknown.Code != http.StatusBadRequest {
		t.Fatal(unknown.Code, unknown.Body.String())
	}
	oversized := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": strings.Repeat("x", 140<<10), "owner_id": "owner-a"})
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatal(oversized.Code, oversized.Body.String())
	}
}

func TestProjectNoteWorkflowRoutesExposeStructuredProjectAndSatisfaction(t *testing.T) {
	router, _, _, actor := newProjectNoteHandlerFixture(t)
	created := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{
		"name": "张宅方案", "owner_id": "owner-a", "location": "杭州市余杭区", "building_area": 328.5, "floors": 3, "style": "现代中式",
	})
	if created.Code != http.StatusOK {
		t.Fatal(created.Code, created.Body.String())
	}
	project := projectNoteData[tracking.ProjectNoteProject](t, created)
	updated := projectNoteRequest(t, router, http.MethodPatch, "/api/v1/miniprogram/project-notes/projects/"+project.ID, map[string]any{
		"expected_revision": project.Revision, "location": "杭州市临平区", "status": "revising",
	})
	if updated.Code != http.StatusOK {
		t.Fatal(updated.Code, updated.Body.String())
	}
	project = projectNoteData[tracking.ProjectNoteProject](t, updated)
	if project.Location != "杭州市临平区" || project.Status != tracking.ProjectNoteProjectStatusRevising {
		t.Fatalf("updated project = %#v", project)
	}
	forged := projectNoteRequest(t, router, http.MethodPatch, "/api/v1/miniprogram/project-notes/projects/"+project.ID, map[string]any{
		"expected_revision": project.Revision, "status": "confirmed",
	})
	if forged.Code != http.StatusBadRequest {
		t.Fatal(forged.Code, forged.Body.String())
	}

	if rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/members/101", nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	noteRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/notes", map[string]any{"title": "首版", "body": "固定方案", "body_format": "plain"})
	note := projectNoteData[tracking.ProjectNote](t, noteRec)
	versionRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/notes/"+note.ID+"/versions", map[string]any{"expected_revision": note.Revision})
	version := projectNoteData[tracking.ProjectNoteVersion](t, versionRec)
	shareRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/versions/"+version.ID+"/shares", map[string]any{"expires_at": "2026-10-28T08:00:00Z", "idempotency_key": "workflow-share-1"})
	share := projectNoteData[tracking.ProjectNoteShare](t, shareRec)
	*actor = 101
	feedback := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/feedback", map[string]any{
		"text": "门厅需要调整", "satisfaction": "needs_adjustment", "idempotency_key": "workflow-feedback-1",
	})
	if feedback.Code != http.StatusOK || projectNoteData[tracking.ProjectNoteFeedback](t, feedback).Satisfaction != tracking.ProjectNoteSatisfactionNeedsAdjustment {
		t.Fatal(feedback.Code, feedback.Body.String())
	}
}

func TestProjectNoteOpenRouteRequiresExplicitAcknowledgement(t *testing.T) {
	router, _, _, actor := newProjectNoteHandlerFixture(t)
	_, _, _, share := createProjectNoteShare(t, router)
	*actor = 101
	for _, body := range []map[string]any{
		{"event_id": "open-without-ack"},
		{"event_id": "open-false-ack", "acknowledged": false},
	} {
		rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/open", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%v status=%d response=%s", body, rec.Code, rec.Body.String())
		}
	}
	accepted := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/open", map[string]any{
		"event_id": "open-explicit-ack", "acknowledged": true,
	})
	if accepted.Code != http.StatusOK {
		t.Fatal(accepted.Code, accepted.Body.String())
	}
}

func TestProjectNoteRoutesDefaultOffAndRequireSignedMiniProgramAccount(t *testing.T) {
	t.Setenv("PROJECT_NOTES_ENABLED", "")
	if projectNotesEnabled() {
		t.Fatal("private project notes must default off")
	}
	t.Setenv("PROJECT_NOTES_ENABLED", "true")
	router := gin.New()
	RegisterTrackingRoutes(router.Group("/api/v1"), &model.UserModel{}, nil, nil)
	rec := projectNoteRequest(t, router, http.MethodGet, "/api/v1/miniprogram/project-notes/shared/forged-share-token", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatal(rec.Code, rec.Body.String())
	}
}

func TestProjectNoteNotificationEligibilityUsesCurrentPrivateGrant(t *testing.T) {
	router, svc, scope, actor := newProjectNoteHandlerFixture(t)
	svc.NoticeRules.BrowseEnabled = true
	_, _, version, share := createProjectNoteShare(t, router)
	*actor = 101
	opened := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/open", map[string]any{"event_id": "open-notice-event-1", "acknowledged": true})
	fact := projectNoteData[tracking.ProjectNoteOpenFact](t, opened)
	if opened.Code != http.StatusOK || !fact.Notify {
		t.Fatal(opened.Code, opened.Body.String())
	}
	event := notification.Event{ID: fact.ID, Kind: "project_note_open", Owner: "owner-a", RecipientID: fact.ActualSharerID, VisitorKey: "u:101", ShareID: share.ID, ProjectID: fact.ProjectID, ContentID: version.ID, Route: "project_note_share", OccurredAt: fact.OpenedAt}
	if eligible, err := phase3NoticeEligible(context.Background(), svc, event, *scope); err != nil || !eligible {
		t.Fatalf("eligible=%v err=%v", eligible, err)
	}
	*scope = tracking.Scope{Tenant: "project-notes", Actor: 7, Owners: []string{"owner-a"}, Manage: true}
	if rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shares/"+share.ID+"/revoke", nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if eligible, err := phase3NoticeEligible(context.Background(), svc, event, *scope); err != nil || eligible {
		t.Fatalf("revoked eligible=%v err=%v", eligible, err)
	}
}
