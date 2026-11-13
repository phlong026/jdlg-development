package tracking

import (
	"context"
	"encoding/json"
)

type ProjectNoteMemberDirectory struct {
	Project    ProjectNoteProject          `json:"project"`
	Versions   []ProjectNoteVersionSummary `json:"versions"`
	NextCursor string                      `json:"next_cursor,omitempty"`
}

// Members receive only currently shared or explicitly delivered versions.
// Freezing alone grants no member access and never opens management drafts.
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
		candidates := make(map[string]bool, len(history.Versions))
		for _, version := range history.Versions {
			candidates[version.ID] = true
		}
		allowed, err := s.projectNoteMemberDeliveredVersions(r, project, actor, candidates)
		if err != nil {
			return err
		}
		// The current working version may be an internal frozen draft.
		project.CurrentVersionID = ""
		out.Project, out.NextCursor = project, history.NextCursor
		for _, version := range history.Versions {
			if allowed[version.ID] {
				out.Versions = append(out.Versions, version)
			}
		}
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteMemberVersion(ctx context.Context, actor int64, projectID, versionID string) (ProjectNoteShareView, error) {
	var out ProjectNoteShareView
	err := s.readQueryTransaction(ctx, func(r Repository) error {
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
		allowed, err := s.projectNoteMemberDeliveredVersions(r, project, actor, map[string]bool{version.ID: true})
		if err != nil {
			return err
		}
		if !allowed[version.ID] {
			return fail(404, "方案尚未向当前成员共享，或分享已失效")
		}
		project.CurrentVersionID = ""
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

// Reads are project scoped and paginated with the shared query budget. No
// fixed "recent N" window may turn an older active grant into a denial.
func (s *Service) projectNoteMemberDeliveredVersions(r Repository, project ProjectNoteProject, actor int64, candidates map[string]bool) (map[string]bool, error) {
	allowed := make(map[string]bool)
	if len(candidates) == 0 {
		return allowed, nil
	}
	for _, kind := range []string{projectNoteShareKind, projectPackageDeliveryKind} {
		query := RecordQuery{Owners: []string{project.Owner}, Sessions: []string{project.ID}, Descending: true, Limit: 100}
		if kind == projectNoteShareKind && len(candidates) == 1 {
			for id := range candidates {
				query.Content = id
			}
		}
		for {
			rows, err := pageRecords(r, kind, query)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				if kind == projectNoteShareKind {
					var share projectNoteShareRecord
					if json.Unmarshal(row.Payload, &share) != nil || share.ProjectID != project.ID || share.ID == "" || share.TokenHash != row.ID {
						return nil, ErrUnavailable
					}
					if !share.Revoked && s.Now().Before(share.ExpiresAt) && candidates[share.VersionID] {
						allowed[share.VersionID] = true
					}
				} else {
					var delivery projectPackageDeliveryRecord
					if json.Unmarshal(row.Payload, &delivery) != nil || delivery.ProjectID != project.ID || delivery.ID == "" || delivery.TokenHash != row.ID {
						return nil, ErrUnavailable
					}
					if !delivery.Revoked && s.Now().Before(delivery.ExpiresAt) && projectPackageRecipient(delivery.RecipientIDs, actor) {
						for _, item := range delivery.Items {
							if candidates[item.VersionID] {
								allowed[item.VersionID] = true
							}
						}
					}
				}
			}
			if len(allowed) == len(candidates) || len(rows) < query.Limit {
				break
			}
			last := rows[len(rows)-1]
			query.BeforeAt, query.BeforeID = last.At, last.ID
		}
		if len(allowed) == len(candidates) {
			break
		}
	}
	return allowed, nil
}
