package route

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	cos "github.com/tencentyun/cos-go-sdk-v5"
	"service/component"
	"service/config"
	"service/function"
	"service/model"
	"service/safeerror"
)

const (
	defaultVideoStones            int64 = 30
	defaultVideoDurationMin       int   = 5
	defaultVideoDurationMax       int   = 12
	defaultVideoAPIBaseURL              = "https://api.laozhang.ai"
	defaultArkVideoAPIBaseURL           = "https://ark.cn-beijing.volces.com"
	videoProviderLaoZhang               = "laozhang"
	videoProviderArkSeedance            = "ark_seedance"
	portraitVideoModel                  = "veo-3.1-fast-fl"
	landscapeVideoModel                 = "veo-3.1-landscape-fast-fl"
	arkSeedanceVideoModel               = "doubao-seedance-1-5-pro-251215"
	aiVideoLongRunningThreshold         = 30 * time.Minute
	aiVideoMonitorMaxAttempts           = 480
	aiVideoSupportTicketCreatedBy       = "system:auto-video-monitor"
)

var (
	aiVideoMonitorTasks       sync.Map
	aiVideoTaskLocks          sync.Map
	aiVideoSupportTicketModel *model.SupportTicketModel
)

type aiVideoRemoteError struct {
	stage         string
	baseURL       string
	externalID    string
	statusCode    int
	bodySummary   string
	unrecoverable bool
}

func (e *aiVideoRemoteError) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{e.stage}
	if e.statusCode > 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.statusCode))
	}
	if strings.TrimSpace(e.baseURL) != "" {
		parts = append(parts, fmt.Sprintf("base_url=%s", strings.TrimSpace(e.baseURL)))
	}
	if strings.TrimSpace(e.externalID) != "" {
		parts = append(parts, "external_id="+strings.TrimSpace(e.externalID))
	}
	if strings.TrimSpace(e.bodySummary) != "" {
		parts = append(parts, "body_summary="+strings.TrimSpace(e.bodySummary))
	}
	return strings.Join(parts, " | ")
}

func isUnrecoverableAIVideoRemoteError(err error) bool {
	var remoteErr *aiVideoRemoteError
	return errors.As(err, &remoteErr) && remoteErr.unrecoverable
}

func shouldFailAIVideoTaskForHTTPError(stage string, statusCode int, bodySummary string) bool {
	normalizedStage := strings.ToLower(strings.TrimSpace(stage))
	normalizedBody := strings.ToLower(strings.TrimSpace(bodySummary))
	if strings.Contains(normalizedBody, "task_not_exist") {
		return true
	}
	if statusCode == http.StatusNotFound && strings.Contains(normalizedStage, "content") {
		return true
	}
	return false
}

type aiVideoProviderAdapter interface {
	Code() string
	BaseURL(cfg *config.Config) string
	APIKey(cfg *config.Config) string
	Model(cfg *config.Config, orientation, ratio string) string
	CreateRequest(baseURL string, input aiVideoCreateInput) (string, io.Reader, string, error)
	StatusURL(baseURL, externalID string) string
	ContentURL(baseURL, externalID string) string
}

type aiVideoCreateInput struct {
	Model       string
	Prompt      string
	Duration    int
	Size        string
	Ratio       string
	Resolution  string
	ImageBodies [][]byte
}

type laozhangVideoProvider struct{}

func (laozhangVideoProvider) Code() string {
	return videoProviderLaoZhang
}

func (laozhangVideoProvider) BaseURL(cfg *config.Config) string {
	if cfg != nil {
		baseURL := strings.TrimSpace(cfg.AI.VideoAPIBaseURL)
		if baseURL != "" && !strings.EqualFold(strings.TrimSuffix(baseURL, "/"), defaultArkVideoAPIBaseURL) {
			return strings.TrimSuffix(baseURL, "/")
		}
	}
	return defaultVideoAPIBaseURL
}

func (laozhangVideoProvider) APIKey(cfg *config.Config) string {
	if cfg != nil && cfg.AI.LaoZhangAPIKey != "" {
		return cfg.AI.LaoZhangAPIKey
	}
	return ""
}

func (laozhangVideoProvider) Model(cfg *config.Config, orientation, ratio string) string {
	if cfg != nil {
		model := strings.TrimSpace(cfg.AI.VideoModel)
		if model != "" && !strings.HasPrefix(model, "doubao-seedance-") {
			return model
		}
	}
	if resolveVideoOrientation(orientation, ratio) == "portrait" {
		return portraitVideoModel
	}
	return landscapeVideoModel
}

func (laozhangVideoProvider) CreateRequest(baseURL string, input aiVideoCreateInput) (string, io.Reader, string, error) {
	createURL := strings.TrimSuffix(baseURL, "/") + "/v1/videos"
	if len(input.ImageBodies) > 0 {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		fields := []struct {
			key   string
			value string
		}{
			{"model", input.Model},
			{"prompt", input.Prompt},
			{"seconds", strconv.Itoa(input.Duration)},
			{"size", input.Size},
		}
		for _, field := range fields {
			if err := writer.WriteField(field.key, field.value); err != nil {
				return "", nil, "", err
			}
		}
		for i, bodyBytes := range input.ImageBodies {
			part, err := writer.CreateFormFile("input_reference", fmt.Sprintf("frame_%d.jpg", i+1))
			if err != nil {
				return "", nil, "", err
			}
			if _, err := part.Write(bodyBytes); err != nil {
				return "", nil, "", err
			}
		}
		if err := writer.Close(); err != nil {
			return "", nil, "", err
		}
		return createURL, &body, writer.FormDataContentType(), nil
	}
	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"model":   input.Model,
		"prompt":  input.Prompt,
		"seconds": strconv.Itoa(input.Duration),
		"size":    input.Size,
	})
	return createURL, bytes.NewReader(bodyBytes), "application/json", nil
}

func (laozhangVideoProvider) StatusURL(baseURL, externalID string) string {
	return strings.TrimSuffix(baseURL, "/") + "/v1/videos/" + externalID
}

func (laozhangVideoProvider) ContentURL(baseURL, externalID string) string {
	return strings.TrimSuffix(baseURL, "/") + "/v1/videos/" + externalID + "/content"
}

type arkSeedanceVideoProvider struct{}

func (arkSeedanceVideoProvider) Code() string {
	return videoProviderArkSeedance
}

func (arkSeedanceVideoProvider) BaseURL(cfg *config.Config) string {
	if cfg != nil {
		baseURL := strings.TrimSpace(cfg.AI.VideoAPIBaseURL)
		if baseURL != "" && !strings.EqualFold(strings.TrimSuffix(baseURL, "/"), defaultVideoAPIBaseURL) {
			return strings.TrimSuffix(baseURL, "/")
		}
	}
	return defaultArkVideoAPIBaseURL
}

func (arkSeedanceVideoProvider) APIKey(cfg *config.Config) string {
	if cfg != nil && strings.TrimSpace(cfg.AI.ArkAPIKey) != "" {
		return strings.TrimSpace(cfg.AI.ArkAPIKey)
	}
	return strings.TrimSpace(os.Getenv("ARK_API_KEY"))
}

func (arkSeedanceVideoProvider) Model(cfg *config.Config, orientation, ratio string) string {
	if cfg != nil {
		model := strings.TrimSpace(cfg.AI.VideoModel)
		if model != "" && strings.HasPrefix(model, "doubao-seedance-") {
			return model
		}
	}
	return arkSeedanceVideoModel
}

func (arkSeedanceVideoProvider) CreateRequest(baseURL string, input aiVideoCreateInput) (string, io.Reader, string, error) {
	content := []map[string]interface{}{{"type": "text", "text": input.Prompt}}
	for i, bodyBytes := range input.ImageBodies {
		if len(bodyBytes) == 0 {
			continue
		}
		role := "first_frame"
		if i == 1 {
			role = "last_frame"
		}
		mimeType := http.DetectContentType(bodyBytes)
		imageURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(bodyBytes))
		content = append(content, map[string]interface{}{
			"type":      "image_url",
			"image_url": map[string]string{"url": imageURL},
			"role":      role,
		})
	}
	body := map[string]interface{}{
		"model":   input.Model,
		"content": content,
	}
	if input.Duration > 0 {
		body["duration"] = input.Duration
	}
	if input.Ratio != "" {
		body["ratio"] = input.Ratio
	}
	if input.Resolution != "" {
		body["resolution"] = input.Resolution
	}
	bodyBytes, _ := json.Marshal(body)
	return strings.TrimSuffix(baseURL, "/") + "/api/v3/contents/generations/tasks", bytes.NewReader(bodyBytes), "application/json", nil
}

