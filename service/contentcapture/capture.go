package contentcapture

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"service/tracking"
	"strconv"
	"strings"
	"time"
)

const (
	defaultCaptureTimeout = 120 * time.Second
	defaultQuietWindow    = 500 * time.Millisecond
	defaultMaxEvents      = 5000
	defaultMaxBytes       = 64 << 20
)

var videoIDPattern = regexp.MustCompile(`^wxv_[A-Za-z0-9_-]{8,120}$`)

type Video struct {
	ID       string `json:"source_id"`
	URL      string `json:"url"`
	FormatID int    `json:"format_id,omitempty"`
}

type Capturer interface {
	Capture(context.Context, string, []string) ([]Video, error)
}

type Error struct{ Code string }

func (captureError *Error) Error() string { return "content capture: " + captureError.Code }

func IsCode(err error, code string) bool {
	var captureError *Error
	return errors.As(err, &captureError) && captureError.Code == code
}

func fail(code string) error { return &Error{Code: code} }

type HostResolver interface {
	LookupIP(context.Context, string, string) ([]net.IP, error)
}

type Config struct {
	Enabled         bool
	NetworkIsolated bool
	CDPURL          string
	AllowedHosts    []string
	Dialer          CDPDialer
	Resolver        HostResolver
	Timeout         time.Duration
	QuietWindow     time.Duration
	MaxEvents       int
	MaxBytes        int64
}

type Capture struct {
	ready       bool
	cdpURL      string
	dialer      CDPDialer
	resolver    HostResolver
	timeout     time.Duration
	quietWindow time.Duration
	maxEvents   int
	maxBytes    int64
	allowed     []string
}

func New(config Config) *Capture {
	timeout := config.Timeout
	if timeout <= 0 || timeout > defaultCaptureTimeout {
		timeout = defaultCaptureTimeout
	}
	quietWindow := config.QuietWindow
	if quietWindow <= 0 || quietWindow > 5*time.Second {
		quietWindow = defaultQuietWindow
	}
	maxEvents := config.MaxEvents
	if maxEvents <= 0 || maxEvents > 20000 {
		maxEvents = defaultMaxEvents
	}
	maxBytes := config.MaxBytes
	if maxBytes <= 0 || maxBytes > 256<<20 {
		maxBytes = defaultMaxBytes
	}
	dialer := config.Dialer
	if dialer == nil {
		dialer = WebSocketDialer{}
	}
	resolver := config.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	allowed := []string{"mp.weixin.qq.com", "res.wx.qq.com", "wxsnsdy.wxs.qq.com", "qpic.cn", "qlogo.cn"}
	validHosts := true
	for _, host := range config.AllowedHosts {
		host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
		if !validConfiguredHost(host) {
			validHosts = false
			continue
		}
		allowed = append(allowed, host)
	}
	if len(allowed) > 32 {
		validHosts = false
	}
	return &Capture{
		ready:       config.Enabled && config.NetworkIsolated && validCDPURL(config.CDPURL) && validHosts,
		cdpURL:      config.CDPURL,
		dialer:      dialer,
		resolver:    resolver,
		timeout:     timeout,
		quietWindow: quietWindow,
		maxEvents:   maxEvents,
		maxBytes:    maxBytes,
		allowed:     allowed,
	}
}

