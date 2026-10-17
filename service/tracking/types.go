// Package tracking implements privacy-scoped facts. No inference of delivery or real-world identity.
package tracking

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const MetricVersion = "visible-v1"
const RuleVersion = "followup-v1"

var ErrUnavailable = errors.New("tracking unavailable")

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string          { return e.Message }
func fail(status int, msg string) error { return &Error{status, msg} }
func opaque() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic("secure random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func marshal(v any) []byte { b, _ := json.Marshal(v); return b }
func validID(s string) bool {
	if len(s) < 8 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ':') {
			return false
		}
	}
	return true
}
func safeURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

type Scope struct {
	Tenant                           string
	Actor                            int64
	Owners                           []string
	View, Manage, CRM, Delete, Rules bool
}

func (s Scope) Allows(owner string) bool {
	for _, o := range s.Owners {
		if o == owner {
			return true
		}
	}
	return false
}

type Config struct {
	Enabled, ContentEnabled, VideoEnabled, IntentEnabled bool
	Tenant, DefaultOwner, ConsentVersion, PrivacyNotice  string
	Routes                                               map[string]Route
	RawDays                                              int
	RetentionDays                                        int
	MediaHosts                                           []string
}
type Route struct {
	Title, Type string
	Public      bool
	// Audience labels the page's consumption domain for customer-vs-channel
	// gating: "customer" (house-building prospects), "bside" (designers,
	// builders, suppliers), "neutral" (shared plumbing). Empty or unknown
	// routes resolve to neutral; excluded pages carry no page_view data.
	Audience string
}

const (
	RouteAudienceCustomer = "customer"
	RouteAudienceBSide    = "bside"
	RouteAudienceNeutral  = "neutral"
)

