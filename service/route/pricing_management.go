package route

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"service/config"
	"service/model"
)

// 后台定价配置管理（定价配置化 P1）。
//
// 可调边界（安全约束，详见 model/pricing_config.go）：
//   - 充值档位金额列表可配，灵石数由系统按兑换比例折算（微信代币模式约束）
//   - 认证道具价格可配（道具模式纯本地定价）
//   - 兑换比例只读展示：必须与微信 mp 后台「代币兑换比例」一致，改它需两边同步，
//     后台不提供修改入口，避免标价与实付不符
//   - 保存经管理端审计中间件留痕，pricing_configs.updated_by 记录修改人

// RegisterPricingManagementRoutes 注册定价配置管理路由。
func RegisterPricingManagementRoutes(r *gin.RouterGroup, pricingModel *model.PricingConfigModel, cfg *config.Config) {
	group := r.Group("/pricing")

	// 读取生效配置：后台配置过返回 db 来源，否则返回默认值并标记 default
	group.GET("", func(c *gin.Context) {
		if pricingModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "定价配置服务不可用"})
			return
		}
		data, err := pricingModel.Get()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取定价配置失败: " + err.Error()})
			return
		}
		source := "default"
		if data == nil {
			data = defaultPricingData(cfg)
		} else {
			source = "db"
		}
		ratio := cfg.VirtualPay.CoinRatio
		if ratio <= 0 {
			ratio = 10
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"source":          source,
			"coin_ratio":      ratio,
			"recharge_plans":  data.RechargePlans,
			"cert_prices":     data.CertPrices,
			"cert_title_map":  map[string]string{"cert_designer": "设计师认证", "cert_enterprise": "企业认证"},
			"membership_note": "会员匹配按「会员计划」里的充值金额精确匹配；调整档位金额后请到「会员计划」同步调整对应金额档",
		}})
	})

	// 保存配置：服务端校验（白名单/整数元/数量上限），立即生效（30 秒缓存失效）
	group.PUT("", func(c *gin.Context) {
		if pricingModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "定价配置服务不可用"})
			return
		}
		var req model.PricingData
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		// 先显式校验给 400 语义；Save 内部会再校验一次兜底并发
		if err := model.ValidatePricingData(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		if err := pricingModel.Save(&req, GetUsername(c)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存定价配置失败: " + err.Error()})
			return
		}
		saved, err := pricingModel.Get()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已保存（回读失败，请刷新查看）"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"source":         "db",
			"recharge_plans": saved.RechargePlans,
			"cert_prices":    saved.CertPrices,
		}})
	})
}

// defaultPricingData 默认定价（与 getRechargePlanItems 硬编码档位、env 认证价一致），
// 供"未配置过"时的展示与前端初始值。
func defaultPricingData(cfg *config.Config) *model.PricingData {
	items := getRechargePlanItems()
	plans := make([]model.PricingRechargePlan, 0, len(items))
	for _, item := range items {
		plans = append(plans, model.PricingRechargePlan{
			Type:        item.Type,
			Title:       item.Title,
			AmountFen:   item.AmountFen,
			Recommended: item.Recommended,
		})
	}
	return &model.PricingData{
		RechargePlans: plans,
		CertPrices: []model.PricingCertPrice{
			{ProductID: "cert_designer", PriceFen: cfg.VirtualPay.CertDesignerFen},
			{ProductID: "cert_enterprise", PriceFen: cfg.VirtualPay.CertEnterpriseFen},
		},
	}
}

// resolveCertPriceOverrides 从定价配置取认证道具价格覆盖表；未配置返回 nil。
func resolveCertPriceOverrides(pricingModel *model.PricingConfigModel) (map[string]int64, error) {
	if pricingModel == nil {
		return nil, nil
	}
	data, err := pricingModel.Get()
	if err != nil || data == nil {
		return nil, err
	}
	overrides := make(map[string]int64, len(data.CertPrices))
	for _, cert := range data.CertPrices {
		if _, allowed := virtualpayCertProducts[cert.ProductID]; allowed && cert.PriceFen > 0 {
			overrides[cert.ProductID] = cert.PriceFen
		}
	}
	if len(overrides) == 0 {
		return nil, nil
	}
	return overrides, nil
}

// resolveRechargePlanItems 档位下发：优先后台配置（灵石按比例折算），未配置回退硬编码默认。
func resolveRechargePlanItems(pricingModel *model.PricingConfigModel, cfg *config.Config) []RechargePlanItem {
	ratio := cfg.VirtualPay.CoinRatio
	if ratio <= 0 {
		ratio = 10
	}
	if pricingModel != nil {
		if data, err := pricingModel.Get(); err == nil && data != nil && len(data.RechargePlans) > 0 {
			items := make([]RechargePlanItem, 0, len(data.RechargePlans))
			for i, plan := range data.RechargePlans {
				planType := plan.Type
				if planType == "" {
					planType = "plan_" + strconv.Itoa(i+1)
				}
				stones := plan.AmountFen * ratio / 100
				items = append(items, RechargePlanItem{
					Type:        planType,
					Title:       plan.Title,
					Amount:      plan.AmountFen / 100,
					AmountFen:   plan.AmountFen,
					AmountText:  strconv.FormatInt(plan.AmountFen/100, 10),
					Stones:      stones,
					StonesText:  strconv.FormatInt(stones, 10) + "灵石",
					Recommended: plan.Recommended,
				})
			}
			return items
		}
	}
	return getRechargePlanItems()
}
