// Package virtualpay 微信小程序「虚拟支付（短链虚拟支付 / xpay）」相关工具。
//
// 本文件实现签名相关的纯函数，不依赖任何外部组件，便于单测覆盖。
//
// 微信虚拟支付的签名分为两层：
//   - paySig：用 AppKey 对 "uri&postBody" 做 HMAC-SHA256，给微信侧服务校验请求来源
//   - signature：用 session_key 对 postBody 做 HMAC-SHA256，给微信侧服务校验用户身份
//
// 详见：https://developers.weixin.qq.com/miniprogram/dev/platform-capabilities/business-capabilities/virtual-payment.html
package virtualpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// HmacSHA256Hex 计算 HMAC-SHA256 并返回小写 hex 字符串。
// key/data 均按 UTF-8 字节直接参与运算（不做 base64 解码）。
func HmacSHA256Hex(key, data string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// CalcPaySig 计算微信虚拟支付服务端接口（如 /xpay/query_order）所需的 paySig。
//
// uri 形如 "/xpay/xxx"（用于服务端调用）或 "requestVirtualPayment"（用于客户端拉起支付）。
// postBody 必须与实际请求体字节级一致，调用方在做 JSON 序列化时需保证字段顺序稳定。
func CalcPaySig(uri, postBody, appKey string) string {
	return HmacSHA256Hex(appKey, uri+"&"+postBody)
}

// CalcSignature 计算 signature：用 session_key 对 postBody 做 HMAC-SHA256。
//
// session_key 来自 wx.login → code2Session 的返回，需在服务端缓存使用。
func CalcSignature(postBody, sessionKey string) string {
	return HmacSHA256Hex(sessionKey, postBody)
}
