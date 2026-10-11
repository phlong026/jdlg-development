package tracking

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This fixture rejects full-tenant reads even though it contains more records
// than a SQL transaction may return. Matching rows retain transaction rollback.
type privacyScopedStore struct {
	rows                reviewStore
	calls               []scopedQueryCall
	failGet, failRemove string
	failPut, failInsert string
	failList            string
}
type privacyScopedRepo struct {
	*privacyScopedStore
	rows reviewStore
}

func (s *privacyScopedStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.rows.Transaction(ctx, tenant, func(r Repository) error {
		return fn(privacyScopedRepo{privacyScopedStore: s, rows: r.(reviewStore)})
	})
}
func (r privacyScopedRepo) Get(kind, id string) (Record, error) {
	if kind+"/"+id == r.failGet {
		return Record{}, ErrUnavailable
	}
	return r.rows.Get(kind, id)
}
func (r privacyScopedRepo) Put(row Record) error {
	if row.Kind == r.failPut {
		return ErrUnavailable
	}
	return r.rows.Put(row)
}
func (r privacyScopedRepo) Insert(row Record) error {
	if row.Kind == r.failInsert {
		return ErrUnavailable
	}
	return r.rows.Insert(row)
}
func (r privacyScopedRepo) Remove(kind, id string) error {
	if kind == r.failRemove {
		return ErrUnavailable
	}
	return r.rows.Remove(kind, id)
}
func (r privacyScopedRepo) List(kind string) ([]Record, error) {
	return nil, fmt.Errorf("privacy attempted unscoped List(%q)", kind)
}
func (r privacyScopedRepo) ListScoped(kind string, q RecordQuery) ([]Record, error) {
	r.calls = append(r.calls, scopedQueryCall{kind: kind, query: q})
	if kind == r.failList {
		return nil, ErrUnavailable
	}
	if q.VisitorKeys == nil && q.Sessions == nil && q.IDs == nil && q.Shares == nil {
		return nil, fmt.Errorf("privacy attempted unbounded scoped query for %s", kind)
	}
	out := []Record{}
	for _, row := range r.rows {
		if row.Kind != kind {
			continue
		}
		match, err := matchesRecordQuery(r.rows, row, q)
		if err != nil {
			return nil, err
		}
		if match {
			out = append(out, row)
		}
	}
	return out, nil
}
func privacyCopy(rows reviewStore) reviewStore {
	out := reviewStore{}
	for key, row := range rows {
		out[key] = row
	}
	return out
}
func privacyFixture(t *testing.T) (*Service, *privacyScopedStore, Scope, time.Time) {
	t.Helper()
	s, rows, scope, now := reviewFixture()
	indexed := &privacyScopedStore{rows: rows}
	s.Store = indexed
	return s, indexed, scope, now
}
func privacySeedGraph(t *testing.T, rows reviewStore, now time.Time) {
	t.Helper()
	reviewPV(rows, "target-view", "one", "a:device", "linked-session", "", now)
	rows.Put(record("identity", "linked-session", "one", now, map[string]string{"visitor_key": "u:1", "original_visitor_key": "a:device"}))
	// Entry and identity legacy rows intentionally have empty metadata columns.
	rows.Put(record("entry", "target-entry", "one", now, Entry{Visitor: "a:device", Session: "linked-session"}))
	rows.Put(record("session", "linked-session", "one", now, Entry{Visitor: "u:1", Session: "linked-session"}))
	rows.Put(record("entry", "direct-entry", "one", now, Entry{Visitor: "u:1", Session: "direct-session"}))
	rows.Put(record("view_binding", "orphan-binding", "one", now, map[string]string{"session": "direct-session"}))
	rows.Put(record("event_hash", "target-view", "one", now, map[string]string{"fingerprint": "hash"}))
	rows.Put(record("view_binding", "target-view", "one", now, map[string]string{"session": "linked-session"}))
	video := Event{ID: "target-video", Type: "video_progress", Session: "linked-session", View: "target-view", Player: "player", Segment: "segment", Seq: 1, Occurred: now, Received: now}
	videoRow := record("event", video.ID, "one", now, video)
	videoRow.Visitor, videoRow.Session = "a:device", "linked-session"
	rows.Put(videoRow)
	rows.Put(record("event_hash", "target-video", "one", now, map[string]string{"fingerprint": "video-hash"}))
	rows.Put(record("sequence", hash("target-view:segment:player:1"), "one", now, "video-hash"))
	rows.Put(record("share", "target-tokenhash", "one", now, Share{ID: "target-share", Sharer: "u:1"}))
	rows.Put(record("share_request", "target-request", "one", now, map[string]string{"share_id": "target-share"}))
	summary := DailySummary{Visitor: "u:1", OriginalVisitor: "a:retained-device", Session: "retained-session", View: PageView{ID: "retained-view"}}
	summaryRow := record("daily_summary", "retained-summary", "one", now, summary)
	summaryRow.Visitor, summaryRow.Session = summary.Visitor, summary.Session
	rows.Put(summaryRow) // Materialized identity remains usable if link is gone.
	rows.Put(record("view_binding", "retained-view", "one", now, map[string]string{"session": "retained-session"}))
	for _, kind := range []string{"summary_state", "business", "crm", "intent_override"} {
		row := record(kind, "target-"+kind, "one", now, map[string]string{"fixture": kind})
		row.Visitor, row.Session = "u:1", "linked-session"
		rows.Put(row)
	}
	rows.Put(record("crm", "u:1", "one", now, map[string]string{"fixture": "legacy-private"}))
	// Same anonymous device, different unverified session and share: preserve.
	reviewPV(rows, "unrelated-view", "one", "a:device", "unrelated-session", "", now)
	rows.Put(record("entry", "unrelated-entry", "one", now, Entry{Visitor: "a:device", Session: "unrelated-session"}))
	rows.Put(record("share", "unrelated-tokenhash", "one", now, Share{ID: "unrelated-share", Sharer: "a:device"}))
	rows.Put(record("share_request", "unrelated-request", "one", now, map[string]string{"share_id": "unrelated-share"}))
}

