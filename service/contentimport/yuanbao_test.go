package contentimport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestYuanbaoResolveUsesTwoStepPayloadAndKeepsCookieOnYuanbao(t *testing.T) {
	type requestRecord struct {
		Host    string
		Path    string
		Cookie  string
		Referer string
		Query   url.Values
		Body    map[string]any
	}
	var mu sync.Mutex
	var records []requestRecord
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		mu.Lock()
		records = append(records, requestRecord{Host: request.URL.Host, Path: request.URL.Path, Cookie: request.Header.Get("Cookie"), Referer: request.Header.Get("Referer"), Query: request.URL.Query(), Body: payload})
		mu.Unlock()
		switch request.URL.Host {
		case "yuanbao.tencent.com":
			return response(http.StatusOK, `{
                    "code":0,
                    "data":{
                        "wx_export_id":"export/from?preview",
                        "author":"视频作者",
                        "desc":"视频标题",
                        "cover_url":"https://img.example/cover.jpg",
                        "playable_url":"https://channels.weixin.qq.com/finder-preview/pages/feed?token=general-token%2Bwith%26symbols&eid=export%2Ffrom%3Fpreview"
                    }
                }`), nil
		case "channels.weixin.qq.com":
			return response(http.StatusOK, `{
                    "errCode":0,
                    "data":{"feedInfo":{"h264VideoInfo":{"videoUrl":"https://video.example/h264.mp4"},"videoUrl":"https://video.example/fallback.mp4"}}
                }`), nil
		default:
			t.Fatalf("unexpected host %q", request.URL.Host)
			return nil, nil
		}
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	channelsURL, err := url.Parse("https://channels.weixin.qq.com/")
	if err != nil {
		t.Fatalf("parse channels URL: %v", err)
	}
	jar.SetCookies(channelsURL, []*http.Cookie{{Name: "broad", Value: "must-not-cross-host", Domain: ".weixin.qq.com"}})
	resolver := NewYuanbaoResolver(YuanbaoConfig{Enabled: true, Cookie: "session=server-secret", HTTPClient: &http.Client{Transport: transport, Jar: jar}})

	source, err := resolver.Resolve(context.Background(), "https://channels.weixin.qq.com/s/AbCdEf")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if source.Platform != Channels || source.ID != "export/from?preview" || source.Title != "视频标题" || source.Author != "视频作者" {
		t.Fatalf("source = %#v", source)
	}
	if len(source.Media) != 1 || source.Media[0].URL != "https://video.example/h264.mp4" {
		t.Fatalf("media = %#v", source.Media)
	}
	if !source.WatermarkExtraction {
		t.Fatal("extraction route intent was not recorded")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(records) != 2 {
		t.Fatalf("request count = %d", len(records))
	}
	if records[0].Host != "yuanbao.tencent.com" || records[0].Path != "/api/weixin/get_parse_result" || records[0].Cookie == "" {
		t.Fatalf("first request = %#v", records[0])
	}
	if records[0].Body["type"] != "video_channel_url" || records[0].Body["scene"] != float64(1) {
		t.Fatalf("first payload = %#v", records[0].Body)
	}
	if records[1].Host != "channels.weixin.qq.com" || records[1].Path != "/finder-preview/api/feed/get_feed_info" || records[1].Cookie != "" {
		t.Fatalf("second request = %#v", records[1])
	}
	if !regexp.MustCompile(`^[0-9a-f]+-[0-9a-f]{8}$`).MatchString(records[1].Query.Get("_rid")) || records[1].Query.Get("_pageUrl") != "https://channels.weixin.qq.com/finder-preview/pages/feed" {
		t.Fatalf("second request query = %#v", records[1].Query)
	}
	referer, err := url.Parse(records[1].Referer)
	if err != nil || referer.Scheme != "https" || referer.Host != "channels.weixin.qq.com" || referer.Path != "/finder-preview/pages/feed" {
		t.Fatalf("second request Referer = %q", records[1].Referer)
	}
	wantReferer := url.Values{"entry_card_type": {"48"}, "comment_scene": {"39"}, "appid": {"0"}, "token": {"general-token+with&symbols"}, "entry_scene": {"0"}, "eid": {"export/from?preview"}}
	if referer.Query().Encode() != wantReferer.Encode() {
		t.Fatalf("second request Referer query = %#v", referer.Query())
	}
	baseReq, ok := records[1].Body["baseReq"].(map[string]any)
	if !ok || baseReq["generalToken"] != "general-token+with&symbols" || records[1].Body["exportId"] != "export/from?preview" {
		t.Fatalf("second payload = %#v", records[1].Body)
	}
}

func TestYuanbaoFailsClosedForConfigurationAndPreviewURL(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		called = true
		return response(http.StatusOK, `{"code":0,"data":{"playable_url":"https://evil.example/feed?token=t&eid=e"}}`), nil
	})}

	if _, err := NewYuanbaoResolver(YuanbaoConfig{}).Resolve(context.Background(), "https://channels.weixin.qq.com/s/a"); errorCode(err) != "disabled" {
		t.Fatalf("disabled code = %q", errorCode(err))
	}
	if _, err := NewYuanbaoResolver(YuanbaoConfig{Enabled: true, HTTPClient: client}).Resolve(context.Background(), "https://channels.weixin.qq.com/s/a"); errorCode(err) != "provider_not_configured" {
		t.Fatalf("missing cookie code = %q", errorCode(err))
	}
	if called {
		t.Fatal("provider called without cookie")
	}
	if _, err := NewYuanbaoResolver(YuanbaoConfig{Enabled: true, Cookie: "session=x", HTTPClient: client}).Resolve(context.Background(), "https://channels.weixin.qq.com/s/a"); errorCode(err) != "invalid_preview_url" {
		t.Fatalf("preview URL code = %q", errorCode(err))
	}
}

