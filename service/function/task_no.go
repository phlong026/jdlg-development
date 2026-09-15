package function

import (
	"crypto/rand"
	"encoding/hex"
)

// GenerateTaskNo 生成32位唯一任务编号（数字+字母）
func GenerateTaskNo() string {
	// 生成16字节随机数据
	bytes := make([]byte, 16)
	rand.Read(bytes)
	// 转为32位十六进制字符串（0-9, a-f）
	taskNo := hex.EncodeToString(bytes)
	return taskNo
}
