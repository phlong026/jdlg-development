package route

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"service/model"
)

const templateCommentAggregateSQL = `
	SELECT template_id, COUNT(*) AS comment_count
	FROM template_comments
	GROUP BY template_id
`

const templateShareAggregateSQL = `
	SELECT template_id, COUNT(*) AS share_count
	FROM template_shares
	GROUP BY template_id
`

const templateUnlockAggregateSQL = `
	SELECT template_id, COUNT(*) AS unlock_count
	FROM template_unlocks
	GROUP BY template_id
`

type contentAnalyticsFilters struct {
	Keyword string
}

func buildContentAnalyticsTemplateWhere(keyword string) (string, []interface{}) {
	where := "t.status = 'published'"
	args := make([]interface{}, 0)
	trimmed := strings.TrimSpace(keyword)
	if trimmed == "" {
		return where, args
	}
	likeKeyword := "%" + trimmed + "%"
	where += ` AND (
		t.name LIKE ?
		OR COALESCE(t.creator, '') LIKE ?
		OR COALESCE(t.category, '') LIKE ?
		OR COALESCE(t.source_type, '') LIKE ?
	)`
	args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	return where, args
}

func buildContentAnalyticsFeaturedWhere(keyword string) (string, []interface{}) {
	where := "1=1"
	args := make([]interface{}, 0)
	trimmed := strings.TrimSpace(keyword)
	if trimmed == "" {
		return where, args
	}
	likeKeyword := "%" + trimmed + "%"
	where += ` AND (
		g.name LIKE ?
		OR COALESCE(t1.name, '') LIKE ?
		OR COALESCE(t2.name, '') LIKE ?
	)`
	args = append(args, likeKeyword, likeKeyword, likeKeyword)
	return where, args
}

func parseManagementPageParams(c *gin.Context) (int, int, int) {
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

func queryContentAnalyticsOverview(db *sql.DB) (gin.H, error) {
	overview := gin.H{}
	var totalTemplates int64
	var publishedTemplates int64
	var totalDownloads int64
	var totalLikes int64
	var weekNewTemplates int64
	var totalUnlocks int64
	var totalComments int64
	var totalShares int64
	var featuredCaseGroupCount int64

	if err := db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN status = 'published' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(download_count), 0),
		       COALESCE(SUM(like_count), 0),
		       COALESCE(SUM(CASE WHEN created_at >= DATE_SUB(NOW(), INTERVAL 7 DAY) THEN 1 ELSE 0 END), 0)
		FROM templates
	`).Scan(&totalTemplates, &publishedTemplates, &totalDownloads, &totalLikes, &weekNewTemplates); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM template_unlocks`).Scan(&totalUnlocks); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM template_comments`).Scan(&totalComments); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM template_shares`).Scan(&totalShares); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM featured_case_groups`).Scan(&featuredCaseGroupCount); err != nil {
		return nil, err
	}

	overview["total_templates"] = totalTemplates
	overview["published_templates"] = publishedTemplates
	overview["total_downloads"] = totalDownloads
	overview["total_unlocks"] = totalUnlocks
	overview["total_interactions"] = totalLikes + totalComments + totalShares
	overview["week_new_templates"] = weekNewTemplates
	overview["featured_case_group_count"] = featuredCaseGroupCount
	return overview, nil
}

func queryContentAnalyticsDownloadRanking(db *sql.DB, filters contentAnalyticsFilters, limit, offset int) ([]gin.H, int64, error) {
	whereSQL, args := buildContentAnalyticsTemplateWhere(filters.Keyword)
	countQuery := `SELECT COUNT(*) FROM templates t WHERE ` + whereSQL
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listQuery := `
		SELECT
			t.id,
			t.name,
			t.category,
			COALESCE(t.creator, '') AS creator,
			COALESCE(t.source_type, '') AS source_type,
			t.is_free,
			t.price,
			t.download_count,
			t.like_count,
			COALESCE(tc.comment_count, 0) AS comment_count,
			COALESCE(ts.share_count, 0) AS share_count,
			COALESCE(tu.unlock_count, 0) AS unlock_count,
			(t.like_count + COALESCE(tc.comment_count, 0) + COALESCE(ts.share_count, 0)) AS engagement_score,
			t.created_at
		FROM templates t
		LEFT JOIN (` + templateCommentAggregateSQL + `) tc ON tc.template_id = t.id
		LEFT JOIN (` + templateShareAggregateSQL + `) ts ON ts.template_id = t.id
		LEFT JOIN (` + templateUnlockAggregateSQL + `) tu ON tu.template_id = t.id
		WHERE ` + whereSQL + `
		ORDER BY t.download_count DESC, tu.unlock_count DESC, engagement_score DESC, t.created_at DESC
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]interface{}{}, args...), limit, offset)
	list, _, err := scanContentAnalyticsTemplateRows(db, listQuery, listArgs, false)
	return list, total, err
}

