package route

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"service/component/virtualpay"
	"service/config"
	"service/function"
	"service/model"
)

// virtualpaySessionKeyTTL session_key 在 Redis 中的 TTL。
//
// 微信文档默认 30 分钟，留 5 分钟余量给后端调用 /xpay/* 之间的网络往返。
const virtualpaySessionKeyTTL = 25 * time.Minute

// virtualpayCertProducts 道具白名单。新增/调整需同步 mp 后台「道具管理」。
var virtualpayCertProducts = map[string]struct{}{
	"cert_designer":   {},
	"cert_enterprise": {},
}

// prepareRequest 是 /miniprogram/virtualpay/prepare 接口的请求体类型。
//
// 抽成命名类型供 buildVirtualPayOrder 单测复用，避免内联匿名结构体两份声明漂移。
type prepareRequest struct {
	Code          string `json:"code" binding:"required"`
	PlanType      string `json:"plan_type" binding:"required"` // recharge / certification / report
	ReportID      string `json:"report_id"`
	AmountFen     int64  `json:"amount_fen"`
	ProductID     string `json:"product_id"`
	ApplicationID int64  `json:"application_id"` // certification 必填：认证工单 ID
	Platform      string `json:"platform"`       // ios/android/windows/ohos/mac/devtools
	Attach        string `json:"attach"`
}

