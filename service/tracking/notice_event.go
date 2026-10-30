package tracking

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrNoticeRecipient = errors.New("notice recipient is not a verified mini program user")

// NoticeRules are opt-in. A zero value never creates browse notices.
type NoticeRules struct {
	BrowseEnabled bool
}

// NoticeEvent is the small immutable handoff from tracking to notification.
// It contains only the fields needed to build an inbox item and delivery row.
type NoticeEvent struct {
	ID          string    `json:"event_id"`
	Kind        string    `json:"kind"`
	Owner       string    `json:"owner_id"`
	RecipientID int64     `json:"recipient_id"`
	VisitorKey  string    `json:"visitor_key"`
	ShareID     string    `json:"share_id"`
	ContentID   string    `json:"content_id,omitempty"`
	ProjectID   string    `json:"project_id,omitempty"`
	Route       string    `json:"route_key"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// NoticeRecipient deliberately has no fallback to Owner or any display name.
func NoticeRecipient(share Share) (int64, error) {
	if share.Origin != "miniprogram" || !strings.HasPrefix(share.Sharer, "u:") {
		return 0, ErrNoticeRecipient
	}
	rawID := strings.TrimPrefix(share.Sharer, "u:")
	for _, char := range rawID {
		if char < '0' || char > '9' {
			return 0, ErrNoticeRecipient
		}
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrNoticeRecipient
	}
	return id, nil
}

func BuildBrowseNotice(entry Entry, event Event, share Share, rules NoticeRules) (NoticeEvent, bool, error) {
	if !rules.BrowseEnabled || event.Type != "page_view" || entry.Attribution != "share" || entry.Share == "" || entry.Self || entry.Preview {
		return NoticeEvent{}, false, nil
	}
	if share.ID != entry.Share || share.Owner != entry.Owner || share.Revoked || !event.Occurred.Before(share.Expires) || share.Origin != "miniprogram" {
		return NoticeEvent{}, false, nil
	}
	recipient, err := NoticeRecipient(share)
	if err != nil {
		return NoticeEvent{}, false, nil
	}
	if entry.Visitor == share.Sharer {
		return NoticeEvent{}, false, nil
	}
	return NoticeEvent{
		ID:          event.ID,
		Kind:        "browse",
		Owner:       entry.Owner,
		RecipientID: recipient,
		VisitorKey:  entry.Visitor,
		ShareID:     share.ID,
		ContentID:   event.Content,
		Route:       event.Route,
		OccurredAt:  event.Occurred.UTC(),
	}, true, nil
}

func browseNotice(r Repository, entry Entry, event Event, rules NoticeRules) (NoticeEvent, bool, error) {
	if !rules.BrowseEnabled || event.Type != "page_view" || entry.Attribution != "share" || entry.Share == "" || entry.Self || entry.Preview {
		return NoticeEvent{}, false, nil
	}
	rows, err := scopedRecords(r, "share", RecordQuery{Owners: []string{entry.Owner}, Shares: []string{entry.Share}})
	if err != nil {
		return NoticeEvent{}, false, err
	}
	if len(rows) != 1 {
		return NoticeEvent{}, false, ErrUnavailable
	}
	var share Share
	if err := json.Unmarshal(rows[0].Payload, &share); err != nil {
		return NoticeEvent{}, false, ErrUnavailable
	}
	return BuildBrowseNotice(entry, event, share, rules)
}
