package route

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"service/model"
)

// ───────────────────────── AI 内容抽检管理（二期批次 4a） ─────────────────────────
// 队列页：列表（状态筛选/关键词）+ 审核（通过/驳回）。抽检不拦截用户结果，
// 驳回仅记录结论并提示可转工单跟进。

func RegisterAIContentAuditManagementRoutes(r *gin.RouterGroup, auditModel *model.AIContentAuditModel) {
	ai := r.Group("/ai")

	ai.GET("/content-audits", func(c *gin.Context) {
		if auditModel == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}, "total": 0, "pending_count": 0}})
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
		status := strings.TrimSpace(c.Query("status"))
		keyword := strings.TrimSpace(c.Query("keyword"))
		list, total, err := auditModel.ListForManagement(status, keyword, pageSize, (page-1)*pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取抽检列表失败: " + err.Error()})
			return
		}
		pendingCount, _ := auditModel.PendingCount()
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"list": list, "total": total, "page": page, "page_size": pageSize, "pending_count": pendingCount,
		}})
	})

	ai.POST("/content-audits/:id/review", func(c *gin.Context) {
		if auditModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "抽检服务不可用"})
			return
		}
		id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
			return
		}
		var req struct {
			Status string `json:"status" binding:"required"` // approved / rejected
			Note   string `json:"note"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		if req.Status != model.AIContentAuditStatusApproved && req.Status != model.AIContentAuditStatusRejected {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "status 仅支持 approved / rejected"})
			return
		}
		note := strings.TrimSpace(req.Note)
		if len(note) > 500 {
			note = note[:500]
		}
		updated, reviewErr := auditModel.Review(id, model.AIContentAuditStatus(req.Status), GetUsername(c), note)
		if reviewErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存审核结论失败: " + reviewErr.Error()})
			return
		}
		if !updated {
			c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "该记录已被审核过（仅待审状态可审核）"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "审核结论已保存"})
	})
}
