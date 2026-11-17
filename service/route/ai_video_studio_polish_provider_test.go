package route

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"service/model"
)

func TestStudioPolishInstructionsPreserveModeAndMaterialBindings(t *testing.T) {
	for _, mode := range []string{"text", "first_frame", "first_last_frame", "reference"} {
		d := model.AIVideoStudioDraft{Mode: mode, Prompt: "庭院，禁止人物", ReferencePrompt: "平稳推进", Ratio: "9:16", Duration: 6, Resolution: "768p", Quantity: 1, References: json.RawMessage(`[]`)}
		if mode == "reference" {
			d.References = json.RawMessage(`[{"media_id":"private-image","media_type":"image","purpose":"建筑外观","position":1},{"media_id":"private-audio","media_type":"audio","purpose":"舒缓节奏","position":2}]`)
		}
		system, input, err := studioPolishInstructions(d)
		if err != nil || !strings.Contains(system, "保留") || !strings.Contains(input, mode) || !strings.Contains(input, "9:16") {
			t.Fatalf("%s: %s %s %v", mode, system, input, err)
		}
		if strings.Contains(input, "private-image") || strings.Contains(input, "private-audio") {
			t.Fatal("private material identifiers leaked to text provider")
		}
		if mode == "reference" && (!strings.Contains(input, "图片1") || !strings.Contains(input, "音频1") || !strings.Contains(input, "建筑外观")) {
			t.Fatal("missing numbered purposes", input)
		}
	}
}

func TestStudioPolishOutputChecksFinalLengthAndReferenceLabels(t *testing.T) {
	d := model.AIVideoStudioDraft{Mode: "reference", References: json.RawMessage(`[{"media_type":"image","position":1,"purpose":"保持建筑"}]`)}
	if got, err := validateStudioPolishOutput(d, "  图片1控制建筑，镜头平稳推进。  "); err != nil || got != "图片1控制建筑，镜头平稳推进。" {
		t.Fatal(got, err)
	}
	for _, output := range []string{"", strings.Repeat("景", 7000), "图片2控制建筑", "音频1控制节奏", `{"prompt":"推进","duration":12}`} {
		if _, err := validateStudioPolishOutput(d, output); err == nil {
			t.Fatalf("accepted invalid output: %.60s", output)
		}
	}
}

func TestStudioPolishHTTPProtocolsAreBoundedAndNeverReplay(t *testing.T) {
	for _, completions := range []bool{true, false} {
		t.Run(map[bool]string{true: "completions", false: "responses"}[completions], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("X-Request-ID") != "polish-fixture" {
					t.Error("credential or request identity mismatch")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["model"] != "configured-model" || body["stream"] != false {
					t.Error("bad request shape", body, err)
				}
				if completions {
					io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"平稳推进庭院"}}]}`)
				} else {
					io.WriteString(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"平稳推进庭院"}]}]}`)
				}
			}))
			defer server.Close()
			p := chatStreamProvider{endpoint: server.URL, apiKey: "fixture-key", completions: completions}
			got, uncertain, err := callStudioPolish(context.Background(), server.Client(), p, "configured-model", "polish-fixture", "规则", "用户文字")
			if err != nil || uncertain || got != "平稳推进庭院" || calls.Load() != 1 {
				t.Fatal(got, uncertain, err, calls.Load())
			}
		})
	}
	for _, test := range []struct {
		name    string
		status  int
		body    string
		unknown bool
	}{
		{"rejected", 400, `{}`, false}, {"throttled", 429, `{}`, false},
		{"server error", 503, `{}`, true}, {"timeout status", 408, `{}`, true},
		{"malformed", 200, `{`, false},
		{"truncated", 200, `{"choices":[{"finish_reason":"length","message":{"content":"截断"}}]}`, false},
		{"tool", 200, `{"choices":[{"finish_reason":"stop","message":{"content":"文字","tool_calls":[{}]}}]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(test.status)
				io.WriteString(w, test.body)
			}))
			defer s.Close()
			_, unknown, err := callStudioPolish(context.Background(), s.Client(), chatStreamProvider{endpoint: s.URL, completions: true}, "model", "task", "system", "input")
			if err == nil || unknown != test.unknown || calls != 1 {
				t.Fatal(unknown, err, calls)
			}
		})
	}
}

func TestStudioPolishTimeoutAndRedirectDoNotReplayOrLeakCredentials(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	_, uncertain, err := callStudioPolish(context.Background(), s.Client(), chatStreamProvider{endpoint: s.URL, apiKey: "fixture-key", completions: true}, "m", "task", "s", "u")
	if err == nil || !uncertain || redirected.Load() != 0 {
		t.Fatal(uncertain, err, redirected.Load())
	}
	delayed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer delayed.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, uncertain, err = callStudioPolish(ctx, delayed.Client(), chatStreamProvider{endpoint: delayed.URL, completions: true}, "m", "task", "s", "u")
	if err == nil || !uncertain {
		t.Fatal(uncertain, err)
	}
}
