package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

type backfillStats struct {
	Scanned int
	Updated int
	Skipped int
	Failed  int
}

func main() {
	envPath := filepath.Join("bin", ".env")
	if err := loadEnvFile(envPath); err != nil && !os.IsNotExist(err) {
		fail(err)
	}

	cfg := config.Init()
	db, err := component.InitMySQL(cfg)
	if err != nil {
		fail(err)
	}
	defer db.Close()

	if _, err := component.InitCOSClient(cfg, nil); err != nil {
		fail(err)
	}

	templateModel := model.NewTemplateModel(db)
	if err := templateModel.InitTable(); err != nil {
		fail(err)
	}
	inspirationModel := model.NewInspirationAssetModel(db)
	if err := inspirationModel.InitTable(); err != nil {
		fail(err)
	}

	ctx := context.Background()
	templateStats, err := backfillTemplates(ctx, cfg, templateModel, 100)
	if err != nil {
		fail(err)
	}
	inspirationStats, err := backfillInspirations(ctx, cfg, inspirationModel, 100)
	if err != nil {
		fail(err)
	}

	total := mergeStats(templateStats, inspirationStats)
	log.Printf("专用缩略图回填完成 scanned=%d updated=%d skipped=%d failed=%d", total.Scanned, total.Updated, total.Skipped, total.Failed)
}

func backfillTemplates(ctx context.Context, cfg *config.Config, templateModel *model.TemplateModel, batchSize int) (backfillStats, error) {
	stats := backfillStats{}
	offset := 0
	for {
		list, err := templateModel.List("", "", batchSize, offset)
		if err != nil {
			return stats, err
		}
		if len(list) == 0 {
			return stats, nil
		}
		for _, item := range list {
			stats.Scanned++
			originalURL := firstTemplateOriginalURL(item)
			if originalURL == "" {
				stats.Skipped++
				continue
			}
			ensureTemplateOriginalImages(item, originalURL)
			result, err := function.GenerateRemoteImageVariants(ctx, cfg, component.GetCOSClient(), originalURL, fmt.Sprintf("templates/%d", item.ID), []function.ImageVariantSpec{
				{Name: "list_thumb", MaxWidth: templateListThumbWidth, Quality: 74},
				{Name: "detail_preview", MaxWidth: templatePreviewWidth, Quality: 82},
			})
			if err != nil {
				stats.Failed++
				log.Printf("模板衍生图回填失败 id=%d err=%v", item.ID, err)
				continue
			}
			thumbnail := strings.TrimSpace(result.URLs["list_thumb"])
			previewURL := strings.TrimSpace(result.URLs["detail_preview"])
			if thumbnail == "" || previewURL == "" {
				stats.Skipped++
				continue
			}
			if err := templateModel.UpdateImageAssetsByID(item.ID, thumbnail, previewURL, item.Images); err != nil {
				stats.Failed++
				log.Printf("模板衍生图写回失败 id=%d err=%v", item.ID, err)
				continue
			}
			stats.Updated++
		}
		offset += len(list)
	}
}

func backfillInspirations(ctx context.Context, cfg *config.Config, inspirationModel *model.InspirationAssetModel, batchSize int) (backfillStats, error) {
	stats := backfillStats{}
	offset := 0
	for {
		list, err := inspirationModel.List("", "", "", "", "", 0, batchSize, offset)
		if err != nil {
			return stats, err
		}
		if len(list) == 0 {
			return stats, nil
		}
		for _, item := range list {
			stats.Scanned++
			originalURL := firstInspirationOriginalURL(item)
			if originalURL == "" {
				stats.Skipped++
				continue
			}
			ensureInspirationOriginalImages(item, originalURL)
			result, err := function.GenerateRemoteImageVariants(ctx, cfg, component.GetCOSClient(), originalURL, fmt.Sprintf("inspirations/%d", item.ID), []function.ImageVariantSpec{
				{Name: "list_thumb", MaxWidth: inspirationListThumbWidth, Quality: 74},
			})
			if err != nil {
				stats.Failed++
				log.Printf("灵感衍生图回填失败 id=%d err=%v", item.ID, err)
				continue
			}
			coverImage := strings.TrimSpace(result.URLs["list_thumb"])
			if coverImage == "" {
				stats.Skipped++
				continue
			}
			if err := inspirationModel.UpdateImageAssetsByID(item.ID, coverImage, item.Images); err != nil {
				stats.Failed++
				log.Printf("灵感衍生图写回失败 id=%d err=%v", item.ID, err)
				continue
			}
			stats.Updated++
		}
		offset += len(list)
	}
}

func firstTemplateOriginalURL(item *model.Template) string {
	urls := model.ParseImageURLList(item.Images)
	if len(urls) > 0 {
		return strings.TrimSpace(urls[0])
	}
	if strings.TrimSpace(item.PreviewURL) != "" {
		return strings.TrimSpace(item.PreviewURL)
	}
	return strings.TrimSpace(item.Thumbnail)
}

func ensureTemplateOriginalImages(item *model.Template, originalURL string) {
	if strings.TrimSpace(originalURL) == "" {
		return
	}
	urls := model.ParseImageURLList(item.Images)
	if len(urls) == 0 {
		payload, _ := jsonMarshalStrings([]string{originalURL})
		item.Images = payload
		return
	}
}

func firstInspirationOriginalURL(item *model.InspirationAsset) string {
	urls := model.ParseImageURLList(item.Images)
	if len(urls) > 0 {
		return strings.TrimSpace(urls[0])
	}
	return strings.TrimSpace(item.CoverImage)
}

func ensureInspirationOriginalImages(item *model.InspirationAsset, originalURL string) {
	if strings.TrimSpace(originalURL) == "" {
		return
	}
	urls := model.ParseImageURLList(item.Images)
	if len(urls) == 0 {
		payload, _ := jsonMarshalStrings([]string{originalURL})
		item.Images = payload
	}
}

func jsonMarshalStrings(values []string) (string, error) {
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func mergeStats(left backfillStats, right backfillStats) backfillStats {
	return backfillStats{
		Scanned: left.Scanned + right.Scanned,
		Updated: left.Updated + right.Updated,
		Skipped: left.Skipped + right.Skipped,
		Failed:  left.Failed + right.Failed,
	}
}

func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		if key == "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
