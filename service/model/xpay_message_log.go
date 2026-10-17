package model

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

// XPayMessageLog 微信虚拟支付消息推送日志（阶段 3.1 / D5）。
//
// 用途：
//   - 推送幂等去重：(out_trade_no, event) 唯一索引，重放推送被 INSERT 唯一冲突拦下
//   - 异步重试追踪：道具模式 notify_provide_goods 失败时通过此表记录与重试
//   - 异常排查：保留 raw_payload 便于追溯具体推送内容
//
// UserID / Username 是管理端列表的反查字段（非本表列），由 List 通过
// out_trade_no → user_orders.order_no → users 联查填充；无对应订单时为 0/空。
type XPayMessageLog struct {
	ID         int64     `json:"id" db:"id"`
	MsgID      string    `json:"msg_id" db:"msg_id"`
	Event      string    `json:"event" db:"event"`
	OpenID     string    `json:"openid" db:"openid"`
	OutTradeNo string    `json:"out_trade_no" db:"out_trade_no"`
	RawPayload string    `json:"raw_payload" db:"raw_payload"`
	Processed  bool      `json:"processed" db:"processed"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UserID     int64     `json:"user_id"`
	Username   string    `json:"username"`
}

// XPayMessageLogModel 数据访问层。
type XPayMessageLogModel struct {
	DB *sql.DB
}

type XPayMessageLogSummary struct {
	TotalCount       int64 `json:"total_count"`
	ProcessedCount   int64 `json:"processed_count"`
	UnprocessedCount int64 `json:"unprocessed_count"`
}

// NewXPayMessageLogModel 创建模型。
func NewXPayMessageLogModel(db *sql.DB) *XPayMessageLogModel {
	return &XPayMessageLogModel{DB: db}
}

// ErrXPayMessageDuplicated 同一 (out_trade_no, event) 已存在，说明这条推送已被处理过。
//
// 调用方应直接返回 ErrCode=0 给微信，不重复发货。
var ErrXPayMessageDuplicated = errors.New("xpay message already processed")

// InitTable 初始化 xpay_message_logs 表。
//
// 沿用项目 InitTable 习惯：CREATE TABLE IF NOT EXISTS + 必要时 ALTER TABLE 兜底。
func (m *XPayMessageLogModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS xpay_message_logs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  msg_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '微信消息ID',
  event VARCHAR(64) NOT NULL COMMENT '事件: xpay_goods_deliver_notify/xpay_refund_notify/...',
  openid VARCHAR(64) NOT NULL DEFAULT '' COMMENT '用户 openid',
  out_trade_no VARCHAR(64) NOT NULL COMMENT '商户订单号',
  raw_payload MEDIUMTEXT NULL COMMENT '原始推送内容(已解密)',
  processed TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否已处理',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_trade_event (out_trade_no, event),
  KEY idx_event_created (event, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='虚拟支付消息推送日志(幂等)';
`
	_, err := m.DB.Exec(schema)
	return err
}

// Insert 插入一条推送日志。
//
// 命中 (out_trade_no, event) 唯一约束时返回 ErrXPayMessageDuplicated，
// 调用方据此识别为重复推送（直接 ack），不再继续业务处理。
func (m *XPayMessageLogModel) Insert(log *XPayMessageLog) error {
	query := `INSERT INTO xpay_message_logs (msg_id, event, openid, out_trade_no, raw_payload, processed)
	          VALUES (?, ?, ?, ?, ?, ?)`
	_, err := m.DB.Exec(query, log.MsgID, log.Event, log.OpenID, log.OutTradeNo, log.RawPayload, log.Processed)
	if isDuplicateEntry(err) {
		return ErrXPayMessageDuplicated
	}
	return err
}

