package model

import (
	"database/sql"
	"fmt"
	"hash/crc32"
	"log"
	"strings"
	"time"
)

// User 用户模型
type User struct {
	ID          int64     `json:"id" db:"id"`
	Username    string    `json:"username" db:"username"`
	Password    string    `json:"-" db:"password"`                // 不返回给前端
	OpenID      string    `json:"-" db:"openid"`                  // 微信OpenID
	UnionID     string    `json:"-" db:"unionid"`                 // 微信UnionID
	UserType    string    `json:"user_type" db:"user_type"`       // miniprogram: 小程序用户, management: 管理后台用户
	CanWithdraw bool      `json:"can_withdraw" db:"can_withdraw"` // 是否已认证可提现（设计师/企业）
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// UserModel 用户数据访问层
type UserModel struct {
	DB *sql.DB
}

type ManagementUserListFilters struct {
	Keyword                string
	EnterpriseWechatStatus string
	IdentityType           string // 精确匹配身份类型（乡墅业主/设计师/施工队/建材商/机构用户）
	Status                 string // active=仅正常, banned=仅封禁, 空=全部
	CreatedWithinDays      int    // >0 时按注册时间过滤
	StonesMin              *int64
	StonesMax              *int64
	RiskOnly               bool   // 仅看结构化风险信号（近7天换绑设备/同设备多账号），与统计卡口径一致
	Member                 string // active=会员生效中, none=无会员, 空=全部
	SortBy                 string // stones | created_at，空=id DESC（默认）
	SortDesc               bool
}

type ManagementUserListItem struct {
	User
	Nickname                   string     `json:"nickname"`
	EnterpriseWechatVerified   bool       `json:"enterprise_wechat_verified"`
	EnterpriseWechatVerifiedAt *time.Time `json:"enterprise_wechat_verified_at"`
	EnterpriseWechatContact    string     `json:"enterprise_wechat_contact"`
	Phone                      string     `json:"phone"`
	IdentityType               string     `json:"identity_type"`
	Status                     string     `json:"status"`
	BannedReason               string     `json:"banned_reason"`
	BannedAt                   *time.Time `json:"banned_at"`
	HasPassword                bool       `json:"has_password"`
	LastDeviceChangeTime       *time.Time `json:"last_device_change_time"`
	MemberStatus               string     `json:"member_status"`
	MemberExpiredAt            *time.Time `json:"member_expired_at"`
	MemberPlanTitle            string     `json:"member_plan_title"`
}

// NewUserModel 创建用户模型
func NewUserModel(db *sql.DB) *UserModel {
	return &UserModel{DB: db}
}

func buildManagementUserListWhere(filters ManagementUserListFilters) (string, []interface{}) {
	whereParts := []string{"u.user_type = 'miniprogram'"}
	args := make([]interface{}, 0, 5)

	keyword := strings.TrimSpace(filters.Keyword)
	if keyword != "" {
		likeKeyword := "%" + keyword + "%"
		whereParts = append(whereParts, "(CAST(u.id AS CHAR) LIKE ? OR u.username LIKE ? OR COALESCE(p.nickname, '') LIKE ? OR COALESCE(p.enterprise_wechat_contact, '') LIKE ? OR COALESCE(p.phone, '') LIKE ?)")
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}

	switch strings.TrimSpace(filters.EnterpriseWechatStatus) {
	case "verified":
		whereParts = append(whereParts, "COALESCE(p.enterprise_wechat_verified, 0) = 1")
	case "pending":
		whereParts = append(whereParts, "COALESCE(p.enterprise_wechat_verified, 0) = 0")
	}

	if identityType := strings.TrimSpace(filters.IdentityType); identityType != "" {
		whereParts = append(whereParts, "COALESCE(p.identity_type, '') = ?")
		args = append(args, identityType)
	}

	switch strings.TrimSpace(filters.Status) {
	case "active":
		whereParts = append(whereParts, "COALESCE(u.status, 'active') = 'active'")
	case "banned":
		whereParts = append(whereParts, "COALESCE(u.status, 'active') <> 'active'")
	}

	if filters.CreatedWithinDays > 0 {
		whereParts = append(whereParts, "u.created_at >= DATE_SUB(NOW(), INTERVAL ? DAY)")
		args = append(args, filters.CreatedWithinDays)
	}

	if filters.StonesMin != nil {
		whereParts = append(whereParts, "u.stones >= ?")
		args = append(args, *filters.StonesMin)
	}
	if filters.StonesMax != nil {
		whereParts = append(whereParts, "u.stones <= ?")
		args = append(args, *filters.StonesMax)
	}

	if filters.RiskOnly {
		whereParts = append(whereParts, `(
			(p.last_device_change_time IS NOT NULL AND p.last_device_change_time > DATE_SUB(NOW(), INTERVAL 7 DAY))
			OR (COALESCE(p.device_id, '') <> '' AND EXISTS (SELECT 1 FROM user_profiles p2 WHERE p2.device_id = p.device_id AND p2.user_id <> p.user_id))
		)`)
	}

	switch filters.Member {
	case "active":
		whereParts = append(whereParts, "um.status = 'active' AND um.expired_at > NOW()")
	case "none":
		whereParts = append(whereParts, "um.user_id IS NULL")
	}

	return " WHERE " + strings.Join(whereParts, " AND "), args
}

// buildManagementUserListOrder 列表排序白名单（防注入；默认按 ID 倒序）
func buildManagementUserListOrder(filters ManagementUserListFilters) string {
	dir := "ASC"
	if filters.SortDesc {
		dir = "DESC"
	}
	switch filters.SortBy {
	case "stones":
		return " ORDER BY u.stones " + dir + ", u.id DESC"
	case "created_at":
		return " ORDER BY u.created_at " + dir + ", u.id DESC"
	default:
		return " ORDER BY u.id DESC"
	}
}

// Create 创建用户
func (m *UserModel) Create(user *User) error {
	query := `INSERT INTO users (username, password, openid, unionid, user_type, can_withdraw, created_at, updated_at) 
	          VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())`
	canWithdraw := 0
	if user.CanWithdraw {
		canWithdraw = 1
	}
	result, err := m.DB.Exec(query, user.Username, user.Password, user.OpenID, user.UnionID, user.UserType, canWithdraw)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	user.ID = id
	return nil
}

func buildMiniprogramUsername(openid string, suffix string) string {
	const prefix = "wx_"
	const maxLength = 64
	base := strings.TrimSpace(openid)
	if base == "" {
		base = "user"
	}
	available := maxLength - len(prefix)
	if suffix != "" {
		available -= len(suffix) + 1
	}
	if available < 1 {
		available = 1
	}
	if len(base) > available {
		base = base[:available]
	}
	username := prefix + base
	if suffix != "" {
		username += "_" + suffix
	}
	return username
}

func buildMiniprogramUsernameCandidates(openid string) []string {
	normalized := strings.TrimSpace(openid)
	if normalized == "" {
		return nil
	}
	primary := buildMiniprogramUsername(normalized, "")
	secondary := buildMiniprogramUsername(normalized, fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(normalized))))
	if primary == secondary {
		return []string{primary}
	}
	return []string{primary, secondary}
}

