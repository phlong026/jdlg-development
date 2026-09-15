package model

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// UserOrder 用户订单（充值/消费/文创/提现）
// type: recharge-充值, consume-消费, culture-文创, withdraw-提现
// amount: 充值为正，消费/文创/提现为负（单位：灵石或元，与业务一致）
type UserOrder struct {
	ID                    int64      `json:"id" db:"id"`
	UserID                int64      `json:"user_id" db:"user_id"`
	DesignerUserID        int64      `json:"designer_user_id" db:"designer_user_id"`
	TemplateID            *int64     `json:"template_id,omitempty" db:"template_id"`
	OrderNo               string     `json:"order_no" db:"order_no"`
	Type                  string     `json:"type" db:"type"` // recharge, consume, culture, withdraw
	OrderCategory         string     `json:"order_category" db:"order_category"`
	Amount                int64      `json:"amount" db:"amount"` // 正数充值，负数消费/文创/提现
	Status                string     `json:"status" db:"status"` // success, pending, failed
	ReviewStatus          string     `json:"review_status" db:"review_status"`
	Title                 string     `json:"title" db:"title"`
	Description           string     `json:"description" db:"description"`
	PayChannel            string     `json:"pay_channel" db:"pay_channel"`
	PayMode               string     `json:"pay_mode" db:"pay_mode"`
	ProductID             string     `json:"product_id" db:"product_id"`
	AmountFen             int64      `json:"amount_fen" db:"amount_fen"`
	DeliverReported       bool       `json:"deliver_reported" db:"deliver_reported"`
	DeliverReportAttempts int        `json:"deliver_report_attempts" db:"deliver_report_attempts"`
	OpenID                string     `json:"openid,omitempty" db:"openid"`
	CompletedAt           *time.Time `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt             time.Time  `json:"created_at" db:"created_at"`
}

type OrderManagementItem struct {
	ID                    int64      `json:"id"`
	UserID                int64      `json:"user_id"`
	Username              string     `json:"username"`
	DesignerUserID        int64      `json:"designer_user_id"`
	TemplateID            *int64     `json:"template_id,omitempty"`
	OrderNo               string     `json:"order_no"`
	Type                  string     `json:"type"`
	OrderCategory         string     `json:"order_category"`
	Amount                int64      `json:"amount"`
	Status                string     `json:"status"`
	ReviewStatus          string     `json:"review_status"`
	Title                 string     `json:"title"`
	Description           string     `json:"description"`
	PayChannel            string     `json:"pay_channel"`
	PayMode               string     `json:"pay_mode"`
	ProductID             string     `json:"product_id"`
	AmountFen             int64      `json:"amount_fen"`
	DeliverReported       bool       `json:"deliver_reported"`
	DeliverReportAttempts int        `json:"deliver_report_attempts"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
}

type OrderManagementSummary struct {
	TotalCount    int64 `json:"total_count"`
	SuccessAmount int64 `json:"success_amount"`
	SuccessCount  int64 `json:"success_count"`
	PendingCount  int64 `json:"pending_count"`
}

// UserOrderModel 用户订单数据访问层
type UserOrderModel struct {
	DB *sql.DB
}

type userOrderExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// NewUserOrderModel 创建模型
func NewUserOrderModel(db *sql.DB) *UserOrderModel {
	return &UserOrderModel{DB: db}
}

