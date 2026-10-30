package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNoticeRecipientIsActualMiniProgramSharer(t *testing.T) {
	recipient, err := NoticeRecipient(Share{Owner: "company", Sharer: "u:42", Origin: "miniprogram"})
	if err != nil || recipient != 42 {
		t.Fatalf("recipient=%d error=%v", recipient, err)
	}
	for _, share := range []Share{
		{Owner: "u:99", Sharer: "a:anonymous", Origin: "miniprogram"},
		{Owner: "u:99", Sharer: "u:42", Origin: "console"},
		{Owner: "u:99", Sharer: "42", Origin: "miniprogram"},
		{Owner: "u:99", Sharer: "u:+42", Origin: "miniprogram"},
	} {
		if _, err := NoticeRecipient(share); err == nil {
			t.Fatalf("unverified recipient accepted: %+v", share)
		}
	}
}

func TestBuildBrowseNoticeRejectsDisabledSelfPreviewAndUnattributed(t *testing.T) {
	share := Share{ID: "share-0001", Owner: "owner-a", Sharer: "u:42", Origin: "miniprogram", Expires: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)}
	event := Event{ID: "event-0001", Type: "page_view", View: "view-0001", Occurred: time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)}
	entry := Entry{Owner: "owner-a", Visitor: "u:7", Share: share.ID, Attribution: "share"}

	for name, mutate := range map[string]func(*Entry, *Event, *NoticeRules){
		"disabled":      func(_ *Entry, _ *Event, rules *NoticeRules) { rules.BrowseEnabled = false },
		"self":          func(entry *Entry, _ *Event, _ *NoticeRules) { entry.Self = true },
		"preview":       func(entry *Entry, _ *Event, _ *NoticeRules) { entry.Preview = true },
		"direct":        func(entry *Entry, _ *Event, _ *NoticeRules) { entry.Attribution = "unattributed" },
		"non page view": func(_ *Entry, event *Event, _ *NoticeRules) { event.Type = "page_end" },
	} {
		t.Run(name, func(t *testing.T) {
			gotEntry, gotEvent, rules := entry, event, NoticeRules{BrowseEnabled: true}
			mutate(&gotEntry, &gotEvent, &rules)
			if _, ok, err := BuildBrowseNotice(gotEntry, gotEvent, share, rules); err != nil || ok {
				t.Fatalf("ok=%v err=%v", ok, err)
			}
		})
	}
}

func TestAcceptWritesNoticeEventOnceInSameTransaction(t *testing.T) {
	s, _ := fixture(t)
	s.NoticeRules.BrowseEnabled = true
	sharer := enter(t, s, 42)
	created, err := s.CreateShare(context.Background(), ShareInput{Route: sharer.Route, Key: "request-notice-0001"}, sharer.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := s.Enter(context.Background(), EntryInput{Route: sharer.Route, Client: "visitor-client-0001", ShareToken: created["share_token"].(string), Consent: "v1"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	pageView := ev(s, visitor, "event-notice-0001", "page_view", s.Now())
	results := accept(t, s, visitor, pageView, pageView)
	if results[0].Status != "accepted" || results[1].Status != "duplicate" {
		t.Fatal(results)
	}
	rows := s.Store.(*memoryStore).data[s.Config.Tenant]
	row, ok := rows["notice_event:"+pageView.ID]
	if !ok {
		t.Fatal("accepted page view did not create notice event")
	}
	var notice NoticeEvent
	if err := json.Unmarshal(row.Payload, &notice); err != nil {
		t.Fatal(err)
	}
	if notice.RecipientID != 42 || notice.VisitorKey != "u:7" || notice.ShareID != created["share_id"] {
		t.Fatalf("wrong notice: %+v", notice)
	}
}

type failNoticeStore struct{ Store }
type failNoticeRepo struct{ Repository }

func (s failNoticeStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.Store.Transaction(ctx, tenant, func(repo Repository) error { return fn(failNoticeRepo{repo}) })
}
func (r failNoticeRepo) Insert(row Record) error {
	if row.Kind == "notice_event" {
		return errors.New("notice unavailable")
	}
	return r.Repository.Insert(row)
}

func TestAcceptRollsBackEventWhenNoticeEventFails(t *testing.T) {
	s, _ := fixture(t)
	s.NoticeRules.BrowseEnabled = true
	sharer := enter(t, s, 42)
	created, err := s.CreateShare(context.Background(), ShareInput{Route: sharer.Route, Key: "request-notice-0002"}, sharer.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := s.Enter(context.Background(), EntryInput{Route: sharer.Route, Client: "visitor-client-0002", ShareToken: created["share_token"].(string), Consent: "v1"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	base := s.Store
	s.Store = failNoticeStore{base}
	pageView := ev(s, visitor, "event-notice-0002", "page_view", s.Now())
	if _, err := s.Accept(context.Background(), visitor.Token, []Event{pageView}); err == nil {
		t.Fatal("notice insert failure did not abort accept")
	}
	rows := base.(*memoryStore).data[s.Config.Tenant]
	if _, ok := rows["event:"+pageView.ID]; ok {
		t.Fatal("event committed without its notice event")
	}
}
