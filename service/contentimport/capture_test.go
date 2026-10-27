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

func TestCaptureWorkerDialAddressMustBeNumericLoopback443(t *testing.T) {
	for _, address := range []string{"localhost:443", "127.0.0.1:8443", "10.0.0.1:443", "127.0.0.1"} {
		worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", Secret: strings.Repeat("s", 32), DialAddress: address})
		if worker.Ready() {
			t.Fatalf("accepted dial address %q", address)
		}
	}
	if worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", Secret: strings.Repeat("s", 32), DialAddress: "127.0.0.1:443"}); !worker.Ready() {
		t.Fatal("rejected numeric loopback port 443")
	}
}

func TestCaptureWorkerUsesDedicatedFixedDialTransport(t *testing.T) {
	injected := captureRoundTripper(func(*http.Request) (*http.Response, error) {
		return captureResponse(http.StatusOK, `{}`), nil
	})
	worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://api.jiadilingguang.com/_internal/content-capture/v1/capture", Secret: strings.Repeat("s", 32), DialAddress: "127.0.0.1:443", HTTPClient: &http.Client{Transport: injected}})
	transport, ok := worker.client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.DialContext == nil || transport.DialTLSContext != nil {
		t.Fatalf("transport=%#v", worker.client.Transport)
	}
	if transport.TLSClientConfig != nil && (transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.ServerName != "") {
		t.Fatal("transport overrides normal hostname verification")
	}
}

func TestCaptureWorkerProbeRequiresAuthenticatedStrictReadyResponse(t *testing.T) {
	secret := strings.Repeat("s", 32)
	calls := 0
	client := &http.Client{Transport: captureRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet || request.URL.String() != "https://capture.internal/health/ready" || request.Header.Get("Authorization") != "Bearer "+secret {
			t.Fatalf("unexpected health request: %s %s", request.Method, request.URL)
		}
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) > defaultProbeTimeout {
			t.Fatal("health request is not bounded by the probe timeout")
		}
		return captureResponse(http.StatusOK, `{"ready":true}`), nil
	})}
	worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", HealthEndpoint: "https://capture.internal/health/ready", Secret: secret, HTTPClient: client})
	if err := worker.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !worker.Available(context.Background()) || !worker.Available(context.Background()) || calls != 2 {
		t.Fatalf("available=%v calls=%d", worker.Available(context.Background()), calls)
	}
}

func TestCaptureWorkerProbeRejectsUntrustedOrMalformedHealth(t *testing.T) {
	secret := strings.Repeat("s", 32)
	for name, health := range map[string]string{
		"missing":      "",
		"http":         "http://capture.internal/health/ready",
		"other origin": "https://other.internal/health/ready",
		"query":        "https://capture.internal/health/ready?token=x",
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", HealthEndpoint: health, Secret: secret, HTTPClient: &http.Client{Transport: captureRoundTripper(func(*http.Request) (*http.Response, error) {
				called = true
				return captureResponse(http.StatusOK, `{"ready":true}`), nil
			})}})
			if !worker.Ready() {
				t.Fatal("health configuration changed legacy Ready semantics")
			}
			if worker.Available(context.Background()) || called {
				t.Fatalf("health=%q called=%v", health, called)
			}
		})
	}
	for name, statusBody := range map[string]struct {
		status int
		body   string
	}{
		"not ready":     {http.StatusOK, `{"ready":false}`},
		"unknown field": {http.StatusOK, `{"ready":true,"detail":"ok"}`},
		"trailing json": {http.StatusOK, `{"ready":true}{}`},
		"non-200":       {http.StatusServiceUnavailable, `{"ready":true}`},
	} {
		t.Run(name, func(t *testing.T) {
			worker := NewCaptureWorker(CaptureWorkerConfig{Endpoint: "https://capture.internal/v1/capture", HealthEndpoint: "https://capture.internal/health/ready", Secret: secret, HTTPClient: &http.Client{Transport: captureRoundTripper(func(*http.Request) (*http.Response, error) {
				return captureResponse(statusBody.status, statusBody.body), nil
			})}})
			if worker.Available(context.Background()) {
				t.Fatal("accepted invalid health response")
			}
		})
	}
}

func TestCaptureWorkerCachesFailedProbeBriefly(t *testing.T) {
	calls := 0
	worker := NewCaptureWorker(CaptureWorkerConfig{
		Endpoint:       "https://capture.internal/v1/capture",
		HealthEndpoint: "https://capture.internal/health/ready",
		Secret:         strings.Repeat("s", 32),
		HTTPClient: &http.Client{Transport: captureRoundTripper(func(*http.Request) (*http.Response, error) {
			calls++
			return captureResponse(http.StatusServiceUnavailable, `{}`), nil
		})},
	})
	if worker.Available(context.Background()) || worker.Available(context.Background()) || calls != 1 {
		t.Fatalf("failed readiness probe calls=%d", calls)
	}
}
