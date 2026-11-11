package contentimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"service/tracking"
)

type HTTPFetcher struct{ client *http.Client }

// MediaFile is a private temporary download. Call Remove after persisting it.
// Keeping the payload on disk prevents large imports from being buffered in RAM.
type MediaFile struct {
	Path   string
	MIME   string
	Size   int64
	SHA256 string
}

func (m MediaFile) Remove() error {
	if m.Path == "" {
		return nil
	}
	return os.Remove(m.Path)
}

const (
	maxXHSPageBytes        = 4 << 20
	xhsPublicPageUserAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148"
)

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
	link, err := ParseLink(raw)
	if err == nil && link.Platform == Xiaohongshu {
		return parseXHSPageLink(raw)
	}
	return link, err
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
func (f *HTTPFetcher) FetchXHSPage(ctx context.Context, raw string) ([]byte, error) {
	link, err := parseXHSPageLink(raw)
	if err != nil || link.Platform != Xiaohongshu || !validXHSPageURL(link.URL) {
		return nil, fail("invalid_link")
	}
	body, _, err := f.fetch(ctx, Xiaohongshu, link.URL, maxXHSPageBytes, true)
	var known *ResolveError
	if errors.As(err, &known) && known.Code == "download_failed" {
		err = fail("xhs_page_fetch_failed")
	}
	return body, err
}
func (f *HTTPFetcher) FetchMedia(ctx context.Context, platform Platform, raw string, max int64) ([]byte, string, error) {
	return f.fetch(ctx, platform, raw, max, false)
}

