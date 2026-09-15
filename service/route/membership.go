package route

import (
	"database/sql"
	"net/http"
	"service/model"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func formatMembershipTimeText(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04")
}

func buildMembershipBenefitText(plan *model.MembershipPlan) string {
	if plan == nil || plan.DurationDays <= 0 {
		return ""
	}
	title := plan.Title
	if title == "" {
		title = "模板下载会员"
	}
	return "赠" + strconv.Itoa(plan.DurationDays) + "天" + title
}

func buildMembershipPlanPayload(plan *model.MembershipPlan) gin.H {
	benefitText := buildMembershipBenefitText(plan)
	return gin.H{
		"id":                        plan.ID,
		"plan_code":                 plan.PlanCode,
		"title":                     plan.Title,
		"description":               plan.Description,
		"badge_text":                plan.BadgeText,
		"recharge_amount_fen":       plan.RechargeAmountFen,
		"duration_days":             plan.DurationDays,
		"template_download_enabled": plan.TemplateDownloadEnabled,
		"is_enabled":                plan.IsEnabled,
		"sort_order":                plan.SortOrder,
		"download_validity_days":    plan.DownloadValidityDays,
		"max_total_downloads":       plan.MaxTotalDownloads,
		"daily_download_limit":      plan.DailyDownloadLimit,
		"rate_limit_per_minute":     plan.RateLimitPerMinute,
		"benefit_text":              benefitText,
		"created_at":                plan.CreatedAt,
		"updated_at":                plan.UpdatedAt,
	}
}

func resolveUserMembershipAccess(userMembershipModel *model.UserMembershipModel, userOrderModel *model.UserOrderModel, userID int64) (activeMembership *model.UserMembership, legacyRecharge bool, hasEntitlement bool, err error) {
	if userID <= 0 {
		return nil, false, false, nil
	}
	if userMembershipModel != nil {
		activeMembership, err = userMembershipModel.GetActiveByUserID(userID)
		if err != nil && err != sql.ErrNoRows {
			return nil, false, false, err
		}
		if err == nil && activeMembership != nil {
			return activeMembership, false, activeMembership.TemplateDownloadEnabled, nil
		}
	}
	if userOrderModel != nil {
		legacyRecharge, err = userOrderModel.HasSuccessfulRecharge(userID)
		if err != nil {
			return nil, false, false, err
		}
	}
	return nil, legacyRecharge, legacyRecharge, nil
}

func buildUserMembershipPayload(activeMembership *model.UserMembership, legacyRecharge bool) gin.H {
	payload := gin.H{
		"has_membership":            activeMembership != nil,
		"legacy_recharge_member":    legacyRecharge,
		"download_member_label":     "下载会员",
		"template_download_enabled": legacyRecharge,
		"lifetime_membership":       false,
		"status":                    "inactive",
		"plan_code":                 "",
		"plan_title":                "",
		"source_order_no":           "",
		"started_at":                nil,
		"granted_at":                nil,
		"expired_at":                nil,
		"started_at_text":           "",
		"granted_at_text":           "",
		"expired_at_text":           "",
		"remaining_days":            0,
	}
	if activeMembership == nil {
		if legacyRecharge {
			payload["status"] = "legacy_active"
		}
		return payload
	}
	isLifetime := model.IsLifetimeMembership(activeMembership)
	remaining := int(time.Until(activeMembership.ExpiredAt).Hours() / 24)
	if !isLifetime && time.Until(activeMembership.ExpiredAt) > 0 {
		remaining += 1
		if remaining < 1 {
			remaining = 1
		}
	}
	payload["has_membership"] = true
	payload["legacy_recharge_member"] = legacyRecharge
	payload["download_member_label"] = activeMembership.PlanTitle
	payload["template_download_enabled"] = activeMembership.TemplateDownloadEnabled
	payload["lifetime_membership"] = isLifetime
	payload["status"] = activeMembership.Status
	payload["plan_code"] = activeMembership.PlanCode
	payload["plan_title"] = activeMembership.PlanTitle
	payload["source_order_no"] = activeMembership.SourceOrderNo
	payload["started_at"] = activeMembership.StartedAt
	payload["granted_at"] = activeMembership.GrantedAt
	payload["expired_at"] = activeMembership.ExpiredAt
	payload["started_at_text"] = formatMembershipTimeText(activeMembership.StartedAt)
	payload["granted_at_text"] = formatMembershipTimeText(activeMembership.GrantedAt)
	if isLifetime {
		payload["expired_at_text"] = "长期有效"
		payload["remaining_days"] = 0
		return payload
	}
	payload["expired_at_text"] = formatMembershipTimeText(activeMembership.ExpiredAt)
	payload["remaining_days"] = remaining
	return payload
}

func RegisterMembershipRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, membershipPlanModel *model.MembershipPlanModel, userMembershipModel *model.UserMembershipModel, userOrderModel *model.UserOrderModel) {
	r.GET("/membership/plans", func(c *gin.Context) {
		if membershipPlanModel == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": []gin.H{}}})
			return
		}
		plans, err := membershipPlanModel.ListEnabled()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取会员计划失败: " + err.Error()})
			return
		}
		list := make([]gin.H, 0, len(plans))
		for _, plan := range plans {
			list = append(list, buildMembershipPlanPayload(plan))
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list}})
	})

	user := r.Group("/user")
	user.Use(TokenAuthRequired(codeSessionModel))
	user.GET("/membership", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		activeMembership, legacyRecharge, _, err := resolveUserMembershipAccess(userMembershipModel, userOrderModel, codeSession.UserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取会员状态失败: " + err.Error()})
			return
		}
		if activeMembership == nil && userMembershipModel != nil {
			if currentMembership, currentErr := userMembershipModel.GetByUserID(codeSession.UserID); currentErr == nil && currentMembership != nil {
				if currentMembership.Status == "active" && !currentMembership.ExpiredAt.After(time.Now()) {
					_ = userMembershipModel.UpdateStatusByUserID(codeSession.UserID, "expired")
					currentMembership.Status = "expired"
				}
				if currentMembership.Status != "active" {
					activeMembership = currentMembership
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": buildUserMembershipPayload(activeMembership, legacyRecharge)})
	})
}
