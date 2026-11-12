package route

import (
	"service/model"
	"testing"
)

func TestChatStreamProviderFollowsEffectiveConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		config        *model.AIAPIConfig
		lao, ark      string
		provider      string
		completions   bool
		endpoint, key string
	}{
		{"environment primary", nil, "lao-test", "ark-test", "laozhang", true, laozhangChatBaseURL + "/chat/completions", "lao-test"},
		{"environment fallback", nil, "", "ark-test", "ark", false, arkBaseURL + "/responses", "ark-test"},
		{"empty database keeps environment", &model.AIAPIConfig{}, "", "ark-test", "ark", false, arkBaseURL + "/responses", "ark-test"},
		{"database default with no environment key", &model.AIAPIConfig{ProviderCode: "default", ProtocolType: "chat_sync", APIEndpoint: laozhangChatBaseURL + "/chat/completions", APIKey: "db-test"}, "", "", "laozhang", true, laozhangChatBaseURL + "/chat/completions", "db-test"},
		{"database primary overrides ark environment", &model.AIAPIConfig{APIEndpoint: laozhangChatBaseURL + "/chat/completions", APIKey: "db-test"}, "", "ark-test", "laozhang", true, laozhangChatBaseURL + "/chat/completions", "db-test"},
		{"database ark overrides primary environment", &model.AIAPIConfig{APIEndpoint: arkBaseURL + "/responses"}, "lao-test", "ark-test", "ark", false, arkBaseURL + "/responses", "ark-test"},
		{"never use ark credential for primary", &model.AIAPIConfig{APIEndpoint: laozhangChatBaseURL + "/chat/completions"}, "", "ark-test", "laozhang", true, laozhangChatBaseURL + "/chat/completions", ""},
		{"never use primary credential for ark", &model.AIAPIConfig{APIEndpoint: arkBaseURL + "/responses"}, "lao-test", "", "ark", false, arkBaseURL + "/responses", ""},
		{"explicit primary without endpoint", &model.AIAPIConfig{ProviderCode: "laozhang", APIKey: "db-test"}, "", "ark-test", "laozhang", true, laozhangChatBaseURL + "/chat/completions", "db-test"},
		{"explicit ark without endpoint", &model.AIAPIConfig{ProviderCode: "ark"}, "lao-test", "ark-test", "ark", false, arkBaseURL + "/responses", "ark-test"},
		{"ark completions retains ark model and key", &model.AIAPIConfig{ProviderCode: "ark", APIEndpoint: arkBaseURL + "/chat/completions"}, "lao-test", "ark-test", "ark", true, arkBaseURL + "/chat/completions", "ark-test"},
		{"ark host identifies completions supplier", &model.AIAPIConfig{APIEndpoint: arkBaseURL + "/chat/completions"}, "lao-test", "ark-test", "ark", true, arkBaseURL + "/chat/completions", "ark-test"},
		{"query does not change protocol", &model.AIAPIConfig{APIEndpoint: arkBaseURL + "/chat/completions/?next=/responses", APIKey: " db-test "}, "lao-test", "", "ark", true, arkBaseURL + "/chat/completions/?next=/responses", "db-test"},
		{"custom host keeps explicit ark", &model.AIAPIConfig{ProviderCode: "ark", APIEndpoint: "https://proxy.example/chat/completions"}, "lao-test", "ark-test", "ark", true, "https://proxy.example/chat/completions", "ark-test"},
		{"custom host keeps environment supplier and database key", &model.AIAPIConfig{APIEndpoint: "https://proxy.example/responses", APIKey: "db-test"}, "lao-test", "ark-test", "laozhang", false, "https://proxy.example/responses", "db-test"},
		{"protocol declaration is not supplier identity", &model.AIAPIConfig{ProtocolType: "chat_sync", APIEndpoint: "https://proxy.example/chat/completions", APIKey: "db-test"}, "", "ark-test", "ark", true, "https://proxy.example/chat/completions", "db-test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveChatStreamProvider(tt.config, tt.lao, tt.ark)
			if err != nil {
				t.Fatal(err)
			}
			wantModel := arkModel
			if tt.provider == "laozhang" {
				wantModel = laozhangChatModel
			}
			if got.provider != tt.provider || got.completions != tt.completions || got.endpoint != tt.endpoint || got.apiKey != tt.key || got.model != wantModel {
				t.Fatal("supplier, protocol, model, endpoint or credential selection mismatch")
			}
		})
	}
}

func TestChatStreamProviderRejectsConflictingKnownHost(t *testing.T) {
	for _, config := range []*model.AIAPIConfig{
		{ProviderCode: "ark", APIEndpoint: laozhangChatBaseURL + "/chat/completions", APIKey: "db-test"},
		{ProviderCode: "laozhang", APIEndpoint: arkBaseURL + "/responses", APIKey: "db-test"},
	} {
		got, err := resolveChatStreamProvider(config, "lao-test", "ark-test")
		if err == nil || got.apiKey != "" {
			t.Fatal("conflicting provider must fail before selecting a credential")
		}
	}
}

func TestChatStreamOfficialBackupDomain(t *testing.T) {
	const endpoint = "https://api2.laozhang.ai/v1/chat/completions"
	got, err := resolveChatStreamProvider(&model.AIAPIConfig{ProviderCode: "default", APIEndpoint: endpoint, APIKey: "db-test"}, "", "ark-test")
	if err != nil || got.provider != "laozhang" || got.model != laozhangChatModel || !got.completions || got.endpoint != endpoint || got.apiKey != "db-test" {
		t.Fatal("official backup must retain the primary supplier and protocol")
	}
	got, err = resolveChatStreamProvider(nil, "lao-test", "ark-test")
	if err != nil || got.endpoint != endpoint {
		t.Fatal("unconfigured endpoint must use the verified official backup")
	}
	got, err = resolveChatStreamProvider(&model.AIAPIConfig{ProviderCode: "ark", APIEndpoint: endpoint}, "lao-test", "ark-test")
	if err == nil || got.apiKey != "" {
		t.Fatal("backup domain must reject a conflicting supplier")
	}
	got, err = resolveChatStreamProvider(&model.AIAPIConfig{APIEndpoint: "https://api.laozhang.ai/v1/chat/completions", APIKey: "db-test"}, "", "ark-test")
	if err != nil || got.provider != "laozhang" || got.endpoint != "https://api.laozhang.ai/v1/chat/completions" {
		t.Fatal("explicit original domain remains supported")
	}
}
