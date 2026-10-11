package model

import (
	"database/sql"
	"strings"
	"time"
)

// Template 模板结构
type Template struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Category       string    `json:"category"`  // villa, urban, family, culture, etc.
	MainTab        string    `json:"main_tab"`  // 一级tab value，可为空
	SubTab         string    `json:"sub_tab"`   // 二级tab value，可为空（如果设置了main_tab但sub_tab为空，表示属于父tab）
	ThirdTab       string    `json:"third_tab"` // 三级tab value，可为空（如果设置了sub_tab但third_tab为空，表示属于二级tab）
	Description    string    `json:"description"`
	InternalPrompt string    `json:"-"`
	ImageWidth     int       `json:"image_width"`
	ImageHeight    int       `json:"image_height"`
	Thumbnail      string    `json:"thumbnail"`      // 缩略图URL
	PreviewURL     string    `json:"preview_url"`    // 预览图URL
	Images         string    `json:"images"`         // 多张图片，JSON数组格式
	Price          int64     `json:"price"`          // 价格（灵石）
	IsFree         bool      `json:"is_free"`        // 是否免费
	IsFeatured     bool      `json:"is_featured"`    // 是否为精选案例
	DownloadCount  int64     `json:"download_count"` // 下载次数
	LikeCount      int64     `json:"like_count"`     // 点赞数
	Status         string    `json:"status"`         // published, draft, archived, pending, rejected
	PublishScope   string    `json:"publish_scope"`
	RejectReason   string    `json:"reject_reason"`
	SourceType     string    `json:"source_type"`
	Creator        string    `json:"creator"`          // 创建者展示名
	CreatorUserID  int64     `json:"creator_user_id"`  // 小程序用户提交时的 user_id，用于「我的方案」
	OriginalTaskID int64     `json:"original_task_id"` // 原始AI任务ID，用于获取提示词和参考图
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

const LegacyTemplatePromptPrefix = "\n\n提示词: "

func SplitTemplateDescriptionPrompt(description string) (publicDescription, prompt string) {
	if strings.TrimSpace(description) == "" {
		return "", ""
	}
	index := strings.Index(description, LegacyTemplatePromptPrefix)
	if index < 0 {
		return strings.TrimSpace(description), ""
	}
	return strings.TrimSpace(description[:index]), strings.TrimSpace(description[index+len(LegacyTemplatePromptPrefix):])
}

func normalizeTemplateRecord(template *Template) {
	if template == nil {
		return
	}
	template.Description = strings.TrimSpace(template.Description)
	template.InternalPrompt = strings.TrimSpace(template.InternalPrompt)
	if template.InternalPrompt != "" {
		return
	}
	publicDescription, prompt := SplitTemplateDescriptionPrompt(template.Description)
	if prompt == "" {
		return
	}
	template.Description = publicDescription
	template.InternalPrompt = prompt
}

// TemplateModel 模板模型
type TemplateModel struct {
	DB *sql.DB
}

// NewTemplateModel 创建模板模型
func NewTemplateModel(db *sql.DB) *TemplateModel {
	return &TemplateModel{DB: db}
}

