package route

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"regexp"
	"service/component"
	"service/config"
	"service/function"
	"service/model"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "golang.org/x/image/webp"
)

func getStringFromPayload(payload map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch current := value.(type) {
		case string:
			if strings.TrimSpace(current) != "" {
				return strings.TrimSpace(current)
			}
		case json.Number:
			if strings.TrimSpace(current.String()) != "" {
				return strings.TrimSpace(current.String())
			}
		case float64:
			return strconv.FormatInt(int64(current), 10)
		case float32:
			return strconv.FormatInt(int64(current), 10)
		case int:
			return strconv.Itoa(current)
		case int32:
			return strconv.FormatInt(int64(current), 10)
		case int64:
			return strconv.FormatInt(current, 10)
		}
	}
	return ""
}

func getInt64FromPayload(payload map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch current := value.(type) {
		case float64:
			return int64(current)
		case float32:
			return int64(current)
		case int:
			return int64(current)
		case int32:
			return int64(current)
		case int64:
			return current
		case json.Number:
			if parsed, err := current.Int64(); err == nil {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(current), 10, 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func mergeTemplatePromptPayload(payload map[string]interface{}, userID int64, templateModel *model.TemplateModel, templateUnlockModel *model.TemplateUnlockModel) error {
	if payload == nil || templateModel == nil {
		return nil
	}
	templateID := getInt64FromPayload(payload, "template_id")
	if templateID <= 0 {
		return nil
	}
	template, err := templateModel.GetByID(templateID)
	if err != nil || template == nil {
		return fmt.Errorf("template not found")
	}
	if !isPublicSquareTemplate(template) {
		return fmt.Errorf("template is not available")
	}
	if !template.IsFree && template.Price > 0 && template.CreatorUserID != userID {
		if templateUnlockModel == nil {
			return fmt.Errorf("template unlock service unavailable")
		}
		unlocked, unlockErr := templateUnlockModel.HasUnlocked(userID, templateID)
		if unlockErr != nil {
			return unlockErr
		}
		if !unlocked {
			return fmt.Errorf("please unlock template before generating")
		}
	}

	userPrompt := strings.TrimSpace(getStringFromPayload(payload, "user_prompt"))
	hiddenPrompt := strings.TrimSpace(template.InternalPrompt)
	basePrompt := strings.TrimSpace(getStringFromPayload(payload, "prompt"))
	if basePrompt == "" {
		basePrompt = userPrompt
	}

	if len(parseImageURLArrayFromPayloadMap(payload, "images")) == 0 && len(parseImageURLArrayFromPayloadMap(payload, "reference_image_urls")) == 0 {
		imageURLs := collectTemplateDisplayImageURLs(template)
		if len(imageURLs) > 0 {
			payload["images"] = imageURLs
			payload["reference_image_urls"] = imageURLs
			payload["ordered_image_urls"] = imageURLs
			payload["reference_image_url"] = imageURLs[0]
			if len(imageURLs) == 1 {
				payload["image_url"] = imageURLs[0]
			}
		}
	}

	finalPrompt := basePrompt
	if hiddenPrompt != "" {
		if finalPrompt != "" {
			finalPrompt = finalPrompt + "；" + hiddenPrompt
		} else {
			finalPrompt = hiddenPrompt
		}
		payload["hide_prompt_in_response"] = true
	} else {
		delete(payload, "hide_prompt_in_response")
	}

	payload["user_prompt"] = userPrompt
	if strings.TrimSpace(finalPrompt) == "" {
		delete(payload, "prompt")
	} else {
		payload["prompt"] = finalPrompt
	}
	return nil
}

func joinPromptSegments(parts ...string) string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return strings.Join(result, "，")
}

func appendReferenceImageToPayload(payload map[string]interface{}, imageURL string) {
	refImageURL := strings.TrimSpace(imageURL)
	if refImageURL == "" {
		return
	}
	if getStringFromPayload(payload, "reference_image_url") == "" {
		payload["reference_image_url"] = refImageURL
	}
	existingImages := extractStringArrayFromPayload(payload, "images")
	for _, currentURL := range existingImages {
		if strings.TrimSpace(currentURL) == refImageURL {
			return
		}
	}
	if getStringFromPayload(payload, "original_image_url") != "" || len(existingImages) > 0 {
		payload["images"] = append(existingImages, refImageURL)
	}
}

func removeReferenceImagesFromPayload(payload map[string]interface{}, imageURLs ...string) {
	if payload == nil {
		return
	}
	removeSet := make(map[string]struct{})
	for _, imageURL := range imageURLs {
		if trimmed := strings.TrimSpace(imageURL); trimmed != "" {
			removeSet[trimmed] = struct{}{}
		}
	}
	if len(removeSet) == 0 {
		return
	}
	if currentRef := strings.TrimSpace(getStringFromPayload(payload, "reference_image_url")); currentRef != "" {
		if _, shouldRemove := removeSet[currentRef]; shouldRemove {
			delete(payload, "reference_image_url")
		}
	}
	existingImages := extractStringArrayFromPayload(payload, "images")
	if len(existingImages) == 0 {
		return
	}
	filteredImages := make([]string, 0, len(existingImages))
	for _, currentURL := range existingImages {
		if _, shouldRemove := removeSet[strings.TrimSpace(currentURL)]; shouldRemove {
			continue
		}
		filteredImages = append(filteredImages, currentURL)
	}
	payload["images"] = filteredImages
}

// extractStringArrayFromPayload 从 payload 中提取字符串数组
func extractStringArrayFromPayload(payload map[string]interface{}, key string) []string {
	value, ok := payload[key]
	if !ok {
		return nil
	}
	switch arr := value.(type) {
	case []string:
		return arr
	case []interface{}:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				result = append(result, strings.TrimSpace(s))
			}
		}
		return result
	}
	return nil
}

