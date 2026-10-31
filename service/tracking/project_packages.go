package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	projectPackageDraftKind         = "project_package_draft"
	projectPackageDeliveryKind      = "project_package_delivery"
	projectPackageRequestKind       = "project_package_request"
	projectPackageReceiptKind       = "project_package_receipt"
	ProjectPackageReceiptNoticeKind = "project_package_receipt"
)

type ProjectPackageNoteSelection struct {
	NoteID   string `json:"note_id"`
	Revision int    `json:"revision"`
}

// ProjectPackageDraftInput accepts existing frozen versions and saved project
// notes. Saved notes are frozen only after the user confirms the preview.
type ProjectPackageDraftInput struct {
	PackageID        string                        `json:"package_id,omitempty"`
	ProjectID        string                        `json:"project_id"`
	ExpectedRevision int                           `json:"expected_revision,omitempty"`
	VersionIDs       []string                      `json:"version_ids"`
	NoteSelections   []ProjectPackageNoteSelection `json:"note_selections,omitempty"`
	RecipientIDs     []int64                       `json:"recipient_ids"`
	IdempotencyKey   string                        `json:"idempotency_key"`
}

type ProjectPackageDraft struct {
	ID             string                        `json:"package_id"`
	ProjectID      string                        `json:"project_id"`
	DesignerID     int64                         `json:"designer_id"`
	Revision       int                           `json:"revision"`
	VersionIDs     []string                      `json:"version_ids"`
	NoteSelections []ProjectPackageNoteSelection `json:"note_selections,omitempty"`
	RecipientIDs   []int64                       `json:"recipient_ids"`
	CreatedAt      time.Time                     `json:"created_at"`
	UpdatedAt      time.Time                     `json:"updated_at"`
	Replayed       bool                          `json:"replayed,omitempty"`
}

type ProjectPackageItem struct {
	Key           string    `json:"key"`
	VersionID     string    `json:"version_id,omitempty"`
	NoteID        string    `json:"note_id"`
	Revision      int       `json:"revision"`
	Title         string    `json:"title"`
	FrozenAt      time.Time `json:"frozen_at,omitempty"`
	PendingFreeze bool      `json:"pending_freeze,omitempty"`
}

type ProjectPackagePreview struct {
	PackageID  string               `json:"package_id"`
	Project    ProjectNoteProject   `json:"project"`
	Revision   int                  `json:"revision"`
	Items      []ProjectPackageItem `json:"items"`
	Recipients []ProjectNoteMember  `json:"recipients"`
	Signature  string               `json:"preview_signature"`
}

type ProjectPackagePublishInput struct {
	PackageID        string    `json:"package_id"`
	ExpectedRevision int       `json:"expected_revision"`
	PreviewSignature string    `json:"preview_signature"`
	ExpiresAt        time.Time `json:"expires_at"`
	IdempotencyKey   string    `json:"idempotency_key"`
}

type ProjectPackageDelivery struct {
	ID            string               `json:"delivery_id"`
	PackageID     string               `json:"package_id"`
	ProjectID     string               `json:"project_id"`
	ProjectName   string               `json:"project_name"`
	DraftRevision int                  `json:"draft_revision"`
	SharerID      int64                `json:"sharer_id"`
	RecipientIDs  []int64              `json:"recipient_ids"`
	Items         []ProjectPackageItem `json:"items"`
	ExpiresAt     time.Time            `json:"expires_at"`
	Revoked       bool                 `json:"revoked"`
	CreatedAt     time.Time            `json:"created_at"`
	Token         string               `json:"package_token,omitempty"`
	Replayed      bool                 `json:"replayed,omitempty"`
}

type projectPackageDeliveryRecord struct {
	ProjectPackageDelivery
	TokenHash string `json:"token_hash"`
}

type projectPackageRequest struct {
	Signature      string               `json:"signature"`
	Draft          *ProjectPackageDraft `json:"draft,omitempty"`
	DeliveryRecord string               `json:"delivery_record,omitempty"`
}

type ProjectPackageView struct {
	Project  ProjectNoteProject     `json:"project"`
	Delivery ProjectPackageDelivery `json:"delivery"`
	Items    []ProjectPackageItem   `json:"items"`
}

type ProjectPackageVersionView struct {
	Package           ProjectPackageView              `json:"package"`
	Version           ProjectNoteVersion              `json:"version"`
	Feedbacks         []ProjectNoteFeedback           `json:"feedbacks"`
	Resolutions       []ProjectNoteFeedbackResolution `json:"resolutions"`
	Confirmation      *ProjectNoteConfirmation        `json:"confirmation,omitempty"`
	CanConfirm        bool                            `json:"can_confirm"`
	ActivityTruncated bool                            `json:"activity_truncated"`
}

type ProjectPackageReceipt struct {
	ID             string    `json:"event_id"`
	DeliveryID     string    `json:"delivery_id"`
	ProjectID      string    `json:"project_id"`
	VersionID      string    `json:"version_id,omitempty"`
	ActorID        int64     `json:"actor_id"`
	AcknowledgedAt time.Time `json:"acknowledged_at"`
}

