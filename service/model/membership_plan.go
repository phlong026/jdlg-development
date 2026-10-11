package model

import (
	"database/sql"
	"time"
)

type MembershipPlan struct {
	ID                      int64     `json:"id" db:"id"`
	PlanCode                string    `json:"plan_code" db:"plan_code"`
	Title                   string    `json:"title" db:"title"`
	Description             string    `json:"description" db:"description"`
	BadgeText               string    `json:"badge_text" db:"badge_text"`
	RechargeAmountFen       int64     `json:"recharge_amount_fen" db:"recharge_amount_fen"`
	DurationDays            int       `json:"duration_days" db:"duration_days"`
	TemplateDownloadEnabled bool      `json:"template_download_enabled" db:"template_download_enabled"`
	IsEnabled               bool      `json:"is_enabled" db:"is_enabled"`
	SortOrder               int       `json:"sort_order" db:"sort_order"`
	DownloadValidityDays    int       `json:"download_validity_days" db:"download_validity_days"`       // 下载有效期天数
	MaxTotalDownloads       int       `json:"max_total_downloads" db:"max_total_downloads"`             // 累计可下载模板数，0=无限
	DailyDownloadLimit      int       `json:"daily_download_limit" db:"daily_download_limit"`           // 每日下载上限，0=无限
	RateLimitPerMinute      int       `json:"rate_limit_per_minute" db:"rate_limit_per_minute"`         // 频控：每分钟最多请求次数，0=无限
	CreatedAt               time.Time `json:"created_at" db:"created_at"`
	UpdatedAt               time.Time `json:"updated_at" db:"updated_at"`
}

type MembershipPlanModel struct {
	DB *sql.DB
}

func NewMembershipPlanModel(db *sql.DB) *MembershipPlanModel {
	return &MembershipPlanModel{DB: db}
}

func (m *MembershipPlanModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS membership_plans (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  plan_code VARCHAR(64) NOT NULL,
  title VARCHAR(128) NOT NULL DEFAULT '',
  description VARCHAR(255) NOT NULL DEFAULT '',
  badge_text VARCHAR(64) NOT NULL DEFAULT '',
  recharge_amount_fen BIGINT NOT NULL DEFAULT 0,
  duration_days INT NOT NULL DEFAULT 0,
  template_download_enabled TINYINT(1) NOT NULL DEFAULT 1,
  is_enabled TINYINT(1) NOT NULL DEFAULT 1,
  sort_order INT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_plan_code (plan_code),
  KEY idx_enabled_sort (is_enabled, sort_order),
  KEY idx_recharge_amount_fen (recharge_amount_fen)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD COLUMN badge_text VARCHAR(64) NOT NULL DEFAULT ''`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD COLUMN recharge_amount_fen BIGINT NOT NULL DEFAULT 0`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD COLUMN duration_days INT NOT NULL DEFAULT 0`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD COLUMN template_download_enabled TINYINT(1) NOT NULL DEFAULT 1`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD COLUMN is_enabled TINYINT(1) NOT NULL DEFAULT 1`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD COLUMN sort_order INT NOT NULL DEFAULT 0`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD COLUMN download_validity_days INT NOT NULL DEFAULT 0 COMMENT '下载有效期天数'`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD COLUMN max_total_downloads INT NOT NULL DEFAULT 0 COMMENT '累计可下载模板数，0=无限'`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD COLUMN daily_download_limit INT NOT NULL DEFAULT 0 COMMENT '每日下载上限，0=无限'`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD COLUMN rate_limit_per_minute INT NOT NULL DEFAULT 0 COMMENT '频控：每分钟最多请求次数，0=无限'`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD UNIQUE KEY uk_plan_code (plan_code)`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD KEY idx_enabled_sort (is_enabled, sort_order)`)
	_, _ = m.DB.Exec(`ALTER TABLE membership_plans ADD KEY idx_recharge_amount_fen (recharge_amount_fen)`)
	return nil
}

