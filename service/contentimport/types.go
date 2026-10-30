// Package contentimport resolves source references. Temporary upstream URLs are
// never evidence that media has been imported, stored, or published by JDLG.
package contentimport

import (
	"context"
	"errors"
	"fmt"
	"service/tracking"
	"time"
)

type Platform string

const (
	WechatArticle Platform = "wechat_article"
	Douyin        Platform = "douyin"
	Xiaohongshu   Platform = "xiaohongshu"
	Channels      Platform = "channels"
)

type RemoteMedia struct {
	ID         string `json:"source_id,omitempty"`
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	Caption    string `json:"caption,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

type Source struct {
	Platform  Platform      `json:"platform"`
	ID        string        `json:"source_id"`
	SourceURL string        `json:"source_url"`
	Title     string        `json:"title"`
	Text      string        `json:"text,omitempty"`
	Author    string        `json:"author,omitempty"`
	Media     []RemoteMedia `json:"media"`
	CoverURL  string        `json:"cover_url,omitempty"`
	// This describes provider intent, not a verified pixel-level guarantee.
	WatermarkExtraction bool `json:"watermark_extraction"`
}

type Resolver interface {
	Resolve(context.Context, string) (Source, error)
}

type ResolveError struct {
	Code      string
	Uncertain bool
	Retryable bool
}

func (e *ResolveError) Error() string { return fmt.Sprintf("content import: %s", e.Code) }
func fail(code string) *ResolveError  { return &ResolveError{Code: code} }

// Document is an internal resolution snapshot. Media refs in Body are remote-0000
// etc until the import worker replaces them with permission-checked Asset IDs.
// It must never be returned as a public task response.
type Document struct {
	Source            Source                   `json:"source"`
	Body              tracking.ArticleDocument `json:"body"`
	SourceAccountID   string                   `json:"source_account_id,omitempty"`
	SourceAccountName string                   `json:"source_account_name,omitempty"`
	ArchiveHTML       string                   `json:"archive_html,omitempty"`
	Warnings          []string                 `json:"warnings,omitempty"`
	CoverRef          string                   `json:"cover_ref,omitempty"`
}
type DocumentResolver interface {
	ResolveDocument(context.Context, string) (Document, error)
}

type CaptureVideo struct {
	ID       string `json:"source_id"`
	URL      string `json:"url"`
	FormatID int    `json:"format_id,omitempty"`
}
type VideoCapture interface {
	Capture(context.Context, string, []string) ([]CaptureVideo, error)
}

const (
	Queued      = "queued"
	Resolving   = "resolving"
	Downloading = "downloading"
	Ready       = "ready"
	Failed      = "failed"
	Uncertain   = "uncertain"
	Blocked     = "blocked"
)

type Request struct {
	Tenant        string
	Actor         int64
	Owner         string
	URL           string
	Platform      Platform
	Rights        string
	Key           string
	TargetMPAppID string
}
type Job struct {
	ID            string    `json:"import_id"`
	Tenant        string    `json:"-"`
	Actor         int64     `json:"-"`
	Owner         string    `json:"owner_id"`
	URL           string    `json:"source_url"`
	Platform      Platform  `json:"platform"`
	Rights        string    `json:"-"`
	Key           string    `json:"-"`
	TargetMPAppID string    `json:"target_mp_appid,omitempty"`
	Status        string    `json:"status"`
	CanRetry      bool      `json:"can_retry"`
	ContentID     string    `json:"content_id,omitempty"`
	ErrorCode     string    `json:"error_code,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LeaseToken    string    `json:"-"`
	LeaseUntil    time.Time `json:"-"`
	RequestSent   bool      `json:"-"`
	Document      *Document `json:"-"`
}

type JobStore interface {
	VerifySchema(context.Context) error
	Retry(context.Context, int64, string) (Job, error)
	Submit(context.Context, Request) (Job, error)
	Get(context.Context, int64, string) (Job, error)
	List(context.Context, int64, []string, int) ([]Job, error)
	Claim(context.Context, time.Time, time.Duration) (Job, error)
	BeginResolve(context.Context, string, string) error
	SaveResolution(context.Context, string, string, Document) error
	Renew(context.Context, string, string, time.Time, time.Duration) error
	Finish(context.Context, string, string, string, string, string) error
}

var (
	ErrNoJob       = errors.New("content import: no job")
	ErrNotFound    = errors.New("content import: not found")
	ErrConflict    = errors.New("content import: conflict")
	ErrUnavailable = errors.New("content import: unavailable")
)
