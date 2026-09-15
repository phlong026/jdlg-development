package model

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type SupportTicket struct {
	ID           int64      `json:"id"`
	UserID       int64      `json:"user_id"`
	Username     string     `json:"username"`
	Nickname     string     `json:"nickname"`
	Type         string     `json:"type"`
	SourceType   string     `json:"source_type"`
	SourceID     string     `json:"source_id"`
	Title        string     `json:"title"`
	Content      string     `json:"content"`
	ResolutionNote string   `json:"resolution_note"`
	Priority     string     `json:"priority"`
	Status       string     `json:"status"`
	AssigneeID   int64      `json:"assignee_id"`
	AssigneeName string     `json:"assignee_name"`
	CreatedBy    string     `json:"created_by"`
	SourcePayload string    `json:"source_payload"`
	ClosedAt     *time.Time `json:"closed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type SupportTicketOverview struct {
	TotalCount      int64 `json:"total_count"`
	OpenCount       int64 `json:"open_count"`
	InProgressCount int64 `json:"in_progress_count"`
	ClosedCount     int64 `json:"closed_count"`
	HighPriorityCount int64 `json:"high_priority_count"`
}

type SupportTicketListParams struct {
	Keyword    string
	Status     string
	Type       string
	SourceType string
	Limit      int
	Offset     int
}

type SupportTicketCreateInput struct {
	UserID        int64
	Type          string
	SourceType    string
	SourceID      string
	Title         string
	Content       string
	Priority      string
	CreatedBy     string
	SourcePayload map[string]interface{}
}

type SupportTicketModel struct {
	DB *sql.DB
}

func NewSupportTicketModel(db *sql.DB) *SupportTicketModel {
	return &SupportTicketModel{DB: db}
}

func (m *SupportTicketModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS support_tickets (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  type VARCHAR(32) NOT NULL DEFAULT 'complaint',
  source_type VARCHAR(32) NOT NULL DEFAULT 'manual',
  source_id VARCHAR(64) NOT NULL DEFAULT '',
  title VARCHAR(255) NOT NULL DEFAULT '',
  content TEXT,
	resolution_note TEXT,
  priority VARCHAR(32) NOT NULL DEFAULT 'medium',
  status VARCHAR(32) NOT NULL DEFAULT 'open',
  assignee_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  assignee_name VARCHAR(64) NOT NULL DEFAULT '',
  created_by VARCHAR(64) NOT NULL DEFAULT '',
  source_payload JSON NULL,
  closed_at DATETIME NULL DEFAULT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_user_id (user_id),
  INDEX idx_status (status),
  INDEX idx_type (type),
  INDEX idx_source_type (source_type),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE support_tickets ADD COLUMN assignee_id BIGINT UNSIGNED NOT NULL DEFAULT 0`)
	_, _ = m.DB.Exec(`ALTER TABLE support_tickets ADD COLUMN assignee_name VARCHAR(64) NOT NULL DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE support_tickets ADD COLUMN created_by VARCHAR(64) NOT NULL DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE support_tickets ADD COLUMN source_payload JSON NULL`)
	_, _ = m.DB.Exec(`ALTER TABLE support_tickets ADD COLUMN closed_at DATETIME NULL DEFAULT NULL`)
	_, _ = m.DB.Exec(`ALTER TABLE support_tickets ADD COLUMN resolution_note TEXT`)
	return nil
}

