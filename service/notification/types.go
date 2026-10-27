package notification

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput = errors.New("notification: invalid input")
	ErrNotFound     = errors.New("notification: not found")
	ErrNoJob        = errors.New("notification: no job")
	ErrConflict     = errors.New("notification: conflict")
	ErrUnavailable  = errors.New("notification: store unavailable")
	ErrDeadLettered = errors.New("notification: event deadlettered")
)

const (
	DeliveryPending    = "pending"
	DeliveryProcessing = "processing"
	DeliveryAccepted   = "accepted"
	DeliveryFailed     = "failed"
	DeliveryUnknown    = "unknown"
	DeliveryCancelled  = "cancelled"
)

type Event struct {
	ID          string    `json:"event_id"`
	Kind        string    `json:"kind"`
	Owner       string    `json:"owner_id"`
	RecipientID int64     `json:"recipient_id"`
	VisitorKey  string    `json:"visitor_key"`
	ShareID     string    `json:"share_id"`
	ContentID   string    `json:"content_id,omitempty"`
	ProjectID   string    `json:"project_id,omitempty"`
	Route       string    `json:"route_key"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type EventJob struct {
	Event      Event
	LeaseToken string
}

type Notification struct {
	ID          string     `json:"notification_id"`
	EventID     string     `json:"event_id"`
	RecipientID int64      `json:"recipient_id"`
	Kind        string     `json:"kind"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Route       string     `json:"route_key"`
	ContentID   string     `json:"content_id,omitempty"`
	ShareID     string     `json:"share_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
}

type Delivery struct {
	ID                string     `json:"delivery_id"`
	NotificationID    string     `json:"notification_id"`
	RecipientID       int64      `json:"recipient_id"`
	Channel           string     `json:"channel"`
	Status            string     `json:"status"`
	AttemptCount      int        `json:"attempt_count"`
	BindingVersion    int64      `json:"binding_version"`
	LeaseToken        string     `json:"lease_token,omitempty"`
	LeaseUntil        *time.Time `json:"lease_until,omitempty"`
	NextAttemptAt     *time.Time `json:"next_attempt_at,omitempty"`
	ProviderMessageID string     `json:"provider_msg_id,omitempty"`
	ProviderStatus    string     `json:"provider_status,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type DeliveryJob struct {
	Delivery Delivery
	Notice   Notification
}

type Page struct {
	Items      []Notification `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type ConsumerStore interface {
	ClaimEvent(context.Context, time.Time, time.Duration, int) (*EventJob, error)
	MaterializeEvent(context.Context, *EventJob, string, Eligibility) error
}

// Eligibility is captured immediately before materialization. Eligible=false
// suppresses both inbox and delivery. DeliveryEligible=false still creates the
// inbox item but does not create a delivery for later retroactive sending.
type Eligibility struct {
	Eligible         bool
	DeliveryEligible bool
	BindingVersion   int64
	Reason           string
}

// DeliveryStore commits a lease before returning a job. Provider HTTP calls
// happen after ClaimDelivery returns, never while a database transaction runs.
type DeliveryStore interface {
	ClaimDelivery(context.Context, string, time.Time, time.Duration, int) (*DeliveryJob, error)
	CompleteDelivery(context.Context, string, string, string, string) error
	FailDelivery(context.Context, string, string, string, time.Time) error
	CancelDelivery(context.Context, string, string, string, time.Time) error
	RecordProviderReceipt(context.Context, string, string, time.Time) (bool, error)
}

type InboxStore interface {
	ListInbox(context.Context, int64, string, int) (Page, error)
	GetNotification(context.Context, int64, string) (Notification, error)
	UnreadCount(context.Context, int64) (int64, error)
	MarkRead(context.Context, int64, string, time.Time) error
	MarkAllRead(context.Context, int64, time.Time) (int64, error)
	CancelPending(context.Context, string, string, time.Time) (int64, error)
	DeleteVisitorData(context.Context, string, time.Time) (int64, error)
}

type DeliveryResult struct {
	Status            string
	ProviderMessageID string
	Reason            string
	Retryable         bool
}
