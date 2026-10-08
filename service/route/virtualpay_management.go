package route

import (
"log"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"service/component/virtualpay"
	"service/config"
	"service/function"
	"service/model"
)

type virtualPayHealthItem struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func buildVirtualPayHealth(cfg *config.Config) []virtualPayHealthItem {
	vp := cfg.VirtualPay
	return []virtualPayHealthItem{
		{Key: "enabled", Label: "虚拟支付总开关", OK: vp.Enabled, Message: boolMessage(vp.Enabled, "已启用", "未启用")},
		{Key: "offer_id", Label: "OfferID", OK: vp.OfferID != "", Message: boolMessage(vp.OfferID != "", "已配置", "未配置")},
		{Key: "app_key_prod", Label: "生产 AppKey", OK: vp.AppKeyProd != "", Message: boolMessage(vp.AppKeyProd != "", "已配置", "未配置")},
		{Key: "message_token", Label: "消息推送 Token", OK: vp.MessageToken != "", Message: boolMessage(vp.MessageToken != "", "已配置", "未配置")},
		{Key: "message_aes_key", Label: "消息推送 AESKey", OK: vp.MessageEncodingAESKey != "", Message: boolMessage(vp.MessageEncodingAESKey != "", "已配置", "未配置")},
		{Key: "coin_ratio", Label: "代币兑换比例", OK: vp.CoinRatio > 0, Message: strconv.FormatInt(vp.CoinRatio, 10)},
		{Key: "cert_designer", Label: "个人认证道具价格", OK: vp.CertDesignerFen > 0, Message: formatFenStatus(vp.CertDesignerFen)},
		{Key: "cert_enterprise", Label: "企业认证道具价格", OK: vp.CertEnterpriseFen > 0, Message: formatFenStatus(vp.CertEnterpriseFen)},
	}
}

func boolMessage(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func formatFenStatus(v int64) string {
	if v <= 0 {
		return "未配置"
	}
	return strconv.FormatInt(v, 10) + " 分"
}

func RegisterVirtualPayManagementRoutes(r *gin.RouterGroup, cfg *config.Config, userOrderModel *model.UserOrderModel, userModel *model.UserRedisModel, stoneRecordModel *model.StoneRecordModel, xpayLogModel *model.XPayMessageLogModel) {
	group := r.Group("/virtualpay")

	group.GET("/health", func(c *gin.Context) {
		items := buildVirtualPayHealth(cfg)
		ok := true
		for _, item := range items {
			if !item.OK {
				ok = false
				break
			}
		}

		var totalVirtualpayOrders int64
		var pendingOrders int64
		var refundedOrders int64
		var unreportedGoodsOrders int64
		if userOrderModel != nil && userOrderModel.DB != nil {
			_ = userOrderModel.DB.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE pay_channel = 'virtualpay'`).Scan(&totalVirtualpayOrders)
			_ = userOrderModel.DB.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE pay_channel = 'virtualpay' AND status = 'pending'`).Scan(&pendingOrders)
			_ = userOrderModel.DB.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE pay_channel = 'virtualpay' AND status = 'refunded'`).Scan(&refundedOrders)
			_ = userOrderModel.DB.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE pay_channel = 'virtualpay' AND status = 'success' AND pay_mode = 'short_series_goods' AND COALESCE(deliver_reported, 0) = 0`).Scan(&unreportedGoodsOrders)
		}

		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"ok":           ok,
			"items":        items,
			"sandbox_mode": cfg.VirtualPay.SandboxMode,
			"ios_enabled":  cfg.VirtualPay.IOSEnabled,
			"order_summary": gin.H{
				"total_virtualpay_orders": totalVirtualpayOrders,
				"pending_orders":          pendingOrders,
				"refunded_orders":         refundedOrders,
				"unreported_goods_orders": unreportedGoodsOrders,
			},
		}})
	})

	group.GET("/logs", func(c *gin.Context) {
		if xpayLogModel == nil {
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

		var processed *bool
		switch strings.TrimSpace(c.DefaultQuery("processed", "all")) {
		case "true", "1":
			v := true
			processed = &v
		case "false", "0":
			v := false
			processed = &v
		}

		list, total, err := xpayLogModel.List(
			strings.TrimSpace(c.Query("keyword")),
			strings.TrimSpace(c.DefaultQuery("event", "all")),
			processed,
			pageSize,
			(page-1)*pageSize,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取虚拟支付推送日志失败: " + err.Error()})
			return
		}
		summary, _ := xpayLogModel.Summary()
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"list":      list,
			"summary":   summary,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		}})
	})

	group.POST("/orders/:id/refund", func(c *gin.Context) {
		orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的订单ID"})
			return
		}
		var req struct {
			Reason   string `json:"reason"`
			Platform string `json:"platform"`
		}
		_ = c.ShouldBindJSON(&req)

		result, err := refundVirtualPayRechargeOrder(c.Request.Context(), cfg, VirtualPayDeps{
			UserOrderModel:   userOrderModel,
			UserModel:        userModel,
			StoneRecordModel: stoneRecordModel,
		}, orderID, req.Reason, req.Platform)
		if err != nil {
			status := http.StatusInternalServerError
			if isVirtualPayRefundBadRequest(err) {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"code": status, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": result})
	})
}

