package notification

import (
	"testing"
	"time"
)

func TestDeliveryPreferencesSkipExternalButKeepInbox(t *testing.T) {
	base := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	store := newFakeDurableStore(browseEventFor("event-1", 42, "share-1", base))
	prefs := defaultPreferences(42)
	prefs.BrowseDelivery = false
	store.prefs[42] = prefs

	runConsumer(t, store, base.Add(time.Second))

	if len(store.notices) != 1 {
		t.Fatalf("关闭外发分类仍必须在收件箱保留提醒，got %d", len(store.notices))
	}
	if len(store.deliveries) != 0 {
		t.Fatalf("已关闭的浏览类外发不得创建投递，got %d", len(store.deliveries))
	}

	// 重新开启后恢复外发
	prefs.BrowseDelivery = true
	store.prefs[42] = prefs
	store.events["event-2"] = browseEventFor("event-2", 42, "share-2", base.Add(time.Minute))
	runConsumer(t, store, base.Add(2*time.Minute))
	if len(store.deliveries) != 1 {
		t.Fatalf("重新开启后应恢复外发投递，got %d", len(store.deliveries))
	}
}

func TestProjectCategoryOptOutOnlyAffectsProjectKinds(t *testing.T) {
	base := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	store := newFakeDurableStore(browseEventFor("event-1", 42, "share-1", base))
	prefs := defaultPreferences(42)
	prefs.ProjectDelivery = false
	store.prefs[42] = prefs

	runConsumer(t, store, base.Add(time.Second))
	if len(store.deliveries) != 1 {
		t.Fatalf("关闭项目类不得影响浏览类外发，got %d", len(store.deliveries))
	}
	if got := deliveryCategory("project_note_feedback"); got != "project" {
		t.Fatalf("项目反馈应归入 project 类别，got %q", got)
	}
	if got := deliveryCategory("unknown_kind"); got != "" {
		t.Fatalf("未知类别不可关闭，got %q", got)
	}
}
