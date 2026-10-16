package route

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
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
		return nil, err
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
	// 异常访问（联动B）：单访客近 7 天页面访问量超阈值的访客数；追踪未启用时为 0
	accessAlerts := int64(0)
	if tenant := accessAlertTenant(); tenant != "" {
		if err := db.QueryRow(`SELECT COUNT(*) FROM (SELECT visitor_key FROM tracking_records WHERE tenant_id = ? AND kind = 'event' AND occurred_at >= DATE_SUB(NOW(), INTERVAL 7 DAY) GROUP BY visitor_key HAVING COUNT(*) >= ?) t`, tenant, accessAlertThreshold()).Scan(&accessAlerts); err != nil {
			return nil, err
		}
	}

	return gin.H{
		"shared_devices":        sharedDevices,
		"device_risk_users":     deviceRiskUsers,
		"recent_device_changes": recentDeviceChanges,
		"abnormal_payments":     abnormalPayments,
		"failed_tasks":          failedTasks,
		"access_alerts":         accessAlerts,
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

// accessAlertThreshold 异常访问告警阈值：单访客近 7 天页面访问量达到该值即告警。
// 默认 300；用 TRACKING_ACCESS_ALERT_THRESHOLD 覆盖（tracking_records 是追加型事件流，
// 阈值过低的告警噪音由运营按环境调整）。
func accessAlertThreshold() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("TRACKING_ACCESS_ALERT_THRESHOLD"))); err == nil && v > 0 {
		return v
	}
	return 300
}

