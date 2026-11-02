package notification

import (
	"strings"
	"testing"
	"time"
)

func TestProjectJoinRequestNoticeTargetsDesignerMemberReview(t *testing.T) {
	event := Event{
		ID: "project-join:" + strings.Repeat("r", 64), Kind: "project_note_join_request",
		Owner: "owner-a", RecipientID: 7, VisitorKey: "u:101", ShareID: strings.Repeat("i", 32),
		ContentID: strings.Repeat("r", 64), ProjectID: strings.Repeat("p", 32), Route: "project_note_share",
		OccurredAt: time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC),
	}
	if !validEvent(event) {
		t.Fatal("project join request notice rejected")
	}
	notice := noticeFromEvent(event)
	if notice.Title != "项目有新的加入申请" || notice.Body != "有用户申请加入项目，请审核后授权" {
		t.Fatalf("notice copy = %q / %q", notice.Title, notice.Body)
	}
	if !strings.Contains(notice.Route, "project_id=") || !strings.Contains(notice.Route, "view=members") {
		t.Fatalf("notice route = %q", notice.Route)
	}
	if allowsExternalDelivery(event) {
		t.Fatal("join request stays in the authenticated inbox until an explicit channel is approved")
	}
}
