package contentimport

import "testing"

func TestParseLinkExtractsAndCanonicalizesSupportedCopy(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		platform Platform
		wantURL  string
	}{
		{
			name:     "douyin copied text",
			raw:      "6.66 05/21 user:/ 看看这个 https://www.douyin.com/video/1234567890123456789?foo=bar 复制打开抖音",
			platform: Douyin,
			wantURL:  "https://www.douyin.com/jingxuan?modal_id=1234567890123456789",
		},
		{
			name:     "xiaohongshu token preserved",
			raw:      "https://www.xiaohongshu.com/discovery/item/0123456789abcdef01234567?xsec_token=token%2Bvalue&ignored=1",
			platform: Xiaohongshu,
			wantURL:  "https://www.xiaohongshu.com/explore/0123456789abcdef01234567?xsec_token=token%2Bvalue",
		},
		{
			name:     "short link retained for provider",
			raw:      "http://v.douyin.com:80/AbCdEfG/",
			platform: Douyin,
			wantURL:  "https://v.douyin.com/AbCdEfG/",
		},
		{
			name:     "channels share",
			raw:      "视频号链接：http://channels.weixin.qq.com:80/s/AbCdEf?from=share。",
			platform: Channels,
			wantURL:  "https://channels.weixin.qq.com/s/AbCdEf?from=share",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLink(tt.raw)
			if err != nil {
				t.Fatalf("ParseLink() error = %v", err)
			}
			if got.Platform != tt.platform || got.URL != tt.wantURL {
				t.Fatalf("ParseLink() = %#v, want platform=%q url=%q", got, tt.platform, tt.wantURL)
			}
		})
	}
}

func TestParseLinkRejectsUnsafeOrUnsupportedAuthorities(t *testing.T) {
	tests := []string{
		"http://127.0.0.1/video/1234567890123456789",
		"https://user:pass@www.douyin.com/video/1234567890123456789",
		"https://www.douyin.com:8443/video/1234567890123456789",
		"https://douyin.com.evil.example/video/1234567890123456789",
		"https://example.com/?next=https://www.douyin.com/video/1234567890123456789",
		"https://channels.weixin.qq.com/finder-preview/pages/feed?token=secret&eid=export",
	}
	for _, raw := range tests {
		if _, err := ParseLink(raw); errorCode(err) != "invalid_link" {
			t.Errorf("ParseLink(%q) code = %q, want invalid_link", raw, errorCode(err))
		}
	}
}

func TestParseLinkDoesNotAcceptArticleAsChannelsVideo(t *testing.T) {
	if _, err := ParseLink("https://mp.weixin.qq.com/s/example"); errorCode(err) != "unsupported_platform" {
		t.Fatalf("ParseLink() code = %q, want unsupported_platform", errorCode(err))
	}
}

func errorCode(err error) string {
	if resolved, ok := err.(*ResolveError); ok {
		return resolved.Code
	}
	return ""
}