// MarkProcessed 将一条日志标记为已处理。
//
// 用于业务发货成功后回写状态；推送幂等本身依赖唯一索引，processed 字段用于审计与异步重试判断。
func (m *XPayMessageLogModel) MarkProcessed(outTradeNo, event string) error {
	query := `UPDATE xpay_message_logs SET processed = 1 WHERE out_trade_no = ? AND event = ?`
	_, err := m.DB.Exec(query, outTradeNo, event)
	return err
}

// GetByID 按日志 ID 读取单条推送（管理端"重放推送"用）。
func (m *XPayMessageLogModel) GetByID(id int64) (*XPayMessageLog, error) {
	row := m.DB.QueryRow(
		`SELECT id, msg_id, event, openid, out_trade_no, COALESCE(raw_payload, ''), processed, created_at
		 FROM xpay_message_logs WHERE id = ?`, id)
	item := &XPayMessageLog{}
	if err := row.Scan(&item.ID, &item.MsgID, &item.Event, &item.OpenID, &item.OutTradeNo, &item.RawPayload, &item.Processed, &item.CreatedAt); err != nil {
		return nil, err
	}
	return item, nil
}

// xpayLogCustomerJoin 反查客户用的两个 JOIN：order_no 在 user_orders 全表唯一（uk_order_no），
// JOIN 不会放大行数，COUNT 与列表查询可以共用。
const xpayLogCustomerJoin = " LEFT JOIN user_orders o ON o.order_no = l.out_trade_no LEFT JOIN users u ON u.id = o.user_id"

// buildXPayLogWhere 构造推送日志筛选的 WHERE 子句与参数。
// 注意：keyword 条件引用了 u.username，调用方必须拼接 xpayLogCustomerJoin，
// 否则带关键词的 COUNT 会报 Unknown column 'u.username'。
func buildXPayLogWhere(keyword, event string, processed *bool) (string, []interface{}) {
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	if keyword != "" {
		like := "%" + keyword + "%"
		// 关键词同时命中推送字段与客户名：运营手里通常是订单号或客户名，不是 openid。
		where = append(where, "(l.out_trade_no LIKE ? OR l.openid LIKE ? OR l.msg_id LIKE ? OR u.username LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if event != "" && event != "all" {
		where = append(where, "l.event = ?")
		args = append(args, event)
	}
	if processed != nil {
		where = append(where, "l.processed = ?")
		if *processed {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

func (m *XPayMessageLogModel) List(keyword, event string, processed *bool, limit, offset int) ([]*XPayMessageLog, int64, error) {
	whereSQL, args := buildXPayLogWhere(keyword, event, processed)
	var total int64
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM xpay_message_logs l`+xpayLogCustomerJoin+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := m.DB.Query(`SELECT l.id, l.msg_id, l.event, l.openid, l.out_trade_no, COALESCE(l.raw_payload, ''), l.processed, l.created_at,
		COALESCE(o.user_id, 0), COALESCE(u.username, '')
		FROM xpay_message_logs l`+xpayLogCustomerJoin+whereSQL+` ORDER BY l.created_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]*XPayMessageLog, 0)
	for rows.Next() {
		item := &XPayMessageLog{}
		if err := rows.Scan(&item.ID, &item.MsgID, &item.Event, &item.OpenID, &item.OutTradeNo, &item.RawPayload, &item.Processed, &item.CreatedAt, &item.UserID, &item.Username); err != nil {
			return nil, 0, err
		}
		list = append(list, item)
	}
	return list, total, rows.Err()
}

func (m *XPayMessageLogModel) Summary() (*XPayMessageLogSummary, error) {
	summary := &XPayMessageLogSummary{}
	err := m.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN processed = 1 THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN processed = 0 THEN 1 ELSE 0 END), 0) FROM xpay_message_logs`).
		Scan(&summary.TotalCount, &summary.ProcessedCount, &summary.UnprocessedCount)
	return summary, err
}

// isDuplicateEntry 判断是否为 MySQL 唯一约束冲突（错误码 1062）。
func isDuplicateEntry(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
