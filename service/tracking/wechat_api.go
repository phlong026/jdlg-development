package tracking

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const wechatAPIBase = "https://api.weixin.qq.com"

// WechatError is a business error returned by the WeChat API (errcode != 0). The
// code is safe to log and to show an operator; the message and any credential
// never leave this package through it.
type WechatError struct{ Code int }

func (e *WechatError) Error() string { return fmt.Sprintf("wechat api errcode %d", e.Code) }

// WechatAPI is the shared client for the official WeChat endpoints used here. It
// has an explicit timeout, refuses redirects, bounds every response, and keeps
// credentials out of error values. Base is overridable only so tests can point
// it at a local server; production always uses api.weixin.qq.com.
type WechatAPI struct {
	Base   string
	Client *http.Client
	// SharedTokens routes an AppID whose access_token is also used elsewhere in
	// the process (the mini-program: payment, profile, QR codes) through that
	// shared cache. WeChat keeps one live token per app, so a second independent
	// fetch retires the first one after a short grace period and two caches for
	// the same app would keep breaking each other's calls.
	SharedTokens map[string]TokenFunc

	mu     sync.Mutex
	tokens map[string]cachedToken
	now    func() time.Time // tests only; nil means time.Now
}

// TokenFunc returns a currently valid access_token for one app.
type TokenFunc func(ctx context.Context, appID, secret string) (string, error)

type cachedToken struct {
	value   string
	secret  string // digest, so a rotated secret is never served a stale token
	expires time.Time
}

// tokenRefreshMargin renews a token this long before WeChat expires it, so a
// request that starts just before expiry never carries a dead token.
const tokenRefreshMargin = 5 * time.Minute

func NewWechatAPI() *WechatAPI {
	return &WechatAPI{Base: wechatAPIBase, Client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// errResponseTooLarge is returned when a reply is bigger than the caller allowed.
// It still satisfies errors.Is(err, ErrUnavailable), so existing callers treat it
// as any other unusable reply, but a caller that can ask for less may tell it apart.
var errResponseTooLarge error = responseTooLargeError{}

type responseTooLargeError struct{}

func (responseTooLargeError) Error() string        { return "wechat response too large" }
func (responseTooLargeError) Is(target error) bool { return target == ErrUnavailable }

func (w *WechatAPI) do(req *http.Request, limit int64) ([]byte, error) {
	res, err := w.Client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || res.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	if int64(len(b)) > limit {
		return nil, errResponseTooLarge
	}
	var status struct {
		Code int `json:"errcode"`
	}
	if json.Unmarshal(b, &status) != nil {
		return nil, ErrUnavailable
	}
	if status.Code != 0 {
		return nil, &WechatError{Code: status.Code}
	}
	return b, nil
}

// Token returns an access_token for appID. A token is fetched at most once per
// lifetime and reused, so publishing, link generation and sync runs do not each
// spend the daily /cgi-bin/token quota or retire a token another caller holds.
// Failures are never cached, and no error carries the credential.
func (w *WechatAPI) Token(ctx context.Context, appID, secret string) (string, error) {
	if appID == "" || secret == "" {
		return "", ErrUnavailable
	}
	if source := w.SharedTokens[appID]; source != nil {
		token, err := source(ctx, appID, secret)
		if err != nil || token == "" {
			// The shared source's error may quote its request URL, which holds the secret.
			return "", ErrUnavailable
		}
		return token, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	if w.now != nil {
		now = w.now()
	}
	digest := sha256.Sum256([]byte(secret))
	fingerprint := hex.EncodeToString(digest[:])
	if cached, ok := w.tokens[appID]; ok && cached.secret == fingerprint && cached.expires.After(now.Add(tokenRefreshMargin)) {
		return cached.value, nil
	}
	q := url.Values{"grant_type": {"client_credential"}, "appid": {appID}, "secret": {secret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.Base+"/cgi-bin/token?"+q.Encode(), nil)
	if err != nil {
		return "", ErrUnavailable
	}
	b, err := w.do(req, 64<<10)
	if err != nil {
		return "", err
	}
	var out struct {
		Token     string `json:"access_token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if json.Unmarshal(b, &out) != nil || out.Token == "" {
		return "", ErrUnavailable
	}
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 7200
	}
	if w.tokens == nil {
		w.tokens = map[string]cachedToken{}
	}
	w.tokens[appID] = cachedToken{value: out.Token, secret: fingerprint, expires: now.Add(time.Duration(out.ExpiresIn) * time.Second)}
	return out.Token, nil
}

// PostJSON posts payload to path and decodes a successful response into out.
func (w *WechatAPI) PostJSON(ctx context.Context, path, token string, payload, out any, limit int64) error {
	if token == "" || !strings.HasPrefix(path, "/") {
		return ErrUnavailable
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.Base+path+"?access_token="+url.QueryEscape(token), bytes.NewReader(body))
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	b, err := w.do(req, limit)
	if err != nil {
		return err
	}
	if json.Unmarshal(b, out) != nil {
		return ErrUnavailable
	}
	return nil
}

// URLLinkValidDays is the lifetime requested for a mini-program URL Link. The
// platform allows at most 30 days; the share token behind the link also lives 30
// days and is created first, so the link always ends before the token does.
const URLLinkValidDays = 29

// URLLinkResult is a generated link and the latest time it can still open.
type URLLinkResult struct {
	URL     string    `json:"url_link"`
	Expires time.Time `json:"expires_at"`
}

// WechatURLLink asks WeChat for a mini-program URL Link to page with query. The
// returned link is checked to be a plain https URL before it is handed on.
func WechatURLLink(ctx context.Context, api *WechatAPI, appID, secret, page, query, environment string, now time.Time) (URLLinkResult, error) {
	var out URLLinkResult
	if page == "" || strings.HasPrefix(page, "/") || strings.Contains(page, "?") || len(query) > 1024 || (environment != "release" && environment != "trial" && environment != "develop") {
		return out, ErrUnavailable
	}
	token, err := api.Token(ctx, appID, secret)
	if err != nil {
		return out, err
	}
	var res struct {
		Link string `json:"url_link"`
	}
	payload := map[string]any{"path": page, "query": query, "env_version": environment, "expire_type": 1, "expire_interval": URLLinkValidDays}
	if err = api.PostJSON(ctx, "/wxa/generate_urllink", token, payload, &res, 64<<10); err != nil {
		return out, err
	}
	link, perr := url.Parse(res.Link)
	if perr != nil || link.Scheme != "https" || link.Hostname() == "" || link.User != nil || len(res.Link) > 512 {
		return out, errors.New("url link response invalid")
	}
	return URLLinkResult{URL: res.Link, Expires: now.UTC().Add(URLLinkValidDays * 24 * time.Hour)}, nil
}