func enrichAIToolPayload(payload map[string]interface{}, aiToolModel *model.AIToolModel) (int64, error) {
	if payload == nil || aiToolModel == nil {
		return 0, nil
	}
	toolID := getInt64FromPayload(payload, "tool_id")
	toolCode := strings.TrimSpace(getStringFromPayload(payload, "tool_code"))
	var tool *model.AITool
	var err error
	if toolID > 0 {
		tool, err = aiToolModel.GetByID(toolID)
	} else if toolCode != "" {
		tool, err = aiToolModel.GetByCode(toolCode)
	} else {
		return 0, nil
	}
	if err != nil || tool == nil || !tool.IsPublished {
		return 0, fmt.Errorf("工具不存在或未发布")
	}
	referencePresetID := getStringFromPayload(payload, "reference_preset_id")
	stylePresetID := getStringFromPayload(payload, "style_preset_id")
	userPrompt := getStringFromPayload(payload, "user_prompt")
	selectedReference := tool.FindPresetReferenceByID(referencePresetID)
	selectedStyle := tool.FindStylePresetByID(stylePresetID)
	payload["tool_id"] = tool.ID
	payload["tool_code"] = tool.Code
	payload["tool_name"] = tool.Name
	payload["tool_default_prompt"] = tool.DefaultPrompt
	payload["reference_prompt_suffix"] = ""
	payload["style_prompt_suffix"] = ""
	if selectedReference != nil {
		payload["reference_preset_id"] = selectedReference.ID
		payload["reference_preset_name"] = selectedReference.Name
		payload["reference_prompt_suffix"] = selectedReference.PromptSuffix
		appendReferenceImageToPayload(payload, selectedReference.ImageURL)
	}
	if selectedStyle != nil {
		payload["style_preset_id"] = selectedStyle.ID
		payload["style_preset_name"] = selectedStyle.Name
		payload["style_prompt_suffix"] = selectedStyle.PromptSuffix
		payload["style"] = selectedStyle.Name
		modelReferenceImageURL := selectedStyle.EffectiveModelReferenceImageURL()
		removeReferenceImagesFromPayload(payload, selectedStyle.ImageURL, selectedStyle.PreviewImageURL, selectedStyle.ModelReferenceImageURL)
		appendReferenceImageToPayload(payload, modelReferenceImageURL)
	}
	payload["prompt"] = joinPromptSegments(tool.DefaultPrompt, getStringFromPayload(payload, "reference_prompt_suffix"), getStringFromPayload(payload, "style_prompt_suffix"), userPrompt)
	return tool.ID, nil
}

func isAIToolPayloadScene(scene string) bool {
	return strings.HasPrefix(scene, "ai_draw")
}

func isNewMinnanToolScene(scene string) bool {
	return scene == "new_minnan_tool_text_to_image" || scene == "new_minnan_tool_image_to_image"
}

