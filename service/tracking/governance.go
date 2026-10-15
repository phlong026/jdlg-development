package tracking

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// RetentionPolicy is an explicit maintenance input, not an automatic scheduler.
// Enabling collection does not enable destructive maintenance.
type RetentionPolicy struct {
	Enabled     bool `json:"enabled"`
	RawDays     int  `json:"raw_days"`
	SummaryDays int  `json:"summary_days"`
}

func DefaultRetentionPolicy() RetentionPolicy { return RetentionPolicy{RawDays: 90, SummaryDays: 180} }

type RetentionPlan struct {
	RawBefore     time.Time      `json:"raw_before"`
	SummaryBefore time.Time      `json:"summary_before"`
	Counts        map[string]int `json:"counts"`
	ReviewDigest  string         `json:"review_digest"`
}

var rawRetentionKinds = []string{"event", "event_hash", "sequence", "view_binding", "entry", "rate"}
var summaryRetentionKinds = []string{"session", "identity", "business", "crm", "intent_override", "intent_override_history", "intent_override_boundary", "intent_evaluation", "intent_recompute_request", "share", "share_request", "daily_summary", "summary_state"}

func (s *Service) retentionAllowed(scope Scope, p RetentionPolicy, raw, summary time.Time) error {
	if e := s.authorized(scope); e != nil {
		return e
	}
	if !scope.Delete || !scope.Rules {
		return fail(403, "保留期维护需要删除及规则管理权限")
	}
	if p.RawDays < 1 || p.SummaryDays < p.RawDays || p.SummaryDays > 3650 {
		return fail(400, "保留期配置无效")
	}
	// Explicit cutoffs may be more conservative than the configured minimum,
	// never newer. Exactly-on-cutoff records are retained.
	if raw.IsZero() || summary.IsZero() || summary.After(raw) || raw.After(s.Now().AddDate(0, 0, -p.RawDays)) || summary.After(s.Now().AddDate(0, 0, -p.SummaryDays)) {
		return fail(400, "维护截止时间超出保留期")
	}
	return nil
}
func retentionCandidates(r Repository, scope Scope, raw, summary time.Time) (RetentionPlan, []Record, error) {
	plan := RetentionPlan{RawBefore: raw.UTC(), SummaryBefore: summary.UTC(), Counts: map[string]int{}}
	candidates := []Record{}
	for _, group := range []struct {
		kinds  []string
		before time.Time
	}{{rawRetentionKinds, raw}, {summaryRetentionKinds, summary}} {
		for _, kind := range group.kinds {
			rows, e := r.List(kind)
			if e != nil {
				return plan, nil, e
			}
			plan.Counts[kind] = 0
			for _, row := range rows {
				if scope.Allows(row.Owner) && !row.At.IsZero() && row.At.Before(group.before) {
					candidates = append(candidates, row)
					plan.Counts[kind]++
				}
			}
		}
	}
	// Digest is opaque and bound to actor, tenant, owners, dates, and exact rows.
	// A reviewed plan cannot be reused for different records or permissions.
	fingerprints := []string{}
	for _, r := range candidates {
		fingerprints = append(fingerprints, hash(r.Kind+"\x00"+r.ID+"\x00"+r.Owner+"\x00"+r.At.UTC().Format(time.RFC3339Nano)+"\x00"+string(r.Payload)))
	}
	sort.Strings(fingerprints)
	owners := append([]string{}, scope.Owners...)
	sort.Strings(owners)
	plan.ReviewDigest = hash(fmt.Sprintf("%s:%d:%s:%s:%s:%s", scope.Tenant, scope.Actor, marshal(owners), raw.UTC().Format(time.RFC3339Nano), summary.UTC().Format(time.RFC3339Nano), marshal(fingerprints)))
	return plan, candidates, nil
}

// PlanRetention is a read-only dry run. Results contain counters and an opaque
// review digest, never visitor IDs, content, contact data, or raw event payloads.
func (s *Service) PlanRetention(ctx context.Context, scope Scope, p RetentionPolicy, rawBefore, summaryBefore time.Time) (RetentionPlan, error) {
	var plan RetentionPlan
	if e := s.retentionAllowed(scope, p, rawBefore, summaryBefore); e != nil {
		return plan, e
	}
	e := s.readTransaction(ctx, func(r Repository) error {
		var err error
		plan, _, err = retentionCandidates(r, scope, rawBefore, summaryBefore)
		return err
	})
	return plan, e
}

// PurgeExpired is deliberately not exposed as an HTTP route or started by New.
// A maintenance operator must enable the policy and supply a reviewed dry run.
// No tombstone, audit, or published-content kind is eligible. Restored backups
// must still replay deletion tombstones before they can serve traffic.
func (s *Service) PurgeExpired(ctx context.Context, scope Scope, p RetentionPolicy, reviewed RetentionPlan) (RetentionPlan, error) {
	var out RetentionPlan
	if !p.Enabled {
		return out, fail(403, "保留期清理未启用")
	}
	if e := s.retentionAllowed(scope, p, reviewed.RawBefore, reviewed.SummaryBefore); e != nil {
		return out, e
	}
	if reviewed.ReviewDigest == "" {
		return out, fail(400, "需要审核后的试运行计划")
	}
	err := s.transaction(ctx, func(r Repository) error {
		plan, rows, e := retentionCandidates(r, scope, reviewed.RawBefore, reviewed.SummaryBefore)
		if e != nil {
			return e
		}
		if plan.ReviewDigest != reviewed.ReviewDigest {
			return fail(409, "维护计划已变化，请重新试运行并审核")
		}
		if e = s.PrepareSummaryRetention(r, rows, s.Now(), reviewed.SummaryBefore); e != nil {
			return e
		}
		for _, row := range rows {
			if e = r.Remove(row.Kind, row.ID); e != nil {
				return e
			}
		}
		if e = prepareIntentOverrideRetention(r, rows, reviewed.SummaryBefore, s.Now()); e != nil {
			return e
		}
		out = plan
		owner := ""
		if len(scope.Owners) == 1 {
			owner = scope.Owners[0]
		}
		return audit(r, owner, "retention_purge", scope.Actor, plan.ReviewDigest, s.Now(), map[string]any{"counts": plan.Counts, "raw_before": plan.RawBefore, "summary_before": plan.SummaryBefore})
	})
	if err != nil {
		return RetentionPlan{}, err
	}
	return out, nil
}

// Diagnostics returns finite, owner-scoped counters, never event IDs or payloads.
// Rejected requests that were not persisted are intentionally not guessed here.
func (s *Service) Diagnostics(ctx context.Context, scope Scope) (map[string]int, error) {
	if e := s.authorizeView(scope); e != nil {
		return nil, e
	}
	counters := map[string]int{"accepted_events": 0, "malformed_events": 0, "sampling_gap": 0, "inferred_seek": 0, "queue_loss": 0, "late": 0, "self": 0, "preview": 0}
	err := s.readTransaction(ctx, func(r Repository) error {
		rows, e := r.List("event")
		if e != nil {
			return e
		}
		for _, row := range rows {
			if !scope.Allows(row.Owner) {
				continue
			}
			var event Event
			if json.Unmarshal(row.Payload, &event) != nil {
				counters["malformed_events"]++
				continue
			}
			counters["accepted_events"]++
			seen := map[string]bool{}
			for _, flag := range event.Flags {
				switch flag {
				case "sampling_gap", "inferred_seek", "queue_loss", "late", "self", "preview":
					if !seen[flag] {
						counters[flag]++
						seen[flag] = true
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return counters, nil
}