type ProjectPackageHistory struct {
	Deliveries []ProjectPackageDelivery `json:"deliveries"`
	Receipts   []ProjectPackageReceipt  `json:"receipts"`
	Truncated  bool                     `json:"truncated"`
}

func projectPackageUniqueVersions(ids []string) ([]string, error) {
	if len(ids) > 20 {
		return nil, fail(400, "分享资料最多选择20项")
	}
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !validID(id) || seen[id] {
			return nil, fail(400, "固定版本清单无效")
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func projectPackageUniqueNotes(selections []ProjectPackageNoteSelection) ([]ProjectPackageNoteSelection, error) {
	if len(selections) > 20 {
		return nil, fail(400, "分享资料最多选择20项")
	}
	out := make([]ProjectPackageNoteSelection, 0, len(selections))
	seen := map[string]bool{}
	for _, selection := range selections {
		selection.NoteID = strings.TrimSpace(selection.NoteID)
		if !validID(selection.NoteID) || selection.Revision <= 0 || seen[selection.NoteID] {
			return nil, fail(400, "项目笔记清单无效")
		}
		seen[selection.NoteID] = true
		out = append(out, selection)
	}
	return out, nil
}

func projectPackageUniqueRecipients(ids []int64) ([]int64, error) {
	if len(ids) > 20 {
		return nil, fail(400, "接收人数量超限")
	}
	out := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return nil, fail(400, "接收人清单无效")
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func projectPackageInputSignature(input ProjectPackageDraftInput) string {
	return hash(string(marshal(struct {
		PackageID  string                        `json:"package_id"`
		ProjectID  string                        `json:"project_id"`
		Revision   int                           `json:"expected_revision"`
		Versions   []string                      `json:"version_ids"`
		Notes      []ProjectPackageNoteSelection `json:"note_selections"`
		Recipients []int64                       `json:"recipient_ids"`
	}{input.PackageID, input.ProjectID, input.ExpectedRevision, input.VersionIDs, input.NoteSelections, input.RecipientIDs})))
}

func projectPackageLoadDraft(r Repository, packageID string) (ProjectPackageDraft, error) {
	draft, err := projectNoteRequired[ProjectPackageDraft](r, projectPackageDraftKind, packageID, "分享资料草稿不存在")
	if err != nil {
		return draft, err
	}
	if draft.ID != packageID || draft.ProjectID == "" || draft.DesignerID <= 0 || draft.Revision <= 0 {
		return draft, ErrUnavailable
	}
	return draft, nil
}

func projectPackageValidateVersions(r Repository, projectID string, ids []string) ([]ProjectPackageItem, error) {
	items := make([]ProjectPackageItem, 0, len(ids))
	for _, id := range ids {
		version, err := projectNoteRequired[ProjectNoteVersion](r, projectNoteVersionKind, id, "固定版本不存在")
		if err != nil {
			return nil, err
		}
		if version.ProjectID != projectID {
			return nil, fail(400, "固定版本不属于当前项目")
		}
		items = append(items, ProjectPackageItem{Key: "version:" + version.ID, VersionID: version.ID, NoteID: version.NoteID, Revision: version.Revision, Title: version.Title, FrozenAt: version.FrozenAt})
	}
	return items, nil
}

func projectPackageValidateNotes(r Repository, projectID string, selections []ProjectPackageNoteSelection) ([]ProjectPackageItem, error) {
	items := make([]ProjectPackageItem, 0, len(selections))
	for _, selection := range selections {
		note, err := projectNoteRequired[ProjectNote](r, projectNoteKind, selection.NoteID, "项目笔记不存在")
		if err != nil {
			return nil, err
		}
		if note.ProjectID != projectID {
			return nil, fail(400, "项目笔记不属于当前项目")
		}
		if err = projectNoteRequireActive(r, note.ID); err != nil {
			return nil, err
		}
		if note.Revision != selection.Revision {
			return nil, fail(409, "项目笔记已更新，请重新预览")
		}
		items = append(items, ProjectPackageItem{Key: "note:" + note.ID, NoteID: note.ID, Revision: note.Revision, Title: note.Title, PendingFreeze: true})
	}
	return items, nil
}

func projectPackageValidateRecipients(r Repository, projectID string, ids []int64, required bool) ([]ProjectNoteMember, error) {
	if required && len(ids) == 0 {
		return nil, fail(400, "请明确选择本次接收人")
	}
	members := make([]ProjectNoteMember, 0, len(ids))
	for _, actor := range ids {
		member, err := projectNoteLoadMember(r, projectID, actor)
		if err != nil {
			var denied *Error
			if errors.As(err, &denied) && denied.Status == 403 {
				return nil, fail(400, "接收人已不具备当前项目访问权")
			}
			return nil, err
		}
		members = append(members, member)
	}
	return members, nil
}

func (s *Service) ProjectPackageSaveDraft(ctx context.Context, scope Scope, input ProjectPackageDraftInput) (ProjectPackageDraft, error) {
	var out ProjectPackageDraft
	input.PackageID, input.ProjectID, input.IdempotencyKey = strings.TrimSpace(input.PackageID), strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.IdempotencyKey)
	versions, err := projectPackageUniqueVersions(input.VersionIDs)
	if err != nil {
		return out, err
	}
	notes, err := projectPackageUniqueNotes(input.NoteSelections)
	if err != nil {
		return out, err
	}
	if len(versions)+len(notes) == 0 || len(versions)+len(notes) > 20 {
		return out, fail(400, "分享资料须选择1至20项")
	}
	recipients, err := projectPackageUniqueRecipients(input.RecipientIDs)
	if err != nil {
		return out, err
	}
	if !validID(input.ProjectID) || !validID(input.IdempotencyKey) || input.PackageID != "" && !validID(input.PackageID) || input.ExpectedRevision < 0 {
		return out, fail(400, "分享资料草稿请求无效")
	}
	input.VersionIDs, input.NoteSelections, input.RecipientIDs = versions, notes, recipients
	signature := projectPackageInputSignature(input)
	err = s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, input.ProjectID)
		if err != nil {
			return err
		}
		requestID := "draft:" + hash(fmt.Sprintf("%d:%s", scope.Actor, input.IdempotencyKey))
		if request, readErr := decode[projectPackageRequest](r, projectPackageRequestKind, requestID); readErr == nil {
			if request.Signature != signature || request.Draft == nil {
				return fail(409, "草稿幂等标识已用于其他请求")
			}
			out = *request.Draft
			out.Replayed = true
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		if _, err = projectPackageValidateVersions(r, project.ID, versions); err != nil {
			return err
		}
		if _, err = projectPackageValidateNotes(r, project.ID, notes); err != nil {
			return err
		}
		if _, err = projectPackageValidateRecipients(r, project.ID, recipients, false); err != nil {
			return err
		}
		now := s.Now().UTC()
		if input.PackageID == "" {
			if input.ExpectedRevision != 0 {
				return fail(409, "新草稿版本号无效")
			}
			out = ProjectPackageDraft{ID: opaque(), ProjectID: project.ID, DesignerID: scope.Actor, Revision: 1, VersionIDs: append([]string(nil), versions...), NoteSelections: append([]ProjectPackageNoteSelection(nil), notes...), RecipientIDs: append([]int64(nil), recipients...), CreatedAt: now, UpdatedAt: now}
			if err = r.Insert(projectNoteRecord(projectPackageDraftKind, out.ID, project.Owner, now, out, projectNoteActorKey(scope.Actor), project.ID, out.ID, "")); err != nil {
				return err
			}
		} else {
			out, err = projectPackageLoadDraft(r, input.PackageID)
			if err != nil {
				return err
			}
			if out.ProjectID != project.ID || out.DesignerID != scope.Actor {
				return fail(404, "分享资料草稿不存在")
			}
			if out.Revision != input.ExpectedRevision {
				return fail(409, "分享资料草稿已更新，请重新读取")
			}
			out.Revision++
			out.VersionIDs, out.NoteSelections, out.RecipientIDs, out.UpdatedAt = append([]string(nil), versions...), append([]ProjectPackageNoteSelection(nil), notes...), append([]int64(nil), recipients...), now
			if err = r.Put(projectNoteRecord(projectPackageDraftKind, out.ID, project.Owner, now, out, projectNoteActorKey(scope.Actor), project.ID, out.ID, "")); err != nil {
				return err
			}
		}
		copy := out
		return r.Insert(projectNoteRecord(projectPackageRequestKind, requestID, project.Owner, now, projectPackageRequest{Signature: signature, Draft: &copy}, projectNoteActorKey(scope.Actor), project.ID, out.ID, ""))
	})
	return out, err
}