func (arkSeedanceVideoProvider) StatusURL(baseURL, externalID string) string {
	return strings.TrimSuffix(baseURL, "/") + "/api/v3/contents/generations/tasks/" + externalID
}

func (arkSeedanceVideoProvider) ContentURL(baseURL, externalID string) string {
	return strings.TrimSuffix(baseURL, "/") + "/api/v3/contents/generations/tasks/" + externalID
}

func getVideoProviderCode(cfg *config.Config) string {
	if cfg != nil {
		switch strings.ToLower(strings.TrimSpace(cfg.AI.VideoProvider)) {
		case videoProviderArkSeedance, "ark", "seedance", "volcengine":
			return videoProviderArkSeedance
		case videoProviderLaoZhang, "":
			return videoProviderLaoZhang
		}
	}
	return videoProviderLaoZhang
}

func getVideoProviderAdapter(cfg *config.Config) aiVideoProviderAdapter {
	if getVideoProviderCode(cfg) == videoProviderArkSeedance {
		return arkSeedanceVideoProvider{}
	}
	return laozhangVideoProvider{}
}

func getVideoAPIBase(cfg *config.Config) string {
	return getVideoProviderAdapter(cfg).BaseURL(cfg)
}

func getVideoAPIKey(cfg *config.Config) string {
	return getVideoProviderAdapter(cfg).APIKey(cfg)
}

func resolveVideoOrientation(orientation, ratio string) string {
	orientation = strings.ToLower(strings.TrimSpace(orientation))
	if orientation == "portrait" || orientation == "landscape" {
		return orientation
	}
	switch strings.TrimSpace(ratio) {
	case "9:16", "3:4":
		return "portrait"
	case "16:9", "4:3", "21:9":
		return "landscape"
	}
	if parts := strings.Split(strings.TrimSpace(ratio), ":"); len(parts) == 2 {
		var w, h float64
		if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%f", &w); err == nil {
			if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &h); err == nil && w > 0 && h > 0 {
				if h > w {
					return "portrait"
				}
				return "landscape"
			}
		}
	}
	return "landscape"
}

func getVideoModel(cfg *config.Config, orientation, ratio string) string {
	return getVideoProviderAdapter(cfg).Model(cfg, orientation, ratio)
}

func normalizeVideoResolutionLabel(resolution string) string {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case "480p":
		return "480p"
	case "1080p":
		return "1080p"
	default:
		return "720p"
	}
}

func normalizeVideoRatioLabel(ratio, orientation string) string {
	trimmed := strings.TrimSpace(ratio)
	switch trimmed {
	case "16:9", "4:3", "1:1", "3:4", "9:16", "21:9":
		return trimmed
	case "adaptive":
		if resolveVideoOrientation(orientation, ratio) == "portrait" {
			return "9:16"
		}
		return "16:9"
	default:
		if resolveVideoOrientation(orientation, ratio) == "portrait" {
			return "9:16"
		}
		return "16:9"
	}
}

func resolveLaoZhangVideoSize(resolution, ratio, orientation string) string {
	canonicalResolution := normalizeVideoResolutionLabel(resolution)
	canonicalRatio := normalizeVideoRatioLabel(ratio, orientation)
	sizeMap := map[string]map[string]string{
		"480p": {
			"16:9": "854x480",
			"4:3":  "640x480",
			"1:1":  "480x480",
			"3:4":  "480x640",
			"9:16": "480x854",
			"21:9": "1120x480",
		},
		"720p": {
			"16:9": "1280x720",
			"4:3":  "960x720",
			"1:1":  "720x720",
			"3:4":  "720x960",
			"9:16": "720x1280",
			"21:9": "1680x720",
		},
		"1080p": {
			"16:9": "1920x1080",
			"4:3":  "1440x1080",
			"1:1":  "1080x1080",
			"3:4":  "1080x1440",
			"9:16": "1080x1920",
			"21:9": "2520x1080",
		},
	}
	if sizes, ok := sizeMap[canonicalResolution]; ok {
		if size, exists := sizes[canonicalRatio]; exists {
			return size
		}
	}
	return "1280x720"
}

func normalizeExternalVideoStatus(status string) string {
	return model.NormalizeAIVideoStatus(status)
}

func setAIVideoSupportTicketModel(supportTicketModel *model.SupportTicketModel) {
	if supportTicketModel != nil {
		aiVideoSupportTicketModel = supportTicketModel
	}
}

type aiVideoContentPayload struct {
	Status       string
	URL          string
	Duration     int
	Resolution   string
	ErrorMessage string
}

func getAIVideoTaskLock(taskID int64) *sync.Mutex {
	if lock, ok := aiVideoTaskLocks.Load(taskID); ok {
		return lock.(*sync.Mutex)
	}
	lock := &sync.Mutex{}
	actual, _ := aiVideoTaskLocks.LoadOrStore(taskID, lock)
	return actual.(*sync.Mutex)
}

func extractNestedValue(payload map[string]interface{}, path ...string) interface{} {
	current := interface{}(payload)
	for _, key := range path {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current, ok = obj[key]
		if !ok {
			return nil
		}
	}
	return current
}

