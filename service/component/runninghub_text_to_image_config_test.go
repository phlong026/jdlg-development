package component

import (
	"encoding/json"
	"strings"
	"testing"

	"service/config"
)

func TestRunningHubTextToImageConfigUsesNewAppAndNodeFields(t *testing.T) {
	processor := NewAITaskProcessor(nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{
		AI: config.AIConfig{
			RunningHubAPIKey:           "test-key",
			RunningHubTextToImageAppID: "2049807115933499394",
			RunningHubInstanceType:     "default",
			RunningHubUsePersonalQueue: "false",
		},
	})

	cfg := processor.getRunningHubTextToImageConfig()
	if cfg == nil {
		t.Fatal("expected runninghub config")
	}
	if cfg.APIEndpoint != "https://www.runninghub.cn/openapi/v2/run/ai-app/2049807115933499394" {
		t.Fatalf("unexpected endpoint: %s", cfg.APIEndpoint)
	}
	if cfg.APIKeyLocation != "header_bearer" {
		t.Fatalf("expected bearer auth, got %s", cfg.APIKeyLocation)
	}
	if !json.Valid([]byte(cfg.BodyTemplate)) {
		t.Fatalf("body template should be valid JSON: %s", cfg.BodyTemplate)
	}
	for _, expected := range []string{
		`"nodeId":"19","fieldName":"text","fieldValue":"{{prompt}}"`,
		`"nodeId":"12","fieldName":"height","fieldValue":"{{height}}"`,
		`"nodeId":"12","fieldName":"width","fieldValue":"{{width}}"`,
		`"nodeId":"12","fieldName":"batch_size","fieldValue":"{{batch_size}}"`,
		`"instanceType":"default"`,
		`"usePersonalQueue":"false"`,
	} {
		if !strings.Contains(cfg.BodyTemplate, expected) {
			t.Fatalf("body template missing %s in %s", expected, cfg.BodyTemplate)
		}
	}
}

func TestRunningHubImageToImageConfigUsesAppAndNodeFields(t *testing.T) {
	processor := NewAITaskProcessor(nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{
		AI: config.AIConfig{
			RunningHubAPIKey:            "test-key",
			RunningHubImageToImageAppID: "2049809170538479617",
			RunningHubInstanceType:      "default",
			RunningHubUsePersonalQueue:  "false",
		},
	})

	cfg := processor.getRunningHubImageToImageConfig()
	if cfg == nil {
		t.Fatal("expected runninghub image-to-image config")
	}
	if cfg.APIEndpoint != "https://www.runninghub.cn/openapi/v2/run/ai-app/2049809170538479617" {
		t.Fatalf("unexpected endpoint: %s", cfg.APIEndpoint)
	}
	if cfg.APIKeyLocation != "header_bearer" {
		t.Fatalf("expected bearer auth, got %s", cfg.APIKeyLocation)
	}
	if !json.Valid([]byte(cfg.BodyTemplate)) {
		t.Fatalf("body template should be valid JSON: %s", cfg.BodyTemplate)
	}
	for _, expected := range []string{
		`"nodeId":"76","fieldName":"image","fieldValue":"{{original_image}}"`,
		`"nodeId":"118","fieldName":"image","fieldValue":"{{reference_image}}"`,
		`"nodeId":"111","fieldName":"batch_size","fieldValue":"{{batch_size}}"`,
		`"nodeId":"108","fieldName":"text","fieldValue":"{{prompt}}"`,
		`"instanceType":"default"`,
		`"usePersonalQueue":"false"`,
	} {
		if !strings.Contains(cfg.BodyTemplate, expected) {
			t.Fatalf("body template missing %s in %s", expected, cfg.BodyTemplate)
		}
	}
}

func TestRunningHubImageToImageBodyBuildSkipsGenericMultiImagePlaceholder(t *testing.T) {
	processor := NewAITaskProcessor(nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{
		AI: config.AIConfig{
			RunningHubAPIKey:            "test-key",
			RunningHubImageToImageAppID: "2049809170538479617",
			RunningHubInstanceType:      "default",
			RunningHubUsePersonalQueue:  "false",
		},
	})
	cfg := processor.getRunningHubImageToImageConfig()
	if cfg == nil {
		t.Fatal("expected runninghub image-to-image config")
	}

	pool := &RequestPool{}
	body, err := pool.buildRequestBody(&AITaskContext{
		TaskType:  "ai_draw",
		APIConfig: cfg,
		Payload: map[string]interface{}{
			"original_image_url":  "openapi/original.png",
			"reference_image_url": "openapi/reference.jpg",
			"batch_size":          1,
		},
		Prompt:    "MNMIX, keep original structure",
		ImageURLs: []string{"openapi/original.png", "openapi/reference.jpg"},
	})
	if err != nil {
		t.Fatalf("expected runninghub image-to-image body to build without generic contents handling, got %v", err)
	}
	nodeInfoList, ok := body["nodeInfoList"].([]interface{})
	if !ok || len(nodeInfoList) != 4 {
		t.Fatalf("unexpected nodeInfoList: %#v", body["nodeInfoList"])
	}
}
