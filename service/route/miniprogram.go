package route

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"regexp"
	"service/component"
	"service/config"
	"service/function"
	"service/model"
	"service/processor"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RegisterMiniprogramRoutes 注册小程序路由
func RegisterMiniprogramRoutes(r *gin.RouterGroup, authProcessor *processor.AuthProcessor, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, stoneRecordModel *model.StoneRecordModel, inviteRelationModel *model.InviteRelationModel, userDBModel *model.UserModel, userInviteCodeModel *model.UserInviteCodeModel, userProfileModel *model.UserProfileModel, aiToolModel *model.AIToolModel, templateSquareConfigModel *model.TemplateSquareConfigModel) {
	// rejectBannedMiniprogramUser 登录成功后的封禁校验：已封禁账号不允许建立新会话。
	// 返回 true 表示已写响应并终止，调用方直接 return。
	rejectBannedMiniprogramUser := func(c *gin.Context, userID int64) bool {
		if userDBModel == nil {
			return false
		}
		banned, reason, err := userDBModel.GetUserBanStatus(userID)
		if err != nil || !banned {
			return false
		}
		msg := "账号已被封禁，如有疑问请联系客服"
		if reason != "" {
			msg += "：" + reason
		}
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": msg})
		return true
	}

	// 登录接口
	r.POST("/login", func(c *gin.Context) {
		var req processor.WechatLoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatValidationError("参数错误: " + err.Error()),
			})
			return
		}

		// 使用微信登录策略
		strategy, ok := authProcessor.GetStrategy("wechat")
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "登录策略未配置",
			})
			return
		}

		result, err := strategy.Login(context.Background(), &req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "登录失败: " + err.Error(),
			})
			return
		}

		if rejectBannedMiniprogramUser(c, result.User.ID) {
			return
		}

		// 邀请码绑定与奖励：新用户或未绑定过邀请人的用户，填写邀请码可得双方各50灵石（邀请码由后端 6 位唯一码解析）
		if req.InviteCode != "" && inviteRelationModel != nil && userModel != nil && stoneRecordModel != nil && userDBModel != nil {
			inviteeID := result.User.ID
			hasInviter, _ := inviteRelationModel.HasInviter(inviteeID)
			if !hasInviter {
				var inviterID int64
				var ok bool
				if userInviteCodeModel != nil {
					inviterID, ok = userInviteCodeModel.ResolveInviteCodeToUserID(req.InviteCode)
				} else {
					inviterID, ok = model.ParseInviteCodeToUserID(req.InviteCode)
				}
				if ok && inviterID > 0 && inviterID != inviteeID {
					inviter, err := userDBModel.GetByID(inviterID)
					if err == nil && inviter != nil {
						if err := inviteRelationModel.Create(inviterID, inviteeID); err == nil {
							// 双方各得50灵石
							userModel.AddStones(inviterID, 50)
							userModel.AddStones(inviteeID, 50)
							stoneRecordModel.Create(inviterID, "invite", 50, "邀请好友奖励", "")
							stoneRecordModel.Create(inviteeID, "invite", 50, "被邀请注册奖励", "")
						}
					}
				}
			}
		}

		codeSession := result.CodeSession
		if codeSession == nil || codeSession.SessionID == "" {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "登录会话创建失败",
			})
			return
		}

		// 设置session
		if err := SetUserSession(c, result.User.ID, result.User.Username, result.User.UserType); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "保存会话失败",
			})
			return
		}

		// 生成token（加密session_id）
		token, err := function.GenerateToken(codeSession.SessionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "生成token失败: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "登录成功",
			"data": gin.H{
				"id":          result.User.ID,
				"username":    result.User.Username,
				"token":       token,
				"is_new_user": result.IsNewUser,
			},
		})
	})

	// 账号密码登录接口（小程序）
	r.POST("/login/password", func(c *gin.Context) {
		var req processor.PasswordLoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatValidationError("参数错误: " + err.Error()),
			})
			return
		}

		// 创建小程序专用的账号密码登录策略
		userDBModel := model.NewUserModel(component.GetDB())
		miniprogramPasswordStrategy := processor.NewPasswordAuthStrategy(userDBModel, "miniprogram", userProfileModel)

		result, err := miniprogramPasswordStrategy.Login(context.Background(), &req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "登录失败: " + err.Error(),
			})
			return
		}

		// 检查用户类型，小程序登录只允许miniprogram类型用户
		if result.User.UserType != "miniprogram" {
			c.JSON(http.StatusForbidden, gin.H{
				"code": 403,
				"msg":  "该账号不支持小程序登录",
			})
			return
		}

		if rejectBannedMiniprogramUser(c, result.User.ID) {
			return
		}

		// 设置session（基于 gin-contrib/sessions）
		if err := SetUserSession(c, result.User.ID, result.User.Username, result.User.UserType); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "保存会话失败",
			})
			return
		}

		// 获取当前会话ID（用于生成 token）
		session := sessions.Default(c)
		sessionID := session.ID()

		// 为账号密码登录创建 / 确保一条 code_session 记录，
		// 这样基于 token 的 simpleTokenAuth 才能通过 session_id 找到用户。
		cs := &model.CodeSession{
			Code:      "pwd_" + sessionID, // 人为构造一个唯一 code
			DeviceID:  strings.TrimSpace(req.DeviceID),
			SessionID: sessionID,
			UserID:    result.User.ID,
			IsBanned:  false,
		}
		if err := codeSessionModel.Create(cs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "创建会话映射失败: " + err.Error(),
			})
			return
		}

		// 生成token（加密session_id）
		token, err := function.GenerateToken(sessionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "生成token失败: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "登录成功",
			"data": gin.H{
				"id":       result.User.ID,
				"username": result.User.Username,
				"token":    token,
			},
		})
	})

	// 发送短信验证码
	r.POST("/sms/send", func(c *gin.Context) {
		var req struct {
			Phone string `json:"phone" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请输入手机号"})
			return
		}
		phone := strings.TrimSpace(req.Phone)
		if !isValidPhone(phone) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "手机号格式不正确"})
			return
		}
		rdb := component.GetRedis()
		if rdb == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "服务暂不可用"})
			return
		}
		if err := component.SendVerificationCode(phone, rdb); err != nil {
			// 频率限制错误返回 HTTP 429
			var rateLimitErr *component.ErrSMSRateLimited
			if errors.As(err, &rateLimitErr) {
				c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": err.Error()})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "验证码已发送"})
	})

	// 手机号验证码登录/注册
	r.POST("/login/phone", func(c *gin.Context) {
		var req processor.PhoneLoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		phone := strings.TrimSpace(req.Phone)
		code := strings.TrimSpace(req.Code)
		if !isValidPhone(phone) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "手机号格式不正确"})
			return
		}
		if code == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请输入验证码"})
			return
		}
		rdb := component.GetRedis()
		if rdb == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "服务暂不可用"})
			return
		}
		matched, err := component.VerifyCode(phone, code, rdb)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		if !matched {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "验证码错误"})
			return
		}
		strategy, ok := authProcessor.GetStrategy("phone")
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "登录策略未配置"})
			return
		}
		result, err := strategy.Login(context.Background(), &req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "登录失败: " + err.Error()})
			return
		}

		if rejectBannedMiniprogramUser(c, result.User.ID) {
			return
		}

		// 邀请码绑定与奖励
		if req.InviteCode != "" && inviteRelationModel != nil && userModel != nil && stoneRecordModel != nil && userDBModel != nil {
			inviteeID := result.User.ID
			hasInviter, _ := inviteRelationModel.HasInviter(inviteeID)
			if !hasInviter {
				var inviterID int64
				var inviteOK bool
				if userInviteCodeModel != nil {
					inviterID, inviteOK = userInviteCodeModel.ResolveInviteCodeToUserID(req.InviteCode)
				} else {
					inviterID, inviteOK = model.ParseInviteCodeToUserID(req.InviteCode)
				}
				if inviteOK && inviterID > 0 && inviterID != inviteeID {
					inviter, invErr := userDBModel.GetByID(inviterID)
					if invErr == nil && inviter != nil {
						if err := inviteRelationModel.Create(inviterID, inviteeID); err == nil {
							userModel.AddStones(inviterID, 50)
							userModel.AddStones(inviteeID, 50)
							stoneRecordModel.Create(inviterID, "invite", 50, "邀请好友奖励", "")
							stoneRecordModel.Create(inviteeID, "invite", 50, "被邀请注册奖励", "")
						}
					}
				}
			}
		}
		codeSession := result.CodeSession
		if codeSession == nil || codeSession.SessionID == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "登录会话创建失败"})
			return
		}
		if err := SetUserSession(c, result.User.ID, result.User.Username, result.User.UserType); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存会话失败"})
			return
		}
		token, err := function.GenerateToken(codeSession.SessionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "生成token失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "登录成功",
			"data": gin.H{
				"id":          result.User.ID,
				"username":    result.User.Username,
				"token":       token,
				"is_new_user": result.IsNewUser,
			},
		})
	})

	// 获取当前用户信息
	r.GET("/me", AuthRequired, func(c *gin.Context) {
		userID := GetUserID(c)
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"id": userID,
			},
		})
	})
}

func appendUniqueImageURL(target []string, value interface{}) []string {
	url, ok := value.(string)
	if !ok {
		return target
	}
	trimmed := strings.TrimSpace(url)
	if trimmed == "" {
		return target
	}
	for _, existing := range target {
		if existing == trimmed {
			return target
		}
	}
	return append(target, trimmed)
}

func parseImageURLArrayFromPayloadMap(payload map[string]interface{}, key string) []string {
	urls := make([]string, 0, 4)
	if payload == nil {
		return urls
	}
	if arr, ok := payload[key].([]interface{}); ok {
		for _, item := range arr {
			urls = appendUniqueImageURL(urls, item)
		}
	}
	return urls
}

func parseLegacyOrderedImagesFromPayloadMap(payload map[string]interface{}) []string {
	urls := make([]string, 0, 6)
	if payload == nil {
		return urls
	}
	urls = appendUniqueImageURL(urls, payload["image_url"])
	for _, key := range []string{"images", "image_urls", "reference_images"} {
		for _, item := range parseImageURLArrayFromPayloadMap(payload, key) {
			urls = appendUniqueImageURL(urls, item)
		}
	}
	return urls
}

func parseImageSlotsFromPayload(requestPayload string) (originalURLs []string, referenceURLs []string, orderedURLs []string) {
	if strings.TrimSpace(requestPayload) == "" {
		return nil, nil, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(requestPayload), &payload); err != nil {
		return nil, nil, nil
	}
	originalURLs = parseImageURLArrayFromPayloadMap(payload, "original_image_urls")
	referenceURLs = parseImageURLArrayFromPayloadMap(payload, "reference_image_urls")
	legacyOrdered := parseLegacyOrderedImagesFromPayloadMap(payload)
	orderedURLs = make([]string, 0, len(originalURLs)+len(referenceURLs))
	for _, item := range originalURLs {
		orderedURLs = appendUniqueImageURL(orderedURLs, item)
	}
	for _, item := range referenceURLs {
		orderedURLs = appendUniqueImageURL(orderedURLs, item)
	}
	if len(referenceURLs) == 0 && len(originalURLs) == 0 {
		referenceURLs = append(referenceURLs, legacyOrdered...)
	}
	if len(orderedURLs) == 0 {
		orderedURLs = append(orderedURLs, legacyOrdered...)
	}
	return originalURLs, referenceURLs, orderedURLs
}

func parseOriginalImagesFromPayload(requestPayload string) []string {
	originalURLs, _, _ := parseImageSlotsFromPayload(requestPayload)
	return originalURLs
}

// parseReferenceImageFromPayload 从 request_payload JSON 中解析参考图 URL（优先 reference_image_urls，再兼容旧字段）
func parseReferenceImagesFromPayload(requestPayload string) []string {
	_, referenceURLs, _ := parseImageSlotsFromPayload(requestPayload)
	return referenceURLs
}

func parseOrderedImagesFromPayload(requestPayload string) []string {
	_, _, orderedURLs := parseImageSlotsFromPayload(requestPayload)
	return orderedURLs
}

func parseReferenceImageFromPayload(requestPayload string) string {
	referenceURLs := parseReferenceImagesFromPayload(requestPayload)
	if len(referenceURLs) > 0 {
		return referenceURLs[0]
	}
	orderedURLs := parseOrderedImagesFromPayload(requestPayload)
	if len(orderedURLs) > 0 {
		return orderedURLs[0]
	}
	return ""
}

func shouldHidePromptFromPayloadMap(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	raw, ok := payload["hide_prompt_in_response"]
	if !ok {
		return false
	}
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		normalized := strings.ToLower(strings.TrimSpace(value))
		return normalized == "1" || normalized == "true" || normalized == "yes"
	case float64:
		return value != 0
	case int:
		return value != 0
	case int64:
		return value != 0
	default:
		return false
	}
}

// parseUserPromptFromPayload 从 request_payload 中解析 prompt 并去掉前缀/后缀，只返回用户输入部分
func parseUserPromptFromPayload(requestPayload string) string {
	if requestPayload == "" {
		return ""
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(requestPayload), &payload); err != nil {
		return ""
	}
	if userPrompt, ok := payload["user_prompt"].(string); ok && strings.TrimSpace(userPrompt) != "" {
		return strings.TrimSpace(userPrompt)
	}
	if shouldHidePromptFromPayloadMap(payload) {
		return ""
	}
	prompt, _ := payload["prompt"].(string)
	return component.StripUserPromptFromAIDraw(prompt)
}

func parseToolIDFromPayload(requestPayload string) int64 {
	if strings.TrimSpace(requestPayload) == "" {
		return 0
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(requestPayload), &payload); err != nil {
		return 0
	}
	if raw, ok := payload["tool_id"]; ok {
		switch value := raw.(type) {
		case float64:
			return int64(value)
		case float32:
			return int64(value)
		case int:
			return int64(value)
		case int32:
			return int64(value)
		case int64:
			return value
		case string:
			parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func parseToolNameFromPayload(requestPayload string) string {
	if strings.TrimSpace(requestPayload) == "" {
		return ""
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(requestPayload), &payload); err != nil {
		return ""
	}
	if toolName, ok := payload["tool_name"].(string); ok {
		return strings.TrimSpace(toolName)
	}
	return ""
}

func resolveAIToolName(toolID int64, requestPayload string, aiToolModel *model.AIToolModel) string {
	toolName := parseToolNameFromPayload(requestPayload)
	if toolName != "" || toolID <= 0 || aiToolModel == nil {
		return toolName
	}
	tool, err := aiToolModel.GetByID(toolID)
	if err != nil || tool == nil {
		return ""
	}
	return strings.TrimSpace(tool.Name)
}

func resolveAITaskType(scene string) string {
	switch scene {
	case "ai_chat_single", "ai_chat_multi":
		return "ai_chat"
	case "ai_cost_doc":
		return "ai_cost_doc"
	default:
		return "ai_draw"
	}
}

func buildFilteredAITaskResult(task *model.AITask) gin.H {
	resultStr := task.GetResultPayload()
	if strings.TrimSpace(resultStr) == "" {
		return nil
	}
	var resultPayload map[string]interface{}
	if err := json.Unmarshal([]byte(resultStr), &resultPayload); err != nil {
		return nil
	}
	filteredResult := gin.H{}
	for key, value := range resultPayload {
		if key == "url_raw" || key == "raw_images" {
			continue
		}
		filteredResult[key] = value
	}
	if len(filteredResult) == 0 {
		return nil
	}
	return filteredResult
}

func stringValueFromMap(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func stringSliceValueFromMap(data map[string]interface{}, key string) []string {
	if data == nil {
		return nil
	}
	rawValues, ok := data[key].([]interface{})
	if !ok || len(rawValues) == 0 {
		return nil
	}
	result := make([]string, 0, len(rawValues))
	for _, item := range rawValues {
		value, ok := item.(string)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func buildAITaskResponseData(task *model.AITask, aiToolModel *model.AIToolModel) gin.H {
	userPrompt := parseUserPromptFromPayload(task.RequestPayload)
	originalImageURLs := parseOriginalImagesFromPayload(task.RequestPayload)
	referenceImageURLs := parseReferenceImagesFromPayload(task.RequestPayload)
	orderedImageURLs := parseOrderedImagesFromPayload(task.RequestPayload)
	toolID := task.GetToolID()
	if toolID <= 0 {
		toolID = parseToolIDFromPayload(task.RequestPayload)
	}
	toolName := resolveAIToolName(toolID, task.RequestPayload, aiToolModel)
	responseData := gin.H{
		"id":                   task.ID,
		"task_no":              task.TaskNo,
		"tool_id":              toolID,
		"tool_name":            toolName,
		"scene":                task.Scene,
		"status":               task.Status,
		"model":                task.GetResolvedModel(),
		"api_endpoint":         task.GetResolvedAPIEndpoint(),
		"stones_used":          task.StonesUsed,
		"created_at":           task.CreatedAt,
		"updated_at":           task.UpdatedAt,
		"reference_image_url":  parseReferenceImageFromPayload(task.RequestPayload),
		"reference_image_urls": referenceImageURLs,
		"original_image_urls":  originalImageURLs,
		"ordered_image_urls":   orderedImageURLs,
		"prompt":               userPrompt,
		"user_prompt":          userPrompt,
		"task_type":            resolveAITaskType(task.Scene),
	}
	if filteredResult := buildFilteredAITaskResult(task); len(filteredResult) > 0 {
		responseData["result"] = filteredResult
		if thumbnailURL := stringValueFromMap(filteredResult, "thumbnail_url"); thumbnailURL != "" {
			responseData["thumbnail_url"] = thumbnailURL
		}
		if thumbnailURLs := stringSliceValueFromMap(filteredResult, "thumbnail_urls"); len(thumbnailURLs) > 0 {
			responseData["thumbnail_urls"] = thumbnailURLs
		}
		for _, key := range []string{"requested_count", "generated_count", "refunded_stones", "refunded_image_count", "stones_used"} {
			if value, ok := filteredResult[key]; ok {
				responseData[key] = value
			}
		}
	}
	if task.Status == "failed" {
		responseData["error_message"] = task.GetErrorMessage()
	}
	return responseData
}

func normalizeDesignerCategoryValue(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func designerCategoryAliases(raw string) []string {
	appendUnique := func(target []string, values ...string) []string {
		seen := make(map[string]struct{}, len(target))
		for _, item := range target {
			seen[item] = struct{}{}
		}
		for _, value := range values {
			normalized := normalizeDesignerCategoryValue(value)
			if normalized == "" {
				continue
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			target = append(target, normalized)
		}
		return target
	}

	switch normalizeDesignerCategoryValue(raw) {
	case "", "all":
		return nil
	case "template":
		return appendUnique(nil, "template", "xiangshu", "villa", "villa_exterior", "interior_space")
	case "scene":
		return appendUnique(nil, "scene", "urban", "family", "culture", "villa_exterior", "interior_space", "garden_courtyard", "renovation", "commercial_space", "design_assist")
	case "material":
		return appendUnique(nil, "material", "style", "new_minnan", "new_chinese", "modern", "classic_european", "regional")
	case "latest":
		return appendUnique(nil, "latest", "inspiration", "gallery", "rural_trend", "lifestyle", "regional_culture", "function_innovation", "selected_cases")
	case "villa":
		return appendUnique(nil, "villa", "xiangshu", "villa_exterior", "garden_courtyard", "villa_case", "villa_courtyard", "villa_construction")
	default:
		return appendUnique(nil, raw)
	}
}

func designerCategoryLabel(raw string) string {
	switch normalizeDesignerCategoryValue(raw) {
	case "template", "xiangshu":
		return "乡墅私宅"
	case "scene", "urban", "family", "culture", "villa_exterior", "interior_space", "garden_courtyard", "renovation", "commercial_space", "design_assist":
		return "空间场景"
	case "material", "style", "new_minnan", "new_chinese", "modern", "classic_european", "regional":
		return "材质软装"
	case "latest", "inspiration", "gallery", "rural_trend", "lifestyle", "regional_culture", "function_innovation", "selected_cases":
		return "创意玩法"
	case "villa", "villa_case", "villa_courtyard", "villa_construction":
		return "庭院乡墅"
	default:
		return ""
	}
}

func buildDesignerCategoryMatchCondition(tableAlias, rawCategory string) (string, []interface{}) {
	values := designerCategoryAliases(rawCategory)
	if len(values) == 0 {
		return "", nil
	}
	buildFieldCondition := func(field string) (string, []interface{}) {
		placeholders := make([]string, 0, len(values))
		args := make([]interface{}, 0, len(values))
		for _, value := range values {
			placeholders = append(placeholders, "?")
			args = append(args, value)
		}
		return field + " IN (" + strings.Join(placeholders, ", ") + ")", args
	}
	mainTabCondition, mainTabArgs := buildFieldCondition(tableAlias + ".main_tab")
	categoryCondition, categoryArgs := buildFieldCondition(tableAlias + ".category")
	subTabCondition, subTabArgs := buildFieldCondition(tableAlias + ".sub_tab")
	args := make([]interface{}, 0, len(mainTabArgs)+len(categoryArgs)+len(subTabArgs))
	args = append(args, mainTabArgs...)
	args = append(args, categoryArgs...)
	args = append(args, subTabArgs...)
	return "(" + strings.Join([]string{mainTabCondition, categoryCondition, subTabCondition}, " OR ") + ")", args
}

func designerWorkMatchesCategory(item gin.H, rawCategory string) bool {
	values := designerCategoryAliases(rawCategory)
	if len(values) == 0 {
		return true
	}
	valueSet := make(map[string]struct{}, len(values))
	for _, value := range values {
		valueSet[value] = struct{}{}
	}
	for _, key := range []string{"main_tab", "category", "sub_tab"} {
		candidate, _ := item[key].(string)
		if _, ok := valueSet[normalizeDesignerCategoryValue(candidate)]; ok {
			return true
		}
	}
	return false
}

func buildDesignerSpecialties(templates []*model.Template, identityType string) string {
	parts := make([]string, 0, 3)
	seen := map[string]bool{}
	appendPart := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] || len(parts) >= 3 {
			return
		}
		seen[v] = true
		parts = append(parts, v)
	}
	for _, item := range templates {
		if item == nil {
			continue
		}
		appendPart(designerCategoryLabel(item.MainTab))
		appendPart(designerCategoryLabel(item.SubTab))
		appendPart(designerCategoryLabel(item.Category))
	}
	if len(parts) == 0 {
		if strings.TrimSpace(identityType) != "" {
			return "专业领域：" + strings.TrimSpace(identityType)
		}
		return "专业领域：室内设计，室外设计，庭院设计"
	}
	return "专业领域：" + strings.Join(parts, "，")
}

func buildDesignerWorkTags(item *model.Template) []string {
	tags := make([]string, 0, 3)
	seen := map[string]bool{}
	appendTag := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] || len(tags) >= 3 {
			return
		}
		seen[v] = true
		tags = append(tags, v)
	}
	appendTag(designerCategoryLabel(item.MainTab))
	appendTag(designerCategoryLabel(item.SubTab))
	appendTag(designerCategoryLabel(item.Category))
	if item.IsFree {
		appendTag("免费")
	} else if item.Price > 0 {
		appendTag(strconv.FormatInt(item.Price, 10) + "灵石")
	}
	return tags
}

func buildDesignerSpecialtiesText(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "专业领域：待完善"
	}
	clean = strings.ReplaceAll(clean, "；", "，")
	clean = strings.ReplaceAll(clean, ",", "，")
	if strings.HasPrefix(clean, "专业领域：") {
		return clean
	}
	return "专业领域：" + clean
}

func designerExperienceText(years int64) string {
	if years <= 0 {
		return "设计经验：待完善"
	}
	return "设计经验：" + strconv.FormatInt(years, 10) + "年"
}

func reviewSentimentFromRating(rating int) string {
	if rating >= 4 {
		return "positive"
	}
	return "negative"
}

func buildDesignerReviewRelatedInfo(order *model.UserOrder) (string, string, int64) {
	if order == nil {
		return "", "", 0
	}
	relatedTitle := strings.TrimSpace(order.Description)
	if relatedTitle == "" {
		relatedTitle = strings.TrimSpace(order.Title)
	}
	if relatedTitle == "" {
		return "", "", 0
	}
	relatedTemplateID := int64(0)
	if order.TemplateID != nil && *order.TemplateID > 0 {
		relatedTemplateID = *order.TemplateID
	}
	switch strings.TrimSpace(order.OrderCategory) {
	case "service":
		return "关联服务", relatedTitle, 0
	case "template":
		return "关联作品", relatedTitle, relatedTemplateID
	default:
		return "关联内容", relatedTitle, 0
	}
}

func isDesignerPublicVisible(user *model.User, profileData *model.UserProfile, latestCert *model.CertificationApplication) bool {
	if user == nil || strings.TrimSpace(user.UserType) != "miniprogram" {
		return false
	}
	if profileData != nil && !profileData.DesignerVisible {
		return false
	}
	return true
}

func listPublicDesignerUserIDs(userDBModel *model.UserModel, keyword, mainTab string, limit, offset int) ([]int64, int64, error) {
	if userDBModel == nil || userDBModel.DB == nil {
		return nil, 0, errors.New("服务不可用")
	}
	conditions := []string{
		"u.user_type = 'miniprogram'",
		"COALESCE(up.designer_visible, 1) = 1",
		"EXISTS (SELECT 1 FROM templates tp WHERE tp.creator_user_id = u.id AND tp.status = 'published')",
	}
	args := make([]interface{}, 0)
	trimmedKeyword := strings.TrimSpace(keyword)
	if trimmedKeyword != "" {
		like := "%" + trimmedKeyword + "%"
		conditions = append(conditions, `(u.username LIKE ? OR COALESCE(up.nickname, '') LIKE ? OR COALESCE(up.service_title, '') LIKE ? OR COALESCE(up.designer_bio, '') LIKE ? OR COALESCE(up.specialty_styles, '') LIKE ? OR EXISTS (SELECT 1 FROM templates tx WHERE tx.creator_user_id = u.id AND tx.status = 'published' AND (tx.name LIKE ? OR tx.description LIKE ?)))`)
		args = append(args, like, like, like, like, like, like, like)
	}
	trimmedMainTab := normalizeDesignerCategoryValue(mainTab)
	if trimmedMainTab != "" && trimmedMainTab != "all" {
		categoryCondition, categoryArgs := buildDesignerCategoryMatchCondition("t", trimmedMainTab)
		if categoryCondition != "" {
			conditions = append(conditions, `EXISTS (SELECT 1 FROM templates t WHERE t.creator_user_id = u.id AND t.status = 'published' AND `+categoryCondition+`)`)
			args = append(args, categoryArgs...)
		}
	}
	whereClause := strings.Join(conditions, " AND ")
	countQuery := `SELECT COUNT(*) FROM users u LEFT JOIN user_profiles up ON up.user_id = u.id WHERE ` + whereClause
	var total int64
	if err := userDBModel.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT u.id FROM users u LEFT JOIN user_profiles up ON up.user_id = u.id WHERE ` + whereClause + ` ORDER BY u.id DESC LIMIT ? OFFSET ?`
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := userDBModel.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, 0, err
		}
		ids = append(ids, userID)
	}
	return ids, total, nil
}

