package route

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"mime/multipart"
	"net/http/httptest"
	"service/tracking"
	"testing"
)

func TestTrackingMediaRequiresExplicitAuthorizedOwner(t *testing.T) {
	t.Setenv("TRACKING_MEDIA_DIR", t.TempDir())
	gin.SetMode(gin.TestMode)
	for _, owner := range []string{"", "outside"} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		writer.WriteField("rights", "own")
		writer.WriteField("owner_id", owner)
		writer.Close()
		router := gin.New()
		router.POST("/", func(c *gin.Context) {
			uploadTrackingMedia(c, nil, tracking.Scope{Manage: true, Owners: []string{"first", "second"}})
		})
		request := httptest.NewRequest("POST", "/", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 403 {
			t.Fatalf("owner %q response %d", owner, response.Code)
		}
	}
}
