package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"time"
)

// AIPricing AI计费配置
type AIPricing struct {
	ID          int64          `json:"id" db:"id"`
	Scene       string         `json:"scene" db:"scene"` // ai_draw_single, ai_draw_multi, ai_chat_single, ai_chat_multi
	Stones      int64          `json:"stones" db:"stones"`
	ExtraConfig sql.NullString `json:"-" db:"extra_config"` // JSON格式的额外配置（使用NullString处理NULL）
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" db:"updated_at"`
}

// GetExtraConfig 获取额外配置字符串
func (p *AIPricing) GetExtraConfig() string {
	if p.ExtraConfig.Valid {
		return p.ExtraConfig.String
	}
	return ""
}

// AIPricingModel AI计费配置数据访问层
type AIPricingModel struct {
	DB    *sql.DB
	Redis *redis.Client
	ctx   context.Context
}

// NewAIPricingModel 创建AI计费配置模型
func NewAIPricingModel(db *sql.DB, rdb *redis.Client) *AIPricingModel {
	return &AIPricingModel{
		DB:    db,
		Redis: rdb,
		ctx:   context.Background(),
	}
}

// GetByScene 根据场景获取计费配置（优先从Redis读取）
func (m *AIPricingModel) GetByScene(scene string) (*AIPricing, error) {
	// 先尝试从Redis读取
	key := "ai_pricing:scene:" + scene
	val, err := m.Redis.Get(m.ctx, key).Result()
	if err == nil {
		var pricing AIPricing
		if err := json.Unmarshal([]byte(val), &pricing); err == nil {
			return &pricing, nil
		}
	}

	// Redis中没有，从MySQL读取
	pricing := &AIPricing{}
	query := `SELECT id, scene, stones, extra_config, created_at, updated_at 
	          FROM ai_pricing WHERE scene = ?`
	err = m.DB.QueryRow(query, scene).Scan(
		&pricing.ID, &pricing.Scene, &pricing.Stones, &pricing.ExtraConfig,
		&pricing.CreatedAt, &pricing.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// 写入Redis缓存
	data, _ := json.Marshal(pricing)
	m.Redis.Set(m.ctx, key, data, time.Hour*24)

	return pricing, nil
}

// GetAll 获取所有计费配置
func (m *AIPricingModel) GetAll() ([]*AIPricing, error) {
	query := `SELECT id, scene, stones, extra_config, created_at, updated_at 
	          FROM ai_pricing ORDER BY scene`
	rows, err := m.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pricings []*AIPricing
	for rows.Next() {
		pricing := &AIPricing{}
		err := rows.Scan(
			&pricing.ID, &pricing.Scene, &pricing.Stones, &pricing.ExtraConfig,
			&pricing.CreatedAt, &pricing.UpdatedAt,
		)
		if err != nil {
			continue
		}
		pricings = append(pricings, pricing)
	}
	return pricings, nil
}

// Upsert 创建或更新计费配置
func (m *AIPricingModel) Upsert(pricing *AIPricing) error {
	query := `INSERT INTO ai_pricing (scene, stones, extra_config, created_at, updated_at) 
	          VALUES (?, ?, ?, NOW(), NOW())
	          ON DUPLICATE KEY UPDATE 
	          stones = VALUES(stones),
	          extra_config = VALUES(extra_config),
	          updated_at = NOW()`

	// 如果 ExtraConfig 无效，传 nil
	var extraConfig interface{}
	if pricing.ExtraConfig.Valid {
		extraConfig = pricing.ExtraConfig.String
	}
	_, err := m.DB.Exec(query, pricing.Scene, pricing.Stones, extraConfig)
	if err != nil {
		return err
	}

	// 清除Redis缓存
	key := "ai_pricing:scene:" + pricing.Scene
	m.Redis.Del(m.ctx, key)

	return nil
}

// InitTable 初始化ai_pricing表
func (m *AIPricingModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS ai_pricing (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	scene VARCHAR(64) NOT NULL UNIQUE,
	stones BIGINT UNSIGNED NOT NULL DEFAULT 0,
	extra_config TEXT,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_scene (scene)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}

	// 初始化默认配置：普通类图片20，视频固定30，专业类图片20，造价20
	defaultPricings := []struct {
		Scene  string
		Stones int64
	}{
		{"ai_draw_single", 20},                 // 普通类AI生成图片
		{"ai_draw_multi", 20},                  // 普通类AI生成图片
		{"new_minnan_tool_text_to_image", 20},  // 新闽派工具：文生图
		{"new_minnan_tool_image_to_image", 20}, // 新闽派工具：图生图
		{"ai_chat_single", 10},
		{"ai_chat_multi", 20},
		{"building_code_chat", 0},    // 建规快查：一期默认免费，可在后台调价
		{"rural_villa_design", 20},   // 专业类：乡村别墅设计
		{"allround_design", 20},      // 专业类：全能设计
		{"building_replacement", 20}, // 专业类：建筑换新
		{"parent_child_design", 20},  // 专业类：亲子设计
		{"ai_cost_doc", 20},          // AI造价
		{"ai_video_1", 30},           // 生成视频：固定30灵石
		{"ai_video_2", 30},           // 生成视频：固定30灵石
		{"ai_video_3", 30},           // 生成视频：固定30灵石
		{"ai_video_4", 30},           // 生成视频：固定30灵石
	}

	for _, dp := range defaultPricings {
		query := `INSERT IGNORE INTO ai_pricing (scene, stones) VALUES (?, ?)`
		m.DB.Exec(query, dp.Scene, dp.Stones)
	}

	return nil
}
