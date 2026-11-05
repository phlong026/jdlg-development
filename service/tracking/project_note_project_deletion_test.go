package tracking

import (
	"context"
	"errors"
	"testing"
	"time"
)

func requireProjectDeletionStatus(t *testing.T, err error, status int) {
	t.Helper()
	var target *Error
	if !errors.As(err, &target) || target.Status != status {
		t.Fatalf("status = %#v, want %d", err, status)
	}
}

func TestProjectNoteProjectDeleteRequiresCreatorScopeAndExactRevision(t *testing.T) {
	ctx := context.Background()
	s, designer, other := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteDeleteProject(ctx, other, project.ID, project.Revision); err == nil {
		t.Fatal("同归属其他设计师删除了项目")
	}
	noManage := designer
	noManage.Manage = false
	if _, err = s.ProjectNoteDeleteProject(ctx, noManage, project.ID, project.Revision); err == nil {
		t.Fatal("无管理权限的创建者删除了项目")
	}
	if _, err = s.ProjectNoteDeleteProject(ctx, designer, project.ID, project.Revision+1); err == nil {
		t.Fatal("旧修订号删除了项目")
	} else {
		requireProjectDeletionStatus(t, err, 409)
	}
}

func TestProjectNoteProjectDeletePersistsTombstoneAndBlocksAllActiveEntrypoints(t *testing.T) {
	ctx := context.Background()
	s, designer, project, version, _ := projectPackageFixture(t)
	share, err := s.ProjectNoteCreateShare(ctx, designer, version.ID, s.Now().Add(time.Hour), "project-delete-share")
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, s.Now().Add(time.Hour), "project-delete-invite")
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{ProjectID: project.ID, VersionIDs: []string{version.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "project-delete-package"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ProjectPackagePreview(ctx, designer, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := s.ProjectPackagePublish(ctx, designer, ProjectPackagePublishInput{PackageID: draft.ID, ExpectedRevision: draft.Revision, PreviewSignature: preview.Signature, ExpiresAt: s.Now().Add(time.Hour), IdempotencyKey: "project-delete-publish"})
	if err != nil {
		t.Fatal(err)
	}
	project, err = s.ProjectNoteGetProject(ctx, designer, project.ID)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := s.ProjectNoteDeleteProject(ctx, designer, project.ID, project.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ProjectID != project.ID || deleted.Revision != project.Revision || deleted.DeletedBy != designer.Actor || deleted.DeletedAt.IsZero() {
		t.Fatalf("unexpected deletion: %#v", deleted)
	}
	page, err := s.ProjectNoteListProjects(ctx, designer, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.ID == project.ID {
			t.Fatal("已删除项目仍在列表")
		}
	}
	if _, err = s.ProjectNoteGetProjectDetail(ctx, designer, project.ID); err == nil {
		t.Fatal("已删除项目详情仍可读")
	}
	if _, err = s.ProjectNoteReadShare(ctx, share.Token, 101); err == nil {
		t.Fatal("已删除项目的旧分享仍可读")
	}
	if _, err = s.ProjectPackageRead(ctx, delivery.Token, 101); err == nil {
		t.Fatal("已删除项目的资料包仍可读")
	}
	if _, err = s.ProjectNoteReadInvitation(ctx, invitation.Token, 101); err == nil {
		t.Fatal("已删除项目的邀请仍可读")
	}
	if _, err = s.ProjectNoteApplyInvitation(ctx, invitation.Token, 101, "project-delete-apply"); err == nil {
		t.Fatal("已删除项目的邀请仍可申请")
	}
	if _, err = s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{Title: "复活", Body: "不允许", BodyFormat: "plain"}); err == nil {
		t.Fatal("已删除项目仍可创建笔记")
	}
	if _, err = s.ProjectNoteDeleteProject(ctx, designer, project.ID, project.Revision); err == nil {
		t.Fatal("项目被重复删除")
	}
	if err = s.readTransaction(ctx, func(r Repository) error {
		if _, getErr := r.Get(projectNoteProjectKind, project.ID); getErr != nil {
			t.Fatalf("删除不应物理移除项目: %v", getErr)
		}
		audit, found, getErr := projectNoteLoadProjectDeletion(r, project.ID)
		if getErr != nil || !found || audit.ProjectID != project.ID {
			t.Fatalf("项目删除墓碑不可读: %#v found=%v err=%v", audit, found, getErr)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
