package report

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDecisionConfigRequiresAgentAndAllowsNoLegacyPack(t *testing.T) {
	for _, key := range []string{"REPORT_ENABLED", "REPORT_PAYMENT_ENABLED", "REPORT_DECISION_ENABLED", "REPORT_MULTI_AGENT_ENABLED", "REPORT_PRODUCT_ID", "REPORT_PRICE_FEN", "REPORT_PACK_MANIFEST", "REPORT_PROTOCOL_VERSION", "REPORT_WORKER_EMBEDDED", "REPORT_WORKER_CONCURRENCY"} {
		t.Setenv(key, "")
	}
	t.Setenv("REPORT_ENABLED", "true")
	t.Setenv("REPORT_DECISION_ENABLED", "true")
	t.Setenv("REPORT_PAYMENT_ENABLED", "true")
	t.Setenv("REPORT_PRODUCT_ID", "decision-report")
	t.Setenv("REPORT_PRICE_FEN", "100")
	cfg, err := LoadConfig()
	if err != nil || !cfg.DecisionEnabled || cfg.WorkerConcurrency != 1 || !cfg.EmbeddedWorker {
		t.Fatalf("V5 config: %#v %v", cfg, err)
	}
	t.Setenv("REPORT_MULTI_AGENT_ENABLED", "false")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("V5 accepted without agents")
	}
	t.Setenv("REPORT_MULTI_AGENT_ENABLED", "true")
	t.Setenv("REPORT_WORKER_CONCURRENCY", "0")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("unbounded/zero workers accepted")
	}
}

func TestAgentHTTPAcceptsProviderMetadataAndRejectsTruncation(t *testing.T) {
	for _, finish := range []string{"stop", "length"} {
		t.Run(finish, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := json.Marshal(map[string]any{"id": "response-test", "object": "chat.completion", "created": 1, "model": "provider/model", "usage": map[string]int{"total_tokens": 4}, "choices": []any{map[string]any{"index": 0, "finish_reason": finish, "message": map[string]any{"role": "assistant", "content": "{\"ok\":true}"}}}})
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
			}))
			defer server.Close()
			client, err := NewHTTPAgentClient(MultiAgentHTTPConfig{Endpoint: server.URL, APIKey: "test-only-key", ConfigVersion: "test", PlannerModel: "provider/model", WriterModel: "provider/model", VisualModel: "provider/model", ReviewerModel: "provider/model", Timeout: time.Second, MaxResponseBytes: 4096}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.Run(context.Background(), AgentRequest{Role: "diagnosis", Model: "provider/model", PromptVersion: "test", SystemPrompt: "test", Input: json.RawMessage(`{}`)})
			if finish == "stop" && (err != nil || string(result) != `{"ok":true}`) {
				t.Fatalf("normal provider envelope rejected: %s %v", result, err)
			}
			if finish == "length" && err == nil {
				t.Fatal("truncated completion accepted")
			}
		})
	}
}

func TestAgentHTTPDoesNotFollowCredentialRedirects(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; fmt.Fprint(w, `{}`) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client, err := NewHTTPAgentClient(MultiAgentHTTPConfig{Endpoint: source.URL, APIKey: "test-only-key", ConfigVersion: "test", PlannerModel: "m", WriterModel: "m", VisualModel: "m", ReviewerModel: "m", Timeout: time.Second, MaxResponseBytes: 4096}, source.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Run(context.Background(), AgentRequest{Role: "diagnosis", Model: "m", PromptVersion: "test", SystemPrompt: "test", Input: json.RawMessage(`{}`)})
	if err == nil || reached {
		t.Fatalf("redirect followed=%v err=%v", reached, err)
	}
}
