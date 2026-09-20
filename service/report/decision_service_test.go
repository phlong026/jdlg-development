package report

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type decisionMemoryStore struct {
	*memoryStore
	contexts map[string]DecisionContext
	readErr  error
}

func newDecisionMemoryStore() *decisionMemoryStore {
	return &decisionMemoryStore{memoryStore: newMemoryStore(), contexts: map[string]DecisionContext{}}
}

func (s *decisionMemoryStore) GetDecisionContext(_ context.Context, userID int64, reportID string) (DecisionContext, error) {
	if s.readErr != nil {
		return DecisionContext{}, s.readErr
	}
	record, err := s.GetForUser(context.Background(), userID, reportID)
	if err != nil {
		return DecisionContext{}, err
	}
	context, ok := s.contexts[record.ID]
	if !ok {
		return DecisionContext{}, ErrNotFound
	}
	return cloneDecisionContext(context), nil
}

func (s *decisionMemoryStore) SaveDecisionConfirmation(_ context.Context, userID int64, reportID string, raw json.RawMessage, hash string) (DecisionContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.reports[reportID]
	if record == nil || record.UserID != userID {
		return DecisionContext{}, ErrNotFound
	}
	if record.ProtocolVersion != ProtocolDecisionV5 || record.Status != StatusPreviewReady || len(record.PreviewSnapshot) == 0 || record.Paid || record.PendingOrderNo != "" {
		return DecisionContext{}, ErrConflict
	}
	if existing, ok := s.contexts[reportID]; ok && existing.ConfirmationHash != "" {
		if existing.ConfirmationHash != hash || string(existing.ConfirmationJSON) != string(raw) {
			return DecisionContext{}, ErrConflict
		}
		return cloneDecisionContext(existing), nil
	}
	stored := DecisionContext{ConfirmationJSON: append(json.RawMessage(nil), raw...), ConfirmationHash: hash, Stage: DecisionStageConfirmed}
	s.contexts[reportID] = stored
	return cloneDecisionContext(stored), nil
}

func (s *decisionMemoryStore) SetDecisionStage(_ context.Context, reportID, leaseToken, stage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.reports[reportID]
	if record == nil || record.LeaseToken != leaseToken {
		return ErrConflict
	}
	stored := s.contexts[reportID]
	stored.Stage = stage
	s.contexts[reportID] = stored
	return nil
}

func cloneDecisionContext(source DecisionContext) DecisionContext {
	source.ConfirmationJSON = append(json.RawMessage(nil), source.ConfirmationJSON...)
	return source
}

