package route

import (
	"database/sql"
	"testing"

	"service/model"
)

const testTemplateDerivativeSHA1 = "0123456789abcdef0123456789abcdef01234567"

type fakeOriginalTaskLookup struct {
	task *model.AITask
	err  error
}

func (f fakeOriginalTaskLookup) GetByID(id int64) (*model.AITask, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.task, nil
}

func TestTemplateToResponseOnlyReportsUsableOriginalTask(t *testing.T) {
	template := &model.Template{ID: 1, OriginalTaskID: 99}

	missingTask := templateToResponse(template, false, nil, fakeOriginalTaskLookup{err: sql.ErrNoRows})
	if got := missingTask["has_original_task"]; got != false {
		t.Fatalf("expected missing original task to report false, got %#v", got)
	}

	existingTask := templateToResponse(template, false, nil, fakeOriginalTaskLookup{task: &model.AITask{ID: 99}})
	if got := existingTask["has_original_task"]; got != true {
		t.Fatalf("expected existing original task to report true, got %#v", got)
	}
}

func TestTemplateToResponseHidesPaidOriginalImagesUntilUnlocked(t *testing.T) {
	template := &model.Template{
		ID:         7,
		Thumbnail:  "https://static.jiadilingguang.com/derived_images/templates/admin/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg",
		PreviewURL: "https://static.jiadilingguang.com/derived_images/templates/admin/" + testTemplateDerivativeSHA1 + "_detail_preview.jpg",
		Images:     `["https://cdn.example.com/original-4k.jpg"]`,
		Price:      99,
		IsFree:     false,
	}

	locked := templateToResponse(template, true, nil, nil)
	if got := locked["images"]; got != "[]" {
		t.Fatalf("locked paid template leaked original images: %#v", got)
	}
	if got := locked["preview_url"]; got != template.PreviewURL {
		t.Fatalf("locked paid template lost its public preview: %#v", got)
	}
	unsafe := *template
	unsafe.Thumbnail = "https://cdn.example.com/original-thumb.jpg"
	unsafe.PreviewURL = "https://cdn.example.com/original-preview.jpg"
	unsafeLocked := templateToResponse(&unsafe, true, nil, nil)
	if unsafeLocked["thumbnail"] != "" || unsafeLocked["preview_url"] != "" {
		t.Fatalf("locked paid template exposed unverified preview assets: %#v", unsafeLocked)
	}
	spoofed := *template
	spoofed.Thumbnail = "https://evil.example.com/derived_images/templates/admin/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg"
	spoofed.PreviewURL = "https://evil.example.com/derived_images/templates/admin/" + testTemplateDerivativeSHA1 + "_detail_preview.jpg"
	spoofedLocked := templateToResponse(&spoofed, true, nil, nil)
	if spoofedLocked["thumbnail"] != "" || spoofedLocked["preview_url"] != "" {
		t.Fatalf("locked paid template accepted a spoofed derivative host: %#v", spoofedLocked)
	}

	unlocked := templateToResponse(template, false, nil, nil)
	if got := unlocked["images"]; got != template.Images {
		t.Fatalf("unlocked paid template lost original images: %#v", got)
	}
}

func TestPublicTemplateMediaFieldsProtectPaidOriginalsInLists(t *testing.T) {
	paid := &model.Template{
		Thumbnail:  "https://static.jiadilingguang.com/derived_images/templates/user_submit/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg",
		PreviewURL: "https://static.jiadilingguang.com/derived_images/templates/user_submit/" + testTemplateDerivativeSHA1 + "_detail_preview.jpg",
		Images:     `["https://cdn.example.com/original-4k.jpg"]`,
		Price:      99,
	}
	thumbnail, preview, images := publicTemplateMediaFields(paid)
	if thumbnail != paid.Thumbnail || preview != paid.PreviewURL || images != "[]" {
		t.Fatalf("paid public media projection = thumbnail=%q preview=%q images=%q", thumbnail, preview, images)
	}

	free := *paid
	free.IsFree = true
	free.Price = 0
	thumbnail, preview, images = publicTemplateMediaFields(&free)
	if thumbnail != free.Thumbnail || preview != free.PreviewURL || images != free.Images {
		t.Fatalf("free public media projection = thumbnail=%q preview=%q images=%q", thumbnail, preview, images)
	}
}

func TestPublicTemplateDerivedImageURLRejectsNonCanonicalURLs(t *testing.T) {
	base := "https://static.jiadilingguang.com/derived_images/templates/admin/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg"
	valid := []string{
		base,
		"https://static.jiadilingguang.com/derived_images/templates/album_upload/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg",
		"https://static.jiadilingguang.com/derived_images/templates/user_submit/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg",
		"https://static.jiadilingguang.com/derived_images/templates/7/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg",
	}
	for _, rawURL := range valid {
		if got := publicTemplateDerivedImageURL(rawURL, "list_thumb"); got != rawURL {
			t.Errorf("valid derivative %q rejected: %q", rawURL, got)
		}
	}

	invalid := []string{
		"https://user@static.jiadilingguang.com/derived_images/templates/admin/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg",
		base + "?download=1",
		base + "#original",
		"https://static.jiadilingguang.com/derived_images/templates/admin/../private/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg",
		"https://static.jiadilingguang.com/derived_images/templates/admin/%2e%2e/private/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg",
		"https://static.jiadilingguang.com/prefix/derived_images/templates/admin/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg",
		"https://static.jiadilingguang.com/derived_images/templates//" + testTemplateDerivativeSHA1 + "_list_thumb.jpg",
		"https://static.jiadilingguang.com/derived_images/templates/admin/0123456789abcdef_list_thumb.jpg",
		"https://static.jiadilingguang.com/derived_images/templates/admin/0123456789ABCDEF0123456789ABCDEF01234567_list_thumb.jpg",
		"https://static.jiadilingguang.com/derived_images/templates/admin/zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz_list_thumb.jpg",
		"https://static.jiadilingguang.com/derived_images/templates/admin/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg/extra",
	}
	for _, rawURL := range invalid {
		if got := publicTemplateDerivedImageURL(rawURL, "list_thumb"); got != "" {
			t.Errorf("non-canonical derivative %q accepted as %q", rawURL, got)
		}
	}
}

func TestFeaturedTemplateResponseUsesPaidMediaProjection(t *testing.T) {
	paid := &model.Template{
		ID:            9,
		Name:          "paid",
		Thumbnail:     "https://cdn.example.com/original-thumb.jpg",
		PreviewURL:    "https://cdn.example.com/original-preview.jpg",
		Images:        `["https://cdn.example.com/original-4k.jpg"]`,
		Price:         99,
		PublishScope:  "square",
		Status:        "published",
		CreatorUserID: 2,
	}

	response := featuredTemplateResponse(paid, nil)
	if response["thumbnail"] != "" || response["preview_url"] != "" {
		t.Fatalf("featured paid template leaked unverified media: %#v", response)
	}

	paid.Thumbnail = "https://static.jiadilingguang.com/derived_images/templates/album_upload/" + testTemplateDerivativeSHA1 + "_list_thumb.jpg"
	paid.PreviewURL = "https://static.jiadilingguang.com/derived_images/templates/album_upload/" + testTemplateDerivativeSHA1 + "_detail_preview.jpg"
	response = featuredTemplateResponse(paid, nil)
	if response["thumbnail"] != paid.Thumbnail || response["preview_url"] != paid.PreviewURL {
		t.Fatalf("featured paid template lost verified derivatives: %#v", response)
	}
}
