package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func ruleFixturePV(t *testing.T, s *Service, owner, key, session, id string, at time.Time) {
	t.Helper()
	err := s.transaction(context.Background(), func(r Repository) error {
		event := Event{Schema: 1, ID: id, Type: "page_view", View: id, Session: session, Route: "pages/home/home", Occurred: at, Received: at, AppState: "foreground", PageState: "visible"}
		row := record("event", id, owner, at, event)
		row.Visitor = key
		row.Session = session
		return r.Insert(row)
	})
	if err != nil {
		t.Fatal(err)
	}
}
func ruleFixtureBusiness(t *testing.T, s *Service, owner, key, session, id, action string, at time.Time) {
	t.Helper()
	err := s.transaction(context.Background(), func(r Repository) error {
		row := record("business", id, owner, at, map[string]string{"action": action, "session": session})
		row.Visitor = key
		row.Session = session
		return r.Insert(row)
	})
	if err != nil {
		t.Fatal(err)
	}
}
func ruleVisitor(t *testing.T, s *Service, scope Scope, f Filter) Visitor {
	t.Helper()
	list, e := s.Visitors(context.Background(), scope, f)
	if e != nil {
		t.Fatal(e)
	}
	if len(list.Items.([]Visitor)) != 1 {
		t.Fatalf("visitors: %+v", list)
	}
	return list.Items.([]Visitor)[0]
}
func ruleErrorStatus(t *testing.T, e error, status int) {
	t.Helper()
	var te *Error
	if !errors.As(e, &te) || te.Status != status {
		t.Fatalf("wanted %d, got %v", status, e)
	}
}
func ruleUpdate(p RuleParameters) RuleUpdate {
	return RuleUpdate{RuleParameters: p, Expected: RuleVersion, Reason: "调整经确认的首版阈值"}
}
func TestIntentRulesVersionHistoryWindowAndAutomaticRecompute(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	ruleFixturePV(t, s, "owner-a", "u:1", "session-old", "view-old0001", now.AddDate(0, 0, -10))
	ruleFixturePV(t, s, "owner-a", "u:1", "session-new", "view-new0001", now.Add(-time.Hour))
	ruleFixtureBusiness(t, s, "owner-a", "u:1", "session-new", "favorite0001", "favorite", now.Add(-time.Minute))
	got := ruleVisitor(t, s, scope, Filter{Start: now.Add(-2 * time.Hour)})
	if got.Views != 1 || got.Intent.Level != "medium" || got.Intent.Metrics.Sessions != 2 || got.Intent.Thresholds.WindowDays != 30 || len(got.Intent.Evidence) != 3 {
		t.Fatalf("separate window: %+v", got)
	}
	p := defaultRule("owner-a").RuleParameters
	p.WindowDays = 3
	update, e := s.UpdateRules(context.Background(), scope, ruleUpdate(p))
	if e != nil {
		t.Fatal(e)
	}
	if update.Current.Version != "followup-v2" || len(update.History) != 2 || update.Recomputed.Changed != 1 || update.Recomputed.Evaluated != 1 {
		t.Fatalf("update/recompute: %+v", update)
	}
	got = ruleVisitor(t, s, scope, Filter{})
	if got.Intent.Level != "low" || got.Intent.Rule != "followup-v2" || got.Intent.Metrics.Sessions != 1 {
		t.Fatal(got.Intent)
	}
	old := ruleVisitor(t, s, scope, Filter{AsOf: now.Add(-time.Second)})
	if old.Intent.Rule != RuleVersion || old.Intent.Level != "medium" {
		t.Fatal(old.Intent)
	}
	// Historical replays never overwrite the newer evaluation state.
	_ = s.transaction(context.Background(), func(r Repository) error {
		state, e := decode[intentEvaluation](r, "intent_evaluation", privateKey("owner-a", "u:1"))
		if e != nil || state.Rule != "followup-v2" {
			t.Fatalf("historical query regressed current state: %+v %v", state, e)
		}
		return nil
	})
	_, e = s.UpdateRules(context.Background(), scope, ruleUpdate(p))
	ruleErrorStatus(t, e, 409)
	history, e := s.Rules(context.Background(), scope)
	if e != nil || len(history.History) != 2 || history.History[1].WindowDays != 30 {
		t.Fatalf("immutable history: %+v %v", history, e)
	}
}
func TestIntentRulesPermissionsAndBoundaries(t *testing.T) {
	s, scope := fixture(t)
	p := defaultRule("owner-a").RuleParameters
	readonly := scope
	readonly.Rules = false
	if _, e := s.Rules(context.Background(), readonly); e != nil {
		t.Fatal(e)
	}
	_, e := s.UpdateRules(context.Background(), readonly, ruleUpdate(p))
	ruleErrorStatus(t, e, 403)
	for _, bad := range []Scope{{Tenant: "other", Actor: 7, Owners: scope.Owners, View: true, Rules: true}, {Tenant: scope.Tenant, Actor: 0, Owners: scope.Owners, View: true, Rules: true}, {Tenant: scope.Tenant, Actor: 7, Owners: scope.Owners, Rules: true}} {
		_, e = s.UpdateRules(context.Background(), bad, ruleUpdate(p))
		ruleErrorStatus(t, e, 403)
	}
	multi := scope
	multi.Owners = []string{"owner-a", "owner-b"}
	_, e = s.UpdateRules(context.Background(), multi, ruleUpdate(p))
	ruleErrorStatus(t, e, 409)
	for _, bad := range []RuleParameters{{0, 2, 1, 1, 10, 1}, {91, 2, 1, 1, 10, 1}, {30, 1, 1, 1, 10, 1}, {30, 2, 0, 1, 10, 1}, {30, 2, 1, 0, 10, 1}, {30, 2, 1, 1, 11, 1}, {30, 2, 1, 1, 10, 6}, {1, 100, 1, 1, 1, 1}} {
		_, e = s.UpdateRules(context.Background(), scope, ruleUpdate(bad))
		ruleErrorStatus(t, e, 400)
	}
	history, e := s.Rules(context.Background(), scope)
	if e != nil || len(history.History) != 1 {
		t.Fatal(history, e)
	}
}
func TestIntentRulesDailyCapsClicksAndBusinessEvidence(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	p := defaultRule("owner-a").RuleParameters
	p.HighBusinessResults = 2
	if _, e := s.UpdateRules(context.Background(), scope, ruleUpdate(p)); e != nil {
		t.Fatal(e)
	}
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-one0001", now.Add(-time.Hour))
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-one0002", now.Add(-30*time.Minute))
	for i, id := range []string{"business0001", "business0002", "business0003"} {
		ruleFixtureBusiness(t, s, "owner-a", "u:1", "session-one", id, "consultation", now.Add(time.Duration(-10+i)*time.Minute))
	}
	// Non-server action labels and business events without valid view evidence cannot contribute.
	ruleFixtureBusiness(t, s, "owner-a", "u:1", "session-one", "business0004", "consultation_click", now.Add(-time.Minute))
	ruleFixtureBusiness(t, s, "owner-a", "u:1", "unobserved", "business0005", "appointment", now.Add(-time.Minute))
	got := ruleVisitor(t, s, scope, Filter{})
	if got.Intent.Level != "low" || got.Intent.Metrics.Sessions != 1 || got.Intent.Metrics.BusinessResults != 1 {
		t.Fatal(got.Intent)
	}
	ruleFixtureBusiness(t, s, "owner-a", "u:1", "session-one", "business0006", "appointment", now.Add(-time.Minute))
	got = ruleVisitor(t, s, scope, Filter{})
	if got.Intent.Level != "high" || got.Intent.Metrics.BusinessResults != 2 {
		t.Fatal(got.Intent)
	}
	// Same-day repeated confirmed requests do not create an unbounded score.
	if len(got.Intent.Evidence) != 3 || got.Intent.Evidence[1].Kind != "business" {
		t.Fatal(got.Intent.Evidence)
	}
}
func TestIntentRulesMixedOwnerDoesNotBreakListOrLeakConfig(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	p := defaultRule("owner-a").RuleParameters
	p.WindowDays = 7
	if _, e := s.UpdateRules(context.Background(), scope, ruleUpdate(p)); e != nil {
		t.Fatal(e)
	}
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-one0001", now.Add(-time.Hour))
	ruleFixturePV(t, s, "owner-b", "u:1", "session-two", "view-two0001", now.Add(-time.Hour))
	multi := scope
	multi.Owners = []string{"owner-a", "owner-b"}
	got := ruleVisitor(t, s, multi, Filter{})
	if !got.Intent.ScopeRequired || got.Intent.Level != "unknown" || got.Views != 2 {
		t.Fatal(got)
	}
	got = ruleVisitor(t, s, multi, Filter{Owner: "owner-a"})
	if got.Intent.ScopeRequired || got.Intent.Rule != "followup-v2" || got.Views != 1 {
		t.Fatal(got)
	}
	other := scope
	other.Owners = []string{"owner-b"}
	rules, e := s.Rules(context.Background(), other)
	if e != nil || len(rules.History) != 1 || rules.Current.WindowDays != 30 {
		t.Fatal(rules, e)
	}
}
func TestIntentRecomputeIdempotencyAndChangedEvidence(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-one0001", now.Add(-time.Hour))
	ruleFixtureBusiness(t, s, "owner-a", "u:1", "session-one", "business0001", "appointment", now.Add(-time.Minute))
	if got := ruleVisitor(t, s, scope, Filter{}); got.Intent.Level != "high" {
		t.Fatal(got.Intent)
	}
	input := RecomputeInput{RequestID: "recompute0001", Reason: "核对源数据变化", Version: RuleVersion}
	first, e := s.RecomputeIntents(context.Background(), scope, input)
	if e != nil || first.Evaluated != 1 || first.Changed != 0 {
		t.Fatal(first, e)
	}
	// Removing source evidence changes the calculated result and creates one change audit.
	if e = s.transaction(context.Background(), func(r Repository) error { return r.Remove("business", "business0001") }); e != nil {
		t.Fatal(e)
	}
	input.RequestID = "recompute0002"
	result, e := s.RecomputeIntents(context.Background(), scope, input)
	if e != nil || result.Changed != 1 {
		t.Fatal(result, e)
	}
	replay, e := s.RecomputeIntents(context.Background(), scope, input)
	if e != nil || replay != result {
		t.Fatal(replay, e)
	}
	input.Reason = "different"
	_, e = s.RecomputeIntents(context.Background(), scope, input)
	ruleErrorStatus(t, e, 409)
	input.RequestID = "recompute0003"
	input.Version = "followup-v99"
	_, e = s.RecomputeIntents(context.Background(), scope, input)
	ruleErrorStatus(t, e, 409)
	// Source quality reclassification also drops the label rather than serving a cached result.
	_ = s.transaction(context.Background(), func(r Repository) error {
		row, e := r.Get("event", "view-one0001")
		if e != nil {
			return e
		}
		var event Event
		_ = json.Unmarshal(row.Payload, &event)
		event.Flags = []string{"abnormal"}
		row.Payload = marshal(event)
		return r.Put(row)
	})
	input.RequestID = "recompute0004"
	input.Version = RuleVersion
	result, e = s.RecomputeIntents(context.Background(), scope, input)
	if e != nil || result.Changed != 1 {
		t.Fatal(result, e)
	}
	_ = s.transaction(context.Background(), func(r Repository) error {
		state, e := decode[intentEvaluation](r, "intent_evaluation", privateKey("owner-a", "u:1"))
		if e != nil || state.Level != "unknown" {
			t.Fatal(state, e)
		}
		return nil
	})
}
func TestIntentOverrideExpiryAndDeletionClearEvaluation(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-one0001", now.Add(-time.Hour))
	if e := s.OverrideIntent(context.Background(), scope, "u:1", "high", "人工核实", now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	got := ruleVisitor(t, s, scope, Filter{})
	if got.Intent.OverrideState != "active" || got.Intent.OriginalRule.Level != "low" || got.Intent.Override.Actor != 7 || got.Intent.Override.Created != now {
		t.Fatal(got.Intent)
	}
	s.Now = func() time.Time { return now.Add(2 * time.Hour) }
	got = ruleVisitor(t, s, scope, Filter{})
	if got.Intent.Level != "low" || got.Intent.OverrideState != "expired" {
		t.Fatal(got.Intent)
	}
	if e := s.DeleteVisitor(context.Background(), scope, "u:1", "删除申请"); e != nil {
		t.Fatal(e)
	}
	_ = s.transaction(context.Background(), func(r Repository) error {
		_, e := r.Get("intent_evaluation", privateKey("owner-a", "u:1"))
		if !errors.Is(e, ErrNotFound) {
			t.Fatal("evaluation survived erasure", e)
		}
		return nil
	})
	result, e := s.RecomputeIntents(context.Background(), scope, RecomputeInput{RequestID: "after-delete01", Reason: "验证删除后重放", Version: RuleVersion})
	if e != nil || result.Evaluated != 0 {
		t.Fatal(result, e)
	}
}
func TestConfirmConsultationAuthenticatesAndDeduplicates(t *testing.T) {
	s, scope := fixture(t)
	en := enter(t, s, 1)
	accept(t, s, en, ev(s, en, "event-consult01", "page_view", s.Now()))
	if e := s.ConfirmConsultation(context.Background(), en.Token, 2, 12); e == nil {
		t.Fatal("wrong account accepted")
	}
	if e := s.ConfirmConsultation(context.Background(), en.Token, 1, 12); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfirmConsultation(context.Background(), en.Token, 1, 12); e != nil {
		t.Fatal(e)
	}
	got := ruleVisitor(t, s, scope, Filter{})
	if got.Intent.Level != "high" || got.Intent.Metrics.BusinessResults != 1 {
		t.Fatal(got.Intent)
	}
	if e := s.DeleteVisitor(context.Background(), scope, "u:1", "删除申请"); e != nil {
		t.Fatal(e)
	}
	if e := s.ConfirmConsultation(context.Background(), en.Token, 1, 12); e == nil {
		t.Fatal("deleted event resurrected")
	}
}

func TestIntentIdentityUnlinkRecomputesPreviousIdentity(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	ruleFixturePV(t, s, "owner-a", "a:visitor0001", "session-one", "view-one0001", now.Add(-time.Hour))
	ruleFixtureBusiness(t, s, "owner-a", "a:visitor0001", "session-one", "business0001", "appointment", now.Add(-time.Minute))
	_ = s.transaction(context.Background(), func(r Repository) error {
		return r.Insert(record("identity", "session-one", "owner-a", now.Add(-time.Second), map[string]string{"original_visitor_key": "a:visitor0001", "visitor_key": "u:1"}))
	})
	if got := ruleVisitor(t, s, scope, Filter{}); got.Key != "u:1" || got.Intent.Level != "high" {
		t.Fatal(got)
	}
	_ = s.transaction(context.Background(), func(r Repository) error { return r.Remove("identity", "session-one") })
	result, e := s.RecomputeIntents(context.Background(), scope, RecomputeInput{RequestID: "unlink-recompute01", Reason: "核对身份关联解除", Version: RuleVersion})
	if e != nil || result.Evaluated != 2 || result.Changed != 2 {
		t.Fatal(result, e)
	}
	if got := ruleVisitor(t, s, scope, Filter{}); got.Key != "a:visitor0001" || got.Intent.Level != "unknown" {
		t.Fatal(got)
	}
	_ = s.transaction(context.Background(), func(r Repository) error {
		old, e := decode[intentEvaluation](r, "intent_evaluation", privateKey("owner-a", "u:1"))
		if e != nil || old.Level != "unknown" {
			t.Fatal(old, e)
		}
		return nil
	})
}

type failRuleAuditStore struct{ Store }
type failRuleAuditRepo struct{ Repository }

func (s failRuleAuditStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.Store.Transaction(ctx, tenant, func(r Repository) error { return fn(failRuleAuditRepo{r}) })
}
func (r failRuleAuditRepo) Insert(row Record) error {
	if row.Kind == "audit" {
		return errors.New("audit storage unavailable")
	}
	return r.Repository.Insert(row)
}
func TestIntentRulesAuditFailureRollsBackVersionAndProjection(t *testing.T) {
	s, scope := fixture(t)
	source := s.Store
	s.Store = failRuleAuditStore{source}
	p := defaultRule("owner-a").RuleParameters
	p.WindowDays = 7
	if _, e := s.UpdateRules(context.Background(), scope, ruleUpdate(p)); e == nil {
		t.Fatal("un-audited update committed")
	}
	s.Store = source
	history, e := s.Rules(context.Background(), scope)
	if e != nil || len(history.History) != 1 || history.Current.Version != RuleVersion {
		t.Fatal(history, e)
	}
	_ = s.transaction(context.Background(), func(r Repository) error {
		if _, e := r.Get("intent_rule_current", privateKey("owner-a", "current")); !errors.Is(e, ErrNotFound) {
			t.Fatal("current pointer survived rollback", e)
		}
		return nil
	})
}
func TestIntentRuleDailySessionCapAndWindowBoundary(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	p := defaultRule("owner-a").RuleParameters
	p.WindowDays = 3
	p.DailySessionCap = 1
	if _, e := s.UpdateRules(context.Background(), scope, ruleUpdate(p)); e != nil {
		t.Fatal(e)
	}
	ruleFixturePV(t, s, "owner-a", "u:1", "session-old", "view-old0001", now.AddDate(0, 0, -3).Add(-time.Nanosecond))
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-one0001", now.Add(-time.Hour))
	ruleFixturePV(t, s, "owner-a", "u:1", "session-two", "view-two0001", now.Add(-time.Minute))
	ruleFixtureBusiness(t, s, "owner-a", "u:1", "session-two", "favorite0001", "favorite", now.Add(-time.Minute))
	if got := ruleVisitor(t, s, scope, Filter{}); got.Intent.Metrics.Sessions != 1 || got.Intent.Level != "low" {
		t.Fatal(got.Intent)
	}
	ruleFixturePV(t, s, "owner-a", "u:1", "session-edge", "view-edge001", now.AddDate(0, 0, -3))
	if got := ruleVisitor(t, s, scope, Filter{}); got.Intent.Metrics.Sessions != 2 || got.Intent.Level != "medium" {
		t.Fatal(got.Intent)
	}
}

func TestIntentRuleHistoricalSubMicrosecondBoundary(t *testing.T) {
	s, scope := fixture(t)
	base := s.Now()
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-one0001", base.Add(-time.Hour))
	s.Now = func() time.Time { return base.Add(500 * time.Nanosecond) }
	p := defaultRule("owner-a").RuleParameters
	p.WindowDays = 7
	if _, e := s.UpdateRules(context.Background(), scope, ruleUpdate(p)); e != nil {
		t.Fatal(e)
	}
	if got := ruleVisitor(t, s, scope, Filter{AsOf: base.Add(400 * time.Nanosecond)}); got.Intent.Rule != RuleVersion {
		t.Fatal("future rule leaked into previous snapshot", got.Intent)
	}
}
func TestIntentRuleUnchangedReadAdvancesWatermark(t *testing.T) {
	s, scope := fixture(t)
	base := s.Now()
	ruleFixturePV(t, s, "owner-a", "u:1", "session-one", "view-one0001", base.Add(-time.Hour))
	ruleVisitor(t, s, scope, Filter{})
	s.Now = func() time.Time { return base.Add(time.Hour) }
	if e := s.OverrideIntent(context.Background(), scope, "u:1", "high", "temporary", base.Add(2*time.Hour)); e != nil {
		t.Fatal(e)
	}
	s.Now = func() time.Time { return base.Add(3 * time.Hour) }
	if got := ruleVisitor(t, s, scope, Filter{}); got.Intent.Level != "low" {
		t.Fatal(got.Intent)
	}
	if got := ruleVisitor(t, s, scope, Filter{AsOf: base.Add(90 * time.Minute)}); got.Intent.Level != "high" {
		t.Fatal(got.Intent)
	}
	_ = s.transaction(context.Background(), func(r Repository) error {
		state, e := decode[intentEvaluation](r, "intent_evaluation", privateKey("owner-a", "u:1"))
		if e != nil || state.Level != "low" || !state.AsOf.Equal(base.Add(3*time.Hour)) {
			t.Fatal("history regressed current evaluation", state, e)
		}
		return nil
	})
}
func TestIntentLiveBusinessRevocationRecomputesAndFailsClosed(t *testing.T) {
	s, scope := fixture(t)
	en := enter(t, s, 1)
	accept(t, s, en, ev(s, en, "event-consult01", "page_view", s.Now()))
	if e := s.ConfirmConsultation(context.Background(), en.Token, 1, 12); e != nil {
		t.Fatal(e)
	}
	exists := true
	broken := false
	calls := 0
	s.VerifyBusiness = func(ctx context.Context, p BusinessProof) (bool, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("source verification has no deadline")
		}
		if p.Account != 1 || p.TargetType != "lead" || p.TargetID != 12 || p.Action != "consultation" {
			t.Fatal(p)
		}
		if broken {
			return false, errors.New("source unavailable")
		}
		return exists, nil
	}
	if got := ruleVisitor(t, s, scope, Filter{}); got.Intent.Level != "high" {
		t.Fatal(got.Intent)
	}
	exists = false
	if got := ruleVisitor(t, s, scope, Filter{}); got.Intent.Level != "low" || got.Intent.Metrics.BusinessResults != 0 {
		t.Fatal("invalid/deleted source retained label", got.Intent)
	}
	_ = s.transaction(context.Background(), func(r Repository) error {
		state, e := decode[intentEvaluation](r, "intent_evaluation", privateKey("owner-a", "u:1"))
		if e != nil || state.Level != "low" {
			t.Fatal(state, e)
		}
		return nil
	})
	broken = true
	if _, e := s.Visitors(context.Background(), scope, Filter{}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("source failure did not close query", e)
	}
	if calls != 3 {
		t.Fatal("unexpected source verification", calls)
	}
}
func TestIntentBusinessSourceProofContainsOnlyTrustedIdentifiers(t *testing.T) {
	row := record("business", "proof0001", "owner", time.Now(), map[string]string{"action": "favorite", "target_type": "template", "target_id": "12", "phone": "should-never-be-projected"})
	row.Visitor = "u:7"
	p, ok := sourceBusinessProof(row)
	if !ok || p.Account != 7 || p.TargetID != 12 {
		t.Fatal(p, ok)
	}
	row.Visitor = "a:anonymous01"
	if _, ok := sourceBusinessProof(row); ok {
		t.Fatal("anonymous proof promoted to account")
	}
	row.Visitor = "u:7"
	row.Payload = marshal(map[string]string{"action": "appointment", "business_record_id": "lead:12"})
	if _, ok := sourceBusinessProof(row); ok {
		t.Fatal("unconnected appointment source accepted")
	}
}

func TestIntentRecomputeIncludesActiveOverrideWithoutWindowFacts(t *testing.T) {
	s, scope := fixture(t)
	now := s.Now()
	ruleFixturePV(t, s, "owner-a", "u:1", "session-old", "view-old0001", now.AddDate(0, 0, -40))
	if e := s.OverrideIntent(context.Background(), scope, "u:1", "high", "人工核实", now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	input := RecomputeInput{RequestID: "override-window01", Reason: "核对活跃人工修正", Version: RuleVersion}
	result, e := s.RecomputeIntents(context.Background(), scope, input)
	if e != nil || result.Evaluated != 1 || result.Changed != 1 {
		t.Fatal(result, e)
	}
	_ = s.transaction(context.Background(), func(r Repository) error {
		state, e := decode[intentEvaluation](r, "intent_evaluation", privateKey("owner-a", "u:1"))
		if e != nil || state.Level != "high" {
			t.Fatal(state, e)
		}
		return nil
	})
	s.Now = func() time.Time { return now.Add(2 * time.Hour) }
	input.RequestID = "override-window02"
	result, e = s.RecomputeIntents(context.Background(), scope, input)
	if e != nil || result.Evaluated != 1 || result.Changed != 1 {
		t.Fatal(result, e)
	}
}
