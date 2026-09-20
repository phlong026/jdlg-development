package report

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type workerAgentFixture struct {
	modes []string
	err   error
}

func (a *workerAgentFixture) Run(ctx context.Context, record *Report, mode string, pack *ReviewedPack) (json.RawMessage, string, error) {
	a.modes = append(a.modes, mode)
	if a.err != nil {
		return nil, "", a.err
	}
	g := NewGenerator(nil)
	if mode == ModePreview {
		return g.BuildPreview(ctx, record)
	}
	if pack == nil {
		return nil, "", ErrPackUnavailable
	}
	snapshot, err := g.BuildFull(record.PreviewSnapshot, *pack)
	return snapshot, "multi_agent", err
}

func TestMultiAgentWorkerRequiresOrchestrator(t *testing.T) {
	cfg := testConfig(3)
	cfg.MultiAgentEnabled = true
	_, err := NewWorker(cfg, newMemoryStore(), NewGenerator(nil), fixedCatalog{pack: approvedPack(t)}, nil, nil)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing orchestrator allowed: %v", err)
	}
}

func TestWorkerDispatchesPreviewAndPaidPDFToAgents(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	pack := approvedPack(t)
	catalog := fixedCatalog{pack: pack}
	service := testService(t, store, catalog)
	view, err := service.CreatePreview(ctx, 11, validInput())
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(3)
	cfg.MultiAgentEnabled = true
	agents := &workerAgentFixture{}
	storage := newMemoryStorage()
	worker, err := NewWorker(cfg, store, NewGenerator(nil), catalog, fakeRenderer{}, storage, WithAgentRunner(agents))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	r := store.reports[view.ID]
	preview := append([]byte(nil), r.PreviewSnapshot...)
	r.Paid = true
	r.Status = StatusFullQueued
	r.PackID, r.PackVersion = pack.ID, pack.Version
	r.AttemptCount = 0
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(agents.modes) != 2 || agents.modes[0] != ModePreview || agents.modes[1] != ModeFull {
		t.Fatalf("agent dispatch: %v", agents.modes)
	}
	if r.Status != StatusReady || r.FullObjectKey == "" || len(storage.objects) != 2 {
		t.Fatalf("missing uploaded PDF: %#v", r)
	}
	if string(r.PreviewSnapshot) != string(preview) {
		t.Fatal("paid generation mutated preview")
	}
	public, err := service.Get(ctx, 11, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(public.PreviewSnapshot, &snapshot); err != nil || len(snapshot.Pages) != 4 {
		t.Fatalf("mini-program must only receive preview: %v", err)
	}
}

func TestWorkerNeverPublishesRejectedAgentOutput(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	catalog := fixedCatalog{pack: approvedPack(t)}
	service := testService(t, store, catalog)
	view, err := service.CreatePreview(ctx, 11, validInput())
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(1)
	cfg.MultiAgentEnabled = true
	storage := newMemoryStorage()
	worker, err := NewWorker(cfg, store, NewGenerator(nil), catalog, fakeRenderer{}, storage, WithAgentRunner(&workerAgentFixture{err: errors.New("review rejected")}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(ctx); err == nil {
		t.Fatal("rejection hidden by fallback")
	}
	if store.reports[view.ID].Status != StatusFailed || len(storage.objects) != 0 {
		t.Fatal("unreviewed output published")
	}
}

func TestAgentConfigDefaultsAndRollback(t *testing.T) {
	for _, key := range []string{"REPORT_ENABLED", "REPORT_PAYMENT_ENABLED", "REPORT_MULTI_AGENT_ENABLED", "REPORT_PROTOCOL_VERSION", "REPORT_JOB_TIMEOUT", "REPORT_LEASE_DURATION", "REPORT_PRICE_FEN", "REPORT_MAX_ATTEMPTS", "REPORT_DOWNLOAD_TTL"} {
		t.Setenv(key, "")
	}
	cfg, err := LoadConfig()
	if err != nil || !cfg.MultiAgentEnabled || cfg.ProtocolVersion != "report-agents-v1" || cfg.JobTimeout.Seconds() != 300 {
		t.Fatalf("multiagent defaults: %#v %v", cfg, err)
	}
	t.Setenv("REPORT_MULTI_AGENT_ENABLED", "false")
	cfg, err = LoadConfig()
	if err != nil || cfg.MultiAgentEnabled || cfg.ProtocolVersion != "report-service-v1" || cfg.JobTimeout.Seconds() != 120 {
		t.Fatalf("explicit legacy rollback: %#v %v", cfg, err)
	}
	t.Setenv("REPORT_MULTI_AGENT_ENABLED", "invalid")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("invalid rollout flag accepted")
	}
}
