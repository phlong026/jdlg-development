package route

import (
	"net/http"
	"service/config"
	"service/model"
	"strconv"
	"github.com/gin-gonic/gin"
)

type RechargePlanItem struct {
	Type                  string `json:"type"`
	Title                 string `json:"title"`
	Amount                int64  `json:"amount"`
	AmountFen             int64  `json:"amount_fen"`
	AmountText            string `json:"amount_text"`
	Stones                int64  `json:"stones"`
	StonesText            string `json:"stones_text"`
	Recommended           bool   `json:"recommended,omitempty"`
	MembershipPlanCode    string `json:"membership_plan_code,omitempty"`
	MembershipTitle       string `json:"membership_title,omitempty"`
	MembershipBadgeText   string `json:"membership_badge_text,omitempty"`
	MembershipDescription string `json:"membership_description,omitempty"`
	MembershipDurationDays int   `json:"membership_duration_days,omitempty"`
	MembershipBenefitText string `json:"membership_benefit_text,omitempty"`
}

func getRechargePlanItems() []RechargePlanItem {
	return []RechargePlanItem{
		{Type: "basic", Title: "普通会员", Amount: 50, AmountFen: 5000, AmountText: "50", Stones: 500, StonesText: "500灵石"},
		{Type: "discount", Title: "白银会员", Amount: 300, AmountFen: 30000, AmountText: "300", Stones: 3000, StonesText: "3000灵石", Recommended: true},
		{Type: "super", Title: "黄金会员", Amount: 500, AmountFen: 50000, AmountText: "500", Stones: 5000, StonesText: "5000灵石"},
	}
}

func matchRechargePlanByAmountFen(amountFen int64) (RechargePlanItem, bool) {
	for _, plan := range getRechargePlanItems() {
		if plan.AmountFen == amountFen {
			return plan, true
		}
	}
	return RechargePlanItem{}, false
}

func resolveMiniprogramPaymentMode(cfg *config.Config, configuredMode string) string {
	mode := configuredMode
	if mode == "" {
		mode = "wechat_only"
	}
	if cfg != nil && cfg.WechatPay.Enabled {
		return "wechat_only"
	}
	return mode
}

func attachMembershipBenefits(plans []RechargePlanItem, membershipPlanModel *model.MembershipPlanModel) []RechargePlanItem {
	if len(plans) == 0 {
		return plans
	}
	for index := range plans {
		plans[index].MembershipTitle = model.DefaultRechargePermanentPlanTitle
		plans[index].MembershipBenefitText = "标准充值默认长期有效下载资格"
	}
	if membershipPlanModel == nil {
		return plans
	}
	membershipPlans, err := membershipPlanModel.ListEnabled()
	if err != nil || len(membershipPlans) == 0 {
		return plans
	}
	planMap := make(map[int64]*model.MembershipPlan, len(membershipPlans))
	for _, item := range membershipPlans {
		if item == nil || item.RechargeAmountFen <= 0 {
			continue
		}
		if _, exists := planMap[item.RechargeAmountFen]; !exists {
			planMap[item.RechargeAmountFen] = item
		}
	}
	for index := range plans {
		matched := planMap[plans[index].AmountFen]
		if matched == nil {
			continue
		}
		plans[index].MembershipPlanCode = matched.PlanCode
		plans[index].MembershipTitle = matched.Title
		plans[index].MembershipBadgeText = matched.BadgeText
		plans[index].MembershipDescription = matched.Description
		plans[index].MembershipDurationDays = matched.DurationDays
		if matched.DurationDays > 0 {
			title := matched.Title
			if title == "" {
				title = "模板下载会员"
			}
			plans[index].MembershipBenefitText = "赠" + strconv.Itoa(matched.DurationDays) + "天" + title
		}
	}
	return plans
}

// RegisterRechargeConfigRoutes 注册小程序充值配置路由（公开接口）
func RegisterRechargeConfigRoutes(r *gin.RouterGroup, rechargeConfigModel *model.RechargeConfigModel, membershipPlanModel *model.MembershipPlanModel, cfg *config.Config) {
	recharge := r.Group("/recharge")
	{
		// 获取当前启用的充值配置
		recharge.GET("/config", func(c *gin.Context) {
			config, err := rechargeConfigModel.GetEnabled()
			if err != nil {
				defaultPaymentMode := resolveMiniprogramPaymentMode(cfg, "wechat_only")
				plans := attachMembershipBenefits(getRechargePlanItems(), membershipPlanModel)
				c.JSON(http.StatusOK, gin.H{
					"code": 0,
					"msg":  "success",
					"data": gin.H{
						"payment_mode": defaultPaymentMode,
						"config_data":  nil,
						"plans":        plans,
					},
				})
				return
			}

			paymentMode := resolveMiniprogramPaymentMode(cfg, config.PaymentMode)
			plans := attachMembershipBenefits(getRechargePlanItems(), membershipPlanModel)

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"payment_mode": paymentMode,
					"config_data":  config.ConfigData,
					"plans":        plans,
				},
			})
		})
	}
}
