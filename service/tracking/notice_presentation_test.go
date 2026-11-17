package tracking

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func presentationScope() Scope {
	return Scope{Tenant: "org-a", Actor: 42, Owners: []string{"owner-a"}, View: true}
}

func TestBrowseNoticePresentationNamesAndScope(t *testing.T) {
	for _, name := range []string{"测试访客", ""} {
		t.Run(name, func(t *testing.T) {
			s, id := noticeEligibilityFixture(t)
			s.ResolveVisitorProfiles = func(_ context.Context, ids []int64) (map[int64]VisitorProfile, error) {
				if len(ids) != 1 || ids[0] != 7 {
					t.Fatalf("unscoped profile ids=%v", ids)
				}
				return map[int64]VisitorProfile{7: {Nickname: name}, 99: {Nickname: "无关账号"}}, nil
			}
			got, ok, err := s.BrowseNoticePresentation(context.Background(), presentationScope(), 42, id)
			want := name
			if want == "" {
				want = "已登录访客"
			}
			if err != nil || !ok || got.VisitorName != want || got.ContentTitle != "首页" {
				t.Fatalf("got=%+v eligible=%v err=%v", got, ok, err)
			}
			for _, scope := range []Scope{
				{Tenant: "other", Actor: 42, Owners: []string{"owner-a"}, View: true},
				{Tenant: "org-a", Actor: 99, Owners: []string{"owner-a"}, View: true},
				{Tenant: "org-a", Actor: 42, Owners: []string{"other"}, View: true},
				{Tenant: "org-a", Actor: 42, Owners: []string{"owner-a"}},
			} {
				if got, ok, _ := s.BrowseNoticePresentation(context.Background(), scope, 42, id); ok || got != (BrowseNoticePresentation{}) {
					t.Fatal("unauthorized presentation", got)
				}
			}
		})
	}
}

func TestBrowseNoticePresentationRejectsChangedFacts(t *testing.T) {
	for _, change := range []string{"recipient", "owner", "visitor", "content", "route", "time", "kind", "share", "project", "collection", "other-sharer", "self", "merged-self", "deleted", "deleted-session", "missing-event", "missing-session", "missing-notice", "revoked", "expired", "identity-error"} {
		t.Run(change, func(t *testing.T) {
			s, id := noticeEligibilityFixture(t)
			err := s.transaction(context.Background(), func(r Repository) error {
				nr, _ := r.Get("notice_event", id)
				n, _ := decode[NoticeEvent](r, "notice_event", id)
				row, _ := r.Get("event", id)
				en, _ := decode[Entry](r, "session", row.Session)
				switch change {
				case "recipient":
					n.RecipientID = 99
				case "owner":
					n.Owner = "other"
				case "visitor":
					n.VisitorKey = "u:99"
				case "content":
					n.ContentID = "wrong-content"
				case "route":
					n.Route = "pages/list/list"
				case "time":
					n.OccurredAt = n.OccurredAt.Add(1)
				case "kind":
					n.Kind = "project_note_open"
				case "share":
					n.ShareID = "wrong-share"
				case "project":
					n.ProjectID = "wrong-project"
				case "collection":
					n.CollectionID = "wrong-collection"
				case "self":
					en.Self = true
					return r.Put(record("session", row.Session, en.Owner, s.Now(), en))
				case "merged-self":
					return r.Put(record("identity", row.Session, n.Owner, s.Now(), map[string]string{"original_visitor_key": "u:7", "visitor_key": "u:42"}))
				case "identity-error":
					return r.Put(Record{Kind: "identity", ID: row.Session, Payload: []byte("broken")})
				case "deleted":
					return r.Put(record("deletion", "u:7", n.Owner, s.Now(), true))
				case "deleted-session":
					return r.Put(record("deletion_session", row.Session, n.Owner, s.Now(), true))
				case "missing-event":
					return r.Remove("event", id)
				case "missing-session":
					return r.Remove("session", row.Session)
				case "missing-notice":
					return r.Remove("notice_event", id)
				case "other-sharer", "revoked", "expired":
					shares, e := scopedRecords(r, "share", RecordQuery{Shares: []string{n.ShareID}})
					if e != nil {
						return e
					}
					sh, _ := decode[Share](r, "share", shares[0].ID)
					if change == "other-sharer" {
						sh.Sharer = "u:99"
					}
					if change == "revoked" {
						sh.Revoked = true
					}
					if change == "expired" {
						sh.Expires = s.Now()
					}
					return r.Put(record("share", shares[0].ID, n.Owner, s.Now(), sh))
				}
				nr.Payload = marshal(n)
				return r.Put(nr)
			})
			if err != nil {
				t.Fatal(err)
			}
			got, ok, err := s.BrowseNoticePresentation(context.Background(), presentationScope(), 42, id)
			if ok || got != (BrowseNoticePresentation{}) {
				t.Fatalf("leaked %+v eligible=%v err=%v", got, ok, err)
			}
			if change == "identity-error" && err == nil {
				t.Fatal("core identity failure must retry")
			}
		})
	}
}

func TestBrowseNoticePresentationProfileFailureRemainsRetryable(t *testing.T) {
	s, id := noticeEligibilityFixture(t)
	s.ResolveVisitorProfiles = func(context.Context, []int64) (map[int64]VisitorProfile, error) {
		return nil, errors.New("unavailable")
	}
	got, ok, err := s.BrowseNoticePresentation(context.Background(), presentationScope(), 42, id)
	if err == nil || ok || got != (BrowseNoticePresentation{}) {
		t.Fatalf("got=%+v eligible=%v err=%v", got, ok, err)
	}
}

