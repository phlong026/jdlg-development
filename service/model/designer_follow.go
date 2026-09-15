package model

import "database/sql"

type DesignerFollow struct {
	ID             int64 `json:"id" db:"id"`
	FollowerUserID int64 `json:"follower_user_id" db:"follower_user_id"`
	DesignerUserID int64 `json:"designer_user_id" db:"designer_user_id"`
}

type DesignerFollowModel struct {
	DB *sql.DB
}

func NewDesignerFollowModel(db *sql.DB) *DesignerFollowModel {
	return &DesignerFollowModel{DB: db}
}

func (m *DesignerFollowModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS designer_follows (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  follower_user_id BIGINT UNSIGNED NOT NULL COMMENT '关注者用户ID',
  designer_user_id BIGINT UNSIGNED NOT NULL COMMENT '被关注设计师用户ID',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_follower_designer (follower_user_id, designer_user_id),
  KEY idx_designer_user (designer_user_id),
  KEY idx_follower_user (follower_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='设计师关注关系';
`
	_, err := m.DB.Exec(schema)
	return err
}

func (m *DesignerFollowModel) Follow(followerUserID, designerUserID int64) error {
	_, err := m.DB.Exec(`
		INSERT INTO designer_follows (follower_user_id, designer_user_id)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE follower_user_id = VALUES(follower_user_id)
	`, followerUserID, designerUserID)
	return err
}

func (m *DesignerFollowModel) Unfollow(followerUserID, designerUserID int64) error {
	_, err := m.DB.Exec(`DELETE FROM designer_follows WHERE follower_user_id = ? AND designer_user_id = ?`, followerUserID, designerUserID)
	return err
}

func (m *DesignerFollowModel) IsFollowing(followerUserID, designerUserID int64) (bool, error) {
	var count int64
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM designer_follows WHERE follower_user_id = ? AND designer_user_id = ?`, followerUserID, designerUserID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (m *DesignerFollowModel) CountFollowers(designerUserID int64) (int64, error) {
	var count int64
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM designer_follows WHERE designer_user_id = ?`, designerUserID).Scan(&count)
	return count, err
}

func (m *DesignerFollowModel) CountFollowing(followerUserID int64) (int64, error) {
	var count int64
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM designer_follows WHERE follower_user_id = ?`, followerUserID).Scan(&count)
	return count, err
}
