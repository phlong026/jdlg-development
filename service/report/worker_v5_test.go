package report

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type decisionWorkerAgent struct {
	modes []string
	packs []*ReviewedPack
}

func (a *decisionWorkerAgent) Run(_ context.Context, record *Report, mode string, pack *ReviewedPack) (json.RawMessage, string, error) {
	a.modes = append(a.modes, mode)
	a.packs = append(a.packs, pack)
	var input DecisionInput
	if json.Unmarshal(record.InputJSON, &input) != nil {
		return nil, "", ErrInvalidInput
	}
	brief := PlanDecision(input)
	if mode == ModePreview {
		snapshot, err := BuildDecisionPreview(record, input, brief)
		if err != nil {
			return nil, "", err
		}
		raw, err := json.Marshal(snapshot)
		return raw, GenerationMultiAgent, err
	}
	if len(record.ConfirmationJSON) == 0 || record.ConfirmationHash == "" {
		return nil, "", ErrConflict
	}
	var confirmation DecisionConfirmation
	if json.Unmarshal(record.ConfirmationJSON, &confirmation) != nil {
		return nil, "", ErrConflict
	}
	analysis := []ContentBlock{{Title: "生活影响", Body: "根据家庭使用方式比较不同安排的成立条件。"}, {Title: "待核实事项", Body: "让设计师核对尚未确认的边界和资料。"}}
	budget := []ContentBlock{{Title: "询价口径", Body: "将已包含与待确认项分开询价。"}}
	snapshot, err := BuildDecisionFull(record, input, confirmation, brief, analysis, budget)
	if err != nil {
		return nil, "", err
	}
	raw, err := json.Marshal(snapshot)
	return raw, GenerationMultiAgent, err
}

type countingRenderer struct{ calls int }

func (r *countingRenderer) Render(_ context.Context, _ []byte, _ string) ([]byte, error) {
	r.calls++
	return []byte("%PDF-1.7\ntest"), nil
}

type failingDecisionStageStore struct{ *decisionMemoryStore }

func (s *failingDecisionStageStore) SetDecisionStage(context.Context, string, string, string) error {
	return ErrConflict
}

type recordingDecisionStageStore struct {
	*decisionMemoryStore
	stages []string
}

func (s *recordingDecisionStageStore) SetDecisionStage(ctx context.Context, reportID, leaseToken, stage string) error {
	s.stages = append(s.stages, stage)
	return s.decisionMemoryStore.SetDecisionStage(ctx, reportID, leaseToken, stage)
}

func decisionWorkerConfig(attempts int) Config {
	config := testConfig(attempts)
	config.DecisionEnabled = true
	config.MultiAgentEnabled = true
	return config
}

func TestDecisionWorkerRequiresAgentAndDecisionContextStore(t *testing.T) {
	config := decisionWorkerConfig(1)
	if _, err := NewWorker(config, newDecisionMemoryStore(), nil, nil, fakeRenderer{}, newMemoryStorage()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("V5 worker accepted without agent: %v", err)
	}
	if _, err := NewWorker(config, newMemoryStore(), nil, nil, fakeRenderer{}, newMemoryStorage(), WithAgentRunner(&decisionWorkerAgent{})); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("V5 worker accepted store without decision context: %v", err)
	}
}

