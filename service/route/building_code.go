package route

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"service/component"
	"service/config"
	"service/function"
	"service/model"
)

const maxBuildingCodeFileSize = 50 << 20

type buildingCodeChatRequest struct {
	Messages       []component.FastGPTChatMessage `json:"messages"`
	Question       string                         `json:"question"`
	KnowledgeScope string                         `json:"knowledge_scope"`
	SessionID      int64                          `json:"session_id"`
}

func RegisterBuildingCodeRoutes(r *gin.RouterGroup, documentModel *model.BuildingCodeDocumentModel, chatModel *model.BuildingCodeChatModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, cfg *config.Config) {
	r.GET("/building-code-documents", func(c *gin.Context) {
		handleListBuildingCodeDocuments(c, documentModel)
	})
	r.POST("/building-code-documents/upload", func(c *gin.Context) {
		handleUploadBuildingCodeDocument(c, documentModel, cfg)
	})
	r.DELETE("/building-code-documents/:id", func(c *gin.Context) {
		handleDisableBuildingCodeDocument(c, documentModel, true)
	})
	r.POST("/ai/building-code/chat", func(c *gin.Context) {
		handleBuildingCodeChat(c, chatModel, userModel, pricingModel, cfg)
	})
}

func RegisterBuildingCodeManagementRoutes(r *gin.RouterGroup, documentModel *model.BuildingCodeDocumentModel, cfg *config.Config) {
	group := r.Group("/building-code-documents")
	group.GET("", func(c *gin.Context) {
		limit, offset := parseLimitOffset(c)
		docs, err := documentModel.ListAll(limit, offset, c.Query("scope"), c.Query("review_status"), c.Query("index_status"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询规范资料失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{"list": docs}})
	})
	group.POST("/:id/approve", func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		if err := documentModel.Approve(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "审核通过失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
	})
	group.POST("/:id/reject", func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		_ = c.ShouldBindJSON(&req)
		if err := documentModel.Reject(id, req.Reason); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "审核拒绝失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
	})
	group.POST("/:id/disable", func(c *gin.Context) {
		handleDisableBuildingCodeDocument(c, documentModel, false)
	})
	group.POST("/:id/reindex", func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		doc, err := documentModel.GetByID(id)
		if err != nil {
			status := http.StatusInternalServerError
			msg := "读取资料失败"
			if err == sql.ErrNoRows {
				status = http.StatusNotFound
				msg = "资料不存在"
			}
			c.JSON(status, gin.H{"code": status, "msg": msg})
			return
		}
		go importBuildingCodeToFastGPT(context.Background(), documentModel, doc, nil, cfg)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已重新发起索引", "data": doc})
	})
}

func handleListBuildingCodeDocuments(c *gin.Context, documentModel *model.BuildingCodeDocumentModel) {
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
		return
	}
	limit, offset := parseLimitOffset(c)
	docs, err := documentModel.ListByOwner(codeSession.UserID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询规范资料失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{"list": docs}})
}

func handleUploadBuildingCodeDocument(c *gin.Context, documentModel *model.BuildingCodeDocumentModel, cfg *config.Config) {
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请选择要上传的规范文件"})
		return
	}
	if fileHeader.Size <= 0 || fileHeader.Size > maxBuildingCodeFileSize {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "文件大小需在 50MB 以内"})
		return
	}
	fileType, contentType, err := normalizeBuildingCodeFile(fileHeader.Filename, fileHeader.Header.Get("Content-Type"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "读取文件失败"})
		return
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, maxBuildingCodeFileSize+1))
	if err != nil || int64(len(data)) > maxBuildingCodeFileSize {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "读取文件失败或文件过大"})
		return
	}

	objectKey := buildBuildingCodeObjectKey(codeSession.UserID, fileHeader.Filename)
	fileURL, err := function.UploadBytes(c.Request.Context(), nil, cfg, objectKey, data, contentType)
	if err != nil {
		log.Printf("[BuildingCode] 上传 COS 失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "上传文件到云存储失败"})
		return
	}
	title := strings.TrimSpace(c.PostForm("title"))
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(fileHeader.Filename), filepath.Ext(fileHeader.Filename))
	}
	doc := &model.BuildingCodeDocument{
		OwnerUserID:  codeSession.UserID,
		Title:        title,
		FileName:     filepath.Base(fileHeader.Filename),
		FileType:     fileType,
		FileSize:     int64(len(data)),
		ContentType:  contentType,
		COSKey:       objectKey,
		FileURL:      fileURL,
		Scope:        model.BuildingCodeScopePublic,
		ReviewStatus: model.BuildingCodeReviewApproved,
		IndexStatus:  model.BuildingCodeIndexUploaded,
		IsEnabled:    true,
	}
	if cfg != nil {
		doc.FastGPTDatasetID = cfg.AI.FastGPTDatasetID
	}
	if err := documentModel.Create(doc); err != nil {
		log.Printf("[BuildingCode] 创建资料记录失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存资料记录失败"})
		return
	}

	// 异步索引：避免大文件 embedding 阻塞用户请求
	// 使用 context.Background() 防止请求返回后 context 被取消导致索引中断
	doc.IndexStatus = model.BuildingCodeIndexIndexing
	go importBuildingCodeToFastGPT(context.Background(), documentModel, doc, data, cfg)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": doc})
}

