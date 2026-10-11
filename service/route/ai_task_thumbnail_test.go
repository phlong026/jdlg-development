package route

import (
	"database/sql"
	"testing"

	"service/model"

	"github.com/gin-gonic/gin"
)

func TestBuildAITaskResponseDataExposesThumbnailFields(t *testing.T) {
	task := &model.AITask{
		ID:         7,
		TaskNo:     "task-thumb-1",
		Scene:      "ai_draw_single",
		Status:     "success",
		StonesUsed: 20,
		ResultPayload: sql.NullString{
			Valid: true,
			String: `{
				"url":"https://cdn.example.com/result-watermarked.jpg",
				"url_raw":"https://cdn.example.com/result-raw.jpg",
				"thumbnail_url":"https://cdn.example.com/result-thumb.jpg",
				"thumbnail_urls":["https://cdn.example.com/result-thumb.jpg"],
				"raw_images":["https://cdn.example.com/result-raw.jpg"]
			}`,
		},
	}

	response := buildAITaskResponseData(task, nil)

	if got := response["thumbnail_url"]; got != "https://cdn.example.com/result-thumb.jpg" {
		t.Fatalf("expected top-level thumbnail_url, got %#v", got)
	}

	thumbnailURLs, ok := response["thumbnail_urls"].([]string)
	if !ok || len(thumbnailURLs) != 1 || thumbnailURLs[0] != "https://cdn.example.com/result-thumb.jpg" {
		t.Fatalf("expected top-level thumbnail_urls, got %#v", response["thumbnail_urls"])
	}

	var result map[string]interface{}
	switch current := response["result"].(type) {
	case gin.H:
		result = current
	case map[string]interface{}:
		result = current
	default:
		t.Fatalf("expected result payload map, got %#v", response["result"])
	}

	if _, exists := result["url_raw"]; exists {
		t.Fatalf("url_raw should stay filtered out from miniprogram response")
	}
	if _, exists := result["raw_images"]; exists {
		t.Fatalf("raw_images should stay filtered out from miniprogram response")
	}
	if got := result["thumbnail_url"]; got != "https://cdn.example.com/result-thumb.jpg" {
		t.Fatalf("expected result thumbnail_url, got %#v", got)
	}
}
