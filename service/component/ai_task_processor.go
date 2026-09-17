package component

import (
	"encoding/json"
	"fmt"
	"log"
	"service/safeerror"
	"strings"
	"sync"
	"time"

	"service/config"
	"service/model"
)

const defaultLaoZhangImageBaseURL = "https://api2.laozhang.ai"
const defaultLaoZhangImageModel = "gpt-image-2.5-sunburst-vip"
const geminiDrawGenerateContentEndpoint = defaultLaoZhangImageBaseURL + "/v1beta/models/gemini-3-pro-image-preview:generateContent"
const toapisImageGenerationEndpoint = "https://toapis.com/v1/images/generations"
const laoZhangImageGenerationEndpoint = defaultLaoZhangImageBaseURL + "/v1/images/generations"
const runningHubQueryEndpoint = "https://www.runninghub.cn/openapi/v2/query"

// AITaskProcessor AI任务处理器
type AITaskProcessor struct {
	taskModel        *model.AITaskModel
	apiConfigModel   *model.AIAPIConfigModel
	userModel        *model.UserRedisModel
	stoneRecordModel *model.StoneRecordModel
	requestPool      *RequestPool
	cfg              *config.Config
	running          bool
	stopChan         chan struct{}
	wg               sync.WaitGroup
	pollInterval     time.Duration
}

// NewAITaskProcessor 创建AI任务处理器
func NewAITaskProcessor(
	taskModel *model.AITaskModel,
	apiConfigModel *model.AIAPIConfigModel,
	userModel *model.UserRedisModel,
	stoneRecordModel *model.StoneRecordModel,
	requestPool *RequestPool,
	cfg *config.Config,
) *AITaskProcessor {
	return &AITaskProcessor{
		taskModel:        taskModel,
		apiConfigModel:   apiConfigModel,
		userModel:        userModel,
		stoneRecordModel: stoneRecordModel,
		requestPool:      requestPool,
		cfg:              cfg,
		stopChan:         make(chan struct{}),
		pollInterval:     2 * time.Second, // 每2秒轮询一次
	}
}

// Start 启动任务处理器
func (p *AITaskProcessor) Start() {
	if p.running {
		log.Println("[AITaskProcessor] 处理器已在运行中")
		return
	}

	p.running = true
	p.wg.Add(1)
	go p.pollLoop()
	log.Println("[AITaskProcessor] 任务处理器已启动")
}

// Stop 停止任务处理器
func (p *AITaskProcessor) Stop() {
	if !p.running {
		return
	}

	p.running = false
	close(p.stopChan)
	p.wg.Wait()
	log.Println("[AITaskProcessor] 任务处理器已停止")
}

// pollLoop 轮询任务队列
func (p *AITaskProcessor) pollLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.processPendingTasks()
		}
	}
}

// processPendingTasks 处理待执行的任务
func (p *AITaskProcessor) processPendingTasks() {
	// 原子领取待处理任务（每次最多处理10个）
	tasks, err := p.taskModel.ClaimPendingTasks(10)
	if err != nil {
		log.Printf("[AITaskProcessor] 获取待处理任务失败: %v", err)
		return
	}

	if len(tasks) == 0 {
		return
	}

	log.Printf("========================================")
	log.Printf("[AITaskProcessor] 轮询发现 %d 个待处理任务", len(tasks))
	log.Printf("========================================")

	for _, task := range tasks {
		// 造价文档由 /ai/cost/doc 独立异步流水线处理，勿走绘画队列
		if task.Scene == "ai_cost_doc" {
			log.Printf("[AITaskProcessor] 跳过造价任务（由专用流水线处理）: TaskNo=%s", task.TaskNo)
			continue
		}
		log.Printf("[AITaskProcessor] 准备处理任务: TaskNo=%s, Scene=%s, UserID=%d", task.TaskNo, task.Scene, task.UserID)
		log.Printf("[AITaskProcessor] 任务已原子领取为 processing")

		// 提交任务到请求池
		if err := p.submitTask(task); err != nil {
			log.Printf("[AITaskProcessor] 提交任务失败: TaskNo=%s, Error=%v", task.TaskNo, err)
			// 回退状态为待处理
			p.taskModel.UpdateStatusByTaskNo(task.TaskNo, "pending", "", "")
		}
	}
}

