package notification

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeEventState struct {
	status     string
	attempts   int
	leaseToken string
	leaseUntil time.Time
}

type fakeDurableStore struct {
	mu                sync.Mutex
	events            map[string]Event
	eventJobs         map[string]fakeEventState
	notices           map[string]Notification
	deliveries        map[string]Delivery
	receipts          map[string]string
	failDelivery      bool
	browseMergeWindow time.Duration
	prefs             map[int64]Preferences
	inTx              bool
	seq               int
}

func newFakeDurableStore(events ...Event) *fakeDurableStore {
	store := &fakeDurableStore{events: map[string]Event{}, eventJobs: map[string]fakeEventState{}, notices: map[string]Notification{}, deliveries: map[string]Delivery{}, receipts: map[string]string{}, prefs: map[int64]Preferences{}}
	for _, event := range events {
		store.events[event.ID] = event
	}
	return store
}

// categoryOptedOut 与 SQLStore 的外发类别检查保持同一语义（B16b）。
func (s *fakeDurableStore) categoryOptedOut(recipientID int64, kind string) bool {
	category := deliveryCategory(kind)
	if category == "" || recipientID <= 0 {
		return false
	}
	prefs, ok := s.prefs[recipientID]
	if !ok {
		return false
	}
	if category == "browse" {
		return !prefs.BrowseDelivery
	}
	return !prefs.ProjectDelivery
}

func (s *fakeDurableStore) token() string {
	s.seq++
	return time.Unix(int64(s.seq), 0).UTC().Format(time.RFC3339Nano)
}

func (s *fakeDurableStore) ClaimEvent(_ context.Context, now time.Time, lease time.Duration, maxAttempts int) (*EventJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, event := range s.events {
		state, exists := s.eventJobs[id]
		if exists && (state.status == "processed" || state.status == "failed" || state.leaseUntil.After(now) || state.attempts >= maxAttempts) {
			continue
		}
		state.status, state.attempts, state.leaseToken, state.leaseUntil = "processing", state.attempts+1, s.token(), now.Add(lease)
		s.eventJobs[id] = state
		return &EventJob{Event: event, LeaseToken: state.leaseToken}, nil
	}
	return nil, ErrNoJob
}

func (s *fakeDurableStore) MaterializeEvent(_ context.Context, job *EventJob, channel string, eligibility Eligibility) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inTx = true
	defer func() { s.inTx = false }()
	state := s.eventJobs[job.Event.ID]
	if state.status == "processed" {
		return nil
	}
	if state.status != "processing" || state.leaseToken != job.LeaseToken {
		return ErrConflict
	}
	if !eligibility.Eligible {
		state.status, state.leaseToken, state.leaseUntil = "cancelled", "", time.Time{}
		s.eventJobs[job.Event.ID] = state
		return nil
	}
	notice := noticeFromEvent(job.Event)
	if eligibility.Body != "" {
		notice.Body = notificationBody(eligibility.Body)
	}
	// 与 SQLStore 保持同一合并语义；不新建收件箱条目或外发投递。
	if recipientID, kind, shareID, at, mergeable := browseMergeTarget(job.Event, s.browseMergeWindow); mergeable {
		existing := make([]Notification, 0, len(s.notices))
		for _, item := range s.notices {
			existing = append(existing, item)
		}
		if existingID, found := findMergeableBrowseNotice(existing, recipientID, kind, shareID, at, s.browseMergeWindow); found {
			merged := s.notices[existingID]
			merged.CreatedAt, merged.EventID = notice.CreatedAt, notice.EventID
			merged.Title, merged.Body, merged.Route, merged.ContentID = notice.Title, notice.Body, notice.Route, notice.ContentID
			s.notices[existingID] = merged
			state.status, state.leaseToken, state.leaseUntil = "processed", "", time.Time{}
			s.eventJobs[job.Event.ID] = state
			return nil
		}
	}
	delivery := Delivery{ID: notice.ID + ":" + channel, NotificationID: notice.ID, RecipientID: notice.RecipientID, Channel: channel, Status: DeliveryPending, BindingVersion: eligibility.BindingVersion, CreatedAt: notice.CreatedAt, UpdatedAt: notice.CreatedAt}
	if s.failDelivery {
		return errors.New("delivery insert failed")
	}
	s.notices[notice.ID] = notice
	if eligibility.DeliveryEligible && allowsExternalDelivery(job.Event) && !s.categoryOptedOut(notice.RecipientID, job.Event.Kind) {
		s.deliveries[delivery.ID] = delivery
	}
	state.status, state.leaseToken, state.leaseUntil = "processed", "", time.Time{}
	s.eventJobs[job.Event.ID] = state
	return nil
}