// InitTable 初始化表
func (m *UserOrderModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS user_orders (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
  designer_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '设计师用户ID',
	  template_id BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '关联模板ID',
  order_no VARCHAR(64) NOT NULL COMMENT '订单号',
  type VARCHAR(32) NOT NULL COMMENT '类型: recharge/consume/culture/withdraw',
  order_category VARCHAR(32) NOT NULL DEFAULT '' COMMENT '订单分类: template/service/recharge/ai/withdraw/certification',
  amount BIGINT NOT NULL DEFAULT 0 COMMENT '金额：充值为正，消费/文创/提现为负',
  status VARCHAR(32) NOT NULL DEFAULT 'success' COMMENT '状态: success/pending/failed',
  review_status VARCHAR(32) NOT NULL DEFAULT 'not_applicable' COMMENT '评价状态: not_applicable/pending_review/reviewed',
  title VARCHAR(255) DEFAULT '' COMMENT '标题',
  description VARCHAR(512) DEFAULT '' COMMENT '描述',
  completed_at DATETIME NULL DEFAULT NULL COMMENT '订单完成时间',
  user_deleted TINYINT(1) NOT NULL DEFAULT 0,
  user_deleted_at DATETIME NULL DEFAULT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_order_no (order_no),
  KEY idx_user_created (user_id, created_at),
  KEY idx_user_type (user_id, type),
	  KEY idx_user_deleted_created (user_id, user_deleted, created_at),
	  KEY idx_template_id (template_id),
  KEY idx_designer_user (designer_user_id),
  KEY idx_user_review_status (user_id, review_status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户订单(充值/消费/文创/提现)';
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN designer_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '设计师用户ID'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN template_id BIGINT UNSIGNED NULL DEFAULT NULL COMMENT '关联模板ID'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN order_category VARCHAR(32) NOT NULL DEFAULT '' COMMENT '订单分类: template/service/recharge/ai/withdraw/certification'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN review_status VARCHAR(32) NOT NULL DEFAULT 'not_applicable' COMMENT '评价状态: not_applicable/pending_review/reviewed'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN completed_at DATETIME NULL DEFAULT NULL COMMENT '订单完成时间'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN user_deleted TINYINT(1) NOT NULL DEFAULT 0`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN user_deleted_at DATETIME NULL DEFAULT NULL`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD INDEX idx_user_deleted_created (user_id, user_deleted, created_at)`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD INDEX idx_template_id (template_id)`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD INDEX idx_designer_user (designer_user_id)`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD INDEX idx_user_review_status (user_id, review_status)`)

	// 虚拟支付迁移（详见 docs/虚拟支付迁移TODO.md 阶段 1.3）
	// product_id：道具单填 productId（cert_designer / cert_enterprise），代币单为空
	// pay_channel：支付通道，wechatpay（旧 JSAPI）/ virtualpay（新虚拟支付）
	// pay_mode：虚拟支付下单模式，short_series_goods（道具）/ short_series_coin（代币）
	// xpay_attach：透传业务参数，便于追溯
	// amount_fen：原始充值/支付金额（分）。代币单 amount 存的是灵石数，会员匹配/对账需要原始分数
	// deliver_reported / deliver_report_attempts：道具模式 notify_provide_goods 上报状态与重试次数（D5）
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN product_id VARCHAR(64) NULL DEFAULT NULL COMMENT '虚拟支付道具ID'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN pay_channel VARCHAR(16) NOT NULL DEFAULT 'wechatpay' COMMENT '支付通道: wechatpay/virtualpay'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN pay_mode VARCHAR(32) NULL DEFAULT NULL COMMENT '虚拟支付下单模式: short_series_goods/short_series_coin'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN xpay_attach VARCHAR(128) NULL DEFAULT NULL COMMENT '虚拟支付业务透传'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN amount_fen BIGINT NULL DEFAULT NULL COMMENT '原始支付金额(分)，代币单会员匹配与对账使用'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN deliver_reported TINYINT(1) NOT NULL DEFAULT 0 COMMENT '道具模式是否已成功上报 notify_provide_goods'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN deliver_report_attempts INT NOT NULL DEFAULT 0 COMMENT 'notify_provide_goods 上报重试次数'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD COLUMN openid VARCHAR(64) NULL DEFAULT NULL COMMENT '虚拟支付用户 openid，confirm/补偿/推送分发使用'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_orders ADD INDEX idx_pay_channel_status (pay_channel, status)`)
	return nil
}

// Create 插入一条订单
func (m *UserOrderModel) Create(userID int64, orderNo, typ string, amount int64, status, title, description string) error {
	return m.CreateDetailed(&UserOrder{
		UserID:         userID,
		DesignerUserID: 0,
		TemplateID:     nil,
		OrderNo:        orderNo,
		Type:           typ,
		OrderCategory:  typ,
		Amount:         amount,
		Status:         status,
		ReviewStatus:   "not_applicable",
		Title:          title,
		Description:    description,
	})
}

func (m *UserOrderModel) CreateWithTx(tx *sql.Tx, userID int64, orderNo, typ string, amount int64, status, title, description string) error {
	return m.CreateDetailedWithTx(tx, &UserOrder{
		UserID:         userID,
		DesignerUserID: 0,
		TemplateID:     nil,
		OrderNo:        orderNo,
		Type:           typ,
		OrderCategory:  typ,
		Amount:         amount,
		Status:         status,
		ReviewStatus:   "not_applicable",
		Title:          title,
		Description:    description,
	})
}

func (m *UserOrderModel) CreateDetailed(order *UserOrder) error {
	return m.createDetailedWithExecutor(m.DB, order)
}

func (m *UserOrderModel) CreateDetailedWithTx(tx *sql.Tx, order *UserOrder) error {
	return m.createDetailedWithExecutor(tx, order)
}

func (m *UserOrderModel) createDetailedWithExecutor(executor userOrderExecutor, order *UserOrder) error {
	query := `INSERT INTO user_orders (user_id, designer_user_id, template_id, order_no, type, order_category, amount, status, review_status, title, description, completed_at, created_at)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW())`
	var completedAt interface{}
	var templateID interface{}
	if order.CompletedAt != nil {
		completedAt = *order.CompletedAt
	} else if order.Status == "success" {
		completedAt = time.Now()
	}
	if order.TemplateID != nil && *order.TemplateID > 0 {
		templateID = *order.TemplateID
	}
	_, err := executor.Exec(query, order.UserID, order.DesignerUserID, templateID, order.OrderNo, order.Type, order.OrderCategory, order.Amount, order.Status, order.ReviewStatus, order.Title, order.Description, completedAt)
	return err
}

// VirtualPayOrderInput 虚拟支付下单时需写入 user_orders 的全部字段。
//
// 与旧 JSAPI 通路并存：旧通路仍走 CreateDetailed，pay_channel 默认 'wechatpay'；
// 新通路通过本方法创建，显式落 pay_channel/pay_mode/product_id/amount_fen 等字段。
type VirtualPayOrderInput struct {
	UserID        int64
	OpenID        string // 微信用户 openid，confirm/推送/补偿/退款分发都依赖
	OrderNo       string
	Type          string // recharge / certification
	OrderCategory string // recharge / enterprise_recharge / certification
	Amount        int64  // 充值订单为灵石数；认证费订单为 0（金额走 amount_fen）
	AmountFen     int64  // 原始支付金额（分）；会员匹配/对账使用
	Title         string
	Description   string
	PayMode       string // short_series_coin / short_series_goods
	ProductID     string // 道具单填 productId；代币单为空
	XPayAttach    string
}

// CreateVirtualPayOrder 写入一条 pay_channel='virtualpay' 的待支付订单。
//
// 与 CreateDetailed 分离，是为了让"哪些字段是虚拟支付特有的"显式可见，避免在 createDetailedWithExecutor
// 里强行兼容两套语义。订单创建后，前端拿到 order_no 与 signData，调用 wx.requestVirtualPayment 拉起支付。
func (m *UserOrderModel) CreateVirtualPayOrder(in *VirtualPayOrderInput) error {
	query := `INSERT INTO user_orders
		(user_id, order_no, type, order_category, amount, status, review_status, title, description,
		 pay_channel, pay_mode, product_id, amount_fen, xpay_attach, openid, created_at)
		VALUES (?, ?, ?, ?, ?, 'pending', 'not_applicable', ?, ?, 'virtualpay', ?, ?, ?, ?, ?, NOW())`
	var productID, attach, openid interface{}
	if in.ProductID != "" {
		productID = in.ProductID
	}
	if in.XPayAttach != "" {
		attach = in.XPayAttach
	}
	if in.OpenID != "" {
		openid = in.OpenID
	}
	_, err := m.DB.Exec(query,
		in.UserID, in.OrderNo, in.Type, in.OrderCategory, in.Amount, in.Title, in.Description,
		in.PayMode, productID, in.AmountFen, attach, openid,
	)
	return err
}

// VirtualPayOrderMeta 给 confirm/补偿/推送分发用的最小订单元信息。
//
// 不直接复用 UserOrder：UserOrder 不含 openid/pay_mode/amount_fen 等虚拟支付特有字段。
type VirtualPayOrderMeta struct {
	OrderNo   string
	UserID    int64
	OpenID    string
	Status    string
	Type      string
	PayMode   string
	ProductID string
	AmountFen int64
}

// GetVirtualPayOrderMeta 读取虚拟支付订单元信息。
//
// confirm 接口在调用 /xpay/query_order 前必须先取 openid + status，
// 这条专属 SELECT 比走 GetByOrderNo + 单独查 amount_fen/openid 更直接。
func (m *UserOrderModel) GetVirtualPayOrderMeta(orderNo string) (*VirtualPayOrderMeta, error) {
	row := m.DB.QueryRow(
		`SELECT user_id, COALESCE(openid, ''), status, type, COALESCE(pay_mode, ''), COALESCE(product_id, ''), COALESCE(amount_fen, 0)
		 FROM user_orders WHERE order_no = ? AND pay_channel = 'virtualpay'`, orderNo)
	meta := &VirtualPayOrderMeta{OrderNo: orderNo}
	if err := row.Scan(&meta.UserID, &meta.OpenID, &meta.Status, &meta.Type, &meta.PayMode, &meta.ProductID, &meta.AmountFen); err != nil {
		return nil, err
	}
	return meta, nil
}

// MarkDeliverReported 标记一条订单的微信侧 notify_provide_goods 上报状态。
//
// 道具模式发货后必须向微信上报，本方法用于在上报成功/失败后回写：
//   - success 为 true 时 deliver_reported=1，补偿任务跳过
//   - success 为 false 时仅 deliver_report_attempts++，留给补偿任务下次重试
func (m *UserOrderModel) MarkDeliverReported(orderNo string, success bool) error {
	if success {
		_, err := m.DB.Exec(
			`UPDATE user_orders SET deliver_reported = 1, deliver_report_attempts = deliver_report_attempts + 1
			 WHERE order_no = ?`, orderNo)
		return err
	}
	_, err := m.DB.Exec(
		`UPDATE user_orders SET deliver_report_attempts = deliver_report_attempts + 1 WHERE order_no = ?`,
		orderNo)
	return err
}

// ClaimVirtualPayPendingOrder 尝试把虚拟支付订单从 pending 原子推进到中间状态 'delivering'。
//
// 这是 applyVirtualPayOrderSuccess 用于解决"confirm/推送/补偿三路并发同时发货"问题的关键锁：
//   - 仅 RowsAffected=1 的调用方拿到发货资格，其余调用方会拿到 RowsAffected=0
//   - 拿到资格的调用方在业务发货成功后再调 UpdateStatus(orderNo, 'success') 收尾
//   - 调用方失败时应主动调 ReleasePendingOrder 把状态回退到 pending，避免订单卡死
//
// 设计取舍：用中间态而非直接 pending→success，是为了让"业务发货中途失败"
// 这个真实场景能可靠重试；如果一步到位 success，发货异常时订单状态已变，
// 后续重试时幂等检查会以为已发货过而跳过。
func (m *UserOrderModel) ClaimVirtualPayPendingOrder(orderNo string) (bool, error) {
	res, err := m.DB.Exec(
		`UPDATE user_orders SET status = 'delivering' WHERE order_no = ? AND status = 'pending'`,
		orderNo,
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

// ReleasePendingOrder 把一条 'delivering' 状态的订单回退到 'pending'，便于重试。
//
// 仅在 ClaimVirtualPayPendingOrder 拿到锁但业务发货失败时使用。
// 不影响已经走到 success/failed/refunded 的订单。
func (m *UserOrderModel) ReleasePendingOrder(orderNo string) error {
	_, err := m.DB.Exec(
		`UPDATE user_orders SET status = 'pending' WHERE order_no = ? AND status = 'delivering'`,
		orderNo,
	)
	return err
}

// ListVirtualPayPendingForCompensation 列出需要补偿任务关注的虚拟支付订单。
//
// 选择标准：pay_channel='virtualpay' AND status IN ('pending','delivering') 且创建超过 staleMinutes 分钟。
// 把 'delivering' 也纳入：避免某次 claim 后业务进程崩溃留下的"半成品"订单永久卡住。
//
// staleMinutes 用于避免和 confirm 接口竞争（用户刚下单的订单还在前端 success 路径上）。
//
// 仅返回最少必要字段，让补偿任务批量处理时减少回包大小。
func (m *UserOrderModel) ListVirtualPayPendingForCompensation(staleMinutes, limit int) ([]string, error) {
	rows, err := m.DB.Query(
		`SELECT order_no FROM user_orders
		 WHERE pay_channel = 'virtualpay' AND status IN ('pending','delivering')
		   AND created_at < (NOW() - INTERVAL ? MINUTE)
		 ORDER BY created_at ASC LIMIT ?`,
		staleMinutes, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orderNos []string
	for rows.Next() {
		var no string
		if err := rows.Scan(&no); err != nil {
			return nil, err
		}
		orderNos = append(orderNos, no)
	}
	return orderNos, rows.Err()
}

// MarkVirtualPayPendingExpired 把超过 expireMinutes 仍 pending/delivering 的订单批量置为 failed。
func (m *UserOrderModel) MarkVirtualPayPendingExpired(expireMinutes int) (int64, error) {
	result, err := m.DB.Exec(
		`UPDATE user_orders SET status = 'failed'
		 WHERE pay_channel = 'virtualpay' AND status IN ('pending','delivering')
		   AND created_at < (NOW() - INTERVAL ? MINUTE)`,
		expireMinutes,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetByOrderNo 根据订单号查询订单
func (m *UserOrderModel) GetByOrderNo(orderNo string) (*UserOrder, error) {
	query := `SELECT id, user_id, designer_user_id, template_id, order_no, type, order_category, amount, status, review_status, title, description,
	                 COALESCE(pay_channel, 'wechatpay'), COALESCE(pay_mode, ''), COALESCE(product_id, ''), COALESCE(amount_fen, 0),
	                 COALESCE(deliver_reported, 0), COALESCE(deliver_report_attempts, 0), COALESCE(openid, ''), completed_at, created_at
	          FROM user_orders WHERE order_no = ?`
	o := &UserOrder{}
	var templateID sql.NullInt64
	var completedAt sql.NullTime
	err := m.DB.QueryRow(query, orderNo).Scan(
		&o.ID, &o.UserID, &o.DesignerUserID, &templateID, &o.OrderNo, &o.Type, &o.OrderCategory, &o.Amount, &o.Status, &o.ReviewStatus, &o.Title, &o.Description,
		&o.PayChannel, &o.PayMode, &o.ProductID, &o.AmountFen, &o.DeliverReported, &o.DeliverReportAttempts, &o.OpenID, &completedAt, &o.CreatedAt)
	if err != nil {
		return nil, err
	}
	if templateID.Valid {
		tid := templateID.Int64
		o.TemplateID = &tid
	}
	if completedAt.Valid {
		o.CompletedAt = &completedAt.Time
	}
	return o, nil
}

func (m *UserOrderModel) GetByID(id int64) (*UserOrder, error) {
	query := `SELECT id, user_id, designer_user_id, template_id, order_no, type, order_category, amount, status, review_status, title, description,
	                 COALESCE(pay_channel, 'wechatpay'), COALESCE(pay_mode, ''), COALESCE(product_id, ''), COALESCE(amount_fen, 0),
	                 COALESCE(deliver_reported, 0), COALESCE(deliver_report_attempts, 0), COALESCE(openid, ''), completed_at, created_at
	          FROM user_orders WHERE id = ?`
	o := &UserOrder{}
	var templateID sql.NullInt64
	var completedAt sql.NullTime
	err := m.DB.QueryRow(query, id).Scan(&o.ID, &o.UserID, &o.DesignerUserID, &templateID, &o.OrderNo, &o.Type, &o.OrderCategory, &o.Amount, &o.Status, &o.ReviewStatus, &o.Title, &o.Description,
		&o.PayChannel, &o.PayMode, &o.ProductID, &o.AmountFen, &o.DeliverReported, &o.DeliverReportAttempts, &o.OpenID, &completedAt, &o.CreatedAt)
	if err != nil {
		return nil, err
	}
	if templateID.Valid {
		tid := templateID.Int64
		o.TemplateID = &tid
	}
	if completedAt.Valid {
		o.CompletedAt = &completedAt.Time
	}
	return o, nil
}

func (m *UserOrderModel) GetPendingCertificationOrderByUserAndApplication(userID, applicationID int64) (*UserOrder, error) {
	description := "certification:" + strconv.FormatInt(applicationID, 10)
	query := `SELECT id, user_id, designer_user_id, template_id, order_no, type, order_category, amount, status, review_status, title, description,
	                 COALESCE(pay_channel, 'wechatpay'), COALESCE(pay_mode, ''), COALESCE(product_id, ''), COALESCE(amount_fen, 0),
	                 COALESCE(deliver_reported, 0), COALESCE(deliver_report_attempts, 0), COALESCE(openid, ''), completed_at, created_at
	          FROM user_orders
	          WHERE user_id = ? AND type = 'certification' AND description = ? AND status IN ('pending', 'failed')
	          ORDER BY id DESC LIMIT 1`
	o := &UserOrder{}
	var templateID sql.NullInt64
	var completedAt sql.NullTime
	err := m.DB.QueryRow(query, userID, description).Scan(
		&o.ID, &o.UserID, &o.DesignerUserID, &templateID, &o.OrderNo, &o.Type, &o.OrderCategory, &o.Amount, &o.Status, &o.ReviewStatus, &o.Title, &o.Description,
		&o.PayChannel, &o.PayMode, &o.ProductID, &o.AmountFen, &o.DeliverReported, &o.DeliverReportAttempts, &o.OpenID, &completedAt, &o.CreatedAt)
	if err != nil {
		return nil, err
	}
	if templateID.Valid {
		tid := templateID.Int64
		o.TemplateID = &tid
	}
	if completedAt.Valid {
		o.CompletedAt = &completedAt.Time
	}
	return o, nil
}

// UpdateStatus 更新订单状态
func (m *UserOrderModel) UpdateStatus(orderNo, status string) error {
	query := `UPDATE user_orders SET status = ?, completed_at = CASE WHEN ? = 'success' THEN NOW() ELSE completed_at END WHERE order_no = ?`
	_, err := m.DB.Exec(query, status, status, orderNo)
	return err
}

func (m *UserOrderModel) UpdateStatusByID(id int64, status string) error {
	query := `UPDATE user_orders SET status = ?, completed_at = CASE WHEN ? = 'success' THEN NOW() WHEN ? = 'cancelled' THEN NULL ELSE completed_at END WHERE id = ?`
	_, err := m.DB.Exec(query, status, status, status, id)
	return err
}

func (m *UserOrderModel) CancelPendingByID(userID, id int64) error {
	query := `UPDATE user_orders SET status = 'cancelled', completed_at = NULL WHERE id = ? AND user_id = ? AND status = 'pending'`
	result, err := m.DB.Exec(query, id, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *UserOrderModel) HideByID(userID, id int64) error {
	query := `UPDATE user_orders SET user_deleted = 1, user_deleted_at = NOW() WHERE id = ? AND user_id = ? AND COALESCE(user_deleted, 0) = 0`
	result, err := m.DB.Exec(query, id, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *UserOrderModel) UpdateReviewStatus(id int64, reviewStatus string) error {
	query := `UPDATE user_orders SET review_status = ? WHERE id = ?`
	_, err := m.DB.Exec(query, reviewStatus, id)
	return err
}

// GenerateOrderNo 生成订单号
func GenerateOrderNo(prefix string) string {
	return fmt.Sprintf("%s%d%04d", prefix, time.Now().UnixMilli(), time.Now().Nanosecond()%10000)
}

// List 分页查询用户订单，typ 为空或 all 表示全部
func (m *UserOrderModel) List(userID int64, typ string, limit, offset int) ([]*UserOrder, int64, error) {
	where := "user_id = ? AND COALESCE(user_deleted, 0) = 0"
	args := []interface{}{userID}
	if typ != "" && typ != "all" {
		where += " AND type = ?"
		args = append(args, typ)
	}

	var total int64
	countQuery := "SELECT COUNT(*) FROM user_orders WHERE " + where
	if err := m.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	query := `SELECT id, user_id, designer_user_id, template_id, order_no, type, order_category, amount, status, review_status, title, description, completed_at, created_at
	          FROM user_orders WHERE ` + where + `
	          ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []*UserOrder
	for rows.Next() {
		o := &UserOrder{}
		var templateID sql.NullInt64
		var completedAt sql.NullTime
		if err := rows.Scan(&o.ID, &o.UserID, &o.DesignerUserID, &templateID, &o.OrderNo, &o.Type, &o.OrderCategory, &o.Amount, &o.Status, &o.ReviewStatus, &o.Title, &o.Description, &completedAt, &o.CreatedAt); err != nil {
			return nil, 0, err
		}
		if templateID.Valid {
			tid := templateID.Int64
			o.TemplateID = &tid
		}
		if completedAt.Valid {
			o.CompletedAt = &completedAt.Time
		}
		list = append(list, o)
	}
	return list, total, nil
}