func projectPackagePreviewSignature(draft ProjectPackageDraft, items []ProjectPackageItem, recipients []ProjectNoteMember) string {
	actors := make([]int64, 0, len(recipients))
	for _, member := range recipients {
		actors = append(actors, member.ActorID)
	}
	return hash(string(marshal(struct {
		PackageID  string               `json:"package_id"`
		ProjectID  string               `json:"project_id"`
		Revision   int                  `json:"revision"`
		Items      []ProjectPackageItem `json:"items"`
		Recipients []int64              `json:"recipients"`
	}{draft.ID, draft.ProjectID, draft.Revision, items, actors})))
}

func (s *Service) projectPackagePreviewIn(r Repository, scope Scope, packageID string) (ProjectPackagePreview, error) {
	var out ProjectPackagePreview
	draft, err := projectPackageLoadDraft(r, packageID)
	if err != nil {
		return out, err
	}
	project, err := s.projectNoteManaged(r, scope, draft.ProjectID)
	if err != nil {
		return out, err
	}
	if draft.DesignerID != scope.Actor {
		return out, fail(404, "分享资料草稿不存在")
	}
	items, err := projectPackageValidateVersions(r, project.ID, draft.VersionIDs)
	if err != nil {
		return out, err
	}
	noteItems, err := projectPackageValidateNotes(r, project.ID, draft.NoteSelections)
	if err != nil {
		return out, err
	}
	items = append(items, noteItems...)
	recipients, err := projectPackageValidateRecipients(r, project.ID, draft.RecipientIDs, true)
	if err != nil {
		return out, err
	}
	out = ProjectPackagePreview{PackageID: draft.ID, Project: project, Revision: draft.Revision, Items: items, Recipients: recipients}
	out.Signature = projectPackagePreviewSignature(draft, items, recipients)
	return out, nil
}

