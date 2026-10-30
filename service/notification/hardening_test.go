package notification

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type retryStore struct {
	mu            sync.Mutex
	claimErrors   int
	delivery      DeliveryJob
	completed     string
	providerID    string
	failedAt      time.Time
	failureReason string
}

func (s *retryStore) ClaimDelivery(_ context.Context, _ string, now time.Time, _ time.Duration, _ int) (*DeliveryJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErrors > 0 {
		s.claimErrors--
		return nil, ErrUnavailable
	}
	if s.completed != "" || !s.failedAt.IsZero() && now.Before(s.failedAt) {
		return nil, ErrNoJob
	}
	job := s.delivery
	job.Delivery.Status = DeliveryProcessing
	job.Delivery.LeaseToken = "lease"
	job.Delivery.AttemptCount++
	s.delivery = job
	return &job, nil
}
func (s *retryStore) CompleteDelivery(_ context.Context, _, _ string, status, providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed, s.providerID = status, providerID
	return nil
}
func (s *retryStore) FailDelivery(_ context.Context, _, _, reason string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failureReason, s.failedAt = reason, next
	return nil
}
func (s *retryStore) CancelDelivery(_ context.Context, _, _ string, reason string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed, s.failureReason, s.failedAt = DeliveryCancelled, reason, at
	return nil
}
func (s *retryStore) RecordProviderReceipt(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}

