package component

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"runtime"
	"strings"
	"sync"
	"time"

	"service/config"

	"github.com/google/uuid"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	cos "github.com/tencentyun/cos-go-sdk-v5"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	primaryAIRequestTimeout      = 150 * time.Second
	firstFallbackRequestTimeout  = 150 * time.Second
	secondFallbackRequestTimeout = 60 * time.Second
	defaultAIRequestTimeout      = 300 * time.Second
	seedreamAIRequestTimeout     = 90 * time.Second
	remoteResultDownloadTimeout  = 150 * time.Second
	aiTaskContextTimeout         = 600 * time.Second
	aiTaskListThumbWidth         = 480
	toapisPollInterval           = 3 * time.Second
	toapisMaxWait                = 120 * time.Second
	runningHubMediaUploadURL     = "https://www.runninghub.cn/openapi/v2/media/upload/binary"
)

// SystemResources 系统资源信息
type SystemResources struct {
	CPUCores        int     `json:"cpu_cores"`         // CPU核心数
	CPUUsagePercent float64 `json:"cpu_usage_percent"` // CPU使用率
	TotalMemoryMB   uint64  `json:"total_memory_mb"`   // 总内存（MB）
	AvailMemoryMB   uint64  `json:"avail_memory_mb"`   // 可用内存（MB）
	MemUsagePercent float64 `json:"mem_usage_percent"` // 内存使用率
	GoMaxProcs      int     `json:"go_max_procs"`      // Go最大并发数
}

// GetSystemResources 获取系统资源信息
func GetSystemResources() *SystemResources {
	resources := &SystemResources{
		CPUCores:   runtime.NumCPU(),
		GoMaxProcs: runtime.GOMAXPROCS(0),
	}

	// 获取CPU使用率
	cpuPercent, err := cpu.Percent(time.Second, false)
	if err == nil && len(cpuPercent) > 0 {
		resources.CPUUsagePercent = cpuPercent[0]
	}

	// 获取内存信息
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		resources.TotalMemoryMB = memInfo.Total / 1024 / 1024
		resources.AvailMemoryMB = memInfo.Available / 1024 / 1024
		resources.MemUsagePercent = memInfo.UsedPercent
	}

	return resources
}

// CalculateOptimalPoolSize 根据系统资源计算最优的池大小
// 返回: maxWorkers（工作协程数）, queueSize（队列大小）
func CalculateOptimalPoolSize() (maxWorkers int, queueSize int) {
	resources := GetSystemResources()

	// 计算工作协程数
	// 基于CPU核数：每个核心分配2-4个工作协程（因为AI请求主要是IO密集型）
	cpuBasedWorkers := resources.CPUCores * 4

	// 基于内存：每256MB内存分配1个工作协程（每个协程约需要50-100MB处理大型响应）
	memBasedWorkers := int(resources.AvailMemoryMB / 256)
	if memBasedWorkers < 1 {
		memBasedWorkers = 1
	}

	// 取两者的较小值作为最大工作协程数
	maxWorkers = cpuBasedWorkers
	if memBasedWorkers < maxWorkers {
		maxWorkers = memBasedWorkers
	}

	// 设置上下限
	// 最小值：2（至少保证并发能力）
	// 最大值：100（避免过多协程导致调度开销）
	if maxWorkers < 2 {
		maxWorkers = 2
	}
	if maxWorkers > 100 {
		maxWorkers = 100
	}

	// 队列大小：工作协程数的10倍，用于缓冲突发请求
	queueSize = maxWorkers * 10
	if queueSize < 50 {
		queueSize = 50
	}
	if queueSize > 1000 {
		queueSize = 1000
	}

	log.Printf("[ResourceCalculator] 系统资源: CPU核数=%d, 可用内存=%dMB, CPU使用率=%.1f%%, 内存使用率=%.1f%%",
		resources.CPUCores, resources.AvailMemoryMB, resources.CPUUsagePercent, resources.MemUsagePercent)
	log.Printf("[ResourceCalculator] 计算结果: 基于CPU=%d, 基于内存=%d, 最终工作协程数=%d, 队列大小=%d",
		cpuBasedWorkers, memBasedWorkers, maxWorkers, queueSize)

	return maxWorkers, queueSize
}

// HTTPRequest HTTP请求配置
type HTTPRequest struct {
	URL         string            `json:"url"`          // 请求地址
	Method      string            `json:"method"`       // 请求方法：GET, POST, PUT, DELETE
	Headers     map[string]string `json:"headers"`      // 请求头
	Body        interface{}       `json:"body"`         // 请求体（会被序列化为JSON）
	Timeout     time.Duration     `json:"timeout"`      // 超时时间
	ContentType string            `json:"content_type"` // 内容类型，默认 application/json
}

// HTTPResponse HTTP响应
type HTTPResponse struct {
	StatusCode int               `json:"status_code"` // HTTP状态码
	Headers    map[string]string `json:"headers"`     // 响应头
	Body       []byte            `json:"body"`        // 响应体原始数据
	Error      error             `json:"error"`       // 错误信息
	Duration   time.Duration     `json:"duration"`    // 请求耗时
}

// AITaskContext AI任务上下文（线程隔离）
type AITaskContext struct {
	TaskID          string                 `json:"task_id"`    // 任务ID
	TaskNo          string                 `json:"task_no"`    // 任务编号
	UserID          int64                  `json:"user_id"`    // 用户ID
	TaskType        string                 `json:"task_type"`  // 任务类型：ai_draw, ai_chat
	APIConfig       *AIAPIConfigData       `json:"api_config"` // API配置
	FallbackConfigs []*AIAPIConfigData     `json:"fallback_configs"`
	Payload         map[string]interface{} `json:"payload"`    // 请求参数
	Prompt          string                 `json:"prompt"`     // 提示词
	ImageURL        string                 `json:"image_url"`  // 用户上传的图片URL（单图）
	ImageURLs       []string               `json:"image_urls"` // 用户上传的图片URL数组（多图）
	AttemptIndex    int                    `json:"attempt_index"`
	CreatedAt       time.Time              `json:"created_at"` // 创建时间
	ctx             context.Context        // 上下文（用于取消）
	cancel          context.CancelFunc     // 取消函数
}

// AIAPIConfigData API配置数据
type AIAPIConfigData struct {
	TaskType                 string `json:"task_type"`
	ProviderCode             string `json:"provider_code"`
	ProviderName             string `json:"provider_name"`
	ProtocolType             string `json:"protocol_type"`
	APIEndpoint              string `json:"api_endpoint"`
	Method                   string `json:"method"`
	APIKey                   string `json:"api_key"`          // API Key
	APIKeyLocation           string `json:"api_key_location"` // API Key 发送位置
	APIKeyName               string `json:"api_key_name"`     // API Key 名称
	Headers                  string `json:"headers"`          // JSON格式
	BodyTemplate             string `json:"body_template"`    // JSON格式模板，使用 {{prompt}}、{{image}}、{{aspect_ratio}}、{{image_size}} 占位符
	EnablePromptOptimization bool   `json:"enable_prompt_optimization"`
}

// AITaskResult AI任务结果
type AITaskResult struct {
	TaskID             string                 `json:"task_id"`
	TaskNo             string                 `json:"task_no"`
	Success            bool                   `json:"success"`
	ResultURL          string                 `json:"result_url"`     // OSS 带水印图 URL（默认返回）
	ResultURLRaw       string                 `json:"result_url_raw"` // OSS 原图 URL（无水印，按规则下载）
	ResultURLs         []string               `json:"result_urls"`
	ResultURLsRaw      []string               `json:"result_urls_raw"`
	ThumbnailURL       string                 `json:"thumbnail_url"`
	ThumbnailURLs      []string               `json:"thumbnail_urls"`
	UsedModel          string                 `json:"used_model"`
	APIEndpoint        string                 `json:"api_endpoint"`
	ProviderCode       string                 `json:"provider_code"`
	ProviderName       string                 `json:"provider_name"`
	ProtocolType       string                 `json:"protocol_type"`
	ExternalTaskID     string                 `json:"external_task_id"`
	ExternalStatus     string                 `json:"external_status"`
	AttemptedModels    []string               `json:"attempted_models"`
	AttemptedEndpoints []string               `json:"attempted_endpoints"`
	ResultData         map[string]interface{} `json:"result_data"`
	Error              string                 `json:"error"`
	Duration           time.Duration          `json:"duration"`
}

// TaskCallbackFunc 任务完成回调函数类型
type TaskCallbackFunc func(result *AITaskResult)

// RequestPool HTTP请求池（线程池）
type RequestPool struct {
	maxWorkers    int                 // 最大工作协程数
	taskQueue     chan *aiTaskWrapper // 任务队列
	activeWorkers int                 // 当前活跃协程数
	mu            sync.Mutex          // 互斥锁
	wg            sync.WaitGroup      // 等待组
	cfg           *config.Config      // 配置
	cosClient     *cos.Client         // COS客户端
	running       bool                // 是否运行中
}

// aiTaskWrapper AI任务包装器
type aiTaskWrapper struct {
	taskCtx  *AITaskContext
	callback TaskCallbackFunc
}

var (
	globalRequestPool *RequestPool
	poolOnce          sync.Once
)

// InitRequestPool 初始化全局请求池
// 如果 maxWorkers 或 queueSize 为 0，则自动根据系统资源计算最优值
func InitRequestPool(maxWorkers int, queueSize int, cfg *config.Config) *RequestPool {
	poolOnce.Do(func() {
		// 如果参数为0，则自动计算
		if maxWorkers <= 0 || queueSize <= 0 {
			autoWorkers, autoQueueSize := CalculateOptimalPoolSize()
			if maxWorkers <= 0 {
				maxWorkers = autoWorkers
			}
			if queueSize <= 0 {
				queueSize = autoQueueSize
			}
		}

		globalRequestPool = &RequestPool{
			maxWorkers: maxWorkers,
			taskQueue:  make(chan *aiTaskWrapper, queueSize),
			cfg:        cfg,
			cosClient:  GetCOSClient(),
			running:    true,
		}
		// 启动工作协程
		for i := 0; i < maxWorkers; i++ {
			go globalRequestPool.worker(i)
		}

		// 获取系统资源信息用于日志
		resources := GetSystemResources()
		log.Printf("[RequestPool] 初始化完成")
		log.Printf("[RequestPool] 系统配置: CPU核数=%d, 总内存=%dMB, 可用内存=%dMB",
			resources.CPUCores, resources.TotalMemoryMB, resources.AvailMemoryMB)
		log.Printf("[RequestPool] 池配置: 工作协程数=%d, 队列大小=%d", maxWorkers, queueSize)
	})
	return globalRequestPool
}

// InitRequestPoolAuto 自动初始化请求池（根据系统资源自动配置）
func InitRequestPoolAuto(cfg *config.Config) *RequestPool {
	return InitRequestPool(0, 0, cfg)
}

// GetRequestPool 获取全局请求池
func GetRequestPool() *RequestPool {
	return globalRequestPool
}

// worker 工作协程
func (p *RequestPool) worker(id int) {
	log.Printf("[RequestPool] Worker %d 启动", id)
	for task := range p.taskQueue {
		if task == nil {
			continue
		}
		p.mu.Lock()
		p.activeWorkers++
		p.mu.Unlock()

		// 执行AI任务
		result := p.executeAITask(task.taskCtx)

		// 执行回调
		if task.callback != nil {
			task.callback(result)
		}

		p.mu.Lock()
		p.activeWorkers--
		p.mu.Unlock()
	}
	log.Printf("[RequestPool] Worker %d 停止", id)
}

// SubmitAITask 提交AI任务到队列
func (p *RequestPool) SubmitAITask(taskCtx *AITaskContext, callback TaskCallbackFunc) error {
	if !p.running {
		log.Printf("[RequestPool] ❌ 任务提交失败: 请求池已停止, TaskNo=%s", taskCtx.TaskNo)
		return fmt.Errorf("请求池已停止")
	}

	select {
	case p.taskQueue <- &aiTaskWrapper{taskCtx: taskCtx, callback: callback}:
		p.mu.Lock()
		queueLen := len(p.taskQueue)
		activeWorkers := p.activeWorkers
		p.mu.Unlock()
		log.Printf("========================================")
		log.Printf("[RequestPool] 📥 收到新任务")
		log.Printf("[RequestPool] 任务编号: %s", taskCtx.TaskNo)
		log.Printf("[RequestPool] 任务类型: %s", taskCtx.TaskType)
		log.Printf("[RequestPool] 用户ID: %d", taskCtx.UserID)
		log.Printf("[RequestPool] 当前队列长度: %d", queueLen)
		log.Printf("[RequestPool] 活跃Worker数: %d/%d", activeWorkers, p.maxWorkers)
		log.Printf("[RequestPool] 任务已加入队列，等待处理...")
		log.Printf("========================================")
		return nil
	default:
		log.Printf("[RequestPool] ❌ 任务提交失败: 任务队列已满, TaskNo=%s", taskCtx.TaskNo)
		return fmt.Errorf("任务队列已满")
	}
}

