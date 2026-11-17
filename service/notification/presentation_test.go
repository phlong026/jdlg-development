package notification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestConsumerPresentationGatesAndLegacyCompatibility(t *testing.T) {
	for _, mode := range []string{"nil", "allowed", "inbox_only", "denied", "error", "ineligible"} {
		t.Run(mode, func(t *testing.T) {
			event := browseEvent("event-presentation", 42)
			store := newFakeDurableStore(event)
			consumer := consumerFor(t, store, event.OccurredAt)
			var calls []string
			consumer.cfg.Eligibility = func(context.Context, Event) (bool, error) {
				calls = append(calls, "eligibility")
				return mode != "ineligible", nil
			}
			consumer.cfg.Binding = func(context.Context, int64) (int64, bool, error) {
				calls = append(calls, "binding")
				return 1, true, nil
			}
			if mode != "nil" {
				consumer.cfg.Presentation = func(context.Context, Event) (EventPresentation, bool, error) {
					calls = append(calls, "presentation")
					if mode == "error" {
						return EventPresentation{}, false, ErrUnavailable
					}
					return EventPresentation{Body: "张三浏览了你分享的《方案》", DeliveryAllowed: mode == "allowed"}, mode != "denied", nil
				}
			}
			worked, err := consumer.RunOnce(context.Background())
			if !worked || mode == "error" && !errors.Is(err, ErrUnavailable) || mode != "error" && err != nil {
				t.Fatalf("worked=%v err=%v", worked, err)
			}
			wantCalls := "eligibility,presentation"
			if mode == "nil" {
				wantCalls = "eligibility,binding"
			} else if mode == "allowed" {
				wantCalls += ",binding"
			} else if mode == "ineligible" {
				wantCalls = "eligibility"
			}
			if strings.Join(calls, ",") != wantCalls {
				t.Fatalf("calls=%v want=%s", calls, wantCalls)
			}
			if mode == "denied" || mode == "ineligible" || mode == "error" {
				if len(store.notices) != 0 || len(store.deliveries) != 0 {
					t.Fatal("ineligible or failed presentation created inbox/delivery")
				}
				wantStatus := "cancelled"
				if mode == "error" {
					wantStatus = "processing"
				}
				if store.eventJobs[event.ID].status != wantStatus {
					t.Fatalf("job=%+v", store.eventJobs[event.ID])
				}
				return
			}
			wantBody := "张三浏览了你分享的《方案》"
			wantDelivery := 1
			if mode == "nil" {
				wantBody = noticeFromEvent(event).Body
			} else if mode == "inbox_only" {
				wantDelivery = 0
			}
			if store.notices[event.ID].Body != wantBody || len(store.deliveries) != wantDelivery {
				t.Fatalf("notice=%+v deliveries=%d", store.notices[event.ID], len(store.deliveries))
			}
		})
	}
}

func TestConsumerPresentationRetriesAfterLeaseWithoutMaterializingFailure(t *testing.T) {
	event := browseEvent("event-retry-presentation", 42)
	store := newFakeDurableStore(event)
	consumer := consumerFor(t, store, event.OccurredAt)
	calls := 0
	consumer.cfg.Presentation = func(context.Context, Event) (EventPresentation, bool, error) {
		calls++
		if calls == 1 {
			return EventPresentation{}, false, ErrUnavailable
		}
		return EventPresentation{Body: "可重试的访问", DeliveryAllowed: true}, true, nil
	}
	if _, err := consumer.RunOnce(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err=%v", err)
	}
	consumer.now = func() time.Time { return event.OccurredAt.Add(2 * time.Minute) }
	if worked, err := consumer.RunOnce(context.Background()); !worked || err != nil || calls != 2 || len(store.notices) != 1 || len(store.deliveries) != 1 {
		t.Fatalf("worked=%v err=%v calls=%d notices=%d deliveries=%d", worked, err, calls, len(store.notices), len(store.deliveries))
	}
}

