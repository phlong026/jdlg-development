package officialaccount

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu       sync.Mutex
	intents  map[string]BindingIntent
	bindings map[string]Binding
	events   map[string]bool
}

func NewMemoryStore() *memoryStore {
	return &memoryStore{intents: map[string]BindingIntent{}, bindings: map[string]Binding{}, events: map[string]bool{}}
}

func (s *memoryStore) CreateIntent(_ context.Context, userID int64, now time.Time, ttl time.Duration) (BindingIntent, error) {
	state, err := randomState()
	if err != nil {
		return BindingIntent{}, err
	}
	intent := BindingIntent{State: state, UserID: userID, ExpiresAt: now.Add(ttl)}
	s.mu.Lock()
	s.intents[state] = intent
	s.mu.Unlock()
	return intent, nil
}

func bindingUserKey(appID string, userID int64) string {
	return appID + ":u:" + time.Unix(userID, 0).String()
}
func bindingOpenKey(appID, openID string) string { return appID + ":o:" + openID }

func (s *memoryStore) ConsumeAndBind(_ context.Context, state, appID, openID, unionID string, subscribed bool, now time.Time) (Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	intent, ok := s.intents[state]
	if !ok {
		return Binding{}, ErrInvalidState
	}
	userID, err := consumeBindingIntent(&intent, state, now)
	if err != nil {
		return Binding{}, err
	}
	s.intents[state] = intent
	version := int64(1)
	for _, key := range []string{bindingUserKey(appID, userID), bindingOpenKey(appID, openID)} {
		if previous, exists := s.bindings[key]; exists && previous.Version >= version {
			version = previous.Version + 1
		}
	}
	if previous, exists := s.bindings[bindingOpenKey(appID, openID)]; exists {
		delete(s.bindings, bindingUserKey(appID, previous.UserID))
	}
	if previous, exists := s.bindings[bindingUserKey(appID, userID)]; exists {
		delete(s.bindings, bindingOpenKey(appID, previous.OpenID))
	}
	binding := Binding{AppID: appID, OpenID: openID, UnionID: unionID, UserID: userID, Version: version, Active: true, Subscribed: subscribed, UpdatedAt: now}
	s.bindings[bindingUserKey(appID, userID)] = binding
	s.bindings[bindingOpenKey(appID, openID)] = binding
	return binding, nil
}

func (s *memoryStore) ResolveBinding(_ context.Context, userID int64, appID string) (Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.bindings[bindingUserKey(appID, userID)]
	if !ok {
		return Binding{}, ErrNotBound
	}
	if !binding.Active {
		return Binding{}, ErrNotBound
	}
	if !binding.Subscribed {
		return Binding{}, ErrNotSubscribed
	}
	return binding, nil
}

func (s *memoryStore) Unbind(_ context.Context, userID int64, appID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	userKey := bindingUserKey(appID, userID)
	binding, ok := s.bindings[userKey]
	if !ok || !binding.Active {
		return ErrNotBound
	}
	binding.Active = false
	binding.Subscribed = false
	binding.Version++
	binding.UpdatedAt = now
	s.bindings[userKey] = binding
	s.bindings[bindingOpenKey(appID, binding.OpenID)] = binding
	return nil
}

func (s *memoryStore) ResolveVersion(ctx context.Context, userID int64, appID string, version int64) (Binding, error) {
	binding, err := s.ResolveBinding(ctx, userID, appID)
	if err == nil && binding.Version != version {
		return Binding{}, ErrBindingChanged
	}
	return binding, err
}

func (s *memoryStore) ApplyEvent(_ context.Context, event CallbackEvent, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.events[event.DedupKey] {
		return true, nil
	}
	s.events[event.DedupKey] = true
	binding, ok := s.bindings[bindingOpenKey(event.AppID, event.OpenID)]
	if ok && binding.Active {
		binding.Subscribed = event.Type == EventSubscribe
		binding.Version++
		binding.UpdatedAt = now
		s.bindings[bindingOpenKey(event.AppID, event.OpenID)] = binding
		s.bindings[bindingUserKey(event.AppID, binding.UserID)] = binding
	}
	return false, nil
}

