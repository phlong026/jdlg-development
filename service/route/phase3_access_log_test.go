package route

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPhase3AccessLogsExcludeShareAndOAuthCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var raw, safe bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&safe)
	defer log.SetOutput(previous)
	router := gin.New()
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{Output: &raw, Skip: SkipPhase3RawAccessLog}), Phase3AccessLog())
	router.GET("/share/s/:code", func(c *gin.Context) { c.Status(404) })
	router.GET("/api/v1/miniprogram/project-notes/shared/:token", func(c *gin.Context) { c.Status(403) })
	router.GET("/api/v1/wechat/official/oauth/callback", func(c *gin.Context) { panic("must-not-log-secret") })
	router.POST("/api/v1/miniprogram/project-packages/shared/:token/versions/:version/media/:id/playback", func(c *gin.Context) { c.Status(200) })
	router.GET("/api/v1/shared-video/:ticket", func(c *gin.Context) { c.Status(206) })
	router.GET("/ping", func(c *gin.Context) { c.Status(200) })
	for _, target := range []string{"/share/s/private-share-token?st=private-value", "/api/v1/miniprogram/project-notes/shared/private-project-token?debug=private-project-query", "/api/v1/wechat/official/oauth/callback?code=secret-code&state=secret-state", "/api/v1/shared-video/private-video-ticket?exp=123&sig=private-video-signature", "/ping"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", target, nil))
	}
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/v1/miniprogram/project-packages/shared/private-package-token/versions/v1/media/m1/playback", nil))
	joined := raw.String() + safe.String()
	for _, secret := range []string{"private-share-token", "private-value", "private-project-token", "private-project-query", "secret-code", "secret-state", "must-not-log-secret", "private-video-ticket", "private-video-signature", "private-package-token"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("credential in logs: %s", secret)
		}
	}
	if !strings.Contains(joined, "/share/s/:code") || !strings.Contains(joined, "/api/v1/miniprogram/project-notes/shared/:token") || !strings.Contains(joined, "/api/v1/shared-video/:ticket") || !strings.Contains(raw.String(), "/ping") {
		t.Fatalf("missing sanitized/legacy observability: %s", joined)
	}
}
