package model

import (
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"time"
)

// AITask AI任务
type AITask struct {
	ID             int64          `json:"id" db:"id"`
	TaskNo         string         `json:"task_no" db:"task_no"` // 32位唯一任务编号
	UserID         int64          `json:"user_id" db:"user_id"`
	ToolID         sql.NullInt64  `json:"tool_id" db:"tool_id"`
	Scene          string         `json:"scene" db:"scene"`
	Model          sql.NullString `json:"model" db:"model"`
	APIEndpoint    sql.NullString `json:"api_endpoint" db:"api_endpoint"`
	ProviderCode   sql.NullString `json:"provider_code" db:"provider_code"` // 执行该任务的供应商编码（二期落库，历史为 NULL）
	APICost        sql.NullInt64  `json:"api_cost" db:"api_cost"`           // API 成本估算，单位分（二期落库，历史为 NULL）
	RequestPayload string         `json:"request_payload" db:"request_payload"` // JSON格式的请求数据
	Status         string         `json:"status" db:"status"`                   // pending, running, success, failed
	ResultPayload  sql.NullString `json:"-" db:"result_payload"`                // JSON格式的结果数据（可为NULL）
	StonesUsed     int64          `json:"stones_used" db:"stones_used"`
	ErrorMessage   sql.NullString `json:"-" db:"error_message"` // 错误信息（可为NULL）
	CreatedAt      time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at" db:"updated_at"`
}

