package contentimport

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	yuanbaoParseEndpoint = "https://yuanbao.tencent.com/api/weixin/get_parse_result"
	channelsFeedEndpoint = "https://channels.weixin.qq.com/finder-preview/api/feed/get_feed_info"
)

// YuanbaoConfig is server-side only. Cookie must come from protected runtime
// configuration and is never read from process environment by this package.
type YuanbaoConfig struct {
	Enabled    bool
	Cookie     string
	HTTPClient *http.Client
}

// YuanbaoResolver is experimental because the upstream session and protocol
// are not a public stable API.
type YuanbaoResolver struct {
	enabled bool
	cookie  string
	client  *http.Client
}

func NewYuanbaoResolver(config YuanbaoConfig) *YuanbaoResolver {
	return &YuanbaoResolver{
		enabled: config.Enabled,
		cookie:  strings.TrimSpace(config.Cookie),
		client:  boundedHTTPClient(config.HTTPClient),
	}
}

func (resolver *YuanbaoResolver) Resolve(ctx context.Context, raw string) (Source, error) {
	if !resolver.enabled {
		return Source{}, fail("disabled")
	}
	if resolver.cookie == "" {
		return Source{}, fail("provider_not_configured")
	}
	link, err := ParseLink(raw)
	if err != nil {
		return Source{}, err
	}
	if link.Platform != Channels {
		return Source{}, fail("unsupported_platform")
	}

	parsed, err := resolver.parseShare(ctx, link.URL)
	if err != nil {
		return Source{}, err
	}
	preview, err := parsePreviewURL(parsed.PlayableURL)
	if err != nil {
		return Source{}, err
	}
	firstExportID := strings.TrimSpace(parsed.ExportID)
	if firstExportID != "" && firstExportID != preview.ExportID {
		return Source{}, fail("provider_identity_mismatch")
	}
	feed, err := resolver.fetchFeed(ctx, preview)
	if err != nil {
		return Source{}, err
	}
	if feed.encrypted() {
		return Source{}, fail("unsupported_encryption")
	}
	videoURL := firstNonEmpty(feed.H264.VideoURL, feed.VideoURL, feed.H265.VideoURL)
	videoURL = validRemoteURL(videoURL)
	if videoURL == "" {
		return Source{}, fail("metadata_only")
	}

	sourceID := preview.ExportID
	if sourceID == "" {
		return Source{}, fail("invalid_preview_url")
	}
	title := strings.TrimSpace(firstNonEmpty(parsed.Description, feed.Description))
	if title == "" {
		title = "视频号视频"
	}
	coverURL := validRemoteURL(firstNonEmpty(parsed.CoverURL, feed.CoverURL))
	return Source{
		Platform:  Channels,
		ID:        sourceID,
		SourceURL: link.URL,
		Title:     truncateRunes(title, 200),
		Author:    truncateRunes(strings.TrimSpace(parsed.Author), 120),
		Media: []RemoteMedia{{
			ID:      sourceID,
			Kind:    "video",
			URL:     videoURL,
			Caption: truncateRunes(title, 2000),
		}},
		CoverURL:            coverURL,
		WatermarkExtraction: true,
	}, nil
}

type yuanbaoParseData struct {
	ExportID    string `json:"wx_export_id"`
	CoverURL    string `json:"cover_url"`
	Author      string `json:"author"`
	Description string `json:"desc"`
	PlayableURL string `json:"playable_url"`
}

func (resolver *YuanbaoResolver) parseShare(ctx context.Context, sourceURL string) (yuanbaoParseData, error) {
	body, err := json.Marshal(struct {
		Type  string `json:"type"`
		URL   string `json:"url"`
		Scene int    `json:"scene"`
	}{Type: "video_channel_url", URL: sourceURL, Scene: 1})
	if err != nil {
		return yuanbaoParseData{}, fail("provider_request")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, yuanbaoParseEndpoint, bytes.NewReader(body))
	if err != nil {
		return yuanbaoParseData{}, fail("provider_request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cookie", resolver.cookie)

	responseBody, err := resolver.doJSON(request)
	if err != nil {
		return yuanbaoParseData{}, err
	}
	var envelope struct {
		Code json.RawMessage  `json:"code"`
		Msg  string           `json:"msg"`
		Data yuanbaoParseData `json:"data"`
	}
	if json.Unmarshal(responseBody, &envelope) != nil {
		return yuanbaoParseData{}, fail("invalid_provider_response")
	}
	code, ok := rawInt(envelope.Code)
	if !ok {
		return yuanbaoParseData{}, fail("invalid_provider_response")
	}
	if code != 0 {
		return yuanbaoParseData{}, classifiedProviderError(envelope.Msg)
	}
	if strings.TrimSpace(envelope.Data.PlayableURL) == "" {
		return yuanbaoParseData{}, fail("metadata_only")
	}
	return envelope.Data, nil
}

type previewReference struct {
	Token    string
	ExportID string
}

func parsePreviewURL(raw string) (previewReference, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "channels.weixin.qq.com" || parsed.User != nil || parsed.Port() != "" {
		return previewReference{}, fail("invalid_preview_url")
	}
	if !strings.HasPrefix(parsed.Path, "/finder-preview/") {
		return previewReference{}, fail("invalid_preview_url")
	}
	token := strings.TrimSpace(parsed.Query().Get("token"))
	exportID := strings.TrimSpace(parsed.Query().Get("eid"))
	if token == "" || exportID == "" {
		return previewReference{}, fail("invalid_preview_url")
	}
	return previewReference{Token: token, ExportID: exportID}, nil
}

type feedVideoInfo struct {
	VideoURL  string          `json:"videoUrl"`
	DecodeKey json.RawMessage `json:"decodeKey"`
}

type channelsFeed struct {
	VideoURL    string          `json:"videoUrl"`
	DecodeKey   json.RawMessage `json:"decodeKey"`
	H264        feedVideoInfo   `json:"h264VideoInfo"`
	H265        feedVideoInfo   `json:"h265VideoInfo"`
	Description string          `json:"description"`
	CoverURL    string          `json:"coverUrl"`
}

func (feed channelsFeed) encrypted() bool {
	for _, raw := range []json.RawMessage{feed.DecodeKey, feed.H264.DecodeKey, feed.H265.DecodeKey} {
		if !clearDecodeKey(raw) {
			return true
		}
	}
	return false
}

func clearDecodeKey(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return true
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		value = strings.TrimSpace(value)
		return value == "" || value == "0"
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		value, err := number.Float64()
		return err == nil && value == 0
	}
	// Booleans, objects and arrays are not known clear-text sentinels. Treat
	// unknown shapes as encrypted so an upstream schema change fails closed.
	return false
}

