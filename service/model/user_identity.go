package model

import (
	"database/sql"
	"fmt"
	"time"
)

type UserIdentity struct {
	ID             int64      `json:"id" db:"id"`
	UserID         int64      `json:"user_id" db:"user_id"`
	IdentityType   string     `json:"identity_type" db:"identity_type"`
	IdentityKey    string     `json:"identity_key" db:"identity_key"`
	CredentialHash string     `json:"-" db:"credential_hash"`
	VerifiedAt     *time.Time `json:"verified_at" db:"verified_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

type UserIdentityModel struct {
	DB *sql.DB
}

func NewUserIdentityModel(db *sql.DB) *UserIdentityModel {
	return &UserIdentityModel{DB: db}
}

func (m *UserIdentityModel) InitTable() error {
	_, err := m.DB.Exec(`
CREATE TABLE IF NOT EXISTS user_identities (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  identity_type VARCHAR(32) NOT NULL,
  identity_key VARCHAR(191) NOT NULL,
  credential_hash VARCHAR(255) DEFAULT '',
  verified_at TIMESTAMP NULL DEFAULT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_identity_type_key (identity_type, identity_key),
  KEY idx_identity_type_user (identity_type, user_id)
)`)
	return err
}

func (m *UserIdentityModel) GetUserIDByPhone(phone string) (int64, error) {
	var userID int64
	err := m.DB.QueryRow(
		"SELECT user_id FROM user_identities WHERE identity_type = 'phone' AND identity_key = ?",
		phone,
	).Scan(&userID)
	if err != nil {
		return 0, err
	}
	return userID, nil
}

func (m *UserIdentityModel) BindPhone(userID int64, phone string) error {
	tx, err := m.DB.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	// 1. 删除该用户之前绑定的其他手机号（确保一用户一手机号）
	_, err = tx.Exec(
		"DELETE FROM user_identities WHERE user_id = ? AND identity_type = 'phone' AND identity_key != ?",
		userID, phone,
	)
	if err != nil {
		return fmt.Errorf("清理旧手机号绑定失败: %w", err)
	}

	// 2. 插入或更新绑定（ON DUPLICATE KEY UPDATE 确保幂等）
	_, err = tx.Exec(
		`INSERT INTO user_identities (user_id, identity_type, identity_key, verified_at)
		 VALUES (?, 'phone', ?, NOW())
		 ON DUPLICATE KEY UPDATE user_id = VALUES(user_id), verified_at = NOW()`,
		userID, phone,
	)
	if err != nil {
		return fmt.Errorf("绑定手机号失败: %w", err)
	}

	return tx.Commit()
}

func (m *UserIdentityModel) GetUserPhones(userID int64) ([]string, error) {
	rows, err := m.DB.Query(
		"SELECT identity_key FROM user_identities WHERE identity_type = 'phone' AND user_id = ?",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var phones []string
	for rows.Next() {
		var phone string
		if err := rows.Scan(&phone); err != nil {
			return nil, err
		}
		phones = append(phones, phone)
	}
	return phones, rows.Err()
}

func (m *UserIdentityModel) GetPhoneUserID(phone string) (int64, error) {
	return m.GetUserIDByPhone(phone)
}

func (m *UserIdentityModel) HasPhone(phone string) (bool, error) {
	var exists bool
	err := m.DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM user_identities WHERE identity_type = 'phone' AND identity_key = ?)",
		phone,
	).Scan(&exists)
	return exists, err
}
