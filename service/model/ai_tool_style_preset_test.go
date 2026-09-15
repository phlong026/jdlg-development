package model

import (
	"strings"
	"testing"
)

func TestAIToolStylePresetEffectiveImages(t *testing.T) {
	tests := []struct {
		name         string
		preset       AIToolStylePreset
		wantMode     string
		wantPreview  string
		wantModelRef string
	}{
		{
			name: "legacy image keeps old preview and model behavior",
			preset: AIToolStylePreset{
				ImageURL: "https://example.com/legacy.png",
			},
			wantMode:     AIToolReferenceModePreviewAndModel,
			wantPreview:  "https://example.com/legacy.png",
			wantModelRef: "https://example.com/legacy.png",
		},
		{
			name: "preview only never exposes model reference image",
			preset: AIToolStylePreset{
				ReferenceMode:          AIToolReferenceModePreviewOnly,
				PreviewImageURL:        "https://example.com/preview.png",
				ModelReferenceImageURL: "https://example.com/model.png",
			},
			wantMode:     AIToolReferenceModePreviewOnly,
			wantPreview:  "https://example.com/preview.png",
			wantModelRef: "",
		},
		{
			name: "preview and model separates display and AI reference images",
			preset: AIToolStylePreset{
				ReferenceMode:          AIToolReferenceModePreviewAndModel,
				PreviewImageURL:        "https://example.com/preview.png",
				ModelReferenceImageURL: "https://example.com/model.png",
			},
			wantMode:     AIToolReferenceModePreviewAndModel,
			wantPreview:  "https://example.com/preview.png",
			wantModelRef: "https://example.com/model.png",
		},
		{
			name: "empty legacy style stays prompt only",
			preset: AIToolStylePreset{
				PromptSuffix: "仅提示词",
			},
			wantMode:     AIToolReferenceModePreviewOnly,
			wantPreview:  "",
			wantModelRef: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.preset.EffectiveReferenceMode(); got != tt.wantMode {
				t.Fatalf("EffectiveReferenceMode() = %q, want %q", got, tt.wantMode)
			}
			if got := tt.preset.EffectivePreviewImageURL(); got != tt.wantPreview {
				t.Fatalf("EffectivePreviewImageURL() = %q, want %q", got, tt.wantPreview)
			}
			if got := tt.preset.EffectiveModelReferenceImageURL(); got != tt.wantModelRef {
				t.Fatalf("EffectiveModelReferenceImageURL() = %q, want %q", got, tt.wantModelRef)
			}
		})
	}
}

func TestAIToolDefaultUsageTipsCopywriting(t *testing.T) {
	got := buildDefaultUsageTipsContent(defaultAIToolUploadHint, true)
	if !strings.Contains(got, "风格卡片") {
		t.Fatalf("default usage tips should mention 风格卡片, got %q", got)
	}
	if strings.Contains(got, "预设参考图") || strings.Contains(got, "参考风格规则") {
		t.Fatalf("default usage tips should not keep legacy wording, got %q", got)
	}

	legacy := buildLegacyUsageTipsContent(false)
	if !strings.Contains(legacy, legacyAIToolUploadHint) {
		t.Fatalf("legacy usage tips should preserve old upload hint for migration matching, got %q", legacy)
	}
}
