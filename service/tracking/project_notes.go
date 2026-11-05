package tracking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	projectNoteProjectKind            = "note_project"
	projectNoteMemberKind             = "note_member"
	projectNoteKind                   = "note"
	projectNoteVersionKind            = "note_version"
	projectNoteShareKind              = "note_share"
	projectNoteFeedbackKind           = "note_feedback"
	projectNoteFeedbackResolutionKind = "note_feedback_resolution"
	projectNoteConfirmKind            = "note_confirm"
	projectNoteOpenKind               = "note_open"
	ProjectNoteNoticeRoute            = "project_note_share"
	ProjectNoteFeedbackNoticeKind     = "project_note_feedback"
	projectNoteFeedbackNoticePrefix   = "feedback:"
	ProjectNoteProjectStatusInitial   = "initial"
	ProjectNoteProjectStatusRevising  = "revising"
	ProjectNoteProjectStatusConfirmed = "confirmed"

	ProjectNoteSatisfactionVerySatisfied   = "very_satisfied"
	ProjectNoteSatisfactionNeedsAdjustment = "needs_adjustment"
	ProjectNoteSatisfactionUnsatisfied     = "unsatisfied"
)

type ProjectNoteProjectInput struct {
	Name         string  `json:"name"`
	Owner        string  `json:"owner_id"`
	CRMID        string  `json:"crm_id,omitempty"`
	Location     string  `json:"location,omitempty"`
	BuildingArea float64 `json:"building_area,omitempty"`
	Floors       int     `json:"floors,omitempty"`
	Style        string  `json:"style,omitempty"`
}

type ProjectNoteProjectUpdate struct {
	ExpectedRevision int      `json:"expected_revision"`
	Name             *string  `json:"name,omitempty"`
	Location         *string  `json:"location,omitempty"`
	BuildingArea     *float64 `json:"building_area,omitempty"`
	Floors           *int     `json:"floors,omitempty"`
	Style            *string  `json:"style,omitempty"`
	Status           *string  `json:"status,omitempty"`
}

