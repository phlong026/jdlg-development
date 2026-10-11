package component

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"service/config"
)

const (
	fastGPTImportPath = "/api/core/dataset/collection/create/localFile"
	fastGPTChatPath   = "/api/v1/chat/completions"
)

type FastGPTClient struct {
	baseURL   string
	apiKey    string
	datasetID string
	chatIDPre string
	http      *http.Client
}

type FastGPTImportResult struct {
	CollectionID string          `json:"collection_id"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

type FastGPTChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type FastGPTChatResult struct {
	Content string          `json:"content"`
	Raw     json.RawMessage `json:"raw,omitempty"`
}

func NewFastGPTClient(ai config.AIConfig) *FastGPTClient {
	return &FastGPTClient{
		baseURL:   strings.TrimRight(strings.TrimSpace(ai.FastGPTBaseURL), "/"),
		apiKey:    strings.TrimSpace(ai.FastGPTAPIKey),
		datasetID: strings.TrimSpace(ai.FastGPTDatasetID),
		chatIDPre: strings.TrimSpace(ai.FastGPTChatIDPrefix),
		http: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (c *FastGPTClient) IsConfiguredForImport() bool {
	return c != nil && c.baseURL != "" && c.apiKey != "" && c.datasetID != ""
}

func (c *FastGPTClient) IsConfiguredForChat() bool {
	return c != nil && c.baseURL != "" && c.apiKey != ""
}

func (c *FastGPTClient) ImportLocalFile(ctx context.Context, fileName string, data []byte, metadata map[string]string) (*FastGPTImportResult, error) {
	if !c.IsConfiguredForImport() {
		return nil, fmt.Errorf("FastGPT 导入配置不完整")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"datasetId":     c.datasetID,
		"parentId":      nil,
		"trainingType":  "chunk",
		"chunkSize":     800,
		"chunkSplitter": "",
		"metadata":      metadata,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if err := writer.WriteField("data", string(payloadBytes)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+fastGPTImportPath, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	raw, err := c.do(req)
	if err != nil {
		return nil, err
	}
	return &FastGPTImportResult{CollectionID: parseFastGPTID(raw), Raw: raw}, nil
}

func (c *FastGPTClient) Chat(ctx context.Context, userKey string, messages []FastGPTChatMessage) (*FastGPTChatResult, error) {
	if !c.IsConfiguredForChat() {
		return nil, fmt.Errorf("FastGPT 聊天配置不完整")
	}
	chatID := strings.TrimSpace(userKey)
	if c.chatIDPre != "" {
		chatID = c.chatIDPre + "-" + chatID
	}
	payload := map[string]interface{}{
		"chatId":   chatID,
		"stream":   false,
		"detail":   true,
		"messages": messages,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+fastGPTChatPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	raw, err := c.do(req)
	if err != nil {
		return nil, err
	}
	content := ParseFastGPTChatContent(raw)
	if content == "" {
		return nil, fmt.Errorf("FastGPT 返回内容为空")
	}
	return &FastGPTChatResult{Content: content, Raw: raw}, nil
}

// ChatStream 以流式方式调用 FastGPT 聊天接口。
// 每收到一条 SSE data 事件（非 [DONE]），onEvent 会被回调一次，参数为事件的原始 JSON 字节。
// 回调内返回 error 会立即中止拉流并向上抛出。
func (c *FastGPTClient) ChatStream(ctx context.Context, userKey string, messages []FastGPTChatMessage, onEvent func(raw []byte) error) error {
	if !c.IsConfiguredForChat() {
		return fmt.Errorf("FastGPT 聊天配置不完整")
	}
	if onEvent == nil {
		return fmt.Errorf("onEvent 回调不能为空")
	}
	chatID := strings.TrimSpace(userKey)
	if c.chatIDPre != "" {
		chatID = c.chatIDPre + "-" + chatID
	}
	payload := map[string]interface{}{
		"chatId":   chatID,
		"stream":   true,
		"detail":   true,
		"messages": messages,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+fastGPTChatPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("FastGPT 流式请求失败: status=%d body=%s", resp.StatusCode, string(errBody))
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return nil
		}
		if err := onEvent([]byte(data)); err != nil {
			return err
		}
	}
}

func (c *FastGPTClient) do(req *http.Request) (json.RawMessage, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("FastGPT 请求失败: status=%d body=%s", resp.StatusCode, string(body))
	}
	if err := checkFastGPTBusinessCode(body); err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func checkFastGPTBusinessCode(raw []byte) error {
	var root struct {
		Code interface{} `json:"code"`
		Msg  string      `json:"msg"`
	}
	if json.Unmarshal(raw, &root) != nil || root.Code == nil {
		return nil
	}
	switch code := root.Code.(type) {
	case float64:
		if code == 0 || code == 200 {
			return nil
		}
	case string:
		if code == "0" || code == "200" || code == "success" {
			return nil
		}
	}
	if root.Msg == "" {
		root.Msg = string(raw)
	}
	return fmt.Errorf("FastGPT 业务返回失败: %s", root.Msg)
}

func parseFastGPTID(raw []byte) string {
	var root map[string]interface{}
	if json.Unmarshal(raw, &root) != nil {
		return ""
	}
	for _, key := range []string{"id", "collectionId", "_id"} {
		if v, ok := root[key].(string); ok && v != "" {
			return v
		}
	}
	if data, ok := root["data"].(map[string]interface{}); ok {
		for _, key := range []string{"id", "collectionId", "_id"} {
			if v, ok := data[key].(string); ok && v != "" {
				return v
			}
		}
	}
	return ""
}

func ParseFastGPTChatContent(raw []byte) string {
	var root struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
		ResponseData []struct {
			QuoteList []interface{} `json:"quoteList"`
		} `json:"responseData"`
	}
	if json.Unmarshal(raw, &root) != nil {
		return ""
	}
	if len(root.Choices) > 0 {
		if content := strings.TrimSpace(root.Choices[0].Message.Content); content != "" {
			return content
		}
		return strings.TrimSpace(root.Choices[0].Delta.Content)
	}
	return ""
}

// ParseFastGPTQuotes 从 FastGPT 事件载荷中提取知识库引用列表。
// 兼容以下三种载荷形态：
//  1. 对象且含 responseData 数组：{"responseData":[{"quoteList":[...]}]}
//  2. 对象且直接含 quoteList：    {"quoteList":[...]}
//  3. 数组形式的 responseData：   [{"quoteList":[...]}]
//
// 引用项保持原始字段透传，由前端按需渲染（sourceName / q / a / chunkIndex 等）。
// 若未检出引用或解析失败，返回 nil。
func ParseFastGPTQuotes(raw []byte) []map[string]interface{} {
	data := bytes.TrimSpace(raw)
	if len(data) == 0 {
		return nil
	}
	if data[0] == '[' {
		var arr []struct {
			QuoteList []map[string]interface{} `json:"quoteList"`
		}
		if json.Unmarshal(data, &arr) == nil {
			for _, item := range arr {
				if len(item.QuoteList) > 0 {
					return item.QuoteList
				}
			}
		}
		return nil
	}
	var root struct {
		QuoteList    []map[string]interface{} `json:"quoteList"`
		ResponseData []struct {
			QuoteList []map[string]interface{} `json:"quoteList"`
		} `json:"responseData"`
	}
	if json.Unmarshal(data, &root) != nil {
		return nil
	}
	if len(root.QuoteList) > 0 {
		return root.QuoteList
	}
	for _, item := range root.ResponseData {
		if len(item.QuoteList) > 0 {
			return item.QuoteList
		}
	}
	return nil
}
