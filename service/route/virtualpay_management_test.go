package route

import (
	"strings"
	"testing"

	"service/config"
	"service/model"
)

func TestBuildVirtualPayHealthFailsWhenRequiredConfigMissing(t *testing.T) {
	items := buildVirtualPayHealth(&config.Config{
		VirtualPay: config.VirtualPayConfig{
			Enabled: true,
		},
	})

	foundMissingOffer := false
	for _, item := range items {
		if item.Key == "offer_id" && !item.OK {
			foundMissingOffer = true
		}
	}
	if !foundMissingOffer {
		t.Fatalf("missing OfferID must be visible in management health checks: %#v", items)
	}
}

func TestBuildVirtualPayHealthPassesWhenRequiredConfigExists(t *testing.T) {
	items := buildVirtualPayHealth(&config.Config{
		VirtualPay: config.VirtualPayConfig{
			Enabled:               true,
			OfferID:               "offer",
			AppKeyProd:            "app-key",
			MessageToken:          "token",
			MessageEncodingAESKey: "aes-key",
			CoinRatio:             10,
			CertDesignerFen:       3000,
			CertEnterpriseFen:     5000,
		},
	})

	for _, item := range items {
		if !item.OK {
			t.Fatalf("complete virtual pay config should pass health item %s: %#v", item.Key, item)
		}
	}
}

func TestReportVirtualPayGoodsDeliveredSkipsCoinOrders(t *testing.T) {
	err := reportVirtualPayGoodsDelivered(nil, &config.Config{}, VirtualPayDeps{}, &model.VirtualPayOrderMeta{
		PayMode: "short_series_coin",
	}, "")
	if err != nil {
		t.Fatalf("coin orders do not need goods-delivery reporting: %v", err)
	}
}

func TestValidateVirtualPayRechargeRefundRejectsInsufficientBalance(t *testing.T) {
	order := &model.UserOrder{
		PayChannel: "virtualpay",
		PayMode:    "short_series_coin",
		Type:       "recharge",
		Status:     "success",
		Amount:     500,
		OpenID:     "openid-1",
	}

	err := validateVirtualPayRechargeRefund(order, 499)
	if err == nil {
		t.Fatal("expected insufficient balance error")
	}
	if !strings.Contains(err.Error(), "余额不足") {
		t.Fatalf("error should explain insufficient balance, got: %v", err)
	}
}

func TestValidateVirtualPayRechargeRefundAcceptsSuccessfulCoinRecharge(t *testing.T) {
	order := &model.UserOrder{
		PayChannel: "virtualpay",
		PayMode:    "short_series_coin",
		Type:       "recharge",
		Status:     "success",
		Amount:     500,
		OpenID:     "openid-1",
	}

	if err := validateVirtualPayRechargeRefund(order, 500); err != nil {
		t.Fatalf("expected refund to be allowed: %v", err)
	}
}
