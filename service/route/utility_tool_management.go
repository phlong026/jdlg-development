package route

import (
	"net/http"
	"strconv"
	"service/model"
	"github.com/gin-gonic/gin"
)

// RegisterUtilityToolManagementRoutes 注册实用工具管理路由（管理后台）
func RegisterUtilityToolManagementRoutes(r *gin.RouterGroup, utilityToolModel *model.UtilityToolModel) {
	tools := r.Group("/utility-tools")
	{
		// 获取实用工具内容列表
		tools.GET("", func(c *gin.Context) {
			category := c.Query("category")
			keyword := c.Query("keyword")
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			if page < 1 {
				page = 1
			}
			if pageSize < 1 {
				pageSize = 20
			}
			offset := (page - 1) * pageSize

			var isPublished *bool
			if publishedStr := c.Query("is_published"); publishedStr != "" {
				published := publishedStr == "1"
				isPublished = &published
			}

			toolList, err := utilityToolModel.List(category, keyword, isPublished, pageSize, offset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取实用工具内容列表失败: " + err.Error(),
				})
				return
			}

			total, err := utilityToolModel.Count(category, keyword, isPublished)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取总数失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      toolList,
					"total":     total,
					"page":      page,
					"page_size": pageSize,
				},
			})
		})

		// 获取实用工具内容详情
		tools.GET("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的ID",
				})
				return
			}

			tool, err := utilityToolModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "内容不存在",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": tool,
			})
		})

		// 创建实用工具内容
		tools.POST("", func(c *gin.Context) {
			var req struct {
				Category    string `json:"category" binding:"required,oneof=local_norm faq video_tutorial"`
				Title       string `json:"title" binding:"required"`
				Content     string `json:"content"`
				CoverImage  string `json:"cover_image"`
				VideoURL    string `json:"video_url"`
				FileURL     string `json:"file_url"`
				SortOrder   int    `json:"sort_order"`
				IsPublished bool   `json:"is_published"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			tool := &model.UtilityTool{
				Category:    req.Category,
				Title:       req.Title,
				Content:     req.Content,
				CoverImage:  req.CoverImage,
				VideoURL:    req.VideoURL,
				FileURL:     req.FileURL,
				SortOrder:   req.SortOrder,
				IsPublished: req.IsPublished,
			}

			if err := utilityToolModel.Create(tool); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "创建失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "创建成功",
				"data": tool,
			})
		})

		// 更新实用工具内容
		tools.PUT("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的ID",
				})
				return
			}

			var req struct {
				Category    string `json:"category" binding:"required,oneof=local_norm faq video_tutorial"`
				Title       string `json:"title" binding:"required"`
				Content     string `json:"content"`
				CoverImage  string `json:"cover_image"`
				VideoURL    string `json:"video_url"`
				FileURL     string `json:"file_url"`
				SortOrder   int    `json:"sort_order"`
				IsPublished bool   `json:"is_published"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			tool, err := utilityToolModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "内容不存在",
				})
				return
			}

			tool.Category = req.Category
			tool.Title = req.Title
			tool.Content = req.Content
			tool.CoverImage = req.CoverImage
			tool.VideoURL = req.VideoURL
			tool.FileURL = req.FileURL
			tool.SortOrder = req.SortOrder
			tool.IsPublished = req.IsPublished

			if err := utilityToolModel.Update(tool); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "更新失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "更新成功",
				"data": tool,
			})
		})

		// 删除实用工具内容
		tools.DELETE("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的ID",
				})
				return
			}

			if err := utilityToolModel.Delete(id); err != nil {
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
	}
}
