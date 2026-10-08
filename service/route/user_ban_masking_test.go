package route

import "testing"

// maskPhoneNumber：管理端列表手机号脱敏（纯函数，无 DB 依赖）
func TestMaskPhoneNumber(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"13861026102", "138****6102"},
		{"13933917777", "139****7777"},
		{"12345", "1****5"},
		{"1234567890123", "123****123"},
		{"abc-defg-hij", "abc****hij"},
	}
	for _, tc := range cases {
		if got := maskPhoneNumber(tc.in); got != tc.want {
			t.Fatalf("maskPhoneNumber(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 脱敏绝不能把完整号码原样放行（长度>4 的输入必然包含 ****）
func TestMaskPhoneNumberNeverLeaksFull(t *testing.T) {
	for _, in := range []string{"13861026102", "0571-88991234", "17000000000"} {
		out := maskPhoneNumber(in)
		if len(in) > 4 && out == in {
			t.Fatalf("maskPhoneNumber(%q) leaked full value", in)
		}
	}
}

func TestParsePositiveQueryInt(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0}, {"abc", 0}, {"0", 0}, {"-3", 0}, {"7", 7}, {" 30 ", 30},
	}
	for _, tc := range cases {
		if got := parsePositiveQueryInt(tc.in); got != tc.want {
			t.Fatalf("parsePositiveQueryInt(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
