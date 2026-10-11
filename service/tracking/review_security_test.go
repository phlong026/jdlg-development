package tracking

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type reviewStore map[string]Record

func (m reviewStore) Transaction(_ context.Context, _ string, fn func(Repository) error) error {
	copy := reviewStore{}
	for k, v := range m {
		copy[k] = v
	}
	if e := fn(copy); e != nil {
		return e
	}
	for k := range m {
		delete(m, k)
	}
	for k, v := range copy {
		m[k] = v
	}
	return nil
}
func (m reviewStore) Get(k, id string) (Record, error) {
	v, ok := m[k+"/"+id]
	if !ok {
		return v, ErrNotFound
	}
	return v, nil
}
func (m reviewStore) List(k string) ([]Record, error) {
	out := []Record{}
	for _, v := range m {
		if v.Kind == k {
			out = append(out, v)
		}
	}
	return out, nil
}
func (m reviewStore) Put(v Record) error { m[v.Kind+"/"+v.ID] = v; return nil }
func (m reviewStore) Insert(v Record) error {
	if _, e := m.Get(v.Kind, v.ID); e == nil {
		return fail(409, "duplicate")
	}
	return m.Put(v)
}
func (m reviewStore) Remove(k, id string) error { delete(m, k+"/"+id); return nil }
func reviewFixture() (*Service, reviewStore, Scope, time.Time) {
	m := reviewStore{}
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	s := New(m, Config{Tenant: "tenant", Enabled: true, ContentEnabled: true, IntentEnabled: true, RawDays: 90, Routes: map[string]Route{"page": {Title: "page", Public: true}, "pages/contentreader/contentreader": {Public: true}}})
	s.Now = func() time.Time { return now }
	return s, m, Scope{Tenant: "tenant", Actor: 1, Owners: []string{"one"}, View: true, Manage: true, CRM: true, Rules: true, Delete: true}, now
}
func reviewPV(m reviewStore, id, owner, key, session, content string, at time.Time) {
	ev := Event{ID: id, Type: "page_view", View: id, Session: session, Route: "page", Content: content, Occurred: at, Received: at}
	rec := record("event", id, owner, at, ev)
	rec.Visitor, rec.Session = key, session
	_ = m.Put(rec)
}
func TestReviewViewIsSeparateFromManage(t *testing.T) {
	s, m, scope, now := reviewFixture()
	reviewPV(m, "view0001", "one", "u:1", "session1", "content1", now.Add(-time.Hour))
	_ = m.Put(record("content", "content1", "one", now, Content{ID: "content1", Owner: "one", PageViews: 99, VisibleMS: 99}))
	scope.View = false
	if _, e := s.Visitors(context.Background(), scope, Filter{}); e == nil {
		t.Fatal("manager read visitors")
	}
	if _, e := s.Sessions(context.Background(), scope, "u:1", Filter{}); e == nil {
		t.Fatal("manager read sessions")
	}
	if _, e := s.Events(context.Background(), scope, "view0001", Filter{}); e == nil {
		t.Fatal("manager read events")
	}
	got, e := s.Contents(context.Background(), scope, Filter{}, "")
	if e != nil {
		t.Fatal(e)
	}
	c := got.Items.([]Content)[0]
	if c.PageViews != 0 || c.VisibleMS != 0 || c.LastVisit != nil {
		t.Fatal("manager received metrics")
	}
}
func TestReviewOwnerPrivacyAndVerifiedCRM(t *testing.T) {
	s, m, scope, now := reviewFixture()
	reviewPV(m, "view0001", "one", "u:1", "session1", "", now.Add(-time.Hour))
	reviewPV(m, "view0002", "two", "u:1", "session2", "", now.Add(-time.Hour))
	other := scope
	other.Owners = []string{"two"}
	if e := s.LinkCRM(context.Background(), scope, "u:1", "customer1", "private", "because"); e == nil {
		t.Fatal("client customer id accepted without upstream validation")
	}
	s.VerifyCRM = func(context.Context, Scope, string, string, string) error { return nil }
	if e := s.LinkCRM(context.Background(), scope, "u:1", "customer1", "private", "because"); e != nil {
		t.Fatal(e)
	}
	if e := s.OverrideIntent(context.Background(), scope, "u:1", "high", "private note", now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	got, e := s.Visitors(context.Background(), other, Filter{})
	if e != nil {
		t.Fatal(e)
	}
	v := got.Items.([]Visitor)[0]
	if v.CRMLabel != "" || v.Intent.Note != "" || v.Intent.Level == "high" {
		t.Fatalf("owner leak: %+v", v)
	}
	scope.Owners = []string{"one", "two"}
	if e := s.OverrideIntent(context.Background(), scope, "u:1", "high", "mixed", now.Add(time.Hour)); e == nil {
		t.Fatal("ambiguous owner accepted")
	}
	got, e = s.Visitors(context.Background(), scope, Filter{Owner: "two"})
	if e != nil {
		t.Fatal(e)
	}
	v = got.Items.([]Visitor)[0]
	if v.CRMLabel != "" || v.Intent.Note != "" {
		t.Fatal("owner filter leaked private data")
	}
}
func TestReviewDeletionKeepsOtherDeviceSessions(t *testing.T) {
	s, m, scope, now := reviewFixture()
	reviewPV(m, "view0001", "one", "a:device", "session1", "", now.Add(-time.Hour))
	reviewPV(m, "view0002", "one", "a:device", "session2", "", now.Add(-2*time.Hour))
	_ = m.Put(record("identity", "session1", "one", now.Add(-time.Minute), map[string]string{"visitor_key": "u:1", "original_visitor_key": "a:device"}))
	if e := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Get("deletion", "a:device"); e == nil {
		t.Fatal("entire anonymous device tombstoned")
	}
	if _, e := m.Get("event", "view0002"); e != nil {
		t.Fatal("historical unrelated session deleted")
	}
	if _, e := m.Get("event", "view0001"); e == nil {
		t.Fatal("linked session retained")
	}
	if _, e := m.Get("deletion_session", "session1"); e != nil {
		t.Fatal("session replay not tombstoned")
	}
	got, e := s.Visitors(context.Background(), scope, Filter{})
	if e != nil {
		t.Fatal(e)
	}
	if got.Anonymous != 1 || got.Identified != 0 {
		t.Fatalf("wrong survivors: %+v", got)
	}
}
func TestReviewFilteredCountsSnapshotAndContentSort(t *testing.T) {
	s, m, scope, now := reviewFixture()
	reviewPV(m, "view0001", "one", "u:1", "session1", "content1", now.Add(-2*time.Hour))
	reviewPV(m, "view0002", "one", "u:2", "session2", "content2", now.Add(-time.Hour))
	reviewPV(m, "view0003", "two", "u:3", "session3", "content2", now.Add(-time.Minute))
	for _, id := range []string{"content1", "content2"} {
		_ = m.Put(record("content", id, "one", now, Content{ID: id, Owner: "one", Title: id}))
	}
	f := Filter{AsOf: now, Limit: 1}
	first, e := s.Visitors(context.Background(), scope, f)
	if e != nil {
		t.Fatal(e)
	}
	if first.Identified != 2 || first.Items.([]Visitor)[0].Key != "u:2" || first.Cursor == "" {
		t.Fatalf("bad first page %+v", first)
	}
	f.Cursor = first.Cursor
	second, e := s.Visitors(context.Background(), scope, f)
	if e != nil {
		t.Fatal(e)
	}
	if second.Identified != 2 || second.Items.([]Visitor)[0].Key != "u:1" {
		t.Fatal("unstable count/page")
	}
	f.Sort = "page_views"
	if _, e = s.Visitors(context.Background(), scope, f); e == nil {
		t.Fatal("cursor accepted with different filter")
	}
	got, e := s.Contents(context.Background(), scope, Filter{Search: "does not match", Identity: "anonymous", Intent: "high", Sort: "last_visit"}, "")
	if e != nil {
		t.Fatal(e)
	}
	items := got.Items.([]Content)
	if len(items) != 2 || items[0].ID != "content2" || items[0].PageViews != 1 {
		t.Fatalf("bad content filters/sort %+v", items)
	}
	// A link established after the snapshot must not rewrite a previous page.
	reviewPV(m, "view0004", "one", "a:device", "session4", "", now.Add(-time.Hour))
	_ = m.Put(record("identity", "session4", "one", now, map[string]string{"visitor_key": "u:4", "original_visitor_key": "a:device"}))
	got, e = s.Visitors(context.Background(), scope, Filter{AsOf: now.Add(-time.Minute)})
	if e != nil {
		t.Fatal(e)
	}
	if got.Anonymous != 1 {
		t.Fatal("future identity mutated snapshot")
	}
}
func TestReviewOldVersionDraftAndRevoke(t *testing.T) {
	s, m, scope, now := reviewFixture()
	c := Content{ID: "content1", Owner: "one", Version: 1, Revision: 2, Status: "published", Title: "old", Body: "text", Rights: "own"}
	_ = m.Put(record("content", c.ID, c.Owner, now, c))
	_ = m.Put(record("version", contentKey(c.ID, 1), c.Owner, now, c))
	draft, e := s.SaveContent(context.Background(), scope, c.ID, Draft{Title: "new", Body: "new", Owner: "one", Rights: "own", Revision: 2})
	if e != nil {
		t.Fatal(e)
	}
	old, e := s.ReadContent(context.Background(), c.ID, 1)
	if e != nil || old.Title != "old" {
		t.Fatalf("published old version lost: %v %+v", e, old)
	}
	revoked, e := s.Revoke(context.Background(), scope, c.ID, draft.Revision, "request", "revoke01")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadContent(context.Background(), c.ID, 1); e == nil {
		t.Fatal("revoked version readable")
	}
	if _, e = s.SaveContent(context.Background(), scope, c.ID, Draft{Title: "new", Body: "new", Owner: "one", Rights: "own", Revision: revoked.Revision}); e == nil {
		t.Fatal("revoked content silently reactivated")
	}
	rows, _ := m.List("audit")
	for _, row := range rows {
		var a map[string]any
		_ = json.Unmarshal(row.Payload, &a)
		if a["action"] == "revoke_content" {
			d := a["detail"].(map[string]any)
			if _, ok := d["reason"]; ok {
				t.Fatal("raw reason persisted in audit")
			}
		}
	}
}

func TestReviewDeletionCleansAuxiliaryAndFailsClosed(t *testing.T) {
	s, m, scope, now := reviewFixture()
	reviewPV(m, "view0001", "one", "u:1", "session1", "", now.Add(-time.Hour))
	_ = m.Put(record("event_hash", "view0001", "one", now, map[string]string{"fingerprint": "hash"}))
	_ = m.Put(record("view_binding", "view0001", "one", now, map[string]string{"session": "session1"}))
	_ = m.Put(record("share", "tokenhash", "one", now, Share{ID: "share001", Sharer: "u:1"}))
	_ = m.Put(record("share_request", "request1", "one", now, map[string]string{"share_id": "share001"}))
	reviewPV(m, "view0002", "two", "u:1", "session2", "", now.Add(-time.Hour))
	if e := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); e == nil {
		t.Fatal("partial scope account deletion succeeded")
	}
	if _, e := m.Get("event", "view0001"); e != nil {
		t.Fatal("failed deletion modified records")
	}
	scope.Owners = []string{"one", "two"}
	if e := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"event", "event_hash", "view_binding", "share", "share_request"} {
		rows, _ := m.List(kind)
		if len(rows) != 0 {
			t.Fatalf("personal auxiliary retained: %s", kind)
		}
	}
}
func TestReviewNoMediumIntentFromRepeatVisitsAlone(t *testing.T) {
	s, m, scope, now := reviewFixture()
	reviewPV(m, "view0001", "one", "u:1", "session1", "", now.Add(-time.Hour))
	reviewPV(m, "view0002", "one", "u:1", "session2", "", now.Add(-time.Minute))
	got, e := s.Visitors(context.Background(), scope, Filter{})
	if e != nil {
		t.Fatal(e)
	}
	if got.Items.([]Visitor)[0].Intent.Level != "low" {
		t.Fatal("repeat visits fabricated medium intent")
	}
}

