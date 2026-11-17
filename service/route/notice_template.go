package route

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
	"unicode"

	"service/notification"
	"service/officialaccount"
	"service/tracking"
)

type browsePresentationResolver func(context.Context, int64, string) (tracking.BrowseNoticePresentation, bool, error)

func browseNoticeKind(kind string) bool {
	switch kind {
	case "browse", "collection_directory_browse", "collection_content_browse":
		return true
	default:
		return false
	}
}

func visitorTemplateMode(fields map[string]string) bool {
	for _, source := range fields {
		if source == "visitor" {
			return true
		}
	}
	return false
}

func singleLineNoticeValue(value string, limit int, fallback string) string {
	value = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return ' '
		}
		return char
	}, strings.ToValidUTF8(value, " "))
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		value = fallback
	}
	chars := []rune(value)
	if len(chars) > limit {
		value = string(chars[:limit])
	}
	return value
}

func noticeEventPresentation(fields map[string]string, resolve browsePresentationResolver) notification.EventPresentationResolver {
	visitorMode := visitorTemplateMode(fields)
	return func(ctx context.Context, event notification.Event) (notification.EventPresentation, bool, error) {
		presentation := notification.EventPresentation{DeliveryAllowed: !visitorMode || browseNoticeKind(event.Kind)}
		if !visitorMode || !browseNoticeKind(event.Kind) {
			return presentation, true, nil
		}
		if resolve == nil {
			return presentation, false, notification.ErrUnavailable
		}
		details, eligible, err := resolve(ctx, event.RecipientID, event.ID)
		if err != nil {
			log.Printf("[notification] warn operation=browse_presentation event_id=%q error_type=%T", event.ID, err)
			return presentation, false, err
		}
		if !eligible {
			return presentation, false, nil
		}
		visitor := singleLineNoticeValue(details.VisitorName, 64, "访客")
		content := singleLineNoticeValue(details.ContentTitle, 200, "分享内容")
		presentation.Body = fmt.Sprintf("%s浏览了你分享的《%s》", visitor, content)
		return presentation, true, nil
	}
}

func noticeTemplateData(fields map[string]string, notice notification.Notification, visitor string) (map[string]string, error) {
	values := map[string]string{
		"title":   notice.Title,
		"body":    notice.Body,
		"time":    notice.CreatedAt.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04"),
		"visitor": singleLineNoticeValue(visitor, 20, "访客"),
	}
	if len(fields) == 0 {
		return nil, notification.ErrInvalidInput
	}
	data := make(map[string]string, len(fields))
	for key, source := range fields {
		value, ok := values[source]
		if !ok {
			return nil, notification.ErrInvalidInput
		}
		chars := []rune(value)
		if len(chars) > 200 {
			value = string(chars[:200])
		}
		data[key] = value
	}
	return data, nil
}

func noticeTemplateSend(sender officialaccount.DeliverySender, fields map[string]string, resolve browsePresentationResolver) notification.DeliverySend {
	visitorMode := visitorTemplateMode(fields)
	return func(ctx context.Context, job notification.DeliveryJob) notification.DeliveryResult {
		visitor := ""
		if visitorMode {
			if !browseNoticeKind(job.Notice.Kind) || job.Notice.EventID == "" || job.Delivery.RecipientID <= 0 {
				return notification.DeliveryResult{Status: notification.DeliveryCancelled, Reason: "ineligible"}
			}
			if resolve == nil {
				return notification.DeliveryResult{Status: notification.DeliveryFailed, Reason: "presentation_unavailable", Retryable: true}
			}
			details, eligible, err := resolve(ctx, job.Delivery.RecipientID, job.Notice.EventID)
			if err != nil {
				log.Printf("[notification] warn operation=browse_template event_id=%q error_type=%T", job.Notice.EventID, err)
				return notification.DeliveryResult{Status: notification.DeliveryFailed, Reason: "presentation_unavailable", Retryable: true}
			}
			if !eligible {
				return notification.DeliveryResult{Status: notification.DeliveryCancelled, Reason: "ineligible"}
			}
			visitor = details.VisitorName
		}
		data, err := noticeTemplateData(fields, job.Notice, visitor)
		if err != nil {
			return notification.DeliveryResult{Status: notification.DeliveryFailed, Reason: "template_mapping_failed"}
		}
		mapper := func(context.Context, notification.DeliveryJob) (officialaccount.SendRequest, error) {
			return officialaccount.SendRequest{DetailQuery: url.Values{"notification_id": {job.Notice.ID}}.Encode(), Data: data}, nil
		}
		return officialaccount.DeliveryAdapter(sender, mapper)(ctx, job)
	}
}
