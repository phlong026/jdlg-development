package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// RuleParameters deliberately exposes only the reviewed first-release predicates.
// Client clicks, AI scores and arbitrary expressions cannot become business facts.
type RuleParameters struct {
	WindowDays          int `json:"window_days"`
	MediumSessions      int `json:"medium_sessions"`
	MediumFavorites     int `json:"medium_favorites"`
	HighBusinessResults int `json:"high_business_results"`
	DailySessionCap     int `json:"daily_session_cap"`
	DailyActionCap      int `json:"daily_action_cap"`
}
type RuleConfig struct {
	RuleParameters
	Version    string    `json:"rule_version"`
	Revision   int       `json:"revision"`
	Owner      string    `json:"owner_id"`
	Effective  time.Time `json:"effective_at"`
	Actor      int64     `json:"actor"`
	Reason     string    `json:"reason"`
	Timezone   string    `json:"timezone"`
	Metrics    []string  `json:"participating_metrics"`
	Exclusions []string  `json:"exclusions"`
}
type RuleUpdate struct {
	RuleParameters
	Expected string `json:"expected_rule_version"`
	Reason   string `json:"reason"`
}
type RulesSnapshot struct {
	Current    RuleConfig       `json:"current"`
	History    []RuleConfig     `json:"history"`
	Recomputed *RecomputeResult `json:"recomputed,omitempty"`
}
type RecomputeInput struct {
	RequestID string `json:"request_id"`
	Reason    string `json:"reason"`
	Version   string `json:"rule_version"`
}
type RecomputeResult struct {
	Version   string    `json:"rule_version"`
	Evaluated int       `json:"evaluated_count"`
	Changed   int       `json:"changed_count"`
	AsOf      time.Time `json:"as_of"`
}

// BusinessProof contains only server-bound source identifiers, never customer text.
type BusinessProof struct {
	Action     string
	Account    int64
	TargetType string
	TargetID   int64
}

type IntentMetrics struct {
	Sessions        int `json:"sessions"`
	Favorites       int `json:"favorites"`
	BusinessResults int `json:"business_results"`
}
type IntentEvidence struct {
	Kind    string    `json:"kind"`
	ID      string    `json:"record_id"`
	Session string    `json:"session_id"`
	View    string    `json:"page_view_id,omitempty"`
	Action  string    `json:"action,omitempty"`
	At      time.Time `json:"occurred_at"`
	Content string    `json:"content_id,omitempty"`
	Version int       `json:"content_version,omitempty"`
}

