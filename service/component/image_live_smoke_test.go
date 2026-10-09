//go:build live_smoke

package component

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"service/config"
)

const liveImageSmokePrompt = "A clean architectural concept sketch of a small modern courtyard house, white background, simple black lines, no text, no logo"

type liveImageSmokeSummary struct {
	Model            string `json:"model"`
	EndpointHost     string `json:"endpoint_host"`
	GeneratedCount   int    `json:"generated_count"`
	ImageBytes       int64  `json:"image_bytes"`
	StorageHost      string `json:"storage_host"`
	StorageGETStatus int    `json:"storage_get_status"`
}

func TestLiveImageSmoke(t *testing.T) {
	if os.Getenv("JDLG_LIVE_IMAGE_SMOKE") != "1" {
		t.Skip("set JDLG_LIVE_IMAGE_SMOKE=1 to run the charged production smoke test")
	}

	cfg := config.Init()
	if cfg.AI.LaoZhangImageBaseURL != defaultLaoZhangImageBaseURL {
		t.Fatalf("image gateway safety gate rejected host %q", hostname(cfg.AI.LaoZhangImageBaseURL))
	}
	if cfg.AI.LaoZhangImageModel != defaultLaoZhangImageModel {
		t.Fatalf("image model safety gate rejected %q", cfg.AI.LaoZhangImageModel)
	}
	if strings.TrimSpace(cfg.AI.LaoZhangAPIKey) == "" {
		t.Fatal("LAOZHANG_API_KEY is not configured")
	}

	db, err := sql.Open("mysql", cfg.MySQL.DSN)
	if err != nil {
		t.Fatalf("open production database: %v", err)
	}
	defer db.Close()

	dbCtx, cancelDB := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelDB()
	tx, err := db.BeginTx(dbCtx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin read-only config transaction: %v", err)
	}
	defer tx.Rollback()

	dbConfig, err := readLiveImageConfig(dbCtx, tx)
	if err != nil {
		t.Fatalf("read production image config: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("close read-only config transaction: %v", err)
	}
	cancelDB()

	processor := NewAITaskProcessor(nil, nil, nil, nil, nil, nil, nil, nil, cfg)
	runtimeConfig := processor.applyRuntimeAIDrawProviderDefaults(dbConfig, "ai_draw", false)
	runtimeConfig = processor.normalizeRuntimeAIConfigData(runtimeConfig, "ai_draw", false)
	if runtimeConfig == nil {
		t.Fatal("production image config resolved to nil")
	}

	model := resolveConfiguredModel(runtimeConfig)
	endpointHost := hostname(runtimeConfig.APIEndpoint)
	if endpointHost != "api2.laozhang.ai" || model != defaultLaoZhangImageModel {
		t.Fatalf("runtime route safety gate rejected model=%q host=%q", model, endpointHost)
	}

	cosClient, err := InitCOSClient(cfg, nil)
	if err != nil {
		t.Fatalf("initialize COS client: %v", err)
	}
	pool := &RequestPool{cfg: cfg, cosClient: cosClient}
	taskCtx := CreateAITaskContext(
		fmt.Sprintf("live-smoke-%d", time.Now().Unix()),
		0,
		"ai_draw",
		runtimeConfig,
		nil,
		map[string]interface{}{
			"generate_count": 1,
			"image_size":     "1024x1024",
		},
		liveImageSmokePrompt,
		"",
		nil,
	)
	defer taskCtx.Cancel()

	result := pool.executeAITask(taskCtx)
	if !result.Success {
		t.Fatalf("image pipeline failed; inspect the private smoke log")
	}
	if len(result.ResultURLs) != 1 || result.ResultURL == "" {
		t.Fatalf("expected exactly one generated image, got %d", len(result.ResultURLs))
	}

	status, size, storageHost, err := verifyGeneratedImage(taskCtx.ctx, result.ResultURL)
	if err != nil {
		t.Fatalf("verify generated image URL: %v", err)
	}

	summaryJSON, err := json.Marshal(liveImageSmokeSummary{
		Model:            result.UsedModel,
		EndpointHost:     hostname(result.APIEndpoint),
		GeneratedCount:   len(result.ResultURLs),
		ImageBytes:       size,
		StorageHost:      storageHost,
		StorageGETStatus: status,
	})
	if err != nil {
		t.Fatalf("marshal smoke summary: %v", err)
	}
	fmt.Printf("LIVE_IMAGE_SMOKE_RESULT %s\n", summaryJSON)
}

func readLiveImageConfig(ctx context.Context, tx *sql.Tx) (*AIAPIConfigData, error) {
	const query = `SELECT
		COALESCE(task_type, ''), COALESCE(provider_code, ''), COALESCE(provider_name, ''),
		COALESCE(protocol_type, ''), COALESCE(api_endpoint, ''), COALESCE(method, ''),
		COALESCE(api_key, ''), COALESCE(api_key_location, ''), COALESCE(api_key_name, ''),
		COALESCE(headers, ''), COALESCE(body_template, ''), enable_prompt_optimization
		FROM ai_api_config
		WHERE task_type = 'ai_draw'
		ORDER BY is_active DESC, id ASC
		LIMIT 1`

	result := &AIAPIConfigData{}
	err := tx.QueryRowContext(ctx, query).Scan(
		&result.TaskType,
		&result.ProviderCode,
		&result.ProviderName,
		&result.ProtocolType,
		&result.APIEndpoint,
		&result.Method,
		&result.APIKey,
		&result.APIKeyLocation,
		&result.APIKeyName,
		&result.Headers,
		&result.BodyTemplate,
		&result.EnablePromptOptimization,
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func verifyGeneratedImage(ctx context.Context, imageURL string) (status int, size int64, host string, err error) {
	parsed, err := url.Parse(imageURL)
	if err != nil {
		return 0, 0, "", err
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" {
		return 0, 0, "", fmt.Errorf("result URL is not HTTPS")
	}

	requestCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, imageURL, nil)
	if err != nil {
		return 0, 0, parsed.Hostname(), err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, parsed.Hostname(), err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, 0, parsed.Hostname(), fmt.Errorf("storage returned HTTP %d", resp.StatusCode)
	}
	if contentType := strings.ToLower(resp.Header.Get("Content-Type")); !strings.HasPrefix(contentType, "image/") {
		return resp.StatusCode, 0, parsed.Hostname(), fmt.Errorf("storage returned non-image content type")
	}

	const maxImageBytes = int64(32 << 20)
	size, err = io.Copy(io.Discard, io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return resp.StatusCode, size, parsed.Hostname(), err
	}
	if size == 0 || size > maxImageBytes {
		return resp.StatusCode, size, parsed.Hostname(), fmt.Errorf("unexpected image size")
	}
	return resp.StatusCode, size, parsed.Hostname(), nil
}

func hostname(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
