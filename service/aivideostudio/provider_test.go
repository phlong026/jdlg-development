package aivideostudio

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type providerTransport func(*http.Request) (*http.Response, error)

func (f providerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func providerResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func testProvider(fn providerTransport) *metasoProvider {
	p := NewMetasoProvider("test-only-token").(*metasoProvider)
	p.client.Transport = fn
	return p
}

func validProviderRequest() GenerationRequest {
	return GenerationRequest{IdempotencyKey: "task-1", Mode: "text", Prompt: "镜头从庭院缓慢推进", Ratio: "16:9", Resolution: "768P", Duration: 4}
}

func TestProviderMissingKeyNeverCallsNetwork(t *testing.T) {
	p := NewMetasoProvider("")
	if _, err := p.Capabilities(context.Background()); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("capabilities: %v", err)
	}
	if _, err := p.Submit(context.Background(), validProviderRequest()); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("submit: %v", err)
	}
}

func TestProviderFourModesUseNativeProtocol(t *testing.T) {
	for _, mode := range []string{"text", "first_frame", "first_last_frame", "reference"} {
		t.Run(mode, func(t *testing.T) {
			input := validProviderRequest()
			input.Mode = mode
			if mode == "first_frame" || mode == "first_last_frame" {
				input.FirstFrameURL = "https://media.example.com/first.jpg"
				input.Ratio = "adaptive"
			}
			if mode == "first_last_frame" {
				input.LastFrameURL = "https://media.example.com/last.jpg"
			}
			if mode == "reference" {
				input.References = []GenerationReference{{MediaID: "stable-1", Type: "image", URL: "https://media.example.com/ref.jpg"}}
			}
			calls := 0
			p := testProvider(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != "https://metaso.cn/api/minimax/v2/video_generation" || r.Method != "POST" {
					t.Fatalf("wrong native endpoint: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer test-only-token" {
					t.Fatal("missing auth")
				}
				var body struct {
					Model   string           `json:"model"`
					Content []map[string]any `json:"content"`
					Ratio   string           `json:"ratio"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Model != "MiniMax-H3" || body.Ratio != input.Ratio {
					t.Fatalf("snapshot changed: %+v", body)
				}
				want := map[string]int{"text": 1, "first_frame": 2, "first_last_frame": 3, "reference": 2}[mode]
				if len(body.Content) != want {
					t.Fatalf("media count = %d, want %d", len(body.Content), want)
				}
				return providerResponse(200, `{"task_id":"external-1"}`), nil
			})
			receipt, err := p.Submit(context.Background(), input)
			if err != nil || receipt.ExternalID != "external-1" || calls != 1 {
				t.Fatalf("receipt=%+v err=%v calls=%d", receipt, err, calls)
			}
		})
	}
}

func TestProviderSubmissionUncertaintyNeverLooksRejected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"server error", 500, `{"error":"secret upstream detail"}`, ErrSubmissionUncertain},
		{"request timeout", 408, `{}`, ErrSubmissionUncertain},
		{"lost response", 200, `{}`, ErrSubmissionUncertain},
		{"invalid JSON", 200, `not json`, ErrSubmissionUncertain},
		{"unverified rejection", 422, `{"error":"secret upstream detail"}`, ErrSubmissionUncertain},
		{"rate limit", 429, `{}`, ErrSubmissionUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testProvider(func(*http.Request) (*http.Response, error) { return providerResponse(tc.status, tc.body), nil })
			_, err := p.Submit(context.Background(), validProviderRequest())
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("wrong or unsafe error: %v", err)
			}
		})
	}
	p := testProvider(func(*http.Request) (*http.Response, error) { return nil, errors.New("request included secret") })
	if _, err := p.Submit(context.Background(), validProviderRequest()); !errors.Is(err, ErrSubmissionUncertain) || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
}

func TestProviderRejectsInvalidInputsBeforeNetwork(t *testing.T) {
	for _, mutate := range []func(*GenerationRequest){
		func(r *GenerationRequest) { r.Ratio = "adaptive" },
		func(r *GenerationRequest) { r.FirstFrameURL = "https://media.example.com/stale.jpg" },
		func(r *GenerationRequest) {
			r.Mode = "first_last_frame"
			r.LastFrameURL = "https://media.example.com/tail.jpg"
		},
		func(r *GenerationRequest) { r.Duration = 16; r.Resolution = "2K" },
		func(r *GenerationRequest) {
			r.Mode = "reference"
			r.References = []GenerationReference{{Type: "audio", URL: "https://media.example.com/a.mp3", Duration: 16}}
		},
		func(r *GenerationRequest) {
			r.Mode = "first_frame"
			r.Ratio = "adaptive"
			r.FirstFrameURL = "http://localhost/image.jpg"
		},
	} {
		r := validProviderRequest()
		mutate(&r)
		p := testProvider(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid request reached provider")
			return nil, nil
		})
		if _, err := p.Submit(context.Background(), r); !errors.Is(err, ErrSubmissionRejected) {
			t.Fatalf("input=%+v err=%v", r, err)
		}
	}
}

func TestProviderQueryChecksIdentityAndPreservesPending(t *testing.T) {
	for _, tc := range []struct {
		body, status string
		wantErr      bool
	}{
		{`{"task":{"id":"external-1","status":"succeeded","content":{"url":"https://cdn.example.com/result.mp4"},"usage":{"output_seconds":4}}}`, "succeeded", false},
		{`{"task":{"id":"other-task","status":"succeeded","content":{"url":"https://cdn.example.com/result.mp4"}}}`, "", true},
		{`{"task":{"status":"unknown-new-state"}}`, "", true},
		{`{"items":[{"id":"other-task","status":"failed"}]}`, "", true},
		{`{"task":{"status":"succeeded","content":{"url":"http://127.0.0.1/secret"}}}`, "", true},
		{`{"task":{"status":"failed","error":{"message":"secret token"}}}`, "failed", false},
	} {
		p := testProvider(func(r *http.Request) (*http.Response, error) {
			if r.Method != "GET" || r.URL.Path != "/api/minimax/v2/query/video_generation/external-1" {
				t.Fatal(r.URL)
			}
			return providerResponse(200, tc.body), nil
		})
		result, err := p.Query(context.Background(), "external-1")
		if (err != nil) != tc.wantErr || result.Status != tc.status || strings.Contains(result.ErrorCode, "secret") {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}

func TestProviderDoesNotFollowRedirectsOrClaimCancellation(t *testing.T) {
	p := NewMetasoProvider("test-only-token").(*metasoProvider)
	if p.client.CheckRedirect == nil || p.client.CheckRedirect(&http.Request{}, nil) != http.ErrUseLastResponse {
		t.Fatal("credential redirect protection missing")
	}
	if err := p.Cancel(context.Background(), "external-1"); !errors.Is(err, ErrCancellationUnsupported) {
		t.Fatal(err)
	}
}

func TestProviderReferencePurposeAndOrderReachNativeRequest(t *testing.T) {
	input := validProviderRequest()
	input.Mode = "reference"
	input.References = []GenerationReference{
		{MediaID: "courtyard", Type: "image", URL: "https://media.example.com/garden.jpg", Position: 1, Purpose: "空间与材质"},
		{MediaID: "camera", Type: "video", URL: "https://media.example.com/motion.mp4", Position: 2, Duration: 5, Purpose: "镜头运动"},
		{MediaID: "rain", Type: "audio", URL: "https://media.example.com/rain.wav", Position: 3, Duration: 5, Purpose: "自然环境声"},
	}
	p := testProvider(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Content []map[string]any `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		prompt, _ := body.Content[0]["text"].(string)
		for _, expected := range []string{"图片1", "视频1", "音频1", "空间与材质", "镜头运动", "自然环境声"} {
			if !strings.Contains(prompt, expected) {
				t.Fatalf("missing binding %q in %q", expected, prompt)
			}
		}
		if body.Content[1]["name"] != "图片1" || body.Content[2]["type"] != "video_url" || body.Content[3]["type"] != "audio_url" {
			t.Fatalf("native order mismatch: %+v", body.Content)
		}
		return providerResponse(200, `{"task_id":"external-1"}`), nil
	})
	if _, err := p.Submit(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	input.References[1].Position = 1
	if _, err := p.Submit(context.Background(), input); !errors.Is(err, ErrSubmissionRejected) {
		t.Fatalf("mismatched position accepted: %v", err)
	}
}

func TestReferencePromptWithPurposesUsesProviderVisibleLimit(t *testing.T) {
	refs := []GenerationReference{{Type: "image", Purpose: "控制庭院风格", Position: 1}}
	if _, err := ReferencePromptWithPurposes(strings.Repeat("参", 7000), refs); err == nil {
		t.Fatal("combined prompt over provider limit was accepted")
	}
	refs[0].Purpose = strings.Repeat("用", 121)
	if _, err := ReferencePromptWithPurposes("庭院镜头", refs); err == nil {
		t.Fatal("purpose over provider limit was accepted")
	}
	refs[0].Purpose = "控制庭院风格"
	got, err := ReferencePromptWithPurposes("庭院镜头", refs)
	if err != nil || !strings.Contains(got, "图片1（引用编号1）：控制庭院风格") {
		t.Fatalf("prompt=%q err=%v", got, err)
	}
}
