// Package reportnotify delivers completion notices independently of report generation.
package reportnotify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/url"
	"strings"
	"time"
)

const (
	EventReportReady    = "report_ready"
	StatePending        = "pending"
	StateSending        = "sending"
	StateSent           = "sent"
	StateManualRequired = "manual_required"
	StateUncertain      = "uncertain"
	BatchSize           = 100
	LeaseDuration       = 45 * time.Second
	LookupDelay         = 5 * time.Minute
)

var (
	ErrInvalidConfig    = errors.New("reportnotify: invalid gateway configuration")
	ErrInvalidInput     = errors.New("reportnotify: invalid notification input")
	ErrNoJob            = errors.New("reportnotify: no notification available")
	ErrConflict         = errors.New("reportnotify: notification lease conflict")
	ErrStoreUnavailable = errors.New("reportnotify: notification store unavailable")
	ErrUncertain        = errors.New("reportnotify: delivery outcome is uncertain")
	ErrRejected         = errors.New("reportnotify: gateway rejected delivery")
)

type Notification struct {
	EventID         string `json:"event_id"`
	ReportID        string `json:"report_id"`
	UserID          int64  `json:"user_id"`
	ExternalUserID  string `json:"external_user_id"`
	MiniProgramPath string `json:"mini_program_path"`
	Text            string `json:"text"`
}

type Receipt struct {
	Status  string `json:"status"`
	Receipt string `json:"receipt,omitempty"`
}

type Job struct {
	EventID        string
	ReportID       string
	UserID         int64
	ExternalUserID string
	LeaseToken     string
	AttemptCount   int
	Reconcile      bool
}

type Outcome struct {
	State     string
	Receipt   string
	ErrorCode string
}

type Store interface {
	CatchUp(context.Context, bool, int) (int64, error)
	Claim(context.Context, time.Duration) (*Job, error)
	Finish(context.Context, *Job, Outcome) error
}

type Provider interface {
	Send(context.Context, Notification) (Receipt, error)
	Lookup(context.Context, string) (Receipt, error)
}

type Service struct {
	configured bool
	store      Store
	provider   Provider
}

// NewService uses an injected provider in tests. A nil provider creates the
// explicitly configured gateway, or remains inactive when both env values lack.
func NewService(config Config, store Store, provider Provider) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrInvalidInput
	}
	if config.Configured() && provider == nil {
		var err error
		provider, err = NewHTTPProvider(config, nil)
		if err != nil {
			return nil, err
		}
	}
	return &Service{configured: config.Configured(), store: store, provider: provider}, nil
}

// RunOnce first catches up durable ready reports, then sends or reconciles one
// claimed event. It never changes report readiness or retries an uncertain POST.
func (s *Service) RunOnce(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, ErrInvalidInput
	}
	count, err := s.store.CatchUp(ctx, s.configured, BatchSize)
	if err != nil {
		return false, err
	}
	if !s.configured {
		return count > 0, nil
	}
	job, err := s.store.Claim(ctx, LeaseDuration)
	if errors.Is(err, ErrNoJob) {
		return count > 0, nil
	}
	if err != nil {
		return false, err
	}
	if !validJob(job) {
		return false, ErrInvalidInput
	}
	var receipt Receipt
	if job.Reconcile {
		receipt, err = s.provider.Lookup(ctx, job.EventID)
	} else {
		receipt, err = s.provider.Send(ctx, Notification{
			EventID: job.EventID, ReportID: job.ReportID, UserID: job.UserID,
			ExternalUserID:  job.ExternalUserID,
			MiniProgramPath: "pages/report/delivery?id=" + url.QueryEscape(job.ReportID),
			Text:            "您的建房报告已就绪，可进入报告中心查看。",
		})
	}
	outcome := Outcome{State: StateUncertain, ErrorCode: "delivery_unconfirmed"}
	if err == nil && receipt.Status == StateSent && validText(receipt.Receipt, 256) {
		outcome = Outcome{State: StateSent, Receipt: receipt.Receipt}
	} else if !job.Reconcile && errors.Is(err, ErrRejected) {
		outcome = Outcome{State: StateManualRequired, ErrorCode: "gateway_rejected"}
	}
	if outcome.State != StateSent {
		log.Printf("[report-notify] warn event_id=%s stage=delivery result=%s", job.EventID, outcome.ErrorCode)
	}
	if err := s.store.Finish(ctx, job, outcome); err != nil {
		return true, err
	}
	return true, nil
}

func EventID(reportID string) string {
	sum := sha256.Sum256([]byte(EventReportReady + ":" + reportID))
	return hex.EncodeToString(sum[:])
}

func validJob(job *Job) bool {
	return job != nil && validText(job.ReportID, 64) && job.EventID == EventID(job.ReportID) &&
		job.UserID > 0 && validText(job.ExternalUserID, 128) && validText(job.LeaseToken, 64)
}

func validText(value string, maximum int) bool {
	if len(value) == 0 || len(value) > maximum || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 32 || char == 127 {
			return false
		}
	}
	return true
}
