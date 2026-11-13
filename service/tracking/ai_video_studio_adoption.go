package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	aiVideoStudioContentAdoptionKind = "ai_video_content_adoption"
	aiVideoStudioNoteAdoptionKind    = "ai_video_note_adoption"
)

// AIVideoStudioContentAdoption contains only server-derived source and media
// facts plus the client's stable draft identity. The caller must copy the
// source bytes into Asset.ID before entering this transaction.
type AIVideoStudioContentAdoption struct {
	DraftInstanceID string `json:"draft_instance_id"`
	SourceMediaID   string `json:"source_media_id"`
	SourceTaskID    string `json:"source_task_id,omitempty"`
	Owner           string `json:"owner_id"`
	Title           string `json:"title"`
	Asset           Asset  `json:"asset"`
}

type AIVideoStudioProjectNoteAdoption struct {
	DraftInstanceID  string `json:"draft_instance_id"`
	SourceMediaID    string `json:"source_media_id"`
	SourceTaskID     string `json:"source_task_id,omitempty"`
	ProjectID        string `json:"project_id"`
	NoteID           string `json:"note_id"`
	ExpectedRevision int    `json:"expected_revision"`
	InsertAt         int    `json:"insert_at"`
	MediaID          string `json:"media_id"`
}

type aiVideoStudioContentAdoptionRecord struct {
	Request AIVideoStudioContentAdoption `json:"request"`
	Content Content                      `json:"content"`
}

type aiVideoStudioNoteAdoptionRecord struct {
	Request AIVideoStudioProjectNoteAdoption `json:"request"`
	Note    ProjectNote                      `json:"note"`
}

func aiVideoStudioAdoptionID(kind string, actor int64, draftInstanceID string) string {
	return hash(fmt.Sprintf("%s:%d:%s", kind, actor, draftInstanceID))
}

func validAIVideoStudioAdoptionIdentity(draftInstanceID, sourceMediaID, sourceTaskID string) bool {
	return validID(draftInstanceID) && validID(sourceMediaID) && (sourceTaskID == "" || validID(sourceTaskID))
}

func sameAIVideoStudioContentAdoption(a, b AIVideoStudioContentAdoption) bool {
	return a.DraftInstanceID == b.DraftInstanceID && a.SourceMediaID == b.SourceMediaID && a.SourceTaskID == b.SourceTaskID &&
		a.Owner == b.Owner && strings.TrimSpace(a.Title) == strings.TrimSpace(b.Title) && sameMediaObject(a.Asset, b.Asset)
}

func sameAIVideoStudioNoteAdoption(a, b AIVideoStudioProjectNoteAdoption) bool {
	return a == b
}

// AdoptAIVideoStudioContent registers the copied public media and creates one
// unpublished content draft in the same tracking transaction.
func (s *Service) AdoptAIVideoStudioContent(ctx context.Context, scope Scope, input AIVideoStudioContentAdoption) (Content, error) {
	var out Content
	if !validAIVideoStudioAdoptionIdentity(input.DraftInstanceID, input.SourceMediaID, input.SourceTaskID) || input.Asset.Kind != "video" || input.Asset.MIME != "video/mp4" || input.Asset.Width < 1 || input.Asset.Height < 1 {
		return out, fail(400, "AI视频草稿来源或媒体无效")
	}
	document, err := json.Marshal(ArticleDocument{Schema: 1, Presentation: "video", Blocks: []ArticleBlock{{Kind: "video", MediaID: input.Asset.ID}}})
	if err != nil {
		return out, ErrUnavailable
	}
	draft, err := s.validateDraft(scope, Draft{
		Title: strings.TrimSpace(input.Title), Body: string(document), BodyFormat: "article_v2",
		SourceNote: "AI视频创作素材 " + input.SourceMediaID, Rights: "own", Owner: input.Owner,
		MediaIDs: []string{input.Asset.ID},
	})
	if err != nil {
		return out, err
	}
	id := aiVideoStudioAdoptionID(aiVideoStudioContentAdoptionKind, scope.Actor, input.DraftInstanceID)
	err = s.transaction(ctx, func(r Repository) error {
		stored, readErr := decode[aiVideoStudioContentAdoptionRecord](r, aiVideoStudioContentAdoptionKind, id)
		if readErr == nil {
			if !sameAIVideoStudioContentAdoption(stored.Request, input) {
				return fail(409, "草稿实例已用于其他AI视频")
			}
			current, loadErr := loadCatalogContent(r, "content", stored.Content.ID)
			if loadErr != nil || current.Owner != stored.Content.Owner || !scope.Allows(current.Owner) {
				return fail(404, "素材不可访问")
			}
			if current.Status == "deleted" {
				return fail(409, "已删除草稿不能恢复")
			}
			if current.Status == "revoked" {
				return fail(409, "撤回素材不可重新回填")
			}
			out = current
			return nil
		}
		if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		now := s.Now().UTC()
		if err := s.addMediaIn(r, scope, input.Asset, input.Owner, now); err != nil {
			return err
		}
		out, err = s.saveContentIn(r, scope, "", draft, now)
		if err != nil {
			return err
		}
		return r.Insert(record(aiVideoStudioContentAdoptionKind, id, input.Owner, now, aiVideoStudioContentAdoptionRecord{Request: input, Content: out}))
	})
	return out, err
}

