package route

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"

	"service/component/wechatpay"
	"service/config"
	"service/function"
	"service/model"
)

const (
	standardRechargeOrderCategory = "recharge"
	enterpriseRechargeOrderCategory = "enterprise_recharge"
	enterpriseRechargeMinAmountFen = 50000
)

// RegisterWechatPayRoutes 注册微信支付相关路由
// 需在 miniprogram 组下注册 POST /wechatpay/jsapi/prepay（需 token）、POST /wechatpay/withdraw（需 token，已认证用户提现）
// 在 v1 组下注册 POST /wechatpay/notify（不需 token，验签解密后更新订单并加灵石/认证工单状态）
func RegisterWechatPayRoutes(
	miniprogramAuth *gin.RouterGroup,
	v1 *gin.RouterGroup,
	cfg *config.Config,
	userOrderModel *model.UserOrderModel,
	stoneRecordModel *model.StoneRecordModel,
	userModel *model.UserRedisModel,
	userDBModel *model.UserModel,
	certificationModel *model.CertificationApplicationModel,
	membershipPlanModel *model.MembershipPlanModel,
	userMembershipModel *model.UserMembershipModel,
) {
	wpCfg := wechatpay.Config{
		MchID:     cfg.WechatPay.MchID,
		APIv3Key:  cfg.WechatPay.APIv3Key,
		CertDir:   cfg.WechatPay.CertDir,
		NotifyURL: cfg.WechatPay.NotifyURL,
		Enabled:   cfg.WechatPay.Enabled,
	}

	// 小程序内发起 JSAPI 预支付（需登录）
	miniprogramAuth.POST("/wechatpay/jsapi/prepay", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		// 阶段 4.1：虚拟支付启用后，旧 JSAPI 预下单接口降级。
		// 不删除旧代码，便于回滚到旧通路（cfg.VirtualPay.Enabled=false 即可恢复）。
		if cfg.VirtualPay.Enabled {
			c.JSON(http.StatusGone, gin.H{"code": 410, "msg": "支付通道已升级，请将小程序更新到最新版本"})
			return
		}
		if !cfg.WechatPay.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "微信支付未开启或配置不完整。请任选其一：① 在服务器设置环境变量 WECHAT_PAY_MCH_ID、WECHAT_PAY_NOTIFY_URL；② 或在 certificate 目录下添加 wechatpay_config.json，内容 {\"mch_id\":\"商户号\",\"notify_url\":\"https://域名/api/v1/wechatpay/notify\"}。证书目录与 APIv3 密钥（APIv3密钥.md）也需就绪。"})
			return
		}
		var req struct {
			Code        string `json:"code" binding:"required"`         // wx.login 得到的 code，用于换取 openid
			AmountFen   int64  `json:"amount_fen" binding:"required,min=1"` // 支付金额，单位分
			Stones      int64  `json:"stones"`                               // 到账灵石数量（以后端档位为准）
			PlanType    string `json:"plan_type"`
			Description string `json:"description"`                          // 商品描述，可选
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		actualStones := int64(0)
		plan, ok := matchRechargePlanByAmountFen(req.AmountFen)
		if ok {
			actualStones = plan.Stones
		} else {
			if req.AmountFen%100 != 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "自定义充值金额需为整数元"})
				return
			}
			actualStones = req.AmountFen / 10
		}
		orderCategory := standardRechargeOrderCategory
		if req.PlanType == enterpriseRechargeOrderCategory {
			orderCategory = enterpriseRechargeOrderCategory
			if req.AmountFen <= enterpriseRechargeMinAmountFen {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "企业级客户充值金额需大于500元"})
				return
			}
		}
		session, err := function.Code2Session(cfg.Wechat.AppID, cfg.Wechat.AppSecret, req.Code)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "获取openid失败: " + err.Error()})
			return
		}
		if session == nil || session.OpenID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "openid为空"})
			return
		}
		orderNo := model.GenerateOrderNo("WX")
		title := "充值"
		desc := req.Description
		if desc == "" {
			amountText := strconv.FormatInt(req.AmountFen/100, 10)
			if ok {
				amountText = strconv.FormatInt(plan.Amount, 10)
			}
			desc = amountText + "元兑换" + strconv.FormatInt(actualStones, 10) + "灵石"
		}
		if userOrderModel != nil {
			if err := userOrderModel.CreateDetailed(&model.UserOrder{
				UserID:        codeSession.UserID,
				DesignerUserID: 0,
				TemplateID:    nil,
				OrderNo:       orderNo,
				Type:          "recharge",
				OrderCategory: orderCategory,
				Amount:        actualStones,
				Status:        "pending",
				ReviewStatus:  "not_applicable",
				Title:         title,
				Description:   desc,
			}); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建订单失败"})
				return
			}
		}
		params, err := wechatpay.JSAPIPrepay(c.Request.Context(), cfg.Wechat.AppID, wpCfg, session.OpenID, req.AmountFen, desc, orderNo)
		if err != nil {
			if userOrderModel != nil {
				_ = userOrderModel.UpdateStatus(orderNo, "failed")
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "调起支付失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "ok",
			"data": gin.H{
				"order_no": orderNo,
				"payment":  params,
			},
		})
	})

	// 发起提现（商家转账用户确认模式）：仅已认证设计师/企业可提现，扣灵石并调微信转账接口
	miniprogramAuth.POST("/wechatpay/withdraw", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		// 阶段 4.3（D3 决策）：虚拟支付启用后，提现功能一期暂停。
		// 等阶段 0.6 资金路径调研完成、二级商户号→对公账户提现链路理顺后再恢复。
		if cfg.VirtualPay.Enabled {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "提现功能维护中，恢复时间将另行通知"})
			return
		}
		if !cfg.WechatPay.Enabled || !cfg.WechatPay.WithdrawEnabled {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "提现功能未开放或微信支付未配置"})
			return
		}
		user, err := userDBModel.GetByID(codeSession.UserID)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "用户不存在"})
			return
		}
		if certificationModel != nil {
			if currentCanWithdraw, syncErr := certificationModel.SyncUserCanWithdraw(userDBModel, codeSession.UserID); syncErr == nil {
				user.CanWithdraw = currentCanWithdraw
			} else {
				log.Printf("[WechatPay] sync can_withdraw before withdraw failed: userID=%d err=%v", codeSession.UserID, syncErr)
			}
		}
		if !user.CanWithdraw {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "仅已认证的设计师或企业可提现"})
			return
		}
		if user.OpenID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无法获取收款身份，请使用小程序登录"})
			return
		}
		var req struct {
			AmountFen int64 `json:"amount_fen" binding:"required,min=1"` // 提现金额，单位分
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		if req.AmountFen < cfg.WechatPay.WithdrawMinFen {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "提现金额低于最低限制"})
			return
		}
		ratio := cfg.WechatPay.WithdrawStoneToFen
		if ratio <= 0 {
			ratio = 1
		}
		stonesToDeduct := req.AmountFen / ratio
		if stonesToDeduct <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "提现金额换算灵石无效"})
			return
		}
		if userModel != nil {
			if err := userModel.DeductStones(codeSession.UserID, stonesToDeduct); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
				return
			}
		}
		orderNo := model.GenerateOrderNo("WD")
		if stoneRecordModel != nil {
			_ = stoneRecordModel.Create(codeSession.UserID, "withdraw", -stonesToDeduct, "余额提现", orderNo)
		}
		title := "余额提现"
		desc := "提现" + strconv.FormatInt(req.AmountFen/100, 10) + "元"
		if userOrderModel != nil {
			if err := userOrderModel.Create(codeSession.UserID, orderNo, "withdraw", -stonesToDeduct, "pending", title, desc); err != nil {
				if userModel != nil {
					_ = userModel.AddStones(codeSession.UserID, stonesToDeduct)
				}
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建提现订单失败"})
				return
			}
		}
		notifyURL := cfg.WechatPay.TransferNotifyURL
		if notifyURL == "" {
			notifyURL = cfg.WechatPay.NotifyURL
		}
		a1, a2, b1, b2 := "活动名称", "余额提现", "奖励说明", "设计师/企业认证用户提现"
		reportInfos := []wechatpay.TransferSceneReportInfo{
			{InfoType: &a1, InfoContent: &a2},
			{InfoType: &b1, InfoContent: &b2},
		}
		resp, err := wechatpay.CreateTransferBill(c.Request.Context(), cfg.Wechat.AppID, orderNo, cfg.WechatPay.TransferSceneId, user.OpenID, req.AmountFen, desc, notifyURL, reportInfos)
		if err != nil {
			if userModel != nil {
				_ = userModel.AddStones(codeSession.UserID, stonesToDeduct)
			}
			if userOrderModel != nil {
				_ = userOrderModel.UpdateStatus(orderNo, "failed")
			}
			log.Printf("[WechatPay] withdraw CreateTransferBill err: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "发起转账失败: " + err.Error()})
			return
		}
		if userOrderModel != nil && resp.State != "WAIT_USER_CONFIRM" && resp.State != "ACCEPTED" && resp.State != "PROCESSING" && resp.State != "TRANSFERING" && resp.State != "SUCCESS" {
			_ = userOrderModel.UpdateStatus(orderNo, "failed")
			if userModel != nil {
				_ = userModel.AddStones(codeSession.UserID, stonesToDeduct)
			}
		}
		data := gin.H{
			"order_no":         orderNo,
			"transfer_bill_no": resp.TransferBillNo,
			"state":            resp.State,
		}
		if resp.PackageInfo != nil && *resp.PackageInfo != "" && resp.State == "WAIT_USER_CONFIRM" {
			data["package_info"] = *resp.PackageInfo
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": data})
	})

	// 支付结果回调（微信服务器调用，无需 token）
	v1.POST("/wechatpay/notify", func(c *gin.Context) {
		if !cfg.WechatPay.Enabled {
			c.JSON(http.StatusOK, gin.H{"code": "FAIL", "message": "微信支付未开启"})
			return
		}
		certVisitor := downloader.MgrInstance().GetCertificateVisitor(cfg.WechatPay.MchID)
		if certVisitor == nil {
			log.Println("[WechatPay] notify: certificate visitor nil")
			c.JSON(http.StatusOK, gin.H{"code": "FAIL", "message": "证书未就绪"})
			return
		}
		handler := notify.NewNotifyHandler(cfg.WechatPay.APIv3Key, verifiers.NewSHA256WithRSAVerifier(certVisitor))
		transaction := new(payments.Transaction)
		_, err := handler.ParseNotifyRequest(context.Background(), c.Request, transaction)
		if err != nil {
			log.Printf("[WechatPay] notify parse err: %v", err)
			c.JSON(http.StatusOK, gin.H{"code": "FAIL", "message": err.Error()})
			return
		}
		if transaction.OutTradeNo == nil || *transaction.OutTradeNo == "" {
			c.JSON(http.StatusOK, gin.H{"code": "FAIL", "message": "无商户订单号"})
			return
		}
		orderNo := *transaction.OutTradeNo
		order, err := userOrderModel.GetByOrderNo(orderNo)
		if err != nil || order == nil {
			log.Printf("[WechatPay] order not found: %s", orderNo)
			c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "ok"})
			return
		}
		if order.Status == "success" {
			c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "ok"})
			return
		}
		tradeState := ""
		if transaction.TradeState != nil {
			tradeState = *transaction.TradeState
		}
		if tradeState != "SUCCESS" {
			_ = userOrderModel.UpdateStatus(orderNo, "failed")
			c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "ok"})
			return
		}
		if err := userOrderModel.UpdateStatus(orderNo, "success"); err != nil {
			log.Printf("[WechatPay] update order status err: %v", err)
		}
		if order.Type == "certification" && certificationModel != nil && order.Description != "" {
			const prefix = "certification:"
			if len(order.Description) > len(prefix) && order.Description[:len(prefix)] == prefix {
				if appID, err := strconv.ParseInt(order.Description[len(prefix):], 10, 64); err == nil && appID > 0 {
					if promoteErr := certificationModel.PromoteToPendingReviewIfConsistent(appID); promoteErr != nil {
						log.Printf("[WechatPay] certification promote to pending_review blocked: appID=%d err=%v", appID, promoteErr)
					}
				}
			}
		}
		if order.Type == "recharge" && order.OrderCategory == enterpriseRechargeOrderCategory {
			log.Printf("[WechatPay] enterprise recharge paid, waiting manual stone grant: orderNo=%s userID=%d", orderNo, order.UserID)
			c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "ok"})
			return
		}
		if order.Type == "recharge" && userModel != nil && order.Amount > 0 {
			if err := userModel.AddStones(order.UserID, order.Amount); err != nil {
				log.Printf("[WechatPay] add stones err: %v", err)
			} else if stoneRecordModel != nil {
				_ = stoneRecordModel.Create(order.UserID, "recharge", order.Amount, "微信支付充值", orderNo)
			}
			amountFen := int64(0)
			if transaction.Amount != nil && transaction.Amount.Total != nil {
				amountFen = *transaction.Amount.Total
			}
			if userMembershipModel != nil {
				now := time.Now()
				if amountFen > 0 && membershipPlanModel != nil {
					membershipPlan, membershipErr := membershipPlanModel.FindMatchedByRechargeAmountFen(amountFen)
					if membershipErr == nil && membershipPlan != nil && membershipPlan.DurationDays > 0 {
						if _, grantErr := userMembershipModel.GrantOrExtend(order.UserID, membershipPlan, orderNo, now); grantErr != nil {
							log.Printf("[WechatPay] grant membership err: %v", grantErr)
						}
					} else {
						if _, grantErr := userMembershipModel.GrantPermanentDownloadMembership(order.UserID, orderNo, now); grantErr != nil {
							log.Printf("[WechatPay] grant permanent membership err: %v", grantErr)
						}
					}
				} else {
					if _, grantErr := userMembershipModel.GrantPermanentDownloadMembership(order.UserID, orderNo, now); grantErr != nil {
						log.Printf("[WechatPay] grant permanent membership err: %v", grantErr)
					}
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "ok"})
	})
}
