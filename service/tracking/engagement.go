package tracking

import (
	"context"
	"strconv"
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
