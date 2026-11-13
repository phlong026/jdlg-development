package route

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func TestGenerationResultImagesOnlyTakesOutputs(t *testing.T) {
	payload := map[string]interface{}{}
	raw := `{
		"images": ["https://cdn.example.com/a.png", "https://cdn.example.com/a.png", ""],
		"reference_images": ["https://ref.example.com/upload.png"],
		"original_image": "https://tpl.example.com/origin.png",
		"other": {"image_urls": ["https://nested.example.com/x.png"]}
	}`
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	got := generationResultImages(payload)
	if len(got) != 1 || got[0] != "https://cdn.example.com/a.png" {
		t.Fatalf("只取结果图并去重，got %v", got)
	}

	single := map[string]interface{}{"image_url": "https://cdn.example.com/single.png"}
	if got := generationResultImages(single); len(got) != 1 {
		t.Fatalf("单图字段应命中，got %v", got)
	}
	if got := generationResultImages(map[string]interface{}{}); len(got) != 0 {
		t.Fatalf("空 payload 应返回空，got %v", got)
	}
	// 参考图/模板原图是任务输入，绝不能混进可发布内容
	if got := generationResultImages(map[string]interface{}{"reference_images": []interface{}{"https://ref.example.com/u.png"}}); len(got) != 0 {
		t.Fatalf("参考图不得进入结果图，got %v", got)
	}
}

func TestGenerationImportRequiresAuthAndValidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{Tenant: "workbench"})
	registerContentWorkbenchRoutes(r.Group("/api/v1"), svc, &model.UserModel{}, nil)

	// 未认证一律 401
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/miniprogram/workbench/contents/from-generation?user_id=1", nil)
	req.Header.Set("token", "client-asserted-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d %s", w.Code, w.Body.String())
	}
}

func TestGenerationImportFixturePathRejectsInvalidTaskNo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, svc := newWorkbenchHandlerFixture(t, &tracking.Scope{Actor: 7, Manage: true, Owners: []string{"owner-a"}})
	// from-generation 挂在带认证的 routes 版本；fixture 走无认证 handlers 组，补挂同前缀组并注入 scope。
	testScope := tracking.Scope{Actor: 7, Manage: true, Owners: []string{"owner-a"}}
	registerGenerationImportRoute(r.Group("/workbench", func(c *gin.Context) { c.Set("workbench.scope", testScope); c.Next() }), svc, &model.UserModel{})
	for _, body := range []string{`{}`, `{"task_no":"short"}`, `{"task_no":"../../etc","title":"x"}`, `{"task_no":"valid-task-0001","idempotency_key":"short"}`, `{"task_no":"valid-task-0001","idempotency_key":"../../invalid-key"}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/workbench/contents/from-generation", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d %s", body, w.Code, w.Body.String())
		}
	}
}