func (s *fakeDurableStore) ClaimDelivery(_ context.Context, channel string, now time.Time, lease time.Duration, maxAttempts int) (*DeliveryJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, delivery := range s.deliveries {
		if delivery.Channel != channel || delivery.Status == DeliveryCancelled || delivery.Status == DeliveryAccepted || delivery.Status == DeliveryFailed || delivery.Status == DeliveryUnknown || delivery.AttemptCount >= maxAttempts {
			continue
		}
		if delivery.NextAttemptAt != nil && delivery.NextAttemptAt.After(now) {
			continue
		}
		if delivery.Status == DeliveryProcessing && delivery.LeaseUntil != nil && delivery.LeaseUntil.After(now) {
			continue
		}
		token := s.token()
		until := now.Add(lease)
		delivery.Status, delivery.AttemptCount, delivery.LeaseToken, delivery.LeaseUntil, delivery.UpdatedAt = DeliveryProcessing, delivery.AttemptCount+1, token, &until, now
		s.deliveries[id] = delivery
		notice := s.notices[delivery.NotificationID]
		if notice.CancelledAt != nil {
			continue
		}
		return &DeliveryJob{Delivery: delivery, Notice: notice}, nil
	}
	return nil, ErrNoJob
}

func (s *fakeDurableStore) CompleteDelivery(_ context.Context, id, token, status, providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delivery, ok := s.deliveries[id]
	if !ok || delivery.Status != DeliveryProcessing || delivery.LeaseToken != token {
		return ErrConflict
	}
	delivery.Status, delivery.LeaseToken, delivery.LeaseUntil = status, "", nil
	delivery.ProviderMessageID = providerID
	if receipt, exists := s.receipts[providerID]; exists {
		delivery.ProviderStatus = receipt
	}
	s.deliveries[id] = delivery
	return nil
}

func (s *fakeDurableStore) FailDelivery(_ context.Context, id, token, reason string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delivery, ok := s.deliveries[id]
	if !ok || delivery.Status != DeliveryProcessing || delivery.LeaseToken != token {
		return ErrConflict
	}
	delivery.Status = DeliveryFailed
	if !next.IsZero() {
		delivery.Status = DeliveryPending
		delivery.NextAttemptAt = &next
	}
	delivery.LastError, delivery.LeaseToken, delivery.LeaseUntil = reason, "", nil
	s.deliveries[id] = delivery
	return nil
}

func (s *fakeDurableStore) CancelDelivery(_ context.Context, id, token, reason string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delivery, ok := s.deliveries[id]
	if !ok || delivery.Status != DeliveryProcessing || delivery.LeaseToken != token {
		return ErrConflict
	}
	delivery.Status, delivery.LastError, delivery.LeaseToken, delivery.LeaseUntil, delivery.UpdatedAt = DeliveryCancelled, reason, "", nil, at
	s.deliveries[id] = delivery
	return nil
}

func (s *fakeDurableStore) RecordProviderReceipt(_ context.Context, providerID, status string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receipts[providerID] = status
	for id, delivery := range s.deliveries {
		if delivery.ProviderMessageID == providerID {
			delivery.ProviderStatus = status
			s.deliveries[id] = delivery
			return true, nil
		}
	}
	return false, nil
}

func TestProviderReceiptArrivingBeforeCompletionIsReconciled(t *testing.T) {
	store := newFakeDurableStore()
	store.deliveries["delivery-early"] = Delivery{ID: "delivery-early", Status: DeliveryProcessing, LeaseToken: "lease", BindingVersion: 1}
	matched, err := store.RecordProviderReceipt(context.Background(), "provider-early", "success", time.Now())
	if err != nil || matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	if err := store.CompleteDelivery(context.Background(), "delivery-early", "lease", DeliveryAccepted, "provider-early"); err != nil {
		t.Fatal(err)
	}
	if got := store.deliveries["delivery-early"].ProviderStatus; got != "success" {
		t.Fatalf("provider status=%q", got)
	}
}