// AdoptAIVideoStudioProjectNote inserts one already-finalized private video at
// the exact selected block position. It never changes title, cover or existing blocks.
func (s *Service) AdoptAIVideoStudioProjectNote(ctx context.Context, scope Scope, input AIVideoStudioProjectNoteAdoption) (ProjectNote, error) {
	var out ProjectNote
	if !validAIVideoStudioAdoptionIdentity(input.DraftInstanceID, input.SourceMediaID, input.SourceTaskID) || !validID(input.ProjectID) || !validID(input.NoteID) || !validID(input.MediaID) || input.ExpectedRevision < 1 || input.InsertAt < 0 {
		return out, fail(400, "AI视频项目回填参数无效")
	}
	id := aiVideoStudioAdoptionID(aiVideoStudioNoteAdoptionKind, scope.Actor, input.DraftInstanceID)
	err := s.transaction(ctx, func(r Repository) error {
		stored, readErr := decode[aiVideoStudioNoteAdoptionRecord](r, aiVideoStudioNoteAdoptionKind, id)
		if readErr == nil {
			if !sameAIVideoStudioNoteAdoption(stored.Request, input) {
				return fail(409, "草稿实例已用于其他项目位置")
			}
			current, currentErr := projectNoteRequired[ProjectNote](r, projectNoteKind, stored.Note.ID, "项目笔记不存在")
			if currentErr != nil {
				return currentErr
			}
			project, projectErr := s.projectNoteManaged(r, scope, stored.Request.ProjectID)
			if projectErr != nil {
				return projectErr
			}
			if current.ProjectID != project.ID {
				return fail(404, "项目笔记不存在")
			}
			if activeErr := projectNoteRequireActive(r, current.ID); activeErr != nil {
				return activeErr
			}
			out = stored.Note
			return nil
		}
		if !errors.Is(readErr, ErrNotFound) {
			return readErr
		}
		note, err := projectNoteRequired[ProjectNote](r, projectNoteKind, input.NoteID, "项目笔记不存在")
		if err != nil {
			return err
		}
		project, err := s.projectNoteManaged(r, scope, input.ProjectID)
		if err != nil {
			return err
		}
		if note.ProjectID != project.ID {
			return fail(404, "项目笔记不存在")
		}
		if err = projectNoteRequireActive(r, note.ID); err != nil {
			return err
		}
		if note.Revision != input.ExpectedRevision {
			return fail(409, "项目笔记已更新，请重新读取")
		}
		if note.BodyFormat != "article_v2" {
			return fail(409, "请先将项目笔记转为块编辑格式")
		}
		privateMedia, err := projectNoteRequired[ProjectNoteMedia](r, projectNoteMediaKind, input.MediaID, "私人项目媒体不存在")
		if err != nil {
			return err
		}
		if privateMedia.ProjectID != project.ID || privateMedia.Owner != project.Owner || privateMedia.Asset.Kind != "video" || privateMedia.Asset.MIME != "video/mp4" {
			return fail(404, "私人项目媒体不存在")
		}
		var document ArticleDocument
		if json.Unmarshal([]byte(note.Body), &document) != nil || document.Schema != 1 {
			return ErrUnavailable
		}
		if input.InsertAt > len(document.Blocks) || len(document.Blocks) >= maxArticleBlocks {
			return fail(409, "选定的笔记插入位置已失效")
		}
		document.Blocks = append(document.Blocks, ArticleBlock{})
		copy(document.Blocks[input.InsertAt+1:], document.Blocks[input.InsertAt:])
		document.Blocks[input.InsertAt] = ArticleBlock{Kind: "video", MediaID: input.MediaID}
		body, err := json.Marshal(document)
		if err != nil {
			return ErrUnavailable
		}
		draft, err := s.projectNoteValidateDraft(scope, project.Owner, ProjectNoteDraft{
			Title: note.Title, Body: string(body), BodyFormat: note.BodyFormat,
			CoverID: note.CoverID, MediaIDs: append(append([]string(nil), note.MediaIDs...), input.MediaID),
		})
		if err != nil {
			return err
		}
		if _, _, err = projectNoteSnapshotAssets(r, project.ID, project.Owner, draft); err != nil {
			return err
		}
		now := s.Now().UTC()
		note.Revision++
		note.Body, note.BodyFormat = draft.Body, draft.BodyFormat
		note.CoverID, note.MediaIDs, note.UpdatedAt = draft.CoverID, draft.MediaIDs, now
		out = note
		if err = r.Put(projectNoteRecord(projectNoteKind, note.ID, project.Owner, now, note, projectNoteActorKey(project.DesignerID), project.ID, project.ID, "")); err != nil {
			return err
		}
		current, hasCurrent, err := projectNoteCurrentVersion(r, project)
		if err != nil {
			return err
		}
		if hasCurrent && current.NoteID == note.ID {
			project.CurrentVersionID = current.ID
			project.Status = ProjectNoteProjectStatusRevising
			project.Revision++
			project.UpdatedAt = now
			if err = r.Put(projectNoteRecord(projectNoteProjectKind, project.ID, project.Owner, now, project, projectNoteActorKey(project.DesignerID), "", project.ID, "")); err != nil {
				return err
			}
		}
		return r.Insert(projectNoteRecord(aiVideoStudioNoteAdoptionKind, id, project.Owner, now, aiVideoStudioNoteAdoptionRecord{Request: input, Note: out}, projectNoteActorKey(project.DesignerID), project.ID, note.ID, ""))
	})
	return out, err
}
