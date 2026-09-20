package reportnotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayUsesStableKeyAndAuthoritativeLookup(t *testing.T) {
	event := EventID("report-1")
	var methods []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Header.Get("Idempotency-Key") != event || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("gateway identity headers missing")
		}
		if r.Method == http.MethodPost {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload) != 6 || payload["mini_program_path"] != "pages/report/delivery?id=report-1" {
				t.Errorf("unexpected gateway payload fields: %+v", payload)
			}
		} else if r.URL.Path != "/deliveries/"+event {
			t.Errorf("lookup path=%s", r.URL.Path)
		}
		fmt.Fprint(w, `{"status":"sent","receipt":"receipt-confirmed"}`)
	}))
	defer server.Close()
	provider, err := NewHTTPProvider(Config{Endpoint: server.URL + "/deliveries", Token: "test-token"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	notice := Notification{EventID: event, ReportID: "report-1", UserID: 7, ExternalUserID: "owner",
		MiniProgramPath: "pages/report/delivery?id=report-1", Text: "报告已就绪"}
	if receipt, err := provider.Send(context.Background(), notice); err != nil || receipt.Receipt != "receipt-confirmed" {
		t.Fatalf("send=%+v %v", receipt, err)
	}
	if receipt, err := provider.Lookup(context.Background(), event); err != nil || receipt.Status != StateSent {
		t.Fatalf("lookup=%+v %v", receipt, err)
	}
	if strings.Join(methods, ",") != "POST,GET" {
		t.Fatalf("requests=%v", methods)
	}
}

func TestGatewayRejectsRedirectsAndNeverLeaksCredentials(t *testing.T) {
	reached := false
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	provider, err := NewHTTPProvider(Config{Endpoint: source.URL, Token: "test-token"}, source.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Lookup(context.Background(), EventID("report-1"))
	if !errors.Is(err, ErrUncertain) || reached {
		t.Fatalf("redirect reached=%v error=%v", reached, err)
	}
}

func TestGatewayOnlyAcceptsBoundedUnambiguousSentReceipt(t *testing.T) {
	for _, body := range []string{
		`{"status":"sent"}`, `{"status":"pending"}`, `{"status":"sent","receipt":"ok"}{}`,
		`{"status":"sent","receipt":"ok","private_details":"unexpected"}`,
		strings.Repeat("x", maxGatewayResponse+1),
	} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			provider, err := NewHTTPProvider(Config{Endpoint: server.URL, Token: "test-token"}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = provider.Lookup(context.Background(), EventID("report-1")); !errors.Is(err, ErrUncertain) {
				t.Fatalf("ambiguous response accepted: %v", err)
			}
		})
	}
}

func TestCanceledGatewayCallIsUncertainAndConfigurationIsBounded(t *testing.T) {
	provider, err := NewHTTPProvider(configured(), &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.client.Timeout != gatewayTimeout {
		t.Fatal("gateway timeout changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = provider.Lookup(ctx, EventID("report-1")); !errors.Is(err, ErrUncertain) {
		t.Fatalf("canceled result=%v", err)
	}
}
