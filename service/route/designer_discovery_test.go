package route

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestDesignerWorkPreviewsSortBeforeLimit(t *testing.T) {
	works := []gin.H{
		{"id": int64(1), "likes": int64(2), "created_at": time.Unix(1, 0), "main_tab": "template"},
		{"id": int64(2), "likes": int64(99), "created_at": time.Unix(2, 0), "main_tab": "template"},
		{"id": int64(3), "likes": int64(1), "created_at": time.Unix(3, 0), "main_tab": "template"},
		{"id": int64(4), "likes": int64(999), "created_at": time.Unix(4, 0), "main_tab": "material"},
	}
	popular := designerWorkPreviews(works, "template", false)
	latest := designerWorkPreviews(works, "template", true)
	if len(popular) != 2 || popular[0]["id"] != int64(2) || popular[1]["id"] != int64(1) {
		t.Fatalf("popular=%v", popular)
	}
	if latest[0]["id"] != int64(3) || latest[1]["id"] != int64(2) {
		t.Fatalf("latest=%v", latest)
	}
	if works[0]["id"] != int64(1) {
		t.Fatal("mutated original works")
	}
	if len(designerWorkPreviews(nil, "", false)) != 0 {
		t.Fatal("empty works")
	}
}