func extractNestedString(payload map[string]interface{}, paths ...[]string) string {
	for _, path := range paths {
		value := extractNestedValue(payload, path...)
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		case fmt.Stringer:
			if trimmed := strings.TrimSpace(typed.String()); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func extractNestedInt(payload map[string]interface{}, paths ...[]string) int {
	for _, path := range paths {
		value := extractNestedValue(payload, path...)
		switch typed := value.(type) {
		case float64:
			return int(typed)
		case float32:
			return int(typed)
		case int:
			return typed
		case int32:
			return int(typed)
		case int64:
			return int(typed)
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func appendUniqueVideoErrorPart(parts []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return parts
	}
	for _, existing := range parts {
		if existing == trimmed {
			return parts
		}
	}
	return append(parts, trimmed)
}

func parseAIVideoErrorMessage(payload map[string]interface{}) string {
	message := extractNestedString(
		payload,
		[]string{"error", "message"},
		[]string{"message"},
		[]string{"error_message"},
		[]string{"data", "error", "message"},
		[]string{"data", "message"},
		[]string{"data", "error_message"},
		[]string{"fail_reason"},
	)
	parts := make([]string, 0, 4)
	parts = appendUniqueVideoErrorPart(parts, message)
	parts = appendUniqueVideoErrorPart(parts, extractNestedString(payload, []string{"fail_reason"}))
	if code := extractNestedString(payload, []string{"error", "code"}, []string{"data", "error", "code"}); code != "" {
		parts = appendUniqueVideoErrorPart(parts, "code="+code)
	}
	if errType := extractNestedString(payload, []string{"error", "type"}, []string{"data", "error", "type"}); errType != "" {
		parts = appendUniqueVideoErrorPart(parts, "type="+errType)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}

func summarizeAIVideoRemoteBody(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	collapsed := strings.Join(strings.Fields(trimmed), " ")
	runes := []rune(collapsed)
	if len(runes) > 220 {
		return string(runes[:220]) + "..."
	}
	return collapsed
}

func buildAIVideoRemoteHTTPError(stage string, baseURL string, externalID string, statusCode int, body []byte) error {
	summary := summarizeAIVideoRemoteBody(body)
	return &aiVideoRemoteError{
		stage:         stage,
		baseURL:       baseURL,
		externalID:    externalID,
		statusCode:    statusCode,
		bodySummary:   summary,
		unrecoverable: shouldFailAIVideoTaskForHTTPError(stage, statusCode, summary),
	}
}

func buildAIVideoRemoteJSONError(stage string, baseURL string, externalID string, body []byte, err error) error {
	summary := summarizeAIVideoRemoteBody(body)
	if summary == "" && err != nil {
		summary = err.Error()
	}
	return &aiVideoRemoteError{
		stage:         stage,
		baseURL:       baseURL,
		externalID:    externalID,
		bodySummary:   summary,
		unrecoverable: true,
	}
}

func fetchVideoStatus(provider aiVideoProviderAdapter, baseURL, apiKey, externalID string) (string, string, error) {
	httpReq, err := http.NewRequest("GET", provider.StatusURL(baseURL, externalID), nil)
	if err != nil {
		return "", "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", summarizeAIVideoRemoteBody(respBody), buildAIVideoRemoteHTTPError("status query failed", baseURL, externalID, resp.StatusCode, respBody)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return "", summarizeAIVideoRemoteBody(respBody), buildAIVideoRemoteJSONError("status response invalid", baseURL, externalID, respBody, err)
	}
	status := normalizeExternalVideoStatus(extractNestedString(
		payload,
		[]string{"status"},
		[]string{"state"},
		[]string{"data", "status"},
		[]string{"data", "state"},
		[]string{"task", "status"},
		[]string{"task", "state"},
	))
	errMsg := parseAIVideoErrorMessage(payload)
	if status == "" {
		log.Printf("[AIVideo] 视频状态响应无法识别 | provider=%s base_url=%s external_id=%s body_summary=%s", provider.Code(), strings.TrimSpace(baseURL), strings.TrimSpace(externalID), summarizeAIVideoRemoteBody(respBody))
	}
	return status, errMsg, nil
}

func fetchVideoContent(provider aiVideoProviderAdapter, baseURL, apiKey, externalID string) (*aiVideoContentPayload, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	httpReq, err := http.NewRequest("GET", provider.ContentURL(baseURL, externalID), nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, buildAIVideoRemoteHTTPError("content query failed", baseURL, externalID, resp.StatusCode, respBody)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, buildAIVideoRemoteJSONError("content response invalid", baseURL, externalID, respBody, err)
	}
	content := &aiVideoContentPayload{
		Status: normalizeExternalVideoStatus(extractNestedString(
			payload,
			[]string{"status"},
			[]string{"state"},
			[]string{"data", "status"},
			[]string{"data", "state"},
			[]string{"task", "status"},
			[]string{"task", "state"},
			[]string{"content", "status"},
		)),
		URL: extractNestedString(
			payload,
			[]string{"url"},
			[]string{"video_url"},
			[]string{"data", "url"},
			[]string{"data", "video_url"},
			[]string{"content", "url"},
			[]string{"content", "video_url"},
			[]string{"output", "url"},
		),
		Duration: extractNestedInt(
			payload,
			[]string{"duration"},
			[]string{"data", "duration"},
			[]string{"content", "duration"},
		),
		Resolution: extractNestedString(
			payload,
			[]string{"resolution"},
			[]string{"data", "resolution"},
			[]string{"content", "resolution"},
		),
		ErrorMessage: parseAIVideoErrorMessage(payload),
	}
	if content.Status == "" {
		log.Printf("[AIVideo] 视频结果响应无法识别 | provider=%s base_url=%s external_id=%s body_summary=%s", provider.Code(), strings.TrimSpace(baseURL), strings.TrimSpace(externalID), summarizeAIVideoRemoteBody(respBody))
	}
	if content.Status == model.AIVideoStatusCompleted && content.URL == "" {
		log.Printf("[AIVideo] 视频结果缺少下载地址 | provider=%s base_url=%s external_id=%s body_summary=%s", provider.Code(), strings.TrimSpace(baseURL), strings.TrimSpace(externalID), summarizeAIVideoRemoteBody(respBody))
	}
	return content, nil
}

func loadLatestAIVideoTask(videoTaskModel *model.AIVideoTaskModel, task *model.AIVideoTask) *model.AIVideoTask {
	if videoTaskModel == nil || task == nil {
		return task
	}
	latest, err := videoTaskModel.GetByID(task.ID)
	if err == nil && latest != nil {
		return latest
	}
	return task
}

func markAIVideoTaskFailed(task *model.AIVideoTask, videoTaskModel *model.AIVideoTaskModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, stoneRecordModel *model.StoneRecordModel, errMsg string) *model.AIVideoTask {
	if task == nil {
		return nil
	}
	rawErrMsg := strings.TrimSpace(errMsg)
	safeErrMsg := safeerror.SanitizeAIGenerationError(rawErrMsg)
	if safeErrMsg == "" {
		safeErrMsg = "任务失败或已取消"
	}
	statusUpdated := false
	if task.ExternalID != "continuous" && videoTaskModel != nil {
		updated, updateErr := videoTaskModel.UpdateStatusIfCurrentInWithRaw(task.ID, model.AIVideoFailureTransitionSourceStatuses(), model.AIVideoStatusFailed, safeErrMsg, rawErrMsg)
		if updateErr != nil {
			log.Printf("[AIVideo] 更新失败状态失败 | task_id=%d err=%v", task.ID, updateErr)
		} else {
			statusUpdated = updated
		}
		if updated && userModel != nil && pricingModel != nil {
			stones := getVideoStones(pricingModel, task.SegmentCount)
			if stones > 0 {
				_ = userModel.AddStones(task.UserID, stones)
				if stoneRecordModel != nil {
					_ = stoneRecordModel.Create(task.UserID, "task", stones, "视频任务失败退回", "")
				}
			}
		}
	}
	if !statusUpdated && task.Status != model.AIVideoStatusFailed && videoTaskModel != nil {
		_ = videoTaskModel.UpdateStatusWithRaw(task.ID, model.AIVideoStatusFailed, safeErrMsg, rawErrMsg)
	}
	task.Status = model.AIVideoStatusFailed
	task.ErrorMessage = sql.NullString{String: safeErrMsg, Valid: true}
	if rawErrMsg != "" {
		task.RawErrorMessage = sql.NullString{String: rawErrMsg, Valid: true}
	}
	return loadLatestAIVideoTask(videoTaskModel, task)
}

func finalizeAIVideoTaskResult(task *model.AIVideoTask, videoURL string, duration int, resolution string, videoTaskModel *model.AIVideoTaskModel, cfg *config.Config) (*model.AIVideoTask, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}
	videoClient := &http.Client{Timeout: 2 * time.Minute}
	videoReq, err := http.NewRequest("GET", videoURL, nil)
	if err != nil {
		return task, err
	}
	videoResp, err := videoClient.Do(videoReq)
	if err != nil {
		return task, err
	}
	defer videoResp.Body.Close()
	if videoResp.StatusCode != http.StatusOK {
		return task, fmt.Errorf("download status %d", videoResp.StatusCode)
	}

	videoData, err := io.ReadAll(videoResp.Body)
	if err != nil {
		return task, err
	}
	if watermarked, wErr := component.AddVideoWatermark(videoData); wErr == nil {
		videoData = watermarked
	} else {
		log.Printf("[AIVideo] 视频水印未添加，使用原片 | task_id=%d err=%v", task.ID, wErr)
	}

	key := path.Join("ai_video", fmt.Sprintf("%d_%s.mp4", task.ID, task.ExternalID))
	cosClient := component.GetCOSClient()
	if cosClient == nil {
		return task, fmt.Errorf("存储服务未初始化")
	}
	ctx := context.Background()
	ossURL, err := function.UploadBytes(ctx, cosClient, cfg, key, videoData, "video/mp4")
	if err != nil {
		return task, err
	}
	resolutionStr := strings.TrimSpace(resolution)
	if resolutionStr == "" {
		resolutionStr = "unknown"
	}
	if err := videoTaskModel.UpdateOSSURL(task.ID, ossURL, duration, resolutionStr); err != nil {
		log.Printf("[AIVideo] update oss_url failed | task_id=%d err=%v", task.ID, err)
	}
	updatedTask := loadLatestAIVideoTask(videoTaskModel, task)
	updatedTask.Status = model.AIVideoStatusCompleted
	updatedTask.OSSURL = ossURL
	updatedTask.Duration = duration
	updatedTask.Resolution = resolutionStr
	return updatedTask, nil
}

func syncAIVideoTask(task *model.AIVideoTask, videoTaskModel *model.AIVideoTaskModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, stoneRecordModel *model.StoneRecordModel, cfg *config.Config) (*model.AIVideoTask, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}
	lock := getAIVideoTaskLock(task.ID)
	lock.Lock()
	defer lock.Unlock()

	task = loadLatestAIVideoTask(videoTaskModel, task)
	if task == nil || task.ExternalID == "continuous" || task.Status == model.AIVideoStatusFailed || strings.TrimSpace(task.OSSURL) != "" {
		return task, nil
	}
	videoProvider := getVideoProviderAdapter(cfg)
	baseURL := videoProvider.BaseURL(cfg)
	apiKey := videoProvider.APIKey(cfg)
	if apiKey == "" {
		return task, fmt.Errorf("未配置视频 API Key")
	}
	ourStatus, errMsg, err := fetchVideoStatus(videoProvider, baseURL, apiKey, task.ExternalID)
	if err != nil {
		if isUnrecoverableAIVideoRemoteError(err) {
			return markAIVideoTaskFailed(task, videoTaskModel, userModel, pricingModel, stoneRecordModel, err.Error()), nil
		}
		return task, err
	}
	if ourStatus == "" {
		ourStatus = task.Status
	}
	if ourStatus == model.AIVideoStatusFailed {
		return markAIVideoTaskFailed(task, videoTaskModel, userModel, pricingModel, stoneRecordModel, errMsg), nil
	}
	rawErrMsg := strings.TrimSpace(errMsg)
	safeErrMsg := safeerror.SanitizeAIGenerationError(rawErrMsg)
	if ourStatus != model.AIVideoStatusCompleted {
		if videoTaskModel != nil {
			_ = videoTaskModel.UpdateStatusWithRaw(task.ID, ourStatus, safeErrMsg, rawErrMsg)
		}
		task.Status = ourStatus
		if safeErrMsg != "" {
			task.ErrorMessage = sql.NullString{String: safeErrMsg, Valid: true}
		}
		if rawErrMsg != "" {
			task.RawErrorMessage = sql.NullString{String: rawErrMsg, Valid: true}
		}
		return loadLatestAIVideoTask(videoTaskModel, task), nil
	}

	content, err := fetchVideoContent(videoProvider, baseURL, apiKey, task.ExternalID)
	if err != nil {
		if isUnrecoverableAIVideoRemoteError(err) {
			return markAIVideoTaskFailed(task, videoTaskModel, userModel, pricingModel, stoneRecordModel, err.Error()), nil
		}
		return task, err
	}
	contentStatus := content.Status
	if contentStatus == "" {
		contentStatus = ourStatus
	}
	if contentStatus == model.AIVideoStatusFailed {
		return markAIVideoTaskFailed(task, videoTaskModel, userModel, pricingModel, stoneRecordModel, content.ErrorMessage), nil
	}
	if contentStatus == model.AIVideoStatusCompleted && strings.TrimSpace(content.URL) == "" {
		rawContentErrMsg := strings.TrimSpace(content.ErrorMessage)
		if videoTaskModel != nil {
			_ = videoTaskModel.UpdateStatusWithRaw(task.ID, model.AIVideoStatusProcessing, "", rawContentErrMsg)
		}
		task.Status = model.AIVideoStatusProcessing
		if rawContentErrMsg != "" {
			task.RawErrorMessage = sql.NullString{String: rawContentErrMsg, Valid: true}
		}
		log.Printf("[AIVideo] 视频结果已完成但暂未返回下载地址 | task_id=%d external_id=%s", task.ID, task.ExternalID)
		return loadLatestAIVideoTask(videoTaskModel, task), nil
	}
	if contentStatus != model.AIVideoStatusCompleted {
		rawContentErrMsg := strings.TrimSpace(content.ErrorMessage)
		safeContentErrMsg := safeerror.SanitizeAIGenerationError(rawContentErrMsg)
		if videoTaskModel != nil {
			_ = videoTaskModel.UpdateStatusWithRaw(task.ID, contentStatus, safeContentErrMsg, rawContentErrMsg)
		}
		task.Status = contentStatus
		if rawContentErrMsg != "" {
			task.ErrorMessage = sql.NullString{String: safeContentErrMsg, Valid: true}
			task.RawErrorMessage = sql.NullString{String: rawContentErrMsg, Valid: true}
		}
		return loadLatestAIVideoTask(videoTaskModel, task), nil
	}
	return finalizeAIVideoTaskResult(task, content.URL, content.Duration, content.Resolution, videoTaskModel, cfg)
}

func aiVideoMonitorDelay(attempt int) time.Duration {
	if attempt < 10 {
		return 5 * time.Second
	}
	if attempt < 30 {
		return 8 * time.Second
	}
	return 12 * time.Second
}

func shouldEscalateLongRunningAIVideoTask(task *model.AIVideoTask) bool {
	if task == nil {
		return false
	}
	if strings.TrimSpace(task.OSSURL) != "" {
		return false
	}
	switch model.NormalizeAIVideoStatus(task.Status) {
	case model.AIVideoStatusQueued, model.AIVideoStatusProcessing:
		return !task.CreatedAt.IsZero() && time.Since(task.CreatedAt) >= aiVideoLongRunningThreshold
	default:
		return false
	}
}

func aiVideoLongRunningTicketPriority(waitDuration time.Duration) string {
	if waitDuration >= 60*time.Minute {
		return "high"
	}
	return "medium"
}

func ensureLongRunningAIVideoSupportTicket(task *model.AIVideoTask) {
	supportTicketModel := aiVideoSupportTicketModel
	if task == nil || supportTicketModel == nil || supportTicketModel.DB == nil {
		return
	}
	sourceID := "video-" + strconv.FormatInt(task.ID, 10)
	if _, err := supportTicketModel.GetLatestOpenTicketIDBySource("task", sourceID); err == nil {
		return
	} else if err != sql.ErrNoRows {
		log.Printf("[AIVideo] 查询长耗时工单失败 | task_id=%d err=%v", task.ID, err)
		return
	}
	waitDuration := time.Since(task.CreatedAt)
	mappedStatus := model.AIVideoStatusForManagement(task.Status)
	_, err := supportTicketModel.Create(&model.SupportTicketCreateInput{
		UserID:     task.UserID,
		Type:       "task",
		SourceType: "task",
		SourceID:   sourceID,
		Title:      fmt.Sprintf("AI视频任务长时间处理中：v%d", task.ID),
		Content:    fmt.Sprintf("任务已持续等待约 %.0f 分钟仍未完成；当前状态：%s；模型：%s；分段：%d；外部任务ID：%s；错误信息：%s", waitDuration.Minutes(), mappedStatus, task.Model, task.SegmentCount, task.ExternalID, task.GetErrorMessage()),
		Priority:   aiVideoLongRunningTicketPriority(waitDuration),
		CreatedBy:  aiVideoSupportTicketCreatedBy,
		SourcePayload: map[string]interface{}{
			"task_id":                task.ID,
			"task_no":                "v" + strconv.FormatInt(task.ID, 10),
			"task_type":              "video",
			"status":                 mappedStatus,
			"raw_status":             task.Status,
			"model":                  task.Model,
			"prompt":                 task.Prompt,
			"external_id":            task.ExternalID,
			"oss_url":                task.OSSURL,
			"duration":               task.Duration,
			"resolution":             task.Resolution,
			"segment_count":          task.SegmentCount,
			"error_message":          task.GetErrorMessage(),
			"created_at":             task.CreatedAt,
			"updated_at":             task.UpdatedAt,
			"auto_created":           true,
			"auto_reason":            "video_long_running",
			"long_running_minutes":   int(waitDuration.Minutes()),
			"long_running_threshold": int(aiVideoLongRunningThreshold / time.Minute),
		},
	})
	if err != nil {
		log.Printf("[AIVideo] 创建长耗时异常工单失败 | task_id=%d err=%v", task.ID, err)
		return
	}
	log.Printf("[AIVideo] 已为长耗时视频任务创建异常工单 | task_id=%d waited_minutes=%.0f", task.ID, waitDuration.Minutes())
}

func StartAIVideoTaskMonitor(taskID int64, videoTaskModel *model.AIVideoTaskModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, stoneRecordModel *model.StoneRecordModel, cfg *config.Config) {
	if taskID <= 0 || videoTaskModel == nil {
		return
	}
	if _, loaded := aiVideoMonitorTasks.LoadOrStore(taskID, struct{}{}); loaded {
		return
	}
	go func() {
		defer aiVideoMonitorTasks.Delete(taskID)
		for attempt := 0; attempt < aiVideoMonitorMaxAttempts; attempt++ {
			task, err := videoTaskModel.GetByID(taskID)
			if err != nil || task == nil {
				return
			}
			if task.Status == model.AIVideoStatusFailed || strings.TrimSpace(task.OSSURL) != "" {
				return
			}
			if _, err := syncAIVideoTask(task, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg); err != nil {
				log.Printf("[AIVideo] monitor 同步失败 | task_id=%d err=%v", taskID, err)
			}
			latestTask, latestErr := videoTaskModel.GetByID(taskID)
			if latestErr == nil && latestTask != nil {
				if latestTask.Status == model.AIVideoStatusFailed || strings.TrimSpace(latestTask.OSSURL) != "" {
					return
				}
				if shouldEscalateLongRunningAIVideoTask(latestTask) {
					ensureLongRunningAIVideoSupportTicket(latestTask)
				}
			}
			time.Sleep(aiVideoMonitorDelay(attempt))
		}
		if task, err := videoTaskModel.GetByID(taskID); err == nil && task != nil && shouldEscalateLongRunningAIVideoTask(task) {
			ensureLongRunningAIVideoSupportTicket(task)
		}
		log.Printf("[AIVideo] monitor 超时停止 | task_id=%d", taskID)
	}()
}

func ResumePendingAIVideoTasks(videoTaskModel *model.AIVideoTaskModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, stoneRecordModel *model.StoneRecordModel, supportTicketModel *model.SupportTicketModel, cfg *config.Config) {
	if videoTaskModel == nil {
		return
	}
	setAIVideoSupportTicketModel(supportTicketModel)
	tasks, err := videoTaskModel.ListActiveForMonitoring(200)
	if err != nil {
		log.Printf("[AIVideo] 恢复未完成任务失败: %v", err)
		return
	}
	for _, task := range tasks {
		if task == nil {
			continue
		}
		StartAIVideoTaskMonitor(task.ID, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg)
	}
}

func downloadImageURL(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func normalizeVideoSegmentCount(segmentCount int) int {
	if segmentCount <= 0 {
		return 1
	}
	if segmentCount > 4 {
		return 4
	}
	return segmentCount
}

func getVideoPricingScene(segmentCount int) string {
	return fmt.Sprintf("ai_video_%d", normalizeVideoSegmentCount(segmentCount))
}

func getVideoDurationRange(pricingModel *model.AIPricingModel, segmentCount int) (int, int) {
	minDuration := defaultVideoDurationMin
	maxDuration := defaultVideoDurationMax
	if pricingModel != nil {
		pricing, err := pricingModel.GetByScene(getVideoPricingScene(segmentCount))
		if err == nil && pricing != nil {
			if cfg := parsePricingExtraConfig(pricing); cfg != nil {
				if configuredMin, ok := getPricingIntOption(cfg, "duration_min", "min_duration"); ok && configuredMin > 0 {
					minDuration = int(configuredMin)
				}
				if configuredMax, ok := getPricingIntOption(cfg, "duration_max", "max_duration"); ok && configuredMax > 0 {
					maxDuration = int(configuredMax)
				}
			}
		}
	}
	if maxDuration < minDuration {
		maxDuration = minDuration
	}
	return minDuration, maxDuration
}

// getVideoStones 获取视频所需灵石，优先读取 pricing 配置
func getVideoStones(pricingModel *model.AIPricingModel, segmentCount int) int64 {
	if pricingModel != nil {
		pricing, err := pricingModel.GetByScene(getVideoPricingScene(segmentCount))
		if err == nil && pricing != nil && pricing.Stones > 0 {
			return pricing.Stones
		}
	}
	return defaultVideoStones
}

// RegisterAIVideoRoutes 注册 AI 生成视频：发起、轮询、查询（火山引擎，结果存自家 OSS）
func RegisterAIVideoRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, stoneRecordModel *model.StoneRecordModel, videoTaskModel *model.AIVideoTaskModel, supportTicketModel *model.SupportTicketModel, cfg *config.Config) {
	setAIVideoSupportTicketModel(supportTicketModel)
	r.POST("/ai/video/create", func(c *gin.Context) {
		handleAIVideoCreate(c, codeSessionModel, userModel, pricingModel, stoneRecordModel, videoTaskModel, cfg)
	})
	r.POST("/ai/video/generate-prompts", func(c *gin.Context) {
		handleGenerateSegmentPrompts(c, codeSessionModel, cfg)
	})
	r.GET("/ai/video/poll/:id", func(c *gin.Context) {
		handleAIVideoPoll(c, codeSessionModel, userModel, pricingModel, stoneRecordModel, videoTaskModel, cfg)
	})
	r.GET("/ai/video/query/:id", func(c *gin.Context) {
		handleAIVideoQuery(c, codeSessionModel, userModel, pricingModel, stoneRecordModel, videoTaskModel, cfg)
	})
}

// AIVideoCreateRequest JSON 请求体（纯文本或带图片 URL）
type AIVideoCreateRequest struct {
	Prompt        string   `json:"prompt" binding:"required"`
	Orientation   string   `json:"orientation"` // 兼容旧版：portrait/landscape，会映射为 ratio
	Resolution    string   `json:"resolution"`  // 480p / 720p / 1080p
	Ratio         string   `json:"ratio"`       // 16:9 / 4:3 / 1:1 / 3:4 / 9:16 / 21:9 / adaptive
	Duration      int      `json:"duration"`    // 默认 5~12 秒，可由 pricing extra_config 覆盖
	CameraFixed   *bool    `json:"camera_fixed"`
	SegmentCount  int      `json:"segment_count"` // 连续生成段数 1~4，多段时用上一段尾帧作下一段首帧，最后 FFmpeg 拼接
	Prompts       []string `json:"prompts"`       // 多段时每段的提示词，长度需等于 segment_count，不传则每段用 prompt
	ImageURL      string   `json:"image_url"`
	StartFrameURL string   `json:"start_frame_url"`
	EndFrameURL   string   `json:"end_frame_url"`
}

// handleAIVideoCreate 发起：纯文本 / 文字+单图 / 首尾帧，调用火山引擎创建任务并落库
func handleAIVideoCreate(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, stoneRecordModel *model.StoneRecordModel, videoTaskModel *model.AIVideoTaskModel, cfg *config.Config) {
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
		return
	}

	var prompt, orientation string
	var ratio, resolution string
	var duration int
	var segmentCount int
	var imageFiles []*multipart.FileHeader
	var imageBodies [][]byte
	parseIntForm := func(s string) int {
		var n int
		fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
		return n
	}

	if c.ContentType() != "" && strings.Contains(strings.ToLower(c.ContentType()), "multipart") {
		if err := c.Request.ParseMultipartForm(20 << 20); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "解析表单失败: " + err.Error()})
			return
		}
		form := c.Request.MultipartForm
		prompt = strings.TrimSpace(c.PostForm("prompt"))
		orientation = strings.TrimSpace(c.PostForm("orientation"))
		ratio = strings.TrimSpace(c.PostForm("ratio"))
		resolution = strings.TrimSpace(c.PostForm("resolution"))
		duration = parseIntForm(c.PostForm("duration"))
		segmentCount = parseIntForm(c.PostForm("segment_count"))
		if segmentCount <= 0 {
			segmentCount = 1
		}
		if segmentCount > 4 {
			segmentCount = 4
		}
		if start := form.File["start_frame"]; len(start) > 0 {
			if end := form.File["end_frame"]; len(end) > 0 {
				imageFiles = []*multipart.FileHeader{start[0], end[0]}
			}
		}
		if len(imageFiles) == 0 {
			if f := form.File["image"]; len(f) > 0 {
				imageFiles = f
			} else if f := form.File["input_reference"]; len(f) > 0 {
				imageFiles = f
			}
		}
	} else {
		var req AIVideoCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		prompt = req.Prompt
		orientation = req.Orientation
		ratio = req.Ratio
		resolution = req.Resolution
		duration = req.Duration
		segmentCount = normalizeVideoSegmentCount(req.SegmentCount)
		if strings.TrimSpace(req.StartFrameURL) != "" && strings.TrimSpace(req.EndFrameURL) != "" {
			body1, err := downloadImageURL(req.StartFrameURL)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "下载首帧图片失败: " + err.Error()})
				return
			}
			body2, err := downloadImageURL(req.EndFrameURL)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "下载尾帧图片失败: " + err.Error()})
				return
			}
			imageBodies = [][]byte{body1, body2}
		} else if strings.TrimSpace(req.ImageURL) != "" {
			body, err := downloadImageURL(req.ImageURL)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "下载图片失败: " + err.Error()})
				return
			}
			imageBodies = [][]byte{body}
		}
	}

	if prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "prompt 不能为空"})
		return
	}
	segmentCount = normalizeVideoSegmentCount(segmentCount)
	resolution = normalizeVideoResolutionLabel(resolution)
	minDuration, maxDuration := getVideoDurationRange(pricingModel, segmentCount)
	if duration < minDuration || duration > maxDuration {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": fmt.Sprintf("duration 必须在 %d-%d 秒之间", minDuration, maxDuration)})
		return
	}
	if segmentCount > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "当前首尾帧视频生成暂不支持连续多段生成"})
		return
	}

	// 扣费：生成视频固定 30 灵石
	videoStones := getVideoStones(pricingModel, segmentCount)
	if userModel != nil && videoStones > 0 {
		currentStones, err := userModel.GetStones(codeSession.UserID)
		if err != nil || currentStones < videoStones {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"code": 402, "msg": "余额不足",
				"data": gin.H{"required": videoStones, "current": currentStones},
			})
			return
		}
		if err := userModel.DeductStones(codeSession.UserID, videoStones); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "扣费失败"})
			return
		}
		if stoneRecordModel != nil {
			_ = stoneRecordModel.Create(codeSession.UserID, "consume", videoStones, "AI生成视频-"+strconv.Itoa(segmentCount)+"段", "")
		}
	}
	refundedVideoStones := false
	refundVideoStones := func() {
		if refundedVideoStones {
			return
		}
		refundedVideoStones = true
		if userModel != nil && videoStones > 0 {
			_ = userModel.AddStones(codeSession.UserID, videoStones)
			if stoneRecordModel != nil {
				_ = stoneRecordModel.Create(codeSession.UserID, "task", videoStones, "视频任务失败退回", "")
			}
		}
	}

	allImageBodies := make([][]byte, 0, len(imageBodies)+len(imageFiles))
	allImageBodies = append(allImageBodies, imageBodies...)
	for _, fh := range imageFiles {
		f, err := fh.Open()
		if err != nil {
			refundVideoStones()
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "读取图片失败: " + err.Error()})
			return
		}
		data, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			refundVideoStones()
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "读取图片失败: " + err.Error()})
			return
		}
		allImageBodies = append(allImageBodies, data)
	}
	if len(allImageBodies) > 2 {
		refundVideoStones()
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "最多支持上传 2 张参考图"})
		return
	}
	numImages := len(allImageBodies)
	mode := "纯文字"
	if numImages == 1 {
		mode = "文字+单图"
	} else if numImages == 2 {
		mode = "首尾帧"
	}
	videoProvider := getVideoProviderAdapter(cfg)
	videoModel := videoProvider.Model(cfg, orientation, ratio)
	videoSize := resolveLaoZhangVideoSize(resolution, ratio, orientation)
	log.Printf("[AIVideo] create 请求 | user_id=%d provider=%s mode=%s model=%s num_images=%d duration=%d resolution=%s size=%s prompt=%s", codeSession.UserID, videoProvider.Code(), mode, videoModel, numImages, duration, resolution, videoSize, prompt)

	baseURL := videoProvider.BaseURL(cfg)
	apiKey := videoProvider.APIKey(cfg)
	if apiKey == "" {
		refundVideoStones()
		keyName := "LAOZHANG_API_KEY"
		if videoProvider.Code() == videoProviderArkSeedance {
			keyName = "ARK_API_KEY"
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "未配置视频 API Key（" + keyName + "）"})
		return
	}

	createURL, requestBody, contentType, err := videoProvider.CreateRequest(baseURL, aiVideoCreateInput{
		Model:       videoModel,
		Prompt:      prompt,
		Duration:    duration,
		Size:        videoSize,
		Ratio:       normalizeVideoRatioLabel(ratio, orientation),
		Resolution:  resolution,
		ImageBodies: allImageBodies,
	})
	if err != nil {
		refundVideoStones()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "构建视频请求失败"})
		return
	}
	httpReq, err := http.NewRequest("POST", createURL, requestBody)
	if err != nil {
		refundVideoStones()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建请求失败"})
		return
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[AIVideo] create request failed | provider=%s base_url=%s model=%s err=%v", videoProvider.Code(), strings.TrimSpace(baseURL), videoModel, err)
		refundVideoStones()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": safeerror.SanitizeAIGenerationError("调用视频服务失败: " + err.Error())})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		log.Printf("[AIVideo] create response failed | status=%d provider=%s base_url=%s model=%s body_summary=%s", resp.StatusCode, videoProvider.Code(), strings.TrimSpace(baseURL), videoModel, summarizeAIVideoRemoteBody(respBody))
		var errBody struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(respBody, &errBody) == nil {
			msg := strings.TrimSpace(errBody.Message)
			if errBody.Error != nil && strings.TrimSpace(errBody.Error.Message) != "" {
				msg = strings.TrimSpace(errBody.Error.Message)
			}
			if msg != "" {
				refundVideoStones()
				c.JSON(http.StatusOK, gin.H{"code": 400, "msg": safeerror.SanitizeAIGenerationError(msg)})
				return
			}
		}
		refundVideoStones()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": safeerror.SanitizeAIGenerationError(fmt.Sprintf("视频服务返回错误: %d", resp.StatusCode))})
		return
	}

	var ext struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(respBody, &ext); err != nil || ext.ID == "" {
		log.Printf("[AIVideo] invalid create response | provider=%s base_url=%s model=%s body_summary=%s", videoProvider.Code(), strings.TrimSpace(baseURL), videoModel, summarizeAIVideoRemoteBody(respBody))
		refundVideoStones()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "视频服务返回格式异常"})
		return
	}
	status := normalizeExternalVideoStatus(ext.Status)
	if status == "" {
		status = model.AIVideoStatusQueued
	}
	log.Printf("[AIVideo] 视频创建成功 | provider=%s external_id=%s", videoProvider.Code(), ext.ID)

	task := &model.AIVideoTask{
		UserID:       codeSession.UserID,
		ExternalID:   ext.ID,
		Model:        videoModel,
		Prompt:       prompt,
		Status:       status,
		Duration:     duration,
		Resolution:   resolution,
		SegmentCount: 1,
	}
	if err := videoTaskModel.Create(task); err != nil {
		log.Printf("[AIVideo] db create failed: %v", err)
		refundVideoStones()
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存任务失败"})
		return
	}

	log.Printf("[AIVideo] create 成功 | user_id=%d task_id=%d external_id=%s | 轮询/查询请使用 task_id: GET /ai/video/poll/%d 或 /ai/video/query/%d",
		codeSession.UserID, task.ID, task.ExternalID, task.ID, task.ID)
	StartAIVideoTaskMonitor(task.ID, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"task_id":     task.ID,
			"external_id": task.ExternalID,
			"status":      task.Status,
			"model":       task.Model,
		},
	})
}

