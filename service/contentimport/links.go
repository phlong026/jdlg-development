package contentimport

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	linkPattern         = regexp.MustCompile(`(?i)(?:https?://|(?:www\.)?(?:v\.douyin\.com|douyin\.com|xiaohongshu\.com|xhslink\.com|v\.kuaishou\.com|kuaishou\.com|m\.chenzhongtech\.com|chenzhongtech\.com|m\.kuai-fei\.com|channels\.weixin\.qq\.com|weixin\.qq\.com)/)[^\s<>\"']*`)
	douyinPathID        = regexp.MustCompile(`/(?:video|note|slides)/(\d{15,22})(?:/|$)`)
	xiaohongshuPathID   = regexp.MustCompile(`/(?:explore|discovery/item)/(?:[^/]+/)?([0-9a-fA-F]{24})(?:/|$)`)
	douyinNativeID      = regexp.MustCompile(`^\d{15,22}$`)
	xiaohongshuNativeID = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)
)

// ParsedLink is a validated source link. Short links stay untouched so the
// configured provider, not this service, owns any redirect resolution.
type ParsedLink struct {
	Platform Platform
	URL      string
}

func ParseLink(raw string) (ParsedLink, error) {
	match := linkPattern.FindString(strings.TrimSpace(raw))
	if match == "" {
		return ParsedLink{}, fail("invalid_link")
	}
	match = strings.TrimRight(match, ",.!?;:)]}\uff0c\u3002\uff01\uff1f\uff1b\uff1a\u3001\uff09\u3011\u3011\u300b\u300d\u201d")
	if !strings.HasPrefix(strings.ToLower(match), "http://") && !strings.HasPrefix(strings.ToLower(match), "https://") {
		match = "https://" + match
	}

	parsed, err := url.Parse(match)
	if err != nil || parsed.Host == "" || parsed.User != nil || !validSourcePort(parsed) {
		return ParsedLink{}, fail("invalid_link")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ParsedLink{}, fail("invalid_link")
	}
	parsed.Fragment = ""
	parsed.Host = strings.ToLower(parsed.Host)

	platform, ok := sourcePlatform(parsed)
	if !ok {
		if parsed.Hostname() == "mp.weixin.qq.com" {
			return ParsedLink{}, fail("unsupported_platform")
		}
		return ParsedLink{}, fail("invalid_link")
	}
	// Supported share hosts all provide TLS. Normalize copied http links before
	// any provider sees them, and drop the now-invalid explicit port 80.
	parsed.Scheme = "https"
	parsed.Host = strings.ToLower(parsed.Hostname())
	if platform == Channels {
		return ParsedLink{Platform: platform, URL: parsed.String()}, nil
	}
	return ParsedLink{Platform: platform, URL: canonicalProviderURL(platform, parsed)}, nil
}

func validSourcePort(parsed *url.URL) bool {
	port := parsed.Port()
	if port == "" {
		return true
	}
	return (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80")
}

func sourcePlatform(parsed *url.URL) (Platform, bool) {
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "douyin.com", "www.douyin.com", "v.douyin.com", "iesdouyin.com", "www.iesdouyin.com":
		return Douyin, true
	case "xiaohongshu.com", "www.xiaohongshu.com", "xhslink.com", "www.xhslink.com":
		return Xiaohongshu, true
	case "kuaishou.com", "www.kuaishou.com", "v.kuaishou.com", "chenzhongtech.com", "m.chenzhongtech.com", "kuai-fei.com", "m.kuai-fei.com":
		return Kuaishou, true
	case "channels.weixin.qq.com":
		if strings.HasPrefix(parsed.Path, "/s/") {
			return Channels, true
		}
	case "weixin.qq.com":
		if strings.HasPrefix(parsed.Path, "/sph/") {
			return Channels, true
		}
	}
	return "", false
}

func canonicalProviderURL(platform Platform, parsed *url.URL) string {
	if isShortLinkHost(parsed.Hostname()) {
		return parsed.String()
	}

	if platform == Douyin {
		id := firstMatch(douyinPathID, parsed.Path)
		if id == "" {
			id = validDouyinID(parsed.Query().Get("modal_id"))
		}
		if id != "" {
			return "https://www.douyin.com/jingxuan?modal_id=" + url.QueryEscape(id)
		}
		return parsed.String()
	}

	id := firstMatch(xiaohongshuPathID, parsed.Path)
	if id == "" {
		id = validXiaohongshuID(parsed.Query().Get("note_id"))
	}
	if id == "" {
		return parsed.String()
	}
	canonical := "https://www.xiaohongshu.com/explore/" + strings.ToLower(id)
	if token := parsed.Query().Get("xsec_token"); token != "" {
		canonical += "?" + url.Values{"xsec_token": []string{token}}.Encode()
	}
	return canonical
}

func isShortLinkHost(host string) bool {
	switch strings.ToLower(host) {
	case "v.douyin.com", "xhslink.com", "www.xhslink.com", "v.kuaishou.com":
		return true
	default:
		return false
	}
}

func firstMatch(pattern *regexp.Regexp, value string) string {
	match := pattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func validDouyinID(value string) string {
	if douyinNativeID.MatchString(value) {
		return value
	}
	return ""
}

func validXiaohongshuID(value string) string {
	if xiaohongshuNativeID.MatchString(value) {
		return strings.ToLower(value)
	}
	return ""
}
