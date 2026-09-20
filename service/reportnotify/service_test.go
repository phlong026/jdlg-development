package reportnotify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type memoryStore struct {
	configured  bool
	queued      int64
	job         *Job
	finished    Outcome
	finishCount int
	err         error
}

func (s *memoryStore) CatchUp(_ context.Context, configured bool, _ int) (int64, error) {
	s.configured = configured
	return s.queued, s.err
}
func (s *memoryStore) Claim(context.Context, time.Duration) (*Job, error) {
	if s.job == nil {
		return nil, ErrNoJob
	}
	job := s.job
	s.job = nil
	return job, nil
}
func (s *memoryStore) Finish(_ context.Context, _ *Job, outcome Outcome) error {
	s.finished = outcome
	s.finishCount++
	return s.err
}

type providerFixture struct {
	sends, lookups int
	notification   Notification
	receipt        Receipt
	err            error
}

func (p *providerFixture) Send(_ context.Context, n Notification) (Receipt, error) {
	p.sends++
	p.notification = n
	return p.receipt, p.err
}
func (p *providerFixture) Lookup(context.Context, string) (Receipt, error) {
	p.lookups++
	return p.receipt, p.err
}
func readyJob(reconcile bool) *Job {
	return &Job{EventID: EventID("report-1"), ReportID: "report-1", UserID: 7,
		ExternalUserID: "external-owner", LeaseToken: "lease-test", Reconcile: reconcile}
}
func configured() Config {
	return Config{Endpoint: "https://gateway.example/deliveries", Token: "test-token"}
}

func TestUnconfiguredServiceQueuesManualFollowupWithoutSending(t *testing.T) {
	store := &memoryStore{queued: 1, job: readyJob(false)}
	provider := &providerFixture{}
	service, err := NewService(Config{}, store, provider)
	if err != nil {
		t.Fatal(err)
	}
	worked, err := service.RunOnce(context.Background())
	if err != nil || !worked || store.configured || provider.sends != 0 || store.finishCount != 0 {
		t.Fatalf("unconfigured delivery changed state: worked=%v err=%v sends=%d", worked, err, provider.sends)
	}
}

func TestConfirmedDeliveryUsesStableIdentityAndMinimalPayload(t *testing.T) {
	store := &memoryStore{job: readyJob(false)}
	provider := &providerFixture{receipt: Receipt{Status: StateSent, Receipt: "receipt-1"}}
	service, err := NewService(configured(), store, provider)
	if err != nil {
		t.Fatal(err)
	}
	worked, err := service.RunOnce(context.Background())
	if err != nil || !worked || provider.sends != 1 || store.finished.State != StateSent || store.finished.Receipt != "receipt-1" {
		t.Fatalf("delivery failed: worked=%v err=%v outcome=%+v", worked, err, store.finished)
	}
	n := provider.notification
	if n.EventID != EventID(n.ReportID) || n.UserID != 7 || n.ExternalUserID != "external-owner" || n.MiniProgramPath != "pages/report/delivery?id=report-1" || strings.Contains(n.Text, "https://") {
		t.Fatalf("unexpected notification contract: %+v", n)
	}
}

func TestTimeoutRemainsUncertainAndReconciliationNeverResends(t *testing.T) {
	store := &memoryStore{job: readyJob(false)}
	provider := &providerFixture{err: context.DeadlineExceeded}
	service, _ := NewService(configured(), store, provider)
	if _, err := service.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.finished.State != StateUncertain {
		t.Fatalf("timeout became %+v", store.finished)
	}
	store.job = readyJob(true)
	provider.err = nil
	provider.receipt = Receipt{Status: "not_found"}
	if _, err := service.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provider.sends != 1 || provider.lookups != 1 || store.finished.State != StateUncertain {
		t.Fatalf("uncertain event resent: sends=%d lookups=%d outcome=%+v", provider.sends, provider.lookups, store.finished)
	}
	store.job = readyJob(true)
	provider.receipt = Receipt{Status: StateSent, Receipt: "reconciled"}
	if _, err := service.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.finished.State != StateSent || provider.sends != 1 {
		t.Fatal("lookup did not resolve delivery")
	}
}

func TestReceiptIsRequiredAndFailuresDoNotPretendSuccess(t *testing.T) {
	for _, test := range []struct {
		name    string
		receipt Receipt
		err     error
		state   string
	}{
		{"missing receipt", Receipt{Status: StateSent}, nil, StateUncertain},
		{"pending", Receipt{Status: "pending"}, nil, StateUncertain},
		{"rejected", Receipt{}, ErrRejected, StateManualRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &memoryStore{job: readyJob(false)}
			provider := &providerFixture{receipt: test.receipt, err: test.err}
			service, _ := NewService(configured(), store, provider)
			if _, err := service.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if store.finished.State != test.state {
				t.Fatalf("outcome=%+v", store.finished)
			}
		})
	}
	store := &memoryStore{err: errors.New("database unavailable")}
	service, _ := NewService(configured(), store, &providerFixture{})
	if _, err := service.RunOnce(context.Background()); err == nil {
		t.Fatal("store failure was swallowed")
	}
}

func TestLoadConfigRejectsPartialOrUnsafeGateway(t *testing.T) {
	for _, test := range []struct {
		endpoint, token string
		valid           bool
	}{
		{"", "", true}, {"https://gateway.example/deliveries", "test-token", true},
		{"http://gateway.example/deliveries", "test-token", false},
		{"https://gateway.example/deliveries", "", false},
		{"", "test-token", false}, {"https://user:pass@gateway.example/deliveries", "test-token", false},
	} {
		t.Setenv("REPORT_NOTIFY_ENDPOINT", test.endpoint)
		t.Setenv("REPORT_NOTIFY_TOKEN", test.token)
		_, err := LoadConfig()
		if (err == nil) != test.valid {
			t.Fatalf("config validity=%v err=%v", test.valid, err)
		}
	}
}
