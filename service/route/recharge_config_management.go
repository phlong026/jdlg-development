package route

import (
	"net/http"
	"strconv"
	"service/model"
	"github.com/gin-gonic/gin"
)

// RegisterRechargeConfigManagementRoutes 注册充值配置管理路由（管理后台）
func RegisterRechargeConfigManagementRoutes(r *gin.RouterGroup, rechargeConfigModel *model.RechargeConfigModel) {
	recharge := r.Group("/recharge-config")
	{
		// 获取所有充值配置列表
		recharge.GET("", func(c *gin.Context) {
			configs, err := rechargeConfigModel.List()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取充值配置列表失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list": configs,
				},
			})
		})

		// 获取单个充值配置
		recharge.GET("/:payment_mode", func(c *gin.Context) {
			paymentMode := c.Param("payment_mode")
			config, err := rechargeConfigModel.GetByPaymentMode(paymentMode)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "配置不存在",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": config,
			})
		})

		// 创建或更新充值配置
		recharge.POST("", func(c *gin.Context) {
			var req struct {
				PaymentMode string                      `json:"payment_mode" binding:"required,oneof=static_qrcode wechat_only alipay_only wechat_alipay"`
				ConfigData  *model.RechargeConfigData    `json:"config_data" binding:"required"`
				IsEnabled   bool                         `json:"is_enabled"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			// 如果要启用此配置，先禁用其他所有配置
			if req.IsEnabled {
				allConfigs, err := rechargeConfigModel.List()
				if err == nil {
					for _, cfg := range allConfigs {
						if cfg.PaymentMode != req.PaymentMode && cfg.IsEnabled {
							cfg.IsEnabled = false
							_ = rechargeConfigModel.CreateOrUpdate(cfg)
						}
					}
				}
			}

			config := &model.RechargeConfig{
				PaymentMode: req.PaymentMode,
				ConfigData:  req.ConfigData,
				IsEnabled:    req.IsEnabled,
			}

			if err := rechargeConfigModel.CreateOrUpdate(config); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "保存失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "保存成功",
				"data": config,
			})
		})

		// 删除充值配置
		recharge.DELETE("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的ID",
				})
				return
			}

			if err := rechargeConfigModel.Delete(id); err != nil {
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
