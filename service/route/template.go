package route

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"

	"service/component"
	"service/config"
	"service/function"
	"service/model"

	"github.com/gin-gonic/gin"
)

const promptPrefix = "\n\n提示词: "
const maxPublicTemplatePageSize = 500

const (
	cosRawHost = "jiadilingguangcos-1393500756.cos.ap-chongqing.myqcloud.com"
	cosCDNHost = "static.jiadilingguang.com"
)

// normalizeCosImageURL 将 COS 原始域名替换为 CDN 域名，确保小程序合法域名可用
func normalizeCosImageURL(url string) string {
	cleanURL := strings.TrimSpace(url)
	if cleanURL == "" {
		return cleanURL
	}
	cdnHost := cosCDNHost
	if cfg := config.Get(); cfg != nil {
		if domain := strings.TrimSpace(cfg.COS.Domain); domain != "" {
			if parsed, err := neturl.Parse(domain); err == nil && parsed.Host != "" {
				cdnHost = parsed.Host
			} else {
				domain = strings.TrimPrefix(domain, "https://")
				domain = strings.TrimPrefix(domain, "http://")
				cdnHost = strings.Trim(domain, "/")
			}
		}
	}
	if cdnHost == "" || cdnHost == cosRawHost {
		return cleanURL
	}
	return strings.ReplaceAll(cleanURL, "://"+cosRawHost, "://"+cdnHost)
}

func collectTemplateDisplayImageURLs(template *model.Template) []string {
	if template == nil {
		return nil
	}
	urls := make([]string, 0, 4)
	appendURL := func(raw string) {
		url := strings.TrimSpace(raw)
		if url == "" {
			return
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return
		}
		for _, existing := range urls {
			if existing == url {
				return
			}
		}
		urls = append(urls, url)
	}
	appendURL(template.PreviewURL)
	appendURL(template.Thumbnail)
	if strings.TrimSpace(template.Images) != "" {
		var images []interface{}
		if err := json.Unmarshal([]byte(template.Images), &images); err == nil {
			for _, item := range images {
				switch value := item.(type) {
				case string:
					appendURL(value)
				case map[string]interface{}:
					if image, ok := value["image"].(string); ok {
						appendURL(image)
					}
					if url, ok := value["url"].(string); ok {
						appendURL(url)
					}
					if previewURL, ok := value["preview_url"].(string); ok {
						appendURL(previewURL)
					}
				}
			}
		}
	}
	return urls
}

func resolveTemplateDisplayImageURL(template *model.Template, imageIndex int) string {
	urls := collectTemplateDisplayImageURLs(template)
	if len(urls) == 0 {
		return ""
	}
	if imageIndex < 0 || imageIndex >= len(urls) {
		imageIndex = 0
	}
	return strings.TrimSpace(urls[imageIndex])
}

type templateGenerateBalanceError struct {
	Required int64
	Current  int64
}

func (e *templateGenerateBalanceError) Error() string {
	return "insufficient balance"
}

func buildTemplateGeneratePayload(template *model.Template, taskModel *model.AITaskModel) (string, map[string]interface{}, error) {
	if template == nil {
		return "", nil, fmt.Errorf("template not found")
	}
	imageURLs := collectTemplateDisplayImageURLs(template)
	if len(imageURLs) == 0 {
		return "", nil, fmt.Errorf("template has no usable reference images")
	}

	payload := make(map[string]interface{})
	scene := ""
	if template.OriginalTaskID > 0 && taskModel != nil {
		task, err := taskModel.GetByID(template.OriginalTaskID)
		if err == nil && task != nil {
			scene = strings.TrimSpace(task.Scene)
			if strings.TrimSpace(task.RequestPayload) != "" {
				_ = json.Unmarshal([]byte(task.RequestPayload), &payload)
			}
		}
	}
	if payload == nil {
		payload = make(map[string]interface{})
	}
	if scene != "ai_draw_single" && scene != "ai_draw_multi" {
		if len(imageURLs) > 1 {
			scene = "ai_draw_multi"
		} else {
			scene = "ai_draw_single"
		}
	}

	prompt := strings.TrimSpace(getStringFromPayload(payload, "prompt"))
	if prompt == "" {
		prompt = strings.TrimSpace(template.InternalPrompt)
	}
	if prompt == "" {
		return "", nil, fmt.Errorf("template has no hidden prompt configured")
	}

	payload["prompt"] = prompt
	payload["user_prompt"] = ""
	payload["template_id"] = template.ID
	payload["hide_prompt_in_response"] = true
	payload["generate_count"] = 1
	payload["images"] = imageURLs
	payload["reference_image_urls"] = imageURLs
	payload["ordered_image_urls"] = imageURLs
	payload["original_image_urls"] = []string{}
	payload["reference_image_url"] = imageURLs[0]
	if len(imageURLs) == 1 {
		payload["image_url"] = imageURLs[0]
	} else {
		delete(payload, "image_url")
	}

	return scene, payload, nil
}

