package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path"
	"time"
)

type Worker struct {
	config    Config
	store     Store
	generator SnapshotGenerator
	catalog   PackCatalog
	renderer  Renderer
	storage   PrivateStorage
	agents    AgentRunner
}

type WorkerOption func(*Worker)

func WithAgentRunner(runner AgentRunner) WorkerOption {
	return func(worker *Worker) { worker.agents = runner }
}

func NewWorker(config Config, store Store, generator SnapshotGenerator, catalog PackCatalog, renderer Renderer, storage PrivateStorage, options ...WorkerOption) (*Worker, error) {
	missingOnlyOnePDFDependency := renderer == nil && storage != nil || renderer != nil && storage == nil
	if store == nil || generator == nil || catalog == nil || config.MaxAttempts < 1 || config.JobTimeout <= 0 || config.LeaseDuration <= config.JobTimeout+10*time.Second || missingOnlyOnePDFDependency || config.PaymentEnabled && (renderer == nil || storage == nil) {
		return nil, ErrInvalidInput
	}
	worker := &Worker{config: config, store: store, generator: generator, catalog: catalog, renderer: renderer, storage: storage}
	for _, option := range options {
		if option != nil {
			option(worker)
		}
	}
	if config.MultiAgentEnabled && worker.agents == nil {
		return nil, ErrInvalidInput
	}
	return worker, nil
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	if !w.config.Enabled {
		return false, ErrDisabled
	}
	job, err := w.store.Claim(ctx, timeNow(), w.config.LeaseDuration, w.config.MaxAttempts)
	if errors.Is(err, ErrNoJob) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	jobCtx, cancel := context.WithTimeout(ctx, w.config.JobTimeout)
	defer cancel()
	if err := w.run(jobCtx, job); err != nil {
		code := workerErrorCode(jobCtx, err)
		retry := job.Report.AttemptCount < w.config.MaxAttempts && !errors.Is(err, ErrAgentRejected)
		if failErr := w.store.Fail(ctx, job.Report.ID, job.LeaseToken, job.Mode, code, retry); failErr != nil {
			log.Printf("[report] report_id=%s stage=%s failure_persist=failed", job.Report.ID, job.Mode)
			return true, ErrConflict
		}
		log.Printf("[report] report_id=%s stage=%s error_code=%s retry=%t", job.Report.ID, job.Mode, code, retry)
		return true, fmt.Errorf("report worker: %s", code)
	}
	return true, nil
}

func (w *Worker) run(ctx context.Context, job *Job) error {
	if job == nil || job.Report == nil || job.Report.ID == "" || job.LeaseToken == "" {
		return ErrInvalidInput
	}
	switch job.Mode {
	case ModePreview:
		var snapshot json.RawMessage
		var mode string
		var err error
		if w.config.MultiAgentEnabled {
			snapshot, mode, err = w.agents.Run(ctx, job.Report, ModePreview, nil)
		} else {
			snapshot, mode, err = w.generator.BuildPreview(ctx, job.Report)
		}
		if err != nil {
			return err
		}
		key := ""
		if w.renderer != nil && w.storage != nil {
			pdf, err := w.renderer.Render(ctx, snapshot, ModePreview)
			if err != nil {
				return err
			}
			key = reportObjectKey(job.Report, ModePreview, job.LeaseToken)
			if err := w.storage.UploadPrivate(ctx, key, pdf); err != nil {
				return err
			}
		}
		return w.store.CompletePreview(ctx, job.Report.ID, job.LeaseToken, snapshot, mode, key)
	case ModeFull:
		if w.renderer == nil || w.storage == nil {
			return ErrNotReady
		}
		if !job.Report.Paid || len(job.Report.PreviewSnapshot) == 0 {
			return ErrPaymentRequired
		}
		pack, err := w.catalog.Get(job.Report.PackID, job.Report.PackVersion)
		if err != nil {
			return ErrPackUnavailable
		}
		var snapshot json.RawMessage
		if w.config.MultiAgentEnabled {
			snapshot, _, err = w.agents.Run(ctx, job.Report, ModeFull, &pack)
		} else {
			snapshot, err = w.generator.BuildFull(job.Report.PreviewSnapshot, pack)
		}
		if err != nil {
			return err
		}
		pdf, err := w.renderer.Render(ctx, snapshot, ModeFull)
		if err != nil {
			return err
		}
		key := reportObjectKey(job.Report, ModeFull, job.LeaseToken)
		if err := w.storage.UploadPrivate(ctx, key, pdf); err != nil {
			return err
		}
		return w.store.CompleteFull(ctx, job.Report.ID, job.LeaseToken, snapshot, key)
	default:
		return ErrInvalidInput
	}
}

func reportObjectKey(record *Report, mode, leaseToken string) string {
	return path.Join("reports", fmt.Sprintf("%d", record.UserID), record.ID, mode+"-"+leaseToken+".pdf")
}

func workerErrorCode(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "generation_timeout"
	}
	switch {
	case errors.Is(err, ErrAgentRejected):
		return "agent_review_rejected"
	case errors.Is(err, ErrAgentUnavailable):
		return "agent_unavailable"
	case errors.Is(err, ErrPackUnavailable):
		return "reviewed_pack_unavailable"
	case errors.Is(err, ErrPaymentRequired):
		return "payment_not_authorized"
	case errors.Is(err, ErrInvalidInput):
		return "invalid_frozen_input"
	case errors.Is(err, ErrNotReady):
		return "pdf_infrastructure_unavailable"
	default:
		return "generation_failed"
	}
}

var timeNow = func() time.Time { return time.Now() }
