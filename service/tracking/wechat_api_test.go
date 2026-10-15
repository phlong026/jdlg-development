package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeWechat struct {
	t        *testing.T
	server   *httptest.Server
	tokens   int
	requests []string
	bodies   []map[string]any
	linkJSON string
	tokenOut string
}

func newFakeWechat(t *testing.T) (*fakeWechat, *WechatAPI) {
	f := &fakeWechat{t: t, tokenOut: `{"access_token":"token-123","expires_in":7200}`, linkJSON: `{"errcode":0,"errmsg":"ok","url_link":"https://wxaurl.cn/abc123"}`}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/cgi-bin/token":
			f.tokens++
			q := r.URL.Query()
			if q.Get("grant_type") != "client_credential" || q.Get("appid") != "app-id" || q.Get("secret") != "app-secret" {
				t.Errorf("bad token request %v", q)
			}
			io.WriteString(w, f.tokenOut)
		case "/wxa/generate_urllink":
			if r.Method != "POST" || r.URL.Query().Get("access_token") != "token-123" || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("bad link request %s %s", r.Method, r.URL.String())
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.bodies = append(f.bodies, body)
			io.WriteString(w, f.linkJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	api := NewWechatAPI()
	api.Base = f.server.URL
	return f, api
}

func TestURLLinkRequestAndResult(t *testing.T) {
	f, api := newFakeWechat(t)
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	got, err := WechatURLLink(context.Background(), api, "app-id", "app-secret", "pages/contentreader/contentreader", "content_id=c&st=t", "release", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://wxaurl.cn/abc123" || !got.Expires.Equal(now.Add(29*24*time.Hour)) {
		t.Fatalf("result: %+v", got)
	}
	body := f.bodies[0]
	if body["path"] != "pages/contentreader/contentreader" || body["query"] != "content_id=c&st=t" || body["env_version"] != "release" || body["expire_type"].(float64) != 1 || body["expire_interval"].(float64) != 29 {
		t.Fatalf("request body: %v", body)
	}
	if f.tokens != 1 {
		t.Fatalf("expected one token request, got %d", f.tokens)
	}
}

func TestURLLinkRefusesInvalidInputBeforeAnyRequest(t *testing.T) {
	f, api := newFakeWechat(t)
	now := time.Now()
	for name, args := range map[string][]string{
		"leading slash": {"/pages/a/a", "q=1", "release"},
		"query in path": {"pages/a/a?x=1", "q=1", "release"},
		"empty page":    {"", "q=1", "release"},
		"long query":    {"pages/a/a", strings.Repeat("a", 1025), "release"},
		"bad env":       {"pages/a/a", "q=1", "production"},
	} {
		if _, err := WechatURLLink(context.Background(), api, "app-id", "app-secret", args[0], args[1], args[2], now); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if len(f.requests) != 0 {
		t.Fatalf("invalid input reached WeChat: %v", f.requests)
	}
}

func TestWechatAPIErrorsAreTypedAndKeepSecretsOut(t *testing.T) {
	f, api := newFakeWechat(t)
	f.linkJSON = `{"errcode":40165,"errmsg":"invalid weapp pagepath hint: secret=app-secret"}`
	_, err := WechatURLLink(context.Background(), api, "app-id", "app-secret", "pages/a/a", "q=1", "release", time.Now())
	var we *WechatError
	if !errors.As(err, &we) || we.Code != 40165 {
		t.Fatalf("expected typed WeChat error, got %v", err)
	}
	if strings.Contains(err.Error(), "app-secret") || strings.Contains(err.Error(), "token-123") {
		t.Fatalf("credential leaked through the error: %v", err)
	}
	// A valid token is cached, so each token failure below needs a client that
	// has none yet.
	fresh := func() *WechatAPI {
		client := NewWechatAPI()
		client.Base = f.server.URL
		return client
	}
	f.tokenOut = `{"errcode":40013,"errmsg":"invalid appid"}`
	if _, err = WechatURLLink(context.Background(), fresh(), "app-id", "app-secret", "pages/a/a", "q=1", "release", time.Now()); !errors.As(err, &we) || we.Code != 40013 {
		t.Fatalf("token failure: %v", err)
	}
	f.tokenOut = `{"access_token":""}`
	if _, err = WechatURLLink(context.Background(), fresh(), "app-id", "app-secret", "pages/a/a", "q=1", "release", time.Now()); err == nil {
		t.Fatal("empty token accepted")
	}
	if _, err = WechatURLLink(context.Background(), api, "", "", "pages/a/a", "q=1", "release", time.Now()); err == nil {
		t.Fatal("missing credentials accepted")
	}
}

func TestURLLinkResponseMustBePlainHTTPS(t *testing.T) {
	f, api := newFakeWechat(t)
	for _, link := range []string{"http://wxaurl.cn/a", "javascript:alert(1)", "https://user:pw@wxaurl.cn/a", "", "https:///a", "https://wxaurl.cn/" + strings.Repeat("a", 600)} {
		f.linkJSON = `{"errcode":0,"url_link":` + string(mustJSON(link)) + `}`
		if got, err := WechatURLLink(context.Background(), api, "app-id", "app-secret", "pages/a/a", "q=1", "release", time.Now()); err == nil {
			t.Fatalf("accepted %q as %+v", link, got)
		}
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestWechatAPIDoesNotFollowRedirectsOrReadUnboundedBodies(t *testing.T) {
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; io.WriteString(w, `{"access_token":"stolen"}`) }))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirector.Close()
	api := NewWechatAPI()
	api.Base = redirector.URL
	if _, err := api.Token(context.Background(), "app-id", "app-secret"); err == nil || hits != 0 {
		t.Fatalf("redirect followed (hits=%d, err=%v)", hits, err)
	}
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"access_token":"`+strings.Repeat("a", 200<<10)+`"}`)
	}))
	defer big.Close()
	api.Base = big.URL
	if _, err := api.Token(context.Background(), "app-id", "app-secret"); err == nil {
		t.Fatal("oversized response accepted")
	}
}

// tokenServer counts /cgi-bin/token requests and issues numbered tokens, so a
// test can tell a reused token from a fresh one.
func tokenServer(t *testing.T, expiresIn int) (*WechatAPI, *int32, *[]string) {
	t.Helper()
	var calls int32
	var secrets []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		mu.Lock()
		secrets = append(secrets, r.URL.Query().Get("secret"))
		mu.Unlock()
		io.WriteString(w, fmt.Sprintf(`{"access_token":"token-%d","expires_in":%d}`, n, expiresIn))
	}))
	t.Cleanup(server.Close)
	api := NewWechatAPI()
	api.Base = server.URL
	return api, &calls, &secrets
}

func TestAccessTokenIsFetchedOnceAndReused(t *testing.T) {
	api, calls, _ := tokenServer(t, 7200)
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	api.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		token, err := api.Token(context.Background(), "app-id", "app-secret")
		if err != nil || token != "token-1" {
			t.Fatalf("call %d: %q %v", i, token, err)
		}
	}
	if *calls != 1 {
		t.Fatalf("a valid token must be reused, but /cgi-bin/token was called %d times", *calls)
	}
}

func TestAccessTokenIsRenewedBeforeItExpires(t *testing.T) {
	api, calls, _ := tokenServer(t, 7200)
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	api.now = func() time.Time { return now }
	first, _ := api.Token(context.Background(), "app-id", "app-secret")
	now = now.Add(7200*time.Second - tokenRefreshMargin - time.Second)
	if again, _ := api.Token(context.Background(), "app-id", "app-secret"); again != first || *calls != 1 {
		t.Fatalf("still inside its lifetime, so it must be reused: %q calls=%d", again, *calls)
	}
	now = now.Add(2 * time.Second) // now inside the refresh margin
	renewed, err := api.Token(context.Background(), "app-id", "app-secret")
	if err != nil || renewed == first || *calls != 2 {
		t.Fatalf("a token inside the refresh margin must be renewed: %q %v calls=%d", renewed, err, *calls)
	}
}

func TestAccessTokenIsKeptPerAppAndNeverServedAfterASecretChange(t *testing.T) {
	api, calls, secrets := tokenServer(t, 7200)
	api.now = func() time.Time { return time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC) }
	a, _ := api.Token(context.Background(), "app-a", "secret-a")
	b, _ := api.Token(context.Background(), "app-b", "secret-b")
	if a == b || *calls != 2 {
		t.Fatalf("two apps must not share a token: %q %q calls=%d", a, b, *calls)
	}
	if again, _ := api.Token(context.Background(), "app-a", "secret-a"); again != a || *calls != 2 {
		t.Fatalf("each app keeps its own cached token: %q calls=%d", again, *calls)
	}
	rotated, _ := api.Token(context.Background(), "app-a", "secret-a2")
	if rotated == a || *calls != 3 || (*secrets)[2] != "secret-a2" {
		t.Fatalf("a rotated secret must fetch a fresh token: %q calls=%d secrets=%v", rotated, *calls, *secrets)
	}
}

func TestAccessTokenFailuresAreNotCached(t *testing.T) {
	f, api := newFakeWechat(t)
	f.tokenOut = `{"errcode":40013,"errmsg":"invalid appid"}`
	var wechat *WechatError
	if _, err := api.Token(context.Background(), "app-id", "app-secret"); !errors.As(err, &wechat) || wechat.Code != 40013 {
		t.Fatalf("expected a typed WeChat error: %v", err)
	}
	f.tokenOut = `{"access_token":"token-123","expires_in":7200}`
	token, err := api.Token(context.Background(), "app-id", "app-secret")
	if err != nil || token != "token-123" || f.tokens != 2 {
		t.Fatalf("a failure must not poison the cache: %q %v requests=%d", token, err, f.tokens)
	}
	f.tokenOut = `{"access_token":"","expires_in":7200}`
	api2 := NewWechatAPI()
	api2.Base = f.server.URL
	if _, err = api2.Token(context.Background(), "app-id", "app-secret"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("an empty token is a failure: %v", err)
	}
}

func TestAccessTokenConcurrentCallersShareOneRequest(t *testing.T) {
	api, calls, _ := tokenServer(t, 7200)
	var wg sync.WaitGroup
	tokens := make([]string, 12)
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tokens[i], _ = api.Token(context.Background(), "app-id", "app-secret")
		}(i)
	}
	wg.Wait()
	if *calls != 1 {
		t.Fatalf("concurrent callers asked WeChat %d times", *calls)
	}
	for _, token := range tokens {
		if token != "token-1" {
			t.Fatalf("callers got different tokens: %v", tokens)
		}
	}
}

// The mini-program's token is cached by the rest of the application. Tracking must
// take it from there, never fetch its own, and must not surface the secret that a
// shared source's error may quote.
func TestSharedAppTokenComesFromTheSharedSourceOnly(t *testing.T) {
	api, calls, _ := tokenServer(t, 7200)
	var asked []string
	api.SharedTokens = map[string]TokenFunc{"mini-app": func(_ context.Context, appID, secret string) (string, error) {
		asked = append(asked, appID+"/"+secret)
		return "shared-token", nil
	}}
	token, err := api.Token(context.Background(), "mini-app", "mini-secret")
	if err != nil || token != "shared-token" || *calls != 0 || len(asked) != 1 || asked[0] != "mini-app/mini-secret" {
		t.Fatalf("the shared app must use the shared source only: %q %v requests=%d asked=%v", token, err, *calls, asked)
	}
	// Another app (the official account) is not affected by the shared mapping.
	other, err := api.Token(context.Background(), "official-app", "official-secret")
	if err != nil || other != "token-1" || *calls != 1 || len(asked) != 1 {
		t.Fatalf("an app without a shared source uses the client's own cache: %q %v requests=%d", other, err, *calls)
	}
	api.SharedTokens["mini-app"] = func(context.Context, string, string) (string, error) {
		return "", errors.New(`Get "https://api.weixin.qq.com/cgi-bin/token?appid=mini-app&secret=mini-secret": dial tcp: timeout`)
	}
	_, err = api.Token(context.Background(), "mini-app", "mini-secret")
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "mini-secret") {
		t.Fatalf("a failing shared source must not leak the secret: %v", err)
	}
}