type AITaskManagementItem struct {
	ID           int64     `json:"id"`
	TaskNo       string    `json:"task_no"`
	UserID       int64     `json:"user_id"`
	Username     string    `json:"username"`
	Scene        string    `json:"scene"`
	Model        string    `json:"model"`
	APIEndpoint  string    `json:"api_endpoint,omitempty"`
	RequestPayload string   `json:"-"`
	ResultPayload string   `json:"-"`
	Status       string    `json:"status"`
	StonesUsed   int64     `json:"stones_used"`
	RefundedStones int64    `json:"refunded_stones"` // 结果载荷里的自动退款灵石（0=无退款）
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AITaskManagementSummary struct {
	TotalCount   int64 `json:"total_count"`
	PendingCount int64 `json:"pending_count"`
	RunningCount int64 `json:"running_count"`
	FailedCount  int64 `json:"failed_count"`
}

// GetResultPayload 获取结果载荷
func (t *AITask) GetResultPayload() string {
	if t.ResultPayload.Valid {
		return t.ResultPayload.String
	}
	return ""
}

// GetErrorMessage 获取错误信息
func (t *AITask) GetErrorMessage() string {
	if t.ErrorMessage.Valid {
		return t.ErrorMessage.String
	}
	return ""
}

func (t *AITask) GetModel() string {
	if t.Model.Valid {
		return t.Model.String
	}
	return ""
}

func (t *AITask) GetAPIEndpoint() string {
	if t.APIEndpoint.Valid {
		return t.APIEndpoint.String
	}
	return ""
}

func (t *AITask) GetToolID() int64 {
	if t.ToolID.Valid {
		return t.ToolID.Int64
	}
	return 0
}

func extractTaskModelFromValue(value interface{}) string {
	switch current := value.(type) {
	case map[string]interface{}:
		if rawModel, ok := current["model"]; ok {
			if modelText, ok := rawModel.(string); ok && strings.TrimSpace(modelText) != "" {
				return strings.TrimSpace(modelText)
			}
		}
		if rawModel, ok := current["used_model"]; ok {
			if modelText, ok := rawModel.(string); ok && strings.TrimSpace(modelText) != "" {
				return strings.TrimSpace(modelText)
			}
		}
		for _, child := range current {
			if modelText := extractTaskModelFromValue(child); modelText != "" {
				return modelText
			}
		}
	case []interface{}:
		for _, child := range current {
			if modelText := extractTaskModelFromValue(child); modelText != "" {
				return modelText
			}
		}
	}
	return ""
}

func extractTaskAPIEndpointFromValue(value interface{}) string {
	switch current := value.(type) {
	case map[string]interface{}:
		if rawEndpoint, ok := current["api_endpoint"]; ok {
			if endpointText, ok := rawEndpoint.(string); ok && strings.TrimSpace(endpointText) != "" {
				return strings.TrimSpace(endpointText)
			}
		}
		for _, child := range current {
			if endpointText := extractTaskAPIEndpointFromValue(child); endpointText != "" {
				return endpointText
			}
		}
	case []interface{}:
		for _, child := range current {
			if endpointText := extractTaskAPIEndpointFromValue(child); endpointText != "" {
				return endpointText
			}
		}
	}
	return ""
}

func extractTaskModelFromEndpoint(endpoint string) string {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if index := strings.Index(lower, "/models/"); index >= 0 {
		segment := raw[index+len("/models/"):]
		for idx, char := range segment {
			if char == ':' || char == '/' || char == '?' {
				segment = segment[:idx]
				break
			}
		}
		return strings.TrimSpace(segment)
	}
	marker := "seedream-"
	if index := strings.Index(lower, marker); index >= 0 {
		segment := raw[index:]
		for idx, char := range segment {
			if !(char == '-' || char == '_' || char == '.' || (char >= '0' && char <= '9') || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')) {
				segment = segment[:idx]
				break
			}
		}
		return strings.TrimSpace(segment)
	}
	return ""
}

func parseTaskExecutionMeta(payload string) (string, string) {
	if strings.TrimSpace(payload) == "" {
		return "", ""
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return "", ""
	}
	return extractTaskModelFromValue(parsed), extractTaskAPIEndpointFromValue(parsed)
}

func parseTaskModelFromJSONPayload(payload string) string {
	if strings.TrimSpace(payload) == "" {
		return ""
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return ""
	}
	return extractTaskModelFromValue(parsed)
}

func (t *AITask) GetResolvedAPIEndpoint() string {
	if endpoint := strings.TrimSpace(t.GetAPIEndpoint()); endpoint != "" {
		return endpoint
	}
	_, endpoint := parseTaskExecutionMeta(t.GetResultPayload())
	return strings.TrimSpace(endpoint)
}

func (t *AITask) GetResolvedModel() string {
	if modelText := strings.TrimSpace(t.GetModel()); modelText != "" {
		return modelText
	}
	if modelText, _ := parseTaskExecutionMeta(t.GetResultPayload()); modelText != "" {
		return modelText
	}
	if modelText := parseTaskModelFromJSONPayload(t.RequestPayload); modelText != "" {
		return modelText
	}
	if modelText := extractTaskModelFromEndpoint(t.GetResolvedAPIEndpoint()); modelText != "" {
		return modelText
	}
	return ""
}

type sqlExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// AITaskModel AI任务数据访问层
type AITaskModel struct {
	DB *sql.DB
}

// NewAITaskModel 创建AI任务模型
func NewAITaskModel(db *sql.DB) *AITaskModel {
	return &AITaskModel{DB: db}
}

func (m *AITaskModel) createWithExecutor(executor sqlExecutor, task *AITask) error {
	query := `INSERT INTO ai_tasks (task_no, user_id, tool_id, scene, model, api_endpoint, request_payload, status, stones_used, created_at, updated_at) 
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`
	var toolIDValue interface{}
	if task.ToolID.Valid && task.ToolID.Int64 > 0 {
		toolIDValue = task.ToolID.Int64
	}
	result, err := executor.Exec(query, task.TaskNo, task.UserID, toolIDValue, task.Scene, task.GetModel(), task.GetAPIEndpoint(), task.RequestPayload, task.Status, task.StonesUsed)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	task.ID = id
	return nil
}

// Create 创建AI任务
func (m *AITaskModel) Create(task *AITask) error {
	return m.createWithExecutor(m.DB, task)
}

func (m *AITaskModel) CreateWithTx(tx *sql.Tx, task *AITask) error {
	return m.createWithExecutor(tx, task)
}

// GetByID 根据ID获取任务
func (m *AITaskModel) GetByID(id int64) (*AITask, error) {
	task := &AITask{}
	query := `SELECT id, task_no, user_id, tool_id, scene, model, api_endpoint, provider_code, api_cost, request_payload, status, result_payload, stones_used, error_message, created_at, updated_at 
	          FROM ai_tasks WHERE id = ?`
	err := m.DB.QueryRow(query, id).Scan(
		&task.ID, &task.TaskNo, &task.UserID, &task.ToolID, &task.Scene, &task.Model, &task.APIEndpoint, &task.ProviderCode, &task.APICost, &task.RequestPayload, &task.Status,
		&task.ResultPayload, &task.StonesUsed, &task.ErrorMessage, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return task, nil
}

// GetByTaskNo 根据任务编号获取任务
func (m *AITaskModel) GetByTaskNo(taskNo string) (*AITask, error) {
	task := &AITask{}
	query := `SELECT id, task_no, user_id, tool_id, scene, model, api_endpoint, provider_code, api_cost, request_payload, status, result_payload, stones_used, error_message, created_at, updated_at 
	          FROM ai_tasks WHERE task_no = ?`
	err := m.DB.QueryRow(query, taskNo).Scan(
		&task.ID, &task.TaskNo, &task.UserID, &task.ToolID, &task.Scene, &task.Model, &task.APIEndpoint, &task.ProviderCode, &task.APICost, &task.RequestPayload, &task.Status,
		&task.ResultPayload, &task.StonesUsed, &task.ErrorMessage, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return task, nil
}

// UpdateStatus 更新任务状态（根据ID）
func (m *AITaskModel) UpdateStatus(id int64, status string, resultPayload string, errorMessage string) error {
	query := `UPDATE ai_tasks SET status = ?, result_payload = ?, error_message = ?, updated_at = NOW() WHERE id = ?`
	// 处理空字符串为NULL
	var rp, em interface{}
	if resultPayload != "" {
		rp = resultPayload
	}
	if errorMessage != "" {
		em = errorMessage
	}
	_, err := m.DB.Exec(query, status, rp, em, id)
	return err
}

func (m *AITaskModel) UpdateStatusAndMetaByTaskNo(taskNo string, status string, resultPayload string, errorMessage string, model string, apiEndpoint string) error {
	query := `UPDATE ai_tasks SET status = ?, result_payload = ?, error_message = ?, model = COALESCE(?, model), api_endpoint = COALESCE(?, api_endpoint), updated_at = NOW() WHERE task_no = ?`
	var rp, em interface{}
	if resultPayload != "" {
		rp = resultPayload
	}
	if errorMessage != "" {
		em = errorMessage
	}
	var modelValue interface{}
	if strings.TrimSpace(model) != "" {
		modelValue = strings.TrimSpace(model)
	}
	var endpointValue interface{}
	if strings.TrimSpace(apiEndpoint) != "" {
		endpointValue = strings.TrimSpace(apiEndpoint)
	}
	_, err := m.DB.Exec(query, status, rp, em, modelValue, endpointValue, taskNo)
	return err
}

func (m *AITaskModel) UpdateExecutionMetaByTaskNo(taskNo string, model string, apiEndpoint string) error {
	query := `UPDATE ai_tasks SET model = COALESCE(?, model), api_endpoint = COALESCE(?, api_endpoint), updated_at = NOW() WHERE task_no = ?`
	var modelValue interface{}
	if strings.TrimSpace(model) != "" {
		modelValue = strings.TrimSpace(model)
	}
	var endpointValue interface{}
	if strings.TrimSpace(apiEndpoint) != "" {
		endpointValue = strings.TrimSpace(apiEndpoint)
	}
	_, err := m.DB.Exec(query, modelValue, endpointValue, taskNo)
	return err
}

func (m *AITaskModel) UpdateStonesUsedByTaskNo(taskNo string, stonesUsed int64) error {
	query := `UPDATE ai_tasks SET stones_used = ?, updated_at = NOW() WHERE task_no = ?`
	_, err := m.DB.Exec(query, stonesUsed, taskNo)
	return err
}

// UpdateStatusByTaskNo 更新任务状态（根据任务编号）
func (m *AITaskModel) UpdateStatusByTaskNo(taskNo string, status string, resultPayload string, errorMessage string) error {
	return m.UpdateStatusAndMetaByTaskNo(taskNo, status, resultPayload, errorMessage, "", "")
}

func (m *AITaskModel) ClaimPendingTasks(limit int) ([]*AITask, error) {
	if limit < 1 {
		limit = 10
	}
	tx, err := m.DB.Begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	query := `SELECT id, task_no, user_id, tool_id, scene, model, api_endpoint, provider_code, api_cost, request_payload, status, result_payload, stones_used, error_message, created_at, updated_at 
	          FROM ai_tasks
	          WHERE status = 'pending'
	            AND scene NOT IN ('ai_cost_doc')
	          ORDER BY created_at ASC LIMIT ? FOR UPDATE`
	rows, err := tx.Query(query, limit)
	if err != nil {
		return nil, err
	}

	tasks := make([]*AITask, 0, limit)
	for rows.Next() {
		task := &AITask{}
		if err := rows.Scan(
			&task.ID, &task.TaskNo, &task.UserID, &task.ToolID, &task.Scene, &task.Model, &task.APIEndpoint, &task.ProviderCode, &task.APICost, &task.RequestPayload, &task.Status,
			&task.ResultPayload, &task.StonesUsed, &task.ErrorMessage, &task.CreatedAt, &task.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if len(tasks) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		committed = true
		return nil, nil
	}

	placeholders := make([]string, 0, len(tasks))
	args := make([]interface{}, 0, len(tasks))
	for _, task := range tasks {
		placeholders = append(placeholders, "?")
		args = append(args, task.ID)
	}
	updateQuery := `UPDATE ai_tasks SET status = 'processing', updated_at = NOW() WHERE status = 'pending' AND id IN (` + strings.Join(placeholders, ",") + `)`
	if _, err := tx.Exec(updateQuery, args...); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	for _, task := range tasks {
		task.Status = "processing"
	}
	return tasks, nil
}

// GetPendingTasks 获取待处理的任务
func (m *AITaskModel) GetPendingTasks(limit int) ([]*AITask, error) {
	query := `SELECT id, task_no, user_id, tool_id, scene, model, api_endpoint, provider_code, api_cost, request_payload, status, result_payload, stones_used, error_message, created_at, updated_at 
	          FROM ai_tasks
	          WHERE status = 'pending'
	            AND scene NOT IN ('ai_cost_doc')
	          ORDER BY created_at ASC LIMIT ?`
	rows, err := m.DB.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*AITask
	for rows.Next() {
		task := &AITask{}
		err := rows.Scan(
			&task.ID, &task.TaskNo, &task.UserID, &task.ToolID, &task.Scene, &task.Model, &task.APIEndpoint, &task.ProviderCode, &task.APICost, &task.RequestPayload, &task.Status,
			&task.ResultPayload, &task.StonesUsed, &task.ErrorMessage, &task.CreatedAt, &task.UpdatedAt,
		)
		if err != nil {
			// 打印扫描错误而不是静默跳过
			log.Printf("[AITaskModel] GetPendingTasks Scan错误: %v", err)
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// GetByUserID 根据用户ID获取任务列表（分页）
func (m *AITaskModel) GetByUserID(userID int64, limit, offset int) ([]*AITask, error) {
	query := `SELECT id, task_no, user_id, tool_id, scene, model, api_endpoint, provider_code, api_cost, request_payload, status, result_payload, stones_used, error_message, created_at, updated_at 
	          FROM ai_tasks WHERE user_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*AITask
	for rows.Next() {
		task := &AITask{}
		err := rows.Scan(
			&task.ID, &task.TaskNo, &task.UserID, &task.ToolID, &task.Scene, &task.Model, &task.APIEndpoint, &task.ProviderCode, &task.APICost, &task.RequestPayload, &task.Status,
			&task.ResultPayload, &task.StonesUsed, &task.ErrorMessage, &task.CreatedAt, &task.UpdatedAt,
		)
		if err != nil {
			log.Printf("[AITaskModel] GetByUserID Scan错误: %v", err)
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// CountByUserID 统计用户任务总数
func (m *AITaskModel) CountByUserID(userID int64) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM ai_tasks WHERE user_id = ?`
	err := m.DB.QueryRow(query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// CountByToolID 统计某个工具关联的 AI 任务数量
func (m *AITaskModel) CountByToolID(toolID int64) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM ai_tasks WHERE tool_id = ?`
	err := m.DB.QueryRow(query, toolID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// DeleteByTaskNo 根据任务编号删除任务（仅允许用户删除自己的任务，由调用方校验 user_id）
func (m *AITaskModel) DeleteByTaskNo(taskNo string) error {
	_, err := m.DB.Exec(`DELETE FROM ai_tasks WHERE task_no = ?`, taskNo)
	return err
}

// InitTable 初始化ai_tasks表
func (m *AITaskModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS ai_tasks (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	task_no VARCHAR(32) NOT NULL UNIQUE,
	user_id BIGINT UNSIGNED NOT NULL,
	tool_id BIGINT UNSIGNED DEFAULT NULL,
	scene VARCHAR(64) NOT NULL,
	model VARCHAR(128) DEFAULT NULL,
	api_endpoint VARCHAR(255) DEFAULT NULL,
	request_payload TEXT NOT NULL,
	status VARCHAR(32) NOT NULL DEFAULT 'pending',
	result_payload LONGTEXT,
	stones_used BIGINT UNSIGNED NOT NULL DEFAULT 0,
	error_message TEXT,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_user_id (user_id),
	INDEX idx_tool_id (tool_id),
	INDEX idx_scene (scene),
	INDEX idx_status (status),
	INDEX idx_task_no (task_no),
	INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}

	// 修改已存在表的字段类型（TEXT -> LONGTEXT）
	m.DB.Exec(`ALTER TABLE ai_tasks MODIFY COLUMN result_payload LONGTEXT`)
	m.DB.Exec(`ALTER TABLE ai_tasks ADD COLUMN tool_id BIGINT UNSIGNED DEFAULT NULL`)
	m.DB.Exec(`ALTER TABLE ai_tasks ADD COLUMN model VARCHAR(128) DEFAULT NULL`)
	m.DB.Exec(`ALTER TABLE ai_tasks ADD COLUMN api_endpoint VARCHAR(255) DEFAULT NULL`)
	m.DB.Exec(`ALTER TABLE ai_tasks ADD INDEX idx_tool_id (tool_id)`)
	// 二期（2026-10）：任务级成本落库。历史行为 NULL（总览页按估算口径补），新任务结算时写入
	m.DB.Exec(`ALTER TABLE ai_tasks ADD COLUMN provider_code VARCHAR(64) DEFAULT NULL`)
	m.DB.Exec(`ALTER TABLE ai_tasks ADD COLUMN api_cost BIGINT UNSIGNED DEFAULT NULL`)

	return nil
}

// UpdateExecutionCostByTaskNo 任务成功结算时写入执行供应商与成本（单位：分）
func (m *AITaskModel) UpdateExecutionCostByTaskNo(taskNo string, providerCode string, apiCostFen int64) error {
	_, err := m.DB.Exec(
		`UPDATE ai_tasks SET provider_code = ?, api_cost = ? WHERE task_no = ?`,
		providerCode, apiCostFen, taskNo)
	return err
}

func (m *AITaskModel) buildManagementWhere(keyword, status, scene string) (string, []interface{}) {
	where := []string{"u.user_type = 'miniprogram'"}
	args := make([]interface{}, 0)
	if keyword != "" {
		likeKeyword := "%" + keyword + "%"
		where = append(where, "(a.task_no LIKE ? OR CAST(a.user_id AS CHAR) LIKE ? OR u.username LIKE ? OR COALESCE(a.model, '') LIKE ?)")
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}
	if status != "" && status != "all" {
		where = append(where, "a.status = ?")
		args = append(args, status)
	}
	if scene != "" && scene != "all" {
		where = append(where, "a.scene = ?")
		args = append(args, scene)
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

func (m *AITaskModel) ListForManagement(keyword, status, scene string, limit, offset int) ([]*AITaskManagementItem, int64, error) {
	whereSQL, args := m.buildManagementWhere(keyword, status, scene)
	countQuery := `SELECT COUNT(*) FROM ai_tasks a LEFT JOIN users u ON u.id = a.user_id` + whereSQL
	var total int64
	if err := m.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	query := `
		SELECT a.id, a.task_no, a.user_id, COALESCE(u.username, ''), a.scene, COALESCE(a.model, ''), COALESCE(a.api_endpoint, ''), COALESCE(a.request_payload, ''), COALESCE(a.result_payload, ''), a.status, a.stones_used, COALESCE(a.error_message, ''), a.created_at, a.updated_at
		FROM ai_tasks a
		LEFT JOIN users u ON u.id = a.user_id` + whereSQL + `
		ORDER BY a.created_at DESC
		LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]*AITaskManagementItem, 0)
	type executionMetaBackfill struct {
		taskNo      string
		model       string
		apiEndpoint string
	}
	backfills := make([]executionMetaBackfill, 0)
	for rows.Next() {
		item := &AITaskManagementItem{}
		if err := rows.Scan(&item.ID, &item.TaskNo, &item.UserID, &item.Username, &item.Scene, &item.Model, &item.APIEndpoint, &item.RequestPayload, &item.ResultPayload, &item.Status, &item.StonesUsed, &item.ErrorMessage, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		rawModel := strings.TrimSpace(item.Model)
		rawEndpoint := strings.TrimSpace(item.APIEndpoint)
		task := &AITask{
			TaskNo:         item.TaskNo,
			Scene:          item.Scene,
			Model:          sql.NullString{String: strings.TrimSpace(item.Model), Valid: strings.TrimSpace(item.Model) != ""},
			APIEndpoint:    sql.NullString{String: strings.TrimSpace(item.APIEndpoint), Valid: strings.TrimSpace(item.APIEndpoint) != ""},
			RequestPayload: item.RequestPayload,
			ResultPayload:  sql.NullString{String: item.ResultPayload, Valid: strings.TrimSpace(item.ResultPayload) != ""},
		}
		item.Model = task.GetResolvedModel()
		item.APIEndpoint = task.GetResolvedAPIEndpoint()
		if item.Status == "success" && strings.TrimSpace(item.Model) != "" && (rawModel == "" || rawEndpoint == "") {
			backfills = append(backfills, executionMetaBackfill{
				taskNo:      item.TaskNo,
				model:       item.Model,
				apiEndpoint: item.APIEndpoint,
			})
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for _, backfill := range backfills {
		_ = m.UpdateExecutionMetaByTaskNo(backfill.taskNo, backfill.model, backfill.apiEndpoint)
	}
	return list, total, nil
}

func (m *AITaskModel) SummaryForManagement(keyword, status, scene string) (*AITaskManagementSummary, error) {
	whereSQL, args := m.buildManagementWhere(keyword, status, scene)
	query := `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN a.status = 'pending' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN a.status = 'running' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN a.status = 'failed' THEN 1 ELSE 0 END), 0)
		FROM ai_tasks a
		LEFT JOIN users u ON u.id = a.user_id` + whereSQL
	summary := &AITaskManagementSummary{}
	if err := m.DB.QueryRow(query, args...).Scan(&summary.TotalCount, &summary.PendingCount, &summary.RunningCount, &summary.FailedCount); err != nil {
		return nil, err
	}
	return summary, nil
}

// extractRefundedStones 从结果载荷 JSON 里提取 refunded_stones（无退款/解析失败返回 0）
func extractRefundedStones(resultPayload string) int64 {
	payload := strings.TrimSpace(resultPayload)
	if payload == "" || !strings.Contains(payload, "refunded_stones") {
		return 0
	}
	var parsed struct {
		RefundedStones int64 `json:"refunded_stones"`
	}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return 0
	}
	return parsed.RefundedStones
}

// queueConditionForManagement 把运营队列（任务中心队列 Tab）翻译为 SQL 片段。
// 队列口径与 QueueCounts 保持一致：failed=近7日失败；timeout=处理中超30分钟；
// running=处理中/待处理；today=今日新增；all=全量。
func queueConditionForManagement(queue string) (string, []interface{}) {
	switch queue {
	case "failed":
		return " AND a.status = 'failed' AND a.created_at >= NOW() - INTERVAL 7 DAY", nil
	case "timeout":
		return " AND a.status IN ('pending', 'running', 'processing') AND a.updated_at < NOW() - INTERVAL 30 MINUTE", nil
	case "running":
		return " AND a.status IN ('pending', 'running', 'processing')", nil
	case "today":
		return " AND a.created_at >= CURDATE()", nil
	default:
		return "", nil
	}
}

// ListForManagementQueue 按运营队列列出图片任务（在原检索条件上叠加队列过滤）。
// 队列与状态筛选互斥：选了 failed/timeout/running 队列就由队列决定状态范围。
func (m *AITaskModel) ListForManagementQueue(queue, keyword, status, scene string, limit, offset int) ([]*AITaskManagementItem, int64, error) {
	effectiveStatus := status
	switch queue {
	case "failed", "timeout", "running":
		effectiveStatus = "all"
	}
	whereSQL, args := m.buildManagementWhere(keyword, effectiveStatus, scene)
	queueCond, queueArgs := queueConditionForManagement(queue)
	whereSQL += queueCond
	args = append(append([]interface{}{}, args...), queueArgs...)

	countQuery := `SELECT COUNT(*) FROM ai_tasks a LEFT JOIN users u ON u.id = a.user_id` + whereSQL
	var total int64
	if err := m.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	query := `
		SELECT a.id, a.task_no, a.user_id, COALESCE(u.username, ''), a.scene, COALESCE(a.model, ''), COALESCE(a.api_endpoint, ''), COALESCE(a.request_payload, ''), COALESCE(a.result_payload, ''), a.status, a.stones_used, COALESCE(a.error_message, ''), a.created_at, a.updated_at
		FROM ai_tasks a
		LEFT JOIN users u ON u.id = a.user_id` + whereSQL + `
		ORDER BY a.created_at DESC
		LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]*AITaskManagementItem, 0)
	for rows.Next() {
		item := &AITaskManagementItem{}
		if err := rows.Scan(&item.ID, &item.TaskNo, &item.UserID, &item.Username, &item.Scene, &item.Model, &item.APIEndpoint, &item.RequestPayload, &item.ResultPayload, &item.Status, &item.StonesUsed, &item.ErrorMessage, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		item.RefundedStones = extractRefundedStones(item.ResultPayload)
		list = append(list, item)
	}
	return list, total, rows.Err()
}

func (m *AITaskModel) BackfillResolvedExecutionMetaForSuccessTasks(limit int) (int, int, error) {
	if limit < 1 {
		limit = 200
	}
	if limit > 5000 {
		limit = 5000
	}
	query := `
		SELECT task_no, COALESCE(model, ''), COALESCE(api_endpoint, ''), COALESCE(request_payload, ''), COALESCE(result_payload, '')
		FROM ai_tasks
		WHERE status = 'success' AND (COALESCE(model, '') = '' OR COALESCE(api_endpoint, '') = '')
		ORDER BY updated_at DESC, id DESC
		LIMIT ?`
	rows, err := m.DB.Query(query, limit)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	updatedCount := 0
	inspectedCount := 0
	for rows.Next() {
		var taskNo string
		var rawModel string
		var rawEndpoint string
		var requestPayload string
		var resultPayload string
		if err := rows.Scan(&taskNo, &rawModel, &rawEndpoint, &requestPayload, &resultPayload); err != nil {
			return updatedCount, inspectedCount, err
		}
		inspectedCount++
		task := &AITask{
			TaskNo:         taskNo,
			Model:          sql.NullString{String: strings.TrimSpace(rawModel), Valid: strings.TrimSpace(rawModel) != ""},
			APIEndpoint:    sql.NullString{String: strings.TrimSpace(rawEndpoint), Valid: strings.TrimSpace(rawEndpoint) != ""},
			RequestPayload: requestPayload,
			ResultPayload:  sql.NullString{String: resultPayload, Valid: strings.TrimSpace(resultPayload) != ""},
		}
		resolvedModel := task.GetResolvedModel()
		resolvedEndpoint := task.GetResolvedAPIEndpoint()
		if strings.TrimSpace(resolvedModel) == "" && strings.TrimSpace(resolvedEndpoint) == "" {
			continue
		}
		if strings.TrimSpace(rawModel) == strings.TrimSpace(resolvedModel) && strings.TrimSpace(rawEndpoint) == strings.TrimSpace(resolvedEndpoint) {
			continue
		}
		if err := m.UpdateExecutionMetaByTaskNo(taskNo, resolvedModel, resolvedEndpoint); err != nil {
			return updatedCount, inspectedCount, err
		}
		updatedCount++
	}
	if err := rows.Err(); err != nil {
		return updatedCount, inspectedCount, err
	}
	return updatedCount, inspectedCount, nil
}
