package contentimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"service/tracking"
)

type HTTPFetcher struct{ client *http.Client }

func NewHTTPFetcher() *HTTPFetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fail("media_address_refused")
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || !tracking.IsPublicImportAddress(ip) {
			return fail("media_address_refused")
		}
		return nil
	}}
	transport := &http.Transport{Proxy: nil, DialContext: dialer.DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second, DisableCompression: true}
	return &HTTPFetcher{client: &http.Client{Transport: transport, Timeout: 60 * time.Second}}
}
func ParseSourceLink(raw string) (ParsedLink, error) {
	candidate := linkPattern.FindString(strings.TrimSpace(raw))
	candidate = strings.TrimRight(candidate, ",.!?;:)]}，。！？；：、）】》」”")
	u, err := url.Parse(candidate)
	if err == nil && strings.EqualFold(u.Hostname(), "mp.weixin.qq.com") {
		u.Scheme = "https"
		u.Host = strings.ToLower(u.Host)
		if !validArticleURL(u.String()) || u.User != nil || u.Port() != "" && u.Port() != "443" {
			return ParsedLink{}, fail("invalid_link")
		}
		u.Fragment = ""
		query := u.Query()
		for _, key := range []string{"scene", "from", "clicktime", "enterid", "sharer_shareinfo", "sharer_shareinfo_first", "sessionid", "isappinstalled"} {
			query.Del(key)
		}
		u.RawQuery = query.Encode()
		return ParsedLink{Platform: WechatArticle, URL: u.String()}, nil
	}
	return ParseLink(raw)
}
func (f *HTTPFetcher) FetchArticle(ctx context.Context, raw string) ([]byte, error) {
	link, err := ParseSourceLink(raw)
	if err != nil || link.Platform != WechatArticle {
		return nil, fail("invalid_link")
	}
	body, _, err := f.fetch(ctx, WechatArticle, link.URL, 4<<20, true)
	var known *ResolveError
	if errors.As(err, &known) && known.Code == "download_failed" {
		err = fail("article_fetch_failed")
	}
	return body, err
}
func (f *HTTPFetcher) FetchMedia(ctx context.Context, platform Platform, raw string, max int64) ([]byte, string, error) {
	return f.fetch(ctx, platform, raw, max, false)
}
func allowedImportURL(platform Platform, raw string, article bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || len(raw) > 16384 || u.Fragment != "" || u.Port() != "" && u.Port() != "443" || u.Scheme != "https" && u.Scheme != "http" {
		return nil, fail("media_host_not_allowed")
	}
	if article {
		if u.Hostname() != "mp.weixin.qq.com" {
			return nil, fail("media_host_not_allowed")
		}
	} else {
		domains := map[Platform][]string{
			WechatArticle: {"qpic.cn", "qlogo.cn"},
			Channels:      {"qpic.cn", "qlogo.cn", "finder.video.qq.com", "findermp.video.qq.com", "wxapp.tc.qq.com"},
			Douyin:        {"douyinvod.com", "douyinvodcdn.com", "douyinvodds.com", "douyinpic.com", "byteimg.com", "ibytedtos.com", "bytecdn.cn"},
			Xiaohongshu:   {"xhscdn.com", "rednotevideo.com", "xiaohongshu.com"},
		}
		host := strings.ToLower(u.Hostname())
		allowed := false
		for _, domain := range domains[platform] {
			if host == domain || strings.HasSuffix(host, "."+domain) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fail("media_host_not_allowed")
		}
	}
	// Public platform CDNs still found in old articles may use http links. Only
	// upgrade to verified TLS; never allow redirects to downgrade the connection.
	u.Scheme = "https"
	return u, nil
}
func (f *HTTPFetcher) fetch(ctx context.Context, platform Platform, raw string, max int64, article bool) ([]byte, string, error) {
	if f == nil || f.client == nil || max <= 0 {
		return nil, "", ErrUnavailable
	}
	u, err := allowedImportURL(platform, raw, article)
	if err != nil {
		return nil, "", err
	}
	client := *f.client
	client.Jar = nil
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.URL.Scheme != "https" {
			return fail("redirect_refused")
		}
		_, err := allowedImportURL(platform, req.URL.String(), article)
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", fail("media_host_not_allowed")
	}
	req.Header.Set("User-Agent", "JDLG-ContentImport/1.0")
	if article {
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
	} else {
		req.Header.Set("Accept", "image/png,image/jpeg,image/gif,video/mp4")
		if platform == WechatArticle || platform == Channels {
			req.Header.Set("Referer", "https://mp.weixin.qq.com/")
		}
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fail("download_failed")
	}
	defer response.Body.Close()
	if article && (response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 429) {
		return nil, "", fail("risk_control")
	}
	if article && (response.StatusCode == 404 || response.StatusCode == 410) {
		return nil, "", fail("source_unavailable")
	}
	if !article && (response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 404 || response.StatusCode == 410) {
		return nil, "", fail("media_source_expired")
	}
	if response.StatusCode != 200 && response.StatusCode != 206 {
		return nil, "", fail("download_failed")
	}
	if response.ContentLength > max {
		return nil, "", fail("media_too_large")
	}
	if response.StatusCode == 206 {
		var start, end, total int64
		if _, err = fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil || start != 0 || total <= 0 || end+1 != total || response.ContentLength != total {
			return nil, "", fail("partial_media")
		}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, max+1))
	if err != nil || len(data) == 0 {
		return nil, "", fail("download_failed")
	}
	if int64(len(data)) > max {
		return nil, "", fail("media_too_large")
	}
	if response.ContentLength >= 0 && int64(len(data)) != response.ContentLength {
		return nil, "", fail("partial_media")
	}
	return data, response.Header.Get("Content-Type"), nil
}
