package report

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	StatusPreviewQueued     = "preview_queued"
	StatusPreviewGenerating = "preview_generating"
	StatusPreviewReady      = "preview_ready"
	StatusFullQueued        = "full_queued"
	StatusFullGenerating    = "full_generating"
	StatusReady             = "ready"
	StatusFailed            = "failed"

	ModePreview = "preview"
	ModeFull    = "full"

	GenerationRulesOnly  = "rules_only"
	GenerationAIAssisted = "ai_assisted"
)

var (
	ErrDisabled        = errors.New("report: module disabled")
	ErrInvalidInput    = errors.New("report: invalid input")
	ErrNotFound        = errors.New("report: not found")
	ErrForbidden       = errors.New("report: forbidden")
	ErrConflict        = errors.New("report: conflicting state")
	ErrPaymentRequired = errors.New("report: payment required")
	ErrBindingRequired = errors.New("report: enterprise contact binding required")
	ErrPackUnavailable = errors.New("report: reviewed pack unavailable")
	ErrNotReady        = errors.New("report: not ready")
	ErrNoJob           = errors.New("report: no queued job")
)

// Input is the five-step questionnaire payload. Server-derived fields never
// come from this structure.
type Input struct {
	Site       SiteStep       `json:"site"`
	Household  HouseholdStep  `json:"household"`
	Space      SpaceStep      `json:"space"`
	Preference PreferenceStep `json:"preference"`
	Budget     BudgetStep     `json:"budget"`
}

type SiteStep struct {
	Region   string  `json:"region"`
	WidthM   float64 `json:"width_m"`
	DepthM   float64 `json:"depth_m"`
	RoadSide string  `json:"road_side,omitempty"`
}

type HouseholdStep struct {
	FamilyType string `json:"family_type"`
	Residents  string `json:"residents"`
}

type SpaceStep struct {
	Floors              int  `json:"floors"`
	Bedrooms            int  `json:"bedrooms"`
	FirstFloorElderRoom bool `json:"first_floor_elder_room"`
}

type PreferenceStep struct {
	Style      string   `json:"style"`
	Priorities []string `json:"priorities"`
	Goal       string   `json:"goal"`
}

type BudgetStep struct {
	Wan   float64 `json:"wan"`
	Scope string  `json:"scope"`
}

type Report struct {
	ID                  string          `json:"id"`
	UserID              int64           `json:"user_id"`
	InputHash           string          `json:"input_hash"`
	ProtocolVersion     string          `json:"protocol_version"`
	InputJSON           json.RawMessage `json:"input_json"`
	ConfirmationJSON    json.RawMessage `json:"-"`
	ConfirmationHash    string          `json:"-"`
	CurrentStage        string          `json:"-"`
	PreviewSnapshot     json.RawMessage `json:"preview_snapshot,omitempty"`
	FullSnapshot        json.RawMessage `json:"full_snapshot,omitempty"`
	Status              string          `json:"status"`
	GenerationMode      string          `json:"generation_mode"`
	Paid                bool            `json:"paid"`
	Bound               bool            `json:"bound"`
	PaymentRef          string          `json:"payment_ref,omitempty"`
	PendingOrderNo      string          `json:"-"`
	ReservedProductID   string          `json:"-"`
	ReservedPriceFen    int64           `json:"-"`
	ReservedPackID      string          `json:"-"`
	ReservedPackVersion string          `json:"-"`
	ExternalUserID      string          `json:"external_user_id,omitempty"`
	PackID              string          `json:"pack_id,omitempty"`
	PackVersion         string          `json:"pack_version,omitempty"`
	PreviewObjectKey    string          `json:"preview_object_key,omitempty"`
	FullObjectKey       string          `json:"full_object_key,omitempty"`
	AttemptCount        int             `json:"attempt_count"`
	FailedStage         string          `json:"failed_stage,omitempty"`
	ErrorCode           string          `json:"error_code,omitempty"`
	LeaseToken          string          `json:"-"`
	LeaseUntil          *time.Time      `json:"-"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// SafeView is the only report shape intended for mini-program responses.
type SafeView struct {
	ID                 string          `json:"id"`
	ProtocolVersion    string          `json:"protocol_version"`
	InputHash          string          `json:"input_hash"`
	Status             string          `json:"status"`
	Stage              string          `json:"stage"`
	Confirmed          bool            `json:"confirmed"`
	NextAction         string          `json:"next_action"`
	PreviewSnapshot    json.RawMessage `json:"preview_snapshot,omitempty"`
	GenerationMode     string          `json:"generation_mode"`
	CanPay             bool            `json:"can_pay"`
	PaymentBlockReason string          `json:"payment_block_reason,omitempty"`
	Paid               bool            `json:"paid"`
	Bound              bool            `json:"bound"`
	ErrorCode          string          `json:"error_code,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type PaymentQuote struct {
	CanPay      bool   `json:"can_pay"`
	BlockReason string `json:"block_reason,omitempty"`
	ProductID   string `json:"product_id,omitempty"`
	PriceFen    int64  `json:"price_fen,omitempty"`
	PackID      string `json:"pack_id,omitempty"`
	PackVersion string `json:"pack_version,omitempty"`
}