type virtualPayRefundResult struct {
	OrderNo      string `json:"order_no"`
	OutRefundNo  string `json:"out_refund_no"`
	RefundAmount int64  `json:"refund_amount"`
	OldStones    int64  `json:"old_stones"`
	NewStones    int64  `json:"new_stones"`
	Status       string `json:"status"`
}

type virtualPayRefundBadRequestError struct {
	msg string
}

func (e *virtualPayRefundBadRequestError) Error() string {
	return e.msg
}

func errVirtualPayRefundBadRequest(msg string) error {
	return &virtualPayRefundBadRequestError{msg: msg}
}

func isVirtualPayRefundBadRequest(err error) bool {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, model.ErrInsufficientStones) {
		return true
	}
	var badRequestErr *virtualPayRefundBadRequestError
	return errors.As(err, &badRequestErr)
}

func validateVirtualPayRechargeRefund(order *model.UserOrder, currentStones int64) error {
	if order == nil {
		return sql.ErrNoRows
	}
	if order.PayChannel != "virtualpay" {
		return errVirtualPayRefundBadRequest("仅支持虚拟支付订单退款")
	}
	if order.Type != "recharge" {
		return errVirtualPayRefundBadRequest("仅支持充值订单退款")
	}
	if order.PayMode != "short_series_coin" {
		return errVirtualPayRefundBadRequest("仅支持代币模式充值退款")
	}
	if order.Status != "success" {
		return errVirtualPayRefundBadRequest(fmt.Sprintf("仅已成功到账订单可退款，当前状态=%s", order.Status))
	}
	if order.Amount <= 0 {
		return errVirtualPayRefundBadRequest("订单灵石数量异常")
	}
	if strings.TrimSpace(order.OpenID) == "" {
		return errVirtualPayRefundBadRequest("订单缺少 openid，无法发起微信虚拟支付退款")
	}
	if currentStones < order.Amount {
		return errVirtualPayRefundBadRequest(fmt.Sprintf("用户余额不足，当前余额 %d，退款需扣回 %d", currentStones, order.Amount))
	}
	return nil
}