func decisionTestService(t *testing.T, store Store, storage PrivateStorage) *Service {
	t.Helper()
	config := testConfig(3)
	config.DecisionEnabled = true
	service, err := NewService(config, store, nil, storage)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func makeDecisionPreviewReady(t *testing.T, store *decisionMemoryStore, reportID string) DecisionConfirmation {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	record := store.reports[reportID]
	var input DecisionInput
	if err := json.Unmarshal(record.InputJSON, &input); err != nil {
		t.Fatal(err)
	}
	preview, err := BuildDecisionPreview(record, input, PlanDecision(input))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	record.PreviewSnapshot = raw
	record.Status = StatusPreviewReady
	return DecisionConfirmation{DiagnosisID: preview.Diagnosis.ID, InputHash: record.InputHash, Choice: ChoiceUndecided, Priorities: input.Priorities}
}

func TestCreateDecisionPreviewUsesRawV5InputAndAsyncQueue(t *testing.T) {
	store := newDecisionMemoryStore()
	service := decisionTestService(t, store, nil)
	first, err := service.CreateDecisionPreview(context.Background(), 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateDecisionPreview(context.Background(), 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Status != StatusPreviewQueued || first.ProtocolVersion != ProtocolDecisionV5 {
		t.Fatalf("unexpected decision create: first=%#v second=%#v", first, second)
	}
	record := store.reports[first.ID]
	if record.ProtocolVersion != ProtocolDecisionV5 || record.Status != StatusPreviewQueued {
		t.Fatalf("stored report = %#v", record)
	}
	var raw map[string]any
	if err := json.Unmarshal(record.InputJSON, &raw); err != nil {
		t.Fatal(err)
	}
	if _, wrapped := raw["input"]; wrapped || raw["primary_goal"] != GoalSpace {
		t.Fatalf("V5 input was wrapped or changed: %s", record.InputJSON)
	}
}

func TestDecisionDisabledCannotSellFrozenUnpaidReport(t *testing.T) {
	store := newDecisionMemoryStore()
	svc := decisionTestService(t, store, nil)
	view, err := svc.CreateDecisionPreview(context.Background(), 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	confirmation := makeDecisionPreviewReady(t, store, view.ID)
	if _, err = svc.ConfirmDecision(context.Background(), 11, view.ID, confirmation); err != nil {
		t.Fatal(err)
	}
	svc.config.DecisionEnabled = false
	quote, err := svc.GetPaymentQuote(context.Background(), 11, view.ID)
	if err != nil || quote.CanPay {
		t.Fatalf("disabled decision worker still accepted new payments: %+v %v", quote, err)
	}
}

func TestDecisionConfirmationIsOwnedValidatedAndImmutable(t *testing.T) {
	store := newDecisionMemoryStore()
	service := decisionTestService(t, store, nil)
	created, err := service.CreateDecisionPreview(context.Background(), 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	confirmation := makeDecisionPreviewReady(t, store, created.ID)
	if _, err := service.ConfirmDecision(context.Background(), 12, created.ID, confirmation); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong owner error = %v", err)
	}
	stale := confirmation
	stale.InputHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := service.ConfirmDecision(context.Background(), 11, created.ID, stale); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("stale input hash error = %v", err)
	}
	confirmed, err := service.ConfirmDecision(context.Background(), 11, created.ID, confirmation)
	if err != nil || !confirmed.Confirmed || confirmed.NextAction != "pay" {
		t.Fatalf("confirmed view = %#v, err=%v", confirmed, err)
	}
	if _, err := service.ConfirmDecision(context.Background(), 11, created.ID, confirmation); err != nil {
		t.Fatalf("same confirmation was not idempotent: %v", err)
	}
	changed := confirmation
	changed.Choice = ChoiceOptionA
	if _, err := service.ConfirmDecision(context.Background(), 11, created.ID, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed confirmation error = %v", err)
	}
	if _, err := service.ReservePaymentOrder(context.Background(), 11, created.ID, "ORDER-V5-CONFIRM"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmDecision(context.Background(), 11, created.ID, confirmation); !errors.Is(err, ErrConflict) {
		t.Fatalf("confirmation changed after payment order reservation: %v", err)
	}
}

func TestDecisionPaymentRequiresConfirmationAndUsesSyntheticPack(t *testing.T) {
	store := newDecisionMemoryStore()
	service := decisionTestService(t, store, nil)
	created, err := service.CreateDecisionPreview(context.Background(), 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	confirmation := makeDecisionPreviewReady(t, store, created.ID)
	quote, err := service.GetPaymentQuote(context.Background(), 11, created.ID)
	if err != nil || quote.CanPay || quote.BlockReason != "confirmation_required" {
		t.Fatalf("unconfirmed quote = %#v err=%v", quote, err)
	}
	if _, err := service.ConfirmDecision(context.Background(), 11, created.ID, confirmation); err != nil {
		t.Fatal(err)
	}
	quote, err = service.GetPaymentQuote(context.Background(), 11, created.ID)
	if err != nil || !quote.CanPay || quote.PackID == "" || quote.PackVersion != DecisionTemplateVersion {
		t.Fatalf("confirmed quote = %#v err=%v", quote, err)
	}
}

func TestDecisionContextStorageFailureSurfaces(t *testing.T) {
	store := newDecisionMemoryStore()
	service := decisionTestService(t, store, nil)
	created, err := service.CreateDecisionPreview(context.Background(), 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	store.readErr = errStoreUnavailable
	if _, err := service.Get(context.Background(), 11, created.ID); !errors.Is(err, errStoreUnavailable) {
		t.Fatalf("context read failure was hidden: %v", err)
	}
}

func TestDecisionConfirmationSemanticJSONSurvivesMySQLNormalization(t *testing.T) {
	confirmation := DecisionConfirmation{DiagnosisID: "diagnosis-1", InputHash: "input-1", Choice: ChoiceUndecided, Priorities: []string{"父母舒适", "投入可控"}}
	canonical, hash, err := marshalDecisionConfirmation(confirmation)
	if err != nil {
		t.Fatal(err)
	}
	mysqlJSON := json.RawMessage(`{"priorities":["父母舒适","投入可控"],"input_hash":"input-1","diagnosis_id":"diagnosis-1","choice":"undecided"}`)
	if !jsonEqual(mysqlJSON, canonical) || !validSHA256(hash) {
		t.Fatalf("semantically identical MySQL JSON rejected: canonical=%s mysql=%s", canonical, mysqlJSON)
	}
	changed := json.RawMessage(`{"priorities":["父母舒适"],"input_hash":"input-1","diagnosis_id":"diagnosis-1","choice":"option_a"}`)
	if jsonEqual(changed, canonical) {
		t.Fatal("changed confirmation accepted as idempotent")
	}
}

func TestVerifiedPaymentPersistsBeforeDecisionContextHydration(t *testing.T) {
	ctx := context.Background()
	store := newDecisionMemoryStore()
	service := decisionTestService(t, store, nil)
	created, err := service.CreateDecisionPreview(ctx, 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	confirmation := makeDecisionPreviewReady(t, store, created.ID)
	if _, err := service.ConfirmDecision(ctx, 11, created.ID, confirmation); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReservePaymentOrder(ctx, 11, created.ID, "ORDER-V5-HYDRATE"); err != nil {
		t.Fatal(err)
	}
	store.readErr = errStoreUnavailable
	if _, err := service.AuthorizeVerifiedPayment(ctx, 11, created.ID, "ORDER-V5-HYDRATE"); !errors.Is(err, errStoreUnavailable) {
		t.Fatalf("post-payment hydration error = %v", err)
	}
	record := store.reports[created.ID]
	if !record.Paid || record.Status != StatusFullQueued || record.PaymentRef != "ORDER-V5-HYDRATE" {
		t.Fatalf("verified payment was not persisted before hydration: %#v", record)
	}
}

func TestDecisionPaidReadyDownloadDoesNotRequireEnterpriseBinding(t *testing.T) {
	store := newDecisionMemoryStore()
	storage := newMemoryStorage()
	service := decisionTestService(t, store, storage)
	created, err := service.CreateDecisionPreview(context.Background(), 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	record := store.reports[created.ID]
	record.Paid, record.Bound, record.Status = true, false, StatusReady
	record.FullObjectKey = "reports/decision.pdf"
	store.mu.Unlock()
	storage.objects[record.FullObjectKey] = []byte("%PDF-test")
	if _, err := service.GetDownloadURL(context.Background(), 11, created.ID, ModeFull); err != nil {
		t.Fatalf("unbound V5 download failed: %v", err)
	}
}

func TestDecisionPreviewPDFIsNeverGeneratedSynchronously(t *testing.T) {
	store := newDecisionMemoryStore()
	service := decisionTestService(t, store, newMemoryStorage())
	created, err := service.CreateDecisionPreview(context.Background(), 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	makeDecisionPreviewReady(t, store, created.ID)
	if err := service.EnsurePreviewPDF(context.Background(), 11, created.ID, fakeRenderer{}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("V5 preview PDF backfilled synchronously: %v", err)
	}
}

func TestDecisionSafeStageFollowsAuthoritativeReportStatus(t *testing.T) {
	tests := []struct {
		status    string
		current   string
		confirmed bool
		want      string
	}{
		{status: StatusPreviewQueued, current: "review", want: DecisionStagePreviewPending},
		{status: StatusPreviewGenerating, current: "diagnosis", want: "diagnosis"},
		{status: StatusPreviewReady, current: "review", want: "awaiting_confirmation"},
		{status: StatusPreviewReady, current: "review", confirmed: true, want: DecisionStageConfirmed},
		{status: StatusFullQueued, current: DecisionStageConfirmed, confirmed: true, want: "full_pending"},
		{status: StatusFullGenerating, current: "analysis", confirmed: true, want: "analysis"},
		{status: StatusReady, current: "saving", confirmed: true, want: StatusReady},
		{status: StatusFailed, current: "analysis", confirmed: true, want: StatusFailed},
	}
	for _, test := range tests {
		record := &Report{ProtocolVersion: ProtocolDecisionV5, Status: test.status, CurrentStage: test.current}
		if got := reportStage(record, test.confirmed); got != test.want {
			t.Fatalf("status=%s current=%s confirmed=%t stage=%s want=%s", test.status, test.current, test.confirmed, got, test.want)
		}
	}
}

func TestLegacyPaymentCanBeBlockedWithoutDisablingDecisionPayment(t *testing.T) {
	store := newMemoryStore()
	config := testConfig(3)
	config.LegacyPaymentBlocked = true
	service, err := NewService(config, store, fixedCatalog{pack: approvedPack(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreatePreview(context.Background(), 11, validInput())
	if err != nil {
		t.Fatal(err)
	}
	record := store.reports[created.ID]
	snapshot, _, err := NewGenerator(nil).BuildPreview(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	record.Status, record.PreviewSnapshot = StatusPreviewReady, snapshot
	quote, err := service.GetPaymentQuote(context.Background(), 11, created.ID)
	if err != nil || quote.CanPay || quote.BlockReason != "delivery_unavailable" {
		t.Fatalf("legacy quote = %#v, %v", quote, err)
	}

	decisionStore := newDecisionMemoryStore()
	decisionService := decisionTestService(t, decisionStore, nil)
	decisionCreated, err := decisionService.CreateDecisionPreview(context.Background(), 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	confirmation := makeDecisionPreviewReady(t, decisionStore, decisionCreated.ID)
	if _, err := decisionService.ConfirmDecision(context.Background(), 11, decisionCreated.ID, confirmation); err != nil {
		t.Fatal(err)
	}
	decisionService.config.LegacyPaymentBlocked = true
	decisionQuote, err := decisionService.GetPaymentQuote(context.Background(), 11, decisionCreated.ID)
	if err != nil || !decisionQuote.CanPay {
		t.Fatalf("legacy delivery gate blocked V5 quote = %#v, %v", decisionQuote, err)
	}
}
