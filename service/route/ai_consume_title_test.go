package route

import "testing"

func TestAIConsumeTitleUsesSceneLabel(t *testing.T) {
	cases := []struct {
		scene string
		count int64
		want  string
	}{
		{"ai_draw_single", 3, "AI生成-AI 绘画-单图-3张"},
		{"ai_draw_multi", 4, "AI生成-AI 绘画-多图-4张"},
		{"new_minnan_tool_text_to_image", 1, "AI生成-新闽派工具-文生图-1张"},
		// 未登记的场景回退到原代码，保证标题仍可追溯
		{"unknown_scene", 2, "AI生成-unknown_scene-2张"},
	}
	for _, tc := range cases {
		if got := aiConsumeTitle(tc.scene, tc.count); got != tc.want {
			t.Errorf("aiConsumeTitle(%q, %d) = %q, want %q", tc.scene, tc.count, got, tc.want)
		}
	}
}