// submitTask 提交任务到请求池
func (p *AITaskProcessor) submitTask(task *model.AITask) error {
	if task != nil && task.Scene == "ai_cost_doc" {
		return fmt.Errorf("ai_cost_doc 不由通用任务处理器执行")
	}

	// 确定任务类型
	taskType := "ai_draw"
	if task.Scene == "ai_chat_single" || task.Scene == "ai_chat_multi" {
		taskType = "ai_chat"
	}
	// 乡村别墅设计、全能设计、建筑换新和亲子设计也是绘画任务
	if task.Scene == "rural_villa_design" || task.Scene == "allround_design" || task.Scene == "building_replacement" || task.Scene == "parent_child_design" {
		taskType = "ai_draw"
	}
	log.Printf("[AITaskProcessor] 任务场景: %s -> 任务类型: %s", task.Scene, taskType)

	// 解析请求载荷
	var payload map[string]interface{}
	if task.RequestPayload != "" {
		if err := json.Unmarshal([]byte(task.RequestPayload), &payload); err != nil {
			p.taskModel.UpdateStatusByTaskNo(task.TaskNo, "failed", "", safeerror.SanitizeAIGenerationError("解析请求参数失败: "+err.Error()))
			return err
		}
	} else {
		payload = make(map[string]interface{})
	}

	// 提取提示词和图片URL（支持单图和多图）
	prompt := ""
	imageURL := ""
	var imageURLs []string
	if v, ok := payload["prompt"].(string); ok {
		prompt = v
	}
	// 优先检查多图数组（支持多个字段名）
	extractImageArray := func(key string) []string {
		if arr, ok := payload[key].([]interface{}); ok && len(arr) > 0 {
			result := make([]string, 0, len(arr))
			for _, item := range arr {
				if str, ok := item.(string); ok && str != "" {
					result = append(result, str)
				}
			}
			return result
		}
		return nil
	}

	// 按优先级检查：images > image_urls > reference_images
	if urls := extractImageArray("images"); len(urls) > 0 {
		imageURLs = urls
	} else if urls := extractImageArray("image_urls"); len(urls) > 0 {
		imageURLs = urls
	} else if urls := extractImageArray("reference_images"); len(urls) > 0 {
		imageURLs = urls
	}
	// 如果没有多图，检查单图
	if len(imageURLs) == 0 {
		if v, ok := payload["image"].(string); ok && v != "" {
			imageURL = v
		}
		if v, ok := payload["image_url"].(string); ok && v != "" {
			imageURL = v
		}
	}

	// AI 绘画任务：若无前缀/后缀则后端兜底拼接「请帮我生成图片，」及画面风格/清晰度/画布大小
	// 乡村别墅设计、全能设计、建筑换新和亲子设计已经有完整的提示词，不需要再次处理
	if taskType == "ai_draw" && task.Scene != "rural_villa_design" && task.Scene != "allround_design" && task.Scene != "building_replacement" && task.Scene != "parent_child_design" && task.Scene != "new_minnan_tool_text_to_image" && task.Scene != "new_minnan_tool_image_to_image" {
		prompt = BuildAIDrawPrompt(payload)
	}

	log.Printf("[AITaskProcessor] 提示词: %s", prompt)
	if len(imageURLs) > 0 {
		log.Printf("[AITaskProcessor] 多图URL数量: %d", len(imageURLs))
	} else {
		log.Printf("[AITaskProcessor] 单图URL: %s", imageURL)
	}

	// 直接使用内置配置（有图片就传 true，无论是单图还是多图）
	hasImage := imageURL != "" || len(imageURLs) > 0
	apiConfigData := p.getConfiguredAIConfig(taskType, hasImage)
	if task.Scene == "new_minnan_tool_text_to_image" {
		apiConfigData = p.getRunningHubTextToImageConfig()
	} else if task.Scene == "new_minnan_tool_image_to_image" {
		apiConfigData = p.getRunningHubImageToImageConfig()
	}
	if apiConfigData == nil {
		errMsg := "不支持的任务类型: " + taskType
		log.Printf("[AITaskProcessor] %s", errMsg)
		p.taskModel.UpdateStatusByTaskNo(task.TaskNo, "failed", "", safeerror.SanitizeAIGenerationError(errMsg))
		return fmt.Errorf("%s", errMsg)
	}
	fallbackConfigs := p.getFallbackAIConfigs(taskType, hasImage, apiConfigData)
	requestedGenerateCount := getRequestedGenerateCountFromTask(task)
	primaryModel := resolveConfiguredModel(apiConfigData)
	log.Printf("[AITaskProcessor] 任务执行计划: requested_generate_count=%d, has_reference_images=%t, primary_model=%s, fallback_count=%d", requestedGenerateCount, hasImage, primaryModel, len(fallbackConfigs))

	taskCtx := CreateAITaskContext(
		task.TaskNo,
		task.UserID,
		taskType,
		apiConfigData,
		fallbackConfigs,
		payload,
		prompt,
		imageURL,
		imageURLs,
	)

	// 提交到请求池
	return p.requestPool.SubmitAITask(taskCtx, func(result *AITaskResult) {
		p.handleTaskResult(task, result)
	})
}

func (p *AITaskProcessor) getRunningHubTextToImageConfig() *AIAPIConfigData {
	if p == nil || p.cfg == nil {
		return nil
	}
	apiKey := strings.TrimSpace(p.cfg.AI.RunningHubAPIKey)
	appID := strings.TrimSpace(p.cfg.AI.RunningHubTextToImageAppID)
	if apiKey == "" || appID == "" {
		return nil
	}
	instanceType := strings.TrimSpace(p.cfg.AI.RunningHubInstanceType)
	if instanceType == "" {
		instanceType = "default"
	}
	usePersonalQueue := strings.TrimSpace(p.cfg.AI.RunningHubUsePersonalQueue)
	if usePersonalQueue == "" {
		usePersonalQueue = "false"
	}
	bodyTemplate := fmt.Sprintf(`{"nodeInfoList":[{"nodeId":"19","fieldName":"text","fieldValue":"{{prompt}}","description":"text"},{"nodeId":"12","fieldName":"height","fieldValue":"{{height}}","description":"height"},{"nodeId":"12","fieldName":"width","fieldValue":"{{width}}","description":"width"},{"nodeId":"12","fieldName":"batch_size","fieldValue":"{{batch_size}}","description":"num"}],"instanceType":%q,"usePersonalQueue":%q}`, instanceType, usePersonalQueue)
	return &AIAPIConfigData{
		TaskType:       "ai_draw",
		ProviderCode:   "runninghub",
		ProviderName:   "RunningHub",
		ProtocolType:   "runninghub_async",
		APIEndpoint:    "https://www.runninghub.cn/openapi/v2/run/ai-app/" + appID,
		Method:         "POST",
		APIKey:         apiKey,
		APIKeyLocation: "header_bearer",
		Headers:        `{"Content-Type":"application/json"}`,
		BodyTemplate:   bodyTemplate,
	}
}

