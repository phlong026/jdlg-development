package model

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 定价配置（定价配置化 P1）。
//
// 可调边界（安全约束，勿放宽）：
//   - 充值档位：只允许配"金额档位 + 文案 + 推荐标记"。灵石数不入库，由系统按
//     兑换比例实时折算——微信代币模式的实扣金额 = buyQuantity ÷ 微信 mp 后台比例，
//     档位灵石若可手填，"页面标价 ≠ 微信实付"会造成资金纠纷（D1：严格 1 元 = 比例灵石）
//   - 认证道具价格：道具模式 goodsPrice 为纯本地定价，可自由调整
//   - 兑换比例：不在本配置内（env VIRTUAL_PAY_COIN_RATIO，且必须与微信 mp 后台一致）
//
// 生效：30 秒内存缓存，保存即失效；无需重启。

const (
	// PricingConfigKeyGlobal 单 key JSON 文档，后续定价项并入此文档
	PricingConfigKeyGlobal = "global"
	// PricingCacheTTL 配置缓存时长
	PricingCacheTTL = 30 * time.Second
)

// CertProductWhitelist 认证道具白名单（与 route 层 virtualpayCertProducts 保持一致）。
var CertProductWhitelist = []string{"cert_designer", "cert_enterprise"}

// PricingRechargePlan 充值档位配置项。AmountFen 为人民币分；不存灵石数（见上安全约束）。
type PricingRechargePlan struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	AmountFen   int64  `json:"amount_fen"`
	Recommended bool   `json:"recommended"`
}

// PricingCertPrice 认证道具价格配置项。
type PricingCertPrice struct {
	ProductID string `json:"product_id"`
	PriceFen  int64  `json:"price_fen"`
}

// PricingData 定价配置文档。
type PricingData struct {
	RechargePlans []PricingRechargePlan `json:"recharge_plans"`
	CertPrices    []PricingCertPrice    `json:"cert_prices"`
}

// PricingConfigModel 定价配置数据访问层（带内存缓存）。
type PricingConfigModel struct {
	DB       *sql.DB
	mu       sync.RWMutex
	cache    *PricingData
	cachedAt time.Time
}

// NewPricingConfigModel 创建定价配置模型。
func NewPricingConfigModel(db *sql.DB) *PricingConfigModel {
	return &PricingConfigModel{DB: db}
}

// ValidatePricingData 校验定价配置。纯函数，供保存前与管理端实时校验复用。
func ValidatePricingData(data *PricingData) error {
	if data == nil {
		return errors.New("配置不能为空")
	}
	if len(data.RechargePlans) < 1 || len(data.RechargePlans) > 10 {
		return errors.New("充值档位需为 1~10 个")
	}
	seenAmount := make(map[int64]bool)
	for i, plan := range data.RechargePlans {
		if strings.TrimSpace(plan.Title) == "" {
			return errors.New("第 " + strconv.Itoa(i+1) + " 个档位名称不能为空")
		}
		if len([]rune(plan.Title)) > 16 {
			return errors.New("第 " + strconv.Itoa(i+1) + " 个档位名称过长（≤16 字）")
		}
		if plan.AmountFen < 100 || plan.AmountFen%100 != 0 {
			return errors.New("档位「" + plan.Title + "」金额需为 ≥1 元的整数元")
		}
		if seenAmount[plan.AmountFen] {
			return errors.New("档位金额重复：" + strconv.Itoa(int(plan.AmountFen)/100) + " 元")
		}
		seenAmount[plan.AmountFen] = true
	}
	if len(data.CertPrices) != len(CertProductWhitelist) {
		return errors.New("认证道具价格需覆盖全部白名单道具")
	}
	seenProduct := make(map[string]bool)
	for _, cert := range data.CertPrices {
		allowed := false
		for _, id := range CertProductWhitelist {
			if cert.ProductID == id {
				allowed = true
				break
			}
		}
		if !allowed {
			return errors.New("未知的认证道具：" + cert.ProductID)
		}
		if seenProduct[cert.ProductID] {
			return errors.New("认证道具价格重复：" + cert.ProductID)
		}
		if cert.PriceFen < 100 || cert.PriceFen%100 != 0 {
			return errors.New("认证道具「" + cert.ProductID + "」价格需为 ≥1 元的整数元")
		}
		seenProduct[cert.ProductID] = true
	}
	return nil
}

// InitTable 初始化 pricing_configs 表（纯增量，幂等）。
func (m *PricingConfigModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS pricing_configs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  config_key VARCHAR(32) NOT NULL COMMENT '配置键，当前仅 global',
  config_value MEDIUMTEXT NOT NULL COMMENT '配置 JSON 文档',
  updated_by VARCHAR(64) NOT NULL DEFAULT '' COMMENT '最近修改人',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_config_key (config_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='定价配置（充值档位/认证道具价格）';
`
	_, err := m.DB.Exec(schema)
	return err
}

// Get 读取生效配置；未配置过（DB 无记录）返回 (nil, nil)，调用方回退默认值。
func (m *PricingConfigModel) Get() (*PricingData, error) {
	m.mu.RLock()
	if m.cache != nil && time.Since(m.cachedAt) < PricingCacheTTL {
		data := m.cache
		m.mu.RUnlock()
		return data, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	// 双检：拿写锁期间可能已被其他 goroutine 填充
	if m.cache != nil && time.Since(m.cachedAt) < PricingCacheTTL {
		return m.cache, nil
	}
	row := m.DB.QueryRow(`SELECT config_value FROM pricing_configs WHERE config_key = ?`, PricingConfigKeyGlobal)
	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	data := &PricingData{}
	if err := json.Unmarshal([]byte(raw), data); err != nil {
		return nil, errors.New("定价配置 JSON 损坏，请联系管理员核查 pricing_configs 表: " + err.Error())
	}
	m.cache = data
	m.cachedAt = time.Now()
	return data, nil
}

// Save 校验并保存配置，立即失效缓存（下次 Get 重新加载）。
func (m *PricingConfigModel) Save(data *PricingData, updatedBy string) error {
	if err := ValidatePricingData(data); err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = m.DB.Exec(`
		INSERT INTO pricing_configs (config_key, config_value, updated_by) VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE config_value = VALUES(config_value), updated_by = VALUES(updated_by), updated_at = NOW()
	`, PricingConfigKeyGlobal, string(raw), updatedBy)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.cache = nil
	m.mu.Unlock()
	return nil
}
