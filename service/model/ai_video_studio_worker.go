package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"service/aivideostudio"
	"time"
)

type StudioWorkerTask struct {
	LeaseUntil                                        time.Time
	OutputSeconds                                     float64
	PollAttempt                                       int
	ID, BatchID, ExternalID, Status, ResultURL, Owner string
	UserID                                            int64
	Snapshot                                          AIVideoStudioDraft
	Created                                           time.Time
	Timeout                                           int
}
type StudioWorker struct {
	Store    *AIVideoStudioStore
	Provider aivideostudio.Provider
	Build    func(context.Context, StudioWorkerTask) (aivideostudio.GenerationRequest, error)
}

type StudioWorkerError struct {
	Operation string
	TaskID    string
	Cause     error
}

func (e *StudioWorkerError) Error() string {
	category := "worker"
	if errors.Is(e.Cause, context.Canceled) || errors.Is(e.Cause, context.DeadlineExceeded) {
		category = "context"
	} else if errors.Is(e.Cause, ErrAIVideoStudioConflict) {
		category = "conflict"
	} else if e.Cause != nil {
		category = "database_or_dependency"
	}
	return "operation=" + e.Operation + " task_id=" + e.TaskID + " category=" + category
}

func (e *StudioWorkerError) Unwrap() error { return e.Cause }

func (s *AIVideoStudioStore) InitWorkerTables() error {
	_, e := s.DB.Exec(`CREATE TABLE IF NOT EXISTS ai_video_studio_reconciliations(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,task_id CHAR(36) NOT NULL,action VARCHAR(40) NOT NULL,external_id VARCHAR(191) NOT NULL DEFAULT '',actor_id BIGINT NOT NULL,actor_name VARCHAR(191) NOT NULL,evidence TEXT NOT NULL,created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,UNIQUE KEY(task_id,action)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	return e
}

// The config row serializes admission across processes. Unknown submissions count
// against capacity until reconciliation; lease expiry never permits resubmission.
func (s *AIVideoStudioStore) claimStudioTask(ctx context.Context) (*StudioWorkerTask, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var limits string
	if e = tx.QueryRowContext(ctx, `SELECT limits_json FROM ai_video_studio_config WHERE id=1 FOR UPDATE`).Scan(&limits); e != nil {
		return nil, e
	}
	var l map[string]int
	if e = json.Unmarshal([]byte(limits), &l); e != nil {
		return nil, e
	}
	max := l["max_concurrency"]
	if max < 1 {
		max = defaultAIVideoStudioLimits()["max_concurrency"]
	}
	if _, e = tx.ExecContext(ctx, `UPDATE ai_video_studio_tasks SET status='uncertain',error_code='submission_interrupted',lease_owner='',lease_until=NULL WHERE status='submitting' AND (lease_until IS NULL OR lease_until<=NOW()) AND external_id='' AND COALESCE(result_url,'')=''`); e != nil {
		return nil, e
	}
	var active int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_tasks WHERE status IN ('submitting','submitted','running','uncertain')`).Scan(&active); e != nil {
		return nil, e
	}
	condition := `((status IN ('submitted','running','uncertain') AND (external_id<>'' OR COALESCE(result_url,'')<>'')) OR (status='submitting' AND (external_id<>'' OR COALESCE(result_url,'')<>'')))`
	if active < max {
		condition += ` OR status='queued'`
	}
	t := &StudioWorkerTask{Owner: uuid.NewString(), Timeout: l["timeout_seconds"]}
	if t.Timeout < 1 {
		t.Timeout = 900
	}
	e = tx.QueryRowContext(ctx, `SELECT id,batch_id,user_id,external_id,status,COALESCE(result_url,''),created_at,attempt_count,output_seconds FROM ai_video_studio_tasks WHERE (`+condition+`) AND (lease_until IS NULL OR lease_until<=NOW()) AND (next_poll_at IS NULL OR next_poll_at<=NOW()) ORDER BY (status='queued'),created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&t.ID, &t.BatchID, &t.UserID, &t.ExternalID, &t.Status, &t.ResultURL, &t.Created, &t.PollAttempt, &t.OutputSeconds)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if e != nil {
		return nil, e
	}
	var snapshot string
	if e = tx.QueryRowContext(ctx, `SELECT input_snapshot FROM ai_video_studio_batches WHERE id=?`, t.BatchID).Scan(&snapshot); e != nil {
		return nil, e
	}
	if e = json.Unmarshal([]byte(snapshot), &t.Snapshot); e != nil {
		return nil, e
	}
	if t.Status == "queued" {
		t.Status = "submitting"
	}
	if _, e = tx.ExecContext(ctx, `UPDATE ai_video_studio_tasks SET status=?,lease_owner=?,lease_until=DATE_ADD(NOW(),INTERVAL 180 SECOND),attempt_count=attempt_count+1 WHERE id=?`, t.Status, t.Owner, t.ID); e != nil {
		return nil, e
	}

	if e = tx.QueryRowContext(ctx, `SELECT lease_until FROM ai_video_studio_tasks WHERE id=?`, t.ID).Scan(&t.LeaseUntil); e != nil {
		return nil, e
	}
	return t, tx.Commit()
}
func (s *AIVideoStudioStore) parkStudioTask(ctx context.Context, t *StudioWorkerTask, status, externalID, code string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var configID int
	if e = tx.QueryRowContext(ctx, `SELECT id FROM ai_video_studio_config WHERE id=1 FOR UPDATE`).Scan(&configID); e != nil {
		return e
	}
	if externalID != "" {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_video_studio_tasks WHERE external_id=? AND id<>?`, externalID, t.ID).Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			status, externalID, code = "uncertain", "", "external_identity_conflict"
		}
	}
	delay := 5 * (t.PollAttempt + 1)
	if delay > 60 {
		delay = 60
	}
	// All writes use a per-claim fence and a lease longer than the bounded network call.
	r, e := tx.ExecContext(ctx, `UPDATE ai_video_studio_tasks SET status=?,external_id=CASE WHEN ?='' THEN external_id ELSE ? END,error_code=?,next_poll_at=DATE_ADD(NOW(),INTERVAL ? SECOND),lease_owner='',lease_until=NULL WHERE id=? AND lease_owner=? AND lease_until>NOW()`, status, externalID, externalID, code, delay, t.ID, t.Owner)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrAIVideoStudioConflict
	}
	// Batch aggregation is read only here to avoid taking task then batch locks.
	if e = tx.Commit(); e != nil {
		return e
	}
	tx, e = s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id string
	if e = tx.QueryRowContext(ctx, `SELECT id FROM ai_video_studio_batches WHERE id=? FOR UPDATE`, t.BatchID).Scan(&id); e != nil {
		return e
	}
	if e = refreshStudioBatch(ctx, tx, t.BatchID); e != nil {
		return e
	}
	return tx.Commit()
}

