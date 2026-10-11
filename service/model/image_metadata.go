package model

import (
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

const imageMetadataRequestTimeout = 8 * time.Second

var imageMetadataHTTPClient = &http.Client{
	Timeout: imageMetadataRequestTimeout,
}

func resolveRemoteImageDimensions(rawURL string) (int, int, bool) {
	url := strings.TrimSpace(rawURL)
	if url == "" || !strings.HasPrefix(strings.ToLower(url), "http") {
		return 0, 0, false
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, false
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", "jdlg-image-metadata/1.0")

	resp, err := imageMetadataHTTPClient.Do(req)
	if err != nil {
		return 0, 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return 0, 0, false
	}

	cfg, _, err := image.DecodeConfig(resp.Body)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, false
	}

	return cfg.Width, cfg.Height, true
}

func parseImageURLList(raw string) []string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}

	var parsed []interface{}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil
	}

	urls := make([]string, 0, len(parsed))
	appendURL := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range urls {
			if existing == value {
				return
			}
		}
		urls = append(urls, value)
	}

	for _, item := range parsed {
		switch value := item.(type) {
		case string:
			appendURL(value)
		case map[string]interface{}:
			appendURL(stringValueFromMap(value, "image"))
			appendURL(stringValueFromMap(value, "url"))
			appendURL(stringValueFromMap(value, "preview_url"))
		}
	}

	return urls
}

func ParseImageURLList(raw string) []string {
	return parseImageURLList(raw)
}

func stringValueFromMap(value map[string]interface{}, key string) string {
	raw, ok := value[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func collectTemplateDisplayImageURLs(template *Template) []string {
	if template == nil {
		return nil
	}

	urls := make([]string, 0, 4)
	appendURL := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range urls {
			if existing == value {
				return
			}
		}
		urls = append(urls, value)
	}

	appendURL(template.Thumbnail)
	appendURL(template.PreviewURL)
	for _, item := range parseImageURLList(template.Images) {
		appendURL(item)
	}

	return urls
}

func CollectTemplateDisplayImageURLs(template *Template) []string {
	return collectTemplateDisplayImageURLs(template)
}

func collectInspirationDisplayImageURLs(asset *InspirationAsset) []string {
	if asset == nil {
		return nil
	}

	urls := make([]string, 0, 4)
	appendURL := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range urls {
			if existing == value {
				return
			}
		}
		urls = append(urls, value)
	}

	appendURL(asset.CoverImage)
	for _, item := range parseImageURLList(asset.Images) {
		appendURL(item)
	}

	return urls
}

func CollectInspirationDisplayImageURLs(asset *InspirationAsset) []string {
	return collectInspirationDisplayImageURLs(asset)
}

func populateTemplateImageMetadata(template *Template) {
	if template == nil {
		return
	}

	template.ImageWidth = 0
	template.ImageHeight = 0

	for _, candidate := range collectTemplateDisplayImageURLs(template) {
		width, height, ok := resolveRemoteImageDimensions(candidate)
		if !ok {
			continue
		}
		template.ImageWidth = width
		template.ImageHeight = height
		return
	}
}

func PopulateTemplateImageMetadata(template *Template) {
	populateTemplateImageMetadata(template)
}

func populateInspirationImageMetadata(asset *InspirationAsset) {
	if asset == nil {
		return
	}

	asset.ImageWidth = 0
	asset.ImageHeight = 0

	for _, candidate := range collectInspirationDisplayImageURLs(asset) {
		width, height, ok := resolveRemoteImageDimensions(candidate)
		if !ok {
			continue
		}
		asset.ImageWidth = width
		asset.ImageHeight = height
		return
	}
}

func PopulateInspirationImageMetadata(asset *InspirationAsset) {
	populateInspirationImageMetadata(asset)
}
