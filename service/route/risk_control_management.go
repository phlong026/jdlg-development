package route

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"service/model"
)

type riskControlFilters struct {
	Keyword string
}

func parseRiskPageParams(c *gin.Context) (int, int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return page, pageSize, (page - 1) * pageSize
}

func buildRiskKeywordWhere(keyword string) (string, []interface{}) {
	where := "u.user_type = 'miniprogram'"
	args := make([]interface{}, 0)
	trimmed := strings.TrimSpace(keyword)
	if trimmed == "" {
		return where, args
	}
	likeKeyword := "%" + trimmed + "%"
	where += ` AND (
		u.username LIKE ?
		OR COALESCE(p.nickname, '') LIKE ?
		OR COALESCE(p.device_id, '') LIKE ?
	)`
	args = append(args, likeKeyword, likeKeyword, likeKeyword)
	return where, args
}

func queryRiskControlOverview(db *sql.DB) (gin.H, error) {
	sharedDevices := int64(0)
	deviceRiskUsers := int64(0)
	recentDeviceChanges := int64(0)
	abnormalPayments := int64(0)
	failedTasks := int64(0)

	if err := db.QueryRow(`SELECT COUNT(*) FROM (SELECT device_id FROM user_profiles WHERE device_id <> '' GROUP BY device_id HAVING COUNT(DISTINCT user_id) > 1) t`).Scan(&sharedDevices); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM (SELECT user_id FROM user_profiles WHERE device_id <> '' GROUP BY device_id, user_id) t WHERE 1=1`).Scan(&deviceRiskUsers); err != nil {
		deviceRiskUsers = 0
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_profiles WHERE last_device_change_time IS NOT NULL AND last_device_change_time >= DATE_SUB(NOW(), INTERVAL 30 DAY)`).Scan(&recentDeviceChanges); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_orders WHERE status IN ('failed', 'pending') AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)`).Scan(&abnormalPayments); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM ai_tasks WHERE status = 'failed' AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)) + (SELECT COUNT(*) FROM ai_video_tasks WHERE status = 'failed' AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY))`).Scan(&failedTasks); err != nil {
		return nil, err
	}

	return gin.H{
		"shared_devices":        sharedDevices,
		"device_risk_users":     deviceRiskUsers,
		"recent_device_changes": recentDeviceChanges,
		"abnormal_payments":     abnormalPayments,
		"failed_tasks":          failedTasks,
	}, nil
}

func querySharedDeviceGroups(db *sql.DB, filters riskControlFilters, limit, offset int) ([]gin.H, int64, error) {
	whereSQL, args := buildRiskKeywordWhere(filters.Keyword)
	countQuery := `
		SELECT COUNT(*)
		FROM (
			SELECT p.device_id
			FROM user_profiles p
			INNER JOIN users u ON u.id = p.user_id
			LEFT JOIN user_profiles up ON up.user_id = u.id
			WHERE p.device_id <> '' AND ` + strings.ReplaceAll(whereSQL, "p.", "up.") + `
			GROUP BY p.device_id
			HAVING COUNT(DISTINCT p.user_id) > 1
		) t`
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `
		SELECT
			p.device_id,
			COUNT(DISTINCT p.user_id) AS user_count,
			GROUP_CONCAT(CONCAT(u.id, ':', u.username, ':', COALESCE(p.nickname, '')) ORDER BY u.id SEPARATOR '|') AS users,
			MAX(COALESCE(p.last_device_change_time, p.device_bind_time, u.updated_at)) AS latest_activity_at
		FROM user_profiles p
		INNER JOIN users u ON u.id = p.user_id
		WHERE p.device_id <> '' AND ` + whereSQL + `
		GROUP BY p.device_id
		HAVING COUNT(DISTINCT p.user_id) > 1
		ORDER BY user_count DESC, latest_activity_at DESC
		LIMIT ? OFFSET ?`
	listArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := db.Query(query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]gin.H, 0, limit)
	for rows.Next() {
		var deviceID string
		var userCount int64
		var usersRaw string
		var latestActivityAt sql.NullTime
		if err := rows.Scan(&deviceID, &userCount, &usersRaw, &latestActivityAt); err != nil {
			return nil, 0, err
		}
		users := make([]gin.H, 0)
		for _, item := range strings.Split(usersRaw, "|") {
			parts := strings.SplitN(item, ":", 3)
			if len(parts) < 2 {
				continue
			}
			users = append(users, gin.H{
				"user_id":   parts[0],
				"username":  parts[1],
				"nickname":  func() string { if len(parts) >= 3 { return parts[2] }; return "" }(),
			})
		}
		list = append(list, gin.H{
			"device_id":          deviceID,
			"user_count":         userCount,
			"users":              users,
			"latest_activity_at": formatRiskNullTime(latestActivityAt),
		})
	}
	return list, total, rows.Err()
}

