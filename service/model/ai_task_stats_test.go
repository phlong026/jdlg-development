package model

import "testing"

func TestClassifyAIFailReason(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want string
	}{
		{"上游超时-英文", "PROV_TIMEOUT: upstream request timeout after 60s", "上游超时"},
		{"上游超时-轮询", "ToAPIs 异步任务轮询超限", "上游超时"},
		{"配额不足", "429 quota exceeded for this key", "配额/余额不足"},
		{"余额不足", "上游返回 insufficient balance", "配额/余额不足"},
		{"内容拦截", "request blocked by safety system", "内容安全拦截"},
		{"敏感词", "命中敏感词拦截", "内容安全拦截"},
		{"解析失败", "failed to parse image from response b64 data", "结果解析失败"},
		{"其他", "connection reset by peer", "其他"},
		{"空串", "", "其他"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyAIFailReason(tc.msg); got != tc.want {
				t.Errorf("ClassifyAIFailReason(%q) = %q, want %q", tc.msg, got, tc.want)
			}
		})
	}
}

func TestExtractRefundedStones(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    int64
	}{
		{"部分退款", `{"images":[],"refunded_stones":10,"refunded_image_count":1}`, 10},
		{"全额退款", `{"refunded_stones":30}`, 30},
		{"无退款字段", `{"images":["a.png"]}`, 0},
		{"空载荷", ``, 0},
		{"坏JSON", `{"refunded_stones":`, 0},
		{"零值", `{"refunded_stones":0}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractRefundedStones(tc.payload); got != tc.want {
				t.Errorf("extractRefundedStones(%q) = %d, want %d", tc.payload, got, tc.want)
			}
		})
	}
}

func TestQueueConditionForManagement(t *testing.T) {
	cases := []struct {
		queue string
		want  string
	}{
		{"failed", " AND a.status = 'failed' AND a.created_at >= NOW() - INTERVAL 7 DAY"},
		{"timeout", " AND a.status IN ('pending', 'running', 'processing') AND a.updated_at < NOW() - INTERVAL 30 MINUTE"},
		{"running", " AND a.status IN ('pending', 'running', 'processing')"},
		{"today", " AND a.created_at >= CURDATE()"},
		{"all", ""},
		{"未知值", ""},
	}
	for _, tc := range cases {
		cond, args := queueConditionForManagement(tc.queue)
		if cond != tc.want {
			t.Errorf("queue=%q cond = %q, want %q", tc.queue, cond, tc.want)
		}
		if len(args) != 0 {
			t.Errorf("queue=%q 应无参数, got %v", tc.queue, args)
		}
	}
}
