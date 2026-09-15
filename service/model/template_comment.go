package model

import (
	"database/sql"
	"time"
)

// TemplateComment 模板评论记录
type TemplateComment struct {
	ID           int64     `json:"id" db:"id"`
	TemplateID   int64     `json:"template_id" db:"template_id"`
	UserID       int64     `json:"user_id" db:"user_id"`
	AuthorName   string    `json:"author_name" db:"author_name"`
	AuthorAvatar string    `json:"author_avatar" db:"author_avatar"`
	Content      string    `json:"content" db:"content"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// TemplateCommentModel 模板评论数据访问层
type TemplateCommentModel struct {
	DB *sql.DB
}

func NewTemplateCommentModel(db *sql.DB) *TemplateCommentModel {
	return &TemplateCommentModel{DB: db}
}

func (m *TemplateCommentModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS template_comments (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  template_id BIGINT UNSIGNED NOT NULL COMMENT '模板ID',
  user_id BIGINT UNSIGNED NOT NULL COMMENT '评论用户ID',
  author_name VARCHAR(128) NOT NULL DEFAULT '' COMMENT '评论用户名快照',
  author_avatar VARCHAR(512) NOT NULL DEFAULT '' COMMENT '评论用户头像快照',
  content VARCHAR(1000) NOT NULL COMMENT '评论内容',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_template_id (template_id),
  KEY idx_user_id (user_id),
  KEY idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='模板评论记录';
`
	_, err := m.DB.Exec(schema)
	return err
}

func (m *TemplateCommentModel) Create(comment *TemplateComment) error {
	query := `INSERT INTO template_comments (template_id, user_id, author_name, author_avatar, content, created_at)
	          VALUES (?, ?, ?, ?, ?, NOW())`
	result, err := m.DB.Exec(query, comment.TemplateID, comment.UserID, comment.AuthorName, comment.AuthorAvatar, comment.Content)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	comment.ID = id
	return nil
}

func (m *TemplateCommentModel) CountByTemplateID(templateID int64) (int64, error) {
	var count int64
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM template_comments WHERE template_id = ?`, templateID).Scan(&count)
	return count, err
}

func (m *TemplateCommentModel) ListByTemplateID(templateID int64, limit, offset int) ([]*TemplateComment, error) {
	rows, err := m.DB.Query(
		`SELECT id, template_id, user_id, author_name, author_avatar, content, created_at
		 FROM template_comments
		 WHERE template_id = ?
		 ORDER BY id DESC
		 LIMIT ? OFFSET ?`,
		templateID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]*TemplateComment, 0)
	for rows.Next() {
		item := &TemplateComment{}
		if err := rows.Scan(&item.ID, &item.TemplateID, &item.UserID, &item.AuthorName, &item.AuthorAvatar, &item.Content, &item.CreatedAt); err != nil {
			continue
		}
		list = append(list, item)
	}
	return list, nil
}