func TestMemoryStoreUnbindInvalidatesAndPreservesVersionHistory(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	intent, _ := store.CreateIntent(ctx, 42, now, time.Minute)
	first, err := store.ConsumeAndBind(ctx, intent.State, "wx-official", "oa-openid", "", true, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Unbind(ctx, 42, "wx-official", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ResolveBinding(ctx, 42, "wx-official"); !errors.Is(err, ErrNotBound) {
		t.Fatalf("unbound binding resolved: %v", err)
	}
	intent, _ = store.CreateIntent(ctx, 42, now.Add(2*time.Second), time.Minute)
	rebound, err := store.ConsumeAndBind(ctx, intent.State, "wx-official", "oa-openid", "", true, now.Add(2*time.Second))
	if err != nil || rebound.Version <= first.Version+1 {
		t.Fatalf("rebound=%+v err=%v", rebound, err)
	}
}

func TestBindingIntentRejectsWrongExpiredAndReplayedState(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	intent := BindingIntent{State: "one-time-state", UserID: 42, ExpiresAt: now.Add(time.Minute)}
	if _, err := consumeBindingIntent(&intent, "other-state", now); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("wrong state error = %v", err)
	}
	if _, err := consumeBindingIntent(&intent, intent.State, now.Add(2*time.Minute)); !errors.Is(err, ErrExpiredState) {
		t.Fatalf("expired state error = %v", err)
	}
	userID, err := consumeBindingIntent(&intent, intent.State, now)
	if err != nil || userID != 42 {
		t.Fatalf("consume = user:%d err:%v", userID, err)
	}
	if _, err := consumeBindingIntent(&intent, intent.State, now); !errors.Is(err, ErrConsumedState) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestMemoryStoreBindingReplacementUnsubscribeAndVersionGuard(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	first, err := store.CreateIntent(ctx, 42, now, 5*time.Minute)
	if err != nil || first.State == "" {
		t.Fatal(first, err)
	}
	binding, err := store.ConsumeAndBind(ctx, first.State, "wx-official", "oa-openid", "", true, now)
	if err != nil || binding.UserID != 42 || binding.Version != 1 || !binding.Subscribed {
		t.Fatal(binding, err)
	}
	second, _ := store.CreateIntent(ctx, 77, now, 5*time.Minute)
	replaced, err := store.ConsumeAndBind(ctx, second.State, "wx-official", "oa-openid", "", true, now.Add(time.Second))
	if err != nil || replaced.UserID != 77 || replaced.Version <= binding.Version {
		t.Fatal(replaced, err)
	}
	if _, err := store.ResolveBinding(ctx, 42, "wx-official"); !errors.Is(err, ErrNotBound) {
		t.Fatalf("old user retained binding: %v", err)
	}
	if _, err := store.ResolveVersion(ctx, 77, "wx-official", binding.Version); !errors.Is(err, ErrBindingChanged) {
		t.Fatalf("stale version accepted: %v", err)
	}
	duplicate, err := store.ApplyEvent(ctx, CallbackEvent{DedupKey: "event-1", AppID: "wx-official", OpenID: "oa-openid", Type: EventUnsubscribe}, now.Add(2*time.Second))
	if err != nil || duplicate {
		t.Fatal(duplicate, err)
	}
	duplicate, err = store.ApplyEvent(ctx, CallbackEvent{DedupKey: "event-1", AppID: "wx-official", OpenID: "oa-openid", Type: EventUnsubscribe}, now.Add(3*time.Second))
	if err != nil || !duplicate {
		t.Fatal("repeated callback was not deduplicated", duplicate, err)
	}
	if _, err := store.ResolveBinding(ctx, 77, "wx-official"); !errors.Is(err, ErrNotSubscribed) {
		t.Fatalf("unsubscribed binding resolved: %v", err)
	}
}
