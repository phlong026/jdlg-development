package route

import "testing"

// TestParseAppIDFromDescription 锁定 description 解析规则。
//
// 这条测试存在的意义：发货分发的认证工单 ID 完全依赖这条解析规则。
// 一旦格式被改坏（例如多了空格、改了分隔符），认证支付成功后工单不会推进，
// 用户会以为"付了钱但认证没生效"，是高客诉风险。
func TestParseAppIDFromDescription(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"certification:42", 42},
		{"certification:0", 0},     // 0 视为非法，避免触发空 GetByID 查询
		{"certification:", 0},      // 缺 id
		{"cert:42", 0},             // 错误前缀
		{"", 0},                    // 空串
		{"certification:abc", 0},   // 非数字
		{"certification:9999999", 9999999},
	}
	for _, c := range cases {
		got := parseAppIDFromDescription(c.in)
		if got != c.want {
			t.Errorf("parse(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
