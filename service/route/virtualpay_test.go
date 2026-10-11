package route

import (
	"encoding/json"
	"strings"
	"testing"

	"service/config"
	"service/model"
)

func newTestVirtualPayCfg() *config.Config {
	return &config.Config{
		VirtualPay: config.VirtualPayConfig{
			Enabled:           true,
			OfferID:           "offer-test",
			AppKeyProd:        "appkey-prod",
			AppKeySandbox:     "appkey-sandbox",
			MessageToken:      "tok",
			IOSEnabled:        true,
			SandboxMode:       false,
			CoinRatio:         10,
			CertDesignerFen:   9900,
			CertEnterpriseFen: 39900,
		},
	}
}

// prepareReq 在测试中复用 route 包的 prepareRequest 类型，
// 别名而非重新声明，避免两份结构体声明漂移。
type prepareReq = prepareRequest

// TestBuildOrder_RechargeAmountValidation 锁定 D1 决策：充值金额必须 ≥ 1 元且整数元。
//
// 这条测试存在的意义：
//   - 代币模式按 amount_fen/100 * ratio 计算灵石，非整数元会出小数（用户感知错乱）
//   - 1 元起是文档约束；低于 1 元下单会被微信侧拒绝，不如本地先挡掉
//   - 任何放宽这两条规则的改动都会立即失败，避免再次踩坑
func TestBuildOrder_RechargeAmountValidation(t *testing.T) {
	cfg := newTestVirtualPayCfg()

	if _, err := buildVirtualPayOrder(cfg, 1, &prepareReq{PlanType: "recharge", AmountFen: 50}, nil); err == nil {
		t.Fatal("amount < 100 fen should fail")
	}
	if _, err := buildVirtualPayOrder(cfg, 1, &prepareReq{PlanType: "recharge", AmountFen: 199}, nil); err == nil {
		t.Fatal("non-integer yuan amount should fail")
	}
}