// InitTable 初始化templates表
func (m *TemplateModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS templates (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255) NOT NULL COMMENT '模板名称',
	category VARCHAR(64) NOT NULL COMMENT '分类：villa（乡村别墅）、urban（城市焕新）、family（亲子）、culture（文创）等',
	main_tab VARCHAR(64) DEFAULT '' COMMENT '一级tab value，可为空',
	sub_tab VARCHAR(64) DEFAULT '' COMMENT '二级tab value，可为空（如果设置了main_tab但sub_tab为空，表示属于父tab）',
	third_tab VARCHAR(64) DEFAULT '' COMMENT '三级tab value，可为空（如果设置了sub_tab但third_tab为空，表示属于二级tab）',
	description TEXT COMMENT '模板描述',
	internal_prompt LONGTEXT COMMENT '模板内部提示词（不对前端公开）',
	thumbnail VARCHAR(512) COMMENT '缩略图URL',
	preview_url VARCHAR(512) COMMENT '预览图URL',
	images TEXT COMMENT '多张图片，JSON数组格式',
	price BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '价格（灵石）',
	is_free TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否免费：0-否，1-是',
	download_count BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '下载次数',
	like_count BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '点赞数',
	status VARCHAR(32) NOT NULL DEFAULT 'draft' COMMENT '状态：published（已发布）、draft（草稿）、archived（已归档）',
	publish_scope VARCHAR(32) NOT NULL DEFAULT 'square' COMMENT '发布去向：square（模板广场+主页）、homepage_only（仅主页）',
	reject_reason VARCHAR(500) NOT NULL DEFAULT '' COMMENT '审核拒绝原因',
	source_type VARCHAR(32) NOT NULL DEFAULT 'admin_upload' COMMENT '来源类型：admin_upload、ai_generated、album_upload',
	creator VARCHAR(128) NOT NULL COMMENT '创建者',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_category (category),
	INDEX idx_status (status),
	INDEX idx_publish_scope (publish_scope),
	INDEX idx_main_tab (main_tab),
	INDEX idx_sub_tab (sub_tab),
	INDEX idx_third_tab (third_tab),
	INDEX idx_created_at (created_at),
	INDEX idx_download_count (download_count),
	INDEX idx_like_count (like_count)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	// 兼容旧表：增加 creator_user_id 用于「我的方案」归属
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN creator_user_id BIGINT NULL DEFAULT NULL COMMENT '小程序用户ID，用于我的方案'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD INDEX idx_creator_user_id (creator_user_id)`)
	// 兼容旧表：增加 is_featured 用于「首页精选案例」
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN is_featured TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否为精选案例：0-否，1-是'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD INDEX idx_is_featured (is_featured)`)
	// 兼容旧表：增加 main_tab 和 sub_tab 用于「模板广场双重Tab配置」
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN main_tab VARCHAR(64) DEFAULT '' COMMENT '一级tab value，可为空'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN sub_tab VARCHAR(64) DEFAULT '' COMMENT '二级tab value，可为空'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN third_tab VARCHAR(64) DEFAULT '' COMMENT '三级tab value，可为空'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN internal_prompt LONGTEXT COMMENT '模板内部提示词（不对前端公开）'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN image_width INT NOT NULL DEFAULT 0 COMMENT 'image width'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN image_height INT NOT NULL DEFAULT 0 COMMENT 'image height'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD INDEX idx_main_tab (main_tab)`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD INDEX idx_sub_tab (sub_tab)`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD INDEX idx_third_tab (third_tab)`)
	// 兼容旧表：增加 original_task_id 用于「获取原始任务的提示词和参考图」
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN original_task_id BIGINT NULL DEFAULT NULL COMMENT '原始AI任务ID，用于获取提示词和参考图'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD INDEX idx_original_task_id (original_task_id)`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN publish_scope VARCHAR(32) NOT NULL DEFAULT 'square' COMMENT '发布去向：square（模板广场+主页）、homepage_only（仅主页）'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN reject_reason VARCHAR(500) NOT NULL DEFAULT '' COMMENT '审核拒绝原因'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD COLUMN source_type VARCHAR(32) NOT NULL DEFAULT 'admin_upload' COMMENT '来源类型：admin_upload、ai_generated、album_upload'`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD INDEX idx_publish_scope (publish_scope)`)
	_, _ = m.DB.Exec(`ALTER TABLE templates ADD INDEX idx_source_type (source_type)`)
	return nil
}