func TestDecisionWorkerPreviewPersistsJSONWithoutRenderingPDF(t *testing.T) {
	ctx := context.Background()
	store := newDecisionMemoryStore()
	service := decisionTestService(t, store, nil)
	created, err := service.CreateDecisionPreview(ctx, 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	agents := &decisionWorkerAgent{}
	renderer := &countingRenderer{}
	storage := newMemoryStorage()
	worker, err := NewWorker(decisionWorkerConfig(1), store, nil, nil, renderer, storage, WithAgentRunner(agents))
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.RunOnce(ctx); err != nil || !worked {
		t.Fatalf("preview worker = %t, %v", worked, err)
	}
	record := store.reports[created.ID]
	if record.Status != StatusPreviewReady || len(record.PreviewSnapshot) == 0 || record.PreviewObjectKey != "" {
		t.Fatalf("preview result = %#v", record)
	}
	if renderer.calls != 0 || len(storage.objects) != 0 {
		t.Fatalf("free preview rendered or uploaded PDF: renders=%d objects=%d", renderer.calls, len(storage.objects))
	}
	if len(agents.modes) != 1 || agents.modes[0] != ModePreview || agents.packs[0] != nil {
		t.Fatalf("preview dispatch = %#v %#v", agents.modes, agents.packs)
	}
}

func TestDecisionWorkerFullLoadsConfirmationAndSkipsPackCatalog(t *testing.T) {
	ctx := context.Background()
	store := &recordingDecisionStageStore{decisionMemoryStore: newDecisionMemoryStore()}
	storage := newMemoryStorage()
	service := decisionTestService(t, store, storage)
	created, err := service.CreateDecisionPreview(ctx, 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	agents := &decisionWorkerAgent{}
	renderer := &countingRenderer{}
	worker, err := NewWorker(decisionWorkerConfig(1), store, nil, nil, renderer, storage, WithAgentRunner(agents))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var preview DecisionSnapshot
	if err := json.Unmarshal(store.reports[created.ID].PreviewSnapshot, &preview); err != nil {
		t.Fatal(err)
	}
	confirmation := DecisionConfirmation{DiagnosisID: preview.Diagnosis.ID, InputHash: created.InputHash, Choice: ChoiceUndecided, Priorities: seasonalSpaceInput().Priorities}
	if _, err := service.ConfirmDecision(ctx, 11, created.ID, confirmation); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReservePaymentOrder(ctx, 11, created.ID, "ORDER-V5-WORKER"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeVerifiedPayment(ctx, 11, created.ID, "ORDER-V5-WORKER"); err != nil {
		t.Fatal(err)
	}
	if store.reports[created.ID].Bound {
		t.Fatal("fixture unexpectedly bound enterprise contact")
	}
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	record := store.reports[created.ID]
	if record.Status != StatusReady || record.FullObjectKey == "" || renderer.calls != 1 || len(storage.objects) != 1 {
		t.Fatalf("full result = %#v renders=%d objects=%d", record, renderer.calls, len(storage.objects))
	}
	if len(agents.modes) != 2 || agents.modes[1] != ModeFull || agents.packs[1] != nil {
		t.Fatalf("full dispatch used reviewed pack: %#v %#v", agents.modes, agents.packs)
	}
	if len(store.stages) != 2 || store.stages[0] != "rendering" || store.stages[1] != "saving" {
		t.Fatalf("fenced stages = %v", store.stages)
	}
	if stage := store.contexts[created.ID].Stage; stage != "saving" {
		t.Fatalf("final fenced stage = %q", stage)
	}
}

func TestDecisionWorkerRejectsMissingConfirmationBeforeAgent(t *testing.T) {
	ctx := context.Background()
	store := newDecisionMemoryStore()
	service := decisionTestService(t, store, nil)
	created, err := service.CreateDecisionPreview(ctx, 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	makeDecisionPreviewReady(t, store, created.ID)
	store.mu.Lock()
	record := store.reports[created.ID]
	record.Paid, record.Status = true, StatusFullQueued
	record.PackID, record.PackVersion = "decision-v5-space", DecisionTemplateVersion
	store.mu.Unlock()
	agents := &decisionWorkerAgent{}
	worker, err := NewWorker(decisionWorkerConfig(1), store, nil, nil, fakeRenderer{}, newMemoryStorage(), WithAgentRunner(agents))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(ctx); err == nil {
		t.Fatal("missing confirmation was accepted")
	}
	if len(agents.modes) != 0 || store.reports[created.ID].Status != StatusFailed {
		t.Fatalf("agent ran before confirmation gate: modes=%v status=%s", agents.modes, store.reports[created.ID].Status)
	}
}

func TestDecisionWorkerStopsBeforeRenderWhenStageLeaseFails(t *testing.T) {
	ctx := context.Background()
	base := newDecisionMemoryStore()
	store := &failingDecisionStageStore{decisionMemoryStore: base}
	service := decisionTestService(t, store, nil)
	created, err := service.CreateDecisionPreview(ctx, 11, seasonalSpaceInput())
	if err != nil {
		t.Fatal(err)
	}
	confirmation := makeDecisionPreviewReady(t, base, created.ID)
	raw, hash, err := marshalDecisionConfirmation(confirmation)
	if err != nil {
		t.Fatal(err)
	}
	base.contexts[created.ID] = DecisionContext{ConfirmationJSON: raw, ConfirmationHash: hash, Stage: DecisionStageConfirmed}
	base.mu.Lock()
	record := base.reports[created.ID]
	record.Paid, record.Status = true, StatusFullQueued
	record.PackID, record.PackVersion = "decision-v5-space", DecisionTemplateVersion
	base.mu.Unlock()
	renderer := &countingRenderer{}
	storage := newMemoryStorage()
	worker, err := NewWorker(decisionWorkerConfig(1), store, nil, nil, renderer, storage, WithAgentRunner(&decisionWorkerAgent{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(ctx); err == nil {
		t.Fatal("lease-fenced stage failure was hidden")
	}
	if renderer.calls != 0 || len(storage.objects) != 0 || base.reports[created.ID].Status != StatusFailed {
		t.Fatalf("output published after stage failure: renders=%d objects=%d status=%s", renderer.calls, len(storage.objects), base.reports[created.ID].Status)
	}
}

func TestDecisionWorkerRejectsChangedFrozenConfirmation(t *testing.T) {
	input := seasonalSpaceInput()
	record := testReportForDecision(t, input)
	record.Paid = true
	brief := PlanDecision(input)
	confirmation := DecisionConfirmation{DiagnosisID: brief.ID, InputHash: record.InputHash, Choice: ChoiceUndecided, Priorities: input.Priorities}
	confirmation, err := NormalizeDecisionConfirmation(input, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	record.ConfirmationJSON, record.ConfirmationHash, err = marshalDecisionConfirmation(confirmation)
	if err != nil {
		t.Fatal(err)
	}
	analysis := []ContentBlock{{Title: "生活影响", Body: "根据家庭使用方式比较不同安排的成立条件。"}, {Title: "待核实事项", Body: "让设计师核对尚未确认的边界和资料。"}}
	budget := []ContentBlock{{Title: "询价口径", Body: "将已包含与待确认项分开询价。"}}
	snapshot, err := BuildDecisionFull(record, input, confirmation, brief, analysis, budget)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Confirmation.Choice = ChoiceOptionA
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDecisionWorkerSnapshot(record, raw, ModeFull); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed frozen confirmation accepted: %v", err)
	}
}
