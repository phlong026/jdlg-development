package model

import "testing"

// 启动场景值（1007 单聊卡片、1047 扫码等）是纯数字，历史上被当作 user_id 解析，
// 导致新用户被错误绑定给 ID 为 1007 的用户并双向发放灵石。
func TestResolveInviteCodeRejectsNumericUserIDFallback(t *testing.T) {
	notFound := func(string) (int64, bool, error) { return 0, false, nil }
	for _, code := range []string{"1007", "1008", "1047", "42", "123456"} {
		if id, ok := resolveInviteCode(code, notFound); ok || id != 0 {
			t.Fatalf("code %q resolved to user %d without a table match", code, id)
		}
	}
}

func TestResolveInviteCodeUsesTableMatch(t *testing.T) {
	found := func(code string) (int64, bool, error) {
		if code == "AB23CD" {
			return 9, true, nil
		}
		return 0, false, nil
	}
	if id, ok := resolveInviteCode("AB23CD", found); !ok || id != 9 {
		t.Fatalf("expected user 9, got %d ok=%v", id, ok)
	}
	if _, ok := resolveInviteCode("", found); ok {
		t.Fatal("empty code must not resolve")
	}
}
