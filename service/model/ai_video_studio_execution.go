package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"math"
	"strings"
	"time"
)

// One quote can fund exactly one batch, even if callers change their request key.
func (s *AIVideoStudioStore) InitExecutionTables() error {
	if _, err := s.DB.Exec(`ALTER TABLE ai_video_studio_quotes ADD COLUMN billing_unit VARCHAR(24) NOT NULL DEFAULT ''`); err != nil {
		var sqlErr *mysql.MySQLError
		if !errors.As(err, &sqlErr) || sqlErr.Number != 1060 {
			return err
		}
	}

	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS ai_video_studio_quote_consumptions(quote_id CHAR(36) PRIMARY KEY,user_id BIGINT UNSIGNED NOT NULL,batch_id CHAR(36) NOT NULL,UNIQUE KEY(batch_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	return err
}

func (s *AIVideoStudioStore) CreatePaidBatch(ctx context.Context, userID int64, quoteID, key string) (string, bool, error) {
	if s == nil || s.DB == nil {
		return "", false, ErrWalletUnavailable
	}
	if userID <= 0 || quoteID == "" || strings.TrimSpace(key) == "" || len(key) > 128 {
		return "", false, ErrAIVideoStudioConflict
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	// All execution transactions lock the wallet first. This also serializes legacy SQL wallet writes.
	var balance int64
	if err = tx.QueryRowContext(ctx, `SELECT stones FROM users WHERE id=? FOR UPDATE`, userID).Scan(&balance); err != nil {
		return "", false, err
	}
	var batchID, existingQuote string
	err = tx.QueryRowContext(ctx, `SELECT id,quote_id FROM ai_video_studio_batches WHERE user_id=? AND idempotency_key=? FOR UPDATE`, userID, key).Scan(&batchID, &existingQuote)
	if err == nil {
		if existingQuote != quoteID {
			return "", false, ErrAIVideoStudioConflict
		}
		return batchID, true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	q := AIVideoStudioQuote{}
	var snapshot, billing string
	err = tx.QueryRowContext(ctx, `SELECT draft_id,draft_revision,quote_version,price_code,unit_stones,billable_units,total_stones,input_snapshot,expires_at,billing_unit FROM ai_video_studio_quotes WHERE id=? AND user_id=? FOR UPDATE`, quoteID, userID).Scan(&q.DraftID, &q.DraftRevision, &q.QuoteVersion, &q.PriceCode, &q.UnitStones, &q.BillableUnits, &q.TotalStones, &snapshot, &q.ExpiresAt, &billing)
	if err != nil {
		return "", false, err
	}
	if !time.Now().Before(q.ExpiresAt) || q.TotalStones <= 0 {
		return "", false, ErrAIVideoStudioConflict
	}
	var consumed string
	err = tx.QueryRowContext(ctx, `SELECT batch_id FROM ai_video_studio_quote_consumptions WHERE quote_id=?`, quoteID).Scan(&consumed)
	if err == nil {
		return "", false, ErrAIVideoStudioConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	draft, err := scanAIVideoStudioDraft(tx.QueryRowContext(ctx, `SELECT `+aiVideoStudioDraftColumns+` FROM ai_video_studio_drafts WHERE id=? AND user_id=? FOR UPDATE`, q.DraftID, userID))
	if err != nil {
		return "", false, err
	}
	if draft.Revision != q.DraftRevision {
		return "", false, ErrAIVideoStudioConflict
	}
	var frozen AIVideoStudioDraft
	if err = json.Unmarshal([]byte(snapshot), &frozen); err != nil {
		return "", false, err
	}
	if frozen.ID != draft.ID || frozen.Revision != draft.Revision || frozen.Quantity != draft.Quantity || frozen.Mode != draft.Mode || frozen.Duration != draft.Duration || frozen.Resolution != draft.Resolution {
		return "", false, ErrAIVideoStudioConflict
	}
	var enabled bool
	var provider, modesJSON, pricesJSON string
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT enabled,provider_code,modes_json,prices_json,version FROM ai_video_studio_config WHERE id=1 FOR SHARE`).Scan(&enabled, &provider, &modesJSON, &pricesJSON, &version); err != nil {
		return "", false, err
	}
	var modes map[string]bool
	var prices []AIVideoStudioPrice
	if err = json.Unmarshal([]byte(modesJSON), &modes); err != nil {
		return "", false, err
	}
	if err = json.Unmarshal([]byte(pricesJSON), &prices); err != nil {
		return "", false, err
	}
	if !enabled || !modes[draft.Mode] {
		return "", false, ErrAIVideoStudioConflict
	}
	expectedCode := "ai_video_studio_" + draft.Resolution
	if draft.Resolution == "768p" && draft.Duration > 15 {
		expectedCode += "_long"
	}
	if q.PriceCode != expectedCode {
		return "", false, ErrAIVideoStudioConflict
	}
	var priceMatches int
	for _, p := range prices {
		if p.Code == q.PriceCode && p.Enabled && p.UnitStones != nil && *p.UnitStones > 0 {
			priceMatches++
		}
	}
	if priceMatches != 1 {
		return "", false, ErrAIVideoStudioConflict
	}
	if draft.Quantity < 1 || draft.Quantity > 100 || draft.Duration < 1 || q.UnitStones <= 0 {
		return "", false, ErrAIVideoStudioConflict
	}
	units := int64(draft.Quantity)
	switch billing {
	case "per_request":
		units = 1
	case "per_video":
	case "per_second":
		if int64(draft.Duration) > math.MaxInt64/units {
			return "", false, ErrAIVideoStudioConflict
		}
		units *= int64(draft.Duration)
	default:
		return "", false, ErrAIVideoStudioConflict
	}
	if q.UnitStones > math.MaxInt64/units || q.BillableUnits != units || q.TotalStones != units*q.UnitStones {
		return "", false, ErrAIVideoStudioConflict
	}
	// Re-check ownership/readiness under row locks; signed URLs are never authority.
	ids := []string{frozen.FirstFrameMediaID, frozen.LastFrameMediaID}
	var refs []struct {
		MediaID string `json:"media_id"`
	}
	if err = json.Unmarshal(frozen.References, &refs); err != nil {
		return "", false, err
	}
	for _, ref := range refs {
		ids = append(ids, ref.MediaID)
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		var ready string
		if err = tx.QueryRowContext(ctx, `SELECT status FROM ai_video_studio_media WHERE id=? AND user_id=? FOR SHARE`, id, userID).Scan(&ready); err != nil {
			return "", false, err
		}
		if ready != "ready" {
			return "", false, ErrAIVideoStudioConflict
		}
	}
	if balance < q.TotalStones {
		return "", false, ErrInsufficientStones
	}
	batchID = uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_video_studio_quote_consumptions(quote_id,user_id,batch_id) VALUES(?,?,?)`, quoteID, userID, batchID); err != nil {
		return "", false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET stones=stones-? WHERE id=?`, q.TotalStones, userID); err != nil {
		return "", false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_video_studio_batches(id,user_id,draft_id,quote_id,idempotency_key,mode,status,input_snapshot,fee_status,total_stones) VALUES(?,?,?,?,?,?,'queued',?,'reserved',?)`, batchID, userID, q.DraftID, quoteID, key, draft.Mode, snapshot, q.TotalStones); err != nil {
		return "", false, err
	}
	for i := 0; i < draft.Quantity; i++ {
		taskID := uuid.NewString()
		amount := q.TotalStones / int64(draft.Quantity)
		if int64(i) < q.TotalStones%int64(draft.Quantity) {
			amount++
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_video_studio_tasks(id,batch_id,user_id,item_index,provider_code,status) VALUES(?,?,?,?,?,'queued')`, taskID, batchID, userID, i, provider); err != nil {
			return "", false, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_video_studio_fees(batch_id,task_id,user_id,quote_id,kind,amount,status,reason,idempotency_key) VALUES(?,?,?,?,'reserve',?,'reserved',?,?)`, batchID, taskID, userID, quoteID, amount, q.PriceCode, taskID+":reserve"); err != nil {
			return "", false, err
		}
	}
	if err = NewStoneRecordModel(s.DB).CreateWithTx(tx, userID, "consume", q.TotalStones, "AI视频工作台预留", batchID); err != nil {
		return "", false, err
	}
	return batchID, false, tx.Commit()
}

type StudioTaskOutcome struct {
	Status, ResultURL, DeliveryType, ErrorCode, ErrorMessage, LeaseOwner string
	OutputSeconds                                                        float64
}

// FinalizeTask accepts only definitive outcomes. Unknown upstream submission stays uncertain.
func (s *AIVideoStudioStore) FinalizeTask(ctx context.Context, taskID string, o StudioTaskOutcome) error {
	if o.Status == "succeeded" {
		o.Status = "completed"
	}
	if o.Status != "completed" && o.Status != "failed" && o.Status != "cancelled" {
		return errors.New("nonterminal outcome cannot settle")
	}
	// A usable provider result survives a later ledger/commit failure. The worker can
	// retry settlement from this durable evidence without requesting paid work again.
	if o.Status == "completed" && o.ResultURL != "" {
		query := `UPDATE ai_video_studio_tasks SET result_url=?,delivery_type=?,output_seconds=? WHERE id=? AND status NOT IN ('completed','failed','cancelled')`
		args := []any{o.ResultURL, o.DeliveryType, o.OutputSeconds, taskID}
		if o.LeaseOwner != "" {
			query += ` AND lease_owner=? AND lease_until>NOW()`
			args = append(args, o.LeaseOwner)
		}
		_, err := s.DB.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		// MySQL reports changed rows, so a repeated identical durable result
		// can return zero. The transaction below checks the actual lease/state.
	}
	var userID int64
	var batchID string
	if err := s.DB.QueryRowContext(ctx, `SELECT user_id,batch_id FROM ai_video_studio_tasks WHERE id=?`, taskID).Scan(&userID, &batchID); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var balance int64
	if err = tx.QueryRowContext(ctx, `SELECT stones FROM users WHERE id=? FOR UPDATE`, userID).Scan(&balance); err != nil {
		return err
	}
	var batchStatus string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM ai_video_studio_batches WHERE id=? FOR UPDATE`, batchID).Scan(&batchStatus); err != nil {
		return err
	}
	var status, owner string
	var existingResult sql.NullString
	var lease sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT status,lease_owner,lease_until,result_url FROM ai_video_studio_tasks WHERE id=? FOR UPDATE`, taskID).Scan(&status, &owner, &lease, &existingResult); err != nil {
		return err
	}
	if status == "completed" || status == "failed" || status == "cancelled" {
		return tx.Commit()
	}
	if o.LeaseOwner != "" && (owner != o.LeaseOwner || !lease.Valid || !lease.Time.After(time.Now())) {
		return ErrAIVideoStudioConflict
	}
	if o.Status == "completed" && o.ResultURL == "" && (!existingResult.Valid || existingResult.String == "") {
		return errors.New("completed task requires durable result evidence")
	}
	if existingResult.Valid && existingResult.String != "" && o.Status != "completed" {
		return ErrAIVideoStudioConflict
	}
	if o.Status == "cancelled" && status != "queued" {
		return ErrAIVideoStudioConflict
	}
	var amount int64
	var feeStatus, quoteID string
	if err = tx.QueryRowContext(ctx, `SELECT amount,status,quote_id FROM ai_video_studio_fees WHERE task_id=? AND kind='reserve' FOR UPDATE`, taskID).Scan(&amount, &feeStatus, &quoteID); err != nil {
		return err
	}
	if feeStatus != "reserved" || amount < 0 {
		return ErrAIVideoStudioConflict
	}
	targetFee := "settled"
	if o.Status != "completed" {
		targetFee = "refunded"
		if balance > math.MaxInt64-amount {
			return errors.New("refund balance overflow")
		}
		if _, err = tx.ExecContext(ctx, `UPDATE users SET stones=stones+? WHERE id=?`, amount, userID); err != nil {
			return err
		}
		if amount > 0 {
			if err = NewStoneRecordModel(s.DB).CreateWithTx(tx, userID, "task", amount, "AI视频工作台退回", taskID); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_video_studio_fees(batch_id,task_id,user_id,quote_id,kind,amount,status,reason,idempotency_key) VALUES(?,?,?,?,'refund',?,'refunded',?,?)`, batchID, taskID, userID, quoteID, amount, o.Status, taskID+":refund"); err != nil {
			return err
		}
	}
	if o.Status == "completed" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_video_studio_fees(batch_id,task_id,user_id,quote_id,kind,amount,status,reason,idempotency_key) VALUES(?,?,?,?,'settle',?,'settled','completed',?)`, batchID, taskID, userID, quoteID, amount, taskID+":settle"); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_video_studio_fees SET status=? WHERE task_id=? AND kind='reserve'`, targetFee, taskID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_video_studio_tasks SET status=?,result_url=CASE WHEN ?='' THEN result_url ELSE ? END,delivery_type=CASE WHEN ?='' THEN delivery_type ELSE ? END,output_seconds=?,error_code=?,error_message=?,lease_owner='',lease_until=NULL WHERE id=?`, o.Status, o.ResultURL, o.ResultURL, o.DeliveryType, o.DeliveryType, o.OutputSeconds, o.ErrorCode, o.ErrorMessage, taskID); err != nil {
		return err
	}
	if err = refreshStudioBatch(ctx, tx, batchID); err != nil {
		return err
	}
	return tx.Commit()
}
func refreshStudioBatch(ctx context.Context, tx *sql.Tx, id string) error {
	var total, completed, failed, cancelled, uncertain int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(status='completed'),0),COALESCE(SUM(status='failed'),0),COALESCE(SUM(status='cancelled'),0),COALESCE(SUM(status='uncertain'),0) FROM ai_video_studio_tasks WHERE batch_id=?`, id).Scan(&total, &completed, &failed, &cancelled, &uncertain); err != nil {
		return err
	}
	status, fee := "running", "reserved"
	if uncertain > 0 {
		status = "uncertain"
	}
	if completed+failed+cancelled == total {
		fee = "settled"
		switch {
		case completed == total:
			status = "completed"
		case completed > 0:
			status = "partial_success"
		case cancelled == total:
			status = "cancelled"
		default:
			status = "failed"
		}
		if completed == 0 {
			fee = "refunded"
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE ai_video_studio_batches SET status=?,fee_status=? WHERE id=?`, status, fee, id)
	return err
}
func (s *AIVideoStudioStore) CancelQueued(ctx context.Context, userID int64, batchID string) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM ai_video_studio_tasks WHERE batch_id=? AND user_id=? AND status='queued'`, batchID, userID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.FinalizeTask(ctx, id, StudioTaskOutcome{Status: "cancelled"}); err != nil && !errors.Is(err, ErrAIVideoStudioConflict) {
			return fmt.Errorf("cancel %s: %w", id, err)
		}
	}
	return nil
}
