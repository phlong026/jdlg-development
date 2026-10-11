package tracking

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// A write must select one concrete owner. Never attach private annotations to a
// tenant-wide identity or choose the first row from a mixed-owner query.
func privateOwner(r Repository, scope Scope, key string) (string, error) {
	owners := map[string]bool{}
	rows, e := r.List("event")
	if e != nil {
		return "", e
	}
	for _, row := range rows {
		if resolvedVisitor(r, row) == key && scope.Allows(row.Owner) {
			owners[row.Owner] = true
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
	return s.transaction(ctx, func(r Repository) error {
		owner, e := privateOwner(r, scope, key)
		if e != nil {
			return e
		}
		if e = s.VerifyCRM(ctx, ownerScope(scope, owner), key, customer, businessNumber); e != nil {
			return e
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
	return s.transaction(ctx, func(r Repository) error {
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
	return s.transaction(ctx, func(r Repository) error {
		owner, e := privateOwner(r, scope, key)
		if e != nil {
			return e
		}
		rec := record("intent_override", privateKey(owner, key), owner, s.Now(), map[string]any{"level": level, "reason": reason, "expires_at": expires, "actor": scope.Actor})
		rec.Visitor = key
		if e = r.Put(rec); e != nil {
			return e
		}
		return audit(r, owner, "intent_override", scope.Actor, hash(key), s.Now(), map[string]string{"level": level})
	})
}
func (s *Service) DeleteVisitor(ctx context.Context, scope Scope, key, reason string) error {
	if e := s.authorized(scope); e != nil {
		return e
	}
	if !scope.Delete || !trim(reason, 500) {
		return fail(403, "需要专门删除权限和原因")
	}
	return s.transaction(ctx, func(r Repository) error {
		sessions := map[string]bool{}
		eventIDs, viewIDs, sequenceIDs, shareIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
		found := false
		// Resolve before deleting identity links. Original anonymous aliases are not
		// global aliases of this account: shared-device history must remain separate.
		for _, kind := range []string{"event", "entry", "session", "business", "share"} {
			rows, e := r.List(kind)
			if e != nil {
				return e
			}
			for _, row := range rows {
				if kind == "entry" || kind == "session" {
					var en Entry
					if json.Unmarshal(row.Payload, &en) != nil {
						return ErrUnavailable
					}
					row.Visitor, row.Session = en.Visitor, en.Session
				}
				if kind == "share" {
					var sh Share
					if json.Unmarshal(row.Payload, &sh) != nil {
						return ErrUnavailable
					}
					row.Visitor = sh.Sharer
					if sh.Sharer == key {
						shareIDs[sh.ID] = true
					}
				}
				if resolvedVisitor(r, row) != key {
					continue
				}
				if !scope.Allows(row.Owner) {
					return fail(403, "删除需覆盖该账号全部授权范围")
				}
				found = true
				if kind == "event" {
					var ev Event
					if json.Unmarshal(row.Payload, &ev) != nil {
						return ErrUnavailable
					}
					eventIDs[row.ID] = true
					viewIDs[ev.View] = true
					sequenceIDs[hash(ev.View+":"+ev.Segment+":"+ev.Player+":"+strconv.Itoa(ev.Seq))] = true
				}
				if row.Session != "" {
					sessions[row.Session] = true
				}
			}
		}
		if !found {
			return fail(404, "访客不可访问")
		}
		if e := r.Put(record("deletion", key, "", s.Now(), map[string]any{"deleted_at": s.Now()})); e != nil {
			return e
		}
		for session := range sessions {
			if e := r.Put(record("deletion_session", session, "", s.Now(), map[string]any{"deleted_at": s.Now()})); e != nil {
				return e
			}
		}
		for _, kind := range []string{"event", "entry", "session", "identity", "crm", "intent_override", "business", "event_hash", "sequence", "view_binding", "share", "share_request"} {
			rows, e := r.List(kind)
			if e != nil {
				return e
			}
			for _, row := range rows {
				remove := row.Visitor == key || sessions[row.Session] || ((kind == "identity" || kind == "session") && sessions[row.ID]) || ((kind == "crm" || kind == "intent_override") && row.ID == key)
				if kind == "entry" || kind == "session" {
					var en Entry
					if json.Unmarshal(row.Payload, &en) != nil {
						return ErrUnavailable
					}
					remove = en.Visitor == key || sessions[en.Session]
				}
				if kind == "event_hash" {
					remove = eventIDs[row.ID]
				}
				if kind == "sequence" {
					remove = sequenceIDs[row.ID]
				}
				if kind == "view_binding" {
					var binding map[string]string
					if json.Unmarshal(row.Payload, &binding) != nil {
						return ErrUnavailable
					}
					remove = viewIDs[row.ID] || sessions[binding["session"]]
				}
				if kind == "share" {
					var sh Share
					if json.Unmarshal(row.Payload, &sh) != nil {
						return ErrUnavailable
					}
					remove = shareIDs[sh.ID]
				}
				if kind == "share_request" {
					var req map[string]any
					if json.Unmarshal(row.Payload, &req) != nil {
						return ErrUnavailable
					}
					id, _ := req["share_id"].(string)
					remove = shareIDs[id]
				}
				if remove {
					if !scope.Allows(row.Owner) {
						return fail(403, "删除需覆盖该账号全部授权范围")
					}
					if e = r.Remove(kind, row.ID); e != nil {
						return e
					}
				}
			}
		}
		// Do not copy the free-text request or deleted identity into the audit log.
		return audit(r, "", "delete_visitor", scope.Actor, hash(key), s.Now(), map[string]string{"reason_hash": hash(reason), "backup_policy": "restore_must_reapply_tombstones"})
	})
}
