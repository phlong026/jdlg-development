package virtualpay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// host 微信虚拟支付 API 主机。可在单测中覆盖。
var host = "https://api.weixin.qq.com"

// errResponse 微信通用错误响应字段（所有 /xpay/* 接口出错时都包含 errcode/errmsg）。
type errResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// Post 调用虚拟支付服务端接口（如 /xpay/query_order、/xpay/refund_order、/xpay/notify_provide_goods）。
//
// 参数：
//   - accessToken：调用方通过 function.GetAccessToken 拿到的小程序 access_token
//   - uri：以 "/" 开头的接口路径，例如 "/xpay/query_order"
//   - body：将被 json.Marshal 后用作请求体并参与签名；调用方需保证字段顺序稳定
//   - sessionKey：用户会话密钥；空字符串表示当前接口不需要 signature（仅服务端接口）
//
// 返回：原始响应 JSON 字节；errcode 非 0 时同时返回错误。
//
// 这里不在客户端内部解析业务字段，而是把原始 JSON 交回上层，避免提前与具体接口的字段名耦合。
func Post(ctx context.Context, accessToken, uri string, body interface{}, sessionKey string) (json.RawMessage, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("virtualpay: access_token 为空")
	}
	cfg := Get()
	if !cfg.Enabled {
		return nil, fmt.Errorf("virtualpay: 未启用")
	}
	appKey := cfg.AppKey()
	if appKey == "" {
		return nil, fmt.Errorf("virtualpay: AppKey 未配置（sandbox=%v）", cfg.SandboxMode)
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("virtualpay: marshal body: %w", err)
	}
	bodyStr := string(raw)

	q := url.Values{}
	q.Set("access_token", accessToken)
	q.Set("pay_sig", CalcPaySig(uri, bodyStr, appKey))
	if sessionKey != "" {
		q.Set("signature", CalcSignature(bodyStr, sessionKey))
	}
	fullURL := host + uri + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("virtualpay: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("virtualpay: http: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("virtualpay: read response: %w", err)
	}

	// 微信对所有 /xpay/* 在出错时都返回 errcode/errmsg，成功时 errcode=0
	var er errResponse
	_ = json.Unmarshal(respBody, &er)
	if er.ErrCode != 0 {
		return respBody, fmt.Errorf("virtualpay: errcode=%d errmsg=%s", er.ErrCode, er.ErrMsg)
	}
	return respBody, nil
}
