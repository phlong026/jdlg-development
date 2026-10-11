package reportwecom

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"
)

type Ticket struct {
	ReportID                                                          string
	UserID                                                            int64
	State, UnionID, ConfigID, QRCodeURL, ExternalUserID, WelcomeState string
	ExpiresAt                                                         time.Time
}
type Store interface {
	GetOrCreate(context.Context, Ticket) (Ticket, error)
	SetContact(context.Context, string, ContactWay) error
	FindByState(context.Context, string) (Ticket, error)
	Verify(context.Context, string, string) error
	ClaimWelcome(context.Context, string) (bool, error)
	FinishWelcome(context.Context, string, string) error
}
type SQLStore struct{ DB *sql.DB }

const ticketColumns = "report_id,user_id,state,unionid,contact_way_id,qr_url,external_user_id,welcome_state,expires_at"

func (s *SQLStore) VerifySchema(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+ticketColumns+" FROM report_wecom_tickets LIMIT 0")
	if err != nil {
		return storeError("verify_schema", err)
	}
	return rows.Close()
}
func storeError(operation string, err error) error {
	log.Printf("[report-wecom] error operation=%s", operation)
	return errors.New("reportwecom: store unavailable")
}
func scan(row *sql.Row) (Ticket, error) {
	var t Ticket
	err := row.Scan(&t.ReportID, &t.UserID, &t.State, &t.UnionID, &t.ConfigID, &t.QRCodeURL, &t.ExternalUserID, &t.WelcomeState, &t.ExpiresAt)
	if err != nil {
		return Ticket{}, storeError("read", err)
	}
	return t, nil
}
func (s *SQLStore) GetOrCreate(ctx context.Context, t Ticket) (Ticket, error) {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO report_wecom_tickets (report_id,user_id,state,unionid,expires_at) VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE report_id=report_id`, t.ReportID, t.UserID, t.State, t.UnionID, t.ExpiresAt)
	if err != nil {
		return Ticket{}, storeError("create", err)
	}
	return scan(s.DB.QueryRowContext(ctx, "SELECT "+ticketColumns+" FROM report_wecom_tickets WHERE report_id=? AND user_id=?", t.ReportID, t.UserID))
}
func (s *SQLStore) SetContact(ctx context.Context, state string, w ContactWay) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE report_wecom_tickets SET contact_way_id=?,qr_url=? WHERE state=? AND contact_way_id=''", w.ConfigID, w.QRCodeURL, state)
	if err != nil {
		return storeError("set_contact", err)
	}
	return nil
}
func (s *SQLStore) FindByState(ctx context.Context, state string) (Ticket, error) {
	return scan(s.DB.QueryRowContext(ctx, "SELECT "+ticketColumns+" FROM report_wecom_tickets WHERE state=?", state))
}
func (s *SQLStore) Verify(ctx context.Context, state, externalID string) error {
	result, err := s.DB.ExecContext(ctx, "UPDATE report_wecom_tickets SET external_user_id=? WHERE state=? AND (external_user_id='' OR external_user_id=?)", externalID, state, externalID)
	if err != nil {
		return storeError("verify", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return storeError("verify_count", err)
	}
	if n == 0 {
		t, err := s.FindByState(ctx, state)
		if err != nil {
			return err
		}
		if t.ExternalUserID != externalID {
			return ErrNotOwner
		}
	}
	return nil
}
func (s *SQLStore) ClaimWelcome(ctx context.Context, state string) (bool, error) {
	result, err := s.DB.ExecContext(ctx, "UPDATE report_wecom_tickets SET welcome_state='sending' WHERE state=? AND welcome_state='pending'", state)
	if err != nil {
		return false, storeError("claim_welcome", err)
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
func (s *SQLStore) FinishWelcome(ctx context.Context, state, status string) error {
	if status != "sent" && status != "manual_required" {
		return ErrProvider
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE report_wecom_tickets SET welcome_state=? WHERE state=? AND welcome_state='sending'", status, state)
	if err != nil {
		return storeError("finish_welcome", err)
	}
	return nil
}
