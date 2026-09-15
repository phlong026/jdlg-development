package model

import (
	"database/sql"
	"encoding/json"
	"time"
)

// TabItem 单个 Tab：label 展示名，value 传参值
type TabItem struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Parent string `json:"parent,omitempty"` // 二级tab的父tab value，一级tab为空
}

// TemplateSquareConfig 模板广场双重 Tab 配置（单行）
type TemplateSquareConfig struct {
	ID        int64     `json:"id" db:"id"`
	MainTabs  string    `json:"main_tabs" db:"main_tabs"`   // JSON 数组 [{label, value}, ...]
	SubTabs   string    `json:"sub_tabs" db:"sub_tabs"`     // JSON 数组 [{label, value}, ...]，空则小程序端用分类+固定项拼
	ThirdTabs string    `json:"third_tabs" db:"third_tabs"` // JSON 数组 [{label, value, parent}, ...]
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// TemplateSquareConfigModel 模板广场配置数据访问
type TemplateSquareConfigModel struct {
	DB *sql.DB
}

// NewTemplateSquareConfigModel 创建模型
func NewTemplateSquareConfigModel(db *sql.DB) *TemplateSquareConfigModel {
	return &TemplateSquareConfigModel{DB: db}
}

// InitTable 创建表并插入默认行（若不存在）
func (m *TemplateSquareConfigModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS template_square_config (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	main_tabs JSON COMMENT '一级 Tab [{label,value},...]',
	sub_tabs JSON COMMENT '二级 Tab [{label,value},...]，空则前端用分类+固定项',
	third_tabs JSON COMMENT '三级 Tab [{label,value,parent},...]，parent 指向二级 Tab value',
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE template_square_config ADD COLUMN third_tabs JSON COMMENT '三级 Tab [{label,value,parent},...]，parent 指向二级 Tab value'`)
	var count int
	if err := m.DB.QueryRow("SELECT COUNT(*) FROM template_square_config").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		defaultMain := `[{"label":"场景","value":"scene"},{"label":"风格","value":"style"},{"label":"灵感","value":"inspiration"}]`
		defaultSub := `[{"label":"乡墅外观","value":"villa_exterior","parent":"scene"},{"label":"室内空间","value":"interior_space","parent":"scene"},{"label":"花园庭院","value":"garden_courtyard","parent":"scene"},{"label":"改造翻新","value":"renovation","parent":"scene"},{"label":"商业空间","value":"commercial_space","parent":"scene"},{"label":"设计辅助","value":"design_assist","parent":"scene"},{"label":"新闽派","value":"new_minnan","parent":"style"},{"label":"新中式","value":"new_chinese","parent":"style"},{"label":"现代风格","value":"modern","parent":"style"},{"label":"经典欧式","value":"classic_european","parent":"style"},{"label":"地域特色","value":"regional","parent":"style"},{"label":"乡建趋势","value":"rural_trend","parent":"inspiration"},{"label":"生活方式","value":"lifestyle","parent":"inspiration"},{"label":"地域文化","value":"regional_culture","parent":"inspiration"},{"label":"功能创新","value":"function_innovation","parent":"inspiration"},{"label":"案例精选","value":"selected_cases","parent":"inspiration"}]`
		defaultThird := `[]`
		_, err := m.DB.Exec(
			`INSERT INTO template_square_config (main_tabs, sub_tabs, third_tabs) VALUES (?, ?, ?)`,
			defaultMain, defaultSub, defaultThird,
		)
		return err
	}
	return nil
}

// Get 获取配置（仅一行）
func (m *TemplateSquareConfigModel) Get() (*TemplateSquareConfig, error) {
	c := &TemplateSquareConfig{}
	err := m.DB.QueryRow(
		`SELECT id, main_tabs, sub_tabs, COALESCE(third_tabs, JSON_ARRAY()), updated_at FROM template_square_config ORDER BY id ASC LIMIT 1`,
	).Scan(&c.ID, &c.MainTabs, &c.SubTabs, &c.ThirdTabs, &c.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return c, nil
}

// Set 更新配置（upsert 唯一行）
func (m *TemplateSquareConfigModel) Set(mainTabsJSON, subTabsJSON, thirdTabsJSON string) error {
	var count int
	if err := m.DB.QueryRow("SELECT COUNT(*) FROM template_square_config").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		_, err := m.DB.Exec(
			`INSERT INTO template_square_config (main_tabs, sub_tabs, third_tabs) VALUES (?, ?, ?)`,
			mainTabsJSON, subTabsJSON, thirdTabsJSON,
		)
		return err
	}
	_, err := m.DB.Exec(
		`UPDATE template_square_config SET main_tabs = ?, sub_tabs = ?, third_tabs = ?, updated_at = NOW() ORDER BY id ASC LIMIT 1`,
		mainTabsJSON, subTabsJSON, thirdTabsJSON,
	)
	return err
}

// ParseMainTabs 解析 main_tabs JSON 为 []TabItem
func (m *TemplateSquareConfigModel) ParseMainTabs(raw string) ([]TabItem, error) {
	if raw == "" {
		return nil, nil
	}
	var list []TabItem
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ParseSubTabs 解析 sub_tabs JSON 为 []TabItem
func (m *TemplateSquareConfigModel) ParseSubTabs(raw string) ([]TabItem, error) {
	if raw == "" {
		return nil, nil
	}
	var list []TabItem
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	return list, nil
}

// ParseThirdTabs 解析 third_tabs JSON 为 []TabItem
func (m *TemplateSquareConfigModel) ParseThirdTabs(raw string) ([]TabItem, error) {
	if raw == "" {
		return nil, nil
	}
	var list []TabItem
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	return list, nil
}