func queryContentAnalyticsEngagementRanking(db *sql.DB, filters contentAnalyticsFilters, limit, offset int) ([]gin.H, int64, error) {
	whereSQL, args := buildContentAnalyticsTemplateWhere(filters.Keyword)
	countQuery := `SELECT COUNT(*) FROM templates t WHERE ` + whereSQL
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listQuery := `
		SELECT
			t.id,
			t.name,
			t.category,
			COALESCE(t.creator, '') AS creator,
			COALESCE(t.source_type, '') AS source_type,
			t.is_free,
			t.price,
			t.download_count,
			t.like_count,
			COALESCE(tc.comment_count, 0) AS comment_count,
			COALESCE(ts.share_count, 0) AS share_count,
			COALESCE(tu.unlock_count, 0) AS unlock_count,
			(t.like_count + COALESCE(tc.comment_count, 0) + COALESCE(ts.share_count, 0)) AS engagement_score,
			t.created_at
		FROM templates t
		LEFT JOIN (` + templateCommentAggregateSQL + `) tc ON tc.template_id = t.id
		LEFT JOIN (` + templateShareAggregateSQL + `) ts ON ts.template_id = t.id
		LEFT JOIN (` + templateUnlockAggregateSQL + `) tu ON tu.template_id = t.id
		WHERE ` + whereSQL + `
		ORDER BY engagement_score DESC, t.download_count DESC, tu.unlock_count DESC, t.created_at DESC
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]interface{}{}, args...), limit, offset)
	list, _, err := scanContentAnalyticsTemplateRows(db, listQuery, listArgs, false)
	return list, total, err
}

func queryContentAnalyticsNewTemplates(db *sql.DB, filters contentAnalyticsFilters, limit, offset int) ([]gin.H, int64, error) {
	whereSQL, args := buildContentAnalyticsTemplateWhere(filters.Keyword)
	whereSQL += ` AND t.created_at >= DATE_SUB(NOW(), INTERVAL 14 DAY)`
	countQuery := `SELECT COUNT(*) FROM templates t WHERE ` + whereSQL
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listQuery := `
		SELECT
			t.id,
			t.name,
			t.category,
			COALESCE(t.creator, '') AS creator,
			COALESCE(t.source_type, '') AS source_type,
			t.is_free,
			t.price,
			t.download_count,
			t.like_count,
			COALESCE(tc.comment_count, 0) AS comment_count,
			COALESCE(ts.share_count, 0) AS share_count,
			COALESCE(tu.unlock_count, 0) AS unlock_count,
			(t.like_count + COALESCE(tc.comment_count, 0) + COALESCE(ts.share_count, 0)) AS engagement_score,
			t.created_at
		FROM templates t
		LEFT JOIN (` + templateCommentAggregateSQL + `) tc ON tc.template_id = t.id
		LEFT JOIN (` + templateShareAggregateSQL + `) ts ON ts.template_id = t.id
		LEFT JOIN (` + templateUnlockAggregateSQL + `) tu ON tu.template_id = t.id
		WHERE ` + whereSQL + `
		ORDER BY t.created_at DESC, engagement_score DESC, t.download_count DESC
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]interface{}{}, args...), limit, offset)
	list, _, err := scanContentAnalyticsTemplateRows(db, listQuery, listArgs, false)
	return list, total, err
}