func (s *fakeDurableStore) ListInbox(_ context.Context, recipientID int64, _ string, limit int) (Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page := Page{}
	for _, notice := range s.notices {
		if notice.RecipientID == recipientID && notice.CancelledAt == nil && len(page.Items) < limit {
			page.Items = append(page.Items, notice)
		}
	}
	return page, nil
}

func (s *fakeDurableStore) MarkRead(_ context.Context, recipientID int64, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	notice, ok := s.notices[id]
	if !ok || notice.RecipientID != recipientID {
		return ErrConflict
	}
	if notice.ReadAt == nil {
		notice.ReadAt = &at
		s.notices[id] = notice
	}
	return nil
}

func (s *fakeDurableStore) GetNotification(_ context.Context, recipientID int64, id string) (Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	notice, ok := s.notices[id]
	if !ok || notice.RecipientID != recipientID {
		return Notification{}, ErrNotFound
	}
	return notice, nil
}

func (s *fakeDurableStore) UnreadCount(_ context.Context, recipientID int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	for _, notice := range s.notices {
		if notice.RecipientID == recipientID && notice.ReadAt == nil && notice.CancelledAt == nil {
			count++
		}
	}
	return count, nil
}

func (s *fakeDurableStore) MarkAllRead(_ context.Context, recipientID int64, at time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	for id, notice := range s.notices {
		if notice.RecipientID == recipientID && notice.ReadAt == nil && notice.CancelledAt == nil {
			notice.ReadAt = &at
			s.notices[id] = notice
			count++
		}
	}
	return count, nil
}

func (s *fakeDurableStore) DeleteVisitorData(_ context.Context, visitorKey string, _ time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	for id, event := range s.events {
		if event.VisitorKey == visitorKey {
			delete(s.events, id)
			delete(s.eventJobs, id)
			delete(s.notices, id)
			delete(s.deliveries, id+":service_account")
			count++
		}
	}
	return count, nil
}

func (s *fakeDurableStore) CancelPending(_ context.Context, contentID, shareID string, at time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	for id, notice := range s.notices {
		if notice.CancelledAt != nil || contentID != "" && notice.ContentID != contentID || shareID != "" && notice.ShareID != shareID {
			continue
		}
		notice.CancelledAt = &at
		s.notices[id] = notice
		count++
		for deliveryID, delivery := range s.deliveries {
			if delivery.NotificationID == id && (delivery.Status == DeliveryPending || delivery.Status == DeliveryProcessing) {
				delivery.Status, delivery.LeaseToken, delivery.LeaseUntil = DeliveryCancelled, "", nil
				s.deliveries[deliveryID] = delivery
			}
		}
	}
	return count, nil
}

func browseEvent(id string, recipient int64) Event {
	return Event{ID: id, Kind: "browse", Owner: "owner-a", RecipientID: recipient, VisitorKey: "u:7", ShareID: "share-0001", ContentID: "content-0001", Route: "pages/contentreader/contentreader", OccurredAt: time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)}
}

func TestProjectNoteOpenNoticeUsesGenericPrivateRoute(t *testing.T) {
	event := Event{ID: "project-open-event-1", Kind: "project_note_open", Owner: "owner-a", RecipientID: 42, VisitorKey: "u:101", ShareID: "share-private-1", ContentID: "version-private-1", ProjectID: "project-private-1", Route: "project_note_share", OccurredAt: time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)}
	if !validEvent(event) {
		t.Fatal("valid private project event was rejected")
	}
	notice := noticeFromEvent(event)
	if notice.Route != "pages/projectnotes/projectnotes?project_id=project-private-1&version_id=version-private-1" || notice.Title != "私人项目有新访问" {
		t.Fatalf("notice = %#v", notice)
	}
	encoded, err := json.Marshal(notice)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"share_token", "private-note-body", "download_url"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("private notice leaked %q: %s", forbidden, encoded)
		}
	}
	event.ProjectID = ""
	if validEvent(event) {
		t.Fatal("private project event without project id was accepted")
	}
	event.ProjectID = strings.Repeat("p", 100)
	event.ContentID = strings.Repeat("v", 100)
	if validEvent(event) {
		t.Fatal("private project event with an oversized persisted route was accepted")
	}
}

