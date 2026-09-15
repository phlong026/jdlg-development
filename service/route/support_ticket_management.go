package route

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"service/model"
)

func parseSupportTicketPageParams(c *gin.Context) (int, int, int) {
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
	return page, pageSize, (page - 1) * pageSize
}

func RegisterSupportTicketManagementRoutes(r *gin.RouterGroup, supportTicketModel *model.SupportTicketModel) {
	tickets := r.Group("/support-tickets")

	tickets.GET("/overview", func(c *gin.Context) {
		if supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "工单中心服务不可用"})
			return
		}
		overview, err := supportTicketModel.Overview()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取工单概览失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": overview})
	})

	tickets.GET("", func(c *gin.Context) {
		if supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "工单中心服务不可用"})
			return
		}
		page, pageSize, offset := parseSupportTicketPageParams(c)
		list, total, err := supportTicketModel.ListForManagement(model.SupportTicketListParams{
			Keyword:    c.Query("keyword"),
			Status:     c.DefaultQuery("status", "all"),
			Type:       c.DefaultQuery("type", "all"),
			SourceType: c.DefaultQuery("source_type", "all"),
			Limit:      pageSize,
			Offset:     offset,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取工单列表失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})

	tickets.GET("/:id", func(c *gin.Context) {
		if supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "工单中心服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的工单ID"})
			return
		}
		item, err := supportTicketModel.GetByID(id)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "工单不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取工单详情失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": item})
	})

	tickets.POST("", func(c *gin.Context) {
		if supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "工单中心服务不可用"})
			return
		}
		var req struct {
			UserID    int64  `json:"user_id"`
			Type      string `json:"type"`
			Title     string `json:"title" binding:"required"`
			Content   string `json:"content"`
			Priority  string `json:"priority"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		id, err := supportTicketModel.Create(&model.SupportTicketCreateInput{
			UserID:     req.UserID,
			Type:       req.Type,
			SourceType: "manual",
			SourceID:   "",
			Title:      req.Title,
			Content:    req.Content,
			Priority:   req.Priority,
			CreatedBy:  GetUsername(c),
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建工单失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": id}})
	})

	tickets.POST("/:id/assign", func(c *gin.Context) {
		if supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "工单中心服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的工单ID"})
			return
		}
		adminID := GetUserID(c)
		adminName := GetUsername(c)
		if adminID <= 0 || strings.TrimSpace(adminName) == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "登录状态失效"})
			return
		}
		if err := supportTicketModel.Assign(id, adminID, adminName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "分配工单失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
	})

	tickets.POST("/:id/status", func(c *gin.Context) {
		if supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "工单中心服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的工单ID"})
			return
		}
		var req struct {
			Status string `json:"status" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		if err := supportTicketModel.UpdateStatus(id, req.Status); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新工单状态失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
	})

	tickets.POST("/:id/resolution-note", func(c *gin.Context) {
		if supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "工单中心服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的工单ID"})
			return
		}
		var req struct {
			ResolutionNote string `json:"resolution_note"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		if err := supportTicketModel.UpdateResolutionNote(id, req.ResolutionNote); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新工单处理备注失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
	})

	tickets.POST("/sync-system-exceptions", func(c *gin.Context) {
		if supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "工单中心服务不可用"})
			return
		}
		createdCount, err := syncSupportTicketSystemExceptions(supportTicketModel)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "同步系统异常工单失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"created_count": createdCount}})
	})
}

func syncSupportTicketSystemExceptions(ticketModel *model.SupportTicketModel) (int64, error) {
	db := ticketModel.DB
	createdCount := int64(0)

	orderRows, err := db.Query(`
		SELECT o.id, o.user_id, o.order_no, o.status, o.title, o.description, o.created_at
		FROM user_orders o
		WHERE o.status IN ('failed', 'pending') AND o.created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
		ORDER BY o.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return 0, err
	}
	defer orderRows.Close()
	for orderRows.Next() {
		var id int64
		var userID int64
		var orderNo string
		var status string
		var title string
		var description string
		var createdAt time.Time
		if err := orderRows.Scan(&id, &userID, &orderNo, &status, &title, &description, &createdAt); err != nil {
			return 0, err
		}
		sourceID := strconv.FormatInt(id, 10)
		exists, err := ticketModel.ExistsOpenBySource("order", sourceID)
		if err != nil {
			return 0, err
		}
		if exists {
			continue
		}
		_, err = ticketModel.Create(&model.SupportTicketCreateInput{
			UserID:     userID,
			Type:       "order",
			SourceType: "order",
			SourceID:   sourceID,
			Title:      fmt.Sprintf("异常订单待跟进：%s", orderNo),
			Content:    fmt.Sprintf("订单状态：%s；标题：%s；描述：%s；异常时间：%s", status, title, description, createdAt.Format("2006-01-02 15:04:05")),
			Priority:   supportTicketPriorityByStatus(status),
			CreatedBy:  "system",
			SourcePayload: map[string]interface{}{
				"order_id":    id,
				"order_no":    orderNo,
				"status":      status,
				"title":       title,
				"description": description,
			},
		})
		if err != nil {
			return 0, err
		}
		createdCount++
	}
	if err := orderRows.Err(); err != nil {
		return 0, err
	}

	taskRows, err := db.Query(`
		SELECT t.source_id, t.user_id, t.task_no, t.task_type, t.scene, t.error_message, t.created_at
		FROM (
			SELECT CONCAT('image-', a.id) AS source_id, a.user_id, a.task_no, 'image' AS task_type, a.scene, COALESCE(a.error_message, '') AS error_message, a.created_at
			FROM ai_tasks a
			WHERE a.status = 'failed' AND a.created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
			UNION ALL
			SELECT CONCAT('video-', v.id) AS source_id, v.user_id, CONCAT('v', v.id) AS task_no, 'video' AS task_type, v.model AS scene, COALESCE(v.error_message, '') AS error_message, v.created_at
			FROM ai_video_tasks v
			WHERE v.status = 'failed' AND v.created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
		) t
		ORDER BY t.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return 0, err
	}
	defer taskRows.Close()
	for taskRows.Next() {
		var sourceID string
		var userID int64
		var taskNo string
		var taskType string
		var scene string
		var errorMessage string
		var createdAt time.Time
		if err := taskRows.Scan(&sourceID, &userID, &taskNo, &taskType, &scene, &errorMessage, &createdAt); err != nil {
			return 0, err
		}
		exists, err := ticketModel.ExistsOpenBySource("task", sourceID)
		if err != nil {
			return 0, err
		}
		if exists {
			continue
		}
		_, err = ticketModel.Create(&model.SupportTicketCreateInput{
			UserID:     userID,
			Type:       "task",
			SourceType: "task",
			SourceID:   sourceID,
			Title:      fmt.Sprintf("失败任务待跟进：%s", taskNo),
			Content:    fmt.Sprintf("任务类型：%s；场景：%s；错误：%s；失败时间：%s", taskType, scene, errorMessage, createdAt.Format("2006-01-02 15:04:05")),
			Priority:   "high",
			CreatedBy:  "system",
			SourcePayload: map[string]interface{}{
				"task_no":        taskNo,
				"task_type":      taskType,
				"scene":          scene,
				"error_message":  errorMessage,
			},
		})
		if err != nil {
			return 0, err
		}
		createdCount++
	}
	if err := taskRows.Err(); err != nil {
		return 0, err
	}

	return createdCount, nil
}

func supportTicketPriorityByStatus(status string) string {
	if strings.TrimSpace(status) == "failed" {
		return "high"
	}
	return "medium"
}
