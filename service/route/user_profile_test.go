package route

import "testing"

func TestNormalizeRecoveryPhone(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "plain mobile", input: "13800138000", want: "13800138000"},
		{name: "with country code", input: "+86 138-0013-8000", want: "13800138000"},
		{name: "with spaces", input: " 138 0013 8000 ", want: "13800138000"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeRecoveryPhone(tc.input)
			if got != tc.want {
				t.Fatalf("normalizeRecoveryPhone(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestMatchRecoveryPhone(t *testing.T) {
	if !matchRecoveryPhone("13800138000", "+86 13800138000") {
		t.Fatalf("expected phone match to be true")
	}
	if matchRecoveryPhone("13800138000", "13900139000") {
		t.Fatalf("expected phone match to be false")
	}
}

func TestMaskRecoveryPhone(t *testing.T) {
	got := maskRecoveryPhone("+86 13800138000")
	want := "138****8000"
	if got != want {
		t.Fatalf("maskRecoveryPhone returned %q, want %q", got, want)
	}
}
