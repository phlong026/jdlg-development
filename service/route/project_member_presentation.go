package route

import (
	"context"
	"database/sql"
	"strings"

	"service/model"
	"service/tracking"
)

type projectNoteProfileResolver func(context.Context, []int64) (map[int64]model.UserProfilePresentation, error)

type projectNoteMemberPresentation struct {
	tracking.ProjectNoteMember
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar,omitempty"`
}

type projectNoteConfirmationPresentation struct {
	tracking.ProjectNoteConfirmation
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar,omitempty"`
}

type projectNoteProjectDetailPresentation struct {
	tracking.ProjectNoteProjectDetail
	Members       []projectNoteMemberPresentation       `json:"members"`
	Confirmations []projectNoteConfirmationPresentation `json:"confirmations"`
}

func projectNoteCreatorDisplayName(profile model.UserProfilePresentation) string {
	name := strings.TrimSpace(profile.Nickname)
	if name == "" {
		return "项目创建人"
	}
	return name
}

func presentProjectNoteProjects(ctx context.Context, projects []tracking.ProjectNoteProject, resolve projectNoteProfileResolver) ([]tracking.ProjectNoteProject, error) {
	ids := make([]int64, 0, len(projects))
	seen := make(map[int64]bool, len(projects))
	for _, project := range projects {
		if project.DesignerID > 0 && !seen[project.DesignerID] {
			seen[project.DesignerID] = true
			ids = append(ids, project.DesignerID)
		}
	}
	profiles := map[int64]model.UserProfilePresentation{}
	if resolve != nil && len(ids) > 0 {
		var err error
		profiles, err = resolve(ctx, ids)
		if err != nil {
			return nil, err
		}
	}
	out := make([]tracking.ProjectNoteProject, len(projects))
	copy(out, projects)
	for i := range out {
		out[i].CreatorDisplayName = projectNoteCreatorDisplayName(profiles[out[i].DesignerID])
	}
	return out, nil
}

func makeProjectNoteProfileResolver(db *sql.DB) projectNoteProfileResolver {
	return func(ctx context.Context, ids []int64) (map[int64]model.UserProfilePresentation, error) {
		q := tracking.SourceReadDB(ctx, db)
		if q == nil {
			return nil, tracking.ErrUnavailable
		}
		return model.GetUserProfilePresentations(ctx, q, ids)
	}
}

func presentProjectNoteProjectDetail(ctx context.Context, detail tracking.ProjectNoteProjectDetail, resolve projectNoteProfileResolver) (projectNoteProjectDetailPresentation, error) {
	ids := make([]int64, 0, 1+len(detail.Members)+len(detail.Confirmations))
	seen := make(map[int64]bool, cap(ids))
	addID := func(id int64) {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	addID(detail.Project.DesignerID)
	for _, member := range detail.Members {
		if member.Active {
			addID(member.ActorID)
		}
	}
	for _, confirmation := range detail.Confirmations {
		addID(confirmation.ConfirmerID)
	}
	profiles := map[int64]model.UserProfilePresentation{}
	if resolve != nil && len(ids) > 0 {
		var err error
		profiles, err = resolve(ctx, ids)
		if err != nil {
			return projectNoteProjectDetailPresentation{}, err
		}
	}
	out := projectNoteProjectDetailPresentation{
		ProjectNoteProjectDetail: detail,
		Members:                  make([]projectNoteMemberPresentation, 0, len(detail.Members)),
		Confirmations:            make([]projectNoteConfirmationPresentation, 0, len(detail.Confirmations)),
	}
	out.Project.CreatorDisplayName = projectNoteCreatorDisplayName(profiles[detail.Project.DesignerID])
	for _, member := range detail.Members {
		profile := model.UserProfilePresentation{}
		if member.Active {
			profile = profiles[member.ActorID]
		}
		name := strings.TrimSpace(profile.Nickname)
		if name == "" {
			name = "项目成员"
		}
		out.Members = append(out.Members, projectNoteMemberPresentation{
			ProjectNoteMember: member,
			DisplayName:       name,
			Avatar:            sanitizePublicImageURL(profile.Avatar),
		})
	}
	for _, confirmation := range detail.Confirmations {
		profile := profiles[confirmation.ConfirmerID]
		name := strings.TrimSpace(profile.Nickname)
		if name == "" {
			name = "方案确认人"
		}
		out.Confirmations = append(out.Confirmations, projectNoteConfirmationPresentation{
			ProjectNoteConfirmation: confirmation,
			DisplayName:             name,
			Avatar:                  sanitizePublicImageURL(profile.Avatar),
		})
	}
	return out, nil
}