type reviewCountRepo struct {
	Repository
	lists map[string]int
}

func (r reviewCountRepo) List(kind string) ([]Record, error) {
	r.lists[kind]++
	return r.Repository.List(kind)
}

type reviewCountStore struct {
	source reviewStore
	lists  map[string]int
}

func (s reviewCountStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.source.Transaction(ctx, tenant, func(r Repository) error { return fn(reviewCountRepo{r, s.lists}) })
}
func TestReviewVisitorIntentDoesNotRescanPerVisitor(t *testing.T) {
	s, m, scope, now := reviewFixture()
	for i := 0; i < 100; i++ {
		id := time.Unix(int64(i), 0).Format("150405")
		reviewPV(m, "view"+id, "one", "u:"+id, "session"+id, "", now.Add(-time.Hour))
	}
	calls := map[string]int{}
	s.Store = reviewCountStore{m, calls}
	got, e := s.Visitors(context.Background(), scope, Filter{})
	if e != nil {
		t.Fatal(e)
	}
	if got.Identified != 100 {
		t.Fatal("visitor count")
	}
	if calls["event"] > 2 || calls["business"] > 1 || calls["crm"] > 1 || calls["intent_override"] > 1 {
		t.Fatalf("per-visitor repeated full scan: %+v", calls)
	}
}

