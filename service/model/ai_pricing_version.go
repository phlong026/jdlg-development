package model

import (
	"database/sql"
	"time"
)

// AIPricingVersion AI 定价版本记录：每次调价留痕，支持回滚（回滚 = 用旧值再调一次，同样留痕）
type AIPricingVersion struct {
	ID        int64     `json:"id"`
	Scene     string    `json:"scene"`
	OldStones int64     `json:"old_stones"`
	NewStones int64     `json:"new_stones"`
	Operator  string    `json:"operator"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// AIPricingVersionModel 定价版本数据访问层
type AIPricingVersionModel struct {
	DB *sql.DB
}

// NewAIPricingVersionModel 创建定价版本模型
func NewAIPricingVersionModel(db *sql.DB) *AIPricingVersionModel {
	return &AIPricingVersionModel{DB: db}
}

// InitTable 初始化定价版本表（只增不改，无回滚风险）
func (m *AIPricingVersionModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS ai_pricing_versions (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	scene VARCHAR(64) NOT NULL,
	old_stones BIGINT UNSIGNED NOT NULL DEFAULT 0,
	new_stones BIGINT UNSIGNED NOT NULL DEFAULT 0,
	operator VARCHAR(64) NOT NULL DEFAULT '',
	note VARCHAR(255) NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	INDEX idx_scene (scene),
	INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	return err
}

// Record 记录一次调价（仅当定价真正变化时由调用方调用）
func (m *AIPricingVersionModel) Record(scene string, oldStones, newStones int64, operator, note string) error {
	_, err := m.DB.Exec(
		`INSERT INTO ai_pricing_versions (scene, old_stones, new_stones, operator, note) VALUES (?, ?, ?, ?, ?)`,
		scene, oldStones, newStones, operator, note)
	return err
}

// ListByScene 某场景的调价历史（新→旧）
func (m *AIPricingVersionModel) ListByScene(scene string, limit int) ([]*AIPricingVersion, error) {
	if limit < 1 {
		limit = 20
	}
	rows, err := m.DB.Query(
		`SELECT id, scene, old_stones, new_stones, operator, COALESCE(note, ''), created_at
		 FROM ai_pricing_versions WHERE scene = ? ORDER BY id DESC LIMIT ?`, scene, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AIPricingVersion
	for rows.Next() {
		v := &AIPricingVersion{}
		if err := rows.Scan(&v.ID, &v.Scene, &v.OldStones, &v.NewStones, &v.Operator, &v.Note, &v.CreatedAt); err != nil {
			continue
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LatestVersionMap 各场景最近一次调价（列表页"最近调价"列）
func (m *AIPricingVersionModel) LatestVersionMap() (map[string]*AIPricingVersion, error) {
	rows, err := m.DB.Query(
		`SELECT v.id, v.scene, v.old_stones, v.new_stones, v.operator, COALESCE(v.note, ''), v.created_at
		 FROM ai_pricing_versions v
		 INNER JOIN (SELECT scene, MAX(id) AS max_id FROM ai_pricing_versions GROUP BY scene) t
		   ON t.scene = v.scene AND t.max_id = v.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]*AIPricingVersion)
	for rows.Next() {
		v := &AIPricingVersion{}
		if err := rows.Scan(&v.ID, &v.Scene, &v.OldStones, &v.NewStones, &v.Operator, &v.Note, &v.CreatedAt); err != nil {
			continue
		}
		out[v.Scene] = v
	}
	return out, rows.Err()
}
