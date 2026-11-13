package route

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"service/component"
	"service/config"
	"service/function"
	"service/model"
	"service/processor"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// abs 返回整数的绝对值
func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func generateAdminUploadObjectKey(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	return fmt.Sprintf("admin_uploads/%s%s", uuid.New().String(), ext)
}

// 管理端单次灵石操作的资金护栏上限：防手滑多打一个零造成不可逆的大额变动
const (
	maxAdminAdjustStones int64 = 10000  // 单次增减上限
	maxAdminSetStones    int64 = 100000 // 直接设置的目标余额上限
)

// clampManagementPageSize 管理端列表 pageSize 的统一钳制（P2-1）：
// 无上限的 page_size 会被传 100000 之类的大值拖垮 DB 与响应内存。
const managementPageSizeMax = 100

func clampManagementPageSize(size int) int {
	if size < 1 {
		return 20
	}
	if size > managementPageSizeMax {
		return managementPageSizeMax
	}
	return size
}

// parsePositiveQueryInt 解析正整数查询参数，非法或非正返回 0（=不过滤）
func parsePositiveQueryInt(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// maskPhoneNumber 管理端列表展示用手机号脱敏：11 位保留前3后4，其余位数按长度截半保留
func maskPhoneNumber(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if digits == 11 && len(s) == 11 {
		return s[:3] + "****" + s[7:]
	}
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	keep := len(s) / 4
	if keep < 1 {
		keep = 1
	}
	return s[:keep] + "****" + s[len(s)-keep:]
}

// 管理端按本次登录选择有效期，不修改共享 store，避免影响其他用户或小程序。
func managementSessionOptions(c *gin.Context, maxAge int) sessions.Options {
	return sessions.Options{
		Path: "/", MaxAge: maxAge, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: crmEnabled() || c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https",
	}
}

// RegisterManagementRoutes 注册管理后台路由
func RegisterManagementRoutes(r *gin.RouterGroup, authProcessor *processor.AuthProcessor, userDBModel *model.UserModel, userRedisModel *model.UserRedisModel, userProfileModel *model.UserProfileModel, stoneRecordModel *model.StoneRecordModel) {
	registerWecomLogin(r, userDBModel)
	// 登录接口（不需要认证）
	loginGate := newManagementLoginGate()
	r.POST("/login", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		var req struct {
			processor.PasswordLoginRequest
			RememberMe bool `json:"remember_me"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatValidationError("参数错误: " + err.Error()),
			})
			return
		}

		if !loginGate.allow(req.Username) {
			crmError(c, 429, "登录尝试过于频繁，请 5 分钟后重试")
			return
		}
		// 使用账号密码登录策略
		strategy, ok := authProcessor.GetStrategy("password")
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "登录策略未配置",
			})
			return
		}

		result, err := strategy.Login(c.Request.Context(), &req.PasswordLoginRequest)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "账号或密码错误",
			})
			return
		}

		// Redis SETEX 与浏览器 Cookie 使用同一固定期限；读取会话不会续期。
		maxAge := 8 * 60 * 60
		if req.RememberMe {
			maxAge = 28 * 24 * 60 * 60
		}
		sessions.Default(c).Options(managementSessionOptions(c, maxAge))
		if err := SetUserSession(c, result.User.ID, result.User.Username, result.User.UserType); err != nil {
			log.Printf("[management-login] ERROR 保存会话失败 user_id=%d: %v", result.User.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "保存会话失败",
			})
			return
		}

		// 返回平台访问范围，前端据此直达落地页（crm_only 无需再经 dashboard 二次跳转）
		accessScope := ""
		if access, accessErr := readManagementAccess(c.Request.Context(), userDBModel.DB, result.User.ID); accessErr == nil && access.Enabled {
			accessScope = access.Scope
		}
		data := gin.H{
			"id":       result.User.ID,
			"username": result.User.Username,
		}
		if accessScope != "" {
			data["scope"] = accessScope
			data["crmEnabled"] = crmEnabled()
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "登录成功",
			"data": data,
		})
	})

	// 需要认证的路由组
	auth := r.Group("")
	auth.Use(AuthRequired, ManagementAccessRequired(userDBModel))

	// 获取当前用户信息
	auth.GET("/me", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		userID := GetUserID(c)
		username := GetUsername(c)
		accessScope := "platform_admin"
		if value, ok := c.Get("management.access"); ok {
			accessScope = value.(managementAccess).Scope
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"id":         userID,
				"username":   username,
				"scope":      accessScope,
				"crmEnabled": crmEnabled(),
			},
		})
	})

	// 登出
	auth.POST("/logout", func(c *gin.Context) {
		// 清空字段不足以删除持久会话，负 MaxAge 同时撤销 Redis 记录和 Cookie。
		sessions.Default(c).Options(managementSessionOptions(c, -1))
		if err := ClearUserSession(c); err != nil {
			log.Printf("[management-logout] ERROR 撤销会话失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "登出失败",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "已登出",
		})
	})

	// 用户管理接口
	user := auth.Group("/users")
	{
		// 获取用户列表（小程序用户）
		user.GET("", func(c *gin.Context) {
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			keyword := strings.TrimSpace(c.Query("keyword"))
			enterpriseWechatStatus := strings.TrimSpace(c.DefaultQuery("enterprise_wechat_status", "all"))
			if page < 1 {
				page = 1
			}
			pageSize = clampManagementPageSize(pageSize)
			if enterpriseWechatStatus != "verified" && enterpriseWechatStatus != "pending" {
				enterpriseWechatStatus = "all"
			}
			offset := (page - 1) * pageSize

			filters := model.ManagementUserListFilters{
				Keyword:                keyword,
				EnterpriseWechatStatus: enterpriseWechatStatus,
				IdentityType:           strings.TrimSpace(c.Query("identity_type")),
				Status:                 strings.TrimSpace(c.Query("status")),
				CreatedWithinDays:      parsePositiveQueryInt(c.Query("created_within_days")),
			}
			if v := c.Query("stones_min"); v != "" {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
					filters.StonesMin = &n
				}
			}
			if v := c.Query("stones_max"); v != "" {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
					filters.StonesMax = &n
				}
			}
			filters.RiskOnly = c.Query("risk") == "1"
			filters.Member = strings.TrimSpace(c.Query("member"))
			filters.SortBy = strings.TrimSpace(c.Query("sort_by"))
			filters.SortDesc = c.Query("sort_order") == "desc"

			users, err := userDBModel.ListManagementUsers(filters, pageSize, offset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取用户列表失败: " + err.Error(),
				})
				return
			}

			total, err := userDBModel.CountManagementUsers(filters)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取用户总数失败: " + err.Error(),
				})
				return
			}

			// 获取每个用户的灵石余额
			userList := make([]gin.H, 0)
			for _, u := range users {
				stones, _ := userRedisModel.GetStones(u.ID)
				verifiedAt := ""
				if u.EnterpriseWechatVerifiedAt != nil {
					verifiedAt = u.EnterpriseWechatVerifiedAt.Format("2006-01-02 15:04:05")
				}
				// 风险标签：列表只展示廉价可算的结构化信号；同设备多账号/未设密码在 360 详情里查
				riskTags := make([]string, 0)
				if u.LastDeviceChangeTime != nil && u.LastDeviceChangeTime.After(time.Now().Add(-7*24*time.Hour)) {
					riskTags = append(riskTags, "recent_device_change")
				}
				memberStatus := ""
				if u.MemberStatus == "active" && u.MemberExpiredAt != nil && u.MemberExpiredAt.After(time.Now()) {
					memberStatus = "active"
				} else if u.MemberStatus != "" {
					memberStatus = "expired"
				}
				bannedAt := ""
				if u.BannedAt != nil {
					bannedAt = u.BannedAt.Format("2006-01-02 15:04:05")
				}
				userList = append(userList, gin.H{
					"id":                            u.ID,
					"username":                      u.Username,
					"user_type":                     u.UserType,
					"stones":                        stones,
					"nickname":                      strings.TrimSpace(u.Nickname),
					"phone":                         maskPhoneNumber(u.Phone),
					"identity_type":                 u.IdentityType,
					"status":                        u.Status,
					"banned_reason":                 u.BannedReason,
					"banned_at":                     bannedAt,
					"risk_tags":                     riskTags,
					"member_status":                 memberStatus,
					"member_plan_title":             u.MemberPlanTitle,
					"enterprise_wechat_verified":    u.EnterpriseWechatVerified,
					"enterprise_wechat_contact":     maskPhoneNumber(strings.TrimSpace(u.EnterpriseWechatContact)),
					"enterprise_wechat_verified_at": verifiedAt,
					"created_at":                    u.CreatedAt,
					"updated_at":                    u.UpdatedAt,
				})
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      userList,
					"total":     total,
					"page":      page,
					"page_size": pageSize,
				},
			})
		})

		// 用户列表全库真实口径统计（替代旧版"按当前页计算"的失真统计）
		auth.GET("/user-stats", func(c *gin.Context) {
			stats, err := userDBModel.GetManagementUserStats()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取用户统计失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": stats})
		})

		// 封禁用户：拦截登录与 AI 任务/支付发起（POST 会被审计中间件记录）
		user.POST("/:id/ban", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的用户ID"})
				return
			}
			var req struct {
				Reason string `json:"reason" binding:"required"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("请填写封禁原因: " + err.Error())})
				return
			}
			reason := strings.TrimSpace(req.Reason)
			if len(reason) > 200 {
				reason = reason[:200]
			}
			u, err := userDBModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
				return
			}
			if u.UserType != "miniprogram" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "仅支持封禁小程序用户"})
				return
			}
			if err := userDBModel.SetUserBanned(id, true, reason); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "封禁失败: " + err.Error()})
				return
			}
			log.Printf("[风控] 管理员 %s 封禁用户 %s (ID:%d)，原因: %s", GetUsername(c), u.Username, id, reason)
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已封禁", "data": gin.H{"id": id, "status": "banned"}})
		})

		// 解封用户
		user.POST("/:id/unban", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的用户ID"})
				return
			}
			var req struct {
				Reason string `json:"reason"`
			}
			_ = c.ShouldBindJSON(&req)
			u, err := userDBModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
				return
			}
			if err := userDBModel.SetUserBanned(id, false, strings.TrimSpace(req.Reason)); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解封失败: " + err.Error()})
				return
			}
			log.Printf("[风控] 管理员 %s 解封用户 %s (ID:%d)，说明: %s", GetUsername(c), u.Username, id, req.Reason)
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已解封", "data": gin.H{"id": id, "status": "active"}})
		})

		// 查看完整手机号（列表默认脱敏；POST 形式让审计中间件自动记录本次查看）
		user.POST("/:id/phone-reveal", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的用户ID"})
				return
			}
			if _, err := userDBModel.GetByID(id); err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
				return
			}
			var phone, wechatContact string
			if err := userDBModel.DB.QueryRow(`SELECT COALESCE(p.phone, ''), COALESCE(p.enterprise_wechat_contact, '') FROM user_profiles p WHERE p.user_id = ?`, id).Scan(&phone, &wechatContact); err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户扩展信息不存在"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"user_id":                   id,
					"phone":                     phone,
					"enterprise_wechat_contact": wechatContact,
				},
			})
		})

		// 获取单个用户信息
		user.GET("/:id", func(c *gin.Context) {
			idStr := c.Param("id")
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的用户ID",
				})
				return
			}

			u, err := userDBModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "用户不存在",
				})
				return
			}

			// 获取灵石余额
			stones, _ := userRedisModel.GetStones(u.ID)
			var profileData *model.UserProfile
			if userProfileModel != nil {
				profileData, _ = userProfileModel.GetByUserID(u.ID)
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"id":        u.ID,
					"username":  u.Username,
					"user_type": u.UserType,
					"stones":    stones,
					"nickname": func() string {
						if profileData != nil {
							return profileData.Nickname
						}
						return ""
					}(),
					"avatar": func() string {
						if profileData != nil {
							return sanitizePublicImageURL(profileData.Avatar)
						}
						return ""
					}(),
					"designer_bio": func() string {
						if profileData != nil {
							return profileData.DesignerBio
						}
						return ""
					}(),
					"specialty_styles": func() string {
						if profileData != nil {
							return profileData.SpecialtyStyles
						}
						return ""
					}(),
					"designer_experience_years": func() int64 {
						if profileData != nil {
							return profileData.DesignerExperienceYears
						}
						return 0
					}(),
					"service_title": func() string {
						if profileData != nil {
							return profileData.ServiceTitle
						}
						return ""
					}(),
					"created_at": u.CreatedAt,
					"updated_at": u.UpdatedAt,
				},
			})
		})

		user.GET("/:id/enterprise-wechat", func(c *gin.Context) {
			idStr := c.Param("id")
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的用户ID"})
				return
			}
			if _, err := userDBModel.GetByID(id); err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
				return
			}
			if userProfileModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户扩展信息服务不可用"})
				return
			}
			profile, err := userProfileModel.GetOrCreate(id, "")
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取手机号授权验证状态失败: " + err.Error()})
				return
			}
			verifiedAt := ""
			if profile.EnterpriseWechatVerifiedAt != nil {
				verifiedAt = profile.EnterpriseWechatVerifiedAt.Format("2006-01-02 15:04:05")
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"user_id":                       id,
					"enterprise_wechat_verified":    profile.EnterpriseWechatVerified,
					"enterprise_wechat_verified_at": verifiedAt,
					"enterprise_wechat_contact":     strings.TrimSpace(profile.EnterpriseWechatContact),
				},
			})
		})

		user.PUT("/:id/enterprise-wechat", func(c *gin.Context) {
			idStr := c.Param("id")
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的用户ID"})
				return
			}
			if _, err := userDBModel.GetByID(id); err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "用户不存在"})
				return
			}
			if userProfileModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "用户扩展信息服务不可用"})
				return
			}
			var req struct {
				Verified bool   `json:"verified"`
				Contact  string `json:"contact"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			if _, err := userProfileModel.GetOrCreate(id, ""); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "初始化用户扩展信息失败: " + err.Error()})
				return
			}
			contact := strings.TrimSpace(req.Contact)
			if req.Verified && contact == "" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "手机号授权通过时必须填写手机号"})
				return
			}
			if err := userProfileModel.SetEnterpriseWechatVerification(id, req.Verified, contact); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存手机号授权状态失败: " + err.Error()})
				return
			}
			profile, err := userProfileModel.GetByUserID(id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取手机号授权状态失败: " + err.Error()})
				return
			}
			verifiedAt := ""
			if profile.EnterpriseWechatVerifiedAt != nil {
				verifiedAt = profile.EnterpriseWechatVerifiedAt.Format("2006-01-02 15:04:05")
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "保存成功",
				"data": gin.H{
					"user_id":                       id,
					"enterprise_wechat_verified":    profile.EnterpriseWechatVerified,
					"enterprise_wechat_verified_at": verifiedAt,
					"enterprise_wechat_contact":     strings.TrimSpace(profile.EnterpriseWechatContact),
				},
			})
		})

		// 更新用户基础信息（用户名、登录密码）
		user.PUT("/:id", func(c *gin.Context) {
			idStr := c.Param("id")
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的用户ID",
				})
				return
			}

			var req struct {
				Username                *string `json:"username"` // 新用户名（可选）
				Password                *string `json:"password"` // 新密码（可选，留空则不修改）
				Nickname                *string `json:"nickname"` // 昵称（用户扩展信息，可选）
				Avatar                  *string `json:"avatar"`   // 头像URL（用户扩展信息，可选）
				DesignerBio             *string `json:"designer_bio"`
				SpecialtyStyles         *string `json:"specialty_styles"`
				DesignerExperienceYears *int64  `json:"designer_experience_years"`
				ServiceTitle            *string `json:"service_title"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			if (req.Username == nil || strings.TrimSpace(*req.Username) == "") &&
				(req.Password == nil || strings.TrimSpace(*req.Password) == "") &&
				(req.Nickname == nil || strings.TrimSpace(*req.Nickname) == "") &&
				(req.Avatar == nil || strings.TrimSpace(*req.Avatar) == "") &&
				req.DesignerBio == nil &&
				req.SpecialtyStyles == nil &&
				req.DesignerExperienceYears == nil &&
				req.ServiceTitle == nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "至少需要提供一项可修改字段",
				})
				return
			}

			u, err := userDBModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "用户不存在",
				})
				return
			}

			updates := []string{}
			args := []interface{}{}

			// 处理用户名修改
			if req.Username != nil {
				newUsername := strings.TrimSpace(*req.Username)
				if newUsername == "" {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  "用户名不能为空",
					})
					return
				}
				if newUsername != u.Username {
					// 检查同类型用户下用户名是否已存在
					if other, err := userDBModel.GetByUsernameAndType(newUsername, u.UserType); err == nil && other != nil && other.ID != u.ID {
						c.JSON(http.StatusBadRequest, gin.H{
							"code": 400,
							"msg":  "该用户名已被占用",
						})
						return
					}
					updates = append(updates, "username = ?")
					args = append(args, newUsername)
				}
			}

			// 处理密码修改
			if req.Password != nil {
				newPassword := strings.TrimSpace(*req.Password)
				if newPassword != "" {
					hashed := function.HashPassword(newPassword)
					updates = append(updates, "password = ?")
					args = append(args, hashed)
				}
			}

			// 如果有需要更新的用户名/密码，则更新 users 表
			if len(updates) > 0 {
				updates = append(updates, "updated_at = NOW()")
				args = append(args, id)
				query := "UPDATE users SET " + strings.Join(updates, ", ") + " WHERE id = ?"
				if _, err := userDBModel.DB.Exec(query, args...); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"code": 500,
						"msg":  "更新用户信息失败: " + err.Error(),
					})
					return
				}
			}

			// 更新扩展信息（昵称、头像）
			if userProfileModel != nil {
				profileData, _ := userProfileModel.GetOrCreate(id, "")
				if req.Nickname != nil {
					if err := userProfileModel.UpdateNickname(id, strings.TrimSpace(*req.Nickname)); err != nil {
						// 不中断整体流程，仅记录错误
						log.Println("更新用户昵称失败:", err)
					}
				}
				if req.Avatar != nil {
					avatar := strings.TrimSpace(*req.Avatar)
					if !isSafeRemoteImageURL(avatar) {
						c.JSON(http.StatusBadRequest, gin.H{
							"code": 400,
							"msg":  "头像必须是可公开访问的 http/https 地址",
						})
						return
					}
					if err := userProfileModel.UpdateAvatar(id, avatar); err != nil {
						log.Println("更新用户头像失败:", err)
					}
				}
				if profileData != nil && (req.DesignerBio != nil || req.SpecialtyStyles != nil || req.DesignerExperienceYears != nil || req.ServiceTitle != nil) {
					designerBio := profileData.DesignerBio
					specialtyStyles := profileData.SpecialtyStyles
					designerExperienceYears := profileData.DesignerExperienceYears
					serviceTitle := profileData.ServiceTitle
					if req.DesignerBio != nil {
						designerBio = strings.TrimSpace(*req.DesignerBio)
					}
					if req.SpecialtyStyles != nil {
						specialtyStyles = strings.TrimSpace(*req.SpecialtyStyles)
					}
					if req.DesignerExperienceYears != nil && *req.DesignerExperienceYears >= 0 {
						designerExperienceYears = *req.DesignerExperienceYears
					}
					if req.ServiceTitle != nil {
						serviceTitle = strings.TrimSpace(*req.ServiceTitle)
					}
					if err := userProfileModel.UpdateDesignerProfile(id, designerBio, specialtyStyles, designerExperienceYears, serviceTitle, profileData.ServiceQuote, profileData.ServiceIntro, profileData.ServiceEnabled); err != nil {
						log.Println("更新设计师资料失败:", err)
					}
				}
			}

			// 重新获取用户信息
			updated, _ := userDBModel.GetByID(id)

			resp := gin.H{
				"id":         updated.ID,
				"username":   updated.Username,
				"user_type":  updated.UserType,
				"created_at": updated.CreatedAt,
				"updated_at": updated.UpdatedAt,
			}
			// 尝试带上扩展信息（如果有）
			if userProfileModel != nil {
				if profile, err := userProfileModel.GetByUserID(id); err == nil && profile != nil {
					resp["nickname"] = profile.Nickname
					resp["avatar"] = profile.Avatar
					resp["designer_bio"] = profile.DesignerBio
					resp["specialty_styles"] = profile.SpecialtyStyles
					resp["designer_experience_years"] = profile.DesignerExperienceYears
					resp["service_title"] = profile.ServiceTitle
				}
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "更新成功",
				"data": resp,
			})
		})

		// 修改用户灵石余额
		user.PUT("/:id/stones", func(c *gin.Context) {
			idStr := c.Param("id")
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的用户ID",
				})
				return
			}

			var req setStonesRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			// 检查用户是否存在
			u, err := userDBModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "用户不存在",
				})
				return
			}

			newStones := *req.Stones
			if newStones > maxAdminSetStones {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "目标余额超过限制"})
				return
			}
			remark := fmt.Sprintf("管理员 %s 直接设置余额：%s", GetUsername(c), req.Remark)
			oldStones, err := userRedisModel.SetStonesWithRecord(id, newStones, remark)
			if err != nil {
				log.Printf("[资金] 管理员设置失败 user=%d err=%v", id, err)
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "设置余额与流水事务失败"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "修改成功",
				"data": gin.H{
					"id":         u.ID,
					"username":   u.Username,
					"old_stones": oldStones,
					"new_stones": newStones,
				},
			})
		})

		// 增减用户灵石（相对操作）
		user.POST("/:id/stones/adjust", func(c *gin.Context) {
			idStr := c.Param("id")
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的用户ID",
				})
				return
			}

			var req struct {
				Amount int64  `json:"amount" binding:"required"` // 调整数量（正数增加，负数减少）
				Remark string `json:"remark"`                    // 备注（可选）
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			if req.Amount == 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "调整数量不能为0",
				})
				return
			}
			if req.Amount > maxAdminAdjustStones || req.Amount < -maxAdminAdjustStones {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  fmt.Sprintf("单次调整不能超过 %d 灵石（如确需更大额度请分次操作并留存审批记录）", maxAdminAdjustStones),
				})
				return
			}

			// 检查用户是否存在
			u, err := userDBModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "用户不存在",
				})
				return
			}

			remark := fmt.Sprintf("管理员 %s 调整余额：%s", GetUsername(c), req.Remark)
			oldStones, newStones, err := userRedisModel.AdjustStonesWithRecord(id, req.Amount, remark)
			if err != nil {
				if errors.Is(err, model.ErrInsufficientStones) {
					c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "余额不足"})
					return
				}
				log.Printf("[资金] 管理员调整事务失败 user=%d err=%v", id, err)
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "余额与流水调整事务失败"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "调整成功",
				"data": gin.H{
					"id":         u.ID,
					"username":   u.Username,
					"old_stones": oldStones,
					"new_stones": newStones,
					"adjustment": req.Amount,
				},
			})
		})
	}

	// 获取COS临时密钥（STS）
	auth.GET("/cos/sts", func(c *gin.Context) {
		cfg := config.Get()
		if !cfg.COS.EnableSTS {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "未启用COS STS，请检查配置",
			})
			return
		}

		if err := component.HealthCheck(cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "COS配置异常: " + err.Error(),
			})
			return
		}

		cred, err := component.RequestSTSCredential(cfg)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取COS临时密钥失败: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "success",
			"data": gin.H{
				"tmp_secret_id":  cred.Credentials.TmpSecretID,
				"tmp_secret_key": cred.Credentials.TmpSecretKey,
				"session_token":  cred.Credentials.SessionToken,
				"start_time":     cred.StartTime,
				"expired_time":   cred.ExpiredTime,
				"bucket":         cfg.COS.Bucket,
				"region":         cfg.COS.Region,
				"prefix":         component.NormalizePrefix(cfg.COS.Prefix),
			},
		})
	})

	// OSS文件管理接口
	oss := auth.Group("/oss")
	{
		ossFileModel := model.NewOSSFileModel(userDBModel.DB)

		// 获取OSS文件列表
		oss.GET("/files", func(c *gin.Context) {
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			fileType := c.DefaultQuery("file_type", "all")
			sourceType := c.DefaultQuery("source_type", "all")
			keyword := c.DefaultQuery("keyword", "")

			if page < 1 {
				page = 1
			}
			pageSize = clampManagementPageSize(pageSize)
			offset := (page - 1) * pageSize

			files, err := ossFileModel.GetAll(pageSize, offset, sourceType, keyword, fileType)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取文件列表失败: " + err.Error(),
				})
				return
			}

			total, err := ossFileModel.Count(sourceType, keyword, fileType)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取文件总数失败: " + err.Error(),
				})
				return
			}

			fileList := make([]gin.H, 0)
			for _, f := range files {
				// 格式化来源信息
				sourceLabel := f.SourceName
				if f.SourceType == "user_ai" {
					sourceLabel = f.SourceName + " AI生成"
				} else if f.SourceType == "admin_upload" {
					sourceLabel = f.SourceName + " 上传"
				}

				fileList = append(fileList, gin.H{
					"id":          f.ID,
					"name":        f.FileName,
					"key":         f.ObjectKey,
					"size":        f.FileSize,
					"type":        getFileTypeFromMIME(f.ContentType),
					"url":         f.FileURL,
					"upload_time": f.CreatedAt,
					"uploader":    sourceLabel,
					"source_type": f.SourceType,
					"source_name": f.SourceName,
				})
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      fileList,
					"total":     total,
					"page":      page,
					"page_size": pageSize,
				},
			})
		})

		// 上传OSS文件（管理员上传）
		oss.POST("/upload", func(c *gin.Context) {
			file, err := c.FormFile("file")
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "文件上传失败: " + err.Error(),
				})
				return
			}

			// 打开文件
			src, err := file.Open()
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "打开文件失败: " + err.Error(),
				})
				return
			}
			defer src.Close()

			// 读取文件内容
			fileData := make([]byte, file.Size)
			_, err = src.Read(fileData)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "读取文件失败: " + err.Error(),
				})
				return
			}

			// 生成对象键（路径）
			cfg := config.Get()
			objectKey := generateAdminUploadObjectKey(file.Filename)

			// 上传到COS
			cosClient := component.GetCOSClient()
			if cosClient == nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "COS客户端未初始化",
				})
				return
			}

			fileURL, err := function.UploadBytes(context.Background(), cosClient, cfg, objectKey, fileData, file.Header.Get("Content-Type"))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "上传到COS失败: " + err.Error(),
				})
				return
			}

			// 获取当前管理员信息
			adminID := GetUserID(c)
			adminName := GetUsername(c)

			// 保存文件记录到数据库
			ossFile := &model.OSSFile{
				ObjectKey:   objectKey,
				FileName:    file.Filename,
				FileSize:    file.Size,
				ContentType: file.Header.Get("Content-Type"),
				FileURL:     fileURL,
				SourceType:  "admin_upload",
				SourceID:    adminID,
				SourceName:  adminName,
			}

			ossFileModel := model.NewOSSFileModel(userDBModel.DB)
			if err := ossFileModel.Create(ossFile); err != nil {
				// 数据库失败后补偿删除已上传对象；补偿失败必须可追踪，不能吞错。
				cleanupErr := compensateUploadedOSSObject(c.Request.Context(), cosClient, cfg, objectKey, ossObjectDeleteTimeout)
				if cleanupErr != nil && !errors.Is(cleanupErr, errOSSObjectNotFound) {
					log.Printf("[OSS][WARN] upload compensation failed admin_id=%d database_error_type=%T compensation_error_type=%T", adminID, err, cleanupErr)
				}
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "保存文件记录失败",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "上传成功",
				"data": gin.H{
					"id":          ossFile.ID,
					"name":        ossFile.FileName,
					"key":         ossFile.ObjectKey,
					"size":        ossFile.FileSize,
					"type":        getFileTypeFromMIME(ossFile.ContentType),
					"url":         ossFile.FileURL,
					"upload_time": ossFile.CreatedAt,
					"uploader":    adminName + " 上传",
				},
			})
		})

		// 删除OSS文件
		oss.DELETE("/files/:id", func(c *gin.Context) {
			idStr := c.Param("id")
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的文件ID",
				})
				return
			}

			ossFileModel := model.NewOSSFileModel(userDBModel.DB)
			err = deleteManagedOSSFile(c.Request.Context(), ossFileModel, newCOSObjectDeleteFunc(component.GetCOSClient(), config.Get()), id, ossObjectDeleteTimeout)
			if err != nil {
				if errors.Is(err, errOSSRecordNotFound) {
					c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "文件已不存在", "already_missing": true})
					return
				}
				reason := classifyOSSDeleteFailure(err)
				status := http.StatusBadGateway
				message := "对象存储删除失败，请重试"
				switch {
				case errors.Is(err, errOSSStorageUnavailable):
					status, message = http.StatusServiceUnavailable, "对象存储暂不可用，请稍后重试"
				case errors.Is(err, errOSSRecordLoadFailed):
					status, message = http.StatusInternalServerError, "文件记录读取失败，请重试"
				case errors.Is(err, errOSSObjectKeyAmbiguous):
					status, message = http.StatusConflict, "文件存储路径无法安全确认，未执行删除"
				case errors.Is(err, errOSSRecordDeleteFailed):
					status, message = http.StatusInternalServerError, "文件记录删除失败，请重试"
				}
				log.Printf("[OSS][WARN] delete failed id=%d reason=%s error_type=%T", id, reason, err)
				c.JSON(status, gin.H{"code": status, "msg": message, "retryable": true})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "删除成功",
			})
		})

		// 批量删除OSS文件
		oss.POST("/files/batch", func(c *gin.Context) {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ossDeleteBatchBodyLimit)
			var req struct {
				IDs []int64 `json:"ids"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "参数错误",
				})
				return
			}

			if len(req.IDs) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "请选择要删除的文件",
				})
				return
			}
			ids, err := normalizeOSSDeleteIDs(req.IDs, ossDeleteBatchLimit)
			if err != nil {
				message := "文件ID必须为正整数"
				if errors.Is(err, errOSSDeleteBatchTooLarge) {
					message = fmt.Sprintf("单次最多删除%d个文件", ossDeleteBatchLimit)
				}
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": message})
				return
			}

			ossFileModel := model.NewOSSFileModel(userDBModel.DB)
			deleteObject := newCOSObjectDeleteFunc(component.GetCOSClient(), config.Get())
			if deleteObject == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"code":      http.StatusServiceUnavailable,
					"msg":       "对象存储暂不可用，未删除任何文件记录",
					"retryable": true,
				})
				return
			}
			batchCtx, cancelBatch := context.WithTimeout(c.Request.Context(), ossBatchDeleteTimeout)
			result := deleteManagedOSSFiles(batchCtx, ossFileModel, deleteObject, ids, ossObjectDeleteTimeout)
			cancelBatch()
			if len(result.Failures) > 0 {
				log.Printf("[OSS][WARN] batch delete partial failure requested=%d deleted=%d failed=%d", len(ids), len(result.DeletedIDs), len(result.Failures))
				c.JSON(http.StatusMultiStatus, gin.H{
					"code":      http.StatusMultiStatus,
					"msg":       "批量删除部分失败，失败项可重试",
					"retryable": true,
					"data":      result,
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "批量删除成功",
				"data": result,
			})
		})
	}

	// 日志管理接口
	logs := auth.Group("/logs")
	{
		logConfigModel := model.NewLogConfigModel(userDBModel.DB)
		logModel := model.NewLogModel(userDBModel.DB)

		// 获取日志配置
		logs.GET("/config", func(c *gin.Context) {
			config, err := logConfigModel.Get()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取日志配置失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"rotate_interval": config.RotateInterval,
					"retention_days":  config.RetentionDays,
				},
			})
		})

		// 更新日志配置
		logs.POST("/config", func(c *gin.Context) {
			var req struct {
				RotateInterval int `json:"rotate_interval" binding:"required,min=1,max=168"` // 1-168小时（1周）
				RetentionDays  int `json:"retention_days" binding:"required,min=1,max=365"`  // 1-365天（1年）
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			if err := logConfigModel.CreateOrUpdate(req.RotateInterval, req.RetentionDays); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "更新日志配置失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "配置更新成功",
			})
		})

		// 获取日志列表
		logs.GET("", func(c *gin.Context) {
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			logType := c.DefaultQuery("type", "all")
			level := c.DefaultQuery("level", "all")
			keyword := c.DefaultQuery("keyword", "")

			if page < 1 {
				page = 1
			}
			pageSize = clampManagementPageSize(pageSize)
			offset := (page - 1) * pageSize

			logList, err := logModel.GetAll(pageSize, offset, logType, level, keyword, nil, nil)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取日志列表失败: " + err.Error(),
				})
				return
			}

			total, err := logModel.Count(logType, level, keyword, nil, nil)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取日志总数失败: " + err.Error(),
				})
				return
			}

			logs := make([]gin.H, 0)
			for _, l := range logList {
				logs = append(logs, gin.H{
					"id":        l.ID,
					"type":      l.Type,
					"level":     l.Level,
					"message":   l.Message,
					"user":      l.Username,
					"timestamp": l.CreatedAt,
					"details":   l.Details,
				})
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      logs,
					"total":     total,
					"page":      page,
					"page_size": pageSize,
				},
			})
		})
	}
}

// getFileTypeFromMIME 根据MIME类型判断文件类型
func getFileTypeFromMIME(contentType string) string {
	if contentType == "" {
		return "other"
	}
	if strings.HasPrefix(contentType, "image/") {
		return "image"
	}
	if strings.HasPrefix(contentType, "video/") {
		return "video"
	}
	if strings.HasPrefix(contentType, "audio/") {
		return "audio"
	}
	if strings.HasPrefix(contentType, "text/") || contentType == "application/pdf" ||
		strings.Contains(contentType, "document") || strings.Contains(contentType, "word") ||
		strings.Contains(contentType, "excel") || strings.Contains(contentType, "powerpoint") {
		return "document"
	}
	return "other"
}
