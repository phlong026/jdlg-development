package route

import (
	"math"
	"strings"
	"testing"

	"service/model"
)

func validStudioDraft() aiVideoStudioDraftInput {
	return aiVideoStudioDraftInput{
		Name:       "庭院漫游",
		Mode:       "text",
		Prompt:     "清晨的现代庭院，镜头缓慢向前",
		Ratio:      "16:9",
		Resolution: "768p",
		Duration:   8,
		Quantity:   1,
	}
}

func TestValidateAIVideoStudioDraftModes(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*aiVideoStudioDraftInput)
		wantErr bool
	}{
		{name: "text", mutate: func(*aiVideoStudioDraftInput) {}},
		{name: "first frame", mutate: func(in *aiVideoStudioDraftInput) {
			in.Mode = "first_frame"
			in.Ratio = "adaptive"
			in.FirstFrameMediaID = "media-11"
		}},
		{name: "first and last", mutate: func(in *aiVideoStudioDraftInput) {
			in.Mode = "first_last_frame"
			in.Ratio = "adaptive"
			in.FirstFrameMediaID = "media-11"
			in.LastFrameMediaID = "media-12"
		}},
		{name: "reference", mutate: func(in *aiVideoStudioDraftInput) {
			in.Mode = "reference"
			in.ReferencePrompt = "以图片主体和空间关系生成连续镜头"
			in.References = []aiVideoStudioReferenceInput{{MediaID: "media-21", MediaType: "image", Position: 1}}
		}},
		{name: "pure last frame rejected", mutate: func(in *aiVideoStudioDraftInput) {
			in.Mode = "first_last_frame"
			in.Ratio = "adaptive"
			in.LastFrameMediaID = "media-12"
		}, wantErr: true},
		{name: "reference cannot carry frame", mutate: func(in *aiVideoStudioDraftInput) {
			in.Mode = "reference"
			in.FirstFrameMediaID = "media-11"
			in.References = []aiVideoStudioReferenceInput{{MediaID: "media-21", MediaType: "image", Position: 1}}
		}, wantErr: true},
		{name: "text cannot carry media", mutate: func(in *aiVideoStudioDraftInput) { in.FirstFrameMediaID = "media-11" }, wantErr: true},
		{name: "2k duration capped", mutate: func(in *aiVideoStudioDraftInput) { in.Resolution = "2k"; in.Duration = 16 }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validStudioDraft()
			tt.mutate(&in)
			err := validateAIVideoStudioDraft(in, defaultAIVideoStudioLimits())
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAIVideoStudioReferences(t *testing.T) {
	in := validStudioDraft()
	in.Mode = "reference"
	in.ReferencePrompt = "按素材编号组织画面"
	for i := 0; i < 8; i++ {
		in.References = append(in.References, aiVideoStudioReferenceInput{MediaID: "image-" + string(rune('a'+i)), MediaType: "image", Position: i + 1})
	}
	if err := validateAIVideoStudioDraft(in, defaultAIVideoStudioLimits()); err != nil {
		t.Fatalf("boundary references should pass: %v", err)
	}
	in.References = append(in.References, aiVideoStudioReferenceInput{MediaID: "image-over", MediaType: "image", Position: 9})
	if err := validateAIVideoStudioDraft(in, defaultAIVideoStudioLimits()); err == nil {
		t.Fatal("image reference over limit should fail")
	}
}

func TestValidateAIVideoStudioReferencesRejectDuplicateAndBrokenOrder(t *testing.T) {
	in := validStudioDraft()
	in.Mode = "reference"
	in.References = []aiVideoStudioReferenceInput{
		{MediaID: "video-7", MediaType: "video", DurationSeconds: 5, Position: 1},
		{MediaID: "video-7", MediaType: "video", DurationSeconds: 5, Position: 2},
	}
	if err := validateAIVideoStudioDraft(in, defaultAIVideoStudioLimits()); err == nil {
		t.Fatal("duplicate media should fail")
	}
	in.References = []aiVideoStudioReferenceInput{{MediaID: "audio-7", MediaType: "audio", DurationSeconds: 5, Position: 2}}
	if err := validateAIVideoStudioDraft(in, defaultAIVideoStudioLimits()); err == nil {
		t.Fatal("non-contiguous positions should fail")
	}
}

func TestValidateAIVideoStudioDraftSaveAllowsIncompleteCreation(t *testing.T) {
	in := validStudioDraft()
	in.Name = ""
	in.Mode = "reference"
	in.Prompt = ""
	in.References = nil
	if err := validateAIVideoStudioDraftSave(in, defaultAIVideoStudioLimits()); err != nil {
		t.Fatalf("incomplete draft should remain saveable: %v", err)
	}
	if err := validateAIVideoStudioDraft(in, defaultAIVideoStudioLimits()); err == nil {
		t.Fatal("incomplete draft must not be quotable")
	}
}

func TestValidateAIVideoStudioConfigFailsClosed(t *testing.T) {
	price := int64(2)
	config := model.AIVideoStudioConfig{
		Enabled: true, ProviderConfigured: false,
		Modes:  map[string]bool{"text": true},
		Limits: map[string]int{"max_concurrency": 1, "timeout_seconds": 900, "max_quantity": 1, "max_duration_768p": 15, "max_duration_2k": 15, "max_reference_images": 8, "max_reference_videos": 3, "max_reference_audios": 3, "max_reference_total": 12},
		Prices: completeStudioPrices(price),
	}
	config.Prices[1].Enabled = true
	if err := validateAIVideoStudioConfig(config); err != nil {
		t.Fatalf("provider readiness must not block saving operational intent: %v", err)
	}
	config.ProviderConfigured = true
	if err := validateAIVideoStudioConfig(config); err != nil {
		t.Fatalf("complete config should pass: %v", err)
	}
	config.Limits["max_duration_2k"] = 16
	if err := validateAIVideoStudioConfig(config); err == nil {
		t.Fatal("2K over 15 seconds must reject")
	}
}

func TestAIVideoStudioGenerationReadinessFailsClosed(t *testing.T) {
	if studioExecutionReady() || studioAccountingReady() {
		t.Fatal("generation must remain closed until execution and accounting are durably integrated")
	}
}

func TestAIVideoStudioManagementAcceptsWorkerBatchStates(t *testing.T) {
	for _, status := range []string{"", "queued", "submitting", "submitted", "running", "uncertain", "completed", "partial_success", "failed", "cancelled"} {
		if !validStudioBatchStatus(status) {
			t.Errorf("worker/client batch state rejected by management: %q", status)
		}
	}
	for _, status := range []string{"retain_failed", "reconciliation_required", "unknown", "completed OR 1=1"} {
		if validStudioBatchStatus(status) {
			t.Errorf("unimplemented or invalid batch filter accepted: %q", status)
		}
	}
}

func completeStudioPrices(unit int64) []model.AIVideoStudioPrice {
	return []model.AIVideoStudioPrice{
		{Code: "ai_video_studio_prompt_optimize", Label: "提示词优化", BillingUnit: "per_request", UnitStones: &unit},
		{Code: "ai_video_studio_768p", Label: "768P", BillingUnit: "per_second", UnitStones: &unit},
		{Code: "ai_video_studio_2k", Label: "2K", BillingUnit: "per_second", UnitStones: &unit},
		{Code: "ai_video_studio_768p_long", Label: "768P长视频", BillingUnit: "per_second", UnitStones: &unit},
	}
}

func TestValidateAIVideoStudioConfigRejectsDuplicateOrMissingPrices(t *testing.T) {
	unit := int64(2)
	config := model.AIVideoStudioConfig{
		Modes:  map[string]bool{"text": true},
		Limits: map[string]int{"max_concurrency": 1, "timeout_seconds": 900, "max_quantity": 1, "max_duration_768p": 15, "max_duration_2k": 15, "max_reference_images": 8, "max_reference_videos": 3, "max_reference_audios": 3, "max_reference_total": 12},
		Prices: completeStudioPrices(unit),
	}
	config.Prices[1].Enabled = true
	config.Prices = append(config.Prices, config.Prices[1])
	if err := validateAIVideoStudioConfig(config); err == nil {
		t.Fatal("duplicate price code must fail closed")
	}
	config.Prices = completeStudioPrices(unit)[:3]
	if err := validateAIVideoStudioConfig(config); err == nil {
		t.Fatal("missing required price code must fail closed")
	}
	config.Prices = completeStudioPrices(unit)
	if _, ok := studioPrice(append(config.Prices, config.Prices[1]), "768p", 8); ok {
		t.Fatal("ambiguous persisted price must not be selected")
	}
}

func TestCalculateAIVideoStudioQuoteTotalRejectsOverflowAndUnsafeValues(t *testing.T) {
	if _, _, err := calculateAIVideoStudioQuoteTotal(math.MaxInt64, 30, 10, "per_second"); err == nil {
		t.Fatal("overflowing quote must fail")
	}
	if _, _, err := calculateAIVideoStudioQuoteTotal(1, 30, 0, "per_second"); err == nil {
		t.Fatal("zero quantity must fail")
	}
	units, total, err := calculateAIVideoStudioQuoteTotal(2, 8, 3, "per_second")
	if err != nil || units != 24 || total != 48 {
		t.Fatalf("units=%d total=%d err=%v", units, total, err)
	}
}

func TestValidateAIVideoStudioPromptLengthBoundary(t *testing.T) {
	in := validStudioDraft()
	in.Prompt = strings.Repeat("画", 7000)
	if err := validateAIVideoStudioDraft(in, defaultAIVideoStudioLimits()); err != nil {
		t.Fatalf("7000 rune prompt should pass: %v", err)
	}
	in.Prompt += "面"
	if err := validateAIVideoStudioDraftSave(in, defaultAIVideoStudioLimits()); err == nil {
		t.Fatal("7001 rune prompt must fail")
	}
	in = validStudioDraft()
	in.Mode = "reference"
	in.Prompt = ""
	in.ReferencePrompt = strings.Repeat("参", 7000)
	in.References = []aiVideoStudioReferenceInput{{MediaID: "image-1", MediaType: "image", Position: 1}}
	if err := validateAIVideoStudioDraft(in, defaultAIVideoStudioLimits()); err != nil {
		t.Fatalf("7000 rune reference prompt should pass: %v", err)
	}
}
