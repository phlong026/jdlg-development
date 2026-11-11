package route

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"service/function"
	"service/model"
	"service/tracking"
)

func publishedPlaybackRouter(t *testing.T, svc *tracking.Service, profile miniprogramProfile) (*gin.Engine, string) {
	t.Helper()
	if err := function.InitCrypto("", "", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))); err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(profileGateAuthConnector{profile: profile})
	t.Cleanup(func() { db.Close() })
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { redisClient.Close() })
	session := model.CodeSession{SessionID: "published-playback-session", DeviceID: "published-playback-device", UserID: 41}
	rawSession, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if err = redisClient.HSet(context.Background(), "code_session:session:"+session.SessionID, "data", rawSession).Err(); err != nil {
		t.Fatal(err)
	}
	token, err := function.GenerateToken(session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	users := model.NewUserModel(db)
	codes := model.NewCodeSessionRedisModel(db, redisClient)
	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.Use(
		miniprogramProfileAuthentication(TokenAuthRequired(codes)),
		miniprogramProfileCompleteness(miniprogramProfileLoaderFromModel(model.NewUserProfileModel(db))),
	)
	registerSharedVideoPlaybackRoutes(v1, svc, func(ctx context.Context, actor int64, kind sharedVideoKind) error {
		return verifyProjectPlaybackActor(ctx, db, actor, kind)
	})
	registerTrackingSignedMediaRoute(v1.Group("/miniprogram/tracking"), svc, users, codes)
	return router, token
}