func (m *SupportTicketModel) Create(input *SupportTicketCreateInput) (int64, error) {
	priority := normalizeSupportTicketPriority(input.Priority)
	status := "open"
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = "manual"
	}
	payloadValue, err := marshalSupportTicketPayload(input.SourcePayload)
	if err != nil {
		return 0, err
	}
	result, err := m.DB.Exec(
		`INSERT INTO support_tickets (user_id, type, source_type, source_id, title, content, resolution_note, priority, status, assignee_id, assignee_name, created_by, source_payload, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, 0, '', ?, ?, NOW(), NOW())`,
		input.UserID,
		normalizeSupportTicketType(input.Type),
		sourceType,
		strings.TrimSpace(input.SourceID),
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Content),
		priority,
		status,
		strings.TrimSpace(input.CreatedBy),
		payloadValue,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (m *SupportTicketModel) ExistsOpenBySource(sourceType, sourceID string) (bool, error) {
	var count int64
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM support_tickets WHERE source_type = ? AND source_id = ? AND status IN ('open', 'in_progress')`, sourceType, sourceID).Scan(&count)
	return count > 0, err
}

func (m *SupportTicketModel) GetLatestOpenTicketIDBySource(sourceType, sourceID string) (int64, error) {
	var id int64
	err := m.DB.QueryRow(`
		SELECT id
		FROM support_tickets
		WHERE source_type = ? AND source_id = ? AND status IN ('open', 'in_progress')
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, sourceType, sourceID).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (m *SupportTicketModel) GetByID(id int64) (*SupportTicket, error) {
	query := `
		SELECT t.id, t.user_id, COALESCE(u.username, ''), COALESCE(p.nickname, ''), t.type, t.source_type, t.source_id, t.title, t.content, COALESCE(t.resolution_note, ''),
		       t.priority, t.status, t.assignee_id, t.assignee_name, t.created_by, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(t.source_payload, '$')), ''),
		       t.closed_at, t.created_at, t.updated_at
		FROM support_tickets t
		LEFT JOIN users u ON u.id = t.user_id
		LEFT JOIN user_profiles p ON p.user_id = t.user_id
		WHERE t.id = ?`
	item := &SupportTicket{}
	var closedAt sql.NullTime
	if err := m.DB.QueryRow(query, id).Scan(
		&item.ID,
		&item.UserID,
		&item.Username,
		&item.Nickname,
		&item.Type,
		&item.SourceType,
		&item.SourceID,
		&item.Title,
		&item.Content,
		&item.ResolutionNote,
		&item.Priority,
		&item.Status,
		&item.AssigneeID,
		&item.AssigneeName,
		&item.CreatedBy,
		&item.SourcePayload,
		&closedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if closedAt.Valid {
		item.ClosedAt = &closedAt.Time
	}
	return item, nil
}

func (m *SupportTicketModel) ListForManagement(params SupportTicketListParams) ([]*SupportTicket, int64, error) {
	where := []string{"1=1"}
	args := make([]interface{}, 0)
	keyword := strings.TrimSpace(params.Keyword)
	if keyword != "" {
		likeKeyword := "%" + keyword + "%"
		where = append(where, `(u.username LIKE ? OR COALESCE(p.nickname, '') LIKE ? OR t.title LIKE ? OR t.content LIKE ? OR t.source_id LIKE ?)`)
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}
	if value := strings.TrimSpace(params.Status); value != "" && value != "all" {
		where = append(where, `t.status = ?`)
		args = append(args, value)
	}
	if value := strings.TrimSpace(params.Type); value != "" && value != "all" {
		where = append(where, `t.type = ?`)
		args = append(args, value)
	}
	if value := strings.TrimSpace(params.SourceType); value != "" && value != "all" {
		where = append(where, `t.source_type = ?`)
		args = append(args, value)
	}
	whereSQL := strings.Join(where, " AND ")
	countQuery := `SELECT COUNT(*) FROM support_tickets t LEFT JOIN users u ON u.id = t.user_id LEFT JOIN user_profiles p ON p.user_id = t.user_id WHERE ` + whereSQL
	var total int64
	if err := m.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `
		SELECT t.id, t.user_id, COALESCE(u.username, ''), COALESCE(p.nickname, ''), t.type, t.source_type, t.source_id, t.title, t.content, COALESCE(t.resolution_note, ''),
		       t.priority, t.status, t.assignee_id, t.assignee_name, t.created_by, COALESCE(JSON_UNQUOTE(JSON_EXTRACT(t.source_payload, '$')), ''),
		       t.closed_at, t.created_at, t.updated_at
		FROM support_tickets t
		LEFT JOIN users u ON u.id = t.user_id
		LEFT JOIN user_profiles p ON p.user_id = t.user_id
		WHERE ` + whereSQL + `
		ORDER BY FIELD(t.status, 'open', 'in_progress', 'closed'), FIELD(t.priority, 'high', 'medium', 'low'), t.created_at DESC
		LIMIT ? OFFSET ?`
	queryArgs := append(append([]interface{}{}, args...), params.Limit, params.Offset)
	rows, err := m.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]*SupportTicket, 0, params.Limit)
	for rows.Next() {
		item := &SupportTicket{}
		var closedAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.Username,
			&item.Nickname,
			&item.Type,
			&item.SourceType,
			&item.SourceID,
			&item.Title,
			&item.Content,
			&item.ResolutionNote,
			&item.Priority,
			&item.Status,
			&item.AssigneeID,
			&item.AssigneeName,
			&item.CreatedBy,
			&item.SourcePayload,
			&closedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		if closedAt.Valid {
			item.ClosedAt = &closedAt.Time
		}
		list = append(list, item)
	}
	return list, total, rows.Err()
}