func (s *Service) ProjectPackagePreview(ctx context.Context, scope Scope, packageID string) (ProjectPackagePreview, error) {
	var out ProjectPackagePreview
	if !validID(strings.TrimSpace(packageID)) {
		return out, fail(400, "分享资料草稿标识无效")
	}
	err := s.readTransaction(ctx, func(r Repository) error {
		var err error
		out, err = s.projectPackagePreviewIn(r, scope, packageID)
		return err
	})
	return out, err
}

func projectPackagePublishSignature(input ProjectPackagePublishInput) string {
	return hash(string(marshal(struct {
		PackageID string    `json:"package_id"`
		Revision  int       `json:"expected_revision"`
		Preview   string    `json:"preview_signature"`
		ExpiresAt time.Time `json:"expires_at"`
	}{input.PackageID, input.ExpectedRevision, input.PreviewSignature, input.ExpiresAt.UTC()})))
}

func (s *Service) ProjectPackagePublish(ctx context.Context, scope Scope, input ProjectPackagePublishInput) (ProjectPackageDelivery, error) {
	var out ProjectPackageDelivery
	input.PackageID, input.PreviewSignature, input.IdempotencyKey = strings.TrimSpace(input.PackageID), strings.TrimSpace(input.PreviewSignature), strings.TrimSpace(input.IdempotencyKey)
	if !validID(input.PackageID) || !validID(input.PreviewSignature) || !validID(input.IdempotencyKey) || input.ExpectedRevision <= 0 || input.ExpiresAt.IsZero() {
		return out, fail(400, "分享资料发布请求无效")
	}
	signature := projectPackagePublishSignature(input)
	err := s.transaction(ctx, func(r Repository) error {
		draft, err := projectPackageLoadDraft(r, input.PackageID)
		if err != nil {
			return err
		}
		project, err := s.projectNoteManaged(r, scope, draft.ProjectID)
		if err != nil {
			return err
		}
		if draft.DesignerID != scope.Actor {
			return fail(404, "分享资料草稿不存在")
		}
		requestID := "publish:" + hash(fmt.Sprintf("%d:%s", scope.Actor, input.IdempotencyKey))
		if request, readErr := decode[projectPackageRequest](r, projectPackageRequestKind, requestID); readErr == nil {
			if request.Signature != signature || request.DeliveryRecord == "" {
				return fail(409, "发布幂等标识已用于其他请求")
			}
			record, getErr := decode[projectPackageDeliveryRecord](r, projectPackageDeliveryKind, request.DeliveryRecord)
			if getErr != nil {
				return getErr
			}
			out = record.ProjectPackageDelivery
			out.Replayed, out.Token = true, ""
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		if !input.ExpiresAt.After(s.Now()) {
			return fail(400, "分享资料发布请求无效")
		}
		if draft.Revision != input.ExpectedRevision {
			return fail(409, "分享资料草稿已更新，请重新预览")
		}
		preview, err := s.projectPackagePreviewIn(r, scope, draft.ID)
		if err != nil {
			return err
		}
		if preview.Signature != input.PreviewSignature {
			return fail(409, "分享资料已变化，请重新预览")
		}
		items := make([]ProjectPackageItem, 0, len(preview.Items))
		for _, item := range preview.Items {
			if !item.PendingFreeze {
				items = append(items, item)
				continue
			}
			version, freezeErr := s.projectNoteFreezeVersionIn(r, scope, item.NoteID, item.Revision)
			if freezeErr != nil {
				return freezeErr
			}
			items = append(items, ProjectPackageItem{Key: "version:" + version.ID, VersionID: version.ID, NoteID: version.NoteID, Revision: version.Revision, Title: version.Title, FrozenAt: version.FrozenAt})
		}
		now, token := s.Now().UTC(), opaque()
		recipients := make([]int64, 0, len(preview.Recipients))
		for _, member := range preview.Recipients {
			recipients = append(recipients, member.ActorID)
		}
		out = ProjectPackageDelivery{ID: opaque(), PackageID: draft.ID, ProjectID: project.ID, ProjectName: project.Name, DraftRevision: draft.Revision, SharerID: scope.Actor, RecipientIDs: recipients, Items: items, ExpiresAt: input.ExpiresAt.UTC(), CreatedAt: now, Token: token}
		recordID := hash(token)
		stored := projectPackageDeliveryRecord{ProjectPackageDelivery: out, TokenHash: recordID}
		stored.Token = ""
		if err = r.Insert(projectNoteRecord(projectPackageDeliveryKind, recordID, project.Owner, now, stored, projectNoteActorKey(scope.Actor), project.ID, draft.ID, out.ID)); err != nil {
			return err
		}
		return r.Insert(projectNoteRecord(projectPackageRequestKind, requestID, project.Owner, now, projectPackageRequest{Signature: signature, DeliveryRecord: recordID}, projectNoteActorKey(scope.Actor), project.ID, draft.ID, out.ID))
	})
	return out, err
}

func projectPackageRecipient(ids []int64, actor int64) bool {
	for _, id := range ids {
		if id == actor {
			return true
		}
	}
	return false
}

func (s *Service) projectPackageReadIn(ctx context.Context, r Repository, token string, actor int64) (ProjectPackageView, projectPackageDeliveryRecord, error) {
	var out ProjectPackageView
	if actor <= 0 {
		return out, projectPackageDeliveryRecord{}, fail(401, "请先登录")
	}
	if !validID(token) {
		return out, projectPackageDeliveryRecord{}, fail(404, "分享资料不存在或已失效")
	}
	stored, err := decode[projectPackageDeliveryRecord](r, projectPackageDeliveryKind, hash(token))
	if errors.Is(err, ErrNotFound) {
		return out, stored, fail(404, "分享资料不存在或已失效")
	}
	if err != nil {
		return out, stored, err
	}
	if stored.TokenHash != hash(token) || stored.Revoked || !s.Now().Before(stored.ExpiresAt) {
		return out, stored, fail(404, "分享资料不存在或已失效")
	}
	project, err := projectNoteLoadProject(r, stored.ProjectID)
	if err != nil {
		return out, stored, err
	}
	if err = s.projectNoteGrant(ctx, r, project); err != nil {
		return out, stored, err
	}
	if !projectPackageRecipient(stored.RecipientIDs, actor) {
		return out, stored, fail(403, "当前账号不在本次接收名单")
	}
	if _, err = projectNoteLoadMember(r, project.ID, actor); err != nil {
		return out, stored, err
	}
	public := stored.ProjectPackageDelivery
	public.Token, public.RecipientIDs, public.Items = "", nil, nil
	out = ProjectPackageView{Project: project, Delivery: public, Items: append([]ProjectPackageItem(nil), stored.Items...)}
	return out, stored, nil
}

func (s *Service) ProjectPackageRead(ctx context.Context, token string, actor int64) (ProjectPackageView, error) {
	var out ProjectPackageView
	err := s.readTransaction(ctx, func(r Repository) error {
		var err error
		out, _, err = s.projectPackageReadIn(ctx, r, token, actor)
		return err
	})
	return out, err
}

func projectPackageHasVersion(items []ProjectPackageItem, versionID string) bool {
	for _, item := range items {
		if item.VersionID == versionID {
			return true
		}
	}
	return false
}

func (s *Service) projectPackageVersionIn(ctx context.Context, r Repository, token string, actor int64, versionID string) (ProjectPackageVersionView, error) {
	var out ProjectPackageVersionView
	view, _, err := s.projectPackageReadIn(ctx, r, token, actor)
	if err != nil {
		return out, err
	}
	if !validID(versionID) || !projectPackageHasVersion(view.Items, versionID) {
		return out, fail(404, "固定版本不在本次交付清单")
	}
	version, err := projectNoteRequired[ProjectNoteVersion](r, projectNoteVersionKind, versionID, "固定版本不存在")
	if err != nil {
		return out, err
	}
	if version.ProjectID != view.Project.ID {
		return out, ErrUnavailable
	}
	out.Package, out.Version = view, version
	return out, nil
}

func (s *Service) ProjectPackageReadVersion(ctx context.Context, token string, actor int64, versionID string) (ProjectPackageVersionView, error) {
	var out ProjectPackageVersionView
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		view, err := s.projectPackageVersionIn(ctx, r, token, actor, versionID)
		if err != nil {
			return err
		}
		view.Feedbacks = []ProjectNoteFeedback{}
		view.Resolutions = []ProjectNoteFeedbackResolution{}
		rows, err := pageRecords(r, projectNoteFeedbackKind, RecordQuery{Content: versionID, Descending: true, Limit: 51})
		if err != nil {
			return err
		}
		if len(rows) > 50 {
			view.ActivityTruncated, rows = true, rows[:50]
		}
		for _, row := range rows {
			var feedback ProjectNoteFeedback
			if json.Unmarshal(row.Payload, &feedback) != nil || feedback.VersionID != versionID {
				return ErrUnavailable
			}
			view.Feedbacks = append(view.Feedbacks, feedback)
		}
		rows, err = pageRecords(r, projectNoteFeedbackResolutionKind, RecordQuery{Content: versionID, Descending: true, Limit: 51})
		if err != nil {
			return err
		}
		if len(rows) > 50 {
			view.ActivityTruncated, rows = true, rows[:50]
		}
		for _, row := range rows {
			var resolution ProjectNoteFeedbackResolution
			if json.Unmarshal(row.Payload, &resolution) != nil {
				return ErrUnavailable
			}
			view.Resolutions = append(view.Resolutions, resolution)
		}
		rows, err = pageRecords(r, projectNoteConfirmKind, RecordQuery{Content: versionID, Limit: 2})
		if err != nil {
			return err
		}
		if len(rows) > 1 {
			return ErrUnavailable
		}
		if len(rows) == 1 {
			var confirmation ProjectNoteConfirmation
			if json.Unmarshal(rows[0].Payload, &confirmation) != nil || confirmation.VersionID != versionID {
				return ErrUnavailable
			}
			view.Confirmation = &confirmation
		}
		view.CanConfirm = view.Confirmation == nil && view.Package.Project.ConfirmerID == actor
		out = view
		return nil
	})
	return out, err
}

