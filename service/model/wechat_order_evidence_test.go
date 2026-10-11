package model

import "testing"

// 回包形状 A：顶层平铺（线上当前版本，state 为字符串）。
func TestParseWechatOrderEvidence_FlatShape(t *testing.T) {
	raw := []byte(`{"errcode":0,"state":"PAID","pay_time":1760000000,"order_fee":30000,"paid_fee":30000,"wxpay_order_id":"wx123","sett_state":2,"platform_fee_fen":900}`)
	ev, err := ParseWechatOrderEvidence(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ev.WechatState != "PAID" {
		t.Errorf("state=%q want PAID", ev.WechatState)
	}
	if ev.PaidFeeFen == nil || *ev.PaidFeeFen != 30000 {
		t.Errorf("paid_fee wrong: %v", ev.PaidFeeFen)
	}
	if ev.WxpayOrderID != "wx123" {
		t.Errorf("wxpay_order_id=%q", ev.WxpayOrderID)
	}
	if ev.SettState == nil || *ev.SettState != 2 {
		t.Errorf("sett_state wrong: %v", ev.SettState)
	}
	if got := ComparePaidAmount(30000, ev); got != AmountMatchMatch {
		t.Errorf("match=%q want match", got)
	}
	if got := ComparePaidAmount(29900, ev); got != AmountMatchMismatch {
		t.Errorf("match=%q want mismatch", got)
	}
}

// 回包形状 B：order 嵌套（官方文档版本，status 为数字）。
func TestParseWechatOrderEvidence_NestedShape(t *testing.T) {
	raw := []byte(`{"errcode":0,"order":{"status":2,"order_fee":30000,"paid_fee":29900,"refund_fee":0,"left_fee":29900,"wx_order_id":"wx456"}}`)
	ev, err := ParseWechatOrderEvidence(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ev.WechatState != "2" {
		t.Errorf("state from numeric status=%q", ev.WechatState)
	}
	if ev.PaidFeeFen == nil || *ev.PaidFeeFen != 29900 {
		t.Errorf("paid_fee wrong: %v", ev.PaidFeeFen)
	}
	if ev.LeftFeeFen == nil || *ev.LeftFeeFen != 29900 {
		t.Errorf("left_fee wrong: %v", ev.LeftFeeFen)
	}
	if ev.WxpayOrderID != "wx456" {
		t.Errorf("wx_order_id 别名解析失败: %q", ev.WxpayOrderID)
	}
	if got := ComparePaidAmount(30000, ev); got != AmountMatchMismatch {
		t.Errorf("match=%q want mismatch", got)
	}
}

// 回包无金额字段（API 版本差异兜底）：证据只剩原文，比对为 unknown 不误报。
func TestParseWechatOrderEvidence_NoMoneyFields(t *testing.T) {
	raw := []byte(`{"errcode":0,"state":"PAYING"}`)
	ev, err := ParseWechatOrderEvidence(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ev.PaidFeeFen != nil {
		t.Fatal("paid_fee should be nil")
	}
	if got := ComparePaidAmount(30000, ev); got != AmountMatchUnknown {
		t.Errorf("match=%q want unknown", got)
	}
	if ev.RawPayload != string(raw) {
		t.Fatal("raw payload must be kept verbatim")
	}
}

// ComparePaidAmount 对 nil 证据与空证据必须安全。
func TestComparePaidAmount_NilSafe(t *testing.T) {
	if got := ComparePaidAmount(100, nil); got != AmountMatchUnknown {
		t.Errorf("nil evidence: %q", got)
	}
}
