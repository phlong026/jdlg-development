package route

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

func createSharedVideoFixture(t *testing.T) (*tracking.Service, tracking.Scope, *int64, *gin.Engine, tracking.ProjectNoteProject, tracking.ProjectNoteVersion, tracking.ProjectNoteShare, tracking.Asset, []byte) {
	t.Helper()
	router, svc, scope, actor := newProjectNoteHandlerFixture(t)
	mediaDir := t.TempDir()
	t.Setenv("TRACKING_MEDIA_DIR", mediaDir)
	projectRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", map[string]any{"name": "视频方案", "owner_id": "owner-a"})
	project := projectNoteData[tracking.ProjectNoteProject](t, projectRec)
	if projectRec.Code != http.StatusOK {
		t.Fatal(projectRec.Code, projectRec.Body.String())
	}
	if rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/members/101", nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	video := []byte("0000ftypisom-video-range-fixture")
	digest := sha256.Sum256(video)
	asset := tracking.Asset{ID: strings.Repeat("d", 32), Kind: "video", MIME: "video/mp4", URL: "/api/v1/miniprogram/project-notes/projects/" + project.ID + "/media/" + strings.Repeat("d", 32), Width: 1280, Height: 720}
	operation, _, err := svc.ProjectNotePrepareMedia(context.Background(), *scope, project.ID, asset, hex.EncodeToString(digest[:]), "upload")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(mediaDir, asset.ID), video, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectNoteFinalizeMedia(context.Background(), *scope, project.ID, asset.ID, operation); err != nil {
		t.Fatal(err)
	}
	noteRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects/"+project.ID+"/notes", map[string]any{"title": "视频首版", "body": "视频", "body_format": "plain", "media_ids": []string{asset.ID}})
	note := projectNoteData[tracking.ProjectNote](t, noteRec)
	versionRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/notes/"+note.ID+"/versions", map[string]any{"expected_revision": note.Revision})
	version := projectNoteData[tracking.ProjectNoteVersion](t, versionRec)
	shareRec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/versions/"+version.ID+"/shares", map[string]any{"expires_at": "2026-10-28T08:00:00Z", "idempotency_key": "shared-video-note"})
	share := projectNoteData[tracking.ProjectNoteShare](t, shareRec)
	if noteRec.Code != http.StatusOK || versionRec.Code != http.StatusOK || shareRec.Code != http.StatusOK {
		t.Fatalf("note=%d version=%d share=%d", noteRec.Code, versionRec.Code, shareRec.Code)
	}
	return svc, *scope, actor, router, project, version, share, asset, video
}

func playbackURL(t *testing.T, recBody []byte) string {
	t.Helper()
	rec := &httptest.ResponseRecorder{Body: bytes.NewBuffer(recBody)}
	response := projectNoteData[struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}](t, rec)
	if response.URL == "" || response.ExpiresAt.Before(time.Now()) {
		t.Fatalf("invalid playback response: %+v", response)
	}
	return response.URL
}

func TestProjectNoteSharedVideoPlaybackTicketStreamsRangesAndRechecksShare(t *testing.T) {
	_, _, actor, router, _, _, share, asset, video := createSharedVideoFixture(t)
	*actor = 101
	issued := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/media/"+asset.ID+"/playback", nil)
	if issued.Code != http.StatusOK {
		t.Fatal(issued.Code, issued.Body.String())
	}
	playback := playbackURL(t, issued.Body.Bytes())
	rangeReq := httptest.NewRequest(http.MethodGet, playback, nil)
	rangeReq.Header.Set("Range", "bytes=4-11")
	rangeRec := httptest.NewRecorder()
	router.ServeHTTP(rangeRec, rangeReq)
	if rangeRec.Code != http.StatusPartialContent || rangeRec.Body.String() != string(video[4:12]) || rangeRec.Header().Get("Content-Range") == "" {
		t.Fatalf("range=%d body=%q headers=%v", rangeRec.Code, rangeRec.Body.String(), rangeRec.Header())
	}
	invalidReq := httptest.NewRequest(http.MethodGet, playback, nil)
	invalidReq.Header.Set("Range", "bytes=999-")
	invalidRec := httptest.NewRecorder()
	router.ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusRequestedRangeNotSatisfiable || invalidRec.Header().Get("Content-Range") == "" {
		t.Fatalf("invalid range=%d headers=%v", invalidRec.Code, invalidRec.Header())
	}
	wrongAsset := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shared/"+share.Token+"/media/"+strings.Repeat("e", 32)+"/playback", nil)
	if wrongAsset.Code != http.StatusNotFound {
		t.Fatalf("cross media=%d %s", wrongAsset.Code, wrongAsset.Body.String())
	}
	u, _ := url.Parse(playback)
	forged := strings.TrimSuffix(playback, u.Query().Get("sig")) + strings.Repeat("0", 64)
	if rec := projectNoteRequest(t, router, http.MethodGet, forged, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("forged=%d %s", rec.Code, rec.Body.String())
	}
	*actor = 7
	if rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/shares/"+share.ID+"/revoke", nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec := projectNoteRequest(t, router, http.MethodGet, playback, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("revoked=%d %s", rec.Code, rec.Body.String())
	}
}

