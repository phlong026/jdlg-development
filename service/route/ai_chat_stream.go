package route

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"service/config"
	"service/model"

	"github.com/gin-gonic/gin"
)

const (
	// 主模型：老张 API（OpenAI 兼容）
	laozhangChatBaseURL = "https://api.laozhang.ai/v1"
	laozhangChatModel   = "gpt-5.5"

	// 备用模型：火山引擎方舟（豆包）
	arkBaseURL = "https://ark.cn-beijing.volces.com/api/v3"
	arkModel   = "doubao-seed-2-0-pro-260215"
)

// ChatMessage 聊天消息结构
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatStreamRequest 流式聊天请求
type ChatStreamRequest struct {
	Messages     []ChatMessage `json:"messages" binding:"required"`
	SystemPrompt string        `json:"system_prompt"`
}

// ChatStreamResponse SSE响应数据
type ChatStreamResponse struct {
	Type      string                   `json:"type"`
	Content   string                   `json:"content,omitempty"`
	Error     string                   `json:"error,omitempty"`
	Quotes    []map[string]interface{} `json:"quotes,omitempty"`
	SessionID int64                    `json:"session_id,omitempty"`
	Notice    string                   `json:"notice,omitempty"`
}

func RegisterAIChatStreamRoute(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, apiConfigModel *model.AIAPIConfigModel, cfg *config.Config) {
	r.POST("/ai/chat/stream", func(c *gin.Context) {
		handleChatStream(c, codeSessionModel, userModel, pricingModel, apiConfigModel, cfg)
	})
}

