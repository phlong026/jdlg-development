package notification

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

type SQLStore struct {
	db     *sql.DB
	tenant string
}

var _ ConsumerStore = (*SQLStore)(nil)
var _ DeliveryStore = (*SQLStore)(nil)
var _ InboxStore = (*SQLStore)(nil)

func NewSQLStore(db *sql.DB, tenant string) (*SQLStore, error) {
	if db == nil || tenant == "" {
		return nil, ErrInvalidInput
	}
	return &SQLStore{db: db, tenant: tenant}, nil
}

func (s *SQLStore) VerifySchema(ctx context.Context) error {
	for _, query := range []string{
		`SELECT tenant_id,event_id,status,attempt_count,lease_token,lease_until,last_error FROM mini_notification_event_jobs LIMIT 0`,
		`SELECT tenant_id,notification_id,event_id,recipient_id,kind,title,body,route_key,content_id,share_id,created_at,read_at,cancelled_at FROM mini_notifications LIMIT 0`,
		`SELECT tenant_id,delivery_id,notification_id,recipient_id,channel,status,attempt_count,binding_version,lease_token,lease_until,next_attempt_at,provider_msg_id,provider_status,provider_received_at,last_error,created_at,updated_at FROM mini_notification_deliveries LIMIT 0`,
		`SELECT tenant_id,provider_msg_id,provider_status,received_at,created_at,updated_at FROM mini_notification_provider_receipts LIMIT 0`,
	} {
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			return storeError("verify_schema", err)
		}
		if err := rows.Close(); err != nil {
			return storeError("verify_schema_close", err)
		}
	}
	return nil
}

func (s *SQLStore) EnqueueEventTx(ctx context.Context, tx *sql.Tx, eventID string, at time.Time) error {
	if tx == nil || eventID == "" || len(eventID) > 128 || at.IsZero() {
		return ErrInvalidInput
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO mini_notification_event_jobs (tenant_id,event_id,status,attempt_count,created_at,updated_at) VALUES (?,?,'pending',0,?,?) ON DUPLICATE KEY UPDATE event_id=event_id`, s.tenant, eventID, at.UTC(), at.UTC())
	if err != nil {
		return storeError("enqueue_event", err)
	}
	return nil
}

func (s *SQLStore) ClaimEvent(ctx context.Context, now time.Time, lease time.Duration, maxAttempts int) (*EventJob, error) {
	if lease <= 0 || maxAttempts < 1 {
		return nil, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, storeError("claim_event_begin", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE mini_notification_event_jobs SET status='failed',lease_token=NULL,lease_until=NULL,updated_at=?
WHERE tenant_id=? AND status='processing' AND lease_until<=? AND attempt_count>=?`, now, s.tenant, now, maxAttempts); err != nil {
		return nil, storeError("claim_event_expire", err)
	}
	var eventID string
	var payload []byte
	err = tx.QueryRowContext(ctx, `SELECT event_id FROM mini_notification_event_jobs
WHERE tenant_id=? AND (status='pending' OR (status='processing' AND lease_until<=? AND attempt_count<?))
ORDER BY created_at,event_id LIMIT 1 FOR UPDATE`, s.tenant, now, maxAttempts).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, storeError("claim_event_empty_commit", err)
		}
		return nil, ErrNoJob
	}
	if err != nil {
		return nil, storeError("claim_event_select", err)
	}
	err = tx.QueryRowContext(ctx, `SELECT payload FROM tracking_records WHERE tenant_id=? AND record_id=? AND kind='notice_event'`, s.tenant, eventID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		_, deadErr := tx.ExecContext(ctx, `UPDATE mini_notification_event_jobs SET status='deadletter',lease_token=NULL,lease_until=NULL,last_error='missing_event',updated_at=? WHERE tenant_id=? AND event_id=?`, now, s.tenant, eventID)
		if deadErr != nil {
			return nil, storeError("claim_event_missing", deadErr)
		}
		if err := tx.Commit(); err != nil {
			return nil, storeError("claim_event_missing_commit", err)
		}
		return nil, ErrDeadLettered
	}
	if err != nil {
		return nil, storeError("claim_event_payload", err)
	}
	var event Event
	if json.Unmarshal(payload, &event) != nil || event.ID != eventID || !validEvent(event) {
		_, deadErr := tx.ExecContext(ctx, `INSERT INTO mini_notification_event_jobs
(tenant_id,event_id,status,attempt_count,last_error,created_at,updated_at)
VALUES (?,?,'deadletter',1,'malformed_event',?,?)
ON DUPLICATE KEY UPDATE status='deadletter',attempt_count=attempt_count+1,lease_token=NULL,lease_until=NULL,last_error='malformed_event',updated_at=VALUES(updated_at)`, s.tenant, eventID, now, now)
		if deadErr != nil {
			return nil, storeError("claim_event_deadletter", deadErr)
		}
		if err := tx.Commit(); err != nil {
			return nil, storeError("claim_event_deadletter_commit", err)
		}
		return nil, ErrDeadLettered
	}
	token, err := randomToken()
	if err != nil {
		return nil, storeError("claim_event_token", err)
	}
	leaseUntil := now.Add(lease)
	result, err := tx.ExecContext(ctx, `UPDATE mini_notification_event_jobs SET status='processing',attempt_count=attempt_count+1,lease_token=?,lease_until=?,last_error='',updated_at=? WHERE tenant_id=? AND event_id=? AND (status='pending' OR (status='processing' AND lease_until<=? AND attempt_count<?))`, token, leaseUntil, now, s.tenant, event.ID, now, maxAttempts)
	if err != nil {
		return nil, storeError("claim_event_update", err)
	}
	if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
		return nil, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, storeError("claim_event_commit", err)
	}
	return &EventJob{Event: event, LeaseToken: token}, nil
}

