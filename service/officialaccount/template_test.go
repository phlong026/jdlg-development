package officialaccount

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"service/notification"
	"service/tracking"
)

func TestTemplateSenderAcceptedRejectedAndUnknown(t *testing.T) {
	for _, test := range []struct {
		name       string
		response   string
		delay      time.Duration
		wantStatus SendStatus
		wantID     string
	}{
		{name: "accepted", response: `{"errcode":0,"errmsg":"ok","msgid":12345}`, wantStatus: SendAccepted, wantID: "12345"},
		{name: "rejected", response: `{"errcode":43004,"errmsg":"require subscribe"}`, wantStatus: SendFailed},
		{name: "timeout", delay: 100 * time.Millisecond, response: `{"errcode":0,"errmsg":"ok","msgid":9}`, wantStatus: SendUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sent struct {
				MiniProgram struct {
					AppID    string `json:"appid"`
					PagePath string `json:"pagepath"`
				} `json:"miniprogram"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/cgi-bin/token" && r.URL.Query().Get("access_token") != "provider-token" {
					t.Errorf("unexpected query %q", r.URL.RawQuery)
				}
				if r.URL.Path == "/cgi-bin/token" {
					_, _ = w.Write([]byte(`{"errcode":0,"access_token":"provider-token","expires_in":7200}`))
					return
				}
				if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
					t.Errorf("decode payload: %v", err)
				}
				time.Sleep(test.delay)
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()
			api := tracking.NewWechatAPI()
			api.Base = server.URL
			api.Client.Timeout = 25 * time.Millisecond
			store := NewMemoryStore()
			intent, _ := store.CreateIntent(context.Background(), 42, time.Now(), time.Minute)
			binding, _ := store.ConsumeAndBind(context.Background(), intent.State, "wx-official", "official-openid", "", true, time.Now())
			sender := NewSender(Config{Enabled: true, AppID: "wx-official", AppSecret: "official-secret", TemplateID: "template-1", PublicOrigin: "https://www.jiadilingguang.com", MiniProgramAppID: "wx-mini", MiniProgramPagePath: "pages/sharevisits/detail"}, store, api)
			result := sender.Send(context.Background(), SendRequest{RecipientID: 42, BindingVersion: binding.Version, DetailQuery: "notification_id=event-1", Data: map[string]string{"verified_field": "客户查看了项目"}})
			if result.Status != test.wantStatus || result.ProviderMessageID != test.wantID {
				t.Fatalf("result = %+v", result)
			}
			if strings.Contains(result.ErrorCode, "secret") || strings.Contains(result.ErrorCode, "provider-token") {
				t.Fatalf("credential leaked: %+v", result)
			}
			if test.wantStatus == SendAccepted && (sent.MiniProgram.AppID != "wx-mini" || sent.MiniProgram.PagePath != "pages/sharevisits/detail?notification_id=event-1") {
				t.Fatalf("mini program target = %+v", sent.MiniProgram)
			}
		})
	}
}

type captureDeliverySender struct{ request SendRequest }

func (s *captureDeliverySender) Send(_ context.Context, request SendRequest) SendResult {
	s.request = request
	return SendResult{Status: SendAccepted, ProviderMessageID: "provider-1"}
}

func TestDeliveryAdapterUsesDurableRecipientAndBindingVersion(t *testing.T) {
	sender := &captureDeliverySender{}
	adapter := DeliveryAdapter(sender, func(context.Context, notification.DeliveryJob) (SendRequest, error) {
		return SendRequest{RecipientID: 999, BindingVersion: 999, DetailQuery: "notification_id=event-1", Data: map[string]string{"verified_field": "value"}}, nil
	})
	result := adapter(context.Background(), notification.DeliveryJob{Delivery: notification.Delivery{RecipientID: 42, BindingVersion: 7}})
	if result.Status != notification.DeliveryAccepted || result.ProviderMessageID != "provider-1" || sender.request.RecipientID != 42 || sender.request.BindingVersion != 7 {
		t.Fatalf("result=%+v request=%+v", result, sender.request)
	}
}

func TestTemplateSenderRejectsWrongRecipientVersionBeforeHTTP(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now()
	intent, _ := store.CreateIntent(context.Background(), 42, now, time.Minute)
	binding, _ := store.ConsumeAndBind(context.Background(), intent.State, "wx-official", "official-openid", "", true, now)
	sender := NewSender(Config{Enabled: true, AppID: "wx-official", AppSecret: "secret", TemplateID: "template", PublicOrigin: "https://www.jiadilingguang.com", MiniProgramAppID: "wx-mini", MiniProgramPagePath: "pages/sharevisits/detail"}, store, tracking.NewWechatAPI())
	result := sender.Send(context.Background(), SendRequest{RecipientID: 42, BindingVersion: binding.Version + 1, DetailQuery: "notification_id=event-1", Data: map[string]string{"verified_field": "x"}})
	if result.Status != SendFailed || result.ErrorCode != "binding_changed" {
		t.Fatalf("result = %+v", result)
	}
}

func TestTemplateSenderTokenFailureIsRetryableFailedBeforeSend(t *testing.T) {
	var sendCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/token" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		sendCalls++
	}))
	defer server.Close()
	api := tracking.NewWechatAPI()
	api.Base = server.URL
	store := NewMemoryStore()
	now := time.Now()
	intent, _ := store.CreateIntent(context.Background(), 42, now, time.Minute)
	binding, _ := store.ConsumeAndBind(context.Background(), intent.State, "wx-official", "official-openid", "", true, now)
	sender := NewSender(Config{Enabled: true, AppID: "wx-official", AppSecret: "secret", TemplateID: "template", PublicOrigin: "https://www.jiadilingguang.com", MiniProgramAppID: "wx-mini", MiniProgramPagePath: "pages/sharevisits/detail"}, store, api)
	result := sender.Send(context.Background(), SendRequest{RecipientID: 42, BindingVersion: binding.Version, DetailQuery: "notification_id=n1", Data: map[string]string{"field": "value"}})
	if result.Status != SendFailed || !result.Retryable || result.ErrorCode != "token_unavailable" || sendCalls != 0 {
		t.Fatalf("result=%+v sends=%d", result, sendCalls)
	}
}
