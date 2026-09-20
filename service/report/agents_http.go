package report

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type MultiAgentHTTPConfig struct {
	Endpoint         string
	APIKey           string
	ConfigVersion    string
	PlannerModel     string
	WriterModel      string
	VisualModel      string
	ReviewerModel    string
	DiagnosisModel   string
	DecisionModel    string
	BudgetModel      string
	EditorModel      string
	SpatialModel     string
	Timeout          time.Duration
	MaxResponseBytes int64
}

type HTTPAgentClient struct {
	endpoint         string
	apiKey           string
	timeout          time.Duration
	maxResponseBytes int64
	httpClient       *http.Client
	identity         AgentIdentity
}

func NewHTTPAgentClient(config MultiAgentHTTPConfig, client *http.Client) (*HTTPAgentClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(config.Endpoint))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		strings.TrimSpace(config.APIKey) == "" || !validIdentifier(config.ConfigVersion, 64) ||
		config.Timeout <= 0 || config.Timeout > 60*time.Second || config.MaxResponseBytes < 1024 || config.MaxResponseBytes > 1<<20 {
		return nil, ErrInvalidInput
	}
	identity := AgentIdentity{ConfigVersion: config.ConfigVersion, Models: map[string]string{
		"planner": config.PlannerModel, "writer": config.WriterModel,
		"visual": config.VisualModel, "reviewer": config.ReviewerModel,
	}}
	for role, models := range map[string][2]string{
		"diagnosis": {config.DiagnosisModel, config.PlannerModel},
		"decision":  {config.DecisionModel, config.PlannerModel},
		"budget":    {config.BudgetModel, config.PlannerModel},
		"editor":    {config.EditorModel, config.WriterModel},
		"spatial":   {config.SpatialModel, config.PlannerModel},
	} {
		model := strings.TrimSpace(models[0])
		if model == "" {
			model = models[1]
		}
		if !validAgentModel(model) {
			return nil, ErrInvalidInput
		}
		identity.Models[role] = model
	}
	if !validAgentIdentity(identity) {
		return nil, ErrInvalidInput
	}
	if client == nil {
		client = &http.Client{}
	}
	// Redirects can move credentials or model input to a different service.
	boundedClient := *client
	boundedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPAgentClient{
		endpoint: parsed.String(), apiKey: config.APIKey, timeout: config.Timeout,
		maxResponseBytes: config.MaxResponseBytes, httpClient: &boundedClient, identity: identity,
	}, nil
}

func (c *HTTPAgentClient) Identity() AgentIdentity {
	return AgentIdentity{ConfigVersion: c.identity.ConfigVersion, Models: cloneStringMap(c.identity.Models)}
}

func (c *HTTPAgentClient) Run(ctx context.Context, request AgentRequest) (json.RawMessage, error) {
	if request.Model == "" || request.Model != c.identity.Models[request.Role] || request.PromptVersion == "" || request.SystemPrompt == "" || len(request.Input) == 0 {
		return nil, ErrAgentUnavailable
	}
	userContent, err := agentUserContent(request)
	if err != nil {
		return nil, err
	}
	payload := struct {
		Model          string `json:"model"`
		Temperature    int    `json:"temperature"`
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}{Model: request.Model, Temperature: 0}
	payload.ResponseFormat.Type = "json_object"
	payload.Messages = append(payload.Messages,
		struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{Role: "system", Content: request.SystemPrompt},
		struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}{Role: "user", Content: userContent},
	)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrAgentUnavailable
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrAgentUnavailable
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, ErrAgentUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ErrAgentUnavailable
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil || int64(len(responseBody)) > c.maxResponseBytes {
		return nil, ErrAgentUnavailable
	}
	var envelope struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	// Provider envelopes contain fields such as id/usage; only the generated
	// stage payload is schema-strict. Truncation and refusal are never success.
	if json.Unmarshal(responseBody, &envelope) != nil || len(envelope.Choices) != 1 || len(envelope.Choices[0].Message.Content) == 0 || envelope.Choices[0].Message.Refusal != "" {
		return nil, ErrAgentUnavailable
	}
	if finish := envelope.Choices[0].FinishReason; finish != "" && finish != "stop" {
		return nil, ErrAgentUnavailable
	}
	return json.RawMessage(envelope.Choices[0].Message.Content), nil
}

func agentUserContent(request AgentRequest) (any, error) {
	if len(request.Images) == 0 {
		return string(request.Input), nil
	}
	if request.Role != "reviewer" || len(request.Images) > 4 {
		return nil, ErrInvalidInput
	}
	parts := []map[string]any{{"type": "text", "text": string(request.Input)}}
	total := 0
	for _, img := range request.Images {
		if len(img.Data) == 0 || len(img.Data) > houseImageMaxBytes {
			return nil, ErrInvalidInput
		}
		info, format, err := image.DecodeConfig(bytes.NewReader(img.Data))
		if err != nil || !oneOf(format, "png", "jpeg") || img.MIMEType != "image/"+format || info.Width < 1 || info.Height < 1 || info.Width > 8192 || info.Height > 8192 || int64(info.Width)*int64(info.Height) > 16_000_000 {
			return nil, ErrInvalidInput
		}
		data, mime := img.Data, img.MIMEType
		if len(data) > 10<<20 {
			// Strip oversized metadata and normalize the vision-only copy to
			// eight-bit pixels. The original 2K report file and hash stay intact.
			source, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return nil, ErrInvalidInput
			}
			pixels := image.NewNRGBA(image.Rect(0, 0, info.Width, info.Height))
			draw.Draw(pixels, pixels.Bounds(), source, source.Bounds().Min, draw.Src)
			var compact bytes.Buffer
			if err := png.Encode(&compact, pixels); err != nil {
				return nil, ErrInvalidInput
			}
			data, mime = compact.Bytes(), "image/png"
		}
		total += len(data)
		if len(data) > 10<<20 || total > 24<<20 {
			return nil, ErrInvalidInput
		}
		parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), "detail": "high"}})
	}
	return parts, nil
}