func TestPrivacyScopedSmallVisitorOperationsInLargeTenant(t *testing.T) {
	s, indexed, scope, now := privacyFixture(t)
	privacySeedGraph(t, indexed.rows, now)
	for i := 0; i <= maxQueryRecords; i++ {
		reviewPV(indexed.rows, fmt.Sprintf("noise-%06d", i), "two", "u:unrelated", "noise-session", "", now)
	}
	s.VerifyCRM = func(_ context.Context, got Scope, visitor, customer, number string) error {
		if !reflect.DeepEqual(got.Owners, []string{"one"}) || visitor != "u:1" {
			t.Fatalf("wrong CRM owner/visitor: %+v %q", got, visitor)
		}
		return nil
	}
	if err := s.LinkCRM(context.Background(), scope, "u:1", "customer", "business-number", "verified"); err != nil {
		t.Fatal(err)
	}
	if err := s.OverrideIntent(context.Background(), scope, "u:1", "high", "reviewed", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkCRM(context.Background(), scope, "u:1"); err != nil {
		t.Fatal(err)
	}
	before := privacyCopy(indexed.rows)
	if err := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); err != nil {
		t.Fatal(err)
	}
	for id, row := range before {
		preserve := strings.HasPrefix(row.ID, "unrelated-") || strings.HasPrefix(row.ID, "noise-") || row.Kind == "audit"
		_, err := indexed.rows.Get(row.Kind, row.ID)
		if preserve && err != nil {
			t.Errorf("unrelated data removed: %s", id)
		}
		if !preserve && !errors.Is(err, ErrNotFound) {
			t.Errorf("target dependency retained: %s", id)
		}
	}
	for _, session := range []string{"linked-session", "direct-session", "retained-session"} {
		if _, err := indexed.rows.Get("deletion_session", session); err != nil {
			t.Errorf("missing session tombstone: %s", session)
		}
	}
	for _, key := range []string{"a:device", "a:retained-device"} {
		if _, err := indexed.rows.Get("deletion", key); !errors.Is(err, ErrNotFound) {
			t.Errorf("anonymous device globally tombstoned: %s", key)
		}
	}
	for _, call := range indexed.calls {
		if call.query.Owners != nil && call.kind != "event" && call.kind != "daily_summary" && call.kind != "crm" && call.kind != "intent_override" {
			t.Fatalf("deletion must inspect owners outside scope: %+v", call)
		}
	}
}

