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

type membershipOperationsFilters struct {
	Keyword         string
	Status          string
	PermissionState string
}

const membershipOperationsBaseJoin = `
LEFT JOIN user_profiles p ON p.user_id = u.id
LEFT JOIN user_memberships um ON um.user_id = u.id
`

const membershipOperationsSuccessfulRechargeExists = `EXISTS (
	SELECT 1 FROM user_orders uo
	WHERE uo.user_id = u.id AND uo.type = 'recharge' AND uo.status = 'success'
)`

func buildMembershipOperationsWhere(filters membershipOperationsFilters) (string, []interface{}) {
	where := `u.user_type = 'miniprogram' AND (um.user_id IS NOT NULL OR ` + membershipOperationsSuccessfulRechargeExists + `)`
	args := make([]interface{}, 0)

	keyword := strings.TrimSpace(filters.Keyword)
	if keyword != "" {
		likeKeyword := "%" + keyword + "%"
		where += ` AND (
			u.username LIKE ?
			OR COALESCE(p.nickname, '') LIKE ?
			OR COALESCE(um.plan_title, '') LIKE ?
			OR COALESCE(um.source_order_no, '') LIKE ?
		)`
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}

	switch strings.TrimSpace(filters.Status) {
	case "active":
		where += ` AND ((um.user_id IS NOT NULL AND um.status = 'active' AND um.expired_at > NOW()) OR (um.user_id IS NULL AND ` + membershipOperationsSuccessfulRechargeExists + `))`
	case "expiring":
		where += ` AND um.user_id IS NOT NULL AND um.status = 'active' AND um.expired_at > NOW() AND um.plan_code <> ? AND um.expired_at <= DATE_ADD(NOW(), INTERVAL 7 DAY)`
		args = append(args, model.DefaultRechargePermanentPlanCode)
	case "expired":
		where += ` AND um.user_id IS NOT NULL AND (um.status <> 'active' OR um.expired_at <= NOW())`
	case "legacy":
		where += ` AND um.user_id IS NULL AND ` + membershipOperationsSuccessfulRechargeExists
	case "permission_disabled":
		where += ` AND um.user_id IS NOT NULL AND um.template_download_enabled = 0`
	}

	switch strings.TrimSpace(filters.PermissionState) {
	case "enabled":
		where += ` AND ((um.user_id IS NOT NULL AND um.template_download_enabled = 1) OR (um.user_id IS NULL AND ` + membershipOperationsSuccessfulRechargeExists + `))`
	case "disabled":
		where += ` AND um.user_id IS NOT NULL AND um.template_download_enabled = 0`
	}

	return where, args
}

func queryMembershipOperationsCount(db *sql.DB, filters membershipOperationsFilters) (int64, error) {
	where, args := buildMembershipOperationsWhere(filters)
	query := `SELECT COUNT(*) FROM users u ` + membershipOperationsBaseJoin + ` WHERE ` + where
	var total int64
	err := db.QueryRow(query, args...).Scan(&total)
	return total, err
}

