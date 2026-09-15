package function

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"github.com/tjfoc/gmsm/sm4"
)

var (
	// SM2公钥和私钥（实际应该从配置读取）
	sm2PrivateKey = "5f4d7e8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"
	sm2PublicKey  = ""
	// SM4密钥（16字节，128位）
	sm4Key = []byte("1234567890123456")
	// HMAC密钥
	hmacKey = []byte("jiadilinguang-hmac-secret-key-2024")
)

// InitCryptoKeys 初始化加密密钥（从配置读取）
func InitCryptoKeys() {
	// TODO: 从配置文件或环境变量读取密钥
	// 这里使用默认值，生产环境必须修改
}

// InitCrypto 初始化加密模块（从配置读取密钥）
func InitCrypto(sm2PrivateKeyStr, sm2PublicKeyStr, sm4KeyStr string) error {
	// 设置SM2密钥
	if sm2PrivateKeyStr != "" {
		sm2PrivateKey = sm2PrivateKeyStr
	}
	if sm2PublicKeyStr != "" {
		sm2PublicKey = sm2PublicKeyStr
	}

	// 设置SM4密钥
	if sm4KeyStr != "" {
		// 如果SM4Key是Base64编码的，需要解码
		decoded, err := base64.StdEncoding.DecodeString(sm4KeyStr)
		if err != nil {
			// 如果不是Base64，直接使用字符串（需要确保是16字节）
			if len(sm4KeyStr) == 16 {
				sm4Key = []byte(sm4KeyStr)
			} else {
				return fmt.Errorf("SM4密钥长度必须为16字节（128位）")
			}
		} else {
			if len(decoded) != 16 {
				return fmt.Errorf("SM4密钥长度必须为16字节（128位）")
			}
			sm4Key = decoded
		}
	}

	return nil
}

// GenerateToken 生成token：使用SM2+SM4加密session_id
func GenerateToken(sessionID string) (string, error) {
	// 1. 使用SM4加密session_id
	encrypted, err := sm4.Sm4Ecb(sm4Key, []byte(sessionID), true)
	if err != nil {
		return "", fmt.Errorf("SM4加密失败: %w", err)
	}

	// 2. Base64编码
	token := base64.StdEncoding.EncodeToString(encrypted)
	return token, nil
}

// DecryptToken 解密token还原session_id
func DecryptToken(token string) (string, error) {
	// 1. Base64解码
	encrypted, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("Base64解码失败: %w", err)
	}

	// 2. 使用SM4解密
	decrypted, err := sm4.Sm4Ecb(sm4Key, encrypted, false)
	if err != nil {
		return "", fmt.Errorf("SM4解密失败: %w", err)
	}

	return string(decrypted), nil
}

// GenerateTokenSignature 生成token的HMAC签名
func GenerateTokenSignature(token string) string {
	h := hmac.New(sha256.New, hmacKey)
	h.Write([]byte(token))
	signature := hex.EncodeToString(h.Sum(nil))
	return signature
}

