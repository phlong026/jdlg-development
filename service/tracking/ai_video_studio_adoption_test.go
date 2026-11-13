package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
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

func TestStudioPlainNoteConvertsPreservesMediaThenSavesFreezesAndReads(t *testing.T) {
	ctx := context.Background()
	svc, scope, _ := projectNoteFixture(t)
	project, err := svc.ProjectNoteCreateProject(ctx, scope, ProjectNoteProjectInput{Name: "测试宅", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range []Asset{
		{ID: "plain-cover-0001", Kind: "image", MIME: "image/jpeg", Width: 800, Height: 600},
		{ID: "plain-video-0001", Kind: "video", MIME: "video/mp4", Width: 1280, Height: 720},
	} {
		asset.URL = projectNoteMediaURL(project.ID, asset.ID)
		op, _, err := svc.ProjectNotePrepareMedia(ctx, scope, project.ID, asset, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "ai_video_studio")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = svc.ProjectNoteFinalizeMedia(ctx, scope, project.ID, asset.ID, op); err != nil {
			t.Fatal(err)
		}
	}
	note, err := svc.ProjectNoteCreateNote(ctx, scope, project.ID, ProjectNoteDraft{Title: "原方案", Body: "原正文上\n\n原正文下", BodyFormat: "plain", CoverID: "plain-cover-0001", MediaIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	target, err := svc.StudioProjectNoteAdoptionTarget(ctx, scope, project.ID, note.ID)
	if err != nil || len(target.Blocks) != 2 {
		t.Fatalf("plain target: %+v %v", target, err)
	}
	adopted, err := svc.AdoptAIVideoStudioProjectNote(ctx, scope, AIVideoStudioProjectNoteAdoption{DraftInstanceID: "plain-adoption-001", SourceMediaID: "studio-media-001", ProjectID: project.ID, NoteID: note.ID, ExpectedRevision: note.Revision, InsertAt: 1, MediaID: "plain-video-0001"})
	if err != nil {
		t.Fatal(err)
	}
	var doc ArticleDocument
	if json.Unmarshal([]byte(adopted.Body), &doc) != nil || len(doc.Blocks) != 3 || doc.Blocks[0].Text != note.Body || doc.Blocks[1].MediaID != "plain-video-0001" || doc.Blocks[2].MediaID != note.CoverID || adopted.CoverID != note.CoverID || adopted.BodyFormat != "article_v2" {
		t.Fatalf("lost original note: %+v", adopted)
	}
	saved, err := svc.ProjectNoteSaveNote(ctx, scope, note.ID, ProjectNoteDraft{Title: "保留视频后保存", Body: adopted.Body, BodyFormat: adopted.BodyFormat, CoverID: adopted.CoverID, MediaIDs: adopted.MediaIDs}, adopted.Revision)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := svc.ProjectNoteFreezeVersion(ctx, scope, note.ID, saved.Revision)
	if err != nil || frozen.Body != adopted.Body || len(frozen.Media) != 2 {
		t.Fatalf("freeze: %+v %v", frozen, err)
	}
	share, err := svc.ProjectNoteCreateShare(ctx, scope, frozen.ID, svc.Now().Add(time.Hour), "plain-share-001")
	if err != nil {
		t.Fatal(err)
	}
	read, err := svc.ProjectNoteReadShare(ctx, share.Token, 102)
	if err != nil || read.Version.Body != adopted.Body {
		t.Fatalf("shared read lost adopted video: %+v %v", read, err)
	}
}

func TestStudioNoteAdoptionRejectsArchivedWritesAndReplayReturnsCurrentRevision(t *testing.T) {
	ctx := context.Background()
	svc, scope := fixture(t)
	project, err := svc.ProjectNoteCreateProject(ctx, scope, ProjectNoteProjectInput{Name: "归档宅", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	asset := Asset{ID: "archived-video-01", Kind: "video", MIME: "video/mp4", URL: projectNoteMediaURL(project.ID, "archived-video-01"), Width: 1280, Height: 720}
	op, _, err := svc.ProjectNotePrepareMedia(ctx, scope, project.ID, asset, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "ai_video_studio")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectNoteFinalizeMedia(ctx, scope, project.ID, asset.ID, op); err != nil {
		t.Fatal(err)
	}
	note, err := svc.ProjectNoteCreateNote(ctx, scope, project.ID, ProjectNoteDraft{Title: "原稿", Body: "原正文", BodyFormat: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	input := AIVideoStudioProjectNoteAdoption{DraftInstanceID: "archived-adoption", SourceMediaID: "studio-media-0001", ProjectID: project.ID, NoteID: note.ID, ExpectedRevision: note.Revision, InsertAt: 1, MediaID: asset.ID}
	adopted, err := svc.AdoptAIVideoStudioProjectNote(ctx, scope, input)
	if err != nil {
		t.Fatal(err)
	}
	newer, err := svc.ProjectNoteSaveNote(ctx, scope, note.ID, ProjectNoteDraft{Title: "后来改的标题", Body: adopted.Body, BodyFormat: adopted.BodyFormat, MediaIDs: adopted.MediaIDs}, adopted.Revision)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.AdoptAIVideoStudioProjectNote(ctx, scope, input)
	if err != nil || replay.Revision != newer.Revision || replay.Title != newer.Title {
		t.Fatalf("replay returned stale snapshot: %+v %v", replay, err)
	}
	archived := ProjectNoteProjectStatusArchived
	if _, err = svc.ProjectNoteUpdateProject(ctx, scope, project.ID, ProjectNoteProjectUpdate{ExpectedRevision: project.Revision, Status: &archived}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.StudioProjectNoteAdoptionTarget(ctx, scope, project.ID, note.ID); err == nil {
		t.Fatal("archived target remained editable")
	}
	input.DraftInstanceID = "archived-new-write"
	input.ExpectedRevision = newer.Revision
	if _, err = svc.AdoptAIVideoStudioProjectNote(ctx, scope, input); err == nil {
		t.Fatal("archived note accepted insertion")
	}
	current, err := svc.ProjectNoteGetNote(ctx, scope, project.ID, note.ID)
	if err != nil || current.Revision != newer.Revision {
		t.Fatalf("archived revision changed: %+v %v", current, err)
	}
}
