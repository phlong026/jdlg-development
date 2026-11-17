package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

const polishPriceCode = "ai_video_studio_prompt_optimize"

// AIVideoStudioSnapshotHash canonicalizes JSON before hashing because MySQL's
// JSON type may reorder object keys when persisting the quote snapshot.
func AIVideoStudioSnapshotHash(snapshot []byte) (string, error) {
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(snapshot))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

type AIVideoStudioPolishTask struct {
	TaskID          string          `json:"task_id"`
	UserID          int64           `json:"-"`
	QuoteID         string          `json:"quote_id"`
	DraftID         int64           `json:"draft_id"`
	DraftRevision   int64           `json:"draft_revision"`
	InputHash       string          `json:"input_hash"`
	InputSnapshot   json.RawMessage `json:"input_snapshot"`
	Status          string          `json:"status"`
	FeeStatus       string          `json:"fee_status"`
	TotalStones     int64           `json:"total_stones"`
	OptimizedPrompt string          `json:"optimized_prompt"`
	ErrorCode       string          `json:"error_code,omitempty"`
	ErrorMessage    string          `json:"error_message,omitempty"`
	IdempotencyKey  string          `json:"-"`
	LeaseOwner      string          `json:"-"`
	LeaseUntil      sql.NullTime    `json:"-"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

const polishTaskColumns = `id,user_id,quote_id,draft_id,draft_revision,input_hash,input_snapshot,status,fee_status,total_stones,optimized_prompt,error_code,error_message,idempotency_key,lease_owner,lease_until,created_at,updated_at`

func scanPolishTask(scanner interface{ Scan(...interface{}) error }) (*AIVideoStudioPolishTask, error) {
	t := &AIVideoStudioPolishTask{}
	var snapshot string
	err := scanner.Scan(&t.TaskID, &t.UserID, &t.QuoteID, &t.DraftID, &t.DraftRevision, &t.InputHash, &snapshot, &t.Status, &t.FeeStatus, &t.TotalStones, &t.OptimizedPrompt, &t.ErrorCode, &t.ErrorMessage, &t.IdempotencyKey, &t.LeaseOwner, &t.LeaseUntil, &t.CreatedAt, &t.UpdatedAt)
	t.InputSnapshot = json.RawMessage(snapshot)
	return t, err
}

func (s *AIVideoStudioStore) InitPolishTables() error {
	if s == nil || s.DB == nil {
		return errors.New("ai video studio database unavailable")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS ai_video_studio_prompt_tasks(
		 id CHAR(36) NOT NULL PRIMARY KEY,user_id BIGINT UNSIGNED NOT NULL,quote_id CHAR(36) NOT NULL,
		 draft_id BIGINT UNSIGNED NOT NULL,draft_revision BIGINT UNSIGNED NOT NULL,input_hash CHAR(64) NOT NULL,input_snapshot JSON NOT NULL,
		 idempotency_key VARCHAR(128) NOT NULL,status VARCHAR(32) NOT NULL,fee_status VARCHAR(32) NOT NULL,total_stones BIGINT UNSIGNED NOT NULL,
		 optimized_prompt TEXT NOT NULL,error_code VARCHAR(64) NOT NULL DEFAULT '',error_message VARCHAR(255) NOT NULL DEFAULT '',
		 lease_owner VARCHAR(128) NOT NULL DEFAULT '',lease_until DATETIME NULL,attempt_count INT NOT NULL DEFAULT 0,
		 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		 UNIQUE KEY uk_ai_video_studio_prompt_quote(quote_id),UNIQUE KEY uk_ai_video_studio_prompt_idempotency(user_id,idempotency_key),
		 KEY idx_ai_video_studio_prompt_status(status,updated_at),KEY idx_ai_video_studio_prompt_user(user_id,updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS ai_video_studio_prompt_fees(
		 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,task_id CHAR(36) NOT NULL,user_id BIGINT UNSIGNED NOT NULL,quote_id CHAR(36) NOT NULL,
		 kind VARCHAR(32) NOT NULL,amount BIGINT UNSIGNED NOT NULL,status VARCHAR(32) NOT NULL,reason VARCHAR(191) NOT NULL DEFAULT '',
		 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,UNIQUE KEY uk_ai_video_studio_prompt_fee(task_id,kind),
		 KEY idx_ai_video_studio_prompt_fee_user(user_id,created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS ai_video_studio_prompt_reconciliations(
		 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,task_id CHAR(36) NOT NULL,action VARCHAR(40) NOT NULL,
		 decision_hash CHAR(64) NOT NULL,actor_id BIGINT NOT NULL,actor_name VARCHAR(191) NOT NULL,evidence TEXT NOT NULL,
		 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,UNIQUE KEY uk_ai_video_studio_prompt_reconcile(task_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}
	for _, statement := range statements {
		if _, err := s.DB.Exec(statement); err != nil {
			return fmt.Errorf("init prompt polish schema: %w", err)
		}
	}
	return nil
}

func (s *AIVideoStudioStore) CreatePolishTask(ctx context.Context, userID int64, quoteID, key string) (*AIVideoStudioPolishTask, bool, error) {
	key = strings.TrimSpace(key)
	if s == nil || s.DB == nil {
		return nil, false, ErrWalletUnavailable
	}
	if userID < 1 || quoteID == "" || key == "" || len(key) > 128 {
		return nil, false, ErrAIVideoStudioConflict
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var balance int64
	if err = tx.QueryRowContext(ctx, `SELECT stones FROM users WHERE id=? FOR UPDATE`, userID).Scan(&balance); err != nil {
		return nil, false, err
	}
	existing, err := scanPolishTask(tx.QueryRowContext(ctx, `SELECT `+polishTaskColumns+` FROM ai_video_studio_prompt_tasks WHERE user_id=? AND idempotency_key=? FOR UPDATE`, userID, key))
	if err == nil {
		if existing.QuoteID != quoteID {
			return nil, false, ErrAIVideoStudioConflict
		}
		return existing, true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	q := AIVideoStudioQuote{}
	var snapshot, billing string
	if err = tx.QueryRowContext(ctx, `SELECT draft_id,draft_revision,quote_version,input_hash,price_code,unit_stones,billable_units,total_stones,input_snapshot,expires_at,billing_unit FROM ai_video_studio_quotes WHERE id=? AND user_id=? FOR UPDATE`, quoteID, userID).Scan(&q.DraftID, &q.DraftRevision, &q.QuoteVersion, &q.InputHash, &q.PriceCode, &q.UnitStones, &q.BillableUnits, &q.TotalStones, &snapshot, &q.ExpiresAt, &billing); err != nil {
		return nil, false, err
	}
	if !time.Now().Before(q.ExpiresAt) || q.PriceCode != polishPriceCode || billing != "per_request" || q.BillableUnits != 1 || q.UnitStones < 1 || q.TotalStones != q.UnitStones {
		return nil, false, fmt.Errorf("%w: invalid prompt quote", ErrAIVideoStudioConflict)
	}
	snapshotHash, hashErr := AIVideoStudioSnapshotHash([]byte(snapshot))
	if hashErr != nil {
		return nil, false, hashErr
	}
	if !strings.EqualFold(q.InputHash, snapshotHash) {
		return nil, false, fmt.Errorf("%w: prompt quote hash mismatch", ErrAIVideoStudioConflict)
	}
	draft, err := scanAIVideoStudioDraft(tx.QueryRowContext(ctx, `SELECT `+aiVideoStudioDraftColumns+` FROM ai_video_studio_drafts WHERE id=? AND user_id=? FOR UPDATE`, q.DraftID, userID))
	if err != nil {
		return nil, false, err
	}
	var frozen AIVideoStudioDraft
	if err = json.Unmarshal([]byte(snapshot), &frozen); err != nil {
		return nil, false, err
	}
	frozenReferencesHash, frozenReferencesErr := AIVideoStudioSnapshotHash(frozen.References)
	draftReferencesHash, draftReferencesErr := AIVideoStudioSnapshotHash(draft.References)
	if frozenReferencesErr != nil || draftReferencesErr != nil {
		return nil, false, fmt.Errorf("%w: invalid prompt references", ErrAIVideoStudioConflict)
	}
	if draft.Revision != q.DraftRevision || frozen.ID != draft.ID || frozen.Revision != draft.Revision || frozen.Name != draft.Name || frozen.Mode != draft.Mode || frozen.Prompt != draft.Prompt || frozen.ReferencePrompt != draft.ReferencePrompt || frozen.FirstFrameMediaID != draft.FirstFrameMediaID || frozen.LastFrameMediaID != draft.LastFrameMediaID || frozenReferencesHash != draftReferencesHash || frozen.Ratio != draft.Ratio || frozen.Resolution != draft.Resolution || frozen.Duration != draft.Duration || frozen.Quantity != draft.Quantity {
		return nil, false, fmt.Errorf("%w: prompt draft changed", ErrAIVideoStudioConflict)
	}
	var enabled bool
	var version int64
	var pricesJSON string
	var polishJSON sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT enabled,prices_json,prompt_optimize_json,version FROM ai_video_studio_config WHERE id=1 FOR SHARE`).Scan(&enabled, &pricesJSON, &polishJSON, &version); err != nil {
		return nil, false, err
	}
	if !enabled || version != q.QuoteVersion || !polishJSON.Valid {
		return nil, false, fmt.Errorf("%w: prompt config version changed", ErrAIVideoStudioConflict)
	}
	polish := defaultAIVideoStudioPolishConfig()
	if err = json.Unmarshal([]byte(polishJSON.String), &polish); err != nil {
		return nil, false, err
	}
	if !polish.Enabled || strings.TrimSpace(polish.Model) == "" {
		return nil, false, fmt.Errorf("%w: prompt polish disabled", ErrAIVideoStudioConflict)
	}
	var metadata struct {
		PolishModel string `json:"polish_model"`
	}
	if err = json.Unmarshal([]byte(snapshot), &metadata); err != nil {
		return nil, false, err
	}
	if metadata.PolishModel != "" && metadata.PolishModel != polish.Model {
		return nil, false, fmt.Errorf("%w: prompt model changed", ErrAIVideoStudioConflict)
	}
	var prices []AIVideoStudioPrice
	if err = json.Unmarshal([]byte(pricesJSON), &prices); err != nil {
		return nil, false, err
	}
	matches := 0
	for _, price := range prices {
		if price.Code == polishPriceCode && price.Enabled && price.BillingUnit == "per_request" && price.UnitStones != nil && *price.UnitStones == q.UnitStones {
			matches++
		}
	}
	if matches != 1 {
		return nil, false, fmt.Errorf("%w: prompt price changed", ErrAIVideoStudioConflict)
	}
	if balance < q.TotalStones {
		return nil, false, ErrInsufficientStones
	}
	taskID := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `UPDATE users SET stones=stones-? WHERE id=?`, q.TotalStones, userID); err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_video_studio_prompt_tasks(id,user_id,quote_id,draft_id,draft_revision,input_hash,input_snapshot,idempotency_key,status,fee_status,total_stones,optimized_prompt) VALUES(?,?,?,?,?,?,?,?,'queued','reserved',?,'')`, taskID, userID, quoteID, q.DraftID, q.DraftRevision, q.InputHash, snapshot, key, q.TotalStones); err != nil {
		var sqlErr *mysql.MySQLError
		if errors.As(err, &sqlErr) && sqlErr.Number == 1062 {
			return nil, false, ErrAIVideoStudioConflict
		}
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_video_studio_prompt_fees(task_id,user_id,quote_id,kind,amount,status,reason) VALUES(?,?,?,'reserve',?,'reserved',?)`, taskID, userID, quoteID, q.TotalStones, polishPriceCode); err != nil {
		return nil, false, err
	}
	if err = NewStoneRecordModel(s.DB).CreateWithTx(tx, userID, "consume", q.TotalStones, "AI视频提示词润色预留", taskID); err != nil {
		return nil, false, err
	}
	task, err := scanPolishTask(tx.QueryRowContext(ctx, `SELECT `+polishTaskColumns+` FROM ai_video_studio_prompt_tasks WHERE id=?`, taskID))
	if err != nil {
		return nil, false, err
	}
	return task, false, tx.Commit()
}

