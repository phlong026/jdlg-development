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
	router.GET("/api/v1/wechat/official/oauth/callback", func(c *gin.Context) { panic("must-not-log-secret") })
	router.GET("/ping", func(c *gin.Context) { c.Status(200) })
	for _, target := range []string{"/share/s/private-share-token?st=private-value", "/api/v1/wechat/official/oauth/callback?code=secret-code&state=secret-state", "/ping"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", target, nil))
	}
	joined := raw.String() + safe.String()
	for _, secret := range []string{"private-share-token", "private-value", "secret-code", "secret-state", "must-not-log-secret"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("credential in logs: %s", secret)
		}
	}
	if !strings.Contains(joined, "/share/s/:code") || !strings.Contains(raw.String(), "/ping") {
		t.Fatalf("missing sanitized/legacy observability: %s", joined)
	}
}