func (s *Service) ProjectPackageReadVersionMedia(ctx context.Context, token string, actor int64, versionID, assetID string) (Asset, error) {
	var out Asset
	err := s.readTransaction(ctx, func(r Repository) error {
		view, err := s.projectPackageVersionIn(ctx, r, token, actor, versionID)
		if err != nil {
			return err
		}
		if view.Version.Cover != nil && view.Version.Cover.ID == assetID {
			out = *view.Version.Cover
			return nil
		}
		for _, asset := range view.Version.Media {
			if asset.ID == assetID {
				out = asset
				return nil
			}
		}
		return fail(404, "媒体不可访问")
	})
	return out, err
}

func projectPackageDeliveryByID(r Repository, owner, deliveryID string) (Record, projectPackageDeliveryRecord, error) {
	rows, err := pageRecords(r, projectPackageDeliveryKind, RecordQuery{Owners: []string{owner}, Shares: []string{deliveryID}, Limit: 2})
	if err != nil {
		return Record{}, projectPackageDeliveryRecord{}, err
	}
	if len(rows) != 1 {
		return Record{}, projectPackageDeliveryRecord{}, fail(404, "分享资料不存在")
	}
	var delivery projectPackageDeliveryRecord
	if json.Unmarshal(rows[0].Payload, &delivery) != nil || delivery.ID != deliveryID {
		return Record{}, delivery, ErrUnavailable
	}
	return rows[0], delivery, nil
}

