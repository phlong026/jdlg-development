package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

var ErrAIVideoStudioConflict = errors.New("ai video studio revision conflict")

type AIVideoStudioStore struct {
	DB    *sql.DB
	Ready bool
}

func NewAIVideoStudioStore(db *sql.DB) *AIVideoStudioStore { return &AIVideoStudioStore{DB: db} }

func defaultAIVideoStudioLimits() map[string]int {
	return map[string]int{
		"max_concurrency": 3, "timeout_seconds": 900, "max_quantity": 1,
		"max_duration_768p": 15, "max_duration_2k": 15,
		"max_reference_images": 8, "max_reference_videos": 3,
		"max_reference_audios": 3, "max_reference_total": 12,
	}
}

type AIVideoStudioConfig struct {
	Enabled            bool                      `json:"enabled"`
	ProviderCode       string                    `json:"provider_code"`
	ProviderConfigured bool                      `json:"provider_configured"`
	Modes              map[string]bool           `json:"modes"`
	Limits             map[string]int            `json:"limits"`
	Prices             []AIVideoStudioPrice      `json:"prices"`
	PromptOptimize     AIVideoStudioPolishConfig `json:"prompt_optimize"`
	Version            int64                     `json:"version"`
	UpdatedAt          time.Time                 `json:"updated_at"`
}