type PaymentReservation struct {
	OrderNo     string `json:"order_no"`
	ProductID   string `json:"product_id"`
	PriceFen    int64  `json:"price_fen"`
	PackID      string `json:"pack_id"`
	PackVersion string `json:"pack_version"`
}

type Snapshot struct {
	Meta    SnapshotMeta      `json:"meta"`
	Facts   SnapshotFacts     `json:"facts"`
	Assets  map[string]string `json:"assets,omitempty"`
	Budget  *SnapshotBudget   `json:"budget,omitempty"`
	Schemes []SnapshotScheme  `json:"schemes,omitempty"`
	Pages   []SnapshotPage    `json:"pages"`
}

type SnapshotMeta struct {
	SchemaVersion   string `json:"schema_version"`
	ProtocolVersion string `json:"protocol_version"`
	Source          string `json:"source"`
	ReportID        string `json:"report_id"`
	InputHash       string `json:"input_hash"`
	Date            string `json:"date"`
	Demonstration   bool   `json:"demonstration"`
	GenerationMode  string `json:"generation_mode"`
	PackID          string `json:"pack_id,omitempty"`
	PackVersion     string `json:"pack_version,omitempty"`
}

type SnapshotFacts struct {
	Region              string   `json:"region"`
	Family              string   `json:"family"`
	Residents           string   `json:"residents"`
	WidthM              float64  `json:"width_m"`
	DepthM              float64  `json:"depth_m"`
	Floors              int      `json:"floors"`
	Bedrooms            int      `json:"bedrooms"`
	FirstFloorElderRoom bool     `json:"first_floor_elder_room"`
	Style               string   `json:"style"`
	BudgetWan           float64  `json:"budget_wan"`
	BudgetScope         string   `json:"budget_scope"`
	Priorities          []string `json:"priorities"`
	Goal                string   `json:"goal"`
	Missing             []string `json:"missing"`
}

type SnapshotPage struct {
	Number     int            `json:"number"`
	Access     string         `json:"access"`
	Kind       string         `json:"kind"`
	Scheme     string         `json:"scheme,omitempty"`
	Title      string         `json:"title"`
	Subtitle   string         `json:"subtitle"`
	Fixed      string         `json:"fixed,omitempty"`
	Dynamic    string         `json:"dynamic,omitempty"`
	AI         string         `json:"ai,omitempty"`
	Sources    []string       `json:"sources,omitempty"`
	Blocks     []ContentBlock `json:"blocks,omitempty"`
	Priorities []ContentBlock `json:"priorities,omitempty"`
	Table      [][]string     `json:"table,omitempty"`
	Note       string         `json:"note,omitempty"`
	Actions    []ContentBlock `json:"actions,omitempty"`
	Disclaimer string         `json:"disclaimer,omitempty"`
}

type ContentBlock struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type SnapshotBudget struct {
	Allocations []BudgetAllocation `json:"allocations"`
}

type BudgetAllocation struct {
	Name    string  `json:"name"`
	Percent float64 `json:"percent"`
}

type SnapshotScheme struct {
	ID                  string          `json:"id"`
	Name                string          `json:"name"`
	ReviewStatus        string          `json:"review_status"`
	Tagline             string          `json:"tagline"`
	Tradeoff            string          `json:"tradeoff"`
	Gain                string          `json:"gain"`
	Facade              string          `json:"facade"`
	FacadeCaution       string          `json:"facade_caution"`
	Bedrooms            int             `json:"bedrooms"`
	FirstFloorElderRoom bool            `json:"first_floor_elder_room"`
	PriorityFit         []string        `json:"priority_fit"`
	CostSensitive       []string        `json:"cost_sensitive"`
	PlanAssets          []string        `json:"plan_assets"`
	FacadeAsset         string          `json:"facade_asset"`
	VolumeAsset         string          `json:"volume_asset"`
	Floors              []SnapshotFloor `json:"floors,omitempty"`
}

