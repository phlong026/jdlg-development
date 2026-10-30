package tracking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	projectNoteMediaKind        = "note_media"
	projectNoteMediaPendingKind = "note_media_pending"
	projectNoteMediaObjectKind  = "note_media_object"
)

type ProjectNoteMedia struct {
	ProjectID string    `json:"project_id"`
	Owner     string    `json:"owner_id"`
	Asset     Asset     `json:"asset"`
	CreatedAt time.Time `json:"created_at"`
}

type ProjectNoteMediaPage struct {
	Items      []ProjectNoteMedia `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type projectNoteMediaJournal struct {
	ProjectID string    `json:"project_id"`
	Owner     string    `json:"owner_id"`
	Asset     Asset     `json:"asset"`
	SHA256    string    `json:"sha256"`
	Origin    string    `json:"origin"`
	Operation string    `json:"operation"`
	CreatedAt time.Time `json:"created_at"`
}

func projectNoteMediaURL(projectID, mediaID string) string {
	return fmt.Sprintf("/api/v1/miniprogram/project-notes/projects/%s/media/%s", projectID, mediaID)
}

func validateProjectNoteMedia(project ProjectNoteProject, asset Asset, sha256, origin string) error {
	sha256 = strings.ToLower(sha256)
	if !validID(asset.ID) || asset.URL != projectNoteMediaURL(project.ID, asset.ID) || !validSHA256(sha256) || !validMediaOrigin(origin) {
		return fail(400, "私人项目媒体标识或校验信息无效")
	}
	if len(asset.Title) > 200 || asset.Width < 1 || asset.Width > 20000 || asset.Height < 1 || asset.Height > 20000 {
		return fail(400, "私人项目媒体元数据无效")
	}
	allowedImage := asset.MIME == "image/png" || asset.MIME == "image/jpeg" || asset.MIME == "image/gif" || asset.MIME == "image/webp"
	validType := asset.Kind == "image" && allowedImage || asset.Kind == "video" && asset.MIME == "video/mp4"
	if !validType {
		return fail(400, "私人项目媒体类型无效")
	}
	return nil
}

func sameProjectNoteMediaJournal(left, right projectNoteMediaJournal) bool {
	return left.ProjectID == right.ProjectID && left.Owner == right.Owner && sameMediaObject(left.Asset, right.Asset) && left.SHA256 == right.SHA256 && left.Origin == right.Origin
}

// ProjectNotePrepareMedia records a private-object intent without publishing
// the asset into the enterprise media catalog.
func (s *Service) ProjectNotePrepareMedia(ctx context.Context, scope Scope, projectID string, asset Asset, sha256, origin string) (string, bool, error) {
	sha256 = strings.ToLower(sha256)
	var operation string
	already := false
	err := s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		if err = validateProjectNoteMedia(project, asset, sha256, origin); err != nil {
			return err
		}
		if _, err = r.Get("media", asset.ID); err == nil {
			return fail(409, "媒体标识已被企业素材占用")
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		expected := projectNoteMediaJournal{ProjectID: project.ID, Owner: project.Owner, Asset: asset, SHA256: sha256, Origin: origin}
		if proof, readErr := decode[projectNoteMediaJournal](r, projectNoteMediaObjectKind, asset.ID); readErr == nil {
			stored, storedErr := decode[ProjectNoteMedia](r, projectNoteMediaKind, asset.ID)
			if storedErr != nil {
				return storedErr
			}
			if !sameProjectNoteMediaJournal(proof, expected) || stored.ProjectID != project.ID || stored.Owner != project.Owner || stored.Asset != proof.Asset {
				return fail(409, "私人项目媒体标识冲突")
			}
			returnOperation := proof.Operation
			operation, already = returnOperation, true
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		if _, err = r.Get(projectNoteMediaKind, asset.ID); err == nil {
			return fail(409, "私人项目媒体缺少校验记录")
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if pending, readErr := decode[projectNoteMediaJournal](r, projectNoteMediaPendingKind, asset.ID); readErr == nil {
			if !sameProjectNoteMediaJournal(pending, expected) {
				return fail(409, "私人项目媒体标识冲突")
			}
			operation = pending.Operation
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		expected.Operation, expected.CreatedAt = opaque(), now
		operation = expected.Operation
		return r.Insert(projectNoteRecord(projectNoteMediaPendingKind, asset.ID, project.Owner, now, expected, projectNoteActorKey(project.DesignerID), project.ID, asset.ID, ""))
	})
	return operation, already, err
}

// ProjectNoteFinalizeMedia exposes only project-scoped metadata after the
// caller has completed the private object-store write.
func (s *Service) ProjectNoteFinalizeMedia(ctx context.Context, scope Scope, projectID, mediaID, operation string) (ProjectNoteMedia, error) {
	var out ProjectNoteMedia
	if !validID(mediaID) || !validID(operation) {
		return out, fail(400, "私人项目媒体完成凭据无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		if proof, readErr := decode[projectNoteMediaJournal](r, projectNoteMediaObjectKind, mediaID); readErr == nil {
			if proof.ProjectID != project.ID || proof.Owner != project.Owner || proof.Operation != operation {
				return fail(409, "私人项目媒体完成凭据冲突")
			}
			stored, storedErr := decode[ProjectNoteMedia](r, projectNoteMediaKind, mediaID)
			if storedErr != nil || stored.ProjectID != project.ID || stored.Asset != proof.Asset {
				return ErrUnavailable
			}
			out = stored
			return nil
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		pending, err := decode[projectNoteMediaJournal](r, projectNoteMediaPendingKind, mediaID)
		if errors.Is(err, ErrNotFound) {
			return fail(409, "私人项目媒体待完成记录不存在")
		}
		if err != nil {
			return err
		}
		if pending.ProjectID != project.ID || pending.Owner != project.Owner || pending.Operation != operation || pending.Asset.ID != mediaID {
			return fail(409, "私人项目媒体完成凭据冲突")
		}
		now := s.Now().UTC()
		out = ProjectNoteMedia{ProjectID: project.ID, Owner: project.Owner, Asset: pending.Asset, CreatedAt: now}
		if err = r.Insert(projectNoteRecord(projectNoteMediaKind, mediaID, project.Owner, now, out, projectNoteActorKey(project.DesignerID), project.ID, mediaID, "")); err != nil {
			return err
		}
		if err = r.Insert(projectNoteRecord(projectNoteMediaObjectKind, mediaID, project.Owner, now, pending, projectNoteActorKey(project.DesignerID), project.ID, mediaID, "")); err != nil {
			return err
		}
		return r.Remove(projectNoteMediaPendingKind, mediaID)
	})
	return out, err
}

func (s *Service) ProjectNoteGetMedia(ctx context.Context, scope Scope, projectID, mediaID string) (ProjectNoteMedia, error) {
	var out ProjectNoteMedia
	err := s.readTransaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		media, err := projectNoteRequired[ProjectNoteMedia](r, projectNoteMediaKind, mediaID, "私人项目媒体不存在")
		if err != nil {
			return err
		}
		if media.ProjectID != project.ID || media.Owner != project.Owner {
			return fail(404, "私人项目媒体不存在")
		}
		out = media
		return nil
	})
	return out, err
}

func (s *Service) ProjectNoteListMedia(ctx context.Context, scope Scope, projectID string, limit int, cursor string) (ProjectNoteMediaPage, error) {
	out := ProjectNoteMediaPage{Items: []ProjectNoteMedia{}}
	if limit <= 0 {
		limit = 30
	}
	if limit > 50 {
		limit = 50
	}
	query := RecordQuery{Sessions: []string{projectID}, Descending: true, Limit: limit + 1}
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		var parsed projectNoteCursor
		if err != nil || json.Unmarshal(decoded, &parsed) != nil || parsed.ID == "" || parsed.At.IsZero() {
			return out, fail(400, "分页游标无效")
		}
		query.BeforeAt, query.BeforeID = parsed.At, parsed.ID
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		project, err := s.projectNoteManaged(r, scope, projectID)
		if err != nil {
			return err
		}
		rows, err := pageRecords(r, projectNoteMediaKind, query)
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
			var media ProjectNoteMedia
			if json.Unmarshal(row.Payload, &media) != nil || media.ProjectID != project.ID || media.Owner != project.Owner {
				return ErrUnavailable
			}
			out.Items = append(out.Items, media)
		}
		return nil
	})
	return out, err
}