type AIVideoStudioPolishConfig struct {
	Enabled        bool   `json:"enabled"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func defaultAIVideoStudioPolishConfig() AIVideoStudioPolishConfig {
	return AIVideoStudioPolishConfig{TimeoutSeconds: 45}
}

type AIVideoStudioPrice struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	BillingUnit string `json:"billing_unit"`
	UnitStones  *int64 `json:"unit_stones"`
	Enabled     bool   `json:"enabled"`
	Version     int64  `json:"version"`
}

type AIVideoStudioDraft struct {
	ID                int64           `json:"draft_id"`
	UserID            int64           `json:"-"`
	Name              string          `json:"name"`
	Mode              string          `json:"mode"`
	Prompt            string          `json:"prompt"`
	ReferencePrompt   string          `json:"reference_prompt"`
	FirstFrameMediaID string          `json:"first_frame_media_id"`
	LastFrameMediaID  string          `json:"last_frame_media_id"`
	References        json.RawMessage `json:"references"`
	Ratio             string          `json:"ratio"`
	Resolution        string          `json:"resolution"`
	Duration          int             `json:"duration"`
	Quantity          int             `json:"quantity"`
	Status            string          `json:"status"`
	Revision          int64           `json:"revision"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type AIVideoStudioPrompt struct {
	ID        int64     `json:"prompt_id"`
	Name      string    `json:"name"`
	Mode      string    `json:"mode"`
	Prompt    string    `json:"prompt"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AIVideoStudioQuote struct {
	ID            string          `json:"quote_id"`
	UserID        int64           `json:"-"`
	DraftID       int64           `json:"draft_id"`
	DraftRevision int64           `json:"draft_revision"`
	QuoteVersion  int64           `json:"quote_version"`
	InputHash     string          `json:"input_hash"`
	PriceCode     string          `json:"price_code"`
	UnitStones    int64           `json:"unit_stones"`
	BillingUnit   string          `json:"billing_unit"`
	BillableUnits int64           `json:"billable_units"`
	TotalStones   int64           `json:"total_stones"`
	InputSnapshot json.RawMessage `json:"input_snapshot"`
	ExpiresAt     time.Time       `json:"expires_at"`
	CreatedAt     time.Time       `json:"created_at"`
}

type AIVideoStudioLedgerItem struct {
	ID           int64     `json:"id"`
	BatchID      string    `json:"batch_id"`
	TaskID       string    `json:"task_id,omitempty"`
	UserID       int64     `json:"user_id"`
	Action       string    `json:"action"`
	Stones       int64     `json:"stones"`
	Status       string    `json:"status"`
	PriceCode    string    `json:"price_code"`
	PriceVersion int64     `json:"price_version"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *AIVideoStudioStore) InitTables() error {
	if s == nil || s.DB == nil {
		return errors.New("ai video studio database unavailable")
	}
	schema := []string{
		`CREATE TABLE IF NOT EXISTS ai_video_studio_config (
		 id TINYINT UNSIGNED NOT NULL PRIMARY KEY, enabled BOOLEAN NOT NULL DEFAULT FALSE,
		 provider_code VARCHAR(64) NOT NULL DEFAULT 'metaso_h3', modes_json JSON NOT NULL,
		 limits_json JSON NOT NULL, prices_json JSON NOT NULL, prompt_optimize_json JSON NULL, version BIGINT UNSIGNED NOT NULL DEFAULT 1,
		 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS ai_video_studio_drafts (
		 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, user_id BIGINT UNSIGNED NOT NULL,
		 name VARCHAR(120) NOT NULL, mode VARCHAR(32) NOT NULL, prompt TEXT NOT NULL, reference_prompt TEXT NOT NULL,
		 first_frame_media_id VARCHAR(128) NOT NULL DEFAULT '', last_frame_media_id VARCHAR(128) NOT NULL DEFAULT '',
		 references_json JSON NOT NULL, ratio VARCHAR(16) NOT NULL, resolution VARCHAR(16) NOT NULL,
		 duration_seconds INT NOT NULL, quantity INT NOT NULL, status VARCHAR(24) NOT NULL DEFAULT 'draft',
		 revision BIGINT UNSIGNED NOT NULL DEFAULT 1, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		 KEY idx_ai_video_studio_drafts_user_updated (user_id, updated_at), KEY idx_ai_video_studio_drafts_user_status (user_id, status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS ai_video_studio_prompts (
		 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, user_id BIGINT UNSIGNED NOT NULL,
		 name VARCHAR(120) NOT NULL, mode VARCHAR(32) NOT NULL, prompt TEXT NOT NULL, revision BIGINT UNSIGNED NOT NULL DEFAULT 1,
		 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		 KEY idx_ai_video_studio_prompts_user_updated (user_id, updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS ai_video_studio_quotes (
		 id CHAR(36) NOT NULL PRIMARY KEY, user_id BIGINT UNSIGNED NOT NULL, draft_id BIGINT UNSIGNED NOT NULL,
		 draft_revision BIGINT UNSIGNED NOT NULL, quote_version BIGINT UNSIGNED NOT NULL, input_hash CHAR(64) NOT NULL,
		 price_code VARCHAR(64) NOT NULL, unit_stones BIGINT UNSIGNED NOT NULL, billable_units BIGINT UNSIGNED NOT NULL,
		 total_stones BIGINT UNSIGNED NOT NULL, input_snapshot JSON NOT NULL, expires_at DATETIME NOT NULL,
		 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, KEY idx_ai_video_studio_quotes_user_created (user_id, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS ai_video_studio_batches (
		 id CHAR(36) NOT NULL PRIMARY KEY, user_id BIGINT UNSIGNED NOT NULL, draft_id BIGINT UNSIGNED NOT NULL,
		 quote_id CHAR(36) NOT NULL, idempotency_key VARCHAR(128) NOT NULL, mode VARCHAR(32) NOT NULL,
		 status VARCHAR(32) NOT NULL, input_snapshot JSON NOT NULL, fee_status VARCHAR(32) NOT NULL DEFAULT 'not_reserved',
		 total_stones BIGINT UNSIGNED NOT NULL DEFAULT 0, error_code VARCHAR(64) NOT NULL DEFAULT '', error_message VARCHAR(255) NOT NULL DEFAULT '',
		 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		 UNIQUE KEY uk_ai_video_studio_batch_idempotency (user_id, idempotency_key), KEY idx_ai_video_studio_batches_user_updated (user_id, updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS ai_video_studio_tasks (
		 id CHAR(36) NOT NULL PRIMARY KEY, batch_id CHAR(36) NOT NULL, user_id BIGINT UNSIGNED NOT NULL, item_index INT NOT NULL,
		 provider_code VARCHAR(64) NOT NULL DEFAULT '', external_id VARCHAR(191) NOT NULL DEFAULT '', status VARCHAR(32) NOT NULL,
		 attempt_count INT NOT NULL DEFAULT 0, result_url TEXT NULL, stable_media_id VARCHAR(128) NOT NULL DEFAULT '',
		 delivery_type VARCHAR(24) NOT NULL DEFAULT '', output_seconds DECIMAL(10,2) NOT NULL DEFAULT 0,
		 lease_owner VARCHAR(128) NOT NULL DEFAULT '', lease_until DATETIME NULL, next_poll_at DATETIME NULL,
		 error_code VARCHAR(64) NOT NULL DEFAULT '', error_message VARCHAR(255) NOT NULL DEFAULT '',
		 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		 UNIQUE KEY uk_ai_video_studio_task_item (batch_id, item_index), KEY idx_ai_video_studio_tasks_user_updated (user_id, updated_at),
		 KEY idx_ai_video_studio_tasks_status (status, updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS ai_video_studio_fees (
		 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, batch_id CHAR(36) NOT NULL, task_id CHAR(36) NOT NULL DEFAULT '',
		 user_id BIGINT UNSIGNED NOT NULL, quote_id CHAR(36) NOT NULL, kind VARCHAR(32) NOT NULL, amount BIGINT NOT NULL,
		 status VARCHAR(32) NOT NULL, reason VARCHAR(191) NOT NULL DEFAULT '', idempotency_key VARCHAR(191) NOT NULL,
		 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE KEY uk_ai_video_studio_fee_idempotency (idempotency_key),
		 KEY idx_ai_video_studio_fees_user_created (user_id, created_at), KEY idx_ai_video_studio_fees_batch (batch_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
	for _, statement := range schema {
		if _, err := s.DB.Exec(statement); err != nil {
			return fmt.Errorf("init ai video studio schema: %w", err)
		}
	}
	if _, err := s.DB.Exec(`ALTER TABLE ai_video_studio_config ADD COLUMN prompt_optimize_json JSON NULL AFTER prices_json`); err != nil {
		var sqlErr *mysql.MySQLError
		if !errors.As(err, &sqlErr) || sqlErr.Number != 1060 {
			return fmt.Errorf("add ai video studio prompt optimize config: %w", err)
		}
	}
	defaultLimits, marshalErr := json.Marshal(defaultAIVideoStudioLimits())
	if marshalErr != nil {
		return marshalErr
	}
	polish, _ := json.Marshal(defaultAIVideoStudioPolishConfig())
	_, err := s.DB.Exec(`INSERT IGNORE INTO ai_video_studio_config (id,modes_json,limits_json,prices_json,prompt_optimize_json) VALUES (1,?,?,?,?)`,
		`{"text":false,"first_frame":false,"first_last_frame":false,"reference":false}`,
		string(defaultLimits),
		`[]`, string(polish))
	if err != nil {
		return err
	}
	return s.InitPolishTables()
}

func scanAIVideoStudioDraft(scanner interface{ Scan(...interface{}) error }) (*AIVideoStudioDraft, error) {
	d := &AIVideoStudioDraft{}
	var refs string
	err := scanner.Scan(&d.ID, &d.UserID, &d.Name, &d.Mode, &d.Prompt, &d.ReferencePrompt, &d.FirstFrameMediaID, &d.LastFrameMediaID, &refs, &d.Ratio, &d.Resolution, &d.Duration, &d.Quantity, &d.Status, &d.Revision, &d.CreatedAt, &d.UpdatedAt)
	d.References = json.RawMessage(refs)
	return d, err
}

const aiVideoStudioDraftColumns = `id,user_id,name,mode,prompt,reference_prompt,first_frame_media_id,last_frame_media_id,references_json,ratio,resolution,duration_seconds,quantity,status,revision,created_at,updated_at`

func (s *AIVideoStudioStore) CreateDraft(ctx context.Context, d *AIVideoStudioDraft) error {
	result, err := s.DB.ExecContext(ctx, `INSERT INTO ai_video_studio_drafts (user_id,name,mode,prompt,reference_prompt,first_frame_media_id,last_frame_media_id,references_json,ratio,resolution,duration_seconds,quantity,status) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, d.UserID, d.Name, d.Mode, d.Prompt, d.ReferencePrompt, d.FirstFrameMediaID, d.LastFrameMediaID, string(d.References), d.Ratio, d.Resolution, d.Duration, d.Quantity, "draft")
	if err != nil {
		return err
	}
	d.ID, err = result.LastInsertId()
	if err != nil {
		return err
	}
	loaded, err := s.GetDraft(ctx, d.UserID, d.ID)
	if err != nil {
		return err
	}
	*d = *loaded
	return nil
}

func (s *AIVideoStudioStore) GetDraft(ctx context.Context, userID, id int64) (*AIVideoStudioDraft, error) {
	return scanAIVideoStudioDraft(s.DB.QueryRowContext(ctx, `SELECT `+aiVideoStudioDraftColumns+` FROM ai_video_studio_drafts WHERE id=? AND user_id=?`, id, userID))
}

func (s *AIVideoStudioStore) ListDrafts(ctx context.Context, userID int64, status, mode string, limit, offset int) ([]*AIVideoStudioDraft, int64, error) {
	where := "user_id=?"
	args := []interface{}{userID}
	if status != "" {
		where += " AND status=?"
		args = append(args, status)
	}
	if mode != "" {
		where += " AND mode=?"
		args = append(args, mode)
	}
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_drafts WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+aiVideoStudioDraftColumns+` FROM ai_video_studio_drafts WHERE `+where+` ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*AIVideoStudioDraft, 0)
	for rows.Next() {
		d, e := scanAIVideoStudioDraft(rows)
		if e != nil {
			return nil, 0, e
		}
		items = append(items, d)
	}
	return items, total, rows.Err()
}

func (s *AIVideoStudioStore) UpdateDraft(ctx context.Context, d *AIVideoStudioDraft, expected int64) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE ai_video_studio_drafts SET name=?,mode=?,prompt=?,reference_prompt=?,first_frame_media_id=?,last_frame_media_id=?,references_json=?,ratio=?,resolution=?,duration_seconds=?,quantity=?,revision=revision+1 WHERE id=? AND user_id=? AND revision=?`, d.Name, d.Mode, d.Prompt, d.ReferencePrompt, d.FirstFrameMediaID, d.LastFrameMediaID, string(d.References), d.Ratio, d.Resolution, d.Duration, d.Quantity, d.ID, d.UserID, expected)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrAIVideoStudioConflict
	}
	loaded, err := s.GetDraft(ctx, d.UserID, d.ID)
	if err != nil {
		return err
	}
	*d = *loaded
	return nil
}

