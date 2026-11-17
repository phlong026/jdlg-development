package route

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"service/notification"
	"service/officialaccount"
	"service/tracking"
)

func TestNoticeTemplateAllowsVisitorSource(t *testing.T) {
	fields, err := parseNoticeTemplateFields(`{"thing1":"visitor","time2":"time"}`)
	if err != nil || fields["thing1"] != "visitor" || fields["time2"] != "time" {
		t.Fatalf("visitor template mapping rejected: fields=%v err=%v", fields, err)
	}
}

func TestNoticeTemplateDoesNotInventConstantSource(t *testing.T) {
	if _, err := parseNoticeTemplateFields(`{"const3":"browse_reason"}`); err == nil {
		t.Fatal("an unverified constant source must not be accepted")
	}
}

func TestNoticeTemplateVisitorIsSingleLineAndTwentyCharacters(t *testing.T) {
	fields := map[string]string{"thing1": "visitor", "time2": "time"}
	notice := notification.Notification{CreatedAt: time.Date(2026, 11, 17, 8, 9, 0, 0, time.UTC)}
	data, err := noticeTemplateData(fields, notice, " 张\n三\t\x00\u200b "+strings.Repeat("名", 30))
	if err != nil || data["time2"] != "2026-11-17 16:09" || len([]rune(data["thing1"])) != 20 {
		t.Fatalf("data=%v err=%v", data, err)
	}
	if !strings.HasPrefix(data["thing1"], "张 三 ") || strings.ContainsAny(data["thing1"], "\r\n\t\x00\u200b") {
		t.Fatalf("visitor is not a single visible line: %q", data["thing1"])
	}
	if _, exists := data["const3"]; exists {
		t.Fatal("unverified constant must not be sent")
	}
	data, err = noticeTemplateData(fields, notice, "\n\t\x00")
	if err != nil || data["thing1"] != "访客" {
		t.Fatalf("empty visitor fallback: data=%v err=%v", data, err)
	}
}

func TestNoticeEventPresentationKeepsPrivateNotificationsAndLegacyDelivery(t *testing.T) {
	for _, kind := range []string{"project_note_open", "project_note_feedback", "project_package_receipt", "project_note_join_request"} {
		for _, fields := range []map[string]string{{"thing1": "visitor"}, {"thing1": "body"}} {
			resolver := func(context.Context, int64, string) (tracking.BrowseNoticePresentation, bool, error) {
				t.Fatal("private project must not resolve public browse identities")
				return tracking.BrowseNoticePresentation{}, false, nil
			}
			presentation, eligible, err := noticeEventPresentation(fields, resolver)(context.Background(), notification.Event{Kind: kind})
			if err != nil || !eligible || presentation.Body != "" || presentation.DeliveryAllowed == visitorTemplateMode(fields) {
				t.Fatalf("kind=%s presentation=%+v eligible=%v err=%v", kind, presentation, eligible, err)
			}
		}
	}
}

func TestNoticeEventPresentationKeepsLegacyBrowseBody(t *testing.T) {
	for _, kind := range []string{"browse", "collection_directory_browse", "collection_content_browse"} {
		calls := 0
		resolver := func(context.Context, int64, string) (tracking.BrowseNoticePresentation, bool, error) {
			calls++
			return tracking.BrowseNoticePresentation{VisitorName: "新访客", ContentTitle: "内容"}, true, nil
		}
		p, eligible, err := noticeEventPresentation(map[string]string{"thing1": "body", "time2": "time"}, resolver)(context.Background(), notification.Event{Kind: kind})
		if err != nil || !eligible || !p.DeliveryAllowed || p.Body != "" || calls != 0 {
			t.Fatalf("legacy kind=%s changed: presentation=%+v eligible=%v err=%v lookups=%d", kind, p, eligible, err, calls)
		}
	}
}

func TestNoticeEventPresentationUsesValidatedDetailsAndThreeStates(t *testing.T) {
	lookupError := errors.New("profile unavailable")
	for _, result := range []struct {
		name     string
		eligible bool
		err      error
	}{
		{name: "allowed", eligible: true},
		{name: "revoked", eligible: false},
		{name: "temporary error", err: lookupError},
	} {
		t.Run(result.name, func(t *testing.T) {
			resolver := func(_ context.Context, actor int64, eventID string) (tracking.BrowseNoticePresentation, bool, error) {
				if actor != 42 || eventID != "event-actual" {
					t.Fatalf("actor=%d event=%s", actor, eventID)
				}
				return tracking.BrowseNoticePresentation{VisitorName: "张先生", ContentTitle: "装修预算"}, result.eligible, result.err
			}
			p, eligible, err := noticeEventPresentation(map[string]string{"thing1": "visitor"}, resolver)(context.Background(), notification.Event{ID: "event-actual", Kind: "browse", RecipientID: 42})
			if eligible != result.eligible || !errors.Is(err, result.err) {
				t.Fatalf("eligible=%v err=%v", eligible, err)
			}
			if result.eligible && (p.Body != "张先生浏览了你分享的《装修预算》" || !p.DeliveryAllowed) {
				t.Fatalf("presentation=%+v", p)
			}
			if !result.eligible && p.Body != "" {
				t.Fatalf("ineligible details leaked into body: %q", p.Body)
			}
		})
	}
}

