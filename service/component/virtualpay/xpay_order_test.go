package virtualpay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestQueryOrder_ParsesPaidState 锁定查询接口对 PAID 状态的解析。
//
// 这条测试存在的意义：confirm/补偿任务都依据 state==PAID 触发发货。
// 一旦字段名解析错（例如把 "state" 看成 "status"），整个发货链路会卡在 pending 直到超时。
func TestQueryOrder_ParsesPaidState(t *testing.T) {
	resetCfg(t, Config{Enabled: true, OfferID: "o", AppKeyProd: "k", MessageToken: "t", MessageEncodingAESKey: "x", CoinRatio: 10})

	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xpay/query_order" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		// 服务端类接口不应带 signature
		if _, ok := r.URL.Query()["signature"]; ok {
			t.Error("query_order 不应携带 signature")
		}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","state":"PAID","pay_time":1747000000}`))
	}, func() {
		resp, err := QueryOrder(context.Background(), "atk", QueryOrderRequest{OpenID: "o1", OutTradeNo: "T1", Env: 0})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.State != OrderStatePaid {
			t.Errorf("state=%q, want PAID", resp.State)
		}
		if resp.PayTime != 1747000000 {
			t.Errorf("pay_time=%d, want 1747000000", resp.PayTime)
		}
	})
}

// TestRefundOrder_RequiresOutRefundNo 锁定退款接口对必填字段的本地校验。
//
// 这条测试存在的意义：微信侧若 out_refund_no 为空会以 -xxxxx 错误码返回，
// 但发货侧此时已经发完货、若本地未先校验就发请求，会误把网络往返计入失败重试，浪费配额且日志噪音。
// 在客户端侧先做必填校验，能让"参数错误"立即可见。
func TestRefundOrder_RequiresOutRefundNo(t *testing.T) {
	resetCfg(t, Config{Enabled: true, OfferID: "o", AppKeyProd: "k", MessageToken: "t", MessageEncodingAESKey: "x", CoinRatio: 10})

	_, err := RefundOrder(context.Background(), "atk", RefundOrderRequest{OpenID: "o1", OutTradeNo: "T1"})
	if err == nil || !strings.Contains(err.Error(), "out_refund_no") {
		t.Fatalf("expected out_refund_no validation error, got: %v", err)
	}
}

// TestNotifyProvideGoods_PathAndBody 锁定道具模式发货回执的路径与必填字段。
//
// 这条测试存在的意义（D5 决策）：发货回执失败不阻塞本地发货，但上报必须能正确发出，
// 否则微信侧订单状态长期不闭环，会触发后台告警与运营排查。
// 锁定路径与字段格式能避免后续重构悄悄改坏。
func TestNotifyProvideGoods_PathAndBody(t *testing.T) {
	resetCfg(t, Config{Enabled: true, OfferID: "o", AppKeyProd: "k", MessageToken: "t", MessageEncodingAESKey: "x", CoinRatio: 10})

	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xpay/notify_provide_goods" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("body not json: %v", err)
		}
		if req["openid"] != "o1" || req["order_id"] != "T1" {
			t.Errorf("body fields wrong: %v", req)
		}
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}, func() {
		err := NotifyProvideGoods(context.Background(), "atk", NotifyProvideGoodsRequest{OpenID: "o1", OutTradeNo: "T1", Env: 0})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
