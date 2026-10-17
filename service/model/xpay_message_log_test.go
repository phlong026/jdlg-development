package model

import (
	"strings"
	"testing"
)

// 回归：带关键词的 COUNT 曾因缺少客户 JOIN 报 Unknown column 'u.username'（线上 500）。
// WHERE 里引用 u.username 时，COUNT 语句必须同样拼接 xpayLogCustomerJoin。
func TestXPayLogWhereRequiresCustomerJoin(t *testing.T) {
	where, args := buildXPayLogWhere("ORD123", "all", nil)
	if !strings.Contains(where, "u.username LIKE ?") {
		t.Fatalf("关键词条件应命中 u.username，实际 WHERE: %s", where)
	}
	if len(args) != 4 {
		t.Fatalf("关键词条件应有 4 个 LIKE 参数，实际 %d 个", len(args))
	}
	countSQL := "SELECT COUNT(*) FROM xpay_message_logs l" + xpayLogCustomerJoin + where
	for _, part := range []string{"LEFT JOIN user_orders o", "LEFT JOIN users u"} {
		if !strings.Contains(countSQL, part) {
			t.Fatalf("COUNT 语句缺 %s，会复现 Unknown column 'u.username'：\n%s", part, countSQL)
		}
	}
}

// 无关键词时 WHERE 不引用 u.username，历史上不带 JOIN 的 COUNT 也成立；
// 锁定这一行为，避免有人误以为 JOIN 是无条件必需。
func TestXPayLogWhereWithoutKeyword(t *testing.T) {
	where, args := buildXPayLogWhere("", "all", nil)
	if strings.Contains(where, "u.username") {
		t.Fatalf("无关键词不应引用 u.username，实际 WHERE: %s", where)
	}
	if len(args) != 0 {
		t.Fatalf("无筛选条件不应有参数，实际 %d 个", len(args))
	}
	processed := false
	where, args = buildXPayLogWhere("", "xpay_coin_pay_notify", &processed)
	if !strings.Contains(where, "l.event = ?") || !strings.Contains(where, "l.processed = ?") {
		t.Fatalf("事件与处理状态条件缺失，实际 WHERE: %s", where)
	}
	if len(args) != 2 {
		t.Fatalf("事件+状态应有 2 个参数，实际 %d 个", len(args))
	}
}
