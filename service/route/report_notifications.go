package route

import (
	"context"
	"database/sql"
	"log"
	"time"

	"service/reportnotify"
)

func configuredReportNotifications(ctx context.Context, db *sql.DB) (*reportnotify.Service, error) {
	cfg, err := reportnotify.LoadConfig()
	if err != nil {
		return nil, err
	}
	store, err := reportnotify.NewSQLStore(db)
	if err != nil {
		return nil, err
	}
	if err := store.VerifySchema(ctx); err != nil {
		return nil, err
	}
	return reportnotify.NewService(cfg, store, nil)
}

func (m *ReportModule) runNotifications(ctx context.Context) {
	for ctx.Err() == nil {
		worked, err := m.Notifications.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("[report] warn notification_cycle=failed")
		}
		if worked && err == nil {
			continue
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
