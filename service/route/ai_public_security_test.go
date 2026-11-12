package route

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/model"
)

type aiPublicPricingReader struct {
	items map[string]*model.AIPricing
	errs  map[string]error
	seen  []string
}

func (f *aiPublicPricingReader) GetByScene(scene string) (*model.AIPricing, error) {
	f.seen = append(f.seen, scene)
	if err := f.errs[scene]; err != nil {
		return nil, err
	}
	if item := f.items[scene]; item != nil {
		return item, nil
	}
	return nil, sql.ErrNoRows
}

func publicPricingRequest(reader *aiPublicPricingReader, target string) *httptest.ResponseRecorder {
	router := gin.New()
	RegisterAIPublicRoutes(router.Group("/api/v1/miniprogram"), reader)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}

func TestAIPublicPricingRejectsUnknownAndExcessiveScenes(t *testing.T) {
	reader := &aiPublicPricingReader{items: map[string]*model.AIPricing{}, errs: map[string]error{}}
	unknown := publicPricingRequest(reader, "/api/v1/miniprogram/ai/pricing?scenes=ai_draw_single,private_provider_scene")
	if unknown.Code != http.StatusBadRequest || len(reader.seen) != 0 {
		t.Fatalf("unknown scene status=%d queried=%v body=%s", unknown.Code, reader.seen, unknown.Body.String())
	}

	reader.seen = nil
	excessive := publicPricingRequest(reader, "/api/v1/miniprogram/ai/pricing?scenes=ai_draw_single,ai_draw_multi,ai_chat_single,ai_video_1,ai_video_2,ai_video_3,ai_video_4,allround_design,parent_child_design")
	if excessive.Code != http.StatusBadRequest || len(reader.seen) != 0 {
		t.Fatalf("excessive scenes status=%d queried=%v body=%s", excessive.Code, reader.seen, excessive.Body.String())
	}
}

