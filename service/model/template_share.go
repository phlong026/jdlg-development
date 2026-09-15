package model

import (
	"database/sql"
	"time"
)

// TemplateShare 模板分享记录
type TemplateShare struct {
	ID         int64     `json:"id" db:"id"`
	TemplateID int64     `json:"template_id" db:"template_id"`
	UserID     int64     `json:"user_id" db:"user_id"`
	Channel    string    `json:"channel" db:"channel"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// TemplateShareModel 模板分享数据访问层
type TemplateShareModel struct {
	DB *sql.DB
}

func NewTemplateShareModel(db *sql.DB) *TemplateShareModel {
	return &TemplateShareModel{DB: db}
}

func (m *TemplateShareModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS template_shares (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  template_id BIGINT UNSIGNED NOT NULL COMMENT '模板ID',
  user_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '分享用户ID，匿名为0',
  channel VARCHAR(64) NOT NULL DEFAULT 'miniprogram_share' COMMENT '分享渠道',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_template_id (template_id),
  KEY idx_user_id (user_id),
  KEY idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='模板分享记录';
`
	_, err := m.DB.Exec(schema)
	return err
}

func (m *TemplateShareModel) Create(record *TemplateShare) error {
	query := `INSERT INTO template_shares (template_id, user_id, channel, created_at) VALUES (?, ?, ?, NOW())`
	result, err := m.DB.Exec(query, record.TemplateID, record.UserID, record.Channel)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	record.ID = id
	return nil
}

func (m *TemplateShareModel) CountByTemplateID(templateID int64) (int64, error) {
	var count int64
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM template_shares WHERE template_id = ?`, templateID).Scan(&count)
	return count, err
}