func TestSharedVideoPlaybackWorksThroughNicknameOnlyProfileMiddleware(t *testing.T) {
	svc, scope, actor, setupRouter, project, version, share, asset, video := createSharedVideoFixture(t)
	registerProjectPackageTestHandlers(setupRouter, svc, &scope, actor)
	delivery := createProjectPackage(t, setupRouter, project, version)
	*actor = 101
	profile := miniprogramProfile{Nickname: "用户"}
	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.Use(
		miniprogramProfileAuthentication(func(c *gin.Context) {
			if c.GetHeader("token") != "valid" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
				return
			}
			c.Set("user_id", int64(101))
			c.Next()
		}),
		miniprogramProfileCompleteness(func(context.Context, int64) (miniprogramProfile, error) { return profile, nil }),
	)
	validationDeadlineSeen := false
	registerSharedVideoPlaybackRoutes(v1, svc, func(ctx context.Context, account int64, kind sharedVideoKind) error {
		_, validationDeadlineSeen = ctx.Deadline()
		if account != 101 || miniprogramProfileNicknameMissing(profile) || kind == sharedVideoPackage && len(miniprogramProfileMissingFields(profile)) > 0 {
			return &tracking.Error{Status: http.StatusPreconditionRequired, Message: "请先补充昵称"}
		}
		return nil
	})
	root := v1.Group("/miniprogram/project-notes")
	manage := root.Group("", func(c *gin.Context) { c.Set(projectNoteScopeKey, scope); c.Next() })
	registerProjectNoteHandlers(root, manage, svc, projectNoteHandlerDeps{actor: func(*gin.Context) int64 { return *actor }})
	packageRoot := v1.Group("/miniprogram/project-packages")
	registerProjectPackageHandlers(manage, packageRoot, svc, func(*gin.Context) int64 { return *actor })

	playbackPath := "/api/v1/miniprogram/project-notes/shared/" + share.Token + "/media/" + asset.ID + "/playback"
	issueRequest := httptest.NewRequest(http.MethodPost, playbackPath, strings.NewReader("{}"))
	issueRequest.Header.Set("Content-Type", "application/json")
	issueRequest.Header.Set("token", "valid")
	issueResponse := httptest.NewRecorder()
	router.ServeHTTP(issueResponse, issueRequest)
	if issueResponse.Code != http.StatusOK {
		t.Fatalf("nickname-only playback issue=%d body=%s", issueResponse.Code, issueResponse.Body.String())
	}
	playback := playbackURL(t, issueResponse.Body.Bytes())
	rangeRequest := httptest.NewRequest(http.MethodGet, playback, nil)
	rangeRequest.Header.Set("Range", "bytes=0-3")
	rangeResponse := httptest.NewRecorder()
	router.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent || rangeResponse.Body.String() != string(video[:4]) || !validationDeadlineSeen {
		t.Fatalf("nickname-only range=%d deadline=%v body=%q", rangeResponse.Code, validationDeadlineSeen, rangeResponse.Body.String())
	}

	profile = miniprogramProfile{Avatar: "https://cdn.example.com/avatar.png"}
	missingNicknameRequest := httptest.NewRequest(http.MethodPost, playbackPath, strings.NewReader("{}"))
	missingNicknameRequest.Header.Set("token", "valid")
	missingNicknameResponse := httptest.NewRecorder()
	router.ServeHTTP(missingNicknameResponse, missingNicknameRequest)
	if missingNicknameResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing nickname=%d body=%s", missingNicknameResponse.Code, missingNicknameResponse.Body.String())
	}

	profile = miniprogramProfile{Nickname: "用户", Avatar: "https://cdn.example.com/avatar.png"}
	packagePlaybackPath := "/api/v1/miniprogram/project-packages/shared/" + delivery.Token + "/versions/" + version.ID + "/media/" + asset.ID + "/playback"
	packageIssueRequest := httptest.NewRequest(http.MethodPost, packagePlaybackPath, strings.NewReader("{}"))
	packageIssueRequest.Header.Set("token", "valid")
	packageIssueResponse := httptest.NewRecorder()
	router.ServeHTTP(packageIssueResponse, packageIssueRequest)
	if packageIssueResponse.Code != http.StatusOK {
		t.Fatalf("complete package issue=%d body=%s", packageIssueResponse.Code, packageIssueResponse.Body.String())
	}
	packagePlayback := playbackURL(t, packageIssueResponse.Body.Bytes())
	profile = miniprogramProfile{Nickname: "用户"}
	packageRangeRequest := httptest.NewRequest(http.MethodGet, packagePlayback, nil)
	packageRangeRequest.Header.Set("Range", "bytes=0-3")
	packageRangeResponse := httptest.NewRecorder()
	router.ServeHTTP(packageRangeResponse, packageRangeRequest)
	if packageRangeResponse.Code != http.StatusPreconditionRequired || packageRangeResponse.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("package GET missing avatar=%d cache=%q body=%s", packageRangeResponse.Code, packageRangeResponse.Header().Get("Cache-Control"), packageRangeResponse.Body.String())
	}
	packagePostRequest := httptest.NewRequest(http.MethodPost, packagePlaybackPath, strings.NewReader("{}"))
	packagePostRequest.Header.Set("token", "valid")
	packagePostResponse := httptest.NewRecorder()
	router.ServeHTTP(packagePostResponse, packagePostRequest)
	if packagePostResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("package POST missing avatar=%d body=%s", packagePostResponse.Code, packagePostResponse.Body.String())
	}

	profile = miniprogramProfile{Nickname: "用户"}
	managementRequest := httptest.NewRequest(http.MethodPost, "/api/v1/miniprogram/project-notes/projects", strings.NewReader("{}"))
	managementRequest.Header.Set("token", "valid")
	managementResponse := httptest.NewRecorder()
	router.ServeHTTP(managementResponse, managementRequest)
	if managementResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("management missing avatar=%d body=%s", managementResponse.Code, managementResponse.Body.String())
	}
}

