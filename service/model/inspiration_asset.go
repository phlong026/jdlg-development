package model

import (
	"database/sql"
	"strings"
	"time"
)

type InspirationAsset struct {
	ID            int64     `json:"id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	CoverImage    string    `json:"cover_image"`
	Images        string    `json:"images"`
	ImageWidth    int       `json:"image_width"`
	ImageHeight   int       `json:"image_height"`
	Tags          string    `json:"tags"`
	Scene         string    `json:"scene"`
	Style         string    `json:"style"`
	Topic         string    `json:"topic"`
	SortOrder     int       `json:"sort_order"`
	Status        string    `json:"status"`
	Creator       string    `json:"creator"`
	CreatorUserID int64     `json:"creator_user_id"`
	ViewCount     int64     `json:"view_count"`
	LikeCount     int64     `json:"like_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type InspirationAssetModel struct {
	DB *sql.DB
}

func NewInspirationAssetModel(db *sql.DB) *InspirationAssetModel {
	return &InspirationAssetModel{DB: db}
}

func (m *InspirationAssetModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS inspiration_assets (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	title VARCHAR(255) NOT NULL COMMENT '灵感标题',
	description TEXT COMMENT '灵感描述',
	cover_image VARCHAR(512) COMMENT '封面图URL',
	images TEXT COMMENT '多图JSON数组',
	tags TEXT COMMENT '标签JSON数组',
	scene VARCHAR(128) DEFAULT '' COMMENT '场景',
	style VARCHAR(128) DEFAULT '' COMMENT '风格',
	topic VARCHAR(64) NOT NULL DEFAULT 'inspiration' COMMENT '一级话题',
	sort_order INT NOT NULL DEFAULT 0 COMMENT '排序值，越大越靠前',
	status VARCHAR(32) NOT NULL DEFAULT 'draft' COMMENT '状态：published、pending、draft、archived',
	creator VARCHAR(128) NOT NULL DEFAULT '' COMMENT '创建者展示名',
	creator_user_id BIGINT NULL DEFAULT NULL COMMENT '投稿用户ID',
	view_count BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '浏览次数',
	like_count BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '点赞次数',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_topic (topic),
	INDEX idx_status (status),
	INDEX idx_scene (scene),
	INDEX idx_style (style),
	INDEX idx_creator_user_id (creator_user_id),
	INDEX idx_sort_order (sort_order),
	INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN cover_image VARCHAR(512) NULL COMMENT '封面图URL'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN images TEXT NULL COMMENT '多图JSON数组'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN tags TEXT NULL COMMENT '标签JSON数组'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN scene VARCHAR(128) NOT NULL DEFAULT '' COMMENT '场景'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN style VARCHAR(128) NOT NULL DEFAULT '' COMMENT '风格'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN topic VARCHAR(64) NOT NULL DEFAULT 'inspiration' COMMENT '一级话题'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN sort_order INT NOT NULL DEFAULT 0 COMMENT '排序值'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN creator_user_id BIGINT NULL DEFAULT NULL COMMENT '投稿用户ID'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN view_count BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '浏览次数'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN like_count BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '点赞次数'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN image_width INT NOT NULL DEFAULT 0 COMMENT 'image width'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD COLUMN image_height INT NOT NULL DEFAULT 0 COMMENT 'image height'`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD INDEX idx_topic (topic)`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD INDEX idx_status (status)`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD INDEX idx_scene (scene)`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD INDEX idx_style (style)`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD INDEX idx_creator_user_id (creator_user_id)`)
	_, _ = m.DB.Exec(`ALTER TABLE inspiration_assets ADD INDEX idx_sort_order (sort_order)`)
	return nil
}

func (m *InspirationAssetModel) Create(asset *InspirationAsset) error {
	populateInspirationImageMetadata(asset)
	query := `INSERT INTO inspiration_assets (title, description, cover_image, images, image_width, image_height, tags, scene, style, topic, sort_order, status, creator, creator_user_id)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	result, err := m.DB.Exec(
		query,
		asset.Title,
		asset.Description,
		asset.CoverImage,
		asset.Images,
		asset.ImageWidth,
		asset.ImageHeight,
		asset.Tags,
		asset.Scene,
		asset.Style,
		asset.Topic,
		asset.SortOrder,
		asset.Status,
		asset.Creator,
		nullableInt64(asset.CreatorUserID),
	)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	asset.ID = id
	return nil
}

func (m *InspirationAssetModel) GetByID(id int64) (*InspirationAsset, error) {
	query := `SELECT id, title, description, cover_image, images, image_width, image_height, tags, scene, style, topic, sort_order, status, creator, creator_user_id, view_count, like_count, created_at, updated_at
	          FROM inspiration_assets WHERE id = ?`
	asset := &InspirationAsset{}
	var creatorUserID sql.NullInt64
	err := m.DB.QueryRow(query, id).Scan(
		&asset.ID,
		&asset.Title,
		&asset.Description,
		&asset.CoverImage,
		&asset.Images,
		&asset.ImageWidth,
		&asset.ImageHeight,
		&asset.Tags,
		&asset.Scene,
		&asset.Style,
		&asset.Topic,
		&asset.SortOrder,
		&asset.Status,
		&asset.Creator,
		&creatorUserID,
		&asset.ViewCount,
		&asset.LikeCount,
		&asset.CreatedAt,
		&asset.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if creatorUserID.Valid {
		asset.CreatorUserID = creatorUserID.Int64
	}
	return asset, nil
}

func (m *InspirationAssetModel) List(topic, scene, style, status, keyword string, creatorUserID int64, limit, offset int) ([]*InspirationAsset, error) {
	query := `SELECT id, title, description, cover_image, images, image_width, image_height, tags, scene, style, topic, sort_order, status, creator, creator_user_id, view_count, like_count, created_at, updated_at
	          FROM inspiration_assets WHERE 1=1`
	args := []interface{}{}
	query, args = buildInspirationFilters(query, args, topic, scene, style, status, keyword, creatorUserID)
	query += ` ORDER BY sort_order DESC, created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]*InspirationAsset, 0)
	for rows.Next() {
		asset := &InspirationAsset{}
		var uid sql.NullInt64
		if err := rows.Scan(
			&asset.ID,
			&asset.Title,
			&asset.Description,
			&asset.CoverImage,
			&asset.Images,
			&asset.ImageWidth,
			&asset.ImageHeight,
			&asset.Tags,
			&asset.Scene,
			&asset.Style,
			&asset.Topic,
			&asset.SortOrder,
			&asset.Status,
			&asset.Creator,
			&uid,
			&asset.ViewCount,
			&asset.LikeCount,
			&asset.CreatedAt,
			&asset.UpdatedAt,
		); err != nil {
			continue
		}
		if uid.Valid {
			asset.CreatorUserID = uid.Int64
		}
		list = append(list, asset)
	}
	return list, nil
}

