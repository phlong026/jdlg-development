package route

import (
	"testing"

	"service/notification"
)

func TestBrowseTemplateMappingRequiresExplicitVerifiedFields(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"thing1":"guessed"}`, `{"bad.field":"title"}`, `{"thing1":"title","thing1":"body"}`, `{"thing1":"title"} {}`} {
		if _, err := parseNoticeTemplateFields(raw); err == nil {
			t.Fatalf("accepted unsafe/missing mapping %q", raw)
		}
	}
	fields, err := parseNoticeTemplateFields(`{"thing1":"title","time2":"time"}`)
	if err != nil || fields["thing1"] != "title" {
		t.Fatalf("valid mapping %v %v", fields, err)
	}
}

func TestDeliveryEligibilityKeepsProjectNoteKind(t *testing.T) {
	event := deliveryNoticeEvent(notification.DeliveryJob{
		Notice:   notification.Notification{EventID: "project-open-event-1", Kind: "project_note_open"},
		Delivery: notification.Delivery{RecipientID: 42},
	})
	if event.ID != "project-open-event-1" || event.Kind != "project_note_open" || event.RecipientID != 42 {
		t.Fatalf("event = %#v", event)
	}
}