// executeAITask 执行AI任务
func (p *RequestPool) executeAITask(taskCtx *AITaskContext) *AITaskResult {
	startTime := time.Now()
	result := &AITaskResult{
		TaskID: taskCtx.TaskID,
		TaskNo: taskCtx.TaskNo,
	}

	log.Printf("========================================")
	log.Printf("[AI任务] 开始执行任务")
	log.Printf("[AI任务] 任务编号: %s", taskCtx.TaskNo)
	log.Printf("[AI任务] 任务类型: %s", taskCtx.TaskType)
	log.Printf("[AI任务] 用户ID: %d", taskCtx.UserID)
	log.Printf("[AI任务] 提示词: %s", truncateString(taskCtx.Prompt, 100))
	if taskCtx.ImageURL != "" {
		log.Printf("[AI任务] 图片URL: %s", taskCtx.ImageURL)
	}
	log.Printf("========================================")

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[AI任务] ❌ 任务执行异常(panic): TaskNo=%s, Error=%v", taskCtx.TaskNo, r)
			result.Success = false
			result.Error = fmt.Sprintf("任务执行异常: %v", r)
		}
		result.Duration = time.Since(startTime)
	}()

	// 1. 构建请求体
	log.Printf("[AI任务] 步骤1: 构建请求体...")
	requestBody, err := p.buildRequestBody(taskCtx)
	if err != nil {
		log.Printf("[AI任务] ❌ 构建请求体失败: %v", err)
		result.Error = fmt.Sprintf("构建请求体失败: %v", err)
		return result
	}
	result.UsedModel, result.APIEndpoint = resolveExecutionMeta(taskCtx, requestBody)
	result.AttemptedModels = appendUniqueNonEmpty(result.AttemptedModels, result.UsedModel)
	result.AttemptedEndpoints = appendUniqueNonEmpty(result.AttemptedEndpoints, result.APIEndpoint)
	bodyJSON, _ := json.Marshal(requestBody)
	log.Printf("[AI任务] ✓ 请求体构建成功: %s", truncateString(string(bodyJSON), 500))

	// 2. 构建请求头
	log.Printf("[AI任务] 步骤2: 解析请求头...")
	headers, err := p.parseHeaders(taskCtx.APIConfig.Headers)
	if err != nil {
		log.Printf("[AI任务] ❌ 解析请求头失败: %v", err)
		result.Error = fmt.Sprintf("解析请求头失败: %v", err)
		return result
	}
	log.Printf("[AI任务] ✓ 请求头解析成功")

	// 2.1 处理 API Key
	if taskCtx.APIConfig.APIKey != "" && taskCtx.APIConfig.APIKeyLocation != "none" {
		log.Printf("[AI任务] 步骤2.1: 注入API Key (位置: %s)...", taskCtx.APIConfig.APIKeyLocation)
		switch taskCtx.APIConfig.APIKeyLocation {
		case "header_bearer":
			// Authorization: Bearer {API_KEY}
			headers["Authorization"] = "Bearer " + taskCtx.APIConfig.APIKey
			log.Printf("[AI任务] ✓ API Key已添加到Authorization Header (Bearer)")
		case "header_custom":
			// 自定义 Header 名称
			keyName := taskCtx.APIConfig.APIKeyName
			if keyName == "" {
				keyName = "X-API-Key"
			}
			headers[keyName] = taskCtx.APIConfig.APIKey
			log.Printf("[AI任务] ✓ API Key已添加到自定义Header: %s", keyName)
		case "query":
			// 将 API Key 添加到 URL 查询参数
			keyName := taskCtx.APIConfig.APIKeyName
			if keyName == "" {
				keyName = "api_key"
			}
			separator := "?"
			if strings.Contains(taskCtx.APIConfig.APIEndpoint, "?") {
				separator = "&"
			}
			taskCtx.APIConfig.APIEndpoint = taskCtx.APIConfig.APIEndpoint + separator + keyName + "=" + taskCtx.APIConfig.APIKey
			log.Printf("[AI任务] ✓ API Key已添加到URL查询参数: %s", keyName)
		case "body":
			// 将 API Key 添加到请求体
			keyName := taskCtx.APIConfig.APIKeyName
			if keyName == "" {
				keyName = "api_key"
			}
			requestBody[keyName] = taskCtx.APIConfig.APIKey
			log.Printf("[AI任务] ✓ API Key已添加到请求体: %s", keyName)
		}
	}

	// 3. 发起HTTP请求
	log.Printf("[AI任务] 步骤3: 调用外部API...")
	log.Printf("[AI任务] → 请求地址: %s", taskCtx.APIConfig.APIEndpoint)
	log.Printf("[AI任务] → 请求方法: %s", taskCtx.APIConfig.Method)

	httpReq := &HTTPRequest{
		URL:         taskCtx.APIConfig.APIEndpoint,
		Method:      taskCtx.APIConfig.Method,
		Headers:     headers,
		Body:        requestBody,
		Timeout:     resolveAIRequestTimeout(taskCtx),
		ContentType: "application/json",
	}

	generateCount := 1
	if taskCtx.TaskType == "ai_draw" {
		generateCount = getGenerateCountFromPayload(taskCtx.Payload)
	}

	if taskCtx.TaskType == "ai_draw" {
		if err := p.executeImageTaskWithFallbacks(taskCtx, result, generateCount); err != nil {
			result.Error = err.Error()
			return result
		}
	} else {
		if generateCount <= 1 {
			responseData, watermarkedURL, rawURL, err := p.executeSingleImageRequest(taskCtx, httpReq)
			if err != nil {
				result.Error = err.Error()
				return result
			}
			result.ResultData = responseData
			result.ResultURL = watermarkedURL
			result.ResultURLRaw = rawURL
			if watermarkedURL != "" {
				result.ResultURLs = []string{watermarkedURL}
			}
			if rawURL != "" {
				result.ResultURLsRaw = []string{rawURL}
			}
		} else {
			log.Printf("[AI任务] 步骤3-5: 开始批量生成 %d 张图片...", generateCount)
			requests := make([]*HTTPRequest, 0, generateCount)
			for i := 0; i < generateCount; i++ {
				requests = append(requests, cloneHTTPRequest(httpReq))
			}
			apiStartTime := time.Now()
			responses := DoHTTPRequestBatch(taskCtx.ctx, requests, generateCount)
			apiDuration := time.Since(apiStartTime)
			log.Printf("[AI任务] ✓ 批量API调用完成，请求数: %d，耗时: %v", len(responses), apiDuration)

			watermarkedURLs := make([]string, 0, generateCount)
			rawURLs := make([]string, 0, generateCount)
			responseList := make([]interface{}, 0, generateCount)

			for index, httpResp := range responses {
				if httpResp == nil {
					log.Printf("[AI任务] ⚠ 第 %d 张结果响应为空", index+1)
					continue
				}
				responseData, watermarkedURL, rawURL, err := p.processImageHTTPResponse(taskCtx, httpResp)
				if err != nil {
					log.Printf("[AI任务] ⚠ 第 %d 张结果处理失败: %v", index+1, err)
					continue
				}
				responseList = append(responseList, responseData)
				if watermarkedURL != "" {
					watermarkedURLs = append(watermarkedURLs, watermarkedURL)
				}
				if rawURL != "" {
					rawURLs = append(rawURLs, rawURL)
				}
			}

			if len(watermarkedURLs) == 0 {
				result.Error = "批量生成失败：未获取到任何图片结果"
				return result
			}

			result.ResultURLs = watermarkedURLs
			result.ResultURLsRaw = rawURLs
			result.ResultURL = watermarkedURLs[0]
			if len(rawURLs) > 0 {
				result.ResultURLRaw = rawURLs[0]
			}
			result.ResultData = map[string]interface{}{
				"responses":       responseList,
				"generated_count": len(watermarkedURLs),
				"expected_count":  generateCount,
			}
		}
	}

	result.Success = true
	result.Duration = time.Since(startTime)

	log.Printf("========================================")
	log.Printf("[AI任务] ✅ 任务执行完成!")
	log.Printf("[AI任务] 任务编号: %s", taskCtx.TaskNo)
	log.Printf("[AI任务] 执行结果: 成功")
	log.Printf("[AI任务] 总耗时: %v", result.Duration)
	log.Printf("[AI任务] 结果URL: %s", result.ResultURL)
	log.Printf("========================================")

	return result
}

func getGenerateCountFromPayload(payload map[string]interface{}) int {
	value, ok := payload["generate_count"]
	if !ok {
		return 1
	}
	switch v := value.(type) {
	case float64:
		count := int(v)
		if count < 1 {
			return 1
		}
		if count > 3 {
			return 3
		}
		return count
	case int:
		if v < 1 {
			return 1
		}
		if v > 3 {
			return 3
		}
		return v
	default:
		return 1
	}
}

func (p *RequestPool) executeSingleImageRequest(taskCtx *AITaskContext, httpReq *HTTPRequest) (map[string]interface{}, string, string, error) {
	apiStartTime := time.Now()
	httpResp := DoHTTPRequest(taskCtx.ctx, httpReq)
	apiDuration := time.Since(apiStartTime)
	if httpResp.Error != nil {
		log.Printf("[AI任务] ❌ API调用失败: %v (耗时: %v)", httpResp.Error, apiDuration)
		return nil, "", "", fmt.Errorf("HTTP请求失败: %v", httpResp.Error)
	}
	log.Printf("[AI任务] ✓ API调用完成，状态码: %d，耗时: %v", httpResp.StatusCode, apiDuration)
	return p.processImageHTTPResponse(taskCtx, httpResp)
}

func resolveAIRequestTimeout(taskCtx *AITaskContext) time.Duration {
	if taskCtx != nil {
		if taskCtx.AttemptIndex == 0 {
			return primaryAIRequestTimeout
		}
		if taskCtx.AttemptIndex == 1 {
			return firstFallbackRequestTimeout
		}
		if taskCtx.AttemptIndex == 2 {
			return secondFallbackRequestTimeout
		}
	}
	if taskCtx != nil && isSeedreamAPIConfig(taskCtx.APIConfig) {
		return seedreamAIRequestTimeout
	}
	return defaultAIRequestTimeout
}

func (p *RequestPool) processImageHTTPResponse(taskCtx *AITaskContext, httpResp *HTTPResponse) (map[string]interface{}, string, string, error) {
	if httpResp.StatusCode >= 400 {
		log.Printf("[AI任务] ❌ API返回错误状态码: %d", httpResp.StatusCode)
		log.Printf("[AI任务] → 响应内容: %s", truncateString(string(httpResp.Body), 500))
		return nil, "", "", fmt.Errorf("HTTP请求返回错误状态码: %d, Body: %s", httpResp.StatusCode, string(httpResp.Body))
	}

	log.Printf("[AI任务] 步骤4: 解析API响应...")
	var responseData map[string]interface{}
	if err := json.Unmarshal(httpResp.Body, &responseData); err != nil {
		log.Printf("[AI任务] ⚠ 响应非JSON格式，作为二进制数据处理，大小: %d bytes", len(httpResp.Body))
		responseData = map[string]interface{}{
			"raw_data": true,
		}
	} else {
		log.Printf("[AI任务] ✓ 响应JSON解析成功: %s", truncateString(string(httpResp.Body), 300))
	}

	log.Printf("[AI任务] 步骤5: 保存结果到OSS（原图+水印图）...")
	watermarkedURL, rawURL, err := p.saveResultToOSS(taskCtx, httpResp.Body, responseData)
	if err != nil {
		log.Printf("[AI任务] ⚠ 保存到OSS失败: %v", err)
		if taskCtx.TaskType == "ai_draw" {
			return nil, "", "", fmt.Errorf("保存AI绘画结果失败: %v", err)
		}
		return responseData, "", "", nil
	}
	log.Printf("[AI任务] ✓ 带水印图: %s", watermarkedURL)
	log.Printf("[AI任务] ✓ 原图: %s", rawURL)
	return responseData, watermarkedURL, rawURL, nil
}

func canUseNativeBatchImageRequest(taskCtx *AITaskContext, generateCount int) bool {
	return taskCtx != nil && taskCtx.APIConfig != nil && generateCount > 1 && isSeedreamAPIConfig(taskCtx.APIConfig)
}

func (p *RequestPool) executeNativeBatchImageRequest(taskCtx *AITaskContext, httpReq *HTTPRequest, generateCount int) (map[string]interface{}, []string, []string, error) {
	apiStartTime := time.Now()
	httpResp := DoHTTPRequest(taskCtx.ctx, httpReq)
	apiDuration := time.Since(apiStartTime)
	if httpResp.Error != nil {
		log.Printf("[AI任务] ❌ 原生批量 API 调用失败: %v (耗时: %v)", httpResp.Error, apiDuration)
		return nil, nil, nil, fmt.Errorf("HTTP请求失败: %v", httpResp.Error)
	}
	log.Printf("[AI任务] ✓ 原生批量 API 调用完成，状态码: %d，耗时: %v，期望张数: %d", httpResp.StatusCode, apiDuration, generateCount)
	return p.processImageBatchHTTPResponse(taskCtx, httpResp, generateCount)
}

func (p *RequestPool) processImageBatchHTTPResponse(taskCtx *AITaskContext, httpResp *HTTPResponse, generateCount int) (map[string]interface{}, []string, []string, error) {
	if httpResp.StatusCode >= 400 {
		log.Printf("[AI任务] ❌ 原生批量 API 返回错误状态码: %d", httpResp.StatusCode)
		log.Printf("[AI任务] → 响应内容: %s", truncateString(string(httpResp.Body), 500))
		return nil, nil, nil, fmt.Errorf("HTTP请求返回错误状态码: %d, Body: %s", httpResp.StatusCode, string(httpResp.Body))
	}

	var responseData map[string]interface{}
	if err := json.Unmarshal(httpResp.Body, &responseData); err != nil {
		return nil, nil, nil, fmt.Errorf("解析原生批量响应失败: %v", err)
	}

	dataList, ok := responseData["data"].([]interface{})
	if !ok || len(dataList) == 0 {
		log.Printf("[AI任务] ⚠ 原生批量响应未返回 data 数组，回退按单图结果解析: expected_count=%d", generateCount)
		singleResponseData, watermarkedURL, rawURL, err := p.processImageHTTPResponse(taskCtx, httpResp)
		if err != nil {
			return nil, nil, nil, err
		}

		watermarkedURLs := make([]string, 0, 1)
		rawURLs := make([]string, 0, 1)
		if watermarkedURL != "" {
			watermarkedURLs = append(watermarkedURLs, watermarkedURL)
		}
		if rawURL != "" {
			rawURLs = append(rawURLs, rawURL)
		}
		log.Printf("[AI任务] 原生批量结果汇总: expected_count=%d, upstream_count=%d, saved_count=%d", generateCount, len(watermarkedURLs), len(watermarkedURLs))
		return singleResponseData, watermarkedURLs, rawURLs, nil
	}
	log.Printf("[AI任务] 原生批量响应解析成功: expected_count=%d, upstream_count=%d", generateCount, len(dataList))

	responseList := make([]interface{}, 0, len(dataList))
	watermarkedURLs := make([]string, 0, len(dataList))
	rawURLs := make([]string, 0, len(dataList))
	thumbnailURLs := make([]string, 0, len(dataList))

	for index, item := range dataList {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			log.Printf("[AI任务] ⚠ 原生批量结果[%d]格式异常，已跳过", index+1)
			continue
		}

		singleResponseData := map[string]interface{}{
			"data": []interface{}{itemMap},
		}
		watermarkedURL, rawURL, err := p.saveResultToOSS(taskCtx, httpResp.Body, singleResponseData)
		if err != nil {
			log.Printf("[AI任务] ⚠ 原生批量结果[%d]保存失败: %v", index+1, err)
			continue
		}

		responseList = append(responseList, itemMap)
		if watermarkedURL != "" {
			watermarkedURLs = append(watermarkedURLs, watermarkedURL)
		}
		if rawURL != "" {
			rawURLs = append(rawURLs, rawURL)
		}
		if thumbnailURL := extractThumbnailURLFromResponse(singleResponseData); thumbnailURL != "" {
			thumbnailURLs = append(thumbnailURLs, thumbnailURL)
		}
	}

	if len(watermarkedURLs) == 0 {
		return nil, nil, nil, fmt.Errorf("原生批量生成失败：未获取到任何图片结果")
	}
	log.Printf("[AI任务] 原生批量结果汇总: expected_count=%d, upstream_count=%d, saved_count=%d", generateCount, len(dataList), len(watermarkedURLs))

	resultData := map[string]interface{}{
		"responses":       responseList,
		"generated_count": len(watermarkedURLs),
		"expected_count":  generateCount,
	}
	if len(thumbnailURLs) > 0 {
		resultData["thumbnail_url"] = thumbnailURLs[0]
		resultData["thumbnail_urls"] = thumbnailURLs
	}
	return resultData, watermarkedURLs, rawURLs, nil
}