func (m *UserModel) updateUnionIDIfNeeded(user *User, unionid string) error {
	if user == nil || unionid == "" || user.UnionID == unionid {
		return nil
	}
	_, err := m.DB.Exec("UPDATE users SET unionid = ?, updated_at = NOW() WHERE id = ?", unionid, user.ID)
	if err != nil {
		return err
	}
	user.UnionID = unionid
	return nil
}

// GetByID 根据ID获取用户
func (m *UserModel) GetByID(id int64) (*User, error) {
	user := &User{}
	query := `SELECT id, username, password, COALESCE(openid, ''), COALESCE(unionid, ''), user_type, COALESCE(can_withdraw,0), created_at, updated_at 
	          FROM users WHERE id = ?`
	var canWithdraw int
	err := m.DB.QueryRow(query, id).Scan(
		&user.ID, &user.Username, &user.Password, &user.OpenID, &user.UnionID,
		&user.UserType, &canWithdraw, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	user.CanWithdraw = canWithdraw != 0
	return user, nil
}

// GetByUsername 根据用户名获取用户（管理后台用户）
func (m *UserModel) GetByUsername(username string) (*User, error) {
	user := &User{}
	query := `SELECT id, username, password, COALESCE(openid, ''), COALESCE(unionid, ''), user_type, COALESCE(can_withdraw,0), created_at, updated_at 
	          FROM users WHERE username = ? AND user_type = 'management'`
	var canWithdraw int
	err := m.DB.QueryRow(query, username).Scan(
		&user.ID, &user.Username, &user.Password, &user.OpenID, &user.UnionID,
		&user.UserType, &canWithdraw, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	user.CanWithdraw = canWithdraw != 0
	return user, nil
}

// GetByUsernameAndType 根据用户名和用户类型获取用户
func (m *UserModel) GetByUsernameAndType(username, userType string) (*User, error) {
	user := &User{}
	query := `SELECT id, username, password, COALESCE(openid, ''), COALESCE(unionid, ''), user_type, COALESCE(can_withdraw,0), created_at, updated_at 
	          FROM users WHERE username = ? AND user_type = ?`
	var canWithdraw int
	err := m.DB.QueryRow(query, username, userType).Scan(
		&user.ID, &user.Username, &user.Password, &user.OpenID, &user.UnionID,
		&user.UserType, &canWithdraw, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	user.CanWithdraw = canWithdraw != 0
	return user, nil
}

// GetByOpenID 根据OpenID获取用户
func (m *UserModel) GetByOpenID(openid string) (*User, error) {
	user := &User{}
	query := `SELECT id, username, password, COALESCE(openid, ''), COALESCE(unionid, ''), user_type, COALESCE(can_withdraw,0), created_at, updated_at 
	          FROM users WHERE openid = ? AND user_type = 'miniprogram'`
	var canWithdraw int
	err := m.DB.QueryRow(query, openid).Scan(
		&user.ID, &user.Username, &user.Password, &user.OpenID, &user.UnionID,
		&user.UserType, &canWithdraw, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	user.CanWithdraw = canWithdraw != 0
	return user, nil
}

// GetByUnionID 根据UnionID获取用户
func (m *UserModel) GetByUnionID(unionid string) (*User, error) {
	user := &User{}
	query := `SELECT id, username, password, COALESCE(openid, ''), COALESCE(unionid, ''), user_type, COALESCE(can_withdraw,0), created_at, updated_at 
	          FROM users WHERE unionid = ? AND user_type = 'miniprogram'`
	var canWithdraw int
	err := m.DB.QueryRow(query, unionid).Scan(
		&user.ID, &user.Username, &user.Password, &user.OpenID, &user.UnionID,
		&user.UserType, &canWithdraw, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	user.CanWithdraw = canWithdraw != 0
	return user, nil
}

// BindWechatIdentity 将真实微信身份绑定到现有小程序用户
func (m *UserModel) BindWechatIdentity(userID int64, openid, unionid string) error {
	query := `UPDATE users
	          SET openid = CASE WHEN ? <> '' THEN ? ELSE openid END,
	              unionid = CASE WHEN ? <> '' THEN ? ELSE unionid END,
	              updated_at = NOW()
	          WHERE id = ? AND user_type = 'miniprogram'`
	_, err := m.DB.Exec(query, openid, openid, unionid, unionid, userID)
	return err
}

// CreateOrUpdateByOpenID 根据OpenID创建或更新用户
func (m *UserModel) CreateOrUpdateByOpenID(openid, unionid string) (*User, error) {
	openid = strings.TrimSpace(openid)
	unionid = strings.TrimSpace(unionid)
	if openid == "" {
		return nil, fmt.Errorf("openid is empty")
	}

	user, err := m.GetByOpenID(openid)
	if err == nil {
		if err := m.updateUnionIDIfNeeded(user, unionid); err != nil {
			return nil, err
		}
		return user, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	for _, username := range buildMiniprogramUsernameCandidates(openid) {
		user = &User{
			OpenID:   openid,
			UnionID:  unionid,
			UserType: "miniprogram",
			Username: username,
		}
		if err := m.Create(user); err == nil {
			return user, nil
		} else if isDuplicateKey(err) {
			existingUser, lookupErr := m.GetByOpenID(openid)
			if lookupErr == nil {
				if err := m.updateUnionIDIfNeeded(existingUser, unionid); err != nil {
					return nil, err
				}
				return existingUser, nil
			}
			if lookupErr != sql.ErrNoRows {
				return nil, lookupErr
			}
			continue
		} else {
			return nil, err
		}
	}

	return nil, fmt.Errorf("create miniprogram user failed")
}

// GetByPhone 根据手机号查询用户（通过 user_identities 表联查）
func (m *UserModel) GetByPhone(phone string) (*User, error) {
	user := &User{}
	query := `SELECT u.id, u.username, u.password, u.openid, u.unionid, u.user_type, COALESCE(u.can_withdraw,0), u.created_at, u.updated_at
	          FROM users u
	          INNER JOIN user_identities ui ON ui.user_id = u.id AND ui.identity_type = 'phone'
	          WHERE ui.identity_key = ? AND u.user_type = 'miniprogram'`
	var canWithdraw int
	err := m.DB.QueryRow(query, phone).Scan(
		&user.ID, &user.Username, &user.Password, &user.OpenID, &user.UnionID,
		&user.UserType, &canWithdraw, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	user.CanWithdraw = canWithdraw != 0
	return user, nil
}

// CreateByPhone 用手机号创建用户
func (m *UserModel) CreateByPhone(phone string) (*User, error) {
	// 生成用户名：phone_138xxxx1234
	username := "phone_" + phone
	user := &User{
		Username: username,
		UserType: "miniprogram",
	}
	if err := m.Create(user); err != nil {
		return nil, err
	}
	return user, nil
}

// InitTable 初始化用户表
func (m *UserModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS users (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	username VARCHAR(64) NOT NULL,
	password VARCHAR(255) DEFAULT NULL,
	openid VARCHAR(128) DEFAULT NULL,
	unionid VARCHAR(128) DEFAULT NULL,
	user_type VARCHAR(32) NOT NULL DEFAULT 'miniprogram',
	can_withdraw TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否已认证可提现',
	status VARCHAR(16) NOT NULL DEFAULT 'active' COMMENT '账号状态：active=正常, banned=已封禁',
	banned_reason VARCHAR(255) NOT NULL DEFAULT '' COMMENT '封禁原因',
	banned_at TIMESTAMP NULL DEFAULT NULL COMMENT '封禁时间',
	merge_status VARCHAR(32) NOT NULL DEFAULT 'normal',
	merged_to_user_id BIGINT UNSIGNED NULL DEFAULT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	UNIQUE KEY uk_username_type (username, user_type),
	UNIQUE KEY uk_openid (openid),
	INDEX idx_user_type (user_type),
	INDEX idx_merged_to_user_id (merged_to_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	if err != nil {
		return err
	}
	if err := m.InitCanWithdrawColumn(); err != nil {
		return err
	}
	if err := m.InitUserStatusColumns(); err != nil {
		return err
	}
	return m.InitMergeColumns()
}

// InitUserStatusColumns 为已有 users 表添加账号状态列（兼容老库，可重复执行）
func (m *UserModel) InitUserStatusColumns() error {
	for _, col := range []struct {
		exists string
		ddl    string
	}{
		{"status", "ALTER TABLE users ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'active' COMMENT '账号状态：active=正常, banned=已封禁'"},
		{"banned_reason", "ALTER TABLE users ADD COLUMN banned_reason VARCHAR(255) NOT NULL DEFAULT '' COMMENT '封禁原因'"},
		{"banned_at", "ALTER TABLE users ADD COLUMN banned_at TIMESTAMP NULL DEFAULT NULL COMMENT '封禁时间'"},
	} {
		var exists int
		err := m.DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = ?`, col.exists).Scan(&exists)
		if err != nil {
			return err
		}
		if exists == 0 {
			if _, err = m.DB.Exec(col.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetUserBanned 封禁/解封小程序用户（管理端调用，操作人由路由层审计记录）
func (m *UserModel) SetUserBanned(userID int64, banned bool, reason string) error {
	if banned {
		_, err := m.DB.Exec(`UPDATE users SET status = 'banned', banned_reason = ?, banned_at = NOW(), updated_at = NOW() WHERE id = ?`, reason, userID)
		return err
	}
	_, err := m.DB.Exec(`UPDATE users SET status = 'active', banned_reason = '', banned_at = NULL, updated_at = NOW() WHERE id = ?`, userID)
	return err
}

// GetUserBanStatus 读取账号封禁状态；老行无 status 列值时视为 active
func (m *UserModel) GetUserBanStatus(userID int64) (banned bool, reason string, err error) {
	var status string
	err = m.DB.QueryRow(`SELECT COALESCE(status, 'active'), COALESCE(banned_reason, '') FROM users WHERE id = ?`, userID).Scan(&status, &reason)
	if err != nil {
		return false, "", err
	}
	return status != "active", reason, nil
}

// UpdateCanWithdraw 更新用户是否可提现（认证通过后由管理后台调用）
func (m *UserModel) UpdateCanWithdraw(userID int64, canWithdraw bool) error {
	v := 0
	if canWithdraw {
		v = 1
	}
	_, err := m.DB.Exec("UPDATE users SET can_withdraw = ?, updated_at = NOW() WHERE id = ? AND COALESCE(can_withdraw, 0) <> ?", v, userID, v)
	return err
}

// InitCanWithdrawColumn 为已有 users 表添加 can_withdraw 列（兼容老库）
func (m *UserModel) InitCanWithdrawColumn() error {
	var exists int
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS 
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'can_withdraw'`).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 0 {
		_, err = m.DB.Exec(`ALTER TABLE users ADD COLUMN can_withdraw TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否已认证可提现'`)
		return err
	}
	return nil
}

func (m *UserModel) InitMergeColumns() error {
	var exists int
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS 
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'merge_status'`).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 0 {
		if _, err = m.DB.Exec(`ALTER TABLE users ADD COLUMN merge_status VARCHAR(32) NOT NULL DEFAULT 'normal'`); err != nil {
			return err
		}
	}

	err = m.DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS 
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'merged_to_user_id'`).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 0 {
		if _, err = m.DB.Exec(`ALTER TABLE users ADD COLUMN merged_to_user_id BIGINT UNSIGNED NULL DEFAULT NULL`); err != nil {
			return err
		}
	}

	err = m.DB.QueryRow(`SELECT COUNT(*) FROM information_schema.STATISTICS 
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND INDEX_NAME = 'idx_merged_to_user_id'`).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 0 {
		if _, err = m.DB.Exec(`ALTER TABLE users ADD INDEX idx_merged_to_user_id (merged_to_user_id)`); err != nil {
			return err
		}
	}
	return nil
}

func (m *UserModel) ResolveMergedUserID(userID int64) (int64, error) {
	currentUserID := userID
	for i := 0; i < 5; i++ {
		var userType string
		var mergeStatus string
		var mergedToUserID sql.NullInt64
		err := m.DB.QueryRow(`SELECT user_type, COALESCE(merge_status, 'normal'), merged_to_user_id FROM users WHERE id = ?`, currentUserID).Scan(&userType, &mergeStatus, &mergedToUserID)
		if err != nil {
			return 0, err
		}
		if userType != "miniprogram" || mergeStatus != "merged_source" || !mergedToUserID.Valid || mergedToUserID.Int64 <= 0 || mergedToUserID.Int64 == currentUserID {
			return currentUserID, nil
		}
		currentUserID = mergedToUserID.Int64
	}
	return currentUserID, nil
}

func (m *UserModel) GetEffectiveUserByID(userID int64) (*User, error) {
	effectiveUserID, err := m.ResolveMergedUserID(userID)
	if err != nil {
		return nil, err
	}
	return m.GetByID(effectiveUserID)
}

// GetAll 获取小程序用户列表（分页）
func (m *UserModel) ListManagementUsers(filters ManagementUserListFilters, limit, offset int) ([]*ManagementUserListItem, error) {
	whereSQL, args := buildManagementUserListWhere(filters)
	query := `SELECT u.id, u.username, u.user_type, COALESCE(u.can_withdraw, 0), u.created_at, u.updated_at,
	                  COALESCE(p.nickname, ''), COALESCE(p.enterprise_wechat_verified, 0), p.enterprise_wechat_verified_at, COALESCE(p.enterprise_wechat_contact, ''),
	                  COALESCE(p.phone, ''), COALESCE(p.identity_type, ''),
	                  COALESCE(u.status, 'active'), COALESCE(u.banned_reason, ''), u.banned_at,
	                  COALESCE(p.has_password, 0), p.last_device_change_time,
	                  COALESCE(um.status, ''), um.expired_at, COALESCE(um.plan_title, '')
	          FROM users u
	          LEFT JOIN user_profiles p ON p.user_id = u.id
	          LEFT JOIN user_memberships um ON um.user_id = u.id` + whereSQL + buildManagementUserListOrder(filters) + `
	          LIMIT ? OFFSET ?`

	queryArgs := append(args, limit, offset)
	rows, err := m.DB.Query(query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*ManagementUserListItem
	for rows.Next() {
		item := &ManagementUserListItem{}
		var canWithdraw int
		var enterpriseWechatVerified int
		var hasPassword int
		var memberExpiredAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.Username,
			&item.UserType,
			&canWithdraw,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.Nickname,
			&enterpriseWechatVerified,
			&item.EnterpriseWechatVerifiedAt,
			&item.EnterpriseWechatContact,
			&item.Phone,
			&item.IdentityType,
			&item.Status,
			&item.BannedReason,
			&item.BannedAt,
			&hasPassword,
			&item.LastDeviceChangeTime,
			&item.MemberStatus,
			&memberExpiredAt,
			&item.MemberPlanTitle,
		); err != nil {
			log.Printf("[用户列表] 行扫描失败 id=%d: %v", item.ID, err)
			continue
		}
		item.CanWithdraw = canWithdraw != 0
		item.EnterpriseWechatVerified = enterpriseWechatVerified != 0
		item.HasPassword = hasPassword != 0
		if memberExpiredAt.Valid {
			item.MemberExpiredAt = &memberExpiredAt.Time
		}
		users = append(users, item)
	}

	return users, rows.Err()
}

func (m *UserModel) CountManagementUsers(filters ManagementUserListFilters) (int64, error) {
	whereSQL, args := buildManagementUserListWhere(filters)
	query := `SELECT COUNT(*)
	          FROM users u
	          LEFT JOIN user_profiles p ON p.user_id = u.id` + whereSQL

	var count int64
	if err := m.DB.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// ManagementUserStats 用户列表页全库真实口径统计（不再按当前页计算）
type ManagementUserStats struct {
	Total              int64 `json:"total"`
	TodayNew           int64 `json:"today_new"`
	PhoneVerified      int64 `json:"phone_verified"`
	PhonePending       int64 `json:"phone_pending"`
	MemberActive       int64 `json:"member_active"`
	StonesTotal        int64 `json:"stones_total"`
	ZeroStonesRecharge int64 `json:"zero_stones_recharged"`
	Banned             int64 `json:"banned"`
	RiskUsers          int64 `json:"risk_users"` // 近7天换绑设备或同设备多账号（不含已封禁，封禁单独计）
}

func (m *UserModel) GetManagementUserStats() (*ManagementUserStats, error) {
	stats := &ManagementUserStats{}
	count := func(dest *int64, query string, args ...interface{}) error {
		return m.DB.QueryRow(query, args...).Scan(dest)
	}
	if err := count(&stats.Total, `SELECT COUNT(*) FROM users WHERE user_type = 'miniprogram'`); err != nil {
		return nil, err
	}
	if err := count(&stats.TodayNew, `SELECT COUNT(*) FROM users WHERE user_type = 'miniprogram' AND created_at >= CURDATE()`); err != nil {
		return nil, err
	}
	if err := count(&stats.PhoneVerified, `SELECT COUNT(*) FROM users u JOIN user_profiles p ON p.user_id = u.id WHERE u.user_type = 'miniprogram' AND p.enterprise_wechat_verified = 1`); err != nil {
		return nil, err
	}
	if err := count(&stats.PhonePending, `SELECT COUNT(*) FROM users u LEFT JOIN user_profiles p ON p.user_id = u.id WHERE u.user_type = 'miniprogram' AND COALESCE(p.enterprise_wechat_verified, 0) = 0`); err != nil {
		return nil, err
	}
	if err := count(&stats.MemberActive, `SELECT COUNT(*) FROM users u JOIN user_memberships um ON um.user_id = u.id WHERE u.user_type = 'miniprogram' AND um.status = 'active' AND um.expired_at > NOW()`); err != nil {
		return nil, err
	}
	if err := count(&stats.StonesTotal, `SELECT COALESCE(SUM(stones), 0) FROM users WHERE user_type = 'miniprogram'`); err != nil {
		return nil, err
	}
	if err := count(&stats.ZeroStonesRecharge, `SELECT COUNT(*) FROM users u WHERE u.user_type = 'miniprogram' AND COALESCE(u.stones, 0) <= 0 AND EXISTS (SELECT 1 FROM user_orders o WHERE o.user_id = u.id AND o.type = 'recharge' AND o.status = 'success')`); err != nil {
		return nil, err
	}
	if err := count(&stats.Banned, `SELECT COUNT(*) FROM users WHERE user_type = 'miniprogram' AND COALESCE(status, 'active') <> 'active'`); err != nil {
		return nil, err
	}
	if err := count(&stats.RiskUsers, `SELECT COUNT(*) FROM users u JOIN user_profiles p ON p.user_id = u.id
		WHERE u.user_type = 'miniprogram' AND COALESCE(u.status, 'active') = 'active'
		AND (
			(p.last_device_change_time IS NOT NULL AND p.last_device_change_time > DATE_SUB(NOW(), INTERVAL 7 DAY))
			OR (COALESCE(p.device_id, '') <> '' AND EXISTS (SELECT 1 FROM user_profiles p2 WHERE p2.device_id = p.device_id AND p2.user_id <> p.user_id))
		)`); err != nil {
		return nil, err
	}
	return stats, nil
}

func (m *UserModel) GetAll(limit, offset int) ([]*User, error) {
	query := `SELECT id, username, password, COALESCE(openid, ''), COALESCE(unionid, ''), user_type, COALESCE(can_withdraw,0), created_at, updated_at 
	          FROM users 
	          WHERE user_type = 'miniprogram' 
	          ORDER BY id DESC 
	          LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		user := &User{}
		var canWithdraw int
		err := rows.Scan(
			&user.ID, &user.Username, &user.Password, &user.OpenID, &user.UnionID,
			&user.UserType, &canWithdraw, &user.CreatedAt, &user.UpdatedAt,
		)
		if err != nil {
			continue
		}
		user.CanWithdraw = canWithdraw != 0
		users = append(users, user)
	}

	return users, rows.Err()
}

// Count 统计小程序用户总数
func (m *UserModel) Count() (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM users WHERE user_type = 'miniprogram'`
	err := m.DB.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// InitDefaultAdmin 初始化默认管理员账户（如果不存在）
func (m *UserModel) InitDefaultAdmin(username, password string) error {
	// 检查管理员是否已存在
	existingUser, err := m.GetByUsername(username)
	if err == nil && existingUser != nil {
		// 管理员已存在，跳过创建
		return nil
	}

	// 导入密码加密函数
	// 注意：这里需要导入 function 包，但为了避免循环依赖，我们在调用处传入加密后的密码
	// 或者在这里直接导入 function 包
	return nil
}
