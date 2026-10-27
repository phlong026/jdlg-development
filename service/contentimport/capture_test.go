package contentimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type captureRoundTripper func(*http.Request) (*http.Response, error)

func (f captureRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func captureResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestCaptureWorkerValidatesResponseAndOrdersCandidates(t *testing.T) {
	client := &http.Client{Transport: captureRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://capture.internal/v1/capture" || request.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 32) {
			t.Fatalf("unexpected capture request target or authentication header")
		}
		return captureResponse(http.StatusOK, `{"videos":[
{"source_id":"wxv_11111111","url":"https://mpvideo.qpic.cn/low.mp4?vid=wxv_11111111","format_id":10001},
{"source_id":"wxv_22222222","url":"https://mpvideo.qpic.cn/two.mp4?vid=wxv_22222222","format_id":10002},
{"source_id":"wxv_11111111","url":"https://mpvideo.qpic.cn/candidate.mp4?vid=wxv_11111111","format_id":10003}]}`), nil
	})}
	worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", Secret: strings.Repeat("s", 32), HTTPClient: client})
	videos, err := worker.Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111", "wxv_22222222"})
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 2 || videos[0].FormatID != 10003 || videos[0].ID != "wxv_11111111" || videos[1].ID != "wxv_22222222" {
		t.Fatalf("videos=%#v", videos)
	}
}

func TestCaptureWorkerRejectsForgedHostAndVideoQuery(t *testing.T) {
	for name, body := range map[string]string{
		"host":  `{"videos":[{"source_id":"wxv_11111111","url":"https://mpvideo.qpic.cn.evil.example/a.mp4?vid=wxv_11111111"}]}`,
		"query": `{"videos":[{"source_id":"wxv_11111111","url":"https://mpvideo.qpic.cn/a.mp4?vid=wxv_99999999"}]}`,
		"id":    `{"videos":[{"source_id":"wxv_99999999","url":"https://mpvideo.qpic.cn/a.mp4?vid=wxv_99999999"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", Secret: strings.Repeat("s", 32), HTTPClient: &http.Client{Transport: captureRoundTripper(func(*http.Request) (*http.Response, error) {
				return captureResponse(http.StatusOK, body), nil
			})}})
			_, err := worker.Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"})
			var resolved *ResolveError
			if !errors.As(err, &resolved) || resolved.Code != "invalid_capture_response" {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestCaptureWorkerBoundsTimeoutAndResponse(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", Secret: strings.Repeat("s", 32), HTTPClient: &http.Client{Transport: captureRoundTripper(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})}, Timeout: time.Millisecond})
		_, err := worker.Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"})
		var resolved *ResolveError
		if !errors.As(err, &resolved) || resolved.Code != "capture_timeout" || !resolved.Retryable {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("response cap", func(t *testing.T) {
		worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", Secret: strings.Repeat("s", 32), HTTPClient: &http.Client{Transport: captureRoundTripper(func(*http.Request) (*http.Response, error) {
			return captureResponse(http.StatusOK, strings.Repeat("x", maxCaptureResponseBytes+1)), nil
		})}})
		_, err := worker.Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"})
		var resolved *ResolveError
		if !errors.As(err, &resolved) || resolved.Code != "capture_response_too_large" {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestCaptureWorkerNeedsConfiguredTrustedEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "http://capture.internal/v1/capture", "https://user:pass@capture.internal/v1/capture"} {
		worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: endpoint})
		_, err := worker.Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"})
		var resolved *ResolveError
		if !errors.As(err, &resolved) || resolved.Code != "browser_required" {
			t.Fatalf("endpoint=%q error=%v", endpoint, err)
		}
	}
}

func TestCaptureWorkerRequiresSharedBearerSecret(t *testing.T) {
	called := false
	worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", Secret: "short", HTTPClient: &http.Client{Transport: captureRoundTripper(func(*http.Request) (*http.Response, error) {
		called = true
		return captureResponse(200, `{"videos":[]}`), nil
	})}})
	if _, err := worker.Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"}); err == nil || called {
		t.Fatal("unprotected capture worker used")
	}
}

func TestCaptureWorkerRejectsMoreThanWorkerLimit(t *testing.T) {
	called := false
	worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", Secret: strings.Repeat("s", 32), HTTPClient: &http.Client{Transport: captureRoundTripper(func(*http.Request) (*http.Response, error) {
		called = true
		return captureResponse(http.StatusOK, `{"videos":[]}`), nil
	})}})
	ids := make([]string, 51)
	for index := range ids {
		ids[index] = fmt.Sprintf("wxv_%08d", index)
	}
	_, err := worker.Capture(context.Background(), "https://mp.weixin.qq.com/s/article", ids)
	var resolved *ResolveError
	if !errors.As(err, &resolved) || resolved.Code != "invalid_capture_request" || called {
		t.Fatalf("error=%v called=%v", err, called)
	}
}
