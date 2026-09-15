package model

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	CustomerLeadStatusNew       = "new"
	CustomerLeadStatusContacted = "contacted"
	CustomerLeadStatusConverted = "converted"
	CustomerLeadStatusInvalid   = "invalid"

	CustomerIntentLevelLow    = "low"
	CustomerIntentLevelMedium = "medium"
	CustomerIntentLevelHigh   = "high"
)

var ErrInvalidCustomerLeadID = errors.New("invalid customer lead id")

type CustomerLead struct {
	ID                      int64      `json:"id"`
	UserID                  int64      `json:"user_id"`
	Name                    string     `json:"name"`
	Phone                   string     `json:"phone"`
	Wechat                  string     `json:"wechat"`
	EnterpriseWechatContact string     `json:"enterprise_wechat_contact"`
	DemandSummary           string     `json:"demand_summary"`
	HouseFloors             string     `json:"house_floors"`
	HouseStyle              string     `json:"house_style"`
	LandWidth               string     `json:"land_width"`
	LandDepth               string     `json:"land_depth"`
	RoomRequirement         string     `json:"room_requirement"`
	Source                  string     `json:"source"`
	SessionNo               string     `json:"session_no"`
	SourceTaskNo            string     `json:"source_task_no"`
	IntentLevel             string     `json:"intent_level"`
	Status                  string     `json:"status"`
	AssignedTo              string     `json:"assigned_to"`
	NextFollowupAt          *time.Time `json:"next_followup_at"`
	Remark                  string     `json:"remark"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type CustomerLeadCreateInput struct {
	UserID                  int64
	Name                    string
	Phone                   string
	Wechat                  string
	EnterpriseWechatContact string
	DemandSummary           string
	HouseFloors             string
	HouseStyle              string
	LandWidth               string
	LandDepth               string
	RoomRequirement         string
	Source                  string
	SessionNo               string
	SourceTaskNo            string
	IntentLevel             string
	Status                  string
	Remark                  string
}

type CustomerLeadListParams struct {
	Keyword     string
	Status      string
	IntentLevel string
	Source      string
	Queue       string
	AssignedTo  string
	Limit       int
	Offset      int
}

type CustomerLeadOverview struct {
	TotalCount      int64 `json:"total_count"`
	NewCount        int64 `json:"new_count"`
	ContactedCount  int64 `json:"contacted_count"`
	HighIntentCount int64 `json:"high_intent_count"`
	ConvertedCount  int64 `json:"converted_count"`
	TodayNewCount   int64 `json:"today_new_count"`
	TodoCount       int64 `json:"todo_count"`
	OverdueCount    int64 `json:"overdue_count"`
}

type CustomerFunnelMetric struct {
	Key            string  `json:"key"`
	Label          string  `json:"label"`
	Count          int64   `json:"count"`
	ConversionRate float64 `json:"conversion_rate"`
}

type CustomerServiceEventCreateInput struct {
	UserID        int64
	LeadID        int64
	SessionNo     string
	EventType     string
	Source        string
	SourceTaskNo  string
	IntentLevel   string
	DemandSummary string
	Payload       map[string]interface{}
}

type CustomerServiceEvent struct {
	ID            int64                  `json:"id"`
	UserID        int64                  `json:"user_id"`
	LeadID        int64                  `json:"lead_id"`
	SessionNo     string                 `json:"session_no"`
	EventType     string                 `json:"event_type"`
	Source        string                 `json:"source"`
	SourceTaskNo  string                 `json:"source_task_no"`
	IntentLevel   string                 `json:"intent_level"`
	DemandSummary string                 `json:"demand_summary"`
	Payload       map[string]interface{} `json:"payload"`
	CreatedAt     time.Time              `json:"created_at"`
}

type CustomerLeadFollowup struct {
	ID             int64      `json:"id"`
	LeadID         int64      `json:"lead_id"`
	Status         string     `json:"status"`
	Remark         string     `json:"remark"`
	AssignedTo     string     `json:"assigned_to"`
	NextFollowupAt *time.Time `json:"next_followup_at"`
	OperatorID     int64      `json:"operator_id"`
	OperatorName   string     `json:"operator_name"`
	CreatedAt      time.Time  `json:"created_at"`
}

type CustomerLeadFollowupCreateInput struct {
	LeadID         int64
	Status         string
	Remark         string
	AssignedTo     string
	NextFollowupAt *time.Time
	OperatorID     int64
	OperatorName   string
}

type CustomerLeadActionInput struct {
	Status         string
	Remark         string
	AssignedTo     string
	NextFollowupAt *time.Time
}

type CustomerIntentAnalysis struct {
	Level   string   `json:"level"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
}

