package tracking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const (
	ProjectNoteHistoryNotes         = "notes"
	ProjectNoteHistoryVersions      = "versions"
	ProjectNoteHistoryFeedbacks     = "feedbacks"
	ProjectNoteHistoryResolutions   = "resolutions"
	ProjectNoteHistoryConfirmations = "confirmations"

	projectNoteHistoryPageMax = 50
)

// ProjectNoteHistoryPage keeps one bounded fact family per response. Items is
// the wire representation; typed fields let service callers avoid assertions.
type ProjectNoteHistoryPage struct {
	Type          string                          `json:"type"`
	Items         any                             `json:"items"`
	NextCursor    string                          `json:"next_cursor,omitempty"`
	Notes         []ProjectNote                   `json:"-"`
	Versions      []ProjectNoteVersionSummary     `json:"-"`
	Feedbacks     []ProjectNoteFeedback           `json:"-"`
	Resolutions   []ProjectNoteFeedbackResolution `json:"-"`
	Confirmations []ProjectNoteConfirmation       `json:"-"`
}

type projectNoteHistoryCursor struct {
	Kind      string    `json:"kind"`
	ProjectID string    `json:"project_id"`
	Type      string    `json:"type"`
	VersionID string    `json:"version_id,omitempty"`
	BeforeAt  time.Time `json:"before_at"`
	BeforeID  string    `json:"before_id"`
}

func projectNoteHistoryKind(historyType string) (string, bool) {
	switch historyType {
	case ProjectNoteHistoryNotes:
		return projectNoteKind, true
	case ProjectNoteHistoryVersions:
		return projectNoteVersionKind, true
	case ProjectNoteHistoryFeedbacks:
		return projectNoteFeedbackKind, true
	case ProjectNoteHistoryResolutions:
		return projectNoteFeedbackResolutionKind, true
	case ProjectNoteHistoryConfirmations:
		return projectNoteConfirmKind, true
	default:
		return "", false
	}
}

func projectNoteSharedHistoryType(historyType string) bool {
	return historyType == ProjectNoteHistoryFeedbacks || historyType == ProjectNoteHistoryResolutions || historyType == ProjectNoteHistoryConfirmations
}

func projectNoteHistoryLimit(limit int) int {
	if limit <= 0 {
		return 30
	}
	if limit > projectNoteHistoryPageMax {
		return projectNoteHistoryPageMax
	}
	return limit
}

