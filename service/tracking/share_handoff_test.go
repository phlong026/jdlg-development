package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func handoffFixture(t *testing.T, notices bool) (*Service, Scope, Entry, string, Event) {
	t.Helper()
	s, scope := fixture(t)
	s.NoticeRules.BrowseEnabled = notices
	content := publishMaterial(t, s, scope, "handoff article")
	author, err := s.Enter(context.Background(), EntryInput{Route: publishedReaderRoute, Content: content.ID, Version: content.Version, Client: "handoff-author-client", Consent: "v1"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateShare(context.Background(), ShareInput{Route: author.Route, Content: author.Content, Version: author.Version, Key: "handoff-share-key", Channel: "app_message"}, author.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	code := created["share_token"].(string)
	source, err := s.Enter(context.Background(), EntryInput{Route: author.Route, Content: author.Content, Version: author.Version, Client: "handoff-web-client", Consent: "v1", ShareToken: code, WebSource: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	event := ev(s, source, "handoff-source-event", "page_view", s.Now())
	event.View = "handoff-source-view"
	if got := accept(t, s, source, event); got[0].Status != "accepted" {
		t.Fatal(got)
	}
	return s, scope, source, code, event
}

func TestShareHandoffAssociatesOnceWithoutMergingVisitorsAndDeduplicatesNotice(t *testing.T) {
	s, _, source, code, event := handoffFixture(t, true)
	ctx := context.Background()
	token := strings.Repeat("a", 43)
	handoff, err := s.CreateShareHandoff(ctx, code, source.Token, event.ID, token)
	if err != nil || !handoff.Ready || !handoff.Expires.Equal(s.Now().Add(5*time.Minute)) {
		t.Fatal(handoff, err)
	}
	in := EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "handoff-mini-client", Consent: "v1", ShareToken: code, HandoffToken: token}
	mini, err := s.Enter(ctx, in, 101)
	if err != nil || mini.HandoffKey != hash(token) {
		t.Fatal(mini, err)
	}
	again, err := s.Enter(ctx, in, 101)
	if err != nil || again.HandoffKey != mini.HandoffKey {
		t.Fatal(again, err)
	}
	other, err := s.Enter(ctx, in, 102)
	if err != nil || other.HandoffKey != "" || other.Share != mini.Share {
		t.Fatal(other, err)
	}
	encoded, _ := json.Marshal(mini)
	if strings.Contains(string(encoded), `"_handoff_key"`) || strings.Contains(string(encoded), `"_web_source"`) || strings.Contains(string(encoded), token) {
		t.Fatalf("internal entry fields leaked: %s", encoded)
	}
	miniEvent := ev(s, mini, "handoff-mini-event", "page_view", s.Now())
	miniEvent.View = "handoff-mini-view"
	if got := accept(t, s, mini, miniEvent); got[0].Status != "accepted" {
		t.Fatal(got)
	}
	rows := s.Store.(*memoryStore).data[s.Config.Tenant]
	if _, ok := rows["event:"+miniEvent.ID]; !ok {
		t.Fatal("mini fact lost")
	}
	if _, ok := rows["event:"+event.ID]; !ok {
		t.Fatal("web fact lost")
	}
	if _, ok := rows["notice_event:"+miniEvent.ID]; ok {
		t.Fatal("handoff repeated the source notice")
	}
	if _, ok := rows["notice_event:"+event.ID]; !ok {
		t.Fatal("source notice missing")
	}
	for _, row := range rows {
		if row.Kind == "identity" {
			t.Fatal("cross-client handoff merged identity")
		}
	}
}

func TestShareHandoffPrivacyDeletionDoesNotCrossTheClientBoundary(t *testing.T) {
	for _, staged := range []bool{false, true} {
		for _, side := range []string{"source", "target"} {
			t.Run(side+"/"+map[bool]string{false: "atomic", true: "staged"}[staged], func(t *testing.T) {
				s, scope, source, code, event := handoffFixture(t, true)
				s.forceStagedDeletion = staged
				ctx := context.Background()
				token := strings.Repeat("d", 43)
				if out, err := s.CreateShareHandoff(ctx, code, source.Token, event.ID, token); err != nil || !out.Ready {
					t.Fatal(out, err)
				}
				mini, err := s.Enter(ctx, EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "handoff-privacy-client", Consent: "v1", ShareToken: code, HandoffToken: token}, 101)
				if err != nil {
					t.Fatal(err)
				}
				miniEvent := ev(s, mini, "handoff-privacy-mini", "page_view", s.Now())
				miniEvent.View = "handoff-privacy-view"
				accept(t, s, mini, miniEvent)
				key := source.Visitor
				if side == "target" {
					key = mini.Visitor
				}
				if err = s.DeleteVisitor(ctx, scope, key, "privacy request"); err != nil {
					t.Fatal(err)
				}
				rows := s.Store.(*memoryStore).data[s.Config.Tenant]
				if side == "source" {
					if _, ok := rows["share_handoff:"+hash(token)]; ok {
						t.Fatal("source bridge survived deletion")
					}
					if _, ok := rows["event:"+miniEvent.ID]; !ok {
						t.Fatal("source deletion removed independent mini fact")
					}
				} else {
					if _, ok := rows["handoff_redeem:"+hash(token)]; ok {
						t.Fatal("target bridge survived deletion")
					}
					if _, ok := rows["event:"+event.ID]; !ok {
						t.Fatal("target deletion removed independent web fact")
					}
					var proof shareHandoff
					if json.Unmarshal(rows["share_handoff:"+hash(token)].Payload, &proof) != nil || !proof.Redeemed {
						t.Fatal("target deletion resurrected consumed handoff")
					}
					other, err := s.Enter(ctx, EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "other-client-after-delete", Consent: "v1", ShareToken: code, HandoffToken: token}, 102)
					if err != nil || other.HandoffKey != "" {
						t.Fatal(other, err)
					}
				}
			})
		}
	}
}

func TestShareHandoffCanClaimOneNoticeWhenSourceWasNotNotified(t *testing.T) {
	s, _, source, code, event := handoffFixture(t, false)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		token := strings.Repeat(string(rune('a'+i)), 43)
		if out, err := s.CreateShareHandoff(ctx, code, source.Token, event.ID, token); err != nil || !out.Ready {
			t.Fatal(out, err)
		}
		mini, err := s.Enter(ctx, EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "handoff-mini-claim-" + string(rune('a'+i)), Consent: "v1", ShareToken: code, HandoffToken: token}, int64(101+i))
		if err != nil {
			t.Fatal(err)
		}
		s.NoticeRules.BrowseEnabled = true
		miniEvent := ev(s, mini, "handoff-claim-event-"+string(rune('a'+i)), "page_view", s.Now())
		miniEvent.View = "handoff-claim-view-" + string(rune('a'+i))
		if got := accept(t, s, mini, miniEvent); got[0].Status != "accepted" {
			t.Fatal(got)
		}
	}
	count := 0
	for _, row := range s.Store.(*memoryStore).data[s.Config.Tenant] {
		if row.Kind == "notice_event" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("handoff notices=%d", count)
	}
}