func handleAIVideoPoll(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, stoneRecordModel *model.StoneRecordModel, videoTaskModel *model.AIVideoTaskModel, cfg *config.Config) {
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
		return
	}
	pathID := c.Param("id")
	var id int64
	if _, err := fmt.Sscanf(pathID, "%d", &id); err != nil || id <= 0 {
		log.Printf("[AIVideo] poll 请求 id 非法 | path_id=%q parsed=%d user_id=%d", pathID, id, codeSession.UserID)
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的 task id，请使用 create 返回的 task_id（数字）"})
		return
	}
	task, err := videoTaskModel.GetByIDAndUserID(id, codeSession.UserID)
	if err != nil || task == nil {
		log.Printf("[AIVideo] poll 任务不存在 | task_id=%d user_id=%d err=%v", id, codeSession.UserID, err)
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在"})
		return
	}
	StartAIVideoTaskMonitor(task.ID, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg)
	log.Printf("[AIVideo] poll | task_id=%d external_id=%s user_id=%d status=%s", task.ID, task.ExternalID, codeSession.UserID, task.Status)

	task, syncErr := syncAIVideoTask(task, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg)
	if syncErr != nil {
		log.Printf("[AIVideo] poll 同步失败 | task_id=%d err=%v", id, syncErr)
		task = loadLatestAIVideoTask(videoTaskModel, task)
	}
	effectiveStatus := model.EffectiveAIVideoStatus(task.Status, strings.TrimSpace(task.OSSURL) != "")
	// completed 但无结果且有错误信息：对客户端按失败处理（避免前端停在「生成中」）
	if strings.TrimSpace(task.OSSURL) == "" &&
		strings.TrimSpace(task.GetErrorMessage()) != "" &&
		model.NormalizeAIVideoStatus(task.Status) == model.AIVideoStatusCompleted {
		effectiveStatus = model.AIVideoStatusFailed
	}
	data := gin.H{
		"task_id":           task.ID,
		"status":            effectiveStatus,
		"error_message":     task.GetErrorMessage(),
		"raw_error_message": task.GetRawErrorMessage(),
	}
	if url := strings.TrimSpace(task.OSSURL); url != "" {
		data["url"] = url
		data["duration"] = task.Duration
		data["resolution"] = task.Resolution
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": data,
	})
}