func (m *InspirationAssetModel) Count(topic, scene, style, status, keyword string, creatorUserID int64) (int64, error) {
	query := `SELECT COUNT(*) FROM inspiration_assets WHERE 1=1`
	args := []interface{}{}
	query, args = buildInspirationFilters(query, args, topic, scene, style, status, keyword, creatorUserID)
	var total int64
	err := m.DB.QueryRow(query, args...).Scan(&total)
	return total, err
}

func (m *InspirationAssetModel) Update(asset *InspirationAsset) error {
	populateInspirationImageMetadata(asset)
	query := `UPDATE inspiration_assets SET title = ?, description = ?, cover_image = ?, images = ?, image_width = ?, image_height = ?, tags = ?, scene = ?, style = ?, topic = ?, sort_order = ?, status = ? WHERE id = ?`
	_, err := m.DB.Exec(
		query,
		asset.Title,
		asset.Description,
		asset.CoverImage,
		asset.Images,
		asset.ImageWidth,
		asset.ImageHeight,
		asset.Tags,
		asset.Scene,
		asset.Style,
		asset.Topic,
		asset.SortOrder,
		asset.Status,
		asset.ID,
	)
	return err
}

func (m *InspirationAssetModel) Delete(id int64) error {
	_, err := m.DB.Exec(`DELETE FROM inspiration_assets WHERE id = ?`, id)
	return err
}

func (m *InspirationAssetModel) ListWithoutImageMetadata(limit int, offset int) ([]*InspirationAsset, error) {
	query := `SELECT id, title, description, cover_image, images, image_width, image_height, tags, scene, style, topic, sort_order, status, creator, creator_user_id, view_count, like_count, created_at, updated_at
	          FROM inspiration_assets
	          WHERE (image_width <= 0 OR image_height <= 0)
	            AND (
	              TRIM(COALESCE(cover_image, '')) <> ''
	              OR TRIM(COALESCE(images, '')) <> ''
	            )
	          ORDER BY id ASC
	          LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]*InspirationAsset, 0)
	for rows.Next() {
		asset := &InspirationAsset{}
		var uid sql.NullInt64
		if err := rows.Scan(
			&asset.ID,
			&asset.Title,
			&asset.Description,
			&asset.CoverImage,
			&asset.Images,
			&asset.ImageWidth,
			&asset.ImageHeight,
			&asset.Tags,
			&asset.Scene,
			&asset.Style,
			&asset.Topic,
			&asset.SortOrder,
			&asset.Status,
			&asset.Creator,
			&uid,
			&asset.ViewCount,
			&asset.LikeCount,
			&asset.CreatedAt,
			&asset.UpdatedAt,
		); err != nil {
			continue
		}
		if uid.Valid {
			asset.CreatorUserID = uid.Int64
		}
		list = append(list, asset)
	}
	return list, nil
}

func (m *InspirationAssetModel) UpdateImageMetadataByID(id int64, width int, height int) error {
	_, err := m.DB.Exec(`UPDATE inspiration_assets SET image_width = ?, image_height = ? WHERE id = ?`, width, height, id)
	return err
}

func (m *InspirationAssetModel) UpdateImageAssetsByID(id int64, coverImage string, images string) error {
	_, err := m.DB.Exec(`UPDATE inspiration_assets SET cover_image = ?, images = ? WHERE id = ?`, strings.TrimSpace(coverImage), strings.TrimSpace(images), id)
	return err
}

func (m *InspirationAssetModel) IncrementViewCount(id int64) error {
	_, err := m.DB.Exec(`UPDATE inspiration_assets SET view_count = view_count + 1 WHERE id = ?`, id)
	return err
}

func buildInspirationFilters(query string, args []interface{}, topic, scene, style, status, keyword string, creatorUserID int64) (string, []interface{}) {
	if topic = strings.TrimSpace(topic); topic != "" {
		query += ` AND topic = ?`
		args = append(args, topic)
	}
	if scene = strings.TrimSpace(scene); scene != "" {
		query += ` AND scene = ?`
		args = append(args, scene)
	}
	if style = strings.TrimSpace(style); style != "" {
		query += ` AND style = ?`
		args = append(args, style)
	}
	if status = strings.TrimSpace(status); status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	if creatorUserID > 0 {
		query += ` AND creator_user_id = ?`
		args = append(args, creatorUserID)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		query += ` AND (title LIKE ? OR description LIKE ? OR scene LIKE ? OR style LIKE ? OR tags LIKE ?)`
		args = append(args, like, like, like, like, like)
	}
	return query, args
}
