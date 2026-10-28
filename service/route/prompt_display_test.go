package route

import "testing"

// 工具任务的 prompt 拼接了平台配置的默认提示词，任务详情只应回显用户自己的输入。
// 回归背景：2026-10-28 用户反馈生成详情页把工具默认提示词（画质参数串）当作提示词展示。
func TestParseUserPromptFromPayload(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "工具任务未填用户输入时不回退默认提示词",
			payload: `{"prompt":"MNMIX，新闽派建筑风格，画质参数串","user_prompt":"","tool_default_prompt":"MNMIX，新闽派建筑风格"}`,
			want:    "",
		},
		{
			name:    "工具任务填了用户输入时只返回用户输入",
			payload: `{"prompt":"MNMIX，新闽派建筑风格，临水小院","user_prompt":"临水小院","tool_default_prompt":"MNMIX，新闽派建筑风格"}`,
			want:    "临水小院",
		},
		{
			name:    "工具任务即使缺隐藏标志也不回退",
			payload: `{"prompt":"默认词","user_prompt":"","tool_default_prompt":"默认词"}`,
			want:    "",
		},
		{
			name:    "普通任务无用户输入仍回退完整提示词",
			payload: `{"prompt":"湖边现代风格别墅"}`,
			want:    "湖边现代风格别墅",
		},
		{
			name:    "普通任务带隐藏标志且用户输入为空返回空",
			payload: `{"prompt":"拼接提示词","user_prompt":"","hide_prompt_in_response":true}`,
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseUserPromptFromPayload(tc.payload); got != tc.want {
				t.Fatalf("parseUserPromptFromPayload(%s) = %q, want %q", tc.payload, got, tc.want)
			}
		})
	}
}
