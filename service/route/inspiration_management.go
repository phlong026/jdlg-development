package route

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"service/model"

	"github.com/gin-gonic/gin"
)

func RegisterInspirationManagementRoutes(r *gin.RouterGroup, inspirationModel *model.InspirationAssetModel) {
	inspirations := r.Group("/inspirations")
	{
		inspirations.GET("", func(c *gin.Context) {
			topic := normalizeInspirationTopic(c.Query("topic"))
			if strings.TrimSpace(c.Query("topic")) == "" {
				topic = ""
			}
			scene := strings.TrimSpace(c.Query("scene"))
			style := strings.TrimSpace(c.Query("style"))
			status := strings.TrimSpace(c.Query("status"))
			keyword := strings.TrimSpace(c.Query("keyword"))
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			if page < 1 {
				page = 1
			}
			if pageSize < 1 {
				pageSize = 20
			}
			offset := (page - 1) * pageSize
			list, err := inspirationModel.List(topic, scene, style, status, keyword, 0, pageSize, offset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取灵感素材列表失败: " + err.Error()})
				return
			}
			total, err := inspirationModel.Count(topic, scene, style, status, keyword, 0)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取灵感素材总数失败: " + err.Error()})
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
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "灵感素材不存在"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": inspirationToResponse(asset)})
		})

		inspirations.POST("", func(c *gin.Context) {
			var req struct {
				Title       string   `json:"title" binding:"required,max=255"`
				Description string   `json:"description"`
				Images      []string `json:"images"`
				Tags        []string `json:"tags"`
				Scene       string   `json:"scene"`
				Style       string   `json:"style"`
				Topic       string   `json:"topic"`
				SortOrder   int      `json:"sort_order"`
				Status      string   `json:"status"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			images := normalizeStringList(req.Images, 9)
			asset := &model.InspirationAsset{
				Title:       strings.TrimSpace(req.Title),
				Description: strings.TrimSpace(req.Description),
				CoverImage:  "",
				Images:      marshalStringList(images),
				Tags:        marshalStringList(req.Tags),
				Scene:       strings.TrimSpace(req.Scene),
				Style:       strings.TrimSpace(req.Style),
				Topic:       normalizeInspirationTopic(req.Topic),
				SortOrder:   req.SortOrder,
				Status:      strings.TrimSpace(req.Status),
				Creator:     GetUsername(c),
			}
			if asset.Status == "" {
				asset.Status = "draft"
			}
			if len(images) > 0 {
				asset.CoverImage = images[0]
			}
			if asset.Creator == "" {
				asset.Creator = "管理员"
			}
			if err := validateInspirationPrimaryImage(asset); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
				return
			}
			if err := generateInspirationDerivedImages(context.Background(), asset, "inspirations/admin"); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "生成灵感衍生图失败: " + err.Error()})
				return
			}
			if err := inspirationModel.Create(asset); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": inspirationToResponse(asset)})
		})

		inspirations.PUT("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的灵感ID"})
				return
			}
			asset, err := inspirationModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "灵感素材不存在"})
				return
			}
			var req struct {
				Title       string   `json:"title" binding:"required,max=255"`
				Description string   `json:"description"`
				Images      []string `json:"images"`
				Tags        []string `json:"tags"`
				Scene       string   `json:"scene"`
				Style       string   `json:"style"`
				Topic       string   `json:"topic"`
				SortOrder   int      `json:"sort_order"`
				Status      string   `json:"status"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			images := normalizeStringList(req.Images, 9)
			asset.Title = strings.TrimSpace(req.Title)
			asset.Description = strings.TrimSpace(req.Description)
			asset.Images = marshalStringList(images)
			asset.Tags = marshalStringList(req.Tags)
			asset.Scene = strings.TrimSpace(req.Scene)
			asset.Style = strings.TrimSpace(req.Style)
			asset.Topic = normalizeInspirationTopic(req.Topic)
			asset.SortOrder = req.SortOrder
			asset.Status = strings.TrimSpace(req.Status)
			if asset.Status == "" {
				asset.Status = "draft"
			}
			asset.CoverImage = ""
			if len(images) > 0 {
				asset.CoverImage = images[0]
			}
			if err := validateInspirationPrimaryImage(asset); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
				return
			}
			if err := generateInspirationDerivedImages(context.Background(), asset, inspirationVariantNamespace(asset)); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "生成灵感衍生图失败: " + err.Error()})
				return
			}
			if err := inspirationModel.Update(asset); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": inspirationToResponse(asset)})
		})

		inspirations.PATCH("/:id/status", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的灵感ID"})
				return
			}
			asset, err := inspirationModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "灵感素材不存在"})
				return
			}
			var req struct {
				Status string `json:"status" binding:"required,oneof=published pending draft archived"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			asset.Status = req.Status
			if err := inspirationModel.Update(asset); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新状态失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "操作成功", "data": inspirationToResponse(asset)})
		})

		inspirations.DELETE("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的灵感ID"})
				return
			}
			if err := inspirationModel.Delete(id); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
		})
	}
}
