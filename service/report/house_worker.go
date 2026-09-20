package report

import (
	"context"
	"time"
)

type houseYieldStore interface {
	YieldHouse(context.Context, string, string, string) error
}

// A completed image is durable progress, not a failed attempt. Release the
// lease before starting another expensive image so other reports can run.
func (s *MySQLStore) YieldHouse(ctx context.Context, reportID, token, mode string) error {
	if !oneOf(mode, ModePreview, ModeFull) {
		return ErrInvalidInput
	}
	status, queued := StatusFullGenerating, StatusFullQueued
	if mode == ModePreview {
		status, queued = StatusPreviewGenerating, StatusPreviewQueued
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `UPDATE building_reports SET status=?,attempt_count=GREATEST(attempt_count-1,0),lease_token=NULL,lease_until=NULL,updated_at=NOW(6)
WHERE report_id=? AND protocol_version=? AND status=? AND lease_token=? AND lease_until>NOW(6)`, queued, reportID, ProtocolHouseV1, status, token)
	return checkedUpdate("house_yield", reportID, result, err)
}
