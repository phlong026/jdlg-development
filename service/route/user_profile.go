package route

import (
	"database/sql"
	"errors"
	"net/http"
	"service/config"
	"service/function"
	"service/model"
	"strings"

	"github.com/gin-gonic/gin"
)

func sanitizePublicImageURL(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	lower := strings.ToLower(clean)
	if strings.HasPrefix(lower, "wxfile://") || strings.HasPrefix(lower, "file://") || strings.HasPrefix(lower, "data:") {
		return ""
	}
	return clean
}

func isSafeRemoteImageURL(raw string) bool {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return true
	}
	lower := strings.ToLower(clean)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func normalizeRecoveryPhone(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	builder := strings.Builder{}
	for _, ch := range trimmed {
		if ch >= '0' && ch <= '9' {
			builder.WriteRune(ch)
		}
	}
	digits := builder.String()
	if len(digits) > 11 && strings.HasPrefix(digits, "86") {
		digits = digits[2:]
	}
	return digits
}

func matchRecoveryPhone(storedPhone, verifiedPhone string) bool {
	stored := normalizeRecoveryPhone(storedPhone)
	verified := normalizeRecoveryPhone(verifiedPhone)
	if stored == "" || verified == "" {
		return false
	}
	return stored == verified
}

func maskRecoveryPhone(raw string) string {
	phone := normalizeRecoveryPhone(raw)
	if len(phone) < 7 {
		return phone
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}

// RegisterUserProfileRoutes 注册用户信息修改路由
func RegisterUserProfileRoutes(r *gin.RouterGroup, codeSessionModel *model.CodeSessionRedisModel, userDBModel *model.UserModel, userProfileModel *model.UserProfileModel) {
	r.POST("/profile/password/recover", func(c *gin.Context) {
		if userDBModel == nil || userProfileModel == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "用户服务不可用",
			})
			return
		}

		var req struct {
			Username    string `json:"username" binding:"required,min=4,max=32"`
			NewPassword string `json:"new_password" binding:"required,min=6,max=32"`
			PhoneCode   string `json:"phone_code" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatValidationError("参数错误: " + err.Error()),
			})
			return
		}

		cfg := config.Get()
		if strings.TrimSpace(cfg.Wechat.AppID) == "" || strings.TrimSpace(cfg.Wechat.AppSecret) == "" {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "微信小程序未配置",
			})
			return
		}

		user, err := userDBModel.GetByUsernameAndType(strings.TrimSpace(req.Username), "miniprogram")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "账号不存在或暂不支持找回",
			})
			return
		}

		profileData, err := userProfileModel.GetByUserID(user.ID)
		if err != nil || profileData == nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "该账号还未绑定可验证手机号，请先使用微信登录后补充账号安全信息",
			})
			return
		}

		storedPhone := strings.TrimSpace(profileData.EnterpriseWechatContact)
		if !profileData.EnterpriseWechatVerified || storedPhone == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "该账号还未绑定可验证手机号，请先使用微信登录后补充账号安全信息",
			})
			return
		}

		accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "获取微信 access_token 失败: " + err.Error(),
			})
			return
		}

		verifiedPhone, err := function.GetUserPhoneNumber(accessToken, strings.TrimSpace(req.PhoneCode))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "获取手机号失败: " + err.Error(),
			})
			return
		}

		if !matchRecoveryPhone(storedPhone, verifiedPhone) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "当前微信手机号与账号已验证手机号不一致，无法找回密码",
			})
			return
		}

		hashedPassword := function.HashPassword(strings.TrimSpace(req.NewPassword))
		if _, err := userDBModel.DB.Exec(`UPDATE users SET password = ?, updated_at = NOW() WHERE id = ?`, hashedPassword, user.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  "重置密码失败: " + err.Error(),
			})
			return
		}

		_, _ = userProfileModel.GetOrCreate(user.ID, "")
		_ = userProfileModel.SetHasPassword(user.ID, true)

		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "密码重置成功",
			"data": gin.H{
				"username":      user.Username,
				"verified_phone": maskRecoveryPhone(verifiedPhone),
			},
		})
	})

	// 需要token验证的路由
	profile := r.Group("/profile")
	profile.Use(TokenAuthRequired(codeSessionModel))
	{
		// 获取用户完整信息
		profile.GET("", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "未登录",
				})
				return
			}

			// 获取用户基本信息（无行也视为 session 失效，删 session 并让前端重新登录）
			user, err := userDBModel.GetByID(codeSession.UserID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					_ = codeSessionModel.Delete(codeSession)
					c.JSON(http.StatusUnauthorized, gin.H{
						"code": 401,
						"msg":  "用户不存在或已失效，请重新登录",
					})
					return
				}
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "用户不存在或已失效，请重新登录",
				})
				return
			}

			// 获取用户扩展信息
			profileData, _ := userProfileModel.GetByUserID(codeSession.UserID)

			response := gin.H{
				"id":         user.ID,
				"username":   user.Username,
				"created_at": user.CreatedAt,
			}

			if profileData != nil {
				response["nickname"] = profileData.Nickname
				response["avatar"] = sanitizePublicImageURL(profileData.Avatar)
				response["designer_bio"] = profileData.DesignerBio
				response["specialty_styles"] = profileData.SpecialtyStyles
  				response["designer_experience_years"] = profileData.DesignerExperienceYears
  				response["service_title"] = profileData.ServiceTitle
  				response["service_quote"] = profileData.ServiceQuote
  				response["service_intro"] = profileData.ServiceIntro
  				response["service_enabled"] = profileData.ServiceEnabled
				response["designer_visible"] = profileData.DesignerVisible
  				response["enterprise_wechat_verified"] = profileData.EnterpriseWechatVerified
  				response["enterprise_wechat_contact"] = profileData.EnterpriseWechatContact
  				response["enterprise_wechat_verified_at"] = profileData.EnterpriseWechatVerifiedAt
				response["has_password"] = profileData.HasPassword
				response["phone"] = profileData.Phone
				response["identity_type"] = profileData.IdentityType
			} else {
				response["nickname"] = ""
				response["avatar"] = ""
				response["designer_bio"] = ""
				response["specialty_styles"] = ""
  				response["designer_experience_years"] = 0
  				response["service_title"] = ""
  				response["service_quote"] = 0
  				response["service_intro"] = ""
  				response["service_enabled"] = false
				response["designer_visible"] = true
  				response["enterprise_wechat_verified"] = false
  				response["enterprise_wechat_contact"] = ""
  				response["enterprise_wechat_verified_at"] = nil
				response["has_password"] = false
				response["phone"] = ""
				response["identity_type"] = ""
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": response,
			})
		})

		// 修改昵称
		profile.PUT("/nickname", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "未登录",
				})
				return
			}

			var req struct {
				Nickname string `json:"nickname" binding:"required,max=32"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			// 确保profile存在
			_, err := userProfileModel.GetOrCreate(codeSession.UserID, "")
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取用户信息失败",
				})
				return
			}

			if err := userProfileModel.UpdateNickname(codeSession.UserID, req.Nickname); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "修改昵称失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "修改成功",
			})
		})

			// 修改身份类型
			profile.PUT("/identity", func(c *gin.Context) {
				codeSession := GetTokenCodeSession(c)
				if codeSession == nil {
					c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
					return
				}
				var req struct {
					IdentityType   string `json:"identity_type" binding:"required"`
					AdminConfirmed bool   `json:"admin_confirmed"`
				}
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "参数错误"})
					return
				}
				profile, err := userProfileModel.GetOrCreate(codeSession.UserID, "")
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取用户资料失败"})
					return
				}
				// 已有身份的用户修改需要管理员确认
				if profile.IdentityType != "" && !req.AdminConfirmed {
					c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "修改身份需要管理员确认"})
					return
				}
				if err := userProfileModel.UpdateIdentityType(codeSession.UserID, req.IdentityType); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "修改身份失败: " + err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "修改成功"})
			})

		// 修改头像
		profile.PUT("/avatar", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "未登录",
				})
				return
			}

			var req struct {
				Avatar string `json:"avatar" binding:"required,url"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			// 确保profile存在
			_, err := userProfileModel.GetOrCreate(codeSession.UserID, "")
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取用户信息失败",
				})
				return
			}

			if err := userProfileModel.UpdateAvatar(codeSession.UserID, req.Avatar); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "修改头像失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "修改成功",
			})
		})

		profile.GET("/designer", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "未登录",
				})
				return
			}

			profileData, err := userProfileModel.GetOrCreate(codeSession.UserID, "")
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取用户信息失败",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"designer_bio":     profileData.DesignerBio,
					"specialty_styles": profileData.SpecialtyStyles,
	  				"designer_experience_years": profileData.DesignerExperienceYears,
  				"service_title":    profileData.ServiceTitle,
  				"service_quote":    profileData.ServiceQuote,
  				"service_intro":    profileData.ServiceIntro,
  				"service_enabled":  profileData.ServiceEnabled,
				"designer_visible": profileData.DesignerVisible,
  			},
  		})
		})

		profile.PUT("/designer", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "未登录",
				})
				return
			}

			var req struct {
				DesignerBio     string `json:"designer_bio" binding:"max=1024"`
				SpecialtyStyles string `json:"specialty_styles" binding:"max=512"`
	  			DesignerExperienceYears int64 `json:"designer_experience_years" binding:"min=0,max=100"`
  			ServiceTitle    string `json:"service_title" binding:"max=128"`
  			ServiceQuote    int64  `json:"service_quote" binding:"min=0"`
  			ServiceIntro    string `json:"service_intro" binding:"max=1024"`
  			ServiceEnabled  bool   `json:"service_enabled"`
			DesignerVisible bool   `json:"designer_visible"`
  		}
  		if err := c.ShouldBindJSON(&req); err != nil {
  			c.JSON(http.StatusBadRequest, gin.H{
  				"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			_, err := userProfileModel.GetOrCreate(codeSession.UserID, "")
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取用户信息失败",
				})
  				return
  			}

  			if err := userProfileModel.UpdateDesignerProfile(codeSession.UserID, req.DesignerBio, req.SpecialtyStyles, req.DesignerExperienceYears, req.ServiceTitle, req.ServiceQuote, req.ServiceIntro, req.ServiceEnabled); err != nil {
  				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "保存设计师资料失败: " + err.Error(),
  				})
  				return
  			}

			if err := userProfileModel.SetDesignerVisible(codeSession.UserID, req.DesignerVisible); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "保存设计师公开状态失败: " + err.Error(),
				})
				return
			}

  			c.JSON(http.StatusOK, gin.H{
  				"code": 0,
  				"msg":  "保存成功",
  			})
		})

		profile.PUT("/designer/service-config", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "未登录",
				})
				return
			}

			var req struct {
				ServiceTitle   string `json:"service_title" binding:"max=128"`
				ServiceQuote   int64  `json:"service_quote" binding:"min=0"`
				ServiceIntro   string `json:"service_intro" binding:"max=1024"`
				ServiceEnabled bool   `json:"service_enabled"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			profileData, err := userProfileModel.GetOrCreate(codeSession.UserID, "")
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取用户信息失败",
				})
				return
			}

			if err := userProfileModel.UpdateDesignerProfile(codeSession.UserID, profileData.DesignerBio, profileData.SpecialtyStyles, profileData.DesignerExperienceYears, req.ServiceTitle, req.ServiceQuote, req.ServiceIntro, req.ServiceEnabled); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "保存服务配置失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "保存成功",
			})
		})

		// 设置账号密码
		profile.POST("/password", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "未登录",
				})
				return
			}

			var req struct {
				Username    string `json:"username" binding:"required,min=4,max=32"`
				Password    string `json:"password" binding:"required,min=6,max=32"`
				OldPassword string `json:"old_password"` // 如果已设置密码，需要验证旧密码
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			// 获取用户信息
			user, err := userDBModel.GetByID(codeSession.UserID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					_ = codeSessionModel.Delete(codeSession)
				}
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "用户不存在或已失效，请重新登录",
				})
				return
			}

			// 获取profile检查是否已设置密码
			profileData, _ := userProfileModel.GetByUserID(codeSession.UserID)
			if profileData != nil && profileData.HasPassword {
				// 已设置密码，需要验证旧密码
				if req.OldPassword == "" {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  "请输入原密码",
					})
					return
				}
				if !function.VerifyPassword(req.OldPassword, user.Password) {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  "原密码错误",
					})
					return
				}
			}

			// 检查用户名是否已被使用（排除自己）
			existingUser, err := userDBModel.GetByUsernameAndType(req.Username, "miniprogram")
			if err == nil && existingUser.ID != codeSession.UserID {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "用户名已被使用",
				})
				return
			}

			// 更新用户名和密码
			hashedPassword := function.HashPassword(req.Password)
			query := `UPDATE users SET username = ?, password = ?, updated_at = NOW() WHERE id = ?`
			_, err = userDBModel.DB.Exec(query, req.Username, hashedPassword, codeSession.UserID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "设置失败: " + err.Error(),
				})
				return
			}

			// 确保profile存在并更新has_password
			userProfileModel.GetOrCreate(codeSession.UserID, "")
			userProfileModel.SetHasPassword(codeSession.UserID, true)

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "设置成功",
			})
		})

		// 修改密码（已设置密码的用户）
		profile.PUT("/password", func(c *gin.Context) {
			codeSession := GetTokenCodeSession(c)
			if codeSession == nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "未登录",
				})
				return
			}

			var req struct {
				OldPassword string `json:"old_password" binding:"required"`
				NewPassword string `json:"new_password" binding:"required,min=6,max=32"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			// 获取用户信息
			user, err := userDBModel.GetByID(codeSession.UserID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					_ = codeSessionModel.Delete(codeSession)
				}
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "用户不存在或已失效，请重新登录",
				})
				return
			}

			// 验证旧密码
			if !function.VerifyPassword(req.OldPassword, user.Password) {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "原密码错误",
				})
				return
			}

			// 更新密码
			hashedPassword := function.HashPassword(req.NewPassword)
			query := `UPDATE users SET password = ?, updated_at = NOW() WHERE id = ?`
			_, err = userDBModel.DB.Exec(query, hashedPassword, codeSession.UserID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "修改密码失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "修改成功",
			})
		})
	}
}
