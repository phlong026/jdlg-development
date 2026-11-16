package route

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func publishCollectionRouteContent(t *testing.T, svc *tracking.Service, scope tracking.Scope, title, key string) tracking.Content {
	t.Helper()
	ctx := context.Background()
	draft, err := svc.SaveContent(ctx, scope, "", tracking.Draft{Title: title, Body: "已发布正文", BodyFormat: "plain", Rights: "own", Owner: scope.Owners[0]})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), key)
	if err != nil {
		t.Fatal(err)
	}
	return result["content"].(tracking.Content)
}

func newProjectCollectionRouteFixture(t *testing.T, scope *tracking.Scope) (*gin.Engine, *tracking.Service) {
	t.Helper()
	return newProjectCollectionRouteFixtureWithProfiles(t, scope, nil)
}

func newProjectCollectionRouteFixtureWithProfiles(t *testing.T, scope *tracking.Scope, profiles projectNoteProfileResolver) (*gin.Engine, *tracking.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{
		Tenant: "workbench", ContentEnabled: true,
		Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}},
	})
	router := gin.New()
	management := router.Group("/workbench", func(c *gin.Context) { c.Set("workbench.scope", *scope); c.Next() })
	registerProjectCollectionHandlers(management, svc)
	registerPublicProjectCollectionHandlers(router.Group("/public"), svc, profiles)
	return router, svc
}