// Create 创建模板
func (m *TemplateModel) Create(template *Template) error {
	publishScope := strings.TrimSpace(template.PublishScope)
	if publishScope == "" {
		publishScope = "square"
	}
	sourceType := strings.TrimSpace(template.SourceType)
	if sourceType == "" {
		if template.CreatorUserID > 0 {
			sourceType = "ai_generated"
		} else {
			sourceType = "admin_upload"
		}
	}
	rejectReason := strings.TrimSpace(template.RejectReason)
	populateTemplateImageMetadata(template)
	query := `INSERT INTO templates (name, category, main_tab, sub_tab, third_tab, description, internal_prompt, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured, status, publish_scope, reject_reason, source_type, creator, creator_user_id, original_task_id)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	isFreeInt := 0
	if template.IsFree {
		isFreeInt = 1
	}
	isFeaturedInt := 0
	if template.IsFeatured {
		isFeaturedInt = 1
	}
	result, err := m.DB.Exec(query, template.Name, template.Category, template.MainTab, template.SubTab, template.ThirdTab, strings.TrimSpace(template.Description), strings.TrimSpace(template.InternalPrompt),
		template.Thumbnail, template.PreviewURL, template.Images, template.ImageWidth, template.ImageHeight, template.Price,
		isFreeInt, isFeaturedInt, template.Status, publishScope, rejectReason, sourceType, template.Creator, nullableInt64(template.CreatorUserID), nullableInt64(template.OriginalTaskID))
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	template.ID = id
	template.PublishScope = publishScope
	template.RejectReason = rejectReason
	template.SourceType = sourceType
	return nil
}

func nullableInt64(v int64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

// GetByID 根据ID获取模板
func (m *TemplateModel) GetByID(id int64) (*Template, error) {
	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, internal_prompt, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, original_task_id, created_at, updated_at
	          FROM templates WHERE id = ?`
	template := &Template{}
	var isFreeInt, isFeaturedInt int
	var creatorUserID, originalTaskID sql.NullInt64
	var internalPrompt sql.NullString
	err := m.DB.QueryRow(query, id).Scan(
		&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description, &internalPrompt,
		&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
		&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
		&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &creatorUserID, &originalTaskID, &template.CreatedAt, &template.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if internalPrompt.Valid {
		template.InternalPrompt = internalPrompt.String
	}
	if creatorUserID.Valid {
		template.CreatorUserID = creatorUserID.Int64
	}
	if originalTaskID.Valid {
		template.OriginalTaskID = originalTaskID.Int64
	}
	template.IsFree = isFreeInt == 1
	template.IsFeatured = isFeaturedInt == 1
	normalizeTemplateRecord(template)
	return template, nil
}

// List 获取模板列表
func (m *TemplateModel) List(category string, status string, limit int, offset int) ([]*Template, error) {
	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, created_at, updated_at
	          FROM templates WHERE 1=1`
	args := []interface{}{}

	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []*Template
	for rows.Next() {
		template := &Template{}
		var isFreeInt, isFeaturedInt int
		var creatorUserID sql.NullInt64
		err := rows.Scan(
			&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description,
			&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
			&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
			&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &creatorUserID, &template.CreatedAt, &template.UpdatedAt)
		if err != nil {
			continue
		}
		if creatorUserID.Valid {
			template.CreatorUserID = creatorUserID.Int64
		}
		template.IsFree = isFreeInt == 1
		template.IsFeatured = isFeaturedInt == 1
		normalizeTemplateRecord(template)
		templates = append(templates, template)
	}
	return templates, nil
}

func (m *TemplateModel) ListPublicByCategory(category string, limit int, offset int) ([]*Template, error) {
	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, created_at, updated_at
	          FROM templates WHERE status = 'published' AND publish_scope = 'square'`
	args := []interface{}{}
	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var templates []*Template
	for rows.Next() {
		template := &Template{}
		var isFreeInt, isFeaturedInt int
		var creatorUserID sql.NullInt64
		err := rows.Scan(
			&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description,
			&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
			&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
			&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &creatorUserID, &template.CreatedAt, &template.UpdatedAt)
		if err != nil {
			continue
		}
		if creatorUserID.Valid {
			template.CreatorUserID = creatorUserID.Int64
		}
		template.IsFree = isFreeInt == 1
		template.IsFeatured = isFeaturedInt == 1
		normalizeTemplateRecord(template)
		templates = append(templates, template)
	}
	return templates, nil
}

func (m *TemplateModel) CountPublicByCategory(category string) (int64, error) {
	query := "SELECT COUNT(*) FROM templates WHERE status = 'published' AND publish_scope = 'square'"
	args := []interface{}{}
	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	var count int64
	err := m.DB.QueryRow(query, args...).Scan(&count)
	return count, err
}

// Count 统计模板数量
func (m *TemplateModel) Count(category string, status string) (int64, error) {
	query := "SELECT COUNT(*) FROM templates WHERE 1=1"
	args := []interface{}{}

	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	var count int64
	err := m.DB.QueryRow(query, args...).Scan(&count)
	return count, err
}

// ListByCreatorUserID 获取某用户发布的模板列表（我的方案）
func (m *TemplateModel) ListByCreatorUserID(creatorUserID int64, category string, limit int, offset int) ([]*Template, error) {
	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, created_at, updated_at
	          FROM templates WHERE creator_user_id = ?`
	args := []interface{}{creatorUserID}
	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []*Template
	for rows.Next() {
		template := &Template{}
		var isFreeInt, isFeaturedInt int
		var cuid sql.NullInt64
		err := rows.Scan(
			&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description,
			&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
			&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
			&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &cuid, &template.CreatedAt, &template.UpdatedAt)
		if err != nil {
			continue
		}
		if cuid.Valid {
			template.CreatorUserID = cuid.Int64
		}
		template.IsFree = isFreeInt == 1
		template.IsFeatured = isFeaturedInt == 1
		normalizeTemplateRecord(template)
		templates = append(templates, template)
	}
	return templates, nil
}

// CountByCreatorUserID 统计某用户发布的模板数量
func (m *TemplateModel) CountByCreatorUserID(creatorUserID int64, category string) (int64, error) {
	query := "SELECT COUNT(*) FROM templates WHERE creator_user_id = ?"
	args := []interface{}{creatorUserID}
	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	var count int64
	err := m.DB.QueryRow(query, args...).Scan(&count)
	return count, err
}

func (m *TemplateModel) SummaryByCreatorUserID(creatorUserID int64) (int64, int64, int64, error) {
	var totalPlans int64
	var totalViews int64
	var totalLikes int64
	err := m.DB.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(download_count), 0), COALESCE(SUM(like_count), 0) FROM templates WHERE creator_user_id = ?`,
		creatorUserID,
	).Scan(&totalPlans, &totalViews, &totalLikes)
	if err != nil {
		return 0, 0, 0, err
	}
	return totalPlans, totalViews, totalLikes, nil
}

