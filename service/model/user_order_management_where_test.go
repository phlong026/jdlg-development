package model

import (
	"strings"
	"testing"
)

// "仅看异常单"必须在服务端对全量数据筛选；前端只过滤当前页会漏掉其他页的异常单。
// 约定：status 传 "exception" 表示 失败(failed)/待处理(pending)/卡发货(delivering>5min)/卡退款(refunding>10min)。

func TestBuildManagementWhereExceptionStatus(t *testing.T) {
	m := &UserOrderModel{}
	where, args := m.buildManagementWhere("", "all", "all", "exception", "all", "", "")

	if !strings.Contains(where, "o.status IN ('failed', 'pending')") {
		t.Fatalf("exception 应包含 failed/pending 两个状态，实际 WHERE: %s", where)
	}
	if !strings.Contains(where, "o.status = 'delivering'") || !strings.Contains(where, "INTERVAL 5 MINUTE") {
		t.Fatalf("exception 应包含卡发货(delivering>5min)条件，实际 WHERE: %s", where)
	}
	if !strings.Contains(where, "o.status = 'refunding'") || !strings.Contains(where, "INTERVAL 10 MINUTE") {
		t.Fatalf("exception 应包含卡退款(refunding>10min)条件，实际 WHERE: %s", where)
	}
	if strings.Contains(where, "o.status = ?") {
		t.Fatalf("exception 不应再按单个状态等值匹配，实际 WHERE: %s", where)
	}
	if len(args) != 0 {
		t.Fatalf("exception 展开为字面量，不应产生绑定参数，实际: %v", args)
	}
}

func TestBuildManagementWhereSingleStatusUnchanged(t *testing.T) {
	m := &UserOrderModel{}
	where, args := m.buildManagementWhere("", "all", "all", "success", "all", "", "")

	if !strings.Contains(where, "o.status = ?") {
		t.Fatalf("普通状态筛选必须保持等值匹配，实际 WHERE: %s", where)
	}
	if len(args) != 1 || args[0] != "success" {
		t.Fatalf("期望参数为 [success]，实际: %v", args)
	}
}

func TestBuildManagementWhereAllStatusAddsNoCondition(t *testing.T) {
	m := &UserOrderModel{}
	where, _ := m.buildManagementWhere("", "all", "all", "all", "all", "", "")

	if strings.Contains(where, "o.status") {
		t.Fatalf("status=all 不应产生状态条件，实际 WHERE: %s", where)
	}
}

func TestBuildManagementWhereExceptionCombinesWithOtherFilters(t *testing.T) {
	m := &UserOrderModel{}
	where, args := m.buildManagementWhere("", "recharge", "all", "exception", "virtualpay", "", "")

	for _, want := range []string{"o.type = ?", "o.status IN ('failed', 'pending')", "COALESCE(o.pay_channel, 'wechatpay') = ?"} {
		if !strings.Contains(where, want) {
			t.Fatalf("缺少条件 %q，实际 WHERE: %s", want, where)
		}
	}
	if len(args) != 2 || args[0] != "recharge" || args[1] != "virtualpay" {
		t.Fatalf("期望参数 [recharge virtualpay]，实际: %v", args)
	}
}
