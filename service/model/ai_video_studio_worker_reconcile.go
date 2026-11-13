package model

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"strings"
	"time"
	"unicode/utf8"
)

// Reconciliation reserves the same lease fence used by workers. Audit and the
// external identity are committed together before any terminal money transaction.
func (s *AIVideoStudioStore) ReconcileStudioTask(ctx context.Context, id, action, external, evidence string, actor int64, name string) error {
	if actor <= 0 || strings.TrimSpace(name) == "" || utf8.RuneCountInString(strings.TrimSpace(evidence)) < 10 || utf8.RuneCountInString(evidence) > 500 {
		return ErrAIVideoStudioConflict
	}
	if action != "attach_external_id" && action != "refund_confirmed_rejected" {
		return ErrAIVideoStudioConflict
	}
	if action == "attach_external_id" && (strings.TrimSpace(external) == "" || len(external) > 191) {
		return ErrAIVideoStudioConflict
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var configID int
	if e = tx.QueryRowContext(ctx, `SELECT id FROM ai_video_studio_config WHERE id=1 FOR UPDATE`).Scan(&configID); e != nil {
		return e
	}
	var status, known, result, owner string
	var lease sql.NullTime
	if e = tx.QueryRowContext(ctx, `SELECT status,external_id,COALESCE(result_url,''),lease_owner,lease_until FROM ai_video_studio_tasks WHERE id=? FOR UPDATE`, id).Scan(&status, &known, &result, &owner, &lease); e != nil {
		return e
	}
	var oldExternal string
	e = tx.QueryRowContext(ctx, `SELECT external_id FROM ai_video_studio_reconciliations WHERE task_id=? AND action=?`, id, action).Scan(&oldExternal)
	if e == nil && action == "attach_external_id" {
		if oldExternal != external {
			return ErrAIVideoStudioConflict
		}
		return tx.Commit()
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if action == "refund_confirmed_rejected" && status == "failed" && e == nil {
		return tx.Commit()
	}
	if status != "uncertain" || known != "" || result != "" || (owner != "" && lease.Valid && lease.Time.After(time.Now())) {
		return ErrAIVideoStudioConflict
	}
	if action == "attach_external_id" {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_tasks WHERE external_id=? AND id<>?`, external, id).Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return ErrAIVideoStudioConflict
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO ai_video_studio_reconciliations(task_id,action,external_id,actor_id,actor_name,evidence) VALUES(?,?,?,?,?,?)`, id, action, external, actor, name, evidence); e != nil {
		return e
	}
	fence := uuid.NewString()
	if action == "attach_external_id" {
		_, e = tx.ExecContext(ctx, `UPDATE ai_video_studio_tasks SET external_id=?,status='submitted',next_poll_at=NULL,lease_owner='',lease_until=NULL WHERE id=?`, external, id)
	} else {
		_, e = tx.ExecContext(ctx, `UPDATE ai_video_studio_tasks SET lease_owner=?,lease_until=DATE_ADD(NOW(),INTERVAL 180 SECOND) WHERE id=?`, fence, id)
	}
	if e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if action == "refund_confirmed_rejected" {
		return s.FinalizeTask(ctx, id, StudioTaskOutcome{Status: "failed", ErrorCode: "confirmed_not_accepted", ErrorMessage: "运营已确认供应商未受理", LeaseOwner: fence})
	}
	return nil
}

// FindStudioSubmission is read-only: reopening a page never turns a lost
// response into a new paid request.
func (s *AIVideoStudioStore) FindStudioSubmission(ctx context.Context, userID int64, key string) (map[string]interface{}, error) {
	if userID <= 0 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return nil, ErrAIVideoStudioConflict
	}
	var id string
	if e := s.DB.QueryRowContext(ctx, `SELECT id FROM ai_video_studio_batches WHERE user_id=? AND idempotency_key=?`, userID, key).Scan(&id); e != nil {
		return nil, e
	}
	return s.GetBatch(ctx, userID, id)
}