func TestYuanbaoRejectsMetadataOnlyAndEncryptedVideo(t *testing.T) {
	tests := []struct {
		name     string
		feedBody string
		wantCode string
	}{
		{
			name:     "metadata only",
			feedBody: `{"errCode":0,"data":{"feedInfo":{"description":"only metadata"}}}`,
			wantCode: "metadata_only",
		},
		{
			name:     "decode key",
			feedBody: `{"errCode":0,"data":{"feedInfo":{"h264VideoInfo":{"videoUrl":"https://video.example/encrypted.mp4","decodeKey":"12345"}}}}`,
			wantCode: "unsupported_encryption",
		},
		{name: "object decode key", feedBody: `{"errCode":0,"data":{"feedInfo":{"h264VideoInfo":{"videoUrl":"https://video.example/encrypted.mp4","decodeKey":{"key":"secret"}}}}}`, wantCode: "unsupported_encryption"},
		{name: "array decode key", feedBody: `{"errCode":0,"data":{"feedInfo":{"h264VideoInfo":{"videoUrl":"https://video.example/encrypted.mp4","decodeKey":[]}}}}`, wantCode: "unsupported_encryption"},
		{name: "boolean decode key", feedBody: `{"errCode":0,"data":{"feedInfo":{"h264VideoInfo":{"videoUrl":"https://video.example/encrypted.mp4","decodeKey":false}}}}`, wantCode: "unsupported_encryption"},
		{name: "numeric zero is clear", feedBody: `{"errCode":0,"data":{"feedInfo":{"h264VideoInfo":{"videoUrl":"https://video.example/clear.mp4","decodeKey":0}}}}`, wantCode: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host == "yuanbao.tencent.com" {
					return response(http.StatusOK, `{"code":0,"data":{"wx_export_id":"export","playable_url":"https://channels.weixin.qq.com/finder-preview/pages/feed?token=token&eid=export"}}`), nil
				}
				return response(http.StatusOK, tt.feedBody), nil
			})
			resolver := NewYuanbaoResolver(YuanbaoConfig{Enabled: true, Cookie: "session=x", HTTPClient: &http.Client{Transport: transport}})
			if _, err := resolver.Resolve(context.Background(), "https://channels.weixin.qq.com/s/a"); errorCode(err) != tt.wantCode {
				t.Fatalf("error code = %q, want %q", errorCode(err), tt.wantCode)
			}
		})
	}
}

func TestYuanbaoRejectsMismatchedExportIdentity(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "yuanbao.tencent.com" {
			return response(http.StatusOK, `{"code":0,"data":{"wx_export_id":"export-a","playable_url":"https://channels.weixin.qq.com/finder-preview/pages/feed?token=token&eid=export-b"}}`), nil
		}
		return response(http.StatusOK, `{"errCode":0,"data":{"feedInfo":{"h264VideoInfo":{"videoUrl":"https://video.example/item.mp4"}}}}`), nil
	})
	resolver := NewYuanbaoResolver(YuanbaoConfig{Enabled: true, Cookie: "session=x", HTTPClient: &http.Client{Transport: transport}})
	if _, err := resolver.Resolve(context.Background(), "https://channels.weixin.qq.com/s/a"); errorCode(err) != "provider_identity_mismatch" {
		t.Fatalf("error code = %q", errorCode(err))
	}
}

func TestYuanbaoClassifiesLoginAndRiskControlWithoutLeakingCookie(t *testing.T) {
	tests := []struct {
		body     string
		wantCode string
	}{
		{body: `{"code":401,"msg":"登录已失效"}`, wantCode: "requires_login"},
		{body: `{"code":429,"msg":"风控拦截，请稍后再试"}`, wantCode: "risk_control"},
	}
	for _, tt := range tests {
		resolver := NewYuanbaoResolver(YuanbaoConfig{
			Enabled: true,
			Cookie:  "cookie-value-must-not-appear-in-error",
			HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return response(http.StatusOK, tt.body), nil
			})},
		})
		_, err := resolver.Resolve(context.Background(), "https://channels.weixin.qq.com/s/a")
		if errorCode(err) != tt.wantCode {
			t.Fatalf("error code = %q, want %q", errorCode(err), tt.wantCode)
		}
		if strings.Contains(err.Error(), "cookie-value") {
			t.Fatal("error leaked cookie")
		}
	}
}

func TestYuanbaoHTTPUnauthorizedUsesCanonicalLoginCode(t *testing.T) {
	resolver := NewYuanbaoResolver(YuanbaoConfig{Enabled: true, Cookie: "session=x", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusUnauthorized, `{}`), nil
	})}})
	if _, err := resolver.Resolve(context.Background(), "https://channels.weixin.qq.com/s/a"); errorCode(err) != "requires_login" {
		t.Fatalf("error code = %q", errorCode(err))
	}
}