func (s *AIVideoStudioStore) GetPolishTask(ctx context.Context, userID int64, id string) (*AIVideoStudioPolishTask, error) {
	if userID < 0 {
		return nil, ErrAIVideoStudioConflict
	}
	if userID == 0 {
		return scanPolishTask(s.DB.QueryRowContext(ctx, `SELECT `+polishTaskColumns+` FROM ai_video_studio_prompt_tasks WHERE id=?`, id))
	}
	return scanPolishTask(s.DB.QueryRowContext(ctx, `SELECT `+polishTaskColumns+` FROM ai_video_studio_prompt_tasks WHERE id=? AND user_id=?`, id, userID))
}

func (s *AIVideoStudioStore) GetPolishSubmission(ctx context.Context, userID int64, key string) (*AIVideoStudioPolishTask, error) {
	return scanPolishTask(s.DB.QueryRowContext(ctx, `SELECT `+polishTaskColumns+` FROM ai_video_studio_prompt_tasks WHERE user_id=? AND idempotency_key=?`, userID, key))
}

func (s *AIVideoStudioStore) ListPolishTasks(ctx context.Context, status, keyword string, limit, offset int) ([]AIVideoStudioPolishTask, int64, error) {
	where, args := "1=1", []interface{}{}
	if status != "" {
		where += " AND status=?"
		args = append(args, status)
	}
	if keyword != "" {
		where += " AND (id LIKE ? OR quote_id LIKE ? OR CAST(user_id AS CHAR)=?)"
		like := "%" + keyword + "%"
		args = append(args, like, like, keyword)
	}
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_prompt_tasks WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+polishTaskColumns+` FROM ai_video_studio_prompt_tasks WHERE `+where+` ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]AIVideoStudioPolishTask, 0)
	for rows.Next() {
		item, err := scanPolishTask(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (s *AIVideoStudioStore) ClaimPolishTask(ctx context.Context) (*AIVideoStudioPolishTask, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var limits string
	if err = tx.QueryRowContext(ctx, `SELECT limits_json FROM ai_video_studio_config WHERE id=1 FOR UPDATE`).Scan(&limits); err != nil {
		return nil, err
	}
	var values map[string]int
	if err = json.Unmarshal([]byte(limits), &values); err != nil {
		return nil, err
	}
	max := values["max_concurrency"]
	if max < 1 {
		max = defaultAIVideoStudioLimits()["max_concurrency"]
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_video_studio_prompt_tasks SET status='uncertain',error_code='submission_interrupted',error_message='润色请求结果待核对',lease_owner='',lease_until=NULL WHERE status='submitting' AND lease_until<=NOW() AND optimized_prompt=''`); err != nil {
		return nil, err
	}
	var videoActive, polishActive int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_tasks WHERE status IN ('submitting','submitted','running','uncertain')`).Scan(&videoActive); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_prompt_tasks WHERE status IN ('submitting','uncertain')`).Scan(&polishActive); err != nil {
		return nil, err
	}
	if videoActive+polishActive >= max {
		return nil, tx.Commit()
	}
	item, err := scanPolishTask(tx.QueryRowContext(ctx, `SELECT `+polishTaskColumns+` FROM ai_video_studio_prompt_tasks WHERE status='queued' ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	item.LeaseOwner = uuid.NewString()
	if _, err = tx.ExecContext(ctx, `UPDATE ai_video_studio_prompt_tasks SET status='submitting',lease_owner=?,lease_until=DATE_ADD(NOW(),INTERVAL 180 SECOND),attempt_count=attempt_count+1 WHERE id=? AND status='queued'`, item.LeaseOwner, item.TaskID); err != nil {
		return nil, err
	}
	item.Status = "submitting"
	if err = tx.QueryRowContext(ctx, `SELECT lease_until FROM ai_video_studio_prompt_tasks WHERE id=?`, item.TaskID).Scan(&item.LeaseUntil); err != nil {
		return nil, err
	}
	return item, tx.Commit()
}

func validOptimizedPrompt(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 7000
}

func (s *AIVideoStudioStore) SavePolishResult(ctx context.Context, id, leaseOwner, result string) error {
	result = strings.TrimSpace(result)
	if !validOptimizedPrompt(result) {
		return ErrAIVideoStudioConflict
	}
	r, err := s.DB.ExecContext(ctx, `UPDATE ai_video_studio_prompt_tasks SET optimized_prompt=? WHERE id=? AND ((status='submitting' AND lease_owner=? AND lease_until>NOW()) OR (optimized_prompt=? AND status IN ('submitting','completed')))`, result, id, leaseOwner, result)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		var saved string
		if err = s.DB.QueryRowContext(ctx, `SELECT optimized_prompt FROM ai_video_studio_prompt_tasks WHERE id=?`, id).Scan(&saved); err != nil || saved != result {
			return ErrAIVideoStudioConflict
		}
	}
	return s.FinalizePolishTask(ctx, id, leaseOwner, "completed", "", "")
}

func (s *AIVideoStudioStore) FinalizePolishTask(ctx context.Context, id, leaseOwner, status, errorCode, errorMessage string) error {
	if status != "completed" && status != "failed" && status != "cancelled" {
		return errors.New("nonterminal polish outcome")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID int64
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM ai_video_studio_prompt_tasks WHERE id=?`, id).Scan(&userID); err != nil {
		return err
	}
	var balance int64
	if err = tx.QueryRowContext(ctx, `SELECT stones FROM users WHERE id=? FOR UPDATE`, userID).Scan(&balance); err != nil {
		return err
	}
	task, err := scanPolishTask(tx.QueryRowContext(ctx, `SELECT `+polishTaskColumns+` FROM ai_video_studio_prompt_tasks WHERE id=? FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if task.Status == "completed" || task.Status == "failed" || task.Status == "cancelled" {
		if task.Status == status {
			return tx.Commit()
		}
		return ErrAIVideoStudioConflict
	}
	if status == "cancelled" && task.Status != "queued" {
		return ErrAIVideoStudioConflict
	}
	if status == "failed" && leaseOwner == "" && task.Status != "uncertain" {
		return ErrAIVideoStudioConflict
	}
	if leaseOwner != "" && task.LeaseOwner != leaseOwner {
		return ErrAIVideoStudioConflict
	}
	if err = s.finalizePolishTaskTx(ctx, tx, task, status, errorCode, errorMessage); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AIVideoStudioStore) finalizePolishTaskTx(ctx context.Context, tx *sql.Tx, task *AIVideoStudioPolishTask, status, errorCode, errorMessage string) error {
	if status == "completed" {
		if !validOptimizedPrompt(task.OptimizedPrompt) {
			return ErrAIVideoStudioConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO ai_video_studio_prompt_fees(task_id,user_id,quote_id,kind,amount,status,reason) VALUES(?,?,?,'settle',?,'settled','prompt_result_saved')`, task.TaskID, task.UserID, task.QuoteID, task.TotalStones); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE ai_video_studio_prompt_tasks SET status='completed',fee_status='settled',error_code='',error_message='',lease_owner='',lease_until=NULL WHERE id=?`, task.TaskID); err != nil {
			return err
		}
		return nil
	}
	if task.FeeStatus == "reserved" {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET stones=stones+? WHERE id=?`, task.TotalStones, task.UserID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO ai_video_studio_prompt_fees(task_id,user_id,quote_id,kind,amount,status,reason) VALUES(?,?,?,'release',?,'released',?)`, task.TaskID, task.UserID, task.QuoteID, task.TotalStones, errorCode); err != nil {
			return err
		}
		if err := NewStoneRecordModel(s.DB).CreateWithTx(tx, task.UserID, "task", task.TotalStones, "AI视频提示词润色退回", task.TaskID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_video_studio_prompt_tasks SET status=?,fee_status='released',error_code=?,error_message=?,lease_owner='',lease_until=NULL WHERE id=?`, status, errorCode, truncatePolishError(errorMessage), task.TaskID); err != nil {
		return err
	}
	return nil
}

