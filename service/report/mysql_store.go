package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

var errStoreUnavailable = errors.New("report: store unavailable")

const reportColumns = `report_id, user_id, input_hash, protocol_version, input_json,
preview_snapshot, full_snapshot, status, generation_mode, paid, bound,
payment_ref, pending_order_no, external_user_id, pack_id, pack_version,
reserved_product_id, reserved_price_fen, reserved_pack_id, reserved_pack_version,
preview_object_key, full_object_key, attempt_count, failed_stage, error_code,
lease_token, lease_until, created_at, updated_at`

type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(db *sql.DB) (*MySQLStore, error) {
	if db == nil {
		return nil, ErrInvalidInput
	}
	return &MySQLStore{db: db}, nil
}

// VerifySchema must succeed during startup before REPORT_ENABLED routes are
// registered. Migrations are explicit files and are never run implicitly.
func (s *MySQLStore) VerifySchema(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT `+reportColumns+` FROM building_reports LIMIT 0`)
	if err != nil {
		return storeFailure("verify_schema", "", err)
	}
	return rows.Close()
}

func (s *MySQLStore) CreateOrGet(ctx context.Context, record *Report) (*Report, bool, error) {
	if record == nil {
		return nil, false, ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO building_reports
(report_id,user_id,input_hash,protocol_version,input_json,status,generation_mode,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?)`, record.ID, record.UserID, record.InputHash, record.ProtocolVersion,
		record.InputJSON, StatusPreviewQueued, GenerationRulesOnly, record.CreatedAt, record.UpdatedAt)
	created := err == nil
	var mysqlErr *mysqlDriver.MySQLError
	if err != nil && (!errors.As(err, &mysqlErr) || mysqlErr.Number != 1062) {
		return nil, false, storeFailure("create", record.ID, err)
	}
	stored, err := s.getByIdentity(ctx, record.UserID, record.InputHash, record.ProtocolVersion)
	if err != nil {
		if !created && errors.Is(err, ErrNotFound) {
			return nil, false, ErrConflict
		}
		return nil, false, err
	}
	return stored, created, nil
}

func (s *MySQLStore) GetForUser(ctx context.Context, userID int64, reportID string) (*Report, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM building_reports WHERE report_id=? AND user_id=?`, reportID, userID)
	return scanReport(row)
}

func (s *MySQLStore) GetLatestForUser(ctx context.Context, userID int64) (*Report, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM building_reports WHERE user_id=? ORDER BY created_at DESC,report_id DESC LIMIT 1`, userID)
	return scanReport(row)
}

