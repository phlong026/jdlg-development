package route

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func gateSwitchStubAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("token") != "valid" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
			return
		}
		c.Set("user_id", int64(41))
		c.Next()
	}
}

func gateSwitchStubLoader(context.Context, int64) (miniprogramProfile, error) {
	return miniprogramProfile{}, nil
}

func gateSwitchRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	for _, middleware := range miniprogramProfileGateMiddlewares(gateSwitchStubAuth(), gateSwitchStubLoader) {
		v1.Use(middleware)
	}
	v1.Any("/miniprogram/*path", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
	return router
}

func TestMiniprogramProfileGateEnabledByDefault(t *testing.T) {
	t.Setenv("MINIPROGRAM_PROFILE_GATE", "")
	middlewares := miniprogramProfileGateMiddlewares(gateSwitchStubAuth(), gateSwitchStubLoader)
	if len(middlewares) != 2 {
		t.Fatalf("默认必须挂载资料门禁中间件，实际 %d 个", len(middlewares))
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/project-notes/projects", nil)
	gateSwitchRouter(t).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("默认开关下匿名业务请求应被拒绝，实际 %d body=%s", response.Code, response.Body.String())
	}
}

func TestMiniprogramProfileGateSwitchOffSkipsGate(t *testing.T) {
	for _, value := range []string{"off", "OFF", "0", "false"} {
		t.Setenv("MINIPROGRAM_PROFILE_GATE", value)
		middlewares := miniprogramProfileGateMiddlewares(gateSwitchStubAuth(), gateSwitchStubLoader)
		if len(middlewares) != 0 {
			t.Fatalf("MINIPROGRAM_PROFILE_GATE=%s 应返回空中间件列表，实际 %d 个", value, len(middlewares))
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/project-notes/projects", nil)
		gateSwitchRouter(t).ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("MINIPROGRAM_PROFILE_GATE=%s 时匿名业务请求应放行，实际 %d body=%s", value, response.Code, response.Body.String())
		}
	}
}