func isGeminiGenerateContentConfig(apiConfig *AIAPIConfigData) bool {
	if apiConfig == nil {
		return false
	}
	endpoint := strings.ToLower(strings.TrimSpace(apiConfig.APIEndpoint))
	return strings.Contains(endpoint, "gemini") && strings.Contains(endpoint, ":generatecontent")
}

func isToAPIsAsyncConfig(apiConfig *AIAPIConfigData) bool {
	if apiConfig == nil {
		return false
	}
	protocolType := strings.ToLower(strings.TrimSpace(apiConfig.ProtocolType))
	providerCode := strings.ToLower(strings.TrimSpace(apiConfig.ProviderCode))
	endpoint := strings.ToLower(strings.TrimSpace(apiConfig.APIEndpoint))
	return protocolType == "toapis_async" ||
		providerCode == "toapis" ||
		(strings.Contains(endpoint, "toapis.com") && strings.Contains(endpoint, "/v1/images/generations"))
}

func isRunningHubAsyncConfig(apiConfig *AIAPIConfigData) bool {
	if apiConfig == nil {
		return false
	}
	protocolType := strings.ToLower(strings.TrimSpace(apiConfig.ProtocolType))
	providerCode := strings.ToLower(strings.TrimSpace(apiConfig.ProviderCode))
	endpoint := strings.ToLower(strings.TrimSpace(apiConfig.APIEndpoint))
	return protocolType == "runninghub_async" ||
		providerCode == "runninghub" ||
		strings.Contains(endpoint, "runninghub.cn/openapi/v2/run/ai-app")
}

func isToAPIsGPTImage2Config(apiConfig *AIAPIConfigData) bool {
	if !isToAPIsAsyncConfig(apiConfig) {
		return false
	}
	return strings.Contains(strings.ToLower(resolveConfiguredModel(apiConfig)), "gpt-image-2")
}

func isGPTImage2VipConfig(apiConfig *AIAPIConfigData) bool {
	if apiConfig == nil {
		return false
	}
	if isToAPIsAsyncConfig(apiConfig) {
		return false
	}
	providerCode := strings.ToLower(strings.TrimSpace(apiConfig.ProviderCode))
	endpoint := strings.ToLower(strings.TrimSpace(apiConfig.APIEndpoint))
	model := strings.ToLower(resolveConfiguredModel(apiConfig))
	return providerCode == "gpt-image-2-vip" ||
		(strings.Contains(endpoint, "api.laozhang.ai") && strings.Contains(model, "gpt-image-2-vip"))
}

func getOrCreateChildMap(parent map[string]interface{}, key string) map[string]interface{} {
	if parent == nil {
		return nil
	}
	if existing, ok := parent[key].(map[string]interface{}); ok {
		return existing
	}
	child := make(map[string]interface{})
	parent[key] = child
	return child
}

func stripGeminiDrawTopLevelFields(body map[string]interface{}) {
	if body == nil {
		return
	}
	delete(body, "service_type")
	delete(body, "service")
	delete(body, "quality")
	delete(body, "canvas")
	delete(body, "generate_count")
	delete(body, "scene_direction")
	delete(body, "style")
	delete(body, "aspect_ratio")
	delete(body, "image_size")
	delete(body, "resolution")
	delete(body, "size")
	delete(body, "original_image_urls")
	delete(body, "reference_image_urls")
	delete(body, "ordered_image_urls")
}

func stripToAPIsDrawTopLevelFields(body map[string]interface{}) {
	if body == nil {
		return
	}
	delete(body, "service_type")
	delete(body, "service")
	delete(body, "quality")
	delete(body, "canvas")
	delete(body, "generate_count")
	delete(body, "scene_direction")
	delete(body, "style")
	delete(body, "original_image_urls")
	delete(body, "reference_image_urls")
	delete(body, "ordered_image_urls")
	delete(body, "image")
	delete(body, "image_url")
	delete(body, "images")
	delete(body, "aspect_ratio")
	delete(body, "image_size")
	if imageItems, ok := body["image_urls"].([]interface{}); ok {
		filtered := make([]interface{}, 0, len(imageItems))
		for _, item := range imageItems {
			itemMap, ok := item.(map[string]interface{})
			if ok {
				imageURL, _ := itemMap["url"].(string)
				imageURL = strings.TrimSpace(imageURL)
				if imageURL == "" || imageURL == "{{image_url}}" {
					continue
				}
				filtered = append(filtered, itemMap)
				continue
			}
			imageURL, _ := item.(string)
			imageURL = strings.TrimSpace(imageURL)
			if imageURL == "" || imageURL == "{{image_url}}" {
				continue
			}
			filtered = append(filtered, imageURL)
		}
		if len(filtered) == 0 {
			delete(body, "image_urls")
		} else {
			body["image_urls"] = filtered
		}
	}
	if referenceImages, ok := body["reference_images"].([]interface{}); ok {
		filtered := make([]interface{}, 0, len(referenceImages))
		for _, item := range referenceImages {
			imageURL, _ := item.(string)
			imageURL = strings.TrimSpace(imageURL)
			if imageURL == "" || imageURL == "{{image_url}}" {
				continue
			}
			filtered = append(filtered, imageURL)
		}
		if len(filtered) == 0 {
			delete(body, "reference_images")
		} else {
			body["reference_images"] = filtered
		}
	}
}

func stripGPTImage2VipDrawTopLevelFields(body map[string]interface{}) {
	if body == nil {
		return
	}
	delete(body, "service_type")
	delete(body, "service")
	delete(body, "quality")
	delete(body, "canvas")
	delete(body, "generate_count")
	delete(body, "scene_direction")
	delete(body, "style")
	delete(body, "aspect_ratio")
	delete(body, "image_size")
	delete(body, "resolution")
	delete(body, "original_image_urls")
	delete(body, "reference_image_urls")
	delete(body, "ordered_image_urls")
	delete(body, "image_url")
	delete(body, "images")
	delete(body, "image_urls")
	delete(body, "reference_images")
}

func shouldMergePayloadFieldToRequestBody(taskCtx *AITaskContext, key string) bool {
	switch key {
	case "prompt", "image", "image_url", "images", "image_urls", "reference_images", "original_image_urls", "reference_image_urls", "ordered_image_urls":
		return false
	}
	if taskCtx != nil && taskCtx.TaskType == "ai_draw" && isGeminiGenerateContentConfig(taskCtx.APIConfig) {
		return false
	}
	if taskCtx != nil && taskCtx.TaskType == "ai_draw" && isToAPIsAsyncConfig(taskCtx.APIConfig) {
		return false
	}
	if taskCtx != nil && taskCtx.TaskType == "ai_draw" && isGPTImage2VipConfig(taskCtx.APIConfig) {
		return false
	}
	if taskCtx != nil && taskCtx.TaskType == "ai_draw" && isRunningHubAsyncConfig(taskCtx.APIConfig) {
		return false
	}
	return true
}

func cloneAIAPIConfigData(configData *AIAPIConfigData) *AIAPIConfigData {
	if configData == nil {
		return nil
	}
	cloned := *configData
	return &cloned
}

func appendUniqueNonEmpty(target []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return target
	}
	for _, item := range target {
		if strings.TrimSpace(item) == trimmed {
			return target
		}
	}
	return append(target, trimmed)
}

func extractModelFromValue(value interface{}) string {
	switch current := value.(type) {
	case map[string]interface{}:
		if rawModel, ok := current["model"]; ok {
			if modelText, ok := rawModel.(string); ok && strings.TrimSpace(modelText) != "" {
				return strings.TrimSpace(modelText)
			}
		}
		for _, child := range current {
			if modelText := extractModelFromValue(child); modelText != "" {
				return modelText
			}
		}
	case []interface{}:
		for _, child := range current {
			if modelText := extractModelFromValue(child); modelText != "" {
				return modelText
			}
		}
	}
	return ""
}

func extractModelFromEndpoint(endpoint string) string {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if index := strings.Index(lower, "/models/"); index >= 0 {
		segment := raw[index+len("/models/"):]
		for idx, char := range segment {
			if char == ':' || char == '/' || char == '?' {
				segment = segment[:idx]
				break
			}
		}
		return strings.TrimSpace(segment)
	}
	marker := "seedream-"
	if index := strings.Index(lower, marker); index >= 0 {
		segment := raw[index:]
		for idx, char := range segment {
			if !(char == '-' || char == '_' || char == '.' || (char >= '0' && char <= '9') || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')) {
				segment = segment[:idx]
				break
			}
		}
		return strings.TrimSpace(segment)
	}
	return ""
}

func resolveExecutionMeta(taskCtx *AITaskContext, requestBody map[string]interface{}) (string, string) {
	apiEndpoint := ""
	if taskCtx != nil && taskCtx.APIConfig != nil {
		apiEndpoint = strings.TrimSpace(taskCtx.APIConfig.APIEndpoint)
	}
	modelText := extractModelFromValue(requestBody)
	if modelText == "" && taskCtx != nil {
		modelText = extractModelFromValue(taskCtx.Payload)
	}
	if modelText == "" {
		modelText = extractModelFromEndpoint(apiEndpoint)
	}
	return strings.TrimSpace(modelText), apiEndpoint
}

func resolveConfiguredModel(configData *AIAPIConfigData) string {
	if configData == nil {
		return ""
	}
	bodyTemplate := strings.TrimSpace(configData.BodyTemplate)
	if bodyTemplate != "" {
		var body map[string]interface{}
		if err := json.Unmarshal([]byte(bodyTemplate), &body); err == nil {
			if modelText := extractModelFromValue(body); modelText != "" {
				return strings.TrimSpace(modelText)
			}
		}
	}
	return extractModelFromEndpoint(configData.APIEndpoint)
}

func isSeedreamAPIConfig(configData *AIAPIConfigData) bool {
	if configData == nil {
		return false
	}
	bodyTemplate := strings.ToLower(configData.BodyTemplate)
	return strings.Contains(bodyTemplate, "seedream-")
}

func getAIDrawAttemptRouteLabel(index int) string {
	if index <= 0 {
		return "主接口"
	}
	return fmt.Sprintf("备用接口%d", index)
}

func countReferenceImages(taskCtx *AITaskContext) int {
	if taskCtx == nil {
		return 0
	}
	if len(taskCtx.ImageURLs) > 0 {
		count := 0
		for _, imageURL := range taskCtx.ImageURLs {
			if strings.TrimSpace(imageURL) != "" {
				count++
			}
		}
		return count
	}
	if strings.TrimSpace(taskCtx.ImageURL) != "" {
		return 1
	}
	return 0
}

func formatLogList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

func summarizeGeminiRequestBody(body map[string]interface{}) (int, int, bool, []string, interface{}, interface{}, []string) {
	if body == nil {
		return 0, 0, false, nil, nil, nil, nil
	}
	partsCount := 0
	inlineImageCount := 0
	hasImagePlaceholder := false
	imageMimeTypes := make([]string, 0)
	contents, ok := body["contents"].([]interface{})
	if ok && len(contents) > 0 {
		if firstContent, ok := contents[0].(map[string]interface{}); ok {
			if parts, ok := firstContent["parts"].([]interface{}); ok {
				partsCount = len(parts)
				for _, part := range parts {
					partMap, ok := part.(map[string]interface{})
					if !ok {
						continue
					}
					inlineData, ok := partMap["inline_data"].(map[string]interface{})
					if !ok {
						inlineData, _ = partMap["inlineData"].(map[string]interface{})
					}
					if inlineData == nil {
						continue
					}
					inlineImageCount++
					if data, _ := inlineData["data"].(string); strings.TrimSpace(data) == "{{image}}" {
						hasImagePlaceholder = true
					}
					if mimeType, _ := inlineData["mime_type"].(string); strings.TrimSpace(mimeType) != "" {
						imageMimeTypes = append(imageMimeTypes, strings.TrimSpace(mimeType))
					} else if mimeType, _ := inlineData["mimeType"].(string); strings.TrimSpace(mimeType) != "" {
						imageMimeTypes = append(imageMimeTypes, strings.TrimSpace(mimeType))
					}
				}
			}
		}
	}
	var aspectRatio interface{}
	var imageSize interface{}
	if generationConfig, ok := body["generationConfig"].(map[string]interface{}); ok {
		if imageConfig, ok := generationConfig["imageConfig"].(map[string]interface{}); ok {
			aspectRatio = imageConfig["aspectRatio"]
			imageSize = imageConfig["imageSize"]
		}
	}
	unexpectedTopLevelFields := make([]string, 0)
	for _, key := range []string{"service_type", "service", "quality", "canvas", "generate_count", "scene_direction", "style", "aspect_ratio", "image_size", "resolution", "size"} {
		if _, exists := body[key]; exists {
			unexpectedTopLevelFields = append(unexpectedTopLevelFields, key)
		}
	}
	return partsCount, inlineImageCount, hasImagePlaceholder, imageMimeTypes, aspectRatio, imageSize, unexpectedTopLevelFields
}

