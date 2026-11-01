package notification

import (
	"context"
	"testing"
	"time"
)

func browseEventFor(id string, recipient int64, shareID string, at time.Time) Event {
	return Event{ID: id, Kind: "browse", RecipientID: recipient, ShareID: shareID, Owner: "owner-a", Route: "pages/contentreader/contentreader", OccurredAt: at}
}

func runConsumer(t *testing.T, store *fakeDurableStore, now time.Time) {
	t.Helper()
	consumer, err := NewConsumer(store, ConsumerConfig{
		PollInterval: time.Second, LeaseDuration: time.Minute, MaxAttempts: 3, Channel: "official_account",
		Eligibility: func(context.Context, Event) (bool, error) { return true, nil },
		Binding:     func(context.Context, int64) (int64, bool, error) { return 3, true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	consumer.now = func() time.Time { return now }
	if _, err := consumer.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBrowseNoticesMergeWithinWindowAndReadResetsIt(t *testing.T) {
	base := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	// fake 的 ClaimEvent 按 map 顺序领取，必须按事件时间分批投喂保证时序。
	store := newFakeDurableStore(browseEventFor("event-1", 42, "share-1", base))
	store.browseMergeWindow = 30 * time.Minute

	runConsumer(t, store, base.Add(time.Second))
	store.events["event-2"] = browseEventFor("event-2", 42, "share-1", base.Add(5*time.Minute))
	runConsumer(t, store, base.Add(6*time.Minute))

	if len(store.notices) != 1 {
		t.Fatalf("窗口内两次浏览应合并为一条提醒，got %d", len(store.notices))
	}
	if len(store.deliveries) != 1 {
		t.Fatalf("窗口内只应外发一次，got %d", len(store.deliveries))
	}
	for _, notice := range store.notices {
		if notice.EventID != "event-2" {
			t.Fatalf("合并后应指向最新事件，got %s", notice.EventID)
		}
		if !notice.CreatedAt.Equal(base.Add(5 * time.Minute)) {
			t.Fatalf("合并后时间应刷新为最新事件时间，got %s", notice.CreatedAt)
		}
	}

	// 读取后窗口重置：新浏览产生新提醒
	readAt := base.Add(7 * time.Minute)
	for id, notice := range store.notices {
		notice.ReadAt = &readAt
		store.notices[id] = notice
	}
	store.events["event-3"] = browseEventFor("event-3", 42, "share-1", base.Add(8*time.Minute))
	runConsumer(t, store, base.Add(9*time.Minute))
	if len(store.notices) != 2 {
		t.Fatalf("已读后新浏览应产生新提醒，got %d", len(store.notices))
	}
}

func TestBrowseNoticesOutsideWindowOrDifferentShareStaySeparate(t *testing.T) {
	base := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	store := newFakeDurableStore(browseEventFor("event-1", 42, "share-1", base))
	store.browseMergeWindow = 30 * time.Minute
	runConsumer(t, store, base.Add(time.Minute))
	store.events["event-2"] = browseEventFor("event-2", 42, "share-1", base.Add(40*time.Minute)) // 超出 30m 窗口
	runConsumer(t, store, base.Add(41*time.Minute))
	store.events["event-3"] = browseEventFor("event-3", 42, "share-2", base.Add(41*time.Minute)) // 不同分享
	runConsumer(t, store, base.Add(42*time.Minute))
	store.events["event-4"] = browseEventFor("event-4", 43, "share-1", base.Add(42*time.Minute)) // 不同收件人
	runConsumer(t, store, base.Add(43*time.Minute))
	if len(store.notices) != 4 {
		t.Fatalf("窗口外/不同分享/不同收件人不得合并，got %d", len(store.notices))
	}
}

func TestZeroWindowKeepsLegacyOneNoticePerEvent(t *testing.T) {
	base := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	store := newFakeDurableStore(
		browseEventFor("event-1", 42, "share-1", base),
		browseEventFor("event-2", 42, "share-1", base.Add(time.Minute)),
	)
	// browseMergeWindow 保持零值：行为必须与历史版本完全一致
	runConsumer(t, store, base.Add(time.Second))
	runConsumer(t, store, base.Add(2*time.Minute))
	if len(store.notices) != 2 {
		t.Fatalf("窗口禁用时一事件一条通知，got %d", len(store.notices))
	}
}

func TestNonBrowseKindsNeverMerge(t *testing.T) {
	base := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	feedback := func(id string, at time.Time) Event {
		return Event{ID: id, Kind: "project_note_feedback", RecipientID: 42, ShareID: "share-1", OccurredAt: at}
	}
	store := newFakeDurableStore(feedback("event-1", base), feedback("event-2", base.Add(time.Minute)))
	store.browseMergeWindow = 30 * time.Minute
	runConsumer(t, store, base.Add(time.Second))
	runConsumer(t, store, base.Add(2*time.Minute))
	if len(store.notices) != 2 {
		t.Fatalf("非浏览类提醒不参与合并，got %d", len(store.notices))
	}
}
