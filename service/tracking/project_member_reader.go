package tracking

import "context"

type ProjectNoteMemberDirectory struct {
	Project    ProjectNoteProject          `json:"project"`
	Versions   []ProjectNoteVersionSummary `json:"versions"`
	NextCursor string                      `json:"next_cursor,omitempty"`
}

// Members receive immutable versions only. This never grants access to the
// designer's editable drafts, invitations or management operations.
func (s *Service) ProjectNoteMemberDirectory(ctx context.Context, actor int64, projectID string, limit int, cursor string) (ProjectNoteMemberDirectory, error) {
	out := ProjectNoteMemberDirectory{Versions: []ProjectNoteVersionSummary{}}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		project, err := projectNoteLoadProject(r, projectID)
		if err != nil {
			return err
		}
		if err = s.projectNoteReadAccess(ctx, r, project, actor); err != nil {
			return err
		}
		history, err := projectNoteHistoryPage(r, projectID, ProjectNoteHistoryVersions, "", limit, cursor)
		if err != nil {
			return err
		}
		out.Project, out.Versions, out.NextCursor = project, history.Versions, history.NextCursor
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteMemberVersion(ctx context.Context, actor int64, projectID, versionID string) (ProjectNoteShareView, error) {
	var out ProjectNoteShareView
	err := s.readTransaction(ctx, func(r Repository) error {
		project, err := projectNoteLoadProject(r, projectID)
		if err != nil {
			return err
		}
		if err = s.projectNoteReadAccess(ctx, r, project, actor); err != nil {
			return err
		}
		version, err := projectNoteRequired[ProjectNoteVersion](r, projectNoteVersionKind, versionID, "方案版本不可访问")
		if err != nil {
			return err
		}
		if version.ProjectID != project.ID {
			return fail(404, "方案版本不可访问")
		}
		out = ProjectNoteShareView{Project: project, Version: version, Feedbacks: []ProjectNoteFeedback{}, Resolutions: []ProjectNoteFeedbackResolution{}}
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteMemberMedia(ctx context.Context, actor int64, projectID, versionID, mediaID string) (Asset, error) {
	view, err := s.ProjectNoteMemberVersion(ctx, actor, projectID, versionID)
	if err != nil {
		return Asset{}, err
	}
	if view.Version.Cover != nil && view.Version.Cover.ID == mediaID {
		return *view.Version.Cover, nil
	}
	for _, asset := range view.Version.Media {
		if asset.ID == mediaID {
			return asset, nil
		}
	}
	return Asset{}, fail(404, "媒体不可访问")
}