func projectCollectionRouteData[T any](t *testing.T, recorderBody []byte) T {
	t.Helper()
	var response struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(recorderBody, &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func TestProjectCollectionRoutesManagePublishAndPublicFixedItem(t *testing.T) {
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, svc := newProjectCollectionRouteFixture(t, &scope)
	first := publishCollectionRouteContent(t, svc, scope, "案例甲", "route-collection-content-a")
	second := publishCollectionRouteContent(t, svc, scope, "案例乙", "route-collection-content-b")

	candidates := workbenchRequest(t, router, http.MethodGet, "/workbench/project-collections/candidates?limit=20", nil)
	if candidates.Code != http.StatusOK || !strings.Contains(candidates.Body.String(), first.ID) || !strings.Contains(candidates.Body.String(), second.ID) {
		t.Fatalf("candidates=%d %s", candidates.Code, candidates.Body.String())
	}
	createdRec := workbenchRequest(t, router, http.MethodPost, "/workbench/project-collections", tracking.ProjectCollectionDraft{
		Title: "精选合集", Owner: "owner-a", IdempotencyKey: "route-collection-create",
		Items: []tracking.ProjectCollectionItemInput{{ContentID: second.ID, ContentVersion: 1}, {ContentID: first.ID, ContentVersion: 1}},
	})
	if createdRec.Code != http.StatusOK {
		t.Fatal(createdRec.Code, createdRec.Body.String())
	}
	created := projectCollectionRouteData[tracking.ProjectCollection](t, createdRec.Body.Bytes())
	if created.ID == "" || len(created.Items) != 2 || created.Items[0].ContentID != second.ID {
		t.Fatalf("created=%#v", created)
	}
	listed := workbenchRequest(t, router, http.MethodGet, "/workbench/project-collections?limit=20", nil)
	opened := workbenchRequest(t, router, http.MethodGet, "/workbench/project-collections/"+created.ID, nil)
	if listed.Code != http.StatusOK || opened.Code != http.StatusOK || !strings.Contains(listed.Body.String(), created.ID) {
		t.Fatalf("list=%d %s open=%d %s", listed.Code, listed.Body.String(), opened.Code, opened.Body.String())
	}
	previewRec := workbenchRequest(t, router, http.MethodPost, "/workbench/project-collections/"+created.ID+"/preview", map[string]any{"expected_revision": created.Revision})
	if previewRec.Code != http.StatusOK {
		t.Fatal(previewRec.Code, previewRec.Body.String())
	}
	preview := projectCollectionRouteData[tracking.ProjectCollectionPreview](t, previewRec.Body.Bytes())
	publishedRec := workbenchRequest(t, router, http.MethodPost, "/workbench/project-collections/"+created.ID+"/publish", map[string]any{
		"expected_revision": created.Revision, "preview_token": preview.PreviewToken, "idempotency_key": "route-collection-publish",
	})
	if publishedRec.Code != http.StatusOK {
		t.Fatal(publishedRec.Code, publishedRec.Body.String())
	}
	published := projectCollectionRouteData[tracking.PublishedProjectCollection](t, publishedRec.Body.Bytes())
	publicPath := "/public/project-collections/" + created.ID + "/versions/" + strconv.Itoa(published.Version)
	publicRec := workbenchRequest(t, router, http.MethodGet, publicPath, nil)
	targetRec := workbenchRequest(t, router, http.MethodGet, publicPath+"/items/0", nil)
	contentRec := workbenchRequest(t, router, http.MethodGet, publicPath+"/items/0/content", nil)
	if publicRec.Code != http.StatusOK || targetRec.Code != http.StatusOK || contentRec.Code != http.StatusOK ||
		!strings.Contains(targetRec.Body.String(), second.ID) || !strings.Contains(targetRec.Body.String(), "collection_id") || strings.Contains(targetRec.Body.String(), "st=") ||
		!strings.Contains(contentRec.Body.String(), "已发布正文") {
		t.Fatalf("public=%d %s target=%d %s content=%d %s", publicRec.Code, publicRec.Body.String(), targetRec.Code, targetRec.Body.String(), contentRec.Code, contentRec.Body.String())
	}
	if invalid := workbenchRequest(t, router, http.MethodGet, publicPath+"/items/not-a-position", nil); invalid.Code != http.StatusNotFound {
		t.Fatalf("invalid position opened first item: %d %s", invalid.Code, invalid.Body.String())
	}

	if _, err := svc.Revoke(context.Background(), scope, second.ID, second.Revision, "撤回来源", "route-collection-source-revoke"); err != nil {
		t.Fatal(err)
	}
	publicRec = workbenchRequest(t, router, http.MethodGet, publicPath, nil)
	targetRec = workbenchRequest(t, router, http.MethodGet, publicPath+"/items/0", nil)
	contentRec = workbenchRequest(t, router, http.MethodGet, publicPath+"/items/0/content", nil)
	if publicRec.Code != http.StatusOK || strings.Contains(publicRec.Body.String(), "案例乙") || targetRec.Code != http.StatusNotFound || contentRec.Code != http.StatusNotFound {
		t.Fatalf("revoked public=%d %s target=%d %s content=%d %s", publicRec.Code, publicRec.Body.String(), targetRec.Code, targetRec.Body.String(), contentRec.Code, contentRec.Body.String())
	}
}

func TestPublishedProjectCollectionMediaSupportsHeadRangeAndRevocation(t *testing.T) {
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	t.Setenv("CONTENT_MEDIA_STORAGE", "filesystem")
	mediaDir := t.TempDir()
	t.Setenv("TRACKING_MEDIA_DIR", mediaDir)
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, svc := newProjectCollectionRouteFixture(t, &scope)
	video := []byte("0123456789abcdef")
	mediaID := strings.Repeat("c", 32)
	if err := os.WriteFile(filepath.Join(mediaDir, mediaID), video, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddMedia(context.Background(), scope, tracking.Asset{ID: mediaID, Kind: "video", MIME: "video/mp4", Width: 1280, Height: 720, URL: "/api/v1/miniprogram/tracking/media/" + mediaID}, "owner-a"); err != nil {
		t.Fatal(err)
	}
	draft, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "合集视频", Body: "视频正文", BodyFormat: "plain", Rights: "own", Owner: "owner-a", MediaIDs: []string{mediaID}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	publishedResult, err := svc.Publish(context.Background(), scope, draft.ID, draft.Revision, preview["preview_token"].(string), "collection-media-source-publish")
	if err != nil {
		t.Fatal(err)
	}
	source := publishedResult["content"].(tracking.Content)
	collection, err := svc.SaveProjectCollection(context.Background(), scope, "", tracking.ProjectCollectionDraft{
		Title: "视频合集", Owner: "owner-a", IdempotencyKey: "collection-media-create",
		Items: []tracking.ProjectCollectionItemInput{{ContentID: source.ID, ContentVersion: source.Version}},
	})
	if err != nil {
		t.Fatal(err)
	}
	collectionPreview, err := svc.PreviewProjectCollection(context.Background(), scope, collection.ID, collection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	publishedCollection, err := svc.PublishProjectCollection(context.Background(), scope, collection.ID, collection.Revision, collectionPreview.PreviewToken, "collection-media-publish")
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := "/public/project-collections/" + collection.ID + "/versions/" + strconv.Itoa(publishedCollection.Version) + "/items/0/media/" + mediaID

	headResponse := httptest.NewRecorder()
	router.ServeHTTP(headResponse, httptest.NewRequest(http.MethodHead, mediaPath, nil))
	if headResponse.Code != http.StatusOK || headResponse.Body.Len() != 0 || headResponse.Header().Get("Content-Length") != strconv.Itoa(len(video)) {
		t.Fatalf("anonymous HEAD=%d length=%q body=%q", headResponse.Code, headResponse.Header().Get("Content-Length"), headResponse.Body.String())
	}
	rangeRequest := httptest.NewRequest(http.MethodGet, mediaPath, nil)
	rangeRequest.Header.Set("Range", "bytes=4-7")
	rangeResponse := httptest.NewRecorder()
	router.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent || !bytes.Equal(rangeResponse.Body.Bytes(), video[4:8]) || rangeResponse.Header().Get("Content-Range") == "" {
		t.Fatalf("anonymous Range=%d headers=%v body=%q", rangeResponse.Code, rangeResponse.Header(), rangeResponse.Body.String())
	}

	if _, err = svc.Revoke(context.Background(), scope, source.ID, source.Revision, "撤回视频", "collection-media-source-revoke"); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, mediaPath, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("revoked %s=%d body=%q", method, response.Code, response.Body.String())
		}
	}
}

func TestProjectCollectionGeneralDeleteRoute(t *testing.T) {
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, svc := newProjectCollectionRouteFixture(t, &scope)
	source := publishCollectionRouteContent(t, svc, scope, "删除来源", "route-collection-delete-source")
	createdRec := workbenchRequest(t, router, http.MethodPost, "/workbench/project-collections", tracking.ProjectCollectionDraft{Title: "待删除合集", Owner: "owner-a", IdempotencyKey: "route-collection-delete-create", Items: []tracking.ProjectCollectionItemInput{{ContentID: source.ID, ContentVersion: 1}}})
	created := projectCollectionRouteData[tracking.ProjectCollection](t, createdRec.Body.Bytes())
	deleted := workbenchRequest(t, router, http.MethodPost, "/workbench/project-collections/"+created.ID+"/delete", map[string]any{"expected_revision": created.Revision})
	if deleted.Code != http.StatusOK {
		t.Fatal(deleted.Code, deleted.Body.String())
	}
}

func TestProjectCollectionManagementRoutesKeepActorAndOwnerScope(t *testing.T) {
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
	router, svc := newProjectCollectionRouteFixture(t, &scope)
	source := publishCollectionRouteContent(t, svc, scope, "隔离案例", "route-collection-isolation-source")
	createdRec := workbenchRequest(t, router, http.MethodPost, "/workbench/project-collections", tracking.ProjectCollectionDraft{
		Title: "隔离合集", Owner: "owner-a", IdempotencyKey: "route-collection-isolation-create",
		Items: []tracking.ProjectCollectionItemInput{{ContentID: source.ID, ContentVersion: 1}},
	})
	created := projectCollectionRouteData[tracking.ProjectCollection](t, createdRec.Body.Bytes())

	scope.Actor = 12
	if rec := workbenchRequest(t, router, http.MethodGet, "/workbench/project-collections/"+created.ID, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign actor read=%d %s", rec.Code, rec.Body.String())
	}
	scope.Actor = 11
	scope.Owners = []string{"owner-b"}
	if rec := workbenchRequest(t, router, http.MethodPatch, "/workbench/project-collections/"+created.ID, tracking.ProjectCollectionDraft{Title: "越权", Owner: "owner-b", ExpectedRevision: created.Revision, Items: []tracking.ProjectCollectionItemInput{{ContentID: source.ID, ContentVersion: 1}}}); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign owner write=%d %s", rec.Code, rec.Body.String())
	}
}

func TestCollectionCandidatesIntersectRequestedOwnerWithGrantedOwners(t *testing.T) {
	t.Setenv("CONTENT_WORKBENCH_ENABLED", "true")
	scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a", "owner-b"}, Manage: true}
	router, svc := newProjectCollectionRouteFixture(t, &scope)
	a := scope
	a.Owners = []string{"owner-a"}
	b := scope
	b.Owners = []string{"owner-b"}
	first := publishCollectionRouteContent(t, svc, a, "个人作品", "owner-candidate-a")
	second := publishCollectionRouteContent(t, svc, b, "公司作品", "owner-candidate-b")
	response := workbenchRequest(t, router, http.MethodGet, "/workbench/project-collections/candidates?owner=owner-a", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), first.ID) || strings.Contains(response.Body.String(), second.ID) {
		t.Fatalf("owner filter: %d %s", response.Code, response.Body.String())
	}
	denied := workbenchRequest(t, router, http.MethodGet, "/workbench/project-collections/candidates?owner=foreign-owner", nil)
	if denied.Code != 403 {
		t.Fatalf("foreign owner: %d %s", denied.Code, denied.Body.String())
	}
}

