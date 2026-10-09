package component

// ───────────────────────── 降级链回归脚本（二期批次 3 前置护栏） ─────────────────────────
// 目的：把当前硬编码降级链的行为用测试锁定。批次 3 把链路 DB 化改造后，跑同一套
// 测试对比——任何"非预期"的行为变化（顺序、模型、协议、模板关键字段）都会在这里失败。
// 覆盖四条路径中的三条纯配置路径；涉及 DB 的"全链失败退款"路径需要集成环境，见文件尾注释。

import (
	"strings"
	"testing"

	"service/config"
)

// 老张主链（主力为非 ToAPIs 配置）的降级顺序与关键约定：
// 备用1 = Gemini 3 Pro（gemini_sync），备用2 = Seedream 4.5（openai_image_sync）
func TestLaoZhangDrawFallbackChainRegression(t *testing.T) {
	primary := &AIAPIConfigData{
		TaskType:       "ai_draw",
		ProviderCode:   "gpt-image-2-vip",
		ProviderName:   "GPT-Image-2 VIP",
		ProtocolType:   "openai_image_sync",
		APIEndpoint:    "https://api2.laozhang.ai/v1/images/generations",
		Method:         "POST",
		APIKey:         "test-key",
		APIKeyLocation: "header_bearer",
		APIKeyName:     "Authorization",
		Headers:        `{"Content-Type":"application/json"}`,
		BodyTemplate:   `{"model":"gpt-image-2-vip","prompt":"{{prompt}}","size":"{{image_size}}","quality":"high","n":1}`,
	}
	// 约定修正：降级配置的密钥来自服务端配置（getLaoZhangAPIKey），不继承主配置的密钥
	processor := NewAITaskProcessor(nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{AI: config.AIConfig{LaoZhangAPIKey: "chain-key"}})
	for _, hasImage := range []bool{true, false} {
		fallbacks := processor.getFallbackAIConfigs("ai_draw", hasImage, primary)
		if len(fallbacks) != 2 {
			t.Fatalf("hasImage=%v fallback count = %d, want 2", hasImage, len(fallbacks))
		}

		if got := fallbacks[0].ProviderCode; got != "laozhang" {
			t.Errorf("fallback[0].ProviderCode = %q, want laozhang", got)
		}
		if got := fallbacks[0].ProtocolType; got != "gemini_sync" {
			t.Errorf("fallback[0].ProtocolType = %q, want gemini_sync（备用1 = Gemini 3 Pro）", got)
		}
		if !strings.Contains(fallbacks[0].APIEndpoint, "gemini-3-pro-image-preview:generateContent") {
			t.Errorf("fallback[0].APIEndpoint = %q, want gemini generateContent", fallbacks[0].APIEndpoint)
		}

		if got := fallbacks[1].ProtocolType; got != "openai_image_sync" {
			t.Errorf("fallback[1].ProtocolType = %q, want openai_image_sync（备用2 = Seedream 4.5）", got)
		}
		if got := resolveConfiguredModel(fallbacks[1]); !strings.HasPrefix(got, "seedream-4") {
			t.Errorf("fallback[1] model = %q, want seedream-4*", got)
		}

		// 降级配置必须带上服务端配置的密钥（上游同一账号体系）
		for i, fallback := range fallbacks {
			if fallback.APIKey != "chain-key" {
				t.Errorf("fallback[%d] 未携带配置密钥", i)
			}
			if fallback.APIKeyLocation != "header_bearer" {
				t.Errorf("fallback[%d].APIKeyLocation = %q, want header_bearer", i, fallback.APIKeyLocation)
			}
		}
		// 图生图路径：备用模板必须带图片占位（否则参考图会在降级时丢失）
		if hasImage {
			if !strings.Contains(fallbacks[0].BodyTemplate, "inline_data") {
				t.Errorf("图生图降级（备用1）模板缺 inline_data 图片占位")
			}
			if !strings.Contains(fallbacks[1].BodyTemplate, "{{image}}") && !strings.Contains(fallbacks[1].BodyTemplate, "image") {
				t.Errorf("图生图降级（备用2）模板缺图片字段")
			}
		}
	}
}

