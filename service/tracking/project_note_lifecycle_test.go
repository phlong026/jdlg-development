package tracking

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProjectNoteCopyKeepsOnlyDraftAndSameProjectMedia(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "复制方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	asset := Asset{ID: "copy-media-1", Kind: "image", URL: projectNoteMediaURL(project.ID, "copy-media-1"), Width: 1200, Height: 800, MIME: "image/jpeg"}
	operation, _, err := s.ProjectNotePrepareMedia(ctx, designer, project.ID, asset, strings.Repeat("c", 64), "upload")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteFinalizeMedia(ctx, designer, project.ID, asset.ID, operation); err != nil {
		t.Fatal(err)
	}
	source, err := s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{
		Title: "原始标题", Body: `{"schema":1,"blocks":[{"kind":"image","media_id":"copy-media-1"}]}`,
		BodyFormat: "article_v2", CoverID: asset.ID, MediaIDs: []string{asset.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	copied, err := s.ProjectNoteCopy(ctx, designer, source.ID, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if copied.ID == source.ID || copied.ProjectID != source.ProjectID || copied.Revision != 1 {
		t.Fatalf("unexpected copied identity: %#v", copied)
	}
	if copied.Title != source.Title || copied.Body != source.Body || copied.BodyFormat != source.BodyFormat || copied.CoverID != source.CoverID || len(copied.MediaIDs) != 1 || copied.MediaIDs[0] != asset.ID {
		t.Fatalf("draft was not copied exactly: %#v", copied)
	}
	if _, err = s.ProjectNoteDelete(ctx, designer, source.ID, source.Revision); err != nil {
		t.Fatal(err)
	}
	if media, err := s.ProjectNoteGetMedia(ctx, designer, project.ID, asset.ID); err != nil || media.Asset.ID != asset.ID {
		t.Fatalf("soft delete removed project media: %#v %v", media, err)
	}
}

func TestProjectNoteCopyAndDeleteRequireDesignerScopeAndExactRevision(t *testing.T) {
	ctx := context.Background()
	s, designer, other := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "权限方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{Title: "私密笔记", Body: "正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteCopy(ctx, other, note.ID, note.Revision); err == nil {
		t.Fatal("same-owner other designer copied a private note")
	}
	noManage := designer
	noManage.Manage = false
	if _, err = s.ProjectNoteCopy(ctx, noManage, note.ID, note.Revision); err == nil {
		t.Fatal("designer without manage permission copied a private note")
	}
	wrongOwner := designer
	wrongOwner.Owners = []string{"owner-b"}
	if _, err = s.ProjectNoteCopy(ctx, wrongOwner, note.ID, note.Revision); err == nil {
		t.Fatal("designer outside the owner scope copied a private note")
	}
	if _, err = s.ProjectNoteCopy(ctx, designer, note.ID, note.Revision+1); err == nil {
		t.Fatal("stale copy revision was accepted")
	}
	if _, err = s.ProjectNoteDelete(ctx, other, note.ID, note.Revision); err == nil {
		t.Fatal("same-owner other designer deleted a private note")
	}
	if _, err = s.ProjectNoteDelete(ctx, designer, note.ID, note.Revision+1); err == nil {
		t.Fatal("stale delete revision was accepted")
	}
}

func TestProjectNoteCopyRejectsForeignProjectMediaEvenIfDraftWasTampered(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	projectA, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "项目甲", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	projectB, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "项目乙", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	foreign := Asset{ID: "foreign-copy-media", Kind: "image", URL: projectNoteMediaURL(projectB.ID, "foreign-copy-media"), Width: 800, Height: 600, MIME: "image/png"}
	operation, _, err := s.ProjectNotePrepareMedia(ctx, designer, projectB.ID, foreign, strings.Repeat("d", 64), "upload")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteFinalizeMedia(ctx, designer, projectB.ID, foreign.ID, operation); err != nil {
		t.Fatal(err)
	}
	note, err := s.ProjectNoteCreateNote(ctx, designer, projectA.ID, ProjectNoteDraft{Title: "原笔记", Body: "正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	err = s.transaction(ctx, func(r Repository) error {
		note.Body = `{"schema":1,"blocks":[{"kind":"image","media_id":"foreign-copy-media"}]}`
		note.BodyFormat = "article_v2"
		note.MediaIDs = []string{foreign.ID}
		return r.Put(projectNoteRecord(projectNoteKind, note.ID, projectA.Owner, note.UpdatedAt, note, projectNoteActorKey(designer.Actor), projectA.ID, projectA.ID, ""))
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteCopy(ctx, designer, note.ID, note.Revision); err == nil {
		t.Fatal("copy accepted media belonging to another project")
	}
}

func TestProjectNoteSoftDeletePreservesFrozenHistoryFeedbackAndMedia(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, note, version, share := seedProjectNote(t, s, designer, 101)
	feedback, err := s.ProjectNoteAddFeedback(ctx, share.Token, 101, "保留这条私密反馈", "delete-feedback-key")
	if err != nil {
		t.Fatal(err)
	}
	var before Record
	if err = s.readTransaction(ctx, func(r Repository) error {
		before, err = r.Get(projectNoteKind, note.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	deleted, err := s.ProjectNoteDelete(ctx, designer, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.NoteID != note.ID || deleted.ProjectID != project.ID || deleted.Revision != note.Revision || deleted.DeletedBy != designer.Actor || deleted.DeletedAt.IsZero() {
		t.Fatalf("invalid deletion audit: %#v", deleted)
	}
	if _, err = s.ProjectNoteCopy(ctx, designer, note.ID, note.Revision); err == nil {
		t.Fatal("deleted note was copied")
	}
	if _, err = s.ProjectNoteDelete(ctx, designer, note.ID, note.Revision); err == nil {
		t.Fatal("deleted note was deleted twice")
	}
	var after Record
	if err = s.readTransaction(ctx, func(r Repository) error {
		after, err = r.Get(projectNoteKind, note.ID)
		return err
	}); err != nil {
		t.Fatal("soft delete physically removed draft", err)
	}
	if after.Kind != before.Kind || after.ID != before.ID || after.Owner != before.Owner || after.Visitor != before.Visitor || after.Content != before.Content || after.Share != before.Share || after.At != before.At || string(after.Payload) != string(before.Payload) || after.Session != "" {
		t.Fatalf("soft delete rewrote draft instead of clearing only active index: before=%#v after=%#v", before, after)
	}
	detail, err := s.ProjectNoteGetProjectDetail(ctx, designer, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Notes) != 0 || len(detail.Versions) != 1 || len(detail.Feedbacks) != 1 {
		t.Fatalf("active draft index or frozen history changed incorrectly: %#v", detail)
	}
	view, err := s.ProjectNoteReadShare(ctx, share.Token, 101)
	if err != nil || view.Version.ID != version.ID || len(view.Feedbacks) != 1 || view.Feedbacks[0].ID != feedback.ID {
		t.Fatalf("soft delete removed frozen history or feedback: %#v %v", view, err)
	}
	if err = s.readTransaction(ctx, func(r Repository) error {
		got, found, readErr := projectNoteLoadDeletion(r, note.ID)
		if readErr != nil || !found || got.NoteID != note.ID {
			t.Fatalf("deletion audit unreadable: %#v found=%v err=%v", got, found, readErr)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectNoteCopyDoesNotCreateMembersVersionsSharesOrFeedback(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, note, _, share := seedProjectNote(t, s, designer, 101)
	if _, err := s.ProjectNoteAddFeedback(ctx, share.Token, 101, "仅属于原版本", "copy-private-feedback"); err != nil {
		t.Fatal(err)
	}
	before, err := s.ProjectNoteGetProjectDetail(ctx, designer, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteCopy(ctx, designer, note.ID, note.Revision); err != nil {
		t.Fatal(err)
	}
	after, err := s.ProjectNoteGetProjectDetail(ctx, designer, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Members) != len(before.Members) || len(after.Versions) != len(before.Versions) || len(after.Shares) != len(before.Shares) || len(after.Feedbacks) != len(before.Feedbacks) || len(after.Confirmations) != len(before.Confirmations) {
		t.Fatalf("copy duplicated private lifecycle records: before=%#v after=%#v", before, after)
	}
	if len(after.Notes) != len(before.Notes)+1 {
		t.Fatalf("copy should add exactly one draft: before=%d after=%d", len(before.Notes), len(after.Notes))
	}
}

func TestProjectNoteDeletionTimestampUsesServiceClock(t *testing.T) {
	s, designer, _ := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(context.Background(), designer, ProjectNoteProjectInput{Name: "时间方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := s.ProjectNoteCreateNote(context.Background(), designer, project.ID, ProjectNoteDraft{Title: "待删", Body: "正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC)
	s.Now = func() time.Time { return want }
	deleted, err := s.ProjectNoteDelete(context.Background(), designer, note.ID, note.Revision)
	if err != nil || !deleted.DeletedAt.Equal(want) {
		t.Fatalf("deleted_at=%s err=%v", deleted.DeletedAt, err)
	}
}

func TestProjectNoteSoftDeleteBlocksSaveAndFreeze(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "删除后不可写", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{Title: "待删除", Body: "原正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteDelete(ctx, designer, note.ID, note.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteSaveNote(ctx, designer, note.ID, ProjectNoteDraft{Title: "复活", Body: "不得写回", BodyFormat: "plain"}, note.Revision); err == nil {
		t.Fatal("soft-deleted note was saved and restored to the active index")
	}
	if _, err = s.ProjectNoteFreezeVersion(ctx, designer, note.ID, note.Revision); err == nil {
		t.Fatal("soft-deleted note created a new frozen version")
	}
}
