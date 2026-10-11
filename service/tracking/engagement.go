package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// ConfirmFavorite records only a successfully committed, signed-in favorite.
// The restricted analytics context must belong to that same verified account.
func (s *Service) ConfirmFavorite(ctx context.Context, token string, account int64, targetType string, targetID int64) error {
	if !s.Config.Enabled || account <= 0 || targetID <= 0 {
		return fail(403, "业务采集不可用")
	}
	allowed := map[string]bool{"template": true, "ai_tool": true, "designer": true, "inspiration": true}
	if !allowed[targetType] {
		return fail(400, "收藏类型无效")
	}
	return s.transaction(ctx, func(r Repository) error {
		en, e := s.entry(r, token)
		if e != nil {
			return e
		}
		if en.Account != account || en.Visitor != "u:"+strconv.FormatInt(account, 10) {
			return fail(403, "收藏账号与访问上下文不一致")
		}
		id := hash("favorite:" + en.Visitor + ":" + targetType + ":" + strconv.FormatInt(targetID, 10) + ":" + s.Now().UTC().Format("2006-01-02"))
		if _, e = r.Get("business", id); e == nil {
			return nil
		}
		rec := record("business", id, en.Owner, s.Now(), map[string]string{"action": "favorite", "session": en.Session, "target_type": targetType, "target_id": strconv.FormatInt(targetID, 10)})
		rec.Visitor = en.Visitor
		rec.Session = en.Session
		return r.Insert(rec)
	})
}

// ConfirmConsultation is called only after an explicit consultation request has
// committed. Generic lead autosaves, generated plans and client event labels are
// not consultation success. The caller supplies the authenticated account and
// committed lead ID; personal lead fields never enter tracking payloads.
func (s *Service) ConfirmConsultation(ctx context.Context, token string, account, leadID int64) error {
	if !s.Config.Enabled || account <= 0 || leadID <= 0 {
		return fail(403, "业务采集不可用")
	}
	return s.transaction(ctx, func(r Repository) error {
		en, e := s.entry(r, token)
		if e != nil {
			return e
		}
		if en.Account != account || en.Visitor != "u:"+strconv.FormatInt(account, 10) {
			return fail(403, "咨询账号与访问上下文不一致")
		}
		if en.Preview || en.Self {
			return fail(403, "预览或自访不计业务结果")
		}
		dead, e := tombstoned(r, "deletion", en.Visitor)
		if e != nil {
			return e
		}
		if dead {
			return fail(410, "访问记录已删除")
		}
		dead, e = tombstoned(r, "deletion_session", en.Session)
		if e != nil {
			return e
		}
		if dead {
			return fail(410, "访问记录已删除")
		}
		id := hash("consultation:lead:" + strconv.FormatInt(leadID, 10))
		old, e := r.Get("business", id)
		if e == nil {
			if old.Visitor != en.Visitor || old.Owner != en.Owner {
				return fail(409, "业务结果已有其他归属")
			}
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		rec := record("business", id, en.Owner, s.Now(), map[string]string{"action": "consultation", "session": en.Session, "business_record_id": "lead:" + strconv.FormatInt(leadID, 10)})
		rec.Visitor = en.Visitor
		rec.Session = en.Session
		return r.Insert(rec)
	})
}

// sourceBusinessProof extracts only identifiers stored by trusted business hooks.
func sourceBusinessProof(row Record) (BusinessProof, bool) {
	var detail map[string]string
	if json.Unmarshal(row.Payload, &detail) != nil || !strings.HasPrefix(row.Visitor, "u:") {
		return BusinessProof{}, false
	}
	account, e := strconv.ParseInt(strings.TrimPrefix(row.Visitor, "u:"), 10, 64)
	if e != nil || account <= 0 {
		return BusinessProof{}, false
	}
	proof := BusinessProof{Action: detail["action"], Account: account}
	switch proof.Action {
	case "favorite":
		proof.TargetType = detail["target_type"]
		proof.TargetID, e = strconv.ParseInt(detail["target_id"], 10, 64)
	case "consultation":
		if !strings.HasPrefix(detail["business_record_id"], "lead:") {
			return proof, false
		}
		proof.TargetType = "lead"
		proof.TargetID, e = strconv.ParseInt(strings.TrimPrefix(detail["business_record_id"], "lead:"), 10, 64)
	default:
		// No production appointment/quote proof adapter has been verified yet.
		return proof, false
	}
	return proof, e == nil && proof.TargetID > 0
}
