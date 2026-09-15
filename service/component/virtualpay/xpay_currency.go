package virtualpay

import (
	"context"
	"encoding/json"
	"fmt"
)

// QueryUserBalanceRequest /xpay/query_user_balance 请求参数。
//
// UserIP 取值：服务端发起调用时填服务器出口 IP，并非用户真实 IP（文档允许）。
type QueryUserBalanceRequest struct {
	OpenID string `json:"openid"`
	UserIP string `json:"user_ip"`
	Env    int    `json:"env"`
}

// QueryUserBalanceResponse /xpay/query_user_balance 响应。
//
// Balance 单位：代币（灵石）个数。代币模式下与本地灵石余额的差值 = 该用户在 AI 服务上的本地消费总额。
type QueryUserBalanceResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	Balance int64  `json:"balance"`
}

// QueryUserBalance 查询用户在微信侧的代币余额。
//
// 调用约束（一期）：
//   - 仅用于财务对账与"双账差额监控"（D7），不参与日常扣减
//   - 该接口需要 signature（带用户身份），调用方必须提供 sessionKey
func QueryUserBalance(ctx context.Context, accessToken, sessionKey string, req QueryUserBalanceRequest) (*QueryUserBalanceResponse, error) {
	if sessionKey == "" {
		return nil, fmt.Errorf("virtualpay: query_user_balance 必须提供 sessionKey")
	}
	raw, err := Post(ctx, accessToken, "/xpay/query_user_balance", req, sessionKey)
	if err != nil {
		return nil, err
	}
	var resp QueryUserBalanceResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("virtualpay: parse query_user_balance: %w", err)
	}
	return &resp, nil
}