func handleAIVideoQuery(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, userModel *model.UserRedisModel, pricingModel *model.AIPricingModel, stoneRecordModel *model.StoneRecordModel, videoTaskModel *model.AIVideoTaskModel, cfg *config.Config) {
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
		return
	}
	pathID := c.Param("id")
	var id int64
	if _, err := fmt.Sscanf(pathID, "%d", &id); err != nil || id <= 0 {
		log.Printf("[AIVideo] query 请求 id 非法 | path_id=%q parsed=%d user_id=%d", pathID, id, codeSession.UserID)
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的 task id，请使用 create 返回的 task_id（数字）"})
		return
	}
	task, err := videoTaskModel.GetByIDAndUserID(id, codeSession.UserID)
	if err != nil || task == nil {
		log.Printf("[AIVideo] query 任务不存在 | task_id=%d user_id=%d err=%v", id, codeSession.UserID, err)
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "任务不存在"})
		return
	}
	StartAIVideoTaskMonitor(task.ID, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg)
	log.Printf("[AIVideo] query | task_id=%d external_id=%s user_id=%d status=%s oss_url=%v", task.ID, task.ExternalID, codeSession.UserID, task.Status, task.OSSURL != "")

	task, syncErr := syncAIVideoTask(task, videoTaskModel, userModel, pricingModel, stoneRecordModel, cfg)
	if syncErr != nil {
		log.Printf("[AIVideo] query 同步失败 | task_id=%d err=%v", id, syncErr)
		task = loadLatestAIVideoTask(videoTaskModel, task)
	}
	effectiveStatus := model.EffectiveAIVideoStatus(task.Status, strings.TrimSpace(task.OSSURL) != "")
	if strings.TrimSpace(task.OSSURL) == "" {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "ok",
			"data": gin.H{"task_id": task.ID, "status": effectiveStatus, "error_message": task.GetErrorMessage(), "raw_error_message": task.GetRawErrorMessage()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"task_id": task.ID, "status": effectiveStatus,
			"url": task.OSSURL, "duration": task.Duration, "resolution": task.Resolution,
		},
	})
}

