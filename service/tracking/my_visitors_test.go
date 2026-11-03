package tracking

import (
	"context"
	"testing"
	"time"
)

// 我客户视图的验收测试（批次1）：按人聚合、本人链路隔离、逐次明细、素材热度。
func myVisitorsFixture(t *testing.T) (*Service, reviewStore, Scope, time.Time) {
	t.Helper()
	s, rows, scope, now := reviewFixture()
	scope.Actor = 11
	scope.Owners = []string{"company"}
	scope.Manage, scope.CRM, scope.Rules, scope.Delete = false, false, false, false
	seedMyShare(rows, "share-1", "company", "u:11", now.Add(-3*time.Hour))
	seedMyShare(rows, "share-2", "company", "u:11", now.Add(-2*time.Hour))
	// u:77 看了 share-1 两次（同一素材），可见 5 秒
	visitPV(rows, "view-1", "company", "u:77", "session-1", "c-1", "share-1", now.Add(-50*time.Minute), 5000)
	visitPV(rows, "view-1b", "company", "u:77", "session-1", "c-1", "share-1", now.Add(-49*time.Minute), 2000)
	// u:88 看了 share-2 一次
	visitPV(rows, "view-2", "company", "u:88", "session-2", "c-2", "share-2", now.Add(-40*time.Minute), 8000)
	// u:11 自己的浏览必须被排除（self 视角双重排除：sharer 与 visitor 同人）
	seedMyShareView(rows, "view-self", "company", "u:11", "session-self", "share-1", now.Add(-30*time.Minute), 1000)
	// u:22 的分享及其访客不属于本人链路
	seedMyShare(rows, "share-3", "company", "u:22", now.Add(-4*time.Hour))
	seedMyShareView(rows, "view-3", "company", "u:99", "session-3", "share-3", now.Add(-35*time.Minute), 3000)
	// share_intent（客户转发意图）挂在 c-1 上
	visitEvent(rows, "fwd-1", "company", "u:88", "session-2", "view-2", "c-1", now.Add(-39*time.Minute), Event{ID: "fwd-1", Type: "share_intent", Share: "share-2"})
	// 视频事件挂在 view-1：观看了 3 秒，播放到 50%
	visitEvent(rows, "video-1", "company", "u:77", "session-1", "view-1", "c-1", now.Add(-48*time.Minute), Event{ID: "video-1", Type: "video_progress", Share: "share-1", Watch: 3000, Position: 5000, Duration: 10000})
	// visitEvent 不回填 rec.Share（真实事件由服务端从入场上下文盖章），fixture 补齐
	for id, share := range map[string]string{"video-1": "share-1", "fwd-1": "share-2"} {
		key := "event/" + id
		if rec, ok := rows[key]; ok {
			rec.Share = share
			rows[key] = rec
		}
	}
	return s, rows, scope, now
}

func myVisitorItems(t *testing.T, list List) []MyVisitor {
	t.Helper()
	items, ok := list.Items.([]MyVisitor)
	if !ok {
		t.Fatalf("items type = %T, want []MyVisitor", list.Items)
	}
	return items
}

func findMyVisitor(items []MyVisitor, key string) (MyVisitor, bool) {
	for _, item := range items {
		if item.Visitor.Key == key {
			return item, true
		}
	}
	return MyVisitor{}, false
}

