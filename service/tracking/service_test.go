package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu   sync.Mutex
	data map[string]map[string]Record
}
type memoryRepo struct{ data map[string]Record }

func (m *memoryStore) Transaction(_ context.Context, tenant string, fn func(Repository) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = map[string]map[string]Record{}
	}
	next := map[string]Record{}
	for k, v := range m.data[tenant] {
		next[k] = v
	}
	e := fn(&memoryRepo{next})
	if e == nil {
		m.data[tenant] = next
	}
	return e
}
func (m *memoryRepo) Get(k, id string) (Record, error) {
	r, ok := m.data[k+":"+id]
	if !ok {
		return r, ErrNotFound
	}
	return r, nil
}
func (m *memoryRepo) List(k string) ([]Record, error) {
	out := []Record{}
	for _, r := range m.data {
		if r.Kind == k {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memoryRepo) Put(r Record) error { m.data[r.Kind+":"+r.ID] = r; return nil }
func (m *memoryRepo) Insert(r Record) error {
	if _, ok := m.data[r.Kind+":"+r.ID]; ok {
		return errors.New("duplicate")
	}
	return m.Put(r)
}
func (m *memoryRepo) Remove(k, id string) error { delete(m.data, k+":"+id); return nil }
func fixture(t *testing.T) (*Service, Scope) {
	t.Helper()
	s := New(&memoryStore{}, Config{Enabled: true, ContentEnabled: true, VideoEnabled: true, IntentEnabled: true, Tenant: "org-a", DefaultOwner: "owner-a", ConsentVersion: "v1", RawDays: 90, Routes: map[string]Route{"pages/home/home": {Title: "首页", Public: true}, "pages/list/list": {Title: "列表", Public: true}, "pages/contentreader/contentreader": {Title: "文章", Public: true}}})
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	return s, Scope{Tenant: "org-a", Actor: 7, Owners: []string{"owner-a"}, View: true, Manage: true, CRM: true, Delete: true, Rules: true}
}
func enter(t *testing.T, s *Service, account int64) Entry {
	t.Helper()
	en, e := s.Enter(context.Background(), EntryInput{Route: "pages/home/home", Client: "client-0000001", Anonymous: "anonymous-0001", Consent: "v1"}, account)
	if e != nil {
		t.Fatal(e)
	}
	return en
}
func ev(s *Service, en Entry, id, typ string, at time.Time) Event {
	return Event{Schema: 1, ID: id, Type: typ, Session: en.Session, View: "pageview-0001", Route: en.Route, Content: en.Content, Version: en.Version, Share: en.Share, Occurred: at, AppState: "foreground", PageState: "visible", Monotonic: 20000}
}
func accept(t *testing.T, s *Service, en Entry, events ...Event) []Result {
	t.Helper()
	rs, e := s.Accept(context.Background(), en.Token, events)
	if e != nil {
		t.Fatal(e)
	}
	return rs
}
func TestCollectionDisabledAndConsent(t *testing.T) {
	s, _ := fixture(t)
	s.Config.Enabled = false
	if _, e := s.Enter(context.Background(), EntryInput{}, 0); e == nil {
		t.Fatal("disabled accepted")
	}
	s.Config.Enabled = true
	if _, e := s.Enter(context.Background(), EntryInput{Client: "client-000001", Route: "pages/home/home"}, 0); e == nil {
		t.Fatal("no consent accepted")
	}
}
func TestShareIntentDoesNotCreatePV(t *testing.T) {
	s, scope := fixture(t)
	en := enter(t, s, 1)
	share, e := s.CreateShare(context.Background(), ShareInput{Route: en.Route, Key: "request-000001"}, en.Token, 1)
	if e != nil {
		t.Fatal(e)
	}
	in := ev(s, en, "intent-000001", "share_intent", s.Now())
	in.View = ""
	in.Share = share["share_id"].(string)
	rs := accept(t, s, en, in)
	if rs[0].Status != "accepted" {
		t.Fatal(rs)
	}
	out, e := s.Visitors(context.Background(), scope, Filter{})
	if e != nil || out.Identified != 0 {
		t.Fatalf("%+v %v", out, e)
	}
}
func TestEventDuplicateConflictAndPVIdempotency(t *testing.T) {
	s, scope := fixture(t)
	en := enter(t, s, 1)
	pv := ev(s, en, "event-000001", "page_view", s.Now())
	rs := accept(t, s, en, pv, pv)
	if rs[0].Status != "accepted" || rs[1].Status != "duplicate" {
		t.Fatal(rs)
	}
	pv.Monotonic++
	if accept(t, s, en, pv)[0].Status != "rejected" {
		t.Fatal("ID conflict")
	}
	pv.ID = "event-000002"
	accept(t, s, en, pv)
	out, e := s.Visitors(context.Background(), scope, Filter{})
	if e != nil {
		t.Fatal(e)
	}
	items := out.Items.([]Visitor)
	if len(items) != 1 || items[0].Views != 1 {
		t.Fatal(items)
	}
}
func TestOutOfOrderAndVisibleIntervals(t *testing.T) {
	s, scope := fixture(t)
	en := enter(t, s, 1)
	start := s.Now()
	s.Now = func() time.Time { return start.Add(time.Minute) }
	seg := ev(s, en, "event-seg001", "visible_segment", start.Add(10*time.Second))
	seg.Segment = "segment-0001"
	seg.Seq = 1
	seg.Delta = 10000
	seg.Monotonic = 10000
	end := ev(s, en, "event-end001", "page_end", start.Add(20*time.Second))
	pv := ev(s, en, "event-pv0001", "page_view", start)
	accept(t, s, en, end, seg)
	out, _ := s.Visitors(context.Background(), scope, Filter{})
	if out.Identified != 0 {
		t.Fatal("heartbeat became PV")
	}
	accept(t, s, en, pv)
	out, e := s.Visitors(context.Background(), scope, Filter{})
	if e != nil {
		t.Fatal(e)
	}
	item := out.Items.([]Visitor)[0]
	if item.Visible != 10000 || item.Views != 1 {
		t.Fatal(item)
	}
	seg.ID = "event-seg002"
	seg.Delta = 5000
	if accept(t, s, en, seg)[0].Status != "rejected" {
		t.Fatal("sequence conflict accepted")
	}
}
func TestOverlappingSegmentsNotDoubleCounted(t *testing.T) {
	s, scope := fixture(t)
	en := enter(t, s, 1)
	start := s.Now()
	s.Now = func() time.Time { return start.Add(time.Minute) }
	pv := ev(s, en, "event-pv0001", "page_view", start)
	seg := ev(s, en, "event-seg001", "visible_segment", start.Add(10*time.Second))
	seg.Segment = "segment-0001"
	seg.Seq = 1
	seg.Delta = 10000
	copy := seg
	copy.ID = "event-seg002"
	copy.Segment = "segment-0002"
	accept(t, s, en, pv, seg, copy)
	out, _ := s.Visitors(context.Background(), scope, Filter{})
	if out.Items.([]Visitor)[0].Visible != 10000 {
		t.Fatal(out)
	}
}
func TestCrossMidnightClipsVisibleDuration(t *testing.T) {
	s, scope := fixture(t)
	start := time.Date(2026, 10, 11, 15, 59, 55, 0, time.UTC)
	s.Now = func() time.Time { return start }
	en := enter(t, s, 1)
	s.Now = func() time.Time { return start.Add(20 * time.Second) }
	pv := ev(s, en, "event-pv0001", "page_view", start)
	seg := ev(s, en, "event-seg001", "visible_segment", start.Add(10*time.Second))
	seg.Segment = "segment-0001"
	seg.Seq = 1
	seg.Delta = 10000
	accept(t, s, en, pv, seg)
	out, e := s.Visitors(context.Background(), scope, Filter{Start: start.Add(5 * time.Second), End: start.Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	v := out.Items.([]Visitor)[0]
	if v.Views != 0 || v.Visible != 5000 {
		t.Fatal(v)
	}
}
func TestClientIdentityAndBusinessCannotBeForged(t *testing.T) {
	s, _ := fixture(t)
	en := enter(t, s, 1)
	for i, typ := range []string{"business_action_confirmed", "unknown"} {
		e := ev(s, en, fmt.Sprintf("event-00000%d", i), typ, s.Now())
		if accept(t, s, en, e)[0].Status != "rejected" {
			t.Fatal(typ)
		}
	}
	e := ev(s, en, "event-000003", "page_view", s.Now())
	e.Session = "session-spoof"
	if accept(t, s, en, e)[0].Status != "rejected" {
		t.Fatal("session spoof")
	}
	e.Session = en.Session
	e.Route = "pages/private/private"
	if accept(t, s, en, e)[0].Status != "rejected" {
		t.Fatal("route spoof")
	}
}
func TestBackgroundAndTooLongSegmentRejected(t *testing.T) {
	s, _ := fixture(t)
	en := enter(t, s, 1)
	e := ev(s, en, "event-000001", "visible_segment", s.Now())
	e.Segment = "segment-0001"
	e.Seq = 1
	e.Delta = 16000
	if accept(t, s, en, e)[0].Status != "rejected" {
		t.Fatal("long interval")
	}
	e.ID = "event-000002"
	e.Delta = 10000
	e.AppState = "background"
	if accept(t, s, en, e)[0].Status != "rejected" {
		t.Fatal("background")
	}
}
func TestServerVerifiedIdentityLinkCurrentSessionOnly(t *testing.T) {
	s, scope := fixture(t)
	anon := enter(t, s, 0)
	pv := ev(s, anon, "event-pv0001", "page_view", s.Now())
	accept(t, s, anon, pv)
	logged, e := s.Enter(context.Background(), EntryInput{Route: anon.Route, Client: anon.Client, Previous: anon.Token, Consent: "v1"}, 42)
	if e != nil {
		t.Fatal(e)
	}
	if logged.Session != anon.Session || logged.Visitor != "u:42" {
		t.Fatal(logged)
	}
	out, e := s.Visitors(context.Background(), scope, Filter{})
	if e != nil || out.Identified != 1 || out.Anonymous != 0 {
		t.Fatalf("%+v %v", out, e)
	}
	switched, e := s.Enter(context.Background(), EntryInput{Route: anon.Route, Client: anon.Client, Previous: logged.Token, Consent: "v1"}, 43)
	if e != nil || switched.Session == logged.Session {
		t.Fatal("account switch reused session")
	}
}
func TestNewShareAndDirectEntryAttribution(t *testing.T) {
	s, _ := fixture(t)
	a := enter(t, s, 1)
	sh, e := s.CreateShare(context.Background(), ShareInput{Route: a.Route, Key: "request-000001"}, a.Token, 1)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Enter(context.Background(), EntryInput{Route: a.Route, Client: "client-000002", ShareToken: sh["share_token"].(string), Consent: "v1"}, 2)
	if e != nil || b.Share == "" {
		t.Fatal(b, e)
	}
	direct, e := s.Enter(context.Background(), EntryInput{Route: a.Route, Client: "client-000002", Consent: "v1"}, 2)
	if e != nil || direct.Share != "" {
		t.Fatal(direct, e)
	}
	forged, e := s.Enter(context.Background(), EntryInput{Route: a.Route, Client: "client-000003", ShareToken: "tampered-token", Consent: "v1"}, 3)
	if e != nil || forged.Share != "" {
		t.Fatal(forged, e)
	}
}
func TestRevokedContentAndReadWithoutAnalytics(t *testing.T) {
	s, scope := fixture(t)
	d, e := s.SaveContent(context.Background(), scope, "", Draft{Title: "测试文章", Body: "<script>alert(1)</script>必须按纯文本展示", Rights: "own", Owner: "owner-a"})
	if e != nil {
		t.Fatal(e)
	}
	pv, e := s.Preview(context.Background(), scope, d.ID, d.Revision)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.Publish(context.Background(), scope, d.ID, d.Revision, pv["preview_token"].(string), "publish-00001")
	if e != nil {
		t.Fatal(e)
	}
	c := p["content"].(Content)
	s.Config.Enabled = false
	if _, e = s.ReadContent(context.Background(), c.ID, c.Version); e != nil {
		t.Fatal("read wrongly depends on analytics", e)
	}
	if _, e = s.Revoke(context.Background(), scope, c.ID, c.Revision, "作者撤回", "revoke-00001"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReadContent(context.Background(), c.ID, c.Version); e == nil {
		t.Fatal("revoke still readable")
	}
}
func TestPaginationSnapshotAndScope(t *testing.T) {
	s, scope := fixture(t)
	for i := int64(1); i <= 3; i++ {
		en := enter(t, s, i)
		e := ev(s, en, fmt.Sprintf("event-pv%04d", i), "page_view", s.Now())
		e.View = fmt.Sprintf("pageview-%04d", i)
		accept(t, s, en, e)
	}
	first, e := s.Visitors(context.Background(), scope, Filter{Limit: 1})
	if e != nil || first.Cursor == "" {
		t.Fatal(first, e)
	}
	now := s.Now()
	s.Now = func() time.Time { return now.Add(time.Minute) }
	next, e := s.Visitors(context.Background(), scope, Filter{Limit: 1, AsOf: first.AsOf, Cursor: first.Cursor})
	if e != nil {
		t.Fatal(e)
	}
	if first.Items.([]Visitor)[0].Key == next.Items.([]Visitor)[0].Key {
		t.Fatal("repeated page")
	}
	other := scope
	other.Owners = []string{"owner-b"}
	out, e := s.Visitors(context.Background(), other, Filter{})
	if e != nil || out.Identified != 0 {
		t.Fatal("scope leaked")
	}
	other.Tenant = "org-b"
	if _, e = s.Visitors(context.Background(), other, Filter{}); e == nil {
		t.Fatal("tenant spoof")
	}
}
func TestBatchLimitAndRate(t *testing.T) {
	s, _ := fixture(t)
	en := enter(t, s, 1)
	if _, e := s.Accept(context.Background(), en.Token, make([]Event, 51)); e == nil {
		t.Fatal("oversize")
	}
	for i := 0; i < 12; i++ {
		if _, e := s.Accept(context.Background(), en.Token, make([]Event, 50)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.Accept(context.Background(), en.Token, []Event{{}}); e == nil {
		t.Fatal("rate not applied")
	}
}
func TestRecordsMarshalDoesNotPersistCredential(t *testing.T) {
	s, _ := fixture(t)
	en := enter(t, s, 1)
	_ = s.transaction(context.Background(), func(r Repository) error {
		row, e := r.Get("entry", hash(en.Token))
		if e != nil {
			return e
		}
		var got map[string]any
		_ = json.Unmarshal(row.Payload, &got)
		if _, ok := got["entry_token"]; ok {
			t.Fatal("raw entry token persisted")
		}
		return nil
	})
}
func TestVerifiedFavoriteEvidenceAndDuplicateCap(t *testing.T) {
	s, scope := fixture(t)
	first := enter(t, s, 8)
	accept(t, s, first, ev(s, first, "favorite-pv1", "page_view", s.Now()))
	second := enter(t, s, 8)
	pv := ev(s, second, "favorite-pv2", "page_view", s.Now())
	pv.View = "favorite-view2"
	accept(t, s, second, pv)
	if e := s.ConfirmFavorite(context.Background(), second.Token, 99, "template", 4); e == nil {
		t.Fatal("account spoof favorite")
	}
	if e := s.ConfirmFavorite(context.Background(), second.Token, 8, "template", 4); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfirmFavorite(context.Background(), second.Token, 8, "template", 4); e != nil {
		t.Fatal(e)
	}
	out, e := s.Visitors(context.Background(), scope, Filter{})
	if e != nil {
		t.Fatal(e)
	}
	v := out.Items.([]Visitor)[0]
	if v.Intent.Level != "medium" {
		t.Fatal(v.Intent)
	}
	_ = s.transaction(context.Background(), func(r Repository) error {
		rows, e := r.List("business")
		if e != nil {
			return e
		}
		if len(rows) != 1 {
			t.Fatal("duplicate favorite overcount", len(rows))
		}
		return nil
	})
}

func TestSourceChannelRejectsArbitraryPayload(t *testing.T) {
	s, _ := fixture(t)
	en := enter(t, s, 0)
	event := ev(s, en, "channel-invalid-0001", "page_view", s.Now())
	event.SourceChannel = "untrusted-personal-field"
	result := accept(t, s, en, event)
	if result[0].Status != "rejected" {
		t.Fatalf("unexpected result %#v", result)
	}
}

func TestOutOfOrderNavigationPredecessorReconciles(t *testing.T) {
	s, scope := fixture(t)
	en := enter(t, s, 0)
	second := ev(s, en, "event-second-0001", "page_view", s.Now())
	second.View = "view-second-0001"
	second.Previous = "view-first-0001"
	if got := accept(t, s, en, second); got[0].Status != "accepted" {
		t.Fatal(got)
	}
	before, e := s.Events(context.Background(), scope, second.View, Filter{})
	if e != nil || before.Items.([]Event)[0].Previous != "" {
		t.Fatalf("unverified edge exposed: %#v %v", before, e)
	}
	first := ev(s, en, "event-first-0001", "page_view", s.Now().Add(-time.Second))
	first.View = second.Previous
	if got := accept(t, s, en, first); got[0].Status != "accepted" {
		t.Fatal(got)
	}
	after, e := s.Events(context.Background(), scope, second.View, Filter{})
	if e != nil || after.Items.([]Event)[0].Previous != first.View {
		t.Fatalf("edge not reconciled: %#v %v", after, e)
	}
}