// VerifyTokenSignature 验证token的HMAC签名
func VerifyTokenSignature(token, signature string) bool {
	expected := GenerateTokenSignature(token)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// GenerateSin 生成sin：请求体SM2+SM4加密后base64编码取前32位
func GenerateSin(requestBody []byte) (string, error) {
	// 如果请求体为空（GET请求），使用固定占位符
	if len(requestBody) == 0 {
		requestBody = []byte("{}")
	}
	
	// 1. 使用SM4加密请求体
	encrypted, err := sm4.Sm4Ecb(sm4Key, requestBody, true)
	if err != nil {
		return "", fmt.Errorf("SM4加密失败: %w", err)
	}

	// 2. Base64编码
	encoded := base64.StdEncoding.EncodeToString(encrypted)

	// 3. 取前32位（如果不足32位，重复填充）
	if len(encoded) < 32 {
		// 重复编码直到足够32位
		for len(encoded) < 32 {
			encoded += encoded
		}
	}
	sin := encoded[:32]
	return sin, nil
}

// GenerateMD5Signature 生成MD5签名：sin + token签名 + 编码后的接口地址，base64编码后用SM2+SM4加密，取前64位
func GenerateMD5Signature(sin, tokenSignature, apiPath string) (string, error) {
	// 1. 组合：sin + token签名 + 编码后的接口地址
	apiPathEncoded := base64.StdEncoding.EncodeToString([]byte(apiPath))
	combined := sin + tokenSignature + apiPathEncoded

	// 2. Base64编码
	combinedEncoded := base64.StdEncoding.EncodeToString([]byte(combined))

	// 3. 使用SM4加密（SM2用于非对称加密，这里用SM4对称加密）
	encrypted, err := sm4.Sm4Ecb(sm4Key, []byte(combinedEncoded), true)
	if err != nil {
		return "", fmt.Errorf("SM4加密失败: %w", err)
	}

	// 4. Base64编码
	encryptedEncoded := base64.StdEncoding.EncodeToString(encrypted)

	// 5. 计算MD5
	hash := md5.Sum([]byte(encryptedEncoded))
	md5Hex := hex.EncodeToString(hash[:])

	// 6. 取前64位（MD5是32位hex，这里可能需要重复或其他处理）
	// 如果MD5不够64位，可以重复或使用其他方式
	if len(md5Hex) < 64 {
		// 重复MD5直到64位
		for len(md5Hex) < 64 {
			md5Hex += md5Hex
		}
		md5Hex = md5Hex[:64]
	} else {
		md5Hex = md5Hex[:64]
	}

	return md5Hex, nil
}

// VerifyMD5Signature 验证MD5签名
func VerifyMD5Signature(sin, tokenSignature, apiPath, receivedSignature string) (bool, error) {
	expected, err := GenerateMD5Signature(sin, tokenSignature, apiPath)
	if err != nil {
		return false, err
	}
	return expected == receivedSignature, nil
}

// VerifySin 验证sin：根据请求体计算sin并与传入的sin比较
func VerifySin(requestBody []byte, receivedSin string) (bool, error) {
	expected, err := GenerateSin(requestBody)
	if err != nil {
		return false, err
	}
	return expected == receivedSin, nil
}

// VerifyTm 验证tm参数：解密tm，提取时间戳和接口地址，验证时间戳是否在合理范围内
// tm的生成规则：时间戳 + 请求地址（base64编码） -> SM4加密 -> base64编码
func VerifyTm(receivedTm, apiPath string, maxAgeSeconds int64) (bool, string, error) {
	// 1. 优先原样解码（前端已不再替换末尾==，原样解码才能得到 16 的倍数字节）
	var encrypted []byte
	var err error
	encrypted, err = base64.StdEncoding.DecodeString(receivedTm)
	if err != nil {
		// 兼容旧版：末尾不是==时曾替换为==，会导致解码长度错，此处仅做兜底
		tm2 := receivedTm
		if len(tm2) >= 2 && !strings.HasSuffix(tm2, "==") {
			tm2 = tm2[:len(tm2)-2] + "=="
		}
		encrypted, err = base64.StdEncoding.DecodeString(tm2)
		if err != nil {
			remainder := len(receivedTm) % 4
			if remainder > 0 {
				tm2 = receivedTm + strings.Repeat("=", 4-remainder)
				encrypted, err = base64.StdEncoding.DecodeString(tm2)
			}
			if err != nil {
				return false, "", fmt.Errorf("tm Base64解码失败: %w（原始tm长度: %d）", err, len(receivedTm))
			}
		}
	}

	if len(encrypted) == 0 {
		return false, "", fmt.Errorf("tm Base64解码后数据为空（原始tm长度: %d）", len(receivedTm))
	}
	// SM4 ECB 密文长度必须为 16 的倍数，否则解密会失败或得到空
	if len(encrypted)%16 != 0 {
		return false, "", fmt.Errorf("tm密文长度非法（%d），应为16的倍数", len(encrypted))
	}

	// 2. SM4解密
	decrypted, err := sm4.Sm4Ecb(sm4Key, encrypted, false)
	if err != nil {
		return false, "", fmt.Errorf("tm SM4解密失败: %w（加密数据长度: %d）", err, len(encrypted))
	}
	
	// 调试：输出解密后的内容长度
	if len(decrypted) == 0 {
		return false, "", fmt.Errorf("tm SM4解密后数据为空（加密数据长度: %d）", len(encrypted))
	}

	// 3. 解析：时间戳 + base64编码的接口地址
	decryptedStr := string(decrypted)
	
	// 时间戳是数字，找到第一个非数字字符的位置
	timestampEnd := 0
	for i, r := range decryptedStr {
		if r < '0' || r > '9' {
			timestampEnd = i
			break
		}
	}
	if timestampEnd == 0 {
		// 如果全部是数字或者没有找到非数字字符，说明格式不对
		if len(decryptedStr) == 0 {
			return false, "", fmt.Errorf("tm格式错误：解密后内容为空")
		}
		// 如果全部是数字，尝试将整个字符串作为时间戳
		if len(decryptedStr) > 0 {
			// 检查是否全部是数字
			allDigits := true
			for _, r := range decryptedStr {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return false, "", fmt.Errorf("tm格式错误：解密后内容全部是数字，无法解析接口地址（内容: %s）", decryptedStr)
			}
		}
		return false, "", fmt.Errorf("tm格式错误：无法解析时间戳（解密后内容长度: %d，前50字符: %s）", len(decryptedStr), decryptedStr[:min(50, len(decryptedStr))])
	}

	timestampStr := decryptedStr[:timestampEnd]
	apiPathEncoded := decryptedStr[timestampEnd:]

	// 4. 解码接口地址
	apiPathDecoded, err := base64.StdEncoding.DecodeString(apiPathEncoded)
	if err != nil {
		return false, "", fmt.Errorf("tm中的接口地址Base64解码失败: %w", err)
	}

	// 5. 验证接口地址是否匹配
	if string(apiPathDecoded) != apiPath {
		return false, timestampStr, fmt.Errorf("tm中的接口地址不匹配：期望 %s，实际 %s", apiPath, string(apiPathDecoded))
	}

	// 6. 验证时间戳（如果提供了maxAgeSeconds）
	if maxAgeSeconds > 0 {
		// 前端传的是毫秒时间戳（Date.now()），需要转换为秒
		var timestamp int64
		for _, r := range timestampStr {
			if r >= '0' && r <= '9' {
				timestamp = timestamp*10 + int64(r-'0')
			}
		}
		
		// 如果时间戳长度大于10位，说明是毫秒，转换为秒
		if len(timestampStr) > 10 {
			timestamp = timestamp / 1000
		}

		now := time.Now().Unix()
		age := now - timestamp
		if age < 0 {
			age = -age // 处理时间戳超前的情况
		}
		if age > maxAgeSeconds {
			return false, timestampStr, fmt.Errorf("tm时间戳过期：已过期 %d 秒", age)
		}
	}

	return true, timestampStr, nil
}

// GeneratePass 生成pass参数（与前端保持一致）
// pass的生成规则：sin + md5 + 设备id + 时间戳 -> base64编码 -> SM4加密 -> base64编码 -> 取128位
func GeneratePass(sin, md5Signature, deviceID, timestamp string) (string, error) {
	// 1. 组合：sin + md5 + 设备id + 时间戳
	combined := sin + md5Signature + deviceID + timestamp

	// 2. Base64编码
	combinedEncoded := base64.StdEncoding.EncodeToString([]byte(combined))

	// 3. 使用SM4加密
	encrypted, err := sm4.Sm4Ecb(sm4Key, []byte(combinedEncoded), true)
	if err != nil {
		return "", fmt.Errorf("pass SM4加密失败: %w", err)
	}

	// 4. Base64编码
	encryptedBase64 := base64.StdEncoding.EncodeToString(encrypted)

	// 5. 取前128位（确保是4的倍数）
	result := encryptedBase64
	if len(result) < 128 {
		// 如果不够128位，重复直到至少128位
		for len(result) < 128 {
			result += encryptedBase64
		}
	}
	// 取前128位
	result = result[:128]

	// 确保是4的倍数（128已经是4的倍数，但为了安全还是检查）
	remainder := len(result) % 4
	if remainder > 0 {
		result += strings.Repeat("=", 4-remainder)
	}

	return result, nil
}

// VerifyPass 验证pass参数：使用相同方法重新生成pass并比较
// pass的生成规则：sin + md5 + 设备id + 时间戳 -> base64编码 -> SM4加密 -> base64编码 -> 取128位
func VerifyPass(receivedPass, sin, md5Signature, deviceID, expectedTimestamp string) (bool, error) {
	// 1. 使用相同方法生成期望的pass
	expectedPass, err := GeneratePass(sin, md5Signature, deviceID, expectedTimestamp)
	if err != nil {
		return false, fmt.Errorf("生成期望pass失败: %w", err)
	}

	// 2. 处理接收到的pass（取前128位）
	pass := receivedPass
	if len(pass) > 128 {
		pass = pass[:128]
	}
	if len(pass) < 128 {
		return false, errors.New("pass长度不足128位")
	}

	// 3. 比较（直接字符串比较）
	if pass != expectedPass {
		return false, fmt.Errorf("pass验证失败：期望 %s，实际 %s", expectedPass, pass)
	}

	return true, nil
}

// min 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
