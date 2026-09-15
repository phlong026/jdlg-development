package route

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"service/model"
)

type designerManagementFilters struct {
	Keyword             string
	CertificationStatus string
	Visible             string
	Specialty           string
}

const designerManagementJoins = `
LEFT JOIN user_profiles p ON p.user_id = u.id
LEFT JOIN certification_applications ca_latest ON ca_latest.id = (
	SELECT ca2.id
	FROM certification_applications ca2
	WHERE ca2.user_id = u.id
	ORDER BY CASE ca2.status
		WHEN 'approved' THEN 0
		WHEN 'pending_review' THEN 1
		WHEN 'pending_payment' THEN 2
		ELSE 3
	END,
	ca2.id DESC
	LIMIT 1
)
`

func buildDesignerManagementWhere(filters designerManagementFilters) (string, []interface{}) {
	where := `u.user_type = 'miniprogram' AND (
		u.can_withdraw = 1
		OR COALESCE(p.service_title, '') <> ''
		OR COALESCE(p.designer_bio, '') <> ''
		OR COALESCE(p.specialty_styles, '') <> ''
		OR ca_latest.id IS NOT NULL
		OR EXISTS (SELECT 1 FROM templates t WHERE t.creator_user_id = u.id)
	)`
	args := make([]interface{}, 0)

	keyword := strings.TrimSpace(filters.Keyword)
	if keyword != "" {
		likeKeyword := "%" + keyword + "%"
		where += ` AND (
			u.username LIKE ?
			OR COALESCE(p.nickname, '') LIKE ?
			OR COALESCE(p.service_title, '') LIKE ?
			OR COALESCE(p.specialty_styles, '') LIKE ?
		)`
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}

	certificationStatus := strings.TrimSpace(filters.CertificationStatus)
	if certificationStatus != "" && certificationStatus != "all" {
		if certificationStatus == "none" {
			where += ` AND ca_latest.id IS NULL`
		} else {
			where += ` AND COALESCE(ca_latest.status, '') = ?`
			args = append(args, certificationStatus)
		}
	}

	visible := strings.TrimSpace(filters.Visible)
	if visible == "public" {
		where += ` AND COALESCE(p.designer_visible, 1) = 1`
	} else if visible == "hidden" {
		where += ` AND COALESCE(p.designer_visible, 1) = 0`
	}

	specialty := strings.TrimSpace(filters.Specialty)
	if specialty != "" {
		where += ` AND COALESCE(p.specialty_styles, '') LIKE ?`
		args = append(args, "%"+specialty+"%")
	}

	return where, args
}

func queryDesignerManagementCount(db *sql.DB, filters designerManagementFilters) (int64, error) {
	where, args := buildDesignerManagementWhere(filters)
	query := `SELECT COUNT(*) FROM users u ` + designerManagementJoins + ` WHERE ` + where
	var total int64
	err := db.QueryRow(query, args...).Scan(&total)
	return total, err
}

func queryDesignerManagementWithWorksCount(db *sql.DB) (int64, error) {
	query := `
		SELECT COUNT(DISTINCT u.id)
		FROM users u
		WHERE u.user_type = 'miniprogram'
		  AND EXISTS (SELECT 1 FROM templates t WHERE t.creator_user_id = u.id)
	`
	var total int64
	err := db.QueryRow(query).Scan(&total)
	return total, err
}

