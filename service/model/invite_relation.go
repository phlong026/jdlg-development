package model

import (
	"database/sql"
	"strconv"
	"time"
)

// InviteRelation 邀请关系
type InviteRelation struct {
	ID                int64     `json:"id" db:"id"`
	InviterUserID     int64     `json:"inviter_user_id" db:"inviter_user_id"`
	InviteeUserID     int64     `json:"invitee_user_id" db:"invitee_user_id"`
	InvitedAt         time.Time `json:"invited_at" db:"invited_at"`
	FirstRechargeDone bool      `json:"first_recharge_done" db:"first_recharge_done"`
}

// InviteRelationModel 邀请关系数据访问层
type InviteRelationModel struct {
	DB *sql.DB
}

// NewInviteRelationModel 创建模型
func NewInviteRelationModel(db *sql.DB) *InviteRelationModel {
	return &InviteRelationModel{DB: db}
}

// InitTable 初始化表
func (m *InviteRelationModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS invite_relations (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  inviter_user_id BIGINT UNSIGNED NOT NULL COMMENT '邀请人用户ID',
  invitee_user_id BIGINT UNSIGNED NOT NULL COMMENT '被邀请人用户ID',
  invited_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  first_recharge_done TINYINT(1) NOT NULL DEFAULT 0 COMMENT '被邀请人是否已完成首次充值',
  UNIQUE KEY uk_invitee (invitee_user_id),
  KEY idx_inviter (inviter_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='邀请关系';
`
	_, err := m.DB.Exec(schema)
	return err
}

// Create 创建邀请关系（被邀请人只能被邀请一次）
func (m *InviteRelationModel) Create(inviterUserID, inviteeUserID int64) error {
	query := `INSERT INTO invite_relations (inviter_user_id, invitee_user_id, invited_at, first_recharge_done)
	          VALUES (?, ?, NOW(), 0)`
	_, err := m.DB.Exec(query, inviterUserID, inviteeUserID)
	return err
}

// GetInviterID 获取邀请人ID（被邀请人 -> 邀请人）
func (m *InviteRelationModel) GetInviterID(inviteeUserID int64) (int64, error) {
	var inviterID int64
	query := `SELECT inviter_user_id FROM invite_relations WHERE invitee_user_id = ? LIMIT 1`
	err := m.DB.QueryRow(query, inviteeUserID).Scan(&inviterID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return inviterID, nil
}

// HasInviter 被邀请人是否已有邀请人
func (m *InviteRelationModel) HasInviter(inviteeUserID int64) (bool, error) {
	inviterID, err := m.GetInviterID(inviteeUserID)
	return inviterID > 0, err
}

// IsFirstRechargeDone 被邀请人是否已首次充值
func (m *InviteRelationModel) IsFirstRechargeDone(inviteeUserID int64) (bool, error) {
	var done int
	query := `SELECT first_recharge_done FROM invite_relations WHERE invitee_user_id = ? LIMIT 1`
	err := m.DB.QueryRow(query, inviteeUserID).Scan(&done)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return done == 1, nil
}

// MarkFirstRechargeDone 标记被邀请人已完成首次充值
func (m *InviteRelationModel) MarkFirstRechargeDone(inviteeUserID int64) error {
	query := `UPDATE invite_relations SET first_recharge_done = 1 WHERE invitee_user_id = ?`
	_, err := m.DB.Exec(query, inviteeUserID)
	return err
}

// CountByInviter 统计邀请人邀请的好友数量
func (m *InviteRelationModel) CountByInviter(inviterUserID int64) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM invite_relations WHERE inviter_user_id = ?`
	err := m.DB.QueryRow(query, inviterUserID).Scan(&count)
	return count, err
}

// ListByInviter 分页查询邀请人邀请的好友列表
func (m *InviteRelationModel) ListByInviter(inviterUserID int64, limit, offset int) ([]*InviteRelation, int64, error) {
	var total int64
	query := `SELECT COUNT(*) FROM invite_relations WHERE inviter_user_id = ?`
	if err := m.DB.QueryRow(query, inviterUserID).Scan(&total); err != nil {
		return nil, 0, err
	}
	query = `SELECT id, inviter_user_id, invitee_user_id, invited_at, first_recharge_done
	         FROM invite_relations WHERE inviter_user_id = ?
	         ORDER BY invited_at DESC LIMIT ? OFFSET ?`
	rows, err := m.DB.Query(query, inviterUserID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*InviteRelation
	for rows.Next() {
		r := &InviteRelation{}
		var done int
		if err := rows.Scan(&r.ID, &r.InviterUserID, &r.InviteeUserID, &r.InvitedAt, &done); err != nil {
			return nil, 0, err
		}
		r.FirstRechargeDone = done == 1
		list = append(list, r)
	}
	return list, total, nil
}

// ParseInviteCodeToUserID 邀请码解析为用户ID（邀请码即邀请人的 user_id 字符串）
func ParseInviteCodeToUserID(inviteCode string) (int64, bool) {
	if inviteCode == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(inviteCode, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