func (s *SQLStore) MaterializeEvent(ctx context.Context, job *EventJob, channel string, eligibility Eligibility) error {
	if job == nil || !validEvent(job.Event) || job.LeaseToken == "" || channel == "" || len(channel) > 32 {
		return ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return storeError("materialize_begin", err)
	}
	defer tx.Rollback()
	var status, leaseToken string
	if err = tx.QueryRowContext(ctx, `SELECT status,lease_token FROM mini_notification_event_jobs WHERE tenant_id=? AND event_id=? FOR UPDATE`, s.tenant, job.Event.ID).Scan(&status, &leaseToken); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return storeError("materialize_lock", err)
	}
	if status == "processed" {
		return tx.Commit()
	}
	if status != "processing" || leaseToken != job.LeaseToken {
		return ErrConflict
	}
	if !eligibility.Eligible {
		reason := eligibility.Reason
		if reason == "" || len(reason) > 128 {
			reason = "ineligible"
		}
		result, updateErr := tx.ExecContext(ctx, `UPDATE mini_notification_event_jobs SET status='cancelled',lease_token=NULL,lease_until=NULL,last_error=?,updated_at=? WHERE tenant_id=? AND event_id=? AND status='processing' AND lease_token=?`, reason, time.Now().UTC(), s.tenant, job.Event.ID, job.LeaseToken)
		if updateErr != nil {
			return storeError("materialize_cancel", updateErr)
		}
		if changed, rowsErr := result.RowsAffected(); rowsErr != nil || changed != 1 {
			return ErrConflict
		}
		return tx.Commit()
	}
	notice := noticeFromEvent(job.Event)
	_, err = tx.ExecContext(ctx, `INSERT INTO mini_notifications
(tenant_id,notification_id,event_id,recipient_id,kind,title,body,route_key,content_id,share_id,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE notification_id=notification_id`,
		s.tenant, notice.ID, notice.EventID, notice.RecipientID, notice.Kind, notice.Title, notice.Body, notice.Route, notice.ContentID, notice.ShareID, notice.CreatedAt)
	if err != nil {
		return storeError("materialize_inbox", err)
	}
	if eligibility.DeliveryEligible {
		if eligibility.BindingVersion <= 0 {
			return ErrInvalidInput
		}
		deliveryID := notice.ID + ":" + channel
		_, err = tx.ExecContext(ctx, `INSERT INTO mini_notification_deliveries
(tenant_id,delivery_id,notification_id,recipient_id,channel,status,attempt_count,binding_version,next_attempt_at,created_at,updated_at)
VALUES (?,?,?,?,?,'pending',0,?,?,?,?) ON DUPLICATE KEY UPDATE delivery_id=delivery_id`,
			s.tenant, deliveryID, notice.ID, notice.RecipientID, channel, eligibility.BindingVersion, notice.CreatedAt, notice.CreatedAt, notice.CreatedAt)
		if err != nil {
			return storeError("materialize_delivery", err)
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE mini_notification_event_jobs SET status='processed',lease_token=NULL,lease_until=NULL,updated_at=? WHERE tenant_id=? AND event_id=? AND status='processing' AND lease_token=?`, time.Now().UTC(), s.tenant, job.Event.ID, job.LeaseToken)
	if err != nil {
		return storeError("materialize_complete", err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return storeError("materialize_commit", err)
	}
	return nil
}

func (s *SQLStore) ClaimDelivery(ctx context.Context, channel string, now time.Time, lease time.Duration, maxAttempts int) (*DeliveryJob, error) {
	if channel == "" || len(channel) > 32 || lease <= 0 || maxAttempts < 1 {
		return nil, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, storeError("claim_delivery_begin", err)
	}
	defer tx.Rollback()
	// Once an in-flight lease expires, the provider may already have accepted
	// the request. Mark it unknown and require reconciliation; never blind retry.
	if _, err = tx.ExecContext(ctx, `UPDATE mini_notification_deliveries SET status='unknown',lease_token=NULL,lease_until=NULL,next_attempt_at=NULL,last_error='lease_expired_possible_commit',updated_at=?
WHERE tenant_id=? AND channel=? AND status='processing' AND lease_until<=?`, now, s.tenant, channel, now); err != nil {
		return nil, storeError("claim_delivery_expire", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mini_notification_deliveries SET status='failed',next_attempt_at=NULL,last_error='attempts_exhausted',updated_at=?
WHERE tenant_id=? AND channel=? AND status='pending' AND attempt_count>=?`, now, s.tenant, channel, maxAttempts); err != nil {
		return nil, storeError("claim_delivery_exhausted", err)
	}
	row := tx.QueryRowContext(ctx, `SELECT delivery_id,notification_id,recipient_id,channel,status,attempt_count,binding_version,lease_until,next_attempt_at,provider_msg_id,provider_status,created_at,updated_at
FROM mini_notification_deliveries
WHERE tenant_id=? AND channel=? AND attempt_count<? AND status='pending' AND (next_attempt_at IS NULL OR next_attempt_at<=?)
ORDER BY COALESCE(next_attempt_at,created_at),delivery_id LIMIT 1 FOR UPDATE`, s.tenant, channel, maxAttempts, now)
	job := &DeliveryJob{}
	var selectedLease, nextAttempt sql.NullTime
	err = row.Scan(&job.Delivery.ID, &job.Delivery.NotificationID, &job.Delivery.RecipientID, &job.Delivery.Channel, &job.Delivery.Status, &job.Delivery.AttemptCount, &job.Delivery.BindingVersion, &selectedLease, &nextAttempt, &job.Delivery.ProviderMessageID, &job.Delivery.ProviderStatus, &job.Delivery.CreatedAt, &job.Delivery.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, storeError("claim_delivery_empty_commit", err)
		}
		return nil, ErrNoJob
	}
	if err != nil {
		return nil, storeError("claim_delivery_select", err)
	}
	if selectedLease.Valid {
		job.Delivery.LeaseUntil = &selectedLease.Time
	}
	if nextAttempt.Valid {
		job.Delivery.NextAttemptAt = &nextAttempt.Time
	}
	var cancelledAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT event_id,recipient_id,kind,title,body,route_key,content_id,share_id,created_at,cancelled_at FROM mini_notifications WHERE tenant_id=? AND notification_id=?`, s.tenant, job.Delivery.NotificationID).Scan(
		&job.Notice.EventID, &job.Notice.RecipientID, &job.Notice.Kind, &job.Notice.Title, &job.Notice.Body, &job.Notice.Route, &job.Notice.ContentID, &job.Notice.ShareID, &job.Notice.CreatedAt, &cancelledAt)
	if errors.Is(err, sql.ErrNoRows) || err == nil && cancelledAt.Valid {
		reason := "notification_cancelled"
		if errors.Is(err, sql.ErrNoRows) {
			reason = "notification_missing"
		}
		result, updateErr := tx.ExecContext(ctx, `UPDATE mini_notification_deliveries SET status='cancelled',next_attempt_at=NULL,last_error=?,updated_at=? WHERE tenant_id=? AND delivery_id=? AND status='pending'`, reason, now, s.tenant, job.Delivery.ID)
		if updateErr = checkedUpdate("claim_delivery_cancel", result, updateErr); updateErr != nil {
			return nil, updateErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return nil, storeError("claim_delivery_cancel_commit", commitErr)
		}
		return nil, ErrNoJob
	}
	if err != nil {
		return nil, storeError("claim_delivery_notice", err)
	}
	job.Notice.ID, job.Notice.RecipientID = job.Delivery.NotificationID, job.Delivery.RecipientID
	token, err := randomToken()
	if err != nil {
		return nil, storeError("claim_delivery_token", err)
	}
	leaseUntil := now.Add(lease)
	result, err := tx.ExecContext(ctx, `UPDATE mini_notification_deliveries SET status='processing',attempt_count=attempt_count+1,lease_token=?,lease_until=?,next_attempt_at=NULL,updated_at=? WHERE tenant_id=? AND delivery_id=? AND status='pending'`, token, leaseUntil, now, s.tenant, job.Delivery.ID)
	if err != nil {
		return nil, storeError("claim_delivery_update", err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return nil, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, storeError("claim_delivery_commit", err)
	}
	job.Delivery.Status, job.Delivery.LeaseToken, job.Delivery.LeaseUntil = DeliveryProcessing, token, &leaseUntil
	job.Delivery.AttemptCount++
	return job, nil
}

func (s *SQLStore) CompleteDelivery(ctx context.Context, deliveryID, leaseToken, status, providerMessageID string) error {
	if deliveryID == "" || leaseToken == "" || status != DeliveryAccepted && status != DeliveryUnknown {
		return ErrInvalidInput
	}
	if len(providerMessageID) > 128 {
		return ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return storeError("complete_delivery_begin", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE mini_notification_deliveries SET status=?,provider_msg_id=?,lease_token=NULL,lease_until=NULL,next_attempt_at=NULL,last_error='',updated_at=? WHERE tenant_id=? AND delivery_id=? AND status='processing' AND lease_token=?`, status, providerMessageID, now, s.tenant, deliveryID, leaseToken)
	if err = checkedUpdate("complete_delivery", result, err); err != nil {
		return err
	}
	if providerMessageID != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE mini_notification_deliveries d JOIN mini_notification_provider_receipts r ON r.tenant_id=d.tenant_id AND r.provider_msg_id=d.provider_msg_id SET d.provider_status=r.provider_status,d.provider_received_at=r.received_at,d.updated_at=? WHERE d.tenant_id=? AND d.delivery_id=?`, now, s.tenant, deliveryID); err != nil {
			return storeError("complete_delivery_reconcile", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return storeError("complete_delivery_commit", err)
	}
	return nil
}

func (s *SQLStore) FailDelivery(ctx context.Context, deliveryID, leaseToken, reason string, nextAttempt time.Time) error {
	if deliveryID == "" || leaseToken == "" || reason == "" || len(reason) > 128 {
		return ErrInvalidInput
	}
	status := DeliveryFailed
	var next any
	if !nextAttempt.IsZero() {
		status = DeliveryPending
		next = nextAttempt.UTC()
	}
	result, err := s.db.ExecContext(ctx, `UPDATE mini_notification_deliveries SET status=?,lease_token=NULL,lease_until=NULL,next_attempt_at=?,last_error=?,updated_at=? WHERE tenant_id=? AND delivery_id=? AND status='processing' AND lease_token=?`, status, next, reason, time.Now().UTC(), s.tenant, deliveryID, leaseToken)
	return checkedUpdate("fail_delivery", result, err)
}

func (s *SQLStore) CancelDelivery(ctx context.Context, deliveryID, leaseToken, reason string, at time.Time) error {
	if deliveryID == "" || leaseToken == "" || reason == "" || len(reason) > 128 || at.IsZero() {
		return ErrInvalidInput
	}
	result, err := s.db.ExecContext(ctx, `UPDATE mini_notification_deliveries SET status='cancelled',lease_token=NULL,lease_until=NULL,next_attempt_at=NULL,last_error=?,updated_at=? WHERE tenant_id=? AND delivery_id=? AND status='processing' AND lease_token=?`, reason, at.UTC(), s.tenant, deliveryID, leaseToken)
	return checkedUpdate("cancel_delivery", result, err)
}

func (s *SQLStore) RecordProviderReceipt(ctx context.Context, providerMessageID, status string, at time.Time) (bool, error) {
	if providerMessageID == "" || len(providerMessageID) > 128 || status == "" || len(status) > 128 || at.IsZero() {
		return false, ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mini_notification_provider_receipts (tenant_id,provider_msg_id,provider_status,received_at,created_at,updated_at) VALUES (?,?,?,?,?,?) ON DUPLICATE KEY UPDATE provider_status=VALUES(provider_status),received_at=VALUES(received_at),updated_at=VALUES(updated_at)`, s.tenant, providerMessageID, status, at.UTC(), at.UTC(), at.UTC())
	if err != nil {
		return false, storeError("provider_receipt_store", err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE mini_notification_deliveries SET provider_status=?,provider_received_at=?,updated_at=? WHERE tenant_id=? AND provider_msg_id=?`, status, at.UTC(), at.UTC(), s.tenant, providerMessageID)
	if err != nil {
		return false, storeError("provider_receipt", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, storeError("provider_receipt_result", err)
	}
	return changed > 0, nil
}

func (s *SQLStore) ListInbox(ctx context.Context, recipientID int64, cursor string, limit int) (Page, error) {
	if recipientID <= 0 || limit < 1 || limit > 100 {
		return Page{}, ErrInvalidInput
	}
	query := `SELECT notification_id,event_id,recipient_id,kind,title,body,route_key,content_id,share_id,created_at,read_at,cancelled_at
FROM mini_notifications WHERE tenant_id=? AND recipient_id=? AND cancelled_at IS NULL`
	args := []any{s.tenant, recipientID}
	if cursor != "" {
		beforeAt, beforeID, err := decodeCursor(cursor)
		if err != nil {
			return Page{}, ErrInvalidInput
		}
		query += ` AND (created_at<? OR (created_at=? AND notification_id<?))`
		args = append(args, beforeAt, beforeAt, beforeID)
	}
	query += ` ORDER BY created_at DESC,notification_id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, storeError("list_inbox", err)
	}
	defer rows.Close()
	page := Page{}
	for rows.Next() {
		var item Notification
		var readAt, cancelledAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.EventID, &item.RecipientID, &item.Kind, &item.Title, &item.Body, &item.Route, &item.ContentID, &item.ShareID, &item.CreatedAt, &readAt, &cancelledAt); err != nil {
			return Page{}, storeError("list_inbox_scan", err)
		}
		if readAt.Valid {
			item.ReadAt = &readAt.Time
		}
		if cancelledAt.Valid {
			item.CancelledAt = &cancelledAt.Time
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return Page{}, storeError("list_inbox_rows", err)
	}
	if len(page.Items) > limit {
		page.NextCursor = encodeCursor(page.Items[limit-1].CreatedAt, page.Items[limit-1].ID)
		page.Items = page.Items[:limit]
	}
	return page, nil
}

