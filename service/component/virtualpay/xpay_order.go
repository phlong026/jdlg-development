package virtualpay

import (
	"context"
	"encoding/json"
	"fmt"
)

// OrderState 微信虚拟支付订单状态。
//
// 取值与文档对齐，调用方按状态机推进：
//   - PAYING：支付中（用户已拉起但未确认）
//   - NOTPAY：未支付（包含尚未发起）
//   - PAID：已支付（应触发发货）
//   - CLOSED：已关闭
//   - REVOKED：已撤销
//   - REFUND：已退款
type OrderState string

const (
	OrderStatePaying  OrderState = "PAYING"
	OrderStateNotPay  OrderState = "NOTPAY"
	OrderStatePaid    OrderState = "PAID"
	OrderStateClosed  OrderState = "CLOSED"
	OrderStateRevoked OrderState = "REVOKED"
	OrderStateRefund  OrderState = "REFUND"
)

// QueryOrderRequest /xpay/query_order 请求参数。
type QueryOrderRequest struct {
	OpenID     string `json:"openid"`
	OutTradeNo string `json:"order_id"`
	Env        int    `json:"env"`
}

// QueryOrderResponse /xpay/query_order 响应。
//
// 仅列出业务实际使用的字段，避免与文档每次更新都强耦合；新字段按需追加。
// Raw 保留回包原文：微信订单证据（两方账拉通 R1）依赖原文做实付金额取证，
// 字段解析在 model.ParseWechatOrderEvidence 中容错兼容两种回包结构。
type QueryOrderResponse struct {
	ErrCode int        `json:"errcode"`
	ErrMsg  string     `json:"errmsg"`
	State   OrderState `json:"state"`
	// PayTime 订单支付完成时间（10 位 unix 秒）。仅 State=PAID 时有值。
	PayTime int64 `json:"pay_time,omitempty"`
	// Raw 回包原文。字段随 API 版本漂移，取证与排查以原文为准。
	Raw []byte `json:"-"`
}

// QueryOrder 查询虚拟支付订单状态。
//
// 调用场景：
//   - 阶段 2.2 的 confirm 接口收到前端 success 回调后主动核对
//   - 阶段 2.3 的补偿任务定时扫描卡 pending 的订单
//   - 阶段 3.3 处理退款推送时核对原订单
//
// errcode 非 0 时返回 (nil, err)，避免上层误用未定义的 state。
func QueryOrder(ctx context.Context, accessToken string, req QueryOrderRequest) (*QueryOrderResponse, error) {
	raw, err := Post(ctx, accessToken, "/xpay/query_order", req, "")
	if err != nil {
		return nil, err
	}
	var resp QueryOrderResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("virtualpay: parse query_order: %w", err)
	}
	resp.Raw = raw
	return &resp, nil
}

// RefundOrderRequest /xpay/refund_order 请求参数。
//
// RefundAmount 单位：分（道具模式）或灵石数（代币模式）；调用方按订单 pay_mode 自行换算。
type RefundOrderRequest struct {
	OpenID       string `json:"openid"`
	OutTradeNo   string `json:"order_id"`
	OutRefundNo  string `json:"out_refund_no"`
	RefundAmount int64  `json:"refund_amount"`
	Reason       string `json:"reason,omitempty"`
	Env          int    `json:"env"`
}

// RefundOrder 发起退款。返回原始响应，业务层按 errcode 判断是否成功。
//
// 退款最终结果通过推送 xpay_refund_notify 到达；本接口仅触发请求。
func RefundOrder(ctx context.Context, accessToken string, req RefundOrderRequest) (json.RawMessage, error) {
	if req.OutRefundNo == "" {
		return nil, fmt.Errorf("virtualpay: out_refund_no 不能为空")
	}
	return Post(ctx, accessToken, "/xpay/refund_order", req, "")
}

// NotifyProvideGoodsRequest /xpay/notify_provide_goods 请求参数。
type NotifyProvideGoodsRequest struct {
	OpenID     string `json:"openid"`
	OutTradeNo string `json:"order_id"`
	Env        int    `json:"env"`
}

// NotifyProvideGoods 道具模式订单发货后上报微信。
//
// 调用约束（D5 决策）：
//   - 仅道具模式（pay_mode=short_series_goods）调用，代币模式不需要
//   - 上报失败不阻塞本地发货；由补偿任务异步重试
func NotifyProvideGoods(ctx context.Context, accessToken string, req NotifyProvideGoodsRequest) error {
	_, err := Post(ctx, accessToken, "/xpay/notify_provide_goods", req, "")
	return err
}