// BSideIdentities are channel accounts (designers, builder crews, material
// suppliers, institutions). They distribute content; their own browsing never
// counts as customer intent, and lead conversion hard-blocks on them.
var BSideIdentities = map[string]bool{
	"设计师":  true,
	"施工队":  true,
	"建材商":  true,
	"机构用户": true,
}
type Record struct {
	Tenant, Kind, ID, Owner, Visitor, Session, Content, Share string
	At                                                        time.Time
	Payload                                                   json.RawMessage
}
type Asset struct {
	ID     string `json:"media_id"`
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	MIME   string `json:"mime"`
	Title  string `json:"title,omitempty"`
}
type Content struct {
	ReadOnly    bool       `json:"read_only,omitempty"`
	Route       string     `json:"route_key,omitempty"`
	VersionMode string     `json:"version_mode,omitempty"`
	ID          string     `json:"content_id"`
	Version     int        `json:"version"`
	Revision    int        `json:"revision"`
	Title       string     `json:"title"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	Cover       *Asset     `json:"cover,omitempty"`
	Media       []Asset    `json:"media"`
	Body        string     `json:"body"`
	BodyFormat  string     `json:"body_format"`
	SourceURL   string     `json:"source_url,omitempty"`
	SourceNote  string     `json:"source_note,omitempty"`
	Rights      string     `json:"rights"`
	Owner       string     `json:"owner_id"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Identified  int        `json:"identified_count"`
	Anonymous   int        `json:"anonymous_count"`
	PageViews   int        `json:"page_views"`
	VisibleMS   int64      `json:"visible_ms"`
	LastVisit   *time.Time `json:"last_visit,omitempty"`
}
type Draft struct {
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	BodyFormat string   `json:"body_format"`
	SourceURL  string   `json:"source_url"`
	SourceNote string   `json:"source_note"`
	Rights     string   `json:"rights"`
	Owner      string   `json:"owner_id"`
	CoverID    string   `json:"cover_media_id"`
	MediaIDs   []string `json:"media_ids"`
	Revision   int      `json:"expected_revision"`
}
type Share struct {
	ID        string    `json:"share_id"`
	TokenHash string    `json:"-"`
	Route     string    `json:"route_key"`
	Content   string    `json:"content_id,omitempty"`
	Version   int       `json:"content_version,omitempty"`
	Owner     string    `json:"owner_id"`
	Sharer    string    `json:"sharer_id"`
	Session   string    `json:"session_id,omitempty"`
	Parent    string    `json:"parent_share_id,omitempty"`
	Expires   time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
}
type EntryInput struct {
	Route      string `json:"route_key"`
	Content    string `json:"content_id"`
	Version    int    `json:"content_version"`
	ShareToken string `json:"share_token"`
	Client     string `json:"client_instance"`
	Anonymous  string `json:"anonymous_id"`
	Consent    string `json:"consent_version"`
	Previous   string `json:"previous_entry_token"`
}
type Entry struct {
	Route           string    `json:"route_key"`
	Content         string    `json:"content_id,omitempty"`
	Version         int       `json:"content_version,omitempty"`
	Session         string    `json:"session_id"`
	Token           string    `json:"entry_token,omitempty"`
	Visitor         string    `json:"visitor_key"`
	Anonymous       string    `json:"anonymous_id,omitempty"`
	Account         int64     `json:"account"`
	Client          string    `json:"client_instance"`
	Owner           string    `json:"owner_id"`
	Share           string    `json:"share_id,omitempty"`
	Started         time.Time `json:"started_at"`
	LastActive      time.Time `json:"last_active_at"`
	Expires         time.Time `json:"expires_at"`
	Accessible      bool      `json:"content_accessible"`
	OriginalVisitor string    `json:"original_visitor_key,omitempty"`
	Preview         bool      `json:"is_preview,omitempty"`
	Self            bool      `json:"is_self_visit,omitempty"`
}
type Event struct {
	SourceChannel string     `json:"source_channel,omitempty"`
	Schema        int        `json:"schema_version"`
	ID            string     `json:"event_id"`
	Type          string     `json:"event_type"`
	SDK           string     `json:"sdk_version,omitempty"`
	Session       string     `json:"session_id"`
	View          string     `json:"page_view_id,omitempty"`
	Segment       string     `json:"segment_id,omitempty"`
	Seq           int        `json:"seq,omitempty"`
	Route         string     `json:"route_key"`
	Previous      string     `json:"previous_page_view_id,omitempty"`
	Share         string     `json:"share_id,omitempty"`
	Content       string     `json:"content_id,omitempty"`
	Version       int        `json:"content_version,omitempty"`
	Target        string     `json:"target_content_id,omitempty"`
	Media         string     `json:"media_id,omitempty"`
	Player        string     `json:"player_instance_id,omitempty"`
	Ranges        [][2]int64 `json:"played_ranges,omitempty"`
	Occurred      time.Time  `json:"occurred_at"`
	Received      time.Time  `json:"received_at"`
	Monotonic     int64      `json:"client_monotonic_ms"`
	Delta         int64      `json:"visible_delta_ms,omitempty"`
	AppState      string     `json:"app_state"`
	PageState     string     `json:"page_state"`
	Action        string     `json:"action_key,omitempty"`
	Component     string     `json:"component_key,omitempty"`
	Scroll        float64    `json:"scroll_depth,omitempty"`
	Position      int64      `json:"current_time_ms,omitempty"`
	Duration      int64      `json:"video_duration_ms,omitempty"`
	Watch         int64      `json:"video_watch_ms,omitempty"`
	Unique        int64      `json:"unique_played_ms,omitempty"`
	Rate          float64    `json:"playback_rate,omitempty"`
	PlayState     string     `json:"play_state,omitempty"`
	Flags         []string   `json:"quality_flags"`
	BusinessID    string     `json:"business_record_id,omitempty"`
}
type Result struct {
	ID     string `json:"event_id"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}
type Filter struct {
	ContentVersion                                     int
	Start, End, AsOf                                   time.Time
	Content, Identity, Intent, Search, ContentSearch   string
	VisitorSearch, Owner, Sort, Direction, Cursor      string
	// Visitor 精确过滤（风控台「查看访问记录」跳转用）：只看该 visitor_key 的记录。
	// 仅匹配解析后的已识别键（u:<id>）或原始匿名键，不做模糊搜索。
	Visitor  string
	Limit    int
	Related  bool
}
type Snapshot struct {
	AsOf   time.Time `json:"as_of"`
	Metric string    `json:"metric_version"`
}
type List struct {
	FirstCursor      string `json:"first_cursor,omitempty"`
	ProfileRevision  string `json:"profile_revision,omitempty"`
	OmittedPageCount int    `json:"omitted_page_count,omitempty"`
	Items            any    `json:"items"`
	Cursor           string `json:"next_cursor,omitempty"`
	Identified       int    `json:"identified_count"`
	Anonymous        int    `json:"anonymous_count"`
	Snapshot
}
type IntentRule struct {
	Level   string    `json:"level"`
	Reasons []string  `json:"reasons"`
	Rule    string    `json:"rule_version"`
	Start   time.Time `json:"window_start"`
	End     time.Time `json:"window_end"`
}
type IntentOverride struct {
	Created time.Time `json:"created_at"`
	Level   string    `json:"level"`
	Reason  string    `json:"reason"`
	Actor   int64     `json:"actor"`
	Expires time.Time `json:"expires_at"`
}
type Intent struct {
	Metrics              IntentMetrics    `json:"metrics"`
	Thresholds           RuleParameters   `json:"thresholds"`
	Exclusions           []string         `json:"exclusions"`
	ParticipatingMetrics []string         `json:"participating_metrics"`
	Evidence             []IntentEvidence `json:"evidence"`
	EvidenceTruncated    bool             `json:"evidence_truncated,omitempty"`
	ScopeRequired        bool             `json:"scope_required,omitempty"`
	Timezone             string           `json:"timezone"`
	OverrideState        string           `json:"override_state"`
	sourceFingerprint    string
	overrideRevision     int64

	OriginalRule *IntentRule     `json:"original_rule,omitempty"`
	Override     *IntentOverride `json:"override,omitempty"`
	Level        string          `json:"level"`
	Reasons      []string        `json:"reasons"`
	Rule         string          `json:"rule_version"`
	Start        time.Time       `json:"window_start"`
	// CustomerShare is the fraction of window visible time spent on
	// customer-domain pages over customer+bside pages (neutral excluded from
	// the denominator so shared tooling does not dilute the signal). It gates
	// gray-zone lead conversion for visitors without a declared identity.
	CustomerShare float64   `json:"customer_share,omitempty"`
	End           time.Time `json:"window_end"`
	Calculated    time.Time `json:"calculated_at"`
	Note          string    `json:"manual_note,omitempty"`
}

// LatestVisit describes one counted page view inside the active query scope.
// Visible is that view's deduplicated, window-clipped duration, not a total
// across visits. A nil projection means no page view started in the window;
// a present projection with zero Visible is still a real counted visit.
type LatestVisit struct {
	ID      string    `json:"page_view_id"`
	Started time.Time `json:"started_at"`
	Visible int64     `json:"visible_ms"`
	Content string    `json:"content_id,omitempty"`
	Version int       `json:"content_version,omitempty"`
}
type Visitor struct {
	Key          string       `json:"visitor_key"`
	Nickname     string       `json:"nickname"`
	AvatarURL    string       `json:"avatar_url,omitempty"`
	State        string       `json:"identity_state"`
	IdentityType string       `json:"identity_type,omitempty"`
	CRMLabel     string       `json:"crm_label,omitempty"`
	Last         time.Time    `json:"last_visit"`
	Views        int          `json:"page_views"`
	Visible      int64        `json:"visible_ms"`
	LatestVisit  *LatestVisit `json:"latest_visit,omitempty"`
	Sessions     int          `json:"session_count"`
	Intent       Intent       `json:"intent"`
}
type PageView struct {
	ID       string     `json:"page_view_id"`
	Route    string     `json:"route_key"`
	Title    string     `json:"title"`
	Content  string     `json:"content_id,omitempty"`
	Version  int        `json:"content_version,omitempty"`
	Previous string     `json:"previous_page_view_id,omitempty"`
	Share    string     `json:"share_id,omitempty"`
	Started  time.Time  `json:"started_at"`
	Ended    *time.Time `json:"ended_at,omitempty"`
	Visible  int64      `json:"visible_ms"`
	Flags    []string   `json:"quality_flags"`
}
type Session struct {
	ID      string     `json:"session_id"`
	Started time.Time  `json:"started_at"`
	Last    time.Time  `json:"last_active_at"`
	Share   string     `json:"entry_share_id,omitempty"`
	Source  string     `json:"source"`
	Views   []PageView `json:"page_views"`
}

func within(t time.Time, f Filter) bool {
	return !t.Before(f.Start) && t.Before(f.End) && !t.After(f.AsOf)
}
func contentKey(id string, version int) string { return fmt.Sprintf("%s:%d", id, version) }
func (c Config) validateRoute(route string) error {
	if _, ok := c.Routes[route]; !ok || !c.Routes[route].Public {
		return fail(404, "页面不在采集范围")
	}
	return nil
}
func trim(s string, n int) bool { return strings.TrimSpace(s) != "" && len([]rune(s)) <= n }
