package route

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"service/component"
	"service/config"
	"service/function"
	"service/model"
)

const (
	templateListThumbWidth    = 480
	templatePreviewWidth      = 1280
	inspirationListThumbWidth = 480
)

func generateTemplateDerivedImages(ctx context.Context, template *model.Template, namespace string) error {
	if template == nil {
		return nil
	}
	originalURL := firstTemplateOriginalImageURL(template)
	if originalURL == "" {
		return nil
	}
	ensureTemplateOriginalImages(template, originalURL)
	result, err := function.GenerateRemoteImageVariants(ctx, config.Get(), component.GetCOSClient(), originalURL, namespace, []function.ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: templateListThumbWidth, Quality: 74},
		{Name: "detail_preview", MaxWidth: templatePreviewWidth, Quality: 82},
	})
	if err != nil {
		return err
	}
	if url := strings.TrimSpace(result.URLs["list_thumb"]); url != "" {
		template.Thumbnail = url
	}
	if url := strings.TrimSpace(result.URLs["detail_preview"]); url != "" {
		template.PreviewURL = url
	}
	return nil
}

func generateInspirationDerivedImages(ctx context.Context, asset *model.InspirationAsset, namespace string) error {
	if asset == nil {
		return nil
	}
	originalURL := firstInspirationOriginalImageURL(asset)
	if originalURL == "" {
		return nil
	}
	ensureInspirationOriginalImages(asset, originalURL)
	result, err := function.GenerateRemoteImageVariants(ctx, config.Get(), component.GetCOSClient(), originalURL, namespace, []function.ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: inspirationListThumbWidth, Quality: 74},
	})
	if err != nil {
		return err
	}
	if url := strings.TrimSpace(result.URLs["list_thumb"]); url != "" {
		asset.CoverImage = url
	}
	return nil
}

func firstTemplateOriginalImageURL(template *model.Template) string {
	for _, item := range model.CollectTemplateDisplayImageURLs(template) {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func ensureTemplateOriginalImages(template *model.Template, originalURL string) {
	if template == nil || strings.TrimSpace(originalURL) == "" {
		return
	}
	current := model.ParseImageURLList(template.Images)
	if len(current) == 0 {
		payload, _ := json.Marshal([]string{originalURL})
		template.Images = string(payload)
		return
	}
	for _, item := range current {
		if strings.TrimSpace(item) == strings.TrimSpace(originalURL) {
			return
		}
	}
	current = append([]string{originalURL}, current...)
	payload, _ := json.Marshal(current)
	template.Images = string(payload)
}

func firstInspirationOriginalImageURL(asset *model.InspirationAsset) string {
	urls := model.CollectInspirationDisplayImageURLs(asset)
	for _, item := range urls {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func ensureInspirationOriginalImages(asset *model.InspirationAsset, originalURL string) {
	if asset == nil || strings.TrimSpace(originalURL) == "" {
		return
	}
	current := model.ParseImageURLList(asset.Images)
	if len(current) == 0 {
		payload, _ := json.Marshal([]string{originalURL})
		asset.Images = string(payload)
		return
	}
	for _, item := range current {
		if strings.TrimSpace(item) == strings.TrimSpace(originalURL) {
			return
		}
	}
	current = append([]string{originalURL}, current...)
	payload, _ := json.Marshal(current)
	asset.Images = string(payload)
}

func templateVariantNamespace(template *model.Template) string {
	return fmt.Sprintf("templates/%d", template.ID)
}

func inspirationVariantNamespace(asset *model.InspirationAsset) string {
	return fmt.Sprintf("inspirations/%d", asset.ID)
}
