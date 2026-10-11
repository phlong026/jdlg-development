package component

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	ReportPDFModePreview = "preview"
	ReportPDFModeFull    = "full"

	maxReportPDFInputBytes  = 8 << 20
	maxReportPDFOutputBytes = 64 << 20
	maxReportPDFTimeout     = 120 * time.Second
)

var (
	ErrReportPDFInvalidArgument = errors.New("report pdf: invalid argument")
	ErrReportPDFProcess         = errors.New("report pdf: renderer process failed")
	ErrReportPDFTimeout         = errors.New("report pdf: renderer timed out")
	ErrReportPDFCanceled        = errors.New("report pdf: rendering canceled")
	ErrReportPDFInvalidOutput   = errors.New("report pdf: invalid renderer output")
	ErrReportPDFTooLarge        = errors.New("report pdf: file exceeds size limit")
	ErrReportPDFIO              = errors.New("report pdf: private file operation failed")
)

type ReportPDFConfig struct {
	PythonPath string
	ScriptPath string
	FontPath   string
	AssetRoot  string
	Timeout    time.Duration
}

type ReportPDFRenderer struct {
	config ReportPDFConfig
	runner reportPDFRunner
}

type reportPDFRunner interface {
	Run(ctx context.Context, executable string, args ...string) error
}

type commandReportPDFRunner struct{}

func (commandReportPDFRunner) Run(ctx context.Context, executable string, args ...string) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func NewReportPDFRenderer(config ReportPDFConfig) (*ReportPDFRenderer, error) {
	return newReportPDFRenderer(config, commandReportPDFRunner{})
}

func newReportPDFRenderer(config ReportPDFConfig, runner reportPDFRunner) (*ReportPDFRenderer, error) {
	if runner == nil || !absolutePath(config.PythonPath) || !absolutePath(config.ScriptPath) || !absolutePath(config.FontPath) || config.Timeout < 0 {
		return nil, ErrReportPDFInvalidArgument
	}
	if config.Timeout == 0 || config.Timeout > maxReportPDFTimeout {
		config.Timeout = maxReportPDFTimeout
	}
	if config.AssetRoot != "" && !absolutePath(config.AssetRoot) {
		return nil, ErrReportPDFInvalidArgument
	}
	return &ReportPDFRenderer{config: config, runner: runner}, nil
}

func (r *ReportPDFRenderer) Render(ctx context.Context, snapshot []byte, mode string) ([]byte, error) {
	if ctx == nil || (mode != ReportPDFModePreview && mode != ReportPDFModeFull) {
		return nil, ErrReportPDFInvalidArgument
	}
	if len(snapshot) == 0 || len(snapshot) > maxReportPDFInputBytes || !json.Valid(snapshot) {
		return nil, ErrReportPDFInvalidArgument
	}

	dir, err := os.MkdirTemp("", "report-pdf-")
	if err != nil {
		return nil, ErrReportPDFIO
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("[report-pdf] warn: temporary-file cleanup failed")
		}
	}()

	inputPath := filepath.Join(dir, "input.json")
	outputPath := filepath.Join(dir, "result.pdf")
	if err := os.WriteFile(inputPath, snapshot, 0o600); err != nil {
		return nil, ErrReportPDFIO
	}
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, ErrReportPDFIO
	}
	if err := output.Close(); err != nil {
		return nil, ErrReportPDFIO
	}

	renderCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()
	args := []string{
		r.config.ScriptPath,
		"--input", inputPath,
		"--output", outputPath,
		"--mode", mode,
		"--font", r.config.FontPath,
	}
	if r.config.AssetRoot != "" {
		args = append(args, "--asset-root", r.config.AssetRoot)
	}
	err = r.runner.Run(renderCtx, r.config.PythonPath, args...)
	if err != nil {
		switch renderCtx.Err() {
		case context.DeadlineExceeded:
			return nil, ErrReportPDFTimeout
		case context.Canceled:
			return nil, ErrReportPDFCanceled
		default:
			return nil, ErrReportPDFProcess
		}
	}

	if err := os.Chmod(outputPath, 0o600); err != nil {
		return nil, ErrReportPDFIO
	}
	return readReportPDF(outputPath)
}

func readReportPDF(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrReportPDFInvalidOutput
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxReportPDFOutputBytes+1))
	if err != nil {
		return nil, ErrReportPDFIO
	}
	if len(data) > maxReportPDFOutputBytes {
		return nil, ErrReportPDFTooLarge
	}
	if len(data) == 0 || !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, ErrReportPDFInvalidOutput
	}
	return data, nil
}

func absolutePath(path string) bool {
	return path != "" && filepath.IsAbs(path)
}