func (m *TemplateModel) ListPublishedByCreatorUserID(creatorUserID int64, category string, limit int, offset int) ([]*Template, error) {
	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, created_at, updated_at
	          FROM templates WHERE creator_user_id = ? AND status = 'published'`
	args := []interface{}{creatorUserID}
	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []*Template
	for rows.Next() {
		template := &Template{}
		var isFreeInt, isFeaturedInt int
		var cuid sql.NullInt64
		err := rows.Scan(
			&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description,
			&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
			&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
			&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &cuid, &template.CreatedAt, &template.UpdatedAt)
		if err != nil {
			continue
		}
		if cuid.Valid {
			template.CreatorUserID = cuid.Int64
		}
		template.IsFree = isFreeInt == 1
		template.IsFeatured = isFeaturedInt == 1
		normalizeTemplateRecord(template)
		templates = append(templates, template)
	}
	return templates, nil
}

func (m *TemplateModel) CountPublishedByCreatorUserID(creatorUserID int64, category string) (int64, error) {
	query := "SELECT COUNT(*) FROM templates WHERE creator_user_id = ? AND status = 'published'"
	args := []interface{}{creatorUserID}
	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	var count int64
	err := m.DB.QueryRow(query, args...).Scan(&count)
	return count, err
}

func (m *TemplateModel) Update(template *Template) error {
	publishScope := strings.TrimSpace(template.PublishScope)
	if publishScope == "" {
		publishScope = "square"
	}
	rejectReason := strings.TrimSpace(template.RejectReason)
	sourceType := strings.TrimSpace(template.SourceType)
	if sourceType == "" {
		if template.CreatorUserID > 0 {
			sourceType = "ai_generated"
		} else {
			sourceType = "admin_upload"
		}
	}
	isFreeInt := 0
	if template.IsFree {
		isFreeInt = 1
	}
	isFeaturedInt := 0
	if template.IsFeatured {
		isFeaturedInt = 1
	}
	populateTemplateImageMetadata(template)
	query := `UPDATE templates
		SET name = ?, category = ?, main_tab = ?, sub_tab = ?, third_tab = ?, description = ?, internal_prompt = ?, thumbnail = ?, preview_url = ?, images = ?, image_width = ?, image_height = ?,
		    price = ?, is_free = ?, is_featured = ?, status = ?, publish_scope = ?, reject_reason = ?, source_type = ?,
		    creator = ?, creator_user_id = ?, original_task_id = ?
		WHERE id = ?`
	_, err := m.DB.Exec(
		query,
		template.Name,
		template.Category,
		template.MainTab,
		template.SubTab,
		template.ThirdTab,
		strings.TrimSpace(template.Description),
		strings.TrimSpace(template.InternalPrompt),
		template.Thumbnail,
		template.PreviewURL,
		template.Images,
		template.ImageWidth,
		template.ImageHeight,
		template.Price,
		isFreeInt,
		isFeaturedInt,
		template.Status,
		publishScope,
		rejectReason,
		sourceType,
		template.Creator,
		nullableInt64(template.CreatorUserID),
		nullableInt64(template.OriginalTaskID),
		template.ID,
	)
	if err != nil {
		return err
	}
	template.PublishScope = publishScope
	template.RejectReason = rejectReason
	template.SourceType = sourceType
	return nil
}

func (m *TemplateModel) Delete(id int64) error {
	_, err := m.DB.Exec(`DELETE FROM templates WHERE id = ?`, id)
	return err
}

func (m *TemplateModel) BatchDeleteByCreatorUserID(creatorUserID int64, ids []int64) (int64, error) {
	if creatorUserID <= 0 || len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, 0, len(ids))
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, creatorUserID)
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	result, err := m.DB.Exec(
		`DELETE FROM templates WHERE creator_user_id = ? AND id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (m *TemplateModel) IncrementDownloadCount(id int64) error {
	_, err := m.DB.Exec(`UPDATE templates SET download_count = download_count + 1 WHERE id = ?`, id)
	return err
}

func (m *TemplateModel) IncrementLikeCount(id int64) error {
	_, err := m.DB.Exec(`UPDATE templates SET like_count = like_count + 1 WHERE id = ?`, id)
	return err
}

func (m *TemplateModel) DecrementLikeCount(id int64) error {
	_, err := m.DB.Exec(`UPDATE templates SET like_count = CASE WHEN like_count > 0 THEN like_count - 1 ELSE 0 END WHERE id = ?`, id)
	return err
}

// SearchPublishedTemplates 按关键字搜索已发布模板（名称或描述包含关键词）
func (m *TemplateModel) SearchPublishedTemplates(keyword string, limit int, offset int) ([]*Template, int64, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return []*Template{}, 0, nil
	}

	like := "%" + kw + "%"

	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, created_at, updated_at
	          FROM templates
	          WHERE status = 'published' AND publish_scope = 'square' AND (name LIKE ? OR description LIKE ?)
	          ORDER BY download_count DESC, created_at DESC
	          LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, like, like, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var templates []*Template
	for rows.Next() {
		template := &Template{}
		var isFreeInt, isFeaturedInt int
		var creatorUserID sql.NullInt64
		if err := rows.Scan(
			&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description,
			&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
			&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
			&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &creatorUserID, &template.CreatedAt, &template.UpdatedAt,
		); err != nil {
			continue
		}
		if creatorUserID.Valid {
			template.CreatorUserID = creatorUserID.Int64
		}
		template.IsFree = isFreeInt == 1
		template.IsFeatured = isFeaturedInt == 1
		normalizeTemplateRecord(template)
		templates = append(templates, template)
	}

	countQuery := `SELECT COUNT(*) FROM templates WHERE status = 'published' AND publish_scope = 'square' AND (name LIKE ? OR description LIKE ?)`
	var total int64
	if err := m.DB.QueryRow(countQuery, like, like).Scan(&total); err != nil {
		return templates, 0, err
	}

	return templates, total, nil
}