func (p *AITaskProcessor) getRunningHubImageToImageConfig() *AIAPIConfigData {
	if p == nil || p.cfg == nil {
		return nil
	}
	apiKey := strings.TrimSpace(p.cfg.AI.RunningHubAPIKey)
	appID := strings.TrimSpace(p.cfg.AI.RunningHubImageToImageAppID)
	if apiKey == "" || appID == "" {
		return nil
	}
	instanceType := strings.TrimSpace(p.cfg.AI.RunningHubInstanceType)
	if instanceType == "" {
		instanceType = "default"
	}
	usePersonalQueue := strings.TrimSpace(p.cfg.AI.RunningHubUsePersonalQueue)
	if usePersonalQueue == "" {
		usePersonalQueue = "false"
	}
	bodyTemplate := fmt.Sprintf(`{"nodeInfoList":[{"nodeId":"76","fieldName":"image","fieldValue":"{{original_image}}","description":"原图"},{"nodeId":"118","fieldName":"image","fieldValue":"{{reference_image}}","description":"参考图"},{"nodeId":"111","fieldName":"batch_size","fieldValue":"{{batch_size}}","description":"生成数量"},{"nodeId":"108","fieldName":"text","fieldValue":"{{prompt}}","description":"提示词"}],"instanceType":%q,"usePersonalQueue":%q}`, instanceType, usePersonalQueue)
	return &AIAPIConfigData{
		TaskType:       "ai_draw",
		ProviderCode:   "runninghub",
		ProviderName:   "RunningHub",
		ProtocolType:   "runninghub_async",
		APIEndpoint:    "https://www.runninghub.cn/openapi/v2/run/ai-app/" + appID,
		Method:         "POST",
		APIKey:         apiKey,
		APIKeyLocation: "header_bearer",
		Headers:        `{"Content-Type":"application/json"}`,
		BodyTemplate:   bodyTemplate,
	}
}

func normalizeRequestedGenerateCount(value interface{}) int64 {
	switch current := value.(type) {
	case float64:
		count := int64(current)
		if count < 1 {
			return 1
		}
		if count > 3 {
			return 3
		}
		return count
	case int:
		count := int64(current)
		if count < 1 {
			return 1
		}
		if count > 3 {
			return 3
		}
		return count
	case int64:
		if current < 1 {
			return 1
		}
		if current > 3 {
			return 3
		}
		return current
	default:
		return 1
	}
}

func getGeminiOpenAICompatibleDrawBodyTemplate(hasImage bool) string {
	if hasImage {
		return `{
  "model": "gemini-3.1-flash-image-preview",
  "stream": false,
  "image_size": "{{image_size}}",
  "aspect_ratio": "{{aspect_ratio}}",
  "size": "{{image_size}}",
  "messages": [{
    "role": "user",
    "content": [
      { "type": "text", "text": "{{prompt}}" },
      { "type": "image_url", "image_url": { "url": "{{image_url}}" } }
    ]
  }]
}`
	}
	return `{
  "model": "gemini-3.1-flash-image-preview",
  "stream": false,
  "image_size": "{{image_size}}",
  "aspect_ratio": "{{aspect_ratio}}",
  "size": "{{image_size}}",
  "messages": [{
    "role": "user",
    "content": [
      { "type": "text", "text": "{{prompt}}" }
    ]
  }]
}`
}

func (p *AITaskProcessor) getConfiguredAIConfig(taskType string, hasImage bool) *AIAPIConfigData {
	if p.apiConfigModel != nil {
		config, err := p.apiConfigModel.GetByTaskType(taskType)
		if err == nil && config != nil {
			apiConfigData := &AIAPIConfigData{
				TaskType:                 config.TaskType,
				ProviderCode:             strings.TrimSpace(config.ProviderCode),
				ProviderName:             strings.TrimSpace(config.ProviderName),
				ProtocolType:             strings.TrimSpace(config.ProtocolType),
				APIEndpoint:              strings.TrimSpace(config.APIEndpoint),
				Method:                   strings.TrimSpace(config.Method),
				APIKey:                   strings.TrimSpace(config.APIKey),
				APIKeyLocation:           strings.TrimSpace(config.APIKeyLocation),
				APIKeyName:               strings.TrimSpace(config.APIKeyName),
				Headers:                  config.Headers,
				BodyTemplate:             config.BodyTemplate,
				EnablePromptOptimization: config.EnablePromptOptimization,
			}
			if apiConfigData.Method == "" {
				apiConfigData.Method = "POST"
			}
			if apiConfigData.ProviderCode == "" {
				apiConfigData.ProviderCode = "laozhang"
			}
			if apiConfigData.ProviderName == "" {
				apiConfigData.ProviderName = apiConfigData.ProviderCode
			}
			if apiConfigData.ProtocolType == "" {
				if apiConfigData.ProviderCode == "toapis" {
					apiConfigData.ProtocolType = "toapis_async"
				} else {
					apiConfigData.ProtocolType = "gemini_sync"
				}
			}
			if apiConfigData.APIKeyLocation == "" {
				if apiConfigData.APIKey != "" {
					apiConfigData.APIKeyLocation = "header_bearer"
				} else {
					apiConfigData.APIKeyLocation = "none"
				}
			}
			if apiConfigData.APIKeyLocation == "header_bearer" && apiConfigData.APIKeyName == "" {
				apiConfigData.APIKeyName = "Authorization"
			}
			if apiConfigData.APIKeyLocation == "header_custom" && apiConfigData.APIKeyName == "" {
				apiConfigData.APIKeyName = "X-API-Key"
			}
			if (apiConfigData.APIKeyLocation == "query" || apiConfigData.APIKeyLocation == "body") && apiConfigData.APIKeyName == "" {
				apiConfigData.APIKeyName = "api_key"
			}
			apiConfigData = p.applyRuntimeAIDrawProviderDefaults(apiConfigData, taskType, hasImage)
			apiConfigData = p.normalizeRuntimeAIConfigData(apiConfigData, taskType, hasImage)
			if apiConfigData.APIEndpoint != "" && apiConfigData.Method != "" && apiConfigData.BodyTemplate != "" {
				log.Printf("[AITaskProcessor] 使用数据库配置: %s -> %s", taskType, apiConfigData.APIEndpoint)
				return apiConfigData
			}
			log.Printf("[AITaskProcessor] 数据库中的 %s 配置不完整，回退到内置配置", taskType)
		} else if err != nil {
			log.Printf("[AITaskProcessor] 读取数据库配置失败，回退到内置配置: taskType=%s, err=%v", taskType, err)
		}
	}
	return p.getHardcodedAIConfig(taskType, hasImage)
}

