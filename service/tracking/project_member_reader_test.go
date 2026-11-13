package tracking

import (
	"context"
	"testing"
	"time"
)

func TestMemberDirectoryReadsFrozenVersionsWithoutDraftOrManagementAccess(t *testing.T) {
	ctx := context.Background()
	s, designer, other := projectNoteFixture(t)
	project, note, version, _ := seedProjectNote(t, s, designer, 101)
	directory, err := s.ProjectNoteMemberDirectory(ctx, 101, project.ID, 10, "")
	if err != nil || len(directory.Versions) != 1 || directory.Versions[0].ID != version.ID {
		t.Fatalf("member directory: %+v %v", directory, err)
	}
	view, err := s.ProjectNoteMemberVersion(ctx, 101, project.ID, version.ID)
	if err != nil || view.Version.Body != note.Body {
		t.Fatalf("member version: %+v %v", view, err)
	}
	if _, err = s.ProjectNoteGetProjectDetail(ctx, other, project.ID); err == nil {
		t.Fatal("member reader must not broaden designer management")
	}
	if _, err = s.ProjectNoteMemberDirectory(ctx, 102, project.ID, 10, ""); err == nil {
		t.Fatal("nonmember read directory")
	}
	if err = s.ProjectNoteRevokeMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteMemberVersion(ctx, 101, project.ID, version.ID); err == nil {
		t.Fatal("revoked member read version")
	}
}

func TestMemberReaderDoesNotExposeInternalFrozenVersionsAndRechecksShares(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, shared, share := seedProjectNote(t, s, designer, 101)
	note, err := s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{Title: "内部冻结稿", Body: "未对客户共享", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	internal, err := s.ProjectNoteFreezeVersion(ctx, designer, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := s.ProjectNoteMemberDirectory(ctx, 101, project.ID, 30, "")
	if err != nil || len(directory.Versions) != 1 || directory.Versions[0].ID != shared.ID {
		t.Fatalf("internal version leaked: %+v %v", directory, err)
	}
	if _, err = s.ProjectNoteMemberVersion(ctx, 101, project.ID, internal.ID); err == nil {
		t.Fatal("member read an unshared frozen draft")
	}
	if _, err = s.ProjectNoteMemberVersion(ctx, 101, project.ID, shared.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ProjectNoteRevokeShare(ctx, designer, share.ID); err != nil {
		t.Fatal(err)
	}
	directory, err = s.ProjectNoteMemberDirectory(ctx, 101, project.ID, 30, "")
	if err != nil || len(directory.Versions) != 0 {
		t.Fatalf("withdrawn version stayed in member directory: %+v %v", directory, err)
	}
	if _, err = s.ProjectNoteMemberVersion(ctx, 101, project.ID, shared.ID); err == nil {
		t.Fatal("active membership bypassed revoked share")
	}
}

func TestMemberReaderRespectsPackageRecipientsAndDeliveryRevocation(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, version, share := seedProjectNote(t, s, designer, 101)
	if _, err := s.ProjectNoteInviteMember(ctx, designer, project.ID, 102); err != nil {
		t.Fatal(err)
	}
	if err := s.ProjectNoteRevokeShare(ctx, designer, share.ID); err != nil {
		t.Fatal(err)
	}
	draft, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{ProjectID: project.ID, VersionIDs: []string{version.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "member-package-draft"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ProjectPackagePreview(ctx, designer, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := s.ProjectPackagePublish(ctx, designer, ProjectPackagePublishInput{PackageID: draft.ID, ExpectedRevision: draft.Revision, PreviewSignature: preview.Signature, ExpiresAt: s.Now().Add(time.Hour), IdempotencyKey: "member-package-publish"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteMemberVersion(ctx, 101, project.ID, version.ID); err != nil {
		t.Fatal(err)
	}
	directory, err := s.ProjectNoteMemberDirectory(ctx, 102, project.ID, 30, "")
	if err != nil || len(directory.Versions) != 0 {
		t.Fatalf("nonrecipient saw package version: %+v %v", directory, err)
	}
	if _, err = s.ProjectNoteMemberVersion(ctx, 102, project.ID, version.ID); err == nil {
		t.Fatal("nonrecipient member bypassed package whitelist")
	}
	if err = s.ProjectPackageRevoke(ctx, designer, delivery.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteMemberVersion(ctx, 101, project.ID, version.ID); err == nil {
		t.Fatal("revoked delivery remained readable through member route")
	}
}

func TestMemberReaderRejectsExpiredShares(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, version, _ := seedProjectNote(t, s, designer, 101)
	now := s.Now()
	s.Now = func() time.Time { return now.Add(2 * time.Hour) }
	directory, err := s.ProjectNoteMemberDirectory(ctx, 101, project.ID, 30, "")
	if err != nil || len(directory.Versions) != 0 {
		t.Fatalf("expired share remained listed: %+v %v", directory, err)
	}
	if _, err = s.ProjectNoteMemberVersion(ctx, 101, project.ID, version.ID); err == nil {
		t.Fatal("expired share remained readable through member route")
	}
}
