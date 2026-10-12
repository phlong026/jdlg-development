package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Original business-review regression, retained unchanged in behavior.
func TestReviewerOverrideSnapshot(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	now := s.Now()
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-0000001", now.Add(-time.Minute))
	if e := s.OverrideIntent(ctx, scope, "u:1", "high", "first reviewed override", now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	before := ruleVisitor(t, s, scope, Filter{AsOf: now})
	s.Now = func() time.Time { return now.Add(time.Minute) }
	if e := s.OverrideIntent(ctx, scope, "u:1", "medium", "second reviewed override", now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	after := ruleVisitor(t, s, scope, Filter{AsOf: now})
	t.Logf("same snapshot before=%s after=%s", before.Intent.Level, after.Intent.Level)
	if before.Intent.Level != after.Intent.Level {
		t.Fatal("overwriting current override erased its earlier snapshot")
	}
}
func TestManualOverrideCompleteImmutableDecisionsAndLatestExpiry(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	now := s.Now()
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-0000001", now.Add(-time.Minute))
	if e := s.OverrideIntent(ctx, scope, "u:1", "high", "private first complete reason", now.Add(24*time.Hour)); e != nil {
		t.Fatal(e)
	}
	second := scope
	second.Actor = 8
	s.Now = func() time.Time { return now.Add(time.Minute) }
	if e := s.OverrideIntent(ctx, second, "u:1", "medium", "second complete reason", now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	if e := s.transaction(ctx, func(r Repository) error {
		rows, e := scopedRecords(r, "intent_override_history", RecordQuery{Owners: scope.Owners, VisitorKeys: []string{"u:1"}})
		if e != nil {
			return e
		}
		if len(rows) != 2 {
			t.Fatalf("history lost: %d", len(rows))
		}
		for _, row := range rows {
			d, e := readOverrideDecision(row)
			if e != nil {
				return e
			}
			if d.Revision == 1 && (d.Reason != "private first complete reason" || d.Actor != 7 || !d.Expires.Equal(now.Add(24*time.Hour)) || !d.Created.Equal(now)) {
				t.Fatalf("old decision changed: %+v", d)
			}
			if d.Revision == 2 && (d.Previous != overrideDecisionID("owner-a", "u:1", 1) || d.Actor != 8 || d.Reason != "second complete reason") {
				t.Fatalf("new decision invalid: %+v", d)
			}
		}
		audits, e := r.List("audit")
		if e != nil {
			return e
		}
		count := 0
		for _, row := range audits {
			var a struct {
				Action string `json:"action"`
				Actor  int64  `json:"actor"`
				Detail struct {
					ID   string `json:"decision_id"`
					Hash string `json:"decision_hash"`
				} `json:"detail"`
			}
			if json.Unmarshal(row.Payload, &a) != nil {
				t.Fatal("bad audit")
			}
			if a.Action != "intent_override" {
				continue
			}
			count++
			decision, e := r.Get("intent_override_history", a.Detail.ID)
			if e != nil {
				return e
			}
			stored, err := readOverrideDecision(decision)
			if err != nil {
				return err
			}
			if overrideDecisionDigest(stored) != a.Detail.Hash {
				t.Fatal("audit cannot verify complete decision")
			}
			if strings.Contains(string(row.Payload), "complete reason") {
				t.Fatal("private reason duplicated in permanent audit")
			}
		}
		if count != 2 {
			t.Fatalf("audit count=%d", count)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	old := ruleVisitor(t, s, scope, Filter{AsOf: now}).Intent
	if old.Override == nil || old.Override.Actor != 7 || old.Override.Reason != "private first complete reason" {
		t.Fatalf("old decision not replayed: %+v", old)
	}
	s.Now = func() time.Time { return now.Add(2 * time.Hour) }
	got := ruleVisitor(t, s, scope, Filter{}).Intent
	if got.Level != "low" || got.OverrideState != "expired" || got.Override != nil {
		t.Fatalf("expired latest decision resurrected its predecessor: %+v", got)
	}
}
func seedOverrideVisitors(t *testing.T, s *Service, scope Scope, n int, legacy bool) {
	t.Helper()
	now := s.Now()
	for i := 1; i <= n; i++ {
		key := fmt.Sprintf("u:%d", i)
		ruleFixturePV(t, s, scope.Owners[0], key, fmt.Sprintf("session-%d", i), fmt.Sprintf("view-%08d", i), now.Add(-time.Minute))
		if legacy {
			if e := s.transaction(context.Background(), func(r Repository) error {
				row := record("intent_override", privateKey(scope.Owners[0], key), scope.Owners[0], now, IntentOverride{Level: "high", Reason: "legacy complete reason", Actor: scope.Actor, Expires: now.Add(time.Hour)})
				row.Visitor = key
				return r.Insert(row)
			}); e != nil {
				t.Fatal(e)
			}
		} else if e := s.OverrideIntent(context.Background(), scope, key, "high", "reviewed", now.Add(time.Hour)); e != nil {
			t.Fatal(e)
		}
	}
}
func TestManualOverrideFilteredCursorStableAcrossSameClockAndLaterReplacement(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, later := range []bool{false, true} {
			t.Run(fmt.Sprintf("legacy=%v/later=%v", legacy, later), func(t *testing.T) {
				s, scope := fixture(t)
				ctx := context.Background()
				now := s.Now()
				seedOverrideVisitors(t, s, scope, 3, legacy)
				f := Filter{AsOf: now, Limit: 1, Intent: "high"}
				first, e := s.Visitors(ctx, scope, f)
				if e != nil {
					t.Fatal(e)
				}
				if first.Identified != 3 || first.Cursor == "" {
					t.Fatalf("bad first page %+v", first)
				}
				if later {
					s.Now = func() time.Time { return now.Add(time.Minute) }
				}
				if e = s.OverrideIntent(ctx, scope, "u:1", "low", "new correction", now.Add(time.Hour)); e != nil {
					t.Fatal(e)
				}
				// Read current first, then replay the older cursor to catch evaluation rewind.
				current, e := s.Visitors(ctx, scope, Filter{})
				if e != nil {
					t.Fatal(e)
				}
				for _, v := range current.Items.([]Visitor) {
					if v.Key == "u:1" && v.Intent.Level != "low" {
						t.Fatal(v.Intent)
					}
				}
				f.Cursor = first.Cursor
				second, e := s.Visitors(ctx, scope, f)
				if e != nil {
					t.Fatal(e)
				}
				if second.Identified != 3 || second.Items.([]Visitor)[0].Key != "u:2" {
					t.Fatalf("count or page membership shifted: %+v", second)
				}
				f.Cursor = first.FirstCursor
				replay, err := s.Visitors(ctx, scope, f)
				if err != nil {
					t.Fatal(err)
				}
				if replay.Identified != 3 || replay.Items.([]Visitor)[0].Key != "u:1" || replay.Items.([]Visitor)[0].Intent.Level != "high" || replay.FirstCursor != first.FirstCursor {
					t.Fatalf("return-to-first-page changed snapshot: %+v", replay)
				}
				f.Cursor = second.Cursor
				third, e := s.Visitors(ctx, scope, f)
				if e != nil {
					t.Fatal(e)
				}
				if third.Identified != 3 || third.Items.([]Visitor)[0].Key != "u:3" || third.Cursor != "" {
					t.Fatalf("third page shifted: %+v", third)
				}
				if e = s.transaction(ctx, func(r Repository) error {
					state, e := decode[intentEvaluation](r, "intent_evaluation", privateKey("owner-a", "u:1"))
					if e == nil && state.Level != "low" {
						t.Fatalf("old cursor rewound current evaluation: %+v", state)
					}
					return e
				}); e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}
func TestManualOverrideLegacyUnknownHistoryIsScopedAndCurrentRemainsUsable(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	now := s.Now()
	seedOverrideVisitors(t, s, scope, 1, true)
	if got := ruleVisitor(t, s, scope, Filter{}).Intent; got.Level != "high" {
		t.Fatal(got)
	}
	if e := s.transaction(ctx, func(r Repository) error {
		_, _, e := loadIntentOverrides(r, scope, now.Add(-time.Second), []string{"u:1"}, nil)
		ruleErrorStatus(t, e, 409)
		rows, _, e := loadIntentOverrides(r, scope, now.Add(-time.Second), []string{"u:unrelated"}, nil)
		if e != nil || len(rows) != 0 {
			t.Fatalf("unrelated visitor blocked: %v %+v", e, rows)
		}
		other := scope
		other.Owners = []string{"owner-b"}
		_, _, e = loadIntentOverrides(r, other, now.Add(-time.Second), []string{"u:1"}, nil)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	s.Now = func() time.Time { return now.Add(time.Minute) }
	if e := s.OverrideIntent(ctx, scope, "u:1", "medium", "replacement after legacy", now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	if got := ruleVisitor(t, s, scope, Filter{AsOf: now}).Intent; got.Level != "high" || got.Override.Reason != "legacy complete reason" {
		t.Fatal(got)
	}
	if e := s.transaction(ctx, func(r Repository) error {
		_, _, e := loadIntentOverrides(r, scope, now.Add(-time.Second), []string{"u:1"}, nil)
		ruleErrorStatus(t, e, 409)
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
func TestManualOverrideConcurrentSameTimeRevisionsAndClockRollback(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	now := s.Now()
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-0000001", now.Add(-time.Minute))
	const n = 16
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			actor := scope
			actor.Actor = int64(i + 1)
			errs <- s.OverrideIntent(ctx, actor, "u:1", "high", fmt.Sprintf("decision-%d", i), now.Add(time.Hour))
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if e := s.transaction(ctx, func(r Repository) error {
		rows, e := r.List("intent_override_history")
		if e != nil {
			return e
		}
		if len(rows) != n {
			t.Fatalf("lost concurrent decisions=%d", len(rows))
		}
		seen := map[int64]bool{}
		for _, row := range rows {
			d, e := readOverrideDecision(row)
			if e != nil {
				return e
			}
			if seen[d.Revision] {
				t.Fatal("duplicate revision")
			}
			seen[d.Revision] = true
		}
		chosen, revision, e := loadIntentOverrides(r, scope, now, []string{"u:1"}, nil)
		if e != nil {
			return e
		}
		d, e := readOverrideDecision(chosen[privateKey("owner-a", "u:1")])
		if e != nil {
			return e
		}
		if revision != n || d.Revision != n {
			t.Fatalf("tie not deterministic: %+v %d", d, revision)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	s.Now = func() time.Time { return now.Add(-time.Second) }
	ruleErrorStatus(t, s.OverrideIntent(ctx, scope, "u:1", "low", "clock moved backwards", now.Add(time.Hour)), 409)
}
func TestManualOverrideExactCreatedTimeSurvivesSQLMicrosecondColumns(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	base := s.Now()
	seedOverrideVisitors(t, s, scope, 1, false)
	s.Now = func() time.Time { return base.Add(500 * time.Nanosecond) }
	if e := s.OverrideIntent(ctx, scope, "u:1", "medium", "sub-microsecond decision", base.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	if e := s.transaction(ctx, func(r Repository) error {
		for _, kind := range []string{"intent_override", "intent_override_history"} {
			rows, e := r.List(kind)
			if e != nil {
				return e
			}
			for _, row := range rows {
				row.At = row.At.Truncate(time.Microsecond)
				if e = r.Put(row); e != nil {
					return e
				}
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if got := ruleVisitor(t, s, scope, Filter{AsOf: base.Add(400 * time.Nanosecond)}).Intent; got.Level != "high" {
		t.Fatal("future decision leaked through truncated SQL time", got)
	}
	if got := ruleVisitor(t, s, scope, Filter{}).Intent; got.Level != "medium" || !got.Override.Created.Equal(base.Add(500*time.Nanosecond)) {
		t.Fatal(got)
	}
}
func TestManualOverrideAuditFailureRollsBackHistoryPointerAndClock(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	now := s.Now()
	seedOverrideVisitors(t, s, scope, 1, true)
	original := s.Store
	s.Store = failRuleAuditStore{original}
	if e := s.OverrideIntent(ctx, scope, "u:1", "medium", "must rollback", now.Add(time.Hour)); e == nil {
		t.Fatal("unaudited decision committed")
	}
	s.Store = original
	if e := s.transaction(ctx, func(r Repository) error {
		for _, kind := range []string{"intent_override_history", "intent_override_clock", "intent_override_boundary"} {
			rows, e := r.List(kind)
			if e != nil {
				return e
			}
			if len(rows) != 0 {
				t.Fatalf("partial write survived: %s", kind)
			}
		}
		d, e := decode[IntentOverride](r, "intent_override", privateKey("owner-a", "u:1"))
		if e == nil && d.Reason != "legacy complete reason" {
			t.Fatal("legacy pointer overwritten")
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
}
func TestManualOverrideHistoryDeletionAndOwnerIsolation(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	now := s.Now()
	seedOverrideVisitors(t, s, scope, 1, true)
	if e := s.OverrideIntent(ctx, scope, "u:1", "medium", "secret complete history", now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	other := scope
	other.Owners = []string{"owner-b"}
	ruleFixturePV(t, s, "owner-b", "u:1", "session-other", "view-other001", now.Add(-time.Minute))
	if e := s.OverrideIntent(ctx, other, "u:1", "low", "other owner decision", now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	ruleErrorStatus(t, s.DeleteVisitor(ctx, scope, "u:1", "request"), 403)
	scope.Owners = append(scope.Owners, "owner-b")
	if e := s.DeleteVisitor(ctx, scope, "u:1", "request"); e != nil {
		t.Fatal(e)
	}
	if e := s.transaction(ctx, func(r Repository) error {
		for _, kind := range []string{"intent_override", "intent_override_history", "intent_override_boundary"} {
			rows, e := r.List(kind)
			if e != nil {
				return e
			}
			if len(rows) != 0 {
				t.Fatalf("personal history survived deletion: %s", kind)
			}
		}
		clocks, e := r.List("intent_override_clock")
		if e != nil {
			return e
		}
		if len(clocks) != 2 {
			t.Fatal("non-personal revision clock removed")
		}
		for _, row := range clocks {
			if row.Visitor != "" || strings.Contains(string(row.Payload), "decision") {
				t.Fatal("clock contains personal data")
			}
		}
		audits, e := r.List("audit")
		if e != nil {
			return e
		}
		for _, row := range audits {
			if strings.Contains(string(row.Payload), "secret complete history") || strings.Contains(string(row.Payload), "legacy complete reason") {
				t.Fatal("erased reason remained in audit")
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
func TestManualOverrideRetentionDeletesReasonsAndMarksUnavailableHistory(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	now := s.Now()
	old := now.AddDate(0, 0, -200)
	s.Now = func() time.Time { return old }
	seedOverrideVisitors(t, s, scope, 1, false)
	s.Now = func() time.Time { return now }
	ruleFixturePV(t, s, "owner-a", "u:1", "session-new", "view-new00001", now.Add(-time.Minute))
	if e := s.OverrideIntent(ctx, scope, "u:1", "medium", "current retained decision", now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	p := DefaultRetentionPolicy()
	p.Enabled = true
	plan, e := s.PlanRetention(ctx, scope, p, now.AddDate(0, 0, -90), now.AddDate(0, 0, -180))
	if e != nil {
		t.Fatal(e)
	}
	if plan.Counts["intent_override_history"] != 1 {
		t.Fatal(plan.Counts)
	}
	if _, e = s.PurgeExpired(ctx, scope, p, plan); e != nil {
		t.Fatal(e)
	}
	if e = s.transaction(ctx, func(r Repository) error {
		if _, e := r.Get("intent_override_history", overrideDecisionID("owner-a", "u:1", 1)); !errors.Is(e, ErrNotFound) {
			t.Fatal("expired history remained", e)
		}
		_, _, e := loadIntentOverrides(r, scope, old, []string{"u:1"}, nil)
		ruleErrorStatus(t, e, 409)
		rows, _, e := loadIntentOverrides(r, scope, now, []string{"u:1"}, nil)
		if e != nil {
			return e
		}
		if len(rows) != 1 {
			t.Fatal("current override blocked")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func TestManualOverrideKindsFitExistingSQLSchema(t *testing.T) {
	for _, kind := range []string{"intent_override", "intent_override_history", "intent_override_boundary", "intent_override_clock"} {
		if len(kind) > 24 {
			t.Fatalf("kind %q exceeds tracking_records.kind VARCHAR(24)", kind)
		}
	}
}
func TestManualOverrideFirstCursorReturnedForEmptyAndSinglePage(t *testing.T) {
	s, scope := fixture(t)
	for _, populated := range []bool{false, true} {
		if populated {
			seedOverrideVisitors(t, s, scope, 1, false)
		}
		first, e := s.Visitors(context.Background(), scope, Filter{})
		if e != nil {
			t.Fatal(e)
		}
		if first.FirstCursor == "" {
			t.Fatal("first-page replay token absent")
		}
		replay, e := s.Visitors(context.Background(), scope, Filter{AsOf: first.AsOf, Cursor: first.FirstCursor})
		if e != nil || replay.Identified != first.Identified {
			t.Fatalf("first-page replay failed: %+v %v", replay, e)
		}
	}
}

func TestManualOverrideIncompleteCurrentHistoryFailsClosed(t *testing.T) {
	for _, kind := range []string{"intent_override_clock", "intent_override_history"} {
		t.Run(kind, func(t *testing.T) {
			s, scope := fixture(t)
			seedOverrideVisitors(t, s, scope, 1, false)
			if e := s.transaction(context.Background(), func(r Repository) error {
				id := privateKey("owner-a", "current")
				if kind == "intent_override_history" {
					id = overrideDecisionID("owner-a", "u:1", 1)
				}
				return r.Remove(kind, id)
			}); e != nil {
				t.Fatal(e)
			}
			if _, e := s.Visitors(context.Background(), scope, Filter{}); !errors.Is(e, ErrUnavailable) {
				t.Fatalf("missing %s silently dropped manual decision: %v", kind, e)
			}
		})
	}
}
func TestManualOverrideAuditDigestUsesTypedCanonicalJSON(t *testing.T) {
	d := intentOverrideDecision{IntentOverride: IntentOverride{Created: time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC), Level: "high", Reason: "full decision", Actor: 7, Expires: time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)}, Revision: 1}
	var reordered map[string]any
	if e := json.Unmarshal(marshal(d), &reordered); e != nil {
		t.Fatal(e)
	}
	b, e := json.MarshalIndent(reordered, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	row := record("intent_override_history", overrideDecisionID("owner-a", "u:1", 1), "owner-a", d.Created, d)
	row.Payload = b
	parsed, e := readOverrideDecision(row)
	if e != nil || overrideDecisionDigest(parsed) != overrideDecisionDigest(d) {
		t.Fatalf("SQL JSON normalization changed digest: %v", e)
	}
}