func (p *AITaskProcessor) normalizeRuntimeAIConfigData(apiConfigData *AIAPIConfigData, taskType string, hasImage bool) *AIAPIConfigData {
	if apiConfigData == nil {
		return nil
	}
	if taskType != "ai_draw" {
		return apiConfigData
	}
	providerCode := strings.ToLower(strings.TrimSpace(apiConfigData.ProviderCode))
	if providerCode != "" && providerCode != "default" && providerCode != "laozhang" && providerCode != "gpt-image-2-vip" {
		return apiConfigData
	}
	endpoint := strings.ToLower(strings.TrimSpace(apiConfigData.APIEndpoint))
	cloned := *apiConfigData
	mutated := false
	if strings.Contains(endpoint, "/v1/chat/completions") {
		cloned.APIEndpoint = p.laoZhangGeminiDrawEndpoint()
		cloned.Method = "POST"
		cloned.BodyTemplate = getGeminiDrawBodyTemplate(hasImage)
		mutated = true
		log.Printf("[AITaskProcessor] 检测到数据库 ai_draw 主配置误用了 chat/completions，已在运行时切换为 generateContent 主接口")
	}
	endpoint = strings.ToLower(strings.TrimSpace(cloned.APIEndpoint))
	bodyTemplate := strings.ToLower(cloned.BodyTemplate)
	if !strings.Contains(endpoint, "gemini-3-pro-image-preview:generatecontent") {
		if mutated {
			return &cloned
		}
		return apiConfigData
	}
	if hasImage && !strings.Contains(bodyTemplate, "inline_data") && !strings.Contains(bodyTemplate, "{{image}}") && !strings.Contains(bodyTemplate, "{{image_url}}") {
		cloned.BodyTemplate = getGeminiDrawBodyTemplate(true)
		mutated = true
		log.Printf("[AITaskProcessor] 检测到数据库 ai_draw 配置仍为纯文字模板，已在运行时切换为带参考图模板")
	}
	if !hasImage && strings.Contains(bodyTemplate, "\"messages\"") {
		cloned.BodyTemplate = getGeminiDrawBodyTemplate(false)
		mutated = true
		log.Printf("[AITaskProcessor] 检测到数据库 ai_draw 请求体仍为聊天模板，已在运行时切换为 Gemini 生图模板")
	}
	if mutated {
		return &cloned
	}
	return apiConfigData
}

func (p *AITaskProcessor) applyRuntimeAIDrawProviderDefaults(apiConfigData *AIAPIConfigData, taskType string, hasImage bool) *AIAPIConfigData {
	if apiConfigData == nil || taskType != "ai_draw" {
		return apiConfigData
	}
	providerCode := strings.ToLower(strings.TrimSpace(apiConfigData.ProviderCode))
	protocolType := strings.ToLower(strings.TrimSpace(apiConfigData.ProtocolType))
	endpoint := strings.ToLower(strings.TrimSpace(apiConfigData.APIEndpoint))
	if providerCode == "" {
		if protocolType == "toapis_async" || strings.Contains(endpoint, "toapis.com") {
			providerCode = "toapis"
		} else {
			providerCode = "gpt-image-2-vip"
		}
	}

	cloned := *apiConfigData
	switch providerCode {
	case "gpt-image-2-vip":
		cloned.ProviderCode = "gpt-image-2-vip"
		cloned.ProviderName = "GPT-Image-2 VIP"
		cloned.ProtocolType = "openai_image_sync"
		cloned.APIEndpoint = p.laoZhangImageGenerationEndpoint()
		cloned.Method = "POST"
		cloned.APIKey = p.getLaoZhangAPIKey()
		cloned.APIKeyLocation = "header_bearer"
		cloned.APIKeyName = "Authorization"
		cloned.Headers = `{"Content-Type": "application/json"}`
		cloned.BodyTemplate = getLaoZhangGPTImage2VipDrawBodyTemplate(p.laoZhangImageModel(), hasImage)
	case "toapis":
		cloned.ProviderCode = "toapis"
		cloned.ProviderName = "ToAPIs"
		cloned.ProtocolType = "toapis_async"
		cloned.APIEndpoint = toapisImageGenerationEndpoint
		cloned.Method = "POST"
		cloned.APIKey = p.getToAPIsAPIKey()
		cloned.APIKeyLocation = "header_bearer"
		cloned.APIKeyName = "Authorization"
		cloned.Headers = `{"Content-Type": "application/json"}`
		cloned.BodyTemplate = getToAPIsGeminiProDrawBodyTemplate()
	case "laozhang":
		cloned.ProviderCode = "laozhang"
		cloned.ProviderName = "老张 API"
		cloned.ProtocolType = "gemini_sync"
		cloned.APIEndpoint = p.laoZhangGeminiDrawEndpoint()
		cloned.Method = "POST"
		cloned.APIKey = p.getLaoZhangAPIKey()
		cloned.APIKeyLocation = "header_bearer"
		cloned.APIKeyName = "Authorization"
		cloned.Headers = `{"Content-Type": "application/json"}`
		cloned.BodyTemplate = getGeminiDrawBodyTemplate(hasImage)
	default:
		return apiConfigData
	}
	return &cloned
}

