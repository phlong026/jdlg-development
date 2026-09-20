package component

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"service/config"
)

const (
	ReportIllustrationProvider = "laozhang-gpt-image"
	ReportIllustrationWidth    = 2048
	ReportIllustrationHeight   = 1152

	reportIllustrationRequestTimeout  = 180 * time.Second
	reportIllustrationResponseMaxSize = 64 << 20
	reportIllustrationImageMaxSize    = 32 << 20
	reportIllustrationMaxPixels       = 16_000_000
)

// ReportIllustrationRequest contains only the material needed for one paid
// generation. ReferenceData must be a local PNG or JPEG; callers must not pass
// user-controlled remote URLs.
type ReportIllustrationRequest struct {
	Prompt        string
	ReferenceData []byte
}

// ReportIllustrationResult is the validated original returned by the provider.
type ReportIllustrationResult struct {
	Data     []byte `json:"-"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	MIMEType string `json:"mime_type"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// ValidateReportIllustration verifies a completed asset without making a
// provider request. It is used by the local CLI before reusing cached output.
func ValidateReportIllustration(data []byte) (mimeType string, width, height int, err error) {
	return inspectReportIllustration(data, true)
}

// ReportIllustrationClient is intentionally synchronous and performs one HTTP
// submission per Generate call. It never retries a paid request automatically.
type ReportIllustrationClient struct {
	endpoint string
	apiKey   string
	model    string
	http     *http.Client
}

func NewReportIllustrationClient(cfg *config.Config, httpClient *http.Client) (*ReportIllustrationClient, error) {
	if cfg == nil {
		return nil, errors.New("report illustration configuration is missing")
	}
	apiKey := strings.TrimSpace(cfg.AI.LaoZhangAPIKey)
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.AI.LaoZhangImageBaseURL), "/")
	model := strings.TrimSpace(cfg.AI.LaoZhangImageModel)
	if apiKey == "" || baseURL == "" || model == "" {
		return nil, errors.New("report illustration provider is not fully configured")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("report illustration base URL is invalid")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: reportIllustrationRequestTimeout}
	}
	clientCopy := *httpClient
	if clientCopy.Timeout <= 0 || clientCopy.Timeout > reportIllustrationRequestTimeout {
		clientCopy.Timeout = reportIllustrationRequestTimeout
	}
	previousRedirectCheck := clientCopy.CheckRedirect
	clientCopy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[0].Method == http.MethodPost {
			return errors.New("paid report illustration request must not follow redirects")
		}
		if req.URL.Scheme != "https" {
			return errors.New("image download redirect must use HTTPS")
		}
		if req.URL.User != nil {
			return errors.New("image download redirect must not contain user information")
		}
		if previousRedirectCheck != nil {
			return previousRedirectCheck(req, via)
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}
	return &ReportIllustrationClient{
		endpoint: baseURL + "/v1/images/generations",
		apiKey:   apiKey,
		model:    model,
		http:     &clientCopy,
	}, nil
}