func TestAIPublicPricingReturnsOnlyDisplayConfiguration(t *testing.T) {
	reader := &aiPublicPricingReader{
		items: map[string]*model.AIPricing{
			"ai_video_1": {
				Scene:  "ai_video_1",
				Stones: 30,
				ExtraConfig: sql.NullString{Valid: true, String: `{
					"duration_min":{"api_key":"nested-secret"},"min_duration":5,"duration_max":12,"api_key":"top-secret",
					"provider":"private-provider","private":{"endpoint":"internal"}
				}`},
			},
		},
		errs: map[string]error{},
	}
	response := publicPricingRequest(reader, "/api/v1/miniprogram/ai/pricing?scenes=ai_video_1")
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"stones"`) && !strings.Contains(body, `"ai_video_1":30`) {
		t.Fatalf("public pricing status=%d body=%s", response.Code, body)
	}
	for _, forbidden := range []string{"top-secret", "nested-secret", "private-provider", "endpoint", "api_key", "provider", "private"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("public pricing leaked %q: %s", forbidden, body)
		}
	}
	for _, required := range []string{"duration_min", "duration_max"} {
		if !strings.Contains(body, required) {
			t.Fatalf("public pricing omitted %q: %s", required, body)
		}
	}
}

func TestAIPublicPricingHidesDatabaseErrors(t *testing.T) {
	reader := &aiPublicPricingReader{
		items: map[string]*model.AIPricing{},
		errs:  map[string]error{"ai_draw_single": errors.New("dial password=database-secret host=internal-db")},
	}
	response := publicPricingRequest(reader, "/api/v1/miniprogram/ai/pricing?scenes=ai_draw_single")
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "database-secret") || strings.Contains(response.Body.String(), "internal-db") {
		t.Fatalf("pricing error leaked status=%d body=%s", response.Code, response.Body.String())
	}
}

type fakeAIToolPublicReader struct {
	listErr error
	tool    *model.AITool
}

func (f *fakeAIToolPublicReader) GetByID(int64) (*model.AITool, error) {
	if f.tool != nil {
		return f.tool, nil
	}
	return nil, sql.ErrNoRows
}
func (f *fakeAIToolPublicReader) GetByCode(string) (*model.AITool, error) {
	if f.tool != nil {
		return f.tool, nil
	}
	return nil, sql.ErrNoRows
}
func (f *fakeAIToolPublicReader) List(string, string, *bool, int, int) ([]*model.AITool, error) {
	return nil, f.listErr
}
func (f *fakeAIToolPublicReader) Count(string, string, *bool) (int64, error) { return 0, nil }

func TestAIToolPublicResponseHidesGenerationRulesButManagementKeepsThem(t *testing.T) {
	tool := &model.AITool{
		ID: 1, Code: "architecture", Name: "建筑工具", Category: "architecture", IsPublished: true,
		PresetReferenceImages: `[{"id":"ref-1","name":"新中式","description":"公开说明","image_url":"https://cdn.example.com/preview.jpg","prompt_suffix":"internal reference rule"}]`,
		StylePresetsRaw:       `[{"id":"style-1","name":"写实","reference_mode":"preview_and_model","preview_image_url":"https://cdn.example.com/style.jpg","model_reference_image_url":"https://private.example.com/model.jpg","prompt_suffix":"internal style rule"},{"id":"style-2","name":"内部参考","reference_mode":"preview_and_model","model_reference_image_url":"https://private.example.com/model-only.jpg","prompt_suffix":"model only rule"}]`,
		DefaultPrompt:         "internal default prompt",
	}
	publicJSON := marshalJSONForTest(t, buildAIToolPublicResponse(tool))
	for _, forbidden := range []string{"prompt_suffix", "model_reference_image_url", "internal reference rule", "internal style rule", "private.example.com", "model-only.jpg"} {
		if strings.Contains(publicJSON, forbidden) {
			t.Fatalf("public AI tool leaked %q: %s", forbidden, publicJSON)
		}
	}
	for _, required := range []string{"ref-1", "style-1", "preview.jpg", "style.jpg", "preview_and_model"} {
		if !strings.Contains(publicJSON, required) {
			t.Fatalf("public AI tool omitted %q: %s", required, publicJSON)
		}
	}

	managementJSON := marshalJSONForTest(t, buildAIToolManagementResponse(tool))
	for _, required := range []string{"internal default prompt", "internal reference rule", "internal style rule", "private.example.com"} {
		if !strings.Contains(managementJSON, required) {
			t.Fatalf("management AI tool omitted %q: %s", required, managementJSON)
		}
	}
}

func TestAIToolServerGenerationStillUsesStoredPrivateRules(t *testing.T) {
	tool := &model.AITool{
		ID: 9, Code: "architecture", Name: "建筑工具", Category: "architecture", IsPublished: true,
		PresetReferenceImages: `[{"id":"ref-1","name":"新中式","image_url":"https://cdn.example.com/reference.jpg","prompt_suffix":"internal reference rule"}]`,
		StylePresetsRaw:       `[{"id":"style-1","name":"写实","reference_mode":"preview_and_model","preview_image_url":"https://cdn.example.com/style.jpg","model_reference_image_url":"https://private.example.com/model.jpg","prompt_suffix":"internal style rule"}]`,
		DefaultPrompt:         "internal default prompt",
	}
	payload := map[string]interface{}{
		"tool_id": 9, "reference_preset_id": "ref-1", "style_preset_id": "style-1",
		"user_prompt": "user request", "original_image_url": "https://cdn.example.com/original.jpg",
		"images": []string{"https://cdn.example.com/original.jpg"},
	}
	resolved, err := enrichAIToolPayload(payload, &fakeAIToolPublicReader{tool: tool})
	if err != nil || resolved != 9 {
		t.Fatalf("enrich result id=%d error=%v", resolved, err)
	}
	prompt := strings.TrimSpace(getStringFromPayload(payload, "prompt"))
	for _, required := range []string{"internal default prompt", "internal reference rule", "internal style rule", "user request"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("server prompt omitted %q: %s", required, prompt)
		}
	}
	images := extractStringArrayFromPayload(payload, "images")
	if !containsString(images, "https://private.example.com/model.jpg") {
		t.Fatalf("server model reference missing: %#v", images)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestAIToolPublicListHidesDatabaseErrors(t *testing.T) {
	router := gin.New()
	RegisterAIToolRoutes(router.Group("/api/v1/miniprogram"), &fakeAIToolPublicReader{listErr: errors.New("password=tool-db-secret")})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/ai-tools", nil))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "tool-db-secret") {
		t.Fatalf("AI tool error leaked status=%d body=%s", response.Code, response.Body.String())
	}
}

func marshalJSONForTest(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