func scanMembershipPlan(scanner interface{ Scan(dest ...interface{}) error }) (*MembershipPlan, error) {
	item := &MembershipPlan{}
	var templateDownloadEnabled int
	var isEnabled int
	if err := scanner.Scan(
		&item.ID,
		&item.PlanCode,
		&item.Title,
		&item.Description,
		&item.BadgeText,
		&item.RechargeAmountFen,
		&item.DurationDays,
		&templateDownloadEnabled,
		&isEnabled,
		&item.SortOrder,
		&item.DownloadValidityDays,
		&item.MaxTotalDownloads,
		&item.DailyDownloadLimit,
		&item.RateLimitPerMinute,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	item.TemplateDownloadEnabled = templateDownloadEnabled == 1
	item.IsEnabled = isEnabled == 1
	return item, nil
}

func (m *MembershipPlanModel) List() ([]*MembershipPlan, error) {
	rows, err := m.DB.Query(`
		SELECT id, plan_code, title, description, badge_text, recharge_amount_fen, duration_days, template_download_enabled, is_enabled, sort_order, download_validity_days, max_total_downloads, daily_download_limit, rate_limit_per_minute, created_at, updated_at
		FROM membership_plans
		ORDER BY sort_order ASC, recharge_amount_fen ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]*MembershipPlan, 0)
	for rows.Next() {
		item, scanErr := scanMembershipPlan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		list = append(list, item)
	}
	return list, nil
}

func (m *MembershipPlanModel) ListEnabled() ([]*MembershipPlan, error) {
	rows, err := m.DB.Query(`
		SELECT id, plan_code, title, description, badge_text, recharge_amount_fen, duration_days, template_download_enabled, is_enabled, sort_order, download_validity_days, max_total_downloads, daily_download_limit, rate_limit_per_minute, created_at, updated_at
		FROM membership_plans
		WHERE is_enabled = 1
		ORDER BY sort_order ASC, recharge_amount_fen ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]*MembershipPlan, 0)
	for rows.Next() {
		item, scanErr := scanMembershipPlan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		list = append(list, item)
	}
	return list, nil
}

func (m *MembershipPlanModel) GetByID(id int64) (*MembershipPlan, error) {
	row := m.DB.QueryRow(`
		SELECT id, plan_code, title, description, badge_text, recharge_amount_fen, duration_days, template_download_enabled, is_enabled, sort_order, download_validity_days, max_total_downloads, daily_download_limit, rate_limit_per_minute, created_at, updated_at
		FROM membership_plans
		WHERE id = ?
	`, id)
	return scanMembershipPlan(row)
}

func (m *MembershipPlanModel) GetByCode(planCode string) (*MembershipPlan, error) {
	row := m.DB.QueryRow(`
		SELECT id, plan_code, title, description, badge_text, recharge_amount_fen, duration_days, template_download_enabled, is_enabled, sort_order, download_validity_days, max_total_downloads, daily_download_limit, rate_limit_per_minute, created_at, updated_at
		FROM membership_plans
		WHERE plan_code = ?
	`, planCode)
	return scanMembershipPlan(row)
}

func (m *MembershipPlanModel) FindMatchedByRechargeAmountFen(amountFen int64) (*MembershipPlan, error) {
	row := m.DB.QueryRow(`
		SELECT id, plan_code, title, description, badge_text, recharge_amount_fen, duration_days, template_download_enabled, is_enabled, sort_order, download_validity_days, max_total_downloads, daily_download_limit, rate_limit_per_minute, created_at, updated_at
		FROM membership_plans
		WHERE is_enabled = 1 AND recharge_amount_fen = ?
		ORDER BY sort_order ASC, id ASC
		LIMIT 1
	`, amountFen)
	item, err := scanMembershipPlan(row)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (m *MembershipPlanModel) CreateOrUpdate(plan *MembershipPlan) error {
	templateDownloadEnabled := 0
	if plan.TemplateDownloadEnabled {
		templateDownloadEnabled = 1
	}
	isEnabled := 0
	if plan.IsEnabled {
		isEnabled = 1
	}
	if plan.ID > 0 {
		_, err := m.DB.Exec(`
			UPDATE membership_plans
			SET plan_code = ?, title = ?, description = ?, badge_text = ?, recharge_amount_fen = ?, duration_days = ?, template_download_enabled = ?, is_enabled = ?, sort_order = ?, download_validity_days = ?, max_total_downloads = ?, daily_download_limit = ?, rate_limit_per_minute = ?, updated_at = NOW()
			WHERE id = ?
		`, plan.PlanCode, plan.Title, plan.Description, plan.BadgeText, plan.RechargeAmountFen, plan.DurationDays, templateDownloadEnabled, isEnabled, plan.SortOrder, plan.DownloadValidityDays, plan.MaxTotalDownloads, plan.DailyDownloadLimit, plan.RateLimitPerMinute, plan.ID)
		return err
	}
	result, err := m.DB.Exec(`
		INSERT INTO membership_plans (plan_code, title, description, badge_text, recharge_amount_fen, duration_days, template_download_enabled, is_enabled, sort_order, download_validity_days, max_total_downloads, daily_download_limit, rate_limit_per_minute)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, plan.PlanCode, plan.Title, plan.Description, plan.BadgeText, plan.RechargeAmountFen, plan.DurationDays, templateDownloadEnabled, isEnabled, plan.SortOrder, plan.DownloadValidityDays, plan.MaxTotalDownloads, plan.DailyDownloadLimit, plan.RateLimitPerMinute)
	if err != nil {
		return err
	}
	if insertedID, lastErr := result.LastInsertId(); lastErr == nil {
		plan.ID = insertedID
	}
	return nil
}

func (m *MembershipPlanModel) Delete(id int64) error {
	_, err := m.DB.Exec(`DELETE FROM membership_plans WHERE id = ?`, id)
	return err
}
