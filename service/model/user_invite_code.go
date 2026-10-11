package model

import (
	"crypto/rand"
	"database/sql"
	"math/big"
	"strings"
	"time"
)

// 邀请码字符集：0-9 + A-Z（去掉易混淆的 0/O、1/I/L），共 32 字符
const inviteCodeChars = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
const inviteCodeLength = 6

// UserInviteCode 用户邀请码（6 位数字+字母，唯一）
type UserInviteCode struct {
	ID         int64     `json:"id" db:"id"`
	UserID     int64     `json:"user_id" db:"user_id"`
	InviteCode string    `json:"invite_code" db:"invite_code"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// UserInviteCodeModel 用户邀请码数据访问层
type UserInviteCodeModel struct {
	DB *sql.DB
}

// NewUserInviteCodeModel 创建模型
func NewUserInviteCodeModel(db *sql.DB) *UserInviteCodeModel {
	return &UserInviteCodeModel{DB: db}
}

// InitTable 初始化表
func (m *UserInviteCodeModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS user_invite_codes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
  invite_code VARCHAR(16) NOT NULL COMMENT '6位邀请码，数字+字母唯一',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_user_id (user_id),
  UNIQUE KEY uk_invite_code (invite_code),
  KEY idx_invite_code (invite_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户邀请码';
`
	_, err := m.DB.Exec(schema)
	return err
}

// generateRandomCode 生成随机 6 位邀请码（字符集内）
func generateRandomCode() (string, error) {
	b := make([]byte, inviteCodeLength)
	max := big.NewInt(int64(len(inviteCodeChars)))
	for i := 0; i < inviteCodeLength; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = inviteCodeChars[n.Int64()]
	}
	return string(b), nil
}

// ExistsByCode 检查邀请码是否已存在
func (m *UserInviteCodeModel) ExistsByCode(code string) (bool, error) {
	var count int
	err := m.DB.QueryRow(`SELECT COUNT(1) FROM user_invite_codes WHERE invite_code = ?`, code).Scan(&count)
	return count > 0, err
}

// GetUserIDByCode 根据邀请码查询用户 ID，不存在返回 0, false
func (m *UserInviteCodeModel) GetUserIDByCode(code string) (int64, bool, error) {
	if code == "" {
		return 0, false, nil
	}
	var userID int64
	err := m.DB.QueryRow(`SELECT user_id FROM user_invite_codes WHERE invite_code = ? LIMIT 1`, code).Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return userID, true, nil
}

// GetByUserID 根据用户 ID 查询邀请码
func (m *UserInviteCodeModel) GetByUserID(userID int64) (*UserInviteCode, error) {
	row := &UserInviteCode{}
	err := m.DB.QueryRow(
		`SELECT id, user_id, invite_code, created_at FROM user_invite_codes WHERE user_id = ? LIMIT 1`,
		userID,
	).Scan(&row.ID, &row.UserID, &row.InviteCode, &row.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// Create 插入一条邀请码记录
func (m *UserInviteCodeModel) Create(userID int64, code string) error {
	_, err := m.DB.Exec(
		`INSERT INTO user_invite_codes (user_id, invite_code, created_at) VALUES (?, ?, NOW())`,
		userID, code,
	)
	return err
}

// GetOrCreateForUser 获取用户邀请码，若不存在则生成并写入（保证唯一）
func (m *UserInviteCodeModel) GetOrCreateForUser(userID int64) (string, error) {
	row, err := m.GetByUserID(userID)
	if err != nil {
		return "", err
	}
	if row != nil {
		return row.InviteCode, nil
	}
	// 生成新邀请码，冲突则重试
	for i := 0; i < 20; i++ {
		code, err := generateRandomCode()
		if err != nil {
			return "", err
		}
		exists, err := m.ExistsByCode(code)
		if err != nil {
			return "", err
		}
		if exists {
			continue
		}
		if err := m.Create(userID, code); err != nil {
			// 唯一键冲突（并发下可能发生）
			if isDuplicateKey(err) {
				continue
			}
			return "", err
		}
		return code, nil
	}
	return "", nil
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Duplicate") || strings.Contains(s, "1062") || strings.Contains(s, "UNIQUE")
}

// ResolveInviteCodeToUserID 解析邀请码为邀请人 user_id：先查 user_invite_codes，再兼容纯数字（旧邀请码）
func (m *UserInviteCodeModel) ResolveInviteCodeToUserID(inviteCode string) (int64, bool) {
	if inviteCode == "" {
		return 0, false
	}
	userID, ok, _ := m.GetUserIDByCode(inviteCode)
	if ok && userID > 0 {
		return userID, true
	}
	// 兼容旧逻辑：纯数字视为 user_id
	id, ok := ParseInviteCodeToUserID(inviteCode)
	return id, ok
}