func TestShareHandoffRejectsUnacceptedNonWebCrossShareExpiredAndRevokedProof(t *testing.T) {
	for _, kind := range []string{"event missing", "not web", "mismatched share", "expired", "revoked", "token short", "consent changed"} {
		t.Run(kind, func(t *testing.T) {
			s, _, source, code, event := handoffFixture(t, true)
			token := strings.Repeat("b", 43)
			switch kind {
			case "event missing":
				event.ID = "missing-event"
			case "not web":
				source2, err := s.Enter(context.Background(), EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "not-web-client", Consent: "v1", ShareToken: code}, 0)
				if err != nil {
					t.Fatal(err)
				}
				source = source2
			case "mismatched share":
				other := enter(t, s, 42)
				created, err := s.CreateShare(context.Background(), ShareInput{Route: other.Route, Key: "handoff-other-share"}, other.Token, 42)
				if err != nil {
					t.Fatal(err)
				}
				code = created["share_token"].(string)
			case "expired":
				now := s.Now().Add(6 * time.Minute)
				s.Now = func() time.Time { return now }
			case "revoked":
				if err := s.transaction(context.Background(), func(r Repository) error {
					row, err := r.Get("share", hash(code))
					if err != nil {
						return err
					}
					var sh Share
					if json.Unmarshal(row.Payload, &sh) != nil {
						return ErrUnavailable
					}
					sh.Revoked = true
					row.Payload = marshal(sh)
					return r.Put(row)
				}); err != nil {
					t.Fatal(err)
				}
			case "token short":
				token = "too-short"
			case "consent changed":
				s.Config.ConsentVersion = "v2"
			}
			out, err := s.CreateShareHandoff(context.Background(), code, source.Token, event.ID, token)
			if err != nil || out.Ready {
				t.Fatalf("invalid proof ready=%v err=%v", out, err)
			}
		})
	}
}

