package route

import (
	"fmt"
	"log"

	"service/component/virtualpay"
	"service/model"
)

// 微信订单实付证据（两方账拉通 R2）。
//
// 挂钩点：advanceVirtualPayOrder 与 verifyRefundingOrderAgainstWechat 里
// 本来就会调 /xpay/query_order，把回包顺手存证并比对——零额外微信调用。
// 旁路设计：存证/比对失败只留日志，绝不影响支付与退款主链路。

// captureWechatOrderEvidence 存证 + 实付比对。
//
//   - 回包解析容错（两种结构），金额字段认不出时比对记 unknown，仅存原文
//   - state=PAID 且实付 != 应付 → 自动生成 [资金] pay_diff 工单（每单一张）
//   - 退款路径（REFUND）只补证据不生成 pay_diff 工单（付款时的比对机会已过）
func captureWechatOrderEvidence(deps VirtualPayDeps, meta *model.VirtualPayOrderMeta, orderNo string, resp *virtualpay.QueryOrderResponse) {
	if deps.WechatOrderEvidenceModel == nil || resp == nil || len(resp.Raw) == 0 {
		return
	}
	ev, err := model.ParseWechatOrderEvidence(resp.Raw)
	if err != nil {
		log.Printf("[VirtualPay][资金] 订单证据解析失败（原文已留推送日志）: orderNo=%s err=%v", orderNo, err)
		return
	}
	ev.OrderNo = orderNo
	ev.WechatState = string(resp.State)
	ev.AmountMatch = model.ComparePaidAmount(meta.AmountFen, ev)
	if err := deps.WechatOrderEvidenceModel.Upsert(ev); err != nil {
		log.Printf("[VirtualPay][资金] 订单证据保存失败: orderNo=%s err=%v", orderNo, err)
		return
	}
	if ev.AmountMatch == model.AmountMatchMismatch {
		log.Printf("[VirtualPay][资金] 实付与应付不一致: orderNo=%s 应付=%d 微信实付=%d", orderNo, meta.AmountFen, *ev.PaidFeeFen)
		createPayDiffTicket(deps, meta, ev)
	}
}

// createPayDiffTicket 实付与应付不一致的核对工单（每单一张未关闭）。
func createPayDiffTicket(deps VirtualPayDeps, meta *model.VirtualPayOrderMeta, ev *model.WechatOrderEvidence) {
	if deps.SupportTicketModel == nil || deps.SupportTicketModel.DB == nil {
		return
	}
	if existingID, err := deps.SupportTicketModel.GetLatestOpenTicketIDBySource("pay_diff", ev.OrderNo); err == nil {
		log.Printf("[VirtualPay][资金] 订单已有未关闭实付差异工单 #%d: orderNo=%s", existingID, ev.OrderNo)
		return
	}
	diff := *ev.PaidFeeFen - meta.AmountFen
	if _, err := deps.SupportTicketModel.Create(&model.SupportTicketCreateInput{
		UserID:     meta.UserID,
		Type:       "pay_diff",
		SourceType: "pay_diff",
		SourceID:   ev.OrderNo,
		Title:      fmt.Sprintf("实付与应付不一致：%s（差 %+d 分）", ev.OrderNo, diff),
		Content: fmt.Sprintf(
			"微信实付 %d 分，我方应付 %d 分，差额 %+d 分（实付-应付）。\n"+
				"支付通道：虚拟支付 · %s；微信侧状态：%s；微信支付单号：%s。\n"+
				"常见原因：微信 mp 后台代币兑换比例与本地配置不一致、优惠/活动扣减、异常部分退款。\n"+
				"处理建议：以 wechat_order_evidence.raw_payload 原文为准核对微信侧真实扣款，必要时联系用户核实。",
			*ev.PaidFeeFen, meta.AmountFen, diff,
			map[string]string{"short_series_coin": "代币模式", "short_series_goods": "道具模式"}[meta.PayMode],
			ev.WechatState, ev.WxpayOrderID),
		Priority:   "high",
		CreatedBy:  "system:pay_evidence",
		SourcePayload: map[string]interface{}{
			"order_no":       ev.OrderNo,
			"user_id":        meta.UserID,
			"expected_fen":   meta.AmountFen,
			"paid_fee_fen":   *ev.PaidFeeFen,
			"diff_fen":       diff,
			"wxpay_order_id": ev.WxpayOrderID,
			"wechat_state":   ev.WechatState,
		},
	}); err != nil {
		log.Printf("[VirtualPay][资金] 实付差异工单创建失败，需人工关注: orderNo=%s err=%v", ev.OrderNo, err)
		return
	}
	log.Printf("[VirtualPay][资金] 实付差异工单已创建: orderNo=%s diff=%+d", ev.OrderNo, diff)
}