func (s *AIVideoStudioStore) DeleteDraft(ctx context.Context, userID, id, expected int64) error {
	r, err := s.DB.ExecContext(ctx, `DELETE FROM ai_video_studio_drafts WHERE id=? AND user_id=? AND revision=?`, id, userID, expected)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrAIVideoStudioConflict
	}
	return nil
}

func (s *AIVideoStudioStore) CreatePrompt(ctx context.Context, userID int64, p *AIVideoStudioPrompt) error {
	r, err := s.DB.ExecContext(ctx, `INSERT INTO ai_video_studio_prompts (user_id,name,mode,prompt) VALUES (?,?,?,?)`, userID, p.Name, p.Mode, p.Prompt)
	if err != nil {
		return err
	}
	p.ID, err = r.LastInsertId()
	if err != nil {
		return err
	}
	return s.DB.QueryRowContext(ctx, `SELECT id,name,mode,prompt,revision,created_at,updated_at FROM ai_video_studio_prompts WHERE id=? AND user_id=?`, p.ID, userID).Scan(&p.ID, &p.Name, &p.Mode, &p.Prompt, &p.Revision, &p.CreatedAt, &p.UpdatedAt)
}

func (s *AIVideoStudioStore) ListPrompts(ctx context.Context, userID int64, limit, offset int) ([]AIVideoStudioPrompt, int64, error) {
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_prompts WHERE user_id=?`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,mode,prompt,revision,created_at,updated_at FROM ai_video_studio_prompts WHERE user_id=? ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]AIVideoStudioPrompt, 0)
	for rows.Next() {
		var p AIVideoStudioPrompt
		if err := rows.Scan(&p.ID, &p.Name, &p.Mode, &p.Prompt, &p.Revision, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, p)
	}
	return items, total, rows.Err()
}

func (s *AIVideoStudioStore) UpdatePrompt(ctx context.Context, userID int64, p *AIVideoStudioPrompt, expected int64) error {
	r, err := s.DB.ExecContext(ctx, `UPDATE ai_video_studio_prompts SET name=?,mode=?,prompt=?,revision=revision+1 WHERE id=? AND user_id=? AND revision=?`, p.Name, p.Mode, p.Prompt, p.ID, userID, expected)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrAIVideoStudioConflict
	}
	return s.DB.QueryRowContext(ctx, `SELECT id,name,mode,prompt,revision,created_at,updated_at FROM ai_video_studio_prompts WHERE id=? AND user_id=?`, p.ID, userID).Scan(&p.ID, &p.Name, &p.Mode, &p.Prompt, &p.Revision, &p.CreatedAt, &p.UpdatedAt)
}

func (s *AIVideoStudioStore) DeletePrompt(ctx context.Context, userID, id, expected int64) error {
	r, err := s.DB.ExecContext(ctx, `DELETE FROM ai_video_studio_prompts WHERE id=? AND user_id=? AND revision=?`, id, userID, expected)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrAIVideoStudioConflict
	}
	return nil
}

func (s *AIVideoStudioStore) Config(ctx context.Context, providerConfigured bool) (AIVideoStudioConfig, error) {
	var c AIVideoStudioConfig
	var modes, limits, prices string
	var polish sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT enabled,provider_code,modes_json,limits_json,prices_json,prompt_optimize_json,version,updated_at FROM ai_video_studio_config WHERE id=1`).Scan(&c.Enabled, &c.ProviderCode, &modes, &limits, &prices, &polish, &c.Version, &c.UpdatedAt); err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(modes), &c.Modes); err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(limits), &c.Limits); err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(prices), &c.Prices); err != nil {
		return c, err
	}
	c.PromptOptimize = defaultAIVideoStudioPolishConfig()
	if polish.Valid && strings.TrimSpace(polish.String) != "" {
		if err := json.Unmarshal([]byte(polish.String), &c.PromptOptimize); err != nil {
			return c, err
		}
	}
	c.ProviderConfigured = providerConfigured
	return c, nil
}

