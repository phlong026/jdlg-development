package model

import (
  "database/sql"
  "time"
)

type EnterpriseWechatBindTicket struct {
  ID             int64      `json:"id" db:"id"`
  UserID         int64      `json:"user_id" db:"user_id"`
  Ticket         string     `json:"ticket" db:"ticket"`
  Scene          string     `json:"scene" db:"scene"`
  Status         string     `json:"status" db:"status"`
  TaskNo         string     `json:"task_no" db:"task_no"`
  ImageIndex     int        `json:"image_index" db:"image_index"`
  ExternalUserID string     `json:"external_user_id" db:"external_user_id"`
  Contact        string     `json:"contact" db:"contact"`
  VerifiedAt     *time.Time `json:"verified_at" db:"verified_at"`
  ExpiredAt      *time.Time `json:"expired_at" db:"expired_at"`
  CreatedAt      time.Time  `json:"created_at" db:"created_at"`
  UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

type EnterpriseWechatBindTicketModel struct {
  DB *sql.DB
}

func NewEnterpriseWechatBindTicketModel(db *sql.DB) *EnterpriseWechatBindTicketModel {
  return &EnterpriseWechatBindTicketModel{DB: db}
}

func (m *EnterpriseWechatBindTicketModel) InitTable() error {
  schema := `
CREATE TABLE IF NOT EXISTS enterprise_wechat_bind_tickets (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  ticket VARCHAR(64) NOT NULL,
  scene VARCHAR(64) NOT NULL DEFAULT 'ai_download',
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  task_no VARCHAR(64) NOT NULL DEFAULT '',
  image_index INT NOT NULL DEFAULT 0,
  external_user_id VARCHAR(128) NOT NULL DEFAULT '',
  contact VARCHAR(128) NOT NULL DEFAULT '',
  verified_at DATETIME NULL DEFAULT NULL,
  expired_at DATETIME NULL DEFAULT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_ticket (ticket),
  KEY idx_user_scene_status (user_id, scene, status),
  KEY idx_expired_at (expired_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='企微扫码绑定票据';
`
  _, err := m.DB.Exec(schema)
  return err
}

func (m *EnterpriseWechatBindTicketModel) Create(ticket *EnterpriseWechatBindTicket) error {
  query := `INSERT INTO enterprise_wechat_bind_tickets (user_id, ticket, scene, status, task_no, image_index, external_user_id, contact, verified_at, expired_at, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`
  result, err := m.DB.Exec(
    query,
    ticket.UserID,
    ticket.Ticket,
    ticket.Scene,
    ticket.Status,
    ticket.TaskNo,
    ticket.ImageIndex,
    ticket.ExternalUserID,
    ticket.Contact,
    nullableTime(ticket.VerifiedAt),
    nullableTime(ticket.ExpiredAt),
  )
  if err != nil {
    return err
  }
  id, err := result.LastInsertId()
  if err != nil {
    return err
  }
  ticket.ID = id
  return nil
}

func (m *EnterpriseWechatBindTicketModel) GetActiveByUserID(userID int64, scene string) (*EnterpriseWechatBindTicket, error) {
  query := `SELECT id, user_id, ticket, scene, status, task_no, image_index, external_user_id, contact, verified_at, expired_at, created_at, updated_at
            FROM enterprise_wechat_bind_tickets
            WHERE user_id = ? AND scene = ? AND status = 'pending' AND (expired_at IS NULL OR expired_at > NOW())
            ORDER BY id DESC LIMIT 1`
  item := &EnterpriseWechatBindTicket{}
  var verifiedAt sql.NullTime
  var expiredAt sql.NullTime
  err := m.DB.QueryRow(query, userID, scene).Scan(
    &item.ID,
    &item.UserID,
    &item.Ticket,
    &item.Scene,
    &item.Status,
    &item.TaskNo,
    &item.ImageIndex,
    &item.ExternalUserID,
    &item.Contact,
    &verifiedAt,
    &expiredAt,
    &item.CreatedAt,
    &item.UpdatedAt,
  )
  if err != nil {
    return nil, err
  }
  if verifiedAt.Valid {
    item.VerifiedAt = &verifiedAt.Time
  }
  if expiredAt.Valid {
    item.ExpiredAt = &expiredAt.Time
  }
  return item, nil
}

func (m *EnterpriseWechatBindTicketModel) GetByTicket(ticket string) (*EnterpriseWechatBindTicket, error) {
  query := `SELECT id, user_id, ticket, scene, status, task_no, image_index, external_user_id, contact, verified_at, expired_at, created_at, updated_at
            FROM enterprise_wechat_bind_tickets WHERE ticket = ? LIMIT 1`
  item := &EnterpriseWechatBindTicket{}
  var verifiedAt sql.NullTime
  var expiredAt sql.NullTime
  err := m.DB.QueryRow(query, ticket).Scan(
    &item.ID,
    &item.UserID,
    &item.Ticket,
    &item.Scene,
    &item.Status,
    &item.TaskNo,
    &item.ImageIndex,
    &item.ExternalUserID,
    &item.Contact,
    &verifiedAt,
    &expiredAt,
    &item.CreatedAt,
    &item.UpdatedAt,
  )
  if err != nil {
    return nil, err
  }
  if verifiedAt.Valid {
    item.VerifiedAt = &verifiedAt.Time
  }
  if expiredAt.Valid {
    item.ExpiredAt = &expiredAt.Time
  }
  return item, nil
}

func (m *EnterpriseWechatBindTicketModel) MarkVerified(ticket, contact, externalUserID string) error {
  query := `UPDATE enterprise_wechat_bind_tickets
            SET status = 'verified', contact = ?, external_user_id = ?, verified_at = NOW(), updated_at = NOW()
            WHERE ticket = ?`
  _, err := m.DB.Exec(query, contact, externalUserID, ticket)
  return err
}

func nullableTime(v *time.Time) interface{} {
  if v == nil || v.IsZero() {
    return nil
  }
  return *v
}
