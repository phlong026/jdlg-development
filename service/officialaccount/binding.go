package officialaccount

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"
)

var (
	ErrNotConfigured  = errors.New("officialaccount: not configured")
	ErrInvalidInput   = errors.New("officialaccount: invalid input")
	ErrInvalidState   = errors.New("officialaccount: invalid state")
	ErrExpiredState   = errors.New("officialaccount: expired state")
	ErrConsumedState  = errors.New("officialaccount: consumed state")
	ErrNotBound       = errors.New("officialaccount: not bound")
	ErrNotSubscribed  = errors.New("officialaccount: not subscribed")
	ErrBindingChanged = errors.New("officialaccount: binding changed")
	ErrUnavailable    = errors.New("officialaccount: unavailable")
)

const DefaultIntentTTL = 10 * time.Minute

type Config struct {
	Enabled             bool
	AppID               string
	AppSecret           string
	CallbackToken       string
	EncodingAESKey      string
	TemplateID          string
	PublicOrigin        string
	MiniProgramAppID    string
	MiniProgramPagePath string
}

type CapabilityStatus struct {
	Enabled           bool     `json:"enabled"`
	Configured        bool     `json:"configured"`
	BindingAvailable  bool     `json:"binding_available"`
	DeliveryAvailable bool     `json:"delivery_available"`
	Problems          []string `json:"problems,omitempty"`
}

func LoadConfigFromEnv() Config {
	return Config{
		Enabled:        os.Getenv("OFFICIAL_ACCOUNT_ENABLED") == "true",
		AppID:          strings.TrimSpace(os.Getenv("OFFICIAL_ACCOUNT_APP_ID")),
		AppSecret:      strings.TrimSpace(os.Getenv("OFFICIAL_ACCOUNT_APP_SECRET")),
		CallbackToken:  strings.TrimSpace(os.Getenv("OFFICIAL_ACCOUNT_CALLBACK_TOKEN")),
		EncodingAESKey: strings.TrimSpace(os.Getenv("OFFICIAL_ACCOUNT_ENCODING_AES_KEY")),
		TemplateID:     strings.TrimSpace(os.Getenv("OFFICIAL_ACCOUNT_BROWSE_TEMPLATE_ID")),
		PublicOrigin:   strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_SHARE_ORIGIN")), "/"),
	}
}

func (c Config) Status() CapabilityStatus {
	status := CapabilityStatus{Enabled: c.Enabled}
	if !c.Enabled {
		return status
	}
	baseOK := c.AppID != "" && c.AppSecret != "" && validPublicOrigin(c.PublicOrigin)
	status.BindingAvailable = baseOK && c.CallbackToken != "" && len(c.EncodingAESKey) == 43
	status.DeliveryAvailable = baseOK && c.TemplateID != "" && validMiniProgramTarget(c.MiniProgramAppID, c.MiniProgramPagePath)
	checks := []struct {
		ok      bool
		problem string
	}{
		{c.AppID != "", "OFFICIAL_ACCOUNT_APP_ID 未配置"},
		{c.AppSecret != "", "OFFICIAL_ACCOUNT_APP_SECRET 未配置"},
		{c.CallbackToken != "", "OFFICIAL_ACCOUNT_CALLBACK_TOKEN 未配置"},
		{len(c.EncodingAESKey) == 43, "OFFICIAL_ACCOUNT_ENCODING_AES_KEY 必须为43位"},
		{c.TemplateID != "", "OFFICIAL_ACCOUNT_BROWSE_TEMPLATE_ID 未配置"},
		{validMiniProgramTarget(c.MiniProgramAppID, c.MiniProgramPagePath), "服务号跳转小程序未核验"},
		{validPublicOrigin(c.PublicOrigin), "PUBLIC_SHARE_ORIGIN 必须为固定 HTTPS 来源"},
	}
	for _, check := range checks {
		if !check.ok {
			status.Problems = append(status.Problems, check.problem)
		}
	}
	status.Configured = len(status.Problems) == 0
	return status
}

func validPublicOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

func validMiniProgramTarget(appID, pagePath string) bool {
	return appID != "" && len(appID) <= 64 && validMiniProgramPagePath(pagePath)
}

func validMiniProgramPagePath(pagePath string) bool {
	return pagePath != "" && len(pagePath) <= 256 && !strings.HasPrefix(pagePath, "/") && !strings.ContainsAny(pagePath, "?#")
}