func enrichNewMinnanToolPayload(scene string, payload map[string]interface{}) {
	if payload == nil {
		return
	}
	userPrompt := getStringFromPayload(payload, "user_prompt")
	payload["tool_code"] = "new_minnan_tool"
	payload["tool_name"] = "新闽派工具"
	defaultPrompt := "MNMIX，新闽派建筑风格，结合闽南红砖、燕尾脊、石雕砖雕、骑楼或地域街巷尺度，建筑比例真实，材质细节清晰，光影自然，画面具有专业建筑摄影质感。"
	if scene == "new_minnan_tool_image_to_image" {
		defaultPrompt = "MNMIX，参考图的特色风格应用于原图建筑外立面，保持原图建筑结构、光影和拍摄角度不变，强化新闽派地域建筑气质。"
	}
	payload["tool_default_prompt"] = defaultPrompt
	payload["main_tab"] = "style"
	payload["sub_tab"] = "new_minnan"
	payload["prompt"] = joinPromptSegments(getStringFromPayload(payload, "tool_default_prompt"), userPrompt)
}

// AIRequest AI请求结构
type AIRequest struct {
	Scene   string                 `json:"scene" binding:"required"`
	Payload map[string]interface{} `json:"payload" binding:"required"`
}

func parsePricingExtraConfig(pricing *model.AIPricing) map[string]interface{} {
	if pricing == nil {
		return nil
	}
	raw := strings.TrimSpace(pricing.GetExtraConfig())
	if raw == "" {
		return nil
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil
	}
	return cfg
}

func getPricingIntOption(cfg map[string]interface{}, keys ...string) (int64, bool) {
	for _, key := range keys {
		value, ok := cfg[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case float64:
			return int64(v), true
		case float32:
			return int64(v), true
		case int:
			return int64(v), true
		case int32:
			return int64(v), true
		case int64:
			return v, true
		case json.Number:
			if parsed, err := v.Int64(); err == nil {
				return parsed, true
			}
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func detectUploadedImageMeta(fileData []byte) (string, string, error) {
	if len(fileData) == 0 {
		return "", "", fmt.Errorf("图片内容为空")
	}
	detectedType := http.DetectContentType(fileData[:minInt(len(fileData), 512)])
	switch detectedType {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		return "", "", fmt.Errorf("仅支持 jpg、png、webp、gif 图片")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(fileData))
	if err != nil {
		return "", "", fmt.Errorf("图片内容校验失败")
	}
	if config.Width <= 0 || config.Height <= 0 {
		return "", "", fmt.Errorf("图片尺寸无效")
	}
	decodedImage, decodedFormat, err := image.Decode(bytes.NewReader(fileData))
	if err != nil || decodedImage == nil {
		return "", "", fmt.Errorf("图片解码失败")
	}
	if strings.TrimSpace(decodedFormat) != "" {
		format = decodedFormat
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return ".jpg", "image/jpeg", nil
	case "png":
		return ".png", "image/png", nil
	case "webp":
		return ".webp", "image/webp", nil
	case "gif":
		return ".gif", "image/gif", nil
	default:
		return "", "", fmt.Errorf("暂不支持的图片格式")
	}
}

func RegisterAIPublicRoutes(r *gin.RouterGroup, pricingModel *model.AIPricingModel) {
	ai := r.Group("/ai")
	ai.GET("/pricing", func(c *gin.Context) {
		scenes := []string{"ai_draw_single", "ai_draw_multi", "ai_chat_single"}
		if scenesParam := strings.TrimSpace(c.Query("scenes")); scenesParam != "" {
			parsed := make([]string, 0)
			seen := make(map[string]struct{})
			for _, rawScene := range strings.Split(scenesParam, ",") {
				scene := strings.TrimSpace(rawScene)
				if scene == "" {
					continue
				}
				if _, ok := seen[scene]; ok {
					continue
				}
				seen[scene] = struct{}{}
				parsed = append(parsed, scene)
			}
			if len(parsed) > 0 {
				scenes = parsed
			}
		}

		prices := gin.H{}
		configs := gin.H{}
		for _, scene := range scenes {
			pricing, err := pricingModel.GetByScene(scene)
			if err != nil {
				if err == sql.ErrNoRows {
					continue
				}
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取 AI 计费配置失败: " + err.Error(),
				})
				return
			}
			prices[scene] = pricing.Stones
			if cfg := parsePricingExtraConfig(pricing); cfg != nil {
				configs[scene] = cfg
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"prices":  prices,
				"configs": configs,
			},
		})
	})
}

