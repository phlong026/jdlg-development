package tracking

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// MediaFetcher downloads one picture. Implementations must bound the size.
type MediaFetcher interface {
	Fetch(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error)
}

var errFetchRefused = errors.New("media fetch refused")

// publicIP reports whether an address may be contacted on behalf of an article.
// Everything that is not ordinary public unicast is refused, including loopback,
// private and link-local ranges, carrier-grade NAT, multicast and the IPv4
// addresses hidden inside IPv4-mapped IPv6 forms.
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, blocked := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "100::/64", "2001:db8::/32"} {
		if netip.MustParsePrefix(blocked).Contains(ip) {
			return false
		}
	}
	return true
}

// SafeMediaFetcher fetches pictures from allowlisted hosts only. The address is
// checked on the connection itself, after name resolution, so a hostname that
// resolves to an internal address (including by DNS rebinding between a check
// and a connect) cannot be used to reach internal services. Redirects are not
// followed, no cookies or credentials are sent, and the proxy environment is
// ignored so the connection check sees the real destination.
type SafeMediaFetcher struct {
	client   *http.Client
	validate func(*url.URL) error
}

func NewSafeMediaFetcher() *SafeMediaFetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return errFetchRefused
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || !publicIP(ip) {
			return errFetchRefused
		}
		return nil
	}}
	transport := &http.Transport{Proxy: nil, DialContext: dialer.DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second, DisableCompression: true}
	return &SafeMediaFetcher{
		client: &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		validate: func(u *url.URL) error {
			if u.Scheme != "https" || u.User != nil || !allowedMPMediaHost(u.Hostname()) || (u.Port() != "" && u.Port() != "443") {
				return errFetchRefused
			}
			return nil
		},
	}
}

func (f *SafeMediaFetcher) Fetch(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || f.validate(u) != nil || maxBytes <= 0 {
		return nil, errFetchRefused
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errFetchRefused
	}
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif")
	req.Header.Set("User-Agent", "jdlg-content-sync/1")
	res, err := f.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(strings.ToLower(res.Header.Get("Content-Type")), "image/") {
		return nil, ErrUnavailable
	}
	if res.ContentLength > maxBytes {
		return nil, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes || len(data) == 0 {
		return nil, ErrUnavailable
	}
	return data, nil
}
