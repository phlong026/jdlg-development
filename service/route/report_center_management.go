package route

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"service/model"
)

type reportCenterFilters struct {
	ReportType string
	Period     string
	StartDate  time.Time
	EndDate    time.Time
}

type reportCenterColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func parseReportCenterFilters(c *gin.Context) (reportCenterFilters, error) {
	reportType := strings.TrimSpace(c.DefaultQuery("report_type", "user_growth"))
	period := strings.TrimSpace(c.DefaultQuery("period", "daily"))
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -6)
	if period == "weekly" {
		startDate = endDate.AddDate(0, 0, -55)
	} else if period == "monthly" {
		startDate = endDate.AddDate(0, -5, 0)
	}
	if raw := strings.TrimSpace(c.Query("start_date")); raw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local)
		if err != nil {
			return reportCenterFilters{}, err
		}
		startDate = parsed
	}
	if raw := strings.TrimSpace(c.Query("end_date")); raw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local)
		if err != nil {
			return reportCenterFilters{}, err
		}
		endDate = parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	}
	if period != "daily" && period != "weekly" && period != "monthly" {
		period = "daily"
	}
	if reportType == "" {
		reportType = "user_growth"
	}
	return reportCenterFilters{ReportType: reportType, Period: period, StartDate: startDate, EndDate: endDate}, nil
}

func reportBucketExpr(column, period string) string {
	switch period {
	case "weekly":
		return fmt.Sprintf("CONCAT(DATE_FORMAT(DATE_SUB(%s, INTERVAL WEEKDAY(%s) DAY), '%%Y-%%m-%%d'), ' ~ ', DATE_FORMAT(DATE_ADD(DATE_SUB(%s, INTERVAL WEEKDAY(%s) DAY), INTERVAL 6 DAY), '%%Y-%%m-%%d'))", column, column, column, column)
	case "monthly":
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m')", column)
	default:
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", column)
	}
}

func buildReportBuckets(filters reportCenterFilters) []string {
	buckets := make([]string, 0)
	cursor := filters.StartDate
	for !cursor.After(filters.EndDate) {
		switch filters.Period {
		case "weekly":
			start := cursor.AddDate(0, 0, -int(cursor.Weekday()+6)%7)
			end := start.AddDate(0, 0, 6)
			label := start.Format("2006-01-02") + " ~ " + end.Format("2006-01-02")
			if len(buckets) == 0 || buckets[len(buckets)-1] != label {
				buckets = append(buckets, label)
			}
			cursor = start.AddDate(0, 0, 7)
		case "monthly":
			label := cursor.Format("2006-01")
			if len(buckets) == 0 || buckets[len(buckets)-1] != label {
				buckets = append(buckets, label)
			}
			cursor = cursor.AddDate(0, 1, 0)
		default:
			buckets = append(buckets, cursor.Format("2006-01-02"))
			cursor = cursor.AddDate(0, 0, 1)
		}
	}
	return buckets
}

func scanReportMetricMap(db *sql.DB, query string, args ...interface{}) (map[string]int64, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int64)
	for rows.Next() {
		var bucket string
		var value int64
		if err := rows.Scan(&bucket, &value); err != nil {
			return nil, err
		}
		result[bucket] = value
	}
	return result, rows.Err()
}

