package reportnotify

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"time"
)

type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) (*SQLStore, error) {
	if db == nil {
		return nil, ErrInvalidInput
	}
	return &SQLStore{db: db}, nil
}

func (s *SQLStore) VerifySchema(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT n.event_id,n.report_id,n.user_id,n.event_type,n.status,
n.external_user_id,n.attempt_count,n.receipt,n.error_code,n.lease_token,n.lease_until,n.next_attempt_at,
n.created_at,n.updated_at,r.paid,r.bound,r.external_user_id,r.protocol_version,r.status
FROM report_notifications n JOIN building_reports r ON r.report_id=n.report_id LIMIT 0`)
	if err != nil {
		return storeError("verify_schema", "")
	}
	if err := rows.Close(); err != nil {
		return storeError("verify_close", "")
	}
	return nil
}

// CatchUp does not need to be atomic with CompleteFull: the indexed unique event
// identity makes repeated scans safe, including reports completed while offline.
func (s *SQLStore) CatchUp(ctx context.Context, configured bool, limit int) (int64, error) {
	if limit < 1 || limit > BatchSize {
		return 0, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	state, code := StateManualRequired, "gateway_unconfigured"
	if configured {
		state, code = StatePending, ""
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO report_notifications
(event_id,report_id,user_id,event_type,status,error_code,next_attempt_at,created_at,updated_at)
SELECT SHA2(CONCAT('report_ready:',r.report_id),256),r.report_id,r.user_id,'report_ready',?,?,NOW(6),NOW(6),NOW(6)
FROM building_reports r
WHERE r.status='ready' AND r.paid=1 AND r.protocol_version='report-house-v1'
AND NOT EXISTS (SELECT 1 FROM report_notifications n WHERE n.report_id=r.report_id AND n.event_type='report_ready')
ORDER BY r.created_at,r.report_id LIMIT ?
ON DUPLICATE KEY UPDATE event_id=VALUES(event_id)`, state, code, limit)
	if err != nil {
		return 0, storeError("catchup_insert", "")
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, storeError("catchup_count", "")
	}
	if configured {
		// Previously unconfigured events become eligible after configuration. A
		// missing binding remains pending and Claim waits for its real owner.
		result, err = s.db.ExecContext(ctx, `UPDATE report_notifications SET status='pending',error_code='',next_attempt_at=NOW(6),updated_at=NOW(6)
WHERE status='manual_required' AND error_code='gateway_unconfigured'
ORDER BY created_at,event_id LIMIT ?`, limit)
		if err != nil {
			return count, storeError("catchup_reactivate", "")
		}
		reactivated, err := result.RowsAffected()
		if err != nil {
			return count, storeError("catchup_reactivate_count", "")
		}
		count += reactivated
	} else {
		// Keep outstanding unsent events visible for manual follow-up if the
		// gateway is disabled. Sending/uncertain events retain their audit state.
		result, err = s.db.ExecContext(ctx, `UPDATE report_notifications
SET status='manual_required',error_code='gateway_unconfigured',updated_at=NOW(6)
WHERE status='pending' ORDER BY created_at,event_id LIMIT ?`, limit)
		if err != nil {
			return count, storeError("catchup_manual", "")
		}
		manual, err := result.RowsAffected()
		if err != nil {
			return count, storeError("catchup_manual_count", "")
		}
		count += manual
	}
	return count, nil
}