func (p *RequestPool) executeImageTaskWithFallbacks(taskCtx *AITaskContext, result *AITaskResult, generateCount int) error {
	configs := make([]*AIAPIConfigData, 0, 1+len(taskCtx.FallbackConfigs))
	if taskCtx.APIConfig != nil {
		configs = append(configs, taskCtx.APIConfig)
	}
	configs = append(configs, taskCtx.FallbackConfigs...)
	log.Printf("[AI任务] 绘图接口链路: requested_generate_count=%d, candidate_configs=%d", generateCount, len(configs))

	attemptErrors := make([]string, 0, len(configs))
	for index, configData := range configs {
		if configData == nil {
			continue
		}

		attemptCtx := *taskCtx
		attemptCtx.APIConfig = cloneAIAPIConfigData(configData)
		attemptCtx.AttemptIndex = index
		configuredModel := resolveConfiguredModel(attemptCtx.APIConfig)
		routeLabel := getAIDrawAttemptRouteLabel(index)
		log.Printf("[AI任务] 开始尝试%s %d/%d: model=%s, endpoint=%s, requested_generate_count=%d", routeLabel, index+1, len(configs), configuredModel, attemptCtx.APIConfig.APIEndpoint, generateCount)

		responseData, watermarkedURLs, rawURLs, usedModel, usedEndpoint, err := p.executeImageTaskAttempt(&attemptCtx, generateCount)
		result.AttemptedModels = appendUniqueNonEmpty(result.AttemptedModels, usedModel)
		result.AttemptedEndpoints = appendUniqueNonEmpty(result.AttemptedEndpoints, usedEndpoint)
		if strings.TrimSpace(usedModel) != "" {
			result.UsedModel = strings.TrimSpace(usedModel)
		}
		if strings.TrimSpace(usedEndpoint) != "" {
			result.APIEndpoint = strings.TrimSpace(usedEndpoint)
		}
		if attemptCtx.APIConfig != nil {
			result.ProviderCode = strings.TrimSpace(attemptCtx.APIConfig.ProviderCode)
			result.ProviderName = strings.TrimSpace(attemptCtx.APIConfig.ProviderName)
			result.ProtocolType = strings.TrimSpace(attemptCtx.APIConfig.ProtocolType)
		}
		if err == nil {
			result.ResultData = responseData
			result.ResultURLs = watermarkedURLs
			result.ResultURLsRaw = rawURLs
			result.ThumbnailURLs = extractThumbnailURLsFromResponse(responseData)
			if len(watermarkedURLs) > 0 {
				result.ResultURL = watermarkedURLs[0]
			}
			if len(rawURLs) > 0 {
				result.ResultURLRaw = rawURLs[0]
			}
			if len(result.ThumbnailURLs) > 0 {
				result.ThumbnailURL = result.ThumbnailURLs[0]
			}
			if responseData != nil {
				if taskID, _ := responseData["external_task_id"].(string); strings.TrimSpace(taskID) != "" {
					result.ExternalTaskID = strings.TrimSpace(taskID)
				}
				if status, _ := responseData["external_status"].(string); strings.TrimSpace(status) != "" {
					result.ExternalStatus = strings.TrimSpace(status)
				}
			}
			log.Printf("[AI任务] ✓ 当前命中%s: model=%s, endpoint=%s, requested_generate_count=%d, generated_count=%d", routeLabel, usedModel, usedEndpoint, generateCount, len(watermarkedURLs))
			return nil
		}

		attemptErrors = append(attemptErrors, fmt.Sprintf("接口%d(%s): %v", index+1, usedEndpoint, err))
		log.Printf("[AI任务] ⚠ %s失败: model=%s, endpoint=%s, requested_generate_count=%d, error=%v", routeLabel, usedModel, usedEndpoint, generateCount, err)
		log.Printf("[AI任务] ⚠ 继续尝试下一个备用接口")
	}

	if len(attemptErrors) == 0 {
		return fmt.Errorf("未找到可用的图像生成接口配置")
	}

	return fmt.Errorf("全部图像生成接口均失败: %s", strings.Join(attemptErrors, " | "))
}

func (p *RequestPool) executeImageTaskAttempt(taskCtx *AITaskContext, generateCount int) (map[string]interface{}, []string, []string, string, string, error) {
	requestBody, err := p.buildRequestBody(taskCtx)
	if err != nil {
		return nil, nil, nil, "", "", fmt.Errorf("构建请求体失败: %v", err)
	}
	usedModel, usedEndpoint := resolveExecutionMeta(taskCtx, requestBody)
	hasReferenceImages := taskCtx != nil && (strings.TrimSpace(taskCtx.ImageURL) != "" || len(taskCtx.ImageURLs) > 0)
	referenceImageCount := countReferenceImages(taskCtx)
	log.Printf("[AI任务] 绘图请求详情: model=%s, endpoint=%s, requested_generate_count=%d, has_reference_images=%t", usedModel, usedEndpoint, generateCount, hasReferenceImages)
	if isGeminiGenerateContentConfig(taskCtx.APIConfig) {
		partsCount, inlineImageCount, hasImagePlaceholder, imageMimeTypes, aspectRatio, imageSize, unexpectedTopLevelFields := summarizeGeminiRequestBody(requestBody)
		log.Printf("[AI任务] Gemini请求结构检查: model=%s, parts_count=%d, inline_image_count=%d, placeholder_image=%t, image_mime_types=%s, image_config.aspect_ratio=%v, image_config.image_size=%v, unexpected_top_level_fields=%s, reference_image_count=%d", usedModel, partsCount, inlineImageCount, hasImagePlaceholder, formatLogList(imageMimeTypes), aspectRatio, imageSize, formatLogList(unexpectedTopLevelFields), referenceImageCount)
	} else {
		log.Printf("[AI任务] 绘图分辨率参数: model=%s, aspect_ratio=%v, image_size=%v, resolution=%v, size=%v", usedModel, requestBody["aspect_ratio"], requestBody["image_size"], requestBody["resolution"], requestBody["size"])
		log.Printf("[AI任务] 最终请求摘要: model=%s, endpoint=%s, final_resolution=%v, final_size_field=%v, aspect_ratio=%v, reference_image_count=%d", usedModel, usedEndpoint, requestBody["image_size"], requestBody["size"], requestBody["aspect_ratio"], referenceImageCount)
	}
	if isSeedreamAPIConfig(taskCtx.APIConfig) && generateCount > 1 {
		requestBody["sequential_image_generation"] = "auto"
		requestBody["sequential_image_generation_options"] = map[string]interface{}{
			"max_images": generateCount,
		}
		log.Printf("[AI任务] 当前模型支持原生批量出图: model=%s, max_images=%d", usedModel, generateCount)
	}

	headers, err := p.parseHeaders(taskCtx.APIConfig.Headers)
	if err != nil {
		return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("解析请求头失败: %v", err)
	}

	if taskCtx.APIConfig.APIKey != "" && taskCtx.APIConfig.APIKeyLocation != "none" {
		switch taskCtx.APIConfig.APIKeyLocation {
		case "header_bearer":
			headers["Authorization"] = "Bearer " + taskCtx.APIConfig.APIKey
		case "header_custom":
			keyName := taskCtx.APIConfig.APIKeyName
			if keyName == "" {
				keyName = "X-API-Key"
			}
			headers[keyName] = taskCtx.APIConfig.APIKey
		case "query":
			keyName := taskCtx.APIConfig.APIKeyName
			if keyName == "" {
				keyName = "api_key"
			}
			separator := "?"
			if strings.Contains(taskCtx.APIConfig.APIEndpoint, "?") {
				separator = "&"
			}
			taskCtx.APIConfig.APIEndpoint = taskCtx.APIConfig.APIEndpoint + separator + keyName + "=" + taskCtx.APIConfig.APIKey
		case "body":
			keyName := taskCtx.APIConfig.APIKeyName
			if keyName == "" {
				keyName = "api_key"
			}
			requestBody[keyName] = taskCtx.APIConfig.APIKey
		}
	}

	httpReq := &HTTPRequest{
		URL:         taskCtx.APIConfig.APIEndpoint,
		Method:      taskCtx.APIConfig.Method,
		Headers:     headers,
		Body:        requestBody,
		Timeout:     resolveAIRequestTimeout(taskCtx),
		ContentType: "application/json",
	}

	if isToAPIsAsyncConfig(taskCtx.APIConfig) {
		log.Printf("[AI任务] 当前走 ToAPIs 异步生图模式: model=%s, requested_generate_count=%d", usedModel, generateCount)
		return p.executeToAPIsAsyncImageTasks(taskCtx, httpReq, generateCount, usedModel, usedEndpoint)
	}
	if isRunningHubAsyncConfig(taskCtx.APIConfig) {
		log.Printf("[AI任务] 当前走 RunningHub 异步生图模式: requested_generate_count=%d", generateCount)
		return p.executeRunningHubAsyncImageTask(taskCtx, httpReq, generateCount, usedModel, usedEndpoint)
	}

	if isSeedreamAPIConfig(taskCtx.APIConfig) && generateCount > 1 {
		if canUseNativeBatchImageRequest(taskCtx, generateCount) {
			log.Printf("[AI任务] 将使用原生批量出图模式: model=%s, requested_generate_count=%d", usedModel, generateCount)
			responseData, watermarkedURLs, rawURLs, err := p.executeNativeBatchImageRequest(taskCtx, httpReq, generateCount)
			if err != nil {
				return nil, nil, nil, usedModel, usedEndpoint, err
			}
			log.Printf("[AI任务] 原生批量出图完成: model=%s, requested_generate_count=%d, generated_count=%d", usedModel, generateCount, len(watermarkedURLs))
			return responseData, watermarkedURLs, rawURLs, usedModel, usedEndpoint, nil
		}
	}

	if generateCount <= 1 {
		log.Printf("[AI任务] 当前走单次出图模式: model=%s", usedModel)
		responseData, watermarkedURL, rawURL, err := p.executeSingleImageRequest(taskCtx, httpReq)
		if err != nil {
			return nil, nil, nil, usedModel, usedEndpoint, err
		}

		watermarkedURLs := make([]string, 0, 1)
		rawURLs := make([]string, 0, 1)
		if watermarkedURL != "" {
			watermarkedURLs = append(watermarkedURLs, watermarkedURL)
		}
		if rawURL != "" {
			rawURLs = append(rawURLs, rawURL)
		}
		log.Printf("[AI任务] 单次出图完成: model=%s, generated_count=%d", usedModel, len(watermarkedURLs))
		return responseData, watermarkedURLs, rawURLs, usedModel, usedEndpoint, nil
	}

	log.Printf("[AI任务] 当前走并发抽卡模式: model=%s, requested_generate_count=%d", usedModel, generateCount)
	requests := make([]*HTTPRequest, 0, generateCount)
	for i := 0; i < generateCount; i++ {
		requests = append(requests, cloneHTTPRequest(httpReq))
	}

	responses := DoHTTPRequestBatch(taskCtx.ctx, requests, generateCount)
	watermarkedURLs := make([]string, 0, generateCount)
	rawURLs := make([]string, 0, generateCount)
	thumbnailURLs := make([]string, 0, generateCount)
	responseList := make([]interface{}, 0, generateCount)

	for index, httpResp := range responses {
		if httpResp == nil {
			log.Printf("[AI任务] ⚠ 第 %d 张结果响应为空", index+1)
			continue
		}
		if httpResp.Error != nil {
			log.Printf("[AI任务] ⚠ 第 %d 张HTTP请求失败: %v", index+1, httpResp.Error)
			continue
		}
		log.Printf("[AI任务] 第 %d 张响应返回: status=%d, duration=%v", index+1, httpResp.StatusCode, httpResp.Duration)

		responseData, watermarkedURL, rawURL, err := p.processImageHTTPResponse(taskCtx, httpResp)
		if err != nil {
			log.Printf("[AI任务] ⚠ 第 %d 张结果处理失败: %v", index+1, err)
			continue
		}

		responseList = append(responseList, responseData)
		if watermarkedURL != "" {
			watermarkedURLs = append(watermarkedURLs, watermarkedURL)
		}
		if rawURL != "" {
			rawURLs = append(rawURLs, rawURL)
		}
		if thumbnailURL := extractThumbnailURLFromResponse(responseData); thumbnailURL != "" {
			thumbnailURLs = append(thumbnailURLs, thumbnailURL)
		}
		log.Printf("[AI任务] ✓ 第 %d 张生成成功: watermarked_url=%s", index+1, watermarkedURL)
	}

	if len(watermarkedURLs) == 0 {
		return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("批量生成失败：未获取到任何图片结果")
	}
	log.Printf("[AI任务] 并发抽卡结果汇总: model=%s, requested_generate_count=%d, success_count=%d, failed_count=%d", usedModel, generateCount, len(watermarkedURLs), generateCount-len(watermarkedURLs))

	resultData := map[string]interface{}{
		"responses":       responseList,
		"generated_count": len(watermarkedURLs),
		"expected_count":  generateCount,
	}
	if len(thumbnailURLs) > 0 {
		resultData["thumbnail_url"] = thumbnailURLs[0]
		resultData["thumbnail_urls"] = thumbnailURLs
	}
	return resultData, watermarkedURLs, rawURLs, usedModel, usedEndpoint, nil
}

func (p *RequestPool) executeToAPIsAsyncImageTasks(taskCtx *AITaskContext, httpReq *HTTPRequest, generateCount int, usedModel string, usedEndpoint string) (map[string]interface{}, []string, []string, string, string, error) {
	if generateCount < 1 {
		generateCount = 1
	}
	watermarkedURLs := make([]string, 0, generateCount)
	rawURLs := make([]string, 0, generateCount)
	responses := make([]interface{}, 0, generateCount)
	externalTaskIDs := make([]string, 0, generateCount)
	externalStatuses := make([]string, 0, generateCount)
	attemptErrors := make([]string, 0)
	for index := 0; index < generateCount; index++ {
		singleReq := cloneHTTPRequest(httpReq)
		if body, ok := cloneJSONMap(httpReq.Body); ok {
			body["n"] = 1
			singleReq.Body = body
		}
		responseData, watermarkedURL, rawURL, err := p.executeToAPIsAsyncImageTask(taskCtx, singleReq, index+1, generateCount)
		if err != nil {
			attemptErrors = append(attemptErrors, fmt.Sprintf("第%d张: %v", index+1, err))
			log.Printf("[AI任务] ⚠️ ToAPIs 第 %d/%d 张生成失败: %v", index+1, generateCount, err)
			continue
		}
		responses = append(responses, responseData)
		if watermarkedURL != "" {
			watermarkedURLs = append(watermarkedURLs, watermarkedURL)
		}
		if rawURL != "" {
			rawURLs = append(rawURLs, rawURL)
		}
		if taskID, _ := responseData["external_task_id"].(string); strings.TrimSpace(taskID) != "" {
			externalTaskIDs = append(externalTaskIDs, strings.TrimSpace(taskID))
		}
		if status, _ := responseData["external_status"].(string); strings.TrimSpace(status) != "" {
			externalStatuses = append(externalStatuses, strings.TrimSpace(status))
		}
	}
	if len(watermarkedURLs) == 0 {
		if len(attemptErrors) > 0 {
			return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("ToAPIs 未获取到任何图片结果: %s", strings.Join(attemptErrors, " | "))
		}
		return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("ToAPIs 未获取到任何图片结果")
	}
	log.Printf("[AI任务] ToAPIs 批量结果汇总: model=%s, requested_generate_count=%d, success_count=%d, failed_count=%d", usedModel, generateCount, len(watermarkedURLs), len(attemptErrors))
	resultData := map[string]interface{}{
		"provider":          "toapis",
		"protocol_type":     "toapis_async",
		"responses":         responses,
		"generated_count":   len(watermarkedURLs),
		"expected_count":    generateCount,
		"failed_count":      len(attemptErrors),
		"external_task_ids": externalTaskIDs,
		"external_statuses": externalStatuses,
	}
	if len(attemptErrors) > 0 {
		resultData["errors"] = attemptErrors
	}
	if len(externalTaskIDs) > 0 {
		resultData["external_task_id"] = externalTaskIDs[0]
	}
	if len(externalStatuses) > 0 {
		resultData["external_status"] = externalStatuses[len(externalStatuses)-1]
	}
	return resultData, watermarkedURLs, rawURLs, usedModel, usedEndpoint, nil
}

