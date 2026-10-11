package virtualpay

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestQueryUserBalance_RequiresSessionKey 锁定该接口必须带 signature 这一文档要求。
//
// 这条测试存在的意义：query_user_balance 是带用户身份的接口。
// 一旦调用方忘记传 sessionKey，微信会返回 -15005，但表象与"过期"难以区分，会浪费排查时间。
// 在客户端侧先做必填校验，能让"调用方误用"在本地立即可见。
func TestQueryUserBalance_RequiresSessionKey(t *testing.T) {
	resetCfg(t, Config{Enabled: true, OfferID: "o", AppKeyProd: "k", MessageToken: "t", MessageEncodingAESKey: "x", CoinRatio: 10})

	_, err := QueryUserBalance(context.Background(), "atk", "", QueryUserBalanceRequest{OpenID: "o1"})
	if err == nil || !strings.Contains(err.Error(), "sessionKey") {
		t.Fatalf("expected sessionKey validation error, got: %v", err)
	}
}

// TestQueryUserBalance_ParsesBalance 锁定 balance 字段解析。
//
// 这条测试存在的意义：D7 双账差额监控会基于此值与本地灵石余额做对照。
// 若字段名错位，监控会全部告警或全部静默，掩盖真实异常。
func TestQueryUserBalance_ParsesBalance(t *testing.T) {
	resetCfg(t, Config{Enabled: true, OfferID: "o", AppKeyProd: "k", MessageToken: "t", MessageEncodingAESKey: "x", CoinRatio: 10})

	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xpay/query_user_balance" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("signature") == "" {
			t.Error("query_user_balance 必须携带 signature")
		}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","balance":1280}`))
	}, func() {
		resp, err := QueryUserBalance(context.Background(), "atk", "session-x", QueryUserBalanceRequest{OpenID: "o1", UserIP: "127.0.0.1", Env: 0})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Balance != 1280 {
			t.Errorf("balance=%d, want 1280", resp.Balance)
		}
	})
}