// TestBuildOrder_RechargeStoneCalc 锁定代币模式按 amount_fen 与 CoinRatio 计算灵石数。
//
// 这条测试存在的意义：D1 决策"取消档位赠送、严格 1 元 = 10 灵石"是用户感知最直接的变化。
// 一旦代码里再混进档位赠送或别的换算方式，本测试立刻失败。
func TestBuildOrder_RechargeStoneCalc(t *testing.T) {
	cfg := newTestVirtualPayCfg()

	in, err := buildVirtualPayOrder(cfg, 42, &prepareReq{PlanType: "recharge", AmountFen: 10000}, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if in.Amount != 1000 {
		t.Errorf("stones=%d want 1000", in.Amount)
	}
	if in.AmountFen != 10000 {
		t.Errorf("amount_fen=%d want 10000", in.AmountFen)
	}
	if in.PayMode != "short_series_coin" {
		t.Errorf("pay_mode=%q want short_series_coin", in.PayMode)
	}
}

// TestBuildOrder_CertWhitelist 锁定认证道具白名单 + 必须配置单价。
//
// 这条测试存在的意义：
//   - 白名单防止前端传任意 productId 下单（如 cheap_product 但实际是高价道具）
//   - 单价未配置时直接报错，避免静默用 0 元下单（会被微信侧拒绝，但耗费一次往返与排查时间）
func TestBuildOrder_CertWhitelist(t *testing.T) {
	cfg := newTestVirtualPayCfg()

	if _, err := buildVirtualPayOrder(cfg, 1, &prepareReq{PlanType: "certification", ProductID: "unknown_product", ApplicationID: 1}, nil); err == nil {
		t.Fatal("unknown productId should fail")
	}

	if _, err := buildVirtualPayOrder(cfg, 1, &prepareReq{PlanType: "certification", ProductID: "cert_designer"}, nil); err == nil {
		t.Fatal("missing application_id should fail")
	}

	cfg2 := newTestVirtualPayCfg()
	cfg2.VirtualPay.CertDesignerFen = 0
	if _, err := buildVirtualPayOrder(cfg2, 1, &prepareReq{PlanType: "certification", ProductID: "cert_designer", ApplicationID: 1}, nil); err == nil {
		t.Fatal("zero price should fail with explicit error")
	}

	in, err := buildVirtualPayOrder(cfg, 1, &prepareReq{PlanType: "certification", ProductID: "cert_designer", ApplicationID: 42}, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if in.PayMode != "short_series_goods" || in.ProductID != "cert_designer" || in.AmountFen != 9900 {
		t.Errorf("fields wrong: %+v", in)
	}
	if in.Description != "certification:42" {
		t.Errorf("description should encode application_id: %q", in.Description)
	}
}

// TestBuildSignData_CoinModeFields 锁定代币模式 signData 字段。
//
// 这条测试存在的意义：signData 是 JSON 字符串，前端原样透传给微信。
// 一旦字段名拼错（例如 buyQuantity 写成 buyAmount），微信会以晦涩的"参数错误"拒绝，不易定位。
// 用解析出的字段 + 必有 outTradeNo/offerId 锁住 schema。
func TestBuildSignData_CoinModeFields(t *testing.T) {
	cfg := newTestVirtualPayCfg()
	in := &model.VirtualPayOrderInput{
		OrderNo: "VP-1",
		Amount:  1000,
		PayMode: "short_series_coin",
	}
	got, err := buildVirtualPaySignData(cfg, 0, in, "from-test")
	if err != nil {
		t.Fatalf("sign data: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if parsed["offerId"] != "offer-test" {
		t.Errorf("offerId wrong: %v", parsed["offerId"])
	}
	if int64(parsed["buyQuantity"].(float64)) != 1000 {
		t.Errorf("buyQuantity wrong: %v", parsed["buyQuantity"])
	}
	if parsed["outTradeNo"] != "VP-1" {
		t.Errorf("outTradeNo wrong: %v", parsed["outTradeNo"])
	}
	// 代币模式不应出现 productId/goodsPrice
	if _, has := parsed["productId"]; has {
		t.Errorf("coin mode should not contain productId")
	}
}

// TestPickXPayEnv_IOSForcesProd 锁定 iOS 必须 env=0 这条文档硬约束。
//
// 这条测试存在的意义：iOS 沙箱不可用是审核驳回的根本原因之一。
// 任何"为开发方便统一让 ios 走沙箱"的改动都必须立刻失败。
func TestPickXPayEnv_IOSForcesProd(t *testing.T) {
	cfg := newTestVirtualPayCfg()
	cfg.VirtualPay.SandboxMode = true

	if env := pickXPayEnv(cfg, "ios"); env != 0 {
		t.Fatalf("ios env must be 0, got %d", env)
	}
	if env := pickXPayEnv(cfg, "android"); env != 1 {
		t.Fatalf("android in sandbox mode should be 1, got %d", env)
	}

	cfg.VirtualPay.SandboxMode = false
	if env := pickXPayEnv(cfg, "android"); env != 0 {
		t.Fatalf("android in prod mode should be 0, got %d", env)
	}
}

// TestBuildSignData_RejectsMissingOfferID 锁定 OfferID 必填。
//
// 这条测试存在的意义：mp 后台尚未发布或环境变量未配齐时，OfferID 为空会被微信侧拒绝且报错信息不直观。
// 本地先校验能让"上线前漏配置"在测试环境立刻可见。
func TestBuildSignData_RejectsMissingOfferID(t *testing.T) {
	cfg := newTestVirtualPayCfg()
	cfg.VirtualPay.OfferID = ""

	in := &model.VirtualPayOrderInput{OrderNo: "VP-1", Amount: 100, PayMode: "short_series_coin"}
	_, err := buildVirtualPaySignData(cfg, 0, in, "")
	if err == nil || !strings.Contains(err.Error(), "OfferID") {
		t.Fatalf("expected OfferID error, got: %v", err)
	}
}

// TestBuildOrder_CertPriceOverride 锁定后台定价配置的覆盖语义：
// 覆盖表命中时用配置价，未命中回退 env 价——定价配置化 P1 的核心行为。
func TestBuildOrder_CertPriceOverride(t *testing.T) {
	cfg := newTestVirtualPayCfg()
	overrides := map[string]int64{"cert_designer": 29900}

	in, err := buildVirtualPayOrder(cfg, 1, &prepareReq{PlanType: "certification", ProductID: "cert_designer", ApplicationID: 7}, overrides)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if in.AmountFen != 29900 {
		t.Errorf("override price should win: got %d want 29900", in.AmountFen)
	}

	// 覆盖表缺项回退 env 价
	in2, err := buildVirtualPayOrder(cfg, 1, &prepareReq{PlanType: "certification", ProductID: "cert_enterprise", ApplicationID: 7}, overrides)
	if err != nil {
		t.Fatalf("build fallback: %v", err)
	}
	if in2.AmountFen != 39900 {
		t.Errorf("fallback price wrong: got %d want 39900", in2.AmountFen)
	}
}

// TestValidatePricingData 锁定定价配置校验规则（白名单/整数元/去重/数量上限）。
func TestValidatePricingData(t *testing.T) {
	valid := &model.PricingData{
		RechargePlans: []model.PricingRechargePlan{{Type: "basic", Title: "普通会员", AmountFen: 5000}},
		CertPrices: []model.PricingCertPrice{
			{ProductID: "cert_designer", PriceFen: 29900},
			{ProductID: "cert_enterprise", PriceFen: 89900},
		},
	}
	if err := model.ValidatePricingData(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	// 非整数元档位
	bad := &model.PricingData{
		RechargePlans: []model.PricingRechargePlan{{Title: "x", AmountFen: 5050}},
		CertPrices:    valid.CertPrices,
	}
	if err := model.ValidatePricingData(bad); err == nil {
		t.Fatal("non-integer-yuan plan should fail")
	}

	// 未知认证道具
	bad2 := &model.PricingData{
		RechargePlans: valid.RechargePlans,
		CertPrices: []model.PricingCertPrice{
			{ProductID: "cert_designer", PriceFen: 29900},
			{ProductID: "cert_vip", PriceFen: 100},
		},
	}
	if err := model.ValidatePricingData(bad2); err == nil {
		t.Fatal("unknown cert product should fail")
	}

	// 重复金额档位
	bad3 := &model.PricingData{
		RechargePlans: []model.PricingRechargePlan{
			{Title: "a", AmountFen: 5000},
			{Title: "b", AmountFen: 5000},
		},
		CertPrices: valid.CertPrices,
	}
	if err := model.ValidatePricingData(bad3); err == nil {
		t.Fatal("duplicate amount should fail")
	}
}
