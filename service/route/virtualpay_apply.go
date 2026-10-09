package route

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"service/model"
	"service/report"
)

// VirtualPayDeps 业务发货所需的依赖集合。
//
// 抽成结构体而非散参数，让 confirm/补偿/推送三处调用点共用相同的注入方式，
// 避免 30+ 参数的函数签名（与 RegisterRoutes 当前局面相比已经很克制）。
type VirtualPayDeps struct {
	UserOrderModel      *model.UserOrderModel
	UserModel           *model.UserRedisModel
	UserDBModel         *model.UserModel
	ReportService       *report.Service
	StoneRecordModel    *model.StoneRecordModel
	MembershipPlanModel *model.MembershipPlanModel
	UserMembershipModel *model.UserMembershipModel
	CertificationModel  *model.CertificationApplicationModel
	XPayMessageLogModel *model.XPayMessageLogModel
	// SessionKeyModel 微信 session_key 缓存：顺路校准（T7）按 openid 取签名凭证
	SessionKeyModel *model.SessionKeyRedisModel
	// WechatCoinCalibrationModel 微信侧代币余额实测记录（D7 双账差额监控）
	WechatCoinCalibrationModel *model.WechatCoinCalibrationModel
	// SupportTicketModel 双账差额自动工单（T7）
	SupportTicketModel *model.SupportTicketModel
	// PricingConfigModel 后台定价配置（充值档位下发/认证道具价格，定价配置化 P1）
	PricingConfigModel *model.PricingConfigModel
}

// ErrOrderNotFound 订单不存在。
var ErrOrderNotFound = errors.New("virtualpay: 订单不存在")

// applyVirtualPayOrderSuccess 在订单"已支付确认"后执行业务发货。
//
// 调用约束：
//   - 必须保证幂等：通过 ClaimVirtualPayPendingOrder 原子锁解决并发重复发货
//   - confirm（阶段 2.2）/ 推送（阶段 3）/ 补偿任务（阶段 2.3）共用此入口
//   - notify_provide_goods 上报由调用方在本函数返回 nil 后异步处理（见 D5）
//
// 状态机：
//   - 已是 success → 直接返回 nil（幂等成功）
//   - pending → claim 成 'delivering' → 走业务发货 → UpdateStatus('success')
//   - claim 失败（status 不是 pending）→ 视为已被其他路径处理；若已 success 返回 nil，否则报错
//
// 处理分支：
//   - recharge 代币模式：加灵石 + 写流水 + 按 amount_fen 匹配会员
//   - certification 道具模式：解析 description 拿 application_id，推进认证工单
//
// 返回值：
//   - 已经处于 success 状态时返回 nil（幂等成功）
//   - 业务发货中途任意一步失败：先 ReleasePendingOrder 把 'delivering' 回退到 'pending'，再返回错误
//     （让调用方稍后重试整笔发货，避免订单永久卡死）
func applyVirtualPayOrderSuccess(deps VirtualPayDeps, orderNo string) error {
	if deps.UserOrderModel == nil {
		return errors.New("virtualpay: UserOrderModel 未注入")
	}

	order, err := deps.UserOrderModel.GetByOrderNo(orderNo)
	if err != nil {
		return fmt.Errorf("get order: %w", err)
	}
	if order == nil {
		return ErrOrderNotFound
	}
	if order.Status == "success" {
		return nil
	}

	// 原子锁：仅 RowsAffected=1 的调用方拿到发货资格
	claimed, err := deps.UserOrderModel.ClaimVirtualPayPendingOrder(orderNo)
	if err != nil {
		return fmt.Errorf("claim order: %w", err)
	}
	if !claimed {
		// 重新读一次状态判断是 success（其它路径已发货完）还是别的终态
		order, err = deps.UserOrderModel.GetByOrderNo(orderNo)
		if err != nil {
			return fmt.Errorf("re-get order: %w", err)
		}
		if order != nil && order.Status == "success" {
			return nil
		}
		// status='delivering' 时另一个调用者正在发货，直接返回让其完成
		// status='failed'/'refunded' 时本次已不可发货
		return fmt.Errorf("virtualpay: 订单状态不可发货 status=%q", order.Status)
	}

	switch order.Type {
	case "recharge":
		if err := deliverRecharge(deps, order); err != nil {
			_ = deps.UserOrderModel.ReleasePendingOrder(orderNo)
			return err
		}
	case "certification":
		if err := deliverCertification(deps, order); err != nil {
			_ = deps.UserOrderModel.ReleasePendingOrder(orderNo)
			return err
		}
	case reportPlanType:
		if err := deliverPaidReport(deps, order); err != nil {
			_ = deps.UserOrderModel.ReleasePendingOrder(orderNo)
			return err
		}
	default:
		_ = deps.UserOrderModel.ReleasePendingOrder(orderNo)
		return fmt.Errorf("virtualpay: 不支持的订单 type=%q", order.Type)
	}

	if err := deps.UserOrderModel.UpdateStatus(orderNo, "success"); err != nil {
		if order.Type == reportPlanType {
			// 报告授权以订单号幂等；回退本地中间态后重试可以收敛，
			// 避免订单永久卡在 delivering。
			_ = deps.UserOrderModel.ReleasePendingOrder(orderNo)
		}
		return fmt.Errorf("update order status: %w", err)
	}
	return nil
}