func (p *RequestPool) executeToAPIsAsyncImageTask(taskCtx *AITaskContext, httpReq *HTTPRequest, current int, total int) (map[string]interface{}, string, string, error) {
	createStart := time.Now()
	createResp := DoHTTPRequest(taskCtx.ctx, httpReq)
	if createResp.Error != nil {
		return nil, "", "", fmt.Errorf("ToAPIs 创建任务失败: %v", createResp.Error)
	}
	if createResp.StatusCode >= 400 {
		return nil, "", "", fmt.Errorf("ToAPIs 创建任务返回错误状态码: %d, Body: %s", createResp.StatusCode, string(createResp.Body))
	}
	createData, err := parseJSONResponseMap(createResp.Body)
	if err != nil {
		return nil, "", "", fmt.Errorf("ToAPIs 创建任务响应不是有效JSON: %v", err)
	}
	taskID := extractToAPIsTaskID(createData)
	if taskID == "" {
		return nil, "", "", fmt.Errorf("ToAPIs 未返回任务 ID")
	}
	status := extractToAPIsStatus(createData)
	log.Printf("[AI任务] ToAPIs 任务已创建: %d/%d task_id=%s status=%s 耗时=%v", current, total, taskID, status, time.Since(createStart))

	statusEndpoint := buildToAPIsStatusEndpoint(taskCtx.APIConfig.APIEndpoint, taskID)
	deadline := time.Now().Add(toapisMaxWait)
	lastData := createData
	for {
		if status == "" || isToAPIsPendingStatus(status) {
			if time.Now().After(deadline) {
				return nil, "", "", fmt.Errorf("ToAPIs 任务等待超时: task_id=%s", taskID)
			}
			select {
			case <-taskCtx.ctx.Done():
				return nil, "", "", fmt.Errorf("ToAPIs 任务被取消: %v", taskCtx.ctx.Err())
			case <-time.After(toapisPollInterval):
			}
			pollReq := &HTTPRequest{
				URL:         statusEndpoint,
				Method:      http.MethodGet,
				Headers:     cloneStringMap(httpReq.Headers),
				Timeout:     toapisPollInterval + 20*time.Second,
				ContentType: "application/json",
			}
			pollResp := DoHTTPRequest(taskCtx.ctx, pollReq)
			if pollResp.Error != nil {
				return nil, "", "", fmt.Errorf("ToAPIs 查询任务失败: %v", pollResp.Error)
			}
			if pollResp.StatusCode >= 400 {
				return nil, "", "", fmt.Errorf("ToAPIs 查询任务返回错误状态码: %d, Body: %s", pollResp.StatusCode, string(pollResp.Body))
			}
			lastData, err = parseJSONResponseMap(pollResp.Body)
			if err != nil {
				return nil, "", "", fmt.Errorf("ToAPIs 查询任务响应不是有效JSON: %v", err)
			}
			status = extractToAPIsStatus(lastData)
			log.Printf("[AI任务] ToAPIs 任务轮询: %d/%d task_id=%s status=%s", current, total, taskID, status)
		}

		if isToAPIsSuccessStatus(status) {
			normalized := normalizeToAPIsImageResponse(lastData, taskID, status)
			normalizedBody, _ := json.Marshal(normalized)
			return p.processImageHTTPResponse(taskCtx, &HTTPResponse{StatusCode: http.StatusOK, Body: normalizedBody})
		}
		if isToAPIsFailedStatus(status) {
			return nil, "", "", fmt.Errorf("ToAPIs 任务处理失败: task_id=%s status=%s", taskID, status)
		}
		if !isToAPIsPendingStatus(status) {
			return nil, "", "", fmt.Errorf("ToAPIs 任务状态未知: task_id=%s status=%s", taskID, status)
		}
	}
}

func (p *RequestPool) executeRunningHubAsyncImageTask(taskCtx *AITaskContext, httpReq *HTTPRequest, generateCount int, usedModel string, usedEndpoint string) (map[string]interface{}, []string, []string, string, string, error) {
	if generateCount < 1 {
		generateCount = 1
	}
	createResp := DoHTTPRequest(taskCtx.ctx, httpReq)
	if createResp.Error != nil {
		return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("RunningHub 创建任务失败: %v", createResp.Error)
	}
	if createResp.StatusCode >= 400 {
		return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("RunningHub 创建任务返回错误状态码: %d, Body: %s", createResp.StatusCode, string(createResp.Body))
	}
	createData, err := parseJSONResponseMap(createResp.Body)
	if err != nil {
		return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("RunningHub 创建任务响应不是有效JSON: %v", err)
	}
	taskID := extractRunningHubTaskID(createData)
	if taskID == "" {
		return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("RunningHub 未返回 taskId")
	}
	status := extractRunningHubStatus(createData)
	log.Printf("[AI任务] RunningHub 任务已创建: task_id=%s status=%s", taskID, status)

	interval := p.runningHubPollInterval()
	deadline := time.Now().Add(p.runningHubMaxWait())
	lastData := createData
	for {
		if isRunningHubSuccessStatus(status) {
			responseData, watermarkedURLs, rawURLs, err := p.processRunningHubSuccessResponse(taskCtx, lastData, taskID, status, generateCount)
			if err != nil {
				return nil, nil, nil, usedModel, usedEndpoint, err
			}
			return responseData, watermarkedURLs, rawURLs, usedModel, usedEndpoint, nil
		}
		if isRunningHubFailedStatus(status) {
			return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("RunningHub 任务处理失败: task_id=%s status=%s error=%s", taskID, status, extractRunningHubError(lastData))
		}
		if time.Now().After(deadline) {
			return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("RunningHub 任务等待超时: task_id=%s", taskID)
		}
		select {
		case <-taskCtx.ctx.Done():
			return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("RunningHub 任务被取消: %v", taskCtx.ctx.Err())
		case <-time.After(interval):
		}
		pollReq := &HTTPRequest{
			URL:         runningHubQueryEndpoint,
			Method:      http.MethodPost,
			Headers:     cloneStringMap(httpReq.Headers),
			Body:        map[string]interface{}{"taskId": taskID},
			Timeout:     interval + 20*time.Second,
			ContentType: "application/json",
		}
		pollResp := DoHTTPRequest(taskCtx.ctx, pollReq)
		if pollResp.Error != nil {
			return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("RunningHub 查询任务失败: %v", pollResp.Error)
		}
		if pollResp.StatusCode >= 400 {
			return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("RunningHub 查询任务返回错误状态码: %d, Body: %s", pollResp.StatusCode, string(pollResp.Body))
		}
		lastData, err = parseJSONResponseMap(pollResp.Body)
		if err != nil {
			return nil, nil, nil, usedModel, usedEndpoint, fmt.Errorf("RunningHub 查询任务响应不是有效JSON: %v", err)
		}
		status = extractRunningHubStatus(lastData)
		log.Printf("[AI任务] RunningHub 任务轮询: task_id=%s status=%s", taskID, status)
	}
}

func (p *RequestPool) processRunningHubSuccessResponse(taskCtx *AITaskContext, data map[string]interface{}, taskID string, status string, generateCount int) (map[string]interface{}, []string, []string, error) {
	results := extractRunningHubResults(data)
	if len(results) == 0 {
		return nil, nil, nil, fmt.Errorf("RunningHub 任务成功但未返回图片结果")
	}
	watermarkedURLs := make([]string, 0, len(results))
	rawURLs := make([]string, 0, len(results))
	responses := make([]interface{}, 0, len(results))
	thumbnailURLs := make([]string, 0, len(results))
	for index, result := range results {
		imageURL, _ := result["url"].(string)
		imageURL = strings.TrimSpace(imageURL)
		if imageURL == "" {
			log.Printf("[AI任务] ⚠ RunningHub 结果[%d]没有图片URL，已跳过", index+1)
			continue
		}
		normalized := map[string]interface{}{
			"provider":         "runninghub",
			"protocol_type":    "runninghub_async",
			"external_task_id": taskID,
			"external_status":  status,
			"raw_response":     data,
			"data":             []interface{}{result},
		}
		normalizedBody, _ := json.Marshal(normalized)
		responseData, watermarkedURL, rawURL, err := p.processImageHTTPResponse(taskCtx, &HTTPResponse{StatusCode: http.StatusOK, Body: normalizedBody})
		if err != nil {
			log.Printf("[AI任务] ⚠ RunningHub 结果[%d]保存失败: %v", index+1, err)
			continue
		}
		responses = append(responses, responseData)
		if watermarkedURL != "" {
			watermarkedURLs = append(watermarkedURLs, watermarkedURL)
		}
		if rawURL != "" {
			rawURLs = append(rawURLs, rawURL)
		}
		if thumbnailURL := extractThumbnailURLFromResponse(responseData); thumbnailURL != "" {
			thumbnailURLs = append(thumbnailURLs, thumbnailURL)
		}
	}
	if len(watermarkedURLs) == 0 {
		return nil, nil, nil, fmt.Errorf("RunningHub 未获取到任何可保存的图片结果")
	}
	resultData := map[string]interface{}{
		"provider":         "runninghub",
		"protocol_type":    "runninghub_async",
		"external_task_id": taskID,
		"external_status":  status,
		"responses":        responses,
		"generated_count":  len(watermarkedURLs),
		"expected_count":   generateCount,
	}
	if len(thumbnailURLs) > 0 {
		resultData["thumbnail_url"] = thumbnailURLs[0]
		resultData["thumbnail_urls"] = thumbnailURLs
	}
	return resultData, watermarkedURLs, rawURLs, nil
}

func (p *RequestPool) runningHubPollInterval() time.Duration {
	seconds := int64(3)
	if p != nil && p.cfg != nil && p.cfg.AI.RunningHubPollIntervalSeconds > 0 {
		seconds = p.cfg.AI.RunningHubPollIntervalSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (p *RequestPool) runningHubMaxWait() time.Duration {
	seconds := int64(180)
	if p != nil && p.cfg != nil && p.cfg.AI.RunningHubMaxWaitSeconds > 0 {
		seconds = p.cfg.AI.RunningHubMaxWaitSeconds
	}
	return time.Duration(seconds) * time.Second
}

func extractRunningHubTaskID(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	if value, ok := data["taskId"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if value, ok := data["task_id"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if nested, ok := data["data"].(map[string]interface{}); ok {
		return extractRunningHubTaskID(nested)
	}
	return ""
}

func extractRunningHubStatus(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	if value, ok := data["status"].(string); ok {
		return strings.ToUpper(strings.TrimSpace(value))
	}
	if nested, ok := data["data"].(map[string]interface{}); ok {
		return extractRunningHubStatus(nested)
	}
	return ""
}

func extractRunningHubError(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	for _, key := range []string{"errorMessage", "error_message", "message"} {
		if value, ok := data[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isRunningHubSuccessStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "SUCCESS")
}

func isRunningHubFailedStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "FAILED", "FAIL", "ERROR", "CANCELED", "CANCELLED":
		return true
	default:
		return false
	}
}

func extractRunningHubResults(data map[string]interface{}) []map[string]interface{} {
	if data == nil {
		return nil
	}
	rawResults, ok := data["results"].([]interface{})
	if !ok {
		if nested, ok := data["data"].(map[string]interface{}); ok {
			return extractRunningHubResults(nested)
		}
		return nil
	}
	results := make([]map[string]interface{}, 0, len(rawResults))
	for _, item := range rawResults {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		imageURL, _ := itemMap["url"].(string)
		if strings.TrimSpace(imageURL) == "" {
			continue
		}
		results = append(results, itemMap)
	}
	return results
}

func parseJSONResponseMap(data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func cloneJSONMap(value interface{}) (map[string]interface{}, bool) {
	source, ok := value.(map[string]interface{})
	if !ok {
		return nil, false
	}
	raw, err := json.Marshal(source)
	if err != nil {
		return nil, false
	}
	var cloned map[string]interface{}
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil, false
	}
	return cloned, true
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func buildToAPIsStatusEndpoint(createEndpoint string, taskID string) string {
	base := strings.TrimSpace(createEndpoint)
	query := ""
	if idx := strings.Index(base, "?"); idx >= 0 {
		query = base[idx:]
		base = base[:idx]
	}
	return strings.TrimRight(base, "/") + "/" + url.PathEscape(strings.TrimSpace(taskID)) + query
}

func extractToAPIsTaskID(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	for _, key := range []string{"id", "task_id"} {
		if value, ok := data[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if nested, ok := data["data"].(map[string]interface{}); ok {
		return extractToAPIsTaskID(nested)
	}
	return ""
}

func extractToAPIsStatus(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	if value, ok := data["status"].(string); ok {
		return strings.ToLower(strings.TrimSpace(value))
	}
	if nested, ok := data["data"].(map[string]interface{}); ok {
		return extractToAPIsStatus(nested)
	}
	return ""
}

func isToAPIsPendingStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "queued", "created", "processing", "running", "in_progress":
		return true
	default:
		return false
	}
}

func isToAPIsSuccessStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete", "succeeded", "success", "finished":
		return true
	default:
		return false
	}
}

func isToAPIsFailedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error", "cancelled", "canceled", "timeout", "expired":
		return true
	default:
		return false
	}
}

func normalizeToAPIsImageResponse(data map[string]interface{}, taskID string, status string) map[string]interface{} {
	normalized := map[string]interface{}{
		"provider":         "toapis",
		"protocol_type":    "toapis_async",
		"external_task_id": taskID,
		"external_status":  status,
		"raw_response":     data,
	}
	urls := extractToAPIsImageURLs(data)
	if len(urls) > 0 {
		items := make([]interface{}, 0, len(urls))
		for _, imageURL := range urls {
			items = append(items, map[string]interface{}{"url": imageURL})
		}
		normalized["data"] = items
	}
	return normalized
}

