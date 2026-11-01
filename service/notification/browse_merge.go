package notification

import "time"

// 浏览提醒合并窗口（三期增补 B16）：同一收件人 + 同一分享 + 同一提醒类型在窗口内
// 只保留一条未读提醒并只外发一次，防止高频分享者被"一次浏览一条通知"轰炸。
// 窗口为 0 时完全保持原有行为；已读/已取消的通知不参与合并，读过的提醒重新开始。

// browseMergeTarget 判定事件是否进入合并维度。
func browseMergeTarget(event Event, window time.Duration) (recipientID int64, kind, shareID string, at time.Time, ok bool) {
	if window <= 0 {
		return 0, "", "", time.Time{}, false
	}
	switch event.Kind {
	case "browse", "collection_directory_browse", "collection_content_browse":
		if event.RecipientID > 0 && event.ShareID != "" && !event.OccurredAt.IsZero() {
			return event.RecipientID, event.Kind, event.ShareID, event.OccurredAt, true
		}
	}
	return 0, "", "", time.Time{}, false
}

// findMergeableBrowseNotice 在已有通知里找窗口内可合并的未读浏览提醒（最新优先）。
// SQLStore 与测试 fake 共用此维度定义，保证测试镜像生产行为。
func findMergeableBrowseNotice(notices []Notification, recipientID int64, kind, shareID string, at time.Time, window time.Duration) (string, bool) {
	threshold := at.Add(-window)
	var bestID string
	var bestAt time.Time
	for _, notice := range notices {
		if notice.RecipientID != recipientID || notice.Kind != kind || notice.ShareID != shareID {
			continue
		}
		if notice.ReadAt != nil || notice.CancelledAt != nil {
			continue
		}
		if notice.CreatedAt.Before(threshold) || notice.CreatedAt.After(at) {
			continue
		}
		if notice.CreatedAt.After(bestAt) {
			bestID, bestAt = notice.ID, notice.CreatedAt
		}
	}
	return bestID, bestID != ""
}
