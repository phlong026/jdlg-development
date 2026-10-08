package route

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"service/model"
)

// 认证材料脱敏（列表与详情默认脱敏；完整值经 POST reveal 端点查看并记审计）
func maskCertRealName(name string) string {
	runes := []rune(strings.TrimSpace(name))
	if len(runes) <= 1 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[0]) + strings.Repeat("*", len(runes)-1)
}

func maskCertIDCard(id string) string {
	s := strings.TrimSpace(id)
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}

func maskCertCreditCode(code string) string {
	s := strings.TrimSpace(code)
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}

// RegisterCertificationManagementRoutes 注册认证工单管理路由（管理后台，需登录）
// 管理员审核认证申请（工单）：查看待审核列表，通过/拒绝并填写备注；通过时更新用户 can_withdraw
func RegisterCertificationManagementRoutes(r *gin.RouterGroup, certificationModel *model.CertificationApplicationModel, userDBModel *model.UserModel, userProfileModel *model.UserProfileModel) {
	// 队列统计（全库口径；独立路径避免与 /certification-applications/:id 通配冲突）
	r.GET("/certification-application-stats", func(c *gin.Context) {
		stats, err := certificationModel.Stats()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "统计失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": stats})
	})

	// 工单列表（认证申请）
	r.GET("/certification-applications", func(c *gin.Context) {
		status := c.Query("status") // 空=全部, pending_review, approved, rejected
		keyword := c.Query("keyword")
		overdue := c.Query("overdue") == "1" // 超时队列：待审核且提交超 48 小时
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limit <= 0 || limit > 100 {
			limit = 20
		}
		if offset < 0 {
			offset = 0
		}
		list, total, err := certificationModel.ListWithOverdue(status, keyword, overdue, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询失败"})
			return
		}
		// 列表层默认脱敏；完整值在详情抽屉内经 reveal 端点按次查看
		for _, app := range list {
			app.RealName = maskCertRealName(app.RealName)
			app.LegalPerson = maskCertRealName(app.LegalPerson)
			app.IDCardNo = maskCertIDCard(app.IDCardNo)
			app.CreditCode = maskCertCreditCode(app.CreditCode)
		}
		c.JSON(http.StatusOK, gin.H{
			"code":  0,
			"msg":   "ok",
			"data":  list,
			"total": total,
		})
	})

	// 查看完整认证材料（POST 形式让审计中间件记录本次查看）
	r.POST("/certification-applications/:id/reveal", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的申请ID"})
			return
		}
		app, err := certificationModel.GetByID(id)
		if err != nil || app == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "申请不存在"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "ok",
			"data": gin.H{
				"id":           app.ID,
				"real_name":    app.RealName,
				"id_card_no":   app.IDCardNo,
				"credit_code":  app.CreditCode,
				"legal_person": app.LegalPerson,
			},
		})
	})

	// 工单详情
	r.GET("/certification-applications/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的申请ID"})
			return
		}
		app, err := certificationModel.GetByID(id)
		if err != nil || app == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "申请不存在"})
			return
		}
		if userDBModel != nil {
			_, _ = certificationModel.SyncUserCanWithdraw(userDBModel, app.UserID)
		}
		user, _ := userDBModel.GetByID(app.UserID)
		// 详情与列表同口径脱敏；完整值走 POST reveal 端点（记审计）
		app.RealName = maskCertRealName(app.RealName)
		app.LegalPerson = maskCertRealName(app.LegalPerson)
		app.IDCardNo = maskCertIDCard(app.IDCardNo)
		app.CreditCode = maskCertCreditCode(app.CreditCode)
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"application": app,
				"user":        user,
			},
		})
	})

	// 审核（通过/拒绝）
	r.POST("/certification-applications/:id/review", func(c *gin.Context) {
		adminID := GetUserID(c)
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的申请ID"})
			return
		}
		var req struct {
			Action      string `json:"action" binding:"required,oneof=approve reject"`
			AdminRemark string `json:"admin_remark"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误: " + err.Error()})
			return
		}
		app, err := certificationModel.GetByID(id)
		if err != nil || app == nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "申请不存在"})
			return
		}
		if app.Status != model.CertificationStatusPendingReview {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "该申请已审核，无法重复操作"})
			return
		}
		status := model.CertificationStatusRejected
		if req.Action == "approve" {
			status = model.CertificationStatusApproved
			if err := certificationModel.ValidateAccountConsistency(app, app.ID); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
				return
			}
		}
		if err := certificationModel.UpdateReview(id, status, req.AdminRemark, adminID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败"})
			return
		}
		currentCanWithdraw := false
		if userDBModel != nil {
			currentCanWithdraw, _ = certificationModel.SyncUserCanWithdraw(userDBModel, app.UserID)
 		}
		if status == model.CertificationStatusApproved && currentCanWithdraw {
			if userProfileModel != nil {
				_, _ = userProfileModel.GetOrCreate(app.UserID, "")
				_ = userProfileModel.SetDesignerVisible(app.UserID, true)
			}
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "操作成功"})
	})
}