// RegisterAIRoutes 注册AI相关路由（绘画和聊天）
func RegisterAIRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel, aiToolModel *model.AIToolModel, templateModel *model.TemplateModel, templateUnlockModel *model.TemplateUnlockModel) {
	// AI绘画接口
	r.POST("/ai/draw", func(c *gin.Context) {
		handleAIRequest(c, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel, aiToolModel, templateModel, templateUnlockModel)
	})

	// AI聊天接口
	r.POST("/ai/chat", func(c *gin.Context) {
		handleAIRequest(c, codeSessionModel, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, inviteRelationModel, aiToolModel, templateModel, templateUnlockModel)
	})

	// 根据聊天内容生成提示词接口
	r.POST("/ai/generate-prompt", func(c *gin.Context) {
		handleGeneratePrompt(c, codeSessionModel, userModel, pricingModel, taskModel)
	})

	// 上传参考图（保存到 OSS，返回 URL；生成时从 OSS 取图转 Base64 嵌入请求）
	r.POST("/ai/upload-reference-image", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}

		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "请选择图片文件: " + err.Error(),
			})
			return
		}
		if file.Size > 10*1024*1024 { // 10MB
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "图片大小不能超过 10MB",
			})
			return
		}

		src, err := file.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "打开文件失败: " + err.Error(),
			})
			return
		}
		defer src.Close()

		fileData, err := io.ReadAll(src)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "读取文件失败: " + err.Error(),
			})
			return
		}
		ext, contentType, err := detectUploadedImageMeta(fileData)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  err.Error(),
			})
			return
		}

		cfg := config.Get()
		cosClient := component.GetCOSClient()
		if cosClient == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "存储服务未初始化",
			})
			return
		}

		objectKey := fmt.Sprintf("reference/%d/%s%s", codeSession.UserID, uuid.New().String(), ext)

		fileURL, err := function.UploadBytes(context.Background(), cosClient, cfg, objectKey, fileData, contentType)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "上传失败: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "上传成功",
			"data": gin.H{
				"url": fileURL,
			},
		})
	})
}

