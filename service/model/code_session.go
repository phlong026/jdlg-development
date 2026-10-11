package model

import (
	"database/sql"
	"time"
)

// CodeSession code与session ID的映射关系
type CodeSession struct {
	ID        int64     `db:"id"`
	Code      string    `db:"code"`
	DeviceID  string    `db:"device_id"` // 设备指纹
	SessionID string    `db:"session_id"`
	UserID    int64     `db:"user_id"`
	IsBanned  bool      `db:"is_banned"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// CodeSessionModel code_session数据访问层
type CodeSessionModel struct {
	DB *sql.DB
}

// NewCodeSessionModel 创建code_session模型
func NewCodeSessionModel(db *sql.DB) *CodeSessionModel {
	return &CodeSessionModel{DB: db}
}

// GetByCode 根据code获取session映射
func (m *CodeSessionModel) GetByCode(code string) (*CodeSession, error) {
	cs := &CodeSession{}
	query := `SELECT id, code, device_id, session_id, user_id, is_banned, created_at, updated_at 
	          FROM code_sessions WHERE code = ?`
	err := m.DB.QueryRow(query, code).Scan(
		&cs.ID, &cs.Code, &cs.DeviceID, &cs.SessionID, &cs.UserID, &cs.IsBanned,
		&cs.CreatedAt, &cs.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return cs, nil
}

// GetByDeviceID 根据device_id获取session映射
func (m *CodeSessionModel) GetByDeviceID(deviceID string) (*CodeSession, error) {
	cs := &CodeSession{}
	query := `SELECT id, code, device_id, session_id, user_id, is_banned, created_at, updated_at 
	          FROM code_sessions WHERE device_id = ?`
	err := m.DB.QueryRow(query, deviceID).Scan(
		&cs.ID, &cs.Code, &cs.DeviceID, &cs.SessionID, &cs.UserID, &cs.IsBanned,
		&cs.CreatedAt, &cs.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return cs, nil
}

// GetBySessionID 根据session ID获取映射
func (m *CodeSessionModel) GetBySessionID(sessionID string) (*CodeSession, error) {
	cs := &CodeSession{}
	query := `SELECT id, code, device_id, session_id, user_id, is_banned, created_at, updated_at 
	          FROM code_sessions WHERE session_id = ?`
	err := m.DB.QueryRow(query, sessionID).Scan(
		&cs.ID, &cs.Code, &cs.DeviceID, &cs.SessionID, &cs.UserID, &cs.IsBanned,
		&cs.CreatedAt, &cs.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return cs, nil
}

// Create 创建code_session映射
func (m *CodeSessionModel) Create(cs *CodeSession) error {
	query := `INSERT INTO code_sessions (code, device_id, session_id, user_id, is_banned, created_at, updated_at) 
	          VALUES (?, ?, ?, ?, ?, NOW(), NOW())`
	result, err := m.DB.Exec(query, cs.Code, cs.DeviceID, cs.SessionID, cs.UserID, cs.IsBanned)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	cs.ID = id
	return nil
}

// BanSession 封禁session ID
func (m *CodeSessionModel) BanSession(sessionID string) error {
	query := `UPDATE code_sessions SET is_banned = 1, updated_at = NOW() WHERE session_id = ?`
	_, err := m.DB.Exec(query, sessionID)
	return err
}

// UnbanSession 解封session ID
func (m *CodeSessionModel) UnbanSession(sessionID string) error {
	query := `UPDATE code_sessions SET is_banned = 0, updated_at = NOW() WHERE session_id = ?`
	_, err := m.DB.Exec(query, sessionID)
	return err
}

// InitTable 初始化code_sessions表
func (m *CodeSessionModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS code_sessions (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	code VARCHAR(128) NOT NULL,
	device_id VARCHAR(128) DEFAULT NULL,
	session_id VARCHAR(64) NOT NULL,
	user_id BIGINT UNSIGNED NOT NULL,
	is_banned TINYINT(1) NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	UNIQUE KEY uk_code (code),
	UNIQUE KEY uk_session_id (session_id),
	INDEX idx_device_id (device_id),
	INDEX idx_user_id (user_id),
	INDEX idx_is_banned (is_banned)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}
	
	// 如果表已存在，添加device_id字段（如果不存在）
	alterQuery := `ALTER TABLE code_sessions 
	               ADD COLUMN IF NOT EXISTS device_id VARCHAR(128) DEFAULT NULL,
	               ADD INDEX IF NOT EXISTS idx_device_id (device_id)`
	// MySQL不支持IF NOT EXISTS，需要先检查
	_, _ = m.DB.Exec(alterQuery)
	return nil
}
