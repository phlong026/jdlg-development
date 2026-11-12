package tracking

import (
	"context"
	"errors"
	"testing"
	"time"
)

func projectInvitationStatus(t *testing.T, err error, status int) {
	t.Helper()
	var target *Error
	if !errors.As(err, &target) || target.Status != status {
		t.Fatalf("error = %v, want status %d", err, status)
	}
}

func TestProjectInvitationRequiresDesignerApprovalBeforeMembership(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, s.Now().Add(time.Hour), "invite-create-key-1")
	if err != nil || issued.Token == "" {
		t.Fatalf("issued = %#v err=%v", issued, err)
	}
	preview, err := s.ProjectNoteReadInvitation(ctx, issued.Token, 101)
	if err != nil || preview.ProjectName != project.Name || preview.ProjectID != "" {
		t.Fatalf("preview leaked or failed: %#v err=%v", preview, err)
	}
	request, err := s.ProjectNoteApplyInvitation(ctx, issued.Token, 101, "invite-apply-key-1")
	if err != nil || request.Status != ProjectNoteJoinApproved {
		t.Fatalf("request = %#v err=%v", request, err)
	}
	if err = s.transaction(ctx, func(r Repository) error {
		_, loadErr := projectNoteLoadMember(r, project.ID, 101)
		return loadErr
	}); err != nil {
		t.Fatal("invitation apply did not create membership:", err)
	}
}

func TestProjectInvitationApprovedPreviewRequiresCurrentMembership(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, s.Now().Add(time.Hour), "invite-membership-create")
	if err != nil {
		t.Fatal(err)
	}
	if preview, readErr := s.ProjectNoteReadInvitation(ctx, issued.Token, 102); readErr != nil || preview.Status != "" {
		t.Fatalf("valid invitation without request = %#v err=%v", preview, readErr)
	}
	request, err := s.ProjectNoteApplyInvitation(ctx, issued.Token, 101, "invite-membership-apply")
	if err != nil {
		t.Fatal(err)
	}
	if preview, readErr := s.ProjectNoteReadInvitation(ctx, issued.Token, 101); readErr != nil || preview.Status != ProjectNoteJoinApproved {
		t.Fatalf("approved invitation preview = %#v err=%v", preview, readErr)
	}
	_ = request
	if err = s.ProjectNoteRevokeMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	_, err = s.ProjectNoteReadInvitation(ctx, issued.Token, 101)
	projectInvitationStatus(t, err, 403)

	reissued, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, s.Now().Add(time.Hour), "invite-membership-reissue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteApplyInvitation(ctx, reissued.Token, 101, "invite-membership-reapply"); err != nil {
		t.Fatal("a new invitation must allow a removed member to apply again:", err)
	}
}

func TestProjectInvitationExpiryRevocationAndDesignerScopeFailClosed(t *testing.T) {
	ctx := context.Background()
	s, designer, other := projectNoteFixture(t)
	now := s.Now()
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, now.Add(time.Hour), "invite-create-key-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteListJoinRequests(ctx, other, project.ID); err == nil {
		t.Fatal("another designer must not list join requests")
	}
	if err = s.ProjectNoteRevokeInvitation(ctx, other, project.ID, issued.ID); err == nil {
		t.Fatal("another designer must not revoke invitation")
	}
	if err = s.ProjectNoteRevokeInvitation(ctx, designer, project.ID, issued.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.ProjectNoteApplyInvitation(ctx, issued.Token, 101, "invite-revoked-apply")
	projectInvitationStatus(t, err, 404)

	expiring, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, now.Add(30*time.Minute), "invite-create-key-3")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteApplyInvitation(ctx, expiring.Token, 102, "invite-expiring-apply"); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return now.Add(31 * time.Minute) }
	_, err = s.ProjectNoteReadInvitation(ctx, expiring.Token, 101)
	projectInvitationStatus(t, err, 404)
	_, err = s.ProjectNoteApplyInvitation(ctx, expiring.Token, 101, "invite-expired-apply")
	projectInvitationStatus(t, err, 404)
}

