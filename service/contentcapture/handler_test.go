package contentcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type captureFunc func(context.Context, string, []string) ([]Video, error)

func (function captureFunc) Capture(ctx context.Context, articleURL string, ids []string) ([]Video, error) {
	return function(ctx, articleURL, ids)
}

type runtimeFunc struct {
	capture captureFunc
	ready   func(context.Context) error
}

func (runtime runtimeFunc) Capture(ctx context.Context, articleURL string, ids []string) ([]Video, error) {
	return runtime.capture(ctx, articleURL, ids)
}

func (runtime runtimeFunc) Ready(ctx context.Context) error { return runtime.ready(ctx) }

func TestHandlerRequiresBearerAuthentication(t *testing.T) {
	handler := NewHandler(true, "12345678901234567890123456789012", captureFunc(func(context.Context, string, []string) ([]Video, error) {
		t.Fatal("capture must not run")
		return nil, nil
	}))
	for _, authorization := range []string{"", "Bearer wrong"} {
		request := httptest.NewRequest(http.MethodPost, "/capture", bytes.NewBufferString(`{"article_url":"https://mp.weixin.qq.com/s/a","video_ids":["wxv_11111111"]}`))
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || response.Body.String() != "{\"videos\":[]}\n" {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
}

func TestHandlerReturnsDisabledWithoutRuntimeGates(t *testing.T) {
	handler := NewHandler(false, "12345678901234567890123456789012", nil)
	request := validCaptureRequest()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusFailedDependency || response.Body.String() != "{\"videos\":[]}\n" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandlerRejectsURLIDsUnknownFieldsAndOversizedLists(t *testing.T) {
	handler := NewHandler(true, "12345678901234567890123456789012", captureFunc(func(context.Context, string, []string) ([]Video, error) {
		t.Fatal("capture must not run")
		return nil, nil
	}))
	tests := []string{
		`{"article_url":"http://mp.weixin.qq.com/s/a","video_ids":["wxv_11111111"]}`,
		`{"article_url":"https://evil.example/s/a","video_ids":["wxv_11111111"]}`,
		`{"article_url":"https://mp.weixin.qq.com/s/a","video_ids":["bad"]}`,
		`{"article_url":"https://mp.weixin.qq.com/s/a","video_ids":["wxv_11111111"],"allowed_hosts":["evil.example"]}`,
	}
	ids := make([]string, 51)
	for index := range ids {
		ids[index] = "wxv_abcdefgh" + string(rune('A'+index%26)) + string(rune('a'+index/26))
	}
	oversized, _ := json.Marshal(map[string]any{"article_url": "https://mp.weixin.qq.com/s/a", "video_ids": ids})
	tests = append(tests, string(oversized))
	for _, body := range tests {
		request := httptest.NewRequest(http.MethodPost, "/capture", bytes.NewBufferString(body))
		request.Header.Set("Authorization", "Bearer 12345678901234567890123456789012")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
}

func TestHandlerReturnsOnlyVideoEnvelope(t *testing.T) {
	handler := NewHandler(true, "12345678901234567890123456789012", captureFunc(func(_ context.Context, articleURL string, ids []string) ([]Video, error) {
		if articleURL != "https://mp.weixin.qq.com/s/a" || len(ids) != 1 {
			t.Fatalf("input url=%s ids=%v", articleURL, ids)
		}
		return []Video{{ID: ids[0], URL: "https://mpvideo.qpic.cn/a.mp4?vid=" + ids[0], FormatID: 10002}}, nil
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, validCaptureRequest())
	if response.Code != http.StatusOK || response.Body.String() != "{\"videos\":[{\"source_id\":\"wxv_11111111\",\"url\":\"https://mpvideo.qpic.cn/a.mp4?vid=wxv_11111111\",\"format_id\":10002}]}\n" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHandlerReadinessIsAuthenticatedAndUsesRuntimeProbe(t *testing.T) {
	probes := 0
	handler := NewHandler(true, "12345678901234567890123456789012", runtimeFunc{
		capture: func(context.Context, string, []string) ([]Video, error) { return nil, nil },
		ready: func(context.Context) error {
			probes++
			return errors.New("chrome unavailable")
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || probes != 0 {
		t.Fatalf("unauthorized status=%d probes=%d", response.Code, probes)
	}

	request = httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	request.Header.Set("Authorization", "Bearer 12345678901234567890123456789012")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || probes != 1 || response.Body.String() != "{\"ready\":false}\n" {
		t.Fatalf("status=%d probes=%d body=%s", response.Code, probes, response.Body.String())
	}
}

func TestHandlerMapsBusyCaptureToServiceUnavailable(t *testing.T) {
	handler := NewHandler(true, "12345678901234567890123456789012", captureFunc(func(context.Context, string, []string) ([]Video, error) {
		return nil, fail("busy")
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, validCaptureRequest())
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func validCaptureRequest() *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/capture", bytes.NewBufferString(`{"article_url":"https://mp.weixin.qq.com/s/a","video_ids":["wxv_11111111"]}`))
	request.Header.Set("Authorization", "Bearer 12345678901234567890123456789012")
	return request
}
