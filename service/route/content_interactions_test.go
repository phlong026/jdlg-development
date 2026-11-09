package route

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func contentInteractionRouteFixture(t *testing.T) (*gin.Engine, *tracking.Service, tracking.Content) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	scope := tracking.Scope{Tenant: "interaction-route", Actor: 7, Owners: []string{"owner-a"}, Manage: true}
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{
		Tenant: "interaction-route", Enabled: true, ContentEnabled: true, DefaultOwner: "owner-a", ConsentVersion: "v1",
		Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}},
	})
	ctx := context.Background()
	draft, err := svc.SaveContent(ctx, scope, "", tracking.Draft{Title: "公开笔记", Body: "正文", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), "interaction-route-publish")
	if err != nil {
		t.Fatal(err)
	}
	content := published["content"].(tracking.Content)
	profiles := projectNoteProfileResolver(func(_ context.Context, ids []int64) (map[int64]model.UserProfilePresentation, error) {
		out := map[int64]model.UserProfilePresentation{}
		for _, id := range ids {
			out[id] = model.UserProfilePresentation{UserID: id, Nickname: "用户甲", Avatar: "https://example.com/avatar.png", IdentityType: "设计师"}
		}
		return out, nil
	})
	h := contentInteractionHandler{service: svc, profiles: profiles}
	router := gin.New()
	base := router.Group("/api/v1/miniprogram/tracking/contents/:id/versions/:version/interactions", func(c *gin.Context) {
		if value := c.GetHeader("test-user"); value == "11" {
			c.Set("user_id", int64(11))
		}
		c.Next()
	})
	base.GET("", h.get)
	base.POST("/comments", h.addComment)
	base.POST("/like", h.setLike)
	base.POST("/favorite", h.setFavorite)
	base.DELETE("/comments/:comment_id", h.deleteComment)
	return router, svc, content
}

func TestContentInteractionRoutesProjectOnlyPublicProfile(t *testing.T) {
	router, _, content := contentInteractionRouteFixture(t)
	path := "/api/v1/miniprogram/tracking/contents/" + content.ID + "/versions/1/interactions"

	comment := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path+"/comments", strings.NewReader(`{"text":"这套设计很实用"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("test-user", "11")
	router.ServeHTTP(comment, req)
	if comment.Code != http.StatusOK {
		t.Fatal(comment.Code, comment.Body.String())
	}
	if strings.Contains(comment.Body.String(), "actor_id") || strings.Contains(comment.Body.String(), "user_id") || !strings.Contains(comment.Body.String(), "用户甲") || !strings.Contains(comment.Body.String(), "avatar.png") {
		t.Fatalf("comment presentation leaked or omitted profile: %s", comment.Body.String())
	}

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, path+"?limit=20", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"comment_count":1`) || !strings.Contains(get.Body.String(), `"liked":false`) {
		t.Fatalf("anonymous GET = %d %s", get.Code, get.Body.String())
	}
	if strings.Contains(get.Body.String(), "identity_type") || strings.Contains(get.Body.String(), "actor_id") || strings.Contains(get.Body.String(), "user_id") {
		t.Fatalf("private profile fields leaked: %s", get.Body.String())
	}
}

func TestContentInteractionRoutesUseExplicitIdempotentState(t *testing.T) {
	router, _, content := contentInteractionRouteFixture(t)
	path := "/api/v1/miniprogram/tracking/contents/" + content.ID + "/versions/1/interactions"
	for _, endpoint := range []string{"like", "favorite"} {
		for i := 0; i < 2; i++ {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path+"/"+endpoint, strings.NewReader(`{"active":true}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("test-user", "11")
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"active":true`) || !strings.Contains(w.Body.String(), `"count":1`) {
				t.Fatalf("%s replay = %d %s", endpoint, w.Code, w.Body.String())
			}
		}
		missing := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path+"/"+endpoint, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("test-user", "11")
		router.ServeHTTP(missing, req)
		if missing.Code != http.StatusBadRequest {
			t.Fatalf("%s accepted missing active: %d %s", endpoint, missing.Code, missing.Body.String())
		}
	}
}

func TestContentInteractionProductionRoutesRequireAuthenticationForWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{Tenant: "route-auth", ContentEnabled: true})
	registerContentInteractionRoutes(router.Group("/api/v1/miniprogram/tracking"), svc, &model.UserModel{}, nil, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/miniprogram/tracking/contents/content-001/versions/1/interactions/like", strings.NewReader(`{"active":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned write = %d %s", w.Code, w.Body.String())
	}

	registered := false
	for _, route := range router.Routes() {
		if route.Method == http.MethodDelete && strings.HasSuffix(route.Path, "/interactions/comments/:comment_id") {
			registered = true
		}
	}
	if !registered {
		encoded, _ := json.Marshal(router.Routes())
		t.Fatalf("comment deletion route missing: %s", encoded)
	}
}

func TestContentInteractionRegistrationDoesNotCachePersonalStateAndRequiresLogin(t *testing.T) {
	_, svc, content := contentInteractionRouteFixture(t)
	router := gin.New()
	registerContentInteractionRoutes(router.Group("/api/v1/miniprogram/tracking"), svc, nil, nil, nil)
	path := "/api/v1/miniprogram/tracking/contents/" + content.ID + "/versions/1/interactions"
	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, path, nil))
	if get.Code != http.StatusOK || get.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("personal interaction response must not be cached: %d %v", get.Code, get.Header())
	}
	for _, endpoint := range []string{"/comments", "/like", "/favorite"} {
		write := httptest.NewRecorder()
		router.ServeHTTP(write, httptest.NewRequest(http.MethodPost, path+endpoint, strings.NewReader(`{}`)))
		if write.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s = %d", endpoint, write.Code)
		}
	}
}