func (f *HTTPFetcher) FetchMediaFile(ctx context.Context, platform Platform, raw string, max int64) (MediaFile, error) {
	var media MediaFile
	if f == nil || f.client == nil {
		return media, ErrUnavailable
	}
	temp, err := os.CreateTemp("", "content-import-media-*")
	if err != nil {
		return media, fail("download_failed")
	}
	media.Path = temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = media.Remove()
		}
	}()
	hasher := sha256.New()
	client := *f.client
	client.Timeout = 15 * time.Minute
	streamingFetcher := *f
	streamingFetcher.client = &client
	media.Size, media.MIME, err = streamingFetcher.fetchInto(ctx, platform, raw, max, false, io.MultiWriter(temp, hasher))
	if err != nil {
		return MediaFile{}, err
	}
	if err = temp.Close(); err != nil {
		return MediaFile{}, fail("download_failed")
	}
	media.SHA256 = hex.EncodeToString(hasher.Sum(nil))
	keep = true
	return media, nil
}
func allowedImportURL(platform Platform, raw string, article bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || len(raw) > 16384 || u.Fragment != "" || u.Port() != "" && u.Port() != "443" || u.Scheme != "https" && u.Scheme != "http" {
		return nil, fail("media_host_not_allowed")
	}
	if article {
		if platform == Xiaohongshu {
			if !validXHSPageURL(u.String()) {
				return nil, fail("media_host_not_allowed")
			}
		} else if platform != WechatArticle || u.Hostname() != "mp.weixin.qq.com" {
			return nil, fail("media_host_not_allowed")
		}
	} else {
		domains := map[Platform][]string{
			WechatArticle: {"qpic.cn", "qlogo.cn"},
			Channels:      {"qpic.cn", "qlogo.cn", "finder.video.qq.com", "findermp.video.qq.com", "wxapp.tc.qq.com"},
			Douyin:        {"douyinvod.com", "douyinvodcdn.com", "douyinvodds.com", "douyinpic.com", "byteimg.com", "ibytedtos.com", "bytecdn.cn"},
			Xiaohongshu:   {"xhscdn.com", "rednotevideo.com", "xiaohongshu.com"},
			// B18-B：快手系 CDN（真实样例验收时按网关实际返回域补全）。
			Kuaishou: {"kuaishou.com", "yximgs.com", "gifshow.com", "ksapisrc.com", "kwimgs.com"},
			// B1：AI 生成结果图转存。URL 来自我们自己的任务结果表（服务端供应商
			// 写入），威胁等级低于用户粘贴链接；域名覆盖在用的生成 CDN，
			// 拨号层公网实 IP 校验仍然全程生效。真实供应商域随任务样例补全。
			"ai_generation": {"byteimg.com", "ibytedtos.com", "douyinpic.com", "bytecdn.cn", "myqcloud.com", "volces.com", "volcengine.com", "aigccloud.com"},
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
	var buffer bytes.Buffer
	_, mime, err := f.fetchInto(ctx, platform, raw, max, article, &buffer)
	if err != nil {
		return nil, "", err
	}
	return buffer.Bytes(), mime, nil
}

func (f *HTTPFetcher) fetchInto(ctx context.Context, platform Platform, raw string, max int64, article bool, destination io.Writer) (int64, string, error) {
	if f == nil || f.client == nil || max <= 0 {
		return 0, "", ErrUnavailable
	}
	u, err := allowedImportURL(platform, raw, article)
	if err != nil {
		return 0, "", err
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
		return 0, "", fail("media_host_not_allowed")
	}
	req.Header.Set("User-Agent", "JDLG-ContentImport/1.0")
	if article {
		if platform == Xiaohongshu {
			req.Header.Set("User-Agent", xhsPublicPageUserAgent)
		}
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
	} else {
		req.Header.Set("Accept", "image/png,image/jpeg,image/gif,video/mp4")
		if platform == WechatArticle || platform == Channels {
			req.Header.Set("Referer", "https://mp.weixin.qq.com/")
		}
	}
	response, err := client.Do(req)
	if err != nil {
		return 0, "", fail("download_failed")
	}
	defer response.Body.Close()
	if article && (response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 429) {
		return 0, "", fail("risk_control")
	}
	if article && (response.StatusCode == 404 || response.StatusCode == 410) {
		return 0, "", fail("source_unavailable")
	}
	if !article && (response.StatusCode == 401 || response.StatusCode == 403 || response.StatusCode == 404 || response.StatusCode == 410) {
		return 0, "", fail("media_source_expired")
	}
	if response.StatusCode != 200 && response.StatusCode != 206 {
		return 0, "", fail("download_failed")
	}
	if article && platform == Xiaohongshu && !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
		return 0, "", fail("download_failed")
	}
	if response.ContentLength > max {
		return 0, "", fail("media_too_large")
	}
	if response.StatusCode == 206 {
		var start, end, total int64
		if _, err = fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil || start != 0 || total <= 0 || end+1 != total || response.ContentLength != total {
			return 0, "", fail("partial_media")
		}
	}
	written, err := io.Copy(destination, io.LimitReader(response.Body, max+1))
	if err != nil || written == 0 {
		return 0, "", fail("download_failed")
	}
	if written > max {
		return 0, "", fail("media_too_large")
	}
	if response.ContentLength >= 0 && written != response.ContentLength {
		return 0, "", fail("partial_media")
	}
	return written, response.Header.Get("Content-Type"), nil
}

func validXHSPageURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" && u.Port() != "443" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.TrimSuffix(u.EscapedPath(), "/")
	if host == "xiaohongshu.com" || host == "www.xiaohongshu.com" {
		return validXiaohongshuID(firstMatch(xiaohongshuPathID, path)) != ""
	}
	if host != "xhslink.com" && host != "www.xhslink.com" {
		return false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] != "m" && parts[0] != "o" {
		return false
	}
	if len(parts[1]) < 4 || len(parts[1]) > 256 {
		return false
	}
	for _, char := range parts[1] {
		if !validXHSShortCodeChar(char) {
			return false
		}
	}
	return true
}

func validXHSShortCodeChar(char rune) bool {
	return char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char == '-' || char == '_'
}
