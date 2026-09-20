package report

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
)

const (
	maxStageRunKeyLength = 128
	maxStageNameLength   = 64
	maxStagePayloadBytes = 8 << 20
)

// VerifyAgentSchema verifies the explicitly migrated multi-agent checkpoint
// table. It never creates or changes schema at runtime.
func (s *MySQLStore) VerifyAgentSchema(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT report_id,run_key,stage,payload_json,created_at FROM report_agent_stages LIMIT 0`)
	if err != nil {
		return storeFailure("verify_agent_schema", "", err)
	}
	return rows.Close()
}

// LoadStage returns one immutable stage result for a report generation run.
func (s *MySQLStore) LoadStage(ctx context.Context, reportID, runKey, stage string) (json.RawMessage, error) {
	if !validStageCheckpointKey(reportID, runKey, stage) {
		return nil, ErrInvalidInput
	}

	var stored []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM report_agent_stages WHERE report_id=? AND run_key=? AND stage=?`, reportID, runKey, stage).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, storeFailure("load_stage", reportID, err)
	}
	payload, err := canonicalStagePayload(stored)
	if err != nil {
		return nil, storeFailure("load_stage_payload", reportID, err)
	}
	return payload, nil
}

// SaveStage persists an immutable stage result. The report row lock and active
// lease check prevent a worker whose lease was replaced from committing work.
func (s *MySQLStore) SaveStage(ctx context.Context, reportID, leaseToken, runKey, stage string, payload json.RawMessage) error {
	if !validStageCheckpointKey(reportID, runKey, stage) || !validIdentifier(leaseToken, 64) {
		return ErrInvalidInput
	}
	canonical, err := canonicalStagePayload(payload)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return storeFailure("save_stage_begin", reportID, err)
	}
	defer tx.Rollback()

	var status string
	var currentLease sql.NullString
	var leaseUntil sql.NullTime
	var leaseActive sql.NullBool
	err = tx.QueryRowContext(ctx, `SELECT status,lease_token,lease_until,lease_until>NOW(6) FROM building_reports WHERE report_id=? FOR UPDATE`, reportID).Scan(&status, &currentLease, &leaseUntil, &leaseActive)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return storeFailure("save_stage_report", reportID, err)
	}
	if !stageWriteAllowed(status, currentLease, leaseUntil, leaseActive, leaseToken) {
		return ErrConflict
	}

	var stored []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM report_agent_stages WHERE report_id=? AND run_key=? AND stage=? FOR UPDATE`, reportID, runKey, stage).Scan(&stored)
	if err == nil {
		existing, canonicalErr := canonicalStagePayload(stored)
		if canonicalErr != nil {
			return storeFailure("save_stage_existing_payload", reportID, canonicalErr)
		}
		if !bytes.Equal(existing, canonical) {
			return ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return storeFailure("save_stage_existing_commit", reportID, err)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storeFailure("save_stage_existing", reportID, err)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO report_agent_stages (report_id,run_key,stage,payload_json,created_at) VALUES (?,?,?,?,NOW(6))`, reportID, runKey, stage, canonical)
	if err != nil {
		return storeFailure("save_stage_insert", reportID, err)
	}
	if err := tx.Commit(); err != nil {
		return storeFailure("save_stage_commit", reportID, err)
	}
	return nil
}

func validStageCheckpointKey(reportID, runKey, stage string) bool {
	return validIdentifier(reportID, 64) &&
		validIdentifier(runKey, maxStageRunKeyLength) &&
		validIdentifier(stage, maxStageNameLength)
}

func canonicalStagePayload(payload []byte) (json.RawMessage, error) {
	if len(payload) == 0 || len(payload) > maxStagePayloadBytes {
		return nil, ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, ErrInvalidInput
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		// json.Decoder returns io.EOF only when the complete input was consumed.
		return nil, ErrInvalidInput
	}
	canonical, err := json.Marshal(value)
	if err != nil || len(canonical) > maxStagePayloadBytes {
		return nil, ErrInvalidInput
	}
	return canonical, nil
}

func stageWriteAllowed(status string, currentLease sql.NullString, leaseUntil sql.NullTime, leaseActive sql.NullBool, leaseToken string) bool {
	generating := status == StatusPreviewGenerating || status == StatusFullGenerating
	return generating && currentLease.Valid && currentLease.String == leaseToken && leaseUntil.Valid && leaseActive.Valid && leaseActive.Bool
}