func TestSharedVideoPlaybackRejectsExpiredTicket(t *testing.T) {
	_, _, _, router, _, _, share, asset, _ := createSharedVideoFixture(t)
	path, err := issueSharedVideoPlaybackAt(sharedVideoPlaybackGrant{Kind: sharedVideoNote, ShareToken: share.Token, Actor: 101, AssetID: asset.ID}, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if rec := projectNoteRequest(t, router, http.MethodGet, path, nil); rec.Code != http.StatusForbidden || rec.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("expired=%d cache=%q body=%s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
}

func TestSharedVideoPlaybackRechecksBoundAccount(t *testing.T) {
	svc, _, _, _, _, _, share, asset, _ := createSharedVideoFixture(t)
	path, _, err := issueSharedVideoPlayback(sharedVideoPlaybackGrant{Kind: sharedVideoNote, ShareToken: share.Token, Actor: 101, AssetID: asset.ID})
	if err != nil {
		t.Fatal(err)
	}
	checked := false
	router := gin.New()
	registerSharedVideoPlaybackRoutes(router.Group("/api/v1"), svc, func(_ context.Context, actor int64, _ sharedVideoKind) error {
		checked = actor == 101
		return &tracking.Error{Status: http.StatusForbidden, Message: "账号已停用"}
	})
	rec := projectNoteRequest(t, router, http.MethodGet, path, nil)
	if rec.Code != http.StatusForbidden || !checked {
		t.Fatalf("inactive account=%d checked=%v body=%s", rec.Code, checked, rec.Body.String())
	}
}

func TestSharedVideoPlaybackFailsClosedWhenDisabledOrVerifierMissing(t *testing.T) {
	svc, _, _, _, _, _, share, asset, _ := createSharedVideoFixture(t)
	path, _, err := issueSharedVideoPlayback(sharedVideoPlaybackGrant{Kind: sharedVideoNote, ShareToken: share.Token, Actor: 101, AssetID: asset.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROJECT_NOTES_ENABLED", "false")
	router := gin.New()
	registerSharedVideoPlaybackRoutes(router.Group("/api/v1"), svc, func(context.Context, int64, sharedVideoKind) error { return nil })
	if rec := projectNoteRequest(t, router, http.MethodGet, path, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("disabled=%d %s", rec.Code, rec.Body.String())
	}
	t.Setenv("PROJECT_NOTES_ENABLED", "true")
	router = gin.New()
	registerSharedVideoPlaybackRoutes(router.Group("/api/v1"), svc, nil)
	if rec := projectNoteRequest(t, router, http.MethodGet, path, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing verifier=%d %s", rec.Code, rec.Body.String())
	}
}

func TestSharedVideoPlaybackTicketPoolIsBounded(t *testing.T) {
	sharedVideoPlaybackGrants.Lock()
	original := sharedVideoPlaybackGrants.items
	sharedVideoPlaybackGrants.items = make(map[[sha256.Size]byte]sharedVideoPlaybackGrant, sharedVideoPlaybackMaxTickets)
	for i := 0; i < sharedVideoPlaybackMaxTickets; i++ {
		key := sha256.Sum256([]byte(strconv.Itoa(i)))
		sharedVideoPlaybackGrants.items[key] = sharedVideoPlaybackGrant{ExpiresAt: time.Now().Add(time.Hour)}
	}
	sharedVideoPlaybackGrants.Unlock()
	t.Cleanup(func() {
		sharedVideoPlaybackGrants.Lock()
		sharedVideoPlaybackGrants.items = original
		sharedVideoPlaybackGrants.Unlock()
	})
	_, err := issueSharedVideoPlaybackAt(sharedVideoPlaybackGrant{Kind: sharedVideoNote, Actor: 101, AssetID: strings.Repeat("d", 32)}, time.Now().Add(time.Minute))
	if !errors.Is(err, errSharedVideoPlaybackFull) {
		t.Fatalf("full pool error = %v", err)
	}
}

func TestProjectPackageSharedVideoPlaybackBindsVersionAndRevocation(t *testing.T) {
	svc, scope, actor, router, project, version, _, asset, video := createSharedVideoFixture(t)
	registerProjectPackageTestHandlers(router, svc, &scope, actor)
	delivery := createProjectPackage(t, router, project, version)
	*actor = 101
	issued := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-packages/shared/"+delivery.Token+"/versions/"+version.ID+"/media/"+asset.ID+"/playback", nil)
	if issued.Code != http.StatusOK {
		t.Fatal(issued.Code, issued.Body.String())
	}
	playback := playbackURL(t, issued.Body.Bytes())
	rangeReq := httptest.NewRequest(http.MethodGet, playback, nil)
	rangeReq.Header.Set("Range", "bytes=0-3")
	rangeRec := httptest.NewRecorder()
	router.ServeHTTP(rangeRec, rangeReq)
	if rangeRec.Code != http.StatusPartialContent || rangeRec.Body.String() != string(video[:4]) {
		t.Fatalf("package range=%d body=%q", rangeRec.Code, rangeRec.Body.String())
	}
	crossVersion := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-packages/shared/"+delivery.Token+"/versions/not-in-package/media/"+asset.ID+"/playback", nil)
	if crossVersion.Code != http.StatusNotFound {
		t.Fatalf("cross version=%d %s", crossVersion.Code, crossVersion.Body.String())
	}
	*actor = 7
	if rec := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/package-deliveries/"+delivery.ID+"/revoke", nil); rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec := projectNoteRequest(t, router, http.MethodGet, playback, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("revoked package=%d %s", rec.Code, rec.Body.String())
	}
}
