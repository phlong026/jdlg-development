package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// A write must select one concrete owner. Never attach private annotations to a
// tenant-wide identity or choose the first row from a mixed-owner query.
func privateOwner(r Repository, scope Scope, key string) (string, error) {
	owners := map[string]bool{}
	for _, kind := range []string{"event", "daily_summary", "crm", "intent_override", "intent_override_history", "intent_override_boundary"} {
		rows, e := scopedRecords(r, kind, RecordQuery{Owners: permittedOwners(scope, ""), VisitorKeys: []string{key}})
		if e != nil {
			return "", e
		}
		for _, row := range rows {
			visitor, err := privacyVisitor(r, row)
			if err != nil {
				return "", err
			}
			if visitor == key && scope.Allows(row.Owner) {
				owners[row.Owner] = true
			}
		}
	}
	if len(owners) == 0 {
		return "", fail(404, "账号记录不可访问")
	}
	if len(owners) != 1 {
		return "", fail(409, "请明确单一归属范围")
	}
	for owner := range owners {
		return owner, nil
	}
	return "", ErrUnavailable
}

// crmLinkVerifyTimeout bounds the CRM check made when an operator links a visitor
// to a customer. It is one explicit action, so it allows more than the 2 seconds
// VisitorCRMTarget uses while drawing a list. A variable only so a test can shorten it.
var crmLinkVerifyTimeout = 5 * time.Second

func (s *Service) LinkCRM(ctx context.Context, scope Scope, key, customer, businessNumber, basis string) error {
	if e := s.authorizeView(scope); e != nil {
		return e
	}
	if !scope.CRM || !strings.HasPrefix(key, "u:") || !trim(customer, 128) || !trim(businessNumber, 100) || !trim(basis, 500) {
		return fail(403, "需要明确CRM映射权限和依据")
	}
	// A caller-supplied customer ID, display number, or rationale is not business validation.
	if s.VerifyCRM == nil {
		return fail(403, "CRM业务关系验证尚未接入")
	}
	var owner string
	if e := s.readQueryTransaction(ctx, func(r Repository) error {
		var e error
		owner, e = privateOwner(r, scope, key)
		return e
	}); e != nil {
		return e
	}
	// The CRM bridge is an external HTTP call. It must not run while holding the
	// serialized tenant lock that fact acceptance needs, and it must not hold the
	// operator's request for as long as the bridge's own 20 second client timeout.
	verifyCtx, cancel := context.WithTimeout(ctx, crmLinkVerifyTimeout)
	defer cancel()
	if e := s.VerifyCRM(verifyCtx, ownerScope(scope, owner), key, customer, businessNumber); e != nil {
		return e
	}
	return s.queryTransaction(ctx, func(r Repository) error {
		// Re-resolve under the lock: the visitor may have been deleted or gained a
		// second owner while the bridge answered.
		current, e := privateOwner(r, scope, key)
		if e != nil {
			return e
		}
		if current != owner {
			return fail(409, "访客归属已变化，请刷新后重试")
		}
		rec := record("crm", privateKey(owner, key), owner, s.Now(), map[string]string{"customer_id": customer, "business_number": businessNumber, "basis": basis, "source": "verified_upstream"})
		rec.Visitor = key
		if e = r.Put(rec); e != nil {
			return e
		}
		return audit(r, owner, "crm_link", scope.Actor, hash(key), s.Now(), map[string]string{"source": "verified_upstream"})
	})
}
func (s *Service) UnlinkCRM(ctx context.Context, scope Scope, key string) error {
	if e := s.authorizeView(scope); e != nil {
		return e
	}
	if !scope.CRM {
		return fail(403, "无映射权限")
	}
	return s.queryTransaction(ctx, func(r Repository) error {
		owner, e := privateOwner(r, scope, key)
		if e != nil {
			return e
		}
		id := privateKey(owner, key)
		rec, e := r.Get("crm", id)
		if e != nil || rec.Owner != owner {
			return fail(404, "映射不可访问")
		}
		if e = r.Remove("crm", id); e != nil {
			return e
		}
		return audit(r, owner, "crm_unlink", scope.Actor, hash(key), s.Now(), nil)
	})
}
func (s *Service) OverrideIntent(ctx context.Context, scope Scope, key, level, reason string, expires time.Time) error {
	if e := s.authorizeView(scope); e != nil {
		return e
	}
	if !scope.Rules || !trim(reason, 500) || !expires.After(s.Now()) || expires.After(s.Now().AddDate(0, 0, 30)) || (level != "high" && level != "medium" && level != "low" && level != "unknown") {
		return fail(403, "规则修正未授权或参数无效")
	}
	return s.queryTransaction(ctx, func(r Repository) error {
		owner, e := privateOwner(r, scope, key)
		if e != nil {
			return e
		}
		return appendIntentOverride(r, owner, key, scope.Actor, level, reason, expires, s.Now())
	})
}

