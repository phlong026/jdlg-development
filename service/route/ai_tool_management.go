package route

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"service/model"
)

type aiToolManagementRequest struct {
	Code              string                        `json:"code" binding:"required"`
	Name              string                        `json:"name" binding:"required"`
	Category          string                        `json:"category" binding:"required,oneof=architecture interior landscape planning"`
	ShortDescription  string                        `json:"short_description"`
	DetailDescription string                        `json:"detail_description"`
	ListCoverImage    string                        `json:"list_cover_image"`
	DetailBeforeImage string                        `json:"detail_before_image"`
	DetailAfterImage  string                        `json:"detail_after_image"`
	PromptPlaceholder string                        `json:"prompt_placeholder"`
	DefaultPrompt     string                        `json:"default_prompt"`
	UploadHint        string                        `json:"upload_hint"`
	ShowUsageTips     bool                          `json:"show_usage_tips"`
	UsageTipsTitle    string                        `json:"usage_tips_title"`
	UsageTipsContent  string                        `json:"usage_tips_content"`
	SortOrder         int                           `json:"sort_order"`
	IsPublished       bool                          `json:"is_published"`
	IsCommon          bool                          `json:"is_common"`
	PresetReferences  []model.AIToolPresetReference `json:"preset_references"`
	StylePresets      []model.AIToolStylePreset     `json:"style_presets"`
}

func normalizeAIToolManagementRequest(req *aiToolManagementRequest) {
	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	req.Category = strings.TrimSpace(req.Category)
	req.ShortDescription = strings.TrimSpace(req.ShortDescription)
	req.DetailDescription = strings.TrimSpace(req.DetailDescription)
	req.ListCoverImage = strings.TrimSpace(req.ListCoverImage)
	req.DetailBeforeImage = strings.TrimSpace(req.DetailBeforeImage)
	req.DetailAfterImage = strings.TrimSpace(req.DetailAfterImage)
	req.PromptPlaceholder = strings.TrimSpace(req.PromptPlaceholder)
	req.DefaultPrompt = strings.TrimSpace(req.DefaultPrompt)
	req.UploadHint = strings.TrimSpace(req.UploadHint)
	req.UsageTipsTitle = strings.TrimSpace(req.UsageTipsTitle)
	req.UsageTipsContent = strings.TrimSpace(req.UsageTipsContent)
	if req.UsageTipsTitle == "" {
		req.UsageTipsTitle = "使用提示"
	}
	for index := range req.PresetReferences {
		req.PresetReferences[index].ID = strings.TrimSpace(req.PresetReferences[index].ID)
		req.PresetReferences[index].Name = strings.TrimSpace(req.PresetReferences[index].Name)
		req.PresetReferences[index].Description = strings.TrimSpace(req.PresetReferences[index].Description)
		req.PresetReferences[index].ImageURL = strings.TrimSpace(req.PresetReferences[index].ImageURL)
		req.PresetReferences[index].PromptSuffix = strings.TrimSpace(req.PresetReferences[index].PromptSuffix)
	}
	for index := range req.StylePresets {
		req.StylePresets[index].ID = strings.TrimSpace(req.StylePresets[index].ID)
		req.StylePresets[index].Name = strings.TrimSpace(req.StylePresets[index].Name)
		req.StylePresets[index].ReferenceMode = strings.TrimSpace(req.StylePresets[index].ReferenceMode)
		req.StylePresets[index].PreviewImageURL = strings.TrimSpace(req.StylePresets[index].PreviewImageURL)
		req.StylePresets[index].ModelReferenceImageURL = strings.TrimSpace(req.StylePresets[index].ModelReferenceImageURL)
		req.StylePresets[index].ImageURL = strings.TrimSpace(req.StylePresets[index].ImageURL)
		if req.StylePresets[index].ReferenceMode == "" {
			if req.StylePresets[index].ImageURL == "" && req.StylePresets[index].ModelReferenceImageURL == "" {
				req.StylePresets[index].ReferenceMode = model.AIToolReferenceModePreviewOnly
			} else {
				req.StylePresets[index].ReferenceMode = model.AIToolReferenceModePreviewAndModel
			}
		}
		if req.StylePresets[index].ReferenceMode != model.AIToolReferenceModePreviewOnly {
			req.StylePresets[index].ReferenceMode = model.AIToolReferenceModePreviewAndModel
		}
		if req.StylePresets[index].PreviewImageURL == "" {
			req.StylePresets[index].PreviewImageURL = req.StylePresets[index].ImageURL
		}
		if req.StylePresets[index].ReferenceMode == model.AIToolReferenceModePreviewAndModel && req.StylePresets[index].ModelReferenceImageURL == "" {
			req.StylePresets[index].ModelReferenceImageURL = req.StylePresets[index].ImageURL
		}
		if req.StylePresets[index].ImageURL == "" {
			req.StylePresets[index].ImageURL = req.StylePresets[index].PreviewImageURL
		}
		req.StylePresets[index].PromptSuffix = strings.TrimSpace(req.StylePresets[index].PromptSuffix)
	}
}

