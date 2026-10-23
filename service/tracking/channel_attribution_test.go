package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func channelErrorStatus(err error) int {
	var target *Error
	if errors.As(err, &target) {
		return target.Status
	}
	return 0
}

func TestCreateSharePersistsValidatedChannelAndPreparation(t *testing.T) {
	s, _ := fixture(t)
	en := enter(t, s, 1)
	for _, channel := range []string{"wechat", "TIMELINE", "timeline ", "direct"} {
		if _, err := s.CreateShare(context.Background(), ShareInput{Route: en.Route, Key: "bad-channel-0001", Channel: channel}, en.Token, 1); channelErrorStatus(err) != 400 {
			t.Fatalf("channel %q: %v, want 400", channel, err)
		}
	}
	for i, channel := range []string{"app_message", "timeline", ""} {
		in := ShareInput{Route: en.Route, Key: "channel-key-000" + string(rune('1'+i)), Channel: channel, Prepared: i == 1}
		created, err := s.CreateShare(context.Background(), in, en.Token, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.transaction(context.Background(), func(r Repository) error {
			sh, err := decode[Share](r, "share", hash(created["share_token"].(string)))
			if err == nil && (sh.Channel != channel || sh.Prepared != in.Prepared || sh.Session != en.Session || sh.Sharer != "u:1") {
				t.Fatalf("persisted share = %+v", sh)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err = s.CreateShare(context.Background(), in, en.Token, 1); err != nil {
			t.Fatal("same request lost idempotency", err)
		}
		in.Channel = "timeline"
		in.Prepared = !in.Prepared
		if _, err = s.CreateShare(context.Background(), in, en.Token, 1); channelErrorStatus(err) != 409 {
			t.Fatal("channel/preparation mutation reused idempotent share", err)
		}
	}
	if _, err := s.CreateConsoleShare(context.Background(), ShareInput{Route: en.Route, Key: "console-key-0001", Channel: "timeline"}, 1); channelErrorStatus(err) != 400 {
		t.Fatal("console code/link claimed a native channel", err)
	}
}

func TestStoredChannelAttributesDownstreamWithoutShareIntent(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	sharer := enter(t, s, 1)
	created, err := s.CreateShare(ctx, ShareInput{Route: sharer.Route, Key: "prepared-share-0001", Channel: "timeline", Prepared: true}, sharer.Token, 1)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := s.Enter(ctx, EntryInput{Route: sharer.Route, ShareToken: created["share_token"].(string), Client: "client-reader-0001", Anonymous: "anon-reader-0001", Consent: "v1"}, 2)
	if err != nil || reader.Attribution != "share" {
		t.Fatalf("reader entry = %+v, %v", reader, err)
	}
	if rs := accept(t, s, reader, ev(s, reader, "reader-page-0001", "page_view", s.Now())); rs[0].Status != "accepted" {
		t.Fatal(rs)
	}
	list, err := s.Visits(ctx, scope, Filter{Channel: "timeline"})
	if err != nil {
		t.Fatal(err)
	}
	items := visitItems(t, list)
	if len(items) != 1 || items[0].Source.Kind != "share" || items[0].Source.Channel != "timeline" || items[0].Visitor.Key != "u:2" {
		t.Fatalf("lost-intent timeline visit = %+v", items)
	}
	shares, err := s.VisitorShares(ctx, scope, "u:1", Filter{})
	if err != nil || len(shares.Items.([]VisitorShare)) != 0 {
		t.Fatalf("preparation counted as user gesture: %+v, %v", shares, err)
	}
	intent := ev(s, sharer, "share-intent-0001", "share_intent", s.Now())
	intent.View, intent.Share, intent.SourceChannel = "", created["share_id"].(string), "timeline"
	if rs := accept(t, s, sharer, intent); rs[0].Status != "accepted" {
		t.Fatal(rs)
	}
	shares, err = s.VisitorShares(ctx, scope, "u:1", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	forwarded := shares.Items.([]VisitorShare)
	if len(forwarded) != 1 || forwarded[0].Channel != "timeline" || forwarded[0].Visitors != 1 {
		t.Fatalf("observed gesture/downstream = %+v", forwarded)
	}
}

func TestShareIntentForDifferentPublicTargetUsesSignedSourceSession(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	sharer := enter(t, s, 1)
	created, err := s.CreateShare(ctx, ShareInput{Route: "pages/list/list", Key: "cross-target-share-0001", Channel: "timeline", Prepared: true}, sharer.Token, 1)
	if err != nil {
		t.Fatal(err)
	}
	intent := ev(s, sharer, "cross-target-intent-0001", "share_intent", s.Now())
	intent.View, intent.Share, intent.SourceChannel = "", created["share_id"].(string), "app_message"
	if rs := accept(t, s, sharer, intent); rs[0].Status != "rejected" {
		t.Fatal("intent rewrote persisted target channel", rs)
	}
	intent.ID, intent.SourceChannel = "cross-target-intent-0002", "timeline"
	if rs := accept(t, s, sharer, intent); rs[0].Status != "accepted" {
		t.Fatal("valid source-page gesture for a different public target rejected", rs)
	}
	shares, err := s.VisitorShares(ctx, scope, "u:1", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	items := shares.Items.([]VisitorShare)
	if len(items) != 1 || items[0].Route != "pages/list/list" || items[0].Channel != "timeline" {
		t.Fatalf("cross-target share metrics = %+v", items)
	}
}

func TestPreparedShareMetricsRequireBoundedAuthorizedIntent(t *testing.T) {
	s, m, scope, now := reviewFixture()
	created := now.Add(-time.Hour)
	seedShareMetric(m, "prepared", "one", "u:1", "session1", "content1", 1, created)
	row, _ := m.Get("share", "tokhash-prepared")
	var sh Share
	_ = json.Unmarshal(row.Payload, &sh)
	sh.Prepared, sh.Channel = true, "timeline"
	row.Payload = marshal(sh)
	_ = m.Put(row)
	f, err := s.Normalize(Filter{Owner: "one", AsOf: now.Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	assertCount := func(want int) {
		t.Helper()
		got, err := clientShares(m, scope, f)
		if err != nil || len(got) != want {
			t.Fatalf("client shares = %+v, %v, want %d", got, err, want)
		}
	}
	assertCount(0)
	for _, invalid := range []struct{ owner, visitor, session, channel string }{
		{"two", "u:1", "session1", "timeline"},
		{"one", "u:2", "session1", "timeline"},
		{"one", "u:1", "other-session", "timeline"},
		{"one", "u:1", "session1", "bogus"},
	} {
		seedShareIntent(m, invalid.owner, invalid.visitor, invalid.session, sh.ID, invalid.channel, created.Add(time.Minute))
		assertCount(0)
	}
	seedTimedShareIntent(m, "one", "u:1", "session1", sh.ID, "timeline", created.Add(time.Minute), now)
	assertCount(0) // Event received after the pinned snapshot cannot activate an old page.
	seedShareIntent(m, "one", "u:1", "session1", sh.ID, "timeline", created.Add(time.Minute))
	assertCount(1)
	_ = m.Put(record("deletion", "u:1", "one", now, map[string]string{"reason": "delete"}))
	if _, err := s.VisitorShares(context.Background(), scope, "u:1", Filter{}); channelErrorStatus(err) != 404 {
		t.Fatal("deleted sharer history was readable", err)
	}
}

func TestPreparedShareAcrossCreationWindowBoundaryIsNotInferred(t *testing.T) {
	s, m, scope, now := reviewFixture()
	created := now.Add(-2 * time.Hour)
	seedShareMetric(m, "boundary", "one", "u:1", "session1", "content1", 1, created)
	row, _ := m.Get("share", "tokhash-boundary")
	var sh Share
	_ = json.Unmarshal(row.Payload, &sh)
	sh.Prepared, sh.Channel = true, "timeline"
	row.Payload = marshal(sh)
	_ = m.Put(row)
	seedShareIntent(m, "one", "u:1", "session1", sh.ID, "timeline", now.Add(-30*time.Minute))
	all, _ := s.Normalize(Filter{Owner: "one"})
	shares, err := clientShares(m, scope, all)
	if err != nil || len(shares) != 1 {
		t.Fatalf("full creation window = %+v, %v", shares, err)
	}
	narrow, _ := s.Normalize(Filter{Owner: "one", Start: now.Add(-time.Hour)})
	shares, err = clientShares(m, scope, narrow)
	if err != nil || len(shares) != 0 {
		t.Fatalf("boundary inferred a new share: %+v, %v", shares, err)
	}
}

func TestStoredChannelAndLegacyFallbackKeepSnapshotAndOwnerScope(t *testing.T) {
	s, m, scope, now := visitsFixture(t)
	at := now.Add(-2 * time.Hour)
	seedShareMetric(m, "stored", "one", "u:3", "stored-session", "content1", 1, at)
	row, _ := m.Get("share", "tokhash-stored")
	var sh Share
	_ = json.Unmarshal(row.Payload, &sh)
	sh.Channel = "timeline"
	row.Payload = marshal(sh)
	_ = m.Put(row)
	// Even a contradictory event cannot mutate an immutable stored channel.
	seedShareIntent(m, "one", "u:3", "stored-session", sh.ID, "app_message", at)
	visitPV(m, "stored-reader", "one", "u:2", "reader-session", "content1", sh.ID, at.Add(time.Minute), 0)
	seedShareMetric(m, "legacy", "one", "u:3", "legacy-session", "content1", 1, at)
	seedShareIntent(m, "one", "u:3", "legacy-session", "sid-legacy", "timeline", at)
	visitPV(m, "legacy-reader", "one", "u:1", "legacy-reader-session", "content1", "sid-legacy", at.Add(time.Minute), 0)
	seedShareMetric(m, "foreign-stored", "two", "u:3", "foreign-session", "content1", 1, at)
	foreign, _ := m.Get("share", "tokhash-foreign-stored")
	_ = json.Unmarshal(foreign.Payload, &sh)
	sh.Channel = "timeline"
	foreign.Payload = marshal(sh)
	_ = m.Put(foreign)
	visitPV(m, "foreign-reader", "one", "a:foreign-reader", "foreign-reader-session", "content1", sh.ID, at.Add(time.Minute), 0)
	list, err := s.Visits(context.Background(), scope, Filter{Channel: "timeline", AsOf: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	items := visitItems(t, list)
	if len(items) != 2 {
		t.Fatalf("stored/legacy/foreign channels = %+v", items)
	}
	for _, item := range items {
		if item.Source.Channel != "timeline" || item.Visitor.Key == "a:foreign-reader" {
			t.Fatalf("channel owner leak or stored mutation = %+v", item)
		}
	}
}

func TestEntryAttributionStatesAndNavigationAreNotDirect(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		mutate       func(*Share, *EntryInput)
	}{
		{"valid", "share", nil},
		{"missing", "unattributed", func(_ *Share, in *EntryInput) { in.ShareToken = "" }},
		{"invalid", "invalid_share", func(_ *Share, in *EntryInput) { in.ShareToken = "does-not-exist" }},
		{"expired", "expired_share", func(sh *Share, _ *EntryInput) { sh.Expires = time.Time{} }},
		{"revoked", "revoked_share", func(sh *Share, _ *EntryInput) { sh.Revoked = true }},
		{"wrong-route", "mismatched_share", func(_ *Share, in *EntryInput) { in.Route = "pages/list/list" }},
		{"wrong-version", "mismatched_share", func(sh *Share, _ *EntryInput) { sh.Version++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, scope := fixture(t)
			ctx := context.Background()
			sharer := enter(t, s, 1)
			created, err := s.CreateShare(ctx, ShareInput{Route: sharer.Route, Key: "share-state-0001", Channel: "timeline"}, sharer.Token, 1)
			if err != nil {
				t.Fatal(err)
			}
			in := EntryInput{Route: sharer.Route, ShareToken: created["share_token"].(string), Client: "client-reader-0001", Anonymous: "anonymous-reader-0001", Consent: "v1"}
			if err = s.transaction(ctx, func(r Repository) error {
				row, err := r.Get("share", hash(in.ShareToken))
				if err != nil {
					return err
				}
				var sh Share
				_ = json.Unmarshal(row.Payload, &sh)
				if tc.mutate != nil {
					tc.mutate(&sh, &in)
				}
				row.Payload = marshal(sh)
				return r.Put(row)
			}); err != nil {
				t.Fatal(err)
			}
			reader, err := s.Enter(ctx, in, 0)
			if err != nil || reader.Attribution != tc.status {
				t.Fatalf("entry = %+v, %v, want %s", reader, err, tc.status)
			}
			// Anonymous-to-login and in-app navigation preserve the signed source.
			in.Route, in.ShareToken, in.Previous = "pages/list/list", "", reader.Token
			loggedIn, err := s.Enter(ctx, in, 2)
			if err != nil || loggedIn.Session != reader.Session || loggedIn.Attribution != reader.Attribution || loggedIn.Share != reader.Share {
				t.Fatalf("navigation/login lost source: before=%+v after=%+v err=%v", reader, loggedIn, err)
			}
			pv := ev(s, loggedIn, "attribution-pv-0001", "page_view", s.Now())
			pv.Attribution = "direct" // A client cannot overwrite the server result.
			if rs := accept(t, s, loggedIn, pv); rs[0].Status != "accepted" {
				t.Fatal(rs)
			}
			wantKind, channel := "unknown", "unknown"
			if tc.status == "share" {
				wantKind, channel = "share", "timeline"
			}
			for _, raw := range []bool{false, true} {
				if raw {
					if err = s.transaction(ctx, func(r Repository) error { return r.Remove("summary_state", loggedIn.Session) }); err != nil {
						t.Fatal(err)
					}
				}
				visits, err := s.Visits(ctx, scope, Filter{Channel: channel})
				if err != nil {
					t.Fatal(err)
				}
				items := visitItems(t, visits)
				if len(items) != 1 || items[0].Source.Kind != wantKind || wantKind == "unknown" && items[0].Source.Reason != tc.status {
					t.Fatalf("raw=%v visits = %+v", raw, items)
				}
				direct, err := s.Visits(ctx, scope, Filter{Channel: "direct"})
				if err != nil || len(visitItems(t, direct)) != 0 {
					t.Fatalf("raw=%v unknown/share mislabeled direct: %+v %v", raw, direct, err)
				}
				sessions, err := s.Sessions(ctx, scope, "u:2", Filter{})
				if err != nil {
					t.Fatal(err)
				}
				ss := sessions.Items.([]Session)
				if len(ss) != 1 || ss[0].Source != wantKind || ss[0].Views[0].Attribution != tc.status {
					t.Fatalf("raw=%v session = %+v", raw, ss)
				}
			}
		})
	}
}

type shareReadErrorStore struct{ Store }
type shareReadErrorRepo struct{ Repository }

func (s shareReadErrorStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.Store.Transaction(ctx, tenant, func(r Repository) error { return fn(shareReadErrorRepo{r}) })
}
func (r shareReadErrorRepo) Get(kind, id string) (Record, error) {
	if kind == "share" {
		return Record{}, errors.New("fixture share read unavailable")
	}
	return r.Repository.Get(kind, id)
}

func TestEntryShareStorageFailureIsNotSuccessfulUnknownOrDirect(t *testing.T) {
	s, _ := fixture(t)
	s.Store = shareReadErrorStore{s.Store}
	_, err := s.Enter(context.Background(), EntryInput{Route: "pages/home/home", ShareToken: "supplied-token", Client: "client-reader-0001", Anonymous: "anon-reader-0001", Consent: "v1"}, 0)
	if err == nil || err.Error() != "fixture share read unavailable" {
		t.Fatalf("share repository failure was hidden: %v", err)
	}
}

type entryReadErrorStore struct{ Store }
type entryReadErrorRepo struct{ Repository }

func (s entryReadErrorStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.Store.Transaction(ctx, tenant, func(r Repository) error { return fn(entryReadErrorRepo{r}) })
}
func (r entryReadErrorRepo) Get(kind, id string) (Record, error) {
	if kind == "entry" {
		return Record{}, errors.New("fixture entry read unavailable")
	}
	return r.Repository.Get(kind, id)
}

func TestLoggedInMiniShareRequiresValidEntryButConsoleRemainsIndependent(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	in := ShareInput{Route: "pages/home/home", Key: "require-entry-0001", Channel: "timeline"}
	for _, token := range []string{"", "unknown-entry-0001"} {
		if _, err := s.CreateShare(ctx, in, token, 1); channelErrorStatus(err) != 401 {
			t.Fatal("logged-in share without valid entry was accepted", err)
		}
	}
	en := enter(t, s, 1)
	now := s.Now()
	s.Now = func() time.Time { return now.Add(73 * time.Hour) }
	if _, err := s.CreateShare(ctx, in, en.Token, 1); channelErrorStatus(err) != 401 {
		t.Fatal("expired entry created a detached share", err)
	}
	s.Now = func() time.Time { return now }
	s.Store = entryReadErrorStore{s.Store}
	if _, err := s.CreateShare(ctx, in, en.Token, 1); err == nil || err.Error() != "fixture entry read unavailable" {
		t.Fatal("entry storage failure was hidden", err)
	}
	// A console-created code/link is separately authorized and has no mini-program session.
	in.Channel, in.Key = "", "console-independent-0001"
	if _, err := s.CreateConsoleShare(ctx, in, 1); err != nil {
		t.Fatal("console mint incorrectly required a native entry", err)
	}
}
