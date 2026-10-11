package model

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// 微信订单证据（微信虚拟支付实付金额拉通 R1）。
//
// 背景与约束：
//   - user_orders.amount_fen 是"我方应付口径"；微信实扣金额此前从未回读入库，
//     两方账无法逐单对平（虚拟支付无对账单下载接口，query_order 是唯一取证手段）
//   - 我们线上解析的回包（顶层字符串 state）与官方文档描述（order 嵌套对象、
//     数字 status）存在版本差异，因此解析必须两种形状都兼容；
//     金额字段认不出时存原文、比对记 unknown，绝不误报
//
// 每个订单号一行，每次 query_order 覆盖更新；可空字段采用 COALESCE 合并
// （退款查询回包缺 paid_fee 时不清掉此前存下的实付证据）。

// WechatOrderEvidence 微信侧订单实付证据。
type WechatOrderEvidence struct {
	OrderNo        string    `json:"order_no"`
	WechatState    string    `json:"wechat_state"`
	OrderFeeFen    *int64    `json:"order_fee_fen"`
	PaidFeeFen     *int64    `json:"paid_fee_fen"`
	RefundFeeFen   *int64    `json:"refund_fee_fen"`
	LeftFeeFen     *int64    `json:"left_fee_fen"`
	WxpayOrderID   string    `json:"wxpay_order_id"`
	ChannelOrderID string    `json:"channel_order_id"`
	SettState      *int64    `json:"sett_state"`
	PlatformFeeFen *int64    `json:"platform_fee_fen"`
	PayTime        *int64    `json:"pay_time"`
	AmountMatch    string    `json:"amount_match"`
	RawPayload     string    `json:"raw_payload"`
	QueriedAt      time.Time `json:"queried_at"`
}

// 金额比对结论（AmountMatch 取值）。
const (
	AmountMatchMatch    = "match"    // 微信实付 = 我方应付
	AmountMatchMismatch = "mismatch" // 不一致，需人工核对（已自动生成工单）
	AmountMatchUnknown  = "unknown"  // 回包无金额字段（API 版本差异），仅存原文
)

// WechatOrderEvidenceModel 微信订单证据数据访问层。
type WechatOrderEvidenceModel struct {
	DB *sql.DB
}

// NewWechatOrderEvidenceModel 创建模型。
func NewWechatOrderEvidenceModel(db *sql.DB) *WechatOrderEvidenceModel {
	return &WechatOrderEvidenceModel{DB: db}
}

