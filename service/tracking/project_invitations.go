package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	projectNoteInvitationKind        = "project_note_invitation"
	projectNoteInvitationRequestKind = "project_note_invitation_request"
	projectNoteJoinRequestKind       = "project_note_join_request"

	ProjectNoteJoinPending      = "pending"
	ProjectNoteJoinApproved     = "approved"
	ProjectNoteInvitationActive = "active"

	ProjectNoteJoinRequestNoticeKind = "project_note_join_request"
)

type ProjectNoteInvitation struct {
	ID         string    `json:"invitation_id"`
	ProjectID  string    `json:"project_id"`
	DesignerID int64     `json:"designer_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
	Revoked    bool      `json:"revoked"`
	Token      string    `json:"invitation_token,omitempty"`
	Replayed   bool      `json:"replayed,omitempty"`
}

type projectNoteInvitationRecord struct {
	ProjectNoteInvitation
	TokenHash string `json:"token_hash"`
}

type projectNoteInvitationCreateRequest struct {
	InvitationRecordID string    `json:"invitation_record_id"`
	ProjectID          string    `json:"project_id"`
	ExpiresAt          time.Time `json:"expires_at"`
	Token              string    `json:"invitation_token"`
}

type ProjectNoteInvitationPreview struct {
	ProjectID   string    `json:"-"`
	DesignerID  int64     `json:"-"`
	ProjectName string    `json:"project_name"`
	ExpiresAt   time.Time `json:"expires_at"`
	Status      string    `json:"request_status,omitempty"`
}

type ProjectNoteInvitationSummary struct {
	ID        string    `json:"invitation_id"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

type ProjectNoteJoinRequest struct {
	ID              string    `json:"request_id"`
	InvitationID    string    `json:"invitation_id"`
	ProjectID       string    `json:"project_id"`
	ActorID         int64     `json:"actor_id"`
	Status          string    `json:"status"`
	RequestedAt     time.Time `json:"requested_at"`
	ApprovedAt      time.Time `json:"approved_at,omitempty"`
	ApprovedBy      int64     `json:"approved_by,omitempty"`
	FirstKeyHash    string    `json:"first_key_hash"`
	ApprovalKeyHash string    `json:"approval_key_hash,omitempty"`
	Replayed        bool      `json:"replayed,omitempty"`
}

func projectNoteJoinRequestID(invitationID string, actor int64) string {
	return hash(fmt.Sprintf("project-note-join:%s:%d", invitationID, actor))
}

func projectNoteJoinNoticeID(requestID string) string {
	return "project-join:" + requestID
}

func (s *Service) projectNoteJoinNoticeEligible(ctx context.Context, r Repository, notice NoticeEvent, recipient int64) (bool, error) {
	request, err := decode[ProjectNoteJoinRequest](r, projectNoteJoinRequestKind, notice.ContentID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if notice.ID != projectNoteJoinNoticeID(request.ID) || request.Status != ProjectNoteJoinPending || request.ProjectID != notice.ProjectID || request.InvitationID != notice.ShareID || projectNoteActorKey(request.ActorID) != notice.VisitorKey {
		return false, nil
	}
	project, err := projectNoteLoadProject(r, request.ProjectID)
	if err != nil {
		var missing *Error
		if errors.As(err, &missing) && missing.Status == 404 {
			return false, nil
		}
		return false, err
	}
	if project.Owner != notice.Owner || project.DesignerID != recipient {
		return false, nil
	}
	rows, err := pageRecords(r, projectNoteInvitationKind, RecordQuery{Owners: []string{project.Owner}, Sessions: []string{project.ID}, Shares: []string{request.InvitationID}, Limit: 2})
	if err != nil {
		return false, err
	}
	if len(rows) != 1 {
		return false, nil
	}
	invitation, err := decode[projectNoteInvitationRecord](r, projectNoteInvitationKind, rows[0].ID)
	if err != nil {
		return false, err
	}
	if invitation.Revoked || !s.Now().Before(invitation.ExpiresAt) {
		return false, nil
	}
	if err = s.projectNoteGrant(ctx, r, project); err != nil {
		var denied *Error
		if errors.As(err, &denied) && (denied.Status == 401 || denied.Status == 403 || denied.Status == 404) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Service) projectNoteInvitationByToken(r Repository, token string) (projectNoteInvitationRecord, error) {
	var invitation projectNoteInvitationRecord
	if !validID(token) {
		return invitation, fail(404, "邀请不存在或已失效")
	}
	invitation, err := decode[projectNoteInvitationRecord](r, projectNoteInvitationKind, hash(token))
	if errors.Is(err, ErrNotFound) {
		return invitation, fail(404, "邀请不存在或已失效")
	}
	if err != nil {
		return invitation, err
	}
	if invitation.TokenHash != hash(token) || invitation.Revoked || !s.Now().Before(invitation.ExpiresAt) {
		return invitation, fail(404, "邀请不存在或已失效")
	}
	return invitation, nil
}

func (s *Service) ProjectNoteCreateInvitation(ctx context.Context, scope Scope, projectID string, expiresAt time.Time, key string) (ProjectNoteInvitation, error) {
	var out ProjectNoteInvitation
	if !validID(key) || expiresAt.IsZero() {
		return out, fail(400, "邀请有效期或幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		requestID := "request:" + hash(fmt.Sprintf("%d:%s:%s", scope.Actor, projectID, key))
		if request, readErr := decode[projectNoteInvitationCreateRequest](r, projectNoteInvitationRequestKind, requestID); readErr == nil {
			if request.ProjectID != projectID || !request.ExpiresAt.Equal(expiresAt.UTC()) {
				return fail(409, "邀请幂等标识已用于其他请求")
			}
			stored, getErr := decode[projectNoteInvitationRecord](r, projectNoteInvitationKind, request.InvitationRecordID)
			if getErr != nil {
				return getErr
			}
			out = stored.ProjectNoteInvitation
			out.Token, out.Replayed = request.Token, true
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		if !expiresAt.After(now) || expiresAt.After(now.Add(7*24*time.Hour)) {
			return fail(400, "邀请有效期无效")
		}
		token := opaque()
		recordID := hash(token)
		stored := projectNoteInvitationRecord{
			ProjectNoteInvitation: ProjectNoteInvitation{ID: opaque(), ProjectID: project.ID, DesignerID: project.DesignerID, ExpiresAt: expiresAt.UTC(), CreatedAt: now},
			TokenHash:             recordID,
		}
		if err = r.Insert(projectNoteRecord(projectNoteInvitationKind, recordID, project.Owner, now, stored, projectNoteActorKey(scope.Actor), project.ID, project.ID, stored.ID)); err != nil {
			return err
		}
		request := projectNoteInvitationCreateRequest{InvitationRecordID: recordID, ProjectID: projectID, ExpiresAt: expiresAt.UTC(), Token: token}
		if err = r.Insert(projectNoteRecord(projectNoteInvitationRequestKind, requestID, project.Owner, now, request, projectNoteActorKey(scope.Actor), project.ID, project.ID, stored.ID)); err != nil {
			return err
		}
		out = stored.ProjectNoteInvitation
		out.Token = token
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteListInvitations(ctx context.Context, scope Scope, projectID string) ([]ProjectNoteInvitationSummary, error) {
	out := []ProjectNoteInvitationSummary{}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		rows, err := pageRecords(r, projectNoteInvitationKind, RecordQuery{Owners: []string{project.Owner}, Sessions: []string{project.ID}, Descending: true, Limit: 100})
		if err != nil {
			return err
		}
		now := s.Now()
		for _, row := range rows {
			var invitation projectNoteInvitationRecord
			if err = json.Unmarshal(row.Payload, &invitation); err != nil {
				return ErrUnavailable
			}
			if invitation.ProjectID == project.ID && !invitation.Revoked && now.Before(invitation.ExpiresAt) {
				out = append(out, ProjectNoteInvitationSummary{ID: invitation.ID, Status: ProjectNoteInvitationActive, ExpiresAt: invitation.ExpiresAt})
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteReadInvitation(ctx context.Context, token string, actor int64) (ProjectNoteInvitationPreview, error) {
	var out ProjectNoteInvitationPreview
	if actor <= 0 {
		return out, fail(401, "请先登录")
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		invitation, err := s.projectNoteInvitationByToken(r, token)
		if err != nil {
			return err
		}
		project, err := projectNoteLoadProject(r, invitation.ProjectID)
		if err != nil {
			return err
		}
		if err = s.projectNoteGrant(ctx, r, project); err != nil {
			return err
		}
		out = ProjectNoteInvitationPreview{ProjectName: project.Name, ExpiresAt: invitation.ExpiresAt, DesignerID: project.DesignerID}
		request, readErr := decode[ProjectNoteJoinRequest](r, projectNoteJoinRequestKind, projectNoteJoinRequestID(invitation.ID, actor))
		if readErr == nil {
			if request.Status == ProjectNoteJoinApproved {
				if _, memberErr := projectNoteLoadMember(r, project.ID, actor); memberErr != nil {
					return memberErr
				}
			}
			out.Status = request.Status
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteApplyInvitation(ctx context.Context, token string, actor int64, key string) (ProjectNoteJoinRequest, error) {
	var out ProjectNoteJoinRequest
	if actor <= 0 || !validID(key) {
		return out, fail(400, "申请账号或幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		invitation, err := s.projectNoteInvitationByToken(r, token)
		if err != nil {
			return err
		}
		project, err := projectNoteLoadProject(r, invitation.ProjectID)
		if err != nil {
			return err
		}
		if err = s.projectNoteGrant(ctx, r, project); err != nil {
			return err
		}
		id := projectNoteJoinRequestID(invitation.ID, actor)
		if old, readErr := decode[ProjectNoteJoinRequest](r, projectNoteJoinRequestKind, id); readErr == nil {
			if old.FirstKeyHash != hash(key) {
				return fail(409, "加入申请已提交")
			}
			old.Replayed = true
			out = old
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		// 邀请链接本身就是所有者发出的准入凭据：受邀人登录后直接成为成员，不再二次审批。
		out = ProjectNoteJoinRequest{ID: id, InvitationID: invitation.ID, ProjectID: project.ID, ActorID: actor, Status: ProjectNoteJoinApproved, RequestedAt: now, ApprovedAt: now, ApprovedBy: project.DesignerID, FirstKeyHash: hash(key), ApprovalKeyHash: hash(key)}
		if err = r.Insert(projectNoteRecord(projectNoteJoinRequestKind, id, project.Owner, now, out, projectNoteActorKey(actor), project.ID, id, invitation.ID)); err != nil {
			return err
		}
		if _, err = s.projectNoteJoinMemberIn(r, project, actor, invitation.DesignerID, invitation.ID); err != nil {
			return err
		}
		return nil
	})
	return out, err
}

// ProjectNoteRequestShareAccess 允许持有有效方案分享链接的非成员发起加入申请；
// 所有者批准后即可查看。分享链接本身仍不能直接放行，避免转发链接绕过审批。
func (s *Service) ProjectNoteRequestShareAccess(ctx context.Context, token string, actor int64, key string) (ProjectNoteJoinRequest, error) {
	var out ProjectNoteJoinRequest
	if actor <= 0 || !validID(key) {
		return out, fail(400, "申请账号或幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		if !validID(token) {
			return fail(404, "分享不存在或已失效")
		}
		share, err := decode[projectNoteShareRecord](r, projectNoteShareKind, hash(token))
		if errors.Is(err, ErrNotFound) {
			return fail(404, "分享不存在或已失效")
		}
		if err != nil {
			return err
		}
		if share.TokenHash != hash(token) || share.Revoked || !s.Now().Before(share.ExpiresAt) {
			return fail(404, "分享不存在或已失效")
		}
		project, err := projectNoteLoadProject(r, share.ProjectID)
		if err != nil {
			return err
		}
		id := projectNoteJoinRequestID(share.ID, actor)
		if old, readErr := decode[ProjectNoteJoinRequest](r, projectNoteJoinRequestKind, id); readErr == nil {
			if old.FirstKeyHash != hash(key) {
				return fail(409, "加入申请已提交")
			}
			old.Replayed = true
			out = old
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		out = ProjectNoteJoinRequest{ID: id, InvitationID: share.ID, ProjectID: project.ID, ActorID: actor, Status: ProjectNoteJoinPending, RequestedAt: now, FirstKeyHash: hash(key)}
		if err = r.Insert(projectNoteRecord(projectNoteJoinRequestKind, id, project.Owner, now, out, projectNoteActorKey(actor), project.ID, id, share.ID)); err != nil {
			return err
		}
		if !s.Config.Enabled || actor == project.DesignerID {
			return nil
		}
		notice := NoticeEvent{ID: projectNoteJoinNoticeID(id), Kind: ProjectNoteJoinRequestNoticeKind, Owner: project.Owner, RecipientID: project.DesignerID, VisitorKey: projectNoteActorKey(actor), ShareID: share.ID, ProjectID: project.ID, ContentID: id, Route: ProjectNoteNoticeRoute, OccurredAt: now}
		noticeRow := record("notice_event", notice.ID, notice.Owner, now, notice)
		noticeRow.Visitor, noticeRow.Content, noticeRow.Share = notice.VisitorKey, notice.ContentID, notice.ShareID
		if err = r.Insert(noticeRow); err != nil {
			return err
		}
		if s.OnNoticeEvent != nil {
			return s.OnNoticeEvent(sourceReadContext(ctx, r), notice)
		}
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteListJoinRequests(ctx context.Context, scope Scope, projectID string) ([]ProjectNoteJoinRequest, error) {
	out := []ProjectNoteJoinRequest{}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		if _, err := s.projectNoteManaged(r, scope, projectID); err != nil {
			return err
		}
		rows, err := pageRecords(r, projectNoteJoinRequestKind, RecordQuery{Sessions: []string{projectID}, Descending: true, Limit: 100})
		if err != nil {
			return err
		}
		for _, row := range rows {
			var request ProjectNoteJoinRequest
			if json.Unmarshal(row.Payload, &request) != nil {
				return ErrUnavailable
			}
			if request.ProjectID == projectID {
				out = append(out, request)
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteGetJoinRequest(ctx context.Context, scope Scope, projectID, requestID string) (ProjectNoteJoinRequest, error) {
	var out ProjectNoteJoinRequest
	if !validID(requestID) {
		return out, fail(404, "加入申请不存在")
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		out, err = projectNoteRequired[ProjectNoteJoinRequest](r, projectNoteJoinRequestKind, requestID, "加入申请不存在")
		if err != nil {
			return err
		}
		if out.ProjectID != project.ID {
			return fail(404, "加入申请不存在")
		}
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteApproveJoinRequest(ctx context.Context, scope Scope, projectID, requestID, key string) (ProjectNoteJoinRequest, error) {
	var out ProjectNoteJoinRequest
	if !validID(requestID) || !validID(key) {
		return out, fail(400, "申请或幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		request, err := projectNoteRequired[ProjectNoteJoinRequest](r, projectNoteJoinRequestKind, requestID, "加入申请不存在")
		if err != nil {
			return err
		}
		if request.ProjectID != project.ID {
			return fail(404, "加入申请不存在")
		}
		if request.Status == ProjectNoteJoinApproved {
			if request.ApprovalKeyHash != hash(key) {
				return fail(409, "加入申请已审批")
			}
			request.Replayed = true
			out = request
			return nil
		}
		invitationRows, err := pageRecords(r, projectNoteInvitationKind, RecordQuery{Owners: []string{project.Owner}, Sessions: []string{project.ID}, Shares: []string{request.InvitationID}, Limit: 2})
		if err != nil {
			return err
		}
		switch len(invitationRows) {
		case 1:
			invitation, decodeErr := decode[projectNoteInvitationRecord](r, projectNoteInvitationKind, invitationRows[0].ID)
			if decodeErr != nil {
				return decodeErr
			}
			if invitation.Revoked || !s.Now().Before(invitation.ExpiresAt) {
				return fail(409, "邀请已失效，请重新邀请")
			}
		case 0:
			// 方案分享链接发起的加入申请：链接有效即允许所有者批准。
			_, shared, shareErr := projectNoteShareByID(r, project.Owner, request.InvitationID)
			if shareErr != nil {
				return fail(404, "邀请不存在或已失效")
			}
			if shared.Revoked || !s.Now().Before(shared.ExpiresAt) {
				return fail(409, "分享已失效，请重新分享")
			}
		default:
			return fail(404, "邀请不存在或已失效")
		}
		if _, err = s.projectNoteJoinMemberIn(r, project, request.ActorID, project.DesignerID, request.InvitationID); err != nil {
			return err
		}
		request.Status = ProjectNoteJoinApproved
		request.ApprovedAt = s.Now().UTC()
		request.ApprovedBy = scope.Actor
		request.ApprovalKeyHash = hash(key)
		if err = r.Put(projectNoteRecord(projectNoteJoinRequestKind, request.ID, project.Owner, request.ApprovedAt, request, projectNoteActorKey(request.ActorID), project.ID, request.ID, request.InvitationID)); err != nil {
			return err
		}
		out = request
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteRevokeInvitation(ctx context.Context, scope Scope, projectID, invitationID string) error {
	if !validID(invitationID) {
		return fail(400, "邀请标识无效")
	}
	return s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		rows, err := pageRecords(r, projectNoteInvitationKind, RecordQuery{Owners: []string{project.Owner}, Sessions: []string{project.ID}, Shares: []string{invitationID}, Limit: 2})
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			return fail(404, "邀请不存在")
		}
		invitation, err := decode[projectNoteInvitationRecord](r, projectNoteInvitationKind, rows[0].ID)
		if err != nil {
			return err
		}
		if invitation.Revoked {
			return nil
		}
		invitation.Revoked = true
		return r.Put(projectNoteRecord(projectNoteInvitationKind, rows[0].ID, project.Owner, s.Now().UTC(), invitation, rows[0].Visitor, rows[0].Session, rows[0].Content, rows[0].Share))
	})
}
