package report

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"service/component"
)

// Providers and database are deterministic doubles; PDF generation uses the
// actual Go subprocess adapter and Python renderer, including its validation.
func TestDecisionPipelineWithRealPDF(t *testing.T) {
	if os.Getenv("REPORT_V5_REAL_PDF_TEST") != "1" {
		t.Skip("set REPORT_V5_REAL_PDF_TEST=1 with REPORT_PDF_PYTHON and REPORT_PDF_FONT")
	}
	script, err := filepath.Abs("../reportpdf/render.py")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := component.NewReportPDFRenderer(component.ReportPDFConfig{PythonPath: os.Getenv("REPORT_PDF_PYTHON"), FontPath: os.Getenv("REPORT_PDF_FONT"), ScriptPath: script, Timeout: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store := newDecisionMemoryStore()
	storage := newMemoryStorage()
	svc := decisionTestService(t, store, storage)
	client := &decisionClientFixture{}
	orchestrator, err := NewReportOrchestrator(client, &decisionCheckpoints{})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(decisionWorkerConfig(2), store, nil, nil, renderer, storage, WithAgentRunner(orchestrator))
	if err != nil {
		t.Fatal(err)
	}
	input := seasonalSpaceInput()
	created, err := svc.CreateDecisionPreview(ctx, 11, input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusPreviewQueued || len(client.calls) != 0 {
		t.Fatal("submission performed synchronous generation")
	}
	if _, err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(storage.objects) != 0 {
		t.Fatal("free phase rendered a PDF")
	}
	view, err := svc.Get(ctx, 11, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var preview DecisionSnapshot
	if err = json.Unmarshal(view.PreviewSnapshot, &preview); err != nil {
		t.Fatal(err)
	}
	confirmation := DecisionConfirmation{DiagnosisID: preview.Diagnosis.ID, InputHash: view.InputHash, Choice: ChoiceOptionA, Priorities: input.Priorities}
	if _, err = svc.ConfirmDecision(ctx, 11, created.ID, confirmation); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ReservePaymentOrder(ctx, 11, created.ID, "ORDER-INTEGRATION"); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AuthorizeVerifiedPayment(ctx, 11, created.ID, "ORDER-INTEGRATION"); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Get(ctx, 11, created.ID)
	if err != nil || view.Status != StatusReady || view.Stage != StatusReady || view.Bound || view.NextAction != "download" {
		t.Fatalf("unexpected completion: %+v %v", view, err)
	}
	if _, err = svc.GetDownloadURL(ctx, 11, created.ID, ModeFull); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.GetDownloadURL(ctx, 12, created.ID, ModeFull); err == nil {
		t.Fatal("another owner could download")
	}
	if len(storage.objects) != 1 {
		t.Fatalf("objects=%d", len(storage.objects))
	}
	for _, pdf := range storage.objects {
		if len(pdf) < 100_000 || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			t.Fatal("real PDF missing")
		}
	}
	for role, want := range map[string]int{"diagnosis": 1, "decision": 1, "budget": 1, "editor": 1, "reviewer": 2} {
		if client.calls[role] != want {
			t.Fatalf("role %s calls=%d want=%d", role, client.calls[role], want)
		}
	}
}