func queryDesignerManagementUserIDs(db *sql.DB, filters designerManagementFilters, limit, offset int) ([]int64, int64, error) {
	where, args := buildDesignerManagementWhere(filters)
	countQuery := `SELECT COUNT(*) FROM users u ` + designerManagementJoins + ` WHERE ` + where
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listQuery := `SELECT u.id FROM users u ` + designerManagementJoins + ` WHERE ` + where + ` ORDER BY COALESCE(u.updated_at, u.created_at) DESC, u.id DESC LIMIT ? OFFSET ?`
	listArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := db.Query(listQuery, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	ids := make([]int64, 0, limit)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, 0, err
		}
		ids = append(ids, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return ids, total, nil
}

func pickDesignerRepresentativeWork(templateModel *model.TemplateModel, userID int64) gin.H {
	if templateModel == nil || userID <= 0 {
		return gin.H{}
	}
	publishedWorks, _ := templateModel.ListPublishedByCreatorUserID(userID, "", 1, 0)
	if len(publishedWorks) > 0 && publishedWorks[0] != nil {
		item := publishedWorks[0]
		thumbnail := strings.TrimSpace(item.PreviewURL)
		if thumbnail == "" {
			thumbnail = strings.TrimSpace(item.Thumbnail)
		}
		return gin.H{
			"id":            item.ID,
			"title":         item.Name,
			"thumbnail":     thumbnail,
			"status":        item.Status,
			"publish_scope": item.PublishScope,
		}
	}
	works, _ := templateModel.ListByCreatorUserID(userID, "", 1, 0)
	if len(works) > 0 && works[0] != nil {
		item := works[0]
		thumbnail := strings.TrimSpace(item.PreviewURL)
		if thumbnail == "" {
			thumbnail = strings.TrimSpace(item.Thumbnail)
		}
		return gin.H{
			"id":            item.ID,
			"title":         item.Name,
			"thumbnail":     thumbnail,
			"status":        item.Status,
			"publish_scope": item.PublishScope,
		}
	}
	return gin.H{}
}

func RegisterDesignerManagementRoutes(r *gin.RouterGroup, userDBModel *model.UserModel, templateModel *model.TemplateModel, userProfileModel *model.UserProfileModel, certificationModel *model.CertificationApplicationModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel, designerReviewModel *model.DesignerReviewModel, designerFollowModel *model.DesignerFollowModel) {
	designers := r.Group("/designers")

	designers.GET("", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "设计师中心服务不可用"})
			return
		}

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
		offset := (page - 1) * pageSize

		filters := designerManagementFilters{
			Keyword:             c.Query("keyword"),
			CertificationStatus: c.DefaultQuery("certification_status", "all"),
			Visible:             c.DefaultQuery("visible", "all"),
			Specialty:           c.Query("specialty"),
		}
		ids, total, err := queryDesignerManagementUserIDs(userDBModel.DB, filters, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取设计师列表失败: " + err.Error()})
			return
		}

		summaryTotal, err := queryDesignerManagementCount(userDBModel.DB, designerManagementFilters{})
		if err != nil {
			summaryTotal = 0
		}
		publicCount, err := queryDesignerManagementCount(userDBModel.DB, designerManagementFilters{Visible: "public"})
		if err != nil {
			publicCount = 0
		}
		approvedCount, err := queryDesignerManagementCount(userDBModel.DB, designerManagementFilters{CertificationStatus: model.CertificationStatusApproved})
		if err != nil {
			approvedCount = 0
		}
		withWorksCount, err := queryDesignerManagementWithWorksCount(userDBModel.DB)
		if err != nil {
			withWorksCount = 0
		}

		list := make([]gin.H, 0, len(ids))
		for _, userID := range ids {
			user, err := userDBModel.GetByID(userID)
			if err != nil || user == nil {
				continue
			}
			payload, err := buildDesignerCenterPayload(userID, userDBModel, templateModel, userProfileModel, certificationModel, stoneRecordModel, userOrderModel, designerReviewModel)
			if err != nil {
				continue
			}
			profile, _ := payload["profile"].(gin.H)
			stats, _ := payload["stats"].(gin.H)
			reviewSummary, _ := payload["review_summary"].(gin.H)
			followCount := int64(0)
			if designerFollowModel != nil {
				followCount, _ = designerFollowModel.CountFollowers(userID)
			}
			reviewCount := int64(0)
			if positiveCount, ok := reviewSummary["positive_count"].(int64); ok {
				reviewCount += positiveCount
			}
			if negativeCount, ok := reviewSummary["negative_count"].(int64); ok {
				reviewCount += negativeCount
			}
			var latestCert *model.CertificationApplication
			if certificationModel != nil {
				latestCert, _ = certificationModel.GetLatestByUser(userID)
			}
			currentCanWithdraw := latestCert != nil && strings.TrimSpace(latestCert.Status) == model.CertificationStatusApproved
			if user.CanWithdraw != currentCanWithdraw {
				_ = userDBModel.UpdateCanWithdraw(user.ID, currentCanWithdraw)
				user.CanWithdraw = currentCanWithdraw
			}
			representativeWork := pickDesignerRepresentativeWork(templateModel, userID)
			totalWorks := int64(0)
			if stats != nil {
				if value, ok := stats["total_works"].(int64); ok {
					totalWorks = value
				}
			}
			list = append(list, gin.H{
				"user_id":               user.ID,
				"username":              user.Username,
				"display_name":          profile["name"],
				"avatar":                profile["avatar"],
				"service_title":         profile["title"],
				"service_enabled":       profile["service_enabled"],
				"specialty_styles":      profile["specialties_text"],
				"certification_status":  profile["cert_status"],
				"certification_type":    func() string { if latestCert != nil { return latestCert.Type }; return "" }(),
				"designer_visible":      profile["designer_visible"],
				"can_withdraw":          user.CanWithdraw,
				"total_works":           totalWorks,
				"representative_work":   representativeWork,
				"follow_count":          followCount,
				"review_count":          reviewCount,
				"positive_review_count": reviewSummary["positive_count"],
				"negative_review_count": reviewSummary["negative_count"],
				"recent_active_at":      user.UpdatedAt,
				"created_at":            user.CreatedAt,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"list":      list,
				"total":     total,
				"page":      page,
				"page_size": pageSize,
				"summary": gin.H{
					"total_designers":   summaryTotal,
					"public_designers":  publicCount,
					"approved_designers": approvedCount,
					"designers_with_works": withWorksCount,
				},
			},
		})
	})

	designers.GET("/:id", func(c *gin.Context) {
		if userDBModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "设计师中心服务不可用"})
			return
		}
		userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || userID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的设计师ID"})
			return
		}
		user, err := userDBModel.GetByID(userID)
		if err != nil || user == nil || user.UserType != "miniprogram" {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "设计师不存在"})
			return
		}

		var profile *model.UserProfile
		if userProfileModel != nil {
			profile, _ = userProfileModel.GetByUserID(userID)
		}
		var latestCert *model.CertificationApplication
		if certificationModel != nil {
			latestCert, _ = certificationModel.GetLatestByUser(userID)
		}
		currentCanWithdraw := latestCert != nil && strings.TrimSpace(latestCert.Status) == model.CertificationStatusApproved
		if user.CanWithdraw != currentCanWithdraw {
			_ = userDBModel.UpdateCanWithdraw(user.ID, currentCanWithdraw)
			user.CanWithdraw = currentCanWithdraw
		}
		publicPreview, _ := buildDesignerCenterPayload(userID, userDBModel, templateModel, userProfileModel, certificationModel, stoneRecordModel, userOrderModel, designerReviewModel)
		totalWorks := int64(0)
		publishedWorks := int64(0)
		if templateModel != nil {
			totalWorks, _ = templateModel.CountByCreatorUserID(userID, "")
			publishedWorks, _ = templateModel.CountPublishedByCreatorUserID(userID, "")
		}
		totalOrders, monthOrders := int64(0), int64(0)
		if userOrderModel != nil {
			totalOrders, monthOrders, _ = userOrderModel.SummaryByDesignerUserID(userID)
		}
		totalEarnings, monthEarnings := int64(0), int64(0)
		if stoneRecordModel != nil {
			totalEarnings, monthEarnings, _ = stoneRecordModel.TemplateEarningsSummary(userID)
		}
		positiveCount, negativeCount := int64(0), int64(0)
		reviews := make([]*model.DesignerReview, 0)
		if designerReviewModel != nil {
			reviews, _, _ = designerReviewModel.ListByDesignerUserID(userID, 20, 0)
			positiveCount, negativeCount, _ = designerReviewModel.SummaryByDesignerUserID(userID)
		}
		followCount := int64(0)
		if designerFollowModel != nil {
			followCount, _ = designerFollowModel.CountFollowers(userID)
		}
		works := make([]*model.Template, 0)
		if templateModel != nil {
			works, _ = templateModel.ListByCreatorUserID(userID, "", 20, 0)
		}

		workList := make([]gin.H, 0, len(works))
		for _, item := range works {
			if item == nil {
				continue
			}
			thumbnail := strings.TrimSpace(item.PreviewURL)
			if thumbnail == "" {
				thumbnail = strings.TrimSpace(item.Thumbnail)
			}
			workList = append(workList, gin.H{
				"id":            item.ID,
				"name":          item.Name,
				"description":   item.Description,
				"thumbnail":     thumbnail,
				"price":         item.Price,
				"is_free":       item.IsFree,
				"status":        item.Status,
				"publish_scope": item.PublishScope,
				"source_type":   item.SourceType,
				"like_count":    item.LikeCount,
				"download_count": item.DownloadCount,
				"main_tab":      item.MainTab,
				"sub_tab":       item.SubTab,
				"reject_reason": item.RejectReason,
				"created_at":    item.CreatedAt,
				"updated_at":    item.UpdatedAt,
			})
		}

		reviewList := make([]gin.H, 0, len(reviews))
		for _, item := range reviews {
			if item == nil {
				continue
			}
			reviewList = append(reviewList, gin.H{
				"id":            item.ID,
				"reviewer_user_id": item.ReviewerUserID,
				"reviewer_name": item.ReviewerName,
				"reviewer_avatar": item.ReviewerAvatar,
				"rating":        item.Rating,
				"content":       item.Content,
				"sentiment":     item.Sentiment,
				"order_id":      item.OrderID,
				"order_no":      item.OrderNo,
				"created_at":    item.CreatedAt,
			})
		}

		certification := gin.H(nil)
		if latestCert != nil {
			certification = gin.H{
				"id":               latestCert.ID,
				"type":             latestCert.Type,
				"status":           latestCert.Status,
				"identity_type":    latestCert.IdentityType,
				"real_name":        latestCert.RealName,
				"company_name":     latestCert.CompanyName,
				"credit_code":      latestCert.CreditCode,
				"extra_docs_remark": latestCert.ExtraDocsRemark,
				"admin_remark":     latestCert.AdminRemark,
				"created_at":       latestCert.CreatedAt,
				"reviewed_at":      latestCert.ReviewedAt,
			}
		}

		profileData := gin.H{
			"nickname":                   "",
			"avatar":                     "",
			"designer_bio":               "",
			"specialty_styles":           "",
			"designer_experience_years":  int64(0),
			"service_title":              "",
			"service_quote":              int64(0),
			"service_intro":              "",
			"service_enabled":            false,
			"designer_visible":           true,
			"enterprise_wechat_verified": false,
			"enterprise_wechat_contact":  "",
		}
		if profile != nil {
			profileData = gin.H{
				"nickname":                   profile.Nickname,
				"avatar":                     profile.Avatar,
				"designer_bio":               profile.DesignerBio,
				"specialty_styles":           profile.SpecialtyStyles,
				"designer_experience_years":  profile.DesignerExperienceYears,
				"service_title":              profile.ServiceTitle,
				"service_quote":              profile.ServiceQuote,
				"service_intro":              profile.ServiceIntro,
				"service_enabled":            profile.ServiceEnabled,
				"designer_visible":           profile.DesignerVisible,
				"enterprise_wechat_verified": profile.EnterpriseWechatVerified,
				"enterprise_wechat_contact":  profile.EnterpriseWechatContact,
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"user_id":     user.ID,
				"username":    user.Username,
				"can_withdraw": user.CanWithdraw,
				"created_at":  user.CreatedAt,
				"updated_at":  user.UpdatedAt,
				"profile":     profileData,
				"certification": certification,
				"stats": gin.H{
					"total_works":            totalWorks,
					"published_works":        publishedWorks,
					"total_orders":           totalOrders,
					"month_orders":           monthOrders,
					"total_earnings":         totalEarnings,
					"month_earnings":         monthEarnings,
					"follow_count":           followCount,
					"positive_review_count":  positiveCount,
					"negative_review_count":  negativeCount,
					"review_count":           positiveCount + negativeCount,
				},
				"works":         workList,
				"reviews":       reviewList,
				"public_preview": publicPreview,
			},
		})
	})

	designers.PATCH("/:id/visibility", func(c *gin.Context) {
		if userProfileModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "设计师资料服务不可用"})
			return
		}
		userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || userID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的设计师ID"})
			return
		}
		var req struct {
			DesignerVisible bool `json:"designer_visible"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		if _, err := userProfileModel.GetOrCreate(userID, ""); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取设计师资料失败: " + err.Error()})
			return
		}
		if err := userProfileModel.SetDesignerVisible(userID, req.DesignerVisible); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新展示状态失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
	})

	designers.PATCH("/:id/service-status", func(c *gin.Context) {
		if userProfileModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "设计师资料服务不可用"})
			return
		}
		userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || userID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的设计师ID"})
			return
		}
		var req struct {
			ServiceEnabled bool `json:"service_enabled"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
			return
		}
		if _, err := userProfileModel.GetOrCreate(userID, ""); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取设计师资料失败: " + err.Error()})
			return
		}
		if err := userProfileModel.SetServiceEnabled(userID, req.ServiceEnabled); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新接单状态失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
	})
}