type failHandoffStore struct {
	Store
	kind string
}
type failHandoffRepo struct {
	Repository
	kind string
}

func (s failHandoffStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.Store.Transaction(ctx, tenant, func(r Repository) error { return fn(failHandoffRepo{r, s.kind}) })
}
func (r failHandoffRepo) Get(kind, id string) (Record, error) {
	if kind == r.kind {
		return Record{}, errors.New("handoff repository unavailable")
	}
	return r.Repository.Get(kind, id)
}
func TestShareHandoffDoesNotHideRepositoryFailure(t *testing.T) {
	s, _, source, code, event := handoffFixture(t, true)
	base := s.Store
	for _, kind := range []string{"event", "content", "content_revoked", "version", "share", "entry", "share_handoff"} {
		s.Store = failHandoffStore{base, kind}
		if _, err := s.CreateShareHandoff(context.Background(), code, source.Token, event.ID, strings.Repeat("c", 43)); err == nil {
			t.Fatalf("storage failure became ordinary fallback: %s", kind)
		}
	}
}

func TestShareHandoffNoticeClaimRollsBackWithFailedNotification(t *testing.T) {
	s, _, source, code, event := handoffFixture(t, false)
	ctx := context.Background()
	token := strings.Repeat("f", 43)
	if out, err := s.CreateShareHandoff(ctx, code, source.Token, event.ID, token); err != nil || !out.Ready {
		t.Fatal(out, err)
	}
	mini, err := s.Enter(ctx, EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "handoff-rollback-client", Consent: "v1", ShareToken: code, HandoffToken: token}, 101)
	if err != nil {
		t.Fatal(err)
	}
	s.NoticeRules.BrowseEnabled = true
	base := s.Store
	s.Store = failNoticeStore{base}
	view := ev(s, mini, "handoff-rollback-event", "page_view", s.Now())
	view.View = "handoff-rollback-view"
	if _, err = s.Accept(ctx, mini.Token, []Event{view}); err == nil {
		t.Fatal("notification failure committed handoff event")
	}
	rows := base.(*memoryStore).data[s.Config.Tenant]
	for _, key := range []string{"event:" + view.ID, "handoff_notice:" + hash(event.ID), "notice_event:" + view.ID} {
		if _, ok := rows[key]; ok {
			t.Fatalf("failed transaction kept %s", key)
		}
	}
	s.Store = base
	if got := accept(t, s, mini, view); got[0].Status != "accepted" {
		t.Fatal(got)
	}
}