// InitTable 初始化表（纯增量，幂等）。
func (m *WechatOrderEvidenceModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS wechat_order_evidence (
  order_no VARCHAR(64) NOT NULL PRIMARY KEY COMMENT '商户订单号',
  wechat_state VARCHAR(16) NOT NULL DEFAULT '' COMMENT '微信侧订单状态原文',
  order_fee_fen BIGINT NULL COMMENT '订单金额(分)',
  paid_fee_fen BIGINT NULL COMMENT '微信实付金额(分)',
  refund_fee_fen BIGINT NULL COMMENT '退款金额(分)',
  left_fee_fen BIGINT NULL COMMENT '退款后剩余金额(分)',
  wxpay_order_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '微信支付交易单号',
  channel_order_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '用户微信支付详情页商户单号',
  sett_state INT NULL COMMENT '结算状态：0未开始 1结算中 2成功 3待结算',
  platform_fee_fen BIGINT NULL COMMENT '虚拟支付技术服务费(分)',
  pay_time BIGINT NULL COMMENT '微信侧支付时间(unix秒)',
  amount_match VARCHAR(16) NOT NULL DEFAULT 'unknown' COMMENT '实付vs应付：match/mismatch/unknown',
  raw_payload MEDIUMTEXT NOT NULL COMMENT 'query_order 回包原文',
  queried_at DATETIME NOT NULL COMMENT '最近核对时间',
  KEY idx_amount_match (amount_match),
  KEY idx_queried_at (queried_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='微信虚拟支付订单实付证据（两方账逐单对平）';
`
	_, err := m.DB.Exec(schema)
	return err
}

// Upsert 覆盖式保存证据。
//
// 可空金额字段用 COALESCE 合并：本次查询没带回的字段保留旧值（退款查询
// 回包通常不含 paid_fee，不能把已存实付证据冲掉）；
// amount_match 遵循"已有确定性结论不被 unknown 降级"。
func (m *WechatOrderEvidenceModel) Upsert(ev *WechatOrderEvidence) error {
	if ev == nil || ev.OrderNo == "" {
		return errors.New("微信订单证据缺少订单号")
	}
	if ev.QueriedAt.IsZero() {
		ev.QueriedAt = time.Now()
	}
	_, err := m.DB.Exec(`
		INSERT INTO wechat_order_evidence
			(order_no, wechat_state, order_fee_fen, paid_fee_fen, refund_fee_fen, left_fee_fen,
			 wxpay_order_id, channel_order_id, sett_state, platform_fee_fen, pay_time,
			 amount_match, raw_payload, queried_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			wechat_state = VALUES(wechat_state),
			order_fee_fen = COALESCE(VALUES(order_fee_fen), order_fee_fen),
			paid_fee_fen = COALESCE(VALUES(paid_fee_fen), paid_fee_fen),
			refund_fee_fen = COALESCE(VALUES(refund_fee_fen), refund_fee_fen),
			left_fee_fen = COALESCE(VALUES(left_fee_fen), left_fee_fen),
			wxpay_order_id = COALESCE(NULLIF(VALUES(wxpay_order_id), ''), wxpay_order_id),
			channel_order_id = COALESCE(NULLIF(VALUES(channel_order_id), ''), channel_order_id),
			sett_state = COALESCE(VALUES(sett_state), sett_state),
			platform_fee_fen = COALESCE(VALUES(platform_fee_fen), platform_fee_fen),
			pay_time = COALESCE(VALUES(pay_time), pay_time),
			amount_match = IF(VALUES(amount_match) = 'unknown', amount_match, VALUES(amount_match)),
			raw_payload = VALUES(raw_payload),
			queried_at = VALUES(queried_at)
	`, ev.OrderNo, ev.WechatState, ev.OrderFeeFen, ev.PaidFeeFen, ev.RefundFeeFen, ev.LeftFeeFen,
		ev.WxpayOrderID, ev.ChannelOrderID, ev.SettState, ev.PlatformFeeFen, ev.PayTime,
		ev.AmountMatch, ev.RawPayload, ev.QueriedAt)
	return err
}

// GetByOrderNo 读取订单证据；无记录返回 sql.ErrNoRows。
func (m *WechatOrderEvidenceModel) GetByOrderNo(orderNo string) (*WechatOrderEvidence, error) {
	row := m.DB.QueryRow(`
		SELECT order_no, wechat_state, order_fee_fen, paid_fee_fen, refund_fee_fen, left_fee_fen,
		       wxpay_order_id, channel_order_id, sett_state, platform_fee_fen, pay_time,
		       amount_match, raw_payload, queried_at
		FROM wechat_order_evidence WHERE order_no = ?`, orderNo)
	ev := &WechatOrderEvidence{}
	if err := row.Scan(&ev.OrderNo, &ev.WechatState, &ev.OrderFeeFen, &ev.PaidFeeFen, &ev.RefundFeeFen, &ev.LeftFeeFen,
		&ev.WxpayOrderID, &ev.ChannelOrderID, &ev.SettState, &ev.PlatformFeeFen, &ev.PayTime,
		&ev.AmountMatch, &ev.RawPayload, &ev.QueriedAt); err != nil {
		return nil, err
	}
	return ev, nil
}

// ParseWechatOrderEvidence 从 query_order 回包原文容错解析证据字段。
//
// 兼容两种结构（线上版本差异）：
//   - 顶层平铺：{"state":"PAID","order_fee":...,"paid_fee":...}
//   - order 嵌套：{"order":{"status":2,"order_fee":...,"paid_fee":...}}
//
// 找不到的字段留空/nil；原文完整保留。解析失败返回错误（调用方仅记日志，
// 不影响支付主链路）。
func ParseWechatOrderEvidence(raw []byte) (*WechatOrderEvidence, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root map[string]interface{}
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	maps := []map[string]interface{}{root}
	if nested, ok := root["order"].(map[string]interface{}); ok {
		maps = append(maps, nested)
	}

	ev := &WechatOrderEvidence{RawPayload: string(raw)}
	// 状态：优先字符串 state，其次数字 status
	if v, ok := pickString(maps, "state"); ok {
		ev.WechatState = v
	} else if n, ok := pickNumber(maps, "status"); ok {
		ev.WechatState = n.String()
	}
	if n, ok := pickNumber(maps, "order_fee"); ok {
		if v, ok := numToInt64(n); ok {
			ev.OrderFeeFen = &v
		}
	}
	if n, ok := pickNumber(maps, "paid_fee"); ok {
		if v, ok := numToInt64(n); ok {
			ev.PaidFeeFen = &v
		}
	}
	if n, ok := pickNumber(maps, "refund_fee"); ok {
		if v, ok := numToInt64(n); ok {
			ev.RefundFeeFen = &v
		}
	}
	if n, ok := pickNumber(maps, "left_fee"); ok {
		if v, ok := numToInt64(n); ok {
			ev.LeftFeeFen = &v
		}
	}
	ev.WxpayOrderID, _ = pickString(maps, "wxpay_order_id", "wx_order_id")
	ev.ChannelOrderID, _ = pickString(maps, "channel_order_id")
	if n, ok := pickNumber(maps, "sett_state"); ok {
		if v, ok := numToInt64(n); ok {
			ev.SettState = &v
		}
	}
	if n, ok := pickNumber(maps, "platform_fee_fen"); ok {
		if v, ok := numToInt64(n); ok {
			ev.PlatformFeeFen = &v
		}
	}
	if n, ok := pickNumber(maps, "pay_time", "paid_time"); ok {
		if v, ok := numToInt64(n); ok {
			ev.PayTime = &v
		}
	}
	return ev, nil
}

// ComparePaidAmount 应付 vs 实付比对（纯函数）。
//
// 仅当回包带 paid_fee 时给出确定性结论；否则 unknown（不误报）。
func ComparePaidAmount(expectedFen int64, ev *WechatOrderEvidence) string {
	if ev == nil || ev.PaidFeeFen == nil {
		return AmountMatchUnknown
	}
	if *ev.PaidFeeFen == expectedFen {
		return AmountMatchMatch
	}
	return AmountMatchMismatch
}

// numToInt64 json.Number 安全转 int64（超长/小数回退 Float64）。
func numToInt64(n json.Number) (int64, bool) {
	if v, err := n.Int64(); err == nil {
		return v, true
	}
	if f, err := n.Float64(); err == nil {
		return int64(f), true
	}
	return 0, false
}

// pickNumber 依次在多层 JSON 对象里找第一个存在的数字字段。
func pickNumber(maps []map[string]interface{}, keys ...string) (json.Number, bool) {
	for _, m := range maps {
		for _, key := range keys {
			if v, ok := m[key]; ok {
				if n, ok := v.(json.Number); ok {
					return n, true
				}
			}
		}
	}
	return "", false
}

// pickString 依次在多层 JSON 对象里找第一个存在的非空字符串字段。
func pickString(maps []map[string]interface{}, keys ...string) (string, bool) {
	for _, m := range maps {
		for _, key := range keys {
			if v, ok := m[key]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s, true
				}
			}
		}
	}
	return "", false
}
