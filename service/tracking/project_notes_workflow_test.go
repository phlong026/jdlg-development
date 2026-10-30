package tracking

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProjectNoteProjectWorkflowKeepsStructuredMetadataAndFrozenSnapshot(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{
		Name:         "张宅方案",
		Owner:        "owner-a",
		Location:     "杭州市余杭区",
		BuildingArea: 328.5,
		Floors:       3,
		Style:        "现代中式",
	})
	if err != nil {
		t.Fatal(err)
	}
	if project.Status != ProjectNoteProjectStatusInitial || project.Location != "杭州市余杭区" || project.BuildingArea != 328.5 || project.Floors != 3 || project.Style != "现代中式" {
		t.Fatalf("created project = %#v", project)
	}

	updated, err := s.ProjectNoteUpdateProject(ctx, designer, project.ID, ProjectNoteProjectUpdate{
		ExpectedRevision: project.Revision,
		Location:         stringPointer("杭州市临平区"),
		Status:           stringPointer(ProjectNoteProjectStatusRevising),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != project.Revision+1 || updated.Status != ProjectNoteProjectStatusRevising || updated.Location != "杭州市临平区" {
		t.Fatalf("updated project = %#v", updated)
	}
	if _, err = s.ProjectNoteUpdateProject(ctx, designer, project.ID, ProjectNoteProjectUpdate{ExpectedRevision: project.Revision, Style: stringPointer("侘寂")}); err == nil {
		t.Fatal("stale project revision overwrote metadata")
	}
	if _, err = s.ProjectNoteUpdateProject(ctx, designer, project.ID, ProjectNoteProjectUpdate{ExpectedRevision: updated.Revision, Status: stringPointer(ProjectNoteProjectStatusConfirmed)}); err == nil {
		t.Fatal("designer forged confirmed business status")
	}

	note, err := s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{Title: "首版", Body: "固定方案", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.ProjectNoteFreezeVersion(ctx, designer, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if version.ProjectSnapshot.Location != updated.Location || version.ProjectSnapshot.BuildingArea != updated.BuildingArea || version.ProjectSnapshot.Revision != updated.Revision {
		t.Fatalf("version snapshot = %#v", version.ProjectSnapshot)
	}
	currentProject, err := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteUpdateProject(ctx, designer, project.ID, ProjectNoteProjectUpdate{ExpectedRevision: currentProject.Revision, Location: stringPointer("杭州市西湖区")}); err != nil {
		t.Fatal(err)
	}
	frozen, err := s.ProjectNoteGetVersion(ctx, designer, project.ID, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.ProjectSnapshot.Location != "杭州市临平区" {
		t.Fatalf("frozen project metadata changed: %#v", frozen.ProjectSnapshot)
	}
}

func TestProjectNoteSatisfactionAndOwnerConfirmationDriveStatus(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, _, share := seedProjectNote(t, s, designer, 101)
	if err := s.ProjectNoteSetConfirmer(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	feedback, err := s.ProjectNoteAddFeedbackWithSatisfaction(ctx, share.Token, 101, "门厅需要调整", ProjectNoteSatisfactionNeedsAdjustment, "feedback-satisfaction-1")
	if err != nil || feedback.Satisfaction != ProjectNoteSatisfactionNeedsAdjustment {
		t.Fatalf("feedback = %#v err=%v", feedback, err)
	}
	revising, err := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if err != nil || revising.Status != ProjectNoteProjectStatusRevising {
		t.Fatalf("project after feedback = %#v err=%v", revising, err)
	}
	legacy, err := s.ProjectNoteAddFeedback(ctx, share.Token, 101, "旧版文字反馈", "feedback-legacy-1")
	if err != nil || legacy.Satisfaction != "" {
		t.Fatalf("legacy text feedback stopped working: %v", err)
	}
	for _, input := range []struct {
		satisfaction string
		key          string
	}{
		{ProjectNoteSatisfactionVerySatisfied, "feedback-very-satisfied-1"},
		{ProjectNoteSatisfactionUnsatisfied, "feedback-unsatisfied-1"},
	} {
		got, addErr := s.ProjectNoteAddFeedbackWithSatisfaction(ctx, share.Token, 101, "补充反馈", input.satisfaction, input.key)
		if addErr != nil || got.Satisfaction != input.satisfaction {
			t.Fatalf("satisfaction %q = %#v err=%v", input.satisfaction, got, addErr)
		}
	}
	if _, err = s.ProjectNoteAddFeedbackWithSatisfaction(ctx, share.Token, 101, "非法满意度", "happy", "feedback-invalid-1"); err == nil {
		t.Fatal("invalid satisfaction accepted")
	}
	if _, err = s.ProjectNoteConfirmShare(ctx, share.Token, 101, "confirm-status-1"); err != nil {
		t.Fatal(err)
	}
	confirmed, err := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if err != nil || confirmed.Status != ProjectNoteProjectStatusConfirmed {
		t.Fatalf("project after owner confirmation = %#v err=%v", confirmed, err)
	}
}

func TestProjectNoteFeedbackNoticeIsAtomicDeduplicatedAndDesignerBound(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, version, share := seedProjectNote(t, s, designer, 101)
	calls := 0
	s.OnNoticeEvent = func(_ context.Context, notice NoticeEvent) error {
		calls++
		if notice.Kind != ProjectNoteFeedbackNoticeKind || notice.RecipientID != designer.Actor || notice.ProjectID != project.ID || notice.ContentID != version.ID || notice.ShareID != share.ID || notice.VisitorKey != "u:101" {
			t.Fatalf("notice = %#v", notice)
		}
		return errors.New("notification outbox unavailable")
	}
	if _, err := s.ProjectNoteAddFeedbackWithSatisfaction(ctx, share.Token, 101, "门厅需要调整", ProjectNoteSatisfactionNeedsAdjustment, "feedback-notice-1"); err == nil {
		t.Fatal("feedback committed while notice projection failed")
	}
	for _, row := range s.Store.(*memoryStore).data[s.Config.Tenant] {
		if row.Kind == projectNoteFeedbackKind || row.Kind == "notice_event" {
			t.Fatalf("partial feedback notification survived rollback: %#v", row)
		}
	}
	s.OnNoticeEvent = func(_ context.Context, notice NoticeEvent) error {
		calls++
		return nil
	}
	feedback, err := s.ProjectNoteAddFeedbackWithSatisfaction(ctx, share.Token, 101, "门厅需要调整", ProjectNoteSatisfactionNeedsAdjustment, "feedback-notice-1")
	if err != nil || calls != 2 {
		t.Fatalf("feedback=%#v calls=%d err=%v", feedback, calls, err)
	}
	if _, err = s.ProjectNoteAddFeedbackWithSatisfaction(ctx, share.Token, 101, "门厅需要调整", ProjectNoteSatisfactionNeedsAdjustment, "feedback-notice-1"); err != nil || calls != 2 {
		t.Fatalf("duplicate feedback projected twice: calls=%d err=%v", calls, err)
	}
	noticeID := projectNoteFeedbackNoticeID(feedback.ID)
	if ok, err := s.ProjectNoteNoticeEligible(ctx, noticeID, designer.Actor, project.Owner); err != nil || !ok {
		t.Fatalf("designer eligibility=%v err=%v", ok, err)
	}
	if ok, err := s.ProjectNoteNoticeEligible(ctx, noticeID, 999, project.Owner); err != nil || ok {
		t.Fatalf("wrong recipient eligibility=%v err=%v", ok, err)
	}
}

func TestProjectNoteOnlyCurrentUnchangedVersionDrivesBusinessStatus(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, note, v1, share1 := seedProjectNote(t, s, designer, 101)
	if err := s.ProjectNoteSetConfirmer(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	updatedNote, err := s.ProjectNoteSaveNote(ctx, designer, note.ID, ProjectNoteDraft{Title: "二版", Body: "当前方案", BodyFormat: "plain"}, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.ProjectNoteFreezeVersion(ctx, designer, note.ID, updatedNote.Revision)
	if err != nil {
		t.Fatal(err)
	}
	share2, err := s.ProjectNoteCreateShare(ctx, designer, v2.ID, s.Now().Add(time.Hour), "share-current-status-2")
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if err != nil || current.CurrentVersionID != v2.ID || current.Status != ProjectNoteProjectStatusRevising {
		t.Fatalf("current project = %#v err=%v", current, err)
	}
	oldConfirmation, err := s.ProjectNoteConfirmShare(ctx, share1.Token, 101, "confirm-old-status-1")
	if err != nil || oldConfirmation.VersionID != v1.ID {
		t.Fatalf("old confirmation = %#v err=%v", oldConfirmation, err)
	}
	afterOldConfirmation, _ := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if afterOldConfirmation.Status != ProjectNoteProjectStatusRevising {
		t.Fatalf("old version confirmed current project: %#v", afterOldConfirmation)
	}
	if _, err = s.ProjectNoteConfirmShare(ctx, share2.Token, 101, "confirm-current-status-2"); err != nil {
		t.Fatal(err)
	}
	confirmed, _ := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if confirmed.Status != ProjectNoteProjectStatusConfirmed {
		t.Fatalf("current version did not confirm project: %#v", confirmed)
	}
	if _, err = s.ProjectNoteAddFeedbackWithSatisfaction(ctx, share1.Token, 101, "旧版仍需调整", ProjectNoteSatisfactionNeedsAdjustment, "feedback-old-status-1"); err != nil {
		t.Fatal(err)
	}
	afterOldFeedback, _ := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if afterOldFeedback.Status != ProjectNoteProjectStatusConfirmed {
		t.Fatalf("old feedback revised current project: %#v", afterOldFeedback)
	}
	thirdRevision, err := s.ProjectNoteSaveNote(ctx, designer, note.ID, ProjectNoteDraft{Title: "三版草稿", Body: "尚未冻结", BodyFormat: "plain"}, updatedNote.Revision)
	if err != nil || thirdRevision.Revision != updatedNote.Revision+1 {
		t.Fatalf("third revision = %#v err=%v", thirdRevision, err)
	}
	revising, _ := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if revising.Status != ProjectNoteProjectStatusRevising {
		t.Fatalf("new draft revision left project confirmed: %#v", revising)
	}
	if _, err = s.ProjectNoteConfirmShare(ctx, share2.Token, 101, "confirm-current-status-replay"); err != nil {
		t.Fatal(err)
	}
	afterStaleReplay, _ := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if afterStaleReplay.Status != ProjectNoteProjectStatusRevising {
		t.Fatalf("stale confirmed version overwrote newer draft status: %#v", afterStaleReplay)
	}
}

func stringPointer(value string) *string { return &value }
