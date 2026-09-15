package model

import (
	"database/sql"
	"encoding/json"
	"time"
)

// RechargeConfig 充值配置结构
type RechargeConfig struct {
	ID          int64     `json:"id" db:"id"`
	PaymentMode string    `json:"payment_mode" db:"payment_mode"` // static_qrcode, wechat_only, alipay_only, wechat_alipay
	Config      string    `json:"config" db:"config"`              // JSON配置，包含二维码URL、账号等信息
	IsEnabled   bool      `json:"is_enabled" db:"is_enabled"`     // 是否启用
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`

	// 解析后的配置（非数据库字段）
	ConfigData *RechargeConfigData `json:"config_data,omitempty"`
}

// RechargeConfigData 充值配置数据
type RechargeConfigData struct {
	// 静态二维码模式
	WechatQRCode string `json:"wechat_qrcode,omitempty"` // 微信收款码URL
	AlipayQRCode string `json:"alipay_qrcode,omitempty"`  // 支付宝收款码URL

	// 微信单总模式
	WechatAccount string `json:"wechat_account,omitempty"` // 微信账号
	WechatName    string `json:"wechat_name,omitempty"`    // 微信昵称

	// 支付宝单总模式
	AlipayAccount string `json:"alipay_account,omitempty"` // 支付宝账号
	AlipayName    string `json:"alipay_name,omitempty"`   // 支付宝昵称

	// 备注信息
	Note string `json:"note,omitempty"` // 备注说明
}

// RechargeConfigModel 充值配置模型
type RechargeConfigModel struct {
	DB *sql.DB
}

// NewRechargeConfigModel 创建充值配置模型
func NewRechargeConfigModel(db *sql.DB) *RechargeConfigModel {
	return &RechargeConfigModel{DB: db}
}

// InitTable 初始化recharge_config表
func (m *RechargeConfigModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS recharge_config (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	payment_mode VARCHAR(32) NOT NULL COMMENT '支付方式：static_qrcode(静态二维码), wechat_only(微信单总), alipay_only(支付宝单总), wechat_alipay(支付宝微信两种)',
	config TEXT COMMENT '配置信息（JSON格式）',
	is_enabled TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否启用：0-否，1-是',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	UNIQUE KEY uk_payment_mode (payment_mode)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	return nil
}

// GetEnabled 获取启用的充值配置
func (m *RechargeConfigModel) GetEnabled() (*RechargeConfig, error) {
	query := `SELECT id, payment_mode, config, is_enabled, created_at, updated_at
	          FROM recharge_config WHERE is_enabled = 1 LIMIT 1`
	config := &RechargeConfig{}
	var isEnabledInt int
	err := m.DB.QueryRow(query).Scan(
		&config.ID, &config.PaymentMode, &config.Config, &isEnabledInt,
		&config.CreatedAt, &config.UpdatedAt)
	if err != nil {
		return nil, err
	}
	config.IsEnabled = isEnabledInt == 1

	// 解析配置JSON
	if config.Config != "" {
		var configData RechargeConfigData
		if err := json.Unmarshal([]byte(config.Config), &configData); err == nil {
			config.ConfigData = &configData
		}
	}

	return config, nil
}

// GetByPaymentMode 根据支付方式获取配置
func (m *RechargeConfigModel) GetByPaymentMode(paymentMode string) (*RechargeConfig, error) {
	query := `SELECT id, payment_mode, config, is_enabled, created_at, updated_at
	          FROM recharge_config WHERE payment_mode = ?`
	config := &RechargeConfig{}
	var isEnabledInt int
	err := m.DB.QueryRow(query, paymentMode).Scan(
		&config.ID, &config.PaymentMode, &config.Config, &isEnabledInt,
		&config.CreatedAt, &config.UpdatedAt)
	if err != nil {
		return nil, err
	}
	config.IsEnabled = isEnabledInt == 1

	// 解析配置JSON
	if config.Config != "" {
		var configData RechargeConfigData
		if err := json.Unmarshal([]byte(config.Config), &configData); err == nil {
			config.ConfigData = &configData
		}
	}

	return config, nil
}

// List 获取所有充值配置列表
func (m *RechargeConfigModel) List() ([]*RechargeConfig, error) {
	query := `SELECT id, payment_mode, config, is_enabled, created_at, updated_at
	          FROM recharge_config ORDER BY created_at DESC`
	rows, err := m.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []*RechargeConfig
	for rows.Next() {
		config := &RechargeConfig{}
		var isEnabledInt int
		err := rows.Scan(
			&config.ID, &config.PaymentMode, &config.Config, &isEnabledInt,
			&config.CreatedAt, &config.UpdatedAt)
		if err != nil {
			continue
		}
		config.IsEnabled = isEnabledInt == 1

		// 解析配置JSON
		if config.Config != "" {
			var configData RechargeConfigData
			if err := json.Unmarshal([]byte(config.Config), &configData); err == nil {
				config.ConfigData = &configData
			}
		}

		configs = append(configs, config)
	}
	return configs, nil
}

// CreateOrUpdate 创建或更新充值配置
func (m *RechargeConfigModel) CreateOrUpdate(config *RechargeConfig) error {
	// 序列化配置数据
	configJSON, err := json.Marshal(config.ConfigData)
	if err != nil {
		return err
	}
	config.Config = string(configJSON)

	// 检查是否已存在
	existing, err := m.GetByPaymentMode(config.PaymentMode)
	if err == nil && existing != nil {
		// 更新
		isEnabledInt := 0
		if config.IsEnabled {
			isEnabledInt = 1
		}
		query := `UPDATE recharge_config SET config = ?, is_enabled = ?, updated_at = NOW() WHERE payment_mode = ?`
		_, err = m.DB.Exec(query, config.Config, isEnabledInt, config.PaymentMode)
		return err
	}

	// 创建
	isEnabledInt := 0
	if config.IsEnabled {
		isEnabledInt = 1
	}
	query := `INSERT INTO recharge_config (payment_mode, config, is_enabled)
	          VALUES (?, ?, ?)`
	result, err := m.DB.Exec(query, config.PaymentMode, config.Config, isEnabledInt)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	config.ID = id
	return nil
}

// Delete 删除充值配置
func (m *RechargeConfigModel) Delete(id int64) error {
	query := "DELETE FROM recharge_config WHERE id = ?"
	_, err := m.DB.Exec(query, id)
	return err
}