func TestBrowsePresentationMergeStaysCoherentWithoutRetroactiveDelivery(t *testing.T) {
	for _, initiallyEnabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "already_bound", false: "bind_and_enable_later"}[initiallyEnabled], func(t *testing.T) {
			first := browseEvent("event-first", 42)
			first.ContentID, first.Route = "content-first", "pages/first"
			store := newFakeDurableStore(first)
			store.browseMergeWindow = 30 * time.Minute
			consumer := consumerFor(t, store, first.OccurredAt)
			enabled := initiallyEnabled
			consumer.cfg.Binding = func(context.Context, int64) (int64, bool, error) { return 1, enabled, nil }
			consumer.cfg.Presentation = func(_ context.Context, e Event) (EventPresentation, bool, error) {
				return EventPresentation{Body: "访客 " + e.ID + " 浏览 " + e.ContentID, DeliveryAllowed: enabled}, true, nil
			}
			if _, err := consumer.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			enabled = true
			second := first
			second.ID, second.ContentID, second.Route = "event-second", "content-second", "pages/second"
			second.OccurredAt = first.OccurredAt.Add(time.Minute)
			store.events[second.ID] = second
			consumer.now = func() time.Time { return second.OccurredAt }
			if _, err := consumer.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			notice := store.notices[first.ID]
			wantDeliveries := 0
			if initiallyEnabled {
				wantDeliveries = 1
			}
			if len(store.notices) != 1 || len(store.deliveries) != wantDeliveries || notice.ID != first.ID || notice.EventID != second.ID || notice.ContentID != second.ContentID || notice.Route != second.Route || notice.Body != "访客 event-second 浏览 content-second" || !notice.CreatedAt.Equal(second.OccurredAt) {
				t.Fatalf("notice=%+v notices=%d deliveries=%d", notice, len(store.notices), len(store.deliveries))
			}
		})
	}
}

func TestMaterializedBodyFitsDatabaseCharactersWithoutBreakingUTF8(t *testing.T) {
	event := browseEvent("event-long-body", 42)
	store := newFakeDurableStore(event)
	consumer := consumerFor(t, store, event.OccurredAt)
	consumer.cfg.Presentation = func(context.Context, Event) (EventPresentation, bool, error) {
		return EventPresentation{Body: strings.Repeat("中🙂", 300)}, true, nil
	}
	if _, err := consumer.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := store.notices[event.ID].Body
	if !utf8.ValidString(body) || utf8.RuneCountInString(body) != 512 || body != strings.Repeat("中🙂", 256) {
		t.Fatalf("body chars=%d valid=%v", utf8.RuneCountInString(body), utf8.ValidString(body))
	}
}

type presentationCancelStore struct {
	retryStore
	completedCalls, failedCalls, cancelledCalls int
}

func (s *presentationCancelStore) CompleteDelivery(context.Context, string, string, string, string) error {
	s.completedCalls++
	return nil
}
func (s *presentationCancelStore) FailDelivery(context.Context, string, string, string, time.Time) error {
	s.failedCalls++
	return nil
}
func (s *presentationCancelStore) CancelDelivery(ctx context.Context, id, token, reason string, at time.Time) error {
	s.cancelledCalls++
	return s.retryStore.CancelDelivery(ctx, id, token, reason, at)
}

func TestDeliveryWorkerCancelsWhenSendRecheckRejectsPresentation(t *testing.T) {
	for _, reason := range []string{"", "content_withdrawn"} {
		t.Run(reason, func(t *testing.T) {
			store := &presentationCancelStore{retryStore: retryStore{delivery: DeliveryJob{Delivery: Delivery{ID: "delivery-revoked", BindingVersion: 1}}}}
			sends := 0
			worker, err := NewDeliveryWorker(store, DeliveryWorkerConfig{
				Channel: "official_account", PollInterval: time.Millisecond, LeaseDuration: time.Minute, MaxAttempts: 3, RetryBase: time.Second, RetryMax: time.Minute,
				Eligibility: func(context.Context, DeliveryJob) (bool, error) { return true, nil },
				Send: func(context.Context, DeliveryJob) DeliveryResult {
					sends++
					return DeliveryResult{Status: DeliveryCancelled, Reason: reason, Retryable: true}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if worked, err := worker.RunOnce(context.Background()); !worked || err != nil {
				t.Fatalf("worked=%v err=%v", worked, err)
			}
			wantReason := reason
			if wantReason == "" {
				wantReason = "ineligible"
			}
			if store.cancelledCalls != 1 || store.completedCalls != 0 || store.failedCalls != 0 || store.failureReason != wantReason || store.completed != DeliveryCancelled {
				t.Fatalf("cancel=%d complete=%d fail=%d status=%s reason=%s", store.cancelledCalls, store.completedCalls, store.failedCalls, store.completed, store.failureReason)
			}
			if worked, err := worker.RunOnce(context.Background()); worked || err != nil || sends != 1 {
				t.Fatalf("cancelled notice retried: worked=%v err=%v sends=%d", worked, err, sends)
			}
		})
	}
}