func (s *SQLStore) GetNotification(ctx context.Context, recipientID int64, notificationID string) (Notification, error) {
	if recipientID <= 0 || notificationID == "" {
		return Notification{}, ErrInvalidInput
	}
	var item Notification
	var readAt, cancelledAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT notification_id,event_id,recipient_id,kind,title,body,route_key,content_id,share_id,created_at,read_at,cancelled_at
FROM mini_notifications WHERE tenant_id=? AND recipient_id=? AND notification_id=? AND cancelled_at IS NULL`, s.tenant, recipientID, notificationID).Scan(
		&item.ID, &item.EventID, &item.RecipientID, &item.Kind, &item.Title, &item.Body, &item.Route, &item.ContentID, &item.ShareID, &item.CreatedAt, &readAt, &cancelledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Notification{}, ErrNotFound
	}
	if err != nil {
		return Notification{}, storeError("get_notification", err)
	}
	if readAt.Valid {
		item.ReadAt = &readAt.Time
	}
	if cancelledAt.Valid {
		item.CancelledAt = &cancelledAt.Time
	}
	return item, nil
}

func (s *SQLStore) UnreadCount(ctx context.Context, recipientID int64) (int64, error) {
	if recipientID <= 0 {
		return 0, ErrInvalidInput
	}
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mini_notifications WHERE tenant_id=? AND recipient_id=? AND read_at IS NULL AND cancelled_at IS NULL`, s.tenant, recipientID).Scan(&count); err != nil {
		return 0, storeError("unread_count", err)
	}
	return count, nil
}

