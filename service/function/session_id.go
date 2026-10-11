package function

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"
)

// GenerateSessionID 生成Session ID: code + 时间戳 + 随机数
func GenerateSessionID(code string) string {
	// 获取当前时间戳（毫秒）
	timestamp := time.Now().UnixNano() / int64(time.Millisecond)
	
	// 生成随机数
	randomNum := rand.Int63n(1000000)
	
	// 组合: code + timestamp + randomNum
	data := fmt.Sprintf("%s_%d_%d", code, timestamp, randomNum)
	
	// MD5加密生成唯一ID
	hash := md5.Sum([]byte(data))
	sessionID := hex.EncodeToString(hash[:])
	
	return sessionID
}