func TestDeletedLoginCannotCreateNewShares(t *testing.T) {
	s, scope := fixture(t)
	en := enter(t, s, 1)
	accept(t, s, en, ev(s, en, "delete-share-view1", "page_view", s.Now()))
	if e := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateShare(context.Background(), ShareInput{Route: en.Route, Key: "new-share-request"}, en.Token, 1); e == nil {
		t.Fatal("deleted account recreated share history")
	}
}
func TestPrivateAnnotationsRemainRemovableAfterRawRetention(t *testing.T) {
	s, scope := fixture(t)
	en := enter(t, s, 1)
	accept(t, s, en, ev(s, en, "retained-view-0001", "page_view", s.Now()))
	if e := s.transaction(context.Background(), func(r Repository) error {
		rows, _ := r.List("event")
		for _, row := range rows {
			if e := r.Remove(row.Kind, row.ID); e != nil {
				return e
			}
		}
		owner, e := privateOwner(r, scope, "u:1")
		if e != nil {
			return e
		}
		if owner != "owner-a" {
			t.Fatal(owner)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func TestAnonymousLinkCannotBeReassignedByReplayedCredential(t *testing.T) {
	s, _ := fixture(t)
	en := enter(t, s, 0)
	input := EntryInput{Route: en.Route, Client: en.Client, Anonymous: en.Anonymous, Consent: "v1", Previous: en.Token}
	a, e := s.Enter(context.Background(), input, 11)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Enter(context.Background(), input, 11)
	if e != nil || again.Session != a.Session {
		t.Fatal("same-account retry", e)
	}
	b, e := s.Enter(context.Background(), input, 12)
	if e != nil || b.Session == a.Session {
		t.Fatal("cross-account reuse", e)
	}
	logout, e := s.Enter(context.Background(), input, 0)
	if e != nil || logout.Session == a.Session {
		t.Fatal("anonymous replay rejoined identified session", e)
	}
	if e := s.transaction(context.Background(), func(r Repository) error {
		identity, e := decode[map[string]string](r, "identity", en.Session)
		if e != nil {
			return e
		}
		if identity["visitor_key"] != "u:11" {
			t.Fatal(identity)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
