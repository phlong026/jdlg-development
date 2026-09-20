package report

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func (s *decisionMemoryStore) YieldHouse(_ context.Context, id, token, mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.reports[id]
	if r == nil || r.ProtocolVersion != ProtocolHouseV1 || r.LeaseToken != token {
		return ErrConflict
	}
	r.Status = StatusFullQueued
	if mode == ModePreview {
		r.Status = StatusPreviewQueued
	}
	r.AttemptCount--
	r.LeaseToken = ""
	return nil
}

func TestHouseServiceConfirmationPaymentAndAsyncResume(t *testing.T) {
	ctx := context.Background()
	store := newDecisionMemoryStore()
	cfg := decisionWorkerConfig(1)
	cfg.HouseEnabled = true
	cfg.JobTimeout, cfg.LeaseDuration = 5*time.Minute, 6*time.Minute
	storage := newMemoryStorage()
	service, err := NewService(cfg, store, nil, storage)
	if err != nil {
		t.Fatal(err)
	}
	input := houseDecisionInput(3, 6, FlexibleUseNo)
	created, err := service.CreateHousePreview(ctx, 11, input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusPreviewQueued {
		t.Fatalf("not asynchronous: %+v", created)
	}
	if _, err := service.CreateDecisionPreview(ctx, 11, input); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("house input accepted by old endpoint")
	}
	renderer := &countingRenderer{}
	images := &houseImageFixture{}
	agent, err := NewHouseOrchestrator(&houseAgentFixture{}, &decisionCheckpoints{}, images)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(cfg, store, nil, nil, renderer, storage, WithAgentRunner(ProtocolDispatcher{House: agent}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, 11, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.NextAction != "confirm_decision" || renderer.calls != 0 {
		t.Fatalf("unexpected preview: %+v", view)
	}
	if _, err := service.ReservePaymentOrder(ctx, 11, created.ID, "HOUSE-ORDER"); err == nil {
		t.Fatal("unconfirmed payment allowed")
	}
	var preview DecisionSnapshot
	if err := json.Unmarshal(view.PreviewSnapshot, &preview); err != nil {
		t.Fatal(err)
	}
	confirmation := DecisionConfirmation{DiagnosisID: preview.Diagnosis.ID, InputHash: view.InputHash, Choice: ChoiceConfirm, Priorities: input.Priorities}
	if _, err := service.ConfirmDecision(ctx, 12, created.ID, confirmation); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong owner: %v", err)
	}
	if _, err := service.ConfirmDecision(ctx, 11, created.ID, confirmation); err != nil {
		t.Fatal(err)
	}
	quote, err := service.GetPaymentQuote(ctx, 11, created.ID)
	if err != nil || !quote.CanPay || quote.PackID != "whole-house-v1" {
		t.Fatalf("quote=%+v err=%v", quote, err)
	}
	if _, err := service.ReservePaymentOrder(ctx, 11, created.ID, "HOUSE-ORDER"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeVerifiedPayment(ctx, 11, created.ID, "HOUSE-ORDER"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := worker.RunOnce(ctx); err != nil {
			t.Fatalf("lease %d: %v", i, err)
		}
		if store.reports[created.ID].Status == StatusReady {
			break
		}
		if store.reports[created.ID].AttemptCount != 0 {
			t.Fatal("successful progress exhausted retry count")
		}
	}
	done := store.reports[created.ID]
	if done.Status != StatusReady || renderer.calls != 1 || images.creates != 10 {
		t.Fatalf("status=%s renders=%d images=%d", done.Status, renderer.calls, images.creates)
	}
	if string(done.PreviewSnapshot) != string(view.PreviewSnapshot) {
		t.Fatal("full generation rewrote frozen preview")
	}
	if _, err := service.GetDownloadURL(ctx, 11, created.ID, ModeFull); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetDownloadURL(ctx, 12, created.ID, ModeFull); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross owner download: %v", err)
	}
}

type uncertainHouseAgent struct{}

func (uncertainHouseAgent) Run(context.Context, *Report, string, *ReviewedPack) (json.RawMessage, string, error) {
	return nil, "", ErrImageUncertain
}

func TestHouseUncertainSubmissionStopsAutomaticAndUserRetry(t *testing.T) {
	ctx := context.Background()
	store := newDecisionMemoryStore()
	cfg := decisionWorkerConfig(3)
	cfg.HouseEnabled = true
	cfg.JobTimeout, cfg.LeaseDuration = 5*time.Minute, 6*time.Minute
	svc, err := NewService(cfg, store, nil, newMemoryStorage())
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.CreateHousePreview(ctx, 11, houseDecisionInput(2, 4, FlexibleUseNo))
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(cfg, store, nil, nil, fakeRenderer{}, newMemoryStorage(), WithAgentRunner(uncertainHouseAgent{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(ctx); err == nil {
		t.Fatal("uncertain submission reported success")
	}
	failed, err := svc.Get(ctx, 11, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusFailed || failed.NextAction != "contact_support" {
		t.Fatalf("unsafe recovery: %+v", failed)
	}
	if _, err := svc.Retry(ctx, 11, view.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("uncertain submission retried: %v", err)
	}
	if worked, err := worker.RunOnce(ctx); err != nil || worked {
		t.Fatalf("automatically retried: %t %v", worked, err)
	}
}
