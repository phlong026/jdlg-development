package route

import (
	"bytes"
	"time"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"service/model"
)

func supportTicketPriorityByOrderStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "failed":
		return "high"
	case "pending":
		return "medium"
	default:
		return "low"
	}
}

func RegisterOrderManagementRoutes(r *gin.RouterGroup, userOrderModel *model.UserOrderModel, userDBModel *model.UserModel, certificationModel *model.CertificationApplicationModel, userMembershipModel *model.UserMembershipModel, supportTicketModel *model.SupportTicketModel) {
	orders := r.Group("/orders")

	orders.GET("", func(c *gin.Context) {
		if userOrderModel == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}, "summary": gin.H{}, "total": 0, "page": 1, "page_size": 20}})
			return
		}
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
		keyword := strings.TrimSpace(c.Query("keyword"))
		orderType := strings.TrimSpace(c.DefaultQuery("type", "all"))
		orderCategory := strings.TrimSpace(c.DefaultQuery("order_category", "all"))
		status := strings.TrimSpace(c.DefaultQuery("status", "all"))
		payChannel := strings.TrimSpace(c.DefaultQuery("pay_channel", "all"))
		startDate := strings.TrimSpace(c.Query("start_date"))
		endDate := strings.TrimSpace(c.Query("end_date"))

		list, total, err := userOrderModel.ListForManagement(keyword, orderType, orderCategory, status, payChannel, startDate, endDate, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取订单列表失败: " + err.Error()})
			return
		}
		summary, err := userOrderModel.SummaryForManagement(keyword, orderType, orderCategory, status, payChannel, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取订单汇总失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"list":      list,
				"summary":   summary,
				"total":     total,
				"page":      page,
				"page_size": pageSize,
			},
		})
	})


	// 订单导出 CSV（B4）：复用列表的全部筛选条件，导出前 10000 条（上限防拖垮）。
	// 文件名带导出时间与筛选状态，财务对账可直接归档。
	orders.GET("/export", func(c *gin.Context) {
		if userOrderModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "订单服务不可用"})
			return
		}
		keyword := strings.TrimSpace(c.Query("keyword"))
		orderType := strings.TrimSpace(c.DefaultQuery("type", "all"))
		orderCategory := strings.TrimSpace(c.DefaultQuery("order_category", "all"))
		status := strings.TrimSpace(c.DefaultQuery("status", "all"))
		payChannel := strings.TrimSpace(c.DefaultQuery("pay_channel", "all"))
		startDate := strings.TrimSpace(c.Query("start_date"))
		endDate := strings.TrimSpace(c.Query("end_date"))

		const exportLimit = 10000
		list, _, err := userOrderModel.ListForManagement(keyword, orderType, orderCategory, status, payChannel, startDate, endDate, exportLimit, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "导出订单失败: " + err.Error()})
			return
		}

		var buf bytes.Buffer
		buf.WriteString("\uFEFF") // Excel 直接打开不乱码
		buf.WriteString("订单号,用户ID,用户名,类型,类别,标题,金额,状态,支付渠道,创建时间\n")
		for _, o := range list {
			cells := []string{
				o.OrderNo, strconv.FormatInt(o.UserID, 10), o.Username,
				o.Type, o.OrderCategory, o.Title,
				strconv.FormatInt(o.Amount, 10), o.Status, o.PayChannel,
				o.CreatedAt.Format("2006-01-02 15:04:05"),
			}
			for i, cell := range cells {
				// CSV 注入防护：以 = + - @ 开头的单元格前置单引号，避免 Excel 当公式执行
				if strings.HasPrefix(cell, "=") || strings.HasPrefix(cell, "+") || strings.HasPrefix(cell, "-") || strings.HasPrefix(cell, "@") {
					cells[i] = "'" + cell
				}
				if strings.ContainsAny(cells[i], ",\"\n") {
					cells[i] = "\"" + strings.ReplaceAll(cells[i], "\"", "\"\"") + "\""
				}
			}
			buf.WriteString(strings.Join(cells, ",") + "\n")
		}

		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=orders-%s.csv", time.Now().Format("20060102-150405")))
		c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
	})

	orders.GET("/:id", func(c *gin.Context) {
		if userOrderModel == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "订单不存在"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的订单ID"})
			return
		}
		order, err := userOrderModel.GetByID(id)
		if err != nil || order == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "订单不存在"})
			return
		}
		var userPayload gin.H
		if userDBModel != nil {
			user, userErr := userDBModel.GetByID(order.UserID)
			if userErr == nil && user != nil {
				if certificationModel != nil {
					if currentCanWithdraw, syncErr := certificationModel.SyncUserCanWithdraw(userDBModel, user.ID); syncErr == nil {
						user.CanWithdraw = currentCanWithdraw
					}
				}
				userPayload = gin.H{
					"id":           user.ID,
					"username":     user.Username,
					"user_type":    user.UserType,
					"can_withdraw": user.CanWithdraw,
					"created_at":   user.CreatedAt,
				}
			}
		}
		var membershipPayload gin.H
		if userMembershipModel != nil {
			membership, membershipErr := userMembershipModel.GetByUserID(order.UserID)
			if membershipErr == nil && membership != nil {
				membershipPayload = gin.H{
					"plan_code":                 membership.PlanCode,
					"plan_title":                membership.PlanTitle,
					"status":                    membership.Status,
					"template_download_enabled": membership.TemplateDownloadEnabled,
					"started_at":                membership.StartedAt,
					"granted_at":                membership.GrantedAt,
					"expired_at":                membership.ExpiredAt,
					"source_order_no":           membership.SourceOrderNo,
					"is_lifetime":               model.IsLifetimeMembership(membership),
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"order":      order,
				"user":       userPayload,
				"membership": membershipPayload,
			},
		})
	})

	orders.POST("/:id/support-ticket", func(c *gin.Context) {
		if userOrderModel == nil || supportTicketModel == nil || supportTicketModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "订单或工单服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的订单ID"})
			return
		}
		order, err := userOrderModel.GetByID(id)
		if err != nil || order == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "订单不存在"})
			return
		}
		sourceID := strconv.FormatInt(order.ID, 10)
		if existingID, existingErr := supportTicketModel.GetLatestOpenTicketIDBySource("order", sourceID); existingErr == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": existingID, "existed": true}})
			return
		} else if existingErr != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询已有工单失败: " + existingErr.Error()})
			return
		}

		ticketID, createErr := supportTicketModel.Create(&model.SupportTicketCreateInput{
			UserID:     order.UserID,
			Type:       "order",
			SourceType: "order",
			SourceID:   sourceID,
			Title:      fmt.Sprintf("订单异常待跟进：%s", order.OrderNo),
			Content:    fmt.Sprintf("订单状态：%s；类型：%s；分类：%s；数值：%d；标题：%s；说明：%s", order.Status, order.Type, order.OrderCategory, order.Amount, order.Title, order.Description),
			Priority:   supportTicketPriorityByOrderStatus(order.Status),
			CreatedBy:  GetUsername(c),
			SourcePayload: map[string]interface{}{
				"order_id":       order.ID,
				"order_no":       order.OrderNo,
				"status":         order.Status,
				"type":           order.Type,
				"order_category": order.OrderCategory,
				"amount":         order.Amount,
				"title":          order.Title,
				"description":    order.Description,
				"completed_at":   order.CompletedAt,
				"created_at":     order.CreatedAt,
			},
		})
		if createErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建异常工单失败: " + createErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"id": ticketID, "existed": false}})
	})
}