func createTemplateGenerateTask(userID int64, scene string, payload map[string]interface{}, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, taskModel *model.AITaskModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, aiToolModel *model.AIToolModel) (*model.AITask, int64, error) {
	pricing, err := pricingModel.GetByScene(scene)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid generation scene: %w", err)
	}

	generateCount := int64(1)
	if pricingConfig := parsePricingExtraConfig(pricing); pricingConfig != nil {
		if configGenerateCount, ok := getPricingIntOption(pricingConfig, "generate_count", "default_generate_count"); ok && configGenerateCount > 0 {
			generateCount = configGenerateCount
		}
	}
	if payloadGenerateCount := getInt64FromPayload(payload, "generate_count"); payloadGenerateCount > 0 {
		generateCount = payloadGenerateCount
	}
	if generateCount > 3 {
		generateCount = 3
	}
	payload["generate_count"] = generateCount

	if strings.HasPrefix(scene, "ai_draw") {
		payload["prompt"] = component.BuildAIDrawPrompt(payload)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal generation payload: %w", err)
	}

	totalStones := pricing.Stones * generateCount
	currentStones, err := userModel.GetStones(userID)
	if err != nil {
		return nil, 0, err
	}
	if currentStones < totalStones {
		return nil, 0, &templateGenerateBalanceError{Required: totalStones, Current: currentStones}
	}
	if err := userModel.DeductStones(userID, totalStones); err != nil {
		return nil, 0, err
	}

	tx, err := taskModel.DB.Begin()
	if err != nil {
		_ = userModel.AddStones(userID, totalStones)
		return nil, 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	consumeTitle := fmt.Sprintf("template-generate-%s-%d", scene, generateCount)
	if stoneRecordModel != nil {
		if err := stoneRecordModel.CreateWithTx(tx, userID, "consume", totalStones, consumeTitle, ""); err != nil {
			_ = userModel.AddStones(userID, totalStones)
			return nil, 0, err
		}
	}
	if userOrderModel != nil {
		orderNo := model.GenerateOrderNo("ORD")
		if err := userOrderModel.CreateWithTx(tx, userID, orderNo, "consume", -totalStones, "success", consumeTitle, ""); err != nil {
			_ = userModel.AddStones(userID, totalStones)
			return nil, 0, err
		}
	}

	task := &model.AITask{
		TaskNo:         function.GenerateTaskNo(),
		UserID:         userID,
		Scene:          scene,
		RequestPayload: string(payloadJSON),
		Status:         "pending",
		StonesUsed:     totalStones,
	}
	if toolID := getInt64FromPayload(payload, "tool_id"); toolID > 0 {
		task.ToolID = sql.NullInt64{Int64: toolID, Valid: true}
	}
	if err := taskModel.CreateWithTx(tx, task); err != nil {
		_ = userModel.AddStones(userID, totalStones)
		return nil, 0, err
	}
	if task.ToolID.Valid && aiToolModel != nil {
		if err := aiToolModel.IncrementUsageCountWithTx(tx, task.ToolID.Int64); err != nil {
			_ = userModel.AddStones(userID, totalStones)
			return nil, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		_ = userModel.AddStones(userID, totalStones)
		return nil, 0, err
	}
	committed = true

	return task, totalStones, nil
}

func defaultTemplateSquareMainTabs() []gin.H {
	return []gin.H{
		{"label": "场景", "value": "scene"},
		{"label": "风格", "value": "style"},
		{"label": "灵感", "value": "inspiration"},
	}
}

func defaultTemplateSquareSubTabs() []gin.H {
	return []gin.H{
		{"label": "乡墅外观", "value": "villa_exterior", "parent": "scene"},
		{"label": "室内空间", "value": "interior_space", "parent": "scene"},
		{"label": "花园庭院", "value": "garden_courtyard", "parent": "scene"},
		{"label": "改造翻新", "value": "renovation", "parent": "scene"},
		{"label": "商业空间", "value": "commercial_space", "parent": "scene"},
		{"label": "设计辅助", "value": "design_assist", "parent": "scene"},
		{"label": "新闽派", "value": "new_minnan", "parent": "style"},
		{"label": "新中式", "value": "new_chinese", "parent": "style"},
		{"label": "现代风格", "value": "modern", "parent": "style"},
		{"label": "经典欧式", "value": "classic_european", "parent": "style"},
		{"label": "地域特色", "value": "regional", "parent": "style"},
		{"label": "乡建趋势", "value": "rural_trend", "parent": "inspiration"},
		{"label": "生活方式", "value": "lifestyle", "parent": "inspiration"},
		{"label": "地域文化", "value": "regional_culture", "parent": "inspiration"},
		{"label": "功能创新", "value": "function_innovation", "parent": "inspiration"},
		{"label": "案例精选", "value": "selected_cases", "parent": "inspiration"},
	}
}

// splitDescriptionPrompt 从 description 中拆出「描述」和「提示词」（description 格式为 "描述\n\n提示词: xxx"）
func splitDescriptionPrompt(description string) (descWithoutPrompt, prompt string) {
	if description == "" {
		return "", ""
	}
	idx := strings.Index(description, promptPrefix)
	if idx < 0 {
		return description, ""
	}
	return strings.TrimSpace(description[:idx]), strings.TrimSpace(description[idx+len(promptPrefix):])
}

func buildTemplateCreatorInfo(creatorUserID int64, userProfileModel *model.UserProfileModel, userDBModel *model.UserModel) *gin.H {
	if creatorUserID <= 0 {
		return nil
	}

	creatorInfo := gin.H{
		"user_id": creatorUserID,
		"name":    "匿名设计师",
		"avatar":  "",
		"title":   "设计师",
	}

	if userProfileModel != nil {
		if profile, err := userProfileModel.GetByUserID(creatorUserID); err == nil && profile != nil {
			if strings.TrimSpace(profile.Nickname) != "" {
				creatorInfo["name"] = strings.TrimSpace(profile.Nickname)
			}
			creatorInfo["avatar"] = sanitizePublicImageURL(profile.Avatar)
			if strings.TrimSpace(profile.ServiceTitle) != "" {
				creatorInfo["title"] = strings.TrimSpace(profile.ServiceTitle)
			}
		}
	}

	if creatorInfo["name"] == "匿名设计师" && userDBModel != nil {
		if user, err := userDBModel.GetByID(creatorUserID); err == nil && user != nil && strings.TrimSpace(user.Username) != "" {
			creatorInfo["name"] = strings.TrimSpace(user.Username)
		}
	}

	return &creatorInfo
}

func isPublicSquareTemplate(template *model.Template) bool {
	return template != nil && template.Status == "published" && strings.TrimSpace(template.PublishScope) == "square"
}

// templateToResponse 组装模板详情响应，保证返回 name、description、thumbnail、preview_url、images 等字段；
// showPrompt 为 true 时返回 prompt 字段；paidLocked 为 true 时 description 不含提示词
type originalTaskLookup interface {
	GetByID(id int64) (*model.AITask, error)
}

func templateHasUsableOriginalTask(template *model.Template, taskLookup originalTaskLookup) bool {
	if template == nil || template.OriginalTaskID <= 0 || taskLookup == nil {
		return false
	}
	task, err := taskLookup.GetByID(template.OriginalTaskID)
	return err == nil && task != nil
}

func templateToResponse(template *model.Template, paidLocked bool, creatorInfo *gin.H, taskLookup originalTaskLookup) gin.H {
	_ = paidLocked
	resp := gin.H{
		"id":                template.ID,
		"name":              template.Name,
		"category":          template.Category,
		"main_tab":          template.MainTab,
		"sub_tab":           template.SubTab,
		"third_tab":         template.ThirdTab,
		"description":       template.Description,
		"thumbnail":         normalizeCosImageURL(template.Thumbnail),
		"preview_url":       normalizeCosImageURL(template.PreviewURL),
		"images":            normalizeCosImageURL(template.Images),
		"image_width":       template.ImageWidth,
		"image_height":      template.ImageHeight,
		"price":             template.Price,
		"is_free":           template.IsFree,
		"download_count":    template.DownloadCount,
		"like_count":        template.LikeCount,
		"status":            template.Status,
		"publish_scope":     template.PublishScope,
		"reject_reason":     template.RejectReason,
		"source_type":       template.SourceType,
		"creator":           template.Creator,
		"creator_user_id":   template.CreatorUserID,
		"created_at":        template.CreatedAt,
		"updated_at":        template.UpdatedAt,
		"has_original_task": templateHasUsableOriginalTask(template, taskLookup),
	}
	resp["unlocked"] = !paidLocked
	// 添加创建者用户信息
	if creatorInfo != nil {
		resp["creator_info"] = creatorInfo
	}
	return resp
}

func resolveTemplateDownloadAccess(userOrderModel *model.UserOrderModel, userProfileModel *model.UserProfileModel, userMembershipModel *model.UserMembershipModel, userID int64) (phoneVerified bool, rechargeMember bool, canDownload bool, activeMembership *model.UserMembership, legacyRecharge bool, err error) {
	if userID <= 0 {
		return false, false, false, nil, false, nil
	}
	if userProfileModel != nil {
		profile, profileErr := userProfileModel.GetOrCreate(userID, "")
		if profileErr != nil {
			return false, false, false, nil, false, profileErr
		}
		if profile != nil {
			phone := strings.TrimSpace(profile.Phone)
			phoneVerified = phone != ""
		}
	}
	activeMembership, legacyRecharge, rechargeMember, err = resolveUserMembershipAccess(userMembershipModel, userOrderModel, userID)
	if err != nil {
		return phoneVerified, false, false, nil, false, err
	}
	canDownload = phoneVerified && rechargeMember
	return phoneVerified, rechargeMember, canDownload, activeMembership, legacyRecharge, nil
}

func appendTemplateDownloadState(resp gin.H, loggedIn bool, phoneVerified bool, rechargeMember bool, activeMembership *model.UserMembership, legacyRecharge bool) {
	resp["download_requires_login"] = true
	resp["download_requires_phone_verify"] = true
	resp["download_requires_recharge_member"] = true
	resp["download_member_label"] = "下载会员"
	resp["user_logged_in"] = loggedIn
	resp["user_phone_verified"] = phoneVerified
	resp["user_recharge_member"] = rechargeMember
	resp["legacy_recharge_member"] = legacyRecharge
	resp["can_download_images"] = loggedIn && phoneVerified && rechargeMember
	if activeMembership != nil {
		resp["membership_active"] = true
		resp["membership_plan_code"] = activeMembership.PlanCode
		resp["membership_plan_title"] = activeMembership.PlanTitle
		resp["membership_expired_at"] = activeMembership.ExpiredAt
		resp["membership_expired_at_text"] = formatMembershipTimeText(activeMembership.ExpiredAt)
		if strings.TrimSpace(activeMembership.PlanTitle) != "" {
			resp["download_member_label"] = activeMembership.PlanTitle
		}
	} else {
		resp["membership_active"] = false
		resp["membership_plan_code"] = ""
		resp["membership_plan_title"] = ""
		resp["membership_expired_at"] = nil
		resp["membership_expired_at_text"] = ""
		if legacyRecharge {
			resp["download_member_label"] = "已充值用户（一期兼容）"
		}
	}
	if !loggedIn {
		resp["download_action_text"] = "登录后下载"
		resp["download_action_hint"] = "登录后可继续验证并下载当前模板图片"
		return
	}
	if !phoneVerified {
		resp["download_action_text"] = "验证后下载"
		resp["download_action_hint"] = "完成手机号授权验证后即可继续下载模板图片"
		return
	}
	if !rechargeMember {
		resp["download_action_text"] = "充值后下载"
		resp["download_action_hint"] = "当前模板图片下载仅对已完成标准充值的用户开放，标准充值成功后即可下载；若后续配置了具体会员时长，则按对应档位为准"
		return
	}
	resp["download_action_text"] = "下载当前图片"
	if activeMembership != nil && !activeMembership.ExpiredAt.IsZero() {
		if model.IsLifetimeMembership(activeMembership) {
			resp["download_action_hint"] = "你当前已开通长期有效下载资格，可保存当前模板图片到相册"
			return
		}
		resp["download_action_hint"] = "会员有效期至" + formatMembershipTimeText(activeMembership.ExpiredAt) + "，可保存当前模板图片到相册"
		return
	}
	if legacyRecharge {
		resp["download_action_hint"] = "你当前享有一期兼容下载资格，可保存当前模板图片到相册"
		return
	}
	resp["download_action_hint"] = "已开通下载资格，可保存当前模板图片到相册"
}

func fillTemplateStats(resp gin.H, templateID int64, templateCommentModel *model.TemplateCommentModel, templateShareModel *model.TemplateShareModel) {
	commentCount := int64(0)
	shareCount := int64(0)
	if templateCommentModel != nil {
		if count, err := templateCommentModel.CountByTemplateID(templateID); err == nil {
			commentCount = count
		}
	}
	if templateShareModel != nil {
		if count, err := templateShareModel.CountByTemplateID(templateID); err == nil {
			shareCount = count
		}
	}
	resp["comment_count"] = commentCount
	resp["share_count"] = shareCount
}

// RegisterTemplateRoutes 注册小程序模板路由
func RegisterTemplateRoutes(r *gin.RouterGroup, templateModel *model.TemplateModel, templateCategoryModel *model.TemplateCategoryModel, templateSquareConfigModel *model.TemplateSquareConfigModel, templateUnlockModel *model.TemplateUnlockModel, templateLikeModel *model.TemplateLikeModel, templateCommentModel *model.TemplateCommentModel, templateShareModel *model.TemplateShareModel, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, userProfileModel *model.UserProfileModel, userMembershipModel *model.UserMembershipModel, userDBModel *model.UserModel, featuredCaseGroupModel *model.FeaturedCaseGroupModel, taskModel *model.AITaskModel, aiToolModel *model.AIToolModel) {
	templates := r.Group("/templates")
	{
		// 获取模板列表（公开接口，不需要认证）
		templates.GET("", func(c *gin.Context) {
			category := c.Query("category")  // villa, urban, family, culture, hot, latest
			mainTab := c.Query("main_tab")   // 一级tab value
			subTab := c.Query("sub_tab")     // 二级tab value
			thirdTab := c.Query("third_tab") // 三级tab value
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

			if page < 1 {
				page = 1
			}
			if pageSize < 1 || pageSize > maxPublicTemplatePageSize {
				pageSize = 20
			}

			var templateList []*model.Template
			var total int64
			var err error

			// 如果指定了 main_tab，使用新的查询方法（支持按使用人数排序）
			if mainTab != "" {
				offset := (page - 1) * pageSize
				templateList, err = templateModel.ListByMainTabAndSubTab(mainTab, subTab, thirdTab, "published", pageSize, offset)
				if err == nil {
					total, err = templateModel.CountByMainTabAndSubTab(mainTab, subTab, thirdTab, "published")
				}
			} else {
				// 处理特殊分类：hot（最热）和 latest（最新）
				if category == "hot" {
					templateList, err = templateModel.GetHotTemplates(pageSize)
					if err == nil {
						total, _ = templateModel.CountPublicByCategory("")
					}
				} else if category == "latest" {
					templateList, err = templateModel.GetLatestTemplates(pageSize)
					if err == nil {
						total, _ = templateModel.CountPublicByCategory("")
					}
				} else {
					offset := (page - 1) * pageSize
					templateList, err = templateModel.ListPublicByCategory(category, pageSize, offset)
					if err == nil {
						total, err = templateModel.CountPublicByCategory(category)
					}
				}
			}

			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取模板列表失败: " + err.Error(),
				})
				return
			}

			// 显式组装列表项，保证返回 name、description、thumbnail、preview_url 等字段
			list := make([]gin.H, 0, len(templateList))
			for _, t := range templateList {
				creatorInfo := buildTemplateCreatorInfo(t.CreatorUserID, userProfileModel, userDBModel)
				list = append(list, gin.H{
					"id":              t.ID,
					"name":            t.Name,
					"description":     t.Description,
					"thumbnail":       normalizeCosImageURL(t.Thumbnail),
					"preview_url":     normalizeCosImageURL(t.PreviewURL),
					"images":          normalizeCosImageURL(t.Images),
					"image_width":     t.ImageWidth,
					"image_height":    t.ImageHeight,
					"category":        t.Category,
					"main_tab":        t.MainTab,
					"sub_tab":         t.SubTab,
					"third_tab":       t.ThirdTab,
					"price":           t.Price,
					"is_free":         t.IsFree,
					"download_count":  t.DownloadCount,
					"like_count":      t.LikeCount,
					"status":          t.Status,
					"publish_scope":   t.PublishScope,
					"source_type":     t.SourceType,
					"creator":         t.Creator,
					"creator_user_id": t.CreatorUserID,
					"created_at":      t.CreatedAt,
					"updated_at":      t.UpdatedAt,
					"creator_info":    creatorInfo,
				})
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

		// 获取模板分类列表（公开接口，从 DB 读取）
		templates.GET("/categories", func(c *gin.Context) {
			list, err := templateCategoryModel.List()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取分类列表失败: " + err.Error(),
				})
				return
			}
			categories := make([]gin.H, 0, len(list))
			for _, cat := range list {
				categories = append(categories, gin.H{"id": cat.ID, "name": cat.Name, "sort_order": cat.SortOrder})
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{"list": categories},
			})
		})

		// 获取精选案例组列表（公开接口，供小程序首页调用）
		templates.GET("/featured", func(c *gin.Context) {
			groups, err := featuredCaseGroupModel.GetAll()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取精选案例组失败: " + err.Error(),
				})
				return
			}

			// 获取每个组的案例详情
			var result []gin.H
			for _, group := range groups {
				groupData := gin.H{
					"id":           group.ID,
					"name":         group.Name,
					"display_mode": group.DisplayMode,
					"case1_label":  group.Case1Label,
					"case2_label":  group.Case2Label,
				}

				// 获取第一个案例
				case1, err := templateModel.GetByID(group.Case1ID)
				if err == nil && case1 != nil && isPublicSquareTemplate(case1) {
					creatorInfo := buildTemplateCreatorInfo(case1.CreatorUserID, userProfileModel, userDBModel)
					groupData["case1"] = gin.H{
						"id":              case1.ID,
						"name":            case1.Name,
						"description":     case1.Description,
						"thumbnail":       case1.Thumbnail,
						"preview_url":     case1.PreviewURL,
						"image_width":     case1.ImageWidth,
						"image_height":    case1.ImageHeight,
						"price":           case1.Price,
						"is_free":         case1.IsFree,
						"download_count":  case1.DownloadCount,
						"like_count":      case1.LikeCount,
						"creator":         case1.Creator,
						"creator_user_id": case1.CreatorUserID,
						"creator_info":    creatorInfo,
					}
				}

				// 获取第二个案例（如果存在）
				if group.Case2ID > 0 {
					case2, err := templateModel.GetByID(group.Case2ID)
					if err == nil && case2 != nil && isPublicSquareTemplate(case2) {
						creatorInfo := buildTemplateCreatorInfo(case2.CreatorUserID, userProfileModel, userDBModel)
						groupData["case2"] = gin.H{
							"id":              case2.ID,
							"name":            case2.Name,
							"description":     case2.Description,
							"thumbnail":       case2.Thumbnail,
							"preview_url":     case2.PreviewURL,
							"image_width":     case2.ImageWidth,
							"image_height":    case2.ImageHeight,
							"price":           case2.Price,
							"is_free":         case2.IsFree,
							"download_count":  case2.DownloadCount,
							"like_count":      case2.LikeCount,
							"creator":         case2.Creator,
							"creator_user_id": case2.CreatorUserID,
							"creator_info":    creatorInfo,
						}
					}
				}

				// 只有当至少有一个有效案例时才添加到结果中
				if _, ok := groupData["case1"]; ok {
					result = append(result, groupData)
				}
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"groups": result,
				},
			})
		})

		// 获取模板广场双重 Tab 配置（公开接口）
		templates.GET("/tab-config", func(c *gin.Context) {
			parent := c.Query("parent") // 可选：只返回指定父级的二级tab
			if templateSquareConfigModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Tab 配置未就绪"})
				return
			}

			mainTabs := make([]gin.H, 0)
			subTabs := make([]gin.H, 0)
			thirdTabs := make([]gin.H, 0)
			cfg, err := templateSquareConfigModel.Get()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取 Tab 配置失败: " + err.Error()})
				return
			}
			if cfg != nil {
				mainList, err := templateSquareConfigModel.ParseMainTabs(cfg.MainTabs)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析一级 Tab 配置失败: " + err.Error()})
					return
				}
				for _, t := range mainList {
					mainTabs = append(mainTabs, gin.H{"label": t.Label, "value": t.Value})
				}

				subList, err := templateSquareConfigModel.ParseSubTabs(cfg.SubTabs)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析二级 Tab 配置失败: " + err.Error()})
					return
				}
				for _, t := range subList {
					if parent == "" || t.Parent == parent {
						subTabs = append(subTabs, gin.H{"label": t.Label, "value": t.Value, "parent": t.Parent})
					}
				}
				thirdList, err := templateSquareConfigModel.ParseThirdTabs(cfg.ThirdTabs)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析三级 Tab 配置失败: " + err.Error()})
					return
				}
				for _, t := range thirdList {
					thirdTabs = append(thirdTabs, gin.H{"label": t.Label, "value": t.Value, "parent": t.Parent})
				}
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{"main_tabs": mainTabs, "sub_tabs": subTabs, "third_tabs": thirdTabs},
			})
		})

		// 模板搜索（公开接口，按名称/描述关键字搜索已发布模板）
		templates.GET("/search", func(c *gin.Context) {
			keyword := strings.TrimSpace(c.Query("keyword"))
			if keyword == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "搜索关键词不能为空",
				})
				return
			}
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			if page < 1 {
				page = 1
			}
			if pageSize < 1 || pageSize > maxPublicTemplatePageSize {
				pageSize = 20
			}
			offset := (page - 1) * pageSize

			list, total, err := templateModel.SearchPublishedTemplates(keyword, pageSize, offset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "搜索模板失败: " + err.Error(),
				})
				return
			}

			// 与列表接口保持一致的字段结构
			respList := make([]gin.H, 0, len(list))
			for _, t := range list {
				if !isPublicSquareTemplate(t) {
					continue
				}
				creatorInfo := buildTemplateCreatorInfo(t.CreatorUserID, userProfileModel, userDBModel)
				respList = append(respList, gin.H{
					"id":              t.ID,
					"name":            t.Name,
					"description":     t.Description,
					"thumbnail":       normalizeCosImageURL(t.Thumbnail),
					"preview_url":     normalizeCosImageURL(t.PreviewURL),
					"images":          normalizeCosImageURL(t.Images),
					"image_width":     t.ImageWidth,
					"image_height":    t.ImageHeight,
					"price":           t.Price,
					"is_free":         t.IsFree,
					"download_count":  t.DownloadCount,
					"like_count":      t.LikeCount,
					"category":        t.Category,
					"main_tab":        t.MainTab,
					"sub_tab":         t.SubTab,
					"third_tab":       t.ThirdTab,
					"status":          t.Status,
					"publish_scope":   t.PublishScope,
					"source_type":     t.SourceType,
					"creator":         t.Creator,
					"creator_user_id": t.CreatorUserID,
					"created_at":      t.CreatedAt,
					"updated_at":      t.UpdatedAt,
					"creator_info":    creatorInfo,
				})
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      respList,
					"total":     total,
					"page":      page,
					"page_size": pageSize,
				},
			})
		})

		// 获取当前用户已点赞的模板 ID 列表（用于模板广场展示 liked 状态）
		templates.GET("/liked-ids", TokenAuthRequired(codeSessionModel), func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
				return
			}
			if templateLikeModel == nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"ids": []int64{}}})
				return
			}
			ids, err := templateLikeModel.GetLikedTemplateIDs(codeSession.UserID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取失败: " + err.Error()})
				return
			}
			if ids == nil {
				ids = []int64{}
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"ids": ids}})
		})

		templates.GET("/favorites", TokenAuthRequired(codeSessionModel), func(c *gin.Context) {
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
			if pageSize < 1 || pageSize > 50 {
				pageSize = 20
			}
			if templateLikeModel == nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}, "total": 0, "page": page, "page_size": pageSize}})
				return
			}
			ids, err := templateLikeModel.GetLikedTemplateIDs(codeSession.UserID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取收藏列表失败: " + err.Error()})
				return
			}
			total := len(ids)
			if total == 0 {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}, "total": 0, "page": page, "page_size": pageSize}})
				return
			}
			start := (page - 1) * pageSize
			if start >= total {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}, "total": total, "page": page, "page_size": pageSize}})
				return
			}
			end := start + pageSize
			if end > total {
				end = total
			}
			respList := make([]gin.H, 0, end-start)
			for _, id := range ids[start:end] {
				template, err := templateModel.GetByID(id)
				if err != nil || !isPublicSquareTemplate(template) {
					continue
				}
				creatorInfo := buildTemplateCreatorInfo(template.CreatorUserID, userProfileModel, userDBModel)
				item := templateToResponse(template, false, creatorInfo, taskModel)
				item["main_tab"] = template.MainTab
				item["sub_tab"] = template.SubTab
				item["third_tab"] = template.ThirdTab
				item["liked"] = true
				fillTemplateStats(item, template.ID, templateCommentModel, templateShareModel)
				respList = append(respList, item)
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": respList, "total": total, "page": page, "page_size": pageSize}})
		})

		// 获取模板详情（公开接口；付费模板不返回提示词，仅返回描述与价格）
		templates.GET("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的模板ID",
				})
				return
			}

			template, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "模板不存在",
				})
				return
			}

			if !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "模板不存在",
				})
				return
			}

			// 付费模板：公开接口不返回提示词，只返回描述（不含提示词部分），unlocked 为 false
			paidLocked := !template.IsFree && template.Price > 0

			// 获取创建者用户信息
			creatorInfo := buildTemplateCreatorInfo(template.CreatorUserID, userProfileModel, userDBModel)

			data := templateToResponse(template, paidLocked, creatorInfo, taskModel)
			fillTemplateStats(data, template.ID, templateCommentModel, templateShareModel)
			appendTemplateDownloadState(data, false, false, false, nil, false)
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": data,
			})
		})

		// 获取模板评论列表（公开接口）
		templates.GET("/:id/comments", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的模板ID"})
				return
			}
			if templateCommentModel == nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}, "total": 0}})
				return
			}
			template, err := templateModel.GetByID(id)
			if err != nil || !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			if page < 1 {
				page = 1
			}
			if pageSize < 1 || pageSize > 50 {
				pageSize = 20
			}
			offset := (page - 1) * pageSize
			list, err := templateCommentModel.ListByTemplateID(id, pageSize, offset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取评论失败: " + err.Error()})
				return
			}
			total, _ := templateCommentModel.CountByTemplateID(id)
			respList := make([]gin.H, 0, len(list))
			for _, item := range list {
				respList = append(respList, gin.H{
					"id":            item.ID,
					"template_id":   item.TemplateID,
					"user_id":       item.UserID,
					"author_name":   item.AuthorName,
					"author_avatar": item.AuthorAvatar,
					"content":       item.Content,
					"created_at":    item.CreatedAt,
				})
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": respList, "total": total, "page": page, "page_size": pageSize}})
		})

		// 记录模板分享（公开接口）
		templates.POST("/:id/share", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的模板ID"})
				return
			}
			if templateShareModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "分享功能未就绪"})
				return
			}
			template, err := templateModel.GetByID(id)
			if err != nil || !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			var req struct {
				Channel string `json:"channel"`
			}
			_ = c.ShouldBindJSON(&req)
			channel := strings.TrimSpace(req.Channel)
			if channel == "" {
				channel = "miniprogram_share"
			}
			record := &model.TemplateShare{
				TemplateID: id,
				UserID:     0,
				Channel:    channel,
			}
			if err := templateShareModel.Create(record); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "记录分享失败: " + err.Error()})
				return
			}
			shareCount, _ := templateShareModel.CountByTemplateID(id)
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"share_count": shareCount}})
		})

		templatesAuth := templates.Group("")
		templatesAuth.Use(TokenAuthRequired(codeSessionModel))

		// 根据模板ID获取原始任务的提示词和参考图（需要token验证）
		templatesAuth.GET("/:id/original-task", func(c *gin.Context) {
			handleGetTemplateOriginalTask(c, templateModel, taskModel, codeSessionModel)
		})

		// 获取模板详情（需登录；返回 unlocked 与完整 prompt）
		templatesAuth.GET("/:id/detail", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
				return
			}
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的模板ID"})
				return
			}
			template, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			if !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			paidLocked := false
			if !template.IsFree && template.Price > 0 {
				unlocked, _ := templateUnlockModel.HasUnlocked(codeSession.UserID, id)
				paidLocked = !unlocked
			}

			// 获取创建者用户信息
			creatorInfo := buildTemplateCreatorInfo(template.CreatorUserID, userProfileModel, userDBModel)

			data := templateToResponse(template, paidLocked, creatorInfo, taskModel)
			fillTemplateStats(data, template.ID, templateCommentModel, templateShareModel)
			phoneVerified, rechargeMember, canDownload, activeMembership, legacyRecharge, accessErr := resolveTemplateDownloadAccess(userOrderModel, userProfileModel, userMembershipModel, codeSession.UserID)
			if accessErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取模板下载资格失败: " + accessErr.Error()})
				return
			}
			appendTemplateDownloadState(data, true, phoneVerified, rechargeMember, activeMembership, legacyRecharge)
			data["can_download_images"] = canDownload
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": data})
		})

		templatesAuth.POST("/:id/generate", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "token validation failed"})
				return
			}
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "invalid template id"})
				return
			}
			template, err := templateModel.GetByID(id)
			if err != nil || !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "template not found"})
				return
			}
			if !template.IsFree && template.Price > 0 {
				unlocked, _ := templateUnlockModel.HasUnlocked(codeSession.UserID, id)
				if !unlocked {
					c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "please unlock template before generating"})
					return
				}
			}

			scene, payload, err := buildTemplateGeneratePayload(template, taskModel)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
				return
			}
			task, totalStones, err := createTemplateGenerateTask(codeSession.UserID, scene, payload, userModel, pricingModel, taskModel, stoneRecordModel, userOrderModel, aiToolModel)
			if err != nil {
				if balanceErr, ok := err.(*templateGenerateBalanceError); ok {
					c.JSON(http.StatusPaymentRequired, gin.H{"code": 402, "msg": "insufficient balance", "data": gin.H{"required": balanceErr.Required, "current": balanceErr.Current}})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "failed to create template task: " + err.Error()})
				return
			}
			_ = templateModel.IncrementDownloadCount(id)
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "task submitted", "data": gin.H{"task_id": task.ID, "task_no": task.TaskNo, "task_type": "ai_draw", "stones_used": totalStones}})
		})

		// 发表评论（需要 token）
		templatesAuth.POST("/:id/comments", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
				return
			}
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的模板ID"})
				return
			}
			if templateCommentModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "评论功能未就绪"})
				return
			}
			var req struct {
				Content string `json:"content" binding:"required"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			content := strings.TrimSpace(req.Content)
			if content == "" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "评论内容不能为空"})
				return
			}
			if len([]rune(content)) > 300 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "评论内容不能超过300字"})
				return
			}
			template, err := templateModel.GetByID(id)
			if err != nil || !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			authorName := "用户"
			authorAvatar := ""
			if userProfileModel != nil {
				if profile, err := userProfileModel.GetOrCreate(codeSession.UserID, ""); err == nil && profile != nil {
					if strings.TrimSpace(profile.Nickname) != "" {
						authorName = strings.TrimSpace(profile.Nickname)
					}
					authorAvatar = sanitizePublicImageURL(profile.Avatar)
				}
			}
			if authorName == "用户" && userDBModel != nil {
				if user, err := userDBModel.GetByID(codeSession.UserID); err == nil && user != nil && strings.TrimSpace(user.Username) != "" {
					authorName = strings.TrimSpace(user.Username)
				}
			}
			comment := &model.TemplateComment{
				TemplateID:   id,
				UserID:       codeSession.UserID,
				AuthorName:   authorName,
				AuthorAvatar: authorAvatar,
				Content:      content,
			}
			if err := templateCommentModel.Create(comment); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "发布评论失败: " + err.Error()})
				return
			}
			total, _ := templateCommentModel.CountByTemplateID(id)
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "评论成功", "data": gin.H{
				"id":            comment.ID,
				"template_id":   comment.TemplateID,
				"user_id":       comment.UserID,
				"author_name":   comment.AuthorName,
				"author_avatar": sanitizePublicImageURL(comment.AuthorAvatar),
				"content":       comment.Content,
				"created_at":    comment.CreatedAt,
				"comment_count": total,
			}})
		})

		// 解锁模板（付费且未解锁时扣费一次，之后查看提示词/用提示词生成/下载均不再扣费）
		templatesAuth.POST("/:id/unlock", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
				return
			}
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的模板ID"})
				return
			}
			template, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			if !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			if template.IsFree || template.Price <= 0 {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "免费模板无需解锁", "data": gin.H{"unlocked": true}})
				return
			}
			unlocked, _ := templateUnlockModel.HasUnlocked(codeSession.UserID, id)
			if unlocked {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已解锁", "data": gin.H{"unlocked": true}})
				return
			}
			currentStones, err := userModel.GetStones(codeSession.UserID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询余额失败: " + err.Error()})
				return
			}
			if currentStones < template.Price {
				c.JSON(http.StatusPaymentRequired, gin.H{
					"code": 402, "msg": "余额不足",
					"data": gin.H{"required": template.Price, "current": currentStones},
				})
				return
			}
			if err := userModel.DeductStones(codeSession.UserID, template.Price); err != nil {
				c.JSON(http.StatusPaymentRequired, gin.H{"code": 402, "msg": "扣费失败: " + err.Error()})
				return
			}
			if stoneRecordModel != nil {
				_ = stoneRecordModel.Create(codeSession.UserID, "consume", template.Price, "模板解锁", template.Name)
			}
			if userOrderModel != nil {
				orderNo := model.GenerateOrderNo("ORD")
				reviewStatus := "not_applicable"
				if template.CreatorUserID > 0 && template.CreatorUserID != codeSession.UserID {
					reviewStatus = "pending_review"
				}
				_ = userOrderModel.CreateDetailed(&model.UserOrder{
					UserID:         codeSession.UserID,
					DesignerUserID: template.CreatorUserID,
					TemplateID:     &id,
					OrderNo:        orderNo,
					Type:           "culture",
					OrderCategory:  "template",
					Amount:         -int64(template.Price),
					Status:         "success",
					ReviewStatus:   reviewStatus,
					Title:          "模板解锁",
					Description:    template.Name,
				})
			}
			_ = templateUnlockModel.Create(codeSession.UserID, id)
			if template.CreatorUserID > 0 {
				_ = userModel.AddStones(template.CreatorUserID, template.Price)
				if stoneRecordModel != nil {
					_ = stoneRecordModel.CreateWithTemplateID(template.CreatorUserID, "task", template.Price, "模板付费", template.Name, template.ID)
				}
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "解锁成功", "data": gin.H{"unlocked": true}})
		})

		// 下载模板图片（一期复用版：需登录 + 已完成手机号授权 + 已有成功充值记录）
		templatesAuth.POST("/:id/download", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
				return
			}
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的模板ID"})
				return
			}
			template, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			if userProfileModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户扩展信息服务不可用"})
				return
			}
			phoneVerified, rechargeMember, canDownload, _, _, accessErr := resolveTemplateDownloadAccess(userOrderModel, userProfileModel, userMembershipModel, codeSession.UserID)
			if accessErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取模板下载资格失败: " + accessErr.Error()})
				return
			}
			if !phoneVerified {
				c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "请先完成手机号授权验证后再下载保存"})
				return
			}
			if !rechargeMember {
				c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "请先完成任意充值后再下载模板图片"})
				return
			}
			if !canDownload {
				c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "当前账号暂不具备模板下载权限"})
				return
			}

			if !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			imageURLs := collectTemplateDisplayImageURLs(template)
			if len(imageURLs) == 0 {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "当前模板暂无可下载图片"})
				return
			}
			// 会员下载限制检查
			_, _, _, dlMembership, _, _ := resolveTemplateDownloadAccess(userOrderModel, userProfileModel, userMembershipModel, codeSession.UserID)
			if dlMembership != nil {
				dl := component.NewDownloadLimiter(model.NewMembershipPlanModel(component.GetDB()), component.GetRedis())
				if allowed, reason, limErr := dl.CheckDownloadAccess(codeSession.UserID, dlMembership, id, -1); limErr != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "检查下载限制失败: " + limErr.Error()})
					return
				} else if !allowed {
					c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": reason})
					return
				}
			}
			downloadURLs := make([]string, 0, len(imageURLs))
			for index := range imageURLs {
				downloadURLs = append(downloadURLs, "/api/v1/miniprogram/templates/"+strconv.FormatInt(id, 10)+"/download-file?image_index="+strconv.Itoa(index))
			}
			templateModel.IncrementDownloadCount(id)
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "下载成功", "data": gin.H{
				"template_id":   id,
				"image_urls":    imageURLs,
				"download_urls": downloadURLs,
			}})
		})

		templatesAuth.GET("/:id/download-file", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
				return
			}
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的模板ID"})
				return
			}
			template, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			if userProfileModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户扩展信息服务不可用"})
				return
			}
			phoneVerified, rechargeMember, canDownload, _, _, accessErr := resolveTemplateDownloadAccess(userOrderModel, userProfileModel, userMembershipModel, codeSession.UserID)
			if accessErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取模板下载资格失败: " + accessErr.Error()})
				return
			}
			if !phoneVerified {
				c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "请先完成手机号授权验证后再下载保存"})
				return
			}
			if !rechargeMember {
				c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "请先完成任意充值后再下载模板图片"})
				return
			}
			if !canDownload {
				c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "当前账号暂不具备模板下载权限"})
				return
			}

			imageIndex, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("image_index", "0")))
			if imageIndex < 0 {
				imageIndex = 0
			}

			// 会员下载限制检查（文件下载也需受限）
			_, _, _, dlMembership2, _, _ := resolveTemplateDownloadAccess(userOrderModel, userProfileModel, userMembershipModel, codeSession.UserID)
			if dlMembership2 != nil {
				dl2 := component.NewDownloadLimiter(model.NewMembershipPlanModel(component.GetDB()), component.GetRedis())
				if allowed, reason, limErr := dl2.CheckDownloadAccess(codeSession.UserID, dlMembership2, id, imageIndex); limErr != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "检查下载限制失败: " + limErr.Error()})
					return
				} else if !allowed {
					c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": reason})
					return
				}
			}

			if !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			targetURL := resolveTemplateDisplayImageURL(template, imageIndex)
			if targetURL == "" {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "当前模板暂无可下载图片"})
				return
			}
			if dlMembership2 != nil {
				dl2 := component.NewDownloadLimiter(model.NewMembershipPlanModel(component.GetDB()), component.GetRedis())
				dl2.RecordDownload(codeSession.UserID, id, imageIndex)
			}
			proxyRemoteDownload(c, targetURL, "template-image.png", "image/png")
		})

		// 增加模板使用次数（点击"做同款"时调用；公开接口，不需要认证）
		templates.POST("/:id/use", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的模板ID"})
				return
			}
			template, err := templateModel.GetByID(id)
			if err != nil || !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			err = templateModel.IncrementDownloadCount(id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新使用次数失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
		})

		// 点赞/取消点赞模板（每人只能点赞一次，再点即取消；需要 token）
		templatesAuth.POST("/:id/like", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
				return
			}
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的模板ID"})
				return
			}
			if templateLikeModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "点赞功能未就绪"})
				return
			}
			liked, _ := templateLikeModel.HasLiked(codeSession.UserID, id)
			if liked {
				if err := templateLikeModel.Unlike(codeSession.UserID, id); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "取消点赞失败: " + err.Error()})
					return
				}
				_ = templateModel.DecrementLikeCount(id)
				template, _ := templateModel.GetByID(id)
				if !isPublicSquareTemplate(template) {
					c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
					return
				}
				c.JSON(http.StatusOK, gin.H{
					"code": 0,
					"msg":  "已取消点赞",
					"data": gin.H{"like_count": template.LikeCount, "liked": false},
				})
				return
			}
			if err := templateLikeModel.Like(codeSession.UserID, id); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "点赞失败: " + err.Error()})
				return
			}
			_ = templateModel.IncrementLikeCount(id)
			template, _ := templateModel.GetByID(id)
			if !isPublicSquareTemplate(template) {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "模板不存在"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "点赞成功",
				"data": gin.H{"like_count": template.LikeCount, "liked": true},
			})
		})

		// 用户提交模板（发布到模板广场，需要审核）
		templatesAuth.POST("", func(c *gin.Context) {
			// 从中间件获取已验证的session信息
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "未通过token验证",
				})
				return
			}

			var req struct {
				Name           string `json:"name" binding:"required"`
				Description    string `json:"description"`
				Category       string `json:"category"`
				MainTab        string `json:"main_tab"`  // 一级 Tab（可选）
				SubTab         string `json:"sub_tab"`   // 二级 Tab（可选）
				ThirdTab       string `json:"third_tab"` // 三级 Tab（可选）
				ImageURL       string `json:"image_url" binding:"required"`
				Prompt         string `json:"prompt"`
				IsFree         *bool  `json:"is_free"`          // 是否免费，默认 true
				Price          int64  `json:"price"`            // 付费时所需灵石，仅当 is_free=false 时有效
				OriginalTaskID *int64 `json:"original_task_id"` // 原始AI任务ID，用于关联原始任务
			}

			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			isFree := true
			if req.IsFree != nil {
				isFree = *req.IsFree
			}
			price := req.Price
			if !isFree && price <= 0 {
				price = 1 // 付费时至少 1 灵石
			}
			if isFree {
				price = 0
			}

			// 创建模板为待审核（status=pending），后台审核通过后改为 published 才会在广场展示
			template := &model.Template{
				Name:           req.Name,
				Description:    strings.TrimSpace(req.Description),
				InternalPrompt: strings.TrimSpace(req.Prompt),
				Category:       req.Category,
				MainTab:        req.MainTab,
				SubTab:         req.SubTab,
				ThirdTab:       req.ThirdTab,
				Thumbnail:      req.ImageURL,
				PreviewURL:     req.ImageURL,
				IsFree:         isFree,
				Price:          price,
				Creator:        "用户投稿",
				CreatorUserID:  codeSession.UserID, // 用于「我的方案」展示
				Status:         "pending",          // 待审核，后台通过后改为 published
				PublishScope:   "square",
				SourceType:     "ai_generated",
			}
			if err := validateTemplateTabAssignment(templateSquareConfigModel, template.MainTab, template.SubTab, template.ThirdTab); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  err.Error(),
				})
				return
			}

			// 如果提供了原始任务ID，设置关联
			if req.OriginalTaskID != nil && *req.OriginalTaskID > 0 {
				template.OriginalTaskID = *req.OriginalTaskID
			}

			if err := validateTemplatePrimaryImage(template); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  err.Error(),
				})
				return
			}
			if err := generateTemplateDerivedImages(context.Background(), template, "templates/user_submit"); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "生成模板衍生图失败: " + err.Error(),
				})
				return
			}
			if err := templateModel.Create(template); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "提交失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "提交成功，等待审核",
				"data": gin.H{
					"id": template.ID,
				},
			})
		})
	}
}

// handleGetTemplateOriginalTask 处理获取模板原始任务信息请求
func handleGetTemplateOriginalTask(c *gin.Context, templateModel *model.TemplateModel, taskModel *model.AITaskModel, codeSessionModel *model.CodeSessionRedisModel) {
	// 验证token
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "未通过token验证",
		})
		return
	}

	// 获取模板ID
	templateID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  "无效的模板ID",
		})
		return
	}

	// 获取模板
	template, err := templateModel.GetByID(templateID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 404,
			"msg":  "模板不存在",
		})
		return
	}

	// 检查模板是否有原始任务ID
	if template.OriginalTaskID == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 404,
			"msg":  "该模板没有关联的原始任务",
		})
		return
	}

	// 获取原始任务
	task, err := taskModel.GetByID(template.OriginalTaskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code": 404,
			"msg":  "原始任务不存在",
		})
		return
	}

	// 解析请求payload
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(task.RequestPayload), &payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "解析任务数据失败",
		})
		return
	}

	// 提取提示词
	prompt := ""
	if v, ok := payload["prompt"].(string); ok {
		prompt = v
	}
	_ = prompt

	// 提取参考图URL（支持多种字段名）
	var imageURLs []string

	// 检查 images 数组
	if images, ok := payload["images"].([]interface{}); ok {
		for _, img := range images {
			if imgStr, ok := img.(string); ok && imgStr != "" {
				imageURLs = append(imageURLs, imgStr)
			}
		}
	}

	// 检查 image_urls 数组
	if imageUrls, ok := payload["image_urls"].([]interface{}); ok {
		for _, img := range imageUrls {
			if imgStr, ok := img.(string); ok && imgStr != "" {
				imageURLs = append(imageURLs, imgStr)
			}
		}
	}

	// 检查 reference_images 数组
	if refImages, ok := payload["reference_images"].([]interface{}); ok {
		for _, img := range refImages {
			if imgStr, ok := img.(string); ok && imgStr != "" {
				imageURLs = append(imageURLs, imgStr)
			}
		}
	}

	// 如果没有数组，检查单个图片字段
	if len(imageURLs) == 0 {
		if v, ok := payload["image"].(string); ok && v != "" {
			imageURLs = append(imageURLs, v)
		}
		if v, ok := payload["image_url"].(string); ok && v != "" {
			imageURLs = append(imageURLs, v)
		}
		if v, ok := payload["original_image"].(string); ok && v != "" {
			imageURLs = append(imageURLs, v)
		}
		if v, ok := payload["size_images"].([]interface{}); ok {
			for _, img := range v {
				if imgStr, ok := img.(string); ok && imgStr != "" {
					imageURLs = append(imageURLs, imgStr)
				}
			}
		}
	}
	if len(imageURLs) == 0 {
		imageURLs = collectTemplateDisplayImageURLs(template)
	}

	referenceImageURLs := append([]string(nil), imageURLs...)
	orderedImageURLs := append([]string(nil), imageURLs...)

	// 返回结果
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "success",
		"data": gin.H{
			"prompt":               "",
			"image_urls":           imageURLs,
			"original_image_urls":  []string{},
			"reference_image_urls": referenceImageURLs,
			"ordered_image_urls":   orderedImageURLs,
		},
	})
}