func (s *AIVideoStudioStore) UpdateConfig(ctx context.Context, c AIVideoStudioConfig, expected int64) error {
	modes, _ := json.Marshal(c.Modes)
	limits, _ := json.Marshal(c.Limits)
	prices, _ := json.Marshal(c.Prices)
	polish, _ := json.Marshal(c.PromptOptimize)
	r, err := s.DB.ExecContext(ctx, `UPDATE ai_video_studio_config SET enabled=?,provider_code=?,modes_json=?,limits_json=?,prices_json=?,prompt_optimize_json=?,version=version+1 WHERE id=1 AND version=?`, c.Enabled, strings.TrimSpace(c.ProviderCode), modes, limits, prices, polish, expected)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrAIVideoStudioConflict
	}
	return nil
}

func (s *AIVideoStudioStore) CreateQuote(ctx context.Context, q AIVideoStudioQuote) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO ai_video_studio_quotes (id,user_id,draft_id,draft_revision,quote_version,input_hash,price_code,unit_stones,billing_unit,billable_units,total_stones,input_snapshot,expires_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, q.ID, q.UserID, q.DraftID, q.DraftRevision, q.QuoteVersion, q.InputHash, q.PriceCode, q.UnitStones, q.BillingUnit, q.BillableUnits, q.TotalStones, string(q.InputSnapshot), q.ExpiresAt)
	return err
}