func (m *UserOrderModel) ListVirtualPayGoodsDeliverReportPending(limit int) ([]*VirtualPayOrderMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := m.DB.Query(
		`SELECT order_no, user_id, COALESCE(openid, ''), status, type, COALESCE(pay_mode, ''), COALESCE(product_id, ''), COALESCE(amount_fen, 0)
		 FROM user_orders
		 WHERE pay_channel = 'virtualpay'
		   AND status = 'success'
		   AND pay_mode = 'short_series_goods'
		   AND COALESCE(deliver_reported, 0) = 0
		   AND COALESCE(deliver_report_attempts, 0) < 10
		 ORDER BY completed_at ASC, created_at ASC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]*VirtualPayOrderMeta, 0)
	for rows.Next() {
		meta := &VirtualPayOrderMeta{}
		if err := rows.Scan(&meta.OrderNo, &meta.UserID, &meta.OpenID, &meta.Status, &meta.Type, &meta.PayMode, &meta.ProductID, &meta.AmountFen); err != nil {
			return nil, err
		}
		list = append(list, meta)
	}
	return list, rows.Err()
}

// Summary 统计：总订单数、总金额(ABS)、本月订单数、本月金额(ABS)
func (m *UserOrderModel) Summary(userID int64) (totalOrders int64, totalAmount int64, monthOrders int64, monthAmount int64, err error) {
	err = m.DB.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(ABS(amount)), 0)
		FROM user_orders WHERE user_id = ? AND COALESCE(user_deleted, 0) = 0
	`, userID).Scan(&totalOrders, &totalAmount)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	err = m.DB.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(ABS(amount)), 0)
		FROM user_orders
		WHERE user_id = ? AND COALESCE(user_deleted, 0) = 0 AND created_at >= DATE_FORMAT(NOW(), '%Y-%m-01 00:00:00')
	`, userID).Scan(&monthOrders, &monthAmount)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return totalOrders, totalAmount, monthOrders, monthAmount, nil
}

// SummaryByDesignerUserID 统计某设计师被下单的真实订单数
func (m *UserOrderModel) SummaryByDesignerUserID(designerUserID int64) (totalOrders int64, monthOrders int64, err error) {
	err = m.DB.QueryRow(`
		SELECT COUNT(*)
		FROM user_orders
		WHERE designer_user_id = ? AND status = 'success'
	`, designerUserID).Scan(&totalOrders)
	if err != nil {
		return 0, 0, err
	}
	err = m.DB.QueryRow(`
		SELECT COUNT(*)
		FROM user_orders
		WHERE designer_user_id = ? AND status = 'success'
		  AND created_at >= DATE_FORMAT(NOW(), '%Y-%m-01 00:00:00')
	`, designerUserID).Scan(&monthOrders)
	if err != nil {
		return 0, 0, err
	}
	return totalOrders, monthOrders, nil
}

func (m *UserOrderModel) HasSuccessfulRecharge(userID int64) (bool, error) {
	var count int64
	err := m.DB.QueryRow(`
		SELECT COUNT(*)
		FROM user_orders
		WHERE user_id = ? AND type = 'recharge' AND status = 'success' AND amount > 0 AND (order_category = '' OR order_category = 'recharge')
	`, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *UserOrderModel) buildManagementWhere(keyword, orderType, orderCategory, status, payChannel, startDate, endDate string) (string, []interface{}) {
	where := []string{"u.user_type = 'miniprogram'"}
	args := make([]interface{}, 0)
	if keyword != "" {
		likeKeyword := "%" + keyword + "%"
		where = append(where, "(u.username LIKE ? OR CAST(o.user_id AS CHAR) LIKE ? OR o.order_no LIKE ? OR o.title LIKE ?)")
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}
	if orderType != "" && orderType != "all" {
		where = append(where, "o.type = ?")
		args = append(args, orderType)
	}
	if orderCategory != "" && orderCategory != "all" {
		where = append(where, "o.order_category = ?")
		args = append(args, orderCategory)
	}
	if status != "" && status != "all" {
		where = append(where, "o.status = ?")
		args = append(args, status)
	}
	if payChannel != "" && payChannel != "all" {
		where = append(where, "COALESCE(o.pay_channel, 'wechatpay') = ?")
		args = append(args, payChannel)
	}
	if startDate != "" {
		where = append(where, "o.created_at >= ?")
		args = append(args, startDate+" 00:00:00")
	}
	if endDate != "" {
		where = append(where, "o.created_at <= ?")
		args = append(args, endDate+" 23:59:59")
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

func (m *UserOrderModel) ListForManagement(keyword, orderType, orderCategory, status, payChannel, startDate, endDate string, limit, offset int) ([]*OrderManagementItem, int64, error) {
	whereSQL, args := m.buildManagementWhere(keyword, orderType, orderCategory, status, payChannel, startDate, endDate)
	countQuery := `SELECT COUNT(*) FROM user_orders o LEFT JOIN users u ON u.id = o.user_id` + whereSQL
	var total int64
	if err := m.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	query := `
		SELECT o.id, o.user_id, COALESCE(u.username, ''), o.designer_user_id, o.template_id, o.order_no, o.type, o.order_category,
		       o.amount, o.status, o.review_status, o.title, o.description,
		       COALESCE(o.pay_channel, 'wechatpay'), COALESCE(o.pay_mode, ''), COALESCE(o.product_id, ''), COALESCE(o.amount_fen, 0),
		       COALESCE(o.deliver_reported, 0), COALESCE(o.deliver_report_attempts, 0), o.completed_at, o.created_at
		FROM user_orders o
		LEFT JOIN users u ON u.id = o.user_id` + whereSQL + `
		ORDER BY o.created_at DESC
		LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]*OrderManagementItem, 0)
	for rows.Next() {
		item := &OrderManagementItem{}
		var templateID sql.NullInt64
		var completedAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.DesignerUserID, &templateID, &item.OrderNo, &item.Type, &item.OrderCategory, &item.Amount, &item.Status, &item.ReviewStatus, &item.Title, &item.Description,
			&item.PayChannel, &item.PayMode, &item.ProductID, &item.AmountFen, &item.DeliverReported, &item.DeliverReportAttempts, &completedAt, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		if templateID.Valid {
			tid := templateID.Int64
			item.TemplateID = &tid
		}
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (m *UserOrderModel) SummaryForManagement(keyword, orderType, orderCategory, status, payChannel, startDate, endDate string) (*OrderManagementSummary, error) {
	whereSQL, args := m.buildManagementWhere(keyword, orderType, orderCategory, status, payChannel, startDate, endDate)
	query := `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN o.status = 'success' AND o.amount > 0 THEN o.amount ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN o.status = 'success' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN o.status = 'pending' THEN 1 ELSE 0 END), 0)
		FROM user_orders o
		LEFT JOIN users u ON u.id = o.user_id` + whereSQL
	summary := &OrderManagementSummary{}
	if err := m.DB.QueryRow(query, args...).Scan(&summary.TotalCount, &summary.SuccessAmount, &summary.SuccessCount, &summary.PendingCount); err != nil {
		return nil, err
	}
	return summary, nil
}