// handleAIRequest 统一的AI请求处理
func handleAIRequest(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel, aiToolModel *model.AIToolModel, templateModel *model.TemplateModel, templateUnlockModel *model.TemplateUnlockModel) {
	var req AIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  FormatValidationError("参数错误: " + err.Error()),
		})
		return
	}

	// 从中间件获取已验证的session信息（token验证已在中间件完成）
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "未通过token验证",
		})
		return
	}

	resolvedToolID := int64(0)
	if isAIToolPayloadScene(req.Scene) {
		var enrichErr error
		resolvedToolID, enrichErr = enrichAIToolPayload(req.Payload, aiToolModel)
		if enrichErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatValidationError(enrichErr.Error(), "ToolID"),
			})
			return
		}
	}
	if isNewMinnanToolScene(req.Scene) {
		enrichNewMinnanToolPayload(req.Scene, req.Payload)
	}
	if strings.HasPrefix(req.Scene, "ai_draw") {
		if mergeErr := mergeTemplatePromptPayload(req.Payload, codeSession.UserID, templateModel, templateUnlockModel); mergeErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  mergeErr.Error(),
			})
			return
		}
	}

	// 3. 获取计费配置
	pricing, err := pricingModel.GetByScene(req.Scene)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  FormatValidationError("无效的场景配置: "+err.Error(), "Scene"),
		})
		return
	}

	// 5. 根据生成数量计算费用
	generateCount := int64(1) // 默认1张
	if pricingConfig := parsePricingExtraConfig(pricing); pricingConfig != nil {
		if configGenerateCount, ok := getPricingIntOption(pricingConfig, "generate_count", "default_generate_count"); ok && configGenerateCount > 0 {
			generateCount = configGenerateCount
		}
	}
	if payloadGenerateCount, ok := req.Payload["generate_count"].(float64); ok {
		generateCount = int64(payloadGenerateCount)
		if generateCount <= 0 {
			generateCount = 1 // 至少生成1张
		}
	}
	if generateCount > 3 {
		generateCount = 3
	}
	if strings.HasPrefix(req.Scene, "ai_draw") {
		req.Payload["prompt"] = component.BuildAIDrawPrompt(req.Payload)
	}
	payloadJSON, err := json.Marshal(req.Payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "处理请求数据失败: " + err.Error(),
		})
		return
	}

	// 计算总费用（每张图片的费用 × 生成数量）
	totalStones := pricing.Stones * generateCount

	// 6. 原子扣减灵石（Redis Lua 脚本保证并发安全，check + deduct 在同一原子操作中完成）
	if err := userModel.DeductStones(codeSession.UserID, totalStones); err != nil {
		if strings.Contains(err.Error(), "余额不足") {
			currentStones, _ := userModel.GetStones(codeSession.UserID)
			c.JSON(http.StatusPaymentRequired, gin.H{
				"code": 402,
				"msg":  "余额不足",
				"data": gin.H{
					"required": totalStones,
					"current":  currentStones,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "扣费失败: " + err.Error(),
		})
		return
	}

	tx, err := taskModel.DB.Begin()
	if err != nil {
		_ = userModel.AddStones(codeSession.UserID, totalStones)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "创建事务失败: " + err.Error(),
		})
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	consumeTitle := fmt.Sprintf("AI生成-%s-%d张", req.Scene, generateCount)
	if stoneRecordModel != nil {
		if err := stoneRecordModel.CreateWithTx(tx, codeSession.UserID, "consume", totalStones, consumeTitle, ""); err != nil {
			_ = userModel.AddStones(codeSession.UserID, totalStones)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "写入灵石明细失败: " + err.Error(),
			})
			return
		}
	}
	if userOrderModel != nil {
		orderNo := model.GenerateOrderNo("ORD")
		if err := userOrderModel.CreateWithTx(tx, codeSession.UserID, orderNo, "consume", -totalStones, "success", consumeTitle, ""); err != nil {
			_ = userModel.AddStones(codeSession.UserID, totalStones)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "写入订单失败: " + err.Error(),
			})
			return
		}
	}

	// 8. 生成32位唯一任务编号
	taskNo := function.GenerateTaskNo()

	// 9. 创建任务并加入队列
	task := &model.AITask{
		TaskNo:         taskNo,
		UserID:         codeSession.UserID,
		Scene:          req.Scene,
		RequestPayload: string(payloadJSON),
		Status:         "pending",
		StonesUsed:     totalStones,
	}
	if resolvedToolID > 0 {
		task.ToolID = sql.NullInt64{Int64: resolvedToolID, Valid: true}
	}

	if err := taskModel.CreateWithTx(tx, task); err != nil {
		_ = userModel.AddStones(codeSession.UserID, totalStones)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "创建任务失败: " + err.Error(),
		})
		return
	}
	if err := tx.Commit(); err != nil {
		_ = userModel.AddStones(codeSession.UserID, totalStones)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "提交事务失败: " + err.Error(),
		})
		return
	}
	committed = true

	// 异步更新工具使用次数（非关键操作，失败不影响主流程）
	if resolvedToolID > 0 && aiToolModel != nil {
		if err := aiToolModel.IncrementUsageCount(resolvedToolID); err != nil {
			fmt.Printf("[WARN] 更新 AI 工具使用次数失败: tool_id=%d, err=%v\n", resolvedToolID, err)
		}
	}

	// 10. 返回成功
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "任务已提交",
		"data": gin.H{
			"task_id": task.ID,
			"task_no": task.TaskNo,
		},
	})
}

type TaskStatusRequest struct {
	TaskNo   string `json:"task_no" binding:"required"`
	TaskType string `json:"task_type" binding:"required"` // ai_draw / ai_chat / ai_video / ai_cost_doc
}

