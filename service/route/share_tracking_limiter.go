package route

import (
	"container/list"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

// This policy applies only to tracking entry/share limits, never to Gin's global
// proxy trust or another route's client identity. Empty/invalid policy trusts no
// forwarded header. Deployers must verify their proxy chain before opting in.
func trackingTrustedProxyPrefixes(value string) []netip.Prefix {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var out []netip.Prefix
	for _, raw := range strings.Split(value, ",") {
		p, e := netip.ParsePrefix(strings.TrimSpace(raw))
		if e != nil {
			return nil
		}
		// A trust-everyone entry defeats the socket boundary; reject the whole policy.
		if p.Bits() == 0 {
			return nil
		}
		out = append(out, p.Masked())
	}
	return out
}
func trackingPeerIP(address string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.WithZone("").Unmap(), true
}
func trackingRateKey(remote, forwarded string, trusted []netip.Prefix) string {
	peer, ok := trackingPeerIP(remote)
	if !ok {
		return "unknown-peer"
	}
	trustedIP := func(ip netip.Addr) bool {
		for _, prefix := range trusted {
			if prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trustedIP(peer) || forwarded == "" || len(forwarded) > 2048 {
		return peer.String()
	}
	parts := strings.Split(forwarded, ",")
	if len(parts) > 16 {
		return peer.String()
	}
	chain := make([]netip.Addr, len(parts))
	for i, value := range parts {
		ip, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || ip.Zone() != "" {
			return peer.String()
		}
		chain[i] = ip.Unmap()
	}
	// Walk from the socket towards the client and stop at the first untrusted hop.
	// A forged left prefix cannot replace that hop's identity.
	for i := len(chain) - 1; i >= 0; i-- {
		if !trustedIP(chain[i]) || i == 0 {
			return chain[i].String()
		}
	}
	return peer.String()
}

type trackingLimitSlot struct {
	key   string
	at    time.Time
	count int
}
type trackingPeerLimiter struct {
	mu                  sync.Mutex
	capacity, perMinute int
	clock               func() time.Time
	slots               map[string]*list.Element
	order               *list.List
}

func newTrackingPeerLimiter(capacity, perMinute int, clock func() time.Time) *trackingPeerLimiter {
	if capacity < 1 {
		capacity = 1
	}
	if perMinute < 1 {
		perMinute = 1
	}
	return &trackingPeerLimiter{capacity: capacity, perMinute: perMinute, clock: clock, slots: map[string]*list.Element{}, order: list.New()}
}
func (l *trackingPeerLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	item := l.slots[key]
	if item == nil {
		// Evict one least-recently-used peer in O(1). Capacity never makes all known
		// or new clients fail; active peers retain their own bounded fixed window.
		if len(l.slots) >= l.capacity {
			oldest := l.order.Back()
			delete(l.slots, oldest.Value.(*trackingLimitSlot).key)
			l.order.Remove(oldest)
		}
		item = l.order.PushFront(&trackingLimitSlot{key: key, at: now})
		l.slots[key] = item
	}
	l.order.MoveToFront(item)
	slot := item.Value.(*trackingLimitSlot)
	if now.Sub(slot.at) >= time.Minute {
		slot.at = now
		slot.count = 0
	}
	if slot.count >= l.perMinute {
		return false
	}
	slot.count++
	return true
}
func trackingEntryRateLimit() gin.HandlerFunc {
	trusted := trackingTrustedProxyPrefixes(os.Getenv("TRACKING_TRUSTED_PROXY_CIDRS"))
	limiter := newTrackingPeerLimiter(10000, 60, time.Now)
	return func(c *gin.Context) {
		if !limiter.allow(trackingRateKey(c.Request.RemoteAddr, c.GetHeader("X-Forwarded-For"), trusted)) {
			trackingResponse(c, nil, &tracking.Error{Status: 429, Message: "请求过于频繁"})
			return
		}
		c.Next()
	}
}