func (s *MySQLStore) ReservePaymentOrder(ctx context.Context, userID int64, reportID string, reservation PaymentReservation) (PaymentReservation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PaymentReservation{}, storeFailure("reserve_begin", reportID, err)
	}
	defer tx.Rollback()
	record, err := scanReport(tx.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM building_reports WHERE report_id=? AND user_id=? FOR UPDATE`, reportID, userID))
	if err != nil {
		return PaymentReservation{}, err
	}
	if record.PaymentRef != "" {
		return reservationFromRecord(record, record.PaymentRef), nil
	}
	if record.PendingOrderNo != "" {
		return reservationFromRecord(record, record.PendingOrderNo), nil
	}
	if record.Status != StatusPreviewReady || len(record.PreviewSnapshot) == 0 {
		return PaymentReservation{}, ErrConflict
	}
	if !validReservation(reservation) {
		return PaymentReservation{}, ErrInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `UPDATE building_reports SET pending_order_no=?,reserved_product_id=?,reserved_price_fen=?,reserved_pack_id=?,reserved_pack_version=?,updated_at=NOW(6) WHERE report_id=?`, reservation.OrderNo, reservation.ProductID, reservation.PriceFen, reservation.PackID, reservation.PackVersion, reportID); err != nil {
		return PaymentReservation{}, storeFailure("reserve_update", reportID, err)
	}
	if err := tx.Commit(); err != nil {
		return PaymentReservation{}, storeFailure("reserve_commit", reportID, err)
	}
	return reservation, nil
}

func (s *MySQLStore) AuthorizePaid(ctx context.Context, userID int64, reportID, paymentRef string) (*Report, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storeFailure("pay_begin", reportID, err)
	}
	defer tx.Rollback()
	record, err := scanReport(tx.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM building_reports WHERE report_id=? AND user_id=? FOR UPDATE`, reportID, userID))
	if err != nil {
		return nil, err
	}
	if record.Paid {
		if record.PaymentRef != paymentRef {
			return nil, ErrConflict
		}
		return record, nil
	}
	if record.PendingOrderNo == "" || record.PendingOrderNo != paymentRef || record.Status != StatusPreviewReady || len(record.PreviewSnapshot) == 0 || record.ReservedPackID == "" || record.ReservedPackVersion == "" || record.ReservedProductID == "" || record.ReservedPriceFen <= 0 {
		return nil, ErrConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE building_reports SET paid=1,payment_ref=?,pack_id=reserved_pack_id,pack_version=reserved_pack_version,status=?,attempt_count=0,error_code=NULL,failed_stage=NULL,updated_at=NOW(6) WHERE report_id=?`, paymentRef, StatusFullQueued, reportID)
	if err != nil {
		return nil, storeFailure("pay_update", reportID, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, storeFailure("pay_commit", reportID, err)
	}
	return s.GetForUser(ctx, userID, reportID)
}

func (s *MySQLStore) BindEnterpriseContact(ctx context.Context, userID int64, reportID, externalUserID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeFailure("bind_begin", reportID, err)
	}
	defer tx.Rollback()
	record, err := scanReport(tx.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM building_reports WHERE report_id=? AND user_id=? FOR UPDATE`, reportID, userID))
	if err != nil {
		return err
	}
	if record.Bound && record.ExternalUserID != externalUserID {
		return ErrConflict
	}
	if !record.Bound {
		if _, err := tx.ExecContext(ctx, `UPDATE building_reports SET bound=1,external_user_id=?,updated_at=NOW(6) WHERE report_id=?`, externalUserID, reportID); err != nil {
			return storeFailure("bind_update", reportID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return storeFailure("bind_commit", reportID, err)
	}
	return nil
}

func (s *MySQLStore) RetryFailed(ctx context.Context, userID int64, reportID string) (*Report, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storeFailure("retry_begin", reportID, err)
	}
	defer tx.Rollback()
	record, err := scanReport(tx.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM building_reports WHERE report_id=? AND user_id=? FOR UPDATE`, reportID, userID))
	if err != nil {
		return nil, err
	}
	if record.Status != StatusFailed {
		return nil, ErrConflict
	}
	next := StatusPreviewQueued
	if record.Paid {
		if len(record.PreviewSnapshot) == 0 || record.PackID == "" || record.PackVersion == "" {
			return nil, ErrConflict
		}
		next = StatusFullQueued
	}
	_, err = tx.ExecContext(ctx, `UPDATE building_reports SET status=?,attempt_count=0,error_code=NULL,failed_stage=NULL,lease_token=NULL,lease_until=NULL,updated_at=NOW(6) WHERE report_id=?`, next, reportID)
	if err != nil {
		return nil, storeFailure("retry_update", reportID, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, storeFailure("retry_commit", reportID, err)
	}
	return s.GetForUser(ctx, userID, reportID)
}

func (s *MySQLStore) Claim(ctx context.Context, _ time.Time, lease time.Duration, maxAttempts int) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, storeFailure("claim_begin", "", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE building_reports SET status=?,failed_stage=CASE WHEN status=? THEN ? ELSE ? END,error_code='attempts_exhausted',lease_token=NULL,lease_until=NULL,updated_at=NOW(6)
WHERE status IN (?,?) AND lease_until<NOW(6) AND attempt_count>=?`, StatusFailed, StatusPreviewGenerating, ModePreview, ModeFull, StatusPreviewGenerating, StatusFullGenerating, maxAttempts)
	if err != nil {
		return nil, storeFailure("claim_expired", "", err)
	}
	row := tx.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM building_reports
WHERE attempt_count<? AND (status IN (?,?) OR (status IN (?,?) AND lease_until<NOW(6)))
ORDER BY created_at ASC LIMIT 1 FOR UPDATE`, maxAttempts, StatusPreviewQueued, StatusFullQueued, StatusPreviewGenerating, StatusFullGenerating)
	record, err := scanReport(row)
	if errors.Is(err, ErrNotFound) {
		if err := tx.Commit(); err != nil {
			return nil, storeFailure("claim_empty_commit", "", err)
		}
		return nil, ErrNoJob
	}
	if err != nil {
		return nil, err
	}
	mode := ModePreview
	next := StatusPreviewGenerating
	if record.Status == StatusFullQueued || record.Status == StatusFullGenerating {
		mode = ModeFull
		next = StatusFullGenerating
		if !record.Paid {
			return nil, ErrConflict
		}
	}
	token := uuid.NewString()
	leaseUntil := time.Now().Add(lease)
	result, err := tx.ExecContext(ctx, `UPDATE building_reports SET status=?,attempt_count=attempt_count+1,lease_token=?,lease_until=DATE_ADD(NOW(6),INTERVAL ? MICROSECOND),updated_at=NOW(6) WHERE report_id=?`, next, token, lease.Microseconds(), record.ID)
	if err != nil {
		return nil, storeFailure("claim_update", record.ID, err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return nil, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, storeFailure("claim_commit", record.ID, err)
	}
	record.Status = next
	record.AttemptCount++
	record.LeaseToken = token
	record.LeaseUntil = &leaseUntil
	return &Job{Report: record, Mode: mode, LeaseToken: token}, nil
}

func (s *MySQLStore) CompletePreview(ctx context.Context, reportID, leaseToken string, snapshot json.RawMessage, mode, objectKey string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE building_reports SET preview_snapshot=?,preview_object_key=?,generation_mode=?,status=?,attempt_count=0,lease_token=NULL,lease_until=NULL,error_code=NULL,failed_stage=NULL,updated_at=NOW(6)
WHERE report_id=? AND status=? AND lease_token=? AND lease_until>NOW(6)`, snapshot, objectKey, mode, StatusPreviewReady, reportID, StatusPreviewGenerating, leaseToken)
	return checkedUpdate("complete_preview", reportID, result, err)
}

