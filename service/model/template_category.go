package model

import "database/sql"

// TemplateCategory 模板分类
type TemplateCategory struct {
	ID        string `json:"id"`         // 分类ID，如 villa, urban
	Name      string `json:"name"`       // 展示名称
	SortOrder int    `json:"sort_order"` // 排序，越小越靠前
}

// TemplateCategoryModel 模板分类模型
type TemplateCategoryModel struct {
	DB *sql.DB
}

// NewTemplateCategoryModel 创建模板分类模型
func NewTemplateCategoryModel(db *sql.DB) *TemplateCategoryModel {
	return &TemplateCategoryModel{DB: db}
}

// InitTable 创建 template_categories 表并插入默认分类（若不存在）
func (m *TemplateCategoryModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS template_categories (
	id VARCHAR(64) NOT NULL PRIMARY KEY COMMENT '分类ID',
	name VARCHAR(128) NOT NULL COMMENT '展示名称',
	sort_order INT NOT NULL DEFAULT 0 COMMENT '排序'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	// 默认分类（若不存在则插入）
	defaults := []struct {
		id        string
		name      string
		sortOrder int
	}{
		{"villa", "乡村别墅", 1},
		{"urban", "城市焕新", 2},
		{"family", "亲子", 3},
		{"culture", "文创", 4},
	}
	for _, d := range defaults {
		_, err := m.DB.Exec(
			`INSERT IGNORE INTO template_categories (id, name, sort_order) VALUES (?, ?, ?)`,
			d.id, d.name, d.sortOrder,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// List 获取全部分类（按 sort_order 升序）
func (m *TemplateCategoryModel) List() ([]*TemplateCategory, error) {
	rows, err := m.DB.Query(
		`SELECT id, name, sort_order FROM template_categories ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*TemplateCategory
	for rows.Next() {
		c := &TemplateCategory{}
		if err := rows.Scan(&c.ID, &c.Name, &c.SortOrder); err != nil {
			continue
		}
		list = append(list, c)
	}
	return list, nil
}

// GetByID 根据 ID 获取分类
func (m *TemplateCategoryModel) GetByID(id string) (*TemplateCategory, error) {
	c := &TemplateCategory{}
	err := m.DB.QueryRow(
		`SELECT id, name, sort_order FROM template_categories WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.SortOrder)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Create 新增分类
func (m *TemplateCategoryModel) Create(c *TemplateCategory) error {
	_, err := m.DB.Exec(
		`INSERT INTO template_categories (id, name, sort_order) VALUES (?, ?, ?)`,
		c.ID, c.Name, c.SortOrder,
	)
	return err
}

// Update 更新分类（名称与排序）
func (m *TemplateCategoryModel) Update(c *TemplateCategory) error {
	_, err := m.DB.Exec(
		`UPDATE template_categories SET name = ?, sort_order = ? WHERE id = ?`,
		c.Name, c.SortOrder, c.ID,
	)
	return err
}

// Delete 删除分类
func (m *TemplateCategoryModel) Delete(id string) error {
	_, err := m.DB.Exec(`DELETE FROM template_categories WHERE id = ?`, id)
	return err
}

// CountTemplatesByCategory 统计某分类下的模板数量
func (m *TemplateCategoryModel) CountTemplatesByCategory(categoryID string) (int64, error) {
	var count int64
	err := m.DB.QueryRow(
		`SELECT COUNT(*) FROM templates WHERE category = ?`, categoryID,
	).Scan(&count)
	return count, err
}