// GenerateSegmentPromptsRequest 请求豆包生成多段视频提示词
type GenerateSegmentPromptsRequest struct {
	Theme              string `json:"theme" binding:"required"`
	SegmentCount       int    `json:"segment_count" binding:"required"`
	DurationPerSegment int    `json:"duration_per_segment"` // 每段秒数，用于提示模型，默认 5
}

func handleGenerateSegmentPrompts(c *gin.Context, codeSessionModel *model.CodeSessionRedisModel, cfg *config.Config) {
	codeSession := GetTokenCodeSession(c)
	if codeSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未通过token验证"})
		return
	}
	var req GenerateSegmentPromptsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
		return
	}
	if req.SegmentCount < 1 || req.SegmentCount > 4 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "segment_count 需为 1～4"})
		return
	}
	if req.DurationPerSegment <= 0 {
		req.DurationPerSegment = 5
	}
	apiKey := cfg.AI.ArkAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ARK_API_KEY")
	}
	if apiKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "未配置豆包 API Key"})
		return
	}
	arkBase := "https://ark.cn-beijing.volces.com"
	systemPrompt := fmt.Sprintf(`你是视频脚本助手。用户要生成 %d 段连续短视频，每段约 %d 秒。请根据用户给出的主题或想法，生成恰好 %d 段视频的提示词，每段一句或一小段，用于AI视频生成。只输出 %d 行，每行一段提示词，不要编号、不要多余解释。`, req.SegmentCount, req.DurationPerSegment, req.SegmentCount, req.SegmentCount)
	inputList := []map[string]interface{}{
		{"role": "system", "content": []map[string]interface{}{{"type": "input_text", "text": systemPrompt}}},
		{"role": "user", "content": []map[string]interface{}{{"type": "input_text", "text": strings.TrimSpace(req.Theme)}}},
	}
	body := map[string]interface{}{
		"model":  "doubao-seed-2-0-pro-260215",
		"stream": true,
		"input":  inputList,
	}
	bodyBytes, _ := json.Marshal(body)
	httpReq, err := http.NewRequest("POST", arkBase+"/responses", bytes.NewReader(bodyBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建请求失败"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[AIVideo] generate-prompts 请求失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "调用豆包失败"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		log.Printf("[AIVideo] generate-prompts 返回 %d: %s", resp.StatusCode, string(rb))
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "豆包返回错误"})
		return
	}
	var fullText strings.Builder
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var evt map[string]interface{}
		if json.Unmarshal([]byte(data), &evt) != nil {
			continue
		}
		t, _ := evt["type"].(string)
		if strings.Contains(t, "output_text") && strings.Contains(t, "delta") {
			if d, ok := evt["delta"].(string); ok && d != "" {
				fullText.WriteString(d)
			}
		}
	}
	text := strings.TrimSpace(fullText.String())
	var prompts []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		// 去掉行首编号如 1. 2. 或 1、
		if len(line) > 0 {
			for _, prefix := range []string{"1. ", "2. ", "3. ", "4. ", "1、", "2、", "3、", "4、"} {
				if strings.HasPrefix(line, prefix) {
					line = strings.TrimSpace(line[len(prefix):])
					break
				}
			}
		}
		if line != "" {
			prompts = append(prompts, line)
		}
	}
	if len(prompts) > req.SegmentCount {
		prompts = prompts[:req.SegmentCount]
	}
	for len(prompts) < req.SegmentCount && len(prompts) > 0 {
		prompts = append(prompts, prompts[len(prompts)-1])
	}
	if len(prompts) == 0 {
		prompts = make([]string, req.SegmentCount)
		for i := range prompts {
			prompts[i] = req.Theme
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{"prompts": prompts}})
}

