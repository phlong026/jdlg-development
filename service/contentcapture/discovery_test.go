package contentcapture

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCaptureDiscoversFreshBrowserEndpointBeforeEveryDial(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/json/version" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		index := requests.Add(1)
		_, _ = fmt.Fprintf(response, `{"webSocketDebuggerUrl":"ws://%s/devtools/browser/browser-%d"}`, request.Host, index)
	}))
	defer server.Close()

	var endpoints []string
	capturer := New(Config{
		Enabled:         true,
		NetworkIsolated: true,
		CDPDiscoveryURL: server.URL + "/json/version",
		Dialer: dialerFunc(func(_ context.Context, endpoint string) (CDPConnection, error) {
			endpoints = append(endpoints, endpoint)
			return nil, errors.New("unavailable")
		}),
		Timeout: time.Second,
	})
	for range 2 {
		_, err := capturer.Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"})
		if !IsCode(err, "cdp_unavailable") {
			t.Fatalf("error=%v", err)
		}
	}
	if requests.Load() != 2 || len(endpoints) != 2 || endpoints[0] == endpoints[1] {
		t.Fatalf("requests=%d endpoints=%v", requests.Load(), endpoints)
	}
}

func TestDiscoveryRejectsUnsafeConfigurationAndBrowserEndpoints(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:9222/json/version",
		"http://127.0.0.1:9222/json",
		"http://127.0.0.1:9222/json/version?x=1",
		"https://127.0.0.1:9222/json/version",
	} {
		if validDiscoveryURL(raw) {
			t.Fatalf("accepted discovery URL %s", raw)
		}
	}
	for _, endpoint := range []string{
		"ws://127.0.0.2:9222/devtools/browser/a",
		"ws://127.0.0.1:9223/devtools/browser/a",
		"ws://127.0.0.1:9222/devtools/page/a",
		"ws://127.0.0.1:9222/devtools/browser/a/b",
		"ws://127.0.0.1:9222/devtools/browser/a?x=1",
	} {
		if validDiscoveredEndpoint("http://127.0.0.1:9222/json/version", endpoint) {
			t.Fatalf("accepted browser endpoint %s", endpoint)
		}
	}
}

func TestDiscoveryClientDisablesProxyAndBoundsResponse(t *testing.T) {
	client := newDiscoveryClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || client.Timeout <= 0 {
		t.Fatalf("unsafe discovery client=%#v", client)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat("x", maxDiscoveryBody+1)))
	}))
	defer server.Close()
	if _, err := discoverCDPEndpoint(context.Background(), client, server.URL+"/json/version"); err == nil {
		t.Fatal("oversized discovery response accepted")
	}
}
