package tracking

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func projectNoteFixture(t *testing.T) (*Service, Scope, Scope) {
	t.Helper()
	s, designer := fixture(t)
	s.IsContentOperator = func(_ context.Context, actor int64, owner string) (bool, error) {
		return actor == designer.Actor && owner == "owner-a", nil
	}
	otherDesigner := designer
	otherDesigner.Actor = 8
	return s, designer, otherDesigner
}

func seedProjectNote(t *testing.T, s *Service, designer Scope, memberID int64) (ProjectNoteProject, ProjectNote, ProjectNoteVersion, ProjectNoteShare) {
	t.Helper()
	ctx := context.Background()
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteInviteMember(ctx, designer, project.ID, memberID); err != nil {
		t.Fatal(err)
	}
	note, err := s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{Title: "首版", Body: `{"schema":1,"blocks":[{"kind":"paragraph","text":"方案一"}]}`, BodyFormat: "article_v2"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.ProjectNoteFreezeVersion(ctx, designer, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.ProjectNoteCreateShare(ctx, designer, version.ID, s.Now().Add(time.Hour), "share-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	return project, note, version, share
}

func TestProjectNoteSameOwnerOtherDesignerCannotRead(t *testing.T) {
	s, designer, other := projectNoteFixture(t)
	project, _, _, _ := seedProjectNote(t, s, designer, 101)
	if _, err := s.ProjectNoteGetProject(context.Background(), other, project.ID); err == nil {
		t.Fatal("same-owner designer must not read another designer's private project")
	}
}

func TestProjectNoteInvitedMemberCanReadAndFeedbackWithoutManage(t *testing.T) {
	s, designer, _ := projectNoteFixture(t)
	_, _, _, share := seedProjectNote(t, s, designer, 101)
	view, err := s.ProjectNoteReadShare(context.Background(), share.Token, 101)
	if err != nil || view.Version.Body == "" {
		t.Fatalf("read share: %#v %v", view, err)
	}
	feedback, err := s.ProjectNoteAddFeedback(context.Background(), share.Token, 101, "门厅请再优化", "feedback-key-1")
	if err != nil || feedback.ActorID != 101 {
		t.Fatalf("add feedback: %#v %v", feedback, err)
	}
}

func TestProjectNoteForwardedTokenDoesNotGrantBodyOrMedia(t *testing.T) {
	s, designer, _ := projectNoteFixture(t)
	_, _, _, share := seedProjectNote(t, s, designer, 101)
	if _, err := s.ProjectNoteReadShare(context.Background(), share.Token, 102); err == nil {
		t.Fatal("forwarded token must not expose body")
	}
	if _, err := s.ProjectNoteReadShareMedia(context.Background(), share.Token, 102, "asset-0001"); err == nil {
		t.Fatal("forwarded token must not expose media")
	}
}

func TestProjectNoteSharePinsVersionAndFrozenVersionIsImmutable(t *testing.T) {
	s, designer, _ := projectNoteFixture(t)
	_, note, v1, share := seedProjectNote(t, s, designer, 101)
	updated, err := s.ProjectNoteSaveNote(context.Background(), designer, note.ID, ProjectNoteDraft{Title: "二版", Body: `{"schema":1,"blocks":[{"kind":"paragraph","text":"方案二"}]}`, BodyFormat: "article_v2"}, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.ProjectNoteFreezeVersion(context.Background(), designer, updated.ID, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.ProjectNoteReadShare(context.Background(), share.Token, 101)
	if err != nil || view.Version.ID != v1.ID || view.Version.ID == v2.ID || view.Version.Body != v1.Body {
		t.Fatalf("share moved versions: %#v %v", view, err)
	}
	if _, err = s.ProjectNoteSaveNote(context.Background(), designer, note.ID, ProjectNoteDraft{Title: "冲突", Body: v1.Body, BodyFormat: "article_v2"}, note.Revision); err == nil {
		t.Fatal("stale revision overwrote note")
	}
	if got, err := s.ProjectNoteReadShare(context.Background(), share.Token, 101); err != nil || got.Version.Body != v1.Body {
		t.Fatal("frozen version changed after draft update")
	}
}

func TestProjectNoteMemberAndShareRevocationTakeEffectImmediately(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, _, share := seedProjectNote(t, s, designer, 101)
	if err := s.ProjectNoteRevokeMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectNoteReadShare(ctx, share.Token, 101); err == nil {
		t.Fatal("revoked member retained access")
	}
	if _, err := s.ProjectNoteInviteMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if err := s.ProjectNoteRevokeShare(ctx, designer, share.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectNoteReadShare(ctx, share.Token, 101); err == nil {
		t.Fatal("revoked share retained access")
	}
}

func TestProjectNoteFeedbackResolutionRequiresOwningDesigner(t *testing.T) {
	ctx := context.Background()
	s, designer, other := projectNoteFixture(t)
	_, _, _, share := seedProjectNote(t, s, designer, 101)
	feedback, err := s.ProjectNoteAddFeedback(ctx, share.Token, 101, "需要修改", "feedback-key-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteResolveFeedback(ctx, other, feedback.ID, "已调整", "resolve-key-1"); err == nil {
		t.Fatal("other designer resolved private feedback")
	}
	if _, err = s.ProjectNoteResolveFeedback(ctx, designer, feedback.ID, "已调整", "resolve-key-1"); err != nil {
		t.Fatal(err)
	}
}

func TestProjectNoteConfirmationIsDesignatedVersionBoundAndIdempotent(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, version, share := seedProjectNote(t, s, designer, 101)
	if _, err := s.ProjectNoteInviteMember(ctx, designer, project.ID, 102); err != nil {
		t.Fatal(err)
	}
	if err := s.ProjectNoteSetConfirmer(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectNoteConfirmShare(ctx, share.Token, 102, "confirm-key-1"); err == nil {
		t.Fatal("non-designated member confirmed")
	}
	first, err := s.ProjectNoteConfirmShare(ctx, share.Token, 101, "confirm-key-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ProjectNoteConfirmShare(ctx, share.Token, 101, "confirm-key-2")
	if err != nil || first.ID != second.ID || second.VersionID != version.ID {
		t.Fatalf("confirmation not idempotent/version-bound: %#v %#v %v", first, second, err)
	}
	if err = s.ProjectNoteRevokeMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteConfirmShare(ctx, share.Token, 101, "confirm-key-3"); err == nil {
		t.Fatal("inactive designated member confirmed")
	}
}

func TestProjectNoteConfirmationStaysUniqueAfterConfirmerChanges(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, note, _, share1 := seedProjectNote(t, s, designer, 101)
	if _, err := s.ProjectNoteInviteMember(ctx, designer, project.ID, 102); err != nil {
		t.Fatal(err)
	}
	if err := s.ProjectNoteSetConfirmer(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	first, err := s.ProjectNoteConfirmShare(ctx, share1.Token, 101, "confirm-a-v1")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProjectNoteSetConfirmer(ctx, designer, project.ID, 102); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteConfirmShare(ctx, share1.Token, 102, "confirm-b-v1"); err == nil {
		t.Fatal("changed confirmer created a second confirmation for v1")
	}
	view1, err := s.ProjectNoteReadShare(ctx, share1.Token, 102)
	if err != nil || view1.Confirmation == nil || view1.Confirmation.ID != first.ID {
		t.Fatalf("v1 became unreadable after confirmer change: %#v %v", view1, err)
	}
	updated, err := s.ProjectNoteSaveNote(ctx, designer, note.ID, ProjectNoteDraft{Title: "二版", Body: `{"schema":1,"blocks":[{"kind":"paragraph","text":"方案二"}]}`, BodyFormat: "article_v2"}, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.ProjectNoteFreezeVersion(ctx, designer, updated.ID, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}
	share2, err := s.ProjectNoteCreateShare(ctx, designer, v2.ID, s.Now().Add(time.Hour), "share-v2-confirm")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ProjectNoteConfirmShare(ctx, share2.Token, 102, "confirm-b-v2")
	if err != nil || second.VersionID != v2.ID {
		t.Fatalf("new confirmer could not confirm v2: %#v %v", second, err)
	}
	detail, err := s.ProjectNoteGetProjectDetail(ctx, designer, project.ID)
	if err != nil || len(detail.Confirmations) != 2 {
		t.Fatalf("confirmation rows = %d err=%v", len(detail.Confirmations), err)
	}
}

func TestProjectNoteRevokingCurrentConfirmerClearsDesignation(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, _, _ := seedProjectNote(t, s, designer, 101)
	if err := s.ProjectNoteSetConfirmer(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	before, err := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ProjectNoteRevokeMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	after, err := s.ProjectNoteGetProject(ctx, designer, project.ID)
	if err != nil || after.ConfirmerID != 0 || after.Revision != before.Revision+1 {
		t.Fatalf("confirmer not cleared atomically: before=%#v after=%#v err=%v", before, after, err)
	}
}

func TestProjectNoteOpenKeepsActualSharerAndSuppressesSelfNotice(t *testing.T) {
	s, designer, _ := projectNoteFixture(t)
	_, _, _, share := seedProjectNote(t, s, designer, designer.Actor)
	fact, err := s.ProjectNoteOpenShare(context.Background(), share.Token, designer.Actor, "open-event-0001")
	if err != nil {
		t.Fatal(err)
	}
	if fact.ActualSharerID != designer.Actor || fact.Notify {
		t.Fatalf("unexpected self-open fact: %#v", fact)
	}
}

func TestProjectNoteDesignerGrantFailureClosesMemberRead(t *testing.T) {
	s, designer, _ := projectNoteFixture(t)
	_, _, _, share := seedProjectNote(t, s, designer, 101)
	s.IsContentOperator = func(context.Context, int64, string) (bool, error) { return false, errors.New("directory unavailable") }
	if _, err := s.ProjectNoteReadShare(context.Background(), share.Token, 101); err == nil {
		t.Fatal("grant lookup failure must fail closed")
	}
}

func TestProjectNoteMediaUsesFrozenAuthorizedSnapshot(t *testing.T) {
	ctx := context.Background()
	s, designer, other := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "含图方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	asset := Asset{ID: "asset-0001", Kind: "image", URL: projectNoteMediaURL(project.ID, "asset-0001"), Width: 1200, Height: 800, MIME: "image/jpeg"}
	operation, already, err := s.ProjectNotePrepareMedia(ctx, designer, project.ID, asset, strings.Repeat("a", 64), "upload")
	if err != nil || already || operation == "" {
		t.Fatalf("prepare private media: op=%q already=%v err=%v", operation, already, err)
	}
	retryOperation, already, err := s.ProjectNotePrepareMedia(ctx, designer, project.ID, asset, strings.Repeat("a", 64), "upload")
	if err != nil || already || retryOperation != operation {
		t.Fatalf("pending retry: op=%q already=%v err=%v", retryOperation, already, err)
	}
	privateMedia, err := s.ProjectNoteFinalizeMedia(ctx, designer, project.ID, asset.ID, operation)
	if err != nil || privateMedia.Asset != asset {
		t.Fatalf("finalize private media: %#v %v", privateMedia, err)
	}
	if _, err = s.Media(ctx, &designer, asset.ID); err == nil {
		t.Fatal("private project media became readable through generic media API")
	}
	generic, err := s.ListMedia(ctx, designer)
	if err != nil || len(generic.Items.([]Asset)) != 0 {
		t.Fatalf("private media leaked into generic list: %#v %v", generic, err)
	}
	if _, err = s.ProjectNoteGetMedia(ctx, other, project.ID, asset.ID); err == nil {
		t.Fatal("same-owner designer read another designer's private media")
	}
	retryOperation, already, err = s.ProjectNotePrepareMedia(ctx, designer, project.ID, asset, strings.Repeat("a", 64), "upload")
	if err != nil || !already || retryOperation != operation {
		t.Fatalf("finalized retry: op=%q already=%v err=%v", retryOperation, already, err)
	}
	privateList, err := s.ProjectNoteListMedia(ctx, designer, project.ID, 50, "")
	if err != nil || len(privateList.Items) != 1 || privateList.Items[0].Asset != asset {
		t.Fatalf("private media list: %#v %v", privateList, err)
	}
	if _, err = s.ProjectNoteInviteMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	note, err := s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{Title: "含图首版", Body: `{"schema":1,"blocks":[{"kind":"image","media_id":"asset-0001"}]}`, BodyFormat: "article_v2", MediaIDs: []string{"asset-0001"}})
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.ProjectNoteFreezeVersion(ctx, designer, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.ProjectNoteCreateShare(ctx, designer, version.ID, s.Now().Add(time.Hour), "share-media-key")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ProjectNoteReadShareMedia(ctx, share.Token, 101, asset.ID)
	if err != nil || got != asset {
		t.Fatalf("read frozen media: %#v %v", got, err)
	}
	replayed, err := s.ProjectNoteCreateShare(ctx, designer, version.ID, s.Now().Add(time.Hour), "share-media-key")
	if err != nil || !replayed.Replayed || replayed.Token != "" || replayed.ID != share.ID {
		t.Fatalf("unsafe share replay: %#v %v", replayed, err)
	}
}

func TestProjectNoteRejectsGenericOwnerMediaAsPrivateReference(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	asset := Asset{ID: "generic-asset-1", Kind: "image", URL: "/api/v1/miniprogram/tracking/media/generic-asset-1", Width: 1200, Height: 800, MIME: "image/jpeg"}
	if err := s.AddMedia(ctx, designer, asset, "owner-a"); err != nil {
		t.Fatal(err)
	}
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "隔离媒体方案", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{Title: "不得引用", Body: `{"schema":1,"blocks":[{"kind":"image","media_id":"generic-asset-1"}]}`, BodyFormat: "article_v2", MediaIDs: []string{asset.ID}})
	if err == nil {
		t.Fatal("generic owner media was accepted as private project media")
	}
}

func TestProjectNoteDetailIsBoundedAndDoesNotReturnFrozenBodies(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, version, share := seedProjectNote(t, s, designer, 101)
	for actor := int64(200); actor < 251; actor++ {
		if _, err := s.ProjectNoteInviteMember(ctx, designer, project.ID, actor); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := s.ProjectNoteGetProjectDetail(ctx, designer, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Members) != 50 || !detail.Truncated.Members {
		t.Fatalf("unbounded members: %d %#v", len(detail.Members), detail.Truncated)
	}
	if len(detail.Versions) != 1 || detail.Versions[0].ID != version.ID || len(detail.Shares) != 1 || detail.Shares[0].ID != share.ID {
		t.Fatalf("restore payload missing or request row leaked: %#v", detail)
	}
	if detail.Shares[0].Token != "" {
		t.Fatal("detail leaked raw share token")
	}
}

func TestProjectNoteShareViewOnlyAggregatesFixedVersion(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, note, v1, share1 := seedProjectNote(t, s, designer, 101)
	if err := s.ProjectNoteSetConfirmer(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	feedback1, err := s.ProjectNoteAddFeedback(ctx, share1.Token, 101, "版本一反馈", "version-one-feedback")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteResolveFeedback(ctx, designer, feedback1.ID, "版本一已处理", "version-one-resolve"); err != nil {
		t.Fatal(err)
	}
	confirmation, err := s.ProjectNoteConfirmShare(ctx, share1.Token, 101, "version-one-confirm")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.ProjectNoteSaveNote(ctx, designer, note.ID, ProjectNoteDraft{Title: "二版", Body: `{"schema":1,"blocks":[{"kind":"paragraph","text":"方案二"}]}`, BodyFormat: "article_v2"}, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.ProjectNoteFreezeVersion(ctx, designer, updated.ID, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}
	share2, err := s.ProjectNoteCreateShare(ctx, designer, v2.ID, s.Now().Add(time.Hour), "version-two-share")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteAddFeedback(ctx, share2.Token, 101, "版本二反馈", "version-two-feedback"); err != nil {
		t.Fatal(err)
	}
	view, err := s.ProjectNoteReadShare(ctx, share1.Token, 101)
	if err != nil {
		t.Fatal(err)
	}
	if view.Version.ID != v1.ID || len(view.Feedbacks) != 1 || view.Feedbacks[0].VersionID != v1.ID || len(view.Resolutions) != 1 || view.Confirmation == nil || view.Confirmation.ID != confirmation.ID || view.CanConfirm {
		t.Fatalf("cross-version activity leaked: %#v", view)
	}
}

func TestProjectNoteOpenNoticeIsAtomicAndEligibilityRechecksMembership(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, _, share := seedProjectNote(t, s, designer, 101)
	s.NoticeRules.BrowseEnabled = true
	calls := 0
	s.OnNoticeEvent = func(_ context.Context, notice NoticeEvent) error {
		calls++
		if notice.Kind != "project_note_open" || notice.RecipientID != designer.Actor || notice.VisitorKey != "u:101" || notice.ProjectID != project.ID {
			t.Fatalf("wrong notice: %#v", notice)
		}
		return errors.New("projection unavailable")
	}
	if _, err := s.ProjectNoteOpenShare(ctx, share.Token, 101, "project-open-event"); err == nil {
		t.Fatal("projection failure did not roll back open")
	}
	rows := s.Store.(*memoryStore).data[s.Config.Tenant]
	if _, ok := rows[projectNoteOpenKind+":project-open-event"]; ok {
		t.Fatal("open committed without notice projection")
	}
	if _, ok := rows["notice_event:project-open-event"]; ok {
		t.Fatal("notice committed without projection")
	}
	s.OnNoticeEvent = func(context.Context, NoticeEvent) error { calls++; return nil }
	fact, err := s.ProjectNoteOpenShare(ctx, share.Token, 101, "project-open-event")
	if err != nil || !fact.Notify || calls != 2 {
		t.Fatalf("open notice: %#v calls=%d err=%v", fact, calls, err)
	}
	if ok, err := s.ProjectNoteNoticeEligible(ctx, fact.ID, designer.Actor, "owner-a"); err != nil || !ok {
		t.Fatalf("eligible=%v err=%v", ok, err)
	}
	if err = s.ProjectNoteRevokeMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ProjectNoteNoticeEligible(ctx, fact.ID, designer.Actor, "owner-a"); err != nil || ok {
		t.Fatalf("revoked eligible=%v err=%v", ok, err)
	}
}

func TestProjectNotePrivacyDeletionOnlyRemovesSelectedVisitorOpenFacts(t *testing.T) {
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, _, version, share := seedProjectNote(t, s, designer, 101)
	if _, err := s.ProjectNoteInviteMember(ctx, designer, project.ID, 102); err != nil {
		t.Fatal(err)
	}
	s.NoticeRules.BrowseEnabled = true
	for _, visit := range []struct {
		actor int64
		event string
	}{{101, "project-open-user-101"}, {102, "project-open-user-102"}} {
		if _, err := s.ProjectNoteOpenShare(ctx, share.Token, visit.actor, visit.event); err != nil {
			t.Fatal(err)
		}
	}
	rows := s.Store.(*memoryStore).data[s.Config.Tenant]
	for _, event := range []string{"project-open-user-101", "project-open-user-102"} {
		if rows[projectNoteOpenKind+":"+event].Session != "" || rows["notice_event:"+event].Session != "" {
			t.Fatalf("project id was persisted as a tracking session for %s", event)
		}
	}
	done, err := s.DeleteVisitorProgress(ctx, designer, "u:101", "本人请求", 0)
	if err != nil || !done {
		t.Fatalf("delete visitor: done=%v err=%v", done, err)
	}
	rows = s.Store.(*memoryStore).data[s.Config.Tenant]
	for _, kind := range []string{projectNoteOpenKind, "notice_event"} {
		if _, ok := rows[kind+":project-open-user-101"]; ok {
			t.Fatalf("selected visitor %s survived deletion", kind)
		}
		if _, ok := rows[kind+":project-open-user-102"]; !ok {
			t.Fatalf("other project member %s was deleted", kind)
		}
	}
	if _, ok := rows["deletion_session:"+project.ID]; ok {
		t.Fatal("project id was turned into a session tombstone")
	}
	if _, ok := rows[projectNoteProjectKind+":"+project.ID]; !ok {
		t.Fatal("privacy deletion removed private project")
	}
	if _, ok := rows[projectNoteVersionKind+":"+version.ID]; !ok {
		t.Fatal("privacy deletion removed frozen version")
	}
	if _, err = s.ProjectNoteReadShare(ctx, share.Token, 101); err != nil {
		t.Fatalf("privacy tombstone incorrectly blocked private business read: %v", err)
	}
	if _, err = s.ProjectNoteOpenShare(ctx, share.Token, 101, "project-open-after-delete"); err == nil {
		t.Fatal("privacy-deleted visitor created a new raw open fact")
	}
	if _, ok := rows[projectNoteOpenKind+":project-open-after-delete"]; ok {
		t.Fatal("rejected open wrote a raw fact")
	}
}
