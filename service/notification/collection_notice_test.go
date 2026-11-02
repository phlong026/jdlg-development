package notification

import (
	"strings"
	"testing"
	"time"
)

func TestCollectionBrowseNoticesValidateAndUseDistinctCopy(t *testing.T) {
	now := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	directory := Event{
		ID: "collection-directory-event", Kind: "collection_directory_browse", Owner: "owner-a", RecipientID: 42,
		VisitorKey: "u:101", ShareID: "collection-share", ContentID: "collection-content", CollectionID: "collection-content",
		CollectionVersion: 2, Route: "pages/collectionreader/collectionreader", OccurredAt: now,
	}
	content := Event{
		ID: "collection-content-event", Kind: "collection_content_browse", Owner: "owner-a", RecipientID: 42,
		VisitorKey: "u:101", ShareID: "collection-share", ContentID: "fixed-content", CollectionID: "collection-content",
		CollectionVersion: 2, Route: "pages/contentreader/contentreader", OccurredAt: now,
	}
	if !validEvent(directory) || !validEvent(content) {
		t.Fatalf("valid collection events rejected: directory=%v content=%v", validEvent(directory), validEvent(content))
	}
	for _, invalid := range []Event{
		{ID: directory.ID, Kind: directory.Kind, Owner: directory.Owner, RecipientID: directory.RecipientID, ShareID: directory.ShareID, ContentID: directory.ContentID, Route: directory.Route, OccurredAt: now},
		func() Event { value := content; value.CollectionVersion = 0; return value }(),
		func() Event { value := directory; value.Route = "pages/contentreader/contentreader"; return value }(),
	} {
		if validEvent(invalid) {
			t.Fatalf("invalid collection event accepted: %+v", invalid)
		}
	}

	directoryNotice := noticeFromEvent(directory)
	contentNotice := noticeFromEvent(content)
	if directoryNotice.Body != "有人打开了你分享的项目合集" || contentNotice.Body != "有人查看了合集中的作品" {
		t.Fatalf("copy directory=%q content=%q", directoryNotice.Body, contentNotice.Body)
	}
	for _, notice := range []Notification{directoryNotice, contentNotice} {
		if !strings.HasPrefix(notice.Route, "pages/collectionreader/collectionreader?") || !strings.Contains(notice.Route, "collection_id=collection-content") || !strings.Contains(notice.Route, "collection_version=2") {
			t.Fatalf("collection notice route=%q", notice.Route)
		}
	}
}
