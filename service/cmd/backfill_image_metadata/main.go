package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"service/component"
	"service/config"
	"service/model"
)

type backfillStats struct {
	Scanned int
	Updated int
	Skipped int
	Failed  int
}

func main() {
	envPath := flag.String("env", filepath.Join("bin", ".env"), "env file path")
	batchSize := flag.Int("batch-size", 100, "batch size")
	target := flag.String("target", "all", "targets: templates, inspirations, all")
	flag.Parse()

	if err := loadEnvFile(*envPath); err != nil && !os.IsNotExist(err) {
		fail(err)
	}

	cfg := config.Init()
	db, err := component.InitMySQL(cfg)
	if err != nil {
		fail(err)
	}
	defer db.Close()

	templateModel := model.NewTemplateModel(db)
	if err := templateModel.InitTable(); err != nil {
		fail(err)
	}
	inspirationModel := model.NewInspirationAssetModel(db)
	if err := inspirationModel.InitTable(); err != nil {
		fail(err)
	}

	ctx := context.Background()
	total := backfillStats{}
	targetValue := strings.ToLower(strings.TrimSpace(*target))

	switch targetValue {
	case "templates":
		stats, err := backfillTemplates(ctx, templateModel, maxInt(*batchSize, 1))
		if err != nil {
			fail(err)
		}
		total = mergeStats(total, stats)
	case "inspirations":
		stats, err := backfillInspirations(ctx, inspirationModel, maxInt(*batchSize, 1))
		if err != nil {
			fail(err)
		}
		total = mergeStats(total, stats)
	case "all":
		templateStats, err := backfillTemplates(ctx, templateModel, maxInt(*batchSize, 1))
		if err != nil {
			fail(err)
		}
		inspirationStats, err := backfillInspirations(ctx, inspirationModel, maxInt(*batchSize, 1))
		if err != nil {
			fail(err)
		}
		total = mergeStats(templateStats, inspirationStats)
	default:
		fail(fmt.Errorf("unsupported target: %s", *target))
	}

	log.Printf("回填完成 scanned=%d updated=%d skipped=%d failed=%d", total.Scanned, total.Updated, total.Skipped, total.Failed)
}

func backfillTemplates(_ context.Context, templateModel *model.TemplateModel, batchSize int) (backfillStats, error) {
	stats := backfillStats{}
	for {
		list, err := templateModel.ListWithoutImageMetadata(batchSize, 0)
		if err != nil {
			return stats, err
		}
		if len(list) == 0 {
			return stats, nil
		}
		for _, item := range list {
			stats.Scanned++
			model.PopulateTemplateImageMetadata(item)
			if item.ImageWidth <= 0 || item.ImageHeight <= 0 {
				stats.Skipped++
				log.Printf("模板尺寸跳过 id=%d image=%s", item.ID, firstTemplateImage(item))
				continue
			}
			if err := templateModel.UpdateImageMetadataByID(item.ID, item.ImageWidth, item.ImageHeight); err != nil {
				stats.Failed++
				log.Printf("模板尺寸回填失败 id=%d err=%v", item.ID, err)
				continue
			}
			stats.Updated++
			log.Printf("模板尺寸已回填 id=%d width=%d height=%d", item.ID, item.ImageWidth, item.ImageHeight)
		}
	}
}

func backfillInspirations(_ context.Context, inspirationModel *model.InspirationAssetModel, batchSize int) (backfillStats, error) {
	stats := backfillStats{}
	for {
		list, err := inspirationModel.ListWithoutImageMetadata(batchSize, 0)
		if err != nil {
			return stats, err
		}
		if len(list) == 0 {
			return stats, nil
		}
		for _, item := range list {
			stats.Scanned++
			model.PopulateInspirationImageMetadata(item)
			if item.ImageWidth <= 0 || item.ImageHeight <= 0 {
				stats.Skipped++
				log.Printf("灵感尺寸跳过 id=%d image=%s", item.ID, firstInspirationImage(item))
				continue
			}
			if err := inspirationModel.UpdateImageMetadataByID(item.ID, item.ImageWidth, item.ImageHeight); err != nil {
				stats.Failed++
				log.Printf("灵感尺寸回填失败 id=%d err=%v", item.ID, err)
				continue
			}
			stats.Updated++
			log.Printf("灵感尺寸已回填 id=%d width=%d height=%d", item.ID, item.ImageWidth, item.ImageHeight)
		}
	}
}

func firstTemplateImage(item *model.Template) string {
	urls := model.CollectTemplateDisplayImageURLs(item)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func firstInspirationImage(item *model.InspirationAsset) string {
	urls := model.CollectInspirationDisplayImageURLs(item)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func mergeStats(left backfillStats, right backfillStats) backfillStats {
	return backfillStats{
		Scanned: left.Scanned + right.Scanned,
		Updated: left.Updated + right.Updated,
		Skipped: left.Skipped + right.Skipped,
		Failed:  left.Failed + right.Failed,
	}
}

func maxInt(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
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