func buildAIToolFromManagementRequest(req *aiToolManagementRequest) (*model.AITool, error) {
	presetReferences, err := json.Marshal(req.PresetReferences)
	if err != nil {
		return nil, err
	}
	stylePresets, err := json.Marshal(req.StylePresets)
	if err != nil {
		return nil, err
	}
	return &model.AITool{
		Code:                  req.Code,
		Name:                  req.Name,
		Category:              req.Category,
		ShortDescription:      req.ShortDescription,
		DetailDescription:     req.DetailDescription,
		ListCoverImage:        req.ListCoverImage,
		DetailBeforeImage:     req.DetailBeforeImage,
		DetailAfterImage:      req.DetailAfterImage,
		PromptPlaceholder:     req.PromptPlaceholder,
		DefaultPrompt:         req.DefaultPrompt,
		UploadHint:            req.UploadHint,
		ShowUsageTips:         req.ShowUsageTips,
		UsageTipsTitle:        req.UsageTipsTitle,
		UsageTipsContent:      req.UsageTipsContent,
		SortOrder:             req.SortOrder,
		IsPublished:           req.IsPublished,
		IsCommon:              req.IsCommon,
		PresetReferenceImages: string(presetReferences),
		StylePresetsRaw:       string(stylePresets),
	}, nil
}

func validateImageURL(imageURL string, fieldName string) string {
	if imageURL == "" {
		return ""
	}
	parsedURL, err := url.Parse(imageURL)
	if err != nil {
		return fieldName + "不是有效的URL"
	}
	if parsedURL.Scheme != "https" {
		return fieldName + "必须使用HTTPS协议"
	}
	return ""
}

func validateAIToolManagementRequest(req *aiToolManagementRequest) string {
	if req.Code == "" {
		return "工具编码不能为空"
	}
	if req.Name == "" {
		return "工具名称不能为空"
	}
	if len(req.Name) > 100 {
		return "工具名称不能超过100个字符"
	}
	if len(req.ShortDescription) > 200 {
		return "工具短描述不能超过200个字符"
	}
	if len(req.DetailDescription) > 2000 {
		return "工具详细描述不能超过2000个字符"
	}
	if len(req.DefaultPrompt) > 4000 {
		return "默认提示词不能超过4000个字符"
	}
	if len(req.UsageTipsTitle) > 128 {
		return "使用提示标题不能超过128个字符"
	}
	if len(req.UsageTipsContent) > 4000 {
		return "使用提示内容不能超过4000个字符"
	}
	// 校验图片URL必须使用HTTPS
	if errMsg := validateImageURL(req.ListCoverImage, "列表封面图"); errMsg != "" {
		return errMsg
	}
	if errMsg := validateImageURL(req.DetailBeforeImage, "详情前图"); errMsg != "" {
		return errMsg
	}
	if errMsg := validateImageURL(req.DetailAfterImage, "详情后图"); errMsg != "" {
		return errMsg
	}
	presetIDs := make(map[string]struct{})
	for _, item := range req.PresetReferences {
		if item.ID == "" || item.Name == "" {
			return "预设参考图的ID和名称不能为空"
		}
		if errMsg := validateImageURL(item.ImageURL, "预设参考图["+item.Name+"]的图片URL"); errMsg != "" {
			return errMsg
		}
		if _, exists := presetIDs[item.ID]; exists {
			return "预设参考图ID不能重复"
		}
		presetIDs[item.ID] = struct{}{}
	}
	styleIDs := make(map[string]struct{})
	for _, item := range req.StylePresets {
		if item.ID == "" || item.Name == "" {
			return "风格项的ID和名称不能为空"
		}
		if item.ReferenceMode != model.AIToolReferenceModePreviewOnly && item.ReferenceMode != model.AIToolReferenceModePreviewAndModel {
			return "风格项[" + item.Name + "]的模式无效"
		}
		if errMsg := validateImageURL(item.PreviewImageURL, "风格项["+item.Name+"]的用户预览图URL"); errMsg != "" {
			return errMsg
		}
		if errMsg := validateImageURL(item.ModelReferenceImageURL, "风格项["+item.Name+"]的AI引用图URL"); errMsg != "" {
			return errMsg
		}
		if errMsg := validateImageURL(item.ImageURL, "风格项["+item.Name+"]的旧版图片URL"); errMsg != "" {
			return errMsg
		}
		if item.ReferenceMode == model.AIToolReferenceModePreviewAndModel && strings.TrimSpace(item.ModelReferenceImageURL) == "" {
			return "风格项[" + item.Name + "]选择“用户预览 + AI引用”时必须配置AI引用图"
		}
		if _, exists := styleIDs[item.ID]; exists {
			return "风格项ID不能重复"
		}
		styleIDs[item.ID] = struct{}{}
	}
	return ""
}

