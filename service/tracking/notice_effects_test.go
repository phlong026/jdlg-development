package tracking

import (
	"context"
	"errors"
	"testing"
)

func TestNoticeJobFailureRollsBackTrackingEvent(t *testing.T) {
	s, _ := fixture(t)
	s.NoticeRules.BrowseEnabled = true
	sender := enter(t, s, 42)
	share, err := s.CreateShare(context.Background(), ShareInput{Route: sender.Route, Key: "notice-job-share"}, sender.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := s.Enter(context.Background(), EntryInput{Route: sender.Route, Client: "notice-job-client", ShareToken: share["share_token"].(string), Consent: "v1"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.OnNoticeEvent = func(context.Context, NoticeEvent) error { calls++; return errors.New("projection unavailable") }
	event := ev(s, visitor, "notice-job-event", "page_view", s.Now())
	if _, err = s.Accept(context.Background(), visitor.Token, []Event{event}); err == nil {
		t.Fatal("job failure accepted")
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	for _, kind := range []string{"event", "notice_event"} {
		if _, ok := s.Store.(*memoryStore).data[s.Config.Tenant][kind+":"+event.ID]; ok {
			t.Fatal("committed without job", kind)
		}
	}
}
func TestNoticePrivacyCleanupPrecedesRawRemovalAndFailureRollsBack(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(map[bool]string{false: "atomic", true: "staged"}[staged], func(t *testing.T) {
			s, id := noticeEligibilityFixture(t)
			_, scope := fixture(t)
			s.forceStagedDeletion = staged
			called := false
			s.OnRemoveNoticeEvents = func(_ context.Context, ids []string) error {
				for _, v := range ids {
					if v == id {
						called = true
					}
				}
				return errors.New("projection delete unavailable")
			}
			if _, err := s.DeleteVisitorProgress(context.Background(), scope, "u:7", "本人请求", 0); err == nil {
				t.Fatal("cleanup failure ignored")
			}
			if !called {
				t.Fatal("no projection cleanup")
			}
			if _, ok := s.Store.(*memoryStore).data[s.Config.Tenant]["notice_event:"+id]; !ok {
				t.Fatal("raw removed before failed projection cleanup")
			}
		})
	}
}
func TestRevokeNotificationCancellationIsInTheSameMutation(t *testing.T) {
	s, scope := fixture(t)
	id := "notice-content-one"
	if err := s.transaction(context.Background(), func(r Repository) error {
		if err := r.Put(record("content", id, "owner-a", s.Now(), Content{ID: id, Owner: "owner-a", Revision: 1, Version: 1, Status: "published"})); err != nil {
			return err
		}
		row := record("notice_event", "revoke-notice-event", "owner-a", s.Now(), NoticeEvent{ID: "revoke-notice-event", ContentID: id})
		row.Content = id
		return r.Put(row)
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.OnContentRevoke = func(_ context.Context, content string) error {
		if content != id {
			t.Fatal(content)
		}
		calls++
		return errors.New("cancel unavailable")
	}
	if _, err := s.Revoke(context.Background(), scope, id, 1, "撤回", "revoke-notice-key"); err == nil {
		t.Fatal("cancel failure ignored")
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	if err := s.readTransaction(context.Background(), func(r Repository) error {
		c, err := decode[Content](r, "content", id)
		if err == nil && c.Status != "published" {
			t.Fatal("revoke committed without cancel")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
