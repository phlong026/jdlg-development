package model

import (
	"database/sql"
	"time"
)

// TemplateLike 用户点赞模板记录
type TemplateLike struct {
	ID         int64     `json:"id" db:"id"`
	UserID     int64     `json:"user_id" db:"user_id"`
	TemplateID int64     `json:"template_id" db:"template_id"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// TemplateLikeModel 模板点赞数据访问层
type TemplateLikeModel struct {
	DB *sql.DB
}

// NewTemplateLikeModel 创建模型
func NewTemplateLikeModel(db *sql.DB) *TemplateLikeModel {
	return &TemplateLikeModel{DB: db}
}

// InitTable 初始化表
func (m *TemplateLikeModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS template_likes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
  template_id BIGINT UNSIGNED NOT NULL COMMENT '模板ID',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_user_template (user_id, template_id),
  KEY idx_user_id (user_id),
  KEY idx_template_id (template_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户点赞模板记录';
`
	_, err := m.DB.Exec(schema)
	return err
}

// HasLiked 当前用户是否已点赞该模板
func (m *TemplateLikeModel) HasLiked(userID, templateID int64) (bool, error) {
	var count int
	err := m.DB.QueryRow(
		`SELECT COUNT(1) FROM template_likes WHERE user_id = ? AND template_id = ?`,
		userID, templateID,
	).Scan(&count)
	return count > 0, err
}

// Like 点赞（插入记录）
func (m *TemplateLikeModel) Like(userID, templateID int64) error {
	_, err := m.DB.Exec(
		`INSERT INTO template_likes (user_id, template_id, created_at) VALUES (?, ?, NOW())`,
		userID, templateID,
	)
	return err
}

// Unlike 取消点赞（删除记录）
func (m *TemplateLikeModel) Unlike(userID, templateID int64) error {
	_, err := m.DB.Exec(
		`DELETE FROM template_likes WHERE user_id = ? AND template_id = ?`,
		userID, templateID,
	)
	return err
}

// GetLikedTemplateIDs 获取用户已点赞的模板 ID 列表（用于列表展示 liked 状态）
func (m *TemplateLikeModel) GetLikedTemplateIDs(userID int64) ([]int64, error) {
	if userID <= 0 {
		return nil, nil
	}
	rows, err := m.DB.Query(`SELECT template_id FROM template_likes WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}
