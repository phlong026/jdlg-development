package tracking

import (
	"context"
	"database/sql"
)

// NoticeWriteTx borrows the active write transaction for the three notice
// projection hooks. Callers must not commit, roll back, or retain this handle.
func NoticeWriteTx(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(sourceTransactionKey{}).(*sql.Tx)
	return tx, ok
}

func (s *Service) removePrivacyRecords(ctx context.Context, r Repository, rows map[string]Record) error {
	ids := []string{}
	for _, row := range rows {
		if row.Kind == "notice_event" {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) > 0 && s.OnRemoveNoticeEvents != nil {
		if err := s.OnRemoveNoticeEvents(sourceReadContext(ctx, r), ids); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if err := r.Remove(row.Kind, row.ID); err != nil {
			return err
		}
	}
	return nil
}