func (c *ReportIllustrationClient) Generate(ctx context.Context, input ReportIllustrationRequest) (*ReportIllustrationResult, error) {
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return nil, errors.New("report illustration prompt is required")
	}

	var referenceURI string
	if len(input.ReferenceData) > 0 {
		if len(input.ReferenceData) > reportIllustrationImageMaxSize {
			return nil, errors.New("local reference image exceeds size limit")
		}
		mimeType, _, _, err := inspectReportIllustration(input.ReferenceData, false)
		if err != nil {
			return nil, fmt.Errorf("invalid local reference image: %w", err)
		}
		referenceURI = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(input.ReferenceData)
	}

	apiConfig := &AIAPIConfigData{
		TaskType:       "ai_draw",
		ProviderCode:   "gpt-image-2-vip",
		ProviderName:   "GPT-Image-2 VIP",
		ProtocolType:   "openai_image_sync",
		APIEndpoint:    c.endpoint,
		Method:         http.MethodPost,
		APIKey:         c.apiKey,
		APIKeyLocation: "header_bearer",
		APIKeyName:     "Authorization",
		Headers:        `{"Content-Type":"application/json"}`,
		BodyTemplate:   getLaoZhangGPTImage2VipDrawBodyTemplate(c.model, referenceURI != ""),
	}
	task := &AITaskContext{
		TaskType:  "ai_draw",
		APIConfig: apiConfig,
		Payload: map[string]interface{}{
			"aspect_ratio": "16:9",
			"image_size":   "2K",
		},
		Prompt:   prompt,
		ImageURL: referenceURI,
	}
	body, err := (&RequestPool{}).buildRequestBody(task)
	if err != nil {
		return nil, errors.New("cannot build report illustration request")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, errors.New("cannot encode report illustration request")
	}

	requestCtx, cancel := context.WithTimeout(ctx, reportIllustrationRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("cannot create report illustration request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("report illustration request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("report illustration provider returned HTTP %d", resp.StatusCode)
	}
	responseBody, err := readBounded(resp.Body, reportIllustrationResponseMaxSize)
	if err != nil {
		return nil, errors.New("report illustration response exceeds size limit")
	}
	var decoded reportIllustrationResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, errors.New("report illustration provider returned invalid JSON")
	}
	if len(decoded.Data) == 0 {
		return nil, errors.New("report illustration provider returned no image")
	}

	var imageData []byte
	if encoded := strings.TrimSpace(decoded.Data[0].Base64JSON); encoded != "" {
		if base64.StdEncoding.DecodedLen(len(encoded)) > reportIllustrationImageMaxSize {
			return nil, errors.New("report illustration image exceeds size limit")
		}
		imageData, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, errors.New("report illustration provider returned invalid image data")
		}
	} else if rawURL := strings.TrimSpace(decoded.Data[0].URL); rawURL != "" {
		imageData, err = c.downloadImage(requestCtx, rawURL)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("report illustration provider returned no usable image")
	}

	mimeType, width, height, err := inspectReportIllustration(imageData, true)
	if err != nil {
		return nil, err
	}
	return &ReportIllustrationResult{
		Data: imageData, Provider: ReportIllustrationProvider, Model: c.model,
		MIMEType: mimeType, Width: width, Height: height,
	}, nil
}

type reportIllustrationResponse struct {
	Data []struct {
		Base64JSON string `json:"b64_json"`
		URL        string `json:"url"`
	} `json:"data"`
}

func (c *ReportIllustrationClient) downloadImage(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("report illustration download URL must be HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, errors.New("cannot create report illustration download request")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("report illustration download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("report illustration download returned HTTP %d", resp.StatusCode)
	}
	data, err := readBounded(resp.Body, reportIllustrationImageMaxSize)
	if err != nil {
		return nil, errors.New("report illustration download exceeds size limit")
	}
	return data, nil
}

func inspectReportIllustration(data []byte, require2K bool) (string, int, int, error) {
	if len(data) == 0 {
		return "", 0, 0, errors.New("image is empty")
	}
	mimeType := http.DetectContentType(data)
	if mimeType != "image/png" && mimeType != "image/jpeg" {
		return "", 0, 0, errors.New("image must be valid PNG or JPEG")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return "", 0, 0, errors.New("image must be valid PNG or JPEG")
	}
	if format != "png" && format != "jpeg" {
		return "", 0, 0, errors.New("image must be valid PNG or JPEG")
	}
	if int64(cfg.Width)*int64(cfg.Height) > reportIllustrationMaxPixels {
		return "", 0, 0, errors.New("image pixel count exceeds limit")
	}
	if require2K && (cfg.Width != ReportIllustrationWidth || cfg.Height != ReportIllustrationHeight) {
		return "", 0, 0, fmt.Errorf("image must be exactly %dx%d pixels", ReportIllustrationWidth, ReportIllustrationHeight)
	}
	if _, decodedFormat, err := image.Decode(bytes.NewReader(data)); err != nil || decodedFormat != format {
		return "", 0, 0, errors.New("image must be valid PNG or JPEG")
	}
	return mimeType, cfg.Width, cfg.Height, nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("size limit exceeded")
	}
	return data, nil
}
