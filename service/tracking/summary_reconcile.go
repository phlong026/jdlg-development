package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const maxSummaryReconcilePage = 500

type SummaryReconcileOptions struct {
	AsOf    time.Time
	AfterAt time.Time
	AfterID string
	Limit   int
	Apply   bool
}

type SummaryReconcileResult struct {
	Apply          bool                      `json:"apply"`
	AsOf           time.Time                 `json:"as_of"`
	Scanned        int                       `json:"scanned"`
	Candidates     int                       `json:"candidates"`
	Rebuilt        int                       `json:"rebuilt"`
	Skipped        int                       `json:"skipped"`
	Failed         int                       `json:"failed"`
	Sessions       []string                  `json:"sessions"`
	FailedSessions []string                  `json:"failed_sessions,omitempty"`
	Failures       []SummaryReconcileFailure `json:"failures,omitempty"`
	HasMore        bool                      `json:"has_more"`
	NextAt         time.Time                 `json:"next_at,omitempty"`
	NextID         string                    `json:"next_id,omitempty"`
}

type SummaryReconcileFailure struct {
	Session string `json:"session_id"`
	Code    string `json:"code"`
	Error   string `json:"error"`
}

var errSummaryReconcileSkipped = errors.New("summary reconcile candidate changed")

func summaryReconcileFailure(session string, err error) SummaryReconcileFailure {
	code := "rebuild_error"
	switch {
	case errors.Is(err, ErrQueryTooLarge):
		code = "query_too_large"
	case errors.Is(err, context.DeadlineExceeded):
		code = "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		code = "canceled"
	case errors.Is(err, ErrUnavailable):
		code = "tracking_unavailable"
	}
	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	return SummaryReconcileFailure{Session: session, Code: code, Error: message}
}

func summaryReconcileCandidate(row Record, state summaryState, scope Scope, asOf time.Time) bool {
	return scope.Allows(row.Owner) && state.Complete && state.Metric == MetricVersion && !state.RawExpired && state.DeferredFrom != nil && !state.DeferredFrom.After(asOf)
}

// ReconcileDeferredSummaries finds one bounded page of projections left in the
// old deferred state. Apply mode revalidates and rebuilds each session in its
// own transaction, so one oversized session cannot roll back the rest.
func (s *Service) ReconcileDeferredSummaries(ctx context.Context, scope Scope, options SummaryReconcileOptions) (SummaryReconcileResult, error) {
	result := SummaryReconcileResult{Apply: options.Apply, Sessions: []string{}, FailedSessions: []string{}, Failures: []SummaryReconcileFailure{}}
	if err := s.authorized(scope); err != nil {
		return result, err
	}
	if !scope.Manage || !scope.Rules {
		return result, fail(403, "无访问记录规则管理权限")
	}
	now := s.Now()
	if options.AsOf.IsZero() {
		options.AsOf = now
	}
	result.AsOf = options.AsOf
	if options.AsOf.After(now) {
		return result, fail(400, "快照时间无效")
	}
	if options.Limit < 0 {
		return result, fail(400, "对账分页大小无效")
	}
	if options.Limit == 0 {
		options.Limit = 100
	}
	if options.Limit > maxSummaryReconcilePage {
		return result, fail(400, "对账分页大小无效")
	}
	if (options.AfterID == "") != options.AfterAt.IsZero() || options.AfterID != "" && !validID(options.AfterID) {
		return result, fail(400, "对账游标无效")
	}

	var rows []Record
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		var err error
		rows, err = pageRecords(r, "summary_state", RecordQuery{
			Owners:  scope.Owners,
			Limit:   options.Limit + 1,
			AfterAt: options.AfterAt,
			AfterID: options.AfterID,
		})
		return err
	})
	if err != nil {
		return result, err
	}
	if len(rows) > options.Limit {
		result.HasMore = true
		rows = rows[:options.Limit]
	}
	result.Scanned = len(rows)
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		result.NextAt, result.NextID = last.At, last.ID
	}

	candidates := make([]Record, 0, len(rows))
	for _, row := range rows {
		var state summaryState
		if json.Unmarshal(row.Payload, &state) != nil {
			return result, ErrUnavailable
		}
		if summaryReconcileCandidate(row, state, scope, options.AsOf) {
			candidates = append(candidates, row)
			result.Sessions = append(result.Sessions, row.ID)
		}
	}
	result.Candidates = len(candidates)
	if !options.Apply {
		return result, nil
	}

	for _, candidate := range candidates {
		err := boundedTransaction(s.transaction, ctx, func(r Repository) error {
			row, err := r.Get("summary_state", candidate.ID)
			if errors.Is(err, ErrNotFound) {
				return errSummaryReconcileSkipped
			}
			if err != nil {
				return err
			}
			var state summaryState
			if json.Unmarshal(row.Payload, &state) != nil {
				return ErrUnavailable
			}
			if !summaryReconcileCandidate(row, state, scope, options.AsOf) {
				return errSummaryReconcileSkipped
			}
			return s.RebuildSessionSummaries(r, candidate.ID, s.Now())
		})
		switch {
		case err == nil:
			result.Rebuilt++
		case errors.Is(err, errSummaryReconcileSkipped):
			result.Skipped++
		default:
			result.Failed++
			result.FailedSessions = append(result.FailedSessions, candidate.ID)
			result.Failures = append(result.Failures, summaryReconcileFailure(candidate.ID, err))
		}
	}
	return result, nil
}