func (s *MySQLStore) SetPreviewObjectKeyIfEmpty(ctx context.Context, userID int64, reportID, objectKey string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE building_reports SET preview_object_key=?,updated_at=NOW(6)
WHERE report_id=? AND user_id=? AND preview_snapshot IS NOT NULL AND (preview_object_key IS NULL OR preview_object_key='')`, objectKey, reportID, userID)
	if err != nil {
		return storeFailure("backfill_preview_pdf", reportID, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return storeFailure("backfill_preview_pdf_result", reportID, err)
	}
	if changed == 1 {
		return nil
	}
	record, err := s.GetForUser(ctx, userID, reportID)
	if err == nil && record.PreviewObjectKey != "" {
		return nil
	}
	return ErrConflict
}

func (s *MySQLStore) CompleteFull(ctx context.Context, reportID, leaseToken string, snapshot json.RawMessage, objectKey string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE building_reports SET full_snapshot=?,full_object_key=?,status=?,lease_token=NULL,lease_until=NULL,error_code=NULL,failed_stage=NULL,updated_at=NOW(6)
WHERE report_id=? AND paid=1 AND status=? AND lease_token=? AND lease_until>NOW(6)`, snapshot, objectKey, StatusReady, reportID, StatusFullGenerating, leaseToken)
	return checkedUpdate("complete_full", reportID, result, err)
}