func (capture *Capture) Capture(ctx context.Context, articleURL string, ids []string) ([]Video, error) {
	if capture == nil || !capture.ready {
		return nil, fail("disabled")
	}
	if !validArticleURL(articleURL) || !validVideoIDs(ids) {
		return nil, fail("invalid_request")
	}
	captureContext, cancel := context.WithTimeout(ctx, capture.timeout)
	defer cancel()
	connection, err := capture.dialer.Dial(captureContext, capture.cdpURL)
	if err != nil {
		return nil, fail("cdp_unavailable")
	}
	defer connection.Close()

	var browserContext struct {
		ID string `json:"browserContextId"`
	}
	if connection.Call(captureContext, "", "Target.createBrowserContext", map[string]any{"disposeOnDetach": true}, &browserContext) != nil || browserContext.ID == "" {
		return nil, fail("cdp_failed")
	}
	defer cleanupCall(connection, "Target.disposeBrowserContext", map[string]any{"browserContextId": browserContext.ID})

	var target struct {
		ID string `json:"targetId"`
	}
	if connection.Call(captureContext, "", "Target.createTarget", map[string]any{"url": "about:blank", "browserContextId": browserContext.ID}, &target) != nil || target.ID == "" {
		return nil, fail("cdp_failed")
	}
	defer cleanupCall(connection, "Target.closeTarget", map[string]any{"targetId": target.ID})

	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if connection.Call(captureContext, "", "Target.attachToTarget", map[string]any{"targetId": target.ID, "flatten": true}, &attached) != nil || attached.SessionID == "" {
		return nil, fail("cdp_failed")
	}
	defer cleanupCall(connection, "Target.detachFromTarget", map[string]any{"sessionId": attached.SessionID})

	for _, command := range []struct {
		method string
		params any
	}{
		{"Page.enable", map[string]any{}},
		{"Network.enable", map[string]any{"maxTotalBufferSize": 1 << 20, "maxResourceBufferSize": 256 << 10, "maxPostDataSize": 64 << 10}},
		{"Network.setBypassServiceWorker", map[string]any{"bypass": true}},
		{"Network.setCacheDisabled", map[string]any{"cacheDisabled": true}},
		{"Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}}},
	} {
		if connection.Call(captureContext, attached.SessionID, command.method, command.params, nil) != nil {
			return nil, fail("cdp_failed")
		}
	}
	var navigation struct {
		ErrorText string `json:"errorText"`
	}
	if connection.Call(captureContext, attached.SessionID, "Page.navigate", map[string]any{"url": articleURL}, &navigation) != nil || navigation.ErrorText != "" {
		return nil, fail("navigation_failed")
	}
	return capture.collect(captureContext, ctx, connection, attached.SessionID, ids)
}

func (capture *Capture) collect(captureContext, callerContext context.Context, connection CDPConnection, sessionID string, ids []string) ([]Video, error) {
	expected := make(map[string]bool, len(ids))
	for _, id := range ids {
		expected[id] = true
	}
	selected := make(map[string]Video, len(ids))
	var quietTimer *time.Timer
	var quiet <-chan time.Time
	defer func() {
		if quietTimer != nil {
			quietTimer.Stop()
		}
	}()
	resetQuiet := func() {
		if len(selected) != len(expected) {
			return
		}
		if quietTimer == nil {
			quietTimer = time.NewTimer(capture.quietWindow)
		} else {
			if !quietTimer.Stop() {
				select {
				case <-quietTimer.C:
				default:
				}
			}
			quietTimer.Reset(capture.quietWindow)
		}
		quiet = quietTimer.C
	}

	eventCount := 0
	var receivedBytes int64
	scrolled := false
	for {
		select {
		case <-callerContext.Done():
			return nil, callerContext.Err()
		case <-captureContext.Done():
			if callerContext.Err() != nil {
				return nil, callerContext.Err()
			}
			if len(selected) == 0 {
				return nil, fail("timeout")
			}
			return nil, fail("missing_video")
		case <-quiet:
			videos := make([]Video, 0, len(ids))
			for _, id := range ids {
				videos = append(videos, selected[id])
			}
			return videos, nil
		case event, ok := <-connection.Events():
			if !ok {
				return nil, fail("cdp_failed")
			}
			eventCount++
			if eventCount > capture.maxEvents {
				return nil, fail("event_limit")
			}
			if event.SessionID != sessionID {
				continue
			}
			switch event.Method {
			case "Fetch.requestPaused":
				if err := capture.handlePaused(captureContext, connection, sessionID, event.Params); err != nil {
					return nil, err
				}
			case "Page.loadEventFired":
				if !scrolled {
					scrolled = true
					if connection.Call(captureContext, sessionID, "Runtime.evaluate", map[string]any{"expression": "window.scrollTo(0, document.body.scrollHeight)", "returnByValue": true, "awaitPromise": false}, nil) != nil {
						return nil, fail("cdp_failed")
					}
				}
			case "Network.responseReceived":
				video, ok := capture.captureCandidate(captureContext, event.Params, expected)
				if !ok {
					continue
				}
				current, exists := selected[video.ID]
				if !exists || video.FormatID > current.FormatID {
					selected[video.ID] = video
					resetQuiet()
				}
			case "Network.dataReceived":
				var received struct {
					EncodedDataLength int64 `json:"encodedDataLength"`
				}
				if json.Unmarshal(event.Params, &received) != nil || received.EncodedDataLength < 0 {
					return nil, fail("invalid_cdp_event")
				}
				if received.EncodedDataLength > capture.maxBytes-receivedBytes {
					return nil, fail("byte_limit")
				}
				receivedBytes += received.EncodedDataLength
			}
		}
	}
}

func (capture *Capture) handlePaused(ctx context.Context, connection CDPConnection, sessionID string, raw json.RawMessage) error {
	var paused struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	if json.Unmarshal(raw, &paused) != nil || paused.RequestID == "" {
		return fail("invalid_cdp_event")
	}
	method := "Fetch.continueRequest"
	params := map[string]any{"requestId": paused.RequestID}
	if !capture.allowedResourceURL(ctx, paused.Request.URL) {
		method = "Fetch.failRequest"
		params["errorReason"] = "BlockedByClient"
	}
	if connection.Call(ctx, sessionID, method, params, nil) != nil {
		return fail("cdp_failed")
	}
	return nil
}

func (capture *Capture) captureCandidate(ctx context.Context, raw json.RawMessage, expected map[string]bool) (Video, bool) {
	var event struct {
		Type     string `json:"type"`
		Response struct {
			URL             string  `json:"url"`
			Status          float64 `json:"status"`
			MIMEType        string  `json:"mimeType"`
			RemoteIPAddress string  `json:"remoteIPAddress"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &event) != nil || event.Type != "Media" || (event.Response.Status != 200 && event.Response.Status != 206) || strings.ToLower(strings.TrimSpace(event.Response.MIMEType)) != "video/mp4" {
		return Video{}, false
	}
	if len(event.Response.URL) > 8192 {
		return Video{}, false
	}
	parsed, err := url.Parse(event.Response.URL)
	if err != nil || parsed.Port() != "" || !strings.EqualFold(strings.TrimSuffix(parsed.Hostname(), "."), "mpvideo.qpic.cn") || !strings.HasSuffix(strings.ToLower(parsed.Path), ".mp4") || !capture.allowedResourceURL(ctx, event.Response.URL) {
		return Video{}, false
	}
	if remoteIP := net.ParseIP(event.Response.RemoteIPAddress); remoteIP == nil || !publicIP(remoteIP) {
		return Video{}, false
	}
	videoIDs := parsed.Query()["vid"]
	if len(videoIDs) != 1 || !expected[videoIDs[0]] {
		return Video{}, false
	}
	formatID := 0
	if values := parsed.Query()["format_id"]; len(values) == 1 {
		formatID, err = strconv.Atoi(values[0])
		if err != nil || formatID < 0 {
			return Video{}, false
		}
	}
	return Video{ID: videoIDs[0], URL: parsed.String(), FormatID: formatID}, true
}

func (capture *Capture) allowedResourceURL(ctx context.Context, raw string) bool {
	if len(raw) == 0 || len(raw) > 8192 {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Hostname() == "" || (parsed.Port() != "" && parsed.Port() != "443") {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	allowed := false
	for _, domain := range capture.allowed {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			allowed = true
			break
		}
	}
	if !allowed {
		return false
	}
	if literal := net.ParseIP(host); literal != nil {
		return publicIP(literal)
	}
	ips, err := capture.resolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return false
		}
	}
	return true
}

func validArticleURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 2048 {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Port() != "" || !strings.EqualFold(strings.TrimSuffix(parsed.Hostname(), "."), "mp.weixin.qq.com") {
		return false
	}
	return parsed.Path == "/s" || strings.HasPrefix(parsed.Path, "/s/")
}

func validVideoIDs(ids []string) bool {
	if len(ids) == 0 || len(ids) > 50 {
		return false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !videoIDPattern.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func validConfiguredHost(host string) bool {
	if len(host) < 4 || len(host) > 253 || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}

func publicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	return ok && tracking.IsPublicImportAddress(address)
}

func cleanupCall(connection CDPConnection, method string, params any) {
	// disposeOnDetach and closing the WebSocket are the final fallback, but a
	// bounded explicit cleanup keeps normal requests from leaking targets.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = connection.Call(ctx, "", method, params, nil)
}