func getGeminiDrawBodyTemplate(hasImage bool) string {
	if hasImage {
		return `{
  "contents": [{
    "parts": [
      { "text": "{{prompt}}" },
      { "inline_data": { "mime_type": "{{image_mime_type}}", "data": "{{image}}" } }
    ]
  }],
  "generationConfig": {
    "responseModalities": ["IMAGE"],
    "imageConfig": {
      "aspectRatio": "{{aspect_ratio}}",
      "imageSize": "{{image_size}}"
    }
  }
}`
	}
	return `{
  "contents": [{
    "parts": [
      { "text": "{{prompt}}" }
    ]
  }],
  "generationConfig": {
    "responseModalities": ["IMAGE"],
    "imageConfig": {
      "aspectRatio": "{{aspect_ratio}}",
      "imageSize": "{{image_size}}"
    }
  }
}`
}

func getGeminiFlashDrawBodyTemplate(hasImage bool) string {
	return getGeminiDrawBodyTemplate(hasImage)
}

func getToAPIsGeminiProDrawBodyTemplate() string {
	return `{"model":"gemini-3-pro-image-preview","prompt":"{{prompt}}","size":"{{aspect_ratio}}","n":1,"metadata":{"resolution":"{{image_size}}"}}`
}

func getToAPIsGeminiFlashDrawBodyTemplate() string {
	return `{"model":"gemini-3.1-flash-image-preview","prompt":"{{prompt}}","size":"{{aspect_ratio}}","n":1,"metadata":{"resolution":"{{image_size}}"}}`
}

func getLaoZhangGPTImage2VipDrawBodyTemplate(model string, hasImage bool) string {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultLaoZhangImageModel
	}
	if hasImage {
		return fmt.Sprintf(`{"model":%q,"prompt":"{{prompt}}","image":"{{image_url}}","size":"{{image_size}}","response_format":"b64_json","n":1}`, model)
	}
	return fmt.Sprintf(`{"model":%q,"prompt":"{{prompt}}","size":"{{image_size}}","response_format":"b64_json","n":1}`, model)
}

func getToAPIsGPTImage2DrawBodyTemplate() string {
	return `{"model":"gpt-image-2","prompt":"{{prompt}}","size":"{{image_size}}","response_format":"url","n":1}`
}

func getToAPIsSeedreamDrawBodyTemplate() string {
	return `{"model":"seedream-4-5-251128","prompt":"{{prompt}}","sequential_image_generation":"disabled","response_format":"url","size":"{{image_size}}","stream":false,"watermark":false}`
}

func getLaoZhangSeedreamDrawBodyTemplate(hasImage bool) string {
	if hasImage {
		return `{"model":"seedream-4-5-251128","prompt":"{{prompt}}","image":"{{image_url}}","sequential_image_generation":"disabled","response_format":"b64_json","size":"{{image_size}}","stream":false,"watermark":false}`
	}
	return `{"model":"seedream-4-5-251128","prompt":"{{prompt}}","sequential_image_generation":"disabled","response_format":"b64_json","size":"{{image_size}}","stream":false,"watermark":false}`
}

// getHardcodedAIConfig 获取硬编码的AI配置（当数据库中没有配置时使用）
func (p *AITaskProcessor) getHardcodedAIConfig(taskType string, hasImage bool) *AIAPIConfigData {
	// 从配置中获取老张平台 API Key
	apiKey := p.getLaoZhangAPIKey()
	if apiKey != "" {
		log.Printf("[AITaskProcessor] 使用环境配置中的 API Key")
	} else {
		log.Printf("[AITaskProcessor] 当前未配置 LAOZHANG_API_KEY，将按无 Key 方式请求")
	}

	if taskType == "ai_draw" {
		if hasImage {
			log.Printf("[AITaskProcessor] 使用内置生图配置: model=%s, endpoint=%s, has_reference_images=true", p.laoZhangImageModel(), p.laoZhangImageGenerationEndpoint())
			return &AIAPIConfigData{
				TaskType:       "ai_draw",
				ProviderCode:   "gpt-image-2-vip",
				ProviderName:   "GPT-Image-2 VIP",
				ProtocolType:   "openai_image_sync",
				APIEndpoint:    p.laoZhangImageGenerationEndpoint(),
				Method:         "POST",
				APIKey:         apiKey,
				APIKeyLocation: "header_bearer",
				APIKeyName:     "Authorization",
				Headers:        `{"Content-Type": "application/json"}`,
				BodyTemplate:   getLaoZhangGPTImage2VipDrawBodyTemplate(p.laoZhangImageModel(), true),
			}
		}

		log.Printf("[AITaskProcessor] 使用内置生图配置: model=%s, endpoint=%s, has_reference_images=false", p.laoZhangImageModel(), p.laoZhangImageGenerationEndpoint())
		return &AIAPIConfigData{
			TaskType:       "ai_draw",
			ProviderCode:   "gpt-image-2-vip",
			ProviderName:   "GPT-Image-2 VIP",
			ProtocolType:   "openai_image_sync",
			APIEndpoint:    p.laoZhangImageGenerationEndpoint(),
			Method:         "POST",
			APIKey:         apiKey,
			APIKeyLocation: "header_bearer",
			APIKeyName:     "Authorization",
			Headers:        `{"Content-Type": "application/json"}`,
			BodyTemplate:   getLaoZhangGPTImage2VipDrawBodyTemplate(p.laoZhangImageModel(), false),
		}
	} else if taskType == "ai_chat" {
		// AI聊天配置（仍使用 chat/completions）
		log.Printf("[AITaskProcessor] 使用硬编码配置: AI聊天")
		return &AIAPIConfigData{
			TaskType:       "ai_chat",
			ProviderCode:   "default",
			ProviderName:   "默认聊天接口",
			ProtocolType:   "chat_sync",
			APIEndpoint:    "https://api.laozhang.ai/v1/chat/completions",
			Method:         "POST",
			APIKey:         apiKey,
			APIKeyLocation: "header_bearer",
			APIKeyName:     "Authorization",
			Headers:        `{"Content-Type": "application/json"}`,
			BodyTemplate:   `{"model":"gemini-3-pro-image-preview","stream":false,"messages":[{"role":"user","content":"{{prompt}}"}]}`,
		}
	}

	return nil
}