type ProjectNoteProject struct {
	ID                 string    `json:"project_id"`
	Name               string    `json:"name"`
	Owner              string    `json:"owner_id"`
	CRMID              string    `json:"crm_id,omitempty"`
	Location           string    `json:"location,omitempty"`
	BuildingArea       float64   `json:"building_area,omitempty"`
	Floors             int       `json:"floors,omitempty"`
	Style              string    `json:"style,omitempty"`
	Status             string    `json:"status"`
	CurrentVersionID   string    `json:"current_version_id,omitempty"`
	DesignerID         int64     `json:"designer_id"`
	CreatorDisplayName string    `json:"creator_display_name,omitempty"`
	ConfirmerID        int64     `json:"confirmer_id,omitempty"`
	Revision           int       `json:"revision"`
	Stage              int       `json:"stage,omitempty"`
	NextAction         string    `json:"next_action,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ProjectNoteProjectPage struct {
	Items      []ProjectNoteProject `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type ProjectNoteVersionSummary struct {
	ID         string    `json:"version_id"`
	NoteID     string    `json:"note_id"`
	Revision   int       `json:"revision"`
	Title      string    `json:"title"`
	FrozenAt   time.Time `json:"frozen_at"`
	MediaCount int       `json:"media_count"`
}

type ProjectNoteDetailTruncated struct {
	Members       bool `json:"members"`
	Notes         bool `json:"notes"`
	Versions      bool `json:"versions"`
	Shares        bool `json:"shares"`
	Feedbacks     bool `json:"feedbacks"`
	Resolutions   bool `json:"resolutions"`
	Confirmations bool `json:"confirmations"`
}

type ProjectNoteProjectDetail struct {
	Project       ProjectNoteProject              `json:"project"`
	Members       []ProjectNoteMember             `json:"members"`
	Notes         []ProjectNote                   `json:"notes"`
	Versions      []ProjectNoteVersionSummary     `json:"versions"`
	Shares        []ProjectNoteShare              `json:"shares"`
	Feedbacks     []ProjectNoteFeedback           `json:"feedbacks"`
	Resolutions   []ProjectNoteFeedbackResolution `json:"resolutions"`
	Confirmations []ProjectNoteConfirmation       `json:"confirmations"`
	Truncated     ProjectNoteDetailTruncated      `json:"truncated"`
}

type ProjectNoteMember struct {
	ID        string    `json:"member_id"`
	ProjectID string    `json:"project_id"`
	ActorID   int64     `json:"actor_id"`
	Active    bool      `json:"active"`
	InvitedAt time.Time `json:"invited_at"`
	RevokedAt time.Time `json:"revoked_at,omitempty"`
}

type ProjectNoteDraft struct {
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	BodyFormat string   `json:"body_format"`
	CoverID    string   `json:"cover_media_id,omitempty"`
	MediaIDs   []string `json:"media_ids,omitempty"`
}

type ProjectNote struct {
	ID         string    `json:"note_id"`
	ProjectID  string    `json:"project_id"`
	Revision   int       `json:"revision"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	BodyFormat string    `json:"body_format"`
	CoverID    string    `json:"cover_media_id,omitempty"`
	MediaIDs   []string  `json:"media_ids,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ProjectNoteVersion struct {
	ID              string                     `json:"version_id"`
	ProjectID       string                     `json:"project_id"`
	NoteID          string                     `json:"note_id"`
	Revision        int                        `json:"revision"`
	Title           string                     `json:"title"`
	Body            string                     `json:"body"`
	BodyFormat      string                     `json:"body_format"`
	ProjectSnapshot ProjectNoteProjectSnapshot `json:"project_snapshot"`
	Cover           *Asset                     `json:"cover,omitempty"`
	Media           []Asset                    `json:"media"`
	FrozenAt        time.Time                  `json:"frozen_at"`
}

type ProjectNoteProjectSnapshot struct {
	Name         string  `json:"name"`
	Location     string  `json:"location,omitempty"`
	BuildingArea float64 `json:"building_area,omitempty"`
	Floors       int     `json:"floors,omitempty"`
	Style        string  `json:"style,omitempty"`
	Revision     int     `json:"revision"`
}

type ProjectNoteShare struct {
	ID        string    `json:"share_id"`
	ProjectID string    `json:"project_id"`
	VersionID string    `json:"version_id"`
	SharerID  int64     `json:"sharer_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
	Token     string    `json:"share_token,omitempty"`
	Replayed  bool      `json:"replayed,omitempty"`
}

type projectNoteShareRecord struct {
	ProjectNoteShare
	TokenHash string `json:"token_hash"`
}

type projectNoteShareRequest struct {
	ShareRecordID string    `json:"share_record_id"`
	VersionID     string    `json:"version_id"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type ProjectNoteShareView struct {
	Project           ProjectNoteProject              `json:"project"`
	Share             ProjectNoteShare                `json:"share"`
	Version           ProjectNoteVersion              `json:"version"`
	Feedbacks         []ProjectNoteFeedback           `json:"feedbacks"`
	Resolutions       []ProjectNoteFeedbackResolution `json:"resolutions"`
	Confirmation      *ProjectNoteConfirmation        `json:"confirmation,omitempty"`
	CanConfirm        bool                            `json:"can_confirm"`
	ActivityTruncated bool                            `json:"activity_truncated"`
}

type ProjectNoteFeedback struct {
	ID           string    `json:"feedback_id"`
	ProjectID    string    `json:"project_id"`
	VersionID    string    `json:"version_id"`
	ShareID      string    `json:"share_id"`
	ActorID      int64     `json:"actor_id"`
	Text         string    `json:"text"`
	Satisfaction string    `json:"satisfaction,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	Resolved     bool      `json:"resolved"`
	ResolvedAt   time.Time `json:"resolved_at,omitempty"`
	Reply        string    `json:"reply,omitempty"`
}

type ProjectNoteFeedbackResolution struct {
	ID         string    `json:"resolution_id"`
	FeedbackID string    `json:"feedback_id"`
	DesignerID int64     `json:"designer_id"`
	Reply      string    `json:"reply"`
	ResolvedAt time.Time `json:"resolved_at"`
}

type ProjectNoteConfirmation struct {
	ID          string    `json:"confirmation_id"`
	ProjectID   string    `json:"project_id"`
	VersionID   string    `json:"version_id"`
	ConfirmerID int64     `json:"confirmer_id"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

type projectNoteConfirmationRecord struct {
	ProjectNoteConfirmation
	FirstKeyHash string `json:"first_key_hash"`
}

type projectNoteConfirmationRequest struct {
	ConfirmationID string `json:"confirmation_id"`
	ProjectID      string `json:"project_id"`
	VersionID      string `json:"version_id"`
	ActorID        int64  `json:"actor_id"`
}

type ProjectNoteOpenFact struct {
	ID             string    `json:"event_id"`
	ProjectID      string    `json:"project_id"`
	NoteID         string    `json:"note_id"`
	VersionID      string    `json:"version_id"`
	ShareID        string    `json:"share_id"`
	ActorID        int64     `json:"actor_id"`
	ActualSharerID int64     `json:"actual_sharer_id"`
	Notify         bool      `json:"notify"`
	OpenedAt       time.Time `json:"opened_at"`
}

type projectNoteCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func projectNoteActorKey(actor int64) string { return "u:" + strconv.FormatInt(actor, 10) }

func projectNoteFeedbackNoticeID(feedbackID string) string {
	return projectNoteFeedbackNoticePrefix + feedbackID
}

func projectNoteFeedbackIDFromNotice(noticeID string) (string, bool) {
	if !strings.HasPrefix(noticeID, projectNoteFeedbackNoticePrefix) {
		return "", false
	}
	feedbackID := strings.TrimPrefix(noticeID, projectNoteFeedbackNoticePrefix)
	return feedbackID, validID(feedbackID)
}

func projectNoteMemberID(projectID string, actor int64) string {
	return hash(fmt.Sprintf("project-note-member:%s:%d", projectID, actor))
}

func projectNoteRecord(kind, id, owner string, at time.Time, value any, visitor, session, content, share string) Record {
	row := record(kind, id, owner, at, value)
	row.Visitor, row.Session, row.Content, row.Share = visitor, session, content, share
	return row
}

func (s *Service) projectNoteDesigner(scope Scope, project ProjectNoteProject) error {
	if err := s.authorized(scope); err != nil || !scope.Manage || !scope.Allows(project.Owner) || scope.Actor != project.DesignerID {
		return fail(404, "私人项目不可访问")
	}
	return nil
}

func (s *Service) projectNoteGrant(ctx context.Context, r Repository, project ProjectNoteProject) error {
	if s.IsContentOperator == nil {
		return fail(503, "设计师权限校验不可用")
	}
	granted, err := s.IsContentOperator(sourceReadContext(ctx, r), project.DesignerID, project.Owner)
	if err != nil {
		return err
	}
	if !granted {
		return fail(404, "私人项目不可访问")
	}
	return nil
}

func projectNoteLoadProjectRaw(r Repository, projectID string) (ProjectNoteProject, error) {
	project, err := decode[ProjectNoteProject](r, projectNoteProjectKind, projectID)
	if errors.Is(err, ErrNotFound) {
		return project, fail(404, "私人项目不可访问")
	}
	if err != nil {
		return project, err
	}
	return projectNoteNormalizeProject(project), nil
}

func projectNoteLoadProject(r Repository, projectID string) (ProjectNoteProject, error) {
	project, err := projectNoteLoadProjectRaw(r, projectID)
	if err != nil {
		return project, err
	}
	if err = projectNoteRequireProjectActive(r, projectID); err != nil {
		return project, err
	}
	return project, nil
}

func projectNoteNormalizeProject(project ProjectNoteProject) ProjectNoteProject {
	if project.Status == "" {
		project.Status = ProjectNoteProjectStatusInitial
	}
	project.Stage, project.NextAction = projectNoteStage(project)
	return project
}

func projectNoteCurrentVersion(r Repository, project ProjectNoteProject) (ProjectNoteVersion, bool, error) {
	versionID := project.CurrentVersionID
	if versionID == "" {
		rows, err := pageRecords(r, projectNoteVersionKind, RecordQuery{Sessions: []string{project.ID}, Descending: true, Limit: 1})
		if err != nil {
			return ProjectNoteVersion{}, false, err
		}
		if len(rows) == 0 {
			return ProjectNoteVersion{}, false, nil
		}
		versionID = rows[0].ID
	}
	version, err := decode[ProjectNoteVersion](r, projectNoteVersionKind, versionID)
	if errors.Is(err, ErrNotFound) {
		return ProjectNoteVersion{}, false, ErrUnavailable
	}
	if err != nil {
		return ProjectNoteVersion{}, false, err
	}
	if version.ProjectID != project.ID {
		return ProjectNoteVersion{}, false, ErrUnavailable
	}
	return version, true, nil
}

func projectNoteVersionDrivesStatus(r Repository, project ProjectNoteProject, versionID string) (ProjectNoteVersion, bool, error) {
	current, ok, err := projectNoteCurrentVersion(r, project)
	if err != nil || !ok || current.ID != versionID {
		return current, false, err
	}
	note, err := decode[ProjectNote](r, projectNoteKind, current.NoteID)
	if errors.Is(err, ErrNotFound) {
		return current, false, ErrUnavailable
	}
	if err != nil {
		return current, false, err
	}
	return current, note.ProjectID == project.ID && note.Revision == current.Revision, nil
}

func projectNoteLoadMember(r Repository, projectID string, actor int64) (ProjectNoteMember, error) {
	member, err := decode[ProjectNoteMember](r, projectNoteMemberKind, projectNoteMemberID(projectID, actor))
	if errors.Is(err, ErrNotFound) {
		return ProjectNoteMember{}, fail(403, "仅受邀账号可访问")
	}
	if err != nil {
		return ProjectNoteMember{}, err
	}
	if member.ProjectID != projectID || member.ActorID != actor || !member.Active {
		return ProjectNoteMember{}, fail(403, "仅受邀账号可访问")
	}
	return member, nil
}

func (s *Service) projectNoteManaged(r Repository, scope Scope, projectID string) (ProjectNoteProject, error) {
	project, err := projectNoteLoadProject(r, projectID)
	if err != nil {
		return project, err
	}
	return project, s.projectNoteDesigner(scope, project)
}

func projectNoteRequired[T any](r Repository, kind, id, message string) (T, error) {
	value, err := decode[T](r, kind, id)
	if errors.Is(err, ErrNotFound) {
		return value, fail(404, message)
	}
	return value, err
}

func (s *Service) projectNoteReadAccess(ctx context.Context, r Repository, project ProjectNoteProject, actor int64) error {
	if actor <= 0 {
		return fail(401, "请先登录")
	}
	if err := s.projectNoteGrant(ctx, r, project); err != nil {
		return err
	}
	if actor == project.DesignerID {
		return nil
	}
	_, err := projectNoteLoadMember(r, project.ID, actor)
	return err
}

func projectNoteValidArea(area float64) bool {
	return !math.IsNaN(area) && !math.IsInf(area, 0) && area >= 0 && area <= 10000000
}

func projectNoteValidMetadata(name, location, style string, area float64, floors int) bool {
	return name != "" && len(name) <= 100 && len(location) <= 200 && len(style) <= 100 && projectNoteValidArea(area) && floors >= 0 && floors <= 1000
}

func projectNoteEditableStatus(status string) bool {
	return status == ProjectNoteProjectStatusInitial || status == ProjectNoteProjectStatusRevising
}

// projectNoteStatusTransition 归档状态机（仿真稿步骤⑥）：
// 任何非归档状态可归档；归档后仅能取消归档回到修改中；
// confirmed 只能由指定业主确认产生，也仅随归档动作离开（保留业主确认语义）。
func projectNoteStatusTransition(from, to string) bool {
	if from == to {
		return true
	}
	if from == ProjectNoteProjectStatusArchived {
		return to == ProjectNoteProjectStatusRevising
	}
	return to == ProjectNoteProjectStatusRevising || to == ProjectNoteProjectStatusArchived || to == ProjectNoteProjectStatusInitial
}

// ProjectNoteProjectStatusArchived 归档态：一户一档的终态（仿真稿步骤⑥）。
// 任何状态可归档；归档后可取消归档回到修改中继续跟进。
const ProjectNoteProjectStatusArchived = "archived"

// projectNoteStage 甲第工作流阶段推导（纯函数，基于项目现有字段）：
// ①②由素材关联推断（批次5 挂档后回写），这里先按 版本/状态 推导 ③-⑥。
// 返回 stage（2-6）与下一步动作文案。
func projectNoteStage(p ProjectNoteProject) (int, string) {
	switch p.Status {
	case ProjectNoteProjectStatusArchived:
		return 6, "已归档"
	case ProjectNoteProjectStatusConfirmed:
		return 5, "等客户反馈，可先归档交流记录"
	}
	if p.CurrentVersionID != "" {
		return 4, "转发给客户，开始追踪"
	}
	return 2, "生成方案与设计说明"
}

func (s *Service) ProjectNoteCreateProject(ctx context.Context, scope Scope, input ProjectNoteProjectInput) (ProjectNoteProject, error) {
	var out ProjectNoteProject
	input.Name, input.Owner, input.CRMID = strings.TrimSpace(input.Name), strings.TrimSpace(input.Owner), strings.TrimSpace(input.CRMID)
	input.Location, input.Style = strings.TrimSpace(input.Location), strings.TrimSpace(input.Style)
	if err := s.authorized(scope); err != nil {
		return out, err
	}
	if !scope.Manage || !scope.Allows(input.Owner) || len(input.CRMID) > 128 || !projectNoteValidMetadata(input.Name, input.Location, input.Style, input.BuildingArea, input.Floors) {
		return out, fail(400, "私人项目名称或归属无效")
	}
	now := s.Now().UTC()
	out = ProjectNoteProject{
		ID: opaque(), Name: input.Name, Owner: input.Owner, CRMID: input.CRMID,
		Location: input.Location, BuildingArea: input.BuildingArea, Floors: input.Floors, Style: input.Style,
		Status: ProjectNoteProjectStatusInitial, DesignerID: scope.Actor, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	out.Stage, out.NextAction = projectNoteStage(out)
	err := s.transaction(ctx, func(r Repository) error {
		row := projectNoteRecord(projectNoteProjectKind, out.ID, out.Owner, now, out, projectNoteActorKey(scope.Actor), "", out.ID, "")
		return r.Insert(row)
	})
	return out, err
}

func (s *Service) ProjectNoteUpdateProject(ctx context.Context, scope Scope, projectID string, input ProjectNoteProjectUpdate) (ProjectNoteProject, error) {
	var out ProjectNoteProject
	err := s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		if project.Revision != input.ExpectedRevision {
			return fail(409, "项目资料已更新，请重新读取")
		}
		if input.Name != nil {
			project.Name = strings.TrimSpace(*input.Name)
		}
		if input.Location != nil {
			project.Location = strings.TrimSpace(*input.Location)
		}
		if input.BuildingArea != nil {
			project.BuildingArea = *input.BuildingArea
		}
		if input.Floors != nil {
			project.Floors = *input.Floors
		}
		if input.Style != nil {
			project.Style = strings.TrimSpace(*input.Style)
		}
		if input.Status != nil {
			target := strings.TrimSpace(*input.Status)
			if !projectNoteStatusTransition(project.Status, target) {
				return fail(400, "项目状态无效；确认状态仅能由指定业主操作产生，归档后仅能取消归档")
			}
			project.Status = target
		}
		if !projectNoteValidMetadata(project.Name, project.Location, project.Style, project.BuildingArea, project.Floors) {
			return fail(400, "项目结构资料无效")
		}
		project.Revision++
		project.UpdatedAt = s.Now().UTC()
		out = projectNoteNormalizeProject(project)
		return r.Put(projectNoteRecord(projectNoteProjectKind, project.ID, project.Owner, project.UpdatedAt, project, projectNoteActorKey(project.DesignerID), "", project.ID, ""))
	})
	return out, err
}

func (s *Service) ProjectNoteListProjects(ctx context.Context, scope Scope, limit int, cursor string, statusFilter string) (ProjectNoteProjectPage, error) {
	out := ProjectNoteProjectPage{Items: []ProjectNoteProject{}}
	if err := s.authorized(scope); err != nil {
		return out, err
	}
	if !scope.Manage {
		return out, fail(403, "无私人项目管理权限")
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	query := RecordQuery{Owners: scope.Owners, VisitorKeys: []string{projectNoteActorKey(scope.Actor)}, ExactVisitors: true, Descending: true, Limit: limit + 1}
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		var c projectNoteCursor
		if err != nil || json.Unmarshal(decoded, &c) != nil || c.ID == "" || c.At.IsZero() {
			return out, fail(400, "分页游标无效")
		}
		query.BeforeAt, query.BeforeID = c.At, c.ID
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		rows, err := pageRecords(r, projectNoteProjectKind, query)
		if err != nil {
			return err
		}
		if len(rows) > limit {
			last := rows[limit-1]
			encoded, _ := json.Marshal(projectNoteCursor{At: last.At, ID: last.ID})
			out.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
			rows = rows[:limit]
		}
		for _, row := range rows {
			var project ProjectNoteProject
			if json.Unmarshal(row.Payload, &project) != nil || project.DesignerID != scope.Actor {
				return ErrUnavailable
			}
			normalized := projectNoteNormalizeProject(project)
			if statusFilter != "" && normalized.Status != statusFilter {
				continue
			}
			out.Items = append(out.Items, normalized)
		}
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteGetProject(ctx context.Context, scope Scope, projectID string) (ProjectNoteProject, error) {
	var out ProjectNoteProject
	err := s.readTransaction(ctx, func(r Repository) error {
		project, err := projectNoteLoadProject(r, projectID)
		if err != nil {
			return err
		}
		if err = s.projectNoteDesigner(scope, project); err != nil {
			return err
		}
		out = project
		return nil
	})
	return out, err
}

func projectNoteDetailRows(r Repository, kind, projectID string) ([]Record, bool, error) {
	rows, err := pageRecords(r, kind, RecordQuery{Sessions: []string{projectID}, Descending: true, Limit: 51})
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > 50
	if truncated {
		rows = rows[:50]
	}
	return rows, truncated, nil
}

// ProjectNoteGetProjectDetail is the bounded restore payload for the designer
// workspace. Frozen version bodies stay behind the exact share/member check.
func (s *Service) ProjectNoteGetProjectDetail(ctx context.Context, scope Scope, projectID string) (ProjectNoteProjectDetail, error) {
	out := ProjectNoteProjectDetail{
		Members:       []ProjectNoteMember{},
		Notes:         []ProjectNote{},
		Versions:      []ProjectNoteVersionSummary{},
		Shares:        []ProjectNoteShare{},
		Feedbacks:     []ProjectNoteFeedback{},
		Resolutions:   []ProjectNoteFeedbackResolution{},
		Confirmations: []ProjectNoteConfirmation{},
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		out.Project = project
		if rows, truncated, err := projectNoteDetailRows(r, projectNoteMemberKind, projectID); err != nil {
			return err
		} else {
			out.Truncated.Members = truncated
			for _, row := range rows {
				var value ProjectNoteMember
				if json.Unmarshal(row.Payload, &value) != nil {
					return ErrUnavailable
				}
				out.Members = append(out.Members, value)
			}
		}
		if rows, truncated, err := projectNoteDetailRows(r, projectNoteKind, projectID); err != nil {
			return err
		} else {
			out.Truncated.Notes = truncated
			for _, row := range rows {
				var value ProjectNote
				if json.Unmarshal(row.Payload, &value) != nil {
					return ErrUnavailable
				}
				out.Notes = append(out.Notes, value)
			}
		}
		if rows, truncated, err := projectNoteDetailRows(r, projectNoteVersionKind, projectID); err != nil {
			return err
		} else {
			out.Truncated.Versions = truncated
			for _, row := range rows {
				var value ProjectNoteVersion
				if json.Unmarshal(row.Payload, &value) != nil {
					return ErrUnavailable
				}
				mediaCount := len(value.Media)
				if value.Cover != nil {
					mediaCount++
				}
				out.Versions = append(out.Versions, ProjectNoteVersionSummary{ID: value.ID, NoteID: value.NoteID, Revision: value.Revision, Title: value.Title, FrozenAt: value.FrozenAt, MediaCount: mediaCount})
			}
		}
		if rows, truncated, err := projectNoteDetailRows(r, projectNoteShareKind, projectID); err != nil {
			return err
		} else {
			out.Truncated.Shares = truncated
			for _, row := range rows {
				var value projectNoteShareRecord
				if json.Unmarshal(row.Payload, &value) != nil || value.ID == "" {
					return ErrUnavailable
				}
				value.Token, value.Replayed = "", false
				out.Shares = append(out.Shares, value.ProjectNoteShare)
			}
		}
		if rows, truncated, err := projectNoteDetailRows(r, projectNoteFeedbackKind, projectID); err != nil {
			return err
		} else {
			out.Truncated.Feedbacks = truncated
			for _, row := range rows {
				var value ProjectNoteFeedback
				if json.Unmarshal(row.Payload, &value) != nil {
					return ErrUnavailable
				}
				out.Feedbacks = append(out.Feedbacks, value)
			}
		}
		if rows, truncated, err := projectNoteDetailRows(r, projectNoteFeedbackResolutionKind, projectID); err != nil {
			return err
		} else {
			out.Truncated.Resolutions = truncated
			for _, row := range rows {
				var value ProjectNoteFeedbackResolution
				if json.Unmarshal(row.Payload, &value) != nil {
					return ErrUnavailable
				}
				out.Resolutions = append(out.Resolutions, value)
			}
		}
		if rows, truncated, err := projectNoteDetailRows(r, projectNoteConfirmKind, projectID); err != nil {
			return err
		} else {
			out.Truncated.Confirmations = truncated
			for _, row := range rows {
				var value ProjectNoteConfirmation
				if json.Unmarshal(row.Payload, &value) != nil {
					return ErrUnavailable
				}
				out.Confirmations = append(out.Confirmations, value)
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteInviteMember(ctx context.Context, scope Scope, projectID string, actor int64) (ProjectNoteMember, error) {
	var out ProjectNoteMember
	if actor <= 0 {
		return out, fail(400, "受邀账号无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		out, err = s.projectNoteInviteMemberIn(r, project, actor)
		return err
	})
	return out, err
}

func (s *Service) projectNoteInviteMemberIn(r Repository, project ProjectNoteProject, actor int64) (ProjectNoteMember, error) {
	now := s.Now().UTC()
	id := projectNoteMemberID(project.ID, actor)
	if old, err := decode[ProjectNoteMember](r, projectNoteMemberKind, id); err == nil {
		old.Active, old.RevokedAt = true, time.Time{}
		return old, r.Put(projectNoteRecord(projectNoteMemberKind, id, project.Owner, now, old, projectNoteActorKey(actor), project.ID, project.ID, ""))
	} else if !errors.Is(err, ErrNotFound) {
		return ProjectNoteMember{}, err
	}
	member := ProjectNoteMember{ID: id, ProjectID: project.ID, ActorID: actor, Active: true, InvitedAt: now}
	return member, r.Insert(projectNoteRecord(projectNoteMemberKind, id, project.Owner, now, member, projectNoteActorKey(actor), project.ID, project.ID, ""))
}

func (s *Service) ProjectNoteRevokeMember(ctx context.Context, scope Scope, projectID string, actor int64) error {
	return s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		id := projectNoteMemberID(projectID, actor)
		member, err := projectNoteRequired[ProjectNoteMember](r, projectNoteMemberKind, id, "项目成员不存在")
		if err != nil {
			return err
		}
		member.Active, member.RevokedAt = false, s.Now().UTC()
		if err = r.Put(projectNoteRecord(projectNoteMemberKind, id, project.Owner, member.RevokedAt, member, projectNoteActorKey(actor), projectID, projectID, "")); err != nil {
			return err
		}
		if project.ConfirmerID == actor {
			project.ConfirmerID = 0
			project.Revision++
			project.UpdatedAt = member.RevokedAt
			return r.Put(projectNoteRecord(projectNoteProjectKind, project.ID, project.Owner, project.UpdatedAt, project, projectNoteActorKey(project.DesignerID), "", project.ID, ""))
		}
		return nil
	})
}

func (s *Service) ProjectNoteSetConfirmer(ctx context.Context, scope Scope, projectID string, actor int64) error {
	return s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		if _, err = projectNoteLoadMember(r, projectID, actor); err != nil {
			return fail(400, "确认人必须是有效项目成员")
		}
		if project.ConfirmerID == actor {
			return nil
		}
		project.ConfirmerID, project.UpdatedAt = actor, s.Now().UTC()
		project.Revision++
		return r.Put(projectNoteRecord(projectNoteProjectKind, project.ID, project.Owner, project.UpdatedAt, project, projectNoteActorKey(project.DesignerID), "", project.ID, ""))
	})
}

func (s *Service) projectNoteValidateDraft(scope Scope, owner string, draft ProjectNoteDraft) (ProjectNoteDraft, error) {
	validated, err := s.validateDraft(scope, Draft{Title: draft.Title, Body: draft.Body, BodyFormat: draft.BodyFormat, Rights: "own", Owner: owner, CoverID: draft.CoverID, MediaIDs: draft.MediaIDs})
	if err != nil {
		return draft, err
	}
	draft.Title, draft.Body, draft.BodyFormat = strings.TrimSpace(validated.Title), validated.Body, validated.BodyFormat
	draft.CoverID, draft.MediaIDs = validated.CoverID, append([]string(nil), validated.MediaIDs...)
	return draft, nil
}

func (s *Service) ProjectNoteCreateNote(ctx context.Context, scope Scope, projectID string, draft ProjectNoteDraft) (ProjectNote, error) {
	var out ProjectNote
	err := s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		draft, err = s.projectNoteValidateDraft(scope, project.Owner, draft)
		if err != nil {
			return err
		}
		if _, _, err = projectNoteSnapshotAssets(r, project.ID, project.Owner, draft); err != nil {
			return err
		}
		now := s.Now().UTC()
		out = ProjectNote{ID: opaque(), ProjectID: projectID, Revision: 1, Title: draft.Title, Body: draft.Body, BodyFormat: draft.BodyFormat, CoverID: draft.CoverID, MediaIDs: draft.MediaIDs, CreatedAt: now, UpdatedAt: now}
		return r.Insert(projectNoteRecord(projectNoteKind, out.ID, project.Owner, now, out, projectNoteActorKey(project.DesignerID), projectID, projectID, ""))
	})
	return out, err
}

func (s *Service) ProjectNoteSaveNote(ctx context.Context, scope Scope, noteID string, draft ProjectNoteDraft, expectedRevision int) (ProjectNote, error) {
	var out ProjectNote
	err := s.transaction(ctx, func(r Repository) error {
		note, err := projectNoteRequired[ProjectNote](r, projectNoteKind, noteID, "项目笔记不存在")
		if err != nil {
			return err
		}
		project, err := s.projectNoteManaged(r, scope, note.ProjectID)
		if err != nil {
			return err
		}
		if err = projectNoteRequireActive(r, note.ID); err != nil {
			return err
		}
		if note.Revision != expectedRevision {
			return fail(409, "项目笔记已更新，请重新读取")
		}
		draft, err = s.projectNoteValidateDraft(scope, project.Owner, draft)
		if err != nil {
			return err
		}
		if _, _, err = projectNoteSnapshotAssets(r, project.ID, project.Owner, draft); err != nil {
			return err
		}
		current, hasCurrent, err := projectNoteCurrentVersion(r, project)
		if err != nil {
			return err
		}
		note.Revision++
		note.Title, note.Body, note.BodyFormat = draft.Title, draft.Body, draft.BodyFormat
		note.CoverID, note.MediaIDs, note.UpdatedAt = draft.CoverID, draft.MediaIDs, s.Now().UTC()
		out = note
		if err = r.Put(projectNoteRecord(projectNoteKind, note.ID, project.Owner, note.UpdatedAt, note, projectNoteActorKey(project.DesignerID), project.ID, project.ID, "")); err != nil {
			return err
		}
		if !hasCurrent || current.NoteID != note.ID {
			return nil
		}
		project.CurrentVersionID = current.ID
		project.Status = ProjectNoteProjectStatusRevising
		project.Revision++
		project.UpdatedAt = note.UpdatedAt
		return r.Put(projectNoteRecord(projectNoteProjectKind, project.ID, project.Owner, project.UpdatedAt, project, projectNoteActorKey(project.DesignerID), "", project.ID, ""))
	})
	return out, err
}

func projectNoteSnapshotAssets(r Repository, projectID, owner string, draft ProjectNoteDraft) (*Asset, []Asset, error) {
	refs := map[string]string{}
	if draft.BodyFormat == "article_v2" {
		var err error
		refs, err = articleMediaReferences(draft.Body)
		if err != nil {
			return nil, nil, err
		}
	}
	ids := append(append([]string(nil), draft.MediaIDs...), draft.CoverID)
	seen := map[string]bool{}
	media := []Asset{}
	var cover *Asset
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		row, err := r.Get(projectNoteMediaKind, id)
		if errors.Is(err, ErrNotFound) || err == nil && row.Owner != owner {
			return nil, nil, fail(404, "媒体不可访问")
		}
		if err != nil {
			return nil, nil, err
		}
		var privateMedia ProjectNoteMedia
		if json.Unmarshal(row.Payload, &privateMedia) != nil {
			return nil, nil, ErrUnavailable
		}
		if privateMedia.ProjectID != projectID || privateMedia.Owner != owner {
			return nil, nil, fail(404, "媒体不可访问")
		}
		asset := privateMedia.Asset
		if id == draft.CoverID {
			if asset.Kind != "image" {
				return nil, nil, fail(400, "封面必须是图片")
			}
			copy := asset
			cover = &copy
		}
		if expected := refs[id]; expected != "" && expected != asset.Kind {
			return nil, nil, fail(400, "原排版正文媒体类型与授权文件不符")
		}
		if draft.BodyFormat != "article_v2" || refs[id] != "" {
			media = append(media, asset)
		}
	}
	return cover, media, nil
}

func (s *Service) projectNoteFreezeVersionIn(r Repository, scope Scope, noteID string, expectedRevision int) (ProjectNoteVersion, error) {
	note, err := projectNoteRequired[ProjectNote](r, projectNoteKind, noteID, "项目笔记不存在")
	if err != nil {
		return ProjectNoteVersion{}, err
	}
	project, err := s.projectNoteManaged(r, scope, note.ProjectID)
	if err != nil {
		return ProjectNoteVersion{}, err
	}
	if err = projectNoteRequireActive(r, note.ID); err != nil {
		return ProjectNoteVersion{}, err
	}
	if note.Revision != expectedRevision {
		return ProjectNoteVersion{}, fail(409, "项目笔记已更新，请重新读取")
	}
	cover, media, err := projectNoteSnapshotAssets(r, project.ID, project.Owner, ProjectNoteDraft{Body: note.Body, BodyFormat: note.BodyFormat, CoverID: note.CoverID, MediaIDs: note.MediaIDs})
	if err != nil {
		return ProjectNoteVersion{}, err
	}
	now := s.Now().UTC()
	out := ProjectNoteVersion{
		ID: opaque(), ProjectID: project.ID, NoteID: note.ID, Revision: note.Revision,
		Title: note.Title, Body: note.Body, BodyFormat: note.BodyFormat,
		ProjectSnapshot: ProjectNoteProjectSnapshot{
			Name: project.Name, Location: project.Location, BuildingArea: project.BuildingArea,
			Floors: project.Floors, Style: project.Style, Revision: project.Revision,
		},
		Cover: cover, Media: media, FrozenAt: now,
	}
	if err = r.Insert(projectNoteRecord(projectNoteVersionKind, out.ID, project.Owner, now, out, projectNoteActorKey(project.DesignerID), project.ID, note.ID, "")); err != nil {
		return out, err
	}
	project.CurrentVersionID = out.ID
	project.Status = ProjectNoteProjectStatusRevising
	project.Revision++
	project.UpdatedAt = now
	if err = r.Put(projectNoteRecord(projectNoteProjectKind, project.ID, project.Owner, now, project, projectNoteActorKey(project.DesignerID), "", project.ID, "")); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) ProjectNoteFreezeVersion(ctx context.Context, scope Scope, noteID string, expectedRevision int) (ProjectNoteVersion, error) {
	var out ProjectNoteVersion
	err := s.transaction(ctx, func(r Repository) error {
		var err error
		out, err = s.projectNoteFreezeVersionIn(r, scope, noteID, expectedRevision)
		return err
	})
	return out, err
}

func (s *Service) ProjectNoteCreateShare(ctx context.Context, scope Scope, versionID string, expiry time.Time, key string) (ProjectNoteShare, error) {
	var out ProjectNoteShare
	if !validID(key) || !expiry.After(s.Now()) {
		return out, fail(400, "分享有效期或幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		version, err := projectNoteRequired[ProjectNoteVersion](r, projectNoteVersionKind, versionID, "项目笔记版本不存在")
		if err != nil {
			return err
		}
		project, err := s.projectNoteManaged(r, scope, version.ProjectID)
		if err != nil {
			return err
		}
		requestID := "request:" + hash(fmt.Sprintf("%d:%s:%s", scope.Actor, versionID, key))
		if request, readErr := decode[projectNoteShareRequest](r, projectNoteShareKind, requestID); readErr == nil {
			record, getErr := decode[projectNoteShareRecord](r, projectNoteShareKind, request.ShareRecordID)
			if getErr != nil {
				return getErr
			}
			if request.VersionID != versionID || !request.ExpiresAt.Equal(expiry.UTC()) {
				return fail(409, "分享幂等标识已用于其他请求")
			}
			out = record.ProjectNoteShare
			out.Replayed = true
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		token, now := opaque(), s.Now().UTC()
		recordID := hash(token)
		stored := projectNoteShareRecord{ProjectNoteShare: ProjectNoteShare{ID: opaque(), ProjectID: project.ID, VersionID: version.ID, SharerID: scope.Actor, ExpiresAt: expiry.UTC(), CreatedAt: now}, TokenHash: recordID}
		if err = r.Insert(projectNoteRecord(projectNoteShareKind, recordID, project.Owner, now, stored, projectNoteActorKey(scope.Actor), project.ID, version.ID, stored.ID)); err != nil {
			return err
		}
		if err = r.Insert(projectNoteRecord(projectNoteShareKind, requestID, project.Owner, now, projectNoteShareRequest{ShareRecordID: recordID, VersionID: versionID, ExpiresAt: expiry.UTC()}, projectNoteActorKey(scope.Actor), "", version.ID, "")); err != nil {
			return err
		}
		out = stored.ProjectNoteShare
		out.Token = token
		return nil
	})
	return out, err
}

func projectNoteShareByID(r Repository, owner, shareID string) (Record, projectNoteShareRecord, error) {
	rows, err := pageRecords(r, projectNoteShareKind, RecordQuery{Owners: []string{owner}, Shares: []string{shareID}, Limit: 2})
	if err != nil {
		return Record{}, projectNoteShareRecord{}, err
	}
	if len(rows) != 1 {
		return Record{}, projectNoteShareRecord{}, fail(404, "分享不存在")
	}
	var share projectNoteShareRecord
	if json.Unmarshal(rows[0].Payload, &share) != nil || share.ID != shareID {
		return Record{}, share, ErrUnavailable
	}
	return rows[0], share, nil
}

func (s *Service) ProjectNoteRevokeShare(ctx context.Context, scope Scope, shareID string) error {
	if err := s.authorized(scope); err != nil {
		return err
	}
	return s.transaction(ctx, func(r Repository) error {
		for _, owner := range scope.Owners {
			row, share, err := projectNoteShareByID(r, owner, shareID)
			if err != nil {
				var missing *Error
				if errors.As(err, &missing) && missing.Status == 404 {
					continue
				}
				return err
			}
			project, err := s.projectNoteManaged(r, scope, share.ProjectID)
			if err != nil {
				return err
			}
			share.Revoked = true
			return r.Put(projectNoteRecord(projectNoteShareKind, row.ID, project.Owner, s.Now().UTC(), share, row.Visitor, row.Session, row.Content, row.Share))
		}
		return fail(404, "分享不存在")
	})
}

func (s *Service) projectNoteReadShareIn(ctx context.Context, r Repository, token string, actor int64) (ProjectNoteShareView, projectNoteShareRecord, error) {
	var out ProjectNoteShareView
	if !validID(token) {
		return out, projectNoteShareRecord{}, fail(404, "分享不存在或已失效")
	}
	share, err := decode[projectNoteShareRecord](r, projectNoteShareKind, hash(token))
	if errors.Is(err, ErrNotFound) {
		return out, share, fail(404, "分享不存在或已失效")
	}
	if err != nil {
		return out, share, err
	}
	if share.TokenHash != hash(token) || share.Revoked || !s.Now().Before(share.ExpiresAt) {
		return out, share, fail(404, "分享不存在或已失效")
	}
	project, err := projectNoteLoadProject(r, share.ProjectID)
	if err != nil {
		return out, share, err
	}
	if err = s.projectNoteReadAccess(ctx, r, project, actor); err != nil {
		return out, share, err
	}
	version, err := decode[ProjectNoteVersion](r, projectNoteVersionKind, share.VersionID)
	if err != nil {
		return out, share, err
	}
	if version.ProjectID != project.ID {
		return out, share, ErrUnavailable
	}
	publicShare := share.ProjectNoteShare
	publicShare.Token = ""
	out = ProjectNoteShareView{Project: project, Share: publicShare, Version: version}
	return out, share, nil
}

func (s *Service) ProjectNoteReadShare(ctx context.Context, token string, actor int64) (ProjectNoteShareView, error) {
	var out ProjectNoteShareView
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		view, _, err := s.projectNoteReadShareIn(ctx, r, token, actor)
		if err != nil {
			return err
		}
		view.Feedbacks = []ProjectNoteFeedback{}
		view.Resolutions = []ProjectNoteFeedbackResolution{}
		rows, err := pageRecords(r, projectNoteFeedbackKind, RecordQuery{Content: view.Version.ID, Descending: true, Limit: 51})
		if err != nil {
			return err
		}
		if len(rows) > 50 {
			view.ActivityTruncated = true
			rows = rows[:50]
		}
		for _, row := range rows {
			var feedback ProjectNoteFeedback
			if json.Unmarshal(row.Payload, &feedback) != nil || feedback.VersionID != view.Version.ID {
				return ErrUnavailable
			}
			view.Feedbacks = append(view.Feedbacks, feedback)
		}
		rows, err = pageRecords(r, projectNoteFeedbackResolutionKind, RecordQuery{Content: view.Version.ID, Descending: true, Limit: 51})
		if err != nil {
			return err
		}
		if len(rows) > 50 {
			view.ActivityTruncated = true
			rows = rows[:50]
		}
		for _, row := range rows {
			var resolution ProjectNoteFeedbackResolution
			if json.Unmarshal(row.Payload, &resolution) != nil {
				return ErrUnavailable
			}
			view.Resolutions = append(view.Resolutions, resolution)
		}
		rows, err = pageRecords(r, projectNoteConfirmKind, RecordQuery{Content: view.Version.ID, Limit: 2})
		if err != nil {
			return err
		}
		if len(rows) > 1 {
			return ErrUnavailable
		}
		if len(rows) == 1 {
			var confirmation ProjectNoteConfirmation
			if json.Unmarshal(rows[0].Payload, &confirmation) != nil || confirmation.VersionID != view.Version.ID {
				return ErrUnavailable
			}
			view.Confirmation = &confirmation
		}
		if view.Confirmation == nil && view.Project.ConfirmerID == actor {
			if _, memberErr := projectNoteLoadMember(r, view.Project.ID, actor); memberErr == nil {
				view.CanConfirm = true
			}
		}
		out = view
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteOpenShare(ctx context.Context, token string, actor int64, eventID string) (ProjectNoteOpenFact, error) {
	var out ProjectNoteOpenFact
	if !validID(eventID) {
		return out, fail(400, "浏览事件标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		view, stored, err := s.projectNoteReadShareIn(ctx, r, token, actor)
		if err != nil {
			return err
		}
		if _, err = r.Get("deletion", projectNoteActorKey(actor)); err == nil {
			return fail(410, "访问记录已删除")
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if old, readErr := decode[ProjectNoteOpenFact](r, projectNoteOpenKind, eventID); readErr == nil {
			if old.ShareID != view.Share.ID || old.ActorID != actor {
				return fail(409, "浏览事件标识已用于其他访问")
			}
			out = old
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		out = ProjectNoteOpenFact{ID: eventID, ProjectID: view.Project.ID, NoteID: view.Version.NoteID, VersionID: view.Version.ID, ShareID: view.Share.ID, ActorID: actor, ActualSharerID: stored.SharerID, Notify: actor != stored.SharerID, OpenedAt: now}
		if err = r.Insert(projectNoteRecord(projectNoteOpenKind, eventID, view.Project.Owner, now, out, projectNoteActorKey(actor), "", view.Version.ID, view.Share.ID)); err != nil {
			return err
		}
		if !out.Notify || !s.Config.Enabled || !s.NoticeRules.BrowseEnabled {
			return nil
		}
		notice := NoticeEvent{ID: eventID, Kind: "project_note_open", Owner: view.Project.Owner, RecipientID: stored.SharerID, VisitorKey: projectNoteActorKey(actor), ShareID: view.Share.ID, ProjectID: view.Project.ID, ContentID: view.Version.ID, Route: ProjectNoteNoticeRoute, OccurredAt: now}
		noticeRow := record("notice_event", eventID, view.Project.Owner, now, notice)
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

// ProjectNoteNoticeEligible rechecks the private-project grant immediately
// before an inbox row or external delivery is materialized.
func (s *Service) ProjectNoteNoticeEligible(ctx context.Context, eventID string, recipient int64, owners ...string) (bool, error) {
	if !validID(eventID) || recipient <= 0 || !s.Config.Enabled {
		return false, nil
	}
	eligible := false
	err := s.readTransaction(ctx, func(r Repository) error {
		notice, err := decode[NoticeEvent](r, "notice_event", eventID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		allowedOwner := len(owners) == 0
		for _, owner := range owners {
			if owner == notice.Owner {
				allowedOwner = true
				break
			}
		}
		if !allowedOwner || notice.RecipientID != recipient {
			return nil
		}
		if notice.Kind == ProjectNoteJoinRequestNoticeKind {
			eligible, err = s.projectNoteJoinNoticeEligible(ctx, r, notice, recipient)
			return err
		}
		if notice.Kind == ProjectPackageReceiptNoticeKind {
			receiptRows, receiptErr := pageRecords(r, projectPackageReceiptKind, RecordQuery{Owners: []string{notice.Owner}, Sessions: []string{notice.ProjectID}, VisitorKeys: []string{notice.VisitorKey}, Shares: []string{notice.ShareID}, Content: notice.ContentID, ExactVisitors: true, Limit: 2})
			if receiptErr != nil {
				return receiptErr
			}
			if len(receiptRows) == 0 {
				return nil
			}
			var receipt ProjectPackageReceipt
			if json.Unmarshal(receiptRows[0].Payload, &receipt) != nil || projectPackageReceiptNoticeID(receipt) != notice.ID || receipt.DeliveryID != notice.ShareID || receipt.ProjectID != notice.ProjectID || receipt.VersionID != notice.ContentID || projectNoteActorKey(receipt.ActorID) != notice.VisitorKey {
				return nil
			}
			_, delivery, deliveryErr := projectPackageDeliveryByID(r, notice.Owner, receipt.DeliveryID)
			if deliveryErr != nil {
				var missing *Error
				if errors.As(deliveryErr, &missing) && missing.Status == 404 {
					return nil
				}
				return deliveryErr
			}
			if delivery.ProjectID != receipt.ProjectID || !projectPackageHasVersion(delivery.Items, receipt.VersionID) {
				return nil
			}
			project, projectErr := projectNoteLoadProject(r, receipt.ProjectID)
			if projectErr != nil {
				var missing *Error
				if errors.As(projectErr, &missing) && missing.Status == 404 {
					return nil
				}
				return projectErr
			}
			if project.Owner != notice.Owner || project.DesignerID != recipient {
				return nil
			}
			if grantErr := s.projectNoteGrant(ctx, r, project); grantErr != nil {
				var denied *Error
				if errors.As(grantErr, &denied) && (denied.Status == 401 || denied.Status == 403 || denied.Status == 404) {
					return nil
				}
				return grantErr
			}
			eligible = true
			return nil
		}
		if notice.Kind == ProjectNoteFeedbackNoticeKind {
			feedbackID, ok := projectNoteFeedbackIDFromNotice(eventID)
			if !ok {
				return nil
			}
			feedback, feedbackErr := decode[ProjectNoteFeedback](r, projectNoteFeedbackKind, feedbackID)
			if errors.Is(feedbackErr, ErrNotFound) {
				return nil
			}
			if feedbackErr != nil {
				return feedbackErr
			}
			if feedback.ProjectID != notice.ProjectID || feedback.VersionID != notice.ContentID || feedback.ShareID != notice.ShareID || projectNoteActorKey(feedback.ActorID) != notice.VisitorKey {
				return nil
			}
			project, projectErr := projectNoteLoadProject(r, feedback.ProjectID)
			if projectErr != nil {
				var missing *Error
				if errors.As(projectErr, &missing) && missing.Status == 404 {
					return nil
				}
				return projectErr
			}
			if project.Owner != notice.Owner || project.DesignerID != recipient {
				return nil
			}
			if grantErr := s.projectNoteGrant(ctx, r, project); grantErr != nil {
				var denied *Error
				if errors.As(grantErr, &denied) && (denied.Status == 401 || denied.Status == 403 || denied.Status == 404) {
					return nil
				}
				return grantErr
			}
			eligible = true
			return nil
		}
		if notice.Kind != "project_note_open" || !s.NoticeRules.BrowseEnabled {
			return nil
		}
		opened, err := decode[ProjectNoteOpenFact](r, projectNoteOpenKind, eventID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !opened.Notify || opened.ActualSharerID != recipient || opened.ActorID == recipient || opened.ShareID != notice.ShareID || opened.VersionID != notice.ContentID {
			return nil
		}
		if _, err = r.Get("deletion", projectNoteActorKey(opened.ActorID)); err == nil {
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		_, share, err := projectNoteShareByID(r, notice.Owner, opened.ShareID)
		if err != nil {
			var missing *Error
			if errors.As(err, &missing) && missing.Status == 404 {
				return nil
			}
			return err
		}
		if share.Revoked || !s.Now().Before(share.ExpiresAt) || share.SharerID != recipient || share.VersionID != opened.VersionID {
			return nil
		}
		project, err := projectNoteLoadProject(r, opened.ProjectID)
		if err != nil {
			var missing *Error
			if errors.As(err, &missing) && missing.Status == 404 {
				return nil
			}
			return err
		}
		if project.Owner != notice.Owner {
			return nil
		}
		if err = s.projectNoteReadAccess(ctx, r, project, opened.ActorID); err != nil {
			var denied *Error
			if errors.As(err, &denied) && (denied.Status == 401 || denied.Status == 403 || denied.Status == 404) {
				return nil
			}
			return err
		}
		eligible = true
		return nil
	})
	return eligible, err
}

func projectNoteValidSatisfaction(satisfaction string) bool {
	return satisfaction == "" || satisfaction == ProjectNoteSatisfactionVerySatisfied || satisfaction == ProjectNoteSatisfactionNeedsAdjustment || satisfaction == ProjectNoteSatisfactionUnsatisfied
}

func (s *Service) ProjectNoteAddFeedback(ctx context.Context, token string, actor int64, text, key string) (ProjectNoteFeedback, error) {
	return s.ProjectNoteAddFeedbackWithSatisfaction(ctx, token, actor, text, "", key)
}

func (s *Service) ProjectNoteAddFeedbackWithSatisfaction(ctx context.Context, token string, actor int64, text, satisfaction, key string) (ProjectNoteFeedback, error) {
	var out ProjectNoteFeedback
	text = strings.TrimSpace(text)
	satisfaction = strings.TrimSpace(satisfaction)
	if text == "" || len(text) > 4000 || !projectNoteValidSatisfaction(satisfaction) || !validID(key) {
		return out, fail(400, "反馈内容或幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		view, _, err := s.projectNoteReadShareIn(ctx, r, token, actor)
		if err != nil {
			return err
		}
		id := hash(fmt.Sprintf("feedback:%s:%d:%s", view.Share.ID, actor, key))
		if old, readErr := decode[ProjectNoteFeedback](r, projectNoteFeedbackKind, id); readErr == nil {
			if old.Text != text || old.Satisfaction != satisfaction || old.VersionID != view.Version.ID {
				return fail(409, "反馈幂等标识已用于其他内容")
			}
			out = old
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		out = ProjectNoteFeedback{ID: id, ProjectID: view.Project.ID, VersionID: view.Version.ID, ShareID: view.Share.ID, ActorID: actor, Text: text, Satisfaction: satisfaction, CreatedAt: now}
		if err = r.Insert(projectNoteRecord(projectNoteFeedbackKind, id, view.Project.Owner, now, out, projectNoteActorKey(actor), view.Project.ID, view.Version.ID, view.Share.ID)); err != nil {
			return err
		}
		if satisfaction == ProjectNoteSatisfactionNeedsAdjustment || satisfaction == ProjectNoteSatisfactionUnsatisfied {
			current, drivesStatus, statusErr := projectNoteVersionDrivesStatus(r, view.Project, view.Version.ID)
			if statusErr != nil {
				return statusErr
			}
			if drivesStatus {
				view.Project.CurrentVersionID = current.ID
				view.Project.Status = ProjectNoteProjectStatusRevising
				view.Project.Revision++
				view.Project.UpdatedAt = now
				if err = r.Put(projectNoteRecord(projectNoteProjectKind, view.Project.ID, view.Project.Owner, now, view.Project, projectNoteActorKey(view.Project.DesignerID), "", view.Project.ID, "")); err != nil {
					return err
				}
			}
		}
		if actor == view.Project.DesignerID || !s.Config.Enabled {
			return nil
		}
		notice := NoticeEvent{ID: projectNoteFeedbackNoticeID(out.ID), Kind: ProjectNoteFeedbackNoticeKind, Owner: view.Project.Owner, RecipientID: view.Project.DesignerID, VisitorKey: projectNoteActorKey(actor), ShareID: view.Share.ID, ProjectID: view.Project.ID, ContentID: view.Version.ID, Route: ProjectNoteNoticeRoute, OccurredAt: now}
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

func (s *Service) ProjectNoteResolveFeedback(ctx context.Context, scope Scope, feedbackID, reply, key string) (ProjectNoteFeedbackResolution, error) {
	var out ProjectNoteFeedbackResolution
	reply = strings.TrimSpace(reply)
	if reply == "" || len(reply) > 4000 || !validID(key) {
		return out, fail(400, "处理回复或幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		feedback, err := projectNoteRequired[ProjectNoteFeedback](r, projectNoteFeedbackKind, feedbackID, "反馈不存在")
		if err != nil {
			return err
		}
		project, err := s.projectNoteManaged(r, scope, feedback.ProjectID)
		if err != nil {
			return err
		}
		id := hash("resolution:" + feedback.ID)
		if old, readErr := decode[ProjectNoteFeedbackResolution](r, projectNoteFeedbackResolutionKind, id); readErr == nil {
			if old.Reply != reply {
				return fail(409, "反馈已处理")
			}
			out = old
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		out = ProjectNoteFeedbackResolution{ID: id, FeedbackID: feedback.ID, DesignerID: scope.Actor, Reply: reply, ResolvedAt: now}
		if err = r.Insert(projectNoteRecord(projectNoteFeedbackResolutionKind, id, project.Owner, now, out, projectNoteActorKey(scope.Actor), project.ID, feedback.VersionID, feedback.ShareID)); err != nil {
			return err
		}
		feedback.Resolved, feedback.ResolvedAt, feedback.Reply = true, now, reply
		return r.Put(projectNoteRecord(projectNoteFeedbackKind, feedback.ID, project.Owner, feedback.CreatedAt, feedback, projectNoteActorKey(feedback.ActorID), project.ID, feedback.VersionID, feedback.ShareID))
	})
	return out, err
}

func (s *Service) projectNoteMarkCurrentVersionConfirmed(r Repository, project ProjectNoteProject, versionID string, at time.Time) error {
	current, drivesStatus, err := projectNoteVersionDrivesStatus(r, project, versionID)
	if err != nil || !drivesStatus {
		return err
	}
	if project.Status == ProjectNoteProjectStatusConfirmed && project.CurrentVersionID == current.ID {
		return nil
	}
	// 归档态（仿真稿步骤⑥）是终态：业主确认不能把已归档项目拉回工作流，
	// 需设计师先取消归档（codex 四轮 P1：确认入口绕过状态机）。
	if project.Status == ProjectNoteProjectStatusArchived {
		return fail(409, "项目已归档，如需继续确认请先取消归档")
	}
	project.CurrentVersionID = current.ID
	project.Status = ProjectNoteProjectStatusConfirmed
	project.Revision++
	project.UpdatedAt = at
	return r.Put(projectNoteRecord(projectNoteProjectKind, project.ID, project.Owner, at, project, projectNoteActorKey(project.DesignerID), "", project.ID, ""))
}

func (s *Service) ProjectNoteConfirmShare(ctx context.Context, token string, actor int64, key string) (ProjectNoteConfirmation, error) {
	var out ProjectNoteConfirmation
	if !validID(key) {
		return out, fail(400, "确认幂等标识无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		view, _, err := s.projectNoteReadShareIn(ctx, r, token, actor)
		if err != nil {
			return err
		}
		if view.Project.ConfirmerID != actor {
			return fail(403, "仅指定业主可确认方案")
		}
		if _, err = projectNoteLoadMember(r, view.Project.ID, actor); err != nil {
			return err
		}
		id := hash(fmt.Sprintf("confirm:%s:%s", view.Project.ID, view.Version.ID))
		requestID := "request:" + hash(fmt.Sprintf("confirm:%d:%s", actor, key))
		if request, readErr := decode[projectNoteConfirmationRequest](r, projectNoteConfirmKind, requestID); readErr == nil {
			if request.ProjectID != view.Project.ID || request.VersionID != view.Version.ID || request.ActorID != actor || request.ConfirmationID != id {
				return fail(409, "确认幂等标识已用于其他请求")
			}
			stored, storedErr := decode[projectNoteConfirmationRecord](r, projectNoteConfirmKind, id)
			if storedErr != nil {
				return storedErr
			}
			out = stored.ProjectNoteConfirmation
			return s.projectNoteMarkCurrentVersionConfirmed(r, view.Project, view.Version.ID, s.Now().UTC())
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		if stored, readErr := decode[projectNoteConfirmationRecord](r, projectNoteConfirmKind, id); readErr == nil {
			if stored.ConfirmerID != actor {
				return fail(409, "当前版本已由原指定业主确认")
			}
			request := projectNoteConfirmationRequest{ConfirmationID: id, ProjectID: view.Project.ID, VersionID: view.Version.ID, ActorID: actor}
			if err = r.Insert(projectNoteRecord(projectNoteConfirmKind, requestID, view.Project.Owner, s.Now().UTC(), request, projectNoteActorKey(actor), "", "", "")); err != nil {
				return err
			}
			out = stored.ProjectNoteConfirmation
			return s.projectNoteMarkCurrentVersionConfirmed(r, view.Project, view.Version.ID, s.Now().UTC())
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		out = ProjectNoteConfirmation{ID: id, ProjectID: view.Project.ID, VersionID: view.Version.ID, ConfirmerID: actor, ConfirmedAt: now}
		stored := projectNoteConfirmationRecord{ProjectNoteConfirmation: out, FirstKeyHash: hash(key)}
		if err = r.Insert(projectNoteRecord(projectNoteConfirmKind, id, view.Project.Owner, now, stored, projectNoteActorKey(actor), view.Project.ID, view.Version.ID, view.Share.ID)); err != nil {
			return err
		}
		request := projectNoteConfirmationRequest{ConfirmationID: id, ProjectID: view.Project.ID, VersionID: view.Version.ID, ActorID: actor}
		if err = r.Insert(projectNoteRecord(projectNoteConfirmKind, requestID, view.Project.Owner, now, request, projectNoteActorKey(actor), "", "", "")); err != nil {
			return err
		}
		return s.projectNoteMarkCurrentVersionConfirmed(r, view.Project, view.Version.ID, now)
	})
	return out, err
}

func (s *Service) ProjectNoteReadShareMedia(ctx context.Context, token string, actor int64, assetID string) (Asset, error) {
	var out Asset
	err := s.readTransaction(ctx, func(r Repository) error {
		view, _, err := s.projectNoteReadShareIn(ctx, r, token, actor)
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