func (s *Service) ProjectPackageRevoke(ctx context.Context, scope Scope, deliveryID string) error {
	if err := s.authorized(scope); err != nil {
		return err
	}
	if !validID(strings.TrimSpace(deliveryID)) {
		return fail(400, "分享资料标识无效")
	}
	return s.transaction(ctx, func(r Repository) error {
		for _, owner := range scope.Owners {
			row, delivery, err := projectPackageDeliveryByID(r, owner, deliveryID)
			if err != nil {
				var missing *Error
				if errors.As(err, &missing) && missing.Status == 404 {
					continue
				}
				return err
			}
			project, err := s.projectNoteManaged(r, scope, delivery.ProjectID)
			if err != nil {
				return err
			}
			delivery.Revoked = true
			return r.Put(projectNoteRecord(projectPackageDeliveryKind, row.ID, project.Owner, s.Now().UTC(), delivery, row.Visitor, row.Session, row.Content, row.Share))
		}
		return fail(404, "分享资料不存在")
	})
}

func (s *Service) ProjectPackageAddFeedback(ctx context.Context, token string, actor int64, versionID, text, satisfaction, key string) (ProjectNoteFeedback, error) {
	var out ProjectNoteFeedback
	text, satisfaction, key = strings.TrimSpace(text), strings.TrimSpace(satisfaction), strings.TrimSpace(key)
	if text == "" || len(text) > 4000 || !projectNoteValidSatisfaction(satisfaction) || !validID(key) {
		return out, fail(400, "反馈内容或幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		view, err := s.projectPackageVersionIn(ctx, r, token, actor, versionID)
		if err != nil {
			return err
		}
		delivery := view.Package.Delivery
		id := hash(fmt.Sprintf("package-feedback:%s:%s:%d:%s", delivery.ID, versionID, actor, key))
		if old, readErr := decode[ProjectNoteFeedback](r, projectNoteFeedbackKind, id); readErr == nil {
			if old.Text != text || old.Satisfaction != satisfaction || old.VersionID != versionID {
				return fail(409, "反馈幂等标识已用于其他内容")
			}
			out = old
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		out = ProjectNoteFeedback{ID: id, ProjectID: view.Package.Project.ID, VersionID: versionID, ShareID: delivery.ID, ActorID: actor, Text: text, Satisfaction: satisfaction, CreatedAt: now}
		if err = r.Insert(projectNoteRecord(projectNoteFeedbackKind, id, view.Package.Project.Owner, now, out, projectNoteActorKey(actor), view.Package.Project.ID, versionID, delivery.ID)); err != nil {
			return err
		}
		if satisfaction == ProjectNoteSatisfactionNeedsAdjustment || satisfaction == ProjectNoteSatisfactionUnsatisfied {
			current, drives, statusErr := projectNoteVersionDrivesStatus(r, view.Package.Project, versionID)
			if statusErr != nil {
				return statusErr
			}
			if drives {
				project := view.Package.Project
				project.CurrentVersionID, project.Status, project.UpdatedAt = current.ID, ProjectNoteProjectStatusRevising, now
				project.Revision++
				if err = r.Put(projectNoteRecord(projectNoteProjectKind, project.ID, project.Owner, now, project, projectNoteActorKey(project.DesignerID), "", project.ID, "")); err != nil {
					return err
				}
			}
		}
		if actor == view.Package.Project.DesignerID || !s.Config.Enabled {
			return nil
		}
		notice := NoticeEvent{ID: projectNoteFeedbackNoticeID(out.ID), Kind: ProjectNoteFeedbackNoticeKind, Owner: view.Package.Project.Owner, RecipientID: view.Package.Project.DesignerID, VisitorKey: projectNoteActorKey(actor), ShareID: delivery.ID, ProjectID: view.Package.Project.ID, ContentID: versionID, Route: ProjectNoteNoticeRoute, OccurredAt: now}
		row := record("notice_event", notice.ID, notice.Owner, now, notice)
		row.Visitor, row.Content, row.Share = notice.VisitorKey, notice.ContentID, notice.ShareID
		if err = r.Insert(row); err != nil {
			return err
		}
		if s.OnNoticeEvent != nil {
			return s.OnNoticeEvent(sourceReadContext(ctx, r), notice)
		}
		return nil
	})
	return out, err
}

func (s *Service) ProjectPackageConfirmVersion(ctx context.Context, token string, actor int64, versionID, key string) (ProjectNoteConfirmation, error) {
	var out ProjectNoteConfirmation
	if !validID(strings.TrimSpace(key)) {
		return out, fail(400, "确认幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		view, err := s.projectPackageVersionIn(ctx, r, token, actor, versionID)
		if err != nil {
			return err
		}
		project, delivery := view.Package.Project, view.Package.Delivery
		if project.ConfirmerID != actor {
			return fail(403, "仅指定业主可确认方案")
		}
		id := hash(fmt.Sprintf("confirm:%s:%s", project.ID, versionID))
		requestID := "request:" + hash(fmt.Sprintf("confirm:%d:%s", actor, key))
		if request, readErr := decode[projectNoteConfirmationRequest](r, projectNoteConfirmKind, requestID); readErr == nil {
			if request.ProjectID != project.ID || request.VersionID != versionID || request.ActorID != actor || request.ConfirmationID != id {
				return fail(409, "确认幂等标识已用于其他请求")
			}
			stored, storedErr := decode[projectNoteConfirmationRecord](r, projectNoteConfirmKind, id)
			if storedErr != nil {
				return storedErr
			}
			out = stored.ProjectNoteConfirmation
			return s.projectNoteMarkCurrentVersionConfirmed(r, project, versionID, s.Now().UTC())
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		if stored, readErr := decode[projectNoteConfirmationRecord](r, projectNoteConfirmKind, id); readErr == nil {
			if stored.ConfirmerID != actor {
				return fail(409, "当前版本已由原指定业主确认")
			}
			request := projectNoteConfirmationRequest{ConfirmationID: id, ProjectID: project.ID, VersionID: versionID, ActorID: actor}
			if err = r.Insert(projectNoteRecord(projectNoteConfirmKind, requestID, project.Owner, s.Now().UTC(), request, projectNoteActorKey(actor), "", "", "")); err != nil {
				return err
			}
			out = stored.ProjectNoteConfirmation
			return s.projectNoteMarkCurrentVersionConfirmed(r, project, versionID, s.Now().UTC())
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		out = ProjectNoteConfirmation{ID: id, ProjectID: project.ID, VersionID: versionID, ConfirmerID: actor, ConfirmedAt: now}
		stored := projectNoteConfirmationRecord{ProjectNoteConfirmation: out, FirstKeyHash: hash(key)}
		if err = r.Insert(projectNoteRecord(projectNoteConfirmKind, id, project.Owner, now, stored, projectNoteActorKey(actor), project.ID, versionID, delivery.ID)); err != nil {
			return err
		}
		request := projectNoteConfirmationRequest{ConfirmationID: id, ProjectID: project.ID, VersionID: versionID, ActorID: actor}
		if err = r.Insert(projectNoteRecord(projectNoteConfirmKind, requestID, project.Owner, now, request, projectNoteActorKey(actor), "", "", "")); err != nil {
			return err
		}
		return s.projectNoteMarkCurrentVersionConfirmed(r, project, versionID, now)
	})
	return out, err
}

func (s *Service) ProjectPackageAcknowledge(ctx context.Context, token string, actor int64, versionID, eventID string) (ProjectPackageReceipt, error) {
	var out ProjectPackageReceipt
	if !validID(strings.TrimSpace(eventID)) {
		return out, fail(400, "阅读回执标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		view, _, err := s.projectPackageReadIn(ctx, r, token, actor)
		if err != nil {
			return err
		}
		if versionID != "" && (!validID(versionID) || !projectPackageHasVersion(view.Items, versionID)) {
			return fail(404, "固定版本不在本次交付清单")
		}
		if old, readErr := decode[ProjectPackageReceipt](r, projectPackageReceiptKind, eventID); readErr == nil {
			if old.DeliveryID != view.Delivery.ID || old.ActorID != actor || old.VersionID != versionID {
				return fail(409, "阅读回执标识已用于其他资料")
			}
			out = old
			return s.ensureProjectPackageReceiptNotice(ctx, r, view, out)
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		if old, found, readErr := projectPackageReceiptByTuple(r, view.Project.Owner, view.Delivery.ID, view.Project.ID, versionID, actor); readErr != nil {
			return readErr
		} else if found {
			out = old
			return s.ensureProjectPackageReceiptNotice(ctx, r, view, out)
		}
		now := s.Now().UTC()
		out = ProjectPackageReceipt{ID: eventID, DeliveryID: view.Delivery.ID, ProjectID: view.Project.ID, VersionID: versionID, ActorID: actor, AcknowledgedAt: now}
		if err = r.Insert(projectNoteRecord(projectPackageReceiptKind, eventID, view.Project.Owner, now, out, projectNoteActorKey(actor), view.Project.ID, versionID, view.Delivery.ID)); err != nil {
			return err
		}
		return s.ensureProjectPackageReceiptNotice(ctx, r, view, out)
	})
	return out, err
}

func projectPackageReceiptByTuple(r Repository, owner, deliveryID, projectID, versionID string, actor int64) (ProjectPackageReceipt, bool, error) {
	q := RecordQuery{Owners: []string{owner}, Sessions: []string{projectID}, VisitorKeys: []string{projectNoteActorKey(actor)}, Shares: []string{deliveryID}, ExactVisitors: true, Limit: 22}
	if versionID != "" {
		q.Content = versionID
	}
	rows, err := pageRecords(r, projectPackageReceiptKind, q)
	if err != nil {
		return ProjectPackageReceipt{}, false, err
	}
	for _, row := range rows {
		var receipt ProjectPackageReceipt
		if json.Unmarshal(row.Payload, &receipt) != nil {
			return ProjectPackageReceipt{}, false, ErrUnavailable
		}
		if receipt.DeliveryID == deliveryID && receipt.ProjectID == projectID && receipt.VersionID == versionID && receipt.ActorID == actor {
			return receipt, true, nil
		}
	}
	return ProjectPackageReceipt{}, false, nil
}

func projectPackageReceiptNoticeID(receipt ProjectPackageReceipt) string {
	return "package-receipt:" + hash(fmt.Sprintf("%s:%d:%s", receipt.DeliveryID, receipt.ActorID, receipt.VersionID))
}

func (s *Service) ensureProjectPackageReceiptNotice(ctx context.Context, r Repository, view ProjectPackageView, receipt ProjectPackageReceipt) error {
	if receipt.VersionID == "" || receipt.ActorID == view.Project.DesignerID || !s.Config.Enabled {
		return nil
	}
	notice := NoticeEvent{ID: projectPackageReceiptNoticeID(receipt), Kind: ProjectPackageReceiptNoticeKind, Owner: view.Project.Owner, RecipientID: view.Project.DesignerID, VisitorKey: projectNoteActorKey(receipt.ActorID), ShareID: receipt.DeliveryID, ProjectID: receipt.ProjectID, ContentID: receipt.VersionID, Route: ProjectNoteNoticeRoute, OccurredAt: receipt.AcknowledgedAt}
	if _, err := r.Get("notice_event", notice.ID); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	row := record("notice_event", notice.ID, notice.Owner, notice.OccurredAt, notice)
	row.Visitor, row.Content, row.Share = notice.VisitorKey, notice.ContentID, notice.ShareID
	if err := r.Insert(row); err != nil {
		return err
	}
	if s.OnNoticeEvent != nil {
		return s.OnNoticeEvent(sourceReadContext(ctx, r), notice)
	}
	return nil
}

// ProjectPackageGetHistory returns bounded delivery and explicit receipt facts.
// Opening a directory never creates a receipt; only ProjectPackageAcknowledge does.
func (s *Service) ProjectPackageGetHistory(ctx context.Context, scope Scope, projectID string) (ProjectPackageHistory, error) {
	out := ProjectPackageHistory{Deliveries: []ProjectPackageDelivery{}, Receipts: []ProjectPackageReceipt{}}
	if !validID(strings.TrimSpace(projectID)) {
		return out, fail(400, "项目标识无效")
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		rows, err := pageRecords(r, projectPackageDeliveryKind, RecordQuery{Owners: []string{project.Owner}, Sessions: []string{project.ID}, Descending: true, Limit: 51})
		if err != nil {
			return err
		}
		if len(rows) > 50 {
			out.Truncated, rows = true, rows[:50]
		}
		for _, row := range rows {
			var stored projectPackageDeliveryRecord
			if json.Unmarshal(row.Payload, &stored) != nil || stored.ProjectID != project.ID {
				return ErrUnavailable
			}
			delivery := stored.ProjectPackageDelivery
			delivery.Token = ""
			out.Deliveries = append(out.Deliveries, delivery)
		}
		rows, err = pageRecords(r, projectPackageReceiptKind, RecordQuery{Owners: []string{project.Owner}, Sessions: []string{project.ID}, Descending: true, Limit: 51})
		if err != nil {
			return err
		}
		if len(rows) > 50 {
			out.Truncated, rows = true, rows[:50]
		}
		for _, row := range rows {
			var receipt ProjectPackageReceipt
			if json.Unmarshal(row.Payload, &receipt) != nil || receipt.ProjectID != project.ID {
				return ErrUnavailable
			}
			out.Receipts = append(out.Receipts, receipt)
		}
		return nil
	})
	return out, err
}
