package model

import (
	"database/sql"
	"time"
)

// LogConfig 日志配置
type LogConfig struct {
	ID              int64     `json:"id" db:"id"`
	RotateInterval  int       `json:"rotate_interval" db:"rotate_interval"`   // 日志分割间隔（小时），如：24表示每24小时分割一次
	RetentionDays   int       `json:"retention_days" db:"retention_days"`     // 日志保存天数，如：30表示保存30天
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// LogConfigModel 日志配置数据访问层
type LogConfigModel struct {
	DB *sql.DB
}

// NewLogConfigModel 创建日志配置模型
func NewLogConfigModel(db *sql.DB) *LogConfigModel {
	return &LogConfigModel{DB: db}
}

// Get 获取日志配置（只有一条记录）
func (m *LogConfigModel) Get() (*LogConfig, error) {
	config := &LogConfig{}
	query := `SELECT id, rotate_interval, retention_days, created_at, updated_at 
	          FROM log_config ORDER BY id DESC LIMIT 1`
	err := m.DB.QueryRow(query).Scan(
		&config.ID, &config.RotateInterval, &config.RetentionDays, &config.CreatedAt, &config.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			// 如果没有配置，返回默认值
			return &LogConfig{
				RotateInterval: 24,  // 默认24小时分割一次
				RetentionDays:  30,  // 默认保存30天
			}, nil
		}
		return nil, err
	}
	return config, nil
}

// CreateOrUpdate 创建或更新日志配置（只有一条记录）
func (m *LogConfigModel) CreateOrUpdate(rotateInterval, retentionDays int) error {
	// 先检查是否存在记录
	var count int
	err := m.DB.QueryRow("SELECT COUNT(*) FROM log_config").Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		// 如果没有记录，创建新记录
		query := `INSERT INTO log_config (rotate_interval, retention_days, created_at, updated_at) 
		         VALUES (?, ?, NOW(), NOW())`
		_, err = m.DB.Exec(query, rotateInterval, retentionDays)
		return err
	}

	// 如果有记录，更新第一条记录（通常只有一条）
	query := `UPDATE log_config SET rotate_interval = ?, retention_days = ?, updated_at = NOW() 
	          ORDER BY id ASC LIMIT 1`
	_, err = m.DB.Exec(query, rotateInterval, retentionDays)
	return err
}

// InitTable 初始化log_config表
func (m *LogConfigModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS log_config (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	rotate_interval INT NOT NULL DEFAULT 24 COMMENT '日志分割间隔（小时）',
	retention_days INT NOT NULL DEFAULT 30 COMMENT '日志保存天数',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}

	// 如果没有记录，插入默认配置
	var count int
	err = m.DB.QueryRow("SELECT COUNT(*) FROM log_config").Scan(&count)
	if err != nil {
		return err
	}
	if count == 0 {
		_, err = m.DB.Exec("INSERT INTO log_config (rotate_interval, retention_days) VALUES (24, 30)")
		if err != nil {
			return err
		}
	}

	return nil
}
