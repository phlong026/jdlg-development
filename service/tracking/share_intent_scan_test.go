package tracking

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 生产回归（2026-10-29）：访问列表 7 天/30 天档位整体 422「查询范围过大」。
// clientShares 核验预热分享是否有真实转发意图时，把窗口内所有分享会话的原始
// 事件整段拉进内存；分享覆盖的会话随流量增长后，这一扫描单独吃满单请求读预算。
// 核验逻辑只使用 share_intent 事件，扫描必须按事件类型收敛。
func TestClientSharesIntentScanStaysWithinReadBudget(t *testing.T) {
	s, scope := fixture(t)
	r := &memoryRepo{data: map[string]Record{}}
	start := s.Now().AddDate(0, 0, -6)
	const sessions = 400
	const fillerPerSession = 80 // 400×80 条填充事件（3.2 万行）超过 3 万行读预算，share_intent 只有 400 条
	for i := 0; i < sessions; i++ {
		session := fmt.Sprintf("session-%04d", i)
		visitor := fmt.Sprintf("anon-%04d", i)
		shareID := fmt.Sprintf("share-%04d", i)
		shareAt := start.Add(time.Duration(i) * time.Minute)
		for j := 0; j < fillerPerSession; j++ {
			at := shareAt.Add(time.Duration(j+1) * time.Second)
			ev := Event{Schema: 1, ID: fmt.Sprintf("seg-%04d-%03d", i, j), Type: "visible_segment", Session: session, View: "view-" + session, Route: "pages/contentreader/contentreader", Occurred: at, Received: at, Delta: 1000, AppState: "foreground", PageState: "visible", Monotonic: 20000}
			row := record("event", ev.ID, "owner-a", at, ev)
			row.Visitor, row.Session = visitor, session
			if e := r.Insert(row); e != nil {
				t.Fatal(e)
			}
		}
		sh := Share{ID: shareID, Route: "pages/contentreader/contentreader", Owner: "owner-a", Sharer: visitor, Session: session, Prepared: true, Origin: "miniprogram"}
		row := record("share", shareID, "owner-a", shareAt, sh)
		row.Visitor, row.Session = visitor, session
		if e := r.Insert(row); e != nil {
			t.Fatal(e)
		}
		intent := Event{Schema: 1, ID: "intent-" + shareID, Type: "share_intent", Session: session, Route: "pages/contentreader/contentreader", Share: shareID, SourceChannel: "app_message", Occurred: shareAt.Add(time.Minute), Received: shareAt.Add(time.Minute), AppState: "foreground", PageState: "visible", Monotonic: 20000}
		row = record("event", intent.ID, "owner-a", intent.Occurred, intent)
		row.Visitor, row.Session, row.Share = visitor, session, shareID
		if e := r.Insert(row); e != nil {
			t.Fatal(e)
		}
	}
	var out []shareFact
	// 直接以种子 repo 作为读事务通道：这里验证的是预算与扫描路径本身，
	// 不依赖 service 的存储封装。
	run := func(ctx context.Context, fn func(Repository) error) error { return fn(r) }
	err := boundedTransaction(run, context.Background(), func(rr Repository) error {
		var e error
		out, e = clientShares(rr, scope, Filter{Start: start, End: s.Now().Add(time.Nanosecond), AsOf: s.Now()})
		return e
	})
	if err != nil {
		t.Fatalf("分享意图核验应在读预算内完成: %v", err)
	}
	if len(out) != sessions {
		t.Fatalf("预热分享应有意图背书并全部计入: got=%d want=%d", len(out), sessions)
	}
}

// 类型下推必须落在 SQL 谓词里，否则 MySQL 会先把整个会话的事件传回 Go 内存，
// 读预算照样被打满（内存兜底路径只过滤匹配行，SQL 路径靠 WHERE 收敛）。
func TestScopedQueryPushesEventTypeDownToSQL(t *testing.T) {
	query, args := buildScopedQuery("tenant", "event", RecordQuery{Types: []string{"share_intent"}}, 10)
	if !strings.Contains(query, "JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.event_type')) IN (?)") {
		t.Fatalf("类型过滤未下推到 SQL: %s", query)
	}
	// 参数顺序：谓词参数在 LIMIT 之前（前面还有 payload 守卫、租户与 kind 参数）。
	if n := len(args); n < 2 || args[n-2] != "share_intent" || args[n-1] != 10 {
		t.Fatalf("类型参数须在 LIMIT 前且值正确: %v", args)
	}
	if query, _ := buildScopedQuery("tenant", "event", RecordQuery{}, 10); strings.Contains(query, "event_type") {
		t.Fatal("无类型过滤时不应生成 JSON 谓词")
	}
}
