package contentimport

import (
	"context"
	"crypto/md5" // #nosec G501 -- required by the provider's signing protocol.
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	douyidouEndpoint         = "https://gateway.diadi.cn/api/parse"
	providerRequestTimeout   = 25 * time.Second
	maxProviderResponseBytes = 2 << 20
)

type DouyidouConfig struct {
	AppID      string
	AppSecret  string
	HTTPClient *http.Client
}

type DouyidouResolver struct {
	appID     string
	appSecret string
	client    *http.Client
}

func NewDouyidouResolver(config DouyidouConfig) *DouyidouResolver {
	return &DouyidouResolver{
		appID:     strings.TrimSpace(config.AppID),
		appSecret: strings.TrimSpace(config.AppSecret),
		client:    boundedHTTPClient(config.HTTPClient),
	}
}

func (resolver *DouyidouResolver) Resolve(ctx context.Context, raw string) (Source, error) {
	if resolver.appID == "" || resolver.appSecret == "" {
		return Source{}, fail("provider_not_configured")
	}
	link, err := ParseLink(raw)
	if err != nil {
		return Source{}, err
	}
	if link.Platform != Douyin && link.Platform != Xiaohongshu {
		return Source{}, fail("unsupported_platform")
	}

	query, signature := douyidouSignature(resolver.appID, resolver.appSecret, link.URL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, douyidouEndpoint+"?"+query, nil)
	if err != nil {
		return Source{}, fail("provider_request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Sign", signature)

	response, err := resolver.client.Do(request)
	if err != nil {
		// The paid request may already have reached the provider. A transport
		// cancellation cannot prove that it did not consume quota or finish.
		return Source{}, uncertainResult()
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusInternalServerError {
		return Source{}, uncertainResult()
	}
	if response.StatusCode != http.StatusOK {
		return Source{}, fail("provider_failed")
	}

	body, err := readLimitedBody(response.Body)
	if err != nil {
		return Source{}, uncertainResult()
	}
	payload, err := decodeDouyidou(body)
	if err != nil {
		if code := resolveErrorCode(err); code == "parse_failed" || code == "source_unavailable" {
			return Source{}, err
		}
		return Source{}, uncertainResult()
	}
	source, err := douyidouSource(link, payload)
	if err != nil {
		return Source{}, uncertainResult()
	}
	return source, nil
}

func resolveErrorCode(err error) string {
	var resolved *ResolveError
	if errors.As(err, &resolved) {
		return resolved.Code
	}
	return ""
}

func douyidouSignature(appID, appSecret, sourceURL string) (string, string) {
	values := url.Values{"app_id": []string{appID}, "url": []string{sourceURL}}
	query := values.Encode()
	digest := md5.Sum([]byte(query + appSecret)) // #nosec G401 -- provider contract, not cryptographic authentication.
	return query, hex.EncodeToString(digest[:])
}

type douyidouPayload struct {
	AwemeID json.RawMessage `json:"aweme_id"`
	NoteID  json.RawMessage `json:"note_id"`
	ItemID  json.RawMessage `json:"item_id"`
	ID      json.RawMessage `json:"id"`
	Title   string          `json:"title"`
	Text    string          `json:"text"`
	Author  json.RawMessage `json:"author"`
	Cover   string          `json:"cover"`
	Video   json.RawMessage `json:"video"`
	Other   struct {
		Duration json.RawMessage `json:"duration"`
	} `json:"other"`
	Duration      json.RawMessage `json:"duration"`
	VideoDuration json.RawMessage `json:"video_duration"`
	Time          json.RawMessage `json:"time"`
	Length        json.RawMessage `json:"length"`
}

func decodeDouyidou(body []byte) (douyidouPayload, error) {
	var envelope struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Msg     string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return douyidouPayload{}, fail("invalid_provider_response")
	}
	code, ok := rawInt(envelope.Code)
	if !ok {
		return douyidouPayload{}, fail("invalid_provider_response")
	}
	if code != 0 {
		message := strings.ToLower(strings.TrimSpace(envelope.Message + " " + envelope.Msg))
		if containsAny(message, "过期", "失效", "不存在", "下架", "expired", "not found", "offline") {
			return douyidouPayload{}, fail("source_unavailable")
		}
		return douyidouPayload{}, fail("parse_failed")
	}
	var payload douyidouPayload
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, &payload) != nil {
		return douyidouPayload{}, fail("invalid_provider_response")
	}
	return payload, nil
}