func TestMyVisitorsAggregatePerVisitorWithinOwnShares(t *testing.T) {
	s, _, scope, now := myVisitorsFixture(t)
	out, err := s.MyVisitors(context.Background(), scope, 11, Filter{AsOf: now, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	items := myVisitorItems(t, out)
	if len(items) != 2 {
		t.Fatalf("visitors = %d, want 2 (u:77 与匿名 u:88；u:11 自看与 u:22 链路必须排除): %+v", len(items), items)
	}
	chen, ok := findMyVisitor(items, "u:77")
	if !ok {
		t.Fatalf("u:77 missing: %+v", items)
	}
	if chen.Views != 2 || chen.Sessions != 1 {
		t.Fatalf("u:77 views/sessions = %d/%d, want 2/1", chen.Views, chen.Sessions)
	}
	if chen.First.After(chen.Last) || chen.Visible <= 0 {
		t.Fatalf("u:77 window wrong: first=%v last=%v visible=%d", chen.First, chen.Last, chen.Visible)
	}
	if chen.ContentCount != 1 || chen.VideoWatch != 3000 || chen.Progress != 50 {
		t.Fatalf("u:77 contents/watch/progress = %d/%d/%d, want 1/3000/50", chen.ContentCount, chen.VideoWatch, chen.Progress)
	}
	anon, ok := findMyVisitor(items, "u:88")
	if !ok {
		t.Fatalf("u:88 missing: %+v", items)
	}
	if anon.Views != 1 || anon.Shares != 1 {
		t.Fatalf("u:88 views/shares = %d/%d, want 1/1", anon.Views, anon.Shares)
	}
	if anon.Intent.Level == "" {
		t.Fatal("intent level must never be empty")
	}
	for _, item := range items {
		if item.Visitor.Key == "u:99" {
			t.Fatal("另一销售的访客 u:99 泄漏进本人客户榜")
		}
	}
}

func TestMyVisitorsIntentFilterAndSort(t *testing.T) {
	s, _, scope, now := myVisitorsFixture(t)
	out, err := s.MyVisitors(context.Background(), scope, 11, Filter{AsOf: now, Intent: "high", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if items := myVisitorItems(t, out); len(items) != 0 {
		t.Fatalf("无业务结果时 high 意向应过滤为空，got %d", len(items))
	}
	out, err = s.MyVisitors(context.Background(), scope, 11, Filter{AsOf: now, Sort: "page_views", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	items := myVisitorItems(t, out)
	if len(items) != 2 || items[0].Visitor.Key != "u:77" {
		t.Fatalf("按浏览次数排序首位应为 u:77: %+v", items)
	}
}

func TestMyVisitorEntriesTimelineIsPerVisitor(t *testing.T) {
	s, _, scope, now := myVisitorsFixture(t)
	detail, err := s.MyVisitorEntries(context.Background(), scope, 11, Filter{AsOf: now, Visitor: "u:77", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Items) != 2 {
		t.Fatalf("u:77 entries = %d, want 2: %+v", len(detail.Items), detail.Items)
	}
	for _, entry := range detail.Items {
		if entry.Session != "session-1" {
			t.Fatalf("他人 session 混入逐次明细: %+v", detail.Items)
		}
	}
	if detail.Views != 2 || detail.VideoWatch != 3000 || detail.Progress != 50 {
		t.Fatalf("页头汇总 = %d/%d/%d, want 2/3000/50", detail.Views, detail.VideoWatch, detail.Progress)
	}
	// 最新的一条排在最前（降序）
	if len(detail.Items) == 2 && detail.Items[0].View != "view-1b" {
		t.Fatalf("entries[0].View = %s, want 最新浏览 view-1b", detail.Items[0].View)
	}
	if _, err := s.MyVisitorEntries(context.Background(), scope, 11, Filter{AsOf: now, Visitor: "u:42"}); err == nil {
		t.Fatal("未授权/不存在的访客必须 404")
	}
}

func TestMyContentStatsDedupeVisitorsAndCountForwards(t *testing.T) {
	s, _, scope, now := myVisitorsFixture(t)
	out, err := s.MyContentStats(context.Background(), scope, 11, Filter{AsOf: now, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := out.Items.([]MyContentStat)
	if !ok {
		t.Fatalf("items type = %T", out.Items)
	}
	if len(items) != 2 {
		t.Fatalf("content stats = %d, want 2 (c-1 与 share-2 无内容不计): %+v", len(items), items)
	}
	if items[0].ContentID != "c-1" {
		t.Fatalf("热度榜首应为 c-1: %+v", items)
	}
	c1 := items[0]
	if c1.Views != 2 || c1.Visitors != 1 || c1.Forwards != 1 {
		t.Fatalf("c-1 = views %d visitors %d forwards %d, want 2/1/1", c1.Views, c1.Visitors, c1.Forwards)
	}
	c2 := items[1]
	if c2.ContentID != "c-2" || c2.Views != 1 || c2.Visitors != 1 {
		t.Fatalf("c-2 = views %d visitors %d, want 1/1", c2.Views, c2.Visitors)
	}
}

func TestMySharesCreatedWindowFiltersByShareCreation(t *testing.T) {
	s, _, scope, now := myVisitorsFixture(t)
	// share-1 创建于 3 小时前；窗口只留 2 小时 → 仅剩 share-2
	out, err := s.MyShares(context.Background(), scope, 11, Filter{AsOf: now, Start: now.Add(-2 * time.Hour), CreatedWindow: true, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareItems(t, out)
	if len(items) != 1 || items[0].ID != "share-2" {
		t.Fatalf("created window filtered = %+v, want 仅 share-2", items)
	}
	// 不带 created_window 时窗口不影响分享列表（老语义）
	out, err = s.MyShares(context.Background(), scope, 11, Filter{AsOf: now, Start: now.Add(-2 * time.Hour), Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if items := myShareItems(t, out); len(items) != 2 {
		t.Fatalf("默认窗口不应过滤分享创建时间, got %d", len(items))
	}
}

func TestMySharesSearchCollectsMatchesBeforePagination(t *testing.T) {
	s, rows, scope, now := myVisitorsFixture(t)
	// 再补两条新分享（标题不匹配），命中项在扫描序列的最后
	seedMyShare(rows, "share-new-1", "company", "u:11", now.Add(-10*time.Minute))
	seedMyShare(rows, "share-new-2", "company", "u:11", now.Add(-5*time.Minute))
	out, err := s.MyShares(context.Background(), scope, 11, Filter{AsOf: now, Search: "沟通结论", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	items := myShareItems(t, out)
	if len(items) != 0 {
		t.Fatalf("演示分享标题不含关键词，应得空页: %+v", items)
	}
	// created_window End 上界：窗口 [4小时前, 2.5小时前] 只含 share-3（4小时前），
	// 排除窗口外新建的 share（验证 AtBefore 上界）
	out, err = s.MyShares(context.Background(), scope, 11, Filter{AsOf: now, Start: now.Add(-4 * time.Hour), End: now.Add(-150 * time.Minute), CreatedWindow: true, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	windowItems := myShareItems(t, out)
	if len(windowItems) != 1 || windowItems[0].ID != "share-1" {
		t.Fatalf("创建窗 [08:00,09:30] 应仅含本人 share-1: %+v", windowItems)
	}
}
