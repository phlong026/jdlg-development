package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestAdoptStudioVideoAsContentDraftIsAtomicAndIdempotent(t *testing.T) {
	svc, scope := fixture(t)
	input := AIVideoStudioContentAdoption{
		DraftInstanceID: "studio-draft-0001",
		SourceMediaID:   "studio-media-0001",
		SourceTaskID:    "studio-task-0001",
		Owner:           "owner-a",
		Title:           "庭院漫游",
		Asset: Asset{
			ID: "public-video-0001", Kind: "video", MIME: "video/mp4",
			URL: "/api/v1/miniprogram/tracking/media/public-video-0001", Width: 1280, Height: 720,
		},
	}

	first, err := svc.AdoptAIVideoStudioContent(context.Background(), scope, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.AdoptAIVideoStudioContent(context.Background(), scope, input)
	if err != nil || second.ID != first.ID || second.Revision != 1 || second.Status != "draft" {
		t.Fatalf("retry must return the same draft: first=%+v second=%+v err=%v", first, second, err)
	}
	if first.Cover != nil || len(first.Media) != 1 || first.Media[0].ID != input.Asset.ID {
		t.Fatalf("content adoption changed cover or lost video: %+v", first)
	}
	var document ArticleDocument
	if json.Unmarshal([]byte(first.Body), &document) != nil || len(document.Blocks) != 1 || document.Blocks[0].Kind != "video" || document.Blocks[0].MediaID != input.Asset.ID {
		t.Fatalf("unexpected content body: %s", first.Body)
	}

	conflict := input
	conflict.SourceMediaID = "studio-media-0002"
	if _, err = svc.AdoptAIVideoStudioContent(context.Background(), scope, conflict); err == nil {
		t.Fatal("same draft instance accepted different source")
	}
	if _, err = svc.DeleteContentDraft(context.Background(), scope, first.ID, first.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AdoptAIVideoStudioContent(context.Background(), scope, input); err == nil {
		t.Fatal("deleted content was exposed by idempotent replay")
	}
}

func TestAdoptStudioVideoIntoProjectNotePreservesDocumentAndRejectsStaleRevision(t *testing.T) {
	svc, scope := fixture(t)
	project, err := svc.ProjectNoteCreateProject(context.Background(), scope, ProjectNoteProjectInput{Name: "林宅", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	privateVideo := Asset{ID: "private-video-0001", Kind: "video", MIME: "video/mp4", URL: projectNoteMediaURL(project.ID, "private-video-0001"), Width: 1280, Height: 720}
	op, _, err := svc.ProjectNotePrepareMedia(context.Background(), scope, project.ID, privateVideo, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "ai_video_studio")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectNoteFinalizeMedia(context.Background(), scope, project.ID, privateVideo.ID, op); err != nil {
		t.Fatal(err)
	}
	original := ArticleDocument{Schema: 1, Blocks: []ArticleBlock{{Kind: "paragraph", Text: "原正文上"}, {Kind: "paragraph", Text: "原正文下"}}}
	body, _ := json.Marshal(original)
	note, err := svc.ProjectNoteCreateNote(context.Background(), scope, project.ID, ProjectNoteDraft{Title: "方案", Body: string(body), BodyFormat: "article_v2"})
	if err != nil {
		t.Fatal(err)
	}

	input := AIVideoStudioProjectNoteAdoption{
		DraftInstanceID: "note-draft-0001", SourceMediaID: "studio-media-0001", SourceTaskID: "studio-task-0001",
		ProjectID: project.ID, NoteID: note.ID, ExpectedRevision: note.Revision, InsertAt: 1, MediaID: privateVideo.ID,
	}
	updated, err := svc.AdoptAIVideoStudioProjectNote(context.Background(), scope, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CoverID != note.CoverID || updated.Title != note.Title || updated.Revision != note.Revision+1 {
		t.Fatalf("note identity or cover changed: before=%+v after=%+v", note, updated)
	}
	var got ArticleDocument
	if json.Unmarshal([]byte(updated.Body), &got) != nil || len(got.Blocks) != 3 || got.Blocks[0].Text != "原正文上" || got.Blocks[1].MediaID != privateVideo.ID || got.Blocks[2].Text != "原正文下" {
		t.Fatalf("original blocks were not preserved: %s", updated.Body)
	}
	retry, err := svc.AdoptAIVideoStudioProjectNote(context.Background(), scope, input)
	if err != nil || retry.Revision != updated.Revision || retry.Body != updated.Body {
		t.Fatalf("response-loss retry did not converge: %+v %v", retry, err)
	}

	stale := input
	stale.DraftInstanceID = "note-draft-0002"
	if _, err = svc.AdoptAIVideoStudioProjectNote(context.Background(), scope, stale); err == nil {
		t.Fatal("stale revision was accepted")
	}
	wrongActor := scope
	wrongActor.Actor = 99
	wrongActor.Owners = []string{"owner-z"}
	input.DraftInstanceID = "note-draft-0003"
	if _, err = svc.AdoptAIVideoStudioProjectNote(context.Background(), wrongActor, input); err == nil {
		t.Fatal("different account reused an adoption request")
	}
	if _, err = svc.ProjectNoteDeleteProject(context.Background(), scope, project.ID, project.Revision); err != nil {
		t.Fatal(err)
	}
	input.DraftInstanceID = "note-draft-0001"
	if _, err = svc.AdoptAIVideoStudioProjectNote(context.Background(), scope, input); err == nil {
		t.Fatal("deleted project was exposed by idempotent replay")
	}
}

func TestAdoptStudioVideoProjectNoteRequiresExplicitLocationAndPrivateMedia(t *testing.T) {
	svc, scope := fixture(t)
	project, err := svc.ProjectNoteCreateProject(context.Background(), scope, ProjectNoteProjectInput{Name: "林宅", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := svc.ProjectNoteCreateNote(context.Background(), scope, project.ID, ProjectNoteDraft{Title: "方案", Body: `{"schema":1,"blocks":[{"kind":"paragraph","text":"正文"}]}`, BodyFormat: "article_v2"})
	if err != nil {
		t.Fatal(err)
	}
	base := AIVideoStudioProjectNoteAdoption{DraftInstanceID: "note-draft-0001", SourceMediaID: "studio-media-0001", ProjectID: project.ID, NoteID: note.ID, ExpectedRevision: note.Revision, MediaID: "private-video-0001"}
	base.InsertAt = -1
	if _, err = svc.AdoptAIVideoStudioProjectNote(context.Background(), scope, base); err == nil {
		t.Fatal("missing explicit insertion location was accepted")
	}
	base.InsertAt = 0
	if _, err = svc.AdoptAIVideoStudioProjectNote(context.Background(), scope, base); !errors.As(err, new(*Error)) {
		t.Fatalf("missing private media should return a domain error: %v", err)
	}
}