func douyidouSource(link ParsedLink, payload douyidouPayload) (Source, error) {
	videoURL := firstRemoteURL(payload.Video)
	if videoURL == "" {
		return Source{}, fail("no_video")
	}

	var sourceID string
	if link.Platform == Xiaohongshu {
		sourceID = firstRawString(payload.NoteID, payload.ItemID, payload.ID)
		sourceID = validXiaohongshuID(sourceID)
	} else {
		sourceID = firstRawString(payload.AwemeID, payload.ItemID, payload.ID)
		sourceID = validDouyinID(sourceID)
	}
	if sourceID == "" {
		return Source{}, fail("invalid_native_id")
	}

	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = truncateRunes(strings.TrimSpace(payload.Text), 40)
	}
	if title == "" {
		title = "链接视频"
	}
	media := RemoteMedia{
		ID:         sourceID,
		Kind:       "video",
		URL:        videoURL,
		Caption:    truncateRunes(strings.TrimSpace(payload.Text), 2000),
		DurationMS: douyidouDuration(payload),
	}
	return Source{
		Platform:            link.Platform,
		ID:                  sourceID,
		SourceURL:           link.URL,
		Title:               truncateRunes(title, 200),
		Author:              truncateRunes(douyidouAuthor(payload.Author), 120),
		Media:               []RemoteMedia{media},
		CoverURL:            validRemoteURL(payload.Cover),
		WatermarkExtraction: true,
	}, nil
}

func firstRemoteURL(raw json.RawMessage) string {
	var entries []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &entries) != nil {
		return ""
	}
	for _, entry := range entries {
		if candidate := rawString(entry); candidate != "" {
			if candidate = validRemoteURL(candidate); candidate != "" {
				return candidate
			}
			continue
		}
		var object struct {
			URL     string `json:"url"`
			PlayURL string `json:"play_url"`
		}
		if json.Unmarshal(entry, &object) == nil {
			if candidate := validRemoteURL(firstNonEmpty(object.URL, object.PlayURL)); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func validRemoteURL(candidate string) string {
	parsed, err := url.Parse(strings.TrimSpace(candidate))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || !validSourcePort(parsed) {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return ""
	}
	return parsed.String()
}

func douyidouAuthor(raw json.RawMessage) string {
	if value := rawString(raw); value != "" {
		return value
	}
	var author struct {
		Nickname string `json:"nickname"`
		Name     string `json:"name"`
	}
	if json.Unmarshal(raw, &author) != nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(author.Nickname, author.Name))
}

func douyidouDuration(payload douyidouPayload) int64 {
	for _, raw := range []json.RawMessage{payload.Duration, payload.VideoDuration, payload.Time, payload.Length, payload.Other.Duration} {
		if duration := durationMilliseconds(raw); duration > 0 {
			return duration
		}
	}
	return 0
}

func durationMilliseconds(raw json.RawMessage) int64 {
	value := rawString(raw)
	if value == "" {
		return 0
	}
	duration, err := strconv.ParseFloat(value, 64)
	if err != nil || duration <= 0 {
		return 0
	}
	if duration <= 10_000 {
		duration *= 1000
	}
	return int64(duration + 0.5)
}

func firstRawString(values ...json.RawMessage) string {
	for _, value := range values {
		if resolved := rawString(value); resolved != "" {
			return resolved
		}
	}
	return ""
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func rawInt(raw json.RawMessage) (int, bool) {
	value := rawString(raw)
	if value == "" {
		return 0, false
	}
	resolved, err := strconv.Atoi(value)
	return resolved, err == nil
}

func boundedHTTPClient(existing *http.Client) *http.Client {
	client := &http.Client{}
	if existing != nil {
		copy := *existing
		client = &copy
	}
	client.Timeout = providerRequestTimeout
	client.Jar = nil
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

func readLimitedBody(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, maxProviderResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fail("provider_read_failed")
	}
	if len(body) > maxProviderResponseBytes {
		return nil, fail("response_too_large")
	}
	return body, nil
}

func uncertainResult() *ResolveError {
	return &ResolveError{Code: "submission_uncertain", Uncertain: true, Retryable: false}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

var _ Resolver = (*DouyidouResolver)(nil)
