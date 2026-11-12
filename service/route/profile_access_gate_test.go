package route

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func profileGateTestRouter(t *testing.T, loader miniprogramProfileLoader) *gin.Engine {
	t.Helper()
	t.Setenv("MINIPROGRAM_PROFILE_GATE", "")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	middlewares := miniprogramProfileGateMiddlewares(
		func(c *gin.Context) {
			if c.GetHeader("token") != "valid" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
				return
			}
			c.Set("user_id", int64(41))
			c.Next()
		},
		loader,
	)
	if len(middlewares) != 2 {
		t.Fatalf("profile gate middleware count = %d", len(middlewares))
	}
	v1.Use(middlewares...)
	v1.Any("/miniprogram/*path", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
	v1.GET("/management/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
	return router
}

func profileGateRequest(router http.Handler, method, path, token string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	if token != "" {
		request.Header.Set("token", token)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestMiniprogramProfileGateAllowsPublishedTemplateDetailTransportWithoutProfile(t *testing.T) {
	loaded := false
	router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
		loaded = true
		return miniprogramProfile{}, nil
	})

	response := profileGateRequest(router, http.MethodGet, "/api/v1/miniprogram/templates/9", "")
	if response.Code != http.StatusOK || loaded {
		t.Fatalf("anonymous published template detail = %d loaded=%v body=%s", response.Code, loaded, response.Body.String())
	}
	response = profileGateRequest(router, http.MethodGet, "/api/v1/miniprogram/templates/9", "valid")
	if response.Code != http.StatusOK || loaded {
		t.Fatalf("incomplete-profile published template detail = %d loaded=%v body=%s", response.Code, loaded, response.Body.String())
	}
}

func TestMiniprogramProfileGateRequiresCompleteProfile(t *testing.T) {
	tests := []struct {
		name    string
		profile miniprogramProfile
		missing []string
	}{
		{name: "both", missing: []string{"nickname", "avatar"}},
		{name: "nickname", profile: miniprogramProfile{Avatar: "https://cdn.example.com/avatar.png"}, missing: []string{"nickname"}},
		{name: "avatar", profile: miniprogramProfile{Nickname: "用户"}, missing: []string{"avatar"}},
		{name: "temporary avatar", profile: miniprogramProfile{Nickname: "用户", Avatar: "wxfile://tmp/avatar.png"}, missing: []string{"avatar"}},
		{name: "default avatar", profile: miniprogramProfile{Nickname: "用户", Avatar: "https://cdn.example.com/assets/images/default-avatar.png"}, missing: []string{"avatar"}},
		{name: "ip avatar", profile: miniprogramProfile{Nickname: "用户", Avatar: "https://127.0.0.1/avatar.png"}, missing: []string{"avatar"}},
		{name: "invalid avatar escape", profile: miniprogramProfile{Nickname: "用户", Avatar: "https://cdn.example.com/avatar%zz.png"}, missing: []string{"avatar"}},
		{name: "invalid avatar utf8", profile: miniprogramProfile{Nickname: "用户", Avatar: "https://cdn.example.com/avatar%FF.png"}, missing: []string{"avatar"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) { return test.profile, nil })
			response := profileGateRequest(router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects", "valid")
			if response.Code != http.StatusPreconditionRequired {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			var body struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
				Data struct {
					ProfileRequired bool     `json:"profile_required"`
					MissingFields   []string `json:"missing_fields"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != 428 || body.Msg != "请先补充头像和昵称" || !body.Data.ProfileRequired || !reflect.DeepEqual(body.Data.MissingFields, test.missing) {
				t.Fatalf("profile gate body = %#v", body)
			}
		})
	}
}

func TestMiniprogramProfileGateAllowsCompleteProfile(t *testing.T) {
	router := profileGateTestRouter(t, func(_ context.Context, userID int64) (miniprogramProfile, error) {
		if userID != 41 {
			t.Fatalf("user id = %d", userID)
		}
		return miniprogramProfile{Nickname: "  用户  ", Avatar: "https://cdn.example.com/avatar.png"}, nil
	})
	response := profileGateRequest(router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", "valid")
	if response.Code != http.StatusOK {
		t.Fatalf("complete profile = %d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "private,no-store" {
		t.Fatalf("complete profile cache control = %q", got)
	}
}

func TestMiniprogramProfileGateAllowsOnlyAnonymousPublicBrowseAndTrackingTransport(t *testing.T) {
	loaded := 0
	router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
		loaded++
		return miniprogramProfile{}, nil
	})

	allowed := [][2]string{
		{http.MethodGet, "/api/v1/miniprogram/templates/featured"},
		{http.MethodGet, "/api/v1/miniprogram/templates/tab-config"},
		{http.MethodGet, "/api/v1/miniprogram/templates"},
		{http.MethodGet, "/api/v1/miniprogram/templates/search?q=test"},
		{http.MethodGet, "/api/v1/miniprogram/templates/9"},
		{http.MethodGet, "/api/v1/miniprogram/inspirations"},
		{http.MethodGet, "/api/v1/miniprogram/designers"},
		{http.MethodGet, "/api/v1/miniprogram/designers/9/homepage"},
		{http.MethodGet, "/api/v1/miniprogram/inspirations/9"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/contents/content-0001/versions/2"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/media/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{http.MethodHead, "/api/v1/miniprogram/tracking/media/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0/content"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0/media/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{http.MethodHead, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0/media/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{http.MethodPost, "/api/v1/miniprogram/tracking/entry"},
		{http.MethodPost, "/api/v1/miniprogram/tracking/events/batch"},
		{http.MethodPost, "/api/v1/miniprogram/tracking/shares"},
		{http.MethodPost, "/api/v1/miniprogram/public-share-links"},
	}
	for _, item := range allowed {
		response := profileGateRequest(router, item[0], item[1], "")
		if response.Code != http.StatusOK {
			t.Fatalf("anonymous transport %s %s = %d body=%s", item[0], item[1], response.Code, response.Body.String())
		}
	}
	if loaded != 0 {
		t.Fatalf("anonymous transport queried profile %d times", loaded)
	}

	protected := [][2]string{
		{http.MethodGet, "/api/v1/miniprogram/templates/0"},
		{http.MethodGet, "/api/v1/miniprogram/templates/-1"},
		{http.MethodGet, "/api/v1/miniprogram/templates/9/detail"},
		{http.MethodPost, "/api/v1/miniprogram/templates/9"},
		{http.MethodGet, "/api/v1/miniprogram/templates/featured/extra"},
		{http.MethodPost, "/api/v1/miniprogram/templates"},
		{http.MethodGet, "/api/v1/miniprogram/designers/0/homepage"},
		{http.MethodGet, "/api/v1/miniprogram/designers/9/homepage/extra"},
		{http.MethodGet, "/api/v1/miniprogram/inspirations/0"},
		{http.MethodGet, "/api/v1/miniprogram/inspirations/9/extra"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/entry"},
		{http.MethodPost, "/api/v1/miniprogram/tracking/media/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/signed"},
		{http.MethodGet, "/api/v1/miniprogram/public-share-links"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/contents/content-0001/versions/0"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/contents/content-0001/versions/2/extra"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/-1/content"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0/media/short"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0/media/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/extra"},
		{http.MethodHead, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2"},
		{http.MethodHead, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0/content"},
		{http.MethodHead, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0/media/short"},
		{http.MethodHead, "/api/v1/miniprogram/tracking/project-collections/collection-0001/versions/2/items/0/media/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/extra"},
	}
	for _, item := range protected {
		response := profileGateRequest(router, item[0], item[1], "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("protected transport %s %s = %d body=%s", item[0], item[1], response.Code, response.Body.String())
		}
	}
}

func TestMiniprogramProfileGateAllowsOnlyPublishedAIToolAndPricingReadsAnonymously(t *testing.T) {
	loaded := 0
	router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
		loaded++
		return miniprogramProfile{}, nil
	})

	allowed := []string{
		"/api/v1/miniprogram/ai/pricing?scenes=ai_draw_single",
		"/api/v1/miniprogram/ai-tools?category=architecture",
		"/api/v1/miniprogram/ai-tools/42",
		"/api/v1/miniprogram/ai-tools/code/rural-villa",
	}
	for _, path := range allowed {
		if response := profileGateRequest(router, http.MethodGet, path, ""); response.Code != http.StatusOK {
			t.Fatalf("anonymous AI read %s = %d body=%s", path, response.Code, response.Body.String())
		}
	}
	if loaded != 0 {
		t.Fatalf("anonymous AI reads queried profile %d times", loaded)
	}

	protected := []string{
		"/api/v1/miniprogram/ai/pricing/private",
		"/api/v1/miniprogram/ai-tools/0",
		"/api/v1/miniprogram/ai-tools/-1",
		"/api/v1/miniprogram/ai-tools/42/versions",
		"/api/v1/miniprogram/ai-tools/code",
		"/api/v1/miniprogram/ai-tools/code/rural-villa/private",
		"/api/v1/miniprogram/ai-tools/management",
	}
	for _, path := range protected {
		if response := profileGateRequest(router, http.MethodGet, path, ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("protected AI read %s = %d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestMiniprogramProfileGateAIAccountOperationsRequireLoginButNotProfile(t *testing.T) {
	loaded := 0
	router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
		loaded++
		return miniprogramProfile{}, nil
	})

	requests := [][2]string{
		{http.MethodPost, "/api/v1/miniprogram/ai/draw"},
		{http.MethodPost, "/api/v1/miniprogram/ai/chat"},
		{http.MethodPost, "/api/v1/miniprogram/ai/generate-prompt"},
		{http.MethodPost, "/api/v1/miniprogram/ai/upload-reference-image"},
		{http.MethodPost, "/api/v1/miniprogram/ai/rural-villa-design"},
		{http.MethodPost, "/api/v1/miniprogram/ai/allround-design"},
		{http.MethodPost, "/api/v1/miniprogram/ai/building-replacement"},
		{http.MethodPost, "/api/v1/miniprogram/ai/parent-child-design"},
		{http.MethodPost, "/api/v1/miniprogram/ai/cost/calculate"},
		{http.MethodPost, "/api/v1/miniprogram/ai/cost/doc"},
		{http.MethodPost, "/api/v1/miniprogram/ai/chat/stream"},
		{http.MethodPost, "/api/v1/miniprogram/ai/intent/stream"},
		{http.MethodPost, "/api/v1/miniprogram/ai/prompt/optimize"},
		{http.MethodPost, "/api/v1/miniprogram/ai/building-code/chat"},
		{http.MethodPost, "/api/v1/miniprogram/ai/task/status"},
		{http.MethodPost, "/api/v1/miniprogram/ai/video/create"},
		{http.MethodPost, "/api/v1/miniprogram/ai/video/generate-prompts"},
		{http.MethodGet, "/api/v1/miniprogram/ai/task/42/info"},
		{http.MethodGet, "/api/v1/miniprogram/ai/video/poll/task-42"},
		{http.MethodGet, "/api/v1/miniprogram/ai/video/query/task-42"},
		{http.MethodGet, "/api/v1/miniprogram/building-code-documents"},
		{http.MethodPost, "/api/v1/miniprogram/building-code-documents/upload"},
		{http.MethodDelete, "/api/v1/miniprogram/building-code-documents/42"},
		{http.MethodGet, "/api/v1/miniprogram/user/stones"},
		{http.MethodGet, "/api/v1/miniprogram/user/stones/summary"},
		{http.MethodPost, "/api/v1/miniprogram/user/stones/details"},
		{http.MethodGet, "/api/v1/miniprogram/user/tasks"},
		{http.MethodDelete, "/api/v1/miniprogram/user/tasks/task-42"},
		{http.MethodGet, "/api/v1/miniprogram/user/ai/download-config"},
		{http.MethodPost, "/api/v1/miniprogram/user/ai/wecom/verify-phone"},
		{http.MethodPost, "/api/v1/miniprogram/user/ai/task/download-original"},
		{http.MethodGet, "/api/v1/miniprogram/user/ai/task/download-original-file"},
		{http.MethodPost, "/api/v1/miniprogram/user/ai/video/download-original"},
		{http.MethodGet, "/api/v1/miniprogram/user/ai/video/download-original-file"},
	}
	for _, request := range requests {
		if response := profileGateRequest(router, request[0], request[1], ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous AI operation %s %s = %d body=%s", request[0], request[1], response.Code, response.Body.String())
		}
		if response := profileGateRequest(router, request[0], request[1], "valid"); response.Code != http.StatusOK {
			t.Fatalf("incomplete-profile AI operation %s %s = %d body=%s", request[0], request[1], response.Code, response.Body.String())
		}
	}
	if loaded != 0 {
		t.Fatalf("AI account operations queried profile %d times", loaded)
	}

	for _, request := range [][2]string{
		{http.MethodPost, "/api/v1/miniprogram/ai/unknown"},
		{http.MethodGet, "/api/v1/miniprogram/ai/task/42/info/private"},
		{http.MethodGet, "/api/v1/miniprogram/user/tasks/task-42"},
		{http.MethodPost, "/api/v1/miniprogram/project-notes/projects"},
	} {
		response := profileGateRequest(router, request[0], request[1], "valid")
		if response.Code != http.StatusPreconditionRequired {
			t.Fatalf("non-AI profile boundary %s %s = %d body=%s", request[0], request[1], response.Code, response.Body.String())
		}
	}
}

func TestMiniprogramProfileGateShareReadingRequiresCompleteProfile(t *testing.T) {
	requests := [][2]string{
		{http.MethodGet, "/api/v1/miniprogram/project-notes/shared/note-token"},
		{http.MethodGet, "/api/v1/miniprogram/project-notes/shared/note-token/media/media-id"},
		{http.MethodPost, "/api/v1/miniprogram/project-notes/shared/note-token/feedback"},
		{http.MethodPost, "/api/v1/miniprogram/project-notes/shared/note-token/confirm"},
		{http.MethodPost, "/api/v1/miniprogram/project-notes/shared/note-token/open"},
		{http.MethodPost, "/api/v1/miniprogram/project-notes/shared/note-token/media/media-id/playback"},
	}
	for _, request := range requests {
		t.Run(request[0]+" "+request[1], func(t *testing.T) {
			router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
				return miniprogramProfile{Nickname: "用户"}, nil
			})
			if response := profileGateRequest(router, request[0], request[1], "valid"); response.Code != http.StatusPreconditionRequired || !strings.Contains(response.Body.String(), `"missing_fields":["avatar"]`) {
				t.Fatalf("missing avatar share = %d body=%s", response.Code, response.Body.String())
			}
			router = profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
				return miniprogramProfile{Avatar: "https://cdn.example.com/avatar.png"}, nil
			})
			response := profileGateRequest(router, request[0], request[1], "valid")
			if response.Code != http.StatusPreconditionRequired || !strings.Contains(response.Body.String(), `"missing_fields":["nickname"]`) {
				t.Fatalf("missing nickname = %d body=%s", response.Code, response.Body.String())
			}
			router = profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
				return miniprogramProfile{Nickname: "用户", Avatar: "https://cdn.example.com/avatar.png"}, nil
			})
			if response = profileGateRequest(router, request[0], request[1], "valid"); response.Code != http.StatusOK {
				t.Fatalf("complete share = %d body=%s", response.Code, response.Body.String())
			}
		})
	}
	packagePath := "/api/v1/miniprogram/project-packages/shared/package-token/versions/version-id/media/media-id/playback"
	router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
		return miniprogramProfile{Nickname: "用户"}, nil
	})
	if response := profileGateRequest(router, http.MethodPost, packagePath, "valid"); response.Code != http.StatusPreconditionRequired || !strings.Contains(response.Body.String(), `"missing_fields":["avatar"]`) {
		t.Fatalf("package still requires avatar = %d body=%s", response.Code, response.Body.String())
	}

	router = profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
		return miniprogramProfile{Nickname: "用户"}, nil
	})
	response := profileGateRequest(router, http.MethodPost, "/api/v1/miniprogram/project-notes/projects", "valid")
	if response.Code != http.StatusPreconditionRequired || !strings.Contains(response.Body.String(), `"missing_fields":["avatar"]`) {
		t.Fatalf("management still requires avatar = %d body=%s", response.Code, response.Body.String())
	}
}

func TestMiniprogramProfileGateProtectedErrorsAreNotCached(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		profile miniprogramProfile
		status  int
	}{
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "profile required", token: "valid", profile: miniprogramProfile{Nickname: "用户"}, status: http.StatusPreconditionRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) { return test.profile, nil })
			response := profileGateRequest(router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects", test.token)
			if response.Code != test.status {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Cache-Control"); got != "private,no-store" {
				t.Fatalf("cache control = %q", got)
			}
		})
	}
}

func TestMiniprogramProfileGateFailsClosedWhenProfileReadFails(t *testing.T) {
	router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
		return miniprogramProfile{}, errors.New("database unavailable")
	})
	response := profileGateRequest(router, http.MethodGet, "/api/v1/miniprogram/project-notes/projects", "valid")
	if response.Code != http.StatusServiceUnavailable || containsJSONQuoted(response.Body.String(), "database unavailable") {
		t.Fatalf("profile read failure = %d body=%s", response.Code, response.Body.String())
	}
}

func TestMiniprogramProfileGateBootstrapAllowlistIsExact(t *testing.T) {
	loaded := 0
	router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
		loaded++
		return miniprogramProfile{}, nil
	})
	allowed := [][2]string{
		{http.MethodOptions, "/api/v1/miniprogram/templates/9"},
		{http.MethodPost, "/api/v1/miniprogram/login"},
		{http.MethodPost, "/api/v1/miniprogram/login/password"},
		{http.MethodPost, "/api/v1/miniprogram/login/phone"},
		{http.MethodPost, "/api/v1/miniprogram/sms/send"},
		{http.MethodPost, "/api/v1/miniprogram/profile/password/recover"},
		{http.MethodGet, "/api/v1/miniprogram/profile"},
		{http.MethodPut, "/api/v1/miniprogram/profile/nickname"},
		{http.MethodPut, "/api/v1/miniprogram/profile/avatar"},
		{http.MethodPut, "/api/v1/miniprogram/profile/identity"},
		{http.MethodPost, "/api/v1/miniprogram/tracking/share-entry"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/config"},
		{http.MethodPost, "/api/v1/miniprogram/wecom/enterprise-wechat/callback"},
		{http.MethodGet, "/api/v1/miniprogram/templates/featured"},
		{http.MethodGet, "/api/v1/miniprogram/templates/tab-config"},
		{http.MethodGet, "/api/v1/miniprogram/templates"},
		{http.MethodGet, "/api/v1/miniprogram/templates/search"},
		{http.MethodGet, "/api/v1/miniprogram/templates/9"},
		{http.MethodGet, "/api/v1/miniprogram/inspirations"},
		{http.MethodGet, "/api/v1/miniprogram/designers"},
		{http.MethodPost, "/api/v1/miniprogram/tracking/entry"},
		{http.MethodPost, "/api/v1/miniprogram/tracking/events/batch"},
		{http.MethodPost, "/api/v1/miniprogram/tracking/shares"},
	}
	for _, item := range allowed {
		response := profileGateRequest(router, item[0], item[1], "")
		if response.Code != http.StatusOK {
			t.Fatalf("bootstrap %s %s = %d body=%s", item[0], item[1], response.Code, response.Body.String())
		}
	}
	if loaded != 0 {
		t.Fatalf("bootstrap queried profile %d times", loaded)
	}

	for _, path := range []string{
		"/api/v1/miniprogram/profile/designer",
		"/api/v1/miniprogram/profile/avatar/extra",
		"/api/v1/miniprogram/tracking/entry",
		"/api/v1/miniprogram/project-notes/shared/token",
	} {
		response := profileGateRequest(router, http.MethodGet, path, "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("business path %s = %d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestMiniprogramProfileGateDoesNotAffectOtherV1Surfaces(t *testing.T) {
	router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
		t.Fatal("management request queried mini program profile")
		return miniprogramProfile{}, nil
	})
	response := profileGateRequest(router, http.MethodGet, "/api/v1/management/ping", "")
	if response.Code != http.StatusOK {
		t.Fatalf("management = %d body=%s", response.Code, response.Body.String())
	}
}

func TestMiniprogramProfileMissingFieldsOrder(t *testing.T) {
	if got := miniprogramProfileMissingFields(miniprogramProfile{}); !reflect.DeepEqual(got, []string{"nickname", "avatar"}) {
		t.Fatalf("missing fields = %#v", got)
	}
}

func containsJSONQuoted(body, value string) bool {
	needle := `"` + value + `"`
	for index := 0; index+len(needle) <= len(body); index++ {
		if body[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