func (s *SQLStore) MarkRead(ctx context.Context, recipientID int64, notificationID string, at time.Time) error {
	if recipientID <= 0 || notificationID == "" || at.IsZero() {
		return ErrInvalidInput
	}
	result, err := s.db.ExecContext(ctx, `UPDATE mini_notifications SET read_at=COALESCE(read_at,?) WHERE tenant_id=? AND notification_id=? AND recipient_id=? AND cancelled_at IS NULL`, at, s.tenant, notificationID, recipientID)
	return checkedUpdate("mark_read", result, err)
}

func (s *SQLStore) MarkAllRead(ctx context.Context, recipientID int64, at time.Time) (int64, error) {
	if recipientID <= 0 || at.IsZero() {
		return 0, ErrInvalidInput
	}
	result, err := s.db.ExecContext(ctx, `UPDATE mini_notifications SET read_at=? WHERE tenant_id=? AND recipient_id=? AND read_at IS NULL AND cancelled_at IS NULL`, at.UTC(), s.tenant, recipientID)
	if err != nil {
		return 0, storeError("mark_all_read", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, storeError("mark_all_read_result", err)
	}
	return changed, nil
}

func (s *SQLStore) CancelPending(ctx context.Context, contentID, shareID string, at time.Time) (int64, error) {
	if contentID == "" && shareID == "" || at.IsZero() {
		return 0, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, storeError("cancel_begin", err)
	}
	defer tx.Rollback()
	changed, err := s.cancelPendingTx(ctx, tx, contentID, shareID, at)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, storeError("cancel_commit", err)
	}
	return changed, nil
}

