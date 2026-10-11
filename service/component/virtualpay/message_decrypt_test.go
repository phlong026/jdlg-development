package virtualpay

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
)

// TestVerifyPushSignature 锁定 SHA1 排序拼接的签名规则。
//
// 这条测试存在的意义：消息推送验签是防伪造的第一道关。
// 若签名规则被改坏（例如忘记排序、换成 SHA256），会出现"线上正常的推送一律被拒"或"伪造推送被放行"。
// 用一条手动算过的固定向量锁定算法。
func TestVerifyPushSignature(t *testing.T) {
	token, ts, nonce, enc := "TestToken", "1745123456", "abc", "EncryptedPayload"
	arr := []string{token, ts, nonce, enc}
	sort.Strings(arr)
	h := sha1.New()
	for _, s := range arr {
		h.Write([]byte(s))
	}
	sig := hex.EncodeToString(h.Sum(nil))

	if !VerifyPushSignature(token, ts, nonce, enc, sig) {
		t.Fatal("expected signature verification to pass")
	}
	if VerifyPushSignature(token, ts, nonce, enc, "tampered") {
		t.Fatal("tampered signature must not pass")
	}
}

// encryptForTest 仅用于单测：复刻微信加密侧逻辑生成一段密文供解密测试使用。
//
// 复刻这段加密逻辑、而不是引入第三方库，是为了在没有官方测试向量的情况下做 roundtrip 验证。
// 真正联调时还需要拿一条线上真实推送做端到端校验（见 TODO 阶段 6.1）。
func encryptForTest(t *testing.T, key []byte, msg, appID string) string {
	t.Helper()
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("rand: %v", err)
	}
	msgBytes := []byte(msg)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(msgBytes)))

	plain := make([]byte, 0, 16+4+len(msgBytes)+len(appID))
	plain = append(plain, random...)
	plain = append(plain, lenBuf...)
	plain = append(plain, msgBytes...)
	plain = append(plain, []byte(appID)...)

	// 微信变体 PKCS7：以 32 字节为单位填充
	const blockSize = 32
	pad := blockSize - len(plain)%blockSize
	for i := 0; i < pad; i++ {
		plain = append(plain, byte(pad))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	encrypted := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(encrypted, plain)
	return base64.StdEncoding.EncodeToString(encrypted)
}

// TestDecryptPushMessage_RoundTrip 用 roundtrip 验证解密链路自洽。
//
// 这条测试存在的意义：
//   - 微信未公开针对小程序虚拟支付推送的端到端测试向量。
//   - 用相同密钥与 appid 加密、再解密，能锁定"明文结构（16+4+msg+appid）+ 32 字节 PKCS7 + AES-CBC + IV=key[:16]"这套协议的整体一致性。
//   - 任何一处协议偏移（例如 PKCS7 单位变 16、IV 写错、长度字段大端写成小端）都会让本测试立刻失败。
func TestDecryptPushMessage_RoundTrip(t *testing.T) {
	// 43 字符 EncodingAESKey；base64 解码（追加 "=" 后）应得到 32 字节
	const aesKey = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	key, err := base64.StdEncoding.DecodeString(aesKey + "=")
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length=%d want 32", len(key))
	}

	const appID = "wx94fdc48861913ede"
	const msg = `{"Event":"xpay_goods_deliver_notify","OutTradeNo":"T-001","OpenId":"o-1"}`
	encB64 := encryptForTest(t, key, msg, appID)

	got, err := DecryptPushMessage(aesKey, encB64, appID)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != msg {
		t.Fatalf("decrypted message mismatch:\n want %q\n  got %q", msg, string(got))
	}
}

// TestDecryptPushMessage_RejectsAppIDMismatch appid 不一致必须报错。
//
// 这条测试存在的意义：appid 校验是防"重放别家小程序推送"的最后一道防线。
// 一旦 appid 校验被绕过，恶意方拿到任意一份合法推送都能伪造发货。这条测试锁住该护栏。
func TestDecryptPushMessage_RejectsAppIDMismatch(t *testing.T) {
	const aesKey = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	key, _ := base64.StdEncoding.DecodeString(aesKey + "=")
	encB64 := encryptForTest(t, key, "{}", "wx-attacker")

	_, err := DecryptPushMessage(aesKey, encB64, "wx-mine")
	if err == nil || !strings.Contains(err.Error(), "appid mismatch") {
		t.Fatalf("expected appid mismatch error, got: %v", err)
	}
}
