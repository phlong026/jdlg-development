package route

import (
	"fmt"

	"service/model"
)

const (
	minPrimaryImageWidth  = 400
	minPrimaryImageHeight = 400
	maxPrimaryImageRatio  = 3.2
)

func validateTemplatePrimaryImage(template *model.Template) error {
	if template == nil {
		return nil
	}
	urls := model.CollectTemplateDisplayImageURLs(template)
	if len(urls) == 0 {
		return nil
	}

	model.PopulateTemplateImageMetadata(template)
	return validatePrimaryImageDimensions(template.ImageWidth, template.ImageHeight, "模板")
}

func validateInspirationPrimaryImage(asset *model.InspirationAsset) error {
	if asset == nil {
		return nil
	}
	urls := model.CollectInspirationDisplayImageURLs(asset)
	if len(urls) == 0 {
		return nil
	}

	model.PopulateInspirationImageMetadata(asset)
	return validatePrimaryImageDimensions(asset.ImageWidth, asset.ImageHeight, "灵感")
}

func validatePrimaryImageDimensions(width int, height int, label string) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("%s首图尺寸识别失败，请确认图片链接可访问且为有效图片", label)
	}
	if width < minPrimaryImageWidth || height < minPrimaryImageHeight {
		return fmt.Errorf("%s首图尺寸过小，当前为 %dx%d，至少需要 %dx%d", label, width, height, minPrimaryImageWidth, minPrimaryImageHeight)
	}
	ratio := float64(width) / float64(height)
	if ratio > maxPrimaryImageRatio || (1/ratio) > maxPrimaryImageRatio {
		return fmt.Errorf("%s首图比例异常，当前为 %dx%d，请避免过长或过窄图片", label, width, height)
	}
	return nil
}