func refundVirtualPayRechargeOrder(ctx context.Context, cfg *config.Config, deps VirtualPayDeps, orderID int64, reason, platform string) (*virtualPayRefundResult, error) {
	if deps.UserOrderModel == nil || deps.UserModel == nil {
		return nil, errors.New("虚拟支付退款服务未初始化")
	}
	order, err := deps.UserOrderModel.GetByID(orderID)
	if err != nil {
		return nil, err
	}
	oldStones, err := deps.UserModel.GetStones(order.UserID)
	if err != nil {
		return nil, fmt.Errorf("查询用户灵石余额失败: %w", err)
	}
	if err := validateVirtualPayRechargeRefund(order, oldStones); err != nil {
		return nil, err
	}

	claimed, err := claimVirtualPayRefunding(deps.UserOrderModel, order.ID)
	if err != nil {
		return nil, fmt.Errorf("锁定退款订单失败: %w", err)
	}
	if !claimed {
		return nil, errVirtualPayRefundBadRequest("订单状态已变化，请刷新后重试")
	}

	if err := deps.UserModel.DeductStones(order.UserID, order.Amount); err != nil {
		if rbErr := restoreVirtualPayRefunding(deps.UserOrderModel, order.ID); rbErr != nil {
			log.Printf("[VirtualPay][资金] 扣回灵石失败且回滚状态失败，订单卡在 refunding 待补偿核对: orderID=%d err=%v", order.ID, rbErr)
		}
		return nil, fmt.Errorf("扣回灵石失败: %w", err)
	}

	accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
	if err != nil {
		logRollbackRefundFailure(deps, order, "get_access_token", err)
		return nil, fmt.Errorf("获取微信 access_token 失败: %w", err)
	}

	outRefundNo := "RF" + order.OrderNo
	if strings.TrimSpace(reason) == "" {
		reason = "运营后台退款"
	}
	_, err = virtualpay.RefundOrder(ctx, accessToken, virtualpay.RefundOrderRequest{
		OpenID:       order.OpenID,
		OutTradeNo:   order.OrderNo,
		OutRefundNo:  outRefundNo,
		RefundAmount: order.Amount,
		Reason:       reason,
		Env:          pickXPayEnv(cfg, platform),
	})
	if err != nil {
		// 微信退款是"超时不代表失败"的场景：请求可能已被受理、退款推送稍后到达。
		// 若此时回滚（退还灵石+状态回 success），推送到达后用户将"真钱已退、灵石也已
		// 退回"双重得利，且 refund_deduct 流水缺失无法对账。因此保持 refunding 状态
		// 与已扣灵石不动，由补偿任务 10 分钟后按微信侧真实状态核对：
		// REFUND → 落终态 refunded（灵石已扣，账实相符）；其它状态 → [资金] 告警人工处理。
		log.Printf("[VirtualPay][资金] 微信退款请求失败，保持 refunding 待补偿核对（请勿手工改余额）: orderNo=%s err=%v", order.OrderNo, err)
		return nil, fmt.Errorf("微信虚拟支付退款请求失败（订单进入补偿核对，最终结果以微信侧为准）: %w", err)
	}

	if deps.StoneRecordModel != nil {
		_ = deps.StoneRecordModel.Create(order.UserID, "refund_deduct", order.Amount, "虚拟支付退款扣回", order.OrderNo)
	}
	return &virtualPayRefundResult{
		OrderNo:      order.OrderNo,
		OutRefundNo:  outRefundNo,
		RefundAmount: order.Amount,
		OldStones:    oldStones,
		NewStones:    oldStones - order.Amount,
		Status:       "refunding",
	}, nil
}


// logRollbackRefundFailure 退款链路失败后的回滚（退还灵石 + 状态回 success）。
// 回滚自身失败时订单会卡在 refunding 且用户灵石少记，必须留痕供补偿任务/人工核对，
// 不能再像以前那样用 _ = 吞掉。
func logRollbackRefundFailure(deps VirtualPayDeps, order *model.UserOrder, stage string, cause error) {
	failed := false
	if err := deps.UserModel.AddStones(order.UserID, order.Amount); err != nil {
		failed = true
		log.Printf("[VirtualPay][资金][严重] 退款失败(%s)且退还灵石失败，需人工核对: orderNo=%s user=%d amount=%d err=%v",
			stage, order.OrderNo, order.UserID, order.Amount, err)
	}
	if err := restoreVirtualPayRefunding(deps.UserOrderModel, order.ID); err != nil {
		failed = true
		log.Printf("[VirtualPay][资金][严重] 退款失败(%s)且回滚订单状态失败，订单卡在 refunding 待补偿核对: orderNo=%s err=%v",
			stage, order.OrderNo, err)
	}
	// 两步都成功才说"已回滚"，否则上面已有 [严重] 日志，这里再说"已回滚"会误导值班排障
	if !failed {
		log.Printf("[VirtualPay][资金] 退款失败(%s)已回滚（退还灵石+状态回 success）: orderNo=%s cause=%v", stage, order.OrderNo, cause)
	}
}

func claimVirtualPayRefunding(userOrderModel *model.UserOrderModel, orderID int64) (bool, error) {
	res, err := userOrderModel.DB.Exec(
		`UPDATE user_orders SET status = 'refunding' WHERE id = ? AND pay_channel = 'virtualpay' AND type = 'recharge' AND status = 'success'`,
		orderID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func restoreVirtualPayRefunding(userOrderModel *model.UserOrderModel, orderID int64) error {
	_, err := userOrderModel.DB.Exec(`UPDATE user_orders SET status = 'success' WHERE id = ? AND status = 'refunding'`, orderID)
	return err
}
