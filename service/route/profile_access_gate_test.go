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
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.Use(
		miniprogramProfileAuthentication(func(c *gin.Context) {
			if c.GetHeader("token") != "valid" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
				return
			}
			c.Set("user_id", int64(41))
			c.Next()
		}),
		miniprogramProfileCompleteness(loader),
	)
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

func TestMiniprogramProfileGateRejectsOldAnonymousBusinessRequests(t *testing.T) {
	loaded := false
	router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
		loaded = true
		return miniprogramProfile{}, nil
	})

	response := profileGateRequest(router, http.MethodGet, "/api/v1/miniprogram/templates/9", "")
	if response.Code != http.StatusUnauthorized || loaded {
		t.Fatalf("anonymous business request = %d loaded=%v body=%s", response.Code, loaded, response.Body.String())
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
			response := profileGateRequest(router, http.MethodGet, "/api/v1/miniprogram/templates/9", "valid")
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
	response := profileGateRequest(router, http.MethodPost, "/api/v1/miniprogram/tracking/entry", "valid")
	if response.Code != http.StatusOK {
		t.Fatalf("complete profile = %d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "private,no-store" {
		t.Fatalf("complete profile cache control = %q", got)
	}
}

func TestMiniprogramProfileGateShareReadingRequiresNicknameButNotAvatar(t *testing.T) {
	paths := []string{"/api/v1/miniprogram/project-notes/shared/note-token/media/media-id/playback"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			router := profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
				return miniprogramProfile{Nickname: "用户"}, nil
			})
			if response := profileGateRequest(router, http.MethodPost, path, "valid"); response.Code != http.StatusOK {
				t.Fatalf("nickname-only share = %d body=%s", response.Code, response.Body.String())
			}
			router = profileGateTestRouter(t, func(context.Context, int64) (miniprogramProfile, error) {
				return miniprogramProfile{Avatar: "https://cdn.example.com/avatar.png"}, nil
			})
			response := profileGateRequest(router, http.MethodPost, path, "valid")
			if response.Code != http.StatusPreconditionRequired || !strings.Contains(response.Body.String(), `"missing_fields":["nickname"]`) {
				t.Fatalf("missing nickname = %d body=%s", response.Code, response.Body.String())
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
			response := profileGateRequest(router, http.MethodGet, "/api/v1/miniprogram/templates/9", test.token)
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
	response := profileGateRequest(router, http.MethodGet, "/api/v1/miniprogram/templates/9", "valid")
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
		{http.MethodPost, "/api/v1/miniprogram/ai/upload-reference-image"},
		{http.MethodPost, "/api/v1/miniprogram/tracking/share-entry"},
		{http.MethodGet, "/api/v1/miniprogram/tracking/config"},
		{http.MethodPost, "/api/v1/miniprogram/wecom/enterprise-wechat/callback"},
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
