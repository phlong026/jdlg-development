package model

import (
	"database/sql"
	"fmt"
	"time"
)

var allowedUserIdentityTypes = map[string]bool{
	"乡墅业主": true,
	"设计师":  true,
	"施工队":  true,
	"建材商":  true,
	"机构用户": true,
}

func IsAllowedUserIdentityType(identityType string) bool {
	return allowedUserIdentityTypes[identityType]
}

// UserProfile 用户扩展信息
type UserProfile struct {
	ID                         int64      `json:"id" db:"id"`
	UserID                     int64      `json:"user_id" db:"user_id"`
	Nickname                   string     `json:"nickname" db:"nickname"`
	Avatar                     string     `json:"avatar" db:"avatar"`
	DesignerBio                string     `json:"designer_bio" db:"designer_bio"`
	SpecialtyStyles            string     `json:"specialty_styles" db:"specialty_styles"`
	DesignerExperienceYears    int64      `json:"designer_experience_years" db:"designer_experience_years"`
	ServiceTitle               string     `json:"service_title" db:"service_title"`
	ServiceQuote               int64      `json:"service_quote" db:"service_quote"`
	ServiceIntro               string     `json:"service_intro" db:"service_intro"`
	ServiceEnabled             bool       `json:"service_enabled" db:"service_enabled"`
	DesignerVisible            bool       `json:"designer_visible" db:"designer_visible"`
	EnterpriseWechatVerified   bool       `json:"enterprise_wechat_verified" db:"enterprise_wechat_verified"`
	EnterpriseWechatVerifiedAt *time.Time `json:"enterprise_wechat_verified_at" db:"enterprise_wechat_verified_at"`
	EnterpriseWechatContact    string     `json:"enterprise_wechat_contact" db:"enterprise_wechat_contact"`
	DeviceID                   string     `json:"device_id" db:"device_id"`
	DeviceBindTime             *time.Time `json:"device_bind_time" db:"device_bind_time"`               // 设备绑定时间
	LastDeviceChangeTime       *time.Time `json:"last_device_change_time" db:"last_device_change_time"` // 上次换绑设备时间
	HasPassword                bool       `json:"has_password" db:"has_password"`                       // 是否设置了密码
	Phone                      string     `json:"phone" db:"phone"`                                     // 手机号
	IdentityType               string     `json:"identity_type" db:"identity_type"`                     // 用户身份类型：乡墅业主/设计师/施工队/建材商/机构用户
	CreatedAt                  time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at" db:"updated_at"`
}

// UserProfileModel 用户扩展信息数据访问层
type UserProfileModel struct {
	DB *sql.DB
}

// NewUserProfileModel 创建用户扩展信息模型
func NewUserProfileModel(db *sql.DB) *UserProfileModel {
	return &UserProfileModel{DB: db}
}

const userProfileSelectColumns = `id, user_id, nickname, avatar, designer_bio, specialty_styles, designer_experience_years, service_title, service_quote, service_intro, service_enabled, designer_visible, enterprise_wechat_verified, enterprise_wechat_verified_at, enterprise_wechat_contact, device_id, device_bind_time, last_device_change_time, has_password, phone, identity_type, created_at, updated_at`

