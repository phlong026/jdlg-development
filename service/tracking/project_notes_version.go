package tracking

import "context"

// ProjectNoteGetVersion reads an exact immutable version rather than the
// latest draft or the management detail's bounded history window.
func (s *Service) ProjectNoteGetVersion(ctx context.Context, scope Scope, projectID, versionID string) (ProjectNoteVersion, error) {
	var out ProjectNoteVersion
	if !validID(projectID) || !validID(versionID) {
		return out, fail(404, "私人方案版本不可访问")
	}
	err := s.readTransaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		if err = s.projectNoteGrant(ctx, r, project); err != nil {
			return err
		}
		version, err := projectNoteRequired[ProjectNoteVersion](r, projectNoteVersionKind, versionID, "私人方案版本不可访问")
		if err != nil {
			return err
		}
		if version.ProjectID != project.ID {
			return fail(404, "私人方案版本不可访问")
		}
		out = version
		return nil
	})
	return out, err
}

// ProjectNoteSharedVersion performs the same current invitation and share
// checks as reading the note, without fetching feedback for a file export.
func (s *Service) ProjectNoteSharedVersion(ctx context.Context, token string, actor int64) (ProjectNoteVersion, error) {
	var out ProjectNoteVersion
	err := s.readTransaction(ctx, func(r Repository) error {
		view, _, err := s.projectNoteReadShareIn(ctx, r, token, actor)
		if err != nil {
			return err
		}
		out = view.Version
		return nil
	})
	return out, err
}