func (p *AITaskProcessor) getFallbackAIConfigs(taskType string, hasImage bool, primaryConfig *AIAPIConfigData) []*AIAPIConfigData {
	if taskType != "ai_draw" {
		return nil
	}

	if isToAPIsAsyncConfig(primaryConfig) {
		return p.getToAPIsFallbackAIConfigs(primaryConfig)
	}

	apiKey := p.getLaoZhangAPIKey()
	fallbacks := make([]*AIAPIConfigData, 0, 2)
	fallbacks = append(fallbacks, &AIAPIConfigData{
		TaskType:       "ai_draw",
		ProviderCode:   "laozhang",
		ProviderName:   "Gemini 3 Pro Image fallback",
		ProtocolType:   "gemini_sync",
		APIEndpoint:    p.laoZhangGeminiDrawEndpoint(),
		Method:         "POST",
		APIKey:         apiKey,
		APIKeyLocation: "header_bearer",
		APIKeyName:     "Authorization",
		Headers:        `{"Content-Type": "application/json"}`,
		BodyTemplate:   getGeminiDrawBodyTemplate(hasImage),
	})
	fallbacks = append(fallbacks, &AIAPIConfigData{
		TaskType:       "ai_draw",
		ProviderCode:   "laozhang",
		ProviderName:   "Seedream 4.5 second fallback",
		ProtocolType:   "openai_image_sync",
		APIEndpoint:    p.laoZhangImageGenerationEndpoint(),
		Method:         "POST",
		APIKey:         apiKey,
		APIKeyLocation: "header_bearer",
		APIKeyName:     "Authorization",
		Headers:        `{"Content-Type": "application/json"}`,
		BodyTemplate:   getLaoZhangSeedreamDrawBodyTemplate(hasImage),
	})

	return fallbacks
}

func (p *AITaskProcessor) getToAPIsFallbackAIConfigs(primaryConfig *AIAPIConfigData) []*AIAPIConfigData {
	if primaryConfig == nil {
		return nil
	}
	primaryModel := strings.ToLower(strings.TrimSpace(resolveConfiguredModel(primaryConfig)))
	fallbacks := make([]*AIAPIConfigData, 0, 2)
	candidates := []struct {
		providerName string
		bodyTemplate string
	}{
		{"ToAPIs GPT-Image-2 fallback", getToAPIsGPTImage2DrawBodyTemplate()},
		{"ToAPIs Gemini 3.1 Flash second fallback", getToAPIsGeminiFlashDrawBodyTemplate()},
	}
	for _, candidate := range candidates {
		fallback := cloneToAPIsFallbackConfig(primaryConfig, candidate.providerName, candidate.bodyTemplate)
		if fallback == nil {
			continue
		}
		if primaryModel != "" && strings.ToLower(strings.TrimSpace(resolveConfiguredModel(fallback))) == primaryModel {
			continue
		}
		fallbacks = append(fallbacks, fallback)
	}
	return fallbacks
}

func cloneToAPIsFallbackConfig(primaryConfig *AIAPIConfigData, providerName string, bodyTemplate string) *AIAPIConfigData {
	fallback := cloneAIAPIConfigData(primaryConfig)
	if fallback == nil {
		return nil
	}
	fallback.ProviderCode = "toapis"
	fallback.ProviderName = providerName
	fallback.ProtocolType = "toapis_async"
	if strings.TrimSpace(fallback.APIEndpoint) == "" {
		fallback.APIEndpoint = toapisImageGenerationEndpoint
	}
	fallback.BodyTemplate = bodyTemplate
	return fallback
}

func (p *AITaskProcessor) getLaoZhangAPIKey() string {
	if p.cfg == nil {
		return ""
	}
	apiKey := strings.TrimSpace(p.cfg.AI.LaoZhangAPIKey)
	if apiKey == "" {
		log.Printf("[AITaskProcessor] ⚠ 未配置 LAOZHANG_API_KEY")
	}
	return apiKey
}

func (p *AITaskProcessor) laoZhangImageBaseURL() string {
	if p.cfg == nil {
		return defaultLaoZhangImageBaseURL
	}
	baseURL := strings.TrimRight(strings.TrimSpace(p.cfg.AI.LaoZhangImageBaseURL), "/")
	if baseURL == "" {
		return defaultLaoZhangImageBaseURL
	}
	return baseURL
}