func defaultRule(owner string) RuleConfig {
	return RuleConfig{RuleParameters: RuleParameters{WindowDays: 30, MediumSessions: 2, MediumFavorites: 1, HighBusinessResults: 1, DailySessionCap: 10, DailyActionCap: 1}, Version: RuleVersion, Revision: 1, Owner: owner, Timezone: "Asia/Shanghai", Metrics: []string{"independent_sessions", "server_favorite", "server_consultation", "server_appointment", "server_quote"}, Exclusions: []string{"anonymous_identity", "preview", "self", "test", "abnormal", "duplicate", "client_click", "unverified_business", "daily_contribution_over_cap"}, Reason: "首版确定性规则"}
}
func (s *Service) ruleScope(scope Scope, write bool) error {
	if e := s.authorizeView(scope); e != nil {
		return e
	}
	if write && !scope.Rules {
		return fail(403, "无规则管理权限")
	}
	if len(scope.Owners) != 1 {
		return fail(409, "请选择单一归属范围查看或管理规则")
	}
	return nil
}
func (s *Service) validateRule(p RuleParameters) error {
	maxDays := 90
	if s.Config.RetentionDays > 0 && s.Config.RetentionDays < maxDays {
		maxDays = s.Config.RetentionDays
	}
	if p.WindowDays < 1 || p.WindowDays > maxDays || p.MediumSessions < 2 || p.MediumSessions > 100 || p.MediumFavorites < 1 || p.MediumFavorites > 30 || p.HighBusinessResults < 1 || p.HighBusinessResults > 30 || p.DailySessionCap < 1 || p.DailySessionCap > 10 || p.DailyActionCap < 1 || p.DailyActionCap > 5 {
		return fail(400, "规则参数超出首版允许范围")
	}
	if p.MediumSessions > p.WindowDays*p.DailySessionCap || p.MediumFavorites > p.WindowDays*p.DailyActionCap || p.HighBusinessResults > p.WindowDays*p.DailyActionCap*3 {
		return fail(400, "阈值超出窗口内可贡献上限")
	}
	return nil
}
func ruleHistory(r Repository, owner string, asOf time.Time) ([]RuleConfig, error) {
	rows, e := scopedRecords(r, "intent_rule", RecordQuery{Owners: []string{owner}, AtThrough: asOf})
	if e != nil {
		return nil, e
	}
	out := []RuleConfig{defaultRule(owner)}
	for _, row := range rows {
		var rule RuleConfig
		if json.Unmarshal(row.Payload, &rule) != nil || rule.Owner != owner || rule.Version == "" {
			return nil, ErrUnavailable
		}
		if rule.Effective.After(asOf) || row.At.After(asOf) {
			continue
		}
		out = append(out, rule)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision > out[j].Revision })
	return out, nil
}
func activeRule(r Repository, scope Scope, asOf time.Time) (RuleConfig, bool, error) {
	var selected RuleConfig
	for i, owner := range scope.Owners {
		current, e := decode[RuleConfig](r, "intent_rule_current", privateKey(owner, "current"))
		if e != nil && !errors.Is(e, ErrNotFound) {
			return selected, false, e
		}
		if errors.Is(e, ErrNotFound) || current.Effective.After(asOf) {
			h, err := ruleHistory(r, owner, asOf)
			if err != nil {
				return selected, false, err
			}
			current = h[0]
		}
		if current.Owner != owner || current.Version == "" {
			return selected, false, ErrUnavailable
		}
		if i == 0 {
			selected = current
		} else if current.Version != selected.Version || current.RuleParameters != selected.RuleParameters {
			return selected, true, nil
		}
	}
	if len(scope.Owners) == 0 {
		selected = defaultRule("")
	}
	return selected, false, nil
}
func (s *Service) Rules(ctx context.Context, scope Scope) (RulesSnapshot, error) {
	var out RulesSnapshot
	if e := s.ruleScope(scope, false); e != nil {
		return out, e
	}
	e := s.queryTransaction(ctx, func(r Repository) error {
		h, e := ruleHistory(r, scope.Owners[0], s.Now())
		if e != nil {
			return e
		}
		out = RulesSnapshot{Current: h[0], History: h}
		return nil
	})
	return out, e
}
func (s *Service) UpdateRules(ctx context.Context, scope Scope, input RuleUpdate) (RulesSnapshot, error) {
	var out RulesSnapshot
	if e := s.ruleScope(scope, true); e != nil {
		return out, e
	}
	if !trim(input.Reason, 500) || input.Expected == "" {
		return out, fail(400, "请提供当前规则版本与变更原因")
	}
	if e := s.validateRule(input.RuleParameters); e != nil {
		return out, e
	}
	e := s.queryTransaction(ctx, func(r Repository) error {
		now := s.Now()
		h, e := ruleHistory(r, scope.Owners[0], now)
		if e != nil {
			return e
		}
		old := h[0]
		if input.Expected != old.Version {
			return fail(409, "规则版本已变化，请刷新")
		}
		next := defaultRule(scope.Owners[0])
		next.RuleParameters = input.RuleParameters
		next.Revision = old.Revision + 1
		next.Version = fmt.Sprintf("followup-v%d", next.Revision)
		next.Effective = now
		next.Actor = scope.Actor
		next.Reason = input.Reason
		if e = r.Insert(record("intent_rule", privateKey(next.Owner, next.Version), next.Owner, now, next)); e != nil {
			return e
		}
		if e = r.Put(record("intent_rule_current", privateKey(next.Owner, "current"), next.Owner, now, next)); e != nil {
			return e
		}
		result, e := s.recomputeIntents(ctx, r, scope, now)
		if e != nil {
			return e
		}
		if e = audit(r, next.Owner, "intent_rule_update", scope.Actor, next.Version, now, map[string]any{"previous_version": old.Version, "reason": input.Reason, "parameters": next.RuleParameters, "recomputed": result}); e != nil {
			return e
		}
		out = RulesSnapshot{Current: next, History: append([]RuleConfig{next}, h...), Recomputed: &result}
		return nil
	})
	return out, e
}

// Evaluation state is never served as a cached label. Every query recomputes
// authorized current facts; this small record exists solely to audit changes.
type intentEvaluation struct {
	Fingerprint string    `json:"fingerprint"`
	Level       string    `json:"level"`
	Rule        string    `json:"rule_version"`
	AsOf        time.Time `json:"as_of"`
}

