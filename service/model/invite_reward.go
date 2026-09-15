package model

import (
	"database/sql"
	"time"
)

// InviteReward 邀请奖励记录
// reward_type: register-邀请注册, recharge-好友首次充值10%, ai-好友使用AI每次5灵石
type InviteReward struct {
	ID         int64     `json:"id" db:"id"`
	InviterID  int64     `json:"inviter_id" db:"inviter_id"`
	InviteeID  int64     `json:"invitee_id" db:"invitee_id"`
	RewardType string    `json:"reward_type" db:"reward_type"` // register, recharge, ai
	Amount     int64     `json:"amount" db:"amount"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// InviteRewardModel 邀请奖励数据访问层
type InviteRewardModel struct {
	DB *sql.DB
}

// NewInviteRewardModel 创建模型
func NewInviteRewardModel(db *sql.DB) *InviteRewardModel {
	return &InviteRewardModel{DB: db}
}

// InitTable 初始化表
func (m *InviteRewardModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS invite_rewards (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  inviter_id BIGINT UNSIGNED NOT NULL COMMENT '邀请人用户ID',
  invitee_id BIGINT UNSIGNED NOT NULL COMMENT '被邀请人用户ID',
  reward_type VARCHAR(32) NOT NULL COMMENT 'register/recharge/ai',
  amount BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '奖励灵石数',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_inviter_created (inviter_id, created_at),
  KEY idx_invitee (invitee_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='邀请奖励记录';
`
	_, err := m.DB.Exec(schema)
	return err
}

// Create 插入一条邀请奖励
func (m *InviteRewardModel) Create(inviterID, inviteeID int64, rewardType string, amount int64) error {
	query := `INSERT INTO invite_rewards (inviter_id, invitee_id, reward_type, amount, created_at)
	          VALUES (?, ?, ?, ?, NOW())`
	_, err := m.DB.Exec(query, inviterID, inviteeID, rewardType, amount)
	return err
}

// CountByInviter 统计邀请人数（被邀请人去重）
func (m *InviteRewardModel) CountByInviter(inviterID int64) (int64, error) {
	var count int64
	err := m.DB.QueryRow(`
		SELECT COUNT(DISTINCT invitee_id) FROM invite_rewards WHERE inviter_id = ?
	`, inviterID).Scan(&count)
	return count, err
}

// SummaryByInviter 统计邀请人总奖励、本月奖励
func (m *InviteRewardModel) SummaryByInviter(inviterID int64) (totalReward, monthReward int64, err error) {
	err = m.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM invite_rewards WHERE inviter_id = ?
	`, inviterID).Scan(&totalReward)
	if err != nil {
		return 0, 0, err
	}
	err = m.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0) FROM invite_rewards
		WHERE inviter_id = ? AND created_at >= DATE_FORMAT(NOW(), '%Y-%m-01 00:00:00')
	`, inviterID).Scan(&monthReward)
	return totalReward, monthReward, err
}

// ListByInviter 分页查询邀请记录（含被邀请人信息需 join users）
func (m *InviteRewardModel) ListByInviter(inviterID int64, limit, offset int) ([]*InviteReward, int64, error) {
	var total int64
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM invite_rewards WHERE inviter_id = ?`, inviterID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	query := `SELECT id, inviter_id, invitee_id, reward_type, amount, created_at
	          FROM invite_rewards WHERE inviter_id = ?
	          ORDER BY created_at DESC LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, inviterID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*InviteReward
	for rows.Next() {
		r := &InviteReward{}
		if err := rows.Scan(&r.ID, &r.InviterID, &r.InviteeID, &r.RewardType, &r.Amount, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, r)
	}
	return list, total, nil
}