func handleBuildingCodeChat(c *gin.Context, chatModel *model.BuildingCodeChatModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, cfg *config.Config) {
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
		return
	}
	var req buildingCodeChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
		return
	}
	messages := normalizeFastGPTMessages(req)
	if len(messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请输入要咨询的规范问题"})
		return
	}

	pricing, err := pricingModel.GetByScene("building_code_chat")
	if err != nil {
		pricing = &model.AIPricing{Stones: 0}
	}
	if pricing.Stones > 0 {
		if err := userModel.DeductStones(codeSession.UserID, pricing.Stones); err != nil {
			if errors.Is(err, model.ErrInsufficientStones) {
				current, _ := userModel.GetStones(codeSession.UserID)
				c.JSON(http.StatusPaymentRequired, gin.H{"code": 402, "msg": "余额不足", "data": gin.H{"required": pricing.Stones, "current": current}})
				return
			}
			log.Printf("[BuildingCode] 扣费失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "扣费失败"})
			return
		}
	}

	// 会话管理：前端传 session_id 则复用（校验归属），否则新建
	sessionID := req.SessionID
	if sessionID > 0 {
		ok, err := chatModel.SessionBelongsToUser(sessionID, codeSession.UserID)
		if err != nil || !ok {
			sessionID = 0
		}
	}
	latestUserQuestion := lastUserMessageContent(messages)
	if sessionID == 0 {
		newID, err := chatModel.CreateSession(codeSession.UserID, latestUserQuestion)
		if err != nil {
			log.Printf("[BuildingCode] 创建会话失败: %v", err)
		} else {
			sessionID = newID
		}
	}
	if sessionID > 0 && latestUserQuestion != "" {
		if err := chatModel.AppendMessage(sessionID, "user", latestUserQuestion, "", "ok"); err != nil {
			log.Printf("[BuildingCode] 保存用户消息失败: %v", err)
		}
	}

	client := component.NewFastGPTClient(cfg.AI)

	// 审计日志：最后无论成功/失败/兜底，都输出一行结构化统计
	startedAt := time.Now()
	var (
		totalLen      int
		answerBuilder strings.Builder
		lastQuotes    []map[string]interface{}
		usedFallback  bool
		finalStatus   = "ok"
	)
	defer func() {
		log.Printf("[BuildingCode][Audit] user=%d session=%d status=%s fallback=%t quotes=%d question_runes=%d answer_runes=%d elapsed_ms=%d",
			codeSession.UserID, sessionID, finalStatus, usedFallback, len(lastQuotes),
			len([]rune(latestUserQuestion)), totalLen, time.Since(startedAt).Milliseconds(),
		)
	}()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	if sessionID > 0 {
		sendSSEMessage(c, ChatStreamResponse{Type: "session", SessionID: sessionID})
	}

	streamErr := client.ChatStream(c.Request.Context(), fmt.Sprintf("%d", codeSession.UserID), messages, func(raw []byte) error {
		if delta := component.ParseFastGPTChatContent(raw); delta != "" {
			totalLen += len([]rune(delta))
			answerBuilder.WriteString(delta)
			sendSSEMessage(c, ChatStreamResponse{Type: "content", Content: delta})
			return nil
		}
		if quotes := component.ParseFastGPTQuotes(raw); len(quotes) > 0 {
			lastQuotes = quotes
			sendSSEMessage(c, ChatStreamResponse{Type: "quotes", Quotes: quotes})
		}
		return nil
	})

	// FastGPT 完全失败（无任何输出）时尝试兜底：用通用大模型生成，但明确提示风险
	if streamErr != nil && totalLen == 0 {
		log.Printf("[BuildingCode] FastGPT 失败，尝试通用大模型兜底: %v", streamErr)
		sendSSEMessage(c, ChatStreamResponse{Type: "notice", Notice: "本次回答未检索到建筑规范原文，由通用AI生成，内容仅供参考，请以正式规范为准。"})
		fallbackContent, fbErr := streamBuildingCodeFallback(c, messages, cfg, func(delta string) {
			totalLen += len([]rune(delta))
			answerBuilder.WriteString(delta)
			sendSSEMessage(c, ChatStreamResponse{Type: "content", Content: delta})
		})
		if fbErr == nil && fallbackContent != "" {
			usedFallback = true
			streamErr = nil
		} else if fbErr != nil {
			log.Printf("[BuildingCode] 通用大模型兜底失败: %v", fbErr)
		}
	}

	// 记录 assistant 回复（成功或部分成功都保留；完全失败则记 error 状态且 content 为空）
	if sessionID > 0 {
		quotesJSON := ""
		if len(lastQuotes) > 0 {
			if b, mErr := json.Marshal(lastQuotes); mErr == nil {
				quotesJSON = string(b)
			}
		}
		if streamErr != nil {
			finalStatus = "error"
		} else if usedFallback {
			finalStatus = "fallback"
		}
		if err := chatModel.AppendMessage(sessionID, "assistant", answerBuilder.String(), quotesJSON, finalStatus); err != nil {
			log.Printf("[BuildingCode] 保存AI消息失败: %v", err)
		}
		_ = chatModel.TouchSession(sessionID)
	} else if streamErr != nil {
		finalStatus = "error"
	} else if usedFallback {
		finalStatus = "fallback"
	}

	if streamErr != nil {
		log.Printf("[BuildingCode] 建规快查彻底失败: %v (已输出字数=%d)", streamErr, totalLen)
		if totalLen == 0 && pricing.Stones > 0 {
			_ = userModel.AddStones(codeSession.UserID, pricing.Stones)
		}
		sendSSEMessage(c, ChatStreamResponse{Type: "error", Error: "建规快查暂时不可用，请稍后再试"})
		return
	}
	sendSSEMessage(c, ChatStreamResponse{Type: "done"})
}

