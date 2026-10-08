package model

import (
	"database/sql"
	"time"
)

// WechatCoinCalibration 微信侧代币余额的实测校准记录（D7 双账差额监控落地）。
//
// 背景：微信 /xpay/query_user_balance 需要用户级签名（session_key），无法随时查询
// 任意用户，因此"微信侧在账"平时用本地镜像（Σ成功充值 − Σ已退款，见
// UserOrderModel.WechatCoinMirrorByUsers）展示；仅当用户近期发起过支付
// （session_key 未过期）时才实测一次并落本表。
//
// 本表只存"最近一次实测"，不做历史留存；差额核对工单承载审计线索。
type WechatCoinCalibration struct {
	UserID          int64     `json:"user_id"`
	MeasuredBalance int64     `json:"measured_balance"` // 微信实测代币余额
	LocalMirror     int64     `json:"local_mirror"`     // 实测时刻按订单聚合的本地镜像值
	Diff            int64     `json:"diff"`             // measured_balance - local_mirror
	OrderNo         string    `json:"order_no"`         // 触发本次校准的订单号
	CalibratedAt    time.Time `json:"calibrated_at"`
}

// WechatCoinCalibrationModel 微信代币校准记录数据访问层。
type WechatCoinCalibrationModel struct {
	DB *sql.DB
}

// NewWechatCoinCalibrationModel 创建模型。
func NewWechatCoinCalibrationModel(db *sql.DB) *WechatCoinCalibrationModel {
	return &WechatCoinCalibrationModel{DB: db}
}

// InitTable 初始化表。纯增量 CREATE TABLE IF NOT EXISTS，幂等，可随启动自动执行。
func (m *WechatCoinCalibrationModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS wechat_coin_calibrations (
  user_id BIGINT UNSIGNED NOT NULL PRIMARY KEY COMMENT '用户ID',
  measured_balance BIGINT NOT NULL DEFAULT 0 COMMENT '微信实测代币余额',
  local_mirror BIGINT NOT NULL DEFAULT 0 COMMENT '实测时刻本地镜像值',
  diff BIGINT NOT NULL DEFAULT 0 COMMENT '实测 - 本地镜像',
  order_no VARCHAR(64) NOT NULL DEFAULT '' COMMENT '触发校准的订单号',
  calibrated_at DATETIME NOT NULL COMMENT '校准时间',
  KEY idx_calibrated_at (calibrated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='微信侧代币余额实测校准记录';
`
	_, err := m.DB.Exec(schema)
	return err
}

// Upsert 记录一次实测结果（每用户仅保留最近一次）。
func (m *WechatCoinCalibrationModel) Upsert(cal *WechatCoinCalibration) error {
	query := `INSERT INTO wechat_coin_calibrations (user_id, measured_balance, local_mirror, diff, order_no, calibrated_at)
	          VALUES (?, ?, ?, ?, ?, NOW())
	          ON DUPLICATE KEY UPDATE measured_balance = VALUES(measured_balance), local_mirror = VALUES(local_mirror),
	            diff = VALUES(diff), order_no = VALUES(order_no), calibrated_at = NOW()`
	_, err := m.DB.Exec(query, cal.UserID, cal.MeasuredBalance, cal.LocalMirror, cal.Diff, cal.OrderNo)
	return err
}

// GetByUserID 读取某用户最近一次校准记录；无记录返回 sql.ErrNoRows。
func (m *WechatCoinCalibrationModel) GetByUserID(userID int64) (*WechatCoinCalibration, error) {
	row := m.DB.QueryRow(
		`SELECT user_id, measured_balance, local_mirror, diff, order_no, calibrated_at
		 FROM wechat_coin_calibrations WHERE user_id = ?`, userID)
	cal := &WechatCoinCalibration{}
	if err := row.Scan(&cal.UserID, &cal.MeasuredBalance, &cal.LocalMirror, &cal.Diff, &cal.OrderNo, &cal.CalibratedAt); err != nil {
		return nil, err
	}
	return cal, nil
}