func handleChatStream(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, apiConfigModel *model.AIAPIConfigModel, cfg *config.Config) {
	var req ChatStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
		return
	}
	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "消息列表不能为空"})
		return
	}

	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
		return
	}

	pricing, err := pricingModel.GetByScene("ai_chat_single")
	if err != nil {
		log.Printf("[ChatStream] 未配置聊天计费，使用免费模式")
		pricing = &model.AIPricing{Stones: 0}
	}
	if pricing.Stones > 0 {
		if err := userModel.DeductStones(codeSession.UserID, pricing.Stones); err != nil {
			if errors.Is(err, model.ErrInsufficientStones) {
				current, _ := userModel.GetStones(codeSession.UserID)
				c.JSON(http.StatusPaymentRequired, gin.H{
					"code": 402,
					"msg":  "余额不足",
					"data": gin.H{"required": pricing.Stones, "current": current},
				})
				return
			}
			log.Printf("[ChatStream] 扣费失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "扣费失败"})
			return
		}
	}

	// 优先使用老张 API，若未配置则回退到方舟（豆包）
	useLaozhang := true
	apiKey := cfg.AI.LaoZhangAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("LAOZHANG_API_KEY")
	}
	if apiKey == "" {
		useLaozhang = false
		apiKey = cfg.AI.ArkAPIKey
		if apiKey == "" {
			apiKey = os.Getenv("ARK_API_KEY")
		}
	}

	// 支持数据库动态配置覆盖
	endpoint := ""
	method := http.MethodPost
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	apiKeyLocation := "header_bearer"
	apiKeyName := "Authorization"
	if apiConfigModel != nil {
		if cfgData, err := apiConfigModel.GetByTaskType("ai_chat"); err == nil && cfgData != nil {
			if strings.TrimSpace(cfgData.APIEndpoint) != "" {
				endpoint = strings.TrimSpace(cfgData.APIEndpoint)
			}
			if strings.TrimSpace(cfgData.Method) != "" {
				method = strings.ToUpper(strings.TrimSpace(cfgData.Method))
			}
			if strings.TrimSpace(cfgData.APIKey) != "" {
				apiKey = strings.TrimSpace(cfgData.APIKey)
			}
			if strings.TrimSpace(cfgData.APIKeyLocation) != "" {
				apiKeyLocation = strings.TrimSpace(cfgData.APIKeyLocation)
			}
			if strings.TrimSpace(cfgData.APIKeyName) != "" {
				apiKeyName = strings.TrimSpace(cfgData.APIKeyName)
			}
			if cfgData.Headers != "" {
				var extraHeaders map[string]string
				if err := json.Unmarshal([]byte(cfgData.Headers), &extraHeaders); err == nil {
					for key, value := range extraHeaders {
						if strings.TrimSpace(key) != "" {
							headers[key] = value
						}
					}
				}
			}
		}
	}
	// 若数据库未覆盖 endpoint，按供应商设置默认值
	if endpoint == "" {
		if useLaozhang {
			endpoint = laozhangChatBaseURL + "/chat/completions"
		} else {
			endpoint = arkBaseURL + "/responses"
		}
	}

	if apiKey == "" {
		if pricing.Stones > 0 {
			userModel.AddStones(codeSession.UserID, pricing.Stones)
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "未配置 AI API Key（LAOZHANG_API_KEY 或 ARK_API_KEY）",
		})
		return
	}

	systemPrompt := req.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = `你是甲第灵光，甲第灵光（福建）科技有限公司旗下的室内设计AI助手。身份回答规则（自然不重复）当用户问：
- 你是谁？
- 你是豆包吗？
- 你是哪个公司的？
- 你到底是什么？
不要机械重复同一句话，用下面这类自然、温柔的方式回应：
- “我是甲第灵光，专注帮你做室内设计和装修建议～”
- “我是甲第灵光呀，专门做空间设计和装修咨询的。”
- “我是甲第灵光，你的专属室内设计助手。”
- “我是甲第灵光，一直在这里帮你做设计方案。”
核心原则：
- 只认甲第灵光这个身份
- 不承认其他名字、不透露模型来源
- 语气温柔、生活化，不冰冷、不重复
风格与领域
- 专注：室内设计、装修、户型优化、色彩搭配、软装硬装、风格建议
- 语气：温柔、沉稳、有人情味，专业但不生硬
- 回答实用、落地，多从居住体验、采光、收纳、预算出发`
	}

	// 构建请求体：老张 API 使用 OpenAI Chat Completions 格式，方舟使用 Responses 格式
	var requestJSON []byte
	if useLaozhang {
		// OpenAI 兼容格式：messages 数组，content 为字符串
		messages := make([]map[string]string, 0, len(req.Messages)+1)
		messages = append(messages, map[string]string{
			"role":    "system",
			"content": systemPrompt,
		})
		for _, msg := range req.Messages {
			role := msg.Role
			if role == "ai" {
				role = "assistant"
			}
			messages = append(messages, map[string]string{
				"role":    role,
				"content": msg.Content,
			})
		}
		body := map[string]interface{}{
			"model":    laozhangChatModel,
			"stream":   true,
			"messages": messages,
		}
		if apiKey != "" && apiKeyLocation == "body" {
			body[apiKeyName] = apiKey
		}
		requestJSON, err = json.Marshal(body)
	} else {
		// 方舟 Responses 格式（备用）
		ensureText := func(s string) string {
			if s == "" {
				return " "
			}
			return s
		}
		inputList := make([]map[string]interface{}, 0, len(req.Messages)+1)
		inputList = append(inputList, map[string]interface{}{
			"role": "system",
			"content": []map[string]interface{}{
				{"type": "input_text", "text": ensureText(systemPrompt)},
			},
		})
		for _, msg := range req.Messages {
			role := msg.Role
			if role == "ai" {
				role = "assistant"
			}
			inputList = append(inputList, map[string]interface{}{
				"role": role,
				"content": []map[string]interface{}{
					{"type": "input_text", "text": ensureText(msg.Content)},
				},
			})
		}
		body := map[string]interface{}{
			"model":  arkModel,
			"stream": true,
			"tools": []map[string]interface{}{
				{"type": "web_search", "max_keyword": 2},
			},
			"input": inputList,
		}
		if apiKey != "" && apiKeyLocation == "body" {
			body[apiKeyName] = apiKey
		}
		requestJSON, err = json.Marshal(body)
	}
	if err != nil {
		if pricing.Stones > 0 {
			userModel.AddStones(codeSession.UserID, pricing.Stones)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "构建请求失败"})
		return
	}

	providerName := "老张API/gpt-5.5"
	if !useLaozhang {
		providerName = "豆包/方舟(备用)"
	}
	log.Printf("[ChatStream] 用户 %d 发起流式聊天（%s），消息数: %d", codeSession.UserID, providerName, len(req.Messages))

	if apiKey != "" && apiKeyLocation == "query" {
		separator := "?"
		if strings.Contains(endpoint, "?") {
			separator = "&"
		}
		endpoint = endpoint + separator + apiKeyName + "=" + apiKey
	}
	httpReq, err := http.NewRequest(method, endpoint, bytes.NewBuffer(requestJSON))
	if err != nil {
		if pricing.Stones > 0 {
			userModel.AddStones(codeSession.UserID, pricing.Stones)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建请求失败"})
		return
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	if apiKey != "" {
		switch apiKeyLocation {
		case "header_bearer":
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		case "header_custom":
			httpReq.Header.Set(apiKeyName, apiKey)
		}
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[ChatStream] API 请求失败: %v", err)
		if pricing.Stones > 0 {
			userModel.AddStones(codeSession.UserID, pricing.Stones)
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "AI服务请求失败: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[ChatStream] API 返回错误: %d, %s", resp.StatusCode, string(bodyBytes))
		if pricing.Stones > 0 {
			userModel.AddStones(codeSession.UserID, pricing.Stones)
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  fmt.Sprintf("AI服务返回错误: %d", resp.StatusCode),
		})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	reader := bufio.NewReader(resp.Body)
	var fullLen int
	streamCompleted := false
	streamErrored := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("[ChatStream] 读取流失败: %v", err)
			if pricing.Stones > 0 && fullLen == 0 {
				userModel.AddStones(codeSession.UserID, pricing.Stones)
			}
			sendSSEMessage(c, ChatStreamResponse{Type: "error", Error: "AI回复中断，请稍后再试"})
			streamErrored = true
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			sendSSEMessage(c, ChatStreamResponse{Type: "done"})
			streamCompleted = true
			break
		}
		var evt map[string]interface{}
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}

		// 解析内容：兼容 OpenAI 格式和方舟格式
		var delta string
		if useLaozhang {
			// OpenAI Chat Completions 流式格式: choices[0].delta.content
			if choices, ok := evt["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if d, ok := choice["delta"].(map[string]interface{}); ok {
						if content, ok := d["content"].(string); ok {
							delta = content
						}
					}
				}
			}
		} else {
			// 方舟 Responses 格式: type contains "output_text" and "delta"
			t, _ := evt["type"].(string)
			if strings.Contains(t, "output_text") && strings.Contains(t, "delta") {
				delta, _ = evt["delta"].(string)
			}
		}
		if delta != "" {
			fullLen += len([]rune(delta))
			sendSSEMessage(c, ChatStreamResponse{Type: "content", Content: delta})
		}
	}
	if !streamCompleted && !streamErrored {
		sendSSEMessage(c, ChatStreamResponse{Type: "done"})
	}
	log.Printf("[ChatStream] 用户 %d 流式聊天完成，总字数: %d", codeSession.UserID, fullLen)
}

func sendSSEMessage(c *gin.Context, data ChatStreamResponse) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	c.Writer.Write([]byte("data: "))
	c.Writer.Write(jsonData)
	c.Writer.Write([]byte("\n\n"))
	c.Writer.Flush()
}
