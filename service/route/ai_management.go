package route

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"strings"
)

// AIPricingRequest AI计费配置请求
type AIPricingRequest struct {
	Scene       string                 `json:"scene" binding:"required"`
	Stones      int64                  `json:"stones" binding:"required"`
	ExtraConfig map[string]interface{} `json:"extra_config"`
	Note        string                 `json:"note"` // 调价备注（写入版本记录，可空）
}

// validateAIEndpointSecurity 校验 AI 接口地址的安全性。
// "测试/调用"能力与可配置 endpoint 组合构成 SSRF 向量：地址若可指向内网或
// 攻击者服务器，保存的 API Key 会被原样发往目标。因此要求必须是 https 公网地址，
// 拒绝环回/私网/链路本地目标（域名按解析结果再核一遍）。
func validateAIEndpointSecurity(rawEndpoint string) string {
	u, err := url.Parse(strings.TrimSpace(rawEndpoint))
	if err != nil || u.Host == "" {
		return "接口地址格式无效"
	}
	if u.Scheme != "https" {
		return "接口地址必须使用 https（防止密钥明文出网）"
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return "接口地址不能使用本地域名"
	}
	denyIP := func(ip net.IP) bool {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	if ip := net.ParseIP(host); ip != nil {
		if denyIP(ip) {
			return "接口地址不能指向内网/本机地址"
		}
		return ""
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		// 解析失败不阻塞保存，交给后续真实调用暴露问题
		return ""
	}
	for _, a := range addrs {
		if denyIP(a) {
			return "接口地址解析到内网/本机地址，不允许保存"
		}
	}
	return ""
}

func validateAIAPIConfigRequest(taskType, apiEndpoint, protocolType string, bodyTemplate map[string]interface{}) string {
	if strings.TrimSpace(taskType) != "ai_draw" {
		return ""
	}
	protocolType = strings.TrimSpace(protocolType)
	lowerEndpoint := strings.ToLower(strings.TrimSpace(apiEndpoint))
	if protocolType == "toapis_async" {
		if !strings.Contains(lowerEndpoint, "/v1/images/generations") {
			return "ToAPIs 异步生图配置应使用 /v1/images/generations 接口"
		}
		return ""
	}
	if strings.Contains(lowerEndpoint, "/v1/chat/completions") {
		return "AI绘画主配置不能使用 chat/completions，请改用 generateContent 生图接口"
	}
	if len(bodyTemplate) == 0 {
		return ""
	}
	bodyBytes, err := json.Marshal(bodyTemplate)
	if err != nil {
		return ""
	}
	lowerBody := strings.ToLower(string(bodyBytes))
	if strings.Contains(lowerBody, "\"messages\"") {
		return "AI绘画主配置不能保存为聊天 messages 模板，请改为生图请求体模板"
	}
	return ""
}

// RegisterAIManagementRoutes 注册AI管理后台路由
// aiConfigTestResult 连通性测试的分类结论。
// 不发起真实生成请求（避免花掉灵石与生成费），只验证"地址可达、TLS 正常、
// 鉴权头是否被对端接受"，抓住最常见的配置错误（地址抄错、Key 失效）。
type aiConfigTestResult struct {
	OK         bool   `json:"ok"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Category   string `json:"category"` // ok / network_error / timeout / auth_rejected / request_rejected / server_error / unsafe_endpoint
	Message    string `json:"message"`
}

func registerAIAPIConfigTestRoute(ai *gin.RouterGroup, apiConfigModel *model.AIAPIConfigModel) {
	ai.POST("/api/config/test", func(c *gin.Context) {
		if apiConfigModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "AI 配置服务不可用"})
			return
		}
		var req struct {
			ID           int64  `json:"id"`
			TaskType     string `json:"task_type"`
			ProviderCode string `json:"provider_code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}

		config, err := lookupAIAPIConfig(apiConfigModel, req.ID, req.TaskType, req.ProviderCode)
		if err != nil || config == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "配置不存在"})
			return
		}

		// endpoint 安全校验与保存同源：拒绝内网/非 https 目标，防测试能力被用作 SSRF 探测
		if msg := validateAIEndpointSecurity(config.APIEndpoint); msg != "" {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": aiConfigTestResult{
				Category: "unsafe_endpoint", Message: msg,
			}})
			return
		}

		result := probeAIEndpoint(c.Request.Context(), config)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": result})
	})
}