// RegisterVirtualPayRoutes 注册虚拟支付相关路由（小程序端）。
//
// 当前版本：阶段 2.1 仅实现 /prepare（下单）；
//   - confirm 在阶段 2.2 增加
//   - 补偿任务在阶段 2.3 增加
//   - 推送统一入口在阶段 3 增加
func RegisterVirtualPayRoutes(
	miniprogramAuth *gin.RouterGroup,
	cfg *config.Config,
	userOrderModel *model.UserOrderModel,
	deps VirtualPayDeps,
) {
	sessionKeyModel := deps.SessionKeyModel
	miniprogramAuth.POST("/virtualpay/prepare", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		if !cfg.VirtualPay.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "虚拟支付未开启或配置不完整"})
			return
		}

		var req prepareRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}

		// 1. 普通订单沿用原有计价逻辑；报告订单必须等 code2session
		// 身份核验后，再由 ReportService 冻结价格、商品和素材包。
		var input *model.VirtualPayOrderInput
		var err error
		if req.PlanType != reportPlanType {
			// 认证道具价格优先取后台定价配置（30 秒缓存），未配置回退环境变量
			certOverrides, priceErr := resolveCertPriceOverrides(deps.PricingConfigModel)
			if priceErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取定价配置失败: " + priceErr.Error()})
				return
			}
			input, err = buildVirtualPayOrder(cfg, codeSession.UserID, &req, certOverrides)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
				return
			}
		}

		// 2. 取 openid + session_key，并写入 Redis 以便后续接口签名复用
		session, err := function.Code2Session(cfg.Wechat.AppID, cfg.Wechat.AppSecret, req.Code)
		if err != nil || session == nil || session.OpenID == "" || session.SessionKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "获取 openid/session_key 失败"})
			return
		}
		if err := sessionKeyModel.SetSessionKey(session.OpenID, session.SessionKey, virtualpaySessionKeyTTL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "缓存 session_key 失败: " + err.Error()})
			return
		}

		// 3. 落本地订单（pending）。报告重复 prepare 复用已冻结的订单号，
		// 不采信客户端传入的 amount_fen/product_id/attach。
		if req.PlanType == reportPlanType {
			if deps.ReportService == nil || deps.UserDBModel == nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "报告支付暂不可用"})
				return
			}
			user, userErr := deps.UserDBModel.GetByID(codeSession.UserID)
			if userErr != nil || validateReportWechatIdentity(user, session) != nil {
				c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "当前微信身份无法购买该报告"})
				return
			}
			reservation, reserveErr := deps.ReportService.ReservePaymentOrder(
				c.Request.Context(), codeSession.UserID, req.ReportID, model.GenerateOrderNo("VP"),
			)
			if reserveErr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "报告尚不可支付"})
				return
			}
			input, err = buildReportVirtualPayOrder(codeSession.UserID, session.OpenID, req.ReportID, reservation)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "报告支付参数无效"})
				return
			}
			if err := ensureReportVirtualPayOrder(userOrderModel, input, req.ReportID, reservation); err != nil {
				c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "报告支付订单冲突，请联系客服核对"})
				return
			}
		} else {
			input.OpenID = session.OpenID
			input.OrderNo = model.GenerateOrderNo("VP")
			if err := userOrderModel.CreateVirtualPayOrder(input); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建订单失败: " + err.Error()})
				return
			}
		}
		// 4. 组装 signData / 计算 paySig + signature
		env := pickXPayEnv(cfg, req.Platform)
		signData, err := buildVirtualPaySignData(cfg, env, input, input.XPayAttach)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "组装 signData 失败: " + err.Error()})
			return
		}
		appKey := cfg.VirtualPay.AppKeyProd
		if env == 1 {
			appKey = cfg.VirtualPay.AppKeySandbox
		}
		paySig := virtualpay.CalcPaySig("requestVirtualPayment", signData, appKey)
		signature := virtualpay.CalcSignature(signData, session.SessionKey)

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "ok",
			"data": gin.H{
				"order_no":  input.OrderNo,
				"mode":      input.PayMode,
				"signData":  signData, // 必须原样透传给 wx.requestVirtualPayment
				"paySig":    paySig,
				"signature": signature,
			},
		})
	})

	miniprogramAuth.POST("/virtualpay/confirm", func(c *gin.Context) {
		codeSession := GetTokenCodeSession(c)
		if codeSession == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
			return
		}
		if !cfg.VirtualPay.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "虚拟支付未开启"})
			return
		}
		var req struct {
			OrderNo  string `json:"order_no" binding:"required"`
			Platform string `json:"platform"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}

		state, err := confirmVirtualPayOrder(c.Request.Context(), cfg, deps, codeSession.UserID, req.OrderNo, req.Platform)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{"state": state}})
	})
}

// confirmVirtualPayOrder confirm 接口的核心逻辑（含用户身份校验）。
//
// 抽成独立函数便于阶段 2.3 补偿任务通过 advanceVirtualPayOrder 走同一份"查微信状态 + 触发发货"流程。
//
// 返回值：
//   - "PAID"：发货完成（含幂等场景）
//   - "PAYING"/"NOTPAY"：处理中，前端应轮询
//   - "CLOSED"/"REVOKED"：已失败，订单状态已置 failed
func confirmVirtualPayOrder(ctx context.Context, cfg *config.Config, deps VirtualPayDeps, userID int64, orderNo, platform string) (string, error) {
	meta, err := deps.UserOrderModel.GetVirtualPayOrderMeta(orderNo)
	if err != nil {
		return "", errBadParam("订单不存在")
	}
	if meta.UserID != userID {
		return "", errBadParam("订单不属于当前用户")
	}
	state, err := advanceVirtualPayOrder(ctx, cfg, deps, meta, platform)
	if err != nil {
		return state, err
	}
	// 顺路校准（T7）：支付确认成功后，用尚新鲜的 session_key 实测微信侧代币余额。
	// 放在 confirm 而非发货函数内：校准是尽力而为的旁路，不能影响发货结果。
	if state == string(virtualpay.OrderStatePaid) {
		calibrateWechatCoinAfterPaid(ctx, cfg, deps, meta, orderNo)
	}
	return state, nil
}

// advanceVirtualPayOrder 在已知订单元信息的前提下推进订单状态。
//
// 不做用户身份校验，专供：
//   - confirmVirtualPayOrder 在身份校验通过后调用
//   - 补偿任务直接调用（补偿在服务端发起，无 userID 概念）
func advanceVirtualPayOrder(ctx context.Context, cfg *config.Config, deps VirtualPayDeps, meta *model.VirtualPayOrderMeta, platform string) (string, error) {
	if meta.Status == "success" {
		// 幂等：已发货过，直接返回 PAID
		return string(virtualpay.OrderStatePaid), nil
	}
	if meta.Status == "delivering" {
		// 另一路调用方正在执行发货，让前端继续轮询而不是误判失败
		return string(virtualpay.OrderStatePaying), nil
	}
	if meta.Status != "pending" {
		return strings.ToUpper(meta.Status), errBadParam("订单已结束: " + meta.Status)
	}

	accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
	if err != nil {
		return "", fmt.Errorf("get access_token: %w", err)
	}

	resp, err := virtualpay.QueryOrder(ctx, accessToken, virtualpay.QueryOrderRequest{
		OpenID:     meta.OpenID,
		OutTradeNo: meta.OrderNo,
		Env:        pickXPayEnv(cfg, platform),
	})
	if err != nil {
		return "", fmt.Errorf("query_order: %w", err)
	}
	// 顺路存证（R2）：query_order 回包落 wechat_order_evidence 并比对实付 vs 应付；
	// confirm/补偿/管理端推进三路共用本函数，天然全覆盖。旁路失败不影响主链路。
	captureWechatOrderEvidence(deps, meta, meta.OrderNo, resp)

	switch resp.State {
	case virtualpay.OrderStatePaid:
		if applyErr := applyVirtualPayOrderSuccess(deps, meta.OrderNo); applyErr != nil {
			return string(resp.State), fmt.Errorf("apply order success: %w", applyErr)
		}
		if reportErr := reportVirtualPayGoodsDelivered(ctx, cfg, deps, meta, platform); reportErr != nil {
			// 发货回执失败不回滚本地发货；补偿任务会继续重试未上报订单。
			return string(resp.State), nil
		}
		return string(resp.State), nil
	case virtualpay.OrderStateClosed, virtualpay.OrderStateRevoked:
		_ = deps.UserOrderModel.UpdateStatus(meta.OrderNo, "failed")
		return string(resp.State), nil
	case virtualpay.OrderStatePaying, virtualpay.OrderStateNotPay:
		// 仍在支付中，前端轮询
		return string(resp.State), nil
	default:
		return string(resp.State), nil
	}
}

func reportVirtualPayGoodsDelivered(ctx context.Context, cfg *config.Config, deps VirtualPayDeps, meta *model.VirtualPayOrderMeta, platform string) error {
	if meta == nil || meta.PayMode != "short_series_goods" {
		return nil
	}
	if deps.UserOrderModel == nil {
		return errors.New("virtualpay: UserOrderModel 未注入")
	}
	if meta.Type == reportPlanType {
		if err := requireReportReady(deps, meta); err != nil {
			return err
		}
	}
	accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
	if err != nil {
		_ = deps.UserOrderModel.MarkDeliverReported(meta.OrderNo, false)
		return fmt.Errorf("get access_token: %w", err)
	}
	if err := virtualpay.NotifyProvideGoods(ctx, accessToken, virtualpay.NotifyProvideGoodsRequest{
		OpenID:     meta.OpenID,
		OutTradeNo: meta.OrderNo,
		Env:        pickXPayEnv(cfg, platform),
	}); err != nil {
		_ = deps.UserOrderModel.MarkDeliverReported(meta.OrderNo, false)
		return err
	}
	return deps.UserOrderModel.MarkDeliverReported(meta.OrderNo, true)
}

// buildVirtualPayOrder 按入参校验并构造一条待落库的虚拟支付订单输入。
//
// 抽出独立函数便于阶段 2.2/2.3 的单测复用。本函数不接触网络与数据库。
//
// certPriceOverrides 认证道具价格覆盖（后台定价配置，定价配置化 P1）：
// nil 或缺项时回退 cfg 环境变量价。充值档位灵石始终按 CoinRatio 折算——
// 微信代币模式实扣 = buyQuantity ÷ mp 后台比例，档位灵石不可偏离该公式（D1）。
func buildVirtualPayOrder(cfg *config.Config, userID int64, req *prepareRequest, certPriceOverrides map[string]int64) (*model.VirtualPayOrderInput, error) {
	switch req.PlanType {
	case "recharge":
		if req.AmountFen < 100 {
			return nil, errBadParam("充值金额最低 1 元")
		}
		if req.AmountFen%100 != 0 {
			return nil, errBadParam("充值金额需为整数元")
		}
		ratio := cfg.VirtualPay.CoinRatio
		if ratio <= 0 {
			ratio = 10
		}
		stones := req.AmountFen * ratio / 100 // 1 元 = ratio 灵石
		return &model.VirtualPayOrderInput{
			UserID:        userID,
			Type:          "recharge",
			OrderCategory: "recharge",
			Amount:        stones,
			AmountFen:     req.AmountFen,
			Title:         "充值",
			Description:   formatRechargeDesc(req.AmountFen, stones),
			PayMode:       "short_series_coin",
			XPayAttach:    req.Attach,
		}, nil
	case "certification":
		if _, ok := virtualpayCertProducts[req.ProductID]; !ok {
			return nil, errBadParam("无效的认证道具")
		}
		if req.ApplicationID <= 0 {
			return nil, errBadParam("缺少 application_id")
		}
		price := int64(0)
		if certPriceOverrides != nil {
			if p, ok := certPriceOverrides[req.ProductID]; ok {
				price = p
			}
		}
		if price <= 0 {
			price = certProductPrice(cfg, req.ProductID)
		}
		if price <= 0 {
			return nil, errBadParam("认证道具单价未配置，请联系运营")
		}
		title := certProductTitle(req.ProductID)
		// description 沿用旧 JSAPI 通路约定 "certification:<appID>"，
		// 让发货分发函数 applyVirtualPayOrderSuccess 能复用同一份 parse 逻辑。
		desc := "certification:" + strconv.FormatInt(req.ApplicationID, 10)
		return &model.VirtualPayOrderInput{
			UserID:        userID,
			Type:          "certification",
			OrderCategory: "certification",
			Amount:        0,
			AmountFen:     price,
			Title:         title,
			Description:   desc,
			PayMode:       "short_series_goods",
			ProductID:     req.ProductID,
			XPayAttach:    req.Attach,
		}, nil
	default:
		return nil, errBadParam("不支持的 plan_type")
	}
}

// buildVirtualPaySignData 组装 signData JSON 字符串。
//
// 字段顺序通过明确拼接保持稳定，确保 paySig/signature 在前后端可复现。
// 返回值是 JSON 字符串原文，前端必须原样透传给 wx.requestVirtualPayment。
func buildVirtualPaySignData(cfg *config.Config, env int, in *model.VirtualPayOrderInput, attach string) (string, error) {
	if cfg.VirtualPay.OfferID == "" {
		return "", errBadParam("OfferID 未配置")
	}
	switch in.PayMode {
	case "short_series_coin":
		// 代币模式：buyQuantity = 充值后到账的代币数（与本地灵石数一致）
		payload := struct {
			OfferID      string `json:"offerId"`
			BuyQuantity  int64  `json:"buyQuantity"`
			Env          int    `json:"env"`
			CurrencyType string `json:"currencyType"`
			OutTradeNo   string `json:"outTradeNo"`
			Attach       string `json:"attach,omitempty"`
		}{
			OfferID:      cfg.VirtualPay.OfferID,
			BuyQuantity:  in.Amount,
			Env:          env,
			CurrencyType: "CNY",
			OutTradeNo:   in.OrderNo,
			Attach:       attach,
		}
		b, err := json.Marshal(payload)
		return string(b), err
	case "short_series_goods":
		payload := struct {
			OfferID      string `json:"offerId"`
			BuyQuantity  int    `json:"buyQuantity"`
			Env          int    `json:"env"`
			CurrencyType string `json:"currencyType"`
			ProductID    string `json:"productId"`
			GoodsPrice   int64  `json:"goodsPrice"`
			OutTradeNo   string `json:"outTradeNo"`
			Attach       string `json:"attach,omitempty"`
		}{
			OfferID:      cfg.VirtualPay.OfferID,
			BuyQuantity:  1,
			Env:          env,
			CurrencyType: "CNY",
			ProductID:    in.ProductID,
			GoodsPrice:   in.AmountFen,
			OutTradeNo:   in.OrderNo,
			Attach:       attach,
		}
		b, err := json.Marshal(payload)
		return string(b), err
	default:
		return "", errBadParam("未知的 pay_mode")
	}
}

// pickXPayEnv 决定 env 取值。iOS 必须 0；其它平台按 SandboxMode。
//
// 该规则与 mp 后台「iOS 支付开关」无关，是文档明确的客户端拉起约束。
func pickXPayEnv(cfg *config.Config, platform string) int {
	if platform == "ios" {
		return 0
	}
	if cfg.VirtualPay.SandboxMode {
		return 1
	}
	return 0
}

func certProductPrice(cfg *config.Config, productID string) int64 {
	switch productID {
	case "cert_designer":
		return cfg.VirtualPay.CertDesignerFen
	case "cert_enterprise":
		return cfg.VirtualPay.CertEnterpriseFen
	}
	return 0
}

func certProductTitle(productID string) string {
	switch productID {
	case "cert_designer":
		return "设计师认证费"
	case "cert_enterprise":
		return "企业认证费"
	}
	return "认证费"
}

func formatRechargeDesc(amountFen, stones int64) string {
	return fmt.Sprintf("%d元兑换%d灵石", amountFen/100, stones)
}

// errBadParam 简短错误，配合 c.JSON 返回前端。
type virtualpayBadParamError struct{ msg string }

func (e *virtualpayBadParamError) Error() string { return e.msg }

func errBadParam(msg string) error { return &virtualpayBadParamError{msg: msg} }