// 非绘画任务不降级：聊天任务返回空链
func TestChatTaskHasNoFallbackRegression(t *testing.T) {
	primary := &AIAPIConfigData{TaskType: "ai_chat", ProviderCode: "default", ProtocolType: "chat_sync"}
	if fallbacks := (&AITaskProcessor{}).getFallbackAIConfigs("ai_chat", false, primary); fallbacks != nil {
		t.Fatalf("聊天任务不应有降级链，got %d 条", len(fallbacks))
	}
}

// 内置兜底配置：无 DB 配置时的主力行为
func TestHardcodedDrawConfigRegression(t *testing.T) {
	processor := NewAITaskProcessor(nil, nil, nil, nil, nil, nil, nil, nil, &config.Config{AI: config.AIConfig{LaoZhangAPIKey: "regression-key"}})
	for _, hasImage := range []bool{true, false} {
		cfg := processor.getHardcodedAIConfig("ai_draw", hasImage)
		if cfg == nil {
			t.Fatalf("hasImage=%v 内置配置不应为空", hasImage)
		}
		if cfg.ProviderCode != "gpt-image-2-vip" {
			t.Errorf("hasImage=%v ProviderCode = %q, want gpt-image-2-vip", hasImage, cfg.ProviderCode)
		}
		if got := resolveConfiguredModel(cfg); got != "gpt-image-2-vip" {
			t.Errorf("hasImage=%v model = %q, want gpt-image-2-vip", hasImage, got)
		}
		if cfg.APIKey != "regression-key" {
			t.Errorf("内置配置未读取 LAOZHANG_API_KEY")
		}
		if hasImage && !strings.Contains(cfg.BodyTemplate, "image") {
			t.Errorf("图生图内置模板缺图片字段")
		}
	}
	chat := processor.getHardcodedAIConfig("ai_chat", false)
	if chat == nil {
		t.Fatal("聊天内置配置不应为空")
	}
	if chat.ProtocolType != "chat_sync" || !strings.Contains(chat.BodyTemplate, "messages") {
		t.Errorf("聊天内置配置应为 chat_sync + messages 模板, got %s / %s", chat.ProtocolType, chat.BodyTemplate)
	}
}

// ToAPIs 主链去重约定：备用模型不得与主力模型重复（详见 toapis_request_test.go 的既有用例，
// 此处补充"主力为非主流模型时两条备用都在"的场景，防止去重逻辑误杀）
func TestToAPIsFallbackDedupKeepsDistinctModelsRegression(t *testing.T) {
	primary := &AIAPIConfigData{
		TaskType:       "ai_draw",
		ProviderCode:   "toapis",
		ProviderName:   "ToAPIs",
		ProtocolType:   "toapis_async",
		APIEndpoint:    toapisImageGenerationEndpoint,
		Method:         "POST",
		APIKey:         "test-key",
		APIKeyLocation: "header_bearer",
		APIKeyName:     "Authorization",
		BodyTemplate:   `{"model":"seedream-4-5-251128","prompt":"{{prompt}}","size":"{{aspect_ratio}}","n":1}`,
	}
	fallbacks := (&AITaskProcessor{}).getFallbackAIConfigs("ai_draw", false, primary)
	if len(fallbacks) != 2 {
		t.Fatalf("fallback count = %d, want 2（主力 seedream 时 gpt-image-2 与 flash 两条都应保留）", len(fallbacks))
	}
	first := resolveConfiguredModel(fallbacks[0])
	second := resolveConfiguredModel(fallbacks[1])
	if first == second {
		t.Fatalf("两条备用模型不应相同: %s", first)
	}
	if first != "gpt-image-2" {
		t.Errorf("备用1 = %q, want gpt-image-2（顺序约定）", first)
	}
	if second != "gemini-3.1-flash-image-preview" {
		t.Errorf("备用2 = %q, want gemini-3.1-flash-image-preview（顺序约定）", second)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 涉及 DB 的"全链失败自动退款"路径（部分成功按张退、全败全退）无法用纯单测锁定，
// 需要集成环境：构造 ai_tasks 行 + mock 上游全败 → 断言 stones 退还与灵石明细。
// 批次 3 改造动工前，先在测试环境补齐该集成用例（排期 3c）。
// ─────────────────────────────────────────────────────────────────────────────
