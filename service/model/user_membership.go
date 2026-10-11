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
	// PrevExpiredAt 本次授予/续期前的到期时间快照，供退款整单回收回滚用。
	// 首次授予（之前无会员或已过期）为 NULL。只反映最近一次变更，跨多次续期的
	// 历史回滚不支持——退款回收时若会员已被后续订单续期，应跳过并转人工。
	PrevExpiredAt *time.Time `json:"prev_expired_at,omitempty" db:"prev_expired_at"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
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
	// 退款整单回收（T8）：续期前到期时间快照；NULL 表示首次授予
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD COLUMN prev_expired_at DATETIME NULL DEFAULT NULL COMMENT '本次授予/续期前的到期时间'`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD UNIQUE KEY uk_user_id (user_id)`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD KEY idx_status_expired (status, expired_at)`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD KEY idx_plan_id (plan_id)`)
	_, _ = m.DB.Exec(`ALTER TABLE user_memberships ADD KEY idx_source_order_no (source_order_no)`)
	return nil
}

func scanUserMembership(scanner interface {
	Scan(dest ...interface{}) error
}) (*UserMembership, error) {
	item := &UserMembership{}
	var templateDownloadEnabled int
	var prevExpiredAt sql.NullTime
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
		&prevExpiredAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	item.TemplateDownloadEnabled = templateDownloadEnabled == 1
	if prevExpiredAt.Valid {
		t := prevExpiredAt.Time
		item.PrevExpiredAt = &t
	}
	return item, nil
}

func (m *UserMembershipModel) GetByUserID(userID int64) (*UserMembership, error) {
	row := m.DB.QueryRow(`
		SELECT id, user_id, plan_id, plan_code, plan_title, source_order_no, status, template_download_enabled, started_at, granted_at, expired_at, prev_expired_at, created_at, updated_at
		FROM user_memberships
		WHERE user_id = ?
	`, userID)
	return scanUserMembership(row)
}

// CountByPlan 统计引用某会员计划的用户会员数（按 plan_id 或历史 plan_code 匹配）。
// 供删除会员计划前做引用检查，避免产生悬空引用。
func (m *UserMembershipModel) CountByPlan(planID int64, planCode string) (int64, error) {
	var count int64
	err := m.DB.QueryRow(`
		SELECT COUNT(*) FROM user_memberships
		WHERE plan_id = ? OR (plan_id = 0 AND plan_code = ?)
	`, planID, planCode).Scan(&count)
	return count, err
}

func (m *UserMembershipModel) GetActiveByUserID(userID int64) (*UserMembership, error) {
	row := m.DB.QueryRow(`
		SELECT id, user_id, plan_id, plan_code, plan_title, source_order_no, status, template_download_enabled, started_at, granted_at, expired_at, prev_expired_at, created_at, updated_at
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
	var prevExpired interface{}
	if item.PrevExpiredAt != nil {
		prevExpired = *item.PrevExpiredAt
	}
	_, err := m.DB.Exec(`
		INSERT INTO user_memberships (user_id, plan_id, plan_code, plan_title, source_order_no, status, template_download_enabled, started_at, granted_at, expired_at, prev_expired_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			prev_expired_at = VALUES(prev_expired_at),
			updated_at = NOW()
	`, item.UserID, item.PlanID, item.PlanCode, item.PlanTitle, item.SourceOrderNo, item.Status, templateDownloadEnabled, item.StartedAt, item.GrantedAt, item.ExpiredAt, prevExpired)
	return err
}

