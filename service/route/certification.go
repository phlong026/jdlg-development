package route

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"service/component"
	"service/component/wechatpay"
	"service/config"
	"service/function"
	"service/model"
)

// 个人认证 2 元，企业认证 5 元（单位：分）
const (
	CertFeeDesignerFen   = 200
	CertFeeEnterpriseFen = 500
)

// RegisterCertificationRoutes 注册认证申请路由（小程序端，需 token）
// 用户提交认证申请：阿里云基础核验通过后创建工单（待支付），调起微信支付认证费，支付成功后工单进入管理员审核
func RegisterCertificationRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userDBModel *model.UserModel, certificationModel *model.CertificationApplicationModel, cfg *config.Config, userOrderModel *model.UserOrderModel) {
	r.POST("/certification/apply", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
			return
		}
		var req struct {
			Code            string `json:"code"` // wx.login 的 code，用于调起支付
			Type            string `json:"type" binding:"required,oneof=designer enterprise"`
			RealName        string `json:"real_name" binding:"required"`
			IDCardNo        string `json:"id_card_no" binding:"required"`
			IdentityType    string `json:"identity_type"`     // 认证身份：设计师/施工队/企业主等
			CompanyName     string `json:"company_name"`      // 企业必填
			CreditCode      string `json:"credit_code"`       // 企业必填
			LegalPerson     string `json:"legal_person"`      // 企业必填
			ExtraDocsRemark string `json:"extra_docs_remark"` // 其他证件说明（选填）
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		if req.Type == model.CertificationTypeEnterprise {
			if req.CompanyName == "" || req.CreditCode == "" || req.LegalPerson == "" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "企业认证需填写企业名称、统一社会信用代码、法人姓名"})
				return
			}
		}

		app := &model.CertificationApplication{
			UserID:          codeSession.UserID,
			Type:            req.Type,
			RealName:        req.RealName,
			IDCardNo:        req.IDCardNo,
			CompanyName:     req.CompanyName,
			CreditCode:      req.CreditCode,
			LegalPerson:     req.LegalPerson,
			ExtraDocsRemark: req.ExtraDocsRemark,
			IdentityType:    req.IdentityType,
			Status:          model.CertificationStatusPendingPayment,
		}

		if certificationModel != nil {
			if err := certificationModel.ValidateAccountConsistency(app, 0); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
				return
			}
		}

		var aliyunPassed bool
		var aliyunMsg string
		if req.Type == model.CertificationTypeDesigner {
			aliyunPassed, aliyunMsg = component.VerifyIDCardTwo(req.RealName, req.IDCardNo)
		} else {
			aliyunPassed, aliyunMsg = component.VerifyEnterpriseThree(req.CompanyName, req.CreditCode, req.LegalPerson)
		}
		if !aliyunPassed {
			msg := "基础核验未通过"
			if aliyunMsg != "" {
				msg += "：" + aliyunMsg
			}
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": msg})
			return
		}

		amountFen, title := getCertificationFeeInfo(req.Type)
		if userOrderModel == nil || (!cfg.VirtualPay.Enabled && !cfg.WechatPay.Enabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "认证支付暂未开放"})
			return
		}

		app.AliyunPassed = true
		app.AliyunMsg = aliyunMsg
		if err := certificationModel.Create(app); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "提交失败"})
			return
		}

		if cfg.VirtualPay.Enabled {
			productID := "cert_designer"
			if req.Type == model.CertificationTypeEnterprise {
				productID = "cert_enterprise"
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "请使用虚拟支付通道完成支付",
				"data": gin.H{
					"application_id": app.ID,
					"need_payment":   true,
					"amount_fen":     amountFen,
					"pay_channel":    "virtualpay",
					"cert_type":      req.Type,
					"product_id":     productID,
				},
			})
			return
		}

		orderNo := model.GenerateOrderNo("CF")
		if err := userOrderModel.Create(codeSession.UserID, orderNo, "certification", amountFen/100, "pending", title, "certification:"+strconv.FormatInt(app.ID, 10)); err != nil {
			_ = certificationModel.UpdateStatus(app.ID, model.CertificationStatusRejected)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建支付订单失败"})
			return
		}

		if req.Code == "" {
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "请使用小程序提交并完成支付",
				"data": gin.H{
					"application_id": app.ID,
					"order_no":       orderNo,
					"need_payment":   true,
					"amount_fen":     amountFen,
				},
			})
			return
		}
		params, err := buildCertificationPaymentParams(c, cfg, req.Code, orderNo, title, app.ID, amountFen)
		if err != nil {
			_ = userOrderModel.UpdateStatus(orderNo, "failed")
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "调起支付失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "请完成支付后等待管理员审核",
			"data": gin.H{
				"application_id": app.ID,
				"order_no":       orderNo,
				"amount_fen":     amountFen,
				"payment":        params,
				"need_payment":   true,
			},
		})
	})

	r.POST("/certification/continue-pay", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
			return
		}
		// 阶段 4.1：虚拟支付启用时，本接口不再调起旧 JSAPI 支付，
		// 但仍接受请求并查出 cert_type/application_id 返回前端，
		// 让前端拿到这些字段后调用 /miniprogram/virtualpay/prepare 重新发起支付。
		virtualpayMode := cfg.VirtualPay.Enabled
		if certificationModel == nil || userOrderModel == nil || (!virtualpayMode && !cfg.WechatPay.Enabled) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "认证支付暂未开放"})
			return
		}
		var req struct {
			Code          string `json:"code" binding:"required"`
			ApplicationID int64  `json:"application_id"`
			OrderID       int64  `json:"order_id"`
			OrderNo       string `json:"order_no"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		var app *model.CertificationApplication
		var order *model.UserOrder
		var err error

		switch {
		case req.OrderID > 0:
			order, err = userOrderModel.GetByID(req.OrderID)
		case req.OrderNo != "":
			order, err = userOrderModel.GetByOrderNo(req.OrderNo)
		case req.ApplicationID > 0:
			app, err = certificationModel.GetByID(req.ApplicationID)
		default:
			app, err = certificationModel.GetPendingByUser(codeSession.UserID)
		}
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "未找到待支付认证申请"})
			return
		}

		if order != nil {
			if order.UserID != codeSession.UserID || order.Type != "certification" || (order.Status != "pending" && order.Status != "failed") {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前认证订单不可继续支付"})
				return
			}
			appID := parseCertificationApplicationID(order.Description)
			if appID <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前认证订单缺少关联工单"})
				return
			}
			app, err = certificationModel.GetByID(appID)
			if err != nil || app == nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "认证申请不存在"})
				return
			}
		}

		if app == nil || app.UserID != codeSession.UserID || app.Status != model.CertificationStatusPendingPayment {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "认证申请状态不允许继续支付"})
			return
		}
		if order == nil {
			order, err = userOrderModel.GetPendingCertificationOrderByUserAndApplication(codeSession.UserID, app.ID)
			if err != nil || order == nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "获取支付订单失败"})
				return
			}
		}

		amountFen, title := getCertificationFeeInfo(app.Type)
		// 阶段 4.1：虚拟支付启用时，返回 pay_channel + cert_type + application_id，
		// 前端据此调用 /miniprogram/virtualpay/prepare 完成支付，不再走旧 JSAPI。
		if virtualpayMode {
			productID := "cert_designer"
			if app.Type == model.CertificationTypeEnterprise {
				productID = "cert_enterprise"
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "请通过虚拟支付完成支付",
				"data": gin.H{
					"application_id": app.ID,
					"order_id":       order.ID,
					"order_no":       order.OrderNo,
					"amount_fen":     amountFen,
					"need_payment":   true,
					"pay_channel":    "virtualpay",
					"cert_type":      app.Type,
					"product_id":     productID,
				},
			})
			return
		}
		params, err := buildCertificationPaymentParams(c, cfg, req.Code, order.OrderNo, title, app.ID, amountFen)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "调起支付失败: " + err.Error()})
			return
		}
		if order.Status != "pending" {
			_ = userOrderModel.UpdateStatus(order.OrderNo, "pending")
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "请完成支付后等待管理员审核",
			"data": gin.H{
				"application_id": app.ID,
				"order_id":       order.ID,
				"order_no":       order.OrderNo,
				"amount_fen":     amountFen,
				"payment":        params,
				"need_payment":   true,
			},
		})
	})

	// 查询当前用户认证申请状态（含工单号与阶段文案）
	r.GET("/certification/status", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
			return
		}
		if certificationModel == nil {
			c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"status": "", "can_withdraw": false, "application_id": 0, "stage": "", "stage_system": "", "stage_admin": ""}})
			return
		}
		canWithdraw := false
		if userDBModel != nil {
			canWithdraw, _ = certificationModel.SyncUserCanWithdraw(userDBModel, codeSession.UserID)
		}
		latest, _ := certificationModel.GetLatestByUser(codeSession.UserID)
		status := ""
		applicationID := int64(0)
		stage := ""
		stageSystem := ""
		stageAdmin := ""
		certType := ""
		identityType := ""
		realName := ""
		realNameMasked := ""
		idCardMasked := ""
		pendingOrderID := int64(0)
		pendingOrderNo := ""
		pendingOrderStatus := ""
		pendingAmountFen := int64(0)
		canContinuePay := false
		if latest != nil {
			applicationID = latest.ID
			status = latest.Status
			certType = latest.Type
			identityType = latest.IdentityType
			// 个人认证优先展示真实姓名，企业认证展示企业名称
			realName = latest.RealName
			if latest.Type == model.CertificationTypeEnterprise && latest.CompanyName != "" {
				realName = latest.CompanyName
			}
			if realName != "" {
				realNameMasked = maskRealName(realName)
			}
			if latest.IDCardNo != "" {
				idCardMasked = maskIDCard(latest.IDCardNo)
			}
			switch latest.Status {
			case model.CertificationStatusPendingPayment:
				stage = "待支付"
				stageSystem = "待支付"
				stageAdmin = ""
				if userOrderModel != nil {
					if pendingOrder, err := userOrderModel.GetPendingCertificationOrderByUserAndApplication(codeSession.UserID, latest.ID); err == nil && pendingOrder != nil {
						pendingOrderID = pendingOrder.ID
						pendingOrderNo = pendingOrder.OrderNo
						pendingOrderStatus = pendingOrder.Status
						pendingAmountFen = pendingOrder.Amount * 100
						canContinuePay = pendingOrder.Status == "pending" || pendingOrder.Status == "failed"
					}
				}
			case model.CertificationStatusPendingReview:
				stage = "管理员审核中"
				stageSystem = "系统核验已通过"
				stageAdmin = "管理员审核中"
			case model.CertificationStatusApproved:
				stage = "已通过"
				stageSystem = "系统核验已通过"
				stageAdmin = "已通过"
			case model.CertificationStatusRejected:
				stage = "已拒绝"
				stageSystem = "系统核验已通过"
				stageAdmin = "已拒绝"
			default:
				stage = status
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"status":               status,
				"can_withdraw":         canWithdraw,
				"application_id":       applicationID,
				"pending_order_id":     pendingOrderID,
				"pending_order_no":     pendingOrderNo,
				"pending_order_status": pendingOrderStatus,
				"pending_amount_fen":   pendingAmountFen,
				"can_continue_pay":     canContinuePay,
				"stage":                stage,
				"stage_system":         stageSystem,
				"stage_admin":          stageAdmin,
				"cert_type":            certType,
				"identity_type":        identityType,
				"real_name":            realName,
				"real_name_masked":     realNameMasked,
				"id_card_no_masked":    idCardMasked,
			},
		})
	})
}

func getCertificationFeeInfo(certType string) (int64, string) {
	if certType == model.CertificationTypeEnterprise {
		return CertFeeEnterpriseFen, "企业认证费"
	}
	return CertFeeDesignerFen, "个人认证费"
}

func buildCertificationPaymentParams(c *gin.Context, cfg *config.Config, code, orderNo, title string, applicationID, amountFen int64) (map[string]string, error) {
	session, err := function.Code2Session(cfg.Wechat.AppID, cfg.Wechat.AppSecret, code)
	if err != nil {
		return nil, err
	}
	if session == nil || session.OpenID == "" {
		return nil, fmt.Errorf("获取支付身份失败，请重试")
	}
	wpCfg := wechatpay.Config{
		MchID:     cfg.WechatPay.MchID,
		APIv3Key:  cfg.WechatPay.APIv3Key,
		CertDir:   cfg.WechatPay.CertDir,
		NotifyURL: cfg.WechatPay.NotifyURL,
		Enabled:   cfg.WechatPay.Enabled,
	}
	desc := title + strconv.FormatInt(applicationID, 10)
	return wechatpay.JSAPIPrepay(c.Request.Context(), cfg.Wechat.AppID, wpCfg, session.OpenID, amountFen, desc, orderNo)
}

func parseCertificationApplicationID(description string) int64 {
	const prefix = "certification:"
	if len(description) <= len(prefix) || description[:len(prefix)] != prefix {
		return 0
	}
	appID, err := strconv.ParseInt(description[len(prefix):], 10, 64)
	if err != nil {
		return 0
	}
	return appID
}

// maskRealName 对姓名进行中间脱敏，例如：张三丰 -> 张*丰，李四 -> 李*
func maskRealName(name string) string {
	rs := []rune(name)
	n := len(rs)
	if n <= 1 {
		return name
	}
	if n == 2 {
		return string(rs[0]) + "*"
	}
	masked := make([]rune, 0, n)
	masked = append(masked, rs[0])
	for i := 1; i < n-1; i++ {
		masked = append(masked, '*')
	}
	masked = append(masked, rs[n-1])
	return string(masked)
}

// maskIDCard 对身份证号进行中间脱敏，例如：110101199001011234 -> 1101****1234
func maskIDCard(id string) string {
	n := len(id)
	if n <= 8 {
		return id
	}
	prefix := id[:4]
	suffix := id[n-4:]
	return prefix + "****" + suffix
}