// RegisterTaskStatusRoute 注册任务状态轮询接口（支持 AI 绘画、聊天、视频、造价文档）
func RegisterTaskStatusRoute(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, videoTaskModel *model.AIVideoTaskModel, stoneRecordModel *model.StoneRecordModel, aiToolModel *model.AIToolModel, cfg *config.Config) {
	r.POST("/ai/task/status", func(c *gin.Context) {
		var req TaskStatusRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatValidationError("参数错误: " + err.Error()),
			})
			return
		}

		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}

		// AI 生成视频：task_no 为 "v" + 视频任务 id
		if req.TaskType == "ai_video" {
			raw := strings.TrimPrefix(req.TaskNo, "v")
			videoID, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || videoID <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的视频任务编号"})
				return
			}
			task, err := videoTaskModel.GetByIDAndUserID(videoID, codeSession.UserID)
			if err != nil || task == nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在"})
				return
			}
			StartAIVideoTaskMonitor(task.ID, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg)
			task, syncErr := syncAIVideoTask(task, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg)
			if syncErr != nil {
				task = loadLatestAIVideoTask(videoTaskModel, task)
			}
			status := model.AIVideoStatusForUserWithResult(task.Status, strings.TrimSpace(task.OSSURL) != "")
			responseData := gin.H{
				"id":          task.ID,
				"task_no":     req.TaskNo,
				"status":      status,
				"scene":       "ai_video",
				"stones_used": getVideoStones(pricingModel, task.SegmentCount),
				"user_prompt": task.Prompt,
			}
			if task.Status == "completed" && task.OSSURL != "" {
				responseData["result"] = gin.H{"url": task.OSSURL}
			}
			if task.Status == "failed" {
				responseData["error_message"] = task.GetErrorMessage()
				responseData["raw_error_message"] = task.GetRawErrorMessage()
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": responseData})
			return
		}

		// AI 任务（绘画 / 聊天 / 造价文档）：从 ai_tasks 表查
		task, err := taskModel.GetByTaskNo(req.TaskNo)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"code": 404,
				"msg":  "任务不存在",
			})
			return
		}
		if task.UserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{
				"code": 403,
				"msg":  "无权访问此任务",
			})
			return
		}

		// 校验任务类型与 scene
		switch req.TaskType {
		case "ai_draw":
			if task.Scene != "ai_draw_single" && task.Scene != "ai_draw_multi" && !isNewMinnanToolScene(task.Scene) {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("任务类型不匹配", "TaskType")})
				return
			}
		case "ai_chat":
			if task.Scene != "ai_chat_single" && task.Scene != "ai_chat_multi" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("任务类型不匹配", "TaskType")})
				return
			}
		case "ai_cost_doc":
			if task.Scene != "ai_cost_doc" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("任务类型不匹配", "TaskType")})
				return
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatValidationError("无效的任务类型", "TaskType"),
			})
			return
		}

		responseData := buildAITaskResponseData(task, aiToolModel)

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": responseData,
		})
	})
}

// GeneratePromptRequest 生成提示词请求
type GeneratePromptRequest struct {
	Conversation string                   `json:"conversation"` // 对话文本（可选）
	Messages     []map[string]interface{} `json:"messages"`     // 消息列表
}

// handleGeneratePrompt 根据聊天内容生成提示词
func handleGeneratePrompt(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel) {
	var req GeneratePromptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  FormatValidationError("参数错误: " + err.Error()),
		})
		return
	}

	// 从中间件获取已验证的session信息
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "未通过token验证",
		})
		return
	}

	// 提取用户消息内容
	var userMessages []string
	for _, msg := range req.Messages {
		if role, ok := msg["role"].(string); ok && role == "user" {
			if content, ok := msg["content"].(string); ok {
				userMessages = append(userMessages, content)
			}
		}
	}

	// 如果没有消息，使用对话文本
	if len(userMessages) == 0 && req.Conversation != "" {
		// 从对话文本中提取用户消息（按行分割）
		lines := strings.Split(req.Conversation, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "用户:") {
				content := strings.TrimPrefix(line, "用户:")
				content = strings.TrimSpace(content)
				if content != "" {
					userMessages = append(userMessages, content)
				}
			}
		}
	}

	// 生成提示词
	prompt := generatePromptFromMessages(userMessages)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"prompt": prompt,
		},
	})
}

// generatePromptFromMessages 从消息列表中生成提示词
func generatePromptFromMessages(messages []string) string {
	if len(messages) == 0 {
		return "生成设计方案"
	}

	// 合并所有用户消息
	combinedText := strings.Join(messages, "，")

	// 提取关键信息
	var promptParts []string

	// 提取面积信息
	areaPattern := regexp.MustCompile(`(\d+)\s*(m²|平米|平方米|平方)`)
	if matches := areaPattern.FindStringSubmatch(combinedText); len(matches) > 0 {
		promptParts = append(promptParts, fmt.Sprintf("面积：%s%s", matches[1], matches[2]))
	}

	// 提取风格信息
	styleKeywords := []string{"现代简约", "新中式", "新闽派", "传统古建", "海派", "田园风", "欧式", "美式", "日式", "北欧"}
	for _, keyword := range styleKeywords {
		if strings.Contains(combinedText, keyword) {
			promptParts = append(promptParts, fmt.Sprintf("风格：%s", keyword))
			break
		}
	}

	// 提取其他需求关键词
	keywords := []string{"别墅", "住宅", "建筑", "设计", "装修", "改造", "翻新"}
	for _, keyword := range keywords {
		if strings.Contains(combinedText, keyword) {
			promptParts = append(promptParts, keyword)
		}
	}

	// 构建提示词
	if len(promptParts) > 0 {
		return strings.Join(promptParts, "，") + "，" + combinedText
	}

	return combinedText
}