func (p *AITaskProcessor) laoZhangImageModel() string {
	if p.cfg == nil || strings.TrimSpace(p.cfg.AI.LaoZhangImageModel) == "" {
		return defaultLaoZhangImageModel
	}
	return strings.TrimSpace(p.cfg.AI.LaoZhangImageModel)
}

func (p *AITaskProcessor) laoZhangImageGenerationEndpoint() string {
	return p.laoZhangImageBaseURL() + "/v1/images/generations"
}

func (p *AITaskProcessor) laoZhangGeminiDrawEndpoint() string {
	return p.laoZhangImageBaseURL() + "/v1beta/models/gemini-3-pro-image-preview:generateContent"
}

func (p *AITaskProcessor) getToAPIsAPIKey() string {
	if p.cfg == nil {
		return ""
	}
	apiKey := strings.TrimSpace(p.cfg.AI.ToAPIsAPIKey)
	if apiKey == "" {
		log.Printf("[AITaskProcessor] ⚠ 未配置 TOAPIS_API_KEY")
	}
	return apiKey
}

func getRequestedGenerateCountFromTask(task *model.AITask) int64 {
	if task == nil || strings.TrimSpace(task.RequestPayload) == "" {
		return 1
	}
	var requestBody map[string]interface{}
	if err := json.Unmarshal([]byte(task.RequestPayload), &requestBody); err != nil {
		return 1
	}
	if payload, ok := requestBody["payload"].(map[string]interface{}); ok {
		if value, exists := payload["generate_count"]; exists {
			return normalizeRequestedGenerateCount(value)
		}
	}
	value, ok := requestBody["generate_count"]
	if !ok {
		return 1
	}
	return normalizeRequestedGenerateCount(value)
}

func getGeneratedImageCountFromResult(result *AITaskResult) int64 {
	if result == nil {
		return 0
	}
	count := int64(len(result.ResultURLs))
	if count > 0 {
		return count
	}
	if strings.TrimSpace(result.ResultURL) != "" {
		return 1
	}
	return 0
}