type browseTemplateSenderStub struct {
	calls   int
	request officialaccount.SendRequest
}

func (s *browseTemplateSenderStub) Send(_ context.Context, request officialaccount.SendRequest) officialaccount.SendResult {
	s.calls++
	s.request = request
	return officialaccount.SendResult{Status: officialaccount.SendAccepted, ProviderMessageID: "provider-test"}
}

func TestNoticeTemplateSendRechecksLatestEventBeforeAnyProviderCall(t *testing.T) {
	for _, test := range []struct {
		name     string
		kind     string
		eligible bool
		err      error
		want     string
		retry    bool
		calls    int
	}{
		{name: "allowed", kind: "browse", eligible: true, want: notification.DeliveryAccepted, calls: 1},
		{name: "collection directory", kind: "collection_directory_browse", eligible: true, want: notification.DeliveryAccepted, calls: 1},
		{name: "collection content", kind: "collection_content_browse", eligible: true, want: notification.DeliveryAccepted, calls: 1},
		{name: "newly revoked", kind: "browse", want: notification.DeliveryCancelled},
		{name: "temporarily unavailable", kind: "browse", err: errors.New("read unavailable"), want: notification.DeliveryFailed, retry: true},
		{name: "private project", kind: "project_note_open", eligible: true, want: notification.DeliveryCancelled},
		{name: "project feedback", kind: "project_note_feedback", eligible: true, want: notification.DeliveryCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			sender := &browseTemplateSenderStub{}
			resolver := func(_ context.Context, actor int64, eventID string) (tracking.BrowseNoticePresentation, bool, error) {
				if !browseNoticeKind(test.kind) {
					t.Fatal("private notification reached browse identity resolver")
				}
				if actor != 42 || eventID != "latest-event" {
					t.Fatalf("wrong durable recipient or merged event: actor=%d event=%s", actor, eventID)
				}
				return tracking.BrowseNoticePresentation{VisitorName: "李女士"}, test.eligible, test.err
			}
			job := notification.DeliveryJob{
				Delivery: notification.Delivery{RecipientID: 42, BindingVersion: 3},
				Notice:   notification.Notification{ID: "notice-original", EventID: "latest-event", Kind: test.kind, CreatedAt: time.Date(2026, 11, 17, 8, 9, 0, 0, time.UTC)},
			}
			result := noticeTemplateSend(sender, map[string]string{"thing1": "visitor", "time2": "time"}, resolver)(context.Background(), job)
			if result.Status != test.want || result.Retryable != test.retry || sender.calls != test.calls {
				t.Fatalf("result=%+v provider calls=%d", result, sender.calls)
			}
			if test.calls == 1 && (sender.request.RecipientID != 42 || sender.request.BindingVersion != 3 || sender.request.Data["thing1"] != "李女士" || sender.request.Data["time2"] != "2026-11-17 16:09" || sender.request.DetailQuery != "notification_id=notice-original") {
				t.Fatalf("request=%+v", sender.request)
			}
		})
	}
}

func TestNoticeTemplateLegacyMappingKeepsExistingSourcesAndDoesNotReadProfiles(t *testing.T) {
	sender := &browseTemplateSenderStub{}
	resolver := func(context.Context, int64, string) (tracking.BrowseNoticePresentation, bool, error) {
		t.Fatal("legacy mapping should not add profile reads at send time")
		return tracking.BrowseNoticePresentation{}, false, nil
	}
	job := notification.DeliveryJob{
		Delivery: notification.Delivery{RecipientID: 42, BindingVersion: 1},
		Notice:   notification.Notification{ID: "notice-1", Kind: "project_note_open", Title: "项目有新访问", Body: strings.Repeat("内", 210)},
	}
	result := noticeTemplateSend(sender, map[string]string{"thing1": "title", "thing2": "body"}, resolver)(context.Background(), job)
	if result.Status != notification.DeliveryAccepted || sender.calls != 1 || sender.request.Data["thing1"] != "项目有新访问" || len([]rune(sender.request.Data["thing2"])) != 200 {
		t.Fatalf("result=%+v request=%+v", result, sender.request)
	}
}
