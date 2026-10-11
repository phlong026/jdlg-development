package virtualpay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withTestServer 起一个仅用于单测的 HTTP 服务，并把 host 临时指向它。
// 在子测试结束后还原 host，避免污染包级状态。
func withTestServer(t *testing.T, handler http.HandlerFunc, fn func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	old := host
	host = srv.URL
	defer func() { host = old }()

	fn()
}

// resetCfg 在子测试前后恢复全局配置，避免相互污染。
func resetCfg(t *testing.T, c Config) {
	t.Helper()
	old := Get()
	Init(c)
	t.Cleanup(func() { Init(old) })
}

// TestPost_AppendsPaySigAndSignature 锁定客户端最关键的契约：
//   - access_token / pay_sig / signature 三个 query 参数必须按文档要求出现在 URL 中
//   - paySig 用 uri+"&"+body 与 AppKey 计算
//   - signature 用 body 与 sessionKey 计算
// 这条契约一旦走偏，所有 /xpay/* 调用都会被微信侧拒绝，因此单独锁定。
func TestPost_AppendsPaySigAndSignature(t *testing.T) {
	resetCfg(t, Config{Enabled: true, OfferID: "of-1", AppKeyProd: "appkey-prod", MessageToken: "t", MessageEncodingAESKey: "k", CoinRatio: 10})

	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xpay/query_order" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("access_token") != "atk-x" {
			t.Errorf("missing access_token, got %q", q.Get("access_token"))
		}
		// 读取真实请求体，再用同输入算一次签名做对照，确保客户端拼接没走样。
		bodyBytes, _ := io.ReadAll(r.Body)
		wantPaySig := CalcPaySig("/xpay/query_order", string(bodyBytes), "appkey-prod")
		if q.Get("pay_sig") != wantPaySig {
			t.Errorf("pay_sig mismatch:\n want %s\n  got %s", wantPaySig, q.Get("pay_sig"))
		}
		wantSig := CalcSignature(string(bodyBytes), "session-xyz")
		if q.Get("signature") != wantSig {
			t.Errorf("signature mismatch:\n want %s\n  got %s", wantSig, q.Get("signature"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","data":{"state":"PAID"}}`))
	}, func() {
		body := map[string]any{"openid": "o1", "order_id": "T1", "env": 0}
		raw, err := Post(context.Background(), "atk-x", "/xpay/query_order", body, "session-xyz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var got map[string]any
		_ = json.Unmarshal(raw, &got)
		if got["errcode"].(float64) != 0 {
			t.Errorf("expected errcode=0, got %v", got["errcode"])
		}
	})
}

// TestPost_OmitsSignatureWhenSessionKeyEmpty 验证服务端类接口（无用户身份的）不会带 signature。
//
// 这条测试存在的意义：query_order/refund_order 等服务端接口若误带 signature，
// 而客户端又无对应 sessionKey，会导致签名校验失败而非"忽略"。
// 锁住"sessionKey == \"\" 时不带 signature 参数"这条规则，能避免后续接口封装时不小心传错。
func TestPost_OmitsSignatureWhenSessionKeyEmpty(t *testing.T) {
	resetCfg(t, Config{Enabled: true, OfferID: "of-1", AppKeyProd: "appkey-prod", MessageToken: "t", MessageEncodingAESKey: "k", CoinRatio: 10})

	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["signature"]; ok {
			t.Error("signature should not be present when sessionKey is empty")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}, func() {
		_, err := Post(context.Background(), "atk-x", "/xpay/refund_order", map[string]any{"a": 1}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestPost_PropagatesErrcode 验证 errcode 非 0 时返回错误且消息包含 errmsg。
//
// 这条测试存在的意义：上层路由会基于 err 决策"轮询、补偿、放弃"。如果客户端把 errcode=非0 静默吞下，
// 上层会把失败当成功，发货链路会错。这是"显式失败（Fail Loud）"的护栏。
func TestPost_PropagatesErrcode(t *testing.T) {
	resetCfg(t, Config{Enabled: true, OfferID: "of-1", AppKeyProd: "appkey-prod", MessageToken: "t", MessageEncodingAESKey: "k", CoinRatio: 10})

	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":-15005,"errmsg":"session_key is invalid"}`))
	}, func() {
		_, err := Post(context.Background(), "atk-x", "/xpay/query_order", map[string]any{"a": 1}, "sk")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "-15005") || !strings.Contains(err.Error(), "session_key is invalid") {
			t.Errorf("error should include errcode and errmsg, got: %v", err)
		}
	})
}

// TestPost_DisabledConfigRejects 验证总开关关闭时调用直接拒绝，不发出真实请求。
//
// 这条测试存在的意义：cfg.Enabled=false 是回滚到旧通路时的紧急开关，必须保证关闭后无任何虚拟支付出网行为。
func TestPost_DisabledConfigRejects(t *testing.T) {
	resetCfg(t, Config{Enabled: false})

	_, err := Post(context.Background(), "atk-x", "/xpay/query_order", map[string]any{}, "")
	if err == nil || !strings.Contains(err.Error(), "未启用") {
		t.Fatalf("expected 未启用 error, got %v", err)
	}
}
