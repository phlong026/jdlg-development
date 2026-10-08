package route

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"service/model"
)

// RegisterUserMergeManagementRoutes 注册账号合并向导路由（管理后台，需登录）。
// 当前提供只读预检：对照两个账号的资产分布与冲突点，替代此前手工编写的
// merge_*_precheck SQL。真正的执行（20+ 张表的事务迁移，涉及资金与身份）
// 仍走 DBA 定制流程（sql/merge_*_execute_*.sql 模式），不在管理端开放一键执行。
func RegisterUserMergeManagementRoutes(r *gin.RouterGroup, userDBModel *model.UserModel) {
	g := r.Group("/user-merge")

	// 预检：from（被合并源账号）→ to（保留主账号）
	g.GET("/precheck", func(c *gin.Context) {
		if userDBModel == nil || userDBModel.DB == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户服务不可用"})
			return
		}
		fromID, err1 := strconv.ParseInt(c.Query("from"), 10, 64)
		toID, err2 := strconv.ParseInt(c.Query("to"), 10, 64)
		if err1 != nil || err2 != nil || fromID <= 0 || toID <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "请提供有效的 from/to 用户ID"})
			return
		}
		if fromID == toID {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "源账号与主账号不能相同"})
			return
		}
		db := userDBModel.DB

		readAccount := func(userID int64) (gin.H, error) {
			var username, userType, status, phone, wechatContact string
			var stones int64
			if err := db.QueryRow(`SELECT username, user_type, COALESCE(status,'active'), COALESCE(stones,0) FROM users WHERE id = ?`, userID).
				Scan(&username, &userType, &status, &stones); err != nil {
				return nil, err
			}
			if err := db.QueryRow(`SELECT COALESCE(phone,''), COALESCE(enterprise_wechat_contact,'') FROM user_profiles WHERE user_id = ?`, userID).
				Scan(&phone, &wechatContact); err != nil {
				phone, wechatContact = "", ""
			}
			count := func(query string, args ...interface{}) (int64, error) {
				var n int64
				if err := db.QueryRow(query, args...).Scan(&n); err != nil {
					return 0, err
				}
				return n, nil
			}
			orders, err := count(`SELECT COUNT(*) FROM user_orders WHERE user_id = ?`, userID)
			if err != nil {
				return nil, err
			}
			stoneRecords, err := count(`SELECT COUNT(*) FROM stone_records WHERE user_id = ?`, userID)
			if err != nil {
				return nil, err
			}
			tasks, err := count(`SELECT COUNT(*) FROM ai_tasks WHERE user_id = ?`, userID)
			if err != nil {
				return nil, err
			}
			videoTasks, err := count(`SELECT COUNT(*) FROM ai_video_tasks WHERE user_id = ?`, userID)
			if err != nil {
				return nil, err
			}
			templates, err := count(`SELECT COUNT(*) FROM templates WHERE creator_user_id = ?`, userID)
			if err != nil {
				return nil, err
			}
			membership, err := count(`SELECT COUNT(*) FROM user_memberships WHERE user_id = ?`, userID)
			if err != nil {
				return nil, err
			}
			certs, err := count(`SELECT COUNT(*) FROM certification_applications WHERE user_id = ?`, userID)
			if err != nil {
				return nil, err
			}
			identities, err := count(`SELECT COUNT(*) FROM user_identities WHERE user_id = ?`, userID)
			if err != nil {
				return nil, err
			}
			return gin.H{
				"user_id":              userID,
				"username":             username,
				"user_type":            userType,
				"status":               status,
				"stones":               stones,
				"phone":                phone,
				"enterprise_wechat_contact": wechatContact,
				"orders":               orders,
				"stone_records":        stoneRecords,
				"ai_tasks":             tasks,
				"ai_video_tasks":       videoTasks,
				"templates":            templates,
				"memberships":          membership,
				"certifications":       certs,
				"phone_identities":     identities,
			}, nil
		}

		fromAccount, err := readAccount(fromID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "源账号不存在"})
			return
		}
		toAccount, err := readAccount(toID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "主账号不存在"})
			return
		}
		if fromAccount["user_type"] != "miniprogram" || toAccount["user_type"] != "miniprogram" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "仅支持合并小程序用户账号"})
			return
		}

		// 冲突点提示：双方都持有的、合并时需要人工决策的资源
		conflicts := make([]string, 0)
		if m1, _ := fromAccount["memberships"].(int64); m1 > 0 {
			if m2, _ := toAccount["memberships"].(int64); m2 > 0 {
				conflicts = append(conflicts, "双方都有会员记录：需人工决定保留哪个计划（参考既有脚本：保留主账号、删除源账号记录）")
			}
		}
		if c1, _ := fromAccount["certifications"].(int64); c1 > 0 {
			if c2, _ := toAccount["certifications"].(int64); c2 > 0 {
				conflicts = append(conflicts, "双方都有认证申请：已通过的认证需人工决定归属（参考既有脚本：源账号已通过认证改判拒绝并注明合并）")
			}
		}
		if p1, _ := fromAccount["phone"].(string); p1 != "" {
			if p2, _ := toAccount["phone"].(string); p2 != "" && p1 != p2 {
				conflicts = append(conflicts, "双方手机号不同：合并后以主账号为准，源手机号绑定关系需迁移")
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"from": fromAccount,
				"to":   toAccount,
				"conflicts": conflicts,
				"execution_note": "执行合并（20+ 张表事务迁移，涉及资金与身份）不在管理端开放：以本预检为输入，由 DBA 按既有 sql/merge_*_execute 模式定制执行并写入 user_merge_batches 审计。",
			},
		})
	})
}