// runContinuousVideoChain 连续生成多段视频：每段用 segmentPrompts[i-1] 作为文案、上一段尾帧作首帧，最后 FFmpeg 拼接并加水印上传
func runContinuousVideoChain(
	cfg *config.Config,
	cosClient *cos.Client,
	videoTaskModel *model.AIVideoTaskModel,
	taskID int64,
	userID int64,
	videoStones int64,
	userModel *model.UserRedisModel,
	stoneRecordModel *model.StoneRecordModel,
	baseURL, apiKey, videoModel, ratio, resolution string,
	duration int,
	cameraFixed *bool,
	segmentCount int,
	segmentPrompts []string,
	contentSegment1 []map[string]interface{},
) {
	refund := func() {
		if userModel != nil && videoStones > 0 {
			_ = userModel.AddStones(userID, videoStones)
			if stoneRecordModel != nil {
				_ = stoneRecordModel.Create(userID, "task", videoStones, "视频任务失败退回", "")
			}
		}
	}
	var segmentVideos [][]byte
	var totalDuration int
	resolutionStr := resolution
	if resolutionStr == "" {
		resolutionStr = "720p"
	}
	if len(segmentPrompts) < segmentCount {
		// 补齐
		for len(segmentPrompts) < segmentCount {
			segmentPrompts = append(segmentPrompts, segmentPrompts[0])
		}
	}
	lastFrameBase64 := "" // 上一段尾帧图 base64，用作下一段首帧

	for i := 1; i <= segmentCount; i++ {
		segText := segmentPrompts[i-1]
		var content []map[string]interface{}
		if i == 1 {
			content = make([]map[string]interface{}, len(contentSegment1))
			for j := range contentSegment1 {
				content[j] = make(map[string]interface{})
				for k, v := range contentSegment1[j] {
					content[j][k] = v
				}
				if t, _ := contentSegment1[j]["type"].(string); t == "text" {
					content[j]["text"] = segText
				}
			}
		} else if lastFrameBase64 != "" {
			content = []map[string]interface{}{
				{"type": "text", "text": segText},
				{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + lastFrameBase64}, "role": "first_frame"},
			}
		} else {
			content = []map[string]interface{}{{"type": "text", "text": segText}}
		}
		reqBody := map[string]interface{}{
			"model":             videoModel,
			"content":           content,
			"return_last_frame": i < segmentCount,
		}
		if ratio != "" {
			reqBody["ratio"] = ratio
		}
		if resolution != "" {
			reqBody["resolution"] = resolution
		}
		if duration >= 5 && duration <= 15 {
			reqBody["duration"] = duration
		}
		if cameraFixed != nil {
			reqBody["camera_fixed"] = *cameraFixed
		}
		bodyBytes, _ := json.Marshal(reqBody)
		httpReq, err := http.NewRequest("POST", baseURL+"/api/v3/contents/generations/tasks", strings.NewReader(string(bodyBytes)))
		if err != nil {
			refund()
			_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, "创建子任务失败")
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			log.Printf("[AIVideo] continuous segment %d create failed: %v", i, err)
			refund()
			_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, "调用视频服务失败")
			return
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Printf("[AIVideo] continuous segment create response failed | segment=%d status=%d base_url=%s body_summary=%s", i, resp.StatusCode, strings.TrimSpace(baseURL), summarizeAIVideoRemoteBody(respBody))
			refund()
			_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, fmt.Sprintf("第%d段创建失败", i))
			return
		}
		var ext struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(respBody, &ext) != nil || ext.ID == "" {
			refund()
			_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, "第"+fmt.Sprint(i)+"段返回异常")
			return
		}
		externalID := ext.ID
		log.Printf("[AIVideo] continuous segment %d/%d created external_id=%s", i, segmentCount, externalID)

		// 轮询直到完成
		var videoURL, lastFrameURL string
		var segDuration int
		for attempt := 0; attempt < 240; attempt++ {
			time.Sleep(5 * time.Second)
			req2, _ := http.NewRequest("GET", baseURL+"/api/v3/contents/generations/tasks/"+externalID, nil)
			req2.Header.Set("Authorization", "Bearer "+apiKey)
			req2.Header.Set("Content-Type", "application/json")
			resp2, err := client.Do(req2)
			if err != nil {
				continue
			}
			body2, _ := io.ReadAll(resp2.Body)
			resp2.Body.Close()
			var tr struct {
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
				Content *struct {
					VideoURL     string `json:"video_url"`
					LastFrameURL string `json:"last_frame_url"`
					LastFrame    *struct {
						URL string `json:"url"`
					} `json:"last_frame"`
				} `json:"content"`
				Duration   int    `json:"duration"`
				Resolution string `json:"resolution"`
			}
			_ = json.Unmarshal(body2, &tr)
			segmentStatus := normalizeExternalVideoStatus(tr.Status)
			if segmentStatus == model.AIVideoStatusCompleted && tr.Content != nil && tr.Content.VideoURL != "" {
				videoURL = tr.Content.VideoURL
				lastFrameURL = tr.Content.LastFrameURL
				if tr.Content.LastFrame != nil && tr.Content.LastFrame.URL != "" {
					lastFrameURL = tr.Content.LastFrame.URL
				}
				segDuration = tr.Duration
				if tr.Resolution != "" {
					resolutionStr = tr.Resolution
				}
				break
			}
			if segmentStatus == model.AIVideoStatusFailed {
				msg := "任务失败或已取消"
				if tr.Error != nil && tr.Error.Message != "" {
					msg = tr.Error.Message
				}
				log.Printf("[AIVideo] continuous segment %d failed: %s", i, msg)
				refund()
				_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, safeerror.SanitizeAIGenerationError(fmt.Sprintf("第%d段: %s", i, msg)))
				return
			}
		}
		if videoURL == "" {
			refund()
			_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, fmt.Sprintf("第%d段超时未完成", i))
			return
		}
		totalDuration += segDuration

		// 下载本段视频
		videoResp, err := http.Get(videoURL)
		if err != nil {
			refund()
			_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, "下载第"+fmt.Sprint(i)+"段视频失败")
			return
		}
		segData, err := io.ReadAll(videoResp.Body)
		videoResp.Body.Close()
		if err != nil {
			refund()
			_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, "读取第"+fmt.Sprint(i)+"段视频失败")
			return
		}
		segmentVideos = append(segmentVideos, segData)

		if i < segmentCount && lastFrameURL != "" {
			frameResp, err := http.Get(lastFrameURL)
			if err != nil {
				log.Printf("[AIVideo] download last_frame segment %d: %v", i, err)
				refund()
				_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, "获取尾帧失败")
				return
			}
			frameData, err := io.ReadAll(frameResp.Body)
			frameResp.Body.Close()
			if err == nil && len(frameData) > 0 {
				lastFrameBase64 = base64.StdEncoding.EncodeToString(frameData)
			}
		}
	}

	// 拼接
	concatData, err := component.ConcatVideos(segmentVideos)
	if err != nil {
		log.Printf("[AIVideo] concat failed: %v", err)
		refund()
		_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, "视频拼接失败")
		return
	}
	if watermarked, wErr := component.AddVideoWatermark(concatData); wErr == nil {
		concatData = watermarked
	}
	if cosClient == nil {
		cosClient = component.GetCOSClient()
	}
	if cosClient == nil || cfg == nil {
		refund()
		_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, "存储服务未初始化")
		return
	}
	key := path.Join("ai_video", fmt.Sprintf("%d_continuous.mp4", taskID))
	ctx := context.Background()
	ossURL, err := function.UploadBytes(ctx, cosClient, cfg, key, concatData, "video/mp4")
	if err != nil {
		log.Printf("[AIVideo] continuous upload failed: %v", err)
		refund()
		_ = videoTaskModel.UpdateStatus(taskID, model.AIVideoStatusFailed, "上传失败")
		return
	}
	if err := videoTaskModel.UpdateOSSURL(taskID, ossURL, totalDuration, resolutionStr); err != nil {
		log.Printf("[AIVideo] continuous update oss_url failed: %v", err)
	}
	log.Printf("[AIVideo] 连续生成完成 | task_id=%d segments=%d duration=%d url=%s", taskID, segmentCount, totalDuration, ossURL)
}
