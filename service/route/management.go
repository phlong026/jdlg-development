package route

import (
	"context"
	"fmt"
	"io"
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

// maxAdminUploadSize 管理员上传单个文件的大小上限（与规范文件上传的 50MB 保持一致）
const maxAdminUploadSize int64 = 50 << 20

// adminUploadExtensions 管理员上传的扩展名白名单。
// 覆盖素材库常用格式，明确排除 html/svg（存储型 XSS 向量）与可执行/脚本文件。
var adminUploadExtensions = map[string]bool{
	// 图片
	"jpg": true, "jpeg": true, "png": true, "webp": true, "gif": true, "bmp": true, "ico": true, "tiff": true,
	// 文档
	"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true, "ppt": true, "pptx": true, "txt": true, "csv": true,
	// 音视频
	"mp3": true, "wav": true, "aac": true, "mp4": true, "mov": true, "webm": true,
	// 压缩包与设计源文件
	"zip": true, "rar": true, "7z": true, "psd": true, "ai": true, "sketch": true,
}

func generateAdminUploadObjectKey(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	return fmt.Sprintf("admin_uploads/%s%s", uuid.New().String(), ext)
}

// RegisterManagementRoutes 注册管理后台路由
func RegisterManagementRoutes(r *gin.RouterGroup, authProcessor *processor.AuthProcessor, userDBModel *model.UserModel, userRedisModel *model.UserRedisModel, userProfileModel *model.UserProfileModel, stoneRecordModel *model.StoneRecordModel) {
	registerWecomLogin(r, userDBModel)
	// 登录接口（不需要认证）
	loginGate := newManagementLoginGate()
	r.POST("/login", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		var req processor.PasswordLoginRequest
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

		result, err := strategy.Login(context.Background(), &req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  "账号或密码错误",
			})
			return
		}

		// 设置session
		if err := SetUserSession(c, result.User.ID, result.User.Username, result.User.UserType); err != nil {
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
		if err := ClearUserSession(c); err != nil {
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
			if pageSize < 1 {
				pageSize = 20
			}
			if pageSize > 100 {
				pageSize = 100
			}
			if enterpriseWechatStatus != "verified" && enterpriseWechatStatus != "pending" {
				enterpriseWechatStatus = "all"
			}
			offset := (page - 1) * pageSize

			filters := model.ManagementUserListFilters{
				Keyword:                keyword,
				EnterpriseWechatStatus: enterpriseWechatStatus,
			}

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
				userList = append(userList, gin.H{
					"id":                            u.ID,
					"username":                      u.Username,
					"user_type":                     u.UserType,
					"stones":                        stones,
					"nickname":                      strings.TrimSpace(u.Nickname),
					"enterprise_wechat_verified":    u.EnterpriseWechatVerified,
					"enterprise_wechat_contact":     strings.TrimSpace(u.EnterpriseWechatContact),
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
					var hashed string
					if u.UserType == "management" {
						// 管理账号必须用 bcrypt 自适应哈希（MD5+固定盐可被秒级彩虹表反查）；
						// 登录侧 VerifyPassword 兼容两种哈希，改密后下次登录即走 bcrypt 验证
						if len(newPassword) < 12 {
							c.JSON(http.StatusBadRequest, gin.H{
								"code": 400,
								"msg":  "管理账号密码长度至少 12 位",
							})
							return
						}
						bcryptHash, err := function.HashManagementPassword(newPassword)
						if err != nil {
							c.JSON(http.StatusInternalServerError, gin.H{
								"code": 500,
								"msg":  "密码加密失败: " + err.Error(),
							})
							return
						}
						hashed = bcryptHash
					} else {
						// 小程序用户令牌体系与密码无关，保持既有哈希方式，但补最小长度校验
						if len(newPassword) < 8 {
							c.JSON(http.StatusBadRequest, gin.H{
								"code": 400,
								"msg":  "密码长度至少 8 位",
							})
							return
						}
						hashed = function.HashPassword(newPassword)
					}
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

			// 获取当前灵石余额
			oldStones, _ := userRedisModel.GetStones(id)

			// 设置新的灵石余额（0 是合法余额；required 已保证字段存在）
			newStones := *req.Stones
			if err := userRedisModel.SetStones(id, newStones); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "修改灵石余额失败: " + err.Error(),
				})
				return
			}

			// 记录操作日志
			adminName := GetUsername(c)
			logMessage := fmt.Sprintf("管理员 %s 将用户 %s (ID:%d) 的灵石余额从 %d 修改为 %d",
				adminName, u.Username, u.ID, oldStones, newStones)
			if req.Remark != "" {
				logMessage += "，备注: " + req.Remark
			}
			// 可以将日志记录到数据库（这里使用log打印）
			log.Println(logMessage)
			if stoneRecordModel != nil && newStones != oldStones {
				delta := newStones - oldStones
				recordType := "manual_grant"
				sceneDesc := "管理员增加灵石"
				if delta < 0 {
					recordType = "manual_deduct"
					sceneDesc = "管理员扣减灵石"
				}
				remark := fmt.Sprintf("管理员 %s 直接设置余额", adminName)
				if req.Remark != "" {
					remark += "：" + req.Remark
				}
				_ = stoneRecordModel.Create(u.ID, recordType, delta, sceneDesc, remark)
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

			// 检查用户是否存在
			u, err := userDBModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "用户不存在",
				})
				return
			}

			// 获取当前灵石余额
			oldStones, _ := userRedisModel.GetStones(id)

			// 计算新余额
			newStones := oldStones + req.Amount
			if newStones < 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  fmt.Sprintf("操作后余额不能为负数（当前余额: %d，调整: %d）", oldStones, req.Amount),
				})
				return
			}

			// 执行调整
			if req.Amount > 0 {
				if err := userRedisModel.AddStones(id, req.Amount); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"code": 500,
						"msg":  "调整灵石余额失败: " + err.Error(),
					})
					return
				}
			} else {
				if err := userRedisModel.DeductStones(id, -req.Amount); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"code": 500,
						"msg":  "调整灵石余额失败: " + err.Error(),
					})
					return
				}
			}

			// 记录操作日志
			adminName := GetUsername(c)
			operation := "增加"
			if req.Amount < 0 {
				operation = "扣除"
			}
			logMessage := fmt.Sprintf("管理员 %s %s用户 %s (ID:%d) 灵石 %d，余额从 %d 变为 %d",
				adminName, operation, u.Username, u.ID, abs(req.Amount), oldStones, newStones)
			if req.Remark != "" {
				logMessage += "，备注: " + req.Remark
			}
			log.Println(logMessage)
			if stoneRecordModel != nil {
				recordType := "manual_grant"
				sceneDesc := "管理员增加灵石"
				if req.Amount < 0 {
					recordType = "manual_deduct"
					sceneDesc = "管理员扣减灵石"
				}
				remark := fmt.Sprintf("管理员 %s 调整余额", adminName)
				if req.Remark != "" {
					remark += "：" + req.Remark
				}
				_ = stoneRecordModel.Create(u.ID, recordType, req.Amount, sceneDesc, remark)
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
			if pageSize < 1 {
				pageSize = 20
			}
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
			// 限制请求体大小，防止恶意大文件把内存打爆（OOM）
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAdminUploadSize)
			file, err := c.FormFile("file")
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "文件上传失败: " + err.Error(),
				})
				return
			}
			if file.Size <= 0 || file.Size > maxAdminUploadSize {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "文件大小需在 50MB 以内",
				})
				return
			}
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(file.Filename), "."))
			if !adminUploadExtensions[ext] {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "不支持的文件类型: " + ext + "（可上传图片、文档、音视频、压缩包等常见格式）",
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

			// 读取文件内容：io.Reader 的单次 Read 不保证读满，大文件会被静默截断产生损坏文件
			fileData, err := io.ReadAll(io.LimitReader(src, maxAdminUploadSize+1))
			if err != nil || int64(len(fileData)) > maxAdminUploadSize {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "读取文件失败或文件过大",
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
				// 如果数据库保存失败，尝试删除COS中的文件；删除失败会留下孤儿对象，必须留痕人工清理
				if _, delErr := cosClient.Object.Delete(context.Background(), objectKey); delErr != nil {
					log.Printf("管理员上传回滚失败（COS 残留孤儿对象需人工清理）: objectKey=%s, err=%v", objectKey, delErr)
				}
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "保存文件记录失败: " + err.Error(),
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
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的文件ID",
				})
				return
			}

			// 获取文件记录
			ossFileModel := model.NewOSSFileModel(userDBModel.DB)
			file, err := ossFileModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "文件不存在",
				})
				return
			}

			// 从COS删除文件
			cosClient := component.GetCOSClient()
			if cosClient != nil {
				cosClient.Object.Delete(context.Background(), file.ObjectKey)
			}

			// 从数据库删除记录
			if err := ossFileModel.Delete(id); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "删除文件记录失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "删除成功",
			})
		})

		// 批量删除OSS文件
		oss.POST("/files/batch", func(c *gin.Context) {
			var req struct {
				IDs []int64 `json:"ids"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "参数错误: " + err.Error(),
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

			// 获取所有文件记录
			ossFileModel := model.NewOSSFileModel(userDBModel.DB)
			cosClient := component.GetCOSClient()

			// 从COS删除文件
			for _, id := range req.IDs {
				file, err := ossFileModel.GetByID(id)
				if err == nil && cosClient != nil {
					cosClient.Object.Delete(context.Background(), file.ObjectKey)
				}
			}

			// 从数据库批量删除记录
			if err := ossFileModel.DeleteBatch(req.IDs); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "批量删除失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "批量删除成功",
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
			if pageSize < 1 {
				pageSize = 20
			}
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
