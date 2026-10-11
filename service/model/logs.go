package model

import (
	"database/sql"
	"time"
)

// Log 日志记录
type Log struct {
	ID        int64     `json:"id" db:"id"`
	Type      string    `json:"type" db:"type"`           // 日志类型：user_action, system, api, error
	Level     string    `json:"level" db:"level"`          // 日志级别：info, warning, error
	Message   string    `json:"message" db:"message"`      // 日志消息
	UserID    *int64    `json:"user_id" db:"user_id"`     // 用户ID（可选）
	Username  string    `json:"username" db:"username"`    // 用户名（可选）
	Details   string    `json:"details" db:"details"`     // 详细信息（JSON格式）
	IP        string    `json:"ip" db:"ip"`                // IP地址（可选）
	UserAgent string    `json:"user_agent" db:"user_agent"` // User Agent（可选）
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// LogModel 日志数据访问层
type LogModel struct {
	DB *sql.DB
}

// NewLogModel 创建日志模型
func NewLogModel(db *sql.DB) *LogModel {
	return &LogModel{DB: db}
}

// Create 创建日志记录
func (m *LogModel) Create(log *Log) error {
	query := `INSERT INTO logs (type, level, message, user_id, username, details, ip, user_agent, created_at) 
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW())`
	result, err := m.DB.Exec(query, log.Type, log.Level, log.Message, log.UserID, log.Username, 
		log.Details, log.IP, log.UserAgent)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	log.ID = id
	return nil
}

// GetAll 获取日志列表（支持分页和筛选）
func (m *LogModel) GetAll(limit, offset int, logType, level, keyword string, startTime, endTime *time.Time) ([]*Log, error) {
	query := `SELECT id, type, level, message, user_id, username, details, ip, user_agent, created_at 
	          FROM logs WHERE 1=1`
	args := []interface{}{}

	if logType != "" && logType != "all" {
		query += " AND type = ?"
		args = append(args, logType)
	}

	if level != "" && level != "all" {
		query += " AND level = ?"
		args = append(args, level)
	}

	if keyword != "" {
		query += " AND (message LIKE ? OR username LIKE ?)"
		keywordPattern := "%" + keyword + "%"
		args = append(args, keywordPattern, keywordPattern)
	}

	if startTime != nil {
		query += " AND created_at >= ?"
		args = append(args, *startTime)
	}

	if endTime != nil {
		query += " AND created_at <= ?"
		args = append(args, *endTime)
	}

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*Log
	for rows.Next() {
		log := &Log{}
		var userID sql.NullInt64
		err := rows.Scan(
			&log.ID, &log.Type, &log.Level, &log.Message, &userID, &log.Username,
			&log.Details, &log.IP, &log.UserAgent, &log.CreatedAt,
		)
		if err != nil {
			continue
		}
		if userID.Valid {
			uid := userID.Int64
			log.UserID = &uid
		}
		logs = append(logs, log)
	}
	return logs, nil
}

// Count 统计日志总数（支持筛选）
func (m *LogModel) Count(logType, level, keyword string, startTime, endTime *time.Time) (int64, error) {
	query := `SELECT COUNT(*) FROM logs WHERE 1=1`
	args := []interface{}{}

	if logType != "" && logType != "all" {
		query += " AND type = ?"
		args = append(args, logType)
	}

	if level != "" && level != "all" {
		query += " AND level = ?"
		args = append(args, level)
	}

	if keyword != "" {
		query += " AND (message LIKE ? OR username LIKE ?)"
		keywordPattern := "%" + keyword + "%"
		args = append(args, keywordPattern, keywordPattern)
	}

	if startTime != nil {
		query += " AND created_at >= ?"
		args = append(args, *startTime)
	}

	if endTime != nil {
		query += " AND created_at <= ?"
		args = append(args, *endTime)
	}

	var count int64
	err := m.DB.QueryRow(query, args...).Scan(&count)
	return count, err
}

// DeleteOldLogs 删除过期日志（根据保留天数）
func (m *LogModel) DeleteOldLogs(retentionDays int) (int64, error) {
	query := `DELETE FROM logs WHERE created_at < DATE_SUB(NOW(), INTERVAL ? DAY)`
	result, err := m.DB.Exec(query, retentionDays)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := result.RowsAffected()
	return rowsAffected, err
}

// InitTable 初始化logs表
func (m *LogModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS logs (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	type VARCHAR(32) NOT NULL COMMENT '日志类型：user_action, system, api, error',
	level VARCHAR(16) NOT NULL COMMENT '日志级别：info, warning, error',
	message TEXT NOT NULL COMMENT '日志消息',
	user_id BIGINT UNSIGNED DEFAULT NULL COMMENT '用户ID',
	username VARCHAR(128) DEFAULT NULL COMMENT '用户名',
	details TEXT COMMENT '详细信息（JSON格式）',
	ip VARCHAR(45) DEFAULT NULL COMMENT 'IP地址',
	user_agent VARCHAR(512) DEFAULT NULL COMMENT 'User Agent',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	INDEX idx_type (type),
	INDEX idx_level (level),
	INDEX idx_user_id (user_id),
	INDEX idx_created_at (created_at),
	INDEX idx_type_level (type, level)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	return err
}
