package virtualpay

import "testing"

// TestCalcPaySig_OfficialVector 用微信虚拟支付官方文档样例向量校验 paySig。
//
// 这条测试存在的意义：
//   - paySig 一旦算错，所有 /xpay/* 服务端调用都会被微信侧拒绝，整套支付链路瘫痪。
//   - 用官方公开的输入/输出向量做端到端校验，能确保后续重构（哪怕只是改一行字符串拼接）也能立刻被发现。
//   - 当微信将来更新签名算法时，这条测试会立即失败，提示我们必须升级。
func TestCalcPaySig_OfficialVector(t *testing.T) {
	uri := "/xpay/query_user_balance"
	body := `{"openid": "xxx", "user_ip": "127.0.0.1", "env": 0}`
	appKey := "12345"
	want := "c37809f27c6d7fd1837ad2500a04512b66b34fd793a39a385fade56dca89a4b5"

	got := CalcPaySig(uri, body, appKey)
	if got != want {
		t.Fatalf("CalcPaySig 与官方向量不一致\n want=%s\n  got=%s", want, got)
	}
}

// TestCalcSignature_OfficialVector 用微信虚拟支付官方文档样例向量校验 signature。
//
// 这条测试存在的意义：
//   - signature 错了相当于"用户身份校验失败"，微信会返回 -15005，用户支付直接失败。
//   - session_key 是 base64 文本，但 HMAC 时按字节直接使用（不做 base64 解码），这是文档明确要求且容易踩错的点。
//   - 这条测试同时锁定了"按字节使用 session_key 字符串"这一行为约定，避免误改。
func TestCalcSignature_OfficialVector(t *testing.T) {
	body := `{"openid": "xxx", "user_ip": "127.0.0.1", "env": 0}`
	sessionKey := "9hAb/NEYUlkaMBEsmFgzig=="
	want := "089d9e8dc5d308977360c4b79ec600a93d736802802a807d634192328032f6c7"

	got := CalcSignature(body, sessionKey)
	if got != want {
		t.Fatalf("CalcSignature 与官方向量不一致\n want=%s\n  got=%s", want, got)
	}
}

// TestHmacSHA256Hex_StableEncoding 锁定输出格式：小写 hex、长度 64。
//
// 这条测试存在的意义：
//   - 微信侧严格区分大小写。如果误用 EncodeToString 之外的格式（如大写或 base64），
//     会出现"本地自测通过、线上签名校验失败"的诡异现象。
//   - 这条测试用一个固定输入锁定输出格式，重构时不会被静默改坏。
func TestHmacSHA256Hex_StableEncoding(t *testing.T) {
	got := HmacSHA256Hex("key", "data")
	want := "5031fe3d989c6d1537a013fa6e739da23463fdaec3b70137d828e36ace221bd0"
	if got != want {
		t.Fatalf("HmacSHA256Hex 输出格式不稳定\n want=%s\n  got=%s", want, got)
	}
	if len(got) != 64 {
		t.Fatalf("hex 长度应为 64，实际 %d", len(got))
	}
}
