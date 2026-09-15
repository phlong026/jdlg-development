package route

import (
	"database/sql"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"service/model"
	"strings"
)

// AIPricingRequest AI计费配置请求
type AIPricingRequest struct {
	Scene       string                 `json:"scene" binding:"required"`
	Stones      int64                  `json:"stones" binding:"required"`
	ExtraConfig map[string]interface{} `json:"extra_config"`
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
func RegisterAIManagementRoutes(r *gin.RouterGroup, pricingModel *model.AIPricingModel, apiConfigModel *model.AIAPIConfigModel) {
	ai := r.Group("/ai")
	{
		// 获取所有计费配置
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

			if err := pricingModel.Upsert(pricing); err != nil {
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
			var configsWithParsedJSON []gin.H
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
					"api_key":                    config.APIKey,
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

			if validationMsg := validateAIAPIConfigRequest(req.TaskType, req.APIEndpoint, req.ProtocolType, req.BodyTemplate); validationMsg != "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError(validationMsg, "APIEndpoint"),
				})
				return
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
}