// lastUserMessageContent 返回消息序列中最后一条 user 消息的内容，供生成会话标题用
func lastUserMessageContent(messages []component.FastGPTChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func importBuildingCodeToFastGPT(ctx context.Context, documentModel *model.BuildingCodeDocumentModel, doc *model.BuildingCodeDocument, data []byte, cfg *config.Config) {
	if doc == nil || cfg == nil {
		return
	}
	client := component.NewFastGPTClient(cfg.AI)
	if !client.IsConfiguredForImport() {
		_ = documentModel.MarkIndexFailed(doc.ID, "FastGPT 导入配置不完整")
		doc.IndexStatus = model.BuildingCodeIndexFailed
		doc.ErrorMessage = "FastGPT 导入配置不完整"
		return
	}
	if data == nil {
		if doc.FileURL == "" {
			_ = documentModel.MarkIndexFailed(doc.ID, "缺少可重新索引的文件地址")
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, doc.FileURL, nil)
		if err != nil {
			_ = documentModel.MarkIndexFailed(doc.ID, err.Error())
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			_ = documentModel.MarkIndexFailed(doc.ID, err.Error())
			return
		}
		defer resp.Body.Close()
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxBuildingCodeFileSize+1))
		if err != nil || int64(len(data)) > maxBuildingCodeFileSize {
			_ = documentModel.MarkIndexFailed(doc.ID, "重新读取文件失败或文件过大")
			return
		}
	}
	_ = documentModel.MarkIndexing(doc.ID, cfg.AI.FastGPTDatasetID)
	doc.IndexStatus = model.BuildingCodeIndexIndexing
	metadata := map[string]string{
		"document_id":   strconv.FormatInt(doc.ID, 10),
		"owner_user_id": strconv.FormatInt(doc.OwnerUserID, 10),
		"scope":         doc.Scope,
		"review_status": doc.ReviewStatus,
	}

	// 最多尝试 4 次（1 次主调 + 3 次重试），指数退避 2s / 5s / 15s
	backoffs := []time.Duration{0, 2 * time.Second, 5 * time.Second, 15 * time.Second}
	var result *component.FastGPTImportResult
	var lastErr error
	for attempt, wait := range backoffs {
		if wait > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(wait):
			}
			if ctx.Err() != nil {
				lastErr = ctx.Err()
				break
			}
			log.Printf("[BuildingCode] 文档 %d 第 %d 次重试 FastGPT 导入（上一次错误：%v）", doc.ID, attempt, lastErr)
		}
		result, lastErr = client.ImportLocalFile(ctx, doc.FileName, data, metadata)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		_ = documentModel.MarkIndexFailed(doc.ID, lastErr.Error())
		doc.IndexStatus = model.BuildingCodeIndexFailed
		doc.ErrorMessage = lastErr.Error()
		return
	}
	_ = documentModel.MarkIndexed(doc.ID, result.CollectionID)
	doc.IndexStatus = model.BuildingCodeIndexReady
	doc.FastGPTCollectionID = result.CollectionID
}

