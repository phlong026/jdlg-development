package route

import (
	"fmt"
	"net/http"
	"service/model"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func RegisterMembershipManagementRoutes(r *gin.RouterGroup, membershipPlanModel *model.MembershipPlanModel, userMembershipModel *model.UserMembershipModel) {
	membership := r.Group("/membership-plans")
	membership.GET("", func(c *gin.Context) {
		if membershipPlanModel == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}}})
			return
		}
		list, err := membershipPlanModel.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取会员计划失败: " + err.Error()})
			return
		}
		result := make([]gin.H, 0, len(list))
		for _, item := range list {
			result = append(result, buildMembershipPlanPayload(item))
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": result}})
	})

	membership.GET("/:id", func(c *gin.Context) {
		if membershipPlanModel == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "会员计划不存在"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
			return
		}
		item, err := membershipPlanModel.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "会员计划不存在"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": buildMembershipPlanPayload(item)})
	})

	membership.POST("", func(c *gin.Context) {
		if membershipPlanModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "会员计划服务不可用"})
			return
		}
		var req struct {
			ID                      int64  `json:"id"`
			PlanCode                string `json:"plan_code" binding:"required,max=64"`
			Title                   string `json:"title" binding:"required,max=128"`
			Description             string `json:"description"`
			BadgeText               string `json:"badge_text"`
			RechargeAmountFen       int64  `json:"recharge_amount_fen" binding:"required,min=1"`
			DurationDays            int    `json:"duration_days" binding:"required,min=1"`
			TemplateDownloadEnabled bool   `json:"template_download_enabled"`
			IsEnabled               bool   `json:"is_enabled"`
			SortOrder               int    `json:"sort_order"`
			DownloadValidityDays    int    `json:"download_validity_days"`
			MaxTotalDownloads       int    `json:"max_total_downloads"`
			DailyDownloadLimit      int    `json:"daily_download_limit"`
			RateLimitPerMinute      int    `json:"rate_limit_per_minute"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		planCode := strings.TrimSpace(req.PlanCode)
		title := strings.TrimSpace(req.Title)
		if planCode == "" || title == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "计划编码和名称不能为空"})
			return
		}
		item := &model.MembershipPlan{
			ID:                      req.ID,
			PlanCode:                planCode,
			Title:                   title,
			Description:             strings.TrimSpace(req.Description),
			BadgeText:               strings.TrimSpace(req.BadgeText),
			RechargeAmountFen:       req.RechargeAmountFen,
			DurationDays:            req.DurationDays,
			TemplateDownloadEnabled: req.TemplateDownloadEnabled,
			IsEnabled:               req.IsEnabled,
			SortOrder:               req.SortOrder,
			DownloadValidityDays:    req.DownloadValidityDays,
			MaxTotalDownloads:       req.MaxTotalDownloads,
			DailyDownloadLimit:      req.DailyDownloadLimit,
			RateLimitPerMinute:      req.RateLimitPerMinute,
		}
		if err := membershipPlanModel.CreateOrUpdate(item); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存会员计划失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "保存成功", "data": buildMembershipPlanPayload(item)})
	})

	membership.DELETE("/:id", func(c *gin.Context) {
		if membershipPlanModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "会员计划服务不可用"})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
			return
		}
		plan, err := membershipPlanModel.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "会员计划不存在"})
			return
		}
		// 引用检查：已有用户持有（或曾持有）该计划时物理删除会产生悬空引用，
		// 影响会员状态计算与对账；引导改为停用（is_enabled = false）
		if userMembershipModel != nil {
			count, err := userMembershipModel.CountByPlan(plan.ID, plan.PlanCode)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "检查会员计划引用失败: " + err.Error()})
				return
			}
			if count > 0 {
				c.JSON(http.StatusConflict, gin.H{
					"code": 409,
					"msg":  fmt.Sprintf("已有 %d 位用户持有过该计划，不能删除；请改为停用（编辑并关闭「启用」）", count),
				})
				return
			}
		}
		if err := membershipPlanModel.Delete(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除会员计划失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
	})
}