func (s *AIVideoStudioStore) ListBatches(ctx context.Context, userID int64, limit, offset int) ([]map[string]interface{}, int64, error) {
	return s.ListBatchesFiltered(ctx, userID, "", "", limit, offset)
}

func (s *AIVideoStudioStore) ListBatchesFiltered(ctx context.Context, userID int64, status, keyword string, limit, offset int) ([]map[string]interface{}, int64, error) {
	conditions := make([]string, 0, 3)
	args := []interface{}{}
	if userID > 0 {
		conditions = append(conditions, "user_id=?")
		args = append(args, userID)
	}
	if status != "" {
		conditions = append(conditions, "status=?")
		args = append(args, status)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		conditions = append(conditions, "(id LIKE ? OR quote_id LIKE ? OR CAST(user_id AS CHAR)=?)")
		args = append(args, like, like, keyword)
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_batches`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(args, limit, offset)
	rows, err := s.DB.QueryContext(ctx, `SELECT id,user_id,draft_id,quote_id,mode,status,fee_status,total_stones,error_code,error_message,input_snapshot,created_at,updated_at FROM ai_video_studio_batches`+where+` ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, quoteID, mode, status, feeStatus, errorCode, errorMessage, snapshot string
		var rowUserID, draftID, totalStones int64
		var created, updated time.Time
		if err := rows.Scan(&id, &rowUserID, &draftID, &quoteID, &mode, &status, &feeStatus, &totalStones, &errorCode, &errorMessage, &snapshot, &created, &updated); err != nil {
			return nil, 0, err
		}
		var input AIVideoStudioDraft
		if err := json.Unmarshal([]byte(snapshot), &input); err != nil {
			return nil, 0, fmt.Errorf("decode ai video studio batch snapshot: %w", err)
		}
		items = append(items, map[string]interface{}{"id": id, "batch_id": id, "name": input.Name, "user_id": rowUserID, "draft_id": draftID, "quote_id": quoteID, "mode": mode, "status": status, "fee_status": feeStatus, "total_stones": totalStones, "error_code": errorCode, "error_message": errorMessage, "created_at": created, "updated_at": updated})
	}
	return items, total, rows.Err()
}

