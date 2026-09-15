package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"service/component"
	"service/config"
	"service/function"
)

const (
	aiTaskListThumbWidth = 480
	queryBatchSize       = 100
)

type taskRow struct {
	ID            int64
	TaskNo        string
	ResultPayload string
}

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

	stats, err := backfillAITaskThumbnails(context.Background(), db, cfg)
	if err != nil {
		fail(err)
	}

	log.Printf("AI任务缩略图回填完成 scanned=%d updated=%d skipped=%d failed=%d", stats.Scanned, stats.Updated, stats.Skipped, stats.Failed)
}

func backfillAITaskThumbnails(ctx context.Context, db *sql.DB, cfg *config.Config) (backfillStats, error) {
	stats := backfillStats{}
	lastID := int64(0)

	for {
		rows, err := listTaskBatch(db, lastID, queryBatchSize)
		if err != nil {
			return stats, err
		}
		if len(rows) == 0 {
			return stats, nil
		}

		for _, row := range rows {
			lastID = row.ID
			stats.Scanned++

			updated, err := backfillOneTask(ctx, db, cfg, row)
			if err != nil {
				stats.Failed++
				log.Printf("AI任务缩略图回填失败 task_no=%s err=%v", row.TaskNo, err)
				continue
			}
			if updated {
				stats.Updated++
			} else {
				stats.Skipped++
			}
		}
	}
}

func listTaskBatch(db *sql.DB, lastID int64, limit int) ([]taskRow, error) {
	rows, err := db.Query(`
		SELECT id, task_no, result_payload
		FROM ai_tasks
		WHERE status = 'success'
		  AND result_payload IS NOT NULL
		  AND TRIM(result_payload) <> ''
		  AND id > ?
		ORDER BY id ASC
		LIMIT ?
	`, lastID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]taskRow, 0, limit)
	for rows.Next() {
		var item taskRow
		if err := rows.Scan(&item.ID, &item.TaskNo, &item.ResultPayload); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func backfillOneTask(ctx context.Context, db *sql.DB, cfg *config.Config, row taskRow) (bool, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(row.ResultPayload), &payload); err != nil {
		return false, err
	}

	if strings.TrimSpace(stringValue(payload["thumbnail_url"])) != "" {
		return false, nil
	}

	sourceURL := resolveSourceImageURL(payload)
	if sourceURL == "" {
		return false, nil
	}

	result, err := function.GenerateRemoteImageVariants(ctx, cfg, component.GetCOSClient(), sourceURL, path.Join("ai_tasks", row.TaskNo), []function.ImageVariantSpec{
		{Name: "list_thumb", MaxWidth: aiTaskListThumbWidth, Quality: 74},
	})
	if err != nil {
		return false, err
	}

	thumbnailURL := strings.TrimSpace(result.URLs["list_thumb"])
	if thumbnailURL == "" {
		return false, nil
	}

	payload["thumbnail_url"] = thumbnailURL
	payload["thumbnail_urls"] = []string{thumbnailURL}

	nextPayload, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}

	if _, err := db.Exec(`UPDATE ai_tasks SET result_payload = ?, updated_at = NOW() WHERE id = ?`, string(nextPayload), row.ID); err != nil {
		return false, err
	}

	return true, nil
}

func resolveSourceImageURL(payload map[string]interface{}) string {
	if payload == nil {
		return ""
	}

	candidates := []string{
		stringValue(payload["url"]),
		stringValue(payload["image_url"]),
	}
	for _, candidate := range candidates {
		if candidate != "" {
			return candidate
		}
	}

	if images, ok := payload["images"].([]interface{}); ok {
		for _, item := range images {
			if value := stringValue(item); value != "" {
				return value
			}
		}
	}

	return ""
}

func stringValue(value interface{}) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
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