func extractToAPIsImageURLs(data map[string]interface{}) []string {
	if data == nil {
		return nil
	}
	if result, ok := data["result"].(map[string]interface{}); ok {
		if urls := collectImageURLsFromDataValue(result["data"]); len(urls) > 0 {
			return urls
		}
		if urlText, ok := result["url"].(string); ok && strings.TrimSpace(urlText) != "" {
			return []string{strings.TrimSpace(urlText)}
		}
	}
	if urls := collectImageURLsFromDataValue(data["data"]); len(urls) > 0 {
		return urls
	}
	if urlText, ok := data["url"].(string); ok && strings.TrimSpace(urlText) != "" {
		return []string{strings.TrimSpace(urlText)}
	}
	return nil
}

func collectImageURLsFromDataValue(value interface{}) []string {
	switch current := value.(type) {
	case []interface{}:
		result := make([]string, 0, len(current))
		for _, item := range current {
			switch itemValue := item.(type) {
			case map[string]interface{}:
				if urlText, ok := itemValue["url"].(string); ok && strings.TrimSpace(urlText) != "" {
					result = append(result, strings.TrimSpace(urlText))
				}
			case string:
				if strings.TrimSpace(itemValue) != "" {
					result = append(result, strings.TrimSpace(itemValue))
				}
			}
		}
		return result
	case map[string]interface{}:
		if urlText, ok := current["url"].(string); ok && strings.TrimSpace(urlText) != "" {
			return []string{strings.TrimSpace(urlText)}
		}
	case string:
		if strings.TrimSpace(current) != "" {
			return []string{strings.TrimSpace(current)}
		}
	}
	return nil
}

func (p *RequestPool) buildRequestBody(taskCtx *AITaskContext) (map[string]interface{}, error) {
	var body map[string]interface{}
	var aspectRatio string
	var normalizedImageSize string
	var requestImageSize string

	// 解析模板
	if taskCtx.APIConfig.BodyTemplate != "" {
		if err := json.Unmarshal([]byte(taskCtx.APIConfig.BodyTemplate), &body); err != nil {
			return nil, fmt.Errorf("解析请求体模板失败: %v", err)
		}
	} else {
		body = make(map[string]interface{})
	}
	replacePlaceholder(body, "{{image_mime_type}}", "image/jpeg")

	// 自动替换占位符 {{prompt}} 和图片
	if taskCtx.Prompt != "" {
		replacePlaceholder(body, "{{prompt}}", taskCtx.Prompt)
	}
	rawImageURL := taskCtx.ImageURL
	if rawImageURL == "" && len(taskCtx.ImageURLs) > 0 {
		rawImageURL = taskCtx.ImageURLs[0]
	}
	if rawImageURL != "" {
		replacePlaceholder(body, "{{image_url}}", rawImageURL)
	}
	if isRunningHubAsyncConfig(taskCtx.APIConfig) {
		replacePlaceholder(body, "{{width}}", payloadStringValue(taskCtx.Payload, "848", "width"))
		replacePlaceholder(body, "{{height}}", payloadStringValue(taskCtx.Payload, "1280", "height"))
		replacePlaceholder(body, "{{batch_size}}", payloadStringValue(taskCtx.Payload, "1", "batch_size", "generate_count"))
		if err := p.replaceRunningHubImageToImageMedia(taskCtx, body); err != nil {
			return nil, err
		}
		return body, nil
	}

	// 处理多图：展开为多个 inline_data
	if len(taskCtx.ImageURLs) > 0 {
		if isToAPIsAsyncConfig(taskCtx.APIConfig) {
			setToAPIsReferenceImagePayload(body, taskCtx.APIConfig, taskCtx.ImageURLs)
		} else if isGPTImage2VipConfig(taskCtx.APIConfig) {
			imageValues := make([]interface{}, 0, len(taskCtx.ImageURLs))
			for _, imageURL := range taskCtx.ImageURLs {
				imageURL = strings.TrimSpace(imageURL)
				if imageURL != "" {
					imageValues = append(imageValues, imageURL)
				}
			}
			if len(imageValues) == 1 {
				body["image"] = imageValues[0]
			} else if len(imageValues) > 1 {
				body["image"] = imageValues
			}
		} else if isSeedreamAPIConfig(taskCtx.APIConfig) {
			imageValues := make([]interface{}, 0, len(taskCtx.ImageURLs))
			for _, imageURL := range taskCtx.ImageURLs {
				imageURL = strings.TrimSpace(imageURL)
				if imageURL != "" {
					imageValues = append(imageValues, imageURL)
				}
			}
			if len(imageValues) == 1 {
				body["image"] = imageValues[0]
			} else if len(imageValues) > 1 {
				body["image"] = imageValues
			}
		} else if err := replaceImagesPlaceholder(body, taskCtx.ImageURLs); err != nil {
			return nil, fmt.Errorf("处理多图失败: %v", err)
		}
	} else if taskCtx.ImageURL != "" {
		if isToAPIsAsyncConfig(taskCtx.APIConfig) {
			setToAPIsReferenceImagePayload(body, taskCtx.APIConfig, []string{taskCtx.ImageURL})
		} else if isGPTImage2VipConfig(taskCtx.APIConfig) {
			body["image"] = taskCtx.ImageURL
		} else if isSeedreamAPIConfig(taskCtx.APIConfig) {
			body["image"] = taskCtx.ImageURL
		} else if strings.Contains(taskCtx.APIConfig.BodyTemplate, "{{image}}") {
			imageValue := taskCtx.ImageURL
			imageMimeType := "image/jpeg"
			if strings.HasPrefix(imageValue, "http://") || strings.HasPrefix(imageValue, "https://") {
				if base64Str, mimeType, err := fetchImageAsBase64Payload(imageValue); err == nil {
					imageValue = base64Str
					imageMimeType = mimeType
					log.Printf("[AI任务] 参考图已从 OSS 取回并转为 Base64（无 data: 前缀）嵌入，mime_type=%s", imageMimeType)
				} else {
					return nil, fmt.Errorf("处理单图参考图失败: %v", err)
				}
			}
			replacePlaceholder(body, "{{image}}", imageValue)
			replacePlaceholder(body, "{{image_mime_type}}", imageMimeType)
		} else if err := replaceImagesPlaceholder(body, []string{taskCtx.ImageURL}); err != nil {
			return nil, fmt.Errorf("处理单图失败: %v", err)
		}
	}

	// 绘画任务：根据 payload 计算并注入 {{aspect_ratio}} 和 {{image_size}}
	if taskCtx.TaskType == "ai_draw" {
		aspectRatio = GetAspectRatioFromPayload(taskCtx.Payload)
		normalizedImageSize = GetImageSizeFromPayload(taskCtx.Payload)
		requestImageSize = normalizedImageSize
		if isGPTImage2VipConfig(taskCtx.APIConfig) {
			requestImageSize = GetGPTImage2VipSizeFromPayload(taskCtx.Payload)
		}
		if isSeedreamAPIConfig(taskCtx.APIConfig) {
			requestImageSize = GetSeedreamImageSizeFromPayload(taskCtx.Payload)
		}
		if aspectRatio != "" {
			replacePlaceholder(body, "{{aspect_ratio}}", aspectRatio)
		}
		if requestImageSize != "" {
			replacePlaceholder(body, "{{image_size}}", requestImageSize)
		}
	}

	// 合并用户自定义参数
	for k, v := range taskCtx.Payload {
		if shouldMergePayloadFieldToRequestBody(taskCtx, k) {
			body[k] = v
		}
	}

	if taskCtx.TaskType == "ai_draw" && !isRunningHubAsyncConfig(taskCtx.APIConfig) {
		enforceAIDrawResolutionFields(body, taskCtx.APIConfig, aspectRatio, normalizedImageSize, requestImageSize)
		if isGeminiGenerateContentConfig(taskCtx.APIConfig) {
			stripGeminiDrawTopLevelFields(body)
			log.Printf("[AI任务] Gemini generateContent 请求体已按接口文档清理顶层业务字段")
		}
		if isToAPIsAsyncConfig(taskCtx.APIConfig) {
			stripToAPIsDrawTopLevelFields(body)
			log.Printf("[AI任务] ToAPIs 异步生图请求体已按接口文档清理顶层业务字段")
		}
	}

	return body, nil
}

func payloadStringValue(payload map[string]interface{}, defaultValue string, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch current := value.(type) {
		case string:
			if strings.TrimSpace(current) != "" {
				return strings.TrimSpace(current)
			}
		case float64:
			if current > 0 {
				return fmt.Sprintf("%.0f", current)
			}
		case int:
			if current > 0 {
				return fmt.Sprintf("%d", current)
			}
		case int64:
			if current > 0 {
				return fmt.Sprintf("%d", current)
			}
		}
	}
	return defaultValue
}

func (p *RequestPool) replaceRunningHubImageToImageMedia(taskCtx *AITaskContext, body map[string]interface{}) error {
	if taskCtx == nil || taskCtx.APIConfig == nil || !strings.Contains(taskCtx.APIConfig.BodyTemplate, "{{original_image}}") {
		return nil
	}
	originalURL := payloadStringValue(taskCtx.Payload, "", "original_image", "original_image_url", "image_url")
	referenceURL := payloadStringValue(taskCtx.Payload, "", "reference_image", "reference_image_url")
	if originalURL == "" {
		return fmt.Errorf("图生图缺少原图")
	}
	if referenceURL == "" {
		return fmt.Errorf("图生图缺少参考图")
	}
	originalMedia, err := p.resolveRunningHubMediaValue(taskCtx.ctx, taskCtx.APIConfig.APIKey, originalURL)
	if err != nil {
		return fmt.Errorf("上传原图到 RunningHub 失败: %v", err)
	}
	referenceMedia, err := p.resolveRunningHubMediaValue(taskCtx.ctx, taskCtx.APIConfig.APIKey, referenceURL)
	if err != nil {
		return fmt.Errorf("上传参考图到 RunningHub 失败: %v", err)
	}
	replacePlaceholder(body, "{{original_image}}", originalMedia)
	replacePlaceholder(body, "{{reference_image}}", referenceMedia)
	return nil
}

func (p *RequestPool) resolveRunningHubMediaValue(ctx context.Context, apiKey string, imageValue string) (string, error) {
	imageValue = strings.TrimSpace(imageValue)
	if imageValue == "" {
		return "", fmt.Errorf("图片地址为空")
	}
	if !strings.HasPrefix(imageValue, "http://") && !strings.HasPrefix(imageValue, "https://") {
		return imageValue, nil
	}
	fileName, err := uploadRunningHubMediaFromURL(ctx, apiKey, imageValue, runningHubMediaUploadURL)
	if err != nil {
		return "", err
	}
	return fileName, nil
}

