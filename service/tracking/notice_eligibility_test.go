package tracking

import (
	"context"
	"testing"
)

func noticeEligibilityFixture(t *testing.T) (*Service, string) {
	t.Helper()
	s, _ := fixture(t)
	s.NoticeRules.BrowseEnabled = true
	sender := enter(t, s, 42)
	share, err := s.CreateShare(context.Background(), ShareInput{Route: sender.Route, Key: "notice-eligible-share"}, sender.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := s.Enter(context.Background(), EntryInput{Route: sender.Route, Client: "notice-eligible-client", ShareToken: share["share_token"].(string), Consent: "v1"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	event := ev(s, visitor, "notice-eligible-event", "page_view", s.Now())
	accept(t, s, visitor, event)
	return s, event.ID
}
func TestNoticeEligibilityRechecksRecipientAndDeletion(t *testing.T) {
	ctx := context.Background()
	s, id := noticeEligibilityFixture(t)
	if ok, err := s.NoticeEligible(ctx, id, 42); err != nil || !ok {
		t.Fatalf("initial=%v %v", ok, err)
	}
	if ok, _ := s.NoticeEligible(ctx, id, 99); ok {
		t.Fatal("wrong recipient")
	}
	err := s.transaction(ctx, func(r Repository) error {
		return r.Put(record("deletion", "u:7", "owner-a", s.Now(), map[string]string{"reason": "request"}))
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.NoticeEligible(ctx, id, 42); err != nil || ok {
		t.Fatalf("deleted=%v %v", ok, err)
	}
}
func TestNoticeEligibilityRechecksRevokedAndResolvedSelf(t *testing.T) {
	for _, change := range []string{"revoked", "identity"} {
		t.Run(change, func(t *testing.T) {
			s, id := noticeEligibilityFixture(t)
			ctx := context.Background()
			err := s.transaction(ctx, func(r Repository) error {
				n, e := decode[NoticeEvent](r, "notice_event", id)
				if e != nil {
					return e
				}
				row, e := r.Get("event", id)
				if e != nil {
					return e
				}
				if change == "identity" {
					return r.Put(record("identity", row.Session, n.Owner, s.Now(), map[string]string{"original_visitor_key": "u:7", "visitor_key": "u:42"}))
				}
				rows, e := scopedRecords(r, "share", RecordQuery{Shares: []string{n.ShareID}})
				if e != nil {
					return e
				}
				sh, e := decode[Share](r, "share", rows[0].ID)
				if e != nil {
					return e
				}
				sh.Revoked = true
				return r.Put(record("share", rows[0].ID, n.Owner, s.Now(), sh))
			})
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := s.NoticeEligible(ctx, id, 42); err != nil || ok {
				t.Fatalf("%s=%v %v", change, ok, err)
			}
		})
	}
}

func TestNoticeEligibilityRequiresCurrentOwnerGrant(t *testing.T) {
	s, id := noticeEligibilityFixture(t)
	if ok, err := s.NoticeEligible(context.Background(), id, 42, "owner-other"); err != nil || ok {
		t.Fatalf("cross-owner=%v %v", ok, err)
	}
	if ok, err := s.NoticeEligible(context.Background(), id, 42, "owner-a"); err != nil || !ok {
		t.Fatalf("same-owner=%v %v", ok, err)
	}
}

func TestDeleteVisitorAlsoRemovesDurableNoticeEvent(t *testing.T) {
	s, id := noticeEligibilityFixture(t)
	_, scope := fixture(t)
	if err := s.DeleteVisitor(context.Background(), scope, "u:7", "本人删除申请"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Store.(*memoryStore).data[s.Config.Tenant]["notice_event:"+id]; ok {
		t.Fatal("visitor notice_event survived deletion")
	}
}