type BindingIntent struct {
	State      string     `json:"state"`
	UserID     int64      `json:"user_id"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
}

func consumeBindingIntent(intent *BindingIntent, state string, now time.Time) (int64, error) {
	if intent == nil || intent.UserID <= 0 || state == "" || subtle.ConstantTimeCompare([]byte(intent.State), []byte(state)) != 1 {
		return 0, ErrInvalidState
	}
	if intent.ConsumedAt != nil {
		return 0, ErrConsumedState
	}
	if !now.Before(intent.ExpiresAt) {
		return 0, ErrExpiredState
	}
	consumed := now.UTC()
	intent.ConsumedAt = &consumed
	return intent.UserID, nil
}

type Binding struct {
	AppID      string    `json:"app_id"`
	OpenID     string    `json:"-"`
	UnionID    string    `json:"-"`
	UserID     int64     `json:"user_id"`
	Version    int64     `json:"version"`
	Active     bool      `json:"active"`
	Subscribed bool      `json:"subscribed"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const (
	EventSubscribe   = "subscribe"
	EventUnsubscribe = "unsubscribe"
)

type CallbackEvent struct {
	DedupKey          string
	AppID             string
	OpenID            string
	Type              string
	BindingState      string
	ProviderMessageID string
	ProviderStatus    string
}

// BindingStore is the persistence boundary used by routes and template delivery.
// Implementations must atomically consume state and write the binding.
type BindingStore interface {
	CreateIntent(context.Context, int64, time.Time, time.Duration) (BindingIntent, error)
	ConsumeAndBind(context.Context, string, string, string, string, bool, time.Time) (Binding, error)
	ResolveBinding(context.Context, int64, string) (Binding, error)
	ResolveVersion(context.Context, int64, string, int64) (Binding, error)
	Unbind(context.Context, int64, string, time.Time) error
	ApplyEvent(context.Context, CallbackEvent, time.Time) (bool, error)
}

type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) (*SQLStore, error) {
	if db == nil {
		return nil, ErrInvalidInput
	}
	return &SQLStore{db: db}, nil
}

func (s *SQLStore) VerifySchema(ctx context.Context) error {
	for _, query := range []string{
		`SELECT state_hash,user_id,expires_at,consumed_at FROM official_account_binding_intents LIMIT 0`,
		`SELECT app_id,user_id,open_id,union_id,version,active,subscribed,updated_at FROM official_account_bindings LIMIT 0`,
		`SELECT app_id,event_key,event_type,open_id,created_at FROM official_account_callback_events LIMIT 0`,
	} {
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			return ErrUnavailable
		}
		if rows.Close() != nil {
			return ErrUnavailable
		}
	}
	return nil
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", ErrUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func stateHash(state string) string {
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:])
}

func (s *SQLStore) CreateIntent(ctx context.Context, userID int64, now time.Time, ttl time.Duration) (BindingIntent, error) {
	if userID <= 0 || ttl <= 0 || ttl > time.Hour {
		return BindingIntent{}, ErrInvalidInput
	}
	state, err := randomState()
	if err != nil {
		return BindingIntent{}, err
	}
	intent := BindingIntent{State: state, UserID: userID, ExpiresAt: now.UTC().Add(ttl)}
	_, err = s.db.ExecContext(ctx, `INSERT INTO official_account_binding_intents(state_hash,user_id,expires_at,created_at) VALUES (?,?,?,?)`, stateHash(state), userID, intent.ExpiresAt, now.UTC())
	if err != nil {
		return BindingIntent{}, ErrUnavailable
	}
	return intent, nil
}

