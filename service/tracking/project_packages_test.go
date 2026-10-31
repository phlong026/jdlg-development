package tracking

import (
	"context"
	"errors"
	"testing"
	"time"
)

type failingProjectPackageMemberRepo struct{}

type failingProjectPackageVersionStore struct {
	Store
	FailAt int
}

type failingProjectPackageVersionRepo struct {
	Repository
	FailAt int
	Count  int
}

func (s failingProjectPackageVersionStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.Store.Transaction(ctx, tenant, func(repo Repository) error {
		return fn(&failingProjectPackageVersionRepo{Repository: repo, FailAt: s.FailAt})
	})
}

func (s failingProjectPackageVersionStore) ReadTransaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	reader, ok := s.Store.(ReadStore)
	if !ok {
		return s.Store.Transaction(ctx, tenant, fn)
	}
	return reader.ReadTransaction(ctx, tenant, fn)
}

func (r *failingProjectPackageVersionRepo) Insert(row Record) error {
	if row.Kind == projectNoteVersionKind {
		r.Count++
		if r.Count == r.FailAt {
			return errors.New("version snapshot unavailable")
		}
	}
	return r.Repository.Insert(row)
}

func (failingProjectPackageMemberRepo) Get(string, string) (Record, error) {
	return Record{}, errors.New("member store unavailable")
}
func (failingProjectPackageMemberRepo) List(string) ([]Record, error) { return nil, nil }
func (failingProjectPackageMemberRepo) Put(Record) error              { return nil }
func (failingProjectPackageMemberRepo) Insert(Record) error           { return nil }
func (failingProjectPackageMemberRepo) Remove(string, string) error   { return nil }

