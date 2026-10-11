package reportwecom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestProviderUsesCustomerContactEndpointsAndOneShotWelcomeCode(t *testing.T) {
	p := NewHTTPProvider(Config{Enabled: true, CorpID: "corp", Secret: "test-secret", EmployeeID: "advisor", AppID: "wx-test"})
	p.mediaID = "thumbnail"
	p.mediaUntil = time.Now().Add(time.Hour)
	tokenCalls := 0
	p.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "qyapi.weixin.qq.com" || r.URL.Scheme != "https" {
			t.Fatal("untrusted endpoint")
		}
		body := ""
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			tokenCalls++
			body = `{"errcode":0,"access_token":"test-token","expires_in":7200}`
		case "/cgi-bin/externalcontact/add_contact_way":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["state"] != "state" || req["scene"] != float64(2) {
				t.Fatal("wrong contact way")
			}
			body = `{"errcode":0,"config_id":"way","qr_code":"https://work.weixin.qq.com/qr"}`
		case "/cgi-bin/externalcontact/get":
			body = `{"errcode":0,"external_contact":{"external_userid":"external","unionid":"union"},"follow_user":[{"userid":"advisor"}]}`
		case "/cgi-bin/externalcontact/send_welcome_msg":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["welcome_code"] != "welcome" || req["external_userid"] != nil {
				t.Fatal("welcome must not masquerade as delayed direct messaging")
			}
			body = `{"errcode":0}`
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	if _, err := p.CreateContactWay(context.Background(), "state"); err != nil {
		t.Fatal(err)
	}
	contact, err := p.GetExternalContact(context.Background(), "external")
	if err != nil || contact.UnionID != "union" {
		t.Fatal(err)
	}
	if err := p.SendWelcome(context.Background(), "welcome", "report"); err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 {
		t.Fatal("access token was not cached")
	}
}
func TestProviderErrorIsSanitized(t *testing.T) {
	p := NewHTTPProvider(Config{Enabled: true})
	p.token = "token"
	p.tokenUntil = time.Now().Add(time.Hour)
	p.client = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"errcode":40001,"errmsg":"sensitive-provider-response"}`))}, nil
	})}
	if _, err := p.GetExternalContact(context.Background(), "ext"); err != ErrProvider {
		t.Fatalf("error classification %v", err)
	}
}

func TestWelcomeRequiresAnExplicitProviderReceipt(t *testing.T) {
	p := NewHTTPProvider(Config{Enabled: true})
	p.token, p.mediaID = "token", "thumbnail"
	p.tokenUntil, p.mediaUntil = time.Now().Add(time.Hour), time.Now().Add(time.Hour)
	for _, body := range []string{`{}`, `null`, `{"errcode":null}`} {
		p.client = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if err := p.SendWelcome(context.Background(), "welcome", "report"); err == nil {
			t.Fatalf("missing delivery receipt must enter manual reconciliation: %s", body)
		}
	}
}
