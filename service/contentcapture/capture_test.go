package contentcapture

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeCDP struct {
	mu     sync.Mutex
	calls  []string
	events chan Event
	closed bool
	onCall func(method string)
}

func newFakeCDP() *fakeCDP { return &fakeCDP{events: make(chan Event, 32)} }

func (fake *fakeCDP) Call(_ context.Context, sessionID, method string, _ any, result any) error {
	fake.mu.Lock()
	fake.calls = append(fake.calls, sessionID+":"+method)
	fake.mu.Unlock()
	switch method {
	case "Target.createBrowserContext":
		setJSON(result, `{"browserContextId":"context-1"}`)
	case "Target.createTarget":
		setJSON(result, `{"targetId":"target-1"}`)
	case "Target.attachToTarget":
		setJSON(result, `{"sessionId":"session-1"}`)
	}
	if fake.onCall != nil {
		fake.onCall(method)
	}
	return nil
}

func (fake *fakeCDP) Events() <-chan Event { return fake.events }
func (fake *fakeCDP) Close() error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.closed = true
	return nil
}

func (fake *fakeCDP) methods() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]string(nil), fake.calls...)
}

func setJSON(target any, raw string) { _ = json.Unmarshal([]byte(raw), target) }

type fakeDialer struct{ connection CDPConnection }

func (dialer fakeDialer) Dial(context.Context, string) (CDPConnection, error) {
	return dialer.connection, nil
}

type fakeResolver struct{ ips []net.IP }

func (resolver fakeResolver) LookupIP(context.Context, string, string) ([]net.IP, error) {
	return resolver.ips, nil
}

func testCaptureConfig(connection CDPConnection) Config {
	return Config{
		Enabled:         true,
		NetworkIsolated: true,
		CDPURL:          "ws://127.0.0.1:9222/devtools/browser/isolated",
		Dialer:          fakeDialer{connection: connection},
		Resolver:        fakeResolver{ips: []net.IP{net.ParseIP("1.1.1.1")}},
		Timeout:         500 * time.Millisecond,
		QuietWindow:     5 * time.Millisecond,
		MaxEvents:       100,
	}
}

func TestCapturerCreatesFreshContextCapturesEveryIDAndCleansUp(t *testing.T) {
	fake := newFakeCDP()
	fake.onCall = func(method string) {
		if method != "Page.navigate" {
			return
		}
		go func() {
			fake.events <- pausedEvent("pause-1", "https://res.wx.qq.com/a.js")
			fake.events <- Event{Method: "Page.loadEventFired", SessionID: "session-1", Params: json.RawMessage(`{"timestamp":1}`)}
			fake.events <- responseEvent("https://mpvideo.qpic.cn/a.mp4?vid=wxv_11111111&format_id=10001", "1.1.1.1")
			fake.events <- responseEvent("https://mpvideo.qpic.cn/a2.mp4?vid=wxv_11111111&format_id=10003", "1.1.1.1")
			fake.events <- responseEvent("https://mpvideo.qpic.cn/b.mp4?vid=wxv_22222222&format_id=10002", "1.0.0.1")
		}()
	}
	capturer := New(testCaptureConfig(fake))
	videos, err := capturer.Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111", "wxv_22222222"})
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 2 || videos[0].ID != "wxv_11111111" || videos[0].FormatID != 10003 || videos[1].ID != "wxv_22222222" {
		t.Fatalf("videos=%#v", videos)
	}
	methods := fake.methods()
	wantPrefix := []string{
		":Target.createBrowserContext", ":Target.createTarget", ":Target.attachToTarget",
		"session-1:Page.enable", "session-1:Network.enable", "session-1:Network.setBypassServiceWorker",
		"session-1:Network.setCacheDisabled", "session-1:Fetch.enable", "session-1:Page.navigate", "session-1:Fetch.continueRequest",
	}
	if len(methods) < len(wantPrefix)+3 || !reflect.DeepEqual(methods[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("protocol order=%v", methods)
	}
	wantCleanup := []string{":Target.detachFromTarget", ":Target.closeTarget", ":Target.disposeBrowserContext"}
	if !reflect.DeepEqual(methods[len(methods)-3:], wantCleanup) || !fake.closed {
		t.Fatalf("cleanup=%v closed=%v", methods, fake.closed)
	}
	foundScroll := false
	for _, method := range methods {
		foundScroll = foundScroll || method == "session-1:Runtime.evaluate"
	}
	if !foundScroll {
		t.Fatalf("lazy-load scroll missing: %v", methods)
	}
}

func TestCapturerStaysDisabledForPageTargetOrMissingNetworkGate(t *testing.T) {
	for _, configuration := range []Config{
		{Enabled: true, NetworkIsolated: false, CDPURL: "ws://127.0.0.1:9222/devtools/browser/isolated"},
		{Enabled: true, NetworkIsolated: true, CDPURL: "ws://127.0.0.1:9222/devtools/page/user-tab"},
	} {
		_, err := New(configuration).Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"})
		if !IsCode(err, "disabled") {
			t.Fatalf("config=%#v error=%v", configuration, err)
		}
	}
}

func TestCapturerFailsWhenAnyExpectedVideoIsMissing(t *testing.T) {
	fake := newFakeCDP()
	fake.onCall = func(method string) {
		if method == "Page.navigate" {
			go func() {
				fake.events <- responseEvent("https://mpvideo.qpic.cn/a.mp4?vid=wxv_11111111", "1.1.1.1")
			}()
		}
	}
	config := testCaptureConfig(fake)
	config.Timeout = 20 * time.Millisecond
	_, err := New(config).Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111", "wxv_22222222"})
	if !IsCode(err, "missing_video") {
		t.Fatalf("error=%v", err)
	}
}

func TestCapturerTimesOutWithoutAnyMediaResponse(t *testing.T) {
	fake := newFakeCDP()
	config := testCaptureConfig(fake)
	config.Timeout = 10 * time.Millisecond
	_, err := New(config).Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"})
	if !IsCode(err, "timeout") {
		t.Fatalf("error=%v", err)
	}
}

func TestCapturerStopsAtBoundedNetworkBytes(t *testing.T) {
	fake := newFakeCDP()
	fake.onCall = func(method string) {
		if method == "Page.navigate" {
			go func() {
				params, _ := json.Marshal(map[string]any{"encodedDataLength": 17})
				fake.events <- Event{Method: "Network.dataReceived", SessionID: "session-1", Params: params}
			}()
		}
	}
	config := testCaptureConfig(fake)
	config.MaxBytes = 16
	_, err := New(config).Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"})
	if !IsCode(err, "byte_limit") {
		t.Fatalf("error=%v", err)
	}
}

func TestCapturerBlocksUnknownPrivateAndNonHTTPSRequests(t *testing.T) {
	fake := newFakeCDP()
	privateResolver := resolverFunc(func(_ context.Context, _, host string) ([]net.IP, error) {
		if host == "res.wx.qq.com" {
			return []net.IP{net.ParseIP("10.0.0.8")}, nil
		}
		return []net.IP{net.ParseIP("1.1.1.1")}, nil
	})
	fake.onCall = func(method string) {
		if method == "Page.navigate" {
			go func() {
				fake.events <- pausedEvent("private", "https://res.wx.qq.com/a.js")
				fake.events <- pausedEvent("scheme", "file:///etc/passwd")
				fake.events <- pausedEvent("unknown", "https://evil.example/a.js")
				fake.events <- responseEvent("https://mpvideo.qpic.cn.evil.example/a.mp4?vid=wxv_11111111", "1.1.1.1")
				fake.events <- responseEvent("https://mpvideo.qpic.cn/a.mp4?vid=wxv_11111111", "10.0.0.9")
			}()
		}
	}
	config := testCaptureConfig(fake)
	config.Resolver = privateResolver
	config.Timeout = 20 * time.Millisecond
	_, err := New(config).Capture(context.Background(), "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"})
	if !IsCode(err, "timeout") {
		t.Fatalf("error=%v", err)
	}
	methods := fake.methods()
	blocked := 0
	for _, method := range methods {
		if method == "session-1:Fetch.failRequest" {
			blocked++
		}
	}
	if blocked != 3 {
		t.Fatalf("blocked calls=%d methods=%v", blocked, methods)
	}
}

