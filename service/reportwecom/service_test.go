package reportwecom

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProvider struct {
	contact        ExternalContact
	sends, creates int
	sendErr        error
	state          string
}

func (f *fakeProvider) CreateContactWay(_ context.Context, state string) (ContactWay, error) {
	f.creates++
	f.state = state
	return ContactWay{ConfigID: "way", QRCodeURL: "https://work.weixin.qq.com/qr"}, nil
}
func (f *fakeProvider) GetExternalContact(context.Context, string) (ExternalContact, error) {
	return f.contact, nil
}
func (f *fakeProvider) SendWelcome(context.Context, string, string) error {
	f.sends++
	return f.sendErr
}

type fakeStore struct{ t Ticket }

func (f *fakeStore) GetOrCreate(_ context.Context, t Ticket) (Ticket, error) {
	if f.t.State == "" {
		f.t = t
		f.t.WelcomeState = "pending"
	}
	return f.t, nil
}
func (f *fakeStore) SetContact(_ context.Context, _ string, w ContactWay) error {
	f.t.ConfigID = w.ConfigID
	f.t.QRCodeURL = w.QRCodeURL
	return nil
}
func (f *fakeStore) FindByState(_ context.Context, state string) (Ticket, error) {
	if state != f.t.State {
		return Ticket{}, ErrExpired
	}
	return f.t, nil
}
func (f *fakeStore) Verify(_ context.Context, _, ext string) error {
	f.t.ExternalUserID = ext
	return nil
}
func (f *fakeStore) ClaimWelcome(context.Context, string) (bool, error) {
	if f.t.WelcomeState != "pending" {
		return false, nil
	}
	f.t.WelcomeState = "sending"
	return true, nil
}
func (f *fakeStore) FinishWelcome(_ context.Context, _, state string) error {
	f.t.WelcomeState = state
	return nil
}

type fakeIdentity struct {
	union  string
	denied bool
}

func (f *fakeIdentity) OwnerUnionID(context.Context, int64, string) (string, error) {
	if f.denied {
		return "", ErrNotOwner
	}
	return f.union, nil
}

type fakeBinder struct {
	calls    int
	external string
}

func (f *fakeBinder) BindVerifiedEnterpriseContact(_ context.Context, _ int64, _, external string) error {
	f.calls++
	f.external = external
	return nil
}
func setup() (*Service, *fakeProvider, *fakeStore, *fakeBinder, *fakeIdentity) {
	p := &fakeProvider{contact: ExternalContact{UnionID: "same-owner", ExternalUserID: "ext", FollowUsers: []string{"advisor"}}}
	st := &fakeStore{}
	b := &fakeBinder{}
	identity := &fakeIdentity{union: "same-owner"}
	return &Service{Provider: p, Store: st, Identity: identity, Binder: b, EmployeeID: "advisor", Now: func() time.Time { return time.Unix(100, 0) }}, p, st, b, identity
}
func event(st *fakeStore) Event {
	return Event{Event: "change_external_contact", ChangeType: "add_external_contact", State: st.t.State, ExternalUserID: "ext", EmployeeID: "advisor", WelcomeCode: "welcome", CreateTime: 100}
}
func TestVerifiedOwnerGetsOneWelcomeAndRepeatReusesQR(t *testing.T) {
	s, p, st, b, _ := setup()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := s.EnsureContact(ctx, "report", 1); err != nil {
			t.Fatal(err)
		}
	}
	if p.creates != 1 || len(p.state) != 30 {
		t.Fatal("QR must be reusable, state <=30")
	}
	for i := 0; i < 2; i++ {
		if err := s.HandleEvent(ctx, event(st)); err != nil {
			t.Fatal(err)
		}
	}
	if p.sends != 1 || b.external != "ext" || st.t.WelcomeState != "sent" {
		t.Fatal("wrong delivery")
	}
	way, err := s.EnsureContact(ctx, "report", 1)
	if err != nil || !way.Bound {
		t.Fatal("existing verified contact should resume")
	}
}
func TestUnionIDIsComparedNotJustPresent(t *testing.T) {
	for _, union := range []string{"", "someone-else"} {
		s, p, st, b, _ := setup()
		_, _ = s.EnsureContact(context.Background(), "report", 1)
		p.contact.UnionID = union
		if s.HandleEvent(context.Background(), event(st)) == nil || b.calls != 0 || p.sends != 0 {
			t.Fatal("wrong owner bound")
		}
	}
}
func TestUnpaidOwnerCannotCreateContact(t *testing.T) {
	s, p, _, _, identity := setup()
	identity.denied = true
	if _, err := s.EnsureContact(context.Background(), "report", 1); !errors.Is(err, ErrNotOwner) || p.creates != 0 {
		t.Fatal("unpaid request reached provider")
	}
}
func TestEmployeeIDIsNotMiniProgramUserID(t *testing.T) {
	s, p, st, b, _ := setup()
	_, _ = s.EnsureContact(context.Background(), "report", 1)
	e := event(st)
	e.EmployeeID = "1"
	if !errors.Is(s.HandleEvent(context.Background(), e), ErrNotOwner) || b.calls != 0 || p.sends != 0 {
		t.Fatal("wrong employee")
	}
}
func TestExpiredStateAndAmbiguousWelcomeFailSafely(t *testing.T) {
	s, p, st, _, _ := setup()
	_, _ = s.EnsureContact(context.Background(), "report", 1)
	s.Now = func() time.Time { return st.t.ExpiresAt }
	if !errors.Is(s.HandleEvent(context.Background(), event(st)), ErrExpired) {
		t.Fatal("expired state accepted")
	}
	s.Now = func() time.Time { return time.Unix(100, 0) }
	p.sendErr = errors.New("timeout")
	if err := s.HandleEvent(context.Background(), event(st)); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleEvent(context.Background(), event(st)); err != nil {
		t.Fatal(err)
	}
	if p.sends != 1 || st.t.WelcomeState != "manual_required" {
		t.Fatal("must not blindly repeat one-shot welcome")
	}
}
func TestLateOrAbsentWelcomeDoesNotPretendDelivery(t *testing.T) {
	s, p, st, b, _ := setup()
	_, _ = s.EnsureContact(context.Background(), "report", 1)
	e := event(st)
	e.WelcomeCode = ""
	if s.HandleEvent(context.Background(), e) != nil || b.calls != 1 || p.sends != 0 || st.t.WelcomeState != "manual_required" {
		t.Fatal("wrong late welcome")
	}
}
func TestConfigFailsClosed(t *testing.T) {
	t.Setenv("REPORT_WECOM_ENABLED", "true")
	t.Setenv("REPORT_WECOM_SECRET", "")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("missing config accepted")
	}
}
