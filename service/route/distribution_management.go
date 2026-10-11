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

type distributionListFilters struct {
	Keyword string
}

func queryDistributionOverview(db *sql.DB) (gin.H, error) {
	inviterCount := int64(0)
	totalInviteCount := int64(0)
	paidInviteCount := int64(0)
	totalRewardAmount := int64(0)
	monthRewardAmount := int64(0)
	monthNewInvites := int64(0)

	if err := db.QueryRow(`SELECT COUNT(DISTINCT inviter_user_id) FROM invite_relations`).Scan(&inviterCount); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM invite_relations`).Scan(&totalInviteCount); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM invite_relations ir
		WHERE ir.first_recharge_done = 1
		   OR EXISTS (
				SELECT 1 FROM user_orders uo
				WHERE uo.user_id = ir.invitee_user_id
				  AND uo.type = 'recharge'
				  AND uo.status = 'success'
		   )
	`).Scan(&paidInviteCount); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(amount), 0)
		FROM stone_records
		WHERE type IN ('invite', 'invite_reward')
	`).Scan(&totalRewardAmount); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(amount), 0)
		FROM stone_records
		WHERE type IN ('invite', 'invite_reward')
		  AND created_at >= DATE_FORMAT(NOW(), '%Y-%m-01 00:00:00')
	`).Scan(&monthRewardAmount); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM invite_relations
		WHERE invited_at >= DATE_FORMAT(NOW(), '%Y-%m-01 00:00:00')
	`).Scan(&monthNewInvites); err != nil {
		return nil, err
	}

	return gin.H{
		"inviter_count":       inviterCount,
		"total_invite_count":  totalInviteCount,
		"paid_invite_count":   paidInviteCount,
		"total_reward_amount": totalRewardAmount,
		"month_reward_amount": monthRewardAmount,
		"month_new_invites":   monthNewInvites,
	}, nil
}

func buildDistributionKeywordWhere(keyword string) (string, []interface{}) {
	where := "1=1"
	args := make([]interface{}, 0)
	trimmed := strings.TrimSpace(keyword)
	if trimmed == "" {
		return where, args
	}
	likeKeyword := "%" + trimmed + "%"
	where += ` AND (
		u.username LIKE ?
		OR COALESCE(p.nickname, '') LIKE ?
		OR COALESCE(code.invite_code, '') LIKE ?
	)`
	args = append(args, likeKeyword, likeKeyword, likeKeyword)
	return where, args
}

func queryDistributionInviters(db *sql.DB, filters distributionListFilters, limit, offset int) ([]gin.H, int64, error) {
	baseStatsSQL := `
		SELECT
			ir.inviter_user_id,
			COUNT(*) AS invite_count,
			SUM(CASE WHEN ir.first_recharge_done = 1 OR EXISTS (
				SELECT 1 FROM user_orders uo
				WHERE uo.user_id = ir.invitee_user_id
				  AND uo.type = 'recharge'
				  AND uo.status = 'success'
			) THEN 1 ELSE 0 END) AS paid_invite_count,
			MAX(ir.invited_at) AS last_invited_at
		FROM invite_relations ir
		GROUP BY ir.inviter_user_id
	`
	rewardStatsSQL := `
		SELECT
			sr.user_id,
			COALESCE(SUM(sr.amount), 0) AS total_reward_amount,
			COALESCE(SUM(CASE WHEN sr.created_at >= DATE_FORMAT(NOW(), '%Y-%m-01 00:00:00') THEN sr.amount ELSE 0 END), 0) AS month_reward_amount,
			MAX(sr.created_at) AS last_reward_at
		FROM stone_records sr
		WHERE sr.type IN ('invite', 'invite_reward')
		GROUP BY sr.user_id
	`
	whereSQL, args := buildDistributionKeywordWhere(filters.Keyword)
	countQuery := `
		SELECT COUNT(*)
		FROM (` + baseStatsSQL + `) base
		INNER JOIN users u ON u.id = base.inviter_user_id
		LEFT JOIN user_profiles p ON p.user_id = u.id
		LEFT JOIN user_invite_codes code ON code.user_id = u.id
		WHERE ` + whereSQL
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listQuery := `
		SELECT
			u.id,
			u.username,
			COALESCE(p.nickname, '') AS nickname,
			COALESCE(code.invite_code, '') AS invite_code,
			base.invite_count,
			base.paid_invite_count,
			COALESCE(reward.total_reward_amount, 0) AS total_reward_amount,
			COALESCE(reward.month_reward_amount, 0) AS month_reward_amount,
			base.last_invited_at,
			reward.last_reward_at
		FROM (` + baseStatsSQL + `) base
		INNER JOIN users u ON u.id = base.inviter_user_id
		LEFT JOIN user_profiles p ON p.user_id = u.id
		LEFT JOIN user_invite_codes code ON code.user_id = u.id
		LEFT JOIN (` + rewardStatsSQL + `) reward ON reward.user_id = u.id
		WHERE ` + whereSQL + `
		ORDER BY base.invite_count DESC, base.paid_invite_count DESC, total_reward_amount DESC, base.last_invited_at DESC
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
		var inviteCode string
		var inviteCount int64
		var paidInviteCount int64
		var totalRewardAmount int64
		var monthRewardAmount int64
		var lastInvitedAt sql.NullTime
		var lastRewardAt sql.NullTime
		if err := rows.Scan(&userID, &username, &nickname, &inviteCode, &inviteCount, &paidInviteCount, &totalRewardAmount, &monthRewardAmount, &lastInvitedAt, &lastRewardAt); err != nil {
			return nil, 0, err
		}
		displayName := username
		if strings.TrimSpace(nickname) != "" {
			displayName = strings.TrimSpace(nickname)
		}
		list = append(list, gin.H{
			"user_id":             userID,
			"username":            username,
			"nickname":            nickname,
			"display_name":        displayName,
			"invite_code":         inviteCode,
			"invite_count":        inviteCount,
			"paid_invite_count":   paidInviteCount,
			"total_reward_amount": totalRewardAmount,
			"month_reward_amount": monthRewardAmount,
			"last_invited_at":     formatNullTime(lastInvitedAt),
			"last_reward_at":      formatNullTime(lastRewardAt),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func buildDistributionRewardWhere(keyword string) (string, []interface{}) {
	where := `sr.type IN ('invite', 'invite_reward')`
	args := make([]interface{}, 0)
	trimmed := strings.TrimSpace(keyword)
	if trimmed == "" {
		return where, args
	}
	likeKeyword := "%" + trimmed + "%"
	where += ` AND (
		u.username LIKE ?
		OR COALESCE(p.nickname, '') LIKE ?
		OR COALESCE(sr.scene_desc, '') LIKE ?
		OR COALESCE(sr.remark, '') LIKE ?
	)`
	args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	return where, args
}

func queryDistributionRewards(db *sql.DB, filters distributionListFilters, limit, offset int) ([]gin.H, int64, error) {
	whereSQL, args := buildDistributionRewardWhere(filters.Keyword)
	countQuery := `
		SELECT COUNT(*)
		FROM stone_records sr
		INNER JOIN users u ON u.id = sr.user_id
		LEFT JOIN user_profiles p ON p.user_id = u.id
		WHERE ` + whereSQL
	var total int64
	if err := db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listQuery := `
		SELECT
			sr.id,
			sr.user_id,
			u.username,
			COALESCE(p.nickname, '') AS nickname,
			sr.type,
			sr.amount,
			COALESCE(sr.scene_desc, '') AS scene_desc,
			COALESCE(sr.remark, '') AS remark,
			sr.created_at
		FROM stone_records sr
		INNER JOIN users u ON u.id = sr.user_id
		LEFT JOIN user_profiles p ON p.user_id = u.id
		WHERE ` + whereSQL + `
		ORDER BY sr.created_at DESC, sr.id DESC
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
		var userID int64
		var username string
		var nickname string
		var rewardType string
		var amount int64
		var sceneDesc string
		var remark string
		var createdAt time.Time
		if err := rows.Scan(&id, &userID, &username, &nickname, &rewardType, &amount, &sceneDesc, &remark, &createdAt); err != nil {
			return nil, 0, err
		}
		displayName := username
		if strings.TrimSpace(nickname) != "" {
			displayName = strings.TrimSpace(nickname)
		}
		list = append(list, gin.H{
			"id":           id,
			"user_id":      userID,
			"username":     username,
			"nickname":     nickname,
			"display_name": displayName,
			"type":         rewardType,
			"amount":       amount,
			"scene_desc":   sceneDesc,
			"remark":       remark,
			"created_at":   createdAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func formatNullTime(value sql.NullTime) string {
	if !value.Valid || value.Time.IsZero() {
		return ""
	}
	return value.Time.Format(time.RFC3339)
}

func RegisterDistributionManagementRoutes(r *gin.RouterGroup, userDBModel *model.UserModel, inviteRelationModel *model.InviteRelationModel, userInviteCodeModel *model.UserInviteCodeModel, stoneRecordModel *model.StoneRecordModel, userOrderModel *model.UserOrderModel) {
	_ = inviteRelationModel
	_ = userInviteCodeModel
	_ = stoneRecordModel
	_ = userOrderModel
	distribution := r.Group("/distribution")

	distribution.GET("/overview", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "分销邀请服务不可用"})
			return
		}
		overview, err := queryDistributionOverview(userDBModel.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取分销概览失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": overview})
	})

	distribution.GET("/inviters", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "分销邀请服务不可用"})
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
		list, total, err := queryDistributionInviters(userDBModel.DB, distributionListFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取邀请人排行失败: " + err.Error()})
			return
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

	distribution.GET("/rewards", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "分销邀请服务不可用"})
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
		list, total, err := queryDistributionRewards(userDBModel.DB, distributionListFilters{Keyword: c.Query("keyword")}, pageSize, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取奖励明细失败: " + err.Error()})
			return
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