func (resolver *YuanbaoResolver) fetchFeed(ctx context.Context, preview previewReference) (channelsFeed, error) {
	body, err := json.Marshal(struct {
		BaseRequest struct {
			GeneralToken string `json:"generalToken"`
		} `json:"baseReq"`
		ExportID string `json:"exportId"`
	}{
		BaseRequest: struct {
			GeneralToken string `json:"generalToken"`
		}{GeneralToken: preview.Token},
		ExportID: preview.ExportID,
	})
	if err != nil {
		return channelsFeed{}, fail("provider_request")
	}
	rid, err := newYuanbaoRID()
	if err != nil {
		return channelsFeed{}, fail("provider_request")
	}
	endpoint, err := url.Parse(channelsFeedEndpoint)
	if err != nil {
		return channelsFeed{}, fail("provider_request")
	}
	endpoint.RawQuery = url.Values{
		"_rid":     {rid},
		"_pageUrl": {"https://channels.weixin.qq.com/finder-preview/pages/feed"},
	}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return channelsFeed{}, fail("provider_request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://channels.weixin.qq.com")
	referer := url.URL{
		Scheme: "https",
		Host:   "channels.weixin.qq.com",
		Path:   "/finder-preview/pages/feed",
		RawQuery: url.Values{
			"entry_card_type": {"48"},
			"comment_scene":   {"39"},
			"appid":           {"0"},
			"token":           {preview.Token},
			"entry_scene":     {"0"},
			"eid":             {preview.ExportID},
		}.Encode(),
	}
	request.Header.Set("Referer", referer.String())

	responseBody, err := resolver.doJSON(request)
	if err != nil {
		return channelsFeed{}, err
	}
	var envelope struct {
		ErrorCode json.RawMessage `json:"errCode"`
		ErrorMsg  json.RawMessage `json:"errMsg"`
		Data      struct {
			FeedInfo json.RawMessage `json:"feedInfo"`
			ErrorMsg json.RawMessage `json:"errMsg"`
		} `json:"data"`
	}
	if json.Unmarshal(responseBody, &envelope) != nil {
		return channelsFeed{}, fail("invalid_provider_response")
	}
	if len(envelope.ErrorCode) > 0 {
		code, ok := rawInt(envelope.ErrorCode)
		if !ok {
			return channelsFeed{}, fail("invalid_provider_response")
		}
		if code != 0 {
			return channelsFeed{}, classifiedProviderError(string(envelope.ErrorMsg))
		}
	}
	if hasProviderError(envelope.Data.ErrorMsg) {
		return channelsFeed{}, classifiedProviderError(string(envelope.Data.ErrorMsg))
	}
	if len(envelope.Data.FeedInfo) == 0 || string(envelope.Data.FeedInfo) == "null" {
		return channelsFeed{}, fail("metadata_only")
	}
	var feed channelsFeed
	if json.Unmarshal(envelope.Data.FeedInfo, &feed) != nil {
		return channelsFeed{}, fail("invalid_provider_response")
	}
	return feed, nil
}

func newYuanbaoRID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return strconv.FormatInt(time.Now().Unix(), 16) + "-" + hex.EncodeToString(random), nil
}

func (resolver *YuanbaoResolver) doJSON(request *http.Request) ([]byte, error) {
	response, err := resolver.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(request.Context().Err(), context.DeadlineExceeded) {
			return nil, uncertainResult()
		}
		if errors.Is(err, context.Canceled) || errors.Is(request.Context().Err(), context.Canceled) {
			return nil, fail("cancelled")
		}
		return nil, uncertainResult()
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusInternalServerError {
		return nil, uncertainResult()
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, fail("requires_login")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, fail("risk_control")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fail("provider_failed")
	}
	return readLimitedBody(response.Body)
}

func classifiedProviderError(message string) *ResolveError {
	normalized := strings.ToLower(message)
	switch {
	case containsAny(normalized, "登录", "cookie", "unauthorized", "not login", "session expired"):
		return fail("requires_login")
	case containsAny(normalized, "风控", "频繁", "验证码", "risk", "captcha", "too many"):
		return fail("risk_control")
	case containsAny(normalized, "过期", "失效", "不存在", "下架", "expired", "not found", "offline"):
		return fail("source_unavailable")
	default:
		return fail("provider_failed")
	}
}

func hasProviderError(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` || string(raw) == "{}" {
		return false
	}
	var object struct {
		Type    int    `json:"type"`
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return object.Type != 0 || strings.TrimSpace(object.Title) != "" || strings.TrimSpace(object.Content) != ""
	}
	return rawString(raw) != ""
}

var _ Resolver = (*YuanbaoResolver)(nil)