func TestPrivacyScopedDeletionRejectsAnyUncoveredOwnerWithoutMutation(t *testing.T) {
	for _, kind := range []string{"entry", "identity", "share", "share_request", "view_binding", "event_hash", "sequence", "daily_summary", "legacy_crm"} {
		t.Run(kind, func(t *testing.T) {
			s, indexed, scope, now := privacyFixture(t)
			privacySeedGraph(t, indexed.rows, now)
			var row Record
			switch kind {
			case "entry":
				row = record("entry", "forbidden-entry", "two", now, Entry{Visitor: "u:1", Session: "forbidden-session"})
			case "identity":
				row = record("identity", "forbidden-session", "two", now, map[string]string{"visitor_key": "u:1", "original_visitor_key": "a:other-device"})
			case "share":
				row = record("share", "forbidden-tokenhash", "two", now, Share{ID: "forbidden-share", Sharer: "u:1"})
			case "share_request":
				row, _ = indexed.rows.Get(kind, "target-request")
			case "view_binding", "event_hash":
				row, _ = indexed.rows.Get(kind, "target-view")
			case "sequence":
				row, _ = indexed.rows.Get(kind, hash("target-view:segment:player:1"))
			case "daily_summary":
				row, _ = indexed.rows.Get(kind, "retained-summary")
			case "legacy_crm":
				row, _ = indexed.rows.Get("crm", "u:1")
			}
			row.Owner = "two"
			indexed.rows.Put(row)
			before := privacyCopy(indexed.rows)
			err := s.DeleteVisitor(context.Background(), scope, "u:1", "request")
			requireQueryError(t, err, 403)
			if !reflect.DeepEqual(indexed.rows, before) {
				t.Fatal("owner rejection partially deleted data or wrote tombstones")
			}
		})
	}
}

func TestPrivacyScopedDeletionRollsBackEveryFailureStage(t *testing.T) {
	for _, stage := range []string{"identity", "dependency", "tombstone", "remove", "audit"} {
		t.Run(stage, func(t *testing.T) {
			s, indexed, scope, now := privacyFixture(t)
			privacySeedGraph(t, indexed.rows, now)
			switch stage {
			case "identity":
				indexed.failGet = "identity/linked-session"
			case "dependency":
				indexed.failList = "share_request"
			case "tombstone":
				indexed.failPut = "deletion_session"
			case "remove":
				indexed.failRemove = "event"
			case "audit":
				indexed.failInsert = "audit"
			}
			before := privacyCopy(indexed.rows)
			if err := s.DeleteVisitor(context.Background(), scope, "u:1", "request"); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("failure was hidden: %v", err)
			}
			if !reflect.DeepEqual(indexed.rows, before) {
				t.Fatal("transaction failure left partial deletion/tombstones")
			}
		})
	}
}

func TestPrivacyScopedOwnerSelectionAndLookupFailures(t *testing.T) {
	s, indexed, scope, now := privacyFixture(t)
	reviewPV(indexed.rows, "one-view", "one", "a:device", "one-session", "", now)
	indexed.rows.Put(record("identity", "one-session", "one", now, map[string]string{"visitor_key": "u:1", "original_visitor_key": "a:device"}))
	reviewPV(indexed.rows, "two-view", "two", "u:1", "two-session", "", now)
	scope.Owners = []string{"one", "two"}
	err := s.OverrideIntent(context.Background(), scope, "u:1", "high", "reviewed", now.Add(time.Hour))
	requireQueryError(t, err, 409)
	scope.Owners = []string{"one"}
	indexed.failGet = "identity/one-session"
	err = s.OverrideIntent(context.Background(), scope, "u:1", "high", "reviewed", now.Add(time.Hour))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("identity failure treated as anonymous/missing: %v", err)
	}
}

func TestPrivacyScopedDeletionBudgetRejectsWithoutPartialSuccess(t *testing.T) {
	s, indexed, scope, now := privacyFixture(t)
	for i := 0; i <= maxQueryRecords/2; i++ {
		reviewPV(indexed.rows, fmt.Sprintf("target-%06d", i), "one", "u:1", "target-session", "", now)
	}
	before := privacyCopy(indexed.rows)
	err := s.DeleteVisitor(context.Background(), scope, "u:1", "request")
	requireQueryError(t, err, 422)
	if !reflect.DeepEqual(indexed.rows, before) {
		t.Fatal("budget rejection left partial deletion or tombstones")
	}
}

func TestPrivacyScopedAnonymousDeletionKeepsAlreadyIdentifiedSessions(t *testing.T) {
	s, indexed, scope, now := privacyFixture(t)
	reviewPV(indexed.rows, "anonymous-view", "one", "a:device", "anonymous-session", "", now)
	reviewPV(indexed.rows, "identified-view", "one", "a:device", "identified-session", "", now)
	indexed.rows.Put(record("identity", "identified-session", "one", now, map[string]string{"visitor_key": "u:2", "original_visitor_key": "a:device"}))
	if err := s.DeleteVisitor(context.Background(), scope, "a:device", "request"); err != nil {
		t.Fatal(err)
	}
	if _, err := indexed.rows.Get("event", "identified-view"); err != nil {
		t.Fatal("identified account on shared device was deleted")
	}
	if _, err := indexed.rows.Get("identity", "identified-session"); err != nil {
		t.Fatal("other account identity link was deleted")
	}
	if _, err := indexed.rows.Get("deletion_session", "identified-session"); !errors.Is(err, ErrNotFound) {
		t.Fatal("other account session was tombstoned")
	}
}