func TestBrowseNoticePresentationAnonymousNeverIncludesDeviceKey(t *testing.T) {
	s, id := noticeEligibilityFixture(t)
	err := s.transaction(context.Background(), func(r Repository) error {
		nr, _ := r.Get("notice_event", id)
		n, _ := decode[NoticeEvent](r, "notice_event", id)
		row, _ := r.Get("event", id)
		en, _ := decode[Entry](r, "session", row.Session)
		en.Visitor = "a:private-device-key"
		n.VisitorKey = en.Visitor
		row.Visitor = en.Visitor
		nr.Visitor = en.Visitor
		nr.Payload = marshal(n)
		if e := r.Put(record("session", row.Session, en.Owner, s.Now(), en)); e != nil {
			return e
		}
		if e := r.Put(row); e != nil {
			return e
		}
		return r.Put(nr)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.BrowseNoticePresentation(context.Background(), presentationScope(), 42, id)
	if err != nil || !ok || got.VisitorName != "匿名访客" || strings.Contains(got.VisitorName, "key") {
		t.Fatalf("got=%+v eligible=%v err=%v", got, ok, err)
	}
}

func TestBrowseNoticePresentationCollectionChildUsesActualImmutablePage(t *testing.T) {
	s, scope, child, _, _, published := publishedCollectionFixture(t)
	s.NoticeRules.BrowseEnabled = true
	ctx := context.Background()
	sharer, err := s.Enter(ctx, EntryInput{Route: projectCollectionReaderRoute, Content: published.ID, Version: published.Version, Client: "presentation-sharer", Consent: "v1"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateShare(ctx, ShareInput{Route: sharer.Route, Content: sharer.Content, Version: sharer.Version, Key: "presentation-collection-share"}, sharer.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := s.Enter(ctx, EntryInput{Route: publishedReaderRoute, Content: child.ID, Version: child.Version, CollectionID: published.ID, CollectionVersion: published.Version, ShareToken: created["share_token"].(string), Client: "presentation-reader", Consent: "v1"}, 101)
	if err != nil {
		t.Fatal(err)
	}
	event := ev(s, visitor, "presentation-child-event", "page_view", s.Now())
	accept(t, s, visitor, event)
	// Going back to the directory overwrites the same session row.
	_, err = s.Enter(ctx, EntryInput{Route: projectCollectionReaderRoute, Content: published.ID, Version: published.Version, Previous: visitor.Token, Client: visitor.Client, Consent: "v1"}, 101)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.BrowseNoticePresentation(ctx, scope, 42, event.ID)
	if err != nil || !ok || got.ContentTitle != child.Title {
		t.Fatalf("got=%+v eligible=%v err=%v child=%+v", got, ok, err, child)
	}
	store := s.Store
	for _, kind := range []string{"content_revoked", "content", "version", projectCollectionKind, projectCollectionVersionKind} {
		s.Store = noticePresentationReadFailure{store, kind}
		got, ok, err := s.BrowseNoticePresentation(ctx, scope, 42, event.ID)
		if !errors.Is(err, ErrUnavailable) || ok || got != (BrowseNoticePresentation{}) {
			t.Fatalf("core %s read failure: got=%+v eligible=%v err=%v", kind, got, ok, err)
		}
	}
	s.Store = store
	if _, err := s.Revoke(ctx, scope, child.ID, child.Revision, "测试撤回", "presentation-child-revoke"); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.BrowseNoticePresentation(ctx, scope, 42, event.ID); err != nil || ok || got != (BrowseNoticePresentation{}) {
		t.Fatalf("withdrawn child=%+v eligible=%v err=%v", got, ok, err)
	}
}

type noticePresentationReadFailure struct {
	Store
	kind string
}
type noticePresentationFailRepo struct {
	Repository
	kind string
}

func (s noticePresentationReadFailure) ReadTransaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.Store.(ReadStore).ReadTransaction(ctx, tenant, func(r Repository) error { return fn(noticePresentationFailRepo{r, s.kind}) })
}
func (r noticePresentationFailRepo) Get(kind, id string) (Record, error) {
	if kind == r.kind {
		return Record{}, ErrUnavailable
	}
	return r.Repository.Get(kind, id)
}

func TestBrowseNoticePresentationCoreReadErrorsRemainRetryable(t *testing.T) {
	for _, kind := range []string{"notice_event", "event", "session", "identity", "deletion", "deletion_session"} {
		t.Run(kind, func(t *testing.T) {
			s, id := noticeEligibilityFixture(t)
			s.Store = noticePresentationReadFailure{s.Store, kind}
			got, ok, err := s.BrowseNoticePresentation(context.Background(), presentationScope(), 42, id)
			if !errors.Is(err, ErrUnavailable) || ok || got != (BrowseNoticePresentation{}) {
				t.Fatalf("got=%+v eligible=%v err=%v", got, ok, err)
			}
		})
	}
}

func TestBrowseNoticePresentationUsesBoundedQueries(t *testing.T) {
	s, id := noticeEligibilityFixture(t)
	rows := s.Store.(*memoryStore).data[s.Config.Tenant]
	indexed := &indexedTestStore{rows: reviewStore{}, t: t}
	for _, row := range rows {
		indexed.rows.Put(row)
	}
	s.Store = indexed
	got, ok, err := s.BrowseNoticePresentation(context.Background(), presentationScope(), 42, id)
	if err != nil || !ok || got.VisitorName != "已登录访客" {
		t.Fatalf("got=%+v eligible=%v err=%v", got, ok, err)
	}
	for _, call := range indexed.calls {
		if call.kind == "event" || call.kind == "session" || call.kind == "notice_event" {
			t.Fatalf("unexpected fact scan: %+v", call)
		}
	}
}
