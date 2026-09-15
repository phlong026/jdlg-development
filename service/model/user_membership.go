package model

import (
	"database/sql"
	"time"
)

const (
	DefaultRechargePermanentPlanCode  = "default_recharge_permanent"
	DefaultRechargePermanentPlanTitle = "下载会员（长期有效）"
)

func LifetimeMembershipExpiredAt() time.Time {
	return time.Date(2099, 12, 31, 23, 59, 59, 0, time.Local)
}

func IsLifetimeMembership(item *UserMembership) bool {
	if item == nil {
		return false
	}
	if item.PlanCode == DefaultRechargePermanentPlanCode {
		return true
	}
	return item.ExpiredAt.Year() >= 2099
}

type UserMembership struct {
	ID                      int64     `json:"id" db:"id"`
	UserID                  int64     `json:"user_id" db:"user_id"`
	PlanID                  int64     `json:"plan_id" db:"plan_id"`
	PlanCode                string    `json:"plan_code" db:"plan_code"`
	PlanTitle               string    `json:"plan_title" db:"plan_title"`
	SourceOrderNo           string    `json:"source_order_no" db:"source_order_no"`
	Status                  string    `json:"status" db:"status"`
	TemplateDownloadEnabled bool      `json:"template_download_enabled" db:"template_download_enabled"`
	StartedAt               time.Time `json:"started_at" db:"started_at"`
	GrantedAt               time.Time `json:"granted_at" db:"granted_at"`
	ExpiredAt               time.Time `json:"expired_at" db:"expired_at"`
	CreatedAt               time.Time `json:"created_at" db:"created_at"`
	UpdatedAt               time.Time `json:"updated_at" db:"updated_at"`
}

type UserMembershipModel struct {
	DB *sql.DB
}

func NewUserMembershipModel(db *sql.DB) *UserMembershipModel {
	return &UserMembershipModel{DB: db}
}

