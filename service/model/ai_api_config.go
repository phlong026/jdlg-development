package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// AIAPIConfig AI API配置
type AIAPIConfig struct {
	ID                       int64     `json:"id" db:"id"`
	TaskType                 string    `json:"task_type" db:"task_type"`                                   // ai_draw 或 ai_chat
	ProviderCode             string    `json:"provider_code" db:"provider_code"`                           // 供应商编码：laozhang、toapis、default
	ProviderName             string    `json:"provider_name" db:"provider_name"`                           // 供应商名称
	ProtocolType             string    `json:"protocol_type" db:"protocol_type"`                           // 协议类型：gemini_sync、toapis_async、chat_sync
	IsActive                 bool      `json:"is_active" db:"is_active"`                                   // 是否当前启用
	APIEndpoint              string    `json:"api_endpoint" db:"api_endpoint"`                             // API接口地址
	Method                   string    `json:"method" db:"method"`                                         // GET, POST, PUT等
	APIKey                   string    `json:"api_key" db:"api_key"`                                       // API Key
	APIKeyLocation           string    `json:"api_key_location" db:"api_key_location"`                     // API Key 发送位置: header_bearer, header_custom, query, body, none
	APIKeyName               string    `json:"api_key_name" db:"api_key_name"`                             // API Key 名称
	Headers                  string    `json:"headers" db:"headers"`                                       // JSON格式的请求头
	BodyTemplate             string    `json:"body_template" db:"body_template"`                           // JSON格式的请求体模板
	PromptPath               string    `json:"prompt_path" db:"prompt_path"`                               // 提示词在JSON中的路径，如 "prompt" 或 "data.prompt"
	EnablePromptOptimization bool      `json:"enable_prompt_optimization" db:"enable_prompt_optimization"` // 是否开启提示词优化
	ImagePath                string    `json:"image_path" db:"image_path"`                                 // 用户图片在JSON中的路径，如 "image" 或 "data.images[0]"
	CreatedAt                time.Time `json:"created_at" db:"created_at"`
	UpdatedAt                time.Time `json:"updated_at" db:"updated_at"`
}

// AIAPIConfigModel AI API配置数据访问层
type AIAPIConfigModel struct {
	DB    *sql.DB
	Redis *redis.Client
	ctx   context.Context
}

