package model

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// ───────────────────────── AI 工具版本（二期批次2：草稿-发布分离） ─────────────────────────
// 设计：ai_tools 行始终承载"线上配置"（小程序读它，读路径零改动）；
// 管理端编辑写入草稿快照（每工具一条 status=draft），发布时把快照应用到行并落一个
// published 版本（旧 published 转 archived）。回滚 = 用历史版本快照再发布一次。

const (
	AIToolVersionStatusDraft     = "draft"
	AIToolVersionStatusPublished = "published"
	AIToolVersionStatusArchived  = "archived"
)

// AIToolVersion 工具版本记录。VersionNo=0 表示草稿（每工具至多一条）。
type AIToolVersion struct {
	ID          int64      `json:"id"`
	ToolID      int64      `json:"tool_id"`
	VersionNo   int64      `json:"version_no"`
	Status      string     `json:"status"`
	Snapshot    string     `json:"-"`
	Note        string     `json:"note"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// AIToolVersionModel 工具版本数据访问层
type AIToolVersionModel struct {
	DB *sql.DB
}

func NewAIToolVersionModel(db *sql.DB) *AIToolVersionModel {
	return &AIToolVersionModel{DB: db}
}

func (m *AIToolVersionModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS ai_tool_versions (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	tool_id BIGINT UNSIGNED NOT NULL,
	version_no INT UNSIGNED NOT NULL DEFAULT 0,
	status VARCHAR(16) NOT NULL DEFAULT 'draft',
	snapshot LONGTEXT NOT NULL,
	note VARCHAR(255) NOT NULL DEFAULT '',
	created_by VARCHAR(64) NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	published_at TIMESTAMP NULL DEFAULT NULL,
	INDEX idx_tool_status (tool_id, status),
	INDEX idx_tool_version (tool_id, version_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	return err
}

const aiToolVersionColumns = `id, tool_id, version_no, status, snapshot, COALESCE(note, ''), COALESCE(created_by, ''), created_at, published_at`

func scanAIToolVersion(scanner interface{ Scan(dest ...interface{}) error }) (*AIToolVersion, error) {
	v := &AIToolVersion{}
	if err := scanner.Scan(&v.ID, &v.ToolID, &v.VersionNo, &v.Status, &v.Snapshot, &v.Note, &v.CreatedBy, &v.CreatedAt, &v.PublishedAt); err != nil {
		return nil, err
	}
	return v, nil
}

// SaveDraft 写入/覆盖某工具的草稿快照（每工具一条）
func (m *AIToolVersionModel) SaveDraft(toolID int64, snapshotJSON, operator, note string) error {
	note = truncateAIToolVersionNote(note)
	// 优先更新已有草稿，否则插入
	result, err := m.DB.Exec(
		`UPDATE ai_tool_versions SET snapshot = ?, note = ?, created_by = ?, created_at = NOW() WHERE tool_id = ? AND status = 'draft'`,
		snapshotJSON, note, operator, toolID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		return nil
	}
	_, err = m.DB.Exec(
		`INSERT INTO ai_tool_versions (tool_id, version_no, status, snapshot, note, created_by) VALUES (?, 0, 'draft', ?, ?, ?)`,
		toolID, snapshotJSON, note, operator)
	return err
}

func truncateAIToolVersionNote(note string) string {
	note = strings.TrimSpace(note)
	if len(note) > 200 {
		note = note[:200]
	}
	return note
}

// GetDraft 读草稿（无草稿返回 nil, nil）
func (m *AIToolVersionModel) GetDraft(toolID int64) (*AIToolVersion, error) {
	row := m.DB.QueryRow(
		`SELECT `+aiToolVersionColumns+` FROM ai_tool_versions WHERE tool_id = ? AND status = 'draft'`, toolID)
	v, err := scanAIToolVersion(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return v, err
}

// GetLatestPublished 最新已发布版本（无则 nil, nil）
func (m *AIToolVersionModel) GetLatestPublished(toolID int64) (*AIToolVersion, error) {
	row := m.DB.QueryRow(
		`SELECT `+aiToolVersionColumns+` FROM ai_tool_versions
		 WHERE tool_id = ? AND status = 'published' ORDER BY version_no DESC LIMIT 1`, toolID)
	v, err := scanAIToolVersion(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return v, err
}

// RecordPublish 落一个 published 版本：旧 published 转 archived，写入新版本并分配递增版本号。
// 调用方负责把快照应用到 ai_tools 行（route 层完成，保证校验在同一处）。
func (m *AIToolVersionModel) RecordPublish(toolID int64, snapshotJSON, operator, note string) (*AIToolVersion, error) {
	var maxNo int64
	if err := m.DB.QueryRow(
		`SELECT COALESCE(MAX(version_no), 0) FROM ai_tool_versions WHERE tool_id = ?`, toolID).Scan(&maxNo); err != nil {
		return nil, err
	}
	if _, err := m.DB.Exec(
		`UPDATE ai_tool_versions SET status = 'archived' WHERE tool_id = ? AND status = 'published'`, toolID); err != nil {
		return nil, err
	}
	result, err := m.DB.Exec(
		`INSERT INTO ai_tool_versions (tool_id, version_no, status, snapshot, note, created_by, published_at)
		 VALUES (?, ?, 'published', ?, ?, ?, NOW())`,
		toolID, maxNo+1, snapshotJSON, truncateAIToolVersionNote(note), operator)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	// 同步草稿为已发布内容，编辑器打开时与线上一致
	if err := m.SaveDraft(toolID, snapshotJSON, operator, note); err != nil {
		return nil, err
	}
	row := m.DB.QueryRow(`SELECT `+aiToolVersionColumns+` FROM ai_tool_versions WHERE id = ?`, id)
	return scanAIToolVersion(row)
}

// GetVersion 取指定版本（回滚用）
func (m *AIToolVersionModel) GetVersion(versionID int64) (*AIToolVersion, error) {
	row := m.DB.QueryRow(`SELECT `+aiToolVersionColumns+` FROM ai_tool_versions WHERE id = ?`, versionID)
	v, err := scanAIToolVersion(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return v, err
}

// ListVersions 某工具的版本列表（新→旧，草稿除外；数量上限由调用方控制）
func (m *AIToolVersionModel) ListVersions(toolID int64, limit int) ([]*AIToolVersion, error) {
	if limit < 1 {
		limit = 10
	}
	rows, err := m.DB.Query(
		`SELECT `+aiToolVersionColumns+` FROM ai_tool_versions
		 WHERE tool_id = ? AND status != 'draft' ORDER BY version_no DESC LIMIT ?`, toolID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AIToolVersion
	for rows.Next() {
		v, err := scanAIToolVersion(rows)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DraftStateMap 全部工具的草稿状态（列表页徽章用）：tool_id → (有草稿, 草稿与最新发布不同)
func (m *AIToolVersionModel) DraftStateMap() (map[int64]bool, map[int64]bool, error) {
	hasDraft := make(map[int64]bool)
	differs := make(map[int64]bool)
	rows, err := m.DB.Query(
		`SELECT d.tool_id,
		        (SELECT COALESCE(MAX(p.snapshot), '') FROM ai_tool_versions p
		          WHERE p.tool_id = d.tool_id AND p.status = 'published') AS published_snapshot,
		        d.snapshot
		 FROM ai_tool_versions d WHERE d.status = 'draft'`)
	if err != nil {
		return hasDraft, differs, err
	}
	defer rows.Close()
	for rows.Next() {
		var toolID int64
		var publishedSnapshot, draftSnapshot string
		if err := rows.Scan(&toolID, &publishedSnapshot, &draftSnapshot); err != nil {
			continue
		}
		hasDraft[toolID] = true
		differs[toolID] = normalizeAIToolSnapshotJSON(publishedSnapshot) != normalizeAIToolSnapshotJSON(draftSnapshot)
	}
	return hasDraft, differs, rows.Err()
}

// normalizeAIToolSnapshotJSON 序列化无关的比较：解析后按 key 排序重新 marshal，
// 避免字段顺序差异造成"有未发布修改"误报。
func normalizeAIToolSnapshotJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return trimmed
	}
	canonical, err := json.Marshal(parsed)
	if err != nil {
		return trimmed
	}
	return string(canonical)
}

// ───────────────────────── 快照 diff（纯函数，便于单测） ─────────────────────────

// AIToolDiffItem 单字段差异
type AIToolDiffItem struct {
	Field  string `json:"field"`
	Label  string `json:"label"`
	Kind   string `json:"kind"` // changed / added / removed
	Before string `json:"before"`
	After  string `json:"after"`
}

var aiToolDiffFieldLabels = map[string]string{
	"name":                 "工具名称",
	"category":             "分类",
	"short_description":    "短描述",
	"detail_description":   "详细描述",
	"list_cover_image":     "列表封面图",
	"detail_before_image":  "详情前图",
	"detail_after_image":   "详情后图",
	"prompt_placeholder":   "输入框占位文案",
	"default_prompt":       "默认提示词",
	"upload_hint":          "上传提示文案",
	"show_usage_tips":      "显示使用提示",
	"usage_tips_title":     "提示标题",
	"usage_tips_content":   "提示内容",
	"sort_order":           "排序",
	"is_published":         "上线可见",
	"is_common":            "常用标记",
	"style_presets":        "风格卡片",
	"preset_references":    "旧版兼容项",
}

// DiffAIToolSnapshots 比较线上快照与草稿快照，返回人可读的差异列表。
// published 为 nil/空 表示尚未发布过（全部视为新增）。
func DiffAIToolSnapshots(published, draft map[string]interface{}) []AIToolDiffItem {
	items := make([]AIToolDiffItem, 0)
	for field, label := range aiToolDiffFieldLabels {
		beforeVal, hasBefore := lookupSnapshotField(published, field)
		afterVal, hasAfter := lookupSnapshotField(draft, field)
		switch {
		case !hasAfter && !hasBefore:
			continue
		case !hasBefore:
			items = append(items, AIToolDiffItem{Field: field, Label: label, Kind: "added", Before: "", After: snapshotValueText(field, afterVal)})
		case !hasAfter:
			items = append(items, AIToolDiffItem{Field: field, Label: label, Kind: "removed", Before: snapshotValueText(field, beforeVal), After: ""})
		default:
			beforeText := snapshotValueText(field, beforeVal)
			afterText := snapshotValueText(field, afterVal)
			if beforeText != afterText {
				items = append(items, AIToolDiffItem{Field: field, Label: label, Kind: "changed", Before: beforeText, After: afterText})
			}
		}
	}
	// 固定顺序输出：按 label 排序，便于阅读
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].Label < items[i].Label {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	return items
}

func lookupSnapshotField(snapshot map[string]interface{}, field string) (interface{}, bool) {
	if snapshot == nil {
		return nil, false
	}
	value, ok := snapshot[field]
	return value, ok
}

// snapshotValueText 数组字段（风格卡片等）摘要为"数量 + 名称清单"，标量字段原样转字符串
func snapshotValueText(field string, value interface{}) string {
	switch field {
	case "style_presets", "preset_references":
		list, ok := value.([]interface{})
		if !ok {
			return formatSnapshotScalar(value)
		}
		names := make([]string, 0, len(list))
		for _, item := range list {
			if m, ok := item.(map[string]interface{}); ok {
				if name, ok := m["name"].(string); ok && strings.TrimSpace(name) != "" {
					names = append(names, name)
					continue
				}
			}
			names = append(names, "?")
		}
		return strings.Join(names, "、")
	default:
		return formatSnapshotScalar(value)
	}
}

func formatSnapshotScalar(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "是"
		}
		return "否"
	case float64:
		return trimFloat(v)
	default:
		text, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(text)
	}
}

func trimFloat(v float64) string {
	text := strings.TrimRight(strings.TrimRight(jsonNumberFormat(v), "0"), ".")
	if text == "" {
		return "0"
	}
	return text
}

func jsonNumberFormat(v float64) string {
	if v == float64(int64(v)) {
		return jsonIntText(int64(v))
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func jsonIntText(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
