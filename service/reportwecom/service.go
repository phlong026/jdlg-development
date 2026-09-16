package reportwecom

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"time"
)

type Identity interface {
	// OwnerUnionID must verify ownership AND confirmed payment before returning an identity.
	OwnerUnionID(context.Context, int64, string) (string, error)
}
type Binder interface {
	BindVerifiedEnterpriseContact(context.Context, int64, string, string) error
}
type Service struct {
	Provider   Provider
	Store      Store
	Identity   Identity
	Binder     Binder
	EmployeeID string
	Now        func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s *Service) valid() bool {
	return s != nil && s.Provider != nil && s.Store != nil && s.Identity != nil && s.Binder != nil && s.EmployeeID != ""
}
func (s *Service) EnsureContact(ctx context.Context, reportID string, userID int64) (ContactWay, error) {
	if !s.valid() {
		return ContactWay{}, ErrNotConfigured
	}
	union, err := s.Identity.OwnerUnionID(ctx, userID, reportID)
	if err != nil {
		return ContactWay{}, err
	}
	if union == "" {
		return ContactWay{}, ErrUnionIDRequired
	}
	random := make([]byte, 15)
	if _, err := rand.Read(random); err != nil {
		return ContactWay{}, ErrProvider
	}
	ticket, err := s.Store.GetOrCreate(ctx, Ticket{ReportID: reportID, UserID: userID, State: hex.EncodeToString(random), UnionID: union, ExpiresAt: s.now().Add(30 * 24 * time.Hour)})
	if err != nil {
		return ContactWay{}, err
	}
	if !ticket.ExpiresAt.After(s.now()) {
		return ContactWay{}, ErrExpired
	}
	if ticket.ExternalUserID != "" {
		if err := s.verifyIdentity(ctx, ticket, ticket.ExternalUserID); err != nil {
			return ContactWay{}, err
		}
		if err := s.Binder.BindVerifiedEnterpriseContact(ctx, ticket.UserID, ticket.ReportID, ticket.ExternalUserID); err != nil {
			return ContactWay{}, err
		}
		return ContactWay{Bound: true}, nil
	}
	if ticket.ConfigID != "" {
		return ContactWay{ConfigID: ticket.ConfigID, QRCodeURL: ticket.QRCodeURL}, nil
	}
	way, err := s.Provider.CreateContactWay(ctx, ticket.State)
	if err != nil {
		return ContactWay{}, err
	}
	if err := s.Store.SetContact(ctx, ticket.State, way); err != nil {
		return ContactWay{}, err
	}
	return way, nil
}

type Event struct {
	Event          string `xml:"Event"`
	ChangeType     string `xml:"ChangeType"`
	State          string `xml:"State"`
	ExternalUserID string `xml:"ExternalUserID"`
	EmployeeID     string `xml:"UserID"`
	WelcomeCode    string `xml:"WelcomeCode"`
	CreateTime     int64  `xml:"CreateTime"`
	MsgId          string `xml:"MsgId"`
}

func (s *Service) verifyIdentity(ctx context.Context, t Ticket, externalID string) error {
	union, err := s.Identity.OwnerUnionID(ctx, t.UserID, t.ReportID)
	if err != nil {
		return err
	}
	contact, err := s.Provider.GetExternalContact(ctx, externalID)
	if err != nil {
		return err
	}
	if union == "" || contact.UnionID == "" {
		return ErrUnionIDRequired
	}
	if contact.ExternalUserID != externalID || subtle.ConstantTimeCompare([]byte(union), []byte(contact.UnionID)) != 1 || subtle.ConstantTimeCompare([]byte(union), []byte(t.UnionID)) != 1 {
		return ErrNotOwner
	}
	followed := false
	for _, id := range contact.FollowUsers {
		if id == s.EmployeeID {
			followed = true
		}
	}
	if !followed {
		return ErrNotOwner
	}
	return nil
}
func (s *Service) HandleEvent(ctx context.Context, e Event) error {
	if !s.valid() {
		return ErrNotConfigured
	}
	if e.Event != "change_external_contact" || e.ChangeType != "add_external_contact" || e.State == "" {
		return nil
	}
	if e.EmployeeID != s.EmployeeID || e.ExternalUserID == "" {
		return ErrNotOwner
	}
	ticket, err := s.Store.FindByState(ctx, e.State)
	if err != nil {
		return err
	}
	if !ticket.ExpiresAt.After(s.now()) {
		return ErrExpired
	}
	if ticket.ExternalUserID != "" && ticket.ExternalUserID != e.ExternalUserID {
		return ErrNotOwner
	}
	if err := s.verifyIdentity(ctx, ticket, e.ExternalUserID); err != nil {
		return err
	}
	if err := s.Binder.BindVerifiedEnterpriseContact(ctx, ticket.UserID, ticket.ReportID, e.ExternalUserID); err != nil {
		return err
	}
	if err := s.Store.Verify(ctx, e.State, e.ExternalUserID); err != nil {
		return err
	}
	claimed, err := s.Store.ClaimWelcome(ctx, e.State)
	if err != nil || !claimed {
		return err
	}
	state := "sent"
	// A timeout may mean WeCom accepted the only welcome slot; never blindly resend.
	if e.WelcomeCode == "" || s.now().Unix()-e.CreateTime > 18 || s.now().Unix() < e.CreateTime-5 {
		state = "manual_required"
	} else if err := s.Provider.SendWelcome(ctx, e.WelcomeCode, ticket.ReportID); err != nil {
		state = "manual_required"
	}
	if state == "manual_required" {
		log.Printf("[report-wecom] warn report_id=%s welcome=manual_required", ticket.ReportID)
	}
	return s.Store.FinishWelcome(ctx, e.State, state)
}
