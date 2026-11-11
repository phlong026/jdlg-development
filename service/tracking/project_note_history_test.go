package tracking

import (
	"context"
	"testing"
	"time"
)

func TestProjectNoteHistoryPagesStableBoundedFactsAndSummarizesVersions(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, note, firstVersion, share := seedProjectNote(t, s, designer, 101)

	var feedbackIDs []string
	for index, text := range []string{"门厅请调整", "厨房请调整", "卧室请调整"} {
		feedback, err := s.ProjectNoteAddFeedback(ctx, share.Token, 101, text, []string{"history-feedback-1", "history-feedback-2", "history-feedback-3"}[index])
		if err != nil {
			t.Fatal(err)
		}
		feedbackIDs = append(feedbackIDs, feedback.ID)
	}
	if _, err := s.ProjectNoteResolveFeedback(ctx, designer, feedbackIDs[0], "已调整", "history-resolution-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.ProjectNoteSetConfirmer(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectNoteConfirmShare(ctx, share.Token, 101, "history-confirmation-1"); err != nil {
		t.Fatal(err)
	}
	updated, err := s.ProjectNoteSaveNote(ctx, designer, note.ID, ProjectNoteDraft{Title: "二版", Body: "当前方案", BodyFormat: "plain"}, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	secondVersion, err := s.ProjectNoteFreezeVersion(ctx, designer, note.ID, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.ProjectNoteHistory(ctx, designer, project.ID, ProjectNoteHistoryFeedbacks, "", 2, "")
	if err != nil || len(first.Feedbacks) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %#v err=%v", first, err)
	}
	second, err := s.ProjectNoteHistory(ctx, designer, project.ID, ProjectNoteHistoryFeedbacks, "", 2, first.NextCursor)
	if err != nil || len(second.Feedbacks) != 1 || second.NextCursor != "" {
		t.Fatalf("second page = %#v err=%v", second, err)
	}
	seen := map[string]bool{}
	for _, feedback := range append(first.Feedbacks, second.Feedbacks...) {
		if seen[feedback.ID] {
			t.Fatalf("feedback repeated across pages: %s", feedback.ID)
		}
		seen[feedback.ID] = true
	}
	if len(seen) != len(feedbackIDs) {
		t.Fatalf("paged feedback ids = %#v", seen)
	}
	if _, err = s.ProjectNoteHistory(ctx, designer, project.ID, ProjectNoteHistoryConfirmations, "", 2, first.NextCursor); err == nil {
		t.Fatal("cursor was reusable across history types")
	}
	if _, err = s.ProjectNoteHistory(ctx, designer, project.ID, ProjectNoteHistoryFeedbacks, secondVersion.ID, 2, first.NextCursor); err == nil {
		t.Fatal("cursor was reusable with another version filter")
	}
	otherProject, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "游标边界项目", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteHistory(ctx, designer, otherProject.ID, ProjectNoteHistoryFeedbacks, "", 2, first.NextCursor); err == nil {
		t.Fatal("cursor was reusable across projects")
	}

	versions, err := s.ProjectNoteHistory(ctx, designer, project.ID, ProjectNoteHistoryVersions, "", 50, "")
	if err != nil || len(versions.Versions) != 2 {
		t.Fatalf("versions = %#v err=%v", versions, err)
	}
	for _, version := range versions.Versions {
		if version.ID != firstVersion.ID && version.ID != secondVersion.ID {
			t.Fatalf("unexpected version summary: %#v", version)
		}
	}
	notes, err := s.ProjectNoteHistory(ctx, designer, project.ID, ProjectNoteHistoryNotes, "", 50, "")
	if err != nil || len(notes.Notes) != 1 || notes.Notes[0].ID != note.ID {
		t.Fatalf("notes = %#v err=%v", notes, err)
	}
	resolutions, err := s.ProjectNoteHistory(ctx, designer, project.ID, ProjectNoteHistoryResolutions, firstVersion.ID, 50, "")
	if err != nil || len(resolutions.Resolutions) != 1 || resolutions.Resolutions[0].FeedbackID != feedbackIDs[0] {
		t.Fatalf("resolutions = %#v err=%v", resolutions, err)
	}
	confirmations, err := s.ProjectNoteHistory(ctx, designer, project.ID, ProjectNoteHistoryConfirmations, firstVersion.ID, 50, "")
	if err != nil || len(confirmations.Confirmations) != 1 || confirmations.Confirmations[0].VersionID != firstVersion.ID {
		t.Fatalf("confirmations = %#v err=%v", confirmations, err)
	}

	exact, err := s.ProjectNoteGetFeedback(ctx, designer, project.ID, feedbackIDs[0])
	if err != nil || exact.ID != feedbackIDs[0] || exact.Text == "" {
		t.Fatalf("exact feedback = %#v err=%v", exact, err)
	}
}

func TestProjectNoteHistoryLimitIsAlwaysBounded(t *testing.T) {
	if got := projectNoteHistoryLimit(0); got != 30 {
		t.Fatalf("default limit = %d", got)
	}
	if got := projectNoteHistoryLimit(51); got != 50 {
		t.Fatalf("maximum limit = %d", got)
	}
}

func TestProjectNoteSharedHistoryStaysOnInvitedVersionAndRechecksPermission(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, note, firstVersion, firstShare := seedProjectNote(t, s, designer, 101)
	for index, text := range []string{"首版反馈一", "首版反馈二"} {
		if _, err := s.ProjectNoteAddFeedback(ctx, firstShare.Token, 101, text, []string{"shared-history-1", "shared-history-2"}[index]); err != nil {
			t.Fatal(err)
		}
	}
	updated, err := s.ProjectNoteSaveNote(ctx, designer, note.ID, ProjectNoteDraft{Title: "二版", Body: "二版正文", BodyFormat: "plain"}, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	secondVersion, err := s.ProjectNoteFreezeVersion(ctx, designer, note.ID, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}
	secondShare, err := s.ProjectNoteCreateShare(ctx, designer, secondVersion.ID, s.Now().Add(time.Hour), "shared-history-share-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteAddFeedback(ctx, secondShare.Token, 101, "二版反馈", "shared-history-3"); err != nil {
		t.Fatal(err)
	}

	page, err := s.ProjectNoteSharedHistory(ctx, firstShare.Token, 101, ProjectNoteHistoryFeedbacks, 1, "")
	if err != nil || len(page.Feedbacks) != 1 || page.Feedbacks[0].VersionID != firstVersion.ID || page.NextCursor == "" {
		t.Fatalf("shared page = %#v err=%v", page, err)
	}
	// 分享链接即访问凭据：成员被移除后历史仍可读；撤销分享后才失效。
	if err = s.ProjectNoteRevokeMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteSharedHistory(ctx, firstShare.Token, 101, ProjectNoteHistoryFeedbacks, 1, page.NextCursor); err != nil {
		t.Fatalf("valid share history rejected after membership revoke: %v", err)
	}
	if err = s.ProjectNoteRevokeShare(ctx, designer, firstShare.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteSharedHistory(ctx, firstShare.Token, 101, ProjectNoteHistoryFeedbacks, 1, page.NextCursor); err == nil {
		t.Fatal("shared history cursor survived revoked share")
	}
}

func TestProjectNoteGetNoteRequiresSameProjectAndActiveDraft(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, note, _, _ := seedProjectNote(t, s, designer, 101)

	got, err := s.ProjectNoteGetNote(ctx, designer, project.ID, note.ID)
	if err != nil || got.ID != note.ID || got.Revision != note.Revision || got.Body == "" {
		t.Fatalf("active note = %#v err=%v", got, err)
	}
	otherProject, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "李宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteGetNote(ctx, designer, otherProject.ID, note.ID); err == nil {
		t.Fatal("note was readable through another managed project")
	}
	if _, err = s.ProjectNoteDelete(ctx, designer, note.ID, note.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteGetNote(ctx, designer, project.ID, note.ID); err == nil {
		t.Fatal("deleted note remained available as an active draft")
	}
}
