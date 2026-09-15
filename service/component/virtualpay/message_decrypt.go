package virtualpay

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
)

// VerifyPushSignature 校验微信消息推送的 signature 字段。
//
// 算法：sha1(排序后拼接([token, timestamp, nonce, encrypt]))，结果为小写 hex。
// 这套约定与微信公众号 WXBizMsgCrypt 一致；虚拟支付推送沿用同一规则。
func VerifyPushSignature(token, timestamp, nonce, encrypt, signature string) bool {
	arr := []string{token, timestamp, nonce, encrypt}
	sort.Strings(arr)
	h := sha1.New()
	for _, s := range arr {
		h.Write([]byte(s))
	}
	return hex.EncodeToString(h.Sum(nil)) == signature
}

// DecryptPushMessage 用 EncodingAESKey 解密微信消息推送密文，并校验 appid。
//
// 参数：
//   - encodingAESKey：mp 后台「消息推送」配置的 43 字符 EncodingAESKey
//   - encryptB64：推送 JSON 中的 Encrypt 字段（base64 字符串）
//   - expectedAppID：本地 cfg.Wechat.AppID，appid 不匹配视为伪造
//
// 返回：解密后的 message 字节（原始 JSON 推送内容）。
//
// 设计取舍（Fail Loud）：
//   - 长度异常 / 解码失败 / appid 不一致都直接返回错误，不做"尽力解析"
//   - 不在此处反序列化业务字段，业务层按事件类型自行解码
func DecryptPushMessage(encodingAESKey, encryptB64, expectedAppID string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, fmt.Errorf("virtualpay: decode aes key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("virtualpay: aes key length=%d, want 32", len(key))
	}
	cipherBytes, err := base64.StdEncoding.DecodeString(encryptB64)
	if err != nil {
		return nil, fmt.Errorf("virtualpay: decode encrypt: %w", err)
	}
	if len(cipherBytes) == 0 || len(cipherBytes)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("virtualpay: ciphertext length invalid: %d", len(cipherBytes))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("virtualpay: new cipher: %w", err)
	}
	plain := make([]byte, len(cipherBytes))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, cipherBytes)
	plain, err = pkcs7UnpadWeChat(plain)
	if err != nil {
		return nil, err
	}
	// 明文结构：16 字节随机串 + 4 字节 BE 长度 + msg + appid
	if len(plain) < 20 {
		return nil, errors.New("virtualpay: plaintext too short")
	}
	msgLen := binary.BigEndian.Uint32(plain[16:20])
	if 20+int(msgLen) > len(plain) {
		return nil, fmt.Errorf("virtualpay: msg_len=%d exceeds plaintext", msgLen)
	}
	msg := plain[20 : 20+msgLen]
	appid := string(plain[20+msgLen:])
	if appid != expectedAppID {
		return nil, fmt.Errorf("virtualpay: appid mismatch: got %q want %q", appid, expectedAppID)
	}
	return msg, nil
}

// pkcs7UnpadWeChat 微信变体 PKCS7：填充以 32 字节为单位（标准为 16），尾部填充值 ∈ [1, 32]。
//
// 仅根据尾字节值移除填充。上限放宽到 32 即可同时兼容 16 字节单位的标准 PKCS7。
func pkcs7UnpadWeChat(b []byte) ([]byte, error) {
	n := len(b)
	if n == 0 {
		return nil, errors.New("virtualpay: pkcs7 empty")
	}
	pad := int(b[n-1])
	if pad < 1 || pad > 32 {
		return nil, fmt.Errorf("virtualpay: pkcs7 invalid padding=%d", pad)
	}
	if pad > n {
		return nil, errors.New("virtualpay: pkcs7 padding larger than data")
	}
	for i := n - pad; i < n; i++ {
		if int(b[i]) != pad {
			return nil, errors.New("virtualpay: pkcs7 bad padding bytes")
		}
	}
	return b[:n-pad], nil
}