// GetHotTemplates 获取热门模板（按下载量或点赞数）
func (m *TemplateModel) GetHotTemplates(limit int) ([]*Template, error) {
	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, created_at, updated_at
	          FROM templates WHERE status = 'published' AND publish_scope = 'square'
	          ORDER BY (download_count + like_count * 2) DESC, created_at DESC
	          LIMIT ?`
	rows, err := m.DB.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []*Template
	for rows.Next() {
		template := &Template{}
		var isFreeInt, isFeaturedInt int
		var creatorUserID sql.NullInt64
		err := rows.Scan(
			&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description,
			&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
			&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
			&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &creatorUserID, &template.CreatedAt, &template.UpdatedAt)
		if err != nil {
			continue
		}
		if creatorUserID.Valid {
			template.CreatorUserID = creatorUserID.Int64
		}
		template.IsFree = isFreeInt == 1
		template.IsFeatured = isFeaturedInt == 1
		normalizeTemplateRecord(template)
		templates = append(templates, template)
	}
	return templates, nil
}

// ListByMainTabAndSubTab 根据 main_tab / sub_tab / third_tab 获取模板列表，按点赞数排序
// 二级、三级标签筛选采用精确匹配，确保列表结果与数据库标签值一致。
func (m *TemplateModel) ListByMainTabAndSubTab(mainTab string, subTab string, thirdTab string, status string, limit int, offset int) ([]*Template, error) {
	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, created_at, updated_at
	          FROM templates WHERE 1=1`
	args := []interface{}{}

	if mainTab != "" {
		query += " AND main_tab = ?"
		args = append(args, mainTab)
	}
	if subTab != "" {
		query += " AND (sub_tab = ? OR TRIM(COALESCE(sub_tab, '')) = '')"
		args = append(args, subTab)
	}
	if thirdTab != "" {
		query += " AND (third_tab = ? OR TRIM(COALESCE(third_tab, '')) = '')"
		args = append(args, thirdTab)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " AND publish_scope = 'square'"

	query += " ORDER BY like_count DESC, created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []*Template
	for rows.Next() {
		template := &Template{}
		var isFreeInt, isFeaturedInt int
		var creatorUserID sql.NullInt64
		err := rows.Scan(
			&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description,
			&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
			&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
			&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &creatorUserID, &template.CreatedAt, &template.UpdatedAt)
		if err != nil {
			continue
		}
		if creatorUserID.Valid {
			template.CreatorUserID = creatorUserID.Int64
		}
		template.IsFree = isFreeInt == 1
		template.IsFeatured = isFeaturedInt == 1
		normalizeTemplateRecord(template)
		templates = append(templates, template)
	}
	return templates, nil
}

// CountByMainTabAndSubTab 统计按 main_tab / sub_tab / third_tab 筛选的模板数量
// 与 ListByMainTabAndSubTab 保持一致的精确匹配逻辑。
func (m *TemplateModel) CountByMainTabAndSubTab(mainTab string, subTab string, thirdTab string, status string) (int64, error) {
	query := "SELECT COUNT(*) FROM templates WHERE 1=1"
	args := []interface{}{}

	if mainTab != "" {
		query += " AND main_tab = ?"
		args = append(args, mainTab)
	}
	if subTab != "" {
		query += " AND (sub_tab = ? OR TRIM(COALESCE(sub_tab, '')) = '')"
		args = append(args, subTab)
	}
	if thirdTab != "" {
		query += " AND (third_tab = ? OR TRIM(COALESCE(third_tab, '')) = '')"
		args = append(args, thirdTab)
	}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " AND publish_scope = 'square'"

	var count int64
	err := m.DB.QueryRow(query, args...).Scan(&count)
	return count, err
}

// GetLatestTemplates 获取最新模板
func (m *TemplateModel) GetLatestTemplates(limit int) ([]*Template, error) {
	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, created_at, updated_at
	          FROM templates WHERE status = 'published' AND publish_scope = 'square'
	          ORDER BY created_at DESC
	          LIMIT ?`
	rows, err := m.DB.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []*Template
	for rows.Next() {
		template := &Template{}
		var isFreeInt, isFeaturedInt int
		var creatorUserID sql.NullInt64
		err := rows.Scan(
			&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description,
			&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
			&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
			&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &creatorUserID, &template.CreatedAt, &template.UpdatedAt)
		if err != nil {
			continue
		}
		if creatorUserID.Valid {
			template.CreatorUserID = creatorUserID.Int64
		}
		template.IsFree = isFreeInt == 1
		template.IsFeatured = isFeaturedInt == 1
		normalizeTemplateRecord(template)
		templates = append(templates, template)
	}
	return templates, nil
}

// GetFeaturedTemplates 获取精选案例列表
func (m *TemplateModel) GetFeaturedTemplates(limit int) ([]*Template, error) {
	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, created_at, updated_at
	          FROM templates WHERE status = 'published' AND publish_scope = 'square' AND is_featured = 1
	          ORDER BY created_at DESC
	          LIMIT ?`
	rows, err := m.DB.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []*Template
	for rows.Next() {
		template := &Template{}
		var isFreeInt, isFeaturedInt int
		var creatorUserID sql.NullInt64
		err := rows.Scan(
			&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description,
			&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
			&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
			&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &creatorUserID, &template.CreatedAt, &template.UpdatedAt)
		if err != nil {
			continue
		}
		if creatorUserID.Valid {
			template.CreatorUserID = creatorUserID.Int64
		}
		template.IsFree = isFreeInt == 1
		template.IsFeatured = isFeaturedInt == 1
		templates = append(templates, template)
	}
	return templates, nil
}

// SetFeatured 设置模板为精选案例
func (m *TemplateModel) SetFeatured(id int64, isFeatured bool) error {
	isFeaturedInt := 0
	if isFeatured {
		isFeaturedInt = 1
	}
	query := "UPDATE templates SET is_featured = ? WHERE id = ?"
	_, err := m.DB.Exec(query, isFeaturedInt, id)
	return err
}

func (m *TemplateModel) ListWithoutImageMetadata(limit int, offset int) ([]*Template, error) {
	query := `SELECT id, name, category, main_tab, sub_tab, third_tab, description, thumbnail, preview_url, images, image_width, image_height, price, is_free, is_featured,
	          download_count, like_count, status, publish_scope, reject_reason, source_type, creator, creator_user_id, created_at, updated_at
	          FROM templates
	          WHERE (image_width <= 0 OR image_height <= 0)
	            AND (
	              TRIM(COALESCE(thumbnail, '')) <> ''
	              OR TRIM(COALESCE(preview_url, '')) <> ''
	              OR TRIM(COALESCE(images, '')) <> ''
	            )
	          ORDER BY id ASC
	          LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	templates := make([]*Template, 0)
	for rows.Next() {
		template := &Template{}
		var isFreeInt, isFeaturedInt int
		var creatorUserID sql.NullInt64
		if err := rows.Scan(
			&template.ID, &template.Name, &template.Category, &template.MainTab, &template.SubTab, &template.ThirdTab, &template.Description,
			&template.Thumbnail, &template.PreviewURL, &template.Images, &template.ImageWidth, &template.ImageHeight,
			&template.Price, &isFreeInt, &isFeaturedInt, &template.DownloadCount, &template.LikeCount,
			&template.Status, &template.PublishScope, &template.RejectReason, &template.SourceType, &template.Creator, &creatorUserID, &template.CreatedAt, &template.UpdatedAt,
		); err != nil {
			continue
		}
		if creatorUserID.Valid {
			template.CreatorUserID = creatorUserID.Int64
		}
		template.IsFree = isFreeInt == 1
		template.IsFeatured = isFeaturedInt == 1
		normalizeTemplateRecord(template)
		templates = append(templates, template)
	}
	return templates, nil
}

func (m *TemplateModel) UpdateImageMetadataByID(id int64, width int, height int) error {
	_, err := m.DB.Exec(`UPDATE templates SET image_width = ?, image_height = ? WHERE id = ?`, width, height, id)
	return err
}

func (m *TemplateModel) UpdateImageAssetsByID(id int64, thumbnail string, previewURL string, images string) error {
	_, err := m.DB.Exec(`UPDATE templates SET thumbnail = ?, preview_url = ?, images = ? WHERE id = ?`, strings.TrimSpace(thumbnail), strings.TrimSpace(previewURL), strings.TrimSpace(images), id)
	return err
}
