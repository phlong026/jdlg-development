package contentcapture

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

const (
	discoveryTimeout = 3 * time.Second
	maxDiscoveryBody = 32 << 10
)

var browserIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,256}$`)

func newDiscoveryClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:       nil,
			DialContext: (&net.Dialer{Timeout: discoveryTimeout}).DialContext,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       discoveryTimeout,
	}
}

func discoverCDPEndpoint(ctx context.Context, client *http.Client, discoveryURL string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", errors.New("invalid cdp discovery request")
	}
	response, err := client.Do(request)
	if err != nil {
		return "", errors.New("cdp discovery unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("cdp discovery returned non-success status")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDiscoveryBody+1))
	if err != nil || len(body) > maxDiscoveryBody {
		return "", errors.New("invalid cdp discovery response")
	}
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &version); err != nil || !validDiscoveredEndpoint(discoveryURL, version.WebSocketDebuggerURL) {
		return "", errors.New("invalid cdp browser endpoint")
	}
	return version.WebSocketDebuggerURL, nil
}

func validDiscoveryURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.Path != "/json/version" || parsed.RawPath != "" || parsed.Port() == "" {
		return false
	}
	ip := net.ParseIP(parsed.Hostname())
	return ip != nil && ip.IsLoopback()
}

func validDiscoveredEndpoint(discoveryURL, endpoint string) bool {
	discovery, discoveryErr := url.Parse(discoveryURL)
	parsed, endpointErr := url.Parse(endpoint)
	if discoveryErr != nil || endpointErr != nil || parsed.Scheme != "ws" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.RawPath != "" || parsed.Host != discovery.Host {
		return false
	}
	const prefix = "/devtools/browser/"
	if len(parsed.Path) <= len(prefix) || parsed.Path[:len(prefix)] != prefix {
		return false
	}
	return browserIDPattern.MatchString(parsed.Path[len(prefix):])
}