func TestProjectNoteFeedbackNoticeTargetsExactFeedbackWithoutExternalDelivery(t *testing.T) {
	event := Event{ID: "feedback:" + strings.Repeat("f", 64), Kind: "project_note_feedback", Owner: "owner-a", RecipientID: 42, VisitorKey: "u:101", ShareID: strings.Repeat("s", 32), ContentID: strings.Repeat("v", 32), ProjectID: strings.Repeat("p", 32), Route: "project_note_share", OccurredAt: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)}
	if !validEvent(event) {
		t.Fatal("valid project feedback event was rejected")
	}
	notice := noticeFromEvent(event)
	wantRoute := "pages/projectnotes/projectnotes?project_id=" + event.ProjectID
	if notice.Route != wantRoute || notice.Kind != event.Kind || notice.Title != "项目收到业主反馈" {
		t.Fatalf("notice = %#v", notice)
	}
	if notice.ID != event.ID || notice.ContentID != event.ContentID || len(notice.Route) > 128 {
		t.Fatalf("feedback coordinates were not persisted safely: %#v", notice)
	}
	if allowsExternalDelivery(event) {
		t.Fatal("active project feedback unexpectedly enabled external delivery")
	}
	if !allowsExternalDelivery(browseEvent("browse-1", 42)) {
		t.Fatal("existing browse delivery policy changed")
	}
	store := newFakeDurableStore(event)
	consumer := consumerFor(t, store, event.OccurredAt)
	if worked, err := consumer.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if len(store.notices) != 1 || len(store.deliveries) != 0 {
		t.Fatalf("notices=%d deliveries=%d", len(store.notices), len(store.deliveries))
	}
}

func TestProjectPackageReceiptNoticeTargetsExactVersionWithoutExternalDelivery(t *testing.T) {
	event := Event{ID: "package-receipt:" + strings.Repeat("r", 64), Kind: "project_package_receipt", Owner: "owner-a", RecipientID: 42, VisitorKey: "u:101", ShareID: strings.Repeat("d", 32), ContentID: strings.Repeat("v", 32), ProjectID: strings.Repeat("p", 32), Route: "project_note_share", OccurredAt: time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)}
	if !validEvent(event) {
		t.Fatal("valid project package receipt event was rejected")
	}
	notice := noticeFromEvent(event)
	wantRoute := "pages/projectnotes/projectnotes?project_id=" + event.ProjectID + "&version_id=" + event.ContentID
	if notice.Route != wantRoute || notice.Kind != event.Kind || notice.Title != "收到阅读回执" || notice.Body != "接收人已主动确认阅读该固定方案" {
		t.Fatalf("notice = %#v", notice)
	}
	if allowsExternalDelivery(event) {
		t.Fatal("private package receipt unexpectedly enabled external delivery")
	}
	store := newFakeDurableStore(event)
	consumer := consumerFor(t, store, event.OccurredAt)
	if worked, err := consumer.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if len(store.notices) != 1 || len(store.deliveries) != 0 {
		t.Fatalf("notices=%d deliveries=%d", len(store.notices), len(store.deliveries))
	}
}