func (s *SQLStore) Claim(ctx context.Context, lease time.Duration) (*Job, error) {
	if lease < 20*time.Second || lease > 2*time.Minute {
		return nil, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storeError("claim_begin", "")
	}
	defer tx.Rollback()
	// A crash can occur after sending and before storing its receipt. Expired
	// sends therefore become lookup-only jobs, never another delivery POST.
	_, err = tx.ExecContext(ctx, `UPDATE report_notifications
SET status='uncertain',error_code='delivery_lease_expired',lease_token=NULL,lease_until=NULL,next_attempt_at=NOW(6),updated_at=NOW(6)
WHERE status='sending' AND lease_until<NOW(6) ORDER BY lease_until,event_id LIMIT 100`)
	if err != nil {
		return nil, storeError("claim_expired", "")
	}
	var job Job
	var state string
	err = tx.QueryRowContext(ctx, `SELECT n.event_id,n.report_id,n.user_id,r.external_user_id,n.status,n.attempt_count
FROM report_notifications n JOIN building_reports r ON r.report_id=n.report_id AND r.user_id=n.user_id
WHERE n.status IN ('pending','uncertain') AND n.next_attempt_at<=NOW(6)
AND (n.lease_until IS NULL OR n.lease_until<NOW(6))
AND r.status='ready' AND r.paid=1 AND r.protocol_version='report-house-v1'
AND r.bound=1 AND COALESCE(r.external_user_id,'')<>''
ORDER BY n.next_attempt_at,n.created_at,n.event_id LIMIT 1 FOR UPDATE`).Scan(
		&job.EventID, &job.ReportID, &job.UserID, &job.ExternalUserID, &state, &job.AttemptCount)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, storeError("claim_empty_commit", "")
		}
		return nil, ErrNoJob
	}
	if err != nil {
		return nil, storeError("claim_select", "")
	}
	job.Reconcile = state == StateUncertain
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, storeError("claim_token", job.EventID)
	}
	job.LeaseToken = hex.EncodeToString(random[:])
	if !validJob(&job) {
		return nil, ErrInvalidInput
	}
	nextState, increment := StateSending, 1
	if job.Reconcile {
		nextState, increment = StateUncertain, 0
	}
	result, err := tx.ExecContext(ctx, `UPDATE report_notifications
SET status=?,external_user_id=?,attempt_count=attempt_count+?,lease_token=?,lease_until=DATE_ADD(NOW(6),INTERVAL ? MICROSECOND),updated_at=NOW(6)
WHERE event_id=? AND status=?`, nextState, job.ExternalUserID, increment, job.LeaseToken, lease.Microseconds(), job.EventID, state)
	if err != nil {
		return nil, storeError("claim_update", job.EventID)
	}
	if err := requireOne(result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, storeError("claim_commit", job.EventID)
	}
	job.AttemptCount += increment
	return &job, nil
}

func (s *SQLStore) Finish(ctx context.Context, job *Job, outcome Outcome) error {
	if !validJob(job) || !validOutcome(outcome) {
		return ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	expected := StateSending
	if job.Reconcile {
		expected = StateUncertain
	}
	delay := time.Duration(0)
	if outcome.State == StateUncertain {
		delay = LookupDelay
	}
	result, err := s.db.ExecContext(ctx, `UPDATE report_notifications
SET status=?,receipt=?,error_code=?,lease_token=NULL,lease_until=NULL,next_attempt_at=DATE_ADD(NOW(6),INTERVAL ? MICROSECOND),updated_at=NOW(6)
WHERE event_id=? AND status=? AND lease_token=? AND lease_until>NOW(6)`,
		outcome.State, outcome.Receipt, outcome.ErrorCode, delay.Microseconds(), job.EventID, expected, job.LeaseToken)
	if err != nil {
		return storeError("finish_update", job.EventID)
	}
	return requireOne(result)
}

func validOutcome(outcome Outcome) bool {
	switch outcome.State {
	case StateSent:
		return validText(outcome.Receipt, 256) && outcome.ErrorCode == ""
	case StateUncertain, StateManualRequired:
		return outcome.Receipt == "" && validText(outcome.ErrorCode, 64)
	default:
		return false
	}
}

func requireOne(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return storeError("rows_affected", "")
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}

func storeError(stage, eventID string) error {
	if !validEventID(eventID) {
		eventID = ""
	}
	log.Printf("[report-notify] error stage=%s event_id=%s result=store_unavailable", stage, eventID)
	return ErrStoreUnavailable
}