func queryMembershipOperationsUserRows(db *sql.DB, filters membershipOperationsFilters, limit, offset int) ([]gin.H, int64, error) {
	where, args := buildMembershipOperationsWhere(filters)
	countQuery := `SELECT COUNT(*) FROM users u ` + membershipOperationsBaseJoin + ` WHERE ` + where
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listQuery := `
		SELECT u.id, u.username, COALESCE(p.nickname, '') AS nickname
		FROM users u ` + membershipOperationsBaseJoin + `
		WHERE ` + where + `
		ORDER BY
			CASE
				WHEN um.user_id IS NOT NULL AND um.status = 'active' AND um.expired_at > NOW() THEN 0
				WHEN um.user_id IS NULL AND ` + membershipOperationsSuccessfulRechargeExists + ` THEN 1
				ELSE 2
			END ASC,
			COALESCE(um.expired_at, TIMESTAMP('2099-12-31 23:59:59')) ASC,
			u.id DESC
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
		var userID int64
		var username string
		var nickname string
		if err := rows.Scan(&userID, &username, &nickname); err != nil {
			return nil, 0, err
		}
		list = append(list, gin.H{
			"user_id":   userID,
			"username":  username,
			"nickname":  nickname,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func boolFromPayload(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	default:
		return false
	}
}

func intFromPayload(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func stringFromPayload(value interface{}) string {
	if v, ok := value.(string); ok {
		return v
	}
	return ""
}

func timePtrToString(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func queryMembershipOrderByOrderNo(db *sql.DB, orderNo string) gin.H {
	if db == nil || strings.TrimSpace(orderNo) == "" {
		return gin.H{}
	}
	var foundOrderNo string
	var status string
	var amount int64
	var orderCategory string
	var createdAt time.Time
	err := db.QueryRow(`
		SELECT order_no, status, amount, order_category, created_at
		FROM user_orders
		WHERE order_no = ?
		LIMIT 1
	`, orderNo).Scan(&foundOrderNo, &status, &amount, &orderCategory, &createdAt)
	if err != nil {
		return gin.H{}
	}
	return gin.H{
		"order_no":       foundOrderNo,
		"status":         status,
		"amount":         amount,
		"order_category": orderCategory,
		"created_at":     createdAt,
	}
}

func queryLatestSuccessfulRechargeOrder(db *sql.DB, userID int64) gin.H {
	if db == nil || userID <= 0 {
		return gin.H{}
	}
	var orderNo string
	var status string
	var amount int64
	var createdAt time.Time
	err := db.QueryRow(`
		SELECT order_no, status, amount, created_at
		FROM user_orders
		WHERE user_id = ? AND type = 'recharge' AND status = 'success'
		ORDER BY created_at DESC
		LIMIT 1
	`, userID).Scan(&orderNo, &status, &amount, &createdAt)
	if err != nil {
		return gin.H{}
	}
	return gin.H{
		"order_no":   orderNo,
		"status":     status,
		"amount":     amount,
		"created_at": createdAt,
	}
}

func buildManagementMembershipPayload(userMembershipModel *model.UserMembershipModel, userOrderModel *model.UserOrderModel, userID int64) (gin.H, error) {
	activeMembership, legacyRecharge, _, err := resolveUserMembershipAccess(userMembershipModel, userOrderModel, userID)
	if err != nil {
		return nil, err
	}
	if activeMembership == nil && userMembershipModel != nil {
		if currentMembership, currentErr := userMembershipModel.GetByUserID(userID); currentErr == nil && currentMembership != nil {
			if currentMembership.Status == "active" && !currentMembership.ExpiredAt.After(time.Now()) {
				_ = userMembershipModel.UpdateStatusByUserID(userID, "expired")
				currentMembership.Status = "expired"
			}
			if currentMembership.Status != "active" {
				activeMembership = currentMembership
			}
		}
	}
	return buildUserMembershipPayload(activeMembership, legacyRecharge), nil
}

func RegisterMembershipOperationsManagementRoutes(r *gin.RouterGroup, userDBModel *model.UserModel, userProfileModel *model.UserProfileModel, userMembershipModel *model.UserMembershipModel, userOrderModel *model.UserOrderModel) {
	membership := r.Group("/membership-operations")

	membership.GET("/overview", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "会员运营服务不可用"})
			return
		}
		db := userDBModel.DB
		totalUsers, err := queryMembershipOperationsCount(db, membershipOperationsFilters{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取会员用户总数失败: " + err.Error()})
			return
		}
		activeUsers, _ := queryMembershipOperationsCount(db, membershipOperationsFilters{Status: "active"})
		expiringSoonUsers, _ := queryMembershipOperationsCount(db, membershipOperationsFilters{Status: "expiring"})
		expiredUsers, _ := queryMembershipOperationsCount(db, membershipOperationsFilters{Status: "expired"})
		permissionDisabledUsers, _ := queryMembershipOperationsCount(db, membershipOperationsFilters{PermissionState: "disabled"})
		legacyUsers, _ := queryMembershipOperationsCount(db, membershipOperationsFilters{Status: "legacy"})

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"total_users":                totalUsers,
				"active_users":               activeUsers,
				"expiring_soon_users":        expiringSoonUsers,
				"expired_users":              expiredUsers,
				"permission_disabled_users":  permissionDisabledUsers,
				"legacy_recharge_users":      legacyUsers,
			},
		})
	})

	membership.GET("/users", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "会员运营服务不可用"})
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		if page < 1 {
			page = 1
		}
		if pageSize < 1 {
			pageSize = 20
		}
		if pageSize > 50 {
			pageSize = 50
		}
		offset := (page - 1) * pageSize
		filters := membershipOperationsFilters{
			Keyword:         c.Query("keyword"),
			Status:          c.DefaultQuery("status", "all"),
			PermissionState: c.DefaultQuery("permission_state", "all"),
		}
		rows, total, err := queryMembershipOperationsUserRows(userDBModel.DB, filters, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取会员用户列表失败: " + err.Error()})
			return
		}

		list := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			userID, _ := row["user_id"].(int64)
			username, _ := row["username"].(string)
			nickname, _ := row["nickname"].(string)
			membershipPayload, membershipErr := buildManagementMembershipPayload(userMembershipModel, userOrderModel, userID)
			if membershipErr != nil {
				continue
			}
			profile, _ := userProfileModel.GetByUserID(userID)
			serviceTitle := ""
			if profile != nil {
				serviceTitle = profile.ServiceTitle
			}
			sourceOrderNo := stringFromPayload(membershipPayload["source_order_no"])
			sourceOrder := queryMembershipOrderByOrderNo(userDBModel.DB, sourceOrderNo)
			latestRechargeOrder := queryLatestSuccessfulRechargeOrder(userDBModel.DB, userID)
			list = append(list, gin.H{
				"user_id":                    userID,
				"username":                   username,
				"nickname":                   nickname,
				"service_title":              serviceTitle,
				"display_name":               func() string { if strings.TrimSpace(nickname) != "" { return nickname }; return username }(),
				"plan_code":                  stringFromPayload(membershipPayload["plan_code"]),
				"plan_title":                 stringFromPayload(membershipPayload["plan_title"]),
				"status":                     stringFromPayload(membershipPayload["status"]),
				"template_download_enabled":  boolFromPayload(membershipPayload["template_download_enabled"]),
				"lifetime_membership":        boolFromPayload(membershipPayload["lifetime_membership"]),
				"legacy_recharge_member":     boolFromPayload(membershipPayload["legacy_recharge_member"]),
				"remaining_days":             intFromPayload(membershipPayload["remaining_days"]),
				"started_at_text":            stringFromPayload(membershipPayload["started_at_text"]),
				"granted_at_text":            stringFromPayload(membershipPayload["granted_at_text"]),
				"expired_at_text":            stringFromPayload(membershipPayload["expired_at_text"]),
				"source_order_no":            sourceOrderNo,
				"source_order":               sourceOrder,
				"latest_recharge_order":      latestRechargeOrder,
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
			},
		})
	})
}