func TestProjectInvitationPreviewAndApplyRequireCurrentDesignerGrant(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, s.Now().Add(time.Hour), "invite-revoked-grant")
	if err != nil {
		t.Fatal(err)
	}
	s.IsContentOperator = func(context.Context, int64, string) (bool, error) { return false, nil }
	if _, err = s.ProjectNoteReadInvitation(ctx, issued.Token, 101); err == nil {
		t.Fatal("revoked designer grant must hide invitation preview")
	}
	if _, err = s.ProjectNoteApplyInvitation(ctx, issued.Token, 101, "invite-revoked-grant-apply"); err == nil {
		t.Fatal("revoked designer grant must reject join request")
	}
}

func TestProjectInvitationRetriesDoNotDuplicateTokenRequestOrMembership(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	expires := s.Now().Add(time.Hour)
	first, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, expires, "invite-create-key-4")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, expires, "invite-create-key-4")
	if err != nil || replayed.Token != first.Token || !replayed.Replayed {
		t.Fatalf("replayed = %#v err=%v", replayed, err)
	}
	one, err := s.ProjectNoteApplyInvitation(ctx, first.Token, 101, "invite-apply-key-4")
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.ProjectNoteApplyInvitation(ctx, first.Token, 101, "invite-apply-key-4")
	if err != nil || two.ID != one.ID || !two.Replayed {
		t.Fatalf("application replay = %#v err=%v", two, err)
	}
	if one.Status != ProjectNoteJoinApproved {
		t.Fatalf("apply status = %q", one.Status)
	}
	detail, err := s.ProjectNoteGetProjectDetail(ctx, designer, project.ID)
	if err != nil || len(detail.Members) != 1 || detail.Members[0].ActorID != 101 {
		t.Fatalf("members = %#v err=%v", detail.Members, err)
	}
}

func TestProjectInvitationListReturnsOnlyActiveRevocableMetadata(t *testing.T) {
	ctx := context.Background()
	s, designer, other := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	active, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, s.Now().Add(time.Hour), "invite-list-active")
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, s.Now().Add(time.Hour), "invite-list-revoked")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProjectNoteRevokeInvitation(ctx, designer, project.ID, revoked.ID); err != nil {
		t.Fatal(err)
	}
	items, err := s.ProjectNoteListInvitations(ctx, designer, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != active.ID || items[0].Status != ProjectNoteInvitationActive || items[0].ExpiresAt != active.ExpiresAt {
		t.Fatalf("active invitation summaries = %#v", items)
	}
	if _, err = s.ProjectNoteListInvitations(ctx, other, project.ID); err == nil {
		t.Fatal("another designer must not list project invitations")
	}
	s.Now = func() time.Time { return active.ExpiresAt.Add(time.Second) }
	items, err = s.ProjectNoteListInvitations(ctx, designer, project.ID)
	if err != nil || len(items) != 0 {
		t.Fatalf("expired invitation summaries = %#v err=%v", items, err)
	}
}

func TestProjectInvitationApplicationCreatesDesignerNoticeOnly(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	s.Config.Enabled = true
	var notice NoticeEvent
	s.OnNoticeEvent = func(_ context.Context, value NoticeEvent) error { notice = value; return nil }
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, s.Now().Add(time.Hour), "invite-create-key-5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteApplyInvitation(ctx, issued.Token, 101, "invite-apply-key-5"); err != nil {
		t.Fatal(err)
	}
	if notice.Kind != "" {
		t.Fatalf("auto-approved invitation must not queue an approval notice: %#v", notice)
	}
}

func TestProjectInvitationRecordsJoinerAttribution(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := s.ProjectNoteCreateInvitation(ctx, designer, project.ID, s.Now().Add(time.Hour), "invite-attribution-create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteApplyInvitation(ctx, issued.Token, 101, "invite-attribution-apply"); err != nil {
		t.Fatal(err)
	}
	detail, err := s.ProjectNoteGetProjectDetail(ctx, designer, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Members) != 1 {
		t.Fatalf("members = %#v", detail.Members)
	}
	member := detail.Members[0]
	if member.ActorID != 101 || member.InviterID != designer.Actor || member.InvitationID != issued.ID || member.JoinedAt.IsZero() {
		t.Fatalf("attribution = %#v (inviter=%d invitation=%s)", member, designer.Actor, issued.ID)
	}
}
