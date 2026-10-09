package route

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
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

// RegisterVirtualPayManagementRoutes 注册虚拟支付管理端路由。
//
// 参数收拢为 VirtualPayDeps：核对退款/推进发货（T6）需要完整的 confirm 链路依赖，
// 与小程序端共享同一份注入，避免两处参数漂移。
func RegisterVirtualPayManagementRoutes(r *gin.RouterGroup, cfg *config.Config, deps VirtualPayDeps) {
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
		var deliveringOrders int64
		var refundingStuckOrders int64
		var refundedOrders int64
		var unreportedGoodsOrders int64
		if deps.UserOrderModel != nil && deps.UserOrderModel.DB != nil {
			_ = deps.UserOrderModel.DB.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE pay_channel = 'virtualpay'`).Scan(&totalVirtualpayOrders)
			_ = deps.UserOrderModel.DB.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE pay_channel = 'virtualpay' AND status = 'pending'`).Scan(&pendingOrders)
			_ = deps.UserOrderModel.DB.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE pay_channel = 'virtualpay' AND status = 'delivering'`).Scan(&deliveringOrders)
			_ = deps.UserOrderModel.DB.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE pay_channel = 'virtualpay' AND status = 'refunding' AND updated_at < (NOW() - INTERVAL 10 MINUTE)`).Scan(&refundingStuckOrders)
			_ = deps.UserOrderModel.DB.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE pay_channel = 'virtualpay' AND status = 'refunded'`).Scan(&refundedOrders)
			_ = deps.UserOrderModel.DB.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE pay_channel = 'virtualpay' AND status = 'success' AND pay_mode = 'short_series_goods' AND COALESCE(deliver_reported, 0) = 0`).Scan(&unreportedGoodsOrders)
		}

		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"ok":           ok,
			"items":        items,
			"sandbox_mode": cfg.VirtualPay.SandboxMode,
			"ios_enabled":  cfg.VirtualPay.IOSEnabled,
			"order_summary": gin.H{
				"total_virtualpay_orders": totalVirtualpayOrders,
				"pending_orders":          pendingOrders,
				"delivering_orders":       deliveringOrders,
				"refunding_stuck_orders":  refundingStuckOrders,
				"refunded_orders":         refundedOrders,
				"unreported_goods_orders": unreportedGoodsOrders,
			},
		}})
	})

	group.GET("/logs", func(c *gin.Context) {
		xpayLogModel := deps.XPayMessageLogModel
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

		result, err := refundVirtualPayRechargeOrder(c.Request.Context(), cfg, deps, orderID, req.Reason, req.Platform)
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

	// 退款影响预览（T8）：退款确认弹窗的数据源。
	// 展示将扣回的灵石、当前余额（含是否充足）、该笔订单授予的会员及回收计划，
	// 让运营在发起退款前看到全部影响，替代原来"扣完才知道"的裸 prompt。
	group.GET("/orders/:id/refund-preview", func(c *gin.Context) {
		orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的订单ID"})
			return
		}
		preview, err := buildVirtualPayRefundPreview(deps, orderID)
		if err != nil {
			status := http.StatusInternalServerError
			if isVirtualPayRefundBadRequest(err) {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"code": status, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": preview})
	})

	// 核对微信侧（T6）：卡在 refunding 的订单立即查微信真实状态。
	// REFUND → 落终态 refunded（含会员回收）；其它状态原样返回供人工判断。
	group.POST("/orders/:id/verify-refund", func(c *gin.Context) {
		orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的订单ID"})
			return
		}
		order, err := deps.UserOrderModel.GetByID(orderID)
		if err != nil || order == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "订单不存在"})
			return
		}
		if order.Status != "refunding" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "仅退款中(refunding)状态的订单需要核对，当前状态=" + order.Status})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		state, verifyErr := verifyRefundingOrderAgainstWechat(ctx, cfg, deps, order.OrderNo)
		if verifyErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "核对失败: " + verifyErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"order_no":     order.OrderNo,
			"wechat_state": state,
			"settled":      state == "REFUND",
		}})
	})

	// 立即推进（T6）：卡在 pending/delivering 的订单手动触发一次 confirm 链路。
	group.POST("/orders/:id/advance", func(c *gin.Context) {
		orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || orderID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的订单ID"})
			return
		}
		order, err := deps.UserOrderModel.GetByID(orderID)
		if err != nil || order == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "订单不存在"})
			return
		}
		if order.Status != "pending" && order.Status != "delivering" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "仅待处理/发货中状态的订单可推进，当前状态=" + order.Status})
			return
		}
		meta, err := deps.UserOrderModel.GetVirtualPayOrderMeta(order.OrderNo)
		if err != nil || meta == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "虚拟支付订单元信息不存在"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()
		state, advanceErr := advanceVirtualPayOrder(ctx, cfg, deps, meta, "")
		if advanceErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "推进失败: " + advanceErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"order_no": order.OrderNo,
			"state":    state,
		}})
	})

	// 重放推送（T6）：用留存的推送原文重新执行业务分发。
	// 分发函数（发货/退款收尾）全部幂等：发货有 claim 原子锁，退款收尾有状态条件保护。
	group.POST("/xpay-logs/:id/replay", func(c *gin.Context) {
		logID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || logID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的日志ID"})
			return
		}
		xpayLogModel := deps.XPayMessageLogModel
		if xpayLogModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "推送日志服务不可用"})
			return
		}
		item, err := xpayLogModel.GetByID(logID)
		if err != nil || item == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "推送日志不存在"})
			return
		}
		if item.OutTradeNo == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "该日志无商户订单号，无法重放"})
			return
		}
		dispatchErr := dispatchXPayEvent(item.Event, item.OutTradeNo, item.OpenID, cfg, deps)
		if dispatchErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "重放失败: " + dispatchErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"id":       item.ID,
			"event":    item.Event,
			"order_no": item.OutTradeNo,
			"replayed": true,
		}})
	})

	// 双账差额汇总（G3）：校准人数 / 差异人数 / 差异名单（按差额绝对值倒序，前 20）。
	group.GET("/calibrations/summary", func(c *gin.Context) {
		calModel := deps.WechatCoinCalibrationModel
		if calModel == nil || calModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "校准服务不可用"})
			return
		}
		var calibratedUsers, diffUsers int64
		if err := calModel.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(diff != 0), 0) FROM wechat_coin_calibrations`).Scan(&calibratedUsers, &diffUsers); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取校准汇总失败: " + err.Error()})
			return
		}
		rows, err := calModel.DB.Query(`
			SELECT c.user_id, COALESCE(u.username, ''), c.measured_balance, c.local_mirror, c.diff, c.order_no, c.calibrated_at
			FROM wechat_coin_calibrations c
			LEFT JOIN users u ON u.id = c.user_id
			WHERE c.diff != 0
			ORDER BY ABS(c.diff) DESC
			LIMIT 20`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取差异名单失败: " + err.Error()})
			return
		}
		defer rows.Close()
		diffs := make([]gin.H, 0)
		for rows.Next() {
			var userID, measured, mirror, diff int64
			var username, orderNo string
			var calibratedAt time.Time
			if err := rows.Scan(&userID, &username, &measured, &mirror, &diff, &orderNo, &calibratedAt); err != nil {
				continue
			}
			diffs = append(diffs, gin.H{
				"user_id":          userID,
				"username":         username,
				"measured_balance": measured,
				"local_mirror":     mirror,
				"diff":             diff,
				"order_no":         orderNo,
				"calibrated_at":    calibratedAt,
			})
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"calibrated_users": calibratedUsers,
			"diff_users":       diffUsers,
			"diffs":            diffs,
		}})
	})

	// 立即校准（G3）：对近期有过代币充值的用户批量尝试实测。
	// 受微信接口约束，离线用户（session_key 过期）无法实测自动跳过；
	// 本操作只收窄"近期活跃用户"的差额盲区，不能替代补偿任务的持续对账。
	group.POST("/calibrate", func(c *gin.Context) {
		if deps.UserOrderModel == nil || deps.UserDBModel == nil || deps.SessionKeyModel == nil || deps.WechatCoinCalibrationModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "校准服务未初始化"})
			return
		}
		userIDs, err := deps.UserOrderModel.ListRecentVirtualpayCoinUserIDs(300)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取校准用户失败: " + err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
		defer cancel()
		checked := 0
		calibrated := 0
		offline := 0
		for _, userID := range userIDs {
			if ctx.Err() != nil {
				break
			}
			checked++
			ok, _, err := calibrateUserBalance(ctx, cfg, deps, userID)
			if err != nil {
				log.Printf("[VirtualPay] 批量校准单用户失败（继续下一个）: userID=%d err=%v", userID, err)
				continue
			}
			if ok {
				calibrated++
			} else {
				offline++
			}
		}
		var diffUsers int64
		_ = deps.WechatCoinCalibrationModel.DB.QueryRow(`SELECT COALESCE(SUM(diff != 0), 0) FROM wechat_coin_calibrations`).Scan(&diffUsers)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
			"checked":    checked,
			"calibrated": calibrated,
			"offline":    offline,
			"diff_found": diffUsers,
		}})
	})
}

// buildVirtualPayRefundPreview 组装退款影响预览（T8）。
func buildVirtualPayRefundPreview(deps VirtualPayDeps, orderID int64) (gin.H, error) {
	if deps.UserOrderModel == nil || deps.UserModel == nil {
		return nil, errors.New("虚拟支付退款服务未初始化")
	}
	order, err := deps.UserOrderModel.GetByID(orderID)
	if err != nil {
		return nil, err
	}
	currentStones, err := deps.UserModel.GetStones(order.UserID)
	if err != nil {
		return nil, fmt.Errorf("查询用户灵石余额失败: %w", err)
	}
	// 复用退款校验（不含"执行"语义）：通道/类型/模式/状态/openid 合法性
	if err := validateVirtualPayRechargeRefund(order, currentStones); err != nil {
		return nil, err
	}

	preview := gin.H{
		"order_id":                order.ID,
		"order_no":                order.OrderNo,
		"user_id":                 order.UserID,
		"stones_to_deduct":        order.Amount,
		"current_stones":          currentStones,
		"sufficient":              currentStones >= order.Amount,
		"amount_fen":              order.AmountFen,
		"membership_recover_plan": "none",
	}
	if deps.UserMembershipModel != nil {
		if membership, mErr := deps.UserMembershipModel.GetActiveByUserID(order.UserID); mErr == nil && membership != nil {
			plan := gin.H{
				"plan_title":      membership.PlanTitle,
				"expired_at":      membership.ExpiredAt,
				"is_lifetime":     model.IsLifetimeMembership(membership),
				"source_order_no": membership.SourceOrderNo,
			}
			switch {
			case membership.SourceOrderNo == order.OrderNo && membership.PrevExpiredAt != nil:
				plan["recover_plan"] = "restored_prev"
				plan["recover_detail"] = fmt.Sprintf("回滚到期时间 %s（本单续期前快照）", membership.PrevExpiredAt.Format("2006-01-02 15:04"))
			case membership.SourceOrderNo == order.OrderNo:
				plan["recover_plan"] = "revoke"
				plan["recover_detail"] = "本单首次授予，退款后整段撤销"
			default:
				plan["recover_plan"] = "skipped_superseded"
				plan["recover_detail"] = "会员已被后续充值续期（" + membership.SourceOrderNo + "），退款不自动回收，请人工评估"
			}
			preview["membership"] = plan
		}
	}
	return preview, nil
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