// handleTaskResult 处理任务结果
func (p *AITaskProcessor) handleTaskResult(task *model.AITask, result *AITaskResult) {
	log.Printf("========================================")
	log.Printf("[AITaskProcessor] 📨 收到任务执行结果回调")
	log.Printf("[AITaskProcessor] 任务编号: %s", task.TaskNo)
	log.Printf("[AITaskProcessor] 执行结果: %v", result.Success)
	log.Printf("[AITaskProcessor] 执行耗时: %v", result.Duration)

	if result.Success {
		log.Printf("[AITaskProcessor] ✅ 任务执行成功!")
		requestedCount := getRequestedGenerateCountFromTask(task)
		generatedCount := getGeneratedImageCountFromResult(result)
		if requestedCount < 1 {
			requestedCount = 1
		}
		if generatedCount < 0 {
			generatedCount = 0
		}
		if generatedCount > requestedCount {
			generatedCount = requestedCount
		}
		settledStones := task.StonesUsed
		refundImageCount := int64(0)
		refundedStones := int64(0)
		if requestedCount > 0 && generatedCount < requestedCount && task.StonesUsed > 0 {
			unitStones := task.StonesUsed / requestedCount
			refundImageCount = requestedCount - generatedCount
			refundedStones = unitStones * refundImageCount
			if refundedStones > 0 {
				settledStones = task.StonesUsed - refundedStones
				log.Printf("[AITaskProcessor] 检测到部分出图成功: requested=%d, generated=%d, refund_images=%d, refund_stones=%d", requestedCount, generatedCount, refundImageCount, refundedStones)
				if err := p.userModel.AddStones(task.UserID, refundedStones); err != nil {
					log.Printf("[AITaskProcessor] ❌ 部分退款失败: %v", err)
					settledStones = task.StonesUsed
					refundImageCount = 0
					refundedStones = 0
				} else {
					log.Printf("[AITaskProcessor] ✓ 已退回部分灵石: %d", refundedStones)
					if p.stoneRecordModel != nil {
						_ = p.stoneRecordModel.Create(task.UserID, "task", refundedStones, fmt.Sprintf("部分出图退回-%d张", refundImageCount), "")
					}
				}
			}
		}

		// 构建结果 JSON：url 为带水印（默认返回），url_raw 为原图（按规则下载）
		resultPayload := map[string]interface{}{
			"url":             result.ResultURL,
			"url_raw":         result.ResultURLRaw,
			"duration":        result.Duration.Milliseconds(),
			"requested_count": requestedCount,
			"generated_count": generatedCount,
			"stones_used":     settledStones,
		}
		if strings.TrimSpace(result.ThumbnailURL) != "" {
			resultPayload["thumbnail_url"] = strings.TrimSpace(result.ThumbnailURL)
		}
		if result.UsedModel != "" || result.APIEndpoint != "" || result.ProviderCode != "" || result.ProtocolType != "" || result.ExternalTaskID != "" || len(result.AttemptedModels) > 0 || len(result.AttemptedEndpoints) > 0 {
			resultPayload["execution_meta"] = map[string]interface{}{
				"used_model":          result.UsedModel,
				"api_endpoint":        result.APIEndpoint,
				"provider_code":       result.ProviderCode,
				"provider_name":       result.ProviderName,
				"protocol_type":       result.ProtocolType,
				"external_task_id":    result.ExternalTaskID,
				"external_status":     result.ExternalStatus,
				"attempted_models":    result.AttemptedModels,
				"attempted_endpoints": result.AttemptedEndpoints,
			}
		}
		if len(result.ResultURLs) > 0 {
			resultPayload["images"] = result.ResultURLs
		}
		if len(result.ResultURLsRaw) > 0 {
			resultPayload["raw_images"] = result.ResultURLsRaw
		}
		if len(result.ThumbnailURLs) > 0 {
			resultPayload["thumbnail_urls"] = result.ThumbnailURLs
		}
		if refundedStones > 0 {
			resultPayload["refunded_stones"] = refundedStones
			resultPayload["refunded_image_count"] = refundImageCount
		}
		resultJSON, _ := json.Marshal(resultPayload)

		if err := p.taskModel.UpdateStatusAndMetaByTaskNo(task.TaskNo, "success", string(resultJSON), "", result.UsedModel, result.APIEndpoint); err != nil {
			log.Printf("[AITaskProcessor] ❌ 更新任务成功状态失败: %v", err)
		} else {
			if settledStones != task.StonesUsed {
				if err := p.taskModel.UpdateStonesUsedByTaskNo(task.TaskNo, settledStones); err != nil {
					log.Printf("[AITaskProcessor] ❌ 更新任务实际扣费失败: %v", err)
				} else {
					log.Printf("[AITaskProcessor] ✓ 任务实际扣费已更新为: %d", settledStones)
				}
			}
			log.Printf("[AITaskProcessor] ✓ 任务状态已更新为 success")
			log.Printf("[AITaskProcessor] ✓ 结果URL: %s", result.ResultURL)
		}
	} else {
		log.Printf("[AITaskProcessor] ❌ 任务执行失败!")
		log.Printf("[AITaskProcessor] 错误信息: %s", result.Error)
		safeError := safeerror.SanitizeAIGenerationError(result.Error)
		requestedCount := getRequestedGenerateCountFromTask(task)
		refundedStones := task.StonesUsed

		// 任务失败，回退灵石
		if task.StonesUsed > 0 {
			log.Printf("[AITaskProcessor] 正在回退灵石: UserID=%d, Stones=%d", task.UserID, task.StonesUsed)
			if err := p.userModel.AddStones(task.UserID, task.StonesUsed); err != nil {
				log.Printf("[AITaskProcessor] ❌ 回退灵石失败: %v", err)
				refundedStones = 0
			} else {
				log.Printf("[AITaskProcessor] ✓ 灵石已回退: %d", task.StonesUsed)
				if p.stoneRecordModel != nil {
					_ = p.stoneRecordModel.Create(task.UserID, "task", task.StonesUsed, "任务失败退回", "")
				}
			}
		}

		failurePayload := map[string]interface{}{
			"duration":             result.Duration.Milliseconds(),
			"requested_count":      requestedCount,
			"generated_count":      0,
			"stones_used":          0,
			"refunded_stones":      refundedStones,
			"refunded_image_count": requestedCount,
		}
		if result.UsedModel != "" || result.APIEndpoint != "" || result.ProviderCode != "" || result.ProtocolType != "" || result.ExternalTaskID != "" || len(result.AttemptedModels) > 0 || len(result.AttemptedEndpoints) > 0 {
			failurePayload["execution_meta"] = map[string]interface{}{
				"used_model":          result.UsedModel,
				"api_endpoint":        result.APIEndpoint,
				"provider_code":       result.ProviderCode,
				"provider_name":       result.ProviderName,
				"protocol_type":       result.ProtocolType,
				"external_task_id":    result.ExternalTaskID,
				"external_status":     result.ExternalStatus,
				"attempted_models":    result.AttemptedModels,
				"attempted_endpoints": result.AttemptedEndpoints,
			}
		}
		failureJSON, _ := json.Marshal(failurePayload)

		if err := p.taskModel.UpdateStatusAndMetaByTaskNo(task.TaskNo, "failed", string(failureJSON), safeError, result.UsedModel, result.APIEndpoint); err != nil {
			log.Printf("[AITaskProcessor] ❌ 更新任务失败状态失败: %v", err)
		} else {
			if refundedStones > 0 {
				if err := p.taskModel.UpdateStonesUsedByTaskNo(task.TaskNo, 0); err != nil {
					log.Printf("[AITaskProcessor] ❌ 更新失败任务实际扣费失败: %v", err)
				} else {
					log.Printf("[AITaskProcessor] ✓ 失败任务实际扣费已更新为: 0")
				}
			}
			log.Printf("[AITaskProcessor] ✓ 任务状态已更新为 failed")
		}
	}
	log.Printf("========================================")
}

// ProcessSingleTask 立即处理单个任务（用于测试或手动触发）
func (p *AITaskProcessor) ProcessSingleTask(taskNo string) error {
	task, err := p.taskModel.GetByTaskNo(taskNo)
	if err != nil {
		return err
	}
	return p.submitTask(task)
}

var (
	globalAITaskProcessor *AITaskProcessor
	processorOnce         sync.Once
)

// InitAITaskProcessor 初始化全局AI任务处理器
func InitAITaskProcessor(
	taskModel *model.AITaskModel,
	apiConfigModel *model.AIAPIConfigModel,
	userModel *model.UserRedisModel,
	stoneRecordModel *model.StoneRecordModel,
	requestPool *RequestPool,
	cfg *config.Config,
) *AITaskProcessor {
	processorOnce.Do(func() {
		globalAITaskProcessor = NewAITaskProcessor(taskModel, apiConfigModel, userModel, stoneRecordModel, requestPool, cfg)
		globalAITaskProcessor.Start()
	})
	return globalAITaskProcessor
}

// GetAITaskProcessor 获取全局AI任务处理器
func GetAITaskProcessor() *AITaskProcessor {
	return globalAITaskProcessor
}
