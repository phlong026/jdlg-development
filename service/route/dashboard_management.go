package route

import (
    "database/sql"
    "fmt"
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

// 待跟进线索的 hint：有高意向时提示优先承接，否则说明队列含义
func leadTodoHint(highTodoLeads int64) string {
    if highTodoLeads > 0 {
        return fmt.Sprintf("含高意向 %d 条，优先电话或微信承接", highTodoLeads)
    }
    return "新线索和跟进中"
}

func RegisterDashboardManagementRoutes(r *gin.RouterGroup, userDBModel *model.UserModel, customerServiceModel *model.CustomerServiceModel) {
    dashboard := r.Group("/dashboard")

    // 今日待办：运营/客服每天上班第一眼要看的东西（B1）。
    // 每项数字都带直达链接所需的路由，前端不做任何二次拼装。
    // kind=action 需要今天处理；kind=watch 只监控不堆明细（明细在各模块页）。
    dashboard.GET("/todo", func(c *gin.Context) {
        if userDBModel == nil || userDBModel.DB == nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "总控台服务不可用"})
            return
        }
        db := userDBModel.DB

        pendingCertifications, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM certification_applications WHERE status = 'pending_review'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取待审认证数失败: " + err.Error()})
            return
        }
        openTickets, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM support_tickets WHERE status = 'open'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取待处理工单数失败: " + err.Error()})
            return
        }
        overdueTickets, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM support_tickets WHERE status = 'open' AND created_at < (NOW() - INTERVAL 48 HOUR)`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取超时工单数失败: " + err.Error()})
            return
        }
        // 线索计数复用线索总览的口径（与销售线索页角标同源，不会出现两套数字）
        var todoLeads, overdueLeads, highTodoLeads int64
        if customerServiceModel != nil {
            if overview, err := customerServiceModel.Overview(); err == nil && overview != nil {
                todoLeads = overview.TodoCount
                overdueLeads = overview.OverdueCount
                highTodoLeads = overview.HighTodoCount
            }
        }
        // 认证分类型：给待办 hint 一句「设计师 X · 企业 Y」，运营不用点进去就知道构成
        designerCerts, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM certification_applications WHERE status = 'pending_review' AND type = 'designer'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取待审设计师认证数失败: " + err.Error()})
            return
        }
        enterpriseCerts, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM certification_applications WHERE status = 'pending_review' AND type <> 'designer'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取待审企业认证数失败: " + err.Error()})
            return
        }
        // 工单最老等待时长：hint 里用「最老已等待 N 小时」表达积压程度
        oldestTicketHours, err := queryDashboardInt64(db, `SELECT COALESCE(TIMESTAMPDIFF(HOUR, MIN(created_at), NOW()), 0) FROM support_tickets WHERE status = 'open'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取工单最老等待时长失败: " + err.Error()})
            return
        }
        // 失败任务限定近 7 天：全量口径没有清理机制、只增不减，数字会永远虚高
        failedTasks7d, err := queryDashboardInt64(db, `
            SELECT COUNT(*) FROM (
                SELECT id FROM ai_tasks WHERE status = 'failed' AND created_at >= (NOW() - INTERVAL 7 DAY)
                UNION ALL
                SELECT id FROM ai_video_tasks WHERE status = 'failed' AND created_at >= (NOW() - INTERVAL 7 DAY)
            ) t`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取近7天失败任务数失败: " + err.Error()})
            return
        }
        todayFailedTasks, err := queryDashboardInt64(db, `
            SELECT COUNT(*) FROM (
                SELECT id FROM ai_tasks WHERE status = 'failed' AND created_at >= CURDATE()
                UNION ALL
                SELECT id FROM ai_video_tasks WHERE status = 'failed' AND created_at >= CURDATE()
            ) t`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日失败任务数失败: " + err.Error()})
            return
        }
        // 灵石耗尽限定「曾充值」：从未充值的新用户 stones=0 是正常状态，不算异常
        zeroStonesPaying, err := queryDashboardInt64(db, `
            SELECT COUNT(*)
            FROM users u
            WHERE u.user_type = 'miniprogram'
              AND COALESCE(u.stones, 0) <= 0
              AND EXISTS (
                    SELECT 1 FROM user_orders uo
                    WHERE uo.user_id = u.id
                      AND uo.type = 'recharge'
                      AND uo.status = 'success'
                      AND uo.amount > 0
                      AND (uo.order_category = '' OR uo.order_category = 'recharge')
              )`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取灵石耗尽用户数失败: " + err.Error()})
            return
        }
        pendingWechat, err := queryDashboardInt64(db, `
            SELECT COUNT(*)
            FROM users u
            LEFT JOIN user_profiles p ON p.user_id = u.id
            LEFT JOIN user_memberships um ON um.user_id = u.id
            WHERE `+dashboardPendingWechatWhere())
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取下载权限待核验用户数失败: " + err.Error()})
            return
        }

        // hint 是给操作者的一句上下文（构成/积压程度），让待办不用点进去就能判断轻重；
        // path 直接带筛选参数，落地页读到参数后自动应用对应筛选。
        c.JSON(http.StatusOK, gin.H{
            "code": 0,
            "msg":  "success",
            "data": gin.H{
                "items": []gin.H{
                    {"key": "certifications", "kind": "action", "label": "待审认证", "count": pendingCertifications, "path": "/certification",
                        "hint": fmt.Sprintf("设计师 %d 条 · 企业 %d 条", designerCerts, enterpriseCerts), "urgent": false},
                    {"key": "tickets", "kind": "action", "label": "待处理工单", "count": openTickets, "path": "/support-tickets",
                        "hint": fmt.Sprintf("最老已等待 %d 小时", oldestTicketHours), "urgent": false},
                    {"key": "tickets_overdue", "kind": "action", "label": "超时 48h 工单", "count": overdueTickets, "path": "/support-tickets",
                        "hint": "超过 48 小时未响应，建议今天清零", "urgent": overdueTickets > 0},
                    {"key": "leads_todo", "kind": "action", "label": "待跟进线索", "count": todoLeads, "path": "/customer-leads?queue=todo",
                        "hint": leadTodoHint(highTodoLeads), "urgent": false},
                    {"key": "leads_overdue", "kind": "action", "label": "超时未跟进线索", "count": overdueLeads, "path": "/customer-leads?queue=overdue",
                        "hint": "该联系但还没推进", "urgent": overdueLeads > 0},
                    {"key": "failed_tasks_7d", "kind": "action", "label": "失败任务（近7天）", "count": failedTasks7d, "path": "/ai-tasks?status=failed",
                        "hint": fmt.Sprintf("今日新增 %d 条，可一键补偿或转工单", todayFailedTasks), "urgent": failedTasks7d > 0},
                    {"key": "zero_stones_paying", "kind": "watch", "label": "灵石耗尽用户", "count": zeroStonesPaying, "path": "/users",
                        "hint": "曾充值、余额为 0，可能卡在下一步", "urgent": false},
                    {"key": "pending_wechat", "kind": "watch", "label": "下载权限待核验", "count": pendingWechat, "path": "/users?wechat=pending",
                        "hint": "有下载权限但企微手机号未核验", "urgent": false},
                },
            },
        })
    })

    dashboard.GET("/overview", func(c *gin.Context) {
        if userDBModel == nil || userDBModel.DB == nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "总控台服务不可用"})
            return
        }
        db := userDBModel.DB

        // 充值收入一律取 amount_fen（人民币分）。
        // user_orders.amount 记录的是灵石数量（充值为正、消费为负），
        // 老口径 SUM(amount) 会把灵石当钱展示，财务上不可用。
        todayIncomeFen, err := queryDashboardInt64(db, `
            SELECT COALESCE(SUM(CASE WHEN amount_fen > 0 THEN amount_fen ELSE 0 END), 0)
            FROM user_orders
            WHERE type = 'recharge' AND status = 'success' AND created_at >= CURDATE()`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日充值收入失败: " + err.Error()})
            return
        }
        yesterdayIncomeFen, err := queryDashboardInt64(db, `
            SELECT COALESCE(SUM(CASE WHEN amount_fen > 0 THEN amount_fen ELSE 0 END), 0)
            FROM user_orders
            WHERE type = 'recharge' AND status = 'success'
              AND created_at >= (CURDATE() - INTERVAL 1 DAY) AND created_at < CURDATE()`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取昨日充值收入失败: " + err.Error()})
            return
        }

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
        yesterdayNewUsers, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM users WHERE user_type = 'miniprogram' AND created_at >= (CURDATE() - INTERVAL 1 DAY) AND created_at < CURDATE()`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取昨日新增用户失败: " + err.Error()})
            return
        }
        todayOrders, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM user_orders WHERE created_at >= CURDATE()`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日订单数失败: " + err.Error()})
            return
        }
        yesterdayOrders, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM user_orders WHERE created_at >= (CURDATE() - INTERVAL 1 DAY) AND created_at < CURDATE()`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取昨日订单数失败: " + err.Error()})
            return
        }
        todaySuccessOrders, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM user_orders WHERE created_at >= CURDATE() AND status = 'success'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取今日成功订单数失败: " + err.Error()})
            return
        }
        yesterdaySuccessOrders, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM user_orders WHERE created_at >= (CURDATE() - INTERVAL 1 DAY) AND created_at < CURDATE() AND status = 'success'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取昨日成功订单数失败: " + err.Error()})
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
        yesterdayTasks, err := queryDashboardInt64(db, `
            SELECT COUNT(*) FROM (
                SELECT id FROM ai_tasks WHERE created_at >= (CURDATE() - INTERVAL 1 DAY) AND created_at < CURDATE()
                UNION ALL
                SELECT id FROM ai_video_tasks WHERE created_at >= (CURDATE() - INTERVAL 1 DAY) AND created_at < CURDATE()
            ) t`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取昨日任务量失败: " + err.Error()})
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
        yesterdayFailedTasks, err := queryDashboardInt64(db, `
            SELECT COUNT(*) FROM (
                SELECT id FROM ai_tasks WHERE created_at >= (CURDATE() - INTERVAL 1 DAY) AND created_at < CURDATE() AND status = 'failed'
                UNION ALL
                SELECT id FROM ai_video_tasks WHERE created_at >= (CURDATE() - INTERVAL 1 DAY) AND created_at < CURDATE() AND status = 'failed'
            ) t`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取昨日失败任务数失败: " + err.Error()})
            return
        }
        pendingCertifications, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM certification_applications WHERE status = 'pending_review'`)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取待审核认证数失败: " + err.Error()})
            return
        }

        c.JSON(http.StatusOK, gin.H{
            "code": 0,
            "msg":  "success",
            "data": gin.H{
                "total_users":              totalUsers,
                "today_new_users":          todayNewUsers,
                "yesterday_new_users":      yesterdayNewUsers,
                "today_income_fen":         todayIncomeFen,
                "yesterday_income_fen":     yesterdayIncomeFen,
                "today_orders":             todayOrders,
                "yesterday_orders":         yesterdayOrders,
                "today_success_orders":     todaySuccessOrders,
                "yesterday_success_orders": yesterdaySuccessOrders,
                "today_image_tasks":        todayImageTasks,
                "today_video_tasks":        todayVideoTasks,
                "yesterday_tasks":          yesterdayTasks,
                "today_failed_tasks":       todayFailedImageTasks + todayFailedVideoTasks,
                "yesterday_failed_tasks":   yesterdayFailedTasks,
                "pending_certifications":   pendingCertifications,
            },
        })
    })

    dashboard.GET("/trends", func(c *gin.Context) {
        if userDBModel == nil || userDBModel.DB == nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "总控台服务不可用"})
            return
        }
        db := userDBModel.DB

        // days 白名单：只接受 7/30，非法值回落 7（拼进 SQL 的是整数，无注入面）
        days := 7
        if c.DefaultQuery("days", "7") == "30" {
            days = 30
        }
        sinceExpr := fmt.Sprintf("DATE_SUB(CURDATE(), INTERVAL %d DAY)", days-1)

        incomeTrendMap, err := queryDashboardDailyInt64Map(db, fmt.Sprintf(`
            SELECT DATE_FORMAT(created_at, '%%Y-%%m-%%d') AS stat_date,
                   COALESCE(SUM(CASE WHEN amount_fen > 0 THEN amount_fen ELSE 0 END), 0)
            FROM user_orders
            WHERE type = 'recharge' AND status = 'success' AND created_at >= %s
            GROUP BY stat_date`, sinceExpr))
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取充值收入趋势失败: " + err.Error()})
            return
        }
        userTrendMap, err := queryDashboardDailyInt64Map(db, fmt.Sprintf(`SELECT DATE_FORMAT(created_at, '%%Y-%%m-%%d') AS stat_date, COUNT(*) FROM users WHERE user_type = 'miniprogram' AND created_at >= %s GROUP BY stat_date`, sinceExpr))
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取用户趋势失败: " + err.Error()})
            return
        }
        orderTrendMap, err := queryDashboardDailyInt64Map(db, fmt.Sprintf(`SELECT DATE_FORMAT(created_at, '%%Y-%%m-%%d') AS stat_date, COUNT(*) FROM user_orders WHERE created_at >= %s GROUP BY stat_date`, sinceExpr))
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取订单趋势失败: " + err.Error()})
            return
        }
        imageTaskTrendMap, err := queryDashboardDailyInt64Map(db, fmt.Sprintf(`SELECT DATE_FORMAT(created_at, '%%Y-%%m-%%d') AS stat_date, COUNT(*) FROM ai_tasks WHERE created_at >= %s GROUP BY stat_date`, sinceExpr))
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取图片任务趋势失败: " + err.Error()})
            return
        }
        videoTaskTrendMap, err := queryDashboardDailyInt64Map(db, fmt.Sprintf(`SELECT DATE_FORMAT(created_at, '%%Y-%%m-%%d') AS stat_date, COUNT(*) FROM ai_video_tasks WHERE created_at >= %s GROUP BY stat_date`, sinceExpr))
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取视频任务趋势失败: " + err.Error()})
            return
        }
        failedImageTrendMap, err := queryDashboardDailyInt64Map(db, fmt.Sprintf(`SELECT DATE_FORMAT(created_at, '%%Y-%%m-%%d') AS stat_date, COUNT(*) FROM ai_tasks WHERE created_at >= %s AND status = 'failed' GROUP BY stat_date`, sinceExpr))
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取失败图片任务趋势失败: " + err.Error()})
            return
        }
        failedVideoTrendMap, err := queryDashboardDailyInt64Map(db, fmt.Sprintf(`SELECT DATE_FORMAT(created_at, '%%Y-%%m-%%d') AS stat_date, COUNT(*) FROM ai_video_tasks WHERE created_at >= %s AND status = 'failed' GROUP BY stat_date`, sinceExpr))
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取失败视频任务趋势失败: " + err.Error()})
            return
        }

        trendList := make([]gin.H, 0, days)
        now := time.Now()
        for i := days - 1; i >= 0; i-- {
            day := now.AddDate(0, 0, -i)
            dayKey := day.Format("2006-01-02")
            trendList = append(trendList, gin.H{
                "date":           dayKey,
                "label":          day.Format("01-02"),
                "income_fen":     incomeTrendMap[dayKey],
                "new_users":      userTrendMap[dayKey],
                "order_count":    orderTrendMap[dayKey],
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
}