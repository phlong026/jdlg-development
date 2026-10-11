package route

import (
	"service/config"
	"strings"
)

var globalConfig *config.Config

// InitErrorHandler 初始化错误处理器（需要在main.go中调用）
func InitErrorHandler(cfg *config.Config) {
	globalConfig = cfg
}

// FormatValidationError 格式化参数验证错误信息
// 开发环境返回详细错误，生产环境返回模糊错误
// 参数：
//   - detailedMsg: 详细的错误信息，如 "Sin参数不合法" 或 "参数错误: Key: 'AIRequest.Scene' Error:Field validation for 'Scene' failed on the 'required' tag"
//   - fieldName: 字段名称，如 "Sin"、"Scene" 等（可选，用于提取字段名）
// 返回：
//   - 开发环境：返回详细错误信息
//   - 生产环境：返回模糊的"参数不合法"提示
func FormatValidationError(detailedMsg string, fieldName ...string) string {
	if globalConfig == nil {
		// 如果未初始化，默认返回详细错误（开发环境行为）
		return detailedMsg
	}

	// 开发环境返回详细错误
	if globalConfig.IsDevelopment() {
		return detailedMsg
	}

	// 生产环境返回模糊错误
	// 尝试提取字段名，如果提供了fieldName参数，使用它
	if len(fieldName) > 0 && fieldName[0] != "" {
		return fieldName[0] + "参数不合法"
	}

	// 尝试从错误信息中提取字段名
	// 例如："Key: 'AIRequest.Scene' Error:Field validation for 'Scene' failed on the 'required' tag"
	// 提取 "Scene"
	if strings.Contains(detailedMsg, "Field validation for") {
		// 尝试提取字段名
		parts := strings.Split(detailedMsg, "Field validation for '")
		if len(parts) > 1 {
			fieldPart := parts[1]
			fieldEnd := strings.Index(fieldPart, "'")
			if fieldEnd > 0 {
				field := fieldPart[:fieldEnd]
				return field + "参数不合法"
			}
		}
	}

	// 如果包含常见字段名，提取它
	commonFields := []string{"Sin", "token", "Scene", "Payload", "TaskNo", "TaskType"}
	for _, field := range commonFields {
		if strings.Contains(detailedMsg, field) {
			return field + "参数不合法"
		}
	}

	// 默认返回模糊错误
	return "参数不合法"
}

// FormatParamError 格式化参数错误（用于参数缺失、格式错误等）
// 开发环境返回详细错误，生产环境返回模糊错误
// 参数：
//   - detailedMsg: 详细的错误信息
//   - fieldName: 字段名称（可选，如果提供则在生产环境返回"字段名参数不合法"）
func FormatParamError(detailedMsg string, fieldName ...string) string {
	if globalConfig == nil {
		return detailedMsg
	}

	if globalConfig.IsDevelopment() {
		return detailedMsg
	}

	// 生产环境返回模糊错误
	// 如果提供了字段名，返回"字段名参数不合法"
	if len(fieldName) > 0 && fieldName[0] != "" {
		return fieldName[0] + "参数不合法"
	}

	return "参数不合法"
}
