package model

import (
	"database/sql"
	"strings"
	"time"
)

const (
	FavoriteTargetTemplate    = "template"
	FavoriteTargetAITool      = "ai_tool"
	FavoriteTargetDesigner    = "designer"
	FavoriteTargetInspiration = "inspiration"
)

type UserFavorite struct {
	ID         int64     `json:"id" db:"id"`
	UserID     int64     `json:"user_id" db:"user_id"`
	TargetType string    `json:"target_type" db:"target_type"`
	TargetID   int64     `json:"target_id" db:"target_id"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

type UserFavoriteModel struct {
	DB *sql.DB
}

func NewUserFavoriteModel(db *sql.DB) *UserFavoriteModel {
	return &UserFavoriteModel{DB: db}
}

func NormalizeFavoriteTargetType(raw string) string {
	switch strings.TrimSpace(raw) {
	case FavoriteTargetTemplate:
		return FavoriteTargetTemplate
	case FavoriteTargetAITool:
		return FavoriteTargetAITool
	case FavoriteTargetDesigner:
		return FavoriteTargetDesigner
	case FavoriteTargetInspiration:
		return FavoriteTargetInspiration
	default:
		return ""
	}
}

func (m *UserFavoriteModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS user_favorites (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  target_type VARCHAR(32) NOT NULL,
  target_id BIGINT UNSIGNED NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_user_target (user_id, target_type, target_id),
  KEY idx_user_type_created (user_id, target_type, created_at),
  KEY idx_target (target_type, target_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户通用收藏记录';
`
	_, err := m.DB.Exec(schema)
	return err
}

func (m *UserFavoriteModel) Add(userID int64, targetType string, targetID int64) error {
	targetType = NormalizeFavoriteTargetType(targetType)
	if userID <= 0 || targetType == "" || targetID <= 0 {
		return nil
	}
	_, err := m.DB.Exec(
		`INSERT IGNORE INTO user_favorites (user_id, target_type, target_id, created_at) VALUES (?, ?, ?, NOW())`,
		userID,
		targetType,
		targetID,
	)
	return err
}

func (m *UserFavoriteModel) Remove(userID int64, targetType string, targetID int64) error {
	targetType = NormalizeFavoriteTargetType(targetType)
	if userID <= 0 || targetType == "" || targetID <= 0 {
		return nil
	}
	_, err := m.DB.Exec(
		`DELETE FROM user_favorites WHERE user_id = ? AND target_type = ? AND target_id = ?`,
		userID,
		targetType,
		targetID,
	)
	return err
}

func (m *UserFavoriteModel) Exists(userID int64, targetType string, targetID int64) (bool, error) {
	targetType = NormalizeFavoriteTargetType(targetType)
	if userID <= 0 || targetType == "" || targetID <= 0 {
		return false, nil
	}
	var count int
	err := m.DB.QueryRow(
		`SELECT COUNT(1) FROM user_favorites WHERE user_id = ? AND target_type = ? AND target_id = ?`,
		userID,
		targetType,
		targetID,
	).Scan(&count)
	return count > 0, err
}

func (m *UserFavoriteModel) List(userID int64, targetType string, limit int, offset int) ([]*UserFavorite, int64, error) {
	if userID <= 0 {
		return nil, 0, nil
	}
	targetType = NormalizeFavoriteTargetType(targetType)
	where := `WHERE user_id = ?`
	args := []interface{}{userID}
	if targetType != "" {
		where += ` AND target_type = ?`
		args = append(args, targetType)
	}

	var total int64
	if err := m.DB.QueryRow(`SELECT COUNT(1) FROM user_favorites `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	queryArgs := append([]interface{}{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := m.DB.Query(
		`SELECT id, user_id, target_type, target_id, created_at FROM user_favorites `+where+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]*UserFavorite, 0)
	for rows.Next() {
		item := &UserFavorite{}
		if err := rows.Scan(&item.ID, &item.UserID, &item.TargetType, &item.TargetID, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, item)
	}
	return list, total, rows.Err()
}
