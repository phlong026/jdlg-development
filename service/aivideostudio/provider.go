// Package aivideostudio owns the independent designer video workflow.
package aivideostudio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrProviderUnavailable     = errors.New("视频服务尚未配置")
	ErrSubmissionUncertain     = errors.New("提交结果待确认，请勿重复生成")
	ErrSubmissionRejected      = errors.New("视频请求未受理")
	ErrCancellationUnsupported = errors.New("视频服务暂不支持取消已受理任务")
)

type Provider interface {
	Code() string
	Capabilities(context.Context) (ProviderCapabilities, error)
	Submit(context.Context, GenerationRequest) (GenerationReceipt, error)
	Query(context.Context, string) (GenerationStatus, error)
	Cancel(context.Context, string) error
}

type ProviderCapabilities struct{ Modes, Ratios, Resolutions []string }
type GenerationReference struct {
	MediaID, Type, URL, Purpose string
	Position                    int
	Duration                    float64
}
type GenerationRequest struct {
	IdempotencyKey, Mode, Prompt, Ratio, Resolution string
	Duration                                        int
	FirstFrameURL, LastFrameURL                     string
	References                                      []GenerationReference
}
type GenerationReceipt struct{ ExternalID string }
type GenerationStatus struct {
	Status, ResultURL, ErrorCode string
	OutputSeconds                float64
}

type metasoProvider struct {
	apiKey string
	client *http.Client
}

