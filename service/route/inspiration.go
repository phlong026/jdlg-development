package route

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"service/model"

	"github.com/gin-gonic/gin"
)

func parseJSONStringArray(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return []string{}
	}
	return normalizeStringList(list, 20)
}

func normalizeStringList(values []string, maxCount int) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		result = append(result, clean)
		if maxCount > 0 && len(result) >= maxCount {
			break
		}
	}
	return result
}

func marshalStringList(values []string) string {
	list := normalizeStringList(values, 20)
	if len(list) == 0 {
		return "[]"
	}
	bytes, err := json.Marshal(list)
	if err != nil {
		return "[]"
	}
	return string(bytes)
}

func normalizeInspirationTopic(raw string) string {
	topic := strings.TrimSpace(raw)
	switch topic {
	case "scene", "style", "villa", "inspiration":
		return topic
	default:
		return "inspiration"
	}
}

func inspirationToResponse(asset *model.InspirationAsset) gin.H {
	return gin.H{
		"id":              asset.ID,
		"title":           asset.Title,
		"description":     asset.Description,
		"cover_image":     asset.CoverImage,
		"images":          parseJSONStringArray(asset.Images),
		"image_width":     asset.ImageWidth,
		"image_height":    asset.ImageHeight,
		"tags":            parseJSONStringArray(asset.Tags),
		"scene":           asset.Scene,
		"style":           asset.Style,
		"topic":           asset.Topic,
		"sort_order":      asset.SortOrder,
		"status":          asset.Status,
		"creator":         asset.Creator,
		"creator_user_id": asset.CreatorUserID,
		"view_count":      asset.ViewCount,
		"like_count":      asset.LikeCount,
		"created_at":      asset.CreatedAt,
		"updated_at":      asset.UpdatedAt,
	}
}

func RegisterInspirationRoutes(r *gin.RouterGroup, inspirationModel *model.InspirationAssetModel) {
	inspirations := r.Group("/inspirations")
	{
		inspirations.GET("", func(c *gin.Context) {
			topic := normalizeInspirationTopic(c.Query("topic"))
			if strings.TrimSpace(c.Query("topic")) == "" {
				topic = ""
			}
			scene := strings.TrimSpace(c.Query("scene"))
			style := strings.TrimSpace(c.Query("style"))
			keyword := strings.TrimSpace(c.Query("keyword"))
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			if page < 1 {
				page = 1
			}
			if pageSize < 1 || pageSize > 50 {
				pageSize = 20
			}
			offset := (page - 1) * pageSize

			list, err := inspirationModel.List(topic, scene, style, "published", keyword, 0, pageSize, offset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取灵感列表失败: " + err.Error()})
				return
			}
			total, err := inspirationModel.Count(topic, scene, style, "published", keyword, 0)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取灵感总数失败: " + err.Error()})
				return
			}
			result := make([]gin.H, 0, len(list))
			for _, asset := range list {
				result = append(result, inspirationToResponse(asset))
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      result,
					"total":     total,
					"page":      page,
					"page_size": pageSize,
				},
			})
		})

		inspirations.GET("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的灵感ID"})
				return
			}
			asset, err := inspirationModel.GetByID(id)
			if err != nil || asset == nil || asset.Status != "published" {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "灵感内容不存在"})
				return
			}
			_ = inspirationModel.IncrementViewCount(id)
			asset.ViewCount += 1
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": inspirationToResponse(asset)})
		})
	}
}
