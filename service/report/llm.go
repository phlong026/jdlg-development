package report

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

var errTextUnavailable = errors.New("report: text service unavailable")

type HTTPTextClient struct {
	endpoint string
	apiKey   string
	model    string
	timeout  time.Duration
	client   *http.Client
}

func NewHTTPTextClient(endpoint, apiKey, model string, timeout time.Duration, client *http.Client) (*HTTPTextClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(model) == "" || timeout <= 0 || timeout > 30*time.Second {
		return nil, ErrInvalidInput
	}
	if client == nil {
		client = &http.Client{}
	}
	return &HTTPTextClient{endpoint: parsed.String(), apiKey: apiKey, model: model, timeout: timeout, client: client}, nil
}

func (c *HTTPTextClient) Generate(ctx context.Context, request TextRequest) (TextResult, error) {
	payload := struct {
		Model       string `json:"model"`
		Temperature int    `json:"temperature"`
		Messages    []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{Model: c.model, Temperature: 0}
	payload.Messages = append(payload.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		Role:    "system",
		Content: "你只能解释已给建房事实，不得新增或改写数字、地区、家庭成员、预算和专业结论，不得使用保证性措辞。只返回单行JSON，键为profile_insight、core_tradeoff、recommendation，每项不超过120个字，解释字段中不要写数字。",
	})
	facts, err := json.Marshal(request.Facts)
	if err != nil {
		return TextResult{}, errTextUnavailable
	}
	payload.Messages = append(payload.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: string(facts)})
	body, err := json.Marshal(payload)
	if err != nil {
		return TextResult{}, errTextUnavailable
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return TextResult{}, errTextUnavailable
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(httpRequest)
	if err != nil {
		return TextResult{}, errTextUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return TextResult{}, errTextUnavailable
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 64<<10+1))
	if err != nil || len(responseBody) > 64<<10 {
		return TextResult{}, errTextUnavailable
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(responseBody, &envelope) != nil || len(envelope.Choices) != 1 {
		return TextResult{}, errTextUnavailable
	}
	var result TextResult
	if json.Unmarshal([]byte(envelope.Choices[0].Message.Content), &result) != nil || !validTextResult(result) {
		return TextResult{}, errTextUnavailable
	}
	return result, nil
}