func pickRepresentativeDesignerWork(worksAny interface{}, mainTab string) gin.H {
	works, ok := worksAny.([]gin.H)
	if !ok || len(works) == 0 {
		return nil
	}
	trimmedMainTab := normalizeDesignerCategoryValue(mainTab)
	if trimmedMainTab == "" || trimmedMainTab == "all" {
		return works[0]
	}
	for _, item := range works {
		if designerWorkMatchesCategory(item, trimmedMainTab) {
			return item
		}
	}
	return works[0]
}

func buildDesignerCenterPayload(targetUserID int64, userDBModel *model.UserModel, templateModel *model.TemplateModel, userProfileModel *model.UserProfileModel, certificationModel *model.CertificationApplicationModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, designerReviewModel *model.DesignerReviewModel) (gin.H, error) {
	if userDBModel == nil {
		return nil, errors.New("服务不可用")
	}

	user, err := userDBModel.GetByID(targetUserID)
	if err != nil || user == nil {
		return nil, errors.New("用户不存在")
	}

	var profileData *model.UserProfile
	if userProfileModel != nil {
		profileData, _ = userProfileModel.GetByUserID(targetUserID)
	}

	var latestCert *model.CertificationApplication
	if certificationModel != nil {
		latestCert, _ = certificationModel.GetLatestByUser(targetUserID)
	}

	totalWorks := int64(0)
	if templateModel != nil {
		if count, countErr := templateModel.CountPublishedByCreatorUserID(targetUserID, ""); countErr == nil {
			totalWorks = count
		}
	}
	worksLimit := 24
	if totalWorks > 0 {
		worksLimit = int(totalWorks)
	}
	works := make([]*model.Template, 0)
	if templateModel != nil {
		works, _ = templateModel.ListPublishedByCreatorUserID(targetUserID, "", worksLimit, 0)
	}
	if totalWorks == 0 {
		totalWorks = int64(len(works))
	}

	totalOrders, monthOrders := int64(0), int64(0)
	if userOrderModel != nil {
		totalOrders, monthOrders, _ = userOrderModel.SummaryByDesignerUserID(targetUserID)
	}

	totalEarnings, monthEarnings := int64(0), int64(0)
	if stoneRecordModel != nil {
		totalEarnings, monthEarnings, _ = stoneRecordModel.TemplateEarningsSummary(targetUserID)
	}

	positiveCount, negativeCount := int64(0), int64(0)
	reviewList := make([]*model.DesignerReview, 0)
	if designerReviewModel != nil {
		reviewList, _, _ = designerReviewModel.ListByDesignerUserID(targetUserID, 3, 0)
		positiveCount, negativeCount, _ = designerReviewModel.SummaryByDesignerUserID(targetUserID)
	}

	displayName := strings.TrimSpace(user.Username)
	avatar := ""
	if profileData != nil {
		if strings.TrimSpace(profileData.Nickname) != "" {
			displayName = strings.TrimSpace(profileData.Nickname)
		}
		avatar = sanitizePublicImageURL(profileData.Avatar)
	}
	if displayName == "" && latestCert != nil && strings.TrimSpace(latestCert.RealName) != "" {
		displayName = strings.TrimSpace(latestCert.RealName)
	}

	designerTitle := "室内设计师"
	certStatus := ""
	bioText := "个人信息：室内设计师"
	createdAtText := user.CreatedAt.Format("2006年1月2日")
	specialtiesText := "专业领域：待完善"
	if latestCert != nil {
		certStatus = strings.TrimSpace(latestCert.Status)
		if strings.TrimSpace(latestCert.IdentityType) != "" {
			designerTitle = strings.TrimSpace(latestCert.IdentityType)
		}
		if latestCert.CreatedAt.Unix() > 0 {
			createdAtText = latestCert.CreatedAt.Format("2006年1月2日")
		}
		if strings.TrimSpace(latestCert.ExtraDocsRemark) != "" {
			bioText = "个人信息：" + strings.TrimSpace(latestCert.ExtraDocsRemark)
		} else {
			bioText = "个人信息：" + designerTitle
		}
	} else {
		bioText = "个人信息：" + designerTitle
	}
	if profileData != nil {
		if strings.TrimSpace(profileData.ServiceTitle) != "" {
			designerTitle = strings.TrimSpace(profileData.ServiceTitle)
		}
		if strings.TrimSpace(profileData.DesignerBio) != "" {
			bioText = "个人信息：" + strings.TrimSpace(profileData.DesignerBio)
		}
		specialtiesText = buildDesignerSpecialtiesText(profileData.SpecialtyStyles)
	}

	experienceText := designerExperienceText(0)
	if profileData != nil {
		experienceText = designerExperienceText(profileData.DesignerExperienceYears)
	}

	serviceFee := int64(0)
	serviceIntro := ""
	serviceEnabled := false
	if profileData != nil {
		serviceFee = profileData.ServiceQuote
		serviceIntro = strings.TrimSpace(profileData.ServiceIntro)
		serviceEnabled = profileData.ServiceEnabled
	}
	if serviceFee <= 0 {
		for _, item := range works {
			if item != nil && item.Price > 0 {
				serviceFee = item.Price
				break
			}
		}
	}

	worksOut := make([]gin.H, 0, len(works))
	for _, item := range works {
		if item == nil {
			continue
		}
		image := strings.TrimSpace(item.PreviewURL)
		if image == "" {
			image = strings.TrimSpace(item.Thumbnail)
		}
		worksOut = append(worksOut, gin.H{
			"id":              item.ID,
			"title":           item.Name,
			"image":           image,
			"description":     item.Description,
			"main_tab":        item.MainTab,
			"sub_tab":         item.SubTab,
			"third_tab":       item.ThirdTab,
			"category":        item.Category,
			"price":           item.Price,
			"is_free":         item.IsFree,
			"publish_scope":   item.PublishScope,
			"source_type":     item.SourceType,
			"likes":           item.LikeCount,
			"views":           item.DownloadCount,
			"created_at":      item.CreatedAt,
			"updated_at":      item.UpdatedAt,
			"creator_user_id": item.CreatorUserID,
			"creator_info":    buildTemplateCreatorInfo(item.CreatorUserID, userProfileModel, userDBModel),
			"tags":            buildDesignerWorkTags(item),
		})
	}

	reviewsOut := make([]gin.H, 0, len(reviewList))
	for _, item := range reviewList {
		if item == nil {
			continue
		}
		relatedLabel, relatedTitle := "", ""
		relatedTemplateID := int64(0)
		if userOrderModel != nil && item.OrderID > 0 {
			if order, orderErr := userOrderModel.GetByID(item.OrderID); orderErr == nil {
				relatedLabel, relatedTitle, relatedTemplateID = buildDesignerReviewRelatedInfo(order)
			}
		}
		reviewsOut = append(reviewsOut, gin.H{
			"id":                  item.ID,
			"name":                item.ReviewerName,
			"avatar":              sanitizePublicImageURL(item.ReviewerAvatar),
			"score":               item.Rating,
			"content":             item.Content,
			"sentiment":           item.Sentiment,
			"created_at":          item.CreatedAt,
			"related_label":       relatedLabel,
			"related_title":       relatedTitle,
			"related_template_id": relatedTemplateID,
		})
	}

	publicVisible := isDesignerPublicVisible(user, profileData, latestCert)
	designerVisible := true
	if profileData != nil {
		designerVisible = profileData.DesignerVisible
	}

	return gin.H{
		"public_visible": publicVisible,
		"profile": gin.H{
			"user_id":          targetUserID,
			"name":             displayName,
			"avatar":           avatar,
			"title":            designerTitle,
			"experience_text":  experienceText,
			"bio_text":         bioText,
			"specialties_text": specialtiesText,
			"cert_status":      certStatus,
			"designer_visible": designerVisible,
		},
		"stats": gin.H{
			"total_works":    totalWorks,
			"total_orders":   totalOrders,
			"month_orders":   monthOrders,
			"total_earnings": totalEarnings,
			"month_earnings": monthEarnings,
		},
		"service_config": gin.H{
			"service_type":    designerTitle,
			"created_at_text": createdAtText,
			"total_earnings":  totalEarnings,
			"month_earnings":  monthEarnings,
			"total_orders":    totalOrders,
			"fee":             serviceFee,
			"service_intro":   serviceIntro,
			"service_enabled": serviceEnabled,
		},
		"review_summary": gin.H{
			"positive_count": positiveCount,
			"negative_count": negativeCount,
		},
		"works":   worksOut,
		"reviews": reviewsOut,
	}, nil
}

