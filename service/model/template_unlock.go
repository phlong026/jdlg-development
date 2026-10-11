package model

import "database/sql"

// TemplateUnlockModel 模板解锁记录（用户解锁付费模板后，查看提示词/用提示词生成/下载均不再扣费）
type TemplateUnlockModel struct {
	DB *sql.DB
}

// NewTemplateUnlockModel 创建模型
func NewTemplateUnlockModel(db *sql.DB) *TemplateUnlockModel {
	return &TemplateUnlockModel{DB: db}
}

// InitTable 创建 template_unlocks 表
func (m *TemplateUnlockModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS template_unlocks (
	user_id BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
	template_id BIGINT UNSIGNED NOT NULL COMMENT '模板ID',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, template_id),
	KEY idx_template_id (template_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='模板解锁记录';
`
	_, err := m.DB.Exec(schema)
	return err
}

// Create 记录解锁
func (m *TemplateUnlockModel) Create(userID, templateID int64) error {
	_, err := m.DB.Exec(
		`INSERT IGNORE INTO template_unlocks (user_id, template_id) VALUES (?, ?)`,
		userID, templateID,
	)
	return err
}

// HasUnlocked 是否已解锁
func (m *TemplateUnlockModel) HasUnlocked(userID, templateID int64) (bool, error) {
	var count int
	err := m.DB.QueryRow(
		`SELECT COUNT(*) FROM template_unlocks WHERE user_id = ? AND template_id = ?`,
		userID, templateID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