func (s *SQLStore) CancelPendingTx(ctx context.Context, tx *sql.Tx, contentID, shareID string, at time.Time) error {
	if tx == nil || (contentID == "" && shareID == "") || at.IsZero() {
		return ErrInvalidInput
	}
	_, err := s.cancelPendingTx(ctx, tx, contentID, shareID, at)
	return err
}

func (s *SQLStore) cancelPendingTx(ctx context.Context, tx *sql.Tx, contentID, shareID string, at time.Time) (int64, error) {
	jobWhere, jobArgs := "j.tenant_id=? AND j.status IN ('pending','processing')", []any{s.tenant}
	if contentID != "" {
		jobWhere += " AND JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.content_id'))=?"
		jobArgs = append(jobArgs, contentID)
	}
	if shareID != "" {
		jobWhere += " AND JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.share_id'))=?"
		jobArgs = append(jobArgs, shareID)
	}
	_, err := tx.ExecContext(ctx, `UPDATE mini_notification_event_jobs j JOIN tracking_records r ON r.tenant_id=j.tenant_id AND r.record_id=j.event_id AND r.kind='notice_event' SET j.status='cancelled',j.lease_token=NULL,j.lease_until=NULL,j.last_error='cancelled_source',j.updated_at=? WHERE `+jobWhere, append([]any{at.UTC()}, jobArgs...)...)
	if err != nil {
		return 0, storeError("cancel_jobs", err)
	}
	where, args := "tenant_id=? AND cancelled_at IS NULL", []any{s.tenant}
	if contentID != "" {
		where += " AND content_id=?"
		args = append(args, contentID)
	}
	if shareID != "" {
		where += " AND share_id=?"
		args = append(args, shareID)
	}
	result, err := tx.ExecContext(ctx, `UPDATE mini_notifications SET cancelled_at=? WHERE `+where, append([]any{at}, args...)...)
	if err != nil {
		return 0, storeError("cancel_inbox", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, storeError("cancel_count", err)
	}
	deliveryWhere, deliveryArgs := "d.tenant_id=? AND n.tenant_id=? AND d.status IN ('pending','processing')", []any{s.tenant, s.tenant}
	if contentID != "" {
		deliveryWhere += " AND n.content_id=?"
		deliveryArgs = append(deliveryArgs, contentID)
	}
	if shareID != "" {
		deliveryWhere += " AND n.share_id=?"
		deliveryArgs = append(deliveryArgs, shareID)
	}
	_, err = tx.ExecContext(ctx, `UPDATE mini_notification_deliveries d JOIN mini_notifications n ON n.tenant_id=d.tenant_id AND n.notification_id=d.notification_id
SET d.status='cancelled',d.lease_token=NULL,d.lease_until=NULL,d.updated_at=? WHERE `+deliveryWhere, append([]any{at}, deliveryArgs...)...)
	if err != nil {
		return 0, storeError("cancel_delivery", err)
	}
	return changed, nil
}

func (s *SQLStore) DeleteEventProjectionsTx(ctx context.Context, tx *sql.Tx, eventIDs []string) error {
	if tx == nil || len(eventIDs) == 0 {
		return ErrInvalidInput
	}
	for start := 0; start < len(eventIDs); start += 500 {
		end := start + 500
		if end > len(eventIDs) {
			end = len(eventIDs)
		}
		chunk := eventIDs[start:end]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, s.tenant)
		marks := make([]string, len(chunk))
		for i, id := range chunk {
			if id == "" || len(id) > 128 {
				return ErrInvalidInput
			}
			marks[i] = "?"
			args = append(args, id)
		}
		in := strings.Join(marks, ",")
		rows, err := tx.QueryContext(ctx, `SELECT event_id FROM mini_notification_event_jobs WHERE tenant_id=? AND event_id IN (`+in+`) ORDER BY event_id FOR UPDATE`, args...)
		if err != nil {
			return storeError("delete_event_lock", err)
		}
		for rows.Next() {
			var locked string
			if err := rows.Scan(&locked); err != nil {
				rows.Close()
				return storeError("delete_event_lock_scan", err)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return storeError("delete_event_lock_rows", err)
		}
		if err := rows.Close(); err != nil {
			return storeError("delete_event_lock_close", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE d FROM mini_notification_deliveries d JOIN mini_notifications n ON n.tenant_id=d.tenant_id AND n.notification_id=d.notification_id WHERE n.tenant_id=? AND n.event_id IN (`+in+`)`, args...); err != nil {
			return storeError("delete_event_deliveries", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM mini_notifications WHERE tenant_id=? AND event_id IN (`+in+`)`, args...); err != nil {
			return storeError("delete_event_notifications", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM mini_notification_event_jobs WHERE tenant_id=? AND event_id IN (`+in+`)`, args...); err != nil {
			return storeError("delete_event_jobs", err)
		}
	}
	return nil
}

// DeleteVisitorData removes visitor-bearing notice events and their derived
// notification projections. It is called by the privacy deletion orchestrator;
// callers must still delete the visitor's other tracking records separately.
func (s *SQLStore) DeleteVisitorData(ctx context.Context, visitorKey string, at time.Time) (int64, error) {
	if visitorKey == "" || len(visitorKey) > 128 || at.IsZero() || (!strings.HasPrefix(visitorKey, "u:") && !strings.HasPrefix(visitorKey, "a:")) {
		return 0, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, storeError("delete_visitor_begin", err)
	}
	defer tx.Rollback()
	match := `r.tenant_id=? AND r.kind='notice_event' AND (r.visitor_key=? OR JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.visitor_key'))=?)`
	args := []any{s.tenant, visitorKey, visitorKey}
	if _, err = tx.ExecContext(ctx, `DELETE d FROM mini_notification_deliveries d JOIN mini_notifications n ON n.tenant_id=d.tenant_id AND n.notification_id=d.notification_id JOIN tracking_records r ON r.tenant_id=n.tenant_id AND r.record_id=n.event_id WHERE `+match, args...); err != nil {
		return 0, storeError("delete_visitor_deliveries", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE n FROM mini_notifications n JOIN tracking_records r ON r.tenant_id=n.tenant_id AND r.record_id=n.event_id WHERE `+match, args...); err != nil {
		return 0, storeError("delete_visitor_notifications", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE j FROM mini_notification_event_jobs j JOIN tracking_records r ON r.tenant_id=j.tenant_id AND r.record_id=j.event_id WHERE `+match, args...); err != nil {
		return 0, storeError("delete_visitor_jobs", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE r FROM tracking_records r WHERE `+match, args...)
	if err != nil {
		return 0, storeError("delete_visitor_events", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, storeError("delete_visitor_count", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, storeError("delete_visitor_commit", err)
	}
	return changed, nil
}

func validEvent(event Event) bool {
	return event.ID != "" && len(event.ID) <= 128 && event.Kind == "browse" && event.Owner != "" && len(event.Owner) <= 64 && event.RecipientID > 0 && event.ShareID != "" && len(event.ShareID) <= 128 && len(event.ContentID) <= 128 && event.Route != "" && len(event.Route) <= 128 && !event.OccurredAt.IsZero()
}

func noticeFromEvent(event Event) Notification {
	return Notification{ID: event.ID, EventID: event.ID, RecipientID: event.RecipientID, Kind: event.Kind, Title: "分享内容有新访问", Body: "有人浏览了你分享的内容", Route: event.Route, ContentID: event.ContentID, ShareID: event.ShareID, CreatedAt: event.OccurredAt.UTC()}
}

func randomToken() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "\n" + id))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", err
	}
	parts := strings.SplitN(string(raw), "\n", 2)
	if len(parts) != 2 || parts[1] == "" {
		return time.Time{}, "", ErrInvalidInput
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", err
	}
	return at, parts[1], nil
}

func checkedUpdate(operation string, result sql.Result, err error) error {
	if err != nil {
		return storeError(operation, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return storeError(operation+"_result", err)
	}
	if changed != 1 {
		return ErrConflict
	}
	return nil
}

func storeError(operation string, err error) error {
	log.Printf("[notification] store_op=%s error_type=%T", operation, err)
	return fmt.Errorf("%w: %s", ErrUnavailable, operation)
}
