package route

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTrackingLimiterIgnoresForgedHeadersUnderDefaultGin(t *testing.T) {
	t.Setenv("TRACKING_TRUSTED_PROXY_CIDRS", "")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/entry", trackingEntryRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func(peer, forwarded string) int {
		req := httptest.NewRequest(http.MethodPost, "/entry", nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", forwarded)
		req.Header.Set("X-Real-IP", forwarded)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	for i := 0; i < 10001; i++ {
		got := request("198.51.100.7:1000", fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256))
		want := 429
		if i < 60 {
			want = 204
		}
		if got != want {
			t.Fatalf("request %d: got %d want %d", i, got, want)
		}
	}
	if got := request("192.0.2.100:9999", "198.51.100.7"); got != 204 {
		t.Fatalf("fresh socket peer denied after header attack: %d", got)
	}
	if got := request("[::ffff:198.51.100.7]:9999", "203.0.113.222"); got != 429 {
		t.Fatalf("mapped IP/port change bypass: %d", got)
	}
}
func TestTrackingLimiterCapacityNeverGloballyRejectsTraffic(t *testing.T) {
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	l := newTrackingPeerLimiter(2, 2, func() time.Time { return now })
	if !l.allow("a") || !l.allow("b") || !l.allow("b") || l.allow("b") {
		t.Fatal("per-peer window not enforced")
	}
	if !l.allow("c") {
		t.Fatal("capacity rejected fresh peer")
	}
	if l.allow("b") {
		t.Fatal("recent active peer lost its quota")
	}
	if len(l.slots) != 2 || l.order.Len() != 2 {
		t.Fatal("memory capacity exceeded")
	}
	now = now.Add(time.Minute)
	if !l.allow("b") {
		t.Fatal("window did not expire at exact boundary")
	}
	many := newTrackingPeerLimiter(10000, 60, func() time.Time { return now })
	for i := 0; i < 10002; i++ {
		if !many.allow(fmt.Sprint(i)) {
			t.Fatal("global denial at", i)
		}
	}
	if len(many.slots) != 10000 {
		t.Fatal("unbounded key allocation")
	}
}
func TestTrackingProxyPolicyIsExplicitAndRightAnchored(t *testing.T) {
	trusted := trackingTrustedProxyPrefixes("10.0.0.0/24, 2001:db8:100::/64")
	for _, tc := range []struct{ peer, xff, want string }{
		{"203.0.113.8:1234", "192.0.2.99", "203.0.113.8"},
		{"10.0.0.1:1234", "198.51.100.9, 10.0.0.2", "198.51.100.9"},
		{"10.0.0.1:1234", "192.0.2.99, 198.51.100.9, 10.0.0.2", "198.51.100.9"},
		{"10.0.0.1:1234", "malformed, 10.0.0.2", "10.0.0.1"},
		{"10.0.0.1:1234", strings.Repeat("1", 2049), "10.0.0.1"},
		{"10.0.0.1:1234", strings.Repeat("10.0.0.2,", 16) + "198.51.100.9", "10.0.0.1"},
		{"[2001:db8:100::1]:443", "2001:db8:200::2", "2001:db8:200::2"},
		{"malformed-peer", "192.0.2.99", "unknown-peer"},
	} {
		if got := trackingRateKey(tc.peer, tc.xff, trusted); got != tc.want {
			t.Fatalf("%+v got %s", tc, got)
		}
	}
	for _, policy := range []string{"", "10.0.0.0/24,garbage", "0.0.0.0/0", "::/0", "10.0.0.0/24,"} {
		if got := trackingRateKey("10.0.0.1:8080", "192.0.2.88", trackingTrustedProxyPrefixes(policy)); got != "10.0.0.1" {
			t.Fatal("invalid/empty trust enabled forwarding", policy, got)
		}
	}
}
func TestTrackingProxyPolicyWarning(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		raw     string
		warn    bool
	}{
		{false, "", false},
		{true, "", true},
		{true, "  ", true},
		{true, "10.0.0.0/24,garbage", true},
		{true, "0.0.0.0/0", true},
		{true, "10.0.0.0/24, 2001:db8::/64", false},
	} {
		if got := trackingProxyPolicyWarning(tc.enabled, tc.raw); (got != "") != tc.warn {
			t.Fatalf("%+v: got %q", tc, got)
		}
	}
}
func TestTrackingUntrustedForwardingWarnsOnce(t *testing.T) {
	oldLogf, oldOnce := trackingLogf, trackingForwardedWarning
	defer func() { trackingLogf, trackingForwardedWarning = oldLogf, oldOnce }()
	var logged []string
	trackingLogf = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	trackingForwardedWarning = &sync.Once{}
	trusted := trackingTrustedProxyPrefixes("10.0.0.0/24")
	warnUntrustedForwarding("10.0.0.1:443", "198.51.100.9", trusted)
	warnUntrustedForwarding("203.0.113.8:443", "", trusted)
	if len(logged) != 0 {
		t.Fatalf("trusted proxy or plain client warned: %v", logged)
	}
	warnUntrustedForwarding("172.17.0.1:443", "198.51.100.9", trusted)
	warnUntrustedForwarding("172.17.0.1:443", "198.51.100.10", trusted)
	warnUntrustedForwarding("[::ffff:172.17.0.2]:443", "198.51.100.11", nil)
	if len(logged) != 1 || !strings.Contains(logged[0], "172.17.0.1") {
		t.Fatalf("want one warning naming the untrusted peer, got %v", logged)
	}
}
func TestTrackingLimiterConcurrentQuotaIsAtomic(t *testing.T) {
	l := newTrackingPeerLimiter(10000, 60, time.Now)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 250; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.allow("same-peer") {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 60 {
		t.Fatal("quota race", accepted.Load())
	}
}
