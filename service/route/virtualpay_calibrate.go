package route

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"service/component/virtualpay"
	"service/config"
	"service/function"
	"service/model"
)

// 顺路校准（T7 / D7 双账差额监控）。
//
// 微信 /xpay/query_user_balance 需要用户级签名（session_key），而 session_key
// 只在用户发起支付时缓存 25 分钟（virtualpaySessionKeyTTL）。因此校准策略是
// "尽力而为"：
//   - 用户支付确认成功（confirm/补偿推进到 PAID）后顺路实测一次
//   - session_key 不存在（用户离线）时静默跳过，展示继续用本地镜像
//   - 校准失败不阻塞支付主链路，仅留日志
//   - 实测值与本地镜像有差额时更新校准表并自动生成核对工单（每用户一张未关闭工单）

// calibrateWechatCoinAfterPaid 支付确认成功后校准微信侧代币余额。
//
// 仅代币充值单有意义（道具模式直接扣钱，不产生代币账）。
func calibrateWechatCoinAfterPaid(ctx context.Context, cfg *config.Config, deps VirtualPayDeps, meta *model.VirtualPayOrderMeta, orderNo string) {
	if cfg == nil || deps.WechatCoinCalibrationModel == nil || deps.SessionKeyModel == nil || deps.UserOrderModel == nil {
		return
	}
	if meta == nil || meta.Type != "recharge" || meta.PayMode != "short_series_coin" || meta.OpenID == "" {
		return
	}
	sessionKey, err := deps.SessionKeyModel.GetSessionKey(meta.OpenID)
	if err != nil {
		// 用户离线（session_key 已过期）：跳过实测，本地镜像继续兜底
		return
	}
	mirror, err := deps.UserOrderModel.WechatCoinMirrorByUser(meta.UserID)
	if err != nil {
		log.Printf("[VirtualPay] 校准-读本地镜像失败: orderNo=%s err=%v", orderNo, err)
		return
	}
	accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
	if err != nil {
		log.Printf("[VirtualPay] 校准-取 access_token 失败: orderNo=%s err=%v", orderNo, err)
		return
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := virtualpay.QueryUserBalance(queryCtx, accessToken, sessionKey, virtualpay.QueryUserBalanceRequest{
		OpenID: meta.OpenID,
		UserIP: serverEgressIP(),
		Env:    pickXPayEnv(cfg, ""),
	})
	if err != nil {
		log.Printf("[VirtualPay] 校准-query_user_balance 失败: orderNo=%s err=%v", orderNo, err)
		return
	}
	if resp.ErrCode != 0 {
		log.Printf("[VirtualPay] 校准-query_user_balance 返回错误: orderNo=%s errcode=%d errmsg=%s", orderNo, resp.ErrCode, resp.ErrMsg)
		return
	}

	diff := resp.Balance - mirror
	if upsertErr := deps.WechatCoinCalibrationModel.Upsert(&model.WechatCoinCalibration{
		UserID:          meta.UserID,
		MeasuredBalance: resp.Balance,
		LocalMirror:     mirror,
		Diff:            diff,
		OrderNo:         orderNo,
	}); upsertErr != nil {
		log.Printf("[VirtualPay] 校准-写校准记录失败: orderNo=%s err=%v", orderNo, upsertErr)
	}
	if diff == 0 {
		return
	}

	log.Printf("[VirtualPay][资金] 双账差额: userID=%d orderNo=%s 微信实测=%d 本地镜像=%d 差额=%d",
		meta.UserID, orderNo, resp.Balance, mirror, diff)
	createFundDiffTicket(deps, meta.UserID, orderNo, mirror, resp.Balance, diff)
}

// calibrateUserBalance 对单个用户实测微信侧代币余额并落校准记录（管理端"立即校准"用）。
//
// 返回 calibrated=false 表示用户不满足实测条件（离线：session_key 已过期/不存在）。
// 差额非零时同样落表并生成核对工单。
func calibrateUserBalance(ctx context.Context, cfg *config.Config, deps VirtualPayDeps, userID int64) (bool, int64, error) {
	if deps.UserDBModel == nil || deps.SessionKeyModel == nil || deps.WechatCoinCalibrationModel == nil || deps.UserOrderModel == nil {
		return false, 0, errors.New("校准依赖未注入")
	}
	user, err := deps.UserDBModel.GetByID(userID)
	if err != nil || user == nil || strings.TrimSpace(user.OpenID) == "" {
		return false, 0, nil
	}
	sessionKey, err := deps.SessionKeyModel.GetSessionKey(user.OpenID)
	if err != nil {
		return false, 0, nil // 离线用户：静默跳过
	}
	mirror, err := deps.UserOrderModel.WechatCoinMirrorByUser(userID)
	if err != nil {
		return false, 0, fmt.Errorf("读本地镜像: %w", err)
	}
	accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
	if err != nil {
		return false, 0, fmt.Errorf("取 access_token: %w", err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := virtualpay.QueryUserBalance(queryCtx, accessToken, sessionKey, virtualpay.QueryUserBalanceRequest{
		OpenID: user.OpenID,
		UserIP: serverEgressIP(),
		Env:    pickXPayEnv(cfg, ""),
	})
	if err != nil {
		return false, 0, fmt.Errorf("query_user_balance: %w", err)
	}
	if resp.ErrCode != 0 {
		return false, 0, fmt.Errorf("query_user_balance errcode=%d errmsg=%s", resp.ErrCode, resp.ErrMsg)
	}
	diff := resp.Balance - mirror
	if upsertErr := deps.WechatCoinCalibrationModel.Upsert(&model.WechatCoinCalibration{
		UserID:          userID,
		MeasuredBalance: resp.Balance,
		LocalMirror:     mirror,
		Diff:            diff,
		OrderNo:         "",
	}); upsertErr != nil {
		return true, diff, fmt.Errorf("写校准记录: %w", upsertErr)
	}
	if diff != 0 {
		log.Printf("[VirtualPay][资金] 批量校准发现差额: userID=%d 微信实测=%d 本地镜像=%d 差额=%d", userID, resp.Balance, mirror, diff)
		createFundDiffTicket(deps, userID, "", mirror, resp.Balance, diff)
	}
	return true, diff, nil
}

// createFundDiffTicket 为双账差额生成核对工单；每个用户只保留一张未关闭工单。
func createFundDiffTicket(deps VirtualPayDeps, userID int64, orderNo string, mirror, measured, diff int64) {
	if deps.SupportTicketModel == nil || deps.SupportTicketModel.DB == nil {
		return
	}
	sourceID := strconv.FormatInt(userID, 10)
	if existingID, err := deps.SupportTicketModel.GetLatestOpenTicketIDBySource("fund_diff", sourceID); err == nil {
		log.Printf("[VirtualPay][资金] 用户已有未关闭差额工单 #%d，不重复创建: userID=%d", existingID, userID)
		return
	}
	content := fmt.Sprintf(
		"微信实测代币余额 %d，本地镜像 %d，差额 %+d（实测-镜像）。\n"+
			"常见原因：历史退款推送丢失、补偿任务落终态偏差、非虚拟支付来源灵石（邀请/签到/手工调整）影响。\n"+
			"触发来源：%s。处理建议：核对用户最近退款单与灵石流水，必要时手工调账（须填原因，留审计）。",
		measured, mirror, diff, orderNo)
	if _, err := deps.SupportTicketModel.Create(&model.SupportTicketCreateInput{
		UserID:     userID,
		Type:       "fund_diff",
		SourceType: "fund_diff",
		SourceID:   sourceID,
		Title:      fmt.Sprintf("双账差额待核对：用户 %d（差 %+d 枚）", userID, diff),
		Content:    content,
		Priority:   "high",
		CreatedBy:  "system:coin_calibration",
		SourcePayload: map[string]interface{}{
			"user_id":          userID,
			"order_no":         orderNo,
			"measured_balance": measured,
			"local_mirror":     mirror,
			"diff":             diff,
		},
	}); err != nil {
		log.Printf("[VirtualPay][资金] 差额工单创建失败，需人工关注: userID=%d err=%v", userID, err)
		return
	}
	log.Printf("[VirtualPay][资金] 差额工单已创建: userID=%d diff=%+d orderNo=%s", userID, diff, orderNo)
}

// serverEgressIP 取服务器出口 IP（/xpay/query_user_balance 的 UserIP 参数）。
//
// 微信文档允许填服务端出口 IP（并非用户真实 IP）；通过 UDP connect 探测本地路由源地址，
// 失败时回退 127.0.0.1（接口对 IP 校验宽松，仅记录用途）。
func serverEgressIP() string {
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", 2*time.Second)
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return "127.0.0.1"
}