type CustomerServiceModel struct {
	DB *sql.DB
}

func NewCustomerServiceModel(db *sql.DB) *CustomerServiceModel {
	return &CustomerServiceModel{DB: db}
}

func NormalizeCustomerLeadStatus(status string) string {
	switch strings.TrimSpace(status) {
	case CustomerLeadStatusNew, CustomerLeadStatusContacted, CustomerLeadStatusConverted, CustomerLeadStatusInvalid:
		return strings.TrimSpace(status)
	default:
		return CustomerLeadStatusNew
	}
}

func NormalizeCustomerIntentLevel(level string) string {
	switch strings.TrimSpace(level) {
	case CustomerIntentLevelLow, CustomerIntentLevelMedium, CustomerIntentLevelHigh:
		return strings.TrimSpace(level)
	default:
		return CustomerIntentLevelMedium
	}
}

func containsAnyText(text string, keywords ...string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	for _, keyword := range keywords {
		if strings.Contains(normalized, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func appendUniqueReason(reasons []string, reason string) []string {
	for _, item := range reasons {
		if item == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

func defaultText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func (m *CustomerServiceModel) InitTable() error {
	if _, err := m.DB.Exec(`
CREATE TABLE IF NOT EXISTS customer_leads (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  name VARCHAR(64) NOT NULL DEFAULT '',
  phone VARCHAR(32) NOT NULL DEFAULT '',
  wechat VARCHAR(128) NOT NULL DEFAULT '',
  enterprise_wechat_contact VARCHAR(128) NOT NULL DEFAULT '',
  demand_summary TEXT,
  house_floors VARCHAR(64) NOT NULL DEFAULT '',
  house_style VARCHAR(64) NOT NULL DEFAULT '',
  land_width VARCHAR(64) NOT NULL DEFAULT '',
  land_depth VARCHAR(64) NOT NULL DEFAULT '',
  room_requirement VARCHAR(255) NOT NULL DEFAULT '',
  source VARCHAR(64) NOT NULL DEFAULT '',
  session_no VARCHAR(128) NOT NULL DEFAULT '',
  source_task_no VARCHAR(128) NOT NULL DEFAULT '',
  intent_level VARCHAR(32) NOT NULL DEFAULT 'medium',
  status VARCHAR(32) NOT NULL DEFAULT 'new',
  assigned_to VARCHAR(64) NOT NULL DEFAULT '',
  next_followup_at DATETIME NULL,
  remark TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_user_id (user_id),
  INDEX idx_status (status),
  INDEX idx_intent_level (intent_level),
  INDEX idx_source_task_no (source_task_no),
  INDEX idx_session_no (session_no),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`); err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE customer_leads ADD COLUMN session_no VARCHAR(128) NOT NULL DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE customer_leads ADD INDEX idx_session_no (session_no)`)
	_, _ = m.DB.Exec(`ALTER TABLE customer_leads ADD COLUMN next_followup_at DATETIME NULL`)
	_, _ = m.DB.Exec(`ALTER TABLE customer_leads ADD INDEX idx_next_followup_at (next_followup_at)`)

	if _, err := m.DB.Exec(`
CREATE TABLE IF NOT EXISTS customer_lead_followups (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  lead_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  status VARCHAR(32) NOT NULL DEFAULT 'new',
  remark TEXT,
  assigned_to VARCHAR(64) NOT NULL DEFAULT '',
  next_followup_at DATETIME NULL,
  operator_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  operator_name VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_lead_id (lead_id),
  INDEX idx_next_followup_at (next_followup_at),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`); err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE customer_lead_followups ADD COLUMN assigned_to VARCHAR(64) NOT NULL DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE customer_lead_followups ADD COLUMN next_followup_at DATETIME NULL`)
	_, _ = m.DB.Exec(`ALTER TABLE customer_lead_followups ADD INDEX idx_next_followup_at (next_followup_at)`)

	if _, err := m.DB.Exec(`
CREATE TABLE IF NOT EXISTS customer_service_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  lead_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  session_no VARCHAR(128) NOT NULL DEFAULT '',
  event_type VARCHAR(64) NOT NULL DEFAULT '',
  source VARCHAR(64) NOT NULL DEFAULT '',
  source_task_no VARCHAR(128) NOT NULL DEFAULT '',
  intent_level VARCHAR(32) NOT NULL DEFAULT 'medium',
  demand_summary TEXT,
  payload JSON NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_user_id (user_id),
  INDEX idx_lead_id (lead_id),
  INDEX idx_event_type (event_type),
  INDEX idx_source_task_no (source_task_no),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`); err != nil {
		return err
	}

	return nil
}

func (m *CustomerServiceModel) CreateLead(input *CustomerLeadCreateInput) (int64, error) {
	input.IntentLevel = m.AnalyzeLeadIntentFromInput(input).Level
	if id, err := m.findExistingLeadID(input.UserID, input.SessionNo, input.SourceTaskNo); err != nil {
		return 0, err
	} else if id > 0 {
		if err := m.updateExistingLead(id, input); err != nil {
			return 0, err
		}
		return id, m.backfillLeadIDForEvents(id, input.UserID, input.SessionNo, input.SourceTaskNo)
	}
	result, err := m.DB.Exec(`
		INSERT INTO customer_leads (
			user_id, name, phone, wechat, enterprise_wechat_contact, demand_summary,
			house_floors, house_style, land_width, land_depth, room_requirement,
			source, session_no, source_task_no, intent_level, status, assigned_to, remark,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, NOW(), NOW())
	`,
		input.UserID,
		strings.TrimSpace(input.Name),
		strings.TrimSpace(input.Phone),
		strings.TrimSpace(input.Wechat),
		strings.TrimSpace(input.EnterpriseWechatContact),
		strings.TrimSpace(input.DemandSummary),
		strings.TrimSpace(input.HouseFloors),
		strings.TrimSpace(input.HouseStyle),
		strings.TrimSpace(input.LandWidth),
		strings.TrimSpace(input.LandDepth),
		strings.TrimSpace(input.RoomRequirement),
		strings.TrimSpace(input.Source),
		strings.TrimSpace(input.SessionNo),
		strings.TrimSpace(input.SourceTaskNo),
		NormalizeCustomerIntentLevel(input.IntentLevel),
		NormalizeCustomerLeadStatus(input.Status),
		strings.TrimSpace(input.Remark),
	)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, m.backfillLeadIDForEvents(id, input.UserID, input.SessionNo, input.SourceTaskNo)
}

func (m *CustomerServiceModel) AnalyzeLeadIntentFromInput(input *CustomerLeadCreateInput) CustomerIntentAnalysis {
	if input == nil {
		return CustomerIntentAnalysis{Level: CustomerIntentLevelMedium, Reasons: []string{"默认中意向"}}
	}
	score := 0
	reasons := make([]string, 0)
	text := strings.Join([]string{
		input.DemandSummary,
		input.RoomRequirement,
		input.Remark,
		input.Source,
	}, " ")

	if strings.TrimSpace(input.Phone) != "" || strings.TrimSpace(input.Wechat) != "" || strings.TrimSpace(input.EnterpriseWechatContact) != "" {
		score += 35
		reasons = appendUniqueReason(reasons, "已留下联系方式")
	}
	if strings.TrimSpace(input.SourceTaskNo) != "" {
		score += 20
		reasons = appendUniqueReason(reasons, "已关联生成方案")
	}
	if strings.TrimSpace(input.HouseFloors) != "" || strings.TrimSpace(input.LandWidth) != "" || strings.TrimSpace(input.LandDepth) != "" || strings.TrimSpace(input.RoomRequirement) != "" {
		score += 20
		reasons = appendUniqueReason(reasons, "建房需求较明确")
	}
	if containsAnyText(text, "造价", "预算", "多少钱", "价格", "施工", "落地", "施工图", "深化") {
		score += 25
		reasons = appendUniqueReason(reasons, "出现高意向关键词")
	}
	if containsCustomerLeadContactIntent(text) {
		score += 25
		reasons = appendUniqueReason(reasons, "表达明确对接或联系意向")
	}
	if containsAnyText(input.Remark, "handoff_human", "添加微信", "咨询建房顾问", "精准测算造价") {
		score += 20
		reasons = appendUniqueReason(reasons, "触发转人工或强转化动作")
	}

	level := CustomerIntentLevelLow
	if score >= 60 {
		level = CustomerIntentLevelHigh
	} else if score >= 25 {
		level = CustomerIntentLevelMedium
	}
	if normalized := NormalizeCustomerIntentLevel(input.IntentLevel); normalized == CustomerIntentLevelHigh && level != CustomerIntentLevelHigh {
		level = CustomerIntentLevelHigh
		reasons = appendUniqueReason(reasons, "前端判定高意向")
	} else if normalized == CustomerIntentLevelMedium && level == CustomerIntentLevelLow {
		level = CustomerIntentLevelMedium
		reasons = appendUniqueReason(reasons, "前端判定中意向")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "信息较少，暂按低意向处理")
	}
	return CustomerIntentAnalysis{Level: level, Score: score, Reasons: reasons}
}

func containsCustomerLeadContactIntent(text string) bool {
	return containsAnyText(text,
		"联系我",
		"加微信",
		"留电话",
		"打电话",
		"电话联系",
		"微信联系",
		"加我",
		"对接",
		"转人工",
		"人工客服",
		"找设计师",
		"设计师联系",
		"咨询建房顾问",
		"添加微信",
	)
}

func (m *CustomerServiceModel) findExistingLeadID(userID int64, sessionNo, sourceTaskNo string) (int64, error) {
	trimmedSessionNo := strings.TrimSpace(sessionNo)
	trimmedTaskNo := strings.TrimSpace(sourceTaskNo)
	if userID <= 0 {
		return 0, nil
	}
	if trimmedSessionNo == "" && trimmedTaskNo == "" {
		return m.findLatestOpenLeadID(userID)
	}
	where := []string{"user_id = ?"}
	args := []interface{}{userID}
	if trimmedTaskNo != "" {
		where = append(where, "source_task_no = ?")
		args = append(args, trimmedTaskNo)
	} else {
		where = append(where, "session_no = ?")
		args = append(args, trimmedSessionNo)
	}
	var id int64
	err := m.DB.QueryRow(`
		SELECT id FROM customer_leads
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY id DESC
		LIMIT 1
	`, args...).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	return m.findLatestOpenLeadID(userID)
}

func (m *CustomerServiceModel) findLatestOpenLeadID(userID int64) (int64, error) {
	var id int64
	err := m.DB.QueryRow(`
		SELECT id FROM customer_leads
		WHERE user_id = ?
		  AND status IN ('new', 'contacted')
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`, userID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func (m *CustomerServiceModel) updateExistingLead(id int64, input *CustomerLeadCreateInput) error {
	_, err := m.DB.Exec(`
		UPDATE customer_leads
		SET name = IF(? = '', name, ?),
		    phone = IF(? = '', phone, ?),
		    wechat = IF(? = '', wechat, ?),
		    enterprise_wechat_contact = IF(? = '', enterprise_wechat_contact, ?),
		    demand_summary = IF(? = '', demand_summary, ?),
		    house_floors = IF(? = '', house_floors, ?),
		    house_style = IF(? = '', house_style, ?),
		    land_width = IF(? = '', land_width, ?),
		    land_depth = IF(? = '', land_depth, ?),
		    room_requirement = IF(? = '', room_requirement, ?),
		    source = IF(? = '', source, ?),
		    session_no = IF(? = '', session_no, ?),
		    source_task_no = IF(? = '', source_task_no, ?),
		    intent_level = ?,
		    status = CASE WHEN status IN ('contacted', 'converted', 'invalid') THEN status ELSE ? END,
		    remark = IF(? = '', remark, ?),
		    updated_at = NOW()
		WHERE id = ?
	`,
		strings.TrimSpace(input.Name), strings.TrimSpace(input.Name),
		strings.TrimSpace(input.Phone), strings.TrimSpace(input.Phone),
		strings.TrimSpace(input.Wechat), strings.TrimSpace(input.Wechat),
		strings.TrimSpace(input.EnterpriseWechatContact), strings.TrimSpace(input.EnterpriseWechatContact),
		strings.TrimSpace(input.DemandSummary), strings.TrimSpace(input.DemandSummary),
		strings.TrimSpace(input.HouseFloors), strings.TrimSpace(input.HouseFloors),
		strings.TrimSpace(input.HouseStyle), strings.TrimSpace(input.HouseStyle),
		strings.TrimSpace(input.LandWidth), strings.TrimSpace(input.LandWidth),
		strings.TrimSpace(input.LandDepth), strings.TrimSpace(input.LandDepth),
		strings.TrimSpace(input.RoomRequirement), strings.TrimSpace(input.RoomRequirement),
		strings.TrimSpace(input.Source), strings.TrimSpace(input.Source),
		strings.TrimSpace(input.SessionNo), strings.TrimSpace(input.SessionNo),
		strings.TrimSpace(input.SourceTaskNo), strings.TrimSpace(input.SourceTaskNo),
		NormalizeCustomerIntentLevel(input.IntentLevel),
		NormalizeCustomerLeadStatus(input.Status),
		strings.TrimSpace(input.Remark), strings.TrimSpace(input.Remark),
		id,
	)
	return err
}

func (m *CustomerServiceModel) CreateEvent(input *CustomerServiceEventCreateInput) (int64, error) {
	leadID := input.LeadID
	if leadID <= 0 {
		if existingID, err := m.findExistingLeadID(input.UserID, input.SessionNo, input.SourceTaskNo); err != nil {
			return 0, err
		} else if existingID > 0 {
			leadID = existingID
		}
	}
	payload, err := marshalCustomerServicePayload(input.Payload)
	if err != nil {
		return 0, err
	}
	result, err := m.DB.Exec(`
		INSERT INTO customer_service_events (
			user_id, lead_id, session_no, event_type, source, source_task_no,
			intent_level, demand_summary, payload, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW())
		`,
		input.UserID,
		leadID,
		strings.TrimSpace(input.SessionNo),
		strings.TrimSpace(input.EventType),
		strings.TrimSpace(input.Source),
		strings.TrimSpace(input.SourceTaskNo),
		NormalizeCustomerIntentLevel(input.IntentLevel),
		strings.TrimSpace(input.DemandSummary),
		payload,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (m *CustomerServiceModel) backfillLeadIDForEvents(leadID int64, userID int64, sessionNo, sourceTaskNo string) error {
	if leadID <= 0 || userID <= 0 {
		return nil
	}
	trimmedSessionNo := strings.TrimSpace(sessionNo)
	trimmedTaskNo := strings.TrimSpace(sourceTaskNo)
	where := []string{"user_id = ?", "lead_id = 0"}
	args := []interface{}{userID}
	if trimmedTaskNo != "" {
		where = append(where, "source_task_no = ?")
		args = append(args, trimmedTaskNo)
	} else if trimmedSessionNo != "" {
		where = append(where, "session_no = ?")
		args = append(args, trimmedSessionNo)
	} else {
		return nil
	}
	args = append([]interface{}{leadID}, args...)
	_, err := m.DB.Exec(`
		UPDATE customer_service_events
		SET lead_id = ?
		WHERE `+strings.Join(where, " AND "), args...)
	return err
}

func (m *CustomerServiceModel) ListLeads(params CustomerLeadListParams) ([]*CustomerLead, int64, error) {
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	where := []string{"1=1"}
	args := make([]interface{}, 0)
	if keyword := strings.TrimSpace(params.Keyword); keyword != "" {
		likeKeyword := "%" + keyword + "%"
		where = append(where, `(name LIKE ? OR phone LIKE ? OR wechat LIKE ? OR enterprise_wechat_contact LIKE ? OR demand_summary LIKE ? OR source_task_no LIKE ?)`)
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}
	if status := strings.TrimSpace(params.Status); status != "" && status != "all" {
		where = append(where, `status = ?`)
		args = append(args, NormalizeCustomerLeadStatus(status))
	}
	if level := strings.TrimSpace(params.IntentLevel); level != "" && level != "all" {
		where = append(where, `intent_level = ?`)
		args = append(args, NormalizeCustomerIntentLevel(level))
	}
	if source := strings.TrimSpace(params.Source); source != "" && source != "all" {
		where = append(where, `source = ?`)
		args = append(args, source)
	}
	if assignedTo := strings.TrimSpace(params.AssignedTo); assignedTo != "" && assignedTo != "all" {
		if assignedTo == "unassigned" {
			where = append(where, `assigned_to = ''`)
		} else {
			where = append(where, `assigned_to = ?`)
			args = append(args, assignedTo)
		}
	}
	switch strings.TrimSpace(params.Queue) {
	case "todo":
		where = append(where, `status IN ('new', 'contacted')`)
	case "today":
		where = append(where, `created_at >= CURDATE()`)
	case "high":
		where = append(where, `intent_level = 'high' AND status IN ('new', 'contacted')`)
	case "overdue":
		where = append(where, `status IN ('new', 'contacted') AND (next_followup_at IS NULL OR next_followup_at <= NOW() OR updated_at <= DATE_SUB(NOW(), INTERVAL 1 DAY))`)
	case "unassigned":
		where = append(where, `status IN ('new', 'contacted') AND assigned_to = ''`)
	}

	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM customer_leads WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, user_id, name, phone, wechat, enterprise_wechat_contact,
		       COALESCE(demand_summary, ''), house_floors, house_style, land_width, land_depth,
		       room_requirement, source, session_no, source_task_no, intent_level, status, assigned_to, next_followup_at,
		       COALESCE(remark, ''), created_at, updated_at
		FROM customer_leads
		WHERE ` + whereSQL + `
		ORDER BY CASE status WHEN 'new' THEN 1 WHEN 'contacted' THEN 2 WHEN 'converted' THEN 3 WHEN 'invalid' THEN 4 ELSE 99 END, FIELD(intent_level, 'high', 'medium', 'low'), created_at DESC
		LIMIT ? OFFSET ?`
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := m.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]*CustomerLead, 0, limit)
	for rows.Next() {
		item := &CustomerLead{}
		var nextFollowupAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.Name,
			&item.Phone,
			&item.Wechat,
			&item.EnterpriseWechatContact,
			&item.DemandSummary,
			&item.HouseFloors,
			&item.HouseStyle,
			&item.LandWidth,
			&item.LandDepth,
			&item.RoomRequirement,
			&item.Source,
			&item.SessionNo,
			&item.SourceTaskNo,
			&item.IntentLevel,
			&item.Status,
			&item.AssignedTo,
			&nextFollowupAt,
			&item.Remark,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		if nextFollowupAt.Valid {
			item.NextFollowupAt = &nextFollowupAt.Time
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (m *CustomerServiceModel) GetLead(id int64) (*CustomerLead, error) {
	item := &CustomerLead{}
	var nextFollowupAt sql.NullTime
	err := m.DB.QueryRow(`
		SELECT id, user_id, name, phone, wechat, enterprise_wechat_contact,
		       COALESCE(demand_summary, ''), house_floors, house_style, land_width, land_depth,
		       room_requirement, source, session_no, source_task_no, intent_level, status, assigned_to, next_followup_at,
		       COALESCE(remark, ''), created_at, updated_at
		FROM customer_leads
		WHERE id = ?
	`, id).Scan(
		&item.ID,
		&item.UserID,
		&item.Name,
		&item.Phone,
		&item.Wechat,
		&item.EnterpriseWechatContact,
		&item.DemandSummary,
		&item.HouseFloors,
		&item.HouseStyle,
		&item.LandWidth,
		&item.LandDepth,
		&item.RoomRequirement,
		&item.Source,
		&item.SessionNo,
		&item.SourceTaskNo,
		&item.IntentLevel,
		&item.Status,
		&item.AssignedTo,
		&nextFollowupAt,
		&item.Remark,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if nextFollowupAt.Valid {
		item.NextFollowupAt = &nextFollowupAt.Time
	}
	return item, nil
}

func (m *CustomerServiceModel) ListEventsForLead(lead *CustomerLead, limit int) ([]*CustomerServiceEvent, error) {
	if lead == nil || lead.ID <= 0 {
		return []*CustomerServiceEvent{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
		SELECT id, user_id, lead_id, session_no, event_type, source, source_task_no,
		       intent_level, COALESCE(demand_summary, ''), COALESCE(CAST(payload AS CHAR), ''), created_at
		FROM customer_service_events
		WHERE lead_id = ?
		ORDER BY created_at ASC, id ASC
		LIMIT ?`
	args := []interface{}{lead.ID, limit}
	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]*CustomerServiceEvent, 0)
	for rows.Next() {
		item := &CustomerServiceEvent{}
		var payloadRaw string
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.LeadID,
			&item.SessionNo,
			&item.EventType,
			&item.Source,
			&item.SourceTaskNo,
			&item.IntentLevel,
			&item.DemandSummary,
			&payloadRaw,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.Payload = unmarshalCustomerServicePayload(payloadRaw)
		events = append(events, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (m *CustomerServiceModel) CreateFollowup(input *CustomerLeadFollowupCreateInput) (int64, error) {
	if input == nil || input.LeadID <= 0 {
		return 0, ErrInvalidCustomerLeadID
	}
	status := NormalizeCustomerLeadStatus(input.Status)
	remark := strings.TrimSpace(input.Remark)
	if remark != "" {
		result, err := m.DB.Exec(`
			INSERT INTO customer_lead_followups (lead_id, status, remark, assigned_to, next_followup_at, operator_id, operator_name, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, NOW())
		`, input.LeadID, status, remark, strings.TrimSpace(input.AssignedTo), input.NextFollowupAt, input.OperatorID, strings.TrimSpace(input.OperatorName))
		if err != nil {
			return 0, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return 0, err
		}
		return id, nil
	}
	return 0, nil
}

func (m *CustomerServiceModel) ListFollowups(leadID int64) ([]*CustomerLeadFollowup, error) {
	if leadID <= 0 {
		return []*CustomerLeadFollowup{}, nil
	}
	rows, err := m.DB.Query(`
		SELECT id, lead_id, status, COALESCE(remark, ''), assigned_to, next_followup_at, operator_id, operator_name, created_at
		FROM customer_lead_followups
		WHERE lead_id = ?
		ORDER BY created_at DESC, id DESC
	`, leadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]*CustomerLeadFollowup, 0)
	for rows.Next() {
		item := &CustomerLeadFollowup{}
		var nextFollowupAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.LeadID,
			&item.Status,
			&item.Remark,
			&item.AssignedTo,
			&nextFollowupAt,
			&item.OperatorID,
			&item.OperatorName,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if nextFollowupAt.Valid {
			item.NextFollowupAt = &nextFollowupAt.Time
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func (m *CustomerServiceModel) BuildLeadSummary(lead *CustomerLead, events []*CustomerServiceEvent, followups []*CustomerLeadFollowup) string {
	if lead == nil {
		return "暂无客户摘要。"
	}
	parts := make([]string, 0)
	name := strings.TrimSpace(lead.Name)
	if name == "" {
		if lead.UserID > 0 {
			name = "用户" + strconv.FormatInt(lead.UserID, 10)
		} else {
			name = "匿名用户"
		}
	}
	parts = append(parts, name+"来自"+defaultText(lead.Source, "未知来源"))
	if strings.TrimSpace(lead.SourceTaskNo) != "" {
		parts = append(parts, "已关联任务号 "+strings.TrimSpace(lead.SourceTaskNo))
	}
	demand := strings.TrimSpace(lead.DemandSummary)
	if demand != "" {
		parts = append(parts, "需求："+demand)
	}
	buildingParts := []string{}
	if strings.TrimSpace(lead.HouseFloors) != "" {
		buildingParts = append(buildingParts, lead.HouseFloors)
	}
	if strings.TrimSpace(lead.HouseStyle) != "" {
		buildingParts = append(buildingParts, lead.HouseStyle)
	}
	if strings.TrimSpace(lead.LandWidth) != "" || strings.TrimSpace(lead.LandDepth) != "" {
		buildingParts = append(buildingParts, "宅基地"+strings.TrimSpace(lead.LandWidth)+"x"+strings.TrimSpace(lead.LandDepth))
	}
	if strings.TrimSpace(lead.RoomRequirement) != "" {
		buildingParts = append(buildingParts, lead.RoomRequirement)
	}
	if len(buildingParts) > 0 {
		parts = append(parts, "建房信息："+strings.Join(buildingParts, "，"))
	}
	contactParts := []string{}
	if strings.TrimSpace(lead.Phone) != "" {
		contactParts = append(contactParts, "电话")
	}
	if strings.TrimSpace(lead.Wechat) != "" {
		contactParts = append(contactParts, "微信")
	}
	if strings.TrimSpace(lead.EnterpriseWechatContact) != "" {
		contactParts = append(contactParts, "企微")
	}
	if len(contactParts) > 0 {
		parts = append(parts, "已留"+strings.Join(contactParts, "/"))
	}
	if len(events) > 0 {
		parts = append(parts, "累计客服事件 "+strconv.Itoa(len(events))+" 条，最近事件为 "+defaultText(events[len(events)-1].EventType, "未知事件"))
	}
	if len(followups) > 0 {
		parts = append(parts, "最近跟进："+strings.TrimSpace(followups[0].Remark))
	}
	return strings.Join(parts, "；") + "。"
}

func (m *CustomerServiceModel) Overview() (*CustomerLeadOverview, error) {
	overview := &CustomerLeadOverview{}
	query := `
		SELECT
			COUNT(*) AS total_count,
			COALESCE(SUM(CASE WHEN status = 'new' THEN 1 ELSE 0 END), 0) AS new_count,
			COALESCE(SUM(CASE WHEN status = 'contacted' THEN 1 ELSE 0 END), 0) AS contacted_count,
			COALESCE(SUM(CASE WHEN intent_level = 'high' THEN 1 ELSE 0 END), 0) AS high_intent_count,
			COALESCE(SUM(CASE WHEN status = 'converted' THEN 1 ELSE 0 END), 0) AS converted_count,
			COALESCE(SUM(CASE WHEN created_at >= CURDATE() THEN 1 ELSE 0 END), 0) AS today_new_count,
			COALESCE(SUM(CASE WHEN status IN ('new', 'contacted') THEN 1 ELSE 0 END), 0) AS todo_count,
			COALESCE(SUM(CASE WHEN status IN ('new', 'contacted') AND (next_followup_at IS NULL OR next_followup_at <= NOW() OR updated_at <= DATE_SUB(NOW(), INTERVAL 1 DAY)) THEN 1 ELSE 0 END), 0) AS overdue_count
		FROM customer_leads`
	if err := m.DB.QueryRow(query).Scan(
		&overview.TotalCount,
		&overview.NewCount,
		&overview.ContactedCount,
		&overview.HighIntentCount,
		&overview.ConvertedCount,
		&overview.TodayNewCount,
		&overview.TodoCount,
		&overview.OverdueCount,
	); err != nil {
		return nil, err
	}
	return overview, nil
}

func (m *CustomerServiceModel) FunnelMetrics() ([]CustomerFunnelMetric, error) {
	eventKeys := []string{
		"welcome_shown",
		"structured_requirement_collected",
		"generate_click",
		"human_handoff_triggered",
		"lead_submit",
	}
	eventLabels := map[string]string{
		"welcome_shown":                    "进入客服",
		"structured_requirement_collected": "完成需求采集",
		"generate_click":                   "点击生成方案",
		"human_handoff_triggered":          "转人工承接",
		"lead_submit":                      "提交线索",
	}
	counts := map[string]int64{}
	rows, err := m.DB.Query(`
		SELECT event_type, COUNT(DISTINCT session_no) AS count
		FROM customer_service_events
		WHERE event_type IN ('welcome_shown', 'structured_requirement_collected', 'generate_click', 'human_handoff_triggered', 'lead_submit')
		GROUP BY event_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var convertedCount int64
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM customer_leads WHERE status = 'converted'`).Scan(&convertedCount); err != nil {
		return nil, err
	}
	counts["converted"] = convertedCount

	metrics := make([]CustomerFunnelMetric, 0, len(eventKeys)+1)
	var base int64
	for index, key := range eventKeys {
		count := counts[key]
		if index == 0 {
			base = count
		}
		rate := 0.0
		if base > 0 {
			rate = float64(count) * 100 / float64(base)
		}
		metrics = append(metrics, CustomerFunnelMetric{
			Key:            key,
			Label:          eventLabels[key],
			Count:          count,
			ConversionRate: rate,
		})
	}
	rate := 0.0
	if base > 0 {
		rate = float64(convertedCount) * 100 / float64(base)
	}
	metrics = append(metrics, CustomerFunnelMetric{
		Key:            "converted",
		Label:          "已成交",
		Count:          convertedCount,
		ConversionRate: rate,
	})
	return metrics, nil
}

func (m *CustomerServiceModel) UpdateLeadStatus(id int64, status string, remark string) error {
	return m.ApplyLeadAction(id, CustomerLeadActionInput{Status: status, Remark: remark})
}

func (m *CustomerServiceModel) ApplyLeadAction(id int64, input CustomerLeadActionInput) error {
	lead, err := m.GetLead(id)
	if err != nil {
		return err
	}
	status := NormalizeCustomerLeadStatus(input.Status)
	analysis := m.AnalyzeLeadIntentFromInput(&CustomerLeadCreateInput{
		UserID:                  lead.UserID,
		Name:                    lead.Name,
		Phone:                   lead.Phone,
		Wechat:                  lead.Wechat,
		EnterpriseWechatContact: lead.EnterpriseWechatContact,
		DemandSummary:           lead.DemandSummary,
		HouseFloors:             lead.HouseFloors,
		HouseStyle:              lead.HouseStyle,
		LandWidth:               lead.LandWidth,
		LandDepth:               lead.LandDepth,
		RoomRequirement:         lead.RoomRequirement,
		Source:                  lead.Source,
		SessionNo:               lead.SessionNo,
		SourceTaskNo:            lead.SourceTaskNo,
		IntentLevel:             lead.IntentLevel,
		Status:                  status,
		Remark:                  input.Remark,
	})
	assignedTo := strings.TrimSpace(input.AssignedTo)
	if assignedTo == "" {
		assignedTo = lead.AssignedTo
	}
	result, err := m.DB.Exec(`
		UPDATE customer_leads
		SET status = ?,
		    intent_level = ?,
		    assigned_to = ?,
		    next_followup_at = ?,
		    remark = ?,
		    updated_at = NOW()
		WHERE id = ?
	`, status, analysis.Level, assignedTo, input.NextFollowupAt, strings.TrimSpace(input.Remark), id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *CustomerServiceModel) BatchApplyLeadAction(ids []int64, input CustomerLeadActionInput) (int, error) {
	updated := 0
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if err := m.ApplyLeadAction(id, input); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func marshalCustomerServicePayload(payload map[string]interface{}) (interface{}, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func unmarshalCustomerServicePayload(raw string) map[string]interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	return payload
}
