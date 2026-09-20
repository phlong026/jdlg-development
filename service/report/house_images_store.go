package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type MySQLHouseImageStore struct{ db *sql.DB }

func NewMySQLHouseImageStore(db *sql.DB) (*MySQLHouseImageStore, error) {
	if db == nil {
		return nil, ErrInvalidInput
	}
	return &MySQLHouseImageStore{db: db}, nil
}
func (s *MySQLHouseImageStore) VerifySchema(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	markerRows, err := s.db.QueryContext(ctx, `SELECT singleton,storage_id,created_at FROM report_house_image_storage LIMIT 0`)
	if err != nil {
		return storeFailure("verify_house_image_storage", "", err)
	}
	if err := markerRows.Close(); err != nil {
		return storeFailure("close_house_image_storage_schema", "", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT report_id,user_id,image_id,status,request_hash,record_json,created_at,updated_at FROM report_house_images LIMIT 0`)
	if err != nil {
		return storeFailure("verify_house_images", "", err)
	}
	if err := rows.Close(); err != nil {
		return storeFailure("close_house_image_schema", "", err)
	}
	return nil
}

// VerifyStorage registers exactly one physical asset store for this database.
// A worker using another local disk fails startup before it can claim work.
func (s *MySQLHouseImageStore) VerifyStorage(ctx context.Context, storageID string) error {
	if ctx == nil || !isImageHash(storageID) {
		return ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO report_house_image_storage (singleton,storage_id,created_at)
VALUES (1,?,NOW(6)) ON DUPLICATE KEY UPDATE storage_id=storage_id`, storageID)
	if err != nil {
		return storeFailure("register_house_image_storage", "", err)
	}
	var registered string
	if err := s.db.QueryRowContext(ctx, `SELECT storage_id FROM report_house_image_storage WHERE singleton=1`).Scan(&registered); err != nil {
		return storeFailure("read_house_image_storage", "", err)
	}
	if registered != storageID {
		return ErrConflict
	}
	return nil
}

func (s *MySQLHouseImageStore) Get(ctx context.Context, r *Report, id string) (HouseImageRecord, error) {
	if !validHouseImageOwner(r) || !validIdentifier(id, 80) {
		return HouseImageRecord{}, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT i.record_json FROM report_house_images i
JOIN building_reports r ON r.report_id=i.report_id AND r.user_id=i.user_id
JOIN report_decision_contexts d ON d.report_id=r.report_id
WHERE i.report_id=? AND i.user_id=? AND i.image_id=? AND r.input_hash=? AND r.protocol_version=? AND d.confirmation_hash=?`, r.ID, r.UserID, id, r.InputHash, ProtocolHouseV1, r.ConfirmationHash).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return HouseImageRecord{}, ErrNotFound
	}
	if err != nil {
		return HouseImageRecord{}, storeFailure("get_house_image", r.ID, err)
	}
	return decodeHouseImageRecord(raw, r, id)
}

// Lock the parent report first in every write transaction, so simultaneous
// image reservations serialize and a reclaimed report fences stale writers.
func lockHouseImageReport(ctx context.Context, tx *sql.Tx, r *Report) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT r.report_id FROM building_reports r
JOIN report_decision_contexts d ON d.report_id=r.report_id
WHERE r.report_id=? AND r.user_id=? AND r.input_hash=? AND r.protocol_version=?
AND r.paid=1 AND r.status=? AND r.lease_token=? AND r.lease_until>NOW(6) AND d.confirmation_hash=? FOR UPDATE`, r.ID, r.UserID, r.InputHash, ProtocolHouseV1, StatusFullGenerating, r.LeaseToken, r.ConfirmationHash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return storeFailure("lock_house_image_report", r.ID, err)
	}
	return nil
}
func (s *MySQLHouseImageStore) Reserve(ctx context.Context, r *Report, row HouseImageRecord) (HouseImageRecord, bool, error) {
	if !validHouseImageOwner(r) || !validIdentifier(r.LeaseToken, 64) || !imageRecordBelongsTo(row, r) || !validHouseImageTask(row.Task) || !isImageHash(row.RequestHash) || row.Status != "submitting" {
		return HouseImageRecord{}, false, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HouseImageRecord{}, false, storeFailure("reserve_house_image_begin", r.ID, err)
	}
	defer tx.Rollback()
	if err := lockHouseImageReport(ctx, tx, r); err != nil {
		return HouseImageRecord{}, false, err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT record_json FROM report_house_images WHERE report_id=? AND user_id=? AND image_id=? FOR UPDATE`, r.ID, r.UserID, row.Task.ID).Scan(&raw)
	if err == nil {
		existing, err := decodeHouseImageRecord(raw, r, row.Task.ID)
		if err != nil {
			return HouseImageRecord{}, false, err
		}
		if existing.RequestHash != row.RequestHash {
			return HouseImageRecord{}, false, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return HouseImageRecord{}, false, storeFailure("reserve_house_image_existing_commit", r.ID, err)
		}
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return HouseImageRecord{}, false, storeFailure("reserve_house_image_get", r.ID, err)
	}
	raw, err = json.Marshal(row)
	if err != nil {
		return HouseImageRecord{}, false, ErrInvalidInput
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO report_house_images (report_id,user_id,image_id,status,request_hash,record_json,created_at,updated_at)
SELECT ?,?,?,?,?,?,NOW(6),NOW(6) FROM building_reports
WHERE report_id=? AND user_id=? AND paid=1 AND status=? AND lease_token=? AND lease_until>NOW(6)`, r.ID, r.UserID, row.Task.ID, "submitting", row.RequestHash, raw, r.ID, r.UserID, StatusFullGenerating, r.LeaseToken)
	if err != nil {
		return HouseImageRecord{}, false, storeFailure("reserve_house_image_insert", r.ID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return HouseImageRecord{}, false, storeFailure("reserve_house_image_rows", r.ID, err)
	}
	if n != 1 {
		return HouseImageRecord{}, false, ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return HouseImageRecord{}, false, storeFailure("reserve_house_image_commit", r.ID, err)
	}
	return row, true, nil
}
func (s *MySQLHouseImageStore) Complete(ctx context.Context, r *Report, row HouseImageRecord) error {
	if !validHouseImageOwner(r) || !validIdentifier(r.LeaseToken, 64) || !imageRecordBelongsTo(row, r) || !validHouseImageTask(row.Task) || !isImageHash(row.RequestHash) || row.Status != "complete" || row.Asset.ID != row.Task.ID || !isImageHash(row.Asset.SHA256) {
		return ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeFailure("complete_house_image_begin", r.ID, err)
	}
	defer tx.Rollback()
	if err := lockHouseImageReport(ctx, tx, r); err != nil {
		return err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT record_json FROM report_house_images WHERE report_id=? AND user_id=? AND image_id=? FOR UPDATE`, r.ID, r.UserID, row.Task.ID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return storeFailure("complete_house_image_get", r.ID, err)
	}
	existing, err := decodeHouseImageRecord(raw, r, row.Task.ID)
	if err != nil {
		return err
	}
	if existing.RequestHash != row.RequestHash || existing.Task != row.Task || existing.Identity != row.Identity || existing.ReferenceHash != row.ReferenceHash {
		return ErrConflict
	}
	if existing.Status == "complete" {
		if existing.Asset != row.Asset {
			return ErrConflict
		}
	} else {
		raw, err = json.Marshal(row)
		if err != nil {
			return ErrInvalidInput
		}
		result, err := tx.ExecContext(ctx, `UPDATE report_house_images i JOIN building_reports r ON r.report_id=i.report_id AND r.user_id=i.user_id
SET i.status='complete',i.record_json=?,i.updated_at=NOW(6)
WHERE i.report_id=? AND i.user_id=? AND i.image_id=? AND i.request_hash=? AND i.status='submitting'
AND r.status=? AND r.paid=1 AND r.lease_token=? AND r.lease_until>NOW(6)`, raw, r.ID, r.UserID, row.Task.ID, row.RequestHash, StatusFullGenerating, r.LeaseToken)
		if err != nil {
			return storeFailure("complete_house_image_update", r.ID, err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return storeFailure("complete_house_image_rows", r.ID, err)
		}
		if n != 1 {
			return ErrConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return storeFailure("complete_house_image_commit", r.ID, err)
	}
	return nil
}
func decodeHouseImageRecord(raw []byte, r *Report, id string) (HouseImageRecord, error) {
	var row HouseImageRecord
	if len(raw) > 128<<10 || json.Unmarshal(raw, &row) != nil || !imageRecordBelongsTo(row, r) || row.Task.ID != id || !validHouseImageTask(row.Task) || !isImageHash(row.RequestHash) || (row.Status != "submitting" && row.Status != "complete") {
		return HouseImageRecord{}, storeFailure("decode_house_image", r.ID, ErrInvalidInput)
	}
	return row, nil
}

var _ HouseImageStore = (*MySQLHouseImageStore)(nil)
