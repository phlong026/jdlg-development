package component

import "testing"

func TestStripUserPromptFromAIDraw(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "current system prefix and suffix",
			in:   AIDrawPromptPrefix + "三层闽南建筑，生成方向：外立面，画面风格：现代简约，画面清晰度：高清，画布大小：16:9",
			want: "三层闽南建筑",
		},
		{
			name: "legacy system prefix",
			in:   legacyAIDrawPromptPrefix + "三层闽南建筑",
			want: "三层闽南建筑",
		},
		{
			name: "legacy v1 system prefix",
			in:   legacyAIDrawPromptPrefixV1 + "三层闽南建筑",
			want: "三层闽南建筑",
		},
		{
			name: "plain user prompt",
			in:   "三层闽南建筑",
			want: "三层闽南建筑",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripUserPromptFromAIDraw(tt.in); got != tt.want {
				t.Fatalf("StripUserPromptFromAIDraw() = %q, want %q", got, tt.want)
			}
		})
	}
}