func truncatePolishError(value string) string {
	r := []rune(strings.TrimSpace(value))
	if len(r) > 255 {
		r = r[:255]
	}
	return string(r)
}

func (s *AIVideoStudioStore) MarkPolishUncertain(ctx context.Context, id, leaseOwner, errorMessage string) error {
	r, err := s.DB.ExecContext(ctx, `UPDATE ai_video_studio_prompt_tasks SET status='uncertain',error_code='submission_uncertain',error_message=?,lease_owner='',lease_until=NULL WHERE id=? AND status='submitting' AND lease_owner=? AND lease_until>NOW()`, truncatePolishError(errorMessage), id, leaseOwner)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrAIVideoStudioConflict
	}
	return nil
}

func (s *AIVideoStudioStore) CancelPolishTask(ctx context.Context, userID int64, id string) error {
	task, err := s.GetPolishTask(ctx, userID, id)
	if err != nil {
		return err
	}
	if task.Status == "cancelled" {
		return nil
	}
	if task.Status != "queued" {
		return ErrAIVideoStudioConflict
	}
	return s.FinalizePolishTask(ctx, id, "", "cancelled", "user_cancelled", "用户已取消")
}

func (s *AIVideoStudioStore) RecoverPolishTasks(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE ai_video_studio_prompt_tasks SET status='uncertain',error_code='submission_interrupted',error_message='润色请求结果待核对',lease_owner='',lease_until=NULL WHERE status='submitting' AND lease_until<=NOW() AND optimized_prompt=''`); err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,optimized_prompt FROM ai_video_studio_prompt_tasks WHERE optimized_prompt<>'' AND status IN ('submitting','uncertain')`)
	if err != nil {
		return err
	}
	var items [][2]string
	for rows.Next() {
		var id, result string
		if err = rows.Scan(&id, &result); err != nil {
			rows.Close()
			return err
		}
		items = append(items, [2]string{id, result})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range items {
		if err = s.FinalizePolishTask(ctx, item[0], "", "completed", "", ""); err != nil && !errors.Is(err, ErrAIVideoStudioConflict) {
			return err
		}
	}
	return nil
}

