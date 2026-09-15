package model

import (
	"fmt"
	"database/sql"
	"strings"
	"time"
)

// AIVideoTask AI 生成视频任务（对接 laozhang.ai 文本/图生视频，结果存自家 OSS）
type AIVideoTask struct {
	ID          int64     `json:"id" db:"id"`
	UserID      int64     `json:"user_id" db:"user_id"`
	ExternalID  string    `json:"external_id" db:"external_id"`   // 第三方任务 id，如 video_abc123
	Model       string    `json:"model" db:"model"`              // veo-3.1-landscape-fl 横屏 / veo-3.1-fl 竖屏
	Prompt      string    `json:"prompt" db:"prompt"`
	Status      string    `json:"status" db:"status"`           // queued, processing, completed, failed
	OSSURL       string         `json:"oss_url" db:"oss_url"`       // 存到自家 COS 后的访问地址
	Duration     int            `json:"duration" db:"duration"`    // 视频时长秒
	Resolution   string         `json:"resolution" db:"resolution"`   // 如 720p
	SegmentCount int            `json:"segment_count" db:"segment_count"` // 连续生成段数，1=单段
	ErrorMessage sql.NullString `json:"-" db:"error_message"`          // 错误信息（可为 NULL）
	RawErrorMessage sql.NullString `json:"-" db:"raw_error_message"`    // 第三方原始错误信息（可为 NULL）
	CreatedAt    time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type AIVideoTaskManagementItem struct {
	ID           int64     `json:"id"`
	TaskNo       string    `json:"task_no"`
	UserID       int64     `json:"user_id"`
	Username     string    `json:"username"`
	Model        string    `json:"model"`
	Prompt       string    `json:"prompt"`
	Status       string    `json:"status"`
	SegmentCount int       `json:"segment_count"`
	Duration     int       `json:"duration"`
	Resolution   string    `json:"resolution"`
	ErrorMessage string    `json:"error_message"`
	RawErrorMessage string `json:"raw_error_message"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AIVideoTaskModel 数据访问
type AIVideoTaskModel struct {
	DB *sql.DB
}

func NewAIVideoTaskModel(db *sql.DB) *AIVideoTaskModel {
	return &AIVideoTaskModel{DB: db}
}

func (m *AIVideoTaskModel) Create(t *AIVideoTask) error {
	seg := t.SegmentCount
	if seg <= 0 {
		seg = 1
	}
	q := `INSERT INTO ai_video_tasks (user_id, external_id, model, prompt, status, duration, resolution, segment_count, created_at, updated_at)
	      VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`
	normalizedStatus := NormalizeAIVideoStatus(t.Status)
	resolution := strings.TrimSpace(t.Resolution)
	res, err := m.DB.Exec(q, t.UserID, t.ExternalID, t.Model, t.Prompt, normalizedStatus, t.Duration, resolution, seg)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	t.ID = id
	t.Status = normalizedStatus
	t.Resolution = resolution
	t.CreatedAt = time.Now()
	t.UpdatedAt = t.CreatedAt
	return nil
}

func (m *AIVideoTaskModel) GetByID(id int64) (*AIVideoTask, error) {
	q := `SELECT id, user_id, external_id, model, prompt, status, oss_url, duration, resolution, segment_count, error_message, raw_error_message, created_at, updated_at
	      FROM ai_video_tasks WHERE id = ?`
	t := &AIVideoTask{}
	err := m.DB.QueryRow(q, id).Scan(
		&t.ID, &t.UserID, &t.ExternalID, &t.Model, &t.Prompt, &t.Status,
		&t.OSSURL, &t.Duration, &t.Resolution, &t.SegmentCount, &t.ErrorMessage, &t.RawErrorMessage, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.Status = NormalizeAIVideoStatus(t.Status)
	return t, nil
}

// GetErrorMessage 获取错误信息（兼容 error_message 为 NULL）
func (t *AIVideoTask) GetErrorMessage() string {
	if t.ErrorMessage.Valid {
		return t.ErrorMessage.String
	}
	return ""
}

func (t *AIVideoTask) GetRawErrorMessage() string {
	if t.RawErrorMessage.Valid {
		return t.RawErrorMessage.String
	}
	return ""
}

func (m *AIVideoTaskModel) GetByIDAndUserID(id int64, userID int64) (*AIVideoTask, error) {
	q := `SELECT id, user_id, external_id, model, prompt, status, oss_url, duration, resolution, segment_count, error_message, raw_error_message, created_at, updated_at
	      FROM ai_video_tasks WHERE id = ? AND user_id = ?`
	t := &AIVideoTask{}
	err := m.DB.QueryRow(q, id, userID).Scan(
		&t.ID, &t.UserID, &t.ExternalID, &t.Model, &t.Prompt, &t.Status,
		&t.OSSURL, &t.Duration, &t.Resolution, &t.SegmentCount, &t.ErrorMessage, &t.RawErrorMessage, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.Status = NormalizeAIVideoStatus(t.Status)
	return t, nil
}

func (m *AIVideoTaskModel) UpdateStatus(id int64, status string, errMsg string) error {
	return m.UpdateStatusWithRaw(id, status, errMsg, errMsg)
}

func (m *AIVideoTaskModel) UpdateStatusWithRaw(id int64, status string, errMsg string, rawErrMsg string) error {
	status = NormalizeAIVideoStatus(status)
	q := `UPDATE ai_video_tasks SET status = ?, error_message = ?, raw_error_message = ?, updated_at = NOW() WHERE id = ?`
	var em interface{}
	if errMsg != "" {
		em = errMsg
	}
	var rem interface{}
	if rawErrMsg != "" {
		rem = rawErrMsg
	}
	_, err := m.DB.Exec(q, status, em, rem, id)
	return err
}

func (m *AIVideoTaskModel) UpdateStatusIfCurrentIn(id int64, currentStatuses []string, status string, errMsg string) (bool, error) {
	return m.UpdateStatusIfCurrentInWithRaw(id, currentStatuses, status, errMsg, errMsg)
}

func (m *AIVideoTaskModel) UpdateStatusIfCurrentInWithRaw(id int64, currentStatuses []string, status string, errMsg string, rawErrMsg string) (bool, error) {
	expandedStatuses := make([]string, 0, len(currentStatuses)*2)
	for _, currentStatus := range currentStatuses {
		expandedStatuses = append(expandedStatuses, ExpandAIVideoStatusFilter(currentStatus)...)
	}
	expandedStatuses = uniqueAIVideoStatuses(expandedStatuses)
	if len(expandedStatuses) == 0 {
		return false, nil
	}
	status = NormalizeAIVideoStatus(status)
	placeholders := strings.TrimRight(strings.Repeat("?,", len(expandedStatuses)), ",")
	q := fmt.Sprintf(`UPDATE ai_video_tasks SET status = ?, error_message = ?, raw_error_message = ?, updated_at = NOW() WHERE id = ? AND status IN (%s)`, placeholders)
	var em interface{}
	if errMsg != "" {
		em = errMsg
	}
	var rem interface{}
	if rawErrMsg != "" {
		rem = rawErrMsg
	}
	args := make([]interface{}, 0, 4+len(expandedStatuses))
	args = append(args, status, em, rem, id)
	for _, currentStatus := range expandedStatuses {
		args = append(args, currentStatus)
	}
	res, err := m.DB.Exec(q, args...)
	if err != nil {
		return false, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (m *AIVideoTaskModel) UpdateOSSURL(id int64, ossURL string, duration int, resolution string) error {
	q := `UPDATE ai_video_tasks SET oss_url = ?, duration = ?, resolution = ?, status = ?, updated_at = NOW() WHERE id = ?`
	_, err := m.DB.Exec(q, ossURL, duration, resolution, AIVideoStatusCompleted, id)
	return err
}

func (m *AIVideoTaskModel) ListActiveForMonitoring(limit int) ([]*AIVideoTask, error) {
	if limit <= 0 {
		limit = 100
	}
	monitorStatuses := AIVideoActiveMonitoringStatuses()
	placeholders := strings.TrimRight(strings.Repeat("?,", len(monitorStatuses)), ",")
	q := fmt.Sprintf(`SELECT id, user_id, external_id, model, prompt, status, oss_url, duration, resolution, segment_count, error_message, raw_error_message, created_at, updated_at
	      FROM ai_video_tasks
	      WHERE status IN (%s) AND COALESCE(oss_url, '') = ''
	      ORDER BY updated_at ASC
	      LIMIT ?`, placeholders)
	args := make([]interface{}, 0, len(monitorStatuses)+1)
	for _, status := range monitorStatuses {
		args = append(args, status)
	}
	args = append(args, limit)
	rows, err := m.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]*AIVideoTask, 0)
	for rows.Next() {
		t := &AIVideoTask{}
		if err := rows.Scan(&t.ID, &t.UserID, &t.ExternalID, &t.Model, &t.Prompt, &t.Status,
			&t.OSSURL, &t.Duration, &t.Resolution, &t.SegmentCount, &t.ErrorMessage, &t.RawErrorMessage, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Status = NormalizeAIVideoStatus(t.Status)
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

// GetByUserID 按用户分页查询视频任务，按创建时间倒序
func (m *AIVideoTaskModel) GetByUserID(userID int64, limit, offset int) ([]*AIVideoTask, error) {
	q := `SELECT id, user_id, external_id, model, prompt, status, oss_url, duration, resolution, segment_count, error_message, raw_error_message, created_at, updated_at
	      FROM ai_video_tasks WHERE user_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(q, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*AIVideoTask
	for rows.Next() {
		t := &AIVideoTask{}
		err := rows.Scan(&t.ID, &t.UserID, &t.ExternalID, &t.Model, &t.Prompt, &t.Status,
			&t.OSSURL, &t.Duration, &t.Resolution, &t.SegmentCount, &t.ErrorMessage, &t.RawErrorMessage, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			continue
		}
		t.Status = NormalizeAIVideoStatus(t.Status)
		list = append(list, t)
	}
	return list, nil
}

// CountByUserID 用户视频任务总数
func (m *AIVideoTaskModel) CountByUserID(userID int64) (int64, error) {
	var n int64
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM ai_video_tasks WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// DeleteByID 按 id 删除（调用方需校验 user_id）
func (m *AIVideoTaskModel) DeleteByID(id int64) error {
	_, err := m.DB.Exec(`DELETE FROM ai_video_tasks WHERE id = ?`, id)
	return err
}

func (m *AIVideoTaskModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS ai_video_tasks (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	user_id BIGINT UNSIGNED NOT NULL,
	external_id VARCHAR(128) NOT NULL,
	model VARCHAR(64) NOT NULL,
	prompt TEXT NOT NULL,
	status VARCHAR(32) NOT NULL DEFAULT 'queued',
	oss_url VARCHAR(512) NOT NULL DEFAULT '',
	duration INT UNSIGNED NOT NULL DEFAULT 0,
	resolution VARCHAR(32) NOT NULL DEFAULT '',
	segment_count INT UNSIGNED NOT NULL DEFAULT 1,
	error_message TEXT,
	raw_error_message TEXT,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_user_id (user_id),
	INDEX idx_external_id (external_id),
	INDEX idx_status (status),
	INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	// 兼容旧表：若无 segment_count 则添加
	_, _ = m.DB.Exec("ALTER TABLE ai_video_tasks ADD COLUMN segment_count INT UNSIGNED NOT NULL DEFAULT 1")
	_, _ = m.DB.Exec("ALTER TABLE ai_video_tasks ADD COLUMN raw_error_message TEXT")
	return nil
}

func (m *AIVideoTaskModel) buildManagementWhere(keyword, status string) (string, []interface{}) {
	where := []string{"u.user_type = 'miniprogram'"}
	args := make([]interface{}, 0)
	if keyword != "" {
		likeKeyword := "%" + keyword + "%"
		where = append(where, "(v.external_id LIKE ? OR CAST(v.user_id AS CHAR) LIKE ? OR u.username LIKE ? OR v.prompt LIKE ?)")
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}
	if status != "" && status != "all" {
		statusFilters := ExpandAIVideoStatusFilter(status)
		if len(statusFilters) == 1 {
			where = append(where, "v.status = ?")
			args = append(args, statusFilters[0])
		} else if len(statusFilters) > 1 {
			placeholders := strings.TrimRight(strings.Repeat("?,", len(statusFilters)), ",")
			where = append(where, fmt.Sprintf("v.status IN (%s)", placeholders))
			for _, item := range statusFilters {
				args = append(args, item)
			}
		}
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

func (m *AIVideoTaskModel) ListForManagement(keyword, status string, limit, offset int) ([]*AIVideoTaskManagementItem, int64, error) {
	whereSQL, args := m.buildManagementWhere(keyword, status)
	countQuery := `SELECT COUNT(*) FROM ai_video_tasks v LEFT JOIN users u ON u.id = v.user_id` + whereSQL
	var total int64
	if err := m.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	query := `
		SELECT v.id, CONCAT('v', v.id), v.user_id, COALESCE(u.username, ''), v.model, v.prompt, v.status, v.segment_count, v.duration, v.resolution, COALESCE(v.error_message, ''), COALESCE(v.raw_error_message, ''), v.created_at, v.updated_at
		FROM ai_video_tasks v
		LEFT JOIN users u ON u.id = v.user_id` + whereSQL + `
		ORDER BY v.created_at DESC
		LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]*AIVideoTaskManagementItem, 0)
	for rows.Next() {
		item := &AIVideoTaskManagementItem{}
		if err := rows.Scan(&item.ID, &item.TaskNo, &item.UserID, &item.Username, &item.Model, &item.Prompt, &item.Status, &item.SegmentCount, &item.Duration, &item.Resolution, &item.ErrorMessage, &item.RawErrorMessage, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		item.Status = NormalizeAIVideoStatus(item.Status)
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (m *AIVideoTaskModel) SummaryForManagement(keyword, status string) (totalCount, queuedCount, processingCount, failedCount int64, err error) {
	whereSQL, args := m.buildManagementWhere(keyword, status)
	processingStatuses := ExpandAIVideoStatusFilter(AIVideoStatusProcessing)
	processingPlaceholders := strings.TrimRight(strings.Repeat("?,", len(processingStatuses)), ",")
	query := fmt.Sprintf(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN v.status = 'queued' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN v.status IN (%s) THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN v.status = 'failed' THEN 1 ELSE 0 END), 0)
		FROM ai_video_tasks v
		LEFT JOIN users u ON u.id = v.user_id`, processingPlaceholders) + whereSQL
	queryArgs := make([]interface{}, 0, len(processingStatuses)+len(args))
	for _, item := range processingStatuses {
		queryArgs = append(queryArgs, item)
	}
	queryArgs = append(queryArgs, args...)
	err = m.DB.QueryRow(query, queryArgs...).Scan(&totalCount, &queuedCount, &processingCount, &failedCount)
	return
}
