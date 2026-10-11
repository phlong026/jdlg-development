package model

import (
	"database/sql"
	"time"
)

// CheckinRecord 签到记录
type CheckinRecord struct {
	ID                int64     `json:"id" db:"id"`
	UserID            int64     `json:"user_id" db:"user_id"`
	CheckinDate       time.Time `json:"checkin_date" db:"checkin_date"` // 签到日期（只包含日期部分）
	ConsecutiveDays   int       `json:"consecutive_days" db:"consecutive_days"` // 连续签到天数
	Reward            int64     `json:"reward" db:"reward"` // 本次签到获得的灵石
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
}

// CheckinModel 签到数据访问层
type CheckinModel struct {
	DB *sql.DB
}

// NewCheckinModel 创建签到模型
func NewCheckinModel(db *sql.DB) *CheckinModel {
	return &CheckinModel{DB: db}
}

// GetTodayCheckin 获取用户今天的签到记录
func (m *CheckinModel) GetTodayCheckin(userID int64) (*CheckinRecord, error) {
	record := &CheckinRecord{}
	query := `SELECT id, user_id, checkin_date, consecutive_days, reward, created_at 
	          FROM checkin_records 
	          WHERE user_id = ? AND DATE(checkin_date) = CURDATE()`
	err := m.DB.QueryRow(query, userID).Scan(
		&record.ID, &record.UserID, &record.CheckinDate, &record.ConsecutiveDays,
		&record.Reward, &record.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // 今天未签到
	}
	if err != nil {
		return nil, err
	}
	return record, nil
}

// GetConsecutiveDays 获取用户当前连续签到天数
func (m *CheckinModel) GetConsecutiveDays(userID int64) (int, error) {
	// 获取最近一次签到记录
	var consecutiveDays int
	query := `SELECT consecutive_days 
	          FROM checkin_records 
	          WHERE user_id = ? 
	          ORDER BY checkin_date DESC 
	          LIMIT 1`
	err := m.DB.QueryRow(query, userID).Scan(&consecutiveDays)
	if err == sql.ErrNoRows {
		return 0, nil // 从未签到过
	}
	if err != nil {
		return 0, err
	}

	// 检查最后一次签到是否是昨天（连续）还是更早（断签）
	var lastCheckinDate time.Time
	query2 := `SELECT checkin_date 
	           FROM checkin_records 
	           WHERE user_id = ? 
	           ORDER BY checkin_date DESC 
	           LIMIT 1`
	err = m.DB.QueryRow(query2, userID).Scan(&lastCheckinDate)
	if err != nil {
		return 0, err
	}

	// 计算距离今天的天数
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	lastDate := time.Date(lastCheckinDate.Year(), lastCheckinDate.Month(), lastCheckinDate.Day(), 0, 0, 0, 0, lastCheckinDate.Location())
	daysDiff := int(today.Sub(lastDate).Hours() / 24)

	if daysDiff == 1 {
		// 昨天签到了，连续
		return consecutiveDays, nil
	} else if daysDiff > 1 {
		// 断签了
		return 0, nil
	} else if daysDiff == 0 {
		// 今天已签到
		return consecutiveDays, nil
	}

	return 0, nil
}

// CreateCheckin 创建签到记录
func (m *CheckinModel) CreateCheckin(userID int64, consecutiveDays int, reward int64) error {
	query := `INSERT INTO checkin_records (user_id, checkin_date, consecutive_days, reward, created_at) 
	          VALUES (?, CURDATE(), ?, ?, NOW())`
	_, err := m.DB.Exec(query, userID, consecutiveDays, reward)
	return err
}

// InitTable 初始化签到记录表
func (m *CheckinModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS checkin_records (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	user_id BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
	checkin_date DATE NOT NULL COMMENT '签到日期',
	consecutive_days INT NOT NULL DEFAULT 1 COMMENT '连续签到天数',
	reward BIGINT NOT NULL DEFAULT 0 COMMENT '本次签到获得的灵石',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
	UNIQUE KEY uk_user_date (user_id, checkin_date),
	INDEX idx_user_id (user_id),
	INDEX idx_checkin_date (checkin_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	_, err := m.DB.Exec(schema)
	return err
}