func (s *AIVideoStudioStore) ReconcilePolishTask(ctx context.Context, id, action, evidence, result string, actorID int64, actorName string) error {
	evidence, actorName = strings.TrimSpace(evidence), strings.TrimSpace(actorName)
	result = strings.TrimSpace(result)
	if id == "" || evidence == "" || len([]rune(evidence)) > 4000 || actorID < 1 || actorName == "" {
		return ErrAIVideoStudioConflict
	}
	if action != "confirmed_rejected" && action != "confirmed_result" {
		return ErrAIVideoStudioConflict
	}
	if action == "confirmed_rejected" && result != "" {
		return ErrAIVideoStudioConflict
	}
	if action == "confirmed_result" && !validOptimizedPrompt(result) {
		return ErrAIVideoStudioConflict
	}
	decisionSum := sha256.Sum256([]byte(action + "\x00" + result))
	decisionHash := hex.EncodeToString(decisionSum[:])
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID int64
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM ai_video_studio_prompt_tasks WHERE id=?`, id).Scan(&userID); err != nil {
		return err
	}
	var balance int64
	if err = tx.QueryRowContext(ctx, `SELECT stones FROM users WHERE id=? FOR UPDATE`, userID).Scan(&balance); err != nil {
		return err
	}
	task, err := scanPolishTask(tx.QueryRowContext(ctx, `SELECT `+polishTaskColumns+` FROM ai_video_studio_prompt_tasks WHERE id=? FOR UPDATE`, id))
	if err != nil {
		return err
	}
	var existingHash string
	existingErr := tx.QueryRowContext(ctx, `SELECT decision_hash FROM ai_video_studio_prompt_reconciliations WHERE task_id=? FOR UPDATE`, id).Scan(&existingHash)
	if existingErr == nil {
		if existingHash != decisionHash {
			return ErrAIVideoStudioConflict
		}
		return tx.Commit()
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		return existingErr
	}
	if task.Status != "uncertain" {
		return ErrAIVideoStudioConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_video_studio_prompt_reconciliations(task_id,action,decision_hash,actor_id,actor_name,evidence) VALUES(?,?,?,?,?,?)`, id, action, decisionHash, actorID, actorName, evidence); err != nil {
		return err
	}
	if action == "confirmed_result" {
		if _, err = tx.ExecContext(ctx, `UPDATE ai_video_studio_prompt_tasks SET optimized_prompt=? WHERE id=?`, result, id); err != nil {
			return err
		}
		task.OptimizedPrompt = result
		if err = s.finalizePolishTaskTx(ctx, tx, task, "completed", "", ""); err != nil {
			return err
		}
	} else if err = s.finalizePolishTaskTx(ctx, tx, task, "failed", "confirmed_rejected", "供应商确认未受理"); err != nil {
		return err
	}
	return tx.Commit()
}