func TestPublicProjectCollectionContentKeepsStandaloneAuthorAndReadAvailability(t *testing.T) {
	for _, test := range []struct {
		name         string
		profileError error
	}{
		{name: "author available"},
		{name: "profile directory unavailable", profileError: errors.New("profile directory unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope := tracking.Scope{Tenant: "workbench", Actor: 11, Owners: []string{"owner-a"}, Manage: true}
			profiles := func(_ context.Context, ids []int64) (map[int64]model.UserProfilePresentation, error) {
				if len(ids) != 1 || ids[0] != scope.Actor {
					t.Fatalf("author profile IDs = %#v", ids)
				}
				return map[int64]model.UserProfilePresentation{11: {UserID: 11, Nickname: "陈设计师", Avatar: "https://cdn.example.com/author.png"}}, test.profileError
			}
			router, svc := newProjectCollectionRouteFixtureWithProfiles(t, &scope, profiles)
			registerPublicContentReadHandler(router.Group("/public"), svc, profiles)
			source := publishCollectionRouteContent(t, svc, scope, "真实作者的作品", "collection-author-content")
			ctx := context.Background()
			collection, err := svc.SaveProjectCollection(ctx, scope, "", tracking.ProjectCollectionDraft{
				Title: "作者合集", Owner: "owner-a", IdempotencyKey: "collection-author-create",
				Items: []tracking.ProjectCollectionItemInput{{ContentID: source.ID, ContentVersion: source.Version}},
			})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := svc.PreviewProjectCollection(ctx, scope, collection.ID, collection.Revision)
			if err != nil {
				t.Fatal(err)
			}
			published, err := svc.PublishProjectCollection(ctx, scope, collection.ID, collection.Revision, preview.PreviewToken, "collection-author-publish")
			if err != nil {
				t.Fatal(err)
			}
			paths := []string{
				"/public/contents/" + source.ID + "/versions/" + strconv.Itoa(source.Version),
				"/public/project-collections/" + collection.ID + "/versions/" + strconv.Itoa(published.Version) + "/items/0/content",
			}
			for _, path := range paths {
				response := workbenchRequest(t, router, http.MethodGet, path, nil)
				if response.Code != http.StatusOK {
					t.Fatalf("read %s = %d %s", path, response.Code, response.Body.String())
				}
				content := projectCollectionRouteData[tracking.Content](t, response.Body.Bytes())
				if content.ID != source.ID || content.Body != source.Body {
					t.Fatalf("read %s changed content: %#v", path, content)
				}
				if test.profileError == nil {
					if content.Author == nil || content.Author.DisplayName != "陈设计师" || content.Author.Avatar != "https://cdn.example.com/author.png" {
						t.Fatalf("read %s lost author: %#v", path, content.Author)
					}
				} else if content.Author != nil {
					t.Fatalf("read %s fabricated author after profile failure: %#v", path, content.Author)
				}
				if strings.Contains(response.Body.String(), "creator_id") {
					t.Fatalf("read %s exposed internal creator: %s", path, response.Body.String())
				}
			}
		})
	}
}