func RegisterAIToolManagementRoutes(r *gin.RouterGroup, aiToolModel *model.AIToolModel, taskModel *model.AITaskModel) {
	tools := r.Group("/ai-tools")
	{
		tools.GET("", func(c *gin.Context) {
			category := strings.TrimSpace(c.Query("category"))
			keyword := strings.TrimSpace(c.Query("keyword"))
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			if page < 1 {
				page = 1
			}
			if pageSize < 1 {
				pageSize = 20
			}
			if pageSize > 100 {
				pageSize = 100
			}
			offset := (page - 1) * pageSize

			var isPublished *bool
			if publishedStr := strings.TrimSpace(c.Query("is_published")); publishedStr != "" {
				published := publishedStr == "1" || strings.EqualFold(publishedStr, "true")
				isPublished = &published
			}

			list, err := aiToolModel.List(category, keyword, isPublished, pageSize, offset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取 AI 工具列表失败: " + err.Error()})
				return
			}
			total, err := aiToolModel.Count(category, keyword, isPublished)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取 AI 工具总数失败: " + err.Error()})
				return
			}
			responseList := make([]gin.H, 0, len(list))
			for _, item := range list {
				responseList = append(responseList, buildAIToolManagementResponse(item))
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      responseList,
					"total":     total,
					"page":      page,
					"page_size": pageSize,
				},
			})
		})

		tools.GET("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
				return
			}
			tool, err := aiToolModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "工具不存在"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": buildAIToolManagementResponse(tool)})
		})

		tools.POST("", func(c *gin.Context) {
			var req aiToolManagementRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			normalizeAIToolManagementRequest(&req)
			if validateMsg := validateAIToolManagementRequest(&req); validateMsg != "" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": validateMsg})
				return
			}
			tool, err := buildAIToolFromManagementRequest(&req)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "工具配置序列化失败: " + err.Error()})
				return
			}
			if err := aiToolModel.Create(tool); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建失败: " + err.Error()})
				return
			}
			createdTool, err := aiToolModel.GetByID(tool.ID)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": buildAIToolManagementResponse(tool)})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": buildAIToolManagementResponse(createdTool)})
		})

		tools.PUT("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
				return
			}
			storedTool, err := aiToolModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "工具不存在"})
				return
			}
			var req aiToolManagementRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			normalizeAIToolManagementRequest(&req)
			if validateMsg := validateAIToolManagementRequest(&req); validateMsg != "" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": validateMsg})
				return
			}
			tool, err := buildAIToolFromManagementRequest(&req)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "工具配置序列化失败: " + err.Error()})
				return
			}
			tool.ID = storedTool.ID
			tool.UsageCount = storedTool.UsageCount
			tool.CreatedAt = storedTool.CreatedAt
			tool.UpdatedAt = storedTool.UpdatedAt
			if err := aiToolModel.Update(tool); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败: " + err.Error()})
				return
			}
			updatedTool, err := aiToolModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": buildAIToolManagementResponse(tool)})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": buildAIToolManagementResponse(updatedTool)})
		})

		tools.DELETE("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
				return
			}
			if taskModel != nil {
				relatedTaskCount, countErr := taskModel.CountByToolID(id)
				if countErr != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "检查工具关联任务失败: " + countErr.Error()})
					return
				}
				if relatedTaskCount > 0 {
					c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "该工具已有关联生成记录，不能直接删除，请先下线"})
					return
				}
			}
			if err := aiToolModel.Delete(id); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
		})
	}
}
