package component

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"service/model"
)

// tableInitOnce 确保建表 DDL 只执行一次
var tableInitOnce sync.Once
var tableInitErr error

// DownloadLimiter 模板下载限制检查器
type DownloadLimiter struct {
	DB  *model.MembershipPlanModel
	RDB *redis.Client
}

// NewDownloadLimiter 创建下载限制器
func NewDownloadLimiter(planModel *model.MembershipPlanModel, rdb *redis.Client) *DownloadLimiter {
	return &DownloadLimiter{DB: planModel, RDB: rdb}
}

// CheckDownloadAccess 检查用户是否可以下载模板
// 返回: (allowed bool, reason string, err error)
func (dl *DownloadLimiter) CheckDownloadAccess(userID int64, activeMembership *model.UserMembership, templateID int64, imageIndex int) (bool, string, error) {
	if dl.DB == nil {
		return true, "", nil // 没有配置限制器时默认放行
	}
	if activeMembership == nil {
		return true, "", nil // 无会员时走原有逻辑，不额外限制
	}

	// 获取套餐配置
	plan, err := dl.DB.GetByCode(activeMembership.PlanCode)
	if err != nil || plan == nil {
		return true, "", nil // 找不到套餐配置时默认放行
	}

	ctx := context.Background()
	alreadyRecorded := false
	if templateID > 0 && imageIndex >= 0 {
		recorded, recordErr := dl.hasDownloadRecord(userID, templateID, imageIndex)
		if recordErr != nil {
			return false, "", fmt.Errorf("查询下载记录失败: %w", recordErr)
		}
		alreadyRecorded = recorded
	}

	// 1. 检查下载有效期
	if plan.DownloadValidityDays > 0 && !activeMembership.GrantedAt.IsZero() {
		expiry := activeMembership.GrantedAt.Add(time.Duration(plan.DownloadValidityDays) * 24 * time.Hour)
		if time.Now().After(expiry) {
			return false, "下载有效期已过期", nil
		}
	}

	if alreadyRecorded {
		return true, "", nil
	}

	// 2. 检查累计下载量（查真实下载记录）
	if plan.MaxTotalDownloads > 0 {
		count, err := dl.countUserDownloads(userID)
		if err != nil {
			return false, "", fmt.Errorf("查询下载记录失败: %w", err)
		}
		if count >= int64(plan.MaxTotalDownloads) {
			return false, "累计下载次数已用完", nil
		}
	}

	// 3. 检查每日上限
	if plan.DailyDownloadLimit > 0 {
		count, err := dl.countUserDownloadsToday(userID)
		if err != nil {
			return false, "", fmt.Errorf("查询每日下载次数失败: %w", err)
		}
		if count >= int64(plan.DailyDownloadLimit) {
			return false, "今日下载次数已达上限", nil
		}
	}

	// 4. 频控（每分钟限制）
	if plan.RateLimitPerMinute > 0 && dl.RDB != nil {
		minuteKey := fmt.Sprintf("template:rl:%d:%d", userID, time.Now().Unix()/60)
		count, err := dl.RDB.Get(ctx, minuteKey).Int64()
		if err != nil && err != redis.Nil {
			return false, "", fmt.Errorf("查询请求频率失败: %w", err)
		}
		if count >= int64(plan.RateLimitPerMinute) {
			return false, "请求过于频繁，请稍后再试", nil
		}
	}

	return true, "", nil
}

// RecordDownload 记录一次下载（更新 Redis 计数器）
func (dl *DownloadLimiter) RecordDownload(userID int64, templateID int64, imageIndex int) {
	if templateID > 0 && imageIndex >= 0 {
		_ = dl.createDownloadRecord(userID, templateID, imageIndex)
	}
	if dl.RDB == nil {
		return
	}
	ctx := context.Background()

	minuteKey := fmt.Sprintf("template:rl:%d:%d", userID, time.Now().Unix()/60)
	if _, err := dl.RDB.Incr(ctx, minuteKey).Result(); err == nil {
		dl.RDB.Expire(ctx, minuteKey, 2*time.Minute)
	}
}

func (dl *DownloadLimiter) ensureDownloadRecordTable() error {
	tableInitOnce.Do(func() {
		db := GetDB()
		if db == nil {
			return
		}
		_, tableInitErr = db.Exec(`
CREATE TABLE IF NOT EXISTS template_download_records (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	user_id BIGINT UNSIGNED NOT NULL,
	template_id BIGINT UNSIGNED NOT NULL,
	image_index INT NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE KEY uk_user_template_image (user_id, template_id, image_index),
	KEY idx_user_created_at (user_id, created_at),
	KEY idx_template_id (template_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='模板真实下载记录';
`)
	})
	return tableInitErr
}

func (dl *DownloadLimiter) hasDownloadRecord(userID int64, templateID int64, imageIndex int) (bool, error) {
	if err := dl.ensureDownloadRecordTable(); err != nil {
		return false, err
	}
	db := GetDB()
	if db == nil {
		return false, nil
	}
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM template_download_records WHERE user_id = ? AND template_id = ? AND image_index = ?", userID, templateID, imageIndex).Scan(&count)
	return count > 0, err
}

func (dl *DownloadLimiter) createDownloadRecord(userID int64, templateID int64, imageIndex int) error {
	if err := dl.ensureDownloadRecordTable(); err != nil {
		return err
	}
	db := GetDB()
	if db == nil {
		return nil
	}
	_, err := db.Exec("INSERT IGNORE INTO template_download_records (user_id, template_id, image_index) VALUES (?, ?, ?)", userID, templateID, imageIndex)
	return err
}

func (dl *DownloadLimiter) countUserDownloads(userID int64) (int64, error) {
	if err := dl.ensureDownloadRecordTable(); err != nil {
		return 0, err
	}
	db := GetDB()
	if db == nil {
		return 0, nil
	}
	var count int64
	err := db.QueryRow("SELECT COUNT(*) FROM template_download_records WHERE user_id = ?", userID).Scan(&count)
	return count, err
}

func (dl *DownloadLimiter) countUserDownloadsToday(userID int64) (int64, error) {
	if err := dl.ensureDownloadRecordTable(); err != nil {
		return 0, err
	}
	db := GetDB()
	if db == nil {
		return 0, nil
	}
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)
	var count int64
	err := db.QueryRow("SELECT COUNT(*) FROM template_download_records WHERE user_id = ? AND created_at >= ? AND created_at < ?", userID, startOfDay, endOfDay).Scan(&count)
	return count, err
}