// deletionMark is the stored tombstone payload. Tombstones written before
// staged deletion existed carry only deleted_at and count as complete.
type deletionMark struct {
	DeletedAt  time.Time `json:"deleted_at"`
	InProgress bool      `json:"in_progress,omitempty"`
}

func readDeletionMark(r Repository, key string) (mark deletionMark, exists bool, err error) {
	row, e := r.Get("deletion", key)
	if errors.Is(e, ErrNotFound) {
		return mark, false, nil
	}
	if e != nil {
		return mark, false, e
	}
	if json.Unmarshal(row.Payload, &mark) != nil {
		return mark, true, ErrUnavailable
	}
	return mark, true, nil
}

// DeleteVisitor deletes everything for the visitor and returns only when the
// deletion is complete.
func (s *Service) DeleteVisitor(ctx context.Context, scope Scope, key, reason string) error {
	_, err := s.DeleteVisitorProgress(ctx, scope, key, reason, 0)
	return err
}

// DeleteVisitorProgress reports whether deletion finished. A deletion that fits
// the read budget runs in one atomic transaction, as before. One that does not
// fits is staged: scope is verified first, tombstones are written so the data is
// already unreadable and no new data is accepted, then rows are removed in short
// transactions. maxPages > 0 stops a staged run early and returns done=false;
// calling again resumes. A failure after the tombstone leaves data hidden and
// the deletion resumable, never silently complete.
func (s *Service) DeleteVisitorProgress(ctx context.Context, scope Scope, key, reason string, maxPages int) (bool, error) {
	if e := s.authorized(scope); e != nil {
		return false, e
	}
	if !scope.Delete || !trim(reason, 500) {
		return false, fail(403, "需要专门删除权限和原因")
	}
	var mark deletionMark
	var started bool
	if e := s.readQueryTransaction(ctx, func(r Repository) error {
		var err error
		mark, started, err = readDeletionMark(r, key)
		return err
	}); e != nil {
		return false, e
	}
	// Only an unfinished staged deletion may be resumed with no rows left. A
	// finished one answers like an unknown visitor (404), so a caller without
	// access cannot learn that an account was deleted.
	resuming := started && mark.InProgress
	if !s.forceStagedDeletion {
		err := s.deleteVisitorAtomic(ctx, scope, key, reason, resuming)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, ErrQueryTooLarge) {
			return false, err
		}
	}
	return s.deleteVisitorStaged(ctx, scope, key, reason, maxPages)
}

func (s *Service) deleteVisitorAtomic(ctx context.Context, scope Scope, key, reason string, resuming bool) error {
	return s.queryTransaction(ctx, func(r Repository) error {
		plan, e := planVisitorDeletion(r, scope, key, resuming)
		if e != nil {
			return e
		}
		// Resolve all identities, dependencies and owners before mutating anything.
		// Tombstones, removals and audit still commit in the same transaction.
		if e := r.Put(record("deletion", key, "", s.Now(), deletionMark{DeletedAt: s.Now()})); e != nil {
			return e
		}
		for session := range plan.sessions {
			if e := r.Put(sessionTombstone(session, key, s.Now())); e != nil {
				return e
			}
		}
		for _, row := range plan.rows {
			if e := r.Remove(row.Kind, row.ID); e != nil {
				return e
			}
		}
		// Do not copy the free-text request or deleted identity into the audit log.
		return audit(r, "", "delete_visitor", scope.Actor, hash(key), s.Now(), map[string]string{"reason_hash": hash(reason), "backup_policy": "restore_must_reapply_tombstones"})
	})
}