func (s *SQLStore) ConsumeAndBind(ctx context.Context, state, appID, openID, unionID string, subscribed bool, now time.Time) (Binding, error) {
	if state == "" || len(state) > 128 || appID == "" || len(appID) > 64 || openID == "" || len(openID) > 128 || len(unionID) > 128 {
		return Binding{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Binding{}, ErrUnavailable
	}
	defer tx.Rollback()
	binding, err := consumeAndBindTx(ctx, tx, state, appID, openID, unionID, subscribed, now.UTC())
	if err != nil {
		return Binding{}, err
	}
	if tx.Commit() != nil {
		return Binding{}, ErrUnavailable
	}
	return binding, nil
}

func consumeAndBindTx(ctx context.Context, tx *sql.Tx, state, appID, openID, unionID string, subscribed bool, now time.Time) (Binding, error) {
	var userID int64
	var expires time.Time
	var consumed sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT user_id,expires_at,consumed_at FROM official_account_binding_intents WHERE state_hash=? FOR UPDATE`, stateHash(state)).Scan(&userID, &expires, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrInvalidState
	}
	if err != nil {
		return Binding{}, ErrUnavailable
	}
	intent := BindingIntent{State: state, UserID: userID, ExpiresAt: expires}
	if consumed.Valid {
		intent.ConsumedAt = &consumed.Time
	}
	if _, err = consumeBindingIntent(&intent, state, now); err != nil {
		return Binding{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE official_account_binding_intents SET consumed_at=? WHERE state_hash=? AND consumed_at IS NULL`, now, stateHash(state)); err != nil {
		return Binding{}, ErrUnavailable
	}
	var maxVersion sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT MAX(version) FROM official_account_bindings WHERE app_id=? AND (user_id=? OR open_id=?) FOR UPDATE`, appID, userID, openID).Scan(&maxVersion); err != nil {
		return Binding{}, ErrUnavailable
	}
	version := maxVersion.Int64 + 1
	if _, err = tx.ExecContext(ctx, `DELETE FROM official_account_bindings WHERE app_id=? AND (user_id=? OR open_id=?)`, appID, userID, openID); err != nil {
		return Binding{}, ErrUnavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO official_account_bindings(app_id,user_id,open_id,union_id,version,active,subscribed,updated_at) VALUES (?,?,?,?,?,TRUE,?,?)`, appID, userID, openID, unionID, version, subscribed, now); err != nil {
		return Binding{}, ErrUnavailable
	}
	return Binding{AppID: appID, UserID: userID, OpenID: openID, UnionID: unionID, Version: version, Active: true, Subscribed: subscribed, UpdatedAt: now}, nil
}

func (s *SQLStore) ResolveBinding(ctx context.Context, userID int64, appID string) (Binding, error) {
	if userID <= 0 || appID == "" {
		return Binding{}, ErrInvalidInput
	}
	var b Binding
	err := s.db.QueryRowContext(ctx, `SELECT app_id,user_id,open_id,union_id,version,active,subscribed,updated_at FROM official_account_bindings WHERE app_id=? AND user_id=?`, appID, userID).Scan(&b.AppID, &b.UserID, &b.OpenID, &b.UnionID, &b.Version, &b.Active, &b.Subscribed, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrNotBound
	}
	if err != nil {
		return Binding{}, ErrUnavailable
	}
	if !b.Active {
		return Binding{}, ErrNotBound
	}
	if !b.Subscribed {
		return Binding{}, ErrNotSubscribed
	}
	return b, nil
}

func (s *SQLStore) Unbind(ctx context.Context, userID int64, appID string, now time.Time) error {
	if userID <= 0 || appID == "" || len(appID) > 64 || now.IsZero() {
		return ErrInvalidInput
	}
	result, err := s.db.ExecContext(ctx, `UPDATE official_account_bindings SET active=FALSE,subscribed=FALSE,version=version+1,updated_at=? WHERE app_id=? AND user_id=? AND active=TRUE`, now.UTC(), appID, userID)
	if err != nil {
		return ErrUnavailable
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return ErrUnavailable
	}
	if changed != 1 {
		return ErrNotBound
	}
	return nil
}

func (s *SQLStore) ResolveVersion(ctx context.Context, userID int64, appID string, version int64) (Binding, error) {
	b, err := s.ResolveBinding(ctx, userID, appID)
	if err == nil && b.Version != version {
		return Binding{}, ErrBindingChanged
	}
	return b, err
}

func (s *SQLStore) ApplyEvent(ctx context.Context, event CallbackEvent, now time.Time) (bool, error) {
	if event.DedupKey == "" || len(event.DedupKey) > 128 || event.AppID == "" || len(event.AppID) > 64 || event.OpenID == "" || len(event.OpenID) > 128 || len(event.BindingState) > 128 || (event.Type != EventSubscribe && event.Type != EventUnsubscribe) {
		return false, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, ErrUnavailable
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT IGNORE INTO official_account_callback_events(app_id,event_key,event_type,open_id,created_at) VALUES (?,?,?,?,?)`, event.AppID, event.DedupKey, event.Type, event.OpenID, now.UTC())
	if err != nil {
		return false, ErrUnavailable
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, ErrUnavailable
	}
	if rows == 0 {
		return true, tx.Commit()
	}
	if event.Type == EventSubscribe && event.BindingState != "" {
		if _, err = consumeAndBindTx(ctx, tx, event.BindingState, event.AppID, event.OpenID, "", true, now.UTC()); err != nil {
			return false, err
		}
	} else {
		subscribed := event.Type == EventSubscribe
		if _, err = tx.ExecContext(ctx, `UPDATE official_account_bindings SET subscribed=?,version=version+1,updated_at=? WHERE app_id=? AND open_id=? AND active=TRUE`, subscribed, now.UTC(), event.AppID, event.OpenID); err != nil {
			return false, ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return false, ErrUnavailable
	}
	return false, nil
}