func TestShareHandoffExpiredOrCrossShareRedemptionKeepsOrdinaryShare(t *testing.T) {
	for _, kind := range []string{"expired", "cross share", "different client"} {
		t.Run(kind, func(t *testing.T) {
			s, _, source, code, event := handoffFixture(t, true)
			ctx := context.Background()
			token := strings.Repeat("g", 43)
			if out, err := s.CreateShareHandoff(ctx, code, source.Token, event.ID, token); err != nil || !out.Ready {
				t.Fatal(out, err)
			}
			in := EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "handoff-bound-client", Consent: "v1", ShareToken: code, HandoffToken: token}
			switch kind {
			case "expired":
				now := s.Now().Add(5 * time.Minute)
				s.Now = func() time.Time { return now }
			case "cross share":
				author, err := s.Enter(ctx, EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "handoff-other-author", Consent: "v1"}, 43)
				if err != nil {
					t.Fatal(err)
				}
				created, err := s.CreateShare(ctx, ShareInput{Route: source.Route, Content: source.Content, Version: source.Version, Key: "handoff-other-content-share"}, author.Token, 43)
				if err != nil {
					t.Fatal(err)
				}
				in.ShareToken = created["share_token"].(string)
			case "different client":
				if _, err := s.Enter(ctx, in, 101); err != nil {
					t.Fatal(err)
				}
				in.Client = "handoff-replay-other-client"
			}
			mini, err := s.Enter(ctx, in, 101)
			if err != nil || mini.HandoffKey != "" || mini.Attribution != "share" {
				t.Fatal(mini, err)
			}
		})
	}
}

func TestShareHandoffSourceNoticeClaimSurvivesTargetDeletionForOtherIssuedTokens(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(map[bool]string{false: "atomic", true: "staged"}[staged], func(t *testing.T) {
			s, scope, source, code, event := handoffFixture(t, false)
			s.forceStagedDeletion = staged
			ctx := context.Background()
			tokens := []string{strings.Repeat("p", 43), strings.Repeat("q", 43)}
			for _, token := range tokens {
				if out, err := s.CreateShareHandoff(ctx, code, source.Token, event.ID, token); err != nil || !out.Ready {
					t.Fatal(out, err)
				}
			}
			s.NoticeRules.BrowseEnabled = true
			for i, token := range tokens {
				mini, err := s.Enter(ctx, EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "claim-privacy-client-" + string(rune('a'+i)), Consent: "v1", ShareToken: code, HandoffToken: token}, int64(101+i))
				if err != nil {
					t.Fatal(err)
				}
				view := ev(s, mini, "claim-privacy-event-"+string(rune('a'+i)), "page_view", s.Now())
				view.View = "claim-privacy-view-" + string(rune('a'+i))
				accept(t, s, mini, view)
				rows := s.Store.(*memoryStore).data[s.Config.Tenant]
				if i == 0 {
					if _, ok := rows["notice_event:"+view.ID]; !ok {
						t.Fatal("first target did not claim notification")
					}
					if err = s.DeleteVisitor(ctx, scope, mini.Visitor, "privacy request"); err != nil {
						t.Fatal(err)
					}
					claim := s.Store.(*memoryStore).data[s.Config.Tenant]["handoff_notice:"+hash(event.ID)]
					if claim.Visitor != source.Visitor || claim.Session != source.Session || string(claim.Payload) != `{"claimed":true}` {
						t.Fatalf("source claim carries target identity: %+v", claim)
					}
				} else {
					if _, ok := rows["notice_event:"+view.ID]; ok {
						t.Fatal("deleting first target allowed a second handoff notification")
					}
					if _, ok := rows["event:"+view.ID]; !ok {
						t.Fatal("second independent target fact lost")
					}
				}
			}
		})
	}
}

func TestShareHandoffSuppressesOnlyTheFirstPromptPageView(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "first conversion only", true: "late target is ordinary"}[expired], func(t *testing.T) {
			s, _, source, code, event := handoffFixture(t, true)
			ctx := context.Background()
			token := strings.Repeat("r", 43)
			if out, err := s.CreateShareHandoff(ctx, code, source.Token, event.ID, token); err != nil || !out.Ready {
				t.Fatal(out, err)
			}
			mini, err := s.Enter(ctx, EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "first-conversion-client", Consent: "v1", ShareToken: code, HandoffToken: token}, 101)
			if err != nil {
				t.Fatal(err)
			}
			if expired {
				now := s.Now().Add(5 * time.Minute)
				s.Now = func() time.Time { return now }
			}
			first := ev(s, mini, "first-conversion-event", "page_view", s.Now())
			first.View = "first-conversion-view"
			accept(t, s, mini, first)
			rows := s.Store.(*memoryStore).data[s.Config.Tenant]
			_, firstNotice := rows["notice_event:"+first.ID]
			if firstNotice != expired {
				t.Fatalf("first notice=%v expired=%v", firstNotice, expired)
			}
			second := ev(s, mini, "later-independent-event", "page_view", s.Now())
			second.View = "later-independent-view"
			accept(t, s, mini, second)
			rows = s.Store.(*memoryStore).data[s.Config.Tenant]
			if _, ok := rows["notice_event:"+second.ID]; !ok {
				t.Fatal("later independent reading notification suppressed")
			}
		})
	}
}