func queryContentAnalyticsLowConversion(db *sql.DB, filters contentAnalyticsFilters, limit, offset int) ([]gin.H, int64, error) {
	whereSQL, args := buildContentAnalyticsTemplateWhere(filters.Keyword)
	lowConversionCondition := `
		AND t.created_at < DATE_SUB(NOW(), INTERVAL 7 DAY)
		AND (
			((t.is_free = 0 AND t.price > 0) AND (t.like_count + COALESCE(tc.comment_count, 0) + COALESCE(ts.share_count, 0)) >= 3 AND COALESCE(tu.unlock_count, 0) = 0)
			OR
			(((t.is_free = 1 OR t.price = 0) AND (t.like_count + COALESCE(tc.comment_count, 0) + COALESCE(ts.share_count, 0)) >= 3 AND t.download_count <= 1))
		)
	`
	countQuery := `
		SELECT COUNT(*)
		FROM templates t
		LEFT JOIN (` + templateCommentAggregateSQL + `) tc ON tc.template_id = t.id
		LEFT JOIN (` + templateShareAggregateSQL + `) ts ON ts.template_id = t.id
		LEFT JOIN (` + templateUnlockAggregateSQL + `) tu ON tu.template_id = t.id
		WHERE ` + whereSQL + ` ` + lowConversionCondition
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listQuery := `
		SELECT
			t.id,
			t.name,
			t.category,
			COALESCE(t.creator, '') AS creator,
			COALESCE(t.source_type, '') AS source_type,
			t.is_free,
			t.price,
			t.download_count,
			t.like_count,
			COALESCE(tc.comment_count, 0) AS comment_count,
			COALESCE(ts.share_count, 0) AS share_count,
			COALESCE(tu.unlock_count, 0) AS unlock_count,
			(t.like_count + COALESCE(tc.comment_count, 0) + COALESCE(ts.share_count, 0)) AS engagement_score,
			t.created_at,
			CASE WHEN t.is_free = 0 AND t.price > 0 THEN 'unlock' ELSE 'download' END AS conversion_type,
			CASE WHEN t.is_free = 0 AND t.price > 0 THEN COALESCE(tu.unlock_count, 0) ELSE t.download_count END AS conversion_count
		FROM templates t
		LEFT JOIN (` + templateCommentAggregateSQL + `) tc ON tc.template_id = t.id
		LEFT JOIN (` + templateShareAggregateSQL + `) ts ON ts.template_id = t.id
		LEFT JOIN (` + templateUnlockAggregateSQL + `) tu ON tu.template_id = t.id
		WHERE ` + whereSQL + ` ` + lowConversionCondition + `
		ORDER BY engagement_score DESC, conversion_count ASC, t.download_count ASC, t.created_at DESC
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]interface{}{}, args...), limit, offset)
	return scanContentAnalyticsTemplateRows(db, listQuery, listArgs, true)
}

func scanContentAnalyticsTemplateRows(db *sql.DB, query string, args []interface{}, includeConversion bool) ([]gin.H, int64, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]gin.H, 0)
	for rows.Next() {
		var id int64
		var name string
		var category string
		var creator string
		var sourceType string
		var isFreeInt int
		var price int64
		var downloadCount int64
		var likeCount int64
		var commentCount int64
		var shareCount int64
		var unlockCount int64
		var engagementScore int64
		var createdAt time.Time
		if includeConversion {
			var conversionType string
			var conversionCount int64
			if err := rows.Scan(&id, &name, &category, &creator, &sourceType, &isFreeInt, &price, &downloadCount, &likeCount, &commentCount, &shareCount, &unlockCount, &engagementScore, &createdAt, &conversionType, &conversionCount); err != nil {
				return nil, 0, err
			}
			conversionRate := 0.0
			if engagementScore > 0 {
				conversionRate = float64(conversionCount) / float64(engagementScore)
			}
			list = append(list, gin.H{
				"id":               id,
				"name":             name,
				"category":         category,
				"creator":          creator,
				"source_type":      sourceType,
				"is_free":          isFreeInt == 1,
				"price":            price,
				"download_count":   downloadCount,
				"like_count":       likeCount,
				"comment_count":    commentCount,
				"share_count":      shareCount,
				"unlock_count":     unlockCount,
				"engagement_score": engagementScore,
				"created_at":       createdAt.Format(time.RFC3339),
				"conversion_type":  conversionType,
				"conversion_count": conversionCount,
				"conversion_rate":  conversionRate,
			})
			continue
		}
		if err := rows.Scan(&id, &name, &category, &creator, &sourceType, &isFreeInt, &price, &downloadCount, &likeCount, &commentCount, &shareCount, &unlockCount, &engagementScore, &createdAt); err != nil {
			return nil, 0, err
		}
		list = append(list, gin.H{
			"id":               id,
			"name":             name,
			"category":         category,
			"creator":          creator,
			"source_type":      sourceType,
			"is_free":          isFreeInt == 1,
			"price":            price,
			"download_count":   downloadCount,
			"like_count":       likeCount,
			"comment_count":    commentCount,
			"share_count":      shareCount,
			"unlock_count":     unlockCount,
			"engagement_score": engagementScore,
			"created_at":       createdAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, int64(len(list)), nil
}

func queryContentAnalyticsFeaturedCases(db *sql.DB, filters contentAnalyticsFilters, limit, offset int) ([]gin.H, int64, error) {
	whereSQL, args := buildContentAnalyticsFeaturedWhere(filters.Keyword)
	countQuery := `
		SELECT COUNT(*)
		FROM featured_case_groups g
		LEFT JOIN templates t1 ON t1.id = g.case1_id
		LEFT JOIN templates t2 ON t2.id = g.case2_id
		WHERE ` + whereSQL
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listQuery := `
		SELECT
			g.id,
			g.name,
			g.display_mode,
			g.case1_id,
			COALESCE(t1.name, '') AS case1_name,
			COALESCE(t1.download_count, 0) AS case1_download_count,
			(COALESCE(t1.like_count, 0) + COALESCE(tc1.comment_count, 0) + COALESCE(ts1.share_count, 0)) AS case1_engagement_score,
			g.case2_id,
			COALESCE(t2.name, '') AS case2_name,
			COALESCE(t2.download_count, 0) AS case2_download_count,
			(COALESCE(t2.like_count, 0) + COALESCE(tc2.comment_count, 0) + COALESCE(ts2.share_count, 0)) AS case2_engagement_score,
			(COALESCE(t1.download_count, 0) + COALESCE(t2.download_count, 0)) AS combined_download_count,
			((COALESCE(t1.like_count, 0) + COALESCE(tc1.comment_count, 0) + COALESCE(ts1.share_count, 0)) + (COALESCE(t2.like_count, 0) + COALESCE(tc2.comment_count, 0) + COALESCE(ts2.share_count, 0))) AS combined_engagement_score,
			g.updated_at
		FROM featured_case_groups g
		LEFT JOIN templates t1 ON t1.id = g.case1_id
		LEFT JOIN templates t2 ON t2.id = g.case2_id
		LEFT JOIN (` + templateCommentAggregateSQL + `) tc1 ON tc1.template_id = t1.id
		LEFT JOIN (` + templateShareAggregateSQL + `) ts1 ON ts1.template_id = t1.id
		LEFT JOIN (` + templateCommentAggregateSQL + `) tc2 ON tc2.template_id = t2.id
		LEFT JOIN (` + templateShareAggregateSQL + `) ts2 ON ts2.template_id = t2.id
		WHERE ` + whereSQL + `
		ORDER BY combined_download_count DESC, combined_engagement_score DESC, g.updated_at DESC
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := db.Query(listQuery, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]gin.H, 0, limit)
	for rows.Next() {
		var id int64
		var name string
		var displayMode string
		var case1ID int64
		var case1Name string
		var case1DownloadCount int64
		var case1EngagementScore int64
		var case2ID int64
		var case2Name string
		var case2DownloadCount int64
		var case2EngagementScore int64
		var combinedDownloadCount int64
		var combinedEngagementScore int64
		var updatedAt time.Time
		if err := rows.Scan(&id, &name, &displayMode, &case1ID, &case1Name, &case1DownloadCount, &case1EngagementScore, &case2ID, &case2Name, &case2DownloadCount, &case2EngagementScore, &combinedDownloadCount, &combinedEngagementScore, &updatedAt); err != nil {
			return nil, 0, err
		}
		item := gin.H{
			"id":                        id,
			"name":                      name,
			"display_mode":              displayMode,
			"combined_download_count":   combinedDownloadCount,
			"combined_engagement_score": combinedEngagementScore,
			"updated_at":                updatedAt.Format(time.RFC3339),
			"case1": gin.H{
				"id":               case1ID,
				"name":             case1Name,
				"download_count":   case1DownloadCount,
				"engagement_score": case1EngagementScore,
			},
		}
		if case2ID > 0 {
			item["case2"] = gin.H{
				"id":               case2ID,
				"name":             case2Name,
				"download_count":   case2DownloadCount,
				"engagement_score": case2EngagementScore,
			}
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func RegisterContentAnalyticsManagementRoutes(r *gin.RouterGroup, templateModel *model.TemplateModel) {
	contentAnalytics := r.Group("/content-analytics")

	contentAnalytics.GET("/overview", func(c *gin.Context) {
		if templateModel == nil || templateModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "内容运营分析服务不可用"})
			return
		}
		overview, err := queryContentAnalyticsOverview(templateModel.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取内容运营概览失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": overview})
	})

	contentAnalytics.GET("/download-ranking", func(c *gin.Context) {
		if templateModel == nil || templateModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "内容运营分析服务不可用"})
			return
		}
		page, pageSize, offset := parseManagementPageParams(c)
		list, total, err := queryContentAnalyticsDownloadRanking(templateModel.DB, contentAnalyticsFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取模板下载排行失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})

	contentAnalytics.GET("/engagement-ranking", func(c *gin.Context) {
		if templateModel == nil || templateModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "内容运营分析服务不可用"})
			return
		}
		page, pageSize, offset := parseManagementPageParams(c)
		list, total, err := queryContentAnalyticsEngagementRanking(templateModel.DB, contentAnalyticsFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取互动排行失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})

	contentAnalytics.GET("/new-templates", func(c *gin.Context) {
		if templateModel == nil || templateModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "内容运营分析服务不可用"})
			return
		}
		page, pageSize, offset := parseManagementPageParams(c)
		list, total, err := queryContentAnalyticsNewTemplates(templateModel.DB, contentAnalyticsFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取新上架模板表现失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})

	contentAnalytics.GET("/low-conversion", func(c *gin.Context) {
		if templateModel == nil || templateModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "内容运营分析服务不可用"})
			return
		}
		page, pageSize, offset := parseManagementPageParams(c)
		list, total, err := queryContentAnalyticsLowConversion(templateModel.DB, contentAnalyticsFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取低转化模板列表失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})

	contentAnalytics.GET("/featured-cases", func(c *gin.Context) {
		if templateModel == nil || templateModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "内容运营分析服务不可用"})
			return
		}
		page, pageSize, offset := parseManagementPageParams(c)
		list, total, err := queryContentAnalyticsFeaturedCases(templateModel.DB, contentAnalyticsFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取精选案例观察失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"list": list, "total": total, "page": page, "page_size": pageSize}})
	})
}
