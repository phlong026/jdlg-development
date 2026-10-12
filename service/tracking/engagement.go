package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// FavoriteReceipt is provided only by the committed favorite source row. A
// remove/re-add creates a new ID; retries reuse both ID and original creation time.
type FavoriteReceipt struct {
	ID        int64
	CreatedAt time.Time
}

// ConfirmFavorite records a durable success, never the date of a telemetry retry.
// Receipt-less callers from before durable source binding fail closed.
func (s *Service) ConfirmFavorite(ctx context.Context, token string, account int64, targetType string, targetID int64, receipts ...FavoriteReceipt) error {
	if !s.Config.Enabled || account <= 0 || targetID <= 0 {
		return fail(403, "业务采集不可用")
	}
	allowed := map[string]bool{"template": true, "ai_tool": true, "designer": true, "inspiration": true}
	if !allowed[targetType] || len(receipts) != 1 || receipts[0].ID <= 0 || receipts[0].CreatedAt.IsZero() || receipts[0].CreatedAt.After(s.Now().Add(2*time.Minute)) {
		return fail(400, "缺少有效收藏成功凭据")
	}
	receipt := receipts[0]
	return s.transaction(ctx, func(r Repository) error {
		en, e := s.entry(r, token)
		if e != nil {
			return e
		}
		if en.Account != account || en.Visitor != "u:"+strconv.FormatInt(account, 10) {
			return fail(403, "收藏账号与访问上下文不一致")
		}
		if en.Preview || en.Self {
			return fail(403, "预览或自访不计业务结果")
		}
		id := hash("favorite:receipt:" + strconv.FormatInt(receipt.ID, 10))
		old, e := r.Get("business", id)
		if e == nil {
			proof, ok := sourceBusinessProof(old)
			if !ok || proof.Action != "favorite" || proof.FavoriteReceiptID != receipt.ID || old.Owner != en.Owner || proof.Account != account || proof.TargetType != targetType || proof.TargetID != targetID || !proof.FavoriteCreatedAt.Equal(receipt.CreatedAt) {
				return fail(409, "收藏成功凭据已有其他归属")
			}
			return nil
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		// A later session cannot turn an old uncollected favorite into new intent.
		if receipt.CreatedAt.Before(en.Started.Add(-2 * time.Minute)) {
			return fail(409, "收藏成功时间不属于当前访问上下文")
		}
		proof := BusinessProof{Action: "favorite", Account: account, TargetType: targetType, TargetID: targetID, FavoriteReceiptID: receipt.ID, FavoriteCreatedAt: receipt.CreatedAt}
		if s.VerifyBusiness != nil {
			valid, err := s.VerifyBusiness(sourceReadContext(ctx, r), proof)
			if err != nil {
				return ErrUnavailable
			}
			if !valid {
				return fail(409, "收藏成功凭据已失效")
			}
		}
		rec := record("business", id, en.Owner, receipt.CreatedAt.UTC(), map[string]string{"action": "favorite", "session": en.Session, "target_type": targetType, "target_id": strconv.FormatInt(targetID, 10), "favorite_receipt_id": strconv.FormatInt(receipt.ID, 10), "favorite_created_at": receipt.CreatedAt.UTC().Format(time.RFC3339Nano)})
		rec.Visitor, rec.Session = en.Visitor, en.Session
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
		if e != nil {
			return proof, false
		}
		proof.FavoriteReceiptID, e = strconv.ParseInt(detail["favorite_receipt_id"], 10, 64)
		if e != nil || proof.FavoriteReceiptID <= 0 {
			return proof, false
		}
		proof.FavoriteCreatedAt, e = time.Parse(time.RFC3339Nano, detail["favorite_created_at"])
		if e != nil || proof.FavoriteCreatedAt.IsZero() || !row.At.Truncate(time.Microsecond).Equal(proof.FavoriteCreatedAt.Truncate(time.Microsecond)) {
			return proof, false
		}
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
