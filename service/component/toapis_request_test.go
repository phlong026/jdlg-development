package component

import (
	"os"
	"service/config"
	"strings"
	"testing"
)

func TestToAPIsFallbackConfigsStayInToAPIs(t *testing.T) {
	primary := &AIAPIConfigData{
		TaskType:       "ai_draw",
		ProviderCode:   "toapis",
		ProviderName:   "ToAPIs",
		ProtocolType:   "toapis_async",
		APIEndpoint:    "https://toapis.com/v1/images/generations",
		Method:         "POST",
		APIKey:         "test-key",
		APIKeyLocation: "header_bearer",
		APIKeyName:     "Authorization",
		Headers:        `{"Content-Type":"application/json"}`,
		BodyTemplate:   `{"model":"gemini-3-pro-image-preview","prompt":"{{prompt}}","size":"{{aspect_ratio}}","n":1,"metadata":{"resolution":"{{image_size}}"}}`,
	}

	fallbacks := (&AITaskProcessor{}).getFallbackAIConfigs("ai_draw", true, primary)
	if len(fallbacks) != 2 {
		t.Fatalf("fallback count = %d, want 2", len(fallbacks))
	}

	wantModels := []string{"gpt-image-2", "gemini-3.1-flash-image-preview"}
	for i, fallback := range fallbacks {
		if fallback.ProviderCode != "toapis" {
			t.Fatalf("fallback[%d].ProviderCode = %q, want toapis", i, fallback.ProviderCode)
		}
		if fallback.ProtocolType != "toapis_async" {
			t.Fatalf("fallback[%d].ProtocolType = %q, want toapis_async", i, fallback.ProtocolType)
		}
		if fallback.APIEndpoint != primary.APIEndpoint {
			t.Fatalf("fallback[%d].APIEndpoint = %q, want primary endpoint", i, fallback.APIEndpoint)
		}
		if fallback.APIKey != primary.APIKey {
			t.Fatalf("fallback[%d].APIKey was not copied", i)
		}
		if got := resolveConfiguredModel(fallback); got != wantModels[i] {
			t.Fatalf("fallback[%d] model = %q, want %q", i, got, wantModels[i])
		}
	}
}

func TestRuntimeAIDrawProviderDefaultsUseEnvironmentKeys(t *testing.T) {
	processor := &AITaskProcessor{
		cfg: &config.Config{
			AI: config.AIConfig{
				LaoZhangAPIKey: "laozhang-env-key",
				ToAPIsAPIKey:   "toapis-env-key",
			},
		},
	}

	laozhang := processor.applyRuntimeAIDrawProviderDefaults(&AIAPIConfigData{
		TaskType:       "ai_draw",
		ProviderCode:   "laozhang",
		ProviderName:   "数据库老张配置",
		ProtocolType:   "sync",
		APIEndpoint:    "https://wrong.example.com/v1/chat/completions",
		Method:         "POST",
		APIKey:         "db-key",
		APIKeyLocation: "none",
		BodyTemplate:   `{"messages":[{"role":"user","content":"{{prompt}}"}]}`,
	}, "ai_draw", true)
	if laozhang.APIKey != "laozhang-env-key" {
		t.Fatalf("laozhang APIKey = %q, want env key", laozhang.APIKey)
	}
	if laozhang.APIEndpoint != geminiDrawGenerateContentEndpoint {
		t.Fatalf("laozhang endpoint = %q, want built-in generateContent endpoint", laozhang.APIEndpoint)
	}
	if laozhang.ProtocolType != "gemini_sync" {
		t.Fatalf("laozhang protocol = %q, want gemini_sync", laozhang.ProtocolType)
	}
	if !strings.Contains(laozhang.BodyTemplate, "inline_data") {
		t.Fatalf("laozhang image request template should be the built-in image template")
	}

	toapis := processor.applyRuntimeAIDrawProviderDefaults(&AIAPIConfigData{
		TaskType:       "ai_draw",
		ProviderCode:   "toapis",
		ProviderName:   "数据库 ToAPIs 配置",
		ProtocolType:   "sync",
		APIEndpoint:    "https://wrong.example.com",
		Method:         "POST",
		APIKey:         "db-key",
		APIKeyLocation: "none",
		BodyTemplate:   `{"model":"wrong","prompt":"{{prompt}}"}`,
	}, "ai_draw", false)
	if toapis.APIKey != "toapis-env-key" {
		t.Fatalf("toapis APIKey = %q, want env key", toapis.APIKey)
	}
	if toapis.APIEndpoint != toapisImageGenerationEndpoint {
		t.Fatalf("toapis endpoint = %q, want built-in ToAPIs endpoint", toapis.APIEndpoint)
	}
	if toapis.ProtocolType != "toapis_async" {
		t.Fatalf("toapis protocol = %q, want toapis_async", toapis.ProtocolType)
	}
	if got := resolveConfiguredModel(toapis); got != "gemini-3-pro-image-preview" {
		t.Fatalf("toapis model = %q, want built-in primary model", got)
	}
}

