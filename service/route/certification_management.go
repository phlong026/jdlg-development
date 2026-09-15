package route

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"service/model"
)

// RegisterCertificationManagementRoutes 注册认证工单管理路由（管理后台，需登录）
// 管理员审核认证申请（工单）：查看待审核列表，通过/拒绝并填写备注；通过时更新用户 can_withdraw
func RegisterCertificationManagementRoutes(r *gin.RouterGroup, certificationModel *model.CertificationApplicationModel, userDBModel *model.UserModel, userProfileModel *model.UserProfileModel) {
	// 工单列表（认证申请）
	r.GET("/certification-applications", func(c *gin.Context) {
		status := c.Query("status") // 空=全部, pending_review, approved, rejected
		keyword := c.Query("keyword")
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limit <= 0 || limit > 100 {
			limit = 20
		}
		if offset < 0 {
			offset = 0
		}
		list, total, err := certificationModel.List(status, keyword, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code":  0,
			"msg":   "ok",
			"data":  list,
			"total": total,
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