func encodeProjectNoteHistoryCursor(cursor projectNoteHistoryCursor) (string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeProjectNoteHistoryCursor(raw, projectID, historyType, versionID string) (projectNoteHistoryCursor, error) {
	var cursor projectNoteHistoryCursor
	if raw == "" {
		return cursor, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Kind != "project_note_history_v1" || cursor.ProjectID != projectID || cursor.Type != historyType || cursor.VersionID != versionID || cursor.BeforeAt.IsZero() || cursor.BeforeID == "" {
		return cursor, fail(400, "历史游标无效")
	}
	return cursor, nil
}

func projectNoteVersionSummary(version ProjectNoteVersion) ProjectNoteVersionSummary {
	mediaCount := len(version.Media)
	if version.Cover != nil {
		mediaCount++
	}
	return ProjectNoteVersionSummary{ID: version.ID, NoteID: version.NoteID, Revision: version.Revision, Title: version.Title, FrozenAt: version.FrozenAt, MediaCount: mediaCount}
}

func decodeProjectNoteHistoryRows(historyType string, rows []Record) (ProjectNoteHistoryPage, error) {
	page := ProjectNoteHistoryPage{Type: historyType}
	switch historyType {
	case ProjectNoteHistoryNotes:
		page.Notes = make([]ProjectNote, 0, len(rows))
		for _, row := range rows {
			var value ProjectNote
			if json.Unmarshal(row.Payload, &value) != nil {
				return page, ErrUnavailable
			}
			page.Notes = append(page.Notes, value)
		}
		page.Items = page.Notes
	case ProjectNoteHistoryVersions:
		page.Versions = make([]ProjectNoteVersionSummary, 0, len(rows))
		for _, row := range rows {
			var value ProjectNoteVersion
			if json.Unmarshal(row.Payload, &value) != nil {
				return page, ErrUnavailable
			}
			page.Versions = append(page.Versions, projectNoteVersionSummary(value))
		}
		page.Items = page.Versions
	case ProjectNoteHistoryFeedbacks:
		page.Feedbacks = make([]ProjectNoteFeedback, 0, len(rows))
		for _, row := range rows {
			var value ProjectNoteFeedback
			if json.Unmarshal(row.Payload, &value) != nil {
				return page, ErrUnavailable
			}
			page.Feedbacks = append(page.Feedbacks, value)
		}
		page.Items = page.Feedbacks
	case ProjectNoteHistoryResolutions:
		page.Resolutions = make([]ProjectNoteFeedbackResolution, 0, len(rows))
		for _, row := range rows {
			var value ProjectNoteFeedbackResolution
			if json.Unmarshal(row.Payload, &value) != nil {
				return page, ErrUnavailable
			}
			page.Resolutions = append(page.Resolutions, value)
		}
		page.Items = page.Resolutions
	case ProjectNoteHistoryConfirmations:
		page.Confirmations = make([]ProjectNoteConfirmation, 0, len(rows))
		for _, row := range rows {
			var value ProjectNoteConfirmation
			if json.Unmarshal(row.Payload, &value) != nil {
				return page, ErrUnavailable
			}
			page.Confirmations = append(page.Confirmations, value)
		}
		page.Items = page.Confirmations
	}
	return page, nil
}

func projectNoteHistoryPage(r Repository, projectID, historyType, versionID string, limit int, rawCursor string) (ProjectNoteHistoryPage, error) {
	kind, ok := projectNoteHistoryKind(historyType)
	if !ok {
		return ProjectNoteHistoryPage{}, fail(400, "历史类型无效")
	}
	if versionID != "" && (historyType == ProjectNoteHistoryNotes || historyType == ProjectNoteHistoryVersions) {
		return ProjectNoteHistoryPage{}, fail(400, "当前历史类型不支持版本筛选")
	}
	cursor, err := decodeProjectNoteHistoryCursor(rawCursor, projectID, historyType, versionID)
	if err != nil {
		return ProjectNoteHistoryPage{}, err
	}
	limit = projectNoteHistoryLimit(limit)
	query := RecordQuery{Sessions: []string{projectID}, Descending: true, Limit: limit + 1, BeforeAt: cursor.BeforeAt, BeforeID: cursor.BeforeID}
	if versionID != "" {
		query.Content = versionID
	}
	rows, err := pageRecords(r, kind, query)
	if err != nil {
		return ProjectNoteHistoryPage{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	page, err := decodeProjectNoteHistoryRows(historyType, rows)
	if err != nil || !hasMore || len(rows) == 0 {
		return page, err
	}
	last := rows[len(rows)-1]
	page.NextCursor, err = encodeProjectNoteHistoryCursor(projectNoteHistoryCursor{Kind: "project_note_history_v1", ProjectID: projectID, Type: historyType, VersionID: versionID, BeforeAt: last.At, BeforeID: last.ID})
	return page, err
}

func projectNoteHistoryVersionBelongsToProject(r Repository, projectID, versionID string) error {
	if versionID == "" {
		return nil
	}
	version, err := projectNoteRequired[ProjectNoteVersion](r, projectNoteVersionKind, versionID, "版本不存在")
	if err != nil {
		return err
	}
	if version.ProjectID != projectID {
		return fail(404, "版本不存在")
	}
	return nil
}

// ProjectNoteHistory returns one bounded, newest-first project fact family.
func (s *Service) ProjectNoteHistory(ctx context.Context, scope Scope, projectID, historyType, versionID string, limit int, cursor string) (ProjectNoteHistoryPage, error) {
	var out ProjectNoteHistoryPage
	projectID, historyType, versionID = strings.TrimSpace(projectID), strings.TrimSpace(historyType), strings.TrimSpace(versionID)
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		if _, err := s.projectNoteManaged(r, scope, projectID); err != nil {
			return err
		}
		if err := projectNoteHistoryVersionBelongsToProject(r, projectID, versionID); err != nil {
			return err
		}
		var err error
		out, err = projectNoteHistoryPage(r, projectID, historyType, versionID, limit, cursor)
		return err
	})
	return out, err
}

// ProjectNoteSharedHistory revalidates the invitation and share on every page,
// then fixes the query to the exact version carried by that share.
func (s *Service) ProjectNoteSharedHistory(ctx context.Context, token string, actor int64, historyType string, limit int, cursor string) (ProjectNoteHistoryPage, error) {
	var out ProjectNoteHistoryPage
	historyType = strings.TrimSpace(historyType)
	if !projectNoteSharedHistoryType(historyType) {
		return out, fail(400, "共享历史类型无效")
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		view, _, err := s.projectNoteReadShareIn(ctx, r, token, actor)
		if err != nil {
			return err
		}
		out, err = projectNoteHistoryPage(r, view.Project.ID, historyType, view.Version.ID, limit, cursor)
		return err
	})
	return out, err
}

func (s *Service) ProjectNoteGetFeedback(ctx context.Context, scope Scope, projectID, feedbackID string) (ProjectNoteFeedback, error) {
	var out ProjectNoteFeedback
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		if _, err := s.projectNoteManaged(r, scope, projectID); err != nil {
			return err
		}
		feedback, err := projectNoteRequired[ProjectNoteFeedback](r, projectNoteFeedbackKind, feedbackID, "反馈不存在")
		if err != nil {
			return err
		}
		if feedback.ProjectID != projectID {
			return fail(404, "反馈不存在")
		}
		out = feedback
		return nil
	})
	return out, err
}

// ProjectNoteGetNote reads the current editable draft only. Deleted drafts stay
// stored for audit/history but cannot be reopened through this editing API.
func (s *Service) ProjectNoteGetNote(ctx context.Context, scope Scope, projectID, noteID string) (ProjectNote, error) {
	var out ProjectNote
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		if _, err := s.projectNoteManaged(r, scope, projectID); err != nil {
			return err
		}
		note, err := projectNoteRequired[ProjectNote](r, projectNoteKind, noteID, "项目笔记不存在")
		if err != nil {
			return err
		}
		if note.ProjectID != projectID {
			return fail(404, "项目笔记不存在")
		}
		if err = projectNoteRequireActive(r, note.ID); err != nil {
			return err
		}
		out = note
		return nil
	})
	return out, err
}