type aiAPIConfigScanner interface {
	Scan(dest ...interface{}) error
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func scanAIAPIConfig(scanner aiAPIConfigScanner) (*AIAPIConfig, error) {
	config := &AIAPIConfig{}
	var apiKey sql.NullString
	var apiKeyLocation sql.NullString
	var apiKeyName sql.NullString
	var headers sql.NullString
	var bodyTemplate sql.NullString
	var promptPath sql.NullString
	var imagePath sql.NullString
	var providerCode sql.NullString
	var providerName sql.NullString
	var protocolType sql.NullString
	err := scanner.Scan(
		&config.ID,
		&config.TaskType,
		&providerCode,
		&providerName,
		&protocolType,
		&config.IsActive,
		&config.APIEndpoint,
		&config.Method,
		&apiKey,
		&apiKeyLocation,
		&apiKeyName,
		&headers,
		&bodyTemplate,
		&promptPath,
		&config.EnablePromptOptimization,
		&imagePath,
		&config.CreatedAt,
		&config.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	config.ProviderCode = nullStringValue(providerCode)
	config.ProviderName = nullStringValue(providerName)
	config.ProtocolType = nullStringValue(protocolType)
	config.APIKey = nullStringValue(apiKey)
	config.APIKeyLocation = nullStringValue(apiKeyLocation)
	config.APIKeyName = nullStringValue(apiKeyName)
	config.Headers = nullStringValue(headers)
	config.BodyTemplate = nullStringValue(bodyTemplate)
	config.PromptPath = nullStringValue(promptPath)
	config.ImagePath = nullStringValue(imagePath)
	return config, nil
}

func normalizeAIAPIConfig(config *AIAPIConfig) {
	if config == nil {
		return
	}
	config.TaskType = strings.TrimSpace(config.TaskType)
	config.ProviderCode = strings.TrimSpace(config.ProviderCode)
	config.ProviderName = strings.TrimSpace(config.ProviderName)
	config.ProtocolType = strings.TrimSpace(config.ProtocolType)
	if config.ProviderCode == "" {
		if config.TaskType == "ai_draw" {
			config.ProviderCode = "gpt-image-2-vip"
		} else {
			config.ProviderCode = "default"
		}
	}
	if config.ProviderName == "" {
		switch config.ProviderCode {
		case "gpt-image-2-vip":
			config.ProviderName = "GPT-Image-2 VIP"
		case "laozhang":
			config.ProviderName = "老张 API"
		case "toapis":
			config.ProviderName = "ToAPIs"
		default:
			config.ProviderName = config.ProviderCode
		}
	}
	if config.ProtocolType == "" {
		switch config.ProviderCode {
		case "gpt-image-2-vip":
			config.ProtocolType = "openai_image_sync"
		case "toapis":
			config.ProtocolType = "toapis_async"
		case "laozhang":
			config.ProtocolType = "gemini_sync"
		default:
			config.ProtocolType = "sync"
		}
	}
}

// NewAIAPIConfigModel 创建AI API配置模型
func NewAIAPIConfigModel(db *sql.DB, rdb *redis.Client) *AIAPIConfigModel {
	return &AIAPIConfigModel{
		DB:    db,
		Redis: rdb,
		ctx:   context.Background(),
	}
}

// GetByTaskType 根据任务类型获取API配置（优先从Redis读取）
func (m *AIAPIConfigModel) GetByTaskType(taskType string) (*AIAPIConfig, error) {
	// 先尝试从Redis读取
	key := "ai_api_config:task_type:" + taskType
	val, err := m.Redis.Get(m.ctx, key).Result()
	if err == nil {
		var config AIAPIConfig
		if err := json.Unmarshal([]byte(val), &config); err == nil {
			return &config, nil
		}
	}

	// Redis中没有，从MySQL读取当前启用配置；老数据没有 is_active 时回退第一条。
	query := `SELECT id, task_type, provider_code, provider_name, protocol_type, is_active, api_endpoint, method, api_key, api_key_location, api_key_name, headers, body_template, prompt_path, enable_prompt_optimization, image_path, created_at, updated_at
	          FROM ai_api_config WHERE task_type = ? ORDER BY is_active DESC, id ASC LIMIT 1`
	config, err := scanAIAPIConfig(m.DB.QueryRow(query, taskType))
	if err != nil {
		return nil, err
	}

	// 写入Redis缓存
	data, _ := json.Marshal(config)
	m.Redis.Set(m.ctx, key, data, time.Hour*24)

	return config, nil
}

// GetAll 获取所有API配置
func (m *AIAPIConfigModel) GetAll() ([]*AIAPIConfig, error) {
	query := `SELECT id, task_type, provider_code, provider_name, protocol_type, is_active, api_endpoint, method, api_key, api_key_location, api_key_name, headers, body_template, prompt_path, enable_prompt_optimization, image_path, created_at, updated_at
	          FROM ai_api_config ORDER BY task_type, is_active DESC, id ASC`
	rows, err := m.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []*AIAPIConfig
	for rows.Next() {
		config, err := scanAIAPIConfig(rows)
		if err != nil {
			continue
		}
		configs = append(configs, config)
	}
	return configs, nil
}

// Upsert 创建或更新API配置
func (m *AIAPIConfigModel) Upsert(config *AIAPIConfig) error {
	normalizeAIAPIConfig(config)
	tx, err := m.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if config.IsActive {
		if _, err := tx.Exec(`UPDATE ai_api_config SET is_active = 0 WHERE task_type = ?`, config.TaskType); err != nil {
			return err
		}
	}

	if config.ID > 0 {
		query := `UPDATE ai_api_config SET
			task_type = ?,
			provider_code = ?,
			provider_name = ?,
			protocol_type = ?,
			is_active = ?,
			api_endpoint = ?,
			method = ?,
			api_key = ?,
			api_key_location = ?,
			api_key_name = ?,
			headers = ?,
			body_template = ?,
			prompt_path = ?,
			enable_prompt_optimization = ?,
			image_path = ?,
			updated_at = NOW()
			WHERE id = ?`
		if _, err := tx.Exec(query, config.TaskType, config.ProviderCode, config.ProviderName, config.ProtocolType, config.IsActive, config.APIEndpoint, config.Method,
			config.APIKey, config.APIKeyLocation, config.APIKeyName,
			config.Headers, config.BodyTemplate, config.PromptPath, config.EnablePromptOptimization, config.ImagePath, config.ID); err != nil {
			return err
		}
	} else {
		query := `INSERT INTO ai_api_config (task_type, provider_code, provider_name, protocol_type, is_active, api_endpoint, method, api_key, api_key_location, api_key_name, headers, body_template, prompt_path, enable_prompt_optimization, image_path, created_at, updated_at)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
	          ON DUPLICATE KEY UPDATE 
	          provider_name = VALUES(provider_name),
	          protocol_type = VALUES(protocol_type),
	          is_active = VALUES(is_active),
	          api_endpoint = VALUES(api_endpoint),
	          method = VALUES(method),
	          api_key = VALUES(api_key),
	          api_key_location = VALUES(api_key_location),
	          api_key_name = VALUES(api_key_name),
	          headers = VALUES(headers),
	          body_template = VALUES(body_template),
	          prompt_path = VALUES(prompt_path),
	          enable_prompt_optimization = VALUES(enable_prompt_optimization),
	          image_path = VALUES(image_path),
	          updated_at = NOW()`
		if _, err := tx.Exec(query, config.TaskType, config.ProviderCode, config.ProviderName, config.ProtocolType, config.IsActive, config.APIEndpoint, config.Method,
			config.APIKey, config.APIKeyLocation, config.APIKeyName,
			config.Headers, config.BodyTemplate, config.PromptPath, config.EnablePromptOptimization, config.ImagePath); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// 清除Redis缓存
	key := "ai_api_config:task_type:" + config.TaskType
	m.Redis.Del(m.ctx, key)

	return nil
}

// InitTable 初始化ai_api_config表
func (m *AIAPIConfigModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS ai_api_config (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	task_type VARCHAR(32) NOT NULL COMMENT '任务类型：ai_draw（AI绘画）或 ai_chat（AI聊天）',
	provider_code VARCHAR(32) NOT NULL DEFAULT 'default' COMMENT '供应商编码',
	provider_name VARCHAR(64) DEFAULT NULL COMMENT '供应商名称',
	protocol_type VARCHAR(32) NOT NULL DEFAULT 'sync' COMMENT '协议类型：gemini_sync、toapis_async、chat_sync',
	is_active TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否当前启用',
	api_endpoint VARCHAR(512) NOT NULL COMMENT 'API接口地址',
	method VARCHAR(16) NOT NULL DEFAULT 'POST' COMMENT '请求方法：GET, POST, PUT等',
	api_key VARCHAR(256) DEFAULT NULL COMMENT 'API Key',
	api_key_location VARCHAR(32) DEFAULT 'header_bearer' COMMENT 'API Key发送位置：header_bearer, header_custom, query, body, none',
	api_key_name VARCHAR(64) DEFAULT 'Authorization' COMMENT 'API Key名称',
	headers TEXT COMMENT '请求头（JSON格式）',
	body_template TEXT COMMENT '请求体模板（JSON格式）',
	prompt_path VARCHAR(128) DEFAULT NULL COMMENT '提示词在JSON中的路径，如 "prompt" 或 "data.prompt"',
	enable_prompt_optimization TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否开启提示词优化：0-否，1-是',
	image_path VARCHAR(128) DEFAULT NULL COMMENT '用户图片在JSON中的路径，如 "image" 或 "data.images[0]"',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_task_type (task_type),
	UNIQUE KEY uniq_ai_api_task_provider (task_type, provider_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}

	// 检查并添加新字段（如果表已存在但缺少新字段）
	addColumnIfNotExists := func(columnName, columnDef string) {
		var exists bool
		m.DB.QueryRow(`
			SELECT COUNT(*) > 0 
			FROM information_schema.COLUMNS 
			WHERE TABLE_SCHEMA = DATABASE() 
			AND TABLE_NAME = 'ai_api_config' 
			AND COLUMN_NAME = ?
		`, columnName).Scan(&exists)
		if !exists {
			m.DB.Exec(`ALTER TABLE ai_api_config ADD COLUMN ` + columnDef)
		}
	}

	addColumnIfNotExists("prompt_path", "prompt_path VARCHAR(128) DEFAULT NULL COMMENT '提示词在JSON中的路径'")
	addColumnIfNotExists("enable_prompt_optimization", "enable_prompt_optimization TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否开启提示词优化'")
	addColumnIfNotExists("image_path", "image_path VARCHAR(128) DEFAULT NULL COMMENT '用户图片在JSON中的路径'")
	addColumnIfNotExists("api_key", "api_key VARCHAR(256) DEFAULT NULL COMMENT 'API Key'")
	addColumnIfNotExists("api_key_location", "api_key_location VARCHAR(32) DEFAULT 'header_bearer' COMMENT 'API Key发送位置'")
	addColumnIfNotExists("api_key_name", "api_key_name VARCHAR(64) DEFAULT 'Authorization' COMMENT 'API Key名称'")
	addColumnIfNotExists("provider_code", "provider_code VARCHAR(32) NOT NULL DEFAULT 'default' COMMENT '供应商编码'")
	addColumnIfNotExists("provider_name", "provider_name VARCHAR(64) DEFAULT NULL COMMENT '供应商名称'")
	addColumnIfNotExists("protocol_type", "protocol_type VARCHAR(32) NOT NULL DEFAULT 'sync' COMMENT '协议类型'")
	addColumnIfNotExists("is_active", "is_active TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否当前启用'")

	m.DB.Exec(`UPDATE ai_api_config SET provider_code = 'gpt-image-2-vip', provider_name = 'GPT-Image-2 VIP', protocol_type = 'openai_image_sync', is_active = 1 WHERE task_type = 'ai_draw' AND provider_code IN ('default', 'laozhang')`)
	m.DB.Exec(`UPDATE ai_api_config SET provider_code = 'default', provider_name = '默认聊天接口', protocol_type = 'chat_sync', is_active = 1 WHERE task_type = 'ai_chat' AND provider_code = 'default'`)
	dropTaskTypeUniqueIndexes(m.DB)
	addUniqueIndexIfNotExists(m.DB, "uniq_ai_api_task_provider", "ALTER TABLE ai_api_config ADD UNIQUE KEY uniq_ai_api_task_provider (task_type, provider_code)")

	return nil
}

func dropTaskTypeUniqueIndexes(db *sql.DB) {
	rows, err := db.Query(`
		SELECT INDEX_NAME
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = 'ai_api_config'
		AND COLUMN_NAME = 'task_type'
		AND NON_UNIQUE = 0
		AND INDEX_NAME <> 'PRIMARY'
	`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var indexName string
		if err := rows.Scan(&indexName); err != nil {
			continue
		}
		if indexName == "uniq_ai_api_task_provider" {
			continue
		}
		escaped := strings.ReplaceAll(indexName, "`", "``")
		if _, err := db.Exec("ALTER TABLE ai_api_config DROP INDEX `" + escaped + "`"); err != nil {
			log.Printf("[AIAPIConfig] 删除旧 task_type 唯一索引失败: index=%s err=%v", indexName, err)
		}
	}
}

func addUniqueIndexIfNotExists(db *sql.DB, indexName string, ddl string) {
	var exists bool
	if err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE()
		AND TABLE_NAME = 'ai_api_config'
		AND INDEX_NAME = ?
	`, indexName).Scan(&exists); err != nil {
		return
	}
	if !exists {
		if _, err := db.Exec(ddl); err != nil {
			log.Printf("[AIAPIConfig] 添加唯一索引失败: index=%s err=%v", indexName, err)
		}
	}
}

// InitDefaultConfigs 初始化默认AI API配置（如果不存在则插入）
// 这里使用各供应商的环境变量 API Key（为空时保留空值，运行时仍会再次读取环境配置）
func (m *AIAPIConfigModel) InitDefaultConfigs(laoZhangKey string, toAPIsKey string) error {
	// 默认配置列表
	defaultConfigs := []AIAPIConfig{
		{
			TaskType:                 "ai_draw",
			ProviderCode:             "gpt-image-2-vip",
			ProviderName:             "GPT-Image-2 VIP",
			ProtocolType:             "openai_image_sync",
			IsActive:                 true,
			APIEndpoint:              "https://api.laozhang.ai/v1/images/generations",
			Method:                   "POST",
			APIKey:                   laoZhangKey,
			APIKeyLocation:           "header_bearer",
			APIKeyName:               "Authorization",
			Headers:                  `{"Content-Type": "application/json"}`,
			BodyTemplate:             `{"model":"gpt-image-2-vip","prompt":"{{prompt}}","size":"{{image_size}}","response_format":"b64_json","n":1}`,
			EnablePromptOptimization: false,
		},
		{
			TaskType:                 "ai_draw",
			ProviderCode:             "toapis",
			ProviderName:             "ToAPIs",
			ProtocolType:             "toapis_async",
			IsActive:                 false,
			APIEndpoint:              "https://toapis.com/v1/images/generations",
			Method:                   "POST",
			APIKey:                   toAPIsKey,
			APIKeyLocation:           "header_bearer",
			APIKeyName:               "Authorization",
			Headers:                  `{"Content-Type": "application/json"}`,
			BodyTemplate:             `{"model":"gemini-3-pro-image-preview","prompt":"{{prompt}}","size":"{{aspect_ratio}}","n":1,"metadata":{"resolution":"{{image_size}}"}}`,
			EnablePromptOptimization: false,
		},
		{
			TaskType:                 "ai_chat",
			ProviderCode:             "default",
			ProviderName:             "默认聊天接口",
			ProtocolType:             "chat_sync",
			IsActive:                 true,
			APIEndpoint:              "https://api.laozhang.ai/v1/chat/completions",
			Method:                   "POST",
			APIKey:                   laoZhangKey,
			APIKeyLocation:           "header_bearer",
			APIKeyName:               "Authorization",
			Headers:                  `{"Content-Type": "application/json"}`,
			BodyTemplate:             `{"model":"gemini-3-pro-image-preview","stream":false,"messages":[{"role":"user","content":"{{prompt}}"}]}`,
			EnablePromptOptimization: false,
		},
	}

	for _, config := range defaultConfigs {
		normalizeAIAPIConfig(&config)
		// 检查是否已存在
		var exists bool
		m.DB.QueryRow(`SELECT COUNT(*) > 0 FROM ai_api_config WHERE task_type = ? AND provider_code = ?`, config.TaskType, config.ProviderCode).Scan(&exists)
		if exists {
			log.Printf("[AIAPIConfig] %s 配置已存在，跳过初始化", config.TaskType)
			continue
		}

		// 插入默认配置
		query := `INSERT INTO ai_api_config (task_type, provider_code, provider_name, protocol_type, is_active, api_endpoint, method, api_key, api_key_location, api_key_name, headers, body_template, enable_prompt_optimization, created_at, updated_at)
		          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`
		_, err := m.DB.Exec(query,
			config.TaskType,
			config.ProviderCode,
			config.ProviderName,
			config.ProtocolType,
			config.IsActive,
			config.APIEndpoint,
			config.Method,
			config.APIKey,
			config.APIKeyLocation,
			config.APIKeyName,
			config.Headers,
			config.BodyTemplate,
			config.EnablePromptOptimization,
		)
		if err != nil {
			log.Printf("[AIAPIConfig] 插入 %s 默认配置失败: %v", config.TaskType, err)
			return err
		}
		log.Printf("[AIAPIConfig] ✓ 已插入 %s 默认配置", config.TaskType)
	}

	return nil
}