func TestDeliveryWorkerPersistsRetryAndProviderMessageID(t *testing.T) {
	now := time.Date(2026, 10, 27, 14, 0, 0, 0, time.UTC)
	store := &retryStore{delivery: DeliveryJob{Delivery: Delivery{ID: "delivery-1", BindingVersion: 7}, Notice: Notification{RecipientID: 42}}}
	calls := 0
	worker, err := NewDeliveryWorker(store, DeliveryWorkerConfig{
		Channel: "service_account", PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3,
		RetryBase: time.Minute, RetryMax: time.Hour,
		Eligibility: func(context.Context, DeliveryJob) (bool, error) { return true, nil },
		Send: func(context.Context, DeliveryJob) DeliveryResult {
			calls++
			if calls == 1 {
				return DeliveryResult{Status: DeliveryFailed, Reason: "temporary", Retryable: true}
			}
			return DeliveryResult{Status: DeliveryAccepted, ProviderMessageID: "provider-9"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return now }
	if worked, err := worker.RunOnce(context.Background()); err != nil || !worked || !store.failedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("worked=%v err=%v retry=%v", worked, err, store.failedAt)
	}
	worker.now = func() time.Time { return now.Add(time.Minute) }
	if worked, err := worker.RunOnce(context.Background()); err != nil || !worked || store.completed != DeliveryAccepted || store.providerID != "provider-9" {
		t.Fatalf("worked=%v err=%v status=%q provider=%q", worked, err, store.completed, store.providerID)
	}
}

func TestDeliveryWorkerRechecksEligibilityBeforeSend(t *testing.T) {
	store := &retryStore{delivery: DeliveryJob{Delivery: Delivery{ID: "delivery-2", BindingVersion: 3}, Notice: Notification{RecipientID: 42}}}
	sent := false
	worker, err := NewDeliveryWorker(store, DeliveryWorkerConfig{
		Channel: "service_account", PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3, RetryBase: time.Second, RetryMax: time.Minute,
		Eligibility: func(context.Context, DeliveryJob) (bool, error) { return false, nil },
		Send: func(context.Context, DeliveryJob) DeliveryResult {
			sent = true
			return DeliveryResult{Status: DeliveryAccepted}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil || sent || store.completed != DeliveryCancelled || store.failureReason != "ineligible" {
		t.Fatalf("sent=%v reason=%q retry=%v err=%v", sent, store.failureReason, store.failedAt, err)
	}
}

func TestConsumerRunRetriesStoreErrorsUntilCancelled(t *testing.T) {
	store := &transientConsumerStore{failures: 2}
	consumer, err := NewConsumer(store, ConsumerConfig{
		PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3, Channel: "service_account", ErrorBackoff: time.Millisecond,
		Eligibility: func(context.Context, Event) (bool, error) { return true, nil },
		Binding:     func(context.Context, int64) (int64, bool, error) { return 1, true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := consumer.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if store.calls < 3 {
		t.Fatalf("consumer stopped after transient error: calls=%d", store.calls)
	}
}

type transientConsumerStore struct{ failures, calls int }

func (s *transientConsumerStore) ClaimEvent(context.Context, time.Time, time.Duration, int) (*EventJob, error) {
	s.calls++
	if s.failures > 0 {
		s.failures--
		return nil, ErrUnavailable
	}
	return nil, ErrNoJob
}
func (*transientConsumerStore) MaterializeEvent(context.Context, *EventJob, string, Eligibility) error {
	return nil
}

func TestDeadLetterErrorDoesNotPoisonConsumer(t *testing.T) {
	store := &deadLetterStore{}
	consumer, err := NewConsumer(store, ConsumerConfig{PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3, Channel: "service_account", ErrorBackoff: time.Millisecond, Eligibility: func(context.Context, Event) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := consumer.RunOnce(context.Background())
	if err != nil || !worked || store.calls != 1 {
		t.Fatalf("worked=%v err=%v calls=%d", worked, err, store.calls)
	}
}

func TestConsumerCreatesInboxWithoutRetroactiveDeliveryWhenBindingMissing(t *testing.T) {
	store := newFakeDurableStore(browseEvent("event-inbox-only", 42))
	consumer, err := NewConsumer(store, ConsumerConfig{
		PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3, Channel: "service_account",
		Eligibility: func(context.Context, Event) (bool, error) { return true, nil },
		Binding:     func(context.Context, int64) (int64, bool, error) { return 0, false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := consumer.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if len(store.notices) != 1 || len(store.deliveries) != 0 {
		t.Fatalf("notices=%d deliveries=%d", len(store.notices), len(store.deliveries))
	}
}

func TestConsumerSuppressesWithdrawnEventBeforeMaterialization(t *testing.T) {
	store := newFakeDurableStore(browseEvent("event-withdrawn", 42))
	consumer, err := NewConsumer(store, ConsumerConfig{
		PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3, Channel: "service_account",
		Eligibility: func(context.Context, Event) (bool, error) { return false, nil },
		Binding:     func(context.Context, int64) (int64, bool, error) { return 1, true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := consumer.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if len(store.notices) != 0 || len(store.deliveries) != 0 || store.eventJobs["event-withdrawn"].status != "cancelled" {
		t.Fatalf("event=%+v notices=%d deliveries=%d", store.eventJobs["event-withdrawn"], len(store.notices), len(store.deliveries))
	}
}

func TestVisitorCleanupRemovesPendingEventAndDerivedNotice(t *testing.T) {
	store := newFakeDurableStore(browseEvent("event-private", 42), browseEvent("event-other", 43))
	store.events["event-private"] = Event{ID: "event-private", Kind: "browse", Owner: "owner-a", RecipientID: 42, VisitorKey: "u:99", ShareID: "share-private", Route: "pages/contentreader/contentreader", OccurredAt: time.Now().UTC()}
	consumer := consumerFor(t, store, time.Now().UTC())
	for i := 0; i < 2; i++ {
		if _, err := consumer.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := store.DeleteVisitorData(context.Background(), "u:99", time.Now().UTC())
	if err != nil || changed != 1 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	if _, exists := store.events["event-private"]; exists {
		t.Fatal("visitor-bearing notice_event survived deletion")
	}
	if _, exists := store.notices["event-private"]; exists {
		t.Fatal("derived inbox notice survived deletion")
	}
}

type deadLetterStore struct{ calls int }

func (s *deadLetterStore) ClaimEvent(context.Context, time.Time, time.Duration, int) (*EventJob, error) {
	s.calls++
	return nil, ErrDeadLettered
}
func (*deadLetterStore) MaterializeEvent(context.Context, *EventJob, string, Eligibility) error {
	return errors.New("must not materialize")
}