func (m *SupportTicketModel) Overview() (*SupportTicketOverview, error) {
	result := &SupportTicketOverview{}
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM support_tickets`).Scan(&result.TotalCount); err != nil {
		return nil, err
	}
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM support_tickets WHERE status = 'open'`).Scan(&result.OpenCount); err != nil {
		return nil, err
	}
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM support_tickets WHERE status = 'in_progress'`).Scan(&result.InProgressCount); err != nil {
		return nil, err
	}
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM support_tickets WHERE status = 'closed'`).Scan(&result.ClosedCount); err != nil {
		return nil, err
	}
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM support_tickets WHERE priority = 'high' AND status <> 'closed'`).Scan(&result.HighPriorityCount); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *SupportTicketModel) UpdateStatus(id int64, status string) error {
	normalized := normalizeSupportTicketStatus(status)
	if normalized == "closed" {
		_, err := m.DB.Exec(`UPDATE support_tickets SET status = ?, closed_at = NOW(), updated_at = NOW() WHERE id = ?`, normalized, id)
		return err
	}
	_, err := m.DB.Exec(`UPDATE support_tickets SET status = ?, closed_at = NULL, updated_at = NOW() WHERE id = ?`, normalized, id)
	return err
}

func (m *SupportTicketModel) Assign(id int64, assigneeID int64, assigneeName string) error {
	_, err := m.DB.Exec(`UPDATE support_tickets SET assignee_id = ?, assignee_name = ?, status = CASE WHEN status = 'open' THEN 'in_progress' ELSE status END, updated_at = NOW() WHERE id = ?`, assigneeID, strings.TrimSpace(assigneeName), id)
	return err
}

func (m *SupportTicketModel) UpdateResolutionNote(id int64, resolutionNote string) error {
	_, err := m.DB.Exec(`UPDATE support_tickets SET resolution_note = ?, updated_at = NOW() WHERE id = ?`, strings.TrimSpace(resolutionNote), id)
	return err
}

func normalizeSupportTicketType(value string) string {
	switch strings.TrimSpace(value) {
	case "order", "task", "complaint", "certification":
		return strings.TrimSpace(value)
	default:
		return "complaint"
	}
}

func normalizeSupportTicketPriority(value string) string {
	switch strings.TrimSpace(value) {
	case "high", "medium", "low":
		return strings.TrimSpace(value)
	default:
		return "medium"
	}
}

func normalizeSupportTicketStatus(value string) string {
	switch strings.TrimSpace(value) {
	case "open", "in_progress", "closed":
		return strings.TrimSpace(value)
	default:
		return "open"
	}
}

func marshalSupportTicketPayload(payload map[string]interface{}) (interface{}, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}
