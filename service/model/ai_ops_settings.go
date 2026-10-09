package model

import (
	"database/sql"
	"time"
)

// AIOpsSettings AI 运营设置（单行表，id 固定为 1）。
// processor 的供应商健康检查每个基础 tick 读取一次，改完下个周期生效。
// 抽检参数（4a）：成功生成结果按比例随机抽入内容审核队列。
type AIOpsSettings struct {
	ID                   int64     `json:"id"`
	AlertFailedRate      float64   `json:"alert_failed_rate"`      // 触发阈值：近1小时失败率（0~1，默认 0.15）
	AlertMinWindowCalls  int64     `json:"alert_min_window_calls"` // 触发阈值：窗口内最小调用量（默认 20，防低流量误报）
	AlertIntervalMinutes int       `json:"alert_interval_minutes"` // 检查间隔（分钟，默认 10，范围 1~120）
	AuditEnabled         bool      `json:"audit_enabled"`          // 内容抽检开关（默认开）
	AuditSampleRate      float64   `json:"audit_sample_rate"`      // 抽检比例 0~1（默认 0.02 = 2%）
	UpdatedAt            time.Time `json:"updated_at"`
}

// 默认值与 processor 原硬编码一致，建表即写入
const (
	DefaultAIAlertFailedRate      = 0.15
	DefaultAIAlertMinWindowCalls  = int64(20)
	DefaultAIAlertIntervalMinutes = 10
	DefaultAIAuditSampleRate      = 0.02
)

// AIOpsSettingsModel AI 运营设置数据访问层
type AIOpsSettingsModel struct {
	DB *sql.DB
}

func NewAIOpsSettingsModel(db *sql.DB) *AIOpsSettingsModel {
	return &AIOpsSettingsModel{DB: db}
}

func (m *AIOpsSettingsModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS ai_ops_settings (
	id TINYINT UNSIGNED NOT NULL PRIMARY KEY,
	alert_failed_rate DECIMAL(5, 4) NOT NULL DEFAULT 0.1500,
	alert_min_window_calls INT UNSIGNED NOT NULL DEFAULT 20,
	alert_interval_minutes INT UNSIGNED NOT NULL DEFAULT 10,
	audit_enabled TINYINT(1) NOT NULL DEFAULT 1,
	audit_sample_rate DECIMAL(5, 4) NOT NULL DEFAULT 0.0200,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	// 既有表补列（幂等）：抽检参数为 4a 新增
	m.DB.Exec(`ALTER TABLE ai_ops_settings ADD COLUMN audit_enabled TINYINT(1) NOT NULL DEFAULT 1`)
	m.DB.Exec(`ALTER TABLE ai_ops_settings ADD COLUMN audit_sample_rate DECIMAL(5, 4) NOT NULL DEFAULT 0.0200`)
	_, err := m.DB.Exec(`INSERT IGNORE INTO ai_ops_settings (id) VALUES (1)`)
	return err
}

// Get 读取设置（读取失败时由调用方回退默认值，不阻塞告警链路）
func (m *AIOpsSettingsModel) Get() (*AIOpsSettings, error) {
	s := &AIOpsSettings{}
	err := m.DB.QueryRow(
		`SELECT id, alert_failed_rate, alert_min_window_calls, alert_interval_minutes, audit_enabled, audit_sample_rate, updated_at
		 FROM ai_ops_settings WHERE id = 1`).
		Scan(&s.ID, &s.AlertFailedRate, &s.AlertMinWindowCalls, &s.AlertIntervalMinutes, &s.AuditEnabled, &s.AuditSampleRate, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	m.normalize(s)
	return s, nil
}

// Update 更新设置（越界值收敛到安全范围）
func (m *AIOpsSettingsModel) Update(s *AIOpsSettings) error {
	m.normalize(s)
	_, err := m.DB.Exec(
		`UPDATE ai_ops_settings SET alert_failed_rate = ?, alert_min_window_calls = ?, alert_interval_minutes = ?, audit_enabled = ?, audit_sample_rate = ? WHERE id = 1`,
		s.AlertFailedRate, s.AlertMinWindowCalls, s.AlertIntervalMinutes, s.AuditEnabled, s.AuditSampleRate)
	return err
}

// normalize 参数护栏：失败率 1%~100%、最小调用量 ≥1、间隔 1~120 分钟、抽检比例 0~50%
func (m *AIOpsSettingsModel) normalize(s *AIOpsSettings) {
	if s.AlertFailedRate < 0.01 {
		s.AlertFailedRate = 0.01
	}
	if s.AlertFailedRate > 1 {
		s.AlertFailedRate = 1
	}
	if s.AlertMinWindowCalls < 1 {
		s.AlertMinWindowCalls = 1
	}
	if s.AlertIntervalMinutes < 1 {
		s.AlertIntervalMinutes = 1
	}
	if s.AlertIntervalMinutes > 120 {
		s.AlertIntervalMinutes = 120
	}
	if s.AuditSampleRate < 0 {
		s.AuditSampleRate = 0
	}
	if s.AuditSampleRate > 0.5 {
		s.AuditSampleRate = 0.5
	}
}

// DefaultAIOpsSettings 读取失败时的回退值
func DefaultAIOpsSettings() *AIOpsSettings {
	return &AIOpsSettings{
		ID:                   1,
		AlertFailedRate:      DefaultAIAlertFailedRate,
		AlertMinWindowCalls:  DefaultAIAlertMinWindowCalls,
		AlertIntervalMinutes: DefaultAIAlertIntervalMinutes,
		AuditEnabled:         true,
		AuditSampleRate:      DefaultAIAuditSampleRate,
	}
}