func queryRecentDeviceChanges(db *sql.DB, filters riskControlFilters, limit, offset int) ([]gin.H, int64, error) {
	whereSQL, args := buildRiskKeywordWhere(filters.Keyword)
	countQuery := `SELECT COUNT(*) FROM users u LEFT JOIN user_profiles p ON p.user_id = u.id WHERE ` + whereSQL + ` AND p.last_device_change_time IS NOT NULL`
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `
		SELECT u.id, u.username, COALESCE(p.nickname, ''), COALESCE(p.device_id, ''), p.device_bind_time, p.last_device_change_time
		FROM users u
		LEFT JOIN user_profiles p ON p.user_id = u.id
		WHERE ` + whereSQL + ` AND p.last_device_change_time IS NOT NULL
		ORDER BY p.last_device_change_time DESC
		LIMIT ? OFFSET ?`
	listArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := db.Query(query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]gin.H, 0, limit)
	for rows.Next() {
		var userID int64
		var username string
		var nickname string
		var deviceID string
		var bindAt sql.NullTime
		var changedAt sql.NullTime
		if err := rows.Scan(&userID, &username, &nickname, &deviceID, &bindAt, &changedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, gin.H{
			"user_id":                  userID,
			"username":                 username,
			"nickname":                 nickname,
			"device_id":                deviceID,
			"device_bind_time":         formatRiskNullTime(bindAt),
			"last_device_change_time":  formatRiskNullTime(changedAt),
		})
	}
	return list, total, rows.Err()
}

