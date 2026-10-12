package route

import (
	"context"
	"service/model"
	"time"
)

type customerLeadStore interface {
	CreateLead(*model.CustomerLeadCreateInput) (int64, error)
	CreateEvent(*model.CustomerServiceEventCreateInput) (int64, error)
}
type consultationConfirmed func(context.Context, string, int64, int64) error

func confirmConsultationAfterCommit(confirmed consultationConfirmed, token string, account, leadID int64) {
	if confirmed == nil || token == "" || account <= 0 || leadID <= 0 {
		return
	}
	// Business success is already committed. Analytics cannot delay or overturn it;
	// only the opaque context, verified account and durable business ID are copied.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = confirmed(ctx, token, account, leadID)
	}()
}
