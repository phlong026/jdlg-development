package route

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"service/component/virtualpay"
	"service/config"
	"service/function"
)

// 补偿任务调参（按 TODO 阶段 2.3 约定，避免散落到 ENV）。
//
// 这些常量值用代码硬编码而非配置项，是因为：
//   - 它们与"前端 success 时序 + 微信 30 分钟未支付自动关闭"绑死，运营改它没意义
//   - 改这些值通常意味着对状态机有新的认识，应当走代码评审而非 env 调参
const (
	virtualpayCompensatePollInterval     = 60 * time.Second
	virtualpayCompensateStaleMinutes     = 2
	virtualpayCompensateExpireMinutes    = 30
	virtualpayCompensateBatchLimit       = 50
	virtualpayGoodsReportRetryBatchLimit = 50
	// refundRefundingStaleMinutes 退款单卡在 refunding 超过该时长即触发微信侧核对。
	// 正常流转（claim → 微信退款 → 推送 refunded）在分钟级完成，10 分钟仍卡住
	// 基本可判定推送丢失或退款中途崩溃。
	refundRefundingStaleMinutes = 10
)

// VirtualPayCompensator 周期性扫描卡 pending 的虚拟支付订单，主动复用 confirm 链路推进发货。
//
// 设计取舍：
//   - 不依赖外部调度（如 cron 容器），让补偿任务随服务进程一起启动/关闭，简化部署
//   - 不并发处理同一批订单，按时间正序串行处理，便于排查
//   - 仅在 cfg.VirtualPay.Enabled=true 时启动，关闭虚拟支付总开关即停用补偿
type VirtualPayCompensator struct {
	cfg  *config.Config
	deps VirtualPayDeps

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewVirtualPayCompensator 创建补偿任务。
//
// 依赖与 confirm 完全一致，便于直接调用 confirmVirtualPayOrder 实现"三路汇聚同一发货函数"。
func NewVirtualPayCompensator(cfg *config.Config, deps VirtualPayDeps) *VirtualPayCompensator {
	return &VirtualPayCompensator{
		cfg:    cfg,
		deps:   deps,
		stopCh: make(chan struct{}),
	}
}

// Start 启动后台 goroutine。重复调用是安全的（仅生效一次）。
//
// 总开关关闭时直接返回，不打日志噪音；启动成功打一行 INFO 便于运维确认。
func (vc *VirtualPayCompensator) Start() {
	if vc == nil || !vc.cfg.VirtualPay.Enabled {
		return
	}
	vc.wg.Add(1)
	go vc.loop()
	log.Println("[VirtualPay] compensator started")
}

// Stop 通知后台 goroutine 退出并等待其返回。
//
// 调用顺序：服务关闭流程中先 Stop 补偿任务，再关闭 DB/Redis，避免在退出过程中触发底层连接错误。
func (vc *VirtualPayCompensator) Stop() {
	if vc == nil {
		return
	}
	vc.stopOnce.Do(func() { close(vc.stopCh) })
	vc.wg.Wait()
}

func (vc *VirtualPayCompensator) loop() {
	defer vc.wg.Done()
	ticker := time.NewTicker(virtualpayCompensatePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-vc.stopCh:
			return
		case <-ticker.C:
			vc.tick()
		}
	}
}

