package notification

import (
	"context"
	"errors"
	"log"
	"time"
)

type EventEligibility func(context.Context, Event) (bool, error)
type BindingSnapshot func(context.Context, int64) (version int64, eligible bool, err error)

type ConsumerConfig struct {
	PollInterval  time.Duration
	LeaseDuration time.Duration
	ErrorBackoff  time.Duration
	MaxAttempts   int
	Channel       string
	Eligibility   EventEligibility
	Binding       BindingSnapshot
}

type Consumer struct {
	store ConsumerStore
	cfg   ConsumerConfig
	now   func() time.Time
}

func NewConsumer(store ConsumerStore, cfg ConsumerConfig) (*Consumer, error) {
	if store == nil || cfg.PollInterval <= 0 || cfg.LeaseDuration <= 0 || cfg.MaxAttempts < 1 || cfg.Channel == "" || cfg.Eligibility == nil {
		return nil, ErrInvalidInput
	}
	if cfg.ErrorBackoff <= 0 {
		cfg.ErrorBackoff = cfg.PollInterval
	}
	return &Consumer{store: store, cfg: cfg, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (c *Consumer) RunOnce(ctx context.Context) (bool, error) {
	job, err := c.store.ClaimEvent(ctx, c.now(), c.cfg.LeaseDuration, c.cfg.MaxAttempts)
	if errors.Is(err, ErrNoJob) {
		return false, nil
	}
	if errors.Is(err, ErrDeadLettered) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if job == nil || job.Event.ID == "" || job.LeaseToken == "" {
		return true, ErrConflict
	}
	eligible, err := c.cfg.Eligibility(ctx, job.Event)
	if err != nil {
		return true, err
	}
	plan := Eligibility{Eligible: eligible, Reason: "ineligible"}
	if eligible && c.cfg.Binding != nil {
		version, deliveryEligible, bindingErr := c.cfg.Binding(ctx, job.Event.RecipientID)
		if bindingErr != nil {
			return true, bindingErr
		}
		plan.DeliveryEligible, plan.BindingVersion = deliveryEligible, version
		if deliveryEligible && version <= 0 {
			return true, ErrConflict
		}
	}
	if err := c.store.MaterializeEvent(ctx, job, c.cfg.Channel, plan); err != nil {
		return true, err
	}
	return true, nil
}

func (c *Consumer) Run(ctx context.Context) error {
	for {
		worked, err := c.RunOnce(ctx)
		wait := c.cfg.PollInterval
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("[notification] worker=consumer error_type=%T retry_in=%s", err, c.cfg.ErrorBackoff)
			wait = c.cfg.ErrorBackoff
		} else if worked {
			continue
		}
		if !waitContext(ctx, wait) {
			return nil
		}
	}
}

type DeliveryEligibility func(context.Context, DeliveryJob) (bool, error)
type DeliverySend func(context.Context, DeliveryJob) DeliveryResult

type DeliveryWorkerConfig struct {
	Channel       string
	PollInterval  time.Duration
	LeaseDuration time.Duration
	ErrorBackoff  time.Duration
	RetryBase     time.Duration
	RetryMax      time.Duration
	MaxAttempts   int
	Eligibility   DeliveryEligibility
	Send          DeliverySend
}

type DeliveryWorker struct {
	store DeliveryStore
	cfg   DeliveryWorkerConfig
	now   func() time.Time
}

func NewDeliveryWorker(store DeliveryStore, cfg DeliveryWorkerConfig) (*DeliveryWorker, error) {
	if store == nil || cfg.Channel == "" || cfg.PollInterval <= 0 || cfg.LeaseDuration <= 0 || cfg.RetryBase <= 0 || cfg.RetryMax < cfg.RetryBase || cfg.MaxAttempts < 1 || cfg.Eligibility == nil || cfg.Send == nil {
		return nil, ErrInvalidInput
	}
	if cfg.ErrorBackoff <= 0 {
		cfg.ErrorBackoff = cfg.PollInterval
	}
	return &DeliveryWorker{store: store, cfg: cfg, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (w *DeliveryWorker) RunOnce(ctx context.Context) (bool, error) {
	now := w.now()
	job, err := w.store.ClaimDelivery(ctx, w.cfg.Channel, now, w.cfg.LeaseDuration, w.cfg.MaxAttempts)
	if errors.Is(err, ErrNoJob) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if job == nil || job.Delivery.ID == "" || job.Delivery.LeaseToken == "" || job.Delivery.BindingVersion <= 0 {
		return true, ErrConflict
	}
	eligible, err := w.cfg.Eligibility(ctx, *job)
	if err != nil {
		next := now.Add(retryDelay(w.cfg.RetryBase, w.cfg.RetryMax, job.Delivery.AttemptCount))
		if saveErr := w.store.FailDelivery(ctx, job.Delivery.ID, job.Delivery.LeaseToken, "eligibility_unavailable", next); saveErr != nil {
			return true, saveErr
		}
		return true, err
	}
	if !eligible {
		return true, w.store.CancelDelivery(ctx, job.Delivery.ID, job.Delivery.LeaseToken, "ineligible", now)
	}
	result := w.cfg.Send(ctx, *job)
	switch result.Status {
	case DeliveryAccepted, DeliveryUnknown:
		return true, w.store.CompleteDelivery(ctx, job.Delivery.ID, job.Delivery.LeaseToken, result.Status, result.ProviderMessageID)
	case DeliveryFailed:
		next := time.Time{}
		if result.Retryable && job.Delivery.AttemptCount < w.cfg.MaxAttempts {
			next = now.Add(retryDelay(w.cfg.RetryBase, w.cfg.RetryMax, job.Delivery.AttemptCount))
		}
		reason := result.Reason
		if reason == "" {
			reason = "provider_failed"
		}
		return true, w.store.FailDelivery(ctx, job.Delivery.ID, job.Delivery.LeaseToken, reason, next)
	default:
		return true, w.store.CompleteDelivery(ctx, job.Delivery.ID, job.Delivery.LeaseToken, DeliveryUnknown, result.ProviderMessageID)
	}
}

func (w *DeliveryWorker) Run(ctx context.Context) error {
	for {
		worked, err := w.RunOnce(ctx)
		wait := w.cfg.PollInterval
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("[notification] worker=delivery channel=%s error_type=%T retry_in=%s", w.cfg.Channel, err, w.cfg.ErrorBackoff)
			wait = w.cfg.ErrorBackoff
		} else if worked {
			continue
		}
		if !waitContext(ctx, wait) {
			return nil
		}
	}
}

func retryDelay(base, maximum time.Duration, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for i := 1; i < attempt && delay < maximum; i++ {
		if delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
