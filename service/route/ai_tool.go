package route

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"service/model"
)

type aiToolPublicReader interface {
	GetByID(int64) (*model.AITool, error)
	GetByCode(string) (*model.AITool, error)
	List(string, string, *bool, int, int) ([]*model.AITool, error)
	Count(string, string, *bool) (int64, error)
}

type aiToolPublicPresetReference struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url"`
}

type aiToolPublicStylePreset struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ReferenceMode   string `json:"reference_mode"`
	PreviewImageURL string `json:"preview_image_url"`
}

func buildAIToolPublicPresetReferences(tool *model.AITool) []aiToolPublicPresetReference {
	if tool == nil {
		return []aiToolPublicPresetReference{}
	}
	items := tool.GetPresetReferences()
	result := make([]aiToolPublicPresetReference, 0, len(items))
	for _, item := range items {
		result = append(result, aiToolPublicPresetReference{
			ID: item.ID, Name: item.Name, Description: item.Description, ImageURL: item.ImageURL,
		})
	}
	return result
}

func buildAIToolPublicStylePresets(tool *model.AITool) []aiToolPublicStylePreset {
	if tool == nil {
		return []aiToolPublicStylePreset{}
	}
	items := tool.GetStylePresets()
	result := make([]aiToolPublicStylePreset, 0, len(items))
	for _, item := range items {
		previewImageURL := strings.TrimSpace(item.PreviewImageURL)
		if previewImageURL == "" {
			// ImageURL is the legacy shared preview/model field. A dedicated
			// model-only URL is an execution rule and is never a public fallback.
			previewImageURL = strings.TrimSpace(item.ImageURL)
		}
		result = append(result, aiToolPublicStylePreset{
			ID: item.ID, Name: item.Name, ReferenceMode: item.EffectiveReferenceMode(), PreviewImageURL: previewImageURL,
		})
	}
	return result
}

func buildAIToolStylePresetResponse(tool *model.AITool) []model.AIToolStylePreset {
	if tool == nil {
		return []model.AIToolStylePreset{}
	}
	list := tool.GetStylePresets()
	for index := range list {
		list[index].ReferenceMode = list[index].EffectiveReferenceMode()
		list[index].PreviewImageURL = list[index].EffectivePreviewImageURL()
		list[index].ModelReferenceImageURL = list[index].EffectiveModelReferenceImageURL()
		if strings.TrimSpace(list[index].ImageURL) == "" {
			list[index].ImageURL = list[index].PreviewImageURL
		}
	}
	return list
}

func buildAIToolBaseResponse(tool *model.AITool) gin.H {
	if tool == nil {
		return gin.H{}
	}
	tags := []string{tool.Category}
	if tool.IsCommon {
		tags = append(tags, "常用")
	}
	return gin.H{
		"id":                  tool.ID,
		"code":                tool.Code,
		"name":                tool.Name,
		"category":            tool.Category,
		"short_description":   tool.ShortDescription,
		"detail_description":  tool.DetailDescription,
		"list_cover_image":    tool.ListCoverImage,
		"detail_before_image": tool.DetailBeforeImage,
		"detail_after_image":  tool.DetailAfterImage,
		"prompt_placeholder":  tool.PromptPlaceholder,
		"upload_hint":         tool.UploadHint,
		"show_usage_tips":     tool.ShowUsageTips,
		"usage_tips_title":    tool.UsageTipsTitle,
		"usage_tips_content":  tool.UsageTipsContent,
		"sort_order":          tool.SortOrder,
		"is_published":        tool.IsPublished,
		"is_common":           tool.IsCommon,
		"usage_count":         tool.UsageCount,
		"tags":                tags,
	}
}

func buildAIToolPublicResponse(tool *model.AITool) gin.H {
	response := buildAIToolBaseResponse(tool)
	if len(response) == 0 {
		return gin.H{}
	}
	response["preset_references"] = buildAIToolPublicPresetReferences(tool)
	response["style_presets"] = buildAIToolPublicStylePresets(tool)
	return response
}

func buildAIToolManagementResponse(tool *model.AITool) gin.H {
	response := buildAIToolBaseResponse(tool)
	if len(response) == 0 {
		return gin.H{}
	}
	response["preset_references"] = tool.GetPresetReferences()
	response["style_presets"] = buildAIToolStylePresetResponse(tool)
	response["default_prompt"] = tool.DefaultPrompt
	response["created_at"] = tool.CreatedAt
	response["updated_at"] = tool.UpdatedAt
	return response
}

func RegisterAIToolRoutes(r *gin.RouterGroup, aiToolModel aiToolPublicReader) {
	tools := r.Group("/ai-tools")
	{
		tools.GET("", func(c *gin.Context) {
			category := strings.TrimSpace(c.Query("category"))
			keyword := strings.TrimSpace(c.Query("keyword"))
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))
			if page < 1 {
				page = 1
			}
			if pageSize < 1 {
				pageSize = 100
			}
			if pageSize > 200 {
				pageSize = 200
			}
			offset := (page - 1) * pageSize
			isPublished := true
			list, err := aiToolModel.List(category, keyword, &isPublished, pageSize, offset)
			if err != nil {
				log.Printf("[ai-tool-public] list failed error_type=%T", err)
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取 AI 工具列表失败"})
				return
			}
			total, err := aiToolModel.Count(category, keyword, &isPublished)
			if err != nil {
				log.Printf("[ai-tool-public] count failed error_type=%T", err)
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取 AI 工具总数失败"})
				return
			}
			responseList := make([]gin.H, 0, len(list))
			for _, item := range list {
				responseList = append(responseList, buildAIToolPublicResponse(item))
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

		tools.GET("/code/:code", func(c *gin.Context) {
			code := strings.TrimSpace(c.Param("code"))
			if code == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的工具编码",
				})
				return
			}
			tool, err := aiToolModel.GetByCode(code)
			if err != nil || tool == nil || !tool.IsPublished {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "工具不存在",
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": buildAIToolPublicResponse(tool),
			})
		})

		tools.GET("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的ID",
				})
				return
			}
			tool, err := aiToolModel.GetByID(id)
			if err != nil || tool == nil || !tool.IsPublished {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "工具不存在",
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": buildAIToolPublicResponse(tool),
			})
		})
	}
}