// NewMetasoProvider uses a fixed origin and never follows redirects carrying
// credentials. An empty dedicated key cannot enable paid network calls.
func NewMetasoProvider(apiKey string) Provider {
	return &metasoProvider{apiKey: strings.TrimSpace(apiKey), client: &http.Client{
		Timeout:       90 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (*metasoProvider) Code() string { return "metaso_h3" }
func (p *metasoProvider) Capabilities(ctx context.Context) (ProviderCapabilities, error) {
	if p.apiKey == "" {
		return ProviderCapabilities{}, ErrProviderUnavailable
	}
	if err := ctx.Err(); err != nil {
		return ProviderCapabilities{}, err
	}
	// This declares adapter support, not a successful live probe or account grant.
	return ProviderCapabilities{
		Modes:       []string{"text", "first_frame", "first_last_frame", "reference"},
		Ratios:      []string{"adaptive", "21:9", "16:9", "4:3", "1:1", "3:4", "9:16"},
		Resolutions: []string{"768P", "2K"},
	}, nil
}

func (p *metasoProvider) Submit(ctx context.Context, input GenerationRequest) (GenerationReceipt, error) {
	if p.apiKey == "" {
		return GenerationReceipt{}, ErrProviderUnavailable
	}
	body, err := metasoRequest(input)
	if err != nil {
		return GenerationReceipt{}, fmt.Errorf("%w: %s", ErrSubmissionRejected, err)
	}
	response, _, err := p.request(ctx, http.MethodPost, "/video_generation", body)
	if err != nil {
		// Native H3 has no verified idempotency guarantee. Even malformed 2xx
		// or HTTP rejection responses may represent an accepted task: its public
		// native contract does not promise non-acceptance for a particular code.
		return GenerationReceipt{}, ErrSubmissionUncertain
	}
	var receipt struct {
		TaskID string `json:"task_id"`
	}
	if json.Unmarshal(response, &receipt) != nil || strings.TrimSpace(receipt.TaskID) == "" || len(receipt.TaskID) > 256 {
		return GenerationReceipt{}, ErrSubmissionUncertain
	}
	return GenerationReceipt{ExternalID: receipt.TaskID}, nil
}

func (p *metasoProvider) Query(ctx context.Context, externalID string) (GenerationStatus, error) {
	if p.apiKey == "" {
		return GenerationStatus{}, ErrProviderUnavailable
	}
	if strings.TrimSpace(externalID) == "" || len(externalID) > 256 {
		return GenerationStatus{}, errors.New("任务标识无效")
	}
	body, _, err := p.request(ctx, http.MethodGet, "/query/video_generation/"+url.PathEscape(externalID), nil)
	if err != nil {
		return GenerationStatus{}, errors.New("任务状态暂时不可查询")
	}
	var response struct {
		Task  *metasoTask  `json:"task"`
		Items []metasoTask `json:"items"`
		metasoTask
	}
	if json.Unmarshal(body, &response) != nil {
		return GenerationStatus{}, errors.New("任务状态响应无效")
	}
	task := response.Task
	if task == nil && (response.Status != "" || response.ID != "") {
		task = &response.metasoTask
	}
	if task == nil {
		for i := range response.Items {
			if response.Items[i].ID == externalID {
				task = &response.Items[i]
				break
			}
		}
	}
	if task == nil {
		return GenerationStatus{}, errors.New("任务状态响应缺少当前任务")
	}
	if task.ID != "" && task.ID != externalID {
		return GenerationStatus{}, errors.New("任务状态标识不匹配")
	}
	result := GenerationStatus{Status: "running"}
	switch strings.ToLower(task.Status) {
	case "succeeded", "success", "completed", "done":
		if !validProviderMediaURL(task.Content.URL) {
			return GenerationStatus{}, errors.New("成片地址暂不可用，请重新查询")
		}
		result.Status = "succeeded"
		result.ResultURL = task.Content.URL
		if task.Usage.OutputSeconds > 0 && !math.IsInf(task.Usage.OutputSeconds, 0) && !math.IsNaN(task.Usage.OutputSeconds) {
			result.OutputSeconds = task.Usage.OutputSeconds
		}
	case "failed", "fail", "error", "expired":
		result.Status = "failed"
		result.ErrorCode = "provider_failed"
	case "cancelled", "canceled":
		result.Status = "cancelled"
		result.ErrorCode = "provider_cancelled"
	case "queued", "pending", "submitted", "running", "processing", "in_progress", "created":
		// Keep known nonterminal states observable without inventing success.
	default:
		return GenerationStatus{}, errors.New("任务状态暂无法识别，请稍后查询")
	}
	return result, nil
}

type metasoTask struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Content struct {
		URL string `json:"url"`
	} `json:"content"`
	Usage struct {
		OutputSeconds float64 `json:"output_seconds"`
	} `json:"usage"`
}

func (*metasoProvider) Cancel(context.Context, string) error { return ErrCancellationUnsupported }

func (p *metasoProvider) request(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, "https://metaso.cn/api/minimax/v2"+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, errors.New("视频请求构建失败")
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, errors.New("视频服务连接失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, errors.New("视频服务请求未完成")
	}
	const maxBody = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(data) > maxBody {
		return nil, resp.StatusCode, errors.New("视频响应读取失败")
	}
	return data, resp.StatusCode, nil
}

func metasoRequest(input GenerationRequest) ([]byte, error) {
	if strings.TrimSpace(input.Prompt) == "" || utf8.RuneCountInString(input.Prompt) > 7000 {
		return nil, errors.New("请填写不超过7000字的画面描述")
	}
	if input.Resolution != "768P" && input.Resolution != "2K" {
		return nil, errors.New("清晰度不支持")
	}
	if input.Duration < 4 || input.Duration > 30 || (input.Resolution == "2K" && input.Duration > 15) {
		return nil, errors.New("时长与清晰度组合不支持")
	}
	if !providerRatio(input.Ratio) {
		return nil, errors.New("画面比例不支持")
	}
	content := []map[string]any{{"type": "text", "text": input.Prompt}}
	addImage := func(role, raw string) error {
		if !validProviderMediaURL(raw) {
			return errors.New("图片需要有效的 HTTPS 地址")
		}
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": raw}, "role": role})
		return nil
	}
	switch input.Mode {
	case "text":
		if input.FirstFrameURL != "" || input.LastFrameURL != "" || len(input.References) > 0 || input.Ratio == "adaptive" {
			return nil, errors.New("文生视频需选择固定比例且不能携带素材")
		}
	case "first_frame", "first_last_frame":
		if input.FirstFrameURL == "" || len(input.References) > 0 || input.Ratio != "adaptive" {
			return nil, errors.New("首帧模式需有效首帧并使用图片比例")
		}
		if (input.Mode == "first_frame" && input.LastFrameURL != "") || (input.Mode == "first_last_frame" && input.LastFrameURL == "") {
			return nil, errors.New("首尾帧组合无效")
		}
		if err := addImage("first_frame", input.FirstFrameURL); err != nil {
			return nil, err
		}
		if input.LastFrameURL != "" {
			if err := addImage("last_frame", input.LastFrameURL); err != nil {
				return nil, err
			}
		}
	case "reference":
		if input.FirstFrameURL != "" || input.LastFrameURL != "" || len(input.References) == 0 || len(input.References) > 12 {
			return nil, errors.New("参考素材组合无效")
		}
		counts := map[string]int{}
		duration := map[string]float64{}
		seen := map[string]bool{}
		bindings := make([]string, 0, len(input.References))
		for index, ref := range input.References {
			if ref.Position != 0 && ref.Position != index+1 {
				return nil, errors.New("参考素材编号与请求顺序不一致")
			}
			if ref.MediaID == "" || seen[ref.MediaID] || !validProviderMediaURL(ref.URL) {
				return nil, errors.New("参考素材失效或重复")
			}
			seen[ref.MediaID] = true
			counts[ref.Type]++
			if ref.Type == "video" || ref.Type == "audio" {
				if math.IsNaN(ref.Duration) || math.IsInf(ref.Duration, 0) || ref.Duration < 2 || ref.Duration > 15 {
					return nil, errors.New("参考音视频单条须为2–15秒")
				}
				duration[ref.Type] += ref.Duration
			}
			if (ref.Type == "image" && counts[ref.Type] > 8) || (ref.Type != "image" && counts[ref.Type] > 3) || duration[ref.Type] > 15 {
				return nil, errors.New("参考素材数量或时长超限")
			}
			if ref.Type != "image" && ref.Type != "video" && ref.Type != "audio" {
				return nil, errors.New("参考素材类型不支持")
			}
			mediaType := ref.Type + "_url"
			item := map[string]any{"type": mediaType, mediaType: map[string]string{"url": ref.URL}, "role": "reference_" + ref.Type}
			label := map[string]string{"image": "图片", "video": "视频", "audio": "音频"}[ref.Type] + fmt.Sprint(counts[ref.Type])
			if ref.Type == "image" {
				item["name"] = label
			}
			if purpose := strings.TrimSpace(ref.Purpose); purpose != "" {
				if utf8.RuneCountInString(purpose) > 120 {
					return nil, errors.New("单项素材用途不能超过120字")
				}
				bindings = append(bindings, fmt.Sprintf("%s（引用编号%d）：%s", label, index+1, purpose))
			}
			content = append(content, item)
		}
		// The native API has no purpose field. Keep the binding in model-visible
		// text instead of silently dropping user intent or inventing JSON fields.
		if len(bindings) > 0 {
			prompt := input.Prompt + "\n\n参考素材用途：\n" + strings.Join(bindings, "\n")
			if utf8.RuneCountInString(prompt) > 7000 {
				return nil, errors.New("画面描述与素材用途合计不能超过7000字")
			}
			content[0]["text"] = prompt
		}
	default:
		return nil, errors.New("生成模式不支持")
	}
	return json.Marshal(map[string]any{"model": "MiniMax-H3", "content": content, "resolution": input.Resolution, "duration": input.Duration, "ratio": input.Ratio})
}

func providerRatio(ratio string) bool {
	switch ratio {
	case "adaptive", "21:9", "16:9", "4:3", "1:1", "3:4", "9:16":
		return true
	}
	return false
}

// This adapter does not download URLs. Retention must additionally use the
// project's DNS-pinned safe downloader; HTTPS syntax alone cannot stop rebinding.
func validProviderMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
	}
	return true
}

// ValidateGenerationRequest applies the same native schema used by Submit,
// without issuing a network request. Queue builders can fail before dispatch.
func ValidateGenerationRequest(input GenerationRequest) error {
	_, err := metasoRequest(input)
	return err
}