func consumerFor(t *testing.T, store ConsumerStore, now time.Time) *Consumer {
	t.Helper()
	consumer, err := NewConsumer(store, ConsumerConfig{PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3, Channel: "service_account", Eligibility: func(context.Context, Event) (bool, error) { return true, nil }, Binding: func(context.Context, int64) (int64, bool, error) { return 1, true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	consumer.now = func() time.Time { return now }
	return consumer
}

func TestConsumerIsIdempotentAcrossRestartAndDuplicateRun(t *testing.T) {
	now := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	store := newFakeDurableStore(browseEvent("event-0001", 42))
	first := consumerFor(t, store, now)
	if worked, err := first.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	restarted := consumerFor(t, store, now.Add(time.Hour))
	if worked, err := restarted.RunOnce(context.Background()); err != nil || worked {
		t.Fatalf("duplicate restart worked=%v err=%v", worked, err)
	}
	if len(store.notices) != 1 || len(store.deliveries) != 1 {
		t.Fatalf("notices=%d deliveries=%d", len(store.notices), len(store.deliveries))
	}
}

func TestMaterializeRollbackThenLeaseRecovery(t *testing.T) {
	now := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	store := newFakeDurableStore(browseEvent("event-0002", 42))
	store.failDelivery = true
	consumer := consumerFor(t, store, now)
	if worked, err := consumer.RunOnce(context.Background()); err == nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if len(store.notices) != 0 || len(store.deliveries) != 0 {
		t.Fatal("partial inbox committed")
	}
	store.failDelivery = false
	restarted := consumerFor(t, store, now.Add(2*time.Minute))
	if worked, err := restarted.RunOnce(context.Background()); err != nil || !worked {
		t.Fatalf("recovery worked=%v err=%v", worked, err)
	}
	if len(store.notices) != 1 || len(store.deliveries) != 1 {
		t.Fatal("recovery did not materialize exactly once")
	}
}

func TestExpiredDeliveryLeaseBecomesUnknownInsteadOfBlindRetry(t *testing.T) {
	now := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	store := newFakeDurableStore(browseEvent("event-0003", 42))
	consumer := consumerFor(t, store, now)
	if _, err := consumer.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := store.ClaimDelivery(context.Background(), "service_account", now, time.Minute, 3)
	if err != nil || store.inTx {
		t.Fatalf("claim err=%v inTx=%v", err, store.inTx)
	}
	if _, err = store.ClaimDelivery(context.Background(), "service_account", now.Add(30*time.Second), time.Minute, 3); !errors.Is(err, ErrNoJob) {
		t.Fatal("active lease was stolen", err)
	}
	// The in-memory fixture mirrors provider uncertainty: an expired processing
	// lease must not be returned for automatic resend.
	store.mu.Lock()
	delivery := store.deliveries[first.Delivery.ID]
	delivery.Status, delivery.LeaseToken, delivery.LeaseUntil = DeliveryUnknown, "", nil
	store.deliveries[first.Delivery.ID] = delivery
	store.mu.Unlock()
	if _, err = store.ClaimDelivery(context.Background(), "service_account", now.Add(2*time.Minute), time.Minute, 3); !errors.Is(err, ErrNoJob) {
		t.Fatal("unknown delivery was retried", err)
	}
}

func TestInboxIsRecipientScopedAndCancellationStopsPendingDelivery(t *testing.T) {
	now := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	store := newFakeDurableStore(browseEvent("event-0042", 42), browseEvent("event-0043", 43))
	consumer := consumerFor(t, store, now)
	for i := 0; i < 2; i++ {
		if _, err := consumer.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	page42, _ := store.ListInbox(context.Background(), 42, "", 10)
	page43, _ := store.ListInbox(context.Background(), 43, "", 10)
	if len(page42.Items) != 1 || page42.Items[0].RecipientID != 42 || len(page43.Items) != 1 || page43.Items[0].RecipientID != 43 {
		t.Fatalf("42=%+v 43=%+v", page42, page43)
	}
	if err := store.MarkRead(context.Background(), 43, page42.Items[0].ID, now); !errors.Is(err, ErrConflict) {
		t.Fatal("cross-user read accepted", err)
	}
	changed, err := store.CancelPending(context.Background(), "", "share-0001", now)
	if err != nil || changed != 2 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	for _, delivery := range store.deliveries {
		if delivery.Status != DeliveryCancelled {
			t.Fatal("pending delivery survived cancellation", delivery)
		}
	}
}

func TestConsumerRunStopsCleanlyWithContext(t *testing.T) {
	consumer := consumerFor(t, newFakeDurableStore(), time.Now().UTC())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop")
	}
}

func TestInboxCursorRoundTripAndRejectsMalformedValue(t *testing.T) {
	at := time.Date(2026, 10, 27, 2, 3, 4, 500, time.UTC)
	cursor := encodeCursor(at, "event-0001")
	gotAt, gotID, err := decodeCursor(cursor)
	if err != nil || !gotAt.Equal(at) || gotID != "event-0001" {
		t.Fatalf("at=%v id=%q err=%v", gotAt, gotID, err)
	}
	if _, _, err := decodeCursor("not-a-cursor"); err == nil {
		t.Fatal("malformed cursor accepted")
	}
}
