package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	f.tokenOut = `{"errcode":40013,"errmsg":"invalid appid"}`
	if _, err = WechatURLLink(context.Background(), api, "app-id", "app-secret", "pages/a/a", "q=1", "release", time.Now()); !errors.As(err, &we) || we.Code != 40013 {
		t.Fatalf("token failure: %v", err)
	}
	f.tokenOut = `{"access_token":""}`
	if _, err = WechatURLLink(context.Background(), api, "app-id", "app-secret", "pages/a/a", "q=1", "release", time.Now()); err == nil {
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