func (w *StudioWorker) Step(ctx context.Context) error {
	if w.Store == nil || w.Store.DB == nil || w.Provider == nil || w.Build == nil {
		return errors.New("studio worker unavailable")
	}
	t, e := w.Store.claimStudioTask(ctx)
	if e != nil {
		return &StudioWorkerError{Operation: "claim", Cause: e}
	}
	// Recovery without an external ID cannot be queried; expose its durable hold.
	if _, e = w.Store.DB.ExecContext(ctx, `UPDATE ai_video_studio_batches b SET status='uncertain' WHERE status IN ('queued','running') AND EXISTS (SELECT 1 FROM ai_video_studio_tasks t WHERE t.batch_id=b.id AND t.status='uncertain')`); e != nil {
		return &StudioWorkerError{Operation: "sync_uncertain_batches", Cause: e}
	}
	if t == nil {
		return nil
	}
	if e = w.processStudioTask(ctx, t); e != nil {
		return &StudioWorkerError{Operation: "process", TaskID: t.ID, Cause: e}
	}
	return nil
}
func (w *StudioWorker) processStudioTask(ctx context.Context, t *StudioWorkerTask) error {
	if t.ResultURL != "" {
		return w.Store.FinalizeTask(ctx, t.ID, StudioTaskOutcome{Status: "completed", LeaseOwner: t.Owner, DeliveryType: "direct", OutputSeconds: t.OutputSeconds})
	}
	callCtx, cancel := context.WithDeadline(ctx, t.LeaseUntil.Add(-85*time.Second))
	defer cancel()
	if t.Status == "submitting" && t.ExternalID == "" {
		input, e := w.Build(callCtx, *t)
		if e != nil {
			return w.Store.FinalizeTask(ctx, t.ID, StudioTaskOutcome{Status: "failed", LeaseOwner: t.Owner, ErrorCode: "input_unavailable", ErrorMessage: "素材暂不可用"})
		}
		if e = callCtx.Err(); e != nil {
			return e
		}
		var owned int
		if e = w.Store.DB.QueryRowContext(callCtx, `SELECT COUNT(*) FROM ai_video_studio_tasks WHERE id=? AND lease_owner=? AND lease_until>NOW() AND status='submitting' AND external_id='' AND COALESCE(result_url,'')=''`, t.ID, t.Owner).Scan(&owned); e != nil {
			return e
		}
		if owned != 1 {
			return ErrAIVideoStudioConflict
		}
		receipt, e := w.Provider.Submit(callCtx, input)
		if errors.Is(e, aivideostudio.ErrSubmissionRejected) || errors.Is(e, aivideostudio.ErrProviderUnavailable) {
			return w.Store.FinalizeTask(ctx, t.ID, StudioTaskOutcome{Status: "failed", LeaseOwner: t.Owner, ErrorCode: "local_rejected"})
		}
		if e != nil || receipt.ExternalID == "" || len(receipt.ExternalID) > 191 {
			return w.Store.parkStudioTask(ctx, t, "uncertain", "", "submission_uncertain")
		}
		return w.Store.parkStudioTask(ctx, t, "submitted", receipt.ExternalID, "")
	}
	if t.ExternalID == "" {
		return w.Store.parkStudioTask(ctx, t, "uncertain", "", "submission_uncertain")
	}
	result, e := w.Provider.Query(callCtx, t.ExternalID)
	if e == nil {
		switch result.Status {
		case "succeeded", "completed":
			return w.Store.FinalizeTask(ctx, t.ID, StudioTaskOutcome{Status: "completed", ResultURL: result.ResultURL, DeliveryType: "direct", OutputSeconds: result.OutputSeconds, LeaseOwner: t.Owner})
		case "failed", "cancelled":
			return w.Store.FinalizeTask(ctx, t.ID, StudioTaskOutcome{Status: "failed", ErrorCode: "provider_failed", ErrorMessage: "供应商确认任务未完成", LeaseOwner: t.Owner})
		}
	}
	status, code := "running", ""
	if e != nil {
		code = "query_unavailable"
	}
	if time.Since(t.Created) > time.Duration(t.Timeout)*time.Second {
		status, code = "uncertain", "generation_timeout"
	}
	return w.Store.parkStudioTask(ctx, t, status, "", code)
}
