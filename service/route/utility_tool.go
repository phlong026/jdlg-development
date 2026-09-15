package route

import (
	"net/http"
	"strconv"
	"service/model"
	"github.com/gin-gonic/gin"
)

// RegisterUtilityToolRoutes 注册小程序实用工具路由（公开接口）
func RegisterUtilityToolRoutes(r *gin.RouterGroup, utilityToolModel *model.UtilityToolModel) {
	tools := r.Group("/utility-tools")
	{
		// 获取实用工具内容列表（按分类）
		tools.GET("", func(c *gin.Context) {
			category := c.Query("category") // local_norm, faq, video_tutorial
			limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
			if limit < 1 {
				limit = 50
			}
			if limit > 100 {
				limit = 100
			}

			isPublished := true
			toolList, err := utilityToolModel.List(category, "", &isPublished, limit, 0)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取实用工具内容失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list": toolList,
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

			// 只有已发布的内容才能查看
			if !tool.IsPublished {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "内容不存在",
				})
				return
			}

			// 增加查看次数
			_ = utilityToolModel.IncrementViewCount(id)

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": tool,
			})
		})
	}
}
