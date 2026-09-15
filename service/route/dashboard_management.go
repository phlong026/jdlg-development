package route

import (
    "database/sql"
    "net/http"
    "time"

    "github.com/gin-gonic/gin"

    "service/model"
)

func queryDashboardInt64(db *sql.DB, query string, args ...interface{}) (int64, error) {
    var value int64
    err := db.QueryRow(query, args...).Scan(&value)
    return value, err
}

func queryDashboardDailyInt64Map(db *sql.DB, query string, args ...interface{}) (map[string]int64, error) {
    rows, err := db.Query(query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    result := make(map[string]int64)
    for rows.Next() {
        var day string
        var value int64
        if err := rows.Scan(&day, &value); err != nil {
            return nil, err
        }
        result[day] = value
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return result, nil
}

func dashboardPendingWechatWhere() string {
    return `
        u.user_type = 'miniprogram'
        AND (
            (um.user_id IS NOT NULL AND um.status = 'active' AND um.template_download_enabled = 1 AND um.expired_at > NOW())
            OR (
                um.user_id IS NULL AND EXISTS (
                    SELECT 1
                    FROM user_orders uo
                    WHERE uo.user_id = u.id
                      AND uo.type = 'recharge'
                      AND uo.status = 'success'
                      AND uo.amount > 0
                      AND (uo.order_category = '' OR uo.order_category = 'recharge')
                )
            )
        )
        AND (
            COALESCE(p.enterprise_wechat_verified, 0) = 0
            OR TRIM(COALESCE(p.enterprise_wechat_contact, '')) = ''
            OR TRIM(COALESCE(p.enterprise_wechat_contact, '')) IN ('企微已添加，待补全联系方式', '待补全联系方式')
        )
    `
}

func RegisterDashboardManagementRoutes(r *gin.RouterGroup, userDBModel *model.UserModel) {
    dashboard := r.Group("/dashboard")

    dashboard.GET("/overview", func(c *gin.Context) {
        if userDBModel == nil || userDBModel.DB == nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "总控台服务不可用"})
            return
        }
        db := userDBModel.DB

        totalUsers, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM users WHERE user_type = 'miniprogram'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取用户总数失败: " + err.Error()})
            return
        }
        todayNewUsers, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM users WHERE user_type = 'miniprogram' AND created_at >= CURDATE()`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日新增用户失败: " + err.Error()})
            return
        }
        todayOrders, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM user_orders WHERE created_at >= CURDATE()`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日订单数失败: " + err.Error()})
            return
        }
        todaySuccessOrders, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM user_orders WHERE created_at >= CURDATE() AND status = 'success'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日成功订单数失败: " + err.Error()})
            return
        }
        todaySuccessAmount, err := queryDashboardInt64(db, `SELECT COALESCE(SUM(CASE WHEN amount > 0 THEN amount ELSE 0 END), 0) FROM user_orders WHERE created_at >= CURDATE() AND status = 'success'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日成交金额失败: " + err.Error()})
            return
        }
        todayImageTasks, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM ai_tasks WHERE created_at >= CURDATE()`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日图片任务数失败: " + err.Error()})
            return
        }
        todayVideoTasks, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM ai_video_tasks WHERE created_at >= CURDATE()`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日视频任务数失败: " + err.Error()})
            return
        }
        todayFailedImageTasks, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM ai_tasks WHERE created_at >= CURDATE() AND status = 'failed'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日失败图片任务数失败: " + err.Error()})
            return
        }
        todayFailedVideoTasks, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM ai_video_tasks WHERE created_at >= CURDATE() AND status = 'failed'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日失败视频任务数失败: " + err.Error()})
            return
        }
        pendingCertifications, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM certification_applications WHERE status = 'pending_review'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取待审核认证数失败: " + err.Error()})
            return
        }
        zeroStonesCount, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM users WHERE user_type = 'miniprogram' AND COALESCE(stones, 0) <= 0`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取灵石不足用户数失败: " + err.Error()})
            return
        }
        pendingWechatCount, err := queryDashboardInt64(db, `
            SELECT COUNT(*)
            FROM users u
            LEFT JOIN user_profiles p ON p.user_id = u.id
            LEFT JOIN user_memberships um ON um.user_id = u.id
            WHERE `+dashboardPendingWechatWhere())
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取下载权限待核验用户数失败: " + err.Error()})
            return
        }
        failedTaskCount, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM (SELECT user_id FROM ai_tasks WHERE status = 'failed' UNION ALL SELECT user_id FROM ai_video_tasks WHERE status = 'failed') t`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取失败任务数失败: " + err.Error()})
            return
        }
        pendingExceptions := zeroStonesCount + pendingWechatCount + failedTaskCount

        c.JSON(http.StatusOK, gin.H{
            "code": 0,
            "msg":  "success",
            "data": gin.H{
                "total_users":            totalUsers,
                "today_new_users":        todayNewUsers,
                "today_orders":           todayOrders,
                "today_success_orders":   todaySuccessOrders,
                "today_success_amount":   todaySuccessAmount,
                "today_image_tasks":      todayImageTasks,
                "today_video_tasks":      todayVideoTasks,
                "today_failed_tasks":     todayFailedImageTasks + todayFailedVideoTasks,
                "pending_certifications": pendingCertifications,
                "pending_exceptions":     pendingExceptions,
            },
        })
    })

    dashboard.GET("/trends", func(c *gin.Context) {
        if userDBModel == nil || userDBModel.DB == nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "总控台服务不可用"})
            return
        }
        db := userDBModel.DB

        userTrendMap, err := queryDashboardDailyInt64Map(db, `SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS stat_date, COUNT(*) FROM users WHERE user_type = 'miniprogram' AND created_at >= DATE_SUB(CURDATE(), INTERVAL 6 DAY) GROUP BY stat_date`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取用户趋势失败: " + err.Error()})
            return
        }
        orderTrendMap, err := queryDashboardDailyInt64Map(db, `SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS stat_date, COUNT(*) FROM user_orders WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL 6 DAY) GROUP BY stat_date`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取订单趋势失败: " + err.Error()})
            return
        }
        successOrderTrendMap, err := queryDashboardDailyInt64Map(db, `SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS stat_date, COUNT(*) FROM user_orders WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL 6 DAY) AND status = 'success' GROUP BY stat_date`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取成功订单趋势失败: " + err.Error()})
            return
        }
        successAmountTrendMap, err := queryDashboardDailyInt64Map(db, `SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS stat_date, COALESCE(SUM(CASE WHEN amount > 0 THEN amount ELSE 0 END), 0) FROM user_orders WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL 6 DAY) AND status = 'success' GROUP BY stat_date`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取成交金额趋势失败: " + err.Error()})
            return
        }
        imageTaskTrendMap, err := queryDashboardDailyInt64Map(db, `SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS stat_date, COUNT(*) FROM ai_tasks WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL 6 DAY) GROUP BY stat_date`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取图片任务趋势失败: " + err.Error()})
            return
        }
        videoTaskTrendMap, err := queryDashboardDailyInt64Map(db, `SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS stat_date, COUNT(*) FROM ai_video_tasks WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL 6 DAY) GROUP BY stat_date`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取视频任务趋势失败: " + err.Error()})
            return
        }
        failedImageTrendMap, err := queryDashboardDailyInt64Map(db, `SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS stat_date, COUNT(*) FROM ai_tasks WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL 6 DAY) AND status = 'failed' GROUP BY stat_date`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取失败图片任务趋势失败: " + err.Error()})
            return
        }
        failedVideoTrendMap, err := queryDashboardDailyInt64Map(db, `SELECT DATE_FORMAT(created_at, '%Y-%m-%d') AS stat_date, COUNT(*) FROM ai_video_tasks WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL 6 DAY) AND status = 'failed' GROUP BY stat_date`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取失败视频任务趋势失败: " + err.Error()})
            return
        }

        trendList := make([]gin.H, 0, 7)
        now := time.Now()
        for i := 6; i >= 0; i-- {
            day := now.AddDate(0, 0, -i)
            dayKey := day.Format("2006-01-02")
            trendList = append(trendList, gin.H{
                "date":           dayKey,
                "label":          day.Format("01-02"),
                "new_users":      userTrendMap[dayKey],
                "order_count":    orderTrendMap[dayKey],
                "success_orders": successOrderTrendMap[dayKey],
                "success_amount": successAmountTrendMap[dayKey],
                "image_tasks":    imageTaskTrendMap[dayKey],
                "video_tasks":    videoTaskTrendMap[dayKey],
                "failed_tasks":   failedImageTrendMap[dayKey] + failedVideoTrendMap[dayKey],
            })
        }

        c.JSON(http.StatusOK, gin.H{
            "code": 0,
            "msg":  "success",
            "data": trendList,
        })
    })

    dashboard.GET("/todos", func(c *gin.Context) {
        if userDBModel == nil || userDBModel.DB == nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "总控台服务不可用"})
            return
        }
        db := userDBModel.DB

        pendingCertificationCount, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM certification_applications WHERE status = 'pending_review'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取待审核认证数失败: " + err.Error()})
            return
        }
        zeroStonesCount, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM users WHERE user_type = 'miniprogram' AND COALESCE(stones, 0) <= 0`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取灵石不足用户数失败: " + err.Error()})
            return
        }
        pendingWechatCount, err := queryDashboardInt64(db, `
            SELECT COUNT(*)
            FROM users u
            LEFT JOIN user_profiles p ON p.user_id = u.id
            LEFT JOIN user_memberships um ON um.user_id = u.id
            WHERE `+dashboardPendingWechatWhere())
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取下载权限待核验用户数失败: " + err.Error()})
            return
        }
        failedTaskCount, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM (SELECT user_id FROM ai_tasks WHERE status = 'failed' UNION ALL SELECT user_id FROM ai_video_tasks WHERE status = 'failed') t`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取失败任务数失败: " + err.Error()})
            return
        }

        pendingCertRows, err := db.Query(`
            SELECT id, user_id, type, identity_type, created_at
            FROM certification_applications
            WHERE status = 'pending_review'
            ORDER BY created_at DESC
            LIMIT 6
        `)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取待审核认证队列失败: " + err.Error()})
            return
        }
        defer pendingCertRows.Close()
        pendingCertifications := make([]gin.H, 0)
        for pendingCertRows.Next() {
            var id int64
            var userID int64
            var certType string
            var identityType string
            var createdAt time.Time
            if err := pendingCertRows.Scan(&id, &userID, &certType, &identityType, &createdAt); err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析待审核认证队列失败: " + err.Error()})
                return
            }
            pendingCertifications = append(pendingCertifications, gin.H{
                "id":            id,
                "user_id":       userID,
                "type":          certType,
                "identity_type": identityType,
                "created_at":    createdAt,
            })
        }
        if err := pendingCertRows.Err(); err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取待审核认证队列失败: " + err.Error()})
            return
        }

        zeroStonesRows, err := db.Query(`
            SELECT u.id, u.username, COALESCE(p.nickname, ''), COALESCE(u.stones, 0)
            FROM users u
            LEFT JOIN user_profiles p ON p.user_id = u.id
            WHERE u.user_type = 'miniprogram' AND COALESCE(u.stones, 0) <= 0
            ORDER BY u.updated_at DESC
            LIMIT 6
        `)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取灵石不足用户队列失败: " + err.Error()})
            return
        }
        defer zeroStonesRows.Close()
        zeroStonesUsers := make([]gin.H, 0)
        for zeroStonesRows.Next() {
            var userID int64
            var username string
            var nickname string
            var stones int64
            if err := zeroStonesRows.Scan(&userID, &username, &nickname, &stones); err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析灵石不足用户队列失败: " + err.Error()})
                return
            }
            zeroStonesUsers = append(zeroStonesUsers, gin.H{
                "user_id":  userID,
                "username": username,
                "nickname": nickname,
                "stones":   stones,
            })
        }
        if err := zeroStonesRows.Err(); err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取灵石不足用户队列失败: " + err.Error()})
            return
        }

        pendingWechatRows, err := db.Query(`
            SELECT u.id, u.username, COALESCE(p.nickname, ''), COALESCE(p.enterprise_wechat_contact, '')
            FROM users u
            LEFT JOIN user_profiles p ON p.user_id = u.id
            LEFT JOIN user_memberships um ON um.user_id = u.id
            WHERE `+dashboardPendingWechatWhere()+`
            ORDER BY u.updated_at DESC
            LIMIT 6
        `)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取下载权限待核验用户队列失败: " + err.Error()})
            return
        }
        defer pendingWechatRows.Close()
        pendingWechatUsers := make([]gin.H, 0)
        for pendingWechatRows.Next() {
            var userID int64
            var username string
            var nickname string
            var contact string
            if err := pendingWechatRows.Scan(&userID, &username, &nickname, &contact); err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析下载权限待核验用户队列失败: " + err.Error()})
                return
            }
            pendingWechatUsers = append(pendingWechatUsers, gin.H{
                "user_id":                    userID,
                "username":                   username,
                "nickname":                   nickname,
                "enterprise_wechat_contact":  contact,
            })
        }
        if err := pendingWechatRows.Err(); err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取下载权限待核验用户队列失败: " + err.Error()})
            return
        }

        failedTaskRows, err := db.Query(`
            SELECT t.user_id, t.username, t.nickname, t.task_no, t.task_type, t.scene, t.model, t.error_message, t.created_at
            FROM (
                SELECT a.user_id, u.username, COALESCE(p.nickname, '') AS nickname, a.task_no, 'image' AS task_type, a.scene, COALESCE(a.model, '') AS model, COALESCE(a.error_message, '') AS error_message, a.created_at
                FROM ai_tasks a
                LEFT JOIN users u ON u.id = a.user_id
                LEFT JOIN user_profiles p ON p.user_id = a.user_id
                WHERE a.status = 'failed'
                UNION ALL
                SELECT v.user_id, u.username, COALESCE(p.nickname, '') AS nickname, CONCAT('v', v.id) AS task_no, 'video' AS task_type, v.model AS scene, COALESCE(v.model, '') AS model, COALESCE(v.error_message, '') AS error_message, v.created_at
                FROM ai_video_tasks v
                LEFT JOIN users u ON u.id = v.user_id
                LEFT JOIN user_profiles p ON p.user_id = v.user_id
                WHERE v.status = 'failed'
            ) t
            ORDER BY t.created_at DESC
            LIMIT 6
        `)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取失败任务队列失败: " + err.Error()})
            return
        }
        defer failedTaskRows.Close()
        failedTasks := make([]gin.H, 0)
        for failedTaskRows.Next() {
            var userID int64
            var username string
            var nickname string
            var taskNo string
            var taskType string
            var scene string
            var taskModel string
            var errorMessage string
            var createdAt time.Time
            if err := failedTaskRows.Scan(&userID, &username, &nickname, &taskNo, &taskType, &scene, &taskModel, &errorMessage, &createdAt); err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析失败任务队列失败: " + err.Error()})
                return
            }
            failedTasks = append(failedTasks, gin.H{
                "user_id":       userID,
                "username":      username,
                "nickname":      nickname,
                "task_no":       taskNo,
                "task_type":     taskType,
                "scene":         scene,
                "model":         taskModel,
                "error_message": errorMessage,
                "created_at":    createdAt,
            })
        }
        if err := failedTaskRows.Err(); err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取失败任务队列失败: " + err.Error()})
            return
        }

        c.JSON(http.StatusOK, gin.H{
            "code": 0,
            "msg":  "success",
            "data": gin.H{
                "counts": gin.H{
                    "pending_certifications": pendingCertificationCount,
                    "zero_stones_users":     zeroStonesCount,
                    "pending_wechat_users":  pendingWechatCount,
                    "failed_tasks":          failedTaskCount,
                },
                "pending_certifications": pendingCertifications,
                "zero_stones_users":     zeroStonesUsers,
                "pending_wechat_users":  pendingWechatUsers,
                "failed_tasks":          failedTasks,
            },
        })
    })
}