// lookupAIAPIConfig 按 id 或 (task_type, provider_code) 唯一定位一条配置。
func lookupAIAPIConfig(m *model.AIAPIConfigModel, id int64, taskType, providerCode string) (*model.AIAPIConfig, error) {
	configs, err := m.GetAll()
	if err != nil {
		return nil, err
	}
	for _, cfg := range configs {
		if id > 0 && cfg.ID == id {
			return cfg, nil
		}
		if id == 0 && taskType != "" && providerCode != "" && cfg.TaskType == taskType && cfg.ProviderCode == providerCode {
			return cfg, nil
		}
	}
	return nil, nil
}

// probeAIEndpoint 向配置的接口地址发一个最小 POST 并按响应分类。
// 无论 2xx/4xx/5xx，只要拿到 HTTP 响应就证明"地址与 TLS 正确"；401/403 说明
// 地址对但密钥/鉴权方式配置有误——这正是测试要抓的东西。
func probeAIEndpoint(ctx context.Context, config *model.AIAPIConfig) aiConfigTestResult {
	payload := []byte(`{}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.APIEndpoint, bytes.NewReader(payload))
	if err != nil {
		return aiConfigTestResult{Category: "network_error", Message: "构造请求失败: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	// 与真实调用一致的鉴权头注入（api_key_location）
	key := config.APIKey
	switch config.APIKeyLocation {
	case "header_bearer":
		req.Header.Set("Authorization", "Bearer "+key)
	case "header_custom":
		req.Header.Set(config.APIKeyName, key)
	case "query":
		q := req.URL.Query()
		q.Set(config.APIKeyName, key)
		req.URL.RawQuery = q.Encode()
	case "body":
		// 空 JSON 探测即可，不注入（body 模式的真实模板各供应商不同）
	}

	// 禁止跟随重定向：公网端点可 302 到内网/http 地址，跟随会绕过 endpoint 校验
	// 形成 SSRF 探测，且自定义 Header 形态的 API Key 会被带往重定向目标。
	// ErrUseLastResponse 让 client 直接返回 3xx 响应，由下方状态分支归类提示。
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
			return aiConfigTestResult{Category: "timeout", Message: "10 秒内无响应，请检查地址是否正确"}
		}
		return aiConfigTestResult{Category: "network_error", Message: "网络错误: " + err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return aiConfigTestResult{OK: true, HTTPStatus: resp.StatusCode, Category: "ok", Message: "接口可达且接受了请求"}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return aiConfigTestResult{HTTPStatus: resp.StatusCode, Category: "auth_rejected", Message: fmt.Sprintf("地址可达但鉴权被拒（HTTP %d）：请检查 API Key 是否有效、发送位置配置是否正确", resp.StatusCode)}
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return aiConfigTestResult{HTTPStatus: resp.StatusCode, Category: "request_rejected", Message: fmt.Sprintf("对端返回重定向（HTTP %d）：测试不跟随重定向，请确认配置的是最终接口地址", resp.StatusCode)}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return aiConfigTestResult{HTTPStatus: resp.StatusCode, Category: "request_rejected", Message: fmt.Sprintf("对端拒绝了探测请求（HTTP %d）：地址可达；空请求体被拒属正常，可结合真实任务判断", resp.StatusCode)}
	default:
		return aiConfigTestResult{HTTPStatus: resp.StatusCode, Category: "server_error", Message: fmt.Sprintf("对端服务错误（HTTP %d）：稍后重试或检查供应商状态", resp.StatusCode)}
	}
}

func RegisterAIManagementRoutes(r *gin.RouterGroup, pricingModel *model.AIPricingModel, apiConfigModel *model.AIAPIConfigModel, pricingVersionModel *model.AIPricingVersionModel) {

	ai := r.Group("/ai")
	registerAIAPIConfigTestRoute(ai, apiConfigModel)
	ai.GET("/pricing", func(c *gin.Context) {
			pricings, err := pricingModel.GetAll()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取配置失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": pricings,
			})
		})

		// 创建或更新计费配置
		ai.POST("/pricing", func(c *gin.Context) {
			var req AIPricingRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			// 将ExtraConfig转为JSON字符串
			var extraConfigJSON string
			if req.ExtraConfig != nil {
				configBytes, err := json.Marshal(req.ExtraConfig)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  FormatValidationError("extra_config格式错误: "+err.Error(), "ExtraConfig"),
					})
					return
				}
				extraConfigJSON = string(configBytes)
			}

			pricing := &model.AIPricing{
				Scene:  req.Scene,
				Stones: req.Stones,
				ExtraConfig: sql.NullString{
					String: extraConfigJSON,
					Valid:  extraConfigJSON != "",
				},
			}

			// 调价留痕：先取旧值（Upsert 之后就是新值了），保存成功且定价变化时写版本记录。
			// 版本表初始化失败或写入失败不阻塞保存主流程，只记日志。
			var oldStones int64
			var hasOld bool
			if pricingVersionModel != nil {
				if existing, getErr := pricingModel.GetByScene(req.Scene); getErr == nil && existing != nil {
					oldStones, hasOld = existing.Stones, true
				}
			}

			if err := pricingModel.Upsert(pricing); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "保存配置失败: " + err.Error(),
				})
				return
			}

			if pricingVersionModel != nil {
				if hasOld && oldStones != req.Stones {
					note := strings.TrimSpace(req.Note)
					if len(note) > 200 {
						note = note[:200]
					}
					if recordErr := pricingVersionModel.Record(req.Scene, oldStones, req.Stones, GetUsername(c), note); recordErr != nil {
						fmt.Printf("[WARN] AI定价版本记录写入失败: scene=%s, err=%v\n", req.Scene, recordErr)
					}
				}
			} else {
				fmt.Printf("[WARN] AI定价版本模型不可用，本次调价未留痕: scene=%s\n", req.Scene)
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "保存成功",
			})
		})

		// 获取所有API配置
		ai.GET("/api/config", func(c *gin.Context) {
			configs, err := apiConfigModel.GetAll()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取配置失败: " + err.Error(),
				})
				return
			}

			// 解析JSON字符串为对象
			configsWithParsedJSON := make([]gin.H, 0)
			for _, config := range configs {
				var headers map[string]interface{}
				var bodyTemplate map[string]interface{}

				if config.Headers != "" {
					json.Unmarshal([]byte(config.Headers), &headers)
				}
				if config.BodyTemplate != "" {
					json.Unmarshal([]byte(config.BodyTemplate), &bodyTemplate)
				}

				configsWithParsedJSON = append(configsWithParsedJSON, gin.H{
					"id":                         config.ID,
					"task_type":                  config.TaskType,
					"provider_code":              config.ProviderCode,
					"provider_name":              config.ProviderName,
					"protocol_type":              config.ProtocolType,
					"is_active":                  config.IsActive,
					"api_endpoint":               config.APIEndpoint,
					"method":                     config.Method,
					// 密钥只返回掩码与是否已配置，明文进出浏览器会被内存/缓存/截屏带出
					"api_key":                    model.MaskAPIKey(config.APIKey),
					"api_key_configured":         config.APIKey != "",
					"api_key_location":           config.APIKeyLocation,
					"api_key_name":               config.APIKeyName,
					"headers":                    headers,
					"body_template":              bodyTemplate,
					"prompt_path":                config.PromptPath,
					"enable_prompt_optimization": config.EnablePromptOptimization,
					"image_path":                 config.ImagePath,
					"created_at":                 config.CreatedAt,
					"updated_at":                 config.UpdatedAt,
				})
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": configsWithParsedJSON,
			})
		})

		// 创建或更新API配置
		ai.POST("/api/config", func(c *gin.Context) {
			var req struct {
				ID                       int64                  `json:"id"`
				TaskType                 string                 `json:"task_type" binding:"required"` // ai_draw 或 ai_chat
				ProviderCode             string                 `json:"provider_code"`
				ProviderName             string                 `json:"provider_name"`
				ProtocolType             string                 `json:"protocol_type"`
				IsActive                 bool                   `json:"is_active"`
				APIEndpoint              string                 `json:"api_endpoint" binding:"required"`
				Method                   string                 `json:"method" binding:"required"` // GET, POST, PUT等
				APIKey                   string                 `json:"api_key"`
				APIKeyLocation           string                 `json:"api_key_location"`
				APIKeyName               string                 `json:"api_key_name"`
				Headers                  map[string]interface{} `json:"headers"`
				BodyTemplate             map[string]interface{} `json:"body_template"`
				PromptPath               string                 `json:"prompt_path"`                // 提示词在JSON中的路径
				EnablePromptOptimization bool                   `json:"enable_prompt_optimization"` // 是否开启提示词优化
				ImagePath                string                 `json:"image_path"`                 // 用户图片在JSON中的路径
			}

			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			// 验证task_type
			if req.TaskType != "ai_draw" && req.TaskType != "ai_chat" {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("task_type必须是ai_draw或ai_chat", "TaskType"),
				})
				return
			}
			if req.ProviderCode == "" {
				if req.TaskType == "ai_draw" {
					req.ProviderCode = "laozhang"
				} else {
					req.ProviderCode = "default"
				}
			}
			if req.ProtocolType == "" {
				switch req.ProviderCode {
				case "toapis":
					req.ProtocolType = "toapis_async"
				case "laozhang":
					req.ProtocolType = "gemini_sync"
				default:
					req.ProtocolType = "sync"
				}
			}
			switch req.ProtocolType {
			case "gemini_sync", "toapis_async", "chat_sync", "sync":
			default:
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("protocol_type取值无效", "ProtocolType"),
				})
				return
			}

			if req.APIKeyLocation == "" {
				req.APIKeyLocation = "none"
			}
			switch req.APIKeyLocation {
			case "none", "header_bearer", "header_custom", "query", "body":
			default:
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("api_key_location取值无效", "APIKeyLocation"),
				})
				return
			}
			if req.APIKeyLocation == "header_bearer" && req.APIKeyName == "" {
				req.APIKeyName = "Authorization"
			}
			if req.APIKeyLocation == "header_custom" && req.APIKeyName == "" {
				req.APIKeyName = "X-API-Key"
			}
			if (req.APIKeyLocation == "query" || req.APIKeyLocation == "body") && req.APIKeyName == "" {
				req.APIKeyName = "api_key"
			}

			// 将Headers和BodyTemplate转为JSON字符串
			var headersJSON string
			if req.Headers != nil {
				headersBytes, err := json.Marshal(req.Headers)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  FormatValidationError("headers格式错误: "+err.Error(), "Headers"),
					})
					return
				}
				headersJSON = string(headersBytes)
			}

			var bodyTemplateJSON string
			if req.BodyTemplate != nil {
				bodyBytes, err := json.Marshal(req.BodyTemplate)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  FormatValidationError("body_template格式错误: "+err.Error(), "BodyTemplate"),
					})
					return
				}
				bodyTemplateJSON = string(bodyBytes)
			}

			if securityMsg := validateAIEndpointSecurity(req.APIEndpoint); securityMsg != "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError(securityMsg, "APIEndpoint"),
				})
				return
			}
			if validationMsg := validateAIAPIConfigRequest(req.TaskType, req.APIEndpoint, req.ProtocolType, req.BodyTemplate); validationMsg != "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError(validationMsg, "APIEndpoint"),
				})
				return
			}

			// 空密钥不覆盖：列表接口只返回掩码，前端编辑时留空表示"保持原密钥不变"，
			// 此处回填数据库中的原值，避免掩码或空串被写回配置
			if strings.TrimSpace(req.APIKey) == "" {
				existing, err := apiConfigModel.GetExistingAPIKey(req.ID, req.TaskType, req.ProviderCode)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"code": 500,
						"msg":  "读取原密钥失败: " + err.Error(),
					})
					return
				}
				req.APIKey = existing
			}

			apiConfig := &model.AIAPIConfig{
				ID:                       req.ID,
				TaskType:                 req.TaskType,
				ProviderCode:             req.ProviderCode,
				ProviderName:             req.ProviderName,
				ProtocolType:             req.ProtocolType,
				IsActive:                 req.IsActive,
				APIEndpoint:              req.APIEndpoint,
				Method:                   req.Method,
				APIKey:                   req.APIKey,
				APIKeyLocation:           req.APIKeyLocation,
				APIKeyName:               req.APIKeyName,
				Headers:                  headersJSON,
				BodyTemplate:             bodyTemplateJSON,
				PromptPath:               req.PromptPath,
				EnablePromptOptimization: req.EnablePromptOptimization,
				ImagePath:                req.ImagePath,
			}

			if err := apiConfigModel.Upsert(apiConfig); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "保存配置失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "保存成功",
			})
		})
	}