func buildEnterpriseWechatQRCodeURL(baseURL, ticket string) string {
	trimmedBaseURL := strings.TrimSpace(baseURL)
	trimmedTicket := strings.TrimSpace(ticket)
	if trimmedBaseURL == "" || trimmedTicket == "" {
		return trimmedBaseURL
	}
	if strings.Contains(trimmedBaseURL, "ticket=") {
		return trimmedBaseURL
	}
	separator := "?"
	if strings.Contains(trimmedBaseURL, "?") {
		separator = "&"
	}
	return trimmedBaseURL + separator + "ticket=" + trimmedTicket
}

func hasValidEnterpriseWechatContact(contact string) bool {
	trimmed := strings.TrimSpace(contact)
	if trimmed == "" {
		return false
	}
	if trimmed == "企微已添加，待补全联系方式" || trimmed == "待补全联系方式" {
		return false
	}
	return true
}

func proxyRemoteDownload(c *gin.Context, fileURL, defaultFilename, defaultContentType string) {
	trimmedURL := strings.TrimSpace(fileURL)
	if trimmedURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无可下载文件地址"})
		return
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, trimmedURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建下载请求失败"})
		return
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": "下载源文件失败"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		c.JSON(http.StatusBadGateway, gin.H{"code": 502, "msg": "下载源文件失败"})
		return
	}
	filename := strings.TrimSpace(defaultFilename)
	if filename == "" {
		filename = path.Base(strings.TrimSpace(resp.Request.URL.Path))
	}
	if filename == "" || filename == "." || filename == "/" {
		filename = "download"
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" || strings.EqualFold(contentType, "application/octet-stream") || strings.EqualFold(contentType, "binary/octet-stream") {
		contentType = strings.TrimSpace(defaultContentType)
	}
	if contentType != "" {
		c.Header("Content-Type", contentType)
	}
	if contentLength := strings.TrimSpace(resp.Header.Get("Content-Length")); contentLength != "" {
		c.Header("Content-Length", contentLength)
	}
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		return
	}
}

func getOrCreateEnterpriseWechatBindTicket(ticketModel *model.EnterpriseWechatBindTicketModel, userID int64, scene, taskNo string, imageIndex int) (*model.EnterpriseWechatBindTicket, error) {
	if ticketModel == nil {
		return nil, errors.New("企微绑定票据服务不可用")
	}
	item, err := ticketModel.GetActiveByUserID(userID, scene)
	if err == nil && item != nil {
		return item, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	now := time.Now()
	expiredAt := now.Add(24 * time.Hour)
	item = &model.EnterpriseWechatBindTicket{
		UserID:     userID,
		Ticket:     uuid.New().String(),
		Scene:      scene,
		Status:     "pending",
		TaskNo:     strings.TrimSpace(taskNo),
		ImageIndex: imageIndex,
		ExpiredAt:  &expiredAt,
	}
	if err := ticketModel.Create(item); err != nil {
		return nil, err
	}
	return item, nil
}

func generateEnterpriseWechatCallbackSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.TrimSpace(timestamp)))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyEnterpriseWechatCallbackRequest(secret, timestamp, signature string, body []byte, maxSkewSec int64) error {
	trimmedSecret := strings.TrimSpace(secret)
	if trimmedSecret == "" {
		return nil
	}
	trimmedTimestamp := strings.TrimSpace(timestamp)
	trimmedSignature := strings.ToLower(strings.TrimSpace(signature))
	if trimmedTimestamp == "" || trimmedSignature == "" {
		return errors.New("缺少回调签名或时间戳")
	}
	ts, err := strconv.ParseInt(trimmedTimestamp, 10, 64)
	if err != nil {
		return errors.New("回调时间戳格式无效")
	}
	if maxSkewSec > 0 {
		now := time.Now().Unix()
		delta := now - ts
		if delta < 0 {
			delta = -delta
		}
		if delta > maxSkewSec {
			return errors.New("回调时间戳已过期")
		}
	}
	expected := generateEnterpriseWechatCallbackSignature(trimmedSecret, trimmedTimestamp, body)
	if !hmac.Equal([]byte(expected), []byte(trimmedSignature)) {
		return errors.New("回调签名验证失败")
	}
	return nil
}