func TestBuildToAPIsGeminiRequestBody(t *testing.T) {
	ctx := &AITaskContext{
		TaskType: "ai_draw",
		APIConfig: &AIAPIConfigData{
			ProviderCode: "toapis",
			ProtocolType: "toapis_async",
			APIEndpoint:  "https://toapis.com/v1/images/generations",
			BodyTemplate: getToAPIsGeminiFlashDrawBodyTemplate(),
		},
		Payload: map[string]interface{}{
			"aspect_ratio": "9:16",
			"image_size":   "4K",
		},
		Prompt:    "make a poster",
		ImageURLs: []string{"https://example.com/a.png", "https://example.com/b.jpg"},
	}

	body, err := (&RequestPool{}).buildRequestBody(ctx)
	if err != nil {
		t.Fatalf("buildRequestBody returned error: %v", err)
	}
	if body["size"] != "9:16" {
		t.Fatalf("size = %v, want 9:16", body["size"])
	}
	metadata, ok := body["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata missing or wrong type: %#v", body["metadata"])
	}
	if metadata["resolution"] != "4K" {
		t.Fatalf("metadata.resolution = %v, want 4K", metadata["resolution"])
	}
	imageURLs, ok := body["image_urls"].([]interface{})
	if !ok || len(imageURLs) != 2 {
		t.Fatalf("image_urls = %#v, want 2 object items", body["image_urls"])
	}
	if _, exists := body["reference_images"]; exists {
		t.Fatalf("reference_images should not be set for Gemini ToAPIs body")
	}
	if _, exists := body["image_size"]; exists {
		t.Fatalf("image_size should not be kept as a top-level ToAPIs field")
	}
}

func TestBuildToAPIsGPTImage2RequestBody(t *testing.T) {
	ctx := &AITaskContext{
		TaskType: "ai_draw",
		APIConfig: &AIAPIConfigData{
			ProviderCode: "toapis",
			ProtocolType: "toapis_async",
			APIEndpoint:  "https://toapis.com/v1/images/generations",
			BodyTemplate: getToAPIsGPTImage2DrawBodyTemplate(),
		},
		Payload: map[string]interface{}{
			"aspect_ratio": "1:1",
			"image_size":   "2K",
		},
		Prompt:    "make a clean product image",
		ImageURLs: []string{"https://example.com/ref.png"},
	}

	body, err := (&RequestPool{}).buildRequestBody(ctx)
	if err != nil {
		t.Fatalf("buildRequestBody returned error: %v", err)
	}
	if body["size"] != "1:1" {
		t.Fatalf("size = %v, want 1:1", body["size"])
	}
	if body["resolution"] != "2K" {
		t.Fatalf("resolution = %v, want 2K", body["resolution"])
	}
	referenceImages, ok := body["reference_images"].([]string)
	if !ok || len(referenceImages) != 1 || referenceImages[0] != "https://example.com/ref.png" {
		t.Fatalf("reference_images = %#v, want URL string array", body["reference_images"])
	}
	if _, exists := body["image_urls"]; exists {
		t.Fatalf("image_urls should not be set for GPT-Image-2 ToAPIs body")
	}
}

func TestBuildGPTImage2VipRequestBody(t *testing.T) {
	ctx := &AITaskContext{
		TaskType: "ai_draw",
		APIConfig: &AIAPIConfigData{
			ProviderCode: "gpt-image-2-vip",
			ProtocolType: "openai_image_sync",
			APIEndpoint:  laoZhangImageGenerationEndpoint,
			BodyTemplate: getLaoZhangGPTImage2VipDrawBodyTemplate(true),
		},
		Payload: map[string]interface{}{
			"canvas":          "16:9",
			"quality":         "uhd",
			"service":         "rural_villa_design",
			"scene_direction": "front",
		},
		Prompt:   "make a rural villa render",
		ImageURL: "https://example.com/ref.png",
	}

	body, err := (&RequestPool{}).buildRequestBody(ctx)
	if err != nil {
		t.Fatalf("buildRequestBody returned error: %v", err)
	}
	if body["model"] != "gpt-image-2-vip" {
		t.Fatalf("model = %v, want gpt-image-2-vip", body["model"])
	}
	if body["size"] != "3840x2160" {
		t.Fatalf("size = %v, want 3840x2160", body["size"])
	}
	if body["image"] != "https://example.com/ref.png" {
		t.Fatalf("image = %v, want reference URL", body["image"])
	}
	for _, forbidden := range []string{"quality", "resolution", "canvas", "service", "scene_direction", "image_url", "image_size", "aspect_ratio"} {
		if _, exists := body[forbidden]; exists {
			t.Fatalf("%s should not be kept in GPT-Image-2 VIP request body: %#v", forbidden, body)
		}
	}
}

func TestExecuteToAPIsAsyncImageTasksKeepsPartialSuccess(t *testing.T) {
	sourceBytes, err := os.ReadFile("requests.go")
	if err != nil {
		t.Fatalf("read requests.go: %v", err)
	}
	source := string(sourceBytes)
	start := strings.Index(source, "func (p *RequestPool) executeToAPIsAsyncImageTasks(")
	end := strings.Index(source, "func (p *RequestPool) executeToAPIsAsyncImageTask(")
	if start < 0 || end <= start {
		t.Fatalf("could not locate executeToAPIsAsyncImageTasks source")
	}
	batchSource := source[start:end]

	if strings.Contains(batchSource, "if err != nil {\n\t\t\treturn nil, nil, nil, usedModel, usedEndpoint, err\n\t\t}") {
		t.Fatalf("ToAPIs async batch should not return immediately when one image fails")
	}
	for _, want := range []string{
		"attemptErrors = append(attemptErrors",
		"continue",
		"generated_count\":   len(watermarkedURLs)",
		"failed_count\":      len(attemptErrors)",
		"resultData[\"errors\"] = attemptErrors",
	} {
		if !strings.Contains(batchSource, want) {
			t.Fatalf("requests.go missing partial-success guard %q", want)
		}
	}
}