func (s *Service) recordIntentEvaluation(r Repository, scope Scope, key string, in Intent) (bool, error) {
	if !s.Config.IntentEnabled || len(scope.Owners) != 1 || in.ScopeRequired {
		return false, nil
	}
	owner := scope.Owners[0]
	id := privateKey(owner, key)
	next := intentEvaluation{Fingerprint: hash(string(marshal(struct {
		Level, Rule, Source string
		Metrics             IntentMetrics
		Override            *IntentOverride
	}{in.Level, in.Rule, in.sourceFingerprint, in.Metrics, in.Override}))), Level: in.Level, Rule: in.Rule, AsOf: in.Calculated}
	old, e := decode[intentEvaluation](r, "intent_evaluation", id)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return false, e
	}
	if e == nil && old.AsOf.After(next.AsOf) {
		return false, nil
	}
	if e == nil && old.Fingerprint == next.Fingerprint {
		// Advance the observation watermark even when the label is unchanged;
		// otherwise an intermediate historical snapshot can overwrite current state.
		if next.AsOf.After(old.AsOf) {
			rec := record("intent_evaluation", id, owner, next.AsOf, next)
			rec.Visitor = key
			return false, r.Put(rec)
		}
		return false, nil
	}
	rec := record("intent_evaluation", id, owner, next.AsOf, next)
	rec.Visitor = key
	if e = r.Put(rec); e != nil {
		return false, e
	}
	return true, audit(r, owner, "intent_recomputed", scope.Actor, id, next.AsOf, map[string]any{"previous_level": old.Level, "level": next.Level, "previous_rule_version": old.Rule, "rule_version": next.Rule, "reason": "authorized_facts_or_rule_changed"})
}
func (s *Service) recomputeIntents(ctx context.Context, r Repository, scope Scope, now time.Time) (RecomputeResult, error) {
	out := RecomputeResult{AsOf: now}
	data, e := s.loadIntentFacts(ctx, r, scope, now, nil)
	if e != nil {
		return out, e
	}
	out.Version = data.rule.Version
	keys := map[string]bool{}
	for key := range data.views {
		keys[key] = true
	}
	for key := range data.business {
		keys[key] = true
	}
	// An active manual decision still participates when its underlying views
	// have aged out of the rule window and no evaluation baseline exists yet.
	for _, row := range data.overrides {
		if !scope.Allows(row.Owner) || row.Visitor == "" || row.At.After(now) {
			continue
		}
		var override IntentOverride
		if json.Unmarshal(row.Payload, &override) == nil && override.Expires.After(now) {
			keys[row.Visitor] = true
		}
	}
	previous, e := scopedRecords(r, "intent_evaluation", RecordQuery{Owners: scope.Owners})
	if e != nil {
		return out, e
	}
	for _, row := range previous {
		keys[row.Visitor] = true
	}
	for _, key := range deletionKeys(keys) {
		dead, e := tombstoned(r, "deletion", key)
		if e != nil {
			return out, e
		}
		if dead {
			continue
		}
		in := s.intent(scope, key, now, data)
		changed, e := s.recordIntentEvaluation(r, scope, key, in)
		if e != nil {
			return out, e
		}
		out.Evaluated++
		if changed {
			out.Changed++
		}
	}
	return out, nil
}
func (s *Service) RecomputeIntents(ctx context.Context, scope Scope, input RecomputeInput) (RecomputeResult, error) {
	var out RecomputeResult
	if e := s.ruleScope(scope, true); e != nil {
		return out, e
	}
	if !s.Config.IntentEnabled {
		return out, fail(403, "意向展示未启用")
	}
	if !validID(input.RequestID) || !trim(input.Reason, 500) || input.Version == "" {
		return out, fail(400, "请提供幂等标识、规则版本与重算原因")
	}
	e := s.queryTransaction(ctx, func(r Repository) error {
		id := privateKey(scope.Owners[0], input.RequestID)
		signature := hash(fmt.Sprintf("%d:%s", scope.Actor, marshal(input)))
		var prior struct {
			Fingerprint string          `json:"fingerprint"`
			Result      RecomputeResult `json:"result"`
		}
		row, e := r.Get("intent_recompute_request", id)
		if e == nil {
			if json.Unmarshal(row.Payload, &prior) != nil {
				return ErrUnavailable
			}
			if prior.Fingerprint != signature {
				return fail(409, "重算标识已用于不同请求")
			}
			out = prior.Result
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		rule, _, e := activeRule(r, scope, s.Now())
		if e != nil {
			return e
		}
		if rule.Version != input.Version {
			return fail(409, "规则版本已变化，请刷新")
		}
		out, e = s.recomputeIntents(ctx, r, scope, s.Now())
		if e != nil {
			return e
		}
		prior.Fingerprint = signature
		prior.Result = out
		if e = r.Insert(record("intent_recompute_request", id, scope.Owners[0], out.AsOf, prior)); e != nil {
			return e
		}
		return audit(r, scope.Owners[0], "intent_recompute_requested", scope.Actor, input.Version, out.AsOf, map[string]any{"reason": input.Reason, "result": out})
	})
	return out, e
}