// RegisterUserDataRoutes 注册用户数据相关路由（需要token认证）
func RegisterUserDataRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, videoTaskModel *model.AIVideoTaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, inviteRelationModel *model.InviteRelationModel, userDBModel *model.UserModel, templateModel *model.TemplateModel, userProfileModel *model.UserProfileModel, userInviteCodeModel *model.UserInviteCodeModel, certificationModel *model.CertificationApplicationModel, designerReviewModel *model.DesignerReviewModel, designerFollowModel *model.DesignerFollowModel, aiToolModel *model.AIToolModel, cfg *config.Config) {
	// 简化版token认证中间件（只需要token，不需要其他签名参数）
	simpleTokenAuth := func(c *gin.Context) {
		token := c.GetHeader("token")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "缺少token",
			})
			c.Abort()
			return
		}

		sessionID, err := function.DecryptToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "token无效: " + err.Error(),
			})
			c.Abort()
			return
		}

		codeSession, err := codeSessionModel.GetBySessionID(sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "无效的session_id",
			})
			c.Abort()
			return
		}

		if codeSession.IsBanned {
			c.JSON(http.StatusForbidden, gin.H{
				"code": 403,
				"msg":  "账号已被封禁，无法使用",
			})
			c.Abort()
			return
		}

		c.Set("code_session", codeSession)
		c.Next()
	}

	enterpriseWechatBindTicketModel := model.NewEnterpriseWechatBindTicketModel(component.GetDB())

	// 绑定手机号（需登录）
	r.POST("/phone/bind", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		var req struct {
			Phone string `json:"phone" binding:"required"`
			Code  string `json:"code" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
			return
		}
		phone := strings.TrimSpace(req.Phone)
		code := strings.TrimSpace(req.Code)
		if !isValidPhone(phone) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "手机号格式不正确"})
			return
		}
		rdb := component.GetRedis()
		if rdb == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "服务暂不可用"})
			return
		}
		matched, err := component.VerifyCode(phone, code, rdb)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		if !matched {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "验证码错误"})
			return
		}
		userID := codeSession.UserID
		// 解析有效用户（处理已合并账号）
		effectiveUser, resolveErr := userDBModel.GetEffectiveUserByID(userID)
		if resolveErr == nil && effectiveUser != nil {
			userID = effectiveUser.ID
		}
		userIdentityModel := model.NewUserIdentityModel(component.GetDB())
		// 检查手机号是否已被其他用户绑定（user_identities 表）
		existingUserID, err := userIdentityModel.GetUserIDByPhone(phone)
		if err == nil && existingUserID > 0 && existingUserID != userID {
			// 解析对方的有效用户（可能是已合并到当前用户的账号）
			existingEffective, resolveErr2 := userDBModel.GetEffectiveUserByID(existingUserID)
			if resolveErr2 == nil && existingEffective != nil && existingEffective.ID == userID {
				// 对方已合并到当前用户，可放行更新绑定
			} else {
				c.JSON(http.StatusConflict, gin.H{
					"code": 40901,
					"msg":  "该手机号已绑定其他账号，如需合并请联系客服",
					"data": gin.H{"conflict": true},
				})
				return
			}
		}
		// 绑定手机号
		if err := userIdentityModel.BindPhone(userID, phone); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "绑定手机号失败: " + err.Error()})
			return
		}
		// 更新 profile 中的手机号
		if userProfileModel != nil {
			_ = userProfileModel.UpdatePhone(userID, phone)
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "绑定成功"})
	})

	r.POST("/wecom/enterprise-wechat/callback", func(c *gin.Context) {
		cfg := config.Get()
		rawBody, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "读取回调请求体失败"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))
		timestamp := c.GetHeader("X-Enterprise-Timestamp")
		if strings.TrimSpace(timestamp) == "" {
			timestamp = c.Query("timestamp")
		}
		signature := c.GetHeader("X-Enterprise-Signature")
		if strings.TrimSpace(signature) == "" {
			signature = c.Query("signature")
		}
		if err := verifyEnterpriseWechatCallbackRequest(
			cfg.EnterpriseWechat.CallbackSecret,
			timestamp,
			signature,
			rawBody,
			cfg.EnterpriseWechat.CallbackMaxSkewSec,
		); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": err.Error()})
			return
		}
		var req struct {
			Ticket         string `json:"ticket" binding:"required"`
			Contact        string `json:"contact"`
			ExternalUserID string `json:"external_user_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		if enterpriseWechatBindTicketModel == nil || userProfileModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "企微回调服务不可用"})
			return
		}
		item, err := enterpriseWechatBindTicketModel.GetByTicket(strings.TrimSpace(req.Ticket))
		if err != nil || item == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "绑定票据不存在"})
			return
		}
		contact := strings.TrimSpace(req.Contact)
		if contact == "" {
			contact = strings.TrimSpace(item.Contact)
		}
		if !hasValidEnterpriseWechatContact(contact) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "授权手机号不能为空，请先补充手机号后再回调"})
			return
		}
		if err := userProfileModel.SetEnterpriseWechatVerification(item.UserID, true, contact); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新用户手机号授权状态失败: " + err.Error()})
			return
		}
		if err := enterpriseWechatBindTicketModel.MarkVerified(item.Ticket, contact, strings.TrimSpace(req.ExternalUserID)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新绑定票据状态失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"ticket":                     item.Ticket,
				"enterprise_wechat_verified": true,
				"enterprise_wechat_contact":  contact,
			},
		})
	})

	r.GET("/designers", func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 50 {
			pageSize = 10
		}
		mainTab := strings.TrimSpace(c.Query("main_tab"))
		keyword := strings.TrimSpace(c.Query("keyword"))
		offset := (page - 1) * pageSize
		ids, total, err := listPublicDesignerUserIDs(userDBModel, keyword, mainTab, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取设计师列表失败: " + err.Error()})
			return
		}
		list := make([]gin.H, 0, len(ids))
		for _, designerUserID := range ids {
			payload, err := buildDesignerCenterPayload(designerUserID, userDBModel, templateModel, userProfileModel, certificationModel, stoneRecordModel, userOrderModel, designerReviewModel)
			if err != nil {
				continue
			}
			if visible, ok := payload["public_visible"].(bool); ok && !visible {
				continue
			}
			profile, _ := payload["profile"].(gin.H)
			representativeWork := pickRepresentativeDesignerWork(payload["works"], mainTab)
			item := gin.H{
				"designer_user_id": designerUserID,
				"designer_name":    profile["name"],
				"designer_avatar":  profile["avatar"],
				"designer_title":   profile["title"],
				"experience_text":  profile["experience_text"],
				"specialties_text": profile["specialties_text"],
				"cert_status":      profile["cert_status"],
				"designer_visible": profile["designer_visible"],
			}
			if representativeWork != nil {
				item["representative_work"] = representativeWork
			} else {
				item["representative_work"] = gin.H{
					"id":    int64(0),
					"title": "暂未上传代表作品",
					"likes": int64(0),
					"views": int64(0),
				}
			}
			list = append(list, item)
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"list":      list,
				"total":     total,
				"page":      page,
				"page_size": pageSize,
			},
		})
	})

	r.GET("/designers/:id/homepage", func(c *gin.Context) {
		userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || userID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的设计师ID"})
			return
		}

		data, err := buildDesignerCenterPayload(userID, userDBModel, templateModel, userProfileModel, certificationModel, stoneRecordModel, userOrderModel, designerReviewModel)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "设计师不存在"})
			return
		}
		if visible, ok := data["public_visible"].(bool); ok && !visible {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "该设计师主页暂未公开"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": data})
	})

	r.GET("/user/designers/:id/follow", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		designerUserID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || designerUserID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的设计师ID"})
			return
		}
		if designerFollowModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "关注服务不可用"})
			return
		}
		followed, err := designerFollowModel.IsFollowing(codeSession.UserID, designerUserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取关注状态失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"followed": followed}})
	})

	r.POST("/user/designers/:id/follow", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		designerUserID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || designerUserID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的设计师ID"})
			return
		}
		if designerUserID == codeSession.UserID {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "不能关注自己"})
			return
		}
		if designerFollowModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "关注服务不可用"})
			return
		}
		if err := designerFollowModel.Follow(codeSession.UserID, designerUserID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "关注设计师失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "关注成功", "data": gin.H{"followed": true}})
	})

	r.DELETE("/user/designers/:id/follow", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		designerUserID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || designerUserID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的设计师ID"})
			return
		}
		if designerFollowModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "关注服务不可用"})
			return
		}
		if err := designerFollowModel.Unfollow(codeSession.UserID, designerUserID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "取消关注失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "取消关注成功", "data": gin.H{"followed": false}})
	})

	// 获取用户灵石余额
	r.GET("/user/stones", simpleTokenAuth, func(c *gin.Context) {
		// 从中间件获取已验证的session信息
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}

		// 获取用户灵石余额
		stones, err := userModel.GetStones(codeSession.UserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取余额失败: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"stones": stones,
			},
		})
	})

	// 灵石明细汇总（当前余额、近30天消耗/获得、累计签到）
	r.GET("/user/stones/summary", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}
		balance, err := userModel.GetStones(codeSession.UserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取余额失败: " + err.Error(),
			})
			return
		}
		recentConsume, recentGain, checkinTotal := int64(0), int64(0), int64(0)
		if stoneRecordModel != nil {
			recentConsume, recentGain, checkinTotal, err = stoneRecordModel.Summary(codeSession.UserID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取灵石统计失败: " + err.Error(),
				})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"balance":        balance,
				"recent_consume": recentConsume,
				"recent_gain":    recentGain,
				"checkin_total":  checkinTotal,
			},
		})
	})

	// 灵石明细列表（分页，可选类型筛选）
	r.POST("/user/stones/details", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}
		var req struct {
			Page     int    `json:"page"`
			PageSize int    `json:"page_size"`
			Type     string `json:"type"` // all/consume/recharge/checkin/task
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "参数错误",
			})
			return
		}
		page, pageSize := req.Page, req.PageSize
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}
		offset := (page - 1) * pageSize
		typ := req.Type
		if typ == "" {
			typ = "all"
		}
		if stoneRecordModel == nil {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      []interface{}{},
					"total":     0,
					"page":      page,
					"page_size": pageSize,
				},
			})
			return
		}
		list, total, err := stoneRecordModel.List(codeSession.UserID, typ, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取明细失败: " + err.Error(),
			})
			return
		}
		var out []gin.H
		for _, r := range list {
			out = append(out, gin.H{
				"id":         r.ID,
				"type":       r.Type,
				"amount":     r.Amount,
				"desc":       r.SceneDesc,
				"scene_desc": r.SceneDesc,
				"remark":     r.Remark,
				"created_at": r.CreatedAt,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"list":      out,
				"total":     total,
				"page":      page,
				"page_size": pageSize,
			},
		})
	})

	// 我的方案：汇总（方案数、总下载量）
	r.GET("/user/templates/summary", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}
		if templateModel == nil {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"total_plans":     0,
					"total_views":     0,
					"fans_count":      0,
					"following_count": 0,
					"received_likes":  0,
				},
			})
			return
		}
		totalPlans, totalViews, totalLikes, err := templateModel.SummaryByCreatorUserID(codeSession.UserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取统计失败: " + err.Error(),
			})
			return
		}
		totalEarnings, monthEarnings := int64(0), int64(0)
		if stoneRecordModel != nil {
			totalEarnings, monthEarnings, _ = stoneRecordModel.TemplateEarningsSummary(codeSession.UserID)
		}
		fansCount, followingCount := int64(0), int64(0)
		if designerFollowModel != nil {
			fansCount, _ = designerFollowModel.CountFollowers(codeSession.UserID)
			followingCount, _ = designerFollowModel.CountFollowing(codeSession.UserID)
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"total_plans":     totalPlans,
				"total_views":     totalViews,
				"total_earnings":  totalEarnings,
				"month_earnings":  monthEarnings,
				"total_works":     totalPlans,
				"total_income":    totalEarnings,
				"month_income":    monthEarnings,
				"fans_count":      fansCount,
				"following_count": followingCount,
				"received_likes":  totalLikes,
			},
		})
	})

	// 我的方案：列表（发布到模板广场的内容，分页、可选分类）
	r.POST("/user/templates", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}
		var req struct {
			Page     int    `json:"page"`
			PageSize int    `json:"page_size"`
			Category string `json:"category"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "参数错误",
			})
			return
		}
		page, pageSize := req.Page, req.PageSize
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 50 {
			pageSize = 20
		}
		offset := (page - 1) * pageSize
		if templateModel == nil {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      []interface{}{},
					"total":     int64(0),
					"page":      page,
					"page_size": pageSize,
				},
			})
			return
		}
		list, err := templateModel.ListByCreatorUserID(codeSession.UserID, req.Category, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取方案列表失败: " + err.Error(),
			})
			return
		}
		total, _ := templateModel.CountByCreatorUserID(codeSession.UserID, req.Category)
		var out []gin.H
		for _, t := range list {
			isFreeInt := 0
			if t.IsFree {
				isFreeInt = 1
			}
			earnings := int64(0)
			if stoneRecordModel != nil {
				earnings, _ = stoneRecordModel.EarningsByTemplateID(codeSession.UserID, t.ID)
			}
			out = append(out, gin.H{
				"id":             t.ID,
				"name":           t.Name,
				"category":       t.Category,
				"description":    t.Description,
				"thumbnail":      t.Thumbnail,
				"preview_url":    t.PreviewURL,
				"download_count": t.DownloadCount,
				"like_count":     t.LikeCount,
				"status":         t.Status,
				"publish_scope":  t.PublishScope,
				"reject_reason":  t.RejectReason,
				"source_type":    t.SourceType,
				"main_tab":       t.MainTab,
				"sub_tab":        t.SubTab,
				"third_tab":      t.ThirdTab,
				"creator":        t.Creator,
				"is_free":        isFreeInt,
				"price":          t.Price,
				"earnings":       earnings,
				"created_at":     t.CreatedAt,
				"updated_at":     t.UpdatedAt,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"list":      out,
				"total":     total,
				"page":      page,
				"page_size": pageSize,
			},
		})
	})

	// 我的方案：模板付费收益明细（分页）
	r.POST("/user/templates/income", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		var req struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
		}
		_ = c.ShouldBindJSON(&req)
		page, pageSize := req.Page, req.PageSize
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 50 {
			pageSize = 20
		}
		offset := (page - 1) * pageSize
		if stoneRecordModel == nil {
			c.JSON(http.StatusOK, gin.H{
				"code": 0, "msg": "success",
				"data": gin.H{"list": []interface{}{}, "total": int64(0), "page": page, "page_size": pageSize},
			})
			return
		}
		list, total, err := stoneRecordModel.ListTemplateIncome(codeSession.UserID, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取收益明细失败: " + err.Error()})
			return
		}
		var out []gin.H
		for _, r := range list {
			tid := int64(0)
			if r.TemplateID != nil {
				tid = *r.TemplateID
			}
			out = append(out, gin.H{
				"id":          r.ID,
				"amount":      r.Amount,
				"scene_desc":  r.SceneDesc,
				"remark":      r.Remark,
				"template_id": tid,
				"created_at":  r.CreatedAt,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0, "msg": "success",
			"data": gin.H{"list": out, "total": total, "page": page, "page_size": pageSize},
		})
	})

	r.POST("/user/designer-works", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		if templateModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "服务不可用"})
			return
		}
		var req struct {
			Name         string   `json:"name" binding:"required"`
			Description  string   `json:"description"`
			Category     string   `json:"category"`
			MainTab      string   `json:"main_tab"`
			SubTab       string   `json:"sub_tab"`
			ThirdTab     string   `json:"third_tab"`
			ImageURLs    []string `json:"image_urls" binding:"required,min=1"`
			CoverURL     string   `json:"cover_url"`
			PublishScope string   `json:"publish_scope" binding:"required,oneof=homepage_only square"`
			IsFree       *bool    `json:"is_free"`
			Price        int64    `json:"price"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		cleanImages := make([]string, 0, len(req.ImageURLs))
		for _, item := range req.ImageURLs {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				cleanImages = append(cleanImages, trimmed)
			}
		}
		if len(cleanImages) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请至少上传1张作品图"})
			return
		}
		coverURL := strings.TrimSpace(req.CoverURL)
		if coverURL == "" {
			coverURL = cleanImages[0]
		}
		isFree := true
		if req.IsFree != nil {
			isFree = *req.IsFree
		}
		price := req.Price
		if isFree {
			price = 0
		} else if price <= 0 {
			price = 1
		}
		imagesJSON, _ := json.Marshal(cleanImages)
		category := strings.TrimSpace(req.Category)
		if category == "" {
			if strings.TrimSpace(req.MainTab) != "" {
				category = strings.TrimSpace(req.MainTab)
			} else {
				category = "designer_portfolio"
			}
		}
		template := &model.Template{
			Name:          strings.TrimSpace(req.Name),
			Description:   strings.TrimSpace(req.Description),
			Category:      category,
			MainTab:       strings.TrimSpace(req.MainTab),
			SubTab:        strings.TrimSpace(req.SubTab),
			ThirdTab:      strings.TrimSpace(req.ThirdTab),
			Thumbnail:     coverURL,
			PreviewURL:    coverURL,
			Images:        string(imagesJSON),
			Price:         price,
			IsFree:        isFree,
			Status:        "pending",
			PublishScope:  strings.TrimSpace(req.PublishScope),
			RejectReason:  "",
			SourceType:    "album_upload",
			Creator:       "设计师上传",
			CreatorUserID: codeSession.UserID,
		}
		if err := validateTemplateTabAssignment(sharedTemplateSquareConfigModel, template.MainTab, template.SubTab, template.ThirdTab); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		if err := validateTemplatePrimaryImage(template); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		if err := generateTemplateDerivedImages(context.Background(), template, "templates/album_upload"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "生成模板衍生图失败: " + err.Error()})
			return
		}
		if err := templateModel.Create(template); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "提交作品失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "提交成功，等待审核", "data": gin.H{"id": template.ID}})
	})

	r.GET("/user/designer-center", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		if userDBModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "服务不可用"})
			return
		}

		data, err := buildDesignerCenterPayload(codeSession.UserID, userDBModel, templateModel, userProfileModel, certificationModel, stoneRecordModel, userOrderModel, designerReviewModel)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "用户不存在或已失效，请重新登录"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": data})
	})

	r.GET("/user/templates/detail/:id", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "作品ID无效"})
			return
		}
		if templateModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "服务不可用"})
			return
		}
		tpl, err := templateModel.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "作品不存在或已删除"})
			return
		}
		if tpl.CreatorUserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "只能查看自己发布的作品"})
			return
		}
		imageURLs := make([]string, 0)
		if strings.TrimSpace(tpl.Images) != "" {
			_ = json.Unmarshal([]byte(tpl.Images), &imageURLs)
		}
		if len(imageURLs) == 0 {
			if strings.TrimSpace(tpl.PreviewURL) != "" {
				imageURLs = append(imageURLs, tpl.PreviewURL)
			} else if strings.TrimSpace(tpl.Thumbnail) != "" {
				imageURLs = append(imageURLs, tpl.Thumbnail)
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"id":             tpl.ID,
				"name":           tpl.Name,
				"category":       tpl.Category,
				"description":    tpl.Description,
				"thumbnail":      tpl.Thumbnail,
				"preview_url":    tpl.PreviewURL,
				"image_urls":     imageURLs,
				"publish_scope":  tpl.PublishScope,
				"main_tab":       tpl.MainTab,
				"sub_tab":        tpl.SubTab,
				"third_tab":      tpl.ThirdTab,
				"is_free":        tpl.IsFree,
				"price":          tpl.Price,
				"status":         tpl.Status,
				"reject_reason":  tpl.RejectReason,
				"source_type":    tpl.SourceType,
				"download_count": tpl.DownloadCount,
				"like_count":     tpl.LikeCount,
				"created_at":     tpl.CreatedAt,
				"updated_at":     tpl.UpdatedAt,
			},
		})
	})

	r.POST("/user/templates/batch-delete", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}
		if templateModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "服务不可用",
			})
			return
		}

		var req struct {
			IDs []int64 `json:"ids" binding:"required,min=1"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "请选择要删除的作品",
			})
			return
		}
		if len(req.IDs) > 100 {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "单次最多删除100个作品",
			})
			return
		}

		seen := make(map[int64]struct{}, len(req.IDs))
		ids := make([]int64, 0, len(req.IDs))
		for _, id := range req.IDs {
			if id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "作品ID无效",
				})
				return
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "请选择要删除的作品",
			})
			return
		}

		for _, id := range ids {
			tpl, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "作品不存在或已删除",
				})
				return
			}
			if tpl.CreatorUserID != codeSession.UserID {
				c.JSON(http.StatusForbidden, gin.H{
					"code": 403,
					"msg":  "只能删除自己发布的作品",
				})
				return
			}
		}

		deletedCount, err := templateModel.BatchDeleteByCreatorUserID(codeSession.UserID, ids)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "批量删除失败: " + err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "删除成功",
			"data": gin.H{
				"deleted_count": deletedCount,
			},
		})
	})

	// 我的方案：更新模板信息（仅能编辑自己发布的，允许修改名称、描述、分类与收费信息）
	r.PUT("/user/templates/:id", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}

		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "无效的模板ID",
			})
			return
		}
		if templateModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "服务不可用",
			})
			return
		}

		// 仅允许部分字段可选更新
		var req struct {
			Name         *string  `json:"name"`
			Description  *string  `json:"description"`
			Category     *string  `json:"category"`
			MainTab      *string  `json:"main_tab"`
			SubTab       *string  `json:"sub_tab"`
			ThirdTab     *string  `json:"third_tab"`
			ImageURLs    []string `json:"image_urls"`
			CoverURL     *string  `json:"cover_url"`
			PublishScope *string  `json:"publish_scope"`
			IsFree       *bool    `json:"is_free"`
			Price        *int64   `json:"price"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "参数错误: " + err.Error(),
			})
			return
		}

		tpl, err := templateModel.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"code": 404,
				"msg":  "模板不存在",
			})
			return
		}
		// 只能编辑自己发布的模板
		if tpl.CreatorUserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{
				"code": 403,
				"msg":  "只能编辑自己发布的方案",
			})
			return
		}

		// 更新可修改字段
		if req.Name != nil {
			tpl.Name = *req.Name
		}
		if req.Category != nil {
			tpl.Category = *req.Category
		}
		if req.MainTab != nil {
			tpl.MainTab = *req.MainTab
		}
		if req.SubTab != nil {
			tpl.SubTab = *req.SubTab
		}
		if req.ThirdTab != nil {
			tpl.ThirdTab = *req.ThirdTab
		}
		if req.PublishScope != nil {
			publishScope := strings.TrimSpace(*req.PublishScope)
			if publishScope != "homepage_only" && publishScope != "square" {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "发布范围无效",
				})
				return
			}
			tpl.PublishScope = publishScope
		}
		if err := validateTemplateTabAssignment(sharedTemplateSquareConfigModel, tpl.MainTab, tpl.SubTab, tpl.ThirdTab); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  err.Error(),
			})
			return
		}
		if req.Description != nil {
			// Description 中可能包含「提示词: xxx」，这里只替换描述部分，保留原有提示词
			tpl.Description = strings.TrimSpace(*req.Description)
		}
		if req.ImageURLs != nil {
			cleanImages := make([]string, 0, len(req.ImageURLs))
			for _, item := range req.ImageURLs {
				trimmed := strings.TrimSpace(item)
				if trimmed != "" {
					cleanImages = append(cleanImages, trimmed)
				}
			}
			if len(cleanImages) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "请至少保留一张作品图",
				})
				return
			}
			coverURL := ""
			if req.CoverURL != nil {
				coverURL = strings.TrimSpace(*req.CoverURL)
			}
			if coverURL == "" {
				coverURL = cleanImages[0]
			}
			imagesJSON, _ := json.Marshal(cleanImages)
			tpl.Thumbnail = coverURL
			tpl.PreviewURL = coverURL
			tpl.Images = string(imagesJSON)
			if err := validateTemplatePrimaryImage(tpl); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  err.Error(),
				})
				return
			}
			if err := generateTemplateDerivedImages(context.Background(), tpl, "templates/album_upload"); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "生成模板衍生图失败: " + err.Error(),
				})
				return
			}
		}
		if req.IsFree != nil {
			tpl.IsFree = *req.IsFree
			if tpl.IsFree {
				tpl.Price = 0
			}
		}
		if req.Price != nil && !tpl.IsFree {
			price := *req.Price
			if price <= 0 {
				price = 1
			}
			tpl.Price = price
		}
		tpl.Status = "pending"
		tpl.RejectReason = ""

		if err := templateModel.Update(tpl); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "更新失败: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "更新成功",
			"data": gin.H{
				"id":          tpl.ID,
				"name":        tpl.Name,
				"category":    tpl.Category,
				"description": tpl.Description,
				"thumbnail":   tpl.Thumbnail,
				"preview_url": tpl.PreviewURL,
				"is_free":     tpl.IsFree,
				"price":       tpl.Price,
				"status":      tpl.Status,
			},
		})
	})

	// 我的方案：删除（仅能删除自己发布的）
	r.DELETE("/user/templates/:id", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "无效的模板ID",
			})
			return
		}
		if templateModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "服务不可用",
			})
			return
		}
		tpl, err := templateModel.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"code": 404,
				"msg":  "模板不存在",
			})
			return
		}
		if tpl.CreatorUserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{
				"code": 403,
				"msg":  "只能删除自己发布的方案",
			})
			return
		}
		if err := templateModel.Delete(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "删除失败: " + err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "删除成功",
		})
	})

	// 订单管理：汇总（总订单数、总金额、本月订单数、本月金额）
	r.GET("/user/orders/summary", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}
		totalOrders, totalAmount, monthOrders, monthAmount := int64(0), int64(0), int64(0), int64(0)
		if userOrderModel != nil {
			var err error
			totalOrders, totalAmount, monthOrders, monthAmount, err = userOrderModel.Summary(codeSession.UserID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取订单统计失败: " + err.Error(),
				})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"total_orders": totalOrders,
				"total_amount": totalAmount,
				"month_orders": monthOrders,
				"month_amount": monthAmount,
			},
		})
	})

	// 订单管理：列表（分页，可选类型：recharge/consume/culture/withdraw）
	r.POST("/user/orders", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "未通过token验证",
			})
			return
		}
		var req struct {
			Page     int    `json:"page"`
			PageSize int    `json:"page_size"`
			Type     string `json:"type"` // all/recharge/consume/culture/withdraw
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "参数错误",
			})
			return
		}
		page, pageSize := req.Page, req.PageSize
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}
		offset := (page - 1) * pageSize
		typ := req.Type
		if typ == "" {
			typ = "all"
		}
		if userOrderModel == nil {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      []interface{}{},
					"total":     0,
					"page":      page,
					"page_size": pageSize,
				},
			})
			return
		}
		list, total, err := userOrderModel.List(codeSession.UserID, typ, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取订单列表失败: " + err.Error(),
			})
			return
		}
		var out []gin.H
		for _, o := range list {
			templateID := int64(0)
			if o.TemplateID != nil {
				templateID = *o.TemplateID
			}
			canCancel := o.Status == "pending" && o.Type == "recharge"
			canContinuePay := o.OrderCategory == "certification" && (o.Status == "pending" || o.Status == "failed")
			canDelete := o.Status != "pending" && !canContinuePay
			out = append(out, gin.H{
				"id":               o.ID,
				"order_no":         o.OrderNo,
				"designer_user_id": o.DesignerUserID,
				"template_id":      templateID,
				"type":             o.Type,
				"order_category":   o.OrderCategory,
				"title":            o.Title,
				"description":      o.Description,
				"amount":           o.Amount,
				"status":           o.Status,
				"review_status":    o.ReviewStatus,
				"can_cancel":       canCancel,
				"can_continue_pay": canContinuePay,
				"can_delete":       canDelete,
				"completed_at":     o.CompletedAt,
				"created_at":       o.CreatedAt,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"list":      out,
				"total":     total,
				"page":      page,
				"page_size": pageSize,
			},
		})
	})

	r.GET("/user/orders/:id", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		if userOrderModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "订单服务不可用"})
			return
		}

		orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的订单ID"})
			return
		}

		order, err := userOrderModel.GetByID(orderID)
		if err != nil || order == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "订单不存在"})
			return
		}
		if order.UserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权查看该订单"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"id":               order.ID,
				"order_no":         order.OrderNo,
				"designer_user_id": order.DesignerUserID,
				"template_id": func() int64 {
					if order.TemplateID != nil {
						return *order.TemplateID
					}
					return 0
				}(),
				"type":             order.Type,
				"order_category":   order.OrderCategory,
				"title":            order.Title,
				"description":      order.Description,
				"amount":           order.Amount,
				"status":           order.Status,
				"review_status":    order.ReviewStatus,
				"can_cancel":       order.Status == "pending" && order.Type == "recharge",
				"can_continue_pay": order.OrderCategory == "certification" && (order.Status == "pending" || order.Status == "failed"),
				"can_delete":       order.Status != "pending" && !(order.OrderCategory == "certification" && (order.Status == "pending" || order.Status == "failed")),
				"completed_at":     order.CompletedAt,
				"created_at":       order.CreatedAt,
			},
		})
	})

	r.DELETE("/user/orders/:id", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		if userOrderModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "订单服务不可用"})
			return
		}

		orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的订单ID"})
			return
		}

		order, err := userOrderModel.GetByID(orderID)
		if err != nil || order == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "订单不存在"})
			return
		}
		if order.UserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权删除该订单"})
			return
		}
		if order.Status == "pending" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "处理中订单暂不支持删除"})
			return
		}
		if order.OrderCategory == "certification" && (order.Status == "pending" || order.Status == "failed") {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请先处理认证支付订单后再删除"})
			return
		}

		if err := userOrderModel.HideByID(codeSession.UserID, orderID); err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "订单不存在或已删除"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除订单记录失败: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功", "data": gin.H{"id": orderID}})
	})

	r.POST("/user/orders/:id/cancel", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		if userOrderModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "订单服务不可用"})
			return
		}

		orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的订单ID"})
			return
		}

		order, err := userOrderModel.GetByID(orderID)
		if err != nil || order == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "订单不存在"})
			return
		}
		if order.UserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权取消该订单"})
			return
		}
		if order.Status != "pending" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前订单状态不可取消"})
			return
		}
		if order.Type != "recharge" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前订单暂不支持取消"})
			return
		}

		if err := userOrderModel.CancelPendingByID(codeSession.UserID, orderID); err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前订单状态不可取消"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "取消订单失败: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "订单已取消", "data": gin.H{"id": orderID, "status": "cancelled", "can_cancel": false}})
	})

	r.POST("/user/orders/:id/review", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		if userOrderModel == nil || designerReviewModel == nil || userDBModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "服务不可用"})
			return
		}

		orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的订单ID"})
			return
		}

		var req struct {
			Rating  int    `json:"rating" binding:"required,min=1,max=5"`
			Content string `json:"content" binding:"required,max=1024"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}

		order, err := userOrderModel.GetByID(orderID)
		if err != nil || order == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "订单不存在"})
			return
		}
		if order.UserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "不能评价别人的订单"})
			return
		}
		if order.Status != "success" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "订单未完成，暂不能评价"})
			return
		}
		if order.DesignerUserID <= 0 || order.DesignerUserID == codeSession.UserID {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "该订单无需评价"})
			return
		}
		if order.ReviewStatus == "reviewed" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "该订单已评价"})
			return
		}
		if _, err := designerReviewModel.GetByOrderID(order.ID); err == nil {
			_ = userOrderModel.UpdateReviewStatus(order.ID, "reviewed")
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "该订单已评价"})
			return
		}

		reviewer, err := userDBModel.GetByID(codeSession.UserID)
		if err != nil || reviewer == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "用户不存在或已失效，请重新登录"})
			return
		}
		reviewerName := strings.TrimSpace(reviewer.Username)
		reviewerAvatar := ""
		if userProfileModel != nil {
			if reviewerProfile, err := userProfileModel.GetOrCreate(codeSession.UserID, ""); err == nil && reviewerProfile != nil {
				if strings.TrimSpace(reviewerProfile.Nickname) != "" {
					reviewerName = strings.TrimSpace(reviewerProfile.Nickname)
				}
				reviewerAvatar = sanitizePublicImageURL(reviewerProfile.Avatar)
			}
		}

		review := &model.DesignerReview{
			OrderID:        order.ID,
			OrderNo:        order.OrderNo,
			DesignerUserID: order.DesignerUserID,
			ReviewerUserID: codeSession.UserID,
			ReviewerName:   reviewerName,
			ReviewerAvatar: reviewerAvatar,
			Rating:         req.Rating,
			Content:        strings.TrimSpace(req.Content),
			Sentiment:      reviewSentimentFromRating(req.Rating),
		}
		if err := designerReviewModel.Create(review); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "写入评价失败: " + err.Error()})
			return
		}
		if err := userOrderModel.UpdateReviewStatus(order.ID, "reviewed"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新订单评价状态失败: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "评价成功",
			"data": gin.H{
				"order_id":      order.ID,
				"review_status": "reviewed",
			},
		})
	})

	// 邀请好友：汇总信息（邀请码由后端生成 6 位唯一码、已邀请人数、总奖励、本月奖励）
	r.GET("/user/invite/info", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		uid := codeSession.UserID
		inviteCode := strconv.FormatInt(uid, 10)
		if userInviteCodeModel != nil {
			if code, err := userInviteCodeModel.GetOrCreateForUser(uid); err == nil && code != "" {
				inviteCode = code
			}
		}
		inviteCount := int64(0)
		totalReward := int64(0)
		monthReward := int64(0)
		if inviteRelationModel != nil {
			inviteCount, _ = inviteRelationModel.CountByInviter(uid)
		}
		if stoneRecordModel != nil {
			totalReward, monthReward, _ = stoneRecordModel.InviteRewardSummary(uid)
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"invite_code":  inviteCode,
				"invite_count": inviteCount,
				"total_reward": totalReward,
				"month_reward": monthReward,
			},
		})
	})

	// 邀请好友：输入他人邀请码核销绑定（未绑定过邀请人时可绑定，双方各得 50 灵石）
	r.POST("/user/invite/bind", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		var req struct {
			InviteCode string `json:"invite_code" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请填写邀请码"})
			return
		}
		inviteCode := strings.TrimSpace(req.InviteCode)
		if inviteCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "邀请码不能为空"})
			return
		}
		inviteeID := codeSession.UserID
		hasInviter, _ := inviteRelationModel.HasInviter(inviteeID)
		if hasInviter {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "您已绑定过邀请人，无法重复绑定", "data": gin.H{"bound": true}})
			return
		}
		var inviterID int64
		var ok bool
		if userInviteCodeModel != nil {
			inviterID, ok = userInviteCodeModel.ResolveInviteCodeToUserID(inviteCode)
		} else {
			inviterID, ok = model.ParseInviteCodeToUserID(inviteCode)
		}
		if !ok || inviterID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "邀请码无效"})
			return
		}
		if inviterID == inviteeID {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "不能绑定自己的邀请码"})
			return
		}
		if userDBModel == nil || inviteRelationModel == nil || userModel == nil || stoneRecordModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "服务暂不可用"})
			return
		}
		inviter, err := userDBModel.GetByID(inviterID)
		if err != nil || inviter == nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "邀请码无效"})
			return
		}
		if err := inviteRelationModel.Create(inviterID, inviteeID); err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "绑定失败，您可能已绑定过邀请人", "data": gin.H{"bound": true}})
			return
		}
		userModel.AddStones(inviterID, 50)
		userModel.AddStones(inviteeID, 50)
		stoneRecordModel.Create(inviterID, "invite", 50, "邀请好友奖励", "")
		stoneRecordModel.Create(inviteeID, "invite", 50, "被邀请注册奖励", "")
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "绑定成功，您与邀请人各获得 50 灵石",
			"data": gin.H{"bound": true},
		})
	})

	// 邀请好友：生成邀请海报用的小程序码图片，scene 为 6 位邀请码，上传到 OSS 并返回 URL
	r.GET("/user/invite/qrcode", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		uid := codeSession.UserID
		inviteCode := strconv.FormatInt(uid, 10)
		if userInviteCodeModel != nil {
			if code, err := userInviteCodeModel.GetOrCreateForUser(uid); err == nil && code != "" {
				inviteCode = code
			}
		}
		cfg := config.Get()
		if cfg.Wechat.AppID == "" || cfg.Wechat.AppSecret == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "微信小程序未配置"})
			return
		}
		accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取微信 access_token 失败: " + err.Error()})
			return
		}
		// scene 传 6 位邀请码，扫码后小程序可从启动参数获取并完成绑定
		imageData, err := function.GetWxacodeUnlimit(accessToken, inviteCode, "pages/index/index", 280)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "生成小程序码失败: " + err.Error()})
			return
		}
		cosClient := component.GetCOSClient()
		if cosClient == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "存储未初始化"})
			return
		}
		now := time.Now()
		objectKey := path.Join("invite_qrcode", now.Format("2006/01/02"), "uid_"+strconv.FormatInt(uid, 10)+"_"+inviteCode+".png")
		qrcodeURL, err := function.UploadBytes(c.Request.Context(), cosClient, cfg, objectKey, imageData, "image/png")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "上传小程序码失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"qrcode_url":  qrcodeURL,
				"invite_code": inviteCode,
			},
		})
	})

	// 邀请好友：后端生成完整邀请海报图，上传 OSS 并返回海报 URL（邀请码为 6 位唯一码）
	r.GET("/user/invite/poster", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		uid := codeSession.UserID
		inviteCode := strconv.FormatInt(uid, 10)
		if userInviteCodeModel != nil {
			if code, err := userInviteCodeModel.GetOrCreateForUser(uid); err == nil && code != "" {
				inviteCode = code
			}
		}
		nickname := ""
		if userProfileModel != nil {
			if profile, err := userProfileModel.GetByUserID(uid); err == nil && profile != nil {
				nickname = profile.Nickname
			}
		}
		cfg := config.Get()
		if cfg.Wechat.AppID == "" || cfg.Wechat.AppSecret == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "微信小程序未配置"})
			return
		}
		accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取微信 access_token 失败: " + err.Error()})
			return
		}
		qrcodeImage, err := function.GetWxacodeUnlimit(accessToken, inviteCode, "pages/index/index", 280)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "生成小程序码失败: " + err.Error()})
			return
		}
		svgContent, err := component.BuildInvitePosterFromSVG(inviteCode, nickname, qrcodeImage)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "生成海报失败: " + err.Error()})
			return
		}
		// 返回 SVG 的 data URL，由前端用 canvas 转为 PNG 再保存/分享
		posterSvgDataURL := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(svgContent)
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"poster_svg":  posterSvgDataURL,
				"invite_code": inviteCode,
			},
		})
	})

	// 邀请好友：邀请记录列表
	r.GET("/user/invite/records", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}
		offset := (page - 1) * pageSize
		if inviteRelationModel == nil || userDBModel == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}, "total": 0}})
			return
		}
		list, total, err := inviteRelationModel.ListByInviter(codeSession.UserID, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取邀请记录失败: " + err.Error()})
			return
		}
		var out []gin.H
		for _, r := range list {
			name := ""
			if u, err := userDBModel.GetByID(r.InviteeUserID); err == nil && u != nil {
				name = u.Username
			}
			reward := int64(50) // 邀请注册奖励
			out = append(out, gin.H{
				"id":      r.ID,
				"user_id": r.InviteeUserID,
				"name":    name,
				"desc":    "已注册",
				"reward":  reward,
				"time":    r.InvitedAt,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{"list": out, "total": total},
		})
	})

	// 充值（灵石）
	r.POST("/user/recharge", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		var req struct {
			Amount int64 `json:"amount" binding:"required,min=1"` // 充值灵石数量
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
			return
		}
		userID := codeSession.UserID
		amount := req.Amount
		if amount <= 0 || amount%10 != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "充值金额需为整数元，按 1 元 = 10 灵石换算"})
			return
		}
		if userModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "服务不可用"})
			return
		}
		if err := userModel.AddStones(userID, amount); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "充值失败: " + err.Error()})
			return
		}
		if stoneRecordModel != nil {
			_ = stoneRecordModel.Create(userID, "recharge", amount, "充值", "")
		}
		if userOrderModel != nil {
			orderNo := model.GenerateOrderNo("ORD")
			_ = userOrderModel.Create(userID, orderNo, "recharge", amount, "success", "充值", "")
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "充值成功",
			"data": gin.H{"amount": amount},
		})
	})

	// 获取用户任务历史（含 AI 绘画与 AI 生成视频，按创建时间倒序合并分页）
	r.GET("/user/tasks", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}

		// 各取 page*pageSize 条，合并后取当前页
		limit := page * pageSize
		tasks, err := taskModel.GetByUserID(codeSession.UserID, limit, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取任务列表失败: " + err.Error()})
			return
		}
		videoTasks, err := videoTaskModel.GetByUserID(codeSession.UserID, limit, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取视频任务列表失败: " + err.Error()})
			return
		}
		totalTasks, _ := taskModel.CountByUserID(codeSession.UserID)
		totalVideos, _ := videoTaskModel.CountByUserID(codeSession.UserID)
		total := totalTasks + totalVideos

		type rowWithTime struct {
			createdAt time.Time
			row       gin.H
		}
		var rows []rowWithTime
		for _, task := range tasks {
			rows = append(rows, rowWithTime{
				createdAt: task.CreatedAt,
				row:       buildAITaskResponseData(task, aiToolModel),
			})
		}
		for _, v := range videoTasks {
			StartAIVideoTaskMonitor(v.ID, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg)
			if syncedTask, syncErr := syncAIVideoTask(v, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg); syncErr == nil && syncedTask != nil {
				v = syncedTask
			}
			resultData := gin.H{}
			if v.OSSURL != "" {
				resultData["url"] = v.OSSURL
			}
			rows = append(rows, rowWithTime{
				createdAt: v.CreatedAt,
				row: gin.H{
					"id": v.ID, "task_no": "v" + strconv.FormatInt(v.ID, 10), "scene": "ai_video",
					"status": model.AIVideoStatusForUserWithResult(v.Status, strings.TrimSpace(v.OSSURL) != ""), "stones_used": getVideoStones(pricingModel, v.SegmentCount), "result": resultData,
					"error_message": v.GetErrorMessage(), "raw_error_message": v.GetRawErrorMessage(), "created_at": v.CreatedAt, "updated_at": v.UpdatedAt,
					"reference_image_url": "", "reference_image_urls": []string{}, "prompt": v.Prompt, "user_prompt": v.Prompt, "task_type": "ai_video",
				},
			})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].createdAt.After(rows[j].createdAt) })
		start := (page - 1) * pageSize
		if start > len(rows) {
			start = len(rows)
		}
		end := start + pageSize
		if end > len(rows) {
			end = len(rows)
		}
		var taskList []gin.H
		for i := start; i < end; i++ {
			taskList = append(taskList, rows[i].row)
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"tasks":     taskList,
				"total":     total,
				"page":      page,
				"page_size": pageSize,
			},
		})
	})

	// 删除用户任务（生成历史中删除）。task_no 为 "v"+id 表示删除 AI 视频任务
	r.DELETE("/user/tasks/:task_no", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		taskNo := c.Param("task_no")
		if taskNo == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "任务编号不能为空"})
			return
		}
		if strings.HasPrefix(taskNo, "v") {
			idStr := strings.TrimPrefix(taskNo, "v")
			videoID, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || videoID <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的视频任务编号"})
				return
			}
			videoTask, err := videoTaskModel.GetByIDAndUserID(videoID, codeSession.UserID)
			if err != nil || videoTask == nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在"})
				return
			}
			if err := videoTaskModel.DeleteByID(videoID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
			return
		}
		task, err := taskModel.GetByTaskNo(taskNo)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在"})
			return
		}
		if task.UserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "只能删除自己的任务"})
			return
		}
		if err := taskModel.DeleteByTaskNo(taskNo); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
	})

	r.GET("/user/ai/download-config", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		cfg := config.Get()
		tip := strings.TrimSpace(cfg.EnterpriseWechat.Download4KTip)
		if tip == "" {
			tip = "完成手机号授权验证后，可下载保存高清原图。"
		}
		verified := false
		verifiedAt := ""
		contact := ""
		servicePhone := strings.TrimSpace(cfg.EnterpriseWechat.ServicePhone)
		customerServiceCorpID := strings.TrimSpace(cfg.EnterpriseWechat.CustomerServiceCorpID)
		customerServiceURL := strings.TrimSpace(cfg.EnterpriseWechat.CustomerServiceURL)
		bindTicket := ""
		bindStatus := "pending"
		qrcodeURL := strings.TrimSpace(cfg.EnterpriseWechat.Download4KQRCodeURL)
		if userProfileModel != nil {
			if profile, err := userProfileModel.GetOrCreate(codeSession.UserID, ""); err == nil && profile != nil {
				contact = strings.TrimSpace(profile.EnterpriseWechatContact)
				if !hasValidEnterpriseWechatContact(contact) {
					contact = ""
				}
				verified = profile.EnterpriseWechatVerified && hasValidEnterpriseWechatContact(contact)
				if verified && profile.EnterpriseWechatVerifiedAt != nil {
					verifiedAt = profile.EnterpriseWechatVerifiedAt.Format("2006-01-02 15:04")
				}
			}
		}
		if enterpriseWechatBindTicketModel != nil {
			if ticketItem, err := getOrCreateEnterpriseWechatBindTicket(enterpriseWechatBindTicketModel, codeSession.UserID, "ai_download", "", 0); err == nil && ticketItem != nil {
				bindTicket = ticketItem.Ticket
				bindStatus = ticketItem.Status
				qrcodeURL = buildEnterpriseWechatQRCodeURL(qrcodeURL, bindTicket)
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"download_2k_mode":                           "watermarked",
				"download_4k_mode":                           "enterprise_wechat",
				"enterprise_wechat_qrcode_url":               qrcodeURL,
				"enterprise_wechat_tip":                      tip,
				"enterprise_wechat_service_phone":            servicePhone,
				"enterprise_wechat_customer_service_corp_id": customerServiceCorpID,
				"enterprise_wechat_customer_service_url":     customerServiceURL,
				"enterprise_wechat_verified":                 verified,
				"enterprise_wechat_verified_at":              verifiedAt,
				"enterprise_wechat_contact":                  contact,
				"enterprise_wechat_bind_ticket":              bindTicket,
				"enterprise_wechat_bind_status":              bindStatus,
			},
		})
	})
	r.POST("/user/ai/wecom/verify-phone", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		var req struct {
			Code   string `json:"code" binding:"required"`
			Ticket string `json:"ticket"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		if userProfileModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户扩展信息服务不可用"})
			return
		}
		cfg := config.Get()
		if strings.TrimSpace(cfg.Wechat.AppID) == "" || strings.TrimSpace(cfg.Wechat.AppSecret) == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "微信小程序未配置"})
			return
		}
		accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取微信 access_token 失败: " + err.Error()})
			return
		}
		contact, err := function.GetUserPhoneNumber(accessToken, strings.TrimSpace(req.Code))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "获取手机号失败: " + err.Error()})
			return
		}
		contact = strings.TrimSpace(contact)
		if !hasValidEnterpriseWechatContact(contact) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "未获取到有效手机号"})
			return
		}
		if err := userProfileModel.SetEnterpriseWechatVerification(codeSession.UserID, true, contact); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新用户手机号授权状态失败: " + err.Error()})
			return
		}
		bindStatus := "verified"
		if enterpriseWechatBindTicketModel != nil {
			trimmedTicket := strings.TrimSpace(req.Ticket)
			if trimmedTicket != "" {
				ticketItem, ticketErr := enterpriseWechatBindTicketModel.GetByTicket(trimmedTicket)
				if ticketErr != nil || ticketItem == nil {
					c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "绑定票据不存在"})
					return
				}
				if ticketItem.UserID != codeSession.UserID {
					c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权操作此验证票据"})
					return
				}
				if err := enterpriseWechatBindTicketModel.MarkVerified(ticketItem.Ticket, contact, strings.TrimSpace(ticketItem.ExternalUserID)); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新绑定票据状态失败: " + err.Error()})
					return
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"enterprise_wechat_verified":    true,
				"enterprise_wechat_contact":     contact,
				"enterprise_wechat_verified_at": time.Now().Format("2006-01-02 15:04"),
				"enterprise_wechat_bind_status": bindStatus,
			},
		})
	})

	// 下载原图（无水印）：需先完成手机号授权验证
	r.POST("/user/ai/task/download-original", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		var req struct {
			TaskNo     string `json:"task_no" binding:"required"`
			ImageIndex int    `json:"image_index"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		task, err := taskModel.GetByTaskNo(req.TaskNo)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在"})
			return
		}
		if task.UserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问此任务"})
			return
		}
		if task.Status != "success" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "任务未完成，无法下载原图"})
			return
		}
		if userProfileModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户扩展信息服务不可用"})
			return
		}
		profile, err := userProfileModel.GetOrCreate(codeSession.UserID, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取手机号授权验证状态失败"})
			return
		}
		if profile == nil || !profile.EnterpriseWechatVerified || !hasValidEnterpriseWechatContact(profile.EnterpriseWechatContact) {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "请先完成手机号授权验证后再下载保存"})
			return
		}
		resultStr := task.GetResultPayload()
		if resultStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无结果数据"})
			return
		}
		var resultPayload map[string]interface{}
		if err := json.Unmarshal([]byte(resultStr), &resultPayload); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析结果失败"})
			return
		}
		urlRaw := ""
		if rawImages, ok := resultPayload["raw_images"].([]interface{}); ok && len(rawImages) > 0 {
			index := req.ImageIndex
			if index < 0 {
				index = 0
			}
			if index >= len(rawImages) {
				index = 0
			}
			if item, ok := rawImages[index].(string); ok {
				urlRaw = strings.TrimSpace(item)
			}
		}
		if urlRaw == "" {
			urlRaw, _ = resultPayload["url_raw"].(string)
		}
		if urlRaw == "" {
			urlRaw, _ = resultPayload["url"].(string)
		}
		if urlRaw == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无原图地址"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"url": urlRaw}})
	})
	r.GET("/user/ai/task/download-original-file", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		taskNo := strings.TrimSpace(c.Query("task_no"))
		if taskNo == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "任务编号不能为空"})
			return
		}
		imageIndex, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("image_index", "0")))
		task, err := taskModel.GetByTaskNo(taskNo)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在"})
			return
		}
		if task.UserID != codeSession.UserID {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "无权访问此任务"})
			return
		}
		if task.Status != "success" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "任务未完成，无法下载原图"})
			return
		}
		if userProfileModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户扩展信息服务不可用"})
			return
		}
		profile, err := userProfileModel.GetOrCreate(codeSession.UserID, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取手机号授权验证状态失败"})
			return
		}
		if profile == nil || !profile.EnterpriseWechatVerified || !hasValidEnterpriseWechatContact(profile.EnterpriseWechatContact) {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "请先完成手机号授权验证后再下载保存"})
			return
		}
		resultStr := task.GetResultPayload()
		if resultStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无结果数据"})
			return
		}
		var resultPayload map[string]interface{}
		if err := json.Unmarshal([]byte(resultStr), &resultPayload); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析结果失败"})
			return
		}
		urlRaw := ""
		if rawImages, ok := resultPayload["raw_images"].([]interface{}); ok && len(rawImages) > 0 {
			if imageIndex < 0 {
				imageIndex = 0
			}
			if imageIndex >= len(rawImages) {
				imageIndex = 0
			}
			if item, ok := rawImages[imageIndex].(string); ok {
				urlRaw = strings.TrimSpace(item)
			}
		}
		if urlRaw == "" {
			urlRaw, _ = resultPayload["url_raw"].(string)
		}
		if urlRaw == "" {
			urlRaw, _ = resultPayload["url"].(string)
		}
		if urlRaw == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无原图地址"})
			return
		}
		proxyRemoteDownload(c, urlRaw, "generated-image.png", "image/png")
	})

	// 下载视频：需先完成手机号授权验证，再由后端统一放行下载地址
	r.POST("/user/ai/video/download-original", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		var req struct {
			TaskNo string `json:"task_no" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		if videoTaskModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "视频任务服务不可用"})
			return
		}
		videoID, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(req.TaskNo), "v"), 10, 64)
		if err != nil || videoID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的视频任务编号"})
			return
		}
		task, err := videoTaskModel.GetByIDAndUserID(videoID, codeSession.UserID)
		if err != nil || task == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "视频任务不存在"})
			return
		}
		if task.Status != "completed" || strings.TrimSpace(task.OSSURL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "视频尚未生成完成，无法下载"})
			return
		}
		if userProfileModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户扩展信息服务不可用"})
			return
		}
		profile, err := userProfileModel.GetOrCreate(codeSession.UserID, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取手机号授权验证状态失败"})
			return
		}
		if profile == nil || !profile.EnterpriseWechatVerified || !hasValidEnterpriseWechatContact(profile.EnterpriseWechatContact) {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "请先完成手机号授权验证后再下载保存"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"url": strings.TrimSpace(task.OSSURL)}})
	})
	r.GET("/user/ai/video/download-original-file", simpleTokenAuth, func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		taskNo := strings.TrimSpace(c.Query("task_no"))
		videoID, err := strconv.ParseInt(strings.TrimPrefix(taskNo, "v"), 10, 64)
		if err != nil || videoID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的视频任务编号"})
			return
		}
		if videoTaskModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "视频任务服务不可用"})
			return
		}
		task, err := videoTaskModel.GetByIDAndUserID(videoID, codeSession.UserID)
		if err != nil || task == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "视频任务不存在"})
			return
		}
		if task.Status != "completed" || strings.TrimSpace(task.OSSURL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "视频尚未生成完成，无法下载"})
			return
		}
		if userProfileModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户扩展信息服务不可用"})
			return
		}
		profile, err := userProfileModel.GetOrCreate(codeSession.UserID, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取手机号授权验证状态失败"})
			return
		}
		if profile == nil || !profile.EnterpriseWechatVerified || !hasValidEnterpriseWechatContact(profile.EnterpriseWechatContact) {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "请先完成手机号授权验证后再下载保存"})
			return
		}
		proxyRemoteDownload(c, strings.TrimSpace(task.OSSURL), "generated-video.mp4", "video/mp4")
	})

	// 登出
	r.POST("/logout", AuthRequired, func(c *gin.Context) {
		if err := ClearUserSession(c); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "登出失败",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "已登出",
		})
	})

	// 测试接口：验证参数计算（仅开发环境）
	r.POST("/test-params", func(c *gin.Context) {
		// 检查是否为开发环境
		cfg := config.Get()
		if !cfg.IsDevelopment() {
			c.JSON(http.StatusNotFound, gin.H{
				"code": 404,
				"msg":  "接口不存在",
			})
			return
		}

		// 从请求头读取参数
		token := c.GetHeader("token")
		tokenSignature := c.GetHeader("token-signature")
		sin := c.GetHeader("sin")
		md5Signature := c.GetHeader("md5-signature")
		pass := c.GetHeader("pass")
		tm := c.GetHeader("tm")

		// 从请求体读取数据
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "读取请求体失败: " + err.Error(),
			})
			return
		}

		// 获取接口地址
		apiPath := c.GetHeader("X-Test-Api-Path")
		if apiPath == "" {
			apiPath = c.Request.URL.Path
			if c.Request.URL.RawQuery != "" {
				apiPath += "?" + c.Request.URL.RawQuery
			}
		}

		// 验证结果
		results := gin.H{
			"token":           token,
			"token_signature": tokenSignature,
			"sin":             sin,
			"md5_signature":   md5Signature,
			"pass":            pass,
			"tm":              tm,
			"api_path":        apiPath,
			"request_body":    string(bodyBytes),
		}

		// 验证 token-signature
		if token == "" || tokenSignature == "" {
			results["token_signature_valid"] = false
			results["token_signature_error"] = "缺少token或token-signature"
		} else {
			tokenSigValid := function.VerifyTokenSignature(token, tokenSignature)
			results["token_signature_valid"] = tokenSigValid
			if !tokenSigValid {
				expectedTokenSig := function.GenerateTokenSignature(token)
				results["token_signature_expected"] = expectedTokenSig
				results["token_signature_received"] = tokenSignature
			}
		}

		// 验证 sin
		if sin == "" {
			results["sin_valid"] = false
			results["sin_error"] = "缺少sin参数"
		} else {
			sinValid, err := function.VerifySin(bodyBytes, sin)
			results["sin_valid"] = sinValid
			if err != nil {
				results["sin_error"] = err.Error()
			}
			if !sinValid {
				expectedSin, err := function.GenerateSin(bodyBytes)
				if err == nil {
					results["sin_expected"] = expectedSin
					results["sin_received"] = sin
				}
			}
		}

		// 验证 md5-signature
		if md5Signature == "" {
			results["md5_signature_valid"] = false
			results["md5_signature_error"] = "缺少md5-signature参数"
		} else if tokenSignature == "" || sin == "" {
			results["md5_signature_valid"] = false
			results["md5_signature_error"] = "缺少token-signature或sin参数，无法验证md5-signature"
		} else {
			md5Valid, err := function.VerifyMD5Signature(sin, tokenSignature, apiPath, md5Signature)
			results["md5_signature_valid"] = md5Valid
			if err != nil {
				results["md5_signature_error"] = err.Error()
			}
			if !md5Valid {
				expectedMD5, err := function.GenerateMD5Signature(sin, tokenSignature, apiPath)
				if err == nil {
					results["md5_signature_expected"] = expectedMD5
					results["md5_signature_received"] = md5Signature
				}
			}
		}

		// 验证 tm
		if tm == "" {
			results["tm_valid"] = false
			results["tm_error"] = "缺少tm参数"
		} else {
			tmValid, timestamp, err := function.VerifyTm(tm, apiPath, 300) // 5分钟有效期
			results["tm_valid"] = tmValid
			results["tm_timestamp"] = timestamp
			if err != nil {
				results["tm_error"] = err.Error()
			}
		}

		// 验证 pass
		if pass == "" {
			results["pass_valid"] = false
			results["pass_error"] = "缺少pass参数"
		} else if sin == "" || md5Signature == "" {
			results["pass_valid"] = false
			results["pass_error"] = "缺少sin或md5-signature参数，无法验证pass"
		} else {
			// 从tm中获取时间戳
			timestamp := ""
			if tm != "" {
				_, ts, _ := function.VerifyTm(tm, apiPath, 0) // 不验证时间，只提取时间戳
				timestamp = ts
			}
			passValid, err := function.VerifyPass(pass, sin, md5Signature, "", timestamp)
			results["pass_valid"] = passValid
			if err != nil {
				results["pass_error"] = err.Error()
			}
		}

		// 计算总体验证结果
		allValid := true
		if val, ok := results["token_signature_valid"].(bool); !ok || !val {
			allValid = false
		}
		if val, ok := results["sin_valid"].(bool); !ok || !val {
			allValid = false
		}
		if val, ok := results["md5_signature_valid"].(bool); !ok || !val {
			allValid = false
		}
		if val, ok := results["tm_valid"].(bool); !ok || !val {
			allValid = false
		}
		if val, ok := results["pass_valid"].(bool); !ok || !val {
			allValid = false
		}

		results["all_valid"] = allValid

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "参数验证完成",
			"data": results,
		})
	})
}

// RegisterCheckinRoutes 注册签到相关路由
func RegisterCheckinRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, checkinModel *model.CheckinModel, userModel *model.UserRedisModel, stoneRecordModel *model.StoneRecordModel) {
	// 简化版token认证中间件（只需要token，不需要其他签名参数）
	simpleTokenAuth := func(c *gin.Context) {
		token := c.GetHeader("token")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "缺少token",
			})
			c.Abort()
			return
		}

		// 解密token获取sessionId
		sessionID, err := function.DecryptToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "token无效: " + err.Error(),
			})
			c.Abort()
			return
		}

		// 查询sessionId状态
		codeSession, err := codeSessionModel.GetBySessionID(sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "无效的session_id",
			})
			c.Abort()
			return
		}

		// 检查是否被封禁
		if codeSession.IsBanned {
			c.JSON(http.StatusForbidden, gin.H{
				"code": 403,
				"msg":  "账号已被封禁，无法使用",
			})
			c.Abort()
			return
		}

		// 将userID存储到上下文
		c.Set("userID", codeSession.UserID)
		c.Next()
	}

	// 获取签到状态
	r.GET("/checkin/status", simpleTokenAuth, func(c *gin.Context) {
		userID := c.GetInt64("userID")

		// 检查今天是否已签到
		todayCheckin, err := checkinModel.GetTodayCheckin(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取签到状态失败: " + err.Error(),
			})
			return
		}

		// 获取连续签到天数
		consecutiveDays, err := checkinModel.GetConsecutiveDays(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取连续签到天数失败: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"consecutive_days": consecutiveDays,
				"checked_today":    todayCheckin != nil,
			},
		})
	})

	// 执行签到
	r.POST("/checkin", simpleTokenAuth, func(c *gin.Context) {
		userID := c.GetInt64("userID")

		// 检查今天是否已签到
		todayCheckin, err := checkinModel.GetTodayCheckin(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "检查签到状态失败: " + err.Error(),
			})
			return
		}
		if todayCheckin != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "今天已经签到过了",
			})
			return
		}

		// 获取当前连续签到天数
		consecutiveDays, err := checkinModel.GetConsecutiveDays(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取连续签到天数失败: " + err.Error(),
			})
			return
		}

		// 计算新的连续天数（今天签到后）
		newConsecutiveDays := consecutiveDays + 1

		// 根据连续天数计算奖励
		// 规则：
		// 第1天 5，第2天 10，第3天 20，第4天 25，第5天 30，第6天 35，第7天及以后 40
		var reward int64
		switch {
		case newConsecutiveDays == 1:
			reward = 5
		case newConsecutiveDays == 2:
			reward = 10
		case newConsecutiveDays == 3:
			reward = 20
		case newConsecutiveDays == 4:
			reward = 25
		case newConsecutiveDays == 5:
			reward = 30
		case newConsecutiveDays == 6:
			reward = 35
		case newConsecutiveDays == 7:
			reward = 40
		case newConsecutiveDays >= 8:
			reward = 40
		default:
			reward = 5
		}

		// 创建签到记录
		if err := checkinModel.CreateCheckin(userID, newConsecutiveDays, reward); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "创建签到记录失败: " + err.Error(),
			})
			return
		}

		// 增加用户灵石
		if err := userModel.AddStones(userID, reward); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "增加灵石失败: " + err.Error(),
			})
			return
		}
		// 写入灵石明细（签到）
		if stoneRecordModel != nil {
			_ = stoneRecordModel.Create(userID, "checkin", reward, "每日签到", "")
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "签到成功",
			"data": gin.H{
				"consecutive_days": newConsecutiveDays,
				"reward":           reward,
			},
		})
	})
}

// phoneRegex 手机号正则：11位数字，以1开头
var phoneRegex = regexp.MustCompile(`^1\d{10}$`)

// isValidPhone 校验手机号格式
func isValidPhone(phone string) bool {
	return phoneRegex.MatchString(phone)
}