type SnapshotFloor struct {
	Number  int    `json:"number"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	AssetID string `json:"asset_id"`
}

type ReviewedPack struct {
	ID             string             `json:"id"`
	Version        string             `json:"version"`
	ReviewStatus   string             `json:"review_status"`
	Styles         []string           `json:"styles"`
	MinWidthM      float64            `json:"min_width_m"`
	MaxWidthM      float64            `json:"max_width_m"`
	MinDepthM      float64            `json:"min_depth_m"`
	MaxDepthM      float64            `json:"max_depth_m"`
	Floors         int                `json:"floors"`
	BudgetScope    string             `json:"budget_scope"`
	Assets         map[string]string  `json:"assets"`
	Schemes        []PackScheme       `json:"schemes"`
	BudgetTemplate []BudgetAllocation `json:"budget_template"`
}

type PackScheme struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Tagline             string     `json:"tagline"`
	Tradeoff            string     `json:"tradeoff"`
	Gain                string     `json:"gain"`
	Facade              string     `json:"facade"`
	FacadeCaution       string     `json:"facade_caution"`
	Bedrooms            int        `json:"bedrooms"`
	FirstFloorElderRoom bool       `json:"first_floor_elder_room"`
	PriorityFit         []string   `json:"priority_fit"`
	CostSensitive       []string   `json:"cost_sensitive"`
	Plans               []PackPlan `json:"plans"`
	FacadeAssetID       string     `json:"facade_asset_id"`
	VolumeAssetID       string     `json:"volume_asset_id"`
}

type PackPlan struct {
	Floor   int    `json:"floor"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	AssetID string `json:"asset_id"`
}

type Job struct {
	Report     *Report
	Mode       string
	LeaseToken string
}

type Store interface {
	CreateOrGet(ctx context.Context, report *Report) (*Report, bool, error)
	GetForUser(ctx context.Context, userID int64, reportID string) (*Report, error)
	GetLatestForUser(ctx context.Context, userID int64) (*Report, error)
	ReservePaymentOrder(ctx context.Context, userID int64, reportID string, reservation PaymentReservation) (PaymentReservation, error)
	AuthorizePaid(ctx context.Context, userID int64, reportID, paymentRef string) (*Report, error)
	BindEnterpriseContact(ctx context.Context, userID int64, reportID, externalUserID string) error
	RetryFailed(ctx context.Context, userID int64, reportID string) (*Report, error)
	Claim(ctx context.Context, now time.Time, lease time.Duration, maxAttempts int) (*Job, error)
	CompletePreview(ctx context.Context, reportID, leaseToken string, snapshot json.RawMessage, mode, objectKey string) error
	SetPreviewObjectKeyIfEmpty(ctx context.Context, userID int64, reportID, objectKey string) error
	CompleteFull(ctx context.Context, reportID, leaseToken string, snapshot json.RawMessage, objectKey string) error
	Fail(ctx context.Context, reportID, leaseToken, stage, errorCode string, retry bool) error
}

type Renderer interface {
	Render(ctx context.Context, snapshot []byte, mode string) ([]byte, error)
}

type PrivateStorage interface {
	UploadPrivate(ctx context.Context, key string, data []byte) error
	SignDownload(ctx context.Context, key string, ttl time.Duration) (string, error)
}

type PackCatalog interface {
	Match(input Input) (ReviewedPack, error)
	Get(id, version string) (ReviewedPack, error)
}

type SnapshotGenerator interface {
	BuildPreview(ctx context.Context, report *Report) (json.RawMessage, string, error)
	BuildFull(preview json.RawMessage, pack ReviewedPack) (json.RawMessage, error)
}

type TextClient interface {
	Generate(ctx context.Context, input TextRequest) (TextResult, error)
}

type TextRequest struct {
	Facts SnapshotFacts `json:"facts"`
}

type TextResult struct {
	ProfileInsight string `json:"profile_insight"`
	CoreTradeoff   string `json:"core_tradeoff"`
	Recommendation string `json:"recommendation"`
}