func uploadRunningHubMediaFromURL(ctx context.Context, apiKey string, imageURL string, uploadURL string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	apiKey = strings.TrimSpace(apiKey)
	imageURL = strings.TrimSpace(imageURL)
	uploadURL = strings.TrimSpace(uploadURL)
	if apiKey == "" {
		return "", fmt.Errorf("RunningHub API Key 未配置")
	}
	if imageURL == "" {
		return "", fmt.Errorf("图片地址为空")
	}
	if uploadURL == "" {
		return "", fmt.Errorf("RunningHub 上传接口未配置")
	}
	data, mimeType, err := fetchRemoteImageBytes(ctx, imageURL)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	ext := imageExtensionFromMIMEType(mimeType)
	formFile, err := writer.CreateFormFile("file", "reference"+ext)
	if err != nil {
		return "", err
	}
	if _, err := formFile.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("RunningHub 上传返回状态码 %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			DownloadURL string `json:"download_url"`
			FileName    string `json:"fileName"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("RunningHub 上传响应不是有效JSON: %v", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("RunningHub 上传失败: %s", result.Message)
	}
	if value := strings.TrimSpace(result.Data.DownloadURL); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(result.Data.FileName); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("RunningHub 上传未返回文件地址")
}

func fetchRemoteImageBytes(ctx context.Context, imageURL string) ([]byte, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("获取图片失败: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	mimeType := normalizeImageMIMEType(resp.Header.Get("Content-Type"), data)
	return data, mimeType, nil
}

func imageExtensionFromMIMEType(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}

func buildToAPIsImageURLPayload(imageURLs []string) []interface{} {
	items := make([]interface{}, 0, len(imageURLs))
	for _, imageURL := range imageURLs {
		imageURL = strings.TrimSpace(imageURL)
		if imageURL == "" {
			continue
		}
		items = append(items, map[string]interface{}{"url": imageURL})
	}
	return items
}

func buildToAPIsReferenceImagePayload(imageURLs []string) []string {
	items := make([]string, 0, len(imageURLs))
	for _, imageURL := range imageURLs {
		imageURL = strings.TrimSpace(imageURL)
		if imageURL != "" {
			items = append(items, imageURL)
		}
	}
	return items
}

func setToAPIsReferenceImagePayload(body map[string]interface{}, apiConfig *AIAPIConfigData, imageURLs []string) {
	if body == nil {
		return
	}
	if isToAPIsGPTImage2Config(apiConfig) {
		referenceImages := buildToAPIsReferenceImagePayload(imageURLs)
		if len(referenceImages) > 0 {
			body["reference_images"] = referenceImages
		}
		delete(body, "image_urls")
		return
	}
	imageURLItems := buildToAPIsImageURLPayload(imageURLs)
	if len(imageURLItems) > 0 {
		body["image_urls"] = imageURLItems
	}
	delete(body, "reference_images")
}

func enforceAIDrawResolutionFields(body map[string]interface{}, apiConfig *AIAPIConfigData, aspectRatio string, normalizedImageSize string, requestImageSize string) {
	if body == nil {
		return
	}
	if isGPTImage2VipConfig(apiConfig) {
		stripGPTImage2VipDrawTopLevelFields(body)
		delete(body, "quality")
		delete(body, "resolution")
		if requestImageSize != "" {
			body["size"] = requestImageSize
		}
		return
	}
	if isToAPIsAsyncConfig(apiConfig) {
		if aspectRatio != "" {
			body["size"] = aspectRatio
		}
		if normalizedImageSize != "" {
			if isToAPIsGPTImage2Config(apiConfig) {
				body["resolution"] = normalizedImageSize
			} else {
				metadata := getOrCreateChildMap(body, "metadata")
				if metadata != nil {
					metadata["resolution"] = normalizedImageSize
				}
			}
		}
		return
	}
	if isGeminiGenerateContentConfig(apiConfig) {
		generationConfig := getOrCreateChildMap(body, "generationConfig")
		imageConfig := getOrCreateChildMap(generationConfig, "imageConfig")
		if aspectRatio != "" {
			imageConfig["aspectRatio"] = aspectRatio
		}
		if normalizedImageSize != "" {
			imageConfig["imageSize"] = normalizedImageSize
		}
		return
	}
	if aspectRatio != "" {
		body["aspect_ratio"] = aspectRatio
	}
	if normalizedImageSize != "" {
		body["image_size"] = normalizedImageSize
		body["resolution"] = normalizedImageSize
	}
	if requestImageSize == "" {
		requestImageSize = normalizedImageSize
	}
	if requestImageSize == "" {
		return
	}
	if isSeedreamAPIConfig(apiConfig) {
		body["size"] = requestImageSize
		return
	}
	body["size"] = normalizedImageSize
}

func replacePlaceholder(obj interface{}, placeholder string, value string) {
	switch v := obj.(type) {
	case map[string]interface{}:
		for key, val := range v {
			if strVal, ok := val.(string); ok && strVal == placeholder {
				v[key] = value
			} else {
				replacePlaceholder(val, placeholder, value)
			}
		}
	case []interface{}:
		for i, item := range v {
			if strVal, ok := item.(string); ok && strVal == placeholder {
				v[i] = value
			} else {
				replacePlaceholder(item, placeholder, value)
			}
		}
	}
}

func fetchImageAsBase64Payload(imageURL string) (string, string, error) {
	req, err := http.NewRequest(http.MethodGet, imageURL, nil)
	if err != nil {
		return "", "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	mimeType := normalizeImageMIMEType(resp.Header.Get("Content-Type"), data)
	return base64.StdEncoding.EncodeToString(data), mimeType, nil
}

func fetchImageAsBase64DataURL(imageURL string) (string, error) {
	base64Data, _, err := fetchImageAsBase64Payload(imageURL)
	if err != nil {
		return "", err
	}
	return base64Data, nil
}

func fetchImageAsDataURL(imageURL string) (string, error) {
	base64Data, mimeType, err := fetchImageAsBase64Payload(imageURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data), nil
}

func normalizeImageMIMEType(contentType string, data []byte) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if strings.HasPrefix(contentType, "image/") {
		return contentType
	}
	detectedType := strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0])
	if strings.HasPrefix(detectedType, "image/") {
		return detectedType
	}
	return "image/jpeg"
}

func isImagePlaceholderPart(item interface{}) bool {
	partMap, ok := item.(map[string]interface{})
	if !ok {
		return false
	}
	if inlineData, ok := partMap["inline_data"].(map[string]interface{}); ok {
		if data, _ := inlineData["data"].(string); strings.TrimSpace(data) == "{{image}}" {
			return true
		}
	}
	if inlineData, ok := partMap["inlineData"].(map[string]interface{}); ok {
		if data, _ := inlineData["data"].(string); strings.TrimSpace(data) == "{{image}}" {
			return true
		}
	}
	return false
}

func replaceImagesPlaceholder(body map[string]interface{}, imageURLs []string) error {
	if messages, ok := body["messages"].([]interface{}); ok && len(messages) > 0 {
		firstMessage, ok := messages[0].(map[string]interface{})
		if !ok {
			return fmt.Errorf("messages[0] 不是对象")
		}
		contentItems := make([]interface{}, 0)
		switch rawContent := firstMessage["content"].(type) {
		case []interface{}:
			contentItems = rawContent
		case string:
			if strings.TrimSpace(rawContent) != "" {
				contentItems = append(contentItems, map[string]interface{}{
					"type": "text",
					"text": rawContent,
				})
			}
		case map[string]interface{}:
			contentItems = append(contentItems, rawContent)
		case nil:
		default:
			return fmt.Errorf("messages[0].content 类型不支持: %T", rawContent)
		}

		baseItems := make([]interface{}, 0, len(contentItems))
		for _, item := range contentItems {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				baseItems = append(baseItems, item)
				continue
			}
			if itemType, _ := itemMap["type"].(string); itemType == "image_url" {
				continue
			}
			baseItems = append(baseItems, item)
		}

		imageItems := make([]interface{}, 0, len(imageURLs))
		for _, imageURL := range imageURLs {
			imageURL = strings.TrimSpace(imageURL)
			if imageURL == "" {
				continue
			}
			imageURLValue := imageURL
			if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
				if dataURL, err := fetchImageAsDataURL(imageURL); err == nil {
					imageURLValue = dataURL
					log.Printf("[AI任务] 参考图已转为 Base64 data URL 后注入 chat/completions 备用链路")
				} else {
					log.Printf("[AI任务] ⚠ 参考图转 data URL 失败: %v，备用链路继续使用原 URL", err)
				}
			}
			imageItems = append(imageItems, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": imageURLValue,
				},
			})
		}

		firstMessage["content"] = append(baseItems, imageItems...)
		log.Printf("[AI任务] ✓ 已插入 %d 张图片到 messages[0].content 数组", len(imageItems))
		return nil
	}

	// 查找 contents[0].parts 数组
	contents, ok := body["contents"].([]interface{})
	if !ok || len(contents) == 0 {
		return fmt.Errorf("未找到 contents 数组")
	}

	firstContent, ok := contents[0].(map[string]interface{})
	if !ok {
		return fmt.Errorf("contents[0] 不是对象")
	}

	parts, ok := firstContent["parts"].([]interface{})
	if !ok {
		return fmt.Errorf("未找到 parts 数组")
	}
	baseParts := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		if isImagePlaceholderPart(part) {
			continue
		}
		baseParts = append(baseParts, part)
	}

	// 处理每张图片：转换为Base64并创建 inline_data 对象
	imageParts := make([]interface{}, 0, len(imageURLs))
	for i, imageURL := range imageURLs {
		imageValue := imageURL
		imageMimeType := "image/jpeg"
		// 若是 OSS/HTTP(S) URL，从网络取图并转为 Base64（无 data: 前缀）
		if strings.HasPrefix(imageValue, "http://") || strings.HasPrefix(imageValue, "https://") {
			if base64Str, mimeType, err := fetchImageAsBase64Payload(imageValue); err == nil {
				imageValue = base64Str
				imageMimeType = mimeType
				log.Printf("[AI任务] 参考图[%d/%d]已从 OSS 取回并转为 Base64（无 data: 前缀）嵌入，mime_type=%s", i+1, len(imageURLs), imageMimeType)
			} else {
				log.Printf("[AI任务] ⚠ 参考图[%d/%d]取回失败: %v，跳过该图片", i+1, len(imageURLs), err)
				continue // 跳过失败的图片
			}
		}

		// 创建 inline_data 对象
		imageParts = append(imageParts, map[string]interface{}{
			"inline_data": map[string]interface{}{
				"mime_type": imageMimeType,
				"data":      imageValue,
			},
		})
	}

	// 将图片 parts 插入到原有 parts 数组的末尾（在 text 之后）
	firstContent["parts"] = append(baseParts, imageParts...)

	log.Printf("[AI任务] ✓ 已插入 %d 张图片到 parts 数组", len(imageParts))
	return nil
}

func (p *RequestPool) parseHeaders(headersJSON string) (map[string]string, error) {
	headers := make(map[string]string)
	if headersJSON == "" || headersJSON == "{}" {
		return headers, nil
	}
	if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
		return nil, err
	}
	return headers, nil
}

func buildListThumbnail(imageBytes []byte, maxWidth int, quality int) ([]byte, error) {
	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("image bytes are empty")
	}
	if maxWidth <= 0 {
		return nil, fmt.Errorf("max width must be positive")
	}
	if quality <= 0 || quality > 100 {
		quality = 74
	}

	src, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()
	if srcWidth <= 0 || srcHeight <= 0 {
		return nil, fmt.Errorf("invalid source size")
	}

	targetWidth := srcWidth
	targetHeight := srcHeight
	if srcWidth > maxWidth {
		targetWidth = maxWidth
		targetHeight = int(float64(srcHeight) * float64(maxWidth) / float64(srcWidth))
	}
	if targetWidth <= 0 || targetHeight <= 0 {
		return nil, fmt.Errorf("invalid target size")
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	stddraw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, stddraw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (p *RequestPool) populateThumbnailURL(taskCtx *AITaskContext, responseData map[string]interface{}, upload func(string, []byte, string) (string, error), imageBytes []byte) {
	if taskCtx == nil || responseData == nil || len(imageBytes) == 0 || upload == nil {
		return
	}
	thumbnailBytes, err := buildListThumbnail(imageBytes, aiTaskListThumbWidth, 74)
	if err != nil {
		log.Printf("[AI浠诲姟] 鈿?缂╃暐鍥剧敓鎴愬け璐? task_no=%s err=%v", taskCtx.TaskNo, err)
		return
	}
	thumbnailURL, err := upload("list_thumb", thumbnailBytes, "image/jpeg")
	if err != nil {
		log.Printf("[AI浠诲姟] 鈿?缂╃暐鍥句笂浼犲け璐? task_no=%s err=%v", taskCtx.TaskNo, err)
		return
	}
	if thumbnailURL == "" {
		return
	}
	responseData["thumbnail_url"] = strings.TrimSpace(thumbnailURL)
}

func (p *RequestPool) saveResultToOSS(taskCtx *AITaskContext, data []byte, responseData map[string]interface{}) (watermarkedURL, rawURL string, err error) {
	if taskCtx == nil {
		return "", "", fmt.Errorf("任务上下文为空")
	}
	if taskCtx.ctx == nil {
		return "", "", fmt.Errorf("任务上下文的上下文为空")
	}
	if p.cosClient == nil {
		return "", "", fmt.Errorf("COS客户端未初始化")
	}
	if p.cfg == nil {
		return "", "", fmt.Errorf("配置为空")
	}

	var imageData []byte
	var ext string
	var contentType string

	base64Image, imgFormat := extractBase64ImageFromResponse(responseData)
	if base64Image != "" {
		log.Printf("[AI任务] 检测到Base64图片，格式: %s", imgFormat)
		decoded, decErr := base64.StdEncoding.DecodeString(base64Image)
		if decErr != nil {
			log.Printf("[AI任务] Base64解码失败: %v", decErr)
			// 对于 AI 绘画任务，Base64 解码失败视为任务失败，不再保存 JSON
			if taskCtx.TaskType == "ai_draw" {
				return "", "", fmt.Errorf("AI绘画Base64解码失败: %v", decErr)
			}
		} else {
			imageData = decoded
			ext = "." + imgFormat
			contentType = "image/" + imgFormat
			log.Printf("[AI任务] Base64图片解码成功，大小: %d bytes", len(imageData))
		}
	}

	if imageData == nil {
		remoteImageURL := extractImageURLFromResponse(responseData)
		if remoteImageURL != "" {
			log.Printf("[AI任务] 检测到远程图片URL，开始下载: %s", remoteImageURL)
			log.Printf("[AI任务] 远程结果图下载超时设置: %v", remoteResultDownloadTimeout)
			downloadedData, downloadedExt, downloadedContentType, downloadErr := fetchRemoteImage(remoteImageURL)
			if downloadErr != nil {
				if taskCtx.TaskType == "ai_draw" {
					return "", "", fmt.Errorf("下载AI绘画结果失败: %v", downloadErr)
				}
			} else {
				imageData = downloadedData
				ext = downloadedExt
				contentType = downloadedContentType
				log.Printf("[AI任务] 远程图片下载成功，大小: %d bytes", len(imageData))
			}
		}
	}

	if imageData == nil {
		detectedType := http.DetectContentType(data)
		ext = ".json"
		if strings.HasPrefix(detectedType, "image/") {
			switch {
			case strings.Contains(detectedType, "png"):
				ext = ".png"
			case strings.Contains(detectedType, "jpeg"):
				ext = ".jpg"
			case strings.Contains(detectedType, "gif"):
				ext = ".gif"
			case strings.Contains(detectedType, "webp"):
				ext = ".webp"
			default:
				ext = ".png"
			}
			imageData = data
			contentType = detectedType
		} else {
			// 对于 AI 绘画任务，如果没有解析出图片且响应是 JSON，则认为解析失败，标记任务失败并退回灵石，不上传 JSON
			if taskCtx.TaskType == "ai_draw" {
				return "", "", fmt.Errorf("未能从AI绘画响应中解析出图片")
			}
			imageData, _ = json.MarshalIndent(responseData, "", "  ")
			contentType = "application/json"
		}
	}

	now := time.Now()
	prefix := NormalizePrefix(p.cfg.COS.Prefix)
	baseDir := path.Join("ai_results", taskCtx.TaskType, now.Format("2006/01/02"))
	uid := uuid.New().String()[:8]

	uploadOne := func(keySuffix string, body []byte, ct string) (string, error) {
		objectKey := path.Join(baseDir, fmt.Sprintf("%s_%s_%s%s", taskCtx.TaskNo, uid, keySuffix, ext))
		fullKey := objectKey
		if prefix != "" {
			fullKey = path.Join(prefix, objectKey)
		}
		uploadStartTime := time.Now()
		log.Printf("[AI任务] 开始上传OSS对象: key=%s, content_type=%s, size=%d bytes", fullKey, ct, len(body))
		_, e := p.cosClient.Object.Put(taskCtx.ctx, fullKey, bytes.NewReader(body), &cos.ObjectPutOptions{
			ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{ContentType: ct},
		})
		if e != nil {
			return "", e
		}
		log.Printf("[AI任务] OSS上传完成: key=%s, 耗时=%v", fullKey, time.Since(uploadStartTime))
		return BuildObjectURL(p.cfg, fullKey), nil
	}

	// 非图片（如 JSON）只上传一份，水印与原图返回同一 URL
	if !strings.HasPrefix(contentType, "image/") {
		urlOne, e := uploadOne("result", imageData, contentType)
		if e != nil {
			return "", "", e
		}
		return urlOne, urlOne, nil
	}

	// 1. 上传原图
	rawURL, err = uploadOne("raw", imageData, contentType)
	if err != nil {
		return "", "", err
	}
	log.Printf("[AI任务] 原图已上传: %s", rawURL)

	// 2. 加水印后上传
	watermarkedData, wErr := AddWatermark(imageData, "")
	if wErr != nil {
		log.Printf("[AI任务] ⚠ 水印生成失败: %v，原图 URL 仍返回", wErr)
		p.populateThumbnailURL(taskCtx, responseData, uploadOne, imageData)
		return rawURL, rawURL, nil
	}
	watermarkedURL, err = uploadOne("watermarked", watermarkedData, contentType)
	if err != nil {
		return "", rawURL, err
	}
	log.Printf("[AI任务] 带水印图已上传: %s", watermarkedURL)
	p.populateThumbnailURL(taskCtx, responseData, uploadOne, watermarkedData)
	return watermarkedURL, rawURL, nil
}

func extractBase64ImageFromResponse(responseData map[string]interface{}) (string, string) {
	// 1. 优先尝试新的 candidates[].content.parts[].inlineData 结构
	if candidates, ok := responseData["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if first, ok := candidates[0].(map[string]interface{}); ok {
			if content, ok := first["content"].(map[string]interface{}); ok {
				if parts, ok := content["parts"].([]interface{}); ok && len(parts) > 0 {
					if part0, ok := parts[0].(map[string]interface{}); ok {
						if inlineData, ok := part0["inlineData"].(map[string]interface{}); ok {
							data, _ := inlineData["data"].(string)
							if data != "" {
								mime, _ := inlineData["mimeType"].(string)
								format := "png"
								if strings.Contains(mime, "jpeg") || strings.Contains(mime, "jpg") {
									format = "jpeg"
								} else if strings.Contains(mime, "gif") {
									format = "gif"
								} else if strings.Contains(mime, "webp") {
									format = "webp"
								}
								log.Printf("[AI任务] 从 candidates[0].content.parts[0].inlineData 提取到图片，mime=%s，format=%s", mime, format)
								return data, format
							}
						}
					}
				}
			}
		}
	}

	if dataList, ok := responseData["data"].([]interface{}); ok && len(dataList) > 0 {
		if first, ok := dataList[0].(map[string]interface{}); ok {
			if b64JSON, ok := first["b64_json"].(string); ok && b64JSON != "" {
				format := "png"
				if strings.HasPrefix(b64JSON, "/9j/") {
					format = "jpeg"
				} else if strings.HasPrefix(b64JSON, "R0lGOD") {
					format = "gif"
				} else if strings.HasPrefix(b64JSON, "UklGR") {
					format = "webp"
				}
				return b64JSON, format
			}
		}
	}

	// 2. 兼容旧的 choices[0].message.content Markdown 图片格式
	choices, ok := responseData["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", ""
	}

	firstChoice, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", ""
	}

	message, ok := firstChoice["message"].(map[string]interface{})
	if !ok {
		return "", ""
	}

	content, ok := message["content"].(string)
	if !ok {
		return "", ""
	}

	log.Printf("[AI任务] 提取到内容: %s", truncateString(content, 200))

	// 解析内容格式: ![image](base64数据) 或 ![image](data:image/xxx;base64,数据)
	// 查找 ![image]( 和 ) 之间的内容
	startMarker := "![image]("
	startIdx := strings.Index(content, startMarker)
	if startIdx == -1 {
		// 尝试其他格式
		startMarker = "![Image]("
		startIdx = strings.Index(content, startMarker)
	}
	if startIdx == -1 {
		return "", ""
	}

	startIdx += len(startMarker)
	endIdx := strings.LastIndex(content, ")")
	if endIdx == -1 || endIdx <= startIdx {
		return "", ""
	}

	base64Data := content[startIdx:endIdx]

	// 检查是否是 data:image/xxx;base64, 格式
	if strings.HasPrefix(base64Data, "data:image/") {
		// 格式: data:image/png;base64,xxxxx
		parts := strings.SplitN(base64Data, ",", 2)
		if len(parts) == 2 {
			// 提取图片格式
			format := "png" // 默认
			if strings.Contains(parts[0], "jpeg") || strings.Contains(parts[0], "jpg") {
				format = "jpeg"
			} else if strings.Contains(parts[0], "gif") {
				format = "gif"
			} else if strings.Contains(parts[0], "webp") {
				format = "webp"
			}
			return parts[1], format
		}
	}

	// 直接就是Base64数据，尝试检测图片格式
	// PNG: 以 iVBORw0KGgo 开头
	// JPEG: 以 /9j/ 开头
	// GIF: 以 R0lGODlh 或 R0lGODdh 开头
	format := "png" // 默认
	if strings.HasPrefix(base64Data, "/9j/") {
		format = "jpeg"
	} else if strings.HasPrefix(base64Data, "R0lGOD") {
		format = "gif"
	} else if strings.HasPrefix(base64Data, "UklGR") {
		format = "webp"
	}

	return base64Data, format
}

func extractImageURLFromResponse(responseData map[string]interface{}) string {
	dataList, ok := responseData["data"].([]interface{})
	if !ok || len(dataList) == 0 {
		return ""
	}

	first, ok := dataList[0].(map[string]interface{})
	if !ok {
		return ""
	}

	urlValue, ok := first["url"].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(urlValue)
}

func extractImageURLsFromResponse(responseData map[string]interface{}) []string {
	dataList, ok := responseData["data"].([]interface{})
	if !ok || len(dataList) == 0 {
		return nil
	}

	urls := make([]string, 0, len(dataList))
	for _, item := range dataList {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		urlValue, ok := itemMap["url"].(string)
		if !ok {
			continue
		}
		urlValue = strings.TrimSpace(urlValue)
		if urlValue != "" {
			urls = append(urls, urlValue)
		}
	}

	if len(urls) == 0 {
		return nil
	}

	return urls
}

func extractThumbnailURLFromResponse(responseData map[string]interface{}) string {
	if responseData == nil {
		return ""
	}
	value, ok := responseData["thumbnail_url"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func extractThumbnailURLsFromResponse(responseData map[string]interface{}) []string {
	if responseData == nil {
		return nil
	}
	values, ok := responseData["thumbnail_urls"].([]interface{})
	if !ok || len(values) == 0 {
		if single := extractThumbnailURLFromResponse(responseData); single != "" {
			return []string{single}
		}
		return nil
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		value, ok := item.(string)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func fetchRemoteImage(imageURL string) ([]byte, string, string, error) {
	startTime := time.Now()
	req, err := http.NewRequest(http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", "", err
	}

	client := &http.Client{Timeout: remoteResultDownloadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[AI任务] 远程结果图请求失败: %v (耗时: %v)", err, time.Since(startTime))
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[AI任务] 远程结果图响应异常: status=%d, 耗时=%v", resp.StatusCode, time.Since(startTime))
		return nil, "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	log.Printf("[AI任务] 远程结果图响应成功: status=%d, content_length=%s, content_type=%s, 耗时=%v", resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Length")), strings.TrimSpace(resp.Header.Get("Content-Type")), time.Since(startTime))

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[AI任务] 远程结果图读取失败: %v (耗时: %v)", err, time.Since(startTime))
		return nil, "", "", err
	}
	log.Printf("[AI任务] 远程结果图读取完成: size=%d bytes, 总耗时=%v", len(data), time.Since(startTime))

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	ext := imageExtFromContentType(contentType)
	if ext == "" {
		switch {
		case strings.HasPrefix(contentType, "image/png"):
			ext = ".png"
		case strings.HasPrefix(contentType, "image/jpeg"):
			ext = ".jpg"
		case strings.HasPrefix(contentType, "image/gif"):
			ext = ".gif"
		case strings.HasPrefix(contentType, "image/webp"):
			ext = ".webp"
		default:
			ext = ".png"
		}
	}

	return data, ext, contentType, nil
}

func imageExtFromContentType(contentType string) string {
	switch {
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg"):
		return ".jpg"
	case strings.Contains(contentType, "gif"):
		return ".gif"
	case strings.Contains(contentType, "webp"):
		return ".webp"
	default:
		return ""
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (p *RequestPool) Stop() {
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	close(p.taskQueue)
	log.Println("[RequestPool] 请求池已停止")
}

func (p *RequestPool) GetStats() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 获取系统资源信息
	resources := GetSystemResources()

	return map[string]interface{}{
		"max_workers":       p.maxWorkers,
		"active_workers":    p.activeWorkers,
		"queue_size":        len(p.taskQueue),
		"queue_capacity":    cap(p.taskQueue),
		"running":           p.running,
		"cpu_cores":         resources.CPUCores,
		"cpu_usage_percent": resources.CPUUsagePercent,
		"total_memory_mb":   resources.TotalMemoryMB,
		"avail_memory_mb":   resources.AvailMemoryMB,
		"mem_usage_percent": resources.MemUsagePercent,
		"go_routines":       runtime.NumGoroutine(),
	}
}

func DoHTTPRequest(ctx context.Context, req *HTTPRequest) *HTTPResponse {
	startTime := time.Now()
	resp := &HTTPResponse{
		Headers: make(map[string]string),
	}
	if req == nil {
		resp.Error = fmt.Errorf("HTTP请求配置为空")
		resp.Duration = time.Since(startTime)
		return resp
	}

	method := req.Method
	if method == "" {
		method = "GET"
	}
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	contentType := req.ContentType
	if contentType == "" {
		contentType = "application/json"
	}

	// 创建带超时的context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 序列化请求体
	var bodyReader io.Reader
	if req.Body != nil {
		var bodyBytes []byte
		var err error
		switch v := req.Body.(type) {
		case []byte:
			bodyBytes = v
		case string:
			bodyBytes = []byte(v)
		default:
			bodyBytes, err = json.Marshal(req.Body)
			if err != nil {
				resp.Error = fmt.Errorf("序列化请求体失败: %v", err)
				resp.Duration = time.Since(startTime)
				return resp
			}
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// 创建HTTP请求
	httpReq, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), req.URL, bodyReader)
	if err != nil {
		resp.Error = fmt.Errorf("创建HTTP请求失败: %v", err)
		resp.Duration = time.Since(startTime)
		return resp
	}

	// 设置请求头
	httpReq.Header.Set("Content-Type", contentType)
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// 发起请求
	client := &http.Client{
		Timeout: timeout,
	}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		resp.Error = fmt.Errorf("HTTP请求失败: %v", err)
		resp.Duration = time.Since(startTime)
		return resp
	}
	defer httpResp.Body.Close()

	// 读取响应
	resp.StatusCode = httpResp.StatusCode
	for k, v := range httpResp.Header {
		if len(v) > 0 {
			resp.Headers[k] = v[0]
		}
	}

	resp.Body, err = io.ReadAll(httpResp.Body)
	if err != nil {
		resp.Error = fmt.Errorf("读取响应体失败: %v", err)
	}

	resp.Duration = time.Since(startTime)
	return resp
}

func cloneHTTPRequest(req *HTTPRequest) *HTTPRequest {
	if req == nil {
		return nil
	}
	cloned := *req
	if req.Headers != nil {
		cloned.Headers = make(map[string]string, len(req.Headers))
		for k, v := range req.Headers {
			cloned.Headers[k] = v
		}
	}
	return &cloned
}

func DoHTTPRequestAsync(ctx context.Context, req *HTTPRequest, callback func(*HTTPResponse)) {
	go func() {
		resp := DoHTTPRequest(ctx, req)
		if callback != nil {
			callback(resp)
		}
	}()
}

func DoHTTPRequestBatch(ctx context.Context, requests []*HTTPRequest, maxConcurrent int) []*HTTPResponse {
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}

	results := make([]*HTTPResponse, len(requests))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrent)

	for i, req := range requests {
		wg.Add(1)
		semaphore <- struct{}{} // 获取信号量

		go func(index int, r *HTTPRequest) {
			defer wg.Done()
			defer func() { <-semaphore }() // 释放信号量
			results[index] = DoHTTPRequest(ctx, cloneHTTPRequest(r))
		}(i, req)
	}

	wg.Wait()
	return results
}

func setValueByPath(data map[string]interface{}, path string, value interface{}) error {
	if path == "" {
		return fmt.Errorf("路径为空")
	}

	// 简单路径（不含.）
	if !strings.Contains(path, ".") && !strings.Contains(path, "[") {
		data[path] = value
		return nil
	}

	// 复杂路径处理
	parts := strings.Split(path, ".")
	current := data

	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]

		// 处理数组索引
		if idx := strings.Index(part, "["); idx != -1 {
			key := part[:idx]
			// 简化处理：如果是数组，跳过
			if _, ok := current[key]; !ok {
				current[key] = make(map[string]interface{})
			}
			if m, ok := current[key].(map[string]interface{}); ok {
				current = m
			} else {
				return fmt.Errorf("路径 %s 不是对象类型", key)
			}
			continue
		}

		if _, ok := current[part]; !ok {
			current[part] = make(map[string]interface{})
		}
		if m, ok := current[part].(map[string]interface{}); ok {
			current = m
		} else {
			return fmt.Errorf("路径 %s 不是对象类型", part)
		}
	}

	// 设置最终值
	lastPart := parts[len(parts)-1]
	// 处理最后一个部分的数组索引
	if idx := strings.Index(lastPart, "["); idx != -1 {
		lastPart = lastPart[:idx]
	}
	current[lastPart] = value
	return nil
}

func getValueByPath(data map[string]interface{}, path string) (interface{}, error) {
	if path == "" {
		return nil, fmt.Errorf("路径为空")
	}

	// 简单路径
	if !strings.Contains(path, ".") && !strings.Contains(path, "[") {
		if v, ok := data[path]; ok {
			return v, nil
		}
		return nil, fmt.Errorf("路径 %s 不存在", path)
	}

	// 复杂路径
	parts := strings.Split(path, ".")
	var current interface{} = data

	for _, part := range parts {
		// 处理数组索引
		if idx := strings.Index(part, "["); idx != -1 {
			key := part[:idx]
			if m, ok := current.(map[string]interface{}); ok {
				current = m[key]
			} else {
				return nil, fmt.Errorf("路径 %s 不是对象类型", key)
			}
			continue
		}

		if m, ok := current.(map[string]interface{}); ok {
			if v, exists := m[part]; exists {
				current = v
			} else {
				return nil, fmt.Errorf("路径 %s 不存在", part)
			}
		} else {
			return nil, fmt.Errorf("路径 %s 不是对象类型", part)
		}
	}

	return current, nil
}

func CreateAITaskContext(taskNo string, userID int64, taskType string, apiConfig *AIAPIConfigData, fallbackConfigs []*AIAPIConfigData, payload map[string]interface{}, prompt, imageURL string, imageURLs []string) *AITaskContext {
	ctx, cancel := context.WithTimeout(context.Background(), aiTaskContextTimeout)
	return &AITaskContext{
		TaskID:          uuid.New().String(),
		TaskNo:          taskNo,
		UserID:          userID,
		TaskType:        taskType,
		APIConfig:       apiConfig,
		FallbackConfigs: fallbackConfigs,
		Payload:         payload,
		Prompt:          prompt,
		ImageURL:        imageURL,
		ImageURLs:       imageURLs,
		AttemptIndex:    0,
		CreatedAt:       time.Now(),
		ctx:             ctx,
		cancel:          cancel,
	}
}

func (t *AITaskContext) Cancel() {
	if t.cancel != nil {
		t.cancel()
	}
}