func (m *UserMembershipModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS user_memberships (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  plan_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  plan_code VARCHAR(64) NOT NULL DEFAULT '',
  plan_title VARCHAR(128) NOT NULL DEFAULT '',
  source_order_no VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'inactive',
  template_download_enabled TINYINT(1) NOT NULL DEFAULT 1,
  started_at DATETIME NOT NULL,
  granted_at DATETIME NOT NULL,
  expired_at DATETIME NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_user_id (user_id),
  KEY idx_status_expired (status, expired_at),
  KEY idx_plan_id (plan_id),
  KEY idx_source_order_no (source_order_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD COLUMN plan_id BIGINT UNSIGNED NOT NULL DEFAULT 0`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD COLUMN plan_code VARCHAR(64) NOT NULL DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD COLUMN plan_title VARCHAR(128) NOT NULL DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD COLUMN source_order_no VARCHAR(64) NOT NULL DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD COLUMN status VARCHAR(32) NOT NULL DEFAULT 'inactive'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD COLUMN template_download_enabled TINYINT(1) NOT NULL DEFAULT 1`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD COLUMN started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD COLUMN granted_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD COLUMN expired_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD UNIQUE KEY uk_user_id (user_id)`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD KEY idx_status_expired (status, expired_at)`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD KEY idx_plan_id (plan_id)`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD KEY idx_source_order_no (source_order_no)`)
	return nil
}

func scanUserMembership(scanner interface{ Scan(dest ...interface{}) error }) (*UserMembership, error) {
	item := &UserMembership{}
	var templateDownloadEnabled int
	if err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.PlanID,
		&item.PlanCode,
		&item.PlanTitle,
		&item.SourceOrderNo,
		&item.Status,
		&templateDownloadEnabled,
		&item.StartedAt,
		&item.GrantedAt,
		&item.ExpiredAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	item.TemplateDownloadEnabled = templateDownloadEnabled == 1
	return item, nil
}

func (m *UserMembershipModel) GetByUserID(userID int64) (*UserMembership, error) {
	row := m.DB.QueryRow(`
		SELECT id, user_id, plan_id, plan_code, plan_title, source_order_no, status, template_download_enabled, started_at, granted_at, expired_at, created_at, updated_at
		FROM user_memberships
		WHERE user_id = ?
	`, userID)
	return scanUserMembership(row)
}

func (m *UserMembershipModel) GetActiveByUserID(userID int64) (*UserMembership, error) {
	row := m.DB.QueryRow(`
		SELECT id, user_id, plan_id, plan_code, plan_title, source_order_no, status, template_download_enabled, started_at, granted_at, expired_at, created_at, updated_at
		FROM user_memberships
		WHERE user_id = ? AND status = 'active' AND expired_at > NOW()
		LIMIT 1
	`, userID)
	return scanUserMembership(row)
}

func (m *UserMembershipModel) Upsert(item *UserMembership) error {
	templateDownloadEnabled := 0
	if item.TemplateDownloadEnabled {
		templateDownloadEnabled = 1
	}
	_, err := m.DB.Exec(`
		INSERT INTO user_memberships (user_id, plan_id, plan_code, plan_title, source_order_no, status, template_download_enabled, started_at, granted_at, expired_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			plan_id = VALUES(plan_id),
			plan_code = VALUES(plan_code),
			plan_title = VALUES(plan_title),
			source_order_no = VALUES(source_order_no),
			status = VALUES(status),
			template_download_enabled = VALUES(template_download_enabled),
			started_at = VALUES(started_at),
			granted_at = VALUES(granted_at),
			expired_at = VALUES(expired_at),
			updated_at = NOW()
	`, item.UserID, item.PlanID, item.PlanCode, item.PlanTitle, item.SourceOrderNo, item.Status, templateDownloadEnabled, item.StartedAt, item.GrantedAt, item.ExpiredAt)
	return err
}

func (m *UserMembershipModel) GrantOrExtend(userID int64, plan *MembershipPlan, sourceOrderNo string, now time.Time) (*UserMembership, error) {
	startedAt := now
	expiredAt := now.AddDate(0, 0, plan.DurationDays)
	existing, err := m.GetByUserID(userID)
	if err == nil && existing != nil {
		if existing.Status == "active" && existing.ExpiredAt.After(now) && !IsLifetimeMembership(existing) {
			startedAt = existing.StartedAt
			expiredAt = existing.ExpiredAt.AddDate(0, 0, plan.DurationDays)
		}
	}
	item := &UserMembership{
		UserID:                  userID,
		PlanID:                  plan.ID,
		PlanCode:                plan.PlanCode,
		PlanTitle:               plan.Title,
		SourceOrderNo:           sourceOrderNo,
		Status:                  "active",
		TemplateDownloadEnabled: plan.TemplateDownloadEnabled,
		StartedAt:               startedAt,
		GrantedAt:               now,
		ExpiredAt:               expiredAt,
	}
	if upsertErr := m.Upsert(item); upsertErr != nil {
		return nil, upsertErr
	}
	return m.GetByUserID(userID)
}

func (m *UserMembershipModel) GrantPermanentDownloadMembership(userID int64, sourceOrderNo string, now time.Time) (*UserMembership, error) {
	startedAt := now
	expiredAt := LifetimeMembershipExpiredAt()
	existing, err := m.GetByUserID(userID)
	if err == nil && existing != nil {
		if existing.Status == "active" && existing.StartedAt.Before(now) {
			startedAt = existing.StartedAt
		}
	}
	item := &UserMembership{
		UserID:                  userID,
		PlanID:                  0,
		PlanCode:                DefaultRechargePermanentPlanCode,
		PlanTitle:               DefaultRechargePermanentPlanTitle,
		SourceOrderNo:           sourceOrderNo,
		Status:                  "active",
		TemplateDownloadEnabled: true,
		StartedAt:               startedAt,
		GrantedAt:               now,
		ExpiredAt:               expiredAt,
	}
	if upsertErr := m.Upsert(item); upsertErr != nil {
		return nil, upsertErr
	}
	return m.GetByUserID(userID)
}

func (m *UserMembershipModel) UpdateStatusByUserID(userID int64, status string) error {
	_, err := m.DB.Exec(`UPDATE user_memberships SET status = ?, updated_at = NOW() WHERE user_id = ?`, status, userID)
	return err
}
