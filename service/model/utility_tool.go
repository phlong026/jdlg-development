package model

import (
	"database/sql"
	"strings"
	"time"
)

// UtilityTool 实用工具内容结构
type UtilityTool struct {
	ID          int64     `json:"id"`
	Category    string    `json:"category"`     // local_norm(本地规范), faq(FAQ), video_tutorial(视频教程)
	Title       string    `json:"title"`         // 标题
	Content     string    `json:"content"`       // 内容（支持Markdown或HTML）
	CoverImage  string    `json:"cover_image"`   // 封面图URL（视频教程用）
	VideoURL    string    `json:"video_url"`     // 视频URL（视频教程用）
	FileURL     string    `json:"file_url"`      // 文件URL（本地规范用，如PDF）
	SortOrder   int       `json:"sort_order"`    // 排序顺序
	IsPublished bool      `json:"is_published"`  // 是否发布
	ViewCount   int64     `json:"view_count"`    // 查看次数
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UtilityToolModel 实用工具内容模型
type UtilityToolModel struct {
	DB *sql.DB
}

// NewUtilityToolModel 创建实用工具内容模型
func NewUtilityToolModel(db *sql.DB) *UtilityToolModel {
	return &UtilityToolModel{DB: db}
}

// InitTable 初始化utility_tools表
func (m *UtilityToolModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS utility_tools (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	category VARCHAR(32) NOT NULL COMMENT '分类：local_norm(本地规范), faq(FAQ), video_tutorial(视频教程)',
	title VARCHAR(255) NOT NULL COMMENT '标题',
	content TEXT COMMENT '内容（支持Markdown或HTML）',
	cover_image VARCHAR(512) COMMENT '封面图URL',
	video_url VARCHAR(512) COMMENT '视频URL',
	file_url VARCHAR(512) COMMENT '文件URL（如PDF）',
	sort_order INT NOT NULL DEFAULT 0 COMMENT '排序顺序',
	is_published TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否发布：0-否，1-是',
	view_count BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '查看次数',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_category (category),
	INDEX idx_is_published (is_published),
	INDEX idx_sort_order (sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	return nil
}

// Create 创建实用工具内容
func (m *UtilityToolModel) Create(tool *UtilityTool) error {
	query := `INSERT INTO utility_tools (category, title, content, cover_image, video_url, file_url, sort_order, is_published)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	isPublishedInt := 0
	if tool.IsPublished {
		isPublishedInt = 1
	}
	result, err := m.DB.Exec(query, tool.Category, tool.Title, tool.Content, tool.CoverImage,
		tool.VideoURL, tool.FileURL, tool.SortOrder, isPublishedInt)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	tool.ID = id
	return nil
}

// GetByID 根据ID获取实用工具内容
func (m *UtilityToolModel) GetByID(id int64) (*UtilityTool, error) {
	query := `SELECT id, category, title, content, cover_image, video_url, file_url, sort_order, is_published, view_count, created_at, updated_at
	          FROM utility_tools WHERE id = ?`
	tool := &UtilityTool{}
	var isPublishedInt int
	err := m.DB.QueryRow(query, id).Scan(
		&tool.ID, &tool.Category, &tool.Title, &tool.Content, &tool.CoverImage,
		&tool.VideoURL, &tool.FileURL, &tool.SortOrder, &isPublishedInt, &tool.ViewCount,
		&tool.CreatedAt, &tool.UpdatedAt)
	if err != nil {
		return nil, err
	}
	tool.IsPublished = isPublishedInt == 1
	return tool, nil
}

// List 获取实用工具内容列表
func (m *UtilityToolModel) List(category, keyword string, isPublished *bool, limit int, offset int) ([]*UtilityTool, error) {
	query := `SELECT id, category, title, content, cover_image, video_url, file_url, sort_order, is_published, view_count, created_at, updated_at
	          FROM utility_tools WHERE 1=1`
	args := []interface{}{}

	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	trimmedKeyword := strings.TrimSpace(keyword)
	if trimmedKeyword != "" {
		likeKeyword := "%" + trimmedKeyword + "%"
		query += " AND (title LIKE ? OR content LIKE ? OR cover_image LIKE ? OR video_url LIKE ? OR file_url LIKE ?)"
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}
	if isPublished != nil {
		if *isPublished {
			query += " AND is_published = 1"
		} else {
			query += " AND is_published = 0"
		}
	}

	query += " ORDER BY sort_order ASC, created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tools []*UtilityTool
	for rows.Next() {
		tool := &UtilityTool{}
		var isPublishedInt int
		err := rows.Scan(
			&tool.ID, &tool.Category, &tool.Title, &tool.Content, &tool.CoverImage,
			&tool.VideoURL, &tool.FileURL, &tool.SortOrder, &isPublishedInt, &tool.ViewCount,
			&tool.CreatedAt, &tool.UpdatedAt)
		if err != nil {
			continue
		}
		tool.IsPublished = isPublishedInt == 1
		tools = append(tools, tool)
	}
	return tools, nil
}

// Update 更新实用工具内容
func (m *UtilityToolModel) Update(tool *UtilityTool) error {
	query := `UPDATE utility_tools SET category = ?, title = ?, content = ?, cover_image = ?,
	          video_url = ?, file_url = ?, sort_order = ?, is_published = ?
	          WHERE id = ?`
	isPublishedInt := 0
	if tool.IsPublished {
		isPublishedInt = 1
	}
	_, err := m.DB.Exec(query, tool.Category, tool.Title, tool.Content, tool.CoverImage,
		tool.VideoURL, tool.FileURL, tool.SortOrder, isPublishedInt, tool.ID)
	return err
}

// Delete 删除实用工具内容
func (m *UtilityToolModel) Delete(id int64) error {
	query := "DELETE FROM utility_tools WHERE id = ?"
	_, err := m.DB.Exec(query, id)
	return err
}

// IncrementViewCount 增加查看次数
func (m *UtilityToolModel) IncrementViewCount(id int64) error {
	query := "UPDATE utility_tools SET view_count = view_count + 1 WHERE id = ?"
	_, err := m.DB.Exec(query, id)
	return err
}

// Count 统计实用工具内容数量
func (m *UtilityToolModel) Count(category, keyword string, isPublished *bool) (int64, error) {
	query := "SELECT COUNT(*) FROM utility_tools WHERE 1=1"
	args := []interface{}{}

	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	trimmedKeyword := strings.TrimSpace(keyword)
	if trimmedKeyword != "" {
		likeKeyword := "%" + trimmedKeyword + "%"
		query += " AND (title LIKE ? OR content LIKE ? OR cover_image LIKE ? OR video_url LIKE ? OR file_url LIKE ?)"
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}
	if isPublished != nil {
		if *isPublished {
			query += " AND is_published = 1"
		} else {
			query += " AND is_published = 0"
		}
	}

	var count int64
	err := m.DB.QueryRow(query, args...).Scan(&count)
	return count, err
}
