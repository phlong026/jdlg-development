package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"service/component"
)

// Database/model/storage are doubles. Assets use the production image service;
// PDF output uses the actual Go subprocess adapter and Python renderer.
func TestHousePipelineWithRealPDF(t *testing.T) {
	if os.Getenv("REPORT_HOUSE_REAL_PDF_TEST") != "1" {
		t.Skip("set REPORT_HOUSE_REAL_PDF_TEST=1 and PDF Python/font paths")
	}
	for _, floors := range []int{2, 3} {
		t.Run(fmt.Sprintf("%dfloors", floors), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			script, err := filepath.Abs("../reportpdf/render.py")
			if err != nil {
				t.Fatal(err)
			}
			renderer, err := component.NewReportPDFRenderer(component.ReportPDFConfig{PythonPath: os.Getenv("REPORT_PDF_PYTHON"), FontPath: os.Getenv("REPORT_PDF_FONT"), ScriptPath: script, AssetRoot: root, Timeout: 90 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			cfg := decisionWorkerConfig(2)
			cfg.HouseEnabled = true
			cfg.JobTimeout = 5 * time.Minute
			cfg.LeaseDuration = 6 * time.Minute
			store := newDecisionMemoryStore()
			storage := newMemoryStorage()
			svc, err := NewService(cfg, store, nil, storage)
			if err != nil {
				t.Fatal(err)
			}
			input := houseDecisionInput(floors, floors+2, FlexibleUseNo)
			created, err := svc.CreateHousePreview(ctx, 7, input)
			if err != nil {
				t.Fatal(err)
			}
			_, _, provider := imageFixture(t)
			imageStore := newMemoryHouseImages()
			productionImages, err := NewHouseImageService(imageStore, provider, root)
			if err != nil {
				t.Fatal(err)
			}
			images := &leaseTrackingHouseImages{HouseImageService: productionImages, store: imageStore}
			agent, err := NewHouseOrchestrator(&houseAgentFixture{}, &decisionCheckpoints{}, images)
			if err != nil {
				t.Fatal(err)
			}
			worker, err := NewWorker(cfg, store, nil, nil, renderer, storage, WithAgentRunner(agent))
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
			confirmation := DecisionConfirmation{DiagnosisID: preview.Diagnosis.ID, InputHash: created.InputHash, Choice: ChoiceConfirm, Priorities: input.Priorities}
			if _, err := svc.ConfirmDecision(ctx, 7, created.ID, confirmation); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ReservePaymentOrder(ctx, 7, created.ID, "HOUSE-PDF"); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.AuthorizeVerifiedPayment(ctx, 7, created.ID, "HOUSE-PDF"); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 20; i++ {
				if _, err := worker.RunOnce(ctx); err != nil {
					t.Fatalf("lease %d: %v", i, err)
				}
				if store.reports[created.ID].Status == StatusReady {
					break
				}
			}
			done := store.reports[created.ID]
			if done.Status != StatusReady || provider.calls != 2*(floors+2) || len(storage.objects) != 1 {
				t.Fatalf("status=%s images=%d objects=%d", done.Status, provider.calls, len(storage.objects))
			}
			for _, pdf := range storage.objects {
				if len(pdf) < 100000 || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
					t.Fatal("real PDF missing")
				}
			}
			if _, err := svc.GetDownloadURL(ctx, 7, created.ID, ModeFull); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.GetDownloadURL(ctx, 8, created.ID, ModeFull); err == nil {
				t.Fatal("cross-owner download")
			}
		})
	}
}

type leaseTrackingHouseImages struct {
	HouseImageService
	store *memoryHouseImages
}

func (s *leaseTrackingHouseImages) Ensure(ctx context.Context, r *Report, task HouseImageTask) (HouseImageAsset, bool, error) {
	s.store.mu.Lock()
	s.store.lease = r.LeaseToken
	s.store.mu.Unlock()
	return s.HouseImageService.Ensure(ctx, r, task)
}