func queryReportCenterOverview(db *sql.DB) (gin.H, error) {
	totalUsers, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM users WHERE user_type = 'miniprogram'`)
	if err != nil {
		return nil, err
	}
	totalRevenue, err := queryDashboardInt64(db, `SELECT COALESCE(SUM(CASE WHEN amount > 0 THEN amount ELSE 0 END), 0) FROM user_orders WHERE status = 'success'`)
	if err != nil {
		return nil, err
	}
	totalTasks, err := queryDashboardInt64(db, `SELECT (SELECT COUNT(*) FROM ai_tasks) + (SELECT COUNT(*) FROM ai_video_tasks)`)
	if err != nil {
		return nil, err
	}
	totalTemplates, err := queryDashboardInt64(db, `SELECT COUNT(*) FROM templates`)
	if err != nil {
		return nil, err
	}
	designerCount, err := queryDashboardInt64(db, `SELECT COUNT(DISTINCT creator_user_id) FROM templates WHERE creator_user_id IS NOT NULL AND creator_user_id > 0`)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"total_users":     totalUsers,
		"total_revenue":   totalRevenue,
		"total_tasks":     totalTasks,
		"total_templates": totalTemplates,
		"designer_count":  designerCount,
	}, nil
}

func buildUserGrowthReport(db *sql.DB, filters reportCenterFilters) ([]reportCenterColumn, []gin.H, gin.H, error) {
	bucketExprUsers := reportBucketExpr("created_at", filters.Period)
	newUserMap, err := scanReportMetricMap(db, `SELECT `+bucketExprUsers+` AS bucket, COUNT(*) FROM users WHERE user_type = 'miniprogram' AND created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	activityBucketExpr := reportBucketExpr("activity_time", filters.Period)
	activeUserMap, err := scanReportMetricMap(db, `SELECT `+activityBucketExpr+` AS bucket, COUNT(DISTINCT user_id) FROM (SELECT user_id, created_at AS activity_time FROM ai_tasks WHERE created_at BETWEEN ? AND ? UNION ALL SELECT user_id, created_at AS activity_time FROM ai_video_tasks WHERE created_at BETWEEN ? AND ? UNION ALL SELECT user_id, created_at AS activity_time FROM user_orders WHERE created_at BETWEEN ? AND ?) t GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate, filters.StartDate, filters.EndDate, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	rows := make([]gin.H, 0)
	totalNew := int64(0)
	totalActive := int64(0)
	for _, bucket := range buildReportBuckets(filters) {
		newUsers := newUserMap[bucket]
		activeUsers := activeUserMap[bucket]
		rows = append(rows, gin.H{"period": bucket, "new_users": newUsers, "active_users": activeUsers})
		totalNew += newUsers
		totalActive += activeUsers
	}
	return []reportCenterColumn{{Key: "period", Label: "周期"}, {Key: "new_users", Label: "新增用户"}, {Key: "active_users", Label: "活跃用户"}}, rows, gin.H{"new_users": totalNew, "active_users": totalActive}, nil
}

func buildRevenueReport(db *sql.DB, filters reportCenterFilters) ([]reportCenterColumn, []gin.H, gin.H, error) {
	bucketExpr := reportBucketExpr("created_at", filters.Period)
	totalOrderMap, err := scanReportMetricMap(db, `SELECT `+bucketExpr+` AS bucket, COUNT(*) FROM user_orders WHERE created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	successOrderMap, err := scanReportMetricMap(db, `SELECT `+bucketExpr+` AS bucket, COUNT(*) FROM user_orders WHERE created_at BETWEEN ? AND ? AND status = 'success' GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	successAmountMap, err := scanReportMetricMap(db, `SELECT `+bucketExpr+` AS bucket, COALESCE(SUM(CASE WHEN amount > 0 THEN amount ELSE 0 END), 0) FROM user_orders WHERE created_at BETWEEN ? AND ? AND status = 'success' GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	rows := make([]gin.H, 0)
	totalOrders := int64(0)
	totalSuccessOrders := int64(0)
	totalSuccessAmount := int64(0)
	for _, bucket := range buildReportBuckets(filters) {
		orderCount := totalOrderMap[bucket]
		successCount := successOrderMap[bucket]
		successAmount := successAmountMap[bucket]
		conversionRate := 0.0
		if orderCount > 0 {
			conversionRate = float64(successCount) / float64(orderCount)
		}
		rows = append(rows, gin.H{"period": bucket, "order_count": orderCount, "success_orders": successCount, "success_amount": successAmount, "pay_conversion_rate": conversionRate})
		totalOrders += orderCount
		totalSuccessOrders += successCount
		totalSuccessAmount += successAmount
	}
	totalConversionRate := 0.0
	if totalOrders > 0 {
		totalConversionRate = float64(totalSuccessOrders) / float64(totalOrders)
	}
	return []reportCenterColumn{{Key: "period", Label: "周期"}, {Key: "order_count", Label: "订单数"}, {Key: "success_orders", Label: "成功订单"}, {Key: "success_amount", Label: "成交金额"}, {Key: "pay_conversion_rate", Label: "支付转化率"}}, rows, gin.H{"order_count": totalOrders, "success_orders": totalSuccessOrders, "success_amount": totalSuccessAmount, "pay_conversion_rate": totalConversionRate}, nil
}

func buildAITaskReport(db *sql.DB, filters reportCenterFilters) ([]reportCenterColumn, []gin.H, gin.H, error) {
	bucketExprImage := reportBucketExpr("created_at", filters.Period)
	imageTaskMap, err := scanReportMetricMap(db, `SELECT `+bucketExprImage+` AS bucket, COUNT(*) FROM ai_tasks WHERE created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	imageSuccessMap, err := scanReportMetricMap(db, `SELECT `+bucketExprImage+` AS bucket, COUNT(*) FROM ai_tasks WHERE created_at BETWEEN ? AND ? AND status = 'success' GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	videoBucketExpr := reportBucketExpr("created_at", filters.Period)
	videoTaskMap, err := scanReportMetricMap(db, `SELECT `+videoBucketExpr+` AS bucket, COUNT(*) FROM ai_video_tasks WHERE created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	videoSuccessMap, err := scanReportMetricMap(db, `SELECT `+videoBucketExpr+` AS bucket, COUNT(*) FROM ai_video_tasks WHERE created_at BETWEEN ? AND ? AND status = 'completed' GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	rows := make([]gin.H, 0)
	totalTasks := int64(0)
	totalSuccess := int64(0)
	for _, bucket := range buildReportBuckets(filters) {
		imageCount := imageTaskMap[bucket]
		videoCount := videoTaskMap[bucket]
		successCount := imageSuccessMap[bucket] + videoSuccessMap[bucket]
		taskCount := imageCount + videoCount
		failedCount := taskCount - successCount
		successRate := 0.0
		if taskCount > 0 {
			successRate = float64(successCount) / float64(taskCount)
		}
		rows = append(rows, gin.H{"period": bucket, "task_count": taskCount, "image_tasks": imageCount, "video_tasks": videoCount, "success_tasks": successCount, "failed_tasks": failedCount, "success_rate": successRate})
		totalTasks += taskCount
		totalSuccess += successCount
	}
	totalSuccessRate := 0.0
	if totalTasks > 0 {
		totalSuccessRate = float64(totalSuccess) / float64(totalTasks)
	}
	return []reportCenterColumn{{Key: "period", Label: "周期"}, {Key: "task_count", Label: "总任务数"}, {Key: "image_tasks", Label: "图片任务"}, {Key: "video_tasks", Label: "视频任务"}, {Key: "success_tasks", Label: "成功任务"}, {Key: "failed_tasks", Label: "失败任务"}, {Key: "success_rate", Label: "成功率"}}, rows, gin.H{"task_count": totalTasks, "success_tasks": totalSuccess, "success_rate": totalSuccessRate}, nil
}

func buildTemplateConversionReport(db *sql.DB, filters reportCenterFilters) ([]reportCenterColumn, []gin.H, gin.H, error) {
	templateBucketExpr := reportBucketExpr("created_at", filters.Period)
	newTemplateMap, err := scanReportMetricMap(db, `SELECT `+templateBucketExpr+` AS bucket, COUNT(*) FROM templates WHERE created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	downloadMap, err := scanReportMetricMap(db, `SELECT `+templateBucketExpr+` AS bucket, COALESCE(SUM(download_count), 0) FROM templates WHERE created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	unlockBucketExpr := reportBucketExpr("created_at", filters.Period)
	unlockMap, err := scanReportMetricMap(db, `SELECT `+unlockBucketExpr+` AS bucket, COUNT(*) FROM template_unlocks WHERE created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	likeMap, err := scanReportMetricMap(db, `SELECT `+unlockBucketExpr+` AS bucket, COUNT(*) FROM template_likes WHERE created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	commentMap, err := scanReportMetricMap(db, `SELECT `+unlockBucketExpr+` AS bucket, COUNT(*) FROM template_comments WHERE created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	shareMap, err := scanReportMetricMap(db, `SELECT `+unlockBucketExpr+` AS bucket, COUNT(*) FROM template_shares WHERE created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	rows := make([]gin.H, 0)
	totalDownloads := int64(0)
	totalUnlocks := int64(0)
	totalInteractions := int64(0)
	for _, bucket := range buildReportBuckets(filters) {
		downloads := downloadMap[bucket]
		unlocks := unlockMap[bucket]
		interactions := likeMap[bucket] + commentMap[bucket] + shareMap[bucket]
		conversionRate := 0.0
		if downloads > 0 {
			conversionRate = float64(unlocks) / float64(downloads)
		}
		rows = append(rows, gin.H{"period": bucket, "new_templates": newTemplateMap[bucket], "downloads": downloads, "unlocks": unlocks, "interactions": interactions, "template_conversion_rate": conversionRate})
		totalDownloads += downloads
		totalUnlocks += unlocks
		totalInteractions += interactions
	}
	totalConversionRate := 0.0
	if totalDownloads > 0 {
		totalConversionRate = float64(totalUnlocks) / float64(totalDownloads)
	}
	return []reportCenterColumn{{Key: "period", Label: "周期"}, {Key: "new_templates", Label: "新增模板"}, {Key: "downloads", Label: "下载量"}, {Key: "unlocks", Label: "解锁量"}, {Key: "interactions", Label: "互动量"}, {Key: "template_conversion_rate", Label: "模板转化率"}}, rows, gin.H{"downloads": totalDownloads, "unlocks": totalUnlocks, "interactions": totalInteractions, "template_conversion_rate": totalConversionRate}, nil
}

func buildDesignerHealthReport(db *sql.DB, filters reportCenterFilters) ([]reportCenterColumn, []gin.H, gin.H, error) {
	bucketExpr := reportBucketExpr("created_at", filters.Period)
	certifiedMap, err := scanReportMetricMap(db, `SELECT `+bucketExpr+` AS bucket, COUNT(*) FROM certification_applications WHERE status = 'approved' AND created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	publicDesignerMap, err := scanReportMetricMap(db, `SELECT `+bucketExpr+` AS bucket, COUNT(*) FROM user_profiles WHERE COALESCE(designer_visible, 1) = 1 AND created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	publishedWorkMap, err := scanReportMetricMap(db, `SELECT `+bucketExpr+` AS bucket, COUNT(*) FROM templates WHERE status = 'published' AND creator_user_id IS NOT NULL AND creator_user_id > 0 AND created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	activeDesignerRows, err := db.Query(`SELECT `+bucketExpr+` AS bucket, COUNT(DISTINCT creator_user_id) FROM templates WHERE creator_user_id IS NOT NULL AND creator_user_id > 0 AND created_at BETWEEN ? AND ? GROUP BY bucket ORDER BY bucket`, filters.StartDate, filters.EndDate)
	if err != nil {
		return nil, nil, nil, err
	}
	defer activeDesignerRows.Close()
	activeDesignerMap := make(map[string]int64)
	for activeDesignerRows.Next() {
		var bucket string
		var value int64
		if err := activeDesignerRows.Scan(&bucket, &value); err != nil {
			return nil, nil, nil, err
		}
		activeDesignerMap[bucket] = value
	}
	if err := activeDesignerRows.Err(); err != nil {
		return nil, nil, nil, err
	}
	rows := make([]gin.H, 0)
	totalPublishedWorks := int64(0)
	totalActiveDesigners := int64(0)
	for _, bucket := range buildReportBuckets(filters) {
		publishedWorks := publishedWorkMap[bucket]
		activeDesigners := activeDesignerMap[bucket]
		rows = append(rows, gin.H{"period": bucket, "new_certified_designers": certifiedMap[bucket], "public_designers": publicDesignerMap[bucket], "published_works": publishedWorks, "active_designers": activeDesigners})
		totalPublishedWorks += publishedWorks
		totalActiveDesigners += activeDesigners
	}
	return []reportCenterColumn{{Key: "period", Label: "周期"}, {Key: "new_certified_designers", Label: "新增认证设计师"}, {Key: "public_designers", Label: "公开主页数"}, {Key: "published_works", Label: "发布作品数"}, {Key: "active_designers", Label: "活跃设计师数"}}, rows, gin.H{"published_works": totalPublishedWorks, "active_designers": totalActiveDesigners}, nil
}

func queryReportCenterData(db *sql.DB, filters reportCenterFilters) ([]reportCenterColumn, []gin.H, gin.H, error) {
	switch filters.ReportType {
	case "revenue_conversion":
		return buildRevenueReport(db, filters)
	case "ai_success_rate":
		return buildAITaskReport(db, filters)
	case "template_conversion":
		return buildTemplateConversionReport(db, filters)
	case "designer_health":
		return buildDesignerHealthReport(db, filters)
	default:
		return buildUserGrowthReport(db, filters)
	}
}

func exportReportCenterCSV(columns []reportCenterColumn, rows []gin.H) ([]byte, error) {
	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)
	headers := make([]string, 0, len(columns))
	for _, column := range columns {
		headers = append(headers, column.Label)
	}
	if err := writer.Write(headers); err != nil {
		return nil, err
	}
	for _, row := range rows {
		record := make([]string, 0, len(columns))
		for _, column := range columns {
			record = append(record, formatReportValue(row[column.Key]))
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func formatReportValue(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', 4, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', 4, 64)
	default:
		return fmt.Sprint(typed)
	}
}

func RegisterReportCenterManagementRoutes(r *gin.RouterGroup, userDBModel *model.UserModel) {
	reports := r.Group("/report-center")

	reports.GET("/overview", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "报表中心服务不可用"})
			return
		}
		data, err := queryReportCenterOverview(userDBModel.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取报表概览失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": data})
	})

	reports.GET("/reports", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "报表中心服务不可用"})
			return
		}
		filters, err := parseReportCenterFilters(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "时间参数错误: " + err.Error()})
			return
		}
		columns, rows, summary, err := queryReportCenterData(userDBModel.DB, filters)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取报表数据失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"report_type": filters.ReportType, "period": filters.Period, "start_date": filters.StartDate.Format("2006-01-02"), "end_date": filters.EndDate.Format("2006-01-02"), "columns": columns, "rows": rows, "summary": summary}})
	})

	reports.GET("/export", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "报表中心服务不可用"})
			return
		}
		filters, err := parseReportCenterFilters(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "时间参数错误: " + err.Error()})
			return
		}
		columns, rows, _, err := queryReportCenterData(userDBModel.DB, filters)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "导出报表失败: " + err.Error()})
			return
		}
		payload, err := exportReportCenterCSV(columns, rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "生成CSV失败: " + err.Error()})
			return
		}
		filename := fmt.Sprintf("%s_%s_%s.csv", filters.ReportType, filters.Period, time.Now().Format("20060102150405"))
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
		c.Writer.Write([]byte("\xEF\xBB\xBF"))
		_, _ = c.Writer.Write(payload)
	})
}
