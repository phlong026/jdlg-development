package model

import (
	"database/sql"
	"time"
)

// BuildingCodeChatSession 建规问答会话
type BuildingCodeChatSession struct {
	ID        int64     `json:"id" db:"id"`
	UserID    int64     `json:"user_id" db:"user_id"`
	Title     string    `json:"title" db:"title"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// BuildingCodeChatMessage 建规问答消息。
// role: user / assistant
// status: ok / error / fallback（P3-1 会用 fallback 标记兜底回答）
type BuildingCodeChatMessage struct {
	ID         int64     `json:"id" db:"id"`
	SessionID  int64     `json:"session_id" db:"session_id"`
	Role       string    `json:"role" db:"role"`
	Content    string    `json:"content" db:"content"`
	QuotesJSON string    `json:"quotes_json" db:"quotes_json"`
	Status     string    `json:"status" db:"status"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

type BuildingCodeChatModel struct {
	DB *sql.DB
}

func NewBuildingCodeChatModel(db *sql.DB) *BuildingCodeChatModel {
	return &BuildingCodeChatModel{DB: db}
}

func (m *BuildingCodeChatModel) InitTable() error {
	sessionSchema := `
CREATE TABLE IF NOT EXISTS building_code_chat_sessions (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	user_id BIGINT UNSIGNED NOT NULL COMMENT '提问用户ID',
	title VARCHAR(255) NOT NULL DEFAULT '' COMMENT '会话标题，默认取首个问题前若干字',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_user_created (user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	messageSchema := `
CREATE TABLE IF NOT EXISTS building_code_chat_messages (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	session_id BIGINT UNSIGNED NOT NULL COMMENT '会话ID',
	role VARCHAR(16) NOT NULL COMMENT 'user / assistant',
	content MEDIUMTEXT NOT NULL COMMENT '消息内容',
	quotes_json MEDIUMTEXT COMMENT 'assistant 消息引用的知识库条目JSON数组',
	status VARCHAR(16) NOT NULL DEFAULT 'ok' COMMENT 'ok / error / fallback',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	INDEX idx_session_created (session_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(sessionSchema); err != nil {
		return err
	}
	_, err := m.DB.Exec(messageSchema)
	return err
}

// CreateSession 新建会话，title 超过 64 个 rune 自动截断
func (m *BuildingCodeChatModel) CreateSession(userID int64, title string) (int64, error) {
	trimmed := []rune(title)
	if len(trimmed) > 64 {
		trimmed = trimmed[:64]
	}
	res, err := m.DB.Exec(`INSERT INTO building_code_chat_sessions (user_id, title) VALUES (?, ?)`, userID, string(trimmed))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SessionBelongsToUser 返回 session_id 是否存在且归属于该用户
func (m *BuildingCodeChatModel) SessionBelongsToUser(sessionID, userID int64) (bool, error) {
	var count int
	err := m.DB.QueryRow(`SELECT COUNT(1) FROM building_code_chat_sessions WHERE id = ? AND user_id = ?`, sessionID, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// TouchSession 更新会话的 updated_at
func (m *BuildingCodeChatModel) TouchSession(sessionID int64) error {
	_, err := m.DB.Exec(`UPDATE building_code_chat_sessions SET updated_at = NOW() WHERE id = ?`, sessionID)
	return err
}

// AppendMessage 写入一条消息（支持 quotes 为空字符串）
func (m *BuildingCodeChatModel) AppendMessage(sessionID int64, role, content, quotesJSON, status string) error {
	if status == "" {
		status = "ok"
	}
	_, err := m.DB.Exec(
		`INSERT INTO building_code_chat_messages (session_id, role, content, quotes_json, status) VALUES (?, ?, ?, ?, ?)`,
		sessionID, role, content, quotesJSON, status,
	)
	return err
}