func TestCaptureCandidateRequiresExactMPVideoHost(t *testing.T) {
	capturer := New(testCaptureConfig(newFakeCDP()))
	expected := map[string]bool{"wxv_11111111": true}
	for _, candidateURL := range []string{
		"https://mmbiz.qpic.cn/a.mp4?vid=wxv_11111111",
		"https://cdn.mpvideo.qpic.cn/a.mp4?vid=wxv_11111111",
		"https://mpvideo.qpic.cn:443/a.mp4?vid=wxv_11111111",
	} {
		event := responseEvent(candidateURL, "1.1.1.1")
		if video, ok := capturer.captureCandidate(context.Background(), event.Params, expected); ok {
			t.Fatalf("candidate accepted url=%s video=%#v", candidateURL, video)
		}
	}
}

func TestPublicIPUsesSharedImportPolicyForTranslatedAndMappedAddresses(t *testing.T) {
	for raw, expected := range map[string]bool{
		"1.1.1.1":             true,
		"64:ff9b::7f00:1":     false,
		"100::1":              false,
		"::ffff:127.0.0.1":    false,
		"::ffff:192.168.1.10": false,
	} {
		if got := publicIP(net.ParseIP(raw)); got != expected {
			t.Errorf("publicIP(%s)=%v, want %v", raw, got, expected)
		}
	}
}

func TestCapturerRespectsCallerCancellation(t *testing.T) {
	fake := newFakeCDP()
	ctx, cancel := context.WithCancel(context.Background())
	fake.onCall = func(method string) {
		if method == "Page.navigate" {
			cancel()
		}
	}
	_, err := New(testCaptureConfig(fake)).Capture(ctx, "https://mp.weixin.qq.com/s/article", []string{"wxv_11111111"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

type resolverFunc func(context.Context, string, string) ([]net.IP, error)

func (function resolverFunc) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return function(ctx, network, host)
}

func pausedEvent(id, requestURL string) Event {
	params, _ := json.Marshal(map[string]any{"requestId": id, "request": map[string]any{"url": requestURL}})
	return Event{Method: "Fetch.requestPaused", SessionID: "session-1", Params: params}
}

func responseEvent(responseURL, remoteIP string) Event {
	params, _ := json.Marshal(map[string]any{
		"type":     "Media",
		"response": map[string]any{"url": responseURL, "status": 206, "mimeType": "video/mp4", "remoteIPAddress": remoteIP},
	})
	return Event{Method: "Network.responseReceived", SessionID: "session-1", Params: params}
}
