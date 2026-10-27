package contentimport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultCaptureTimeout   = 120 * time.Second
	maxCaptureResponseBytes = 512 << 10
)

type CaptureWorkerConfig struct {
	// Endpoint is deployment configuration. It must never be copied from an
	// article, request parameter or capture response.
	Endpoint   string
	Secret     string
	HTTPClient *http.Client
	Timeout    time.Duration
}

type CaptureWorker struct {
	endpoint string
	secret   string
	client   *http.Client
	ready    bool
}

func NewCaptureWorker(config CaptureWorkerConfig) *CaptureWorker {
	timeout := config.Timeout
	if timeout <= 0 || timeout > defaultCaptureTimeout {
		timeout = defaultCaptureTimeout
	}
	client := &http.Client{}
	if config.HTTPClient != nil {
		copy := *config.HTTPClient
		client = &copy
	}
	client.Timeout = timeout
	client.Jar = nil
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	endpoint := strings.TrimSpace(config.Endpoint)
	parsed, err := url.Parse(endpoint)
	ready := err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.Fragment == "" && parsed.RawQuery == "" && len(config.Secret) >= 32 && !strings.ContainsAny(config.Secret, "\r\n") && (parsed.Port() == "" || parsed.Port() == "443")
	return &CaptureWorker{endpoint: endpoint, secret: config.Secret, client: client, ready: ready}
}

func (worker *CaptureWorker) Ready() bool { return worker != nil && worker.ready }

func (worker *CaptureWorker) Capture(ctx context.Context, articleURL string, ids []string) ([]CaptureVideo, error) {
	if worker == nil || !worker.ready {
		return nil, fail("browser_required")
	}
	if !validArticleURL(articleURL) || len(ids) == 0 || len(ids) > 50 {
		return nil, fail("invalid_capture_request")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !wxvIDPattern.MatchString(id) || seen[id] {
			return nil, fail("invalid_capture_request")
		}
		seen[id] = true
	}
	payload, err := json.Marshal(struct {
		ArticleURL string   `json:"article_url"`
		VideoIDs   []string `json:"video_ids"`
	}{ArticleURL: articleURL, VideoIDs: ids})
	if err != nil {
		return nil, fail("capture_request_failed")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, worker.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fail("capture_request_failed")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if worker.secret != "" {
		request.Header.Set("Authorization", "Bearer "+worker.secret)
	}
	response, err := worker.client.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, fail("cancelled")
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, &ResolveError{Code: "capture_timeout", Retryable: true}
		}
		return nil, &ResolveError{Code: "capture_failed", Retryable: true}
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusConflict, http.StatusFailedDependency, http.StatusNotImplemented:
		return nil, fail("browser_required")
	case http.StatusUnprocessableEntity:
		return nil, fail("missing_video")
	case http.StatusTooManyRequests:
		return nil, fail("risk_control")
	default:
		if response.StatusCode >= 500 {
			return nil, &ResolveError{Code: "capture_failed", Retryable: true}
		}
		return nil, fail("capture_failed")
	}
	body, err := readCaptureResponse(response.Body)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Videos []CaptureVideo `json:"videos"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || len(envelope.Videos) == 0 || len(envelope.Videos) > 500 {
		return nil, fail("invalid_capture_response")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, fail("invalid_capture_response")
	}
	byID, err := validatedCapturedVideos(ids, envelope.Videos)
	if err != nil {
		return nil, err
	}
	ordered := make([]CaptureVideo, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, byID[id])
	}
	return ordered, nil
}

func readCaptureResponse(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, maxCaptureResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fail("capture_read_failed")
	}
	if len(body) > maxCaptureResponseBytes {
		return nil, fail("capture_response_too_large")
	}
	return body, nil
}

func validatedCapturedVideos(ids []string, videos []CaptureVideo) (map[string]CaptureVideo, error) {
	requested := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !wxvIDPattern.MatchString(id) || requested[id] {
			return nil, fail("invalid_capture_request")
		}
		requested[id] = true
	}
	selected := make(map[string]CaptureVideo, len(ids))
	for _, video := range videos {
		if !requested[video.ID] || video.FormatID < 0 || cleanCapturedVideoURL(video.URL, video.ID) == "" {
			return nil, fail("invalid_capture_response")
		}
		current, exists := selected[video.ID]
		// format_id orders candidates only; it is not a pixel-quality claim.
		if !exists || video.FormatID > current.FormatID {
			selected[video.ID] = video
		}
	}
	for _, id := range ids {
		if _, exists := selected[id]; !exists {
			return nil, fail("missing_video")
		}
	}
	return selected, nil
}

func cleanCapturedVideoURL(raw, id string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Port() != "" || !strings.EqualFold(strings.TrimSuffix(parsed.Hostname(), "."), "mpvideo.qpic.cn") {
		return ""
	}
	values, ok := parsed.Query()["vid"]
	if !ok || len(values) != 1 || values[0] != id {
		return ""
	}
	return parsed.String()
}
