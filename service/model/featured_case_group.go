package model

import (
	"database/sql"
	"time"
)

// FeaturedCaseGroup 精选案例组结构
type FeaturedCaseGroup struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`         // 组名称
	DisplayMode string    `json:"display_mode"` // 显示模式: comparison(真实vs AI), side_by_side(真实和AI), normal(普通案例)
	Case1ID     int64     `json:"case1_id"`     // 第一个案例ID
	Case2ID     int64     `json:"case2_id"`     // 第二个案例ID（普通模式可为0）
	Case1Label  string    `json:"case1_label"`  // 第一个案例标签（如"真实"）
	Case2Label  string    `json:"case2_label"`  // 第二个案例标签（如"AI"）
	SortOrder   int       `json:"sort_order"`    // 排序顺序
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// FeaturedCaseGroupModel 精选案例组模型
type FeaturedCaseGroupModel struct {
	DB *sql.DB
}

// NewFeaturedCaseGroupModel 创建精选案例组模型
func NewFeaturedCaseGroupModel(db *sql.DB) *FeaturedCaseGroupModel {
	return &FeaturedCaseGroupModel{DB: db}
}

// InitTable 初始化featured_case_groups表
func (m *FeaturedCaseGroupModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS featured_case_groups (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255) NOT NULL COMMENT '组名称',
	display_mode VARCHAR(32) NOT NULL DEFAULT 'comparison' COMMENT '显示模式: comparison(真实vs AI), side_by_side(真实和AI), normal(普通案例)',
	case1_id BIGINT UNSIGNED NOT NULL COMMENT '第一个案例ID',
	case2_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '第二个案例ID（普通模式可为0）',
	case1_label VARCHAR(64) NOT NULL DEFAULT '真实' COMMENT '第一个案例标签',
	case2_label VARCHAR(64) NOT NULL DEFAULT 'AI' COMMENT '第二个案例标签',
	sort_order INT NOT NULL DEFAULT 0 COMMENT '排序顺序',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_sort_order (sort_order),
	INDEX idx_display_mode (display_mode)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	return nil
}

// Create 创建精选案例组
func (m *FeaturedCaseGroupModel) Create(group *FeaturedCaseGroup) error {
	query := `INSERT INTO featured_case_groups (name, display_mode, case1_id, case2_id, case1_label, case2_label, sort_order)
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	result, err := m.DB.Exec(query, group.Name, group.DisplayMode, group.Case1ID, group.Case2ID,
		group.Case1Label, group.Case2Label, group.SortOrder)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	group.ID = id
	return nil
}

// GetByID 根据ID获取精选案例组
func (m *FeaturedCaseGroupModel) GetByID(id int64) (*FeaturedCaseGroup, error) {
	query := `SELECT id, name, display_mode, case1_id, case2_id, case1_label, case2_label, sort_order, created_at, updated_at
	          FROM featured_case_groups WHERE id = ?`
	group := &FeaturedCaseGroup{}
	err := m.DB.QueryRow(query, id).Scan(
		&group.ID, &group.Name, &group.DisplayMode, &group.Case1ID, &group.Case2ID,
		&group.Case1Label, &group.Case2Label, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return group, nil
}

// List 获取精选案例组列表
func (m *FeaturedCaseGroupModel) List(limit int, offset int) ([]*FeaturedCaseGroup, error) {
	query := `SELECT id, name, display_mode, case1_id, case2_id, case1_label, case2_label, sort_order, created_at, updated_at
	          FROM featured_case_groups
	          ORDER BY sort_order ASC, created_at DESC
	          LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*FeaturedCaseGroup
	for rows.Next() {
		group := &FeaturedCaseGroup{}
		err := rows.Scan(
			&group.ID, &group.Name, &group.DisplayMode, &group.Case1ID, &group.Case2ID,
			&group.Case1Label, &group.Case2Label, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
		if err != nil {
			continue
		}
		groups = append(groups, group)
	}
	return groups, nil
}

// GetAll 获取所有精选案例组（用于小程序）
func (m *FeaturedCaseGroupModel) GetAll() ([]*FeaturedCaseGroup, error) {
	query := `SELECT id, name, display_mode, case1_id, case2_id, case1_label, case2_label, sort_order, created_at, updated_at
	          FROM featured_case_groups
	          ORDER BY sort_order ASC, created_at DESC`
	rows, err := m.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*FeaturedCaseGroup
	for rows.Next() {
		group := &FeaturedCaseGroup{}
		err := rows.Scan(
			&group.ID, &group.Name, &group.DisplayMode, &group.Case1ID, &group.Case2ID,
			&group.Case1Label, &group.Case2Label, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
		if err != nil {
			continue
		}
		groups = append(groups, group)
	}
	return groups, nil
}

// Update 更新精选案例组
func (m *FeaturedCaseGroupModel) Update(group *FeaturedCaseGroup) error {
	query := `UPDATE featured_case_groups SET name = ?, display_mode = ?, case1_id = ?, case2_id = ?,
	          case1_label = ?, case2_label = ?, sort_order = ?
	          WHERE id = ?`
	_, err := m.DB.Exec(query, group.Name, group.DisplayMode, group.Case1ID, group.Case2ID,
		group.Case1Label, group.Case2Label, group.SortOrder, group.ID)
	return err
}

// Delete 删除精选案例组
func (m *FeaturedCaseGroupModel) Delete(id int64) error {
	query := "DELETE FROM featured_case_groups WHERE id = ?"
	_, err := m.DB.Exec(query, id)
	return err
}

// Count 统计精选案例组数量
func (m *FeaturedCaseGroupModel) Count() (int64, error) {
	query := "SELECT COUNT(*) FROM featured_case_groups"
	var count int64
	err := m.DB.QueryRow(query).Scan(&count)
	return count, err
}
