package safeerror

import "strings"

func SanitizeAIGenerationError(raw string) string {
	message := strings.TrimSpace(raw)
	if message == "" {
		return "这次生成没有成功，请调整内容后再试一次。"
	}
	lower := strings.ToLower(message)

	if containsAny(lower,
		"sensitive",
		"敏感",
		"违规",
		"unsafe",
		"forbidden content",
		"filtered",
		"audio_filtered",
		"public_error_audio_filtered",
	) {
		return "当前内容暂不支持生成，请调整描述或图片后再试一次。"
	}

	if containsAny(lower,
		"首帧",
		"尾帧",
		"图片",
		"图像",
		"image",
		"reference",
		"decode",
		"read image",
		"读取图片",
		"下载图片",
		"invalid image",
	) {
		return "图片处理失败，请更换图片或稍后再试一次。"
	}

	if containsAny(lower,
		"timeout",
		"timed out",
		"deadline exceeded",
		"context deadline exceeded",
		"client.timeout",
		"取消",
		"expired",
	) {
		return "当前生成等待时间较长，请稍后再试一次。"
	}

	if containsAny(lower,
		"too many requests",
		"rate limit",
		"service unavailable",
		"bad gateway",
		"queue",
		"busy",
		"429",
		"502",
		"503",
	) {
		return "当前生成服务较忙，请稍后再试一次。"
	}

	if containsAny(lower,
		"api key",
		"unauthorized",
		"forbidden",
		"permission",
		"未配置",
		"401",
		"403",
	) {
		return "当前生成服务暂时不可用，请稍后再试一次。"
	}

	if containsAny(lower,
		"下载视频",
		"读取视频",
		"upload",
		"上传失败",
		"存储服务",
		"concat",
		"拼接",
		"request failed",
		"http请求返回错误状态码",
		"保存ai绘画结果失败",
	) {
		return "结果处理中断，请稍后再试一次。"
	}

	return "这次生成没有成功，请调整内容后再试一次。"
}

func containsAny(source string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(source, strings.ToLower(strings.TrimSpace(keyword))) {
			return true
		}
	}
	return false
}