func scanUserProfile(scanner interface {
	Scan(dest ...interface{}) error
}) (*UserProfile, error) {
	profile := &UserProfile{}
	err := scanner.Scan(
		&profile.ID, &profile.UserID, &profile.Nickname, &profile.Avatar, &profile.DesignerBio, &profile.SpecialtyStyles, &profile.DesignerExperienceYears, &profile.ServiceTitle, &profile.ServiceQuote, &profile.ServiceIntro, &profile.ServiceEnabled, &profile.DesignerVisible, &profile.EnterpriseWechatVerified, &profile.EnterpriseWechatVerifiedAt, &profile.EnterpriseWechatContact, &profile.DeviceID,
		&profile.DeviceBindTime, &profile.LastDeviceChangeTime, &profile.HasPassword,
		&profile.Phone, &profile.IdentityType,
		&profile.CreatedAt, &profile.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

// InitTable 初始化用户扩展信息表
func (m *UserProfileModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS user_profiles (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	user_id BIGINT UNSIGNED NOT NULL UNIQUE,
	nickname VARCHAR(64) DEFAULT '',
	avatar VARCHAR(512) DEFAULT '',
	designer_bio VARCHAR(1024) DEFAULT '',
	specialty_styles VARCHAR(512) DEFAULT '',
	designer_experience_years BIGINT NOT NULL DEFAULT 0,
	service_title VARCHAR(128) DEFAULT '',
	service_quote BIGINT NOT NULL DEFAULT 0,
	service_intro VARCHAR(1024) DEFAULT '',
	service_enabled TINYINT(1) NOT NULL DEFAULT 0,
	designer_visible TINYINT(1) NOT NULL DEFAULT 1,
	enterprise_wechat_verified TINYINT(1) NOT NULL DEFAULT 0,
	enterprise_wechat_verified_at TIMESTAMP NULL DEFAULT NULL,
	enterprise_wechat_contact VARCHAR(128) DEFAULT '',
	device_id VARCHAR(128) DEFAULT '',
	device_bind_time TIMESTAMP NULL DEFAULT NULL COMMENT '设备绑定时间',
	last_device_change_time TIMESTAMP NULL DEFAULT NULL COMMENT '上次换绑设备时间',
	has_password TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否设置了密码',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	INDEX idx_user_id (user_id),
	INDEX idx_device_id (device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN designer_bio VARCHAR(1024) DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN specialty_styles VARCHAR(512) DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN designer_experience_years BIGINT NOT NULL DEFAULT 0`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN service_title VARCHAR(128) DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN service_quote BIGINT NOT NULL DEFAULT 0`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN service_intro VARCHAR(1024) DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN service_enabled TINYINT(1) NOT NULL DEFAULT 0`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN designer_visible TINYINT(1) NOT NULL DEFAULT 1`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN enterprise_wechat_verified TINYINT(1) NOT NULL DEFAULT 0`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN enterprise_wechat_verified_at TIMESTAMP NULL DEFAULT NULL`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN enterprise_wechat_contact VARCHAR(128) DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN phone VARCHAR(32) DEFAULT '' COMMENT '手机号'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_profiles ADD COLUMN identity_type VARCHAR(32) DEFAULT '' COMMENT '用户身份类型：乡墅业主/设计师/施工队/建材商/机构用户'`)
	return nil
}

// GetByUserID 根据用户ID获取扩展信息
func (m *UserProfileModel) GetByUserID(userID int64) (*UserProfile, error) {
	query := `SELECT ` + userProfileSelectColumns + ` FROM user_profiles WHERE user_id = ?`
	return scanUserProfile(m.DB.QueryRow(query, userID))
}

// Create 创建用户扩展信息
func (m *UserProfileModel) Create(profile *UserProfile) error {
	query := `INSERT INTO user_profiles (user_id, nickname, avatar, designer_bio, specialty_styles, designer_experience_years, service_title, service_quote, service_intro, service_enabled, designer_visible, enterprise_wechat_verified, enterprise_wechat_verified_at, enterprise_wechat_contact, device_id, device_bind_time, has_password, phone, identity_type, created_at, updated_at)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`
	result, err := m.DB.Exec(query, profile.UserID, profile.Nickname, profile.Avatar, profile.DesignerBio, profile.SpecialtyStyles, profile.DesignerExperienceYears, profile.ServiceTitle, profile.ServiceQuote, profile.ServiceIntro, profile.ServiceEnabled, profile.DesignerVisible, profile.EnterpriseWechatVerified, profile.EnterpriseWechatVerifiedAt, profile.EnterpriseWechatContact, profile.DeviceID, profile.DeviceBindTime, profile.HasPassword, profile.Phone, profile.IdentityType)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	profile.ID = id
	return nil
}

// GetOrCreate 获取或创建用户扩展信息
func (m *UserProfileModel) GetOrCreate(userID int64, deviceID string) (*UserProfile, error) {
	profile, err := m.GetByUserID(userID)
	if err == nil {
		return profile, nil
	}

	// 不存在则创建
	now := time.Now()
	profile = &UserProfile{
		UserID:                     userID,
		Nickname:                   "",
		Avatar:                     "",
		DesignerBio:                "",
		SpecialtyStyles:            "",
		DesignerExperienceYears:    0,
		ServiceTitle:               "",
		ServiceQuote:               0,
		ServiceIntro:               "",
		ServiceEnabled:             false,
		DesignerVisible:            true,
		EnterpriseWechatVerified:   false,
		EnterpriseWechatVerifiedAt: nil,
		EnterpriseWechatContact:    "",
		DeviceID:                   deviceID,
		DeviceBindTime:             &now,
		HasPassword:                false,
	}
	if err := m.Create(profile); err != nil {
		return nil, err
	}
	return profile, nil
}

// UpdateNickname 更新昵称
func (m *UserProfileModel) UpdateNickname(userID int64, nickname string) error {
	query := `UPDATE user_profiles SET nickname = ?, updated_at = NOW() WHERE user_id = ?`
	_, err := m.DB.Exec(query, nickname, userID)
	return err
}

// UpdateAvatar 更新头像
func (m *UserProfileModel) UpdateAvatar(userID int64, avatar string) error {
	query := `UPDATE user_profiles SET avatar = ?, updated_at = NOW() WHERE user_id = ?`
	_, err := m.DB.Exec(query, avatar, userID)
	return err
}

func (m *UserProfileModel) UpdateDesignerProfile(userID int64, bio, specialtyStyles string, designerExperienceYears int64, serviceTitle string, serviceQuote int64, serviceIntro string, serviceEnabled bool) error {
	query := `UPDATE user_profiles SET designer_bio = ?, specialty_styles = ?, designer_experience_years = ?, service_title = ?, service_quote = ?, service_intro = ?, service_enabled = ?, updated_at = NOW() WHERE user_id = ?`
	_, err := m.DB.Exec(query, bio, specialtyStyles, designerExperienceYears, serviceTitle, serviceQuote, serviceIntro, serviceEnabled, userID)
	return err
}

func (m *UserProfileModel) SetDesignerVisible(userID int64, visible bool) error {
	query := `UPDATE user_profiles SET designer_visible = ?, updated_at = NOW() WHERE user_id = ?`
	_, err := m.DB.Exec(query, visible, userID)
	return err
}

func (m *UserProfileModel) SetServiceEnabled(userID int64, enabled bool) error {
	query := `UPDATE user_profiles SET service_enabled = ?, updated_at = NOW() WHERE user_id = ?`
	_, err := m.DB.Exec(query, enabled, userID)
	return err
}

// SetHasPassword 设置是否有密码
func (m *UserProfileModel) SetHasPassword(userID int64, hasPassword bool) error {
	query := `UPDATE user_profiles SET has_password = ?, updated_at = NOW() WHERE user_id = ?`
	_, err := m.DB.Exec(query, hasPassword, userID)
	return err
}

// UpdateIdentityType 更新用户身份类型
func (m *UserProfileModel) UpdateIdentityType(userID int64, identityType string) error {
	if identityType != "" && !IsAllowedUserIdentityType(identityType) {
		return fmt.Errorf("不支持的用户身份类型")
	}
	query := `UPDATE user_profiles SET identity_type = ?, updated_at = NOW() WHERE user_id = ?`
	_, err := m.DB.Exec(query, identityType, userID)
	return err
}

// UpdatePhone 更新用户手机号
func (m *UserProfileModel) UpdatePhone(userID int64, phone string) error {
	query := `UPDATE user_profiles SET phone = ?, updated_at = NOW() WHERE user_id = ?`
	_, err := m.DB.Exec(query, phone, userID)
	return err
}

func (m *UserProfileModel) SetEnterpriseWechatVerification(userID int64, verified bool, contact string) error {
	if verified {
		query := `UPDATE user_profiles SET enterprise_wechat_verified = 1, enterprise_wechat_verified_at = NOW(), enterprise_wechat_contact = ?, updated_at = NOW() WHERE user_id = ?`
		_, err := m.DB.Exec(query, contact, userID)
		return err
	}
	query := `UPDATE user_profiles SET enterprise_wechat_verified = 0, enterprise_wechat_verified_at = NULL, enterprise_wechat_contact = ?, updated_at = NOW() WHERE user_id = ?`
	_, err := m.DB.Exec(query, contact, userID)
	return err
}
