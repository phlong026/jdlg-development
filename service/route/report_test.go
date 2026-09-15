package route

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/config"
	"service/report"
)

func reportTestRouter(module *ReportModule) *gin.Engine {
	router := gin.New()
	v1 := router.Group("/api/v1")
	RegisterReportRoutes(v1.Group("/public"), v1.Group("/private"), v1, module)
	return router
}

func TestDisabledReportsDoNotRequireDatabaseOrProviders(t *testing.T) {
	t.Setenv("REPORT_ENABLED", "false")
	module, err := NewReportModule(context.Background(), nil, nil)
	if err != nil || module != nil {
		t.Fatalf("disabled reports must preserve existing service startup: %v", err)
	}
	router := reportTestRouter(nil)
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/api/v1/public/reports/config", 200},
		{http.MethodPost, "/api/v1/private/reports", 503},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Fatalf("%s: %d", test.path, response.Code)
		}
		if test.status == 200 && !strings.Contains(response.Body.String(), `"enabled":false`) {
			t.Fatal("disabled module must not advertise a purchasable service")
		}
	}
}

func TestReportRequestsRequireLoginBeforeTouchingService(t *testing.T) {
	router := reportTestRouter(&ReportModule{})
	for _, path := range []string{"/api/v1/private/reports/report_123", "/api/v1/private/reports/report_123/document?mode=full"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != 401 {
			t.Fatalf("unowned report access returned %d", response.Code)
		}
		if !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("private content must not be cached")
		}
	}
}

func TestBindingFailureExplainsTheNextCustomerAction(t *testing.T) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	reportError(c, report.ErrBindingRequired)
	if response.Code != 403 || !strings.Contains(response.Body.String(), "添加企业微信") {
		t.Fatal("a binding failure must not look like a broken or downloadable report")
	}
}

func TestReportsCannotReuseThePublicAssetBucket(t *testing.T) {
	cfg := &config.Config{}
	cfg.COS.Bucket = "public-assets-123"
	t.Setenv("REPORT_COS_REGION", "ap-guangzhou")
	t.Setenv("REPORT_COS_PRIVATE_CONFIRMED", "true")
	for _, bucket := range []string{"public-assets-123", "bucket.example/escape", ""} {
		t.Setenv("REPORT_COS_BUCKET", bucket)
		if _, err := privateReportStorage(cfg); err == nil {
			t.Fatalf("unsafe bucket accepted: %q", bucket)
		}
	}
}