func (s *MySQLStore) Fail(ctx context.Context, reportID, leaseToken, stage, errorCode string, retry bool) error {
	expected, next := StatusPreviewGenerating, StatusFailed
	if stage == ModeFull {
		expected = StatusFullGenerating
	}
	if retry {
		next = StatusPreviewQueued
		if stage == ModeFull {
			next = StatusFullQueued
		}
	}
	result, err := s.db.ExecContext(ctx, `UPDATE building_reports SET status=?,failed_stage=?,error_code=?,lease_token=NULL,lease_until=NULL,updated_at=NOW(6)
WHERE report_id=? AND status=? AND lease_token=? AND lease_until>NOW(6)`, next, stage, errorCode, reportID, expected, leaseToken)
	return checkedUpdate("fail", reportID, result, err)
}

func (s *MySQLStore) getByIdentity(ctx context.Context, userID int64, inputHash, protocolVersion string) (*Report, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM building_reports WHERE user_id=? AND input_hash=? AND protocol_version=?`, userID, inputHash, protocolVersion)
	return scanReport(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReport(scanner rowScanner) (*Report, error) {
	record := &Report{}
	var preview, full, paymentRef, pendingOrder, externalUserID, packID, packVersion sql.NullString
	var reservedProductID, reservedPackID, reservedPackVersion sql.NullString
	var reservedPriceFen sql.NullInt64
	var previewKey, fullKey, failedStage, errorCode, leaseToken sql.NullString
	var leaseUntil sql.NullTime
	err := scanner.Scan(
		&record.ID, &record.UserID, &record.InputHash, &record.ProtocolVersion, &record.InputJSON,
		&preview, &full, &record.Status, &record.GenerationMode, &record.Paid, &record.Bound,
		&paymentRef, &pendingOrder, &externalUserID, &packID, &packVersion,
		&reservedProductID, &reservedPriceFen, &reservedPackID, &reservedPackVersion,
		&previewKey, &fullKey, &record.AttemptCount, &failedStage, &errorCode,
		&leaseToken, &leaseUntil, &record.CreatedAt, &record.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, storeFailure("scan", "", err)
	}
	if preview.Valid {
		record.PreviewSnapshot = json.RawMessage(preview.String)
	}
	if full.Valid {
		record.FullSnapshot = json.RawMessage(full.String)
	}
	record.PaymentRef = paymentRef.String
	record.PendingOrderNo = pendingOrder.String
	record.ExternalUserID = externalUserID.String
	record.PackID = packID.String
	record.PackVersion = packVersion.String
	record.ReservedProductID = reservedProductID.String
	record.ReservedPriceFen = reservedPriceFen.Int64
	record.ReservedPackID = reservedPackID.String
	record.ReservedPackVersion = reservedPackVersion.String
	record.PreviewObjectKey = previewKey.String
	record.FullObjectKey = fullKey.String
	record.FailedStage = failedStage.String
	record.ErrorCode = errorCode.String
	record.LeaseToken = leaseToken.String
	if leaseUntil.Valid {
		record.LeaseUntil = &leaseUntil.Time
	}
	return record, nil
}

func checkedUpdate(operation, reportID string, result sql.Result, err error) error {
	if err != nil {
		return storeFailure(operation, reportID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return storeFailure(operation+"_result", reportID, err)
	}
	if rows != 1 {
		return ErrConflict
	}
	return nil
}

func storeFailure(operation, reportID string, err error) error {
	log.Printf("[report] store_op=%s report_id=%s error_type=%T", operation, reportID, err)
	return errStoreUnavailable
}

func validReservation(reservation PaymentReservation) bool {
	return validIdentifier(reservation.OrderNo, 128) && validIdentifier(reservation.ProductID, 64) && reservation.PriceFen > 0 && validIdentifier(reservation.PackID, 64) && validIdentifier(reservation.PackVersion, 64)
}

func reservationFromRecord(record *Report, orderNo string) PaymentReservation {
	return PaymentReservation{OrderNo: orderNo, ProductID: record.ReservedProductID, PriceFen: record.ReservedPriceFen, PackID: record.ReservedPackID, PackVersion: record.ReservedPackVersion}
}
