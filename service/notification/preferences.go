package notification

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Preferences 控制"外发投递"的分类开关（三期增补 B16b）：关闭某类只停止
// 服务号外发，站内收件箱照常保留每条提醒。无行/无表 = 全部开启。
type Preferences struct {
	UserID           int64     `json:"-"`
	BrowseDelivery   bool      `json:"browse_delivery"`
	ProjectDelivery  bool      `json:"project_delivery"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
}

func defaultPreferences(recipientID int64) Preferences {
	return Preferences{UserID: recipientID, BrowseDelivery: true, ProjectDelivery: true}
}

// deliveryCategory 把通知 kind 归入用户可关闭的外发类别；空类别表示不可关闭。
func deliveryCategory(kind string) string {
	switch kind {
	case "browse", "collection_directory_browse", "collection_content_browse":
		return "browse"
	case "project_note_open", "project_note_feedback", "project_package_receipt", "project_note_join_request":
		return "project"
	}
	return ""
}

// PreferenceStore 暴露收件人偏好读写；路由层用类型断言探测能力。
type PreferenceStore interface {
	LoadPreferences(ctx context.Context, recipientID int64) (Preferences, error)
	SavePreferences(ctx context.Context, recipientID int64, prefs Preferences) error
}

// LoadPreferences 读取偏好；表缺失或无行都返回默认全开，绝不因此阻断提醒。
func (s *SQLStore) LoadPreferences(ctx context.Context, recipientID int64) (Preferences, error) {
	if recipientID <= 0 {
		return Preferences{}, ErrInvalidInput
	}
	prefs := defaultPreferences(recipientID)
	var browse, project int
	err := s.db.QueryRowContext(ctx, `SELECT browse_delivery,project_delivery,updated_at FROM notification_preferences WHERE user_id=?`, recipientID).
		Scan(&browse, &project, &prefs.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return prefs, nil
	}
	if err != nil {
		// 表未迁移或瞬断：按全开处理并让调用方感知非致命错误。
		return prefs, storeError("preferences_load", err)
	}
	prefs.BrowseDelivery, prefs.ProjectDelivery = browse == 1, project == 1
	return prefs, nil
}

// SavePreferences upsert 偏好；表缺失返回错误由路由转为 503（设置失败必须可见，
// 不能静默假装已保存）。
func (s *SQLStore) SavePreferences(ctx context.Context, recipientID int64, prefs Preferences) error {
	if recipientID <= 0 {
		return ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO notification_preferences (user_id,browse_delivery,project_delivery,updated_at)
VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE browse_delivery=VALUES(browse_delivery),project_delivery=VALUES(project_delivery),updated_at=VALUES(updated_at)`,
		recipientID, boolTiny(prefs.BrowseDelivery), boolTiny(prefs.ProjectDelivery), time.Now().UTC())
	if err != nil {
		return storeError("preferences_save", err)
	}
	return nil
}

func boolTiny(value bool) int {
	if value {
		return 1
	}
	return 0
}

// categoryOptedOut 在物化事务内判断该类提醒是否被收件人关闭外发。
// 表缺失（未迁移）视为未关闭，保持迁移前行为。
func (s *SQLStore) categoryOptedOut(ctx context.Context, tx *sql.Tx, recipientID int64, kind string) bool {
	category := deliveryCategory(kind)
	if category == "" || recipientID <= 0 {
		return false
	}
	column := "browse_delivery"
	if category == "project" {
		column = "project_delivery"
	}
	var enabled int
	err := tx.QueryRowContext(ctx, `SELECT `+column+` FROM notification_preferences WHERE user_id=?`, recipientID).Scan(&enabled)
	if err != nil {
		return false
	}
	return enabled == 0
}