func (s *AIVideoStudioStore) ListFeesForManagement(ctx context.Context, status, keyword string, limit, offset int) ([]AIVideoStudioLedgerItem, int64, error) {
	conditions := make([]string, 0, 2)
	args := make([]interface{}, 0, 6)
	if status != "" {
		conditions = append(conditions, "f.status=?")
		args = append(args, status)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		conditions = append(conditions, "(f.batch_id LIKE ? OR f.task_id LIKE ? OR f.quote_id LIKE ? OR CAST(f.user_id AS CHAR)=?)")
		args = append(args, like, like, like, keyword)
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_fees f`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := s.DB.QueryContext(ctx, `SELECT f.id,f.batch_id,f.task_id,f.user_id,f.kind,f.amount,f.status,q.price_code,q.quote_version,f.reason,f.created_at FROM ai_video_studio_fees f LEFT JOIN ai_video_studio_quotes q ON q.id=f.quote_id`+where+` ORDER BY f.created_at DESC,f.id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]AIVideoStudioLedgerItem, 0)
	for rows.Next() {
		var item AIVideoStudioLedgerItem
		var priceCode sql.NullString
		var priceVersion sql.NullInt64
		if err := rows.Scan(&item.ID, &item.BatchID, &item.TaskID, &item.UserID, &item.Action, &item.Stones, &item.Status, &priceCode, &priceVersion, &item.Reason, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		if priceCode.Valid {
			item.PriceCode = priceCode.String
		}
		if priceVersion.Valid {
			item.PriceVersion = priceVersion.Int64
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *AIVideoStudioStore) GetBatch(ctx context.Context, userID int64, id string) (map[string]interface{}, error) {
	var batchID, quoteID, mode, status, feeStatus, errorCode, errorMessage, snapshot string
	var draftID, totalStones int64
	var created, updated time.Time
	where := "id=?"
	args := []interface{}{id}
	if userID > 0 {
		where += " AND user_id=?"
		args = append(args, userID)
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT id,draft_id,quote_id,mode,status,input_snapshot,fee_status,total_stones,error_code,error_message,created_at,updated_at FROM ai_video_studio_batches WHERE `+where, args...).Scan(&batchID, &draftID, &quoteID, &mode, &status, &snapshot, &feeStatus, &totalStones, &errorCode, &errorMessage, &created, &updated); err != nil {
		return nil, err
	}
	canCancel := false
	tasks := make([]map[string]interface{}, 0)
	taskWhere := "batch_id=?"
	taskArgs := []interface{}{id}
	if userID > 0 {
		taskWhere += " AND user_id=?"
		taskArgs = append(taskArgs, userID)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,item_index,status,delivery_type,COALESCE(result_url,''),stable_media_id,output_seconds,error_code,error_message,created_at,updated_at,external_id FROM ai_video_studio_tasks WHERE `+taskWhere+` ORDER BY item_index`, taskArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID, taskStatus, delivery, resultURL, stableID, taskErrorCode, taskErrorMessage string
		var index int
		var output float64
		var taskCreated, taskUpdated time.Time
		var external string
		if err := rows.Scan(&taskID, &index, &taskStatus, &delivery, &resultURL, &stableID, &output, &taskErrorCode, &taskErrorMessage, &taskCreated, &taskUpdated, &external); err != nil {
			return nil, err
		}
		if taskStatus == "queued" {
			canCancel = true
		}
		retentionStatus := "not_retained"
		if stableID != "" {
			retentionStatus = "retained"
		}
		tasks = append(tasks, map[string]interface{}{"result_id": taskID, "item_index": index, "status": taskStatus, "delivery_type": delivery, "result_url": resultURL, "stable_media_id": stableID, "retention_status": retentionStatus, "output_seconds": output, "error_code": taskErrorCode, "error_message": taskErrorMessage, "created_at": taskCreated, "updated_at": taskUpdated})
		if userID == 0 {
			tasks[len(tasks)-1]["external_id"] = external
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var reserved, settled, released int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN status='reserved' THEN amount ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='settled' THEN amount ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='refunded' THEN amount ELSE 0 END),0) FROM ai_video_studio_fees WHERE batch_id=? AND kind='reserve'`, id).Scan(&reserved, &settled, &released); err != nil {
		return nil, err
	}
	return map[string]interface{}{"can_cancel": canCancel, "batch_id": batchID, "draft_id": draftID, "quote_id": quoteID, "mode": mode, "status": status, "snapshot": json.RawMessage(snapshot), "charge": map[string]interface{}{"fee_status": feeStatus, "total_stones": totalStones, "quoted_stones": totalStones, "reserved_stones": reserved, "settled_stones": settled, "released_stones": released}, "items": tasks, "results": tasks, "error_code": errorCode, "error_message": errorMessage, "created_at": created, "updated_at": updated}, rows.Err()
}