// tick 一轮补偿处理。
//
// 流程：
//  1. 先把超 30 分钟仍 pending 的订单批量标 failed（数量通常很少）
//  2. 再列出超 2 分钟仍 pending 的订单，逐条调 confirmVirtualPayOrder 推进
func (vc *VirtualPayCompensator) tick() {
	if expired, err := vc.deps.UserOrderModel.MarkVirtualPayPendingExpired(virtualpayCompensateExpireMinutes); err != nil {
		log.Printf("[VirtualPay] mark expired err: %v", err)
	} else if expired > 0 {
		log.Printf("[VirtualPay] marked %d orders expired (>%dmin pending)", expired, virtualpayCompensateExpireMinutes)
	}

	orderNos, err := vc.deps.UserOrderModel.ListVirtualPayPendingForCompensation(
		virtualpayCompensateStaleMinutes, virtualpayCompensateBatchLimit,
	)
	if err != nil {
		log.Printf("[VirtualPay] list pending err: %v", err)
		return
	}
	if len(orderNos) == 0 {
		vc.scanRefundingStuckOrders()
		vc.retryGoodsDeliverReports()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, orderNo := range orderNos {
		select {
		case <-vc.stopCh:
			return
		default:
		}
		meta, err := vc.deps.UserOrderModel.GetVirtualPayOrderMeta(orderNo)
		if err != nil || meta == nil {
			log.Printf("[VirtualPay] compensate get meta err: orderNo=%s err=%v", orderNo, err)
			continue
		}
		// platform 留空：补偿在服务端发起，按 SandboxMode 决定 env，不感知客户端平台。
		state, err := advanceVirtualPayOrder(ctx, vc.cfg, vc.deps, meta, "")
		if err != nil {
			log.Printf("[VirtualPay] compensate err: orderNo=%s state=%s err=%v", orderNo, state, err)
			continue
		}
		// 顺路校准（T7）：补偿推进到 PAID 时 session_key 往往仍在 25 分钟窗口内，尽力实测一次
		if state == string(virtualpay.OrderStatePaid) {
			calibrateWechatCoinAfterPaid(ctx, vc.cfg, vc.deps, meta, orderNo)
		}
		log.Printf("[VirtualPay] compensate ok: orderNo=%s state=%s", orderNo, state)
	}
	vc.scanRefundingStuckOrders()
	vc.retryGoodsDeliverReports()
}

// verifyRefundingOrderAgainstWechat 核对一笔卡在 refunding 的订单在微信侧的真实状态。
//
// 供补偿任务与管理端"核对微信侧"按钮（T6）共用：
//   - 微信侧订单状态已是 REFUND → 退款确实成功，安全落终态 refunded（幂等，
//     含会员整单回收，SettleVirtualPayRefundCompleted）
//   - 其它状态 → 不自动回滚（无法区分崩溃发生在扣灵石之前还是之后，自动补
//     灵石可能造成重复加账），原样返回由调用方打 [资金] 告警转人工
func verifyRefundingOrderAgainstWechat(ctx context.Context, cfg *config.Config, deps VirtualPayDeps, orderNo string) (virtualpay.OrderState, error) {
	meta, err := deps.UserOrderModel.GetVirtualPayOrderMeta(orderNo)
	if err != nil {
		return "", err
	}
	if meta.OpenID == "" {
		return "", errors.New("订单缺少 openid，无法核对微信侧状态")
	}
	accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
	if err != nil {
		return "", err
	}
	resp, err := virtualpay.QueryOrder(ctx, accessToken, virtualpay.QueryOrderRequest{
		OpenID:     meta.OpenID,
		OutTradeNo: orderNo,
		Env:        pickXPayEnv(cfg, ""),
	})
	if err != nil {
		return "", err
	}
	if resp.State == virtualpay.OrderStateRefund {
		if err := SettleVirtualPayRefundCompleted(deps, orderNo); err != nil {
			return resp.State, err
		}
		log.Printf("[VirtualPay] 微信侧已退款，落终态 refunded: orderNo=%s", orderNo)
	}
	return resp.State, nil
}

// scanRefundingStuckOrders 兜底核对卡在 refunding 的退款单（P1-8）。
//
// 正常流转由微信 xpay_refund_notify 推送把订单推进到 refunded；推送丢失或
// 进程在"claim → 扣灵石 → 微信退款"中途崩溃时订单会永久卡在 refunding，
// 这里周期性按微信侧真实状态收尾（核对逻辑与管理端按钮共用同一函数）。
func (vc *VirtualPayCompensator) scanRefundingStuckOrders() {
	orderNos, err := vc.deps.UserOrderModel.ListVirtualPayRefundingStale(refundRefundingStaleMinutes, virtualpayCompensateBatchLimit)
	if err != nil {
		log.Printf("[VirtualPay] list refunding-stale err: %v", err)
		return
	}
	if len(orderNos) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, orderNo := range orderNos {
		select {
		case <-vc.stopCh:
			return
		default:
		}
		state, err := verifyRefundingOrderAgainstWechat(ctx, vc.cfg, vc.deps, orderNo)
		if err != nil {
			log.Printf("[VirtualPay][资金] refunding-stale 核对失败，需人工处理: orderNo=%s err=%v", orderNo, err)
			continue
		}
		if state != virtualpay.OrderStateRefund {
			log.Printf("[VirtualPay][资金] refunding-stale 微信侧状态=%s（退款未在微信侧生效），订单卡在 refunding，需人工核对扣回与退款是否一致: orderNo=%s", state, orderNo)
		}
	}
}

func (vc *VirtualPayCompensator) retryGoodsDeliverReports() {
	items, err := vc.deps.UserOrderModel.ListVirtualPayGoodsDeliverReportPending(virtualpayGoodsReportRetryBatchLimit)
	if err != nil {
		log.Printf("[VirtualPay] list deliver report pending err: %v", err)
		return
	}
	if len(items) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, meta := range items {
		select {
		case <-vc.stopCh:
			return
		default:
		}
		if err := reportVirtualPayGoodsDelivered(ctx, vc.cfg, vc.deps, meta, ""); err != nil {
			log.Printf("[VirtualPay] retry deliver report err: orderNo=%s err=%v", meta.OrderNo, err)
			continue
		}
		log.Printf("[VirtualPay] retry deliver report ok: orderNo=%s", meta.OrderNo)
	}
}