func projectPackageVersions(t *testing.T, s *Service, designer Scope, project ProjectNoteProject, note ProjectNote) (ProjectNoteVersion, ProjectNoteVersion) {
	t.Helper()
	ctx := context.Background()
	v1, err := s.ProjectNoteFreezeVersion(ctx, designer, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	note, err = s.ProjectNoteSaveNote(ctx, designer, note.ID, ProjectNoteDraft{Title: "第二版", Body: "第二版正文", BodyFormat: "plain"}, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.ProjectNoteFreezeVersion(ctx, designer, note.ID, note.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if v1.ProjectID != project.ID || v2.ProjectID != project.ID {
		t.Fatal("fixture versions escaped project")
	}
	return v1, v2
}

func projectPackageFixture(t *testing.T) (*Service, Scope, ProjectNoteProject, ProjectNoteVersion, ProjectNoteVersion) {
	t.Helper()
	ctx := context.Background()
	s, designer, _ := projectNoteFixture(t)
	project, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "张宅交付", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []int64{101, 102} {
		if _, err = s.ProjectNoteInviteMember(ctx, designer, project.ID, actor); err != nil {
			t.Fatal(err)
		}
	}
	note, err := s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{Title: "第一版", Body: "第一版正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	v1, v2 := projectPackageVersions(t, s, designer, project, note)
	return s, designer, project, v1, v2
}

func packageStatus(t *testing.T, err error, status int) {
	t.Helper()
	var target *Error
	if !errors.As(err, &target) || target.Status != status {
		t.Fatalf("status = %#v, want %d", err, status)
	}
}

func TestProjectPackageDraftIsIdempotentConflictCheckedAndProjectBound(t *testing.T) {
	ctx := context.Background()
	s, designer, project, v1, v2 := projectPackageFixture(t)
	input := ProjectPackageDraftInput{ProjectID: project.ID, VersionIDs: []string{v1.ID, v2.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "package-draft-create-1"}
	first, err := s.ProjectPackageSaveDraft(ctx, designer, input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.ProjectPackageSaveDraft(ctx, designer, input)
	if err != nil || replayed.ID != first.ID || !replayed.Replayed {
		t.Fatalf("draft replay = %#v, %v", replayed, err)
	}
	input.RecipientIDs = []int64{102}
	if _, err = s.ProjectPackageSaveDraft(ctx, designer, input); err == nil {
		t.Fatal("idempotency key accepted different recipients")
	} else {
		packageStatus(t, err, 409)
	}

	updated, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{PackageID: first.ID, ProjectID: project.ID, ExpectedRevision: first.Revision, VersionIDs: []string{v2.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "package-draft-update-1"})
	if err != nil || updated.Revision != first.Revision+1 {
		t.Fatalf("update = %#v, %v", updated, err)
	}
	if _, err = s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{PackageID: first.ID, ProjectID: project.ID, ExpectedRevision: first.Revision, VersionIDs: []string{v1.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "package-draft-stale-1"}); err == nil {
		t.Fatal("stale package draft overwrote current revision")
	} else {
		packageStatus(t, err, 409)
	}

	other, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "李宅", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	otherNote, err := s.ProjectNoteCreateNote(ctx, designer, other.ID, ProjectNoteDraft{Title: "李宅方案", Body: "正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	otherVersion, err := s.ProjectNoteFreezeVersion(ctx, designer, otherNote.ID, otherNote.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{ProjectID: project.ID, VersionIDs: []string{v1.ID, otherVersion.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "package-cross-project-1"}); err == nil {
		t.Fatal("cross-project version entered package")
	} else {
		packageStatus(t, err, 400)
	}
}

func TestProjectPackagePreviewRequiresExplicitActiveRecipientsAndFreshRevision(t *testing.T) {
	ctx := context.Background()
	s, designer, project, v1, v2 := projectPackageFixture(t)
	empty, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{ProjectID: project.ID, VersionIDs: []string{v1.ID}, IdempotencyKey: "package-no-recipient-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectPackagePreview(ctx, designer, empty.ID); err == nil {
		t.Fatal("preview silently selected project members")
	} else {
		packageStatus(t, err, 400)
	}

	draft, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{PackageID: empty.ID, ProjectID: project.ID, ExpectedRevision: empty.Revision, VersionIDs: []string{v1.ID, v2.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "package-select-recipient-1"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ProjectPackagePreview(ctx, designer, draft.ID)
	if err != nil || len(preview.Items) != 2 || len(preview.Recipients) != 1 || preview.Recipients[0].ActorID != 101 || preview.Signature == "" {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	if err = s.ProjectNoteRevokeMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectPackagePreview(ctx, designer, draft.ID); err == nil {
		t.Fatal("preview kept revoked recipient")
	} else {
		packageStatus(t, err, 400)
	}
}

func TestProjectPackageRecipientValidationPreservesStoreFailure(t *testing.T) {
	_, err := projectPackageValidateRecipients(failingProjectPackageMemberRepo{}, "project-one", []int64{101}, true)
	if err == nil || err.Error() != "member store unavailable" {
		t.Fatalf("recipient store error = %v", err)
	}
}

func TestProjectPackagePublishPinsManifestAndDoesNotDuplicateDelivery(t *testing.T) {
	ctx := context.Background()
	s, designer, project, v1, v2 := projectPackageFixture(t)
	draft, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{ProjectID: project.ID, VersionIDs: []string{v1.ID, v2.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "package-publish-draft-1"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ProjectPackagePreview(ctx, designer, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := ProjectPackagePublishInput{PackageID: draft.ID, ExpectedRevision: draft.Revision, PreviewSignature: preview.Signature, ExpiresAt: s.Now().Add(time.Hour), IdempotencyKey: "package-publish-1"}
	delivery, err := s.ProjectPackagePublish(ctx, designer, input)
	if err != nil || delivery.Token == "" || len(delivery.Items) != 2 {
		t.Fatalf("delivery = %#v, %v", delivery, err)
	}
	replayed, err := s.ProjectPackagePublish(ctx, designer, input)
	if err != nil || replayed.ID != delivery.ID || !replayed.Replayed || replayed.Token != "" {
		t.Fatalf("publish replay = %#v, %v", replayed, err)
	}
	originalNow := s.Now
	s.Now = func() time.Time { return input.ExpiresAt.Add(time.Minute) }
	replayed, err = s.ProjectPackagePublish(ctx, designer, input)
	if err != nil || replayed.ID != delivery.ID || !replayed.Replayed || replayed.Token != "" {
		t.Fatalf("expired publish replay = %#v, %v", replayed, err)
	}
	s.Now = originalNow
	if _, err = s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{PackageID: draft.ID, ProjectID: project.ID, ExpectedRevision: draft.Revision, VersionIDs: []string{v2.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "package-edit-after-publish-1"}); err != nil {
		t.Fatal(err)
	}

	view, err := s.ProjectPackageRead(ctx, delivery.Token, 101)
	if err != nil || len(view.Items) != 2 || view.Items[0].VersionID != v1.ID || view.Items[1].VersionID != v2.ID || len(view.Delivery.RecipientIDs) != 0 {
		t.Fatalf("manifest = %#v, %v", view, err)
	}
	if _, err = s.ProjectPackageRead(ctx, delivery.Token, 102); err == nil {
		t.Fatal("active but unselected member read package")
	} else {
		packageStatus(t, err, 403)
	}

	bad := input
	bad.IdempotencyKey = "package-publish-bad-preview"
	bad.PreviewSignature = "stale-preview-signature"
	if _, err = s.ProjectPackagePublish(ctx, designer, bad); err == nil {
		t.Fatal("unchecked preview published")
	} else {
		packageStatus(t, err, 409)
	}
}

func TestProjectPackageReadUsesWhitelistAndLiveMembershipIntersection(t *testing.T) {
	ctx := context.Background()
	s, designer, project, v1, _ := projectPackageFixture(t)
	draft, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{ProjectID: project.ID, VersionIDs: []string{v1.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "package-access-draft-1"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ProjectPackagePreview(ctx, designer, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := s.ProjectPackagePublish(ctx, designer, ProjectPackagePublishInput{PackageID: draft.ID, ExpectedRevision: draft.Revision, PreviewSignature: preview.Signature, ExpiresAt: s.Now().Add(time.Hour), IdempotencyKey: "package-access-publish-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectPackageRead(ctx, delivery.Token, 101); err != nil {
		t.Fatal(err)
	}
	if err = s.ProjectNoteRevokeMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectPackageRead(ctx, delivery.Token, 101); err == nil {
		t.Fatal("revoked member retained package")
	}
	if _, err = s.ProjectNoteInviteMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if err = s.ProjectPackageRevoke(ctx, designer, delivery.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectPackageRead(ctx, delivery.Token, 101); err == nil {
		t.Fatal("revoked delivery remained readable")
	} else {
		packageStatus(t, err, 404)
	}
}

func TestProjectPackageChildContextKeepsFeedbackAndConfirmationOnExactVersions(t *testing.T) {
	ctx := context.Background()
	s, designer, project, v1, v2 := projectPackageFixture(t)
	if err := s.ProjectNoteSetConfirmer(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	draft, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{ProjectID: project.ID, VersionIDs: []string{v1.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "package-child-draft-1"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ProjectPackagePreview(ctx, designer, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := s.ProjectPackagePublish(ctx, designer, ProjectPackagePublishInput{PackageID: draft.ID, ExpectedRevision: draft.Revision, PreviewSignature: preview.Signature, ExpiresAt: s.Now().Add(time.Hour), IdempotencyKey: "package-child-publish-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectPackageReadVersion(ctx, delivery.Token, 101, v2.ID); err == nil {
		t.Fatal("same-project version outside manifest was exposed")
	} else {
		packageStatus(t, err, 404)
	}
	feedback, err := s.ProjectPackageAddFeedback(ctx, delivery.Token, 101, v1.ID, "调整门厅", ProjectNoteSatisfactionNeedsAdjustment, "package-feedback-1")
	if err != nil || feedback.VersionID != v1.ID {
		t.Fatalf("feedback = %#v, %v", feedback, err)
	}
	confirmation, err := s.ProjectPackageConfirmVersion(ctx, delivery.Token, 101, v1.ID, "package-confirm-1")
	if err != nil || confirmation.VersionID != v1.ID {
		t.Fatalf("confirmation = %#v, %v", confirmation, err)
	}
	child, err := s.ProjectPackageReadVersion(ctx, delivery.Token, 101, v1.ID)
	if err != nil || child.Version.ID != v1.ID || len(child.Feedbacks) != 1 || child.Confirmation == nil || child.CanConfirm {
		t.Fatalf("child context = %#v, %v", child, err)
	}
}

func TestProjectPackageReceiptIsExplicitIdempotentAndVersionBound(t *testing.T) {
	ctx := context.Background()
	s, designer, project, v1, _ := projectPackageFixture(t)
	var notices []NoticeEvent
	s.OnNoticeEvent = func(_ context.Context, notice NoticeEvent) error {
		notices = append(notices, notice)
		return nil
	}
	draft, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{ProjectID: project.ID, VersionIDs: []string{v1.ID}, RecipientIDs: []int64{101}, IdempotencyKey: "package-receipt-draft-1"})
	if err != nil {
		t.Fatal(err)
	}
	preview, _ := s.ProjectPackagePreview(ctx, designer, draft.ID)
	delivery, err := s.ProjectPackagePublish(ctx, designer, ProjectPackagePublishInput{PackageID: draft.ID, ExpectedRevision: draft.Revision, PreviewSignature: preview.Signature, ExpiresAt: s.Now().Add(time.Hour), IdempotencyKey: "package-receipt-publish-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectPackageRead(ctx, delivery.Token, 101); err != nil {
		t.Fatal(err)
	}
	history, err := s.ProjectPackageGetHistory(ctx, designer, project.ID)
	if err != nil || len(history.Receipts) != 0 {
		t.Fatalf("directory read created a receipt: %#v %v", history, err)
	}
	first, err := s.ProjectPackageAcknowledge(ctx, delivery.Token, 101, v1.ID, "package-receipt-event-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ProjectPackageAcknowledge(ctx, delivery.Token, 101, v1.ID, "package-receipt-event-1")
	if err != nil || second.ID != first.ID || second.VersionID != v1.ID {
		t.Fatalf("receipt replay = %#v, %v", second, err)
	}
	semanticReplay, err := s.ProjectPackageAcknowledge(ctx, delivery.Token, 101, v1.ID, "package-receipt-event-2")
	if err != nil || semanticReplay.ID != first.ID {
		t.Fatalf("semantic receipt replay = %#v, %v", semanticReplay, err)
	}
	if len(notices) != 1 || notices[0].Kind != ProjectPackageReceiptNoticeKind || notices[0].RecipientID != designer.Actor || notices[0].ProjectID != project.ID || notices[0].ContentID != v1.ID || notices[0].ShareID != delivery.ID || notices[0].VisitorKey != "u:101" {
		t.Fatalf("receipt notices = %#v", notices)
	}
	if ok, eligibleErr := s.ProjectNoteNoticeEligible(ctx, notices[0].ID, designer.Actor, project.Owner); eligibleErr != nil || !ok {
		t.Fatalf("receipt notice eligibility = %v, %v", ok, eligibleErr)
	}
	if ok, eligibleErr := s.ProjectNoteNoticeEligible(ctx, notices[0].ID, 999, project.Owner); eligibleErr != nil || ok {
		t.Fatalf("foreign receipt notice eligibility = %v, %v", ok, eligibleErr)
	}
	if err = s.ProjectPackageRevoke(ctx, designer, delivery.ID); err != nil {
		t.Fatal(err)
	}
	if ok, eligibleErr := s.ProjectNoteNoticeEligible(ctx, notices[0].ID, designer.Actor, project.Owner); eligibleErr != nil || !ok {
		t.Fatalf("historical receipt after delivery revoke = %v, %v", ok, eligibleErr)
	}
	s.IsContentOperator = func(context.Context, int64, string) (bool, error) { return false, nil }
	if ok, eligibleErr := s.ProjectNoteNoticeEligible(ctx, notices[0].ID, designer.Actor, project.Owner); eligibleErr != nil || ok {
		t.Fatalf("receipt remained visible after designer permission removal = %v, %v", ok, eligibleErr)
	}
	s.IsContentOperator = func(_ context.Context, actor int64, owner string) (bool, error) {
		return actor == designer.Actor && owner == project.Owner, nil
	}
	history, err = s.ProjectPackageGetHistory(ctx, designer, project.ID)
	if err != nil || len(history.Deliveries) != 1 || len(history.Receipts) != 1 || history.Deliveries[0].Token != "" {
		t.Fatalf("history = %#v, %v", history, err)
	}
}

func TestProjectPackageDraftNotesConflictWhenChangedAfterPreview(t *testing.T) {
	ctx := context.Background()
	s, designer, project, _, current := projectPackageFixture(t)
	draft, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{
		ProjectID: project.ID, NoteSelections: []ProjectPackageNoteSelection{{NoteID: current.NoteID, Revision: current.Revision}},
		RecipientIDs: []int64{101}, IdempotencyKey: "package-draft-note-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ProjectPackagePreview(ctx, designer, draft.ID)
	if err != nil || len(preview.Items) != 1 || preview.Items[0].VersionID != "" || !preview.Items[0].PendingFreeze {
		t.Fatalf("draft preview = %#v, %v", preview, err)
	}
	note, err := s.ProjectNoteSaveNote(ctx, designer, current.NoteID, ProjectNoteDraft{Title: "预览后更新", Body: "新正文", BodyFormat: "plain"}, current.Revision)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ProjectPackagePublish(ctx, designer, ProjectPackagePublishInput{
		PackageID: draft.ID, ExpectedRevision: draft.Revision, PreviewSignature: preview.Signature,
		ExpiresAt: s.Now().Add(time.Hour), IdempotencyKey: "package-draft-note-publish-1",
	})
	if err == nil {
		t.Fatal("changed note published from stale preview")
	}
	packageStatus(t, err, 409)
	if note.Revision == current.Revision {
		t.Fatal("fixture did not change note revision")
	}
}

func TestProjectPackageDraftFreezeRollsBackAllNotesOnConflict(t *testing.T) {
	ctx := context.Background()
	s, designer, project, _, current := projectPackageFixture(t)
	second, err := s.ProjectNoteCreateNote(ctx, designer, project.ID, ProjectNoteDraft{Title: "景观方案", Body: "景观正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{
		ProjectID:      project.ID,
		NoteSelections: []ProjectPackageNoteSelection{{NoteID: current.NoteID, Revision: current.Revision}, {NoteID: second.ID, Revision: second.Revision}},
		RecipientIDs:   []int64{101}, IdempotencyKey: "package-draft-rollback-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ProjectPackagePreview(ctx, designer, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.ProjectNoteGetProjectDetail(ctx, designer, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.Store = failingProjectPackageVersionStore{Store: s.Store, FailAt: 2}
	_, err = s.ProjectPackagePublish(ctx, designer, ProjectPackagePublishInput{
		PackageID: draft.ID, ExpectedRevision: draft.Revision, PreviewSignature: preview.Signature,
		ExpiresAt: s.Now().Add(time.Hour), IdempotencyKey: "package-draft-rollback-publish-1",
	})
	if err == nil || err.Error() != "version snapshot unavailable" {
		t.Fatalf("partial draft freeze error = %v", err)
	}
	after, readErr := s.ProjectNoteGetProjectDetail(ctx, designer, project.ID)
	if readErr != nil || len(after.Versions) != len(before.Versions) {
		t.Fatalf("partial freeze was not rolled back: before=%d after=%d err=%v", len(before.Versions), len(after.Versions), readErr)
	}
}

func TestProjectPackagePublishFreezesDraftOnceAndKeepsExistingVersion(t *testing.T) {
	ctx := context.Background()
	s, designer, project, existing, current := projectPackageFixture(t)
	draft, err := s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{
		ProjectID: project.ID, VersionIDs: []string{existing.ID},
		NoteSelections: []ProjectPackageNoteSelection{{NoteID: current.NoteID, Revision: current.Revision}},
		RecipientIDs:   []int64{101}, IdempotencyKey: "package-mixed-draft-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ProjectPackagePreview(ctx, designer, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := ProjectPackagePublishInput{PackageID: draft.ID, ExpectedRevision: draft.Revision, PreviewSignature: preview.Signature, ExpiresAt: s.Now().Add(time.Hour), IdempotencyKey: "package-mixed-publish-1"}
	delivery, err := s.ProjectPackagePublish(ctx, designer, input)
	if err != nil || len(delivery.Items) != 2 || delivery.Items[0].VersionID != existing.ID || delivery.Items[1].VersionID == "" {
		t.Fatalf("mixed delivery = %#v, %v", delivery, err)
	}
	firstFrozen := delivery.Items[1].VersionID
	replayed, err := s.ProjectPackagePublish(ctx, designer, input)
	if err != nil || !replayed.Replayed || replayed.Items[1].VersionID != firstFrozen {
		t.Fatalf("mixed replay = %#v, %v", replayed, err)
	}
	detail, err := s.ProjectNoteGetProjectDetail(ctx, designer, project.ID)
	if err != nil || len(detail.Versions) != 3 {
		t.Fatalf("version count = %d, err=%v", len(detail.Versions), err)
	}
}

func TestProjectPackageDraftNotesCannotCrossProjects(t *testing.T) {
	ctx := context.Background()
	s, designer, project, _, _ := projectPackageFixture(t)
	other, err := s.ProjectNoteCreateProject(ctx, designer, ProjectNoteProjectInput{Name: "李宅", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := s.ProjectNoteCreateNote(ctx, designer, other.ID, ProjectNoteDraft{Title: "李宅草稿", Body: "正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ProjectPackageSaveDraft(ctx, designer, ProjectPackageDraftInput{
		ProjectID: project.ID, NoteSelections: []ProjectPackageNoteSelection{{NoteID: note.ID, Revision: note.Revision}},
		RecipientIDs: []int64{101}, IdempotencyKey: "package-cross-project-note-1",
	})
	if err == nil {
		t.Fatal("cross-project note entered package")
	}
	packageStatus(t, err, 400)
}