func (m *UserMembershipModel) GrantOrExtend(userID int64, plan *MembershipPlan, sourceOrderNo string, now time.Time) (*UserMembership, error) {
	startedAt := now
	expiredAt := now.AddDate(0, 0, plan.DurationDays)
	var prevExpiredAt *time.Time
	existing, err := m.GetByUserID(userID)
	if err == nil && existing != nil {
		if existing.Status == "active" && existing.ExpiredAt.After(now) && !IsLifetimeMembership(existing) {
			startedAt = existing.StartedAt
			expiredAt = existing.ExpiredAt.AddDate(0, 0, plan.DurationDays)
			// 续期快照：记录续期前到期时间，退款整单回收时回滚到该时间
			t := existing.ExpiredAt
			prevExpiredAt = &t
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
		PrevExpiredAt:           prevExpiredAt,
	}
	if upsertErr := m.Upsert(item); upsertErr != nil {
		return nil, upsertErr
	}
	return m.GetByUserID(userID)
}

func (m *UserMembershipModel) GrantPermanentDownloadMembership(userID int64, sourceOrderNo string, now time.Time) (*UserMembership, error) {
	startedAt := now
	expiredAt := LifetimeMembershipExpiredAt()
	var prevExpiredAt *time.Time
	existing, err := m.GetByUserID(userID)
	if err == nil && existing != nil {
		if existing.Status == "active" && existing.StartedAt.Before(now) {
			startedAt = existing.StartedAt
			// 永久资格覆盖限时会员同样视为一次"变更"，记快照供退款回滚
			t := existing.ExpiredAt
			prevExpiredAt = &t
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
		PrevExpiredAt:           prevExpiredAt,
	}
	if upsertErr := m.Upsert(item); upsertErr != nil {
		return nil, upsertErr
	}
	return m.GetByUserID(userID)
}

// 退款会员回收动作（RevokeMembershipBySourceOrderNo 返回值语义）。
const (
	MembershipRevokeNotFound         = "not_found"          // 无会员记录
	MembershipRevokeRevoked          = "revoked"            // 首次授予，整段撤销（status 置 revoked）
	MembershipRevokeRestoredPrev     = "restored_prev"      // 续期授予，回滚到续期前到期时间
	MembershipRevokeSkippedSupersede = "skipped_superseded" // 会员已被后续订单续期，跳过转人工
	MembershipRevokeSkippedExpired   = "skipped_not_active" // 会员已过期/非 active，无需回收
)

// RevokeMembershipBySourceOrderNo 退款整单回收：把 source_order_no 对应授予的会员回滚。
//
// 策略（T8 产品决策：整单回收）：
//   - 该订单是会员的最近一次授予者 → 有 prev_expired_at 快照则回滚到期时间，
//     无快照（首次授予）则整段撤销（status='revoked'，下载权限随之失效）
//   - 会员已被后续订单续期（source_order_no 指向别的订单）→ 跳过并返回
//     skipped_superseded，由调用方提示人工评估（跨续期回滚会牵连后续订单的权益）
//   - 会员已过期或非 active → skipped_not_active，无需处理
//
// 幂等：revoked/回滚操作可重复执行（WHERE 条件保证只在合法状态上生效）。
func (m *UserMembershipModel) RevokeMembershipBySourceOrderNo(orderNo string) (string, error) {
	if orderNo == "" {
		return MembershipRevokeNotFound, nil
	}
	item, err := m.GetByUserIDBySourceOrderNo(orderNo)
	if err != nil {
		if err == sql.ErrNoRows {
			return MembershipRevokeNotFound, nil
		}
		return MembershipRevokeNotFound, err
	}
	if item.Status != "active" || !item.ExpiredAt.After(time.Now()) {
		return MembershipRevokeSkippedExpired, nil
	}
	if item.PrevExpiredAt != nil {
		if _, err := m.DB.Exec(
			`UPDATE user_memberships SET expired_at = ?, updated_at = NOW() WHERE user_id = ? AND source_order_no = ? AND status = 'active'`,
			*item.PrevExpiredAt, item.UserID, orderNo,
		); err != nil {
			return MembershipRevokeRestoredPrev, err
		}
		return MembershipRevokeRestoredPrev, nil
	}
	if _, err := m.DB.Exec(
		`UPDATE user_memberships SET status = 'revoked', updated_at = NOW() WHERE user_id = ? AND source_order_no = ? AND status = 'active'`,
		item.UserID, orderNo,
	); err != nil {
		return MembershipRevokeRevoked, err
	}
	return MembershipRevokeRevoked, nil
}

// GetByUserIDBySourceOrderNo 按授予订单号查会员记录；不存在返回 sql.ErrNoRows。
func (m *UserMembershipModel) GetByUserIDBySourceOrderNo(orderNo string) (*UserMembership, error) {
	row := m.DB.QueryRow(`
		SELECT id, user_id, plan_id, plan_code, plan_title, source_order_no, status, template_download_enabled, started_at, granted_at, expired_at, prev_expired_at, created_at, updated_at
		FROM user_memberships
		WHERE source_order_no = ?
		ORDER BY id DESC
		LIMIT 1
	`, orderNo)
	return scanUserMembership(row)
}

func (m *UserMembershipModel) UpdateStatusByUserID(userID int64, status string) error {
	_, err := m.DB.Exec(`UPDATE user_memberships SET status = ?, updated_at = NOW() WHERE user_id = ?`, status, userID)
	return err
}

// UpdateTemplateDownloadEnabled 修复/调整会员模板下载权限（管理端；POST 端点经审计中间件记录）
func (m *UserMembershipModel) UpdateTemplateDownloadEnabled(userID int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := m.DB.Exec(`UPDATE user_memberships SET template_download_enabled = ?, updated_at = NOW() WHERE user_id = ?`, v, userID)
	return err
}