// accessAlertTenant 返回追踪租户（TRACKING_TENANT）。追踪未启用时返回空，访问告警随之无数据。
func accessAlertTenant() string {
	return strings.TrimSpace(os.Getenv("TRACKING_TENANT"))
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
	// 访问告警（联动B）：追踪事件流里单访客近 7 天页面访问量超阈值的，
	// 已识别（u:<id>）关联到账号，匿名访客以 visitor_key 呈现（user_id=0）。
	accessTenant := accessAlertTenant()
	accessBranch := ""
	accessArgs := make([]interface{}, 0)
	if accessTenant != "" {
		accessBranch = `
		UNION ALL
		SELECT user_id, username, nickname, 'access' AS alert_type, pv AS alert_count, latest AS latest_time, detail AS detail, visitor_key AS visitor_key
		FROM (
			SELECT
				CASE WHEN a.visitor_key LIKE 'u:%' THEN CAST(SUBSTRING(a.visitor_key, 3) AS UNSIGNED) ELSE 0 END AS user_id,
				CASE WHEN a.visitor_key LIKE 'u:%' THEN COALESCE(NULLIF(ua.username, ''), CONCAT('用户', SUBSTRING(a.visitor_key, 3))) ELSE '匿名访客' END AS username,
				COALESCE(up.nickname, '') AS nickname,
				a.pv, a.latest,
				CONCAT('近7天页面访问 ', a.pv, ' 次，达到异常阈值 ', ?, '（访客 ', a.visitor_key, '）') AS detail,
				a.visitor_key
			FROM (
				SELECT visitor_key, COUNT(*) AS pv, MAX(occurred_at) AS latest
				FROM tracking_records
				WHERE tenant_id = ? AND kind = 'event' AND occurred_at >= DATE_SUB(NOW(), INTERVAL 7 DAY)
				GROUP BY visitor_key
				HAVING COUNT(*) >= ?
				ORDER BY pv DESC
				LIMIT 200
			) a
			LEFT JOIN users ua ON a.visitor_key LIKE 'u:%' AND ua.id = CAST(SUBSTRING(a.visitor_key, 3) AS UNSIGNED)
			LEFT JOIN user_profiles up ON up.user_id = ua.id
		) access_alerts`
		accessArgs = append(accessArgs, accessAlertThreshold(), accessTenant, accessAlertThreshold())
	}
	baseSQL := `
		SELECT user_id, username, nickname, 'payment' AS alert_type, payment_count AS alert_count, latest_time AS latest_time, latest_detail AS detail, NULL AS visitor_key
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
		SELECT user_id, username, nickname, 'task' AS alert_type, failed_count AS alert_count, latest_time AS latest_time, latest_detail AS detail, NULL AS visitor_key
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
		) task_alerts` + accessBranch
	countQuery := `SELECT COUNT(*) FROM (` + baseSQL + `) alerts WHERE 1=1` + keywordSQL
	countArgs := append(append([]interface{}{}, accessArgs...), args...)
	var total int64
	if err := db.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := `SELECT user_id, username, nickname, alert_type, alert_count, latest_time, detail, visitor_key FROM (` + baseSQL + `) alerts WHERE 1=1` + keywordSQL + ` ORDER BY latest_time DESC, alert_count DESC LIMIT ? OFFSET ?`
	listArgs := append(append(append([]interface{}{}, accessArgs...), args...), limit, offset)
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
		var visitorKey sql.NullString
		if err := rows.Scan(&userID, &username, &nickname, &alertType, &alertCount, &latestTime, &detail, &visitorKey); err != nil {
			return nil, 0, err
		}
		row := gin.H{"user_id": userID, "username": username, "nickname": nickname, "alert_type": alertType, "alert_count": alertCount, "latest_time": latestTime.Format(time.RFC3339), "detail": detail}
		if visitorKey.Valid && visitorKey.String != "" {
			row["visitor_key"] = visitorKey.String
		}
		list = append(list, row)
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

// queryShareAttribution 分享→账号归因（联动C）：从追踪事件流按 share_id 聚合
// 带来的访客数、其中以登录态进入的识别数，以及匿名进入后绑定账号（identity 链接）
// 的转化数。只对已识别账号出数；匿名安装标识不与设备指纹关联（隐私边界）。
// 追踪未启用（TRACKING_TENANT 为空）时返回空列表。
func queryShareAttribution(db *sql.DB, days int) ([]gin.H, error) {
	tenant := accessAlertTenant()
	if tenant == "" || db == nil {
		return []gin.H{}, nil
	}
	if days < 1 {
		days = 30
	}
	if days > 90 {
		days = 90
	}
	// 旧数据 share_id 可能只在 payload 里（见 tracking.addShareIn 的同款兜底）
	visitorBase := `
		SELECT COALESCE(NULLIF(e.share_id, ''), JSON_UNQUOTE(JSON_EXTRACT(e.payload, '$.share_id'))) AS share_id,
		       e.visitor_key, MAX(e.occurred_at) AS entered_at
		FROM tracking_records e
		WHERE e.tenant_id = ? AND e.kind = 'event' AND e.occurred_at >= DATE_SUB(NOW(), INTERVAL ? DAY)
		  AND (e.share_id <> '' OR JSON_UNQUOTE(JSON_EXTRACT(e.payload, '$.share_id')) <> '')
		GROUP BY 1, e.visitor_key`
	rows, err := db.Query(`SELECT share_id, COUNT(*) AS visitor_count,
			SUM(visitor_key LIKE 'u:%') AS identified_count, MAX(entered_at) AS latest_entered_at
		FROM (`+visitorBase+`) v GROUP BY share_id ORDER BY latest_entered_at DESC LIMIT 50`, tenant, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type attribution struct {
		shareID     string
		visitors    int64
		identified  int64
		latestEnter string
	}
	order := make([]string, 0, 50)
	byShare := make(map[string]*attribution, 50)
	for rows.Next() {
		var shareID string
		var visitors, identified int64
		var latest sql.NullTime
		if err := rows.Scan(&shareID, &visitors, &identified, &latest); err != nil {
			return nil, err
		}
		order = append(order, shareID)
		byShare[shareID] = &attribution{shareID: shareID, visitors: visitors, identified: identified, latestEnter: formatRiskNullTime(latest)}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 转化数：匿名访客键后来出现在 identity 链接的 original 侧 → 绑定为账号
	convRows, err := db.Query(`SELECT v.share_id, COUNT(DISTINCT JSON_UNQUOTE(JSON_EXTRACT(i.payload, '$.visitor_key'))) AS converted
		FROM (`+visitorBase+`) v
		JOIN tracking_records i ON i.tenant_id = ? AND i.kind = 'identity'
			AND JSON_UNQUOTE(JSON_EXTRACT(i.payload, '$.original_visitor_key')) = v.visitor_key
		WHERE JSON_UNQUOTE(JSON_EXTRACT(i.payload, '$.visitor_key')) LIKE 'u:%'
		GROUP BY v.share_id`, tenant, days, tenant)
	if err != nil {
		return nil, err
	}
	defer convRows.Close()
	for convRows.Next() {
		var shareID string
		var converted int64
		if err := convRows.Scan(&shareID, &converted); err != nil {
			return nil, err
		}
		if item := byShare[shareID]; item != nil {
			item.identified += converted
		}
	}
	if err := convRows.Err(); err != nil {
		return nil, err
	}

	// 素材标题：share 记录给出 content_id，content 记录给出 title（任一缺失就显示 share_id）
	titles := shareContentTitles(db, tenant, order)

	list := make([]gin.H, 0, len(order))
	for _, shareID := range order {
		item := byShare[shareID]
		title := titles[shareID]
		if title == "" {
			title = shareID
		}
		list = append(list, gin.H{
			"share_id":          shareID,
			"content_title":     title,
			"visitor_count":     item.visitors,
			"identified_count":  item.identified,
			"latest_entered_at": item.latestEnter,
		})
	}
	return list, nil
}

// shareContentTitles 批量取 share → content 标题映射；查询失败返回空表（标题降级为 share_id）。
func shareContentTitles(db *sql.DB, tenant string, shareIDs []string) map[string]string {
	out := make(map[string]string, len(shareIDs))
	if db == nil || len(shareIDs) == 0 {
		return out
	}
	contentIDs := make([]string, 0)
	placeholders := make([]string, len(shareIDs))
	args := make([]interface{}, 0, len(shareIDs)+1)
	args = append(args, tenant)
	for i, id := range shareIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	shareRows, err := db.Query(`SELECT record_id, COALESCE(NULLIF(content_id, ''), JSON_UNQUOTE(JSON_EXTRACT(payload, '$.content_id')))
		FROM tracking_records WHERE tenant_id = ? AND kind = 'share' AND record_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return out
	}
	defer shareRows.Close()
	shareToContent := make(map[string]string, len(shareIDs))
	for shareRows.Next() {
		var shareID, contentID string
		if err := shareRows.Scan(&shareID, &contentID); err != nil {
			return out
		}
		if contentID != "" {
			shareToContent[shareID] = contentID
			contentIDs = append(contentIDs, contentID)
		}
	}
	if len(contentIDs) == 0 {
		return out
	}
	contentPlaceholders := make([]string, len(contentIDs))
	contentArgs := make([]interface{}, 0, len(contentIDs)+1)
	contentArgs = append(contentArgs, tenant)
	seen := make(map[string]bool, len(contentIDs))
	unique := make([]string, 0, len(contentIDs))
	for _, id := range contentIDs {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
			contentPlaceholders[len(unique)-1] = "?"
			contentArgs = append(contentArgs, id)
		}
	}
	contentRows, err := db.Query(`SELECT record_id, JSON_UNQUOTE(JSON_EXTRACT(payload, '$.title'))
		FROM tracking_records WHERE tenant_id = ? AND kind = 'content' AND record_id IN (`+strings.Join(contentPlaceholders, ",")+`)`, contentArgs...)
	if err != nil {
		return out
	}
	defer contentRows.Close()
	titlesByContent := make(map[string]string, len(unique))
	for contentRows.Next() {
		var contentID, title string
		if err := contentRows.Scan(&contentID, &title); err != nil {
			return out
		}
		titlesByContent[contentID] = title
	}
	for shareID, contentID := range shareToContent {
		if title := titlesByContent[contentID]; title != "" {
			out[shareID] = title
		}
	}
	return out
}

func formatRiskNullTime(value sql.NullTime) string {
	if !value.Valid || value.Time.IsZero() {
		return ""
	}
	return value.Time.Format(time.RFC3339)
}

// riskTagsByUserIDs 批量计算用户风险标签，口径与 queryRiskUsers 逐行逻辑一致
// （同设备多账号 / 近期换绑 / 近7天任务失败≥2 / 近7天异常支付≥2）。
// 供分享访问记录接口给已识别访客注入 risk_tags，让风控信号与访问行为在一个页面互相印证。
func riskTagsByUserIDs(db *sql.DB, userIDs []int64) (map[int64][]string, error) {
	out := make(map[int64][]string, len(userIDs))
	if db == nil || len(userIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(userIDs))
	args := make([]interface{}, 0, len(userIDs))
	for i, id := range userIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := `
		SELECT u.id,
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
		WHERE u.id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID int64
		var sharedDeviceCount int64
		var lastDeviceChangeTime sql.NullTime
		var failedTaskCount int64
		var abnormalPaymentCount int64
		if err := rows.Scan(&userID, &sharedDeviceCount, &lastDeviceChangeTime, &failedTaskCount, &abnormalPaymentCount); err != nil {
			return nil, err
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
		if len(tags) > 0 {
			out[userID] = tags
		}
	}
	return out, rows.Err()
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

	// 分享→账号归因（联动C）：分销邀请页「分享归因」区块的数据源
	risk.GET("/share-attribution", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "风控台服务不可用"})
			return
		}
		days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
		list, err := queryShareAttribution(userDBModel.DB, days)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取分享归因失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "days": days}})
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
