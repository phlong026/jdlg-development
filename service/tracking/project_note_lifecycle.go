package tracking

import (
	"context"
	"errors"
	"time"
)

const projectNoteDeletionKind = "note_deletion"

// ProjectNoteDeletion is an append-only tombstone. The draft record, frozen
// versions, feedback, shares and project media remain intact for auditability.
type ProjectNoteDeletion struct {
	NoteID    string    `json:"note_id"`
	ProjectID string    `json:"project_id"`
	Revision  int       `json:"revision"`
	DeletedBy int64     `json:"deleted_by"`
	DeletedAt time.Time `json:"deleted_at"`
}

func projectNoteLoadDeletion(r Repository, noteID string) (ProjectNoteDeletion, bool, error) {
	deletion, err := decode[ProjectNoteDeletion](r, projectNoteDeletionKind, noteID)
	if errors.Is(err, ErrNotFound) {
		return ProjectNoteDeletion{}, false, nil
	}
	if err != nil {
		return ProjectNoteDeletion{}, false, err
	}
	if deletion.NoteID != noteID || deletion.ProjectID == "" || deletion.DeletedBy <= 0 || deletion.DeletedAt.IsZero() {
		return ProjectNoteDeletion{}, false, ErrUnavailable
	}
	return deletion, true, nil
}

func projectNoteRequireActive(r Repository, noteID string) error {
	_, deleted, err := projectNoteLoadDeletion(r, noteID)
	if err != nil {
		return err
	}
	if deleted {
		return fail(404, "项目笔记不存在")
	}
	return nil
}

// ProjectNoteCopy creates one new editable draft in the source project. It
// deliberately copies no membership, frozen version, share or feedback state.
func (s *Service) ProjectNoteCopy(ctx context.Context, scope Scope, noteID string, expectedRevision int) (ProjectNote, error) {
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
		draft, err := s.projectNoteValidateDraft(scope, project.Owner, ProjectNoteDraft{
			Title: note.Title, Body: note.Body, BodyFormat: note.BodyFormat,
			CoverID: note.CoverID, MediaIDs: append([]string(nil), note.MediaIDs...),
		})
		if err != nil {
			return err
		}
		if _, _, err = projectNoteSnapshotAssets(r, project.ID, project.Owner, draft); err != nil {
			return err
		}
		now := s.Now().UTC()
		out = ProjectNote{
			ID: opaque(), ProjectID: project.ID, Revision: 1,
			Title: draft.Title, Body: draft.Body, BodyFormat: draft.BodyFormat,
			CoverID: draft.CoverID, MediaIDs: append([]string(nil), draft.MediaIDs...),
			CreatedAt: now, UpdatedAt: now,
		}
		return r.Insert(projectNoteRecord(projectNoteKind, out.ID, project.Owner, now, out, projectNoteActorKey(project.DesignerID), project.ID, project.ID, ""))
	})
	return out, err
}

// ProjectNoteDelete writes a tombstone and retires only the draft's active
// index. It never removes drafts, media, frozen history or private activity.
func (s *Service) ProjectNoteDelete(ctx context.Context, scope Scope, noteID string, expectedRevision int) (ProjectNoteDeletion, error) {
	var out ProjectNoteDeletion
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
		noteRow, err := r.Get(projectNoteKind, note.ID)
		if err != nil {
			return err
		}
		now := s.Now().UTC()
		out = ProjectNoteDeletion{
			NoteID: note.ID, ProjectID: project.ID, Revision: note.Revision,
			DeletedBy: scope.Actor, DeletedAt: now,
		}
		if err = r.Insert(projectNoteRecord(projectNoteDeletionKind, note.ID, project.Owner, now, out, projectNoteActorKey(scope.Actor), project.ID, note.ID, "")); err != nil {
			return err
		}
		// Project detail lists active drafts through the project Session index.
		// Clearing only that index keeps the immutable payload and every history
		// relation available without an N+1 tombstone lookup.
		noteRow.Session = ""
		return r.Put(noteRow)
	})
	return out, err
}
