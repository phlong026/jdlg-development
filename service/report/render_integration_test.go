package report

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"service/component"
)

// Opt-in because CI must provision ReportLab, pypdf and a licensed CJK font.
// This test uses synthetic reviewed-pack fixtures, never real customer data.
func TestActualGeneratorAndPythonRenderer(t *testing.T) {
	python, font := os.Getenv("REPORT_PDF_PYTHON"), os.Getenv("REPORT_PDF_FONT")
	if python == "" || font == "" {
		t.Skip("set REPORT_PDF_PYTHON and REPORT_PDF_FONT for actual PDF integration")
	}
	script, err := filepath.Abs("../reportpdf/render.py")
	if err != nil {
		t.Fatal(err)
	}
	assetRoot := t.TempDir()
	pack := approvedPack(t)
	for id, path := range pack.Assets {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(assetRoot, id+".png")
		if err := os.WriteFile(target, b, 0600); err != nil {
			t.Fatal(err)
		}
		pack.Assets[id] = target
	}
	renderer, err := component.NewReportPDFRenderer(component.ReportPDFConfig{PythonPath: python, ScriptPath: script, FontPath: font, AssetRoot: assetRoot, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	generator := NewGenerator(nil)
	input := validInput()
	canonical, hash, err := MarshalInput(input)
	if err != nil {
		t.Fatal(err)
	}
	record := &Report{ID: "integration-fixture-not-customer", InputJSON: canonical, InputHash: hash, ProtocolVersion: "report-service-v1"}
	preview, _, err := generator.BuildPreview(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	full, err := generator.BuildFull(preview, pack)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{}
	for _, item := range []struct {
		mode string
		data []byte
	}{{ModePreview, preview}, {ModeFull, full}} {
		b, err := renderer.Render(context.Background(), item.data, item.mode)
		if err != nil {
			// Only synthetic fixture data may be printed during a failed layout test.
			file := filepath.Join(assetRoot, item.mode+".json")
			_ = os.WriteFile(file, item.data, 0600)
			cmd := exec.Command(python, "-c", `import sys,json;sys.path.insert(0,sys.argv[1]);from render import render_report;render_report(json.load(open(sys.argv[2])),sys.argv[3],sys.argv[4],sys.argv[5],sys.argv[6])`, filepath.Dir(script), file, filepath.Join(assetRoot, "debug.pdf"), item.mode, font, assetRoot)
			debug, _ := cmd.CombinedOutput()
			t.Fatalf("actual %s PDF failed: %v\n%s", item.mode, err, debug)
		}
		out := filepath.Join(assetRoot, item.mode+".pdf")
		if err := os.WriteFile(out, b, 0600); err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, out)
		if reviewDir := os.Getenv("REPORT_REVIEW_DIR"); reviewDir != "" {
			if !filepath.IsAbs(reviewDir) {
				t.Fatal("review dir must be absolute")
			}
			if err := os.MkdirAll(reviewDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(reviewDir, "联调夹具-"+item.mode+".pdf"), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := exec.Command(python, "-c", `import sys;from pypdf import PdfReader;a,b=map(PdfReader,sys.argv[1:]);assert len(a.pages)==4;assert len(b.pages)==14;assert all(a.pages[i].get_contents().get_data()==b.pages[i].get_contents().get_data() for i in range(4));assert all(abs(float(p.mediabox.width)-595.28)<1 for p in b.pages)`, outputs[0], outputs[1])
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("PDF page contract: %v %s", err, output)
	}
	// Long allowed text must still fit the native/PDF shared contract.
	input.Household.Residents = strings.Repeat("家", 80)
	input.Preference.Goal = strings.Repeat("住", 80)
	input.Site.Region = strings.Repeat("区", 32)
	canonical, hash, err = MarshalInput(input)
	if err != nil {
		t.Fatal(err)
	}
	record.InputJSON = canonical
	record.InputHash = hash
	preview, _, err = generator.BuildPreview(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := renderer.Render(context.Background(), preview, ModePreview); err != nil {
		var snapshot Snapshot
		_ = json.Unmarshal(preview, &snapshot)
		t.Fatalf("maximum input breaks fixed slots: %v", err)
	}
	for index := range pack.Schemes {
		scheme := &pack.Schemes[index]
		scheme.Name = strings.Repeat("名", 8)
		scheme.Tagline = strings.Repeat("标", 36)
		scheme.Tradeoff = strings.Repeat("舍", 30)
		scheme.Gain = strings.Repeat("得", 30)
		scheme.Facade = strings.Repeat("面", 70)
		scheme.FacadeCaution = strings.Repeat("慎", 70)
		scheme.CostSensitive = []string{strings.Repeat("材", 12), strings.Repeat("价", 12)}
		for floorIndex := range scheme.Plans {
			scheme.Plans[floorIndex].Name = strings.Repeat("层", 12)
			scheme.Plans[floorIndex].Summary = strings.Repeat("房", 50)
		}
	}
	for index := range pack.BudgetTemplate {
		pack.BudgetTemplate[index].Name = strings.Repeat("项", 12)
	}
	full, err = generator.BuildFull(preview, pack)
	if err != nil {
		t.Fatalf("maximum reviewed copy rejected before render: %v", err)
	}
	if _, err := renderer.Render(context.Background(), full, ModeFull); err != nil {
		t.Fatalf("maximum reviewed copy breaks full PDF slots: %v", err)
	}
}
