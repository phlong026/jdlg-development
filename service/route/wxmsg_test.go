package route

import (
	"net/url"
	"strings"
	"testing"

	"service/config"
)

func TestExtractOuterFields_CompatLowerUpperCase(t *testing.T) {
	body := []byte(`{
		"encrypt":"ENC",
		"MsgSignature":"SIG",
		"timestamp":"1700000000",
		"Nonce":"NCE"
	}`)
	got, err := extractOuterFields(body)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got.Encrypt != "ENC" || got.Signature != "SIG" || got.Timestamp != "1700000000" || got.Nonce != "NCE" {
		t.Fatalf("fields wrong: %+v", got)
	}
}

// Real WeChat secure-mode JSON POST keeps Encrypt in the body but sends
// msg_signature, timestamp, and nonce in the query string. Losing this fallback
// makes paid virtual-pay pushes ACK without entering the delivery path.
func TestExtractOuterFieldsWithQuery_WechatPostShape(t *testing.T) {
	body := []byte(`{"Encrypt":"ENC"}`)
	query := url.Values{
		"msg_signature": {"SIG"},
		"timestamp":     {"1700000000"},
		"nonce":         {"NCE"},
	}

	got, err := extractOuterFieldsWithQuery(body, query)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got.Encrypt != "ENC" || got.Signature != "SIG" || got.Timestamp != "1700000000" || got.Nonce != "NCE" {
		t.Fatalf("fields wrong: %+v", got)
	}
}

// Delivery routing depends on these inner fields. This locks the accepted key
// variants so a paid push cannot be silently ignored after decrypting.
func TestExtractInnerFields_PicksKnownKeys(t *testing.T) {
	body := []byte(`{"event":"xpay_goods_deliver_notify","out_trade_no":"VP-1","openid":"o1"}`)
	got, err := extractInnerFields(body)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if got.Event != "xpay_goods_deliver_notify" || got.OutTradeNo != "VP-1" || got.OpenID != "o1" {
		t.Fatalf("fields wrong: %+v", got)
	}

	body2 := []byte(`{"Event":"xpay_refund_notify","OutTradeNo":"VP-2","OpenId":"o2"}`)
	got2, err := extractInnerFields(body2)
	if err != nil {
		t.Fatalf("extract2: %v", err)
	}
	if got2.Event != "xpay_refund_notify" || got2.OutTradeNo != "VP-2" || got2.OpenID != "o2" {
		t.Fatalf("fields2 wrong: %+v", got2)
	}
}

// The feature switch must fail closed to avoid old and new payment paths both
// issuing goods during rollback.
func TestHandleXPayPush_DisabledRejects(t *testing.T) {
	cfg := &config.Config{VirtualPay: config.VirtualPayConfig{Enabled: false}}
	_, _, _, err := handleXPayPush([]byte(`{}`), nil, cfg, VirtualPayDeps{})
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("expected not enabled error, got: %v", err)
	}
}

// Signature verification is the first anti-forgery guard for delivery pushes.
func TestHandleXPayPush_BadSignatureRejects(t *testing.T) {
	cfg := &config.Config{
		Wechat:     config.WechatConfig{AppID: "wx-mine"},
		VirtualPay: config.VirtualPayConfig{Enabled: true, MessageToken: "tok", MessageEncodingAESKey: "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"},
	}
	body := []byte(`{"encrypt":"AAAA","MsgSignature":"WRONG","timestamp":"1","Nonce":"n"}`)
	_, _, _, err := handleXPayPush(body, nil, cfg, VirtualPayDeps{})
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("expected signature error, got: %v", err)
	}
}
