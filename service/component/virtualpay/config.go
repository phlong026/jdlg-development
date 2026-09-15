package virtualpay

import (
	"log"
	"sync"
)

// Config 虚拟支付配置（与 config.VirtualPayConfig 同构，避免循环依赖）。
//
// 字段含义：
//   - OfferID：mp 后台开通虚拟支付后分配的应用 ID（必填）
//   - AppKeyProd / AppKeySandbox：现网 / 沙箱 AppKey，绝不下发到小程序
//   - MessageToken / MessageEncodingAESKey：消息推送 Token 与 AESKey（用于回调验签解密）
//   - IOSEnabled：mp 后台 iOS 支付开关；本地此字段仅用于读侧逻辑判断
//   - SandboxMode：是否使用沙箱（env=1）；iOS 平台无论此值为何都强制 env=0
//   - CoinRatio：1 元 = CoinRatio 个灵石（默认 10），与 mp 后台代币兑换比例必须一致
//   - Enabled：总开关；为 false 时所有虚拟支付链路按未启用处理，便于回滚
type Config struct {
	OfferID               string
	AppKeyProd            string
	AppKeySandbox         string
	MessageToken          string
	MessageEncodingAESKey string
	IOSEnabled            bool
	SandboxMode           bool
	CoinRatio             int64
	Enabled               bool
}

var (
	cfg   Config
	cfgMu sync.RWMutex
)

// Init 装载配置到包级单例。
//
// 设计取舍：
//   - 与 wechatpay.Init 保持一致，未启用或缺关键字段时仅打日志、不 panic，便于本地启动。
//   - 真正调用 /xpay/* 接口前需要再次校验关键字段，避免静默使用零值发起请求。
func Init(c Config) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if c.CoinRatio <= 0 {
		c.CoinRatio = 10
	}
	cfg = c
	if !c.Enabled {
		log.Println("[VirtualPay] not enabled, skip")
		return
	}
	missing := make([]string, 0, 4)
	if c.OfferID == "" {
		missing = append(missing, "OfferID")
	}
	if c.AppKeyProd == "" {
		missing = append(missing, "AppKeyProd")
	}
	if c.MessageToken == "" {
		missing = append(missing, "MessageToken")
	}
	if c.MessageEncodingAESKey == "" {
		missing = append(missing, "MessageEncodingAESKey")
	}
	if len(missing) > 0 {
		log.Printf("[VirtualPay] inited with missing fields: %v (calls will fail until configured)", missing)
		return
	}
	log.Printf("[VirtualPay] inited: offerID=%s sandbox=%v iosEnabled=%v coinRatio=%d", c.OfferID, c.SandboxMode, c.IOSEnabled, c.CoinRatio)
}

// Get 返回当前配置快照（只读）。
func Get() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return cfg
}

// AppKey 按 SandboxMode 返回当前应使用的 AppKey。
//
// 注意：iOS 平台必须用现网 AppKey（env=0）。该选择由调用方在路由层根据 platform 决定，
// 此函数只负责"按当前 SandboxMode 给出对应 AppKey"，不感知平台差异。
func (c Config) AppKey() string {
	if c.SandboxMode {
		return c.AppKeySandbox
	}
	return c.AppKeyProd
}