// deliverRecharge 充值订单发货：加灵石 + 写流水 + 匹配会员。
//
// amount_fen 字段用于会员匹配；订单创建时已落库（CreateVirtualPayOrder），这里不再回查微信侧。
func deliverRecharge(deps VirtualPayDeps, order *model.UserOrder) error {
	if deps.UserModel == nil || order.Amount <= 0 {
		return fmt.Errorf("virtualpay: recharge 缺少 UserModel 或 amount<=0")
	}
	if err := deps.UserModel.AddStones(order.UserID, order.Amount); err != nil {
		return fmt.Errorf("add stones: %w", err)
	}
	if deps.StoneRecordModel != nil {
		if err := deps.StoneRecordModel.Create(order.UserID, "recharge", order.Amount, "虚拟支付充值", order.OrderNo); err != nil {
			// 流水写入失败不阻塞发货，但需要日志告警
			log.Printf("[VirtualPay] stone record err: orderNo=%s err=%v", order.OrderNo, err)
		}
	}

	// 按充值金额匹配会员档位（D2 决策：会员仍走"充值附赠会员"逻辑）
	amountFen, err := readOrderAmountFen(deps, order.OrderNo)
	if err != nil {
		log.Printf("[VirtualPay] read amount_fen err: orderNo=%s err=%v", order.OrderNo, err)
	}
	if deps.UserMembershipModel == nil {
		return nil
	}
	now := time.Now()
	if amountFen > 0 && deps.MembershipPlanModel != nil {
		plan, err := deps.MembershipPlanModel.FindMatchedByRechargeAmountFen(amountFen)
		if err == nil && plan != nil && plan.DurationDays > 0 {
			if _, gerr := deps.UserMembershipModel.GrantOrExtend(order.UserID, plan, order.OrderNo, now); gerr != nil {
				log.Printf("[VirtualPay] grant membership err: orderNo=%s err=%v", order.OrderNo, gerr)
			}
			return nil
		}
	}
	// 没匹配到档位 → 与旧通路一致，授予永久下载权限
	if _, gerr := deps.UserMembershipModel.GrantPermanentDownloadMembership(order.UserID, order.OrderNo, now); gerr != nil {
		log.Printf("[VirtualPay] grant permanent membership err: orderNo=%s err=%v", order.OrderNo, gerr)
	}
	return nil
}

// deliverCertification 认证费订单发货：从 description 解析 application_id，推进工单。
//
// 与旧 JSAPI 通路完全一致：description 形如 "certification:<appID>"。
func deliverCertification(deps VirtualPayDeps, order *model.UserOrder) error {
	if deps.CertificationModel == nil {
		return errors.New("virtualpay: certification 缺少 CertificationModel")
	}
	appID := parseAppIDFromDescription(order.Description)
	if appID <= 0 {
		return fmt.Errorf("virtualpay: certification 订单缺少 application_id, description=%q", order.Description)
	}
	if err := deps.CertificationModel.PromoteToPendingReviewIfConsistent(appID); err != nil {
		return fmt.Errorf("promote certification: %w", err)
	}
	return nil
}

// parseAppIDFromDescription 从 "certification:<id>" 形式的 description 中解析认证工单 ID。
//
// 与 route/certification.go 中 parseCertificationApplicationID 行为一致；
// 没复用是因为本包内私有，避免跨 route 文件耦合带来重构连锁反应。
func parseAppIDFromDescription(desc string) int64 {
	const prefix = "certification:"
	if !strings.HasPrefix(desc, prefix) {
		return 0
	}
	id, err := strconv.ParseInt(desc[len(prefix):], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// readOrderAmountFen 读取 user_orders.amount_fen 列。
//
// 当前 UserOrder struct 还没暴露该字段，独立用一条 SELECT 取出；
// 等 struct 字段补齐后此函数会被替换为直接读 order.AmountFen。
func readOrderAmountFen(deps VirtualPayDeps, orderNo string) (int64, error) {
	row := deps.UserOrderModel.DB.QueryRow(`SELECT COALESCE(amount_fen, 0) FROM user_orders WHERE order_no = ?`, orderNo)
	var v int64
	if err := row.Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

// SettleVirtualPayRefundCompleted 退款在微信侧确认完成后的本地收尾（推送与补偿任务共用）。
//
//  1. 订单落 refunded 终态；
//  2. 整单回收该订单授予的会员（T8 策略：整单回收——首次授予撤销、续期回滚到快照、
//     已被后续订单续期则跳过转人工）。
//
// 收尾放在"退款确认完成"而非"发起退款"时点：若退款中途失败回滚（灵石已退还、
// 状态回 success），会员未被回收，无需补偿；反之在确认完成时回收，不会冤枉用户。
func SettleVirtualPayRefundCompleted(deps VirtualPayDeps, orderNo string) error {
	if deps.UserOrderModel == nil {
		return errors.New("virtualpay: UserOrderModel 未注入")
	}
	if err := deps.UserOrderModel.UpdateStatus(orderNo, "refunded"); err != nil {
		return err
	}
	if deps.UserMembershipModel != nil {
		action, err := deps.UserMembershipModel.RevokeMembershipBySourceOrderNo(orderNo)
		if err != nil {
			log.Printf("[VirtualPay][资金] 退款收尾-会员回收失败，需人工核对: orderNo=%s action=%s err=%v", orderNo, action, err)
		} else {
			log.Printf("[VirtualPay][资金] 退款收尾-会员回收: orderNo=%s action=%s", orderNo, action)
		}
	}
	return nil
}
