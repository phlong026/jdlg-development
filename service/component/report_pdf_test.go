package component

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type reportPDFRunnerFunc func(context.Context, string, ...string) error

func (f reportPDFRunnerFunc) Run(ctx context.Context, executable string, args ...string) error {
	return f(ctx, executable, args...)
}

func TestReportPDFRendererRendersIsolatedPDF(t *testing.T) {
	config := testReportPDFConfig(t)
	wantSnapshot := []byte(`{"report_id":"RPT-001"}`)

	renderer, err := newReportPDFRenderer(config, reportPDFRunnerFunc(func(_ context.Context, executable string, args ...string) error {
		if executable != config.PythonPath {
			t.Fatalf("executable = %q, want %q", executable, config.PythonPath)
		}
		if len(args) != 9 {
			t.Fatalf("args = %#v, want 9 arguments", args)
		}
		if args[0] != config.ScriptPath || args[1] != "--input" || args[3] != "--output" || args[5] != "--mode" || args[6] != ReportPDFModePreview || args[7] != "--font" || args[8] != config.FontPath {
			t.Fatalf("unexpected renderer arguments: %#v", args)
		}

		input, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		if !bytes.Equal(input, wantSnapshot) {
			t.Fatalf("input snapshot = %q, want %q", input, wantSnapshot)
		}
		assertFileMode(t, args[2], 0o600)
		assertFileMode(t, args[4], 0o600)
		return os.WriteFile(args[4], []byte("%PDF-1.7\nreport"), 0o600)
	}))
	if err != nil {
		t.Fatalf("newReportPDFRenderer() error = %v", err)
	}

	got, err := renderer.Render(context.Background(), wantSnapshot, ReportPDFModePreview)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if string(got) != "%PDF-1.7\nreport" {
		t.Fatalf("Render() = %q", got)
	}
}

func TestReportPDFRendererClassifiesProcessFailureWithoutLeakingOutput(t *testing.T) {
	config := testReportPDFConfig(t)
	renderer, err := newReportPDFRenderer(config, reportPDFRunnerFunc(func(context.Context, string, ...string) error {
		return errors.New("sensitive renderer output")
	}))
	if err != nil {
		t.Fatalf("newReportPDFRenderer() error = %v", err)
	}

	_, err = renderer.Render(context.Background(), []byte(`{"private":"family data"}`), ReportPDFModeFull)
	if !errors.Is(err, ErrReportPDFProcess) {
		t.Fatalf("Render() error = %v, want ErrReportPDFProcess", err)
	}
	if strings.Contains(err.Error(), "sensitive renderer output") || strings.Contains(err.Error(), "family data") {
		t.Fatalf("Render() leaked private data: %v", err)
	}
}

func TestReportPDFRendererTimesOut(t *testing.T) {
	config := testReportPDFConfig(t)
	config.Timeout = 20 * time.Millisecond
	renderer, err := newReportPDFRenderer(config, reportPDFRunnerFunc(func(ctx context.Context, _ string, _ ...string) error {
		<-ctx.Done()
		return ctx.Err()
	}))
	if err != nil {
		t.Fatalf("newReportPDFRenderer() error = %v", err)
	}

	_, err = renderer.Render(context.Background(), []byte(`{"report_id":"RPT-001"}`), ReportPDFModeFull)
	if !errors.Is(err, ErrReportPDFTimeout) {
		t.Fatalf("Render() error = %v, want ErrReportPDFTimeout", err)
	}
}

func TestReportPDFRendererRejectsInvalidPDF(t *testing.T) {
	config := testReportPDFConfig(t)
	renderer, err := newReportPDFRenderer(config, reportPDFRunnerFunc(func(_ context.Context, _ string, args ...string) error {
		return os.WriteFile(args[4], []byte("not a pdf"), 0o600)
	}))
	if err != nil {
		t.Fatalf("newReportPDFRenderer() error = %v", err)
	}

	_, err = renderer.Render(context.Background(), []byte(`{"report_id":"RPT-001"}`), ReportPDFModePreview)
	if !errors.Is(err, ErrReportPDFInvalidOutput) {
		t.Fatalf("Render() error = %v, want ErrReportPDFInvalidOutput", err)
	}
}

func TestReportPDFRendererRejectsInvalidModeBeforeRunning(t *testing.T) {
	config := testReportPDFConfig(t)
	called := false
	renderer, err := newReportPDFRenderer(config, reportPDFRunnerFunc(func(context.Context, string, ...string) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("newReportPDFRenderer() error = %v", err)
	}

	_, err = renderer.Render(context.Background(), []byte(`{"report_id":"RPT-001"}`), "sample")
	if !errors.Is(err, ErrReportPDFInvalidArgument) {
		t.Fatalf("Render() error = %v, want ErrReportPDFInvalidArgument", err)
	}
	if called {
		t.Fatal("renderer process was called for an invalid mode")
	}
}

func TestReportPDFRendererRejectsInvalidConfigurationAndSnapshot(t *testing.T) {
	config := testReportPDFConfig(t)
	config.ScriptPath = "relative/render.py"
	if _, err := NewReportPDFRenderer(config); !errors.Is(err, ErrReportPDFInvalidArgument) {
		t.Fatalf("NewReportPDFRenderer() error = %v, want ErrReportPDFInvalidArgument", err)
	}

	config = testReportPDFConfig(t)
	called := false
	renderer, err := newReportPDFRenderer(config, reportPDFRunnerFunc(func(context.Context, string, ...string) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("newReportPDFRenderer() error = %v", err)
	}
	if _, err := renderer.Render(context.Background(), []byte("not json"), ReportPDFModePreview); !errors.Is(err, ErrReportPDFInvalidArgument) {
		t.Fatalf("Render() error = %v, want ErrReportPDFInvalidArgument", err)
	}
	if called {
		t.Fatal("renderer process was called for an invalid snapshot")
	}
}

func TestReportPDFRendererCapsTimeoutAtTwoMinutes(t *testing.T) {
	config := testReportPDFConfig(t)
	config.Timeout = 10 * time.Minute
	renderer, err := NewReportPDFRenderer(config)
	if err != nil {
		t.Fatalf("NewReportPDFRenderer() error = %v", err)
	}
	if renderer.config.Timeout != 120*time.Second {
		t.Fatalf("timeout = %v, want 120s", renderer.config.Timeout)
	}
}

func testReportPDFConfig(t *testing.T) ReportPDFConfig {
	t.Helper()
	dir := t.TempDir()
	paths := []string{
		filepath.Join(dir, "python"),
		filepath.Join(dir, "render.py"),
		filepath.Join(dir, "font.ttf"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return ReportPDFConfig{
		PythonPath: paths[0],
		ScriptPath: paths[1],
		FontPath:   paths[2],
		Timeout:    time.Second,
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode for %s = %o, want %o", path, got, want)
	}
}
