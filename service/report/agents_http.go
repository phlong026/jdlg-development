package report

import (
	"bytes"
	"context"
	"encoding/json"
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
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		strings.TrimSpace(config.APIKey) == "" || !validIdentifier(config.ConfigVersion, 64) ||
		config.Timeout <= 0 || config.Timeout > 60*time.Second || config.MaxResponseBytes < 1024 || config.MaxResponseBytes > 1<<20 {
		return nil, ErrInvalidInput
	}
	identity := AgentIdentity{ConfigVersion: config.ConfigVersion, Models: map[string]string{
		"planner": config.PlannerModel, "writer": config.WriterModel,
		"visual": config.VisualModel, "reviewer": config.ReviewerModel,
	}}
	if !validAgentIdentity(identity) {
		return nil, ErrInvalidInput
	}
	if client == nil {
		client = &http.Client{}
	}
	return &HTTPAgentClient{
		endpoint: parsed.String(), apiKey: config.APIKey, timeout: config.Timeout,
		maxResponseBytes: config.MaxResponseBytes, httpClient: client, identity: identity,
	}, nil
}

func (c *HTTPAgentClient) Identity() AgentIdentity {
	return AgentIdentity{ConfigVersion: c.identity.ConfigVersion, Models: cloneStringMap(c.identity.Models)}
}

func (c *HTTPAgentClient) Run(ctx context.Context, request AgentRequest) (json.RawMessage, error) {
	if request.Model == "" || request.Model != c.identity.Models[request.Role] || request.PromptVersion == "" || request.SystemPrompt == "" || len(request.Input) == 0 {
		return nil, ErrAgentUnavailable
	}
	payload := struct {
		Model          string `json:"model"`
		Temperature    int    `json:"temperature"`
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{Model: request.Model, Temperature: 0}
	payload.ResponseFormat.Type = "json_object"
	payload.Messages = append(payload.Messages,
		struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "system", Content: request.SystemPrompt},
		struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{Role: "user", Content: string(request.Input)},
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
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if strictDecode(responseBody, &envelope) != nil || len(envelope.Choices) != 1 || len(envelope.Choices[0].Message.Content) == 0 {
		return nil, ErrAgentUnavailable
	}
	return json.RawMessage(envelope.Choices[0].Message.Content), nil
}