func TestShareHandoffRegistersFirstPageViewWhileNoticesAreDisabled(t *testing.T) {
	s, _, source, code, sourceEvent := handoffFixture(t, true)
	ctx := context.Background()
	token := strings.Repeat("s", 43)
	if out, err := s.CreateShareHandoff(ctx, code, source.Token, sourceEvent.ID, token); err != nil || !out.Ready {
		t.Fatal(out, err)
	}
	mini, err := s.Enter(ctx, EntryInput{Route: source.Route, Content: source.Content, Version: source.Version, Client: "disabled-first-view-client", Consent: "v1", ShareToken: code, HandoffToken: token}, 101)
	if err != nil {
		t.Fatal(err)
	}
	s.NoticeRules.BrowseEnabled = false
	first := ev(s, mini, "disabled-first-view-event", "page_view", s.Now())
	first.View = "disabled-first-view"
	if got := accept(t, s, mini, first); got[0].Status != "accepted" {
		t.Fatal(got)
	}
	rows := s.Store.(*memoryStore).data[s.Config.Tenant]
	var target handoffRedeem
	if json.Unmarshal(rows["handoff_redeem:"+hash(token)].Payload, &target) != nil || target.FirstPageViewID != first.View {
		t.Fatalf("accepted first view was not recorded while notices were disabled: %+v", target)
	}
	if _, ok := rows["notice_event:"+first.ID]; ok {
		t.Fatal("disabled first view generated a notice")
	}
	if _, ok := rows["handoff_notice:"+hash(sourceEvent.ID)]; ok {
		t.Fatal("disabled first view consumed source notice claim")
	}
	s.NoticeRules.BrowseEnabled = true
	second := ev(s, mini, "enabled-independent-view-event", "page_view", s.Now())
	second.View = "enabled-independent-view"
	if got := accept(t, s, mini, second); got[0].Status != "accepted" {
		t.Fatal(got)
	}
	rows = s.Store.(*memoryStore).data[s.Config.Tenant]
	if _, ok := rows["notice_event:"+second.ID]; !ok {
		t.Fatal("later independent reading notification suppressed after notices resumed")
	}
}

func TestShareHandoffRecordsJoinReviewedRawRetention(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	if err := s.transaction(ctx, func(r Repository) error {
		for _, kind := range []string{"share_handoff", "handoff_redeem", "handoff_notice"} {
			if err := r.Insert(record(kind, "expired-handoff-proof", scope.Owners[0], s.Now().AddDate(0, 0, -91), map[string]bool{"old": true})); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	policy := DefaultRetentionPolicy()
	policy.Enabled = true
	plan, err := s.PlanRetention(ctx, scope, policy, s.Now().AddDate(0, 0, -90), s.Now().AddDate(0, 0, -180))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"share_handoff", "handoff_redeem", "handoff_notice"} {
		if plan.Counts[kind] != 1 {
			t.Fatalf("missing raw retention kind %s", kind)
		}
	}
	if _, err = s.PurgeExpired(ctx, scope, policy, plan); err != nil {
		t.Fatal(err)
	}
	for _, row := range s.Store.(*memoryStore).data[s.Config.Tenant] {
		if row.ID == "expired-handoff-proof" {
			t.Fatal("expired handoff proof retained")
		}
	}
}