func normalizeBuildingCodeFile(fileName, contentType string) (string, string, error) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	switch ext {
	case "pdf":
		return "pdf", "application/pdf", nil
	case "docx":
		return "docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", nil
	case "txt":
		return "txt", "text/plain", nil
	case "md":
		return "md", "text/markdown", nil
	default:
		return "", "", fmt.Errorf("仅支持 PDF、DOCX、TXT、MD 格式")
	}
}

func buildBuildingCodeObjectKey(userID int64, fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		ext = ".dat"
	}
	return fmt.Sprintf("building_codes/%d/%s/%s%s", userID, time.Now().Format("20060102"), uuid.NewString(), ext)
}

func normalizeFastGPTMessages(req buildingCodeChatRequest) []component.FastGPTChatMessage {
	messages := make([]component.FastGPTChatMessage, 0, len(req.Messages)+1)
	for _, msg := range req.Messages {
		role := strings.TrimSpace(msg.Role)
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if role == "ai" {
			role = "assistant"
		}
		if role != "system" && role != "assistant" && role != "user" {
			role = "user"
		}
		messages = append(messages, component.FastGPTChatMessage{Role: role, Content: content})
	}
	if len(messages) == 0 && strings.TrimSpace(req.Question) != "" {
		messages = append(messages, component.FastGPTChatMessage{Role: "user", Content: strings.TrimSpace(req.Question)})
	}
	if len(messages) > 12 {
		messages = messages[len(messages)-12:]
	}
	return messages
}

func parseLimitOffset(c *gin.Context) (int, int) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func parseIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的资料ID"})
		return 0, false
	}
	return id, true
}

func handleDisableBuildingCodeDocument(c *gin.Context, documentModel *model.BuildingCodeDocumentModel, ownerOnly bool) {
	id, ok := parseIDParam(c)
	if !ok {
		return
	}
	if ownerOnly {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		doc, err := documentModel.GetByID(id)
		if err != nil || doc.OwnerUserID != codeSession.UserID {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "资料不存在"})
			return
		}
	}
	if err := documentModel.Disable(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除资料失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

// streamBuildingCodeFallback 在 FastGPT 不可用时，用老张 API 的通用大模型兜底。
// onDelta 每到达一小段增量文本调用一次（由调用方负责 SSE 推送和累积）。
// 返回完整回复文本和错误；若老张 API Key 未配置或请求失败，返回 error。
func streamBuildingCodeFallback(c *gin.Context, messages []component.FastGPTChatMessage, cfg *config.Config, onDelta func(string)) (string, error) {
	apiKey := strings.TrimSpace(cfg.AI.LaoZhangAPIKey)
	if apiKey == "" {
		return "", fmt.Errorf("通用大模型 API Key 未配置")
	}

	systemPrompt := "你是建筑规范咨询助手。现在知识库暂不可用，请基于你的通用知识回答，表达谨慎，不要编造具体规范条文号。若不确定请明确说明，并建议用户核对正式规范。"
	openaiMessages := make([]map[string]string, 0, len(messages)+1)
	openaiMessages = append(openaiMessages, map[string]string{"role": "system", "content": systemPrompt})
	for _, m := range messages {
		openaiMessages = append(openaiMessages, map[string]string{"role": m.Role, "content": m.Content})
	}
	body := map[string]interface{}{
		"model":    "gpt-5.5",
		"stream":   true,
		"messages": openaiMessages,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://api.laozhang.ai/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")

	httpClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("兜底大模型请求失败: status=%d body=%s", resp.StatusCode, string(errBody))
	}

	reader := bufio.NewReader(resp.Body)
	var builder strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return builder.String(), err
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				break
			}
			continue
		}
		var evt map[string]interface{}
		if json.Unmarshal([]byte(data), &evt) != nil {
			continue
		}
		choices, _ := evt["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]interface{})
		if choice == nil {
			continue
		}
		delta, _ := choice["delta"].(map[string]interface{})
		if delta == nil {
			continue
		}
		if content, _ := delta["content"].(string); content != "" {
			builder.WriteString(content)
			onDelta(content)
		}
	}
	if builder.Len() == 0 {
		return "", fmt.Errorf("兜底大模型未返回任何内容")
	}
	return builder.String(), nil
}