func queryRiskAlerts(db *sql.DB, filters riskControlFilters, limit, offset int) ([]gin.H, int64, error) {
	trimmed := strings.TrimSpace(filters.Keyword)
	likeKeyword := "%" + trimmed + "%"
	args := make([]interface{}, 0)
	keywordSQL := ""
	if trimmed != "" {
		keywordSQL = ` AND (username LIKE ? OR nickname LIKE ?)`
		args = append(args, likeKeyword, likeKeyword)
	}
	baseSQL := `
		SELECT user_id, username, nickname, 'payment' AS alert_type, payment_count AS alert_count, latest_time AS latest_time, latest_detail AS detail
		FROM (
			SELECT u.id AS user_id, u.username AS username, COALESCE(p.nickname, '') AS nickname,
			       COUNT(*) AS payment_count,
			       MAX(o.created_at) AS latest_time,
			       MAX(CONCAT(o.order_no, ' / ', o.status, ' / ', o.title)) AS latest_detail
			FROM user_orders o
			INNER JOIN users u ON u.id = o.user_id
			LEFT JOIN user_profiles p ON p.user_id = u.id
			WHERE u.user_type = 'miniprogram' AND o.status IN ('failed', 'pending') AND o.created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
			GROUP BY u.id, u.username, p.nickname
			HAVING COUNT(*) >= 2
		) payment_alerts
		UNION ALL
		SELECT user_id, username, nickname, 'task' AS alert_type, failed_count AS alert_count, latest_time AS latest_time, latest_detail AS detail
		FROM (
			SELECT t.user_id AS user_id, t.username AS username, t.nickname AS nickname,
			       COUNT(*) AS failed_count,
			       MAX(t.created_at) AS latest_time,
			       MAX(CONCAT(t.task_type, ' / ', t.task_no, ' / ', t.error_message)) AS latest_detail
			FROM (
				SELECT a.user_id, u.username, COALESCE(p.nickname, '') AS nickname, 'image' AS task_type, a.task_no, COALESCE(a.error_message, '') AS error_message, a.created_at
				FROM ai_tasks a
				INNER JOIN users u ON u.id = a.user_id
				LEFT JOIN user_profiles p ON p.user_id = a.user_id
				WHERE u.user_type = 'miniprogram' AND a.status = 'failed' AND a.created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
				UNION ALL
				SELECT v.user_id, u.username, COALESCE(p.nickname, '') AS nickname, 'video' AS task_type, CONCAT('v', v.id) AS task_no, COALESCE(v.error_message, '') AS error_message, v.created_at
				FROM ai_video_tasks v
				INNER JOIN users u ON u.id = v.user_id
				LEFT JOIN user_profiles p ON p.user_id = v.user_id
				WHERE u.user_type = 'miniprogram' AND v.status = 'failed' AND v.created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
			) t
			GROUP BY t.user_id, t.username, t.nickname
			HAVING COUNT(*) >= 2
		) task_alerts`
	countQuery := `SELECT COUNT(*) FROM (` + baseSQL + `) alerts WHERE 1=1` + keywordSQL
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT user_id, username, nickname, alert_type, alert_count, latest_time, detail FROM (` + baseSQL + `) alerts WHERE 1=1` + keywordSQL + ` ORDER BY latest_time DESC, alert_count DESC LIMIT ? OFFSET ?`
	listArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := db.Query(query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]gin.H, 0, limit)
	for rows.Next() {
		var userID int64
		var username string
		var nickname string
		var alertType string
		var alertCount int64
		var latestTime time.Time
		var detail string
		if err := rows.Scan(&userID, &username, &nickname, &alertType, &alertCount, &latestTime, &detail); err != nil {
			return nil, 0, err
		}
		list = append(list, gin.H{"user_id": userID, "username": username, "nickname": nickname, "alert_type": alertType, "alert_count": alertCount, "latest_time": latestTime.Format(time.RFC3339), "detail": detail})
	}
	return list, total, rows.Err()
}

func queryRiskUsers(db *sql.DB, filters riskControlFilters, limit, offset int) ([]gin.H, int64, error) {
	whereSQL, args := buildRiskKeywordWhere(filters.Keyword)
	countQuery := `
		SELECT COUNT(*)
		FROM users u
		LEFT JOIN user_profiles p ON p.user_id = u.id
		LEFT JOIN (
			SELECT device_id, COUNT(DISTINCT user_id) AS device_user_count FROM user_profiles WHERE device_id <> '' GROUP BY device_id
		) device_stats ON device_stats.device_id = p.device_id
		LEFT JOIN (
			SELECT user_id, COUNT(*) AS failed_task_count, MAX(created_at) AS latest_failed_task_at FROM (
				SELECT user_id, created_at FROM ai_tasks WHERE status = 'failed' AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
				UNION ALL
				SELECT user_id, created_at FROM ai_video_tasks WHERE status = 'failed' AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
			) task_failures GROUP BY user_id
		) task_stats ON task_stats.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) AS abnormal_payment_count, MAX(created_at) AS latest_abnormal_payment_at FROM user_orders WHERE status IN ('failed', 'pending') AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY) GROUP BY user_id
		) payment_stats ON payment_stats.user_id = u.id
		WHERE ` + whereSQL + ` AND (
			COALESCE(device_stats.device_user_count, 0) > 1 OR
			p.last_device_change_time IS NOT NULL OR
			COALESCE(task_stats.failed_task_count, 0) >= 2 OR
			COALESCE(payment_stats.abnormal_payment_count, 0) >= 2
		)`
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `
		SELECT
			u.id, u.username, COALESCE(p.nickname, ''), COALESCE(p.device_id, ''),
			COALESCE(device_stats.device_user_count, 0),
			p.last_device_change_time,
			COALESCE(task_stats.failed_task_count, 0),
			COALESCE(payment_stats.abnormal_payment_count, 0)
		FROM users u
		LEFT JOIN user_profiles p ON p.user_id = u.id
		LEFT JOIN (
			SELECT device_id, COUNT(DISTINCT user_id) AS device_user_count FROM user_profiles WHERE device_id <> '' GROUP BY device_id
		) device_stats ON device_stats.device_id = p.device_id
		LEFT JOIN (
			SELECT user_id, COUNT(*) AS failed_task_count FROM (
				SELECT user_id FROM ai_tasks WHERE status = 'failed' AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
				UNION ALL
				SELECT user_id FROM ai_video_tasks WHERE status = 'failed' AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
			) task_failures GROUP BY user_id
		) task_stats ON task_stats.user_id = u.id
		LEFT JOIN (
			SELECT user_id, COUNT(*) AS abnormal_payment_count FROM user_orders WHERE status IN ('failed', 'pending') AND created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY) GROUP BY user_id
		) payment_stats ON payment_stats.user_id = u.id
		WHERE ` + whereSQL + ` AND (
			COALESCE(device_stats.device_user_count, 0) > 1 OR
			p.last_device_change_time IS NOT NULL OR
			COALESCE(task_stats.failed_task_count, 0) >= 2 OR
			COALESCE(payment_stats.abnormal_payment_count, 0) >= 2
		)
		ORDER BY COALESCE(payment_stats.abnormal_payment_count, 0) DESC, COALESCE(task_stats.failed_task_count, 0) DESC, COALESCE(device_stats.device_user_count, 0) DESC, p.last_device_change_time DESC
		LIMIT ? OFFSET ?`
	listArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := db.Query(query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]gin.H, 0, limit)
	for rows.Next() {
		var userID int64
		var username string
		var nickname string
		var deviceID string
		var sharedDeviceCount int64
		var lastDeviceChangeTime sql.NullTime
		var failedTaskCount int64
		var abnormalPaymentCount int64
		if err := rows.Scan(&userID, &username, &nickname, &deviceID, &sharedDeviceCount, &lastDeviceChangeTime, &failedTaskCount, &abnormalPaymentCount); err != nil {
			return nil, 0, err
		}
		tags := make([]string, 0)
		if sharedDeviceCount > 1 {
			tags = append(tags, fmt.Sprintf("同设备%d账号", sharedDeviceCount))
		}
		if lastDeviceChangeTime.Valid {
			tags = append(tags, "近期换绑设备")
		}
		if failedTaskCount >= 2 {
			tags = append(tags, "任务失败异常")
		}
		if abnormalPaymentCount >= 2 {
			tags = append(tags, "支付异常")
		}
		list = append(list, gin.H{"user_id": userID, "username": username, "nickname": nickname, "device_id": deviceID, "shared_device_count": sharedDeviceCount, "last_device_change_time": formatRiskNullTime(lastDeviceChangeTime), "failed_task_count": failedTaskCount, "abnormal_payment_count": abnormalPaymentCount, "risk_tags": tags})
	}
	return list, total, rows.Err()
}

func formatRiskNullTime(value sql.NullTime) string {
	if !value.Valid || value.Time.IsZero() {
		return ""
	}
	return value.Time.Format(time.RFC3339)
}

func RegisterRiskControlManagementRoutes(r *gin.RouterGroup, userDBModel *model.UserModel) {
	risk := r.Group("/risk-control")

	risk.GET("/overview", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "风控台服务不可用"})
			return
		}
		data, err := queryRiskControlOverview(userDBModel.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取风控概览失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": data})
	})

	risk.GET("/device-groups", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "风控台服务不可用"})
			return
		}
		page, pageSize, offset := parseRiskPageParams(c)
		list, total, err := querySharedDeviceGroups(userDBModel.DB, riskControlFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取同设备账号列表失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})

	risk.GET("/device-changes", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "风控台服务不可用"})
			return
		}
		page, pageSize, offset := parseRiskPageParams(c)
		list, total, err := queryRecentDeviceChanges(userDBModel.DB, riskControlFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取设备换绑记录失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})

	risk.GET("/alerts", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "风控台服务不可用"})
			return
		}
		page, pageSize, offset := parseRiskPageParams(c)
		list, total, err := queryRiskAlerts(userDBModel.DB, riskControlFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取风险告警失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})

	risk.GET("/users", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "风控台服务不可用"})
			return
		}
		page, pageSize, offset := parseRiskPageParams(c)
		list, total, err := queryRiskUsers(userDBModel.DB, riskControlFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取风险用户失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})
}
