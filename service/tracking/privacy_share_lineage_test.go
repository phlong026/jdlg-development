package tracking

import (
	"context"
	"testing"
)

func lineageShare(t *testing.T, s *Service, en Entry, key string) map[string]any {
	t.Helper()
	sh, e := s.CreateShare(context.Background(), ShareInput{Route: en.Route, Key: key}, en.Token, en.Account)
	if e != nil {
		t.Fatal(e)
	}
	return sh
}
func lineageLogin(t *testing.T, s *Service, en Entry) Entry {
	t.Helper()
	got, e := s.Enter(context.Background(), EntryInput{Route: en.Route, Client: en.Client, Anonymous: en.Anonymous, Previous: en.Token, Consent: "v1"}, 42)
	if e != nil || got.Session != en.Session {
		t.Fatalf("same-session login: %+v %v", got, e)
	}
	return got
}
func legacyShareWithoutSession(t *testing.T, s *Service, share map[string]any) {
	t.Helper()
	if e := s.transaction(context.Background(), func(r Repository) error {
		row, e := r.Get("share", hash(share["share_token"].(string)))
		if e != nil {
			return e
		}
		sh, e := decode[Share](r, "share", row.ID)
		if e != nil {
			return e
		}
		sh.Session = ""
		row.Session = ""
		row.Visitor = ""
		row.Payload = marshal(sh)
		return r.Put(row)
	}); e != nil {
		t.Fatal(e)
	}
}
func lineageIntent(t *testing.T, s *Service, en Entry, share map[string]any, id string) {
	t.Helper()
	event := ev(s, en, id, "share_intent", s.Now())
	event.Share = share["share_id"].(string)
	res := accept(t, s, en, event)
	if len(res) != 1 || res[0].Status != "accepted" {
		t.Fatal(res)
	}
}

func TestDeleteVerifiedSessionRemovesOnlyItsAnonymousShares(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	en := enter(t, s, 0)
	other := enter(t, s, 0)
	own := lineageShare(t, s, en, "share-own-001")
	untouched := lineageShare(t, s, other, "share-other-001")
	lineageLogin(t, s, en)
	if e := s.DeleteVisitor(ctx, scope, "u:42", "requested deletion"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ResolveShare(ctx, own["share_token"].(string)); e == nil {
		t.Fatal("verified session anonymous share survived")
	}
	if _, e := s.ResolveShare(ctx, untouched["share_token"].(string)); e != nil {
		t.Fatal("unrelated same-device anonymous share deleted", e)
	}
	if e := s.transaction(ctx, func(r Repository) error {
		if _, e := r.Get("share_request", hash(en.Visitor+":share-own-001")); e == nil {
			t.Fatal("share token receipt survived")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateShare(ctx, ShareInput{Route: en.Route, Key: "share-own-001"}, en.Token, 0); e == nil {
		t.Fatal("deleted session replay recreated share")
	}
}

func TestHistoricalShareDeletionNeedsUnambiguousIntentEvidence(t *testing.T) {
	for _, mode := range []string{"verified", "missing", "other-session", "conflicting"} {
		t.Run(mode, func(t *testing.T) {
			s, scope := fixture(t)
			ctx := context.Background()
			en := enter(t, s, 0)
			other := enter(t, s, 0)
			sh := lineageShare(t, s, en, "share-legacy-001")
			legacyShareWithoutSession(t, s, sh)
			if mode == "verified" || mode == "conflicting" {
				lineageIntent(t, s, en, sh, "share-intent-001")
			}
			if mode == "other-session" || mode == "conflicting" {
				lineageIntent(t, s, other, sh, "share-intent-002")
			}
			lineageLogin(t, s, en)
			if e := s.DeleteVisitor(ctx, scope, "u:42", "requested deletion"); e != nil {
				t.Fatal(e)
			}
			_, e := s.ResolveShare(ctx, sh["share_token"].(string))
			if (e != nil) != (mode == "verified") {
				t.Fatalf("mode %s resolved error %v", mode, e)
			}
		})
	}
}

func TestShareSessionRejectsForeignAccountAndIntentReplay(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	en := enter(t, s, 0)
	other := enter(t, s, 0)
	sh := lineageShare(t, s, en, "share-session-001")
	if _, e := s.CreateShare(ctx, ShareInput{Route: en.Route, Key: "share-foreign-001"}, en.Token, 42); e == nil {
		t.Fatal("unverified account attached to anonymous session")
	}
	event := ev(s, other, "share-intent-003", "share_intent", s.Now())
	event.Share = sh["share_id"].(string)
	if result := accept(t, s, other, event); len(result) != 1 || result[0].Status != "rejected" {
		t.Fatal("different session claimed share", result)
	}
}

func TestShareDeletionChecksAllOwnersBeforeMutation(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	en := enter(t, s, 0)
	sh := lineageShare(t, s, en, "share-owner-001")
	lineageLogin(t, s, en)
	if e := s.transaction(ctx, func(r Repository) error {
		row, e := r.Get("share", hash(sh["share_token"].(string)))
		if e != nil {
			return e
		}
		row.Owner = "owner-b"
		return r.Put(row)
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.DeleteVisitor(ctx, scope, "u:42", "requested deletion"); e == nil {
		t.Fatal("partial owner deletion accepted")
	}
	if _, e := s.ResolveShare(ctx, sh["share_token"].(string)); e != nil {
		t.Fatal("failed deletion mutated share", e)
	}
}

func TestShareLineageDeletionUsesScopedLegacyJSONRelationships(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "session", true: "legacy-intent"}[legacy], func(t *testing.T) {
			s, indexed, scope, now := privacyFixture(t)
			privacySeedGraph(t, indexed.rows, now)
			share := Share{ID: "linked-anon-share", Sharer: "a:device", Session: "linked-session", Owner: "one"}
			if legacy {
				share.Session = ""
				event := Event{ID: "linked-share-intent", Type: "share_intent", Session: "linked-session", Share: share.ID, Occurred: now, Received: now}
				row := record("event", event.ID, "one", now, event)
				row.Visitor, row.Session = "a:device", event.Session
				indexed.rows.Put(row)
			}
			// JSON is authoritative even if denormalized columns are absent.
			indexed.rows.Put(record("share", "linked-share-token", "one", now, share))
			indexed.rows.Put(record("share_request", "linked-share-request", "one", now, map[string]string{"share_id": share.ID}))
			if e := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); e != nil {
				t.Fatal(e)
			}
			for kind, id := range map[string]string{"share": "linked-share-token", "share_request": "linked-share-request"} {
				if _, e := indexed.rows.Get(kind, id); e == nil {
					t.Fatalf("%s survives indexed deletion", kind)
				}
			}
			if _, e := indexed.rows.Get("share", "unrelated-tokenhash"); e != nil {
				t.Fatal("unrelated anonymous share deleted", e)
			}
		})
	}
}