func signedPlaybackRequest(t *testing.T, path, token, deviceID string) *http.Request {
	t.Helper()
	request := signedProfileGateRequest(t, path, token, deviceID)
	request.Method = http.MethodPost
	request.Body = io.NopCloser(strings.NewReader("{}"))
	request.ContentLength = 2
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestPublishedContentSignedMediaUsesSharedPlaybackTicket(t *testing.T) {
	t.Setenv("CONTENT_MEDIA_STORAGE", "filesystem")
	mediaDir := t.TempDir()
	t.Setenv("TRACKING_MEDIA_DIR", mediaDir)
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{
		Tenant: "published-playback", Enabled: true, ContentEnabled: true, VideoEnabled: true, DefaultOwner: "owner-a",
		Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}},
	})
	scope := tracking.Scope{Tenant: "published-playback", Actor: 7, Owners: []string{"owner-a"}, Manage: true}
	assetID := strings.Repeat("p", 32)
	asset := tracking.Asset{ID: assetID, Kind: "video", MIME: "video/mp4", Width: 1280, Height: 720, URL: "/api/v1/miniprogram/tracking/media/" + assetID}
	if err := svc.AddMedia(context.Background(), scope, asset, "owner-a"); err != nil {
		t.Fatal(err)
	}
	imageID := strings.Repeat("i", 32)
	image := tracking.Asset{ID: imageID, Kind: "image", MIME: "image/jpeg", Width: 640, Height: 480, URL: "/api/v1/miniprogram/tracking/media/" + imageID}
	if err := svc.AddMedia(context.Background(), scope, image, "owner-a"); err != nil {
		t.Fatal(err)
	}
	video := []byte("0000ftypisom-published-video-range")
	if err := os.WriteFile(filepath.Join(mediaDir, assetID), video, 0600); err != nil {
		t.Fatal(err)
	}
	draft, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "已发布视频", Body: "视频正文", BodyFormat: "plain", Rights: "own", Owner: "owner-a", MediaIDs: []string{assetID, imageID}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(context.Background(), scope, draft.ID, draft.Revision, preview["preview_token"].(string), "published-playback-publish")
	if err != nil {
		t.Fatal(err)
	}
	publishedContent := published["content"].(tracking.Content)

	router, token := publishedPlaybackRouter(t, svc, miniprogramProfile{Nickname: "用户", Avatar: "https://cdn.example.com/avatar.png"})
	issuePath := "/api/v1/miniprogram/tracking/media/" + assetID + "/signed"
	issueResponse := httptest.NewRecorder()
	router.ServeHTTP(issueResponse, signedPlaybackRequest(t, issuePath, token, "published-playback-device"))
	if issueResponse.Code != http.StatusOK {
		t.Fatalf("issue=%d body=%s", issueResponse.Code, issueResponse.Body.String())
	}
	playback := playbackURL(t, issueResponse.Body.Bytes())
	if strings.Contains(playback, "/miniprogram/tracking/media/") {
		t.Fatalf("legacy signed media URL returned: %s", playback)
	}
	imageIssueResponse := httptest.NewRecorder()
	router.ServeHTTP(imageIssueResponse, signedPlaybackRequest(t, "/api/v1/miniprogram/tracking/media/"+imageID+"/signed", token, "published-playback-device"))
	if imageIssueResponse.Code != http.StatusNotFound {
		t.Fatalf("image playback issue=%d body=%s", imageIssueResponse.Code, imageIssueResponse.Body.String())
	}
	rangeRequest := httptest.NewRequest(http.MethodGet, playback, nil)
	rangeRequest.Header.Set("Range", "bytes=4-11")
	rangeResponse := httptest.NewRecorder()
	router.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent || !bytes.Equal(rangeResponse.Body.Bytes(), video[4:12]) || rangeResponse.Header().Get("Content-Range") == "" {
		t.Fatalf("range=%d body=%q headers=%v", rangeResponse.Code, rangeResponse.Body.String(), rangeResponse.Header())
	}
	invalidRangeRequest := httptest.NewRequest(http.MethodGet, playback, nil)
	invalidRangeRequest.Header.Set("Range", "bytes=999-")
	invalidRangeResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidRangeResponse, invalidRangeRequest)
	if invalidRangeResponse.Code != http.StatusRequestedRangeNotSatisfiable || invalidRangeResponse.Header().Get("Content-Range") == "" {
		t.Fatalf("invalid range=%d headers=%v", invalidRangeResponse.Code, invalidRangeResponse.Header())
	}

	incompleteRouter, incompleteToken := publishedPlaybackRouter(t, svc, miniprogramProfile{Nickname: "用户"})
	incompleteResponse := httptest.NewRecorder()
	incompleteRouter.ServeHTTP(incompleteResponse, signedPlaybackRequest(t, issuePath, incompleteToken, "published-playback-device"))
	if incompleteResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("incomplete profile=%d body=%s", incompleteResponse.Code, incompleteResponse.Body.String())
	}
	incompletePlaybackResponse := httptest.NewRecorder()
	incompleteRouter.ServeHTTP(incompletePlaybackResponse, httptest.NewRequest(http.MethodGet, playback, nil))
	if incompletePlaybackResponse.Code != http.StatusPreconditionRequired || incompletePlaybackResponse.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("incomplete playback=%d cache=%q body=%s", incompletePlaybackResponse.Code, incompletePlaybackResponse.Header().Get("Cache-Control"), incompletePlaybackResponse.Body.String())
	}

	if _, err = svc.Revoke(context.Background(), scope, publishedContent.ID, publishedContent.Revision, "撤回公开视频", "published-playback-revoke"); err != nil {
		t.Fatal(err)
	}
	revokedResponse := httptest.NewRecorder()
	router.ServeHTTP(revokedResponse, httptest.NewRequest(http.MethodGet, playback, nil))
	if revokedResponse.Code != http.StatusNotFound || revokedResponse.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("revoked=%d cache=%q body=%s", revokedResponse.Code, revokedResponse.Header().Get("Cache-Control"), revokedResponse.Body.String())
	}
}

func TestPublishedMediaSigningKeepsExplicitAuthWhenGlobalGateIsOff(t *testing.T) {
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{Tenant: "published-playback", ContentEnabled: true, VideoEnabled: true})
	router := gin.New()
	registerTrackingSignedMediaRoute(router.Group("/api/v1/miniprogram/tracking"), svc, &model.UserModel{}, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/miniprogram/tracking/media/"+strings.Repeat("p", 32)+"/signed", strings.NewReader("{}")))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned gate-off issue=%d body=%s", response.Code, response.Body.String())
	}
}
