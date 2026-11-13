package route

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestTrackingFilterKeepsShareWindowSeparateFromEntryCursor(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/?share_cursor=earlier-window&cursor=entry-page", nil)
	f, err := trackingFilter(c)
	if err != nil || f.ShareCursor != "earlier-window" || f.Cursor != "entry-page" {
		t.Fatalf("lost independent cursors: %#v err=%v", f, err)
	}
}
