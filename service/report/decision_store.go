package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

const (
	DecisionStagePreviewPending = "preview_pending"
	DecisionStageConfirmed      = "confirmed"
)

type DecisionContext struct {
	ConfirmationJSON json.RawMessage
	ConfirmationHash string
	Stage            string
}

type DecisionContextStore interface {
	GetDecisionContext(ctx context.Context, userID int64, reportID string) (DecisionContext, error)
	SaveDecisionConfirmation(ctx context.Context, userID int64, reportID string, raw json.RawMessage, hash string) (DecisionContext, error)
	SetDecisionStage(ctx context.Context, reportID, leaseToken, stage string) error
}

func (s *MySQLStore) VerifyDecisionSchema(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT c.report_id,c.confirmation_json,c.confirmation_hash,c.stage,c.created_at,c.updated_at
FROM report_decision_contexts c JOIN building_reports r ON r.report_id=c.report_id LIMIT 0`)
	if err != nil {
		return storeFailure("verify_decision_schema", "", err)
	}
	return rows.Close()
}

func (s *MySQLStore) GetDecisionContext(ctx context.Context, userID int64, reportID string) (DecisionContext, error) {
	var confirmation sql.NullString
	var hash sql.NullString
	var stage string
	err := s.db.QueryRowContext(ctx, `SELECT c.confirmation_json,c.confirmation_hash,c.stage
FROM report_decision_contexts c JOIN building_reports r ON r.report_id=c.report_id
WHERE r.user_id=? AND r.report_id=?`, userID, reportID).Scan(&confirmation, &hash, &stage)
	if errors.Is(err, sql.ErrNoRows) {
		return DecisionContext{}, ErrNotFound
	}
	if err != nil {
		return DecisionContext{}, storeFailure("get_decision_context", reportID, err)
	}
	result := DecisionContext{ConfirmationHash: hash.String, Stage: stage}
	if confirmation.Valid {
		result.ConfirmationJSON = json.RawMessage(confirmation.String)
	}
	if !validDecisionContext(result) {
		return DecisionContext{}, ErrConflict
	}
	return result, nil
}

func (s *MySQLStore) SaveDecisionConfirmation(ctx context.Context, userID int64, reportID string, raw json.RawMessage, hash string) (DecisionContext, error) {
	if len(raw) == 0 || !validSHA256(hash) {
		return DecisionContext{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DecisionContext{}, storeFailure("confirm_begin", reportID, err)
	}
	defer tx.Rollback()
	record, err := scanReport(tx.QueryRowContext(ctx, `SELECT `+reportColumns+` FROM building_reports WHERE report_id=? AND user_id=? FOR UPDATE`, reportID, userID))
	if err != nil {
		return DecisionContext{}, err
	}
	if record.ProtocolVersion != ProtocolDecisionV5 || record.Status != StatusPreviewReady || len(record.PreviewSnapshot) == 0 || record.Paid || record.PendingOrderNo != "" {
		return DecisionContext{}, ErrConflict
	}
	var existingJSON sql.NullString
	var existingHash sql.NullString
	var existingStage string
	err = tx.QueryRowContext(ctx, `SELECT confirmation_json,confirmation_hash,stage FROM report_decision_contexts WHERE report_id=? FOR UPDATE`, reportID).Scan(&existingJSON, &existingHash, &existingStage)
	if err == nil && existingJSON.Valid != existingHash.Valid {
		return DecisionContext{}, ErrConflict
	}
	switch {
	case err == nil && existingHash.Valid:
		if existingHash.String != hash || !existingJSON.Valid || !jsonEqual(json.RawMessage(existingJSON.String), raw) {
			return DecisionContext{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return DecisionContext{}, storeFailure("confirm_idempotent_commit", reportID, err)
		}
		return DecisionContext{ConfirmationJSON: append(json.RawMessage(nil), raw...), ConfirmationHash: hash, Stage: existingStage}, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return DecisionContext{}, storeFailure("confirm_read", reportID, err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO report_decision_contexts
(report_id,confirmation_json,confirmation_hash,stage,created_at,updated_at)
VALUES (?,?,?,?,NOW(6),NOW(6))`, reportID, raw, hash, DecisionStageConfirmed)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE report_decision_contexts SET confirmation_json=?,confirmation_hash=?,stage=?,updated_at=NOW(6) WHERE report_id=? AND confirmation_hash IS NULL`, raw, hash, DecisionStageConfirmed, reportID)
		if err == nil {
			var rows int64
			rows, err = result.RowsAffected()
			if err == nil && rows != 1 {
				return DecisionContext{}, ErrConflict
			}
		}
	}
	if err != nil {
		return DecisionContext{}, storeFailure("confirm_write", reportID, err)
	}
	if err := tx.Commit(); err != nil {
		return DecisionContext{}, storeFailure("confirm_commit", reportID, err)
	}
	return DecisionContext{ConfirmationJSON: append(json.RawMessage(nil), raw...), ConfirmationHash: hash, Stage: DecisionStageConfirmed}, nil
}

func (s *MySQLStore) SetDecisionStage(ctx context.Context, reportID, leaseToken, stage string) error {
	stage = strings.TrimSpace(stage)
	if !validIdentifier(reportID, 64) || !validIdentifier(leaseToken, 64) || !validIdentifier(stage, 64) {
		return ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeFailure("decision_stage_begin", reportID, err)
	}
	defer tx.Rollback()
	var found string
	err = tx.QueryRowContext(ctx, `SELECT report_id FROM building_reports
WHERE report_id=? AND protocol_version=? AND status IN (?,?) AND lease_token=? AND lease_until>NOW(6) FOR UPDATE`, reportID, ProtocolDecisionV5, StatusPreviewGenerating, StatusFullGenerating, leaseToken).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return storeFailure("decision_stage_lock", reportID, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO report_decision_contexts (report_id,stage,created_at,updated_at)
VALUES (?,?,NOW(6),NOW(6)) ON DUPLICATE KEY UPDATE stage=VALUES(stage),updated_at=NOW(6)`, reportID, stage)
	if err != nil {
		return storeFailure("decision_stage_write", reportID, err)
	}
	if err := tx.Commit(); err != nil {
		return storeFailure("decision_stage_commit", reportID, err)
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func validDecisionContext(value DecisionContext) bool {
	if !validIdentifier(value.Stage, 64) {
		return false
	}
	if len(value.ConfirmationJSON) == 0 && value.ConfirmationHash == "" {
		return value.Stage != DecisionStageConfirmed
	}
	if len(value.ConfirmationJSON) == 0 || !validSHA256(value.ConfirmationHash) {
		return false
	}
	var confirmation DecisionConfirmation
	return json.Unmarshal(value.ConfirmationJSON, &confirmation) == nil
}
