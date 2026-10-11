package model

import (
	"database/sql"
	"time"
)

// ───────────────────────── AI 生成内容抽检（二期批次 4a） ─────────────────────────
// 成功的生成结果按比例（ai_ops_settings.audit_sample_rate）随机抽入本表待人工复核。
// 只做"抽检记录 + 审核结论"，不拦截用户结果；驳回仅记录并支持转工单跟进。

const (
	AIContentAuditStatusPending  = "pending"
	AIContentAuditStatusApproved = "approved"
	AIContentAuditStatusRejected = "rejected"
)

// AIContentAudit 一条抽检记录
type AIContentAudit struct {
	ID         int64      `json:"id"`
	TaskNo     string     `json:"task_no"`
	TaskType   string     `json:"task_type"` // image / video
	UserID     int64      `json:"user_id"`
	Username   string     `json:"username"`
	Scene      string     `json:"scene"`
	Model      string     `json:"model"`
	ResultURL  string     `json:"result_url"`
	Status     string     `json:"status"` // pending / approved / rejected
	Reviewer   string     `json:"reviewer"`
	ReviewNote string     `json:"review_note"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// AIContentAuditModel 抽检数据访问层
type AIContentAuditModel struct {
	DB *sql.DB
}

func NewAIContentAuditModel(db *sql.DB) *AIContentAuditModel {
	return &AIContentAuditModel{DB: db}
}

func (m *AIContentAuditModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS ai_content_audits (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	task_no VARCHAR(64) NOT NULL,
	task_type VARCHAR(16) NOT NULL DEFAULT 'image',
	user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
	scene VARCHAR(64) NOT NULL DEFAULT '',
	model VARCHAR(128) NOT NULL DEFAULT '',
	result_url TEXT,
	status VARCHAR(16) NOT NULL DEFAULT 'pending',
	reviewer VARCHAR(64) NOT NULL DEFAULT '',
	review_note VARCHAR(500) NOT NULL DEFAULT '',
	reviewed_at TIMESTAMP NULL DEFAULT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE INDEX idx_task_no (task_no),
	INDEX idx_status_created (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	return err
}

// Sampled 抽检判定（纯函数便于单测）：enabled 且 roll < rate 时命中
func AIContentAuditSampled(enabled bool, rate float64, roll float64) bool {
	if !enabled || rate <= 0 {
		return false
	}
	return roll < rate
}

// Create 入队一条抽检（同一任务只入一次；写入失败由调用方吞掉，不影响任务结果）
func (m *AIContentAuditModel) Create(a *AIContentAudit) error {
	_, err := m.DB.Exec(
		`INSERT IGNORE INTO ai_content_audits (task_no, task_type, user_id, scene, model, result_url) VALUES (?, ?, ?, ?, ?, ?)`,
		a.TaskNo, a.TaskType, a.UserID, a.Scene, a.Model, a.ResultURL)
	return err
}

// AIContentAuditManagementItem 管理列表项
type AIContentAuditManagementItem struct {
	ID         int64      `json:"id"`
	TaskNo     string     `json:"task_no"`
	TaskType   string     `json:"task_type"`
	UserID     int64      `json:"user_id"`
	Username   string     `json:"username"`
	Scene      string     `json:"scene"`
	Model      string     `json:"model"`
	ResultURL  string     `json:"result_url"`
	Status     string     `json:"status"`
	Reviewer   string     `json:"reviewer"`
	ReviewNote string     `json:"review_note"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ListForManagement 审核列表（status: pending/approved/rejected/all；keyword 搜任务号/用户名）
func (m *AIContentAuditModel) ListForManagement(status, keyword string, limit, offset int) ([]*AIContentAuditManagementItem, int64, error) {
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	switch status {
	case AIContentAuditStatusPending, AIContentAuditStatusApproved, AIContentAuditStatusRejected:
		where = append(where, "a.status = ?")
		args = append(args, status)
	}
	if keyword = trimAuditKeyword(keyword); keyword != "" {
		where = append(where, "(a.task_no LIKE ? OR u.username LIKE ? OR CAST(a.user_id AS CHAR) LIKE ?)")
		like := "%" + keyword + "%"
		args = append(args, like, like, like)
	}
	whereSQL := " WHERE " + joinAuditWhere(where)
	countQuery := `SELECT COUNT(*) FROM ai_content_audits a LEFT JOIN users u ON u.id = a.user_id` + whereSQL
	var total int64
	if err := m.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	query := `
		SELECT a.id, a.task_no, a.task_type, a.user_id, COALESCE(u.username, ''), a.scene, a.model,
		       COALESCE(a.result_url, ''), a.status, COALESCE(a.reviewer, ''), COALESCE(a.review_note, ''), a.reviewed_at, a.created_at
		FROM ai_content_audits a
		LEFT JOIN users u ON u.id = a.user_id` + whereSQL + `
		ORDER BY a.created_at DESC
		LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]*AIContentAuditManagementItem, 0)
	for rows.Next() {
		item := &AIContentAuditManagementItem{}
		if err := rows.Scan(&item.ID, &item.TaskNo, &item.TaskType, &item.UserID, &item.Username, &item.Scene, &item.Model,
			&item.ResultURL, &item.Status, &item.Reviewer, &item.ReviewNote, &item.ReviewedAt, &item.CreatedAt); err != nil {
			continue
		}
		list = append(list, item)
	}
	return list, total, rows.Err()
}

func trimAuditKeyword(keyword string) string {
	for len(keyword) > 0 && (keyword[0] == ' ' || keyword[0] == '\t') {
		keyword = keyword[1:]
	}
	for len(keyword) > 0 && (keyword[len(keyword)-1] == ' ' || keyword[len(keyword)-1] == '\t') {
		keyword = keyword[:len(keyword)-1]
	}
	return keyword
}

func joinAuditWhere(parts []string) string {
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += " AND "
		}
		result += part
	}
	return result
}

// Review 审核结论（pending 状态才能改；重复审核返回 false）
func (m *AIContentAuditModel) Review(id int64, status AIContentAuditStatus, reviewer, note string) (bool, error) {
	if status != AIContentAuditStatusApproved && status != AIContentAuditStatusRejected {
		return false, nil
	}
	result, err := m.DB.Exec(
		`UPDATE ai_content_audits SET status = ?, reviewer = ?, review_note = ?, reviewed_at = NOW()
		 WHERE id = ? AND status = 'pending'`,
		string(status), reviewer, note, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// PendingCount 待审数量（总览页展示）
func (m *AIContentAuditModel) PendingCount() (int64, error) {
	var count int64
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM ai_content_audits WHERE status = 'pending'`).Scan(&count)
	return count, err
}

// AIContentAuditStatus 审核状态取值
type AIContentAuditStatus string
