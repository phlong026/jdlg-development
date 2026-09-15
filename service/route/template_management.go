package route

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"service/model"
	"strconv"
)

// TemplateRequest 模板请求结构
type TemplateRequest struct {
	Name         string `json:"name" binding:"required"`
	Category     string `json:"category" binding:"required"`
	MainTab      string `json:"main_tab"`  // 一级tab value，可为空
	SubTab       string `json:"sub_tab"`   // 二级tab value，可为空（如果设置了main_tab但sub_tab为空，表示属于父tab）
	ThirdTab     string `json:"third_tab"` // 三级tab value，可为空
	Description  string `json:"description"`
	Thumbnail    string `json:"thumbnail"`
	PreviewURL   string `json:"preview_url"`
	Images       string `json:"images"` // JSON数组格式
	Price        int64  `json:"price"`
	IsFree       bool   `json:"is_free"`
	Status       string `json:"status"`
	PublishScope string `json:"publish_scope"`
	RejectReason string `json:"reject_reason"`
	SourceType   string `json:"source_type"`
}

// RegisterTemplateManagementRoutes 注册模板管理路由（管理后台）
func RegisterTemplateManagementRoutes(r *gin.RouterGroup, templateModel *model.TemplateModel, templateCategoryModel *model.TemplateCategoryModel, templateSquareConfigModel *model.TemplateSquareConfigModel, featuredCaseGroupModel *model.FeaturedCaseGroupModel) {
	templates := r.Group("/templates")
	{
		// ---------- 双重 Tab 配置（需在 /:id 之前注册）----------
		templates.GET("/tab-config", func(c *gin.Context) {
			if templateSquareConfigModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Tab 配置未就绪"})
				return
			}

			mainTabs := make([]gin.H, 0)
			subTabs := make([]gin.H, 0)
			thirdTabs := make([]gin.H, 0)
			cfg, err := templateSquareConfigModel.Get()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "读取 Tab 配置失败: " + err.Error()})
				return
			}
			if cfg != nil {
				mainList, err := templateSquareConfigModel.ParseMainTabs(cfg.MainTabs)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析一级 Tab 配置失败: " + err.Error()})
					return
				}
				for _, t := range mainList {
					mainTabs = append(mainTabs, gin.H{"label": t.Label, "value": t.Value})
				}

				subList, err := templateSquareConfigModel.ParseSubTabs(cfg.SubTabs)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析二级 Tab 配置失败: " + err.Error()})
					return
				}
				for _, t := range subList {
					subTabs = append(subTabs, gin.H{"label": t.Label, "value": t.Value, "parent": t.Parent})
				}

				thirdList, err := templateSquareConfigModel.ParseThirdTabs(cfg.ThirdTabs)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "解析三级 Tab 配置失败: " + err.Error()})
					return
				}
				for _, t := range thirdList {
					thirdTabs = append(thirdTabs, gin.H{"label": t.Label, "value": t.Value, "parent": t.Parent})
				}
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{"main_tabs": mainTabs, "sub_tabs": subTabs, "third_tabs": thirdTabs},
			})
		})
		templates.PUT("/tab-config", func(c *gin.Context) {
			var req struct {
				MainTabs []struct {
					Label string `json:"label"`
					Value string `json:"value"`
				} `json:"main_tabs" binding:"required"`
				SubTabs []struct {
					Label  string `json:"label"`
					Value  string `json:"value"`
					Parent string `json:"parent"` // 二级tab的父tab value
				} `json:"sub_tabs" binding:"required"`
				ThirdTabs []struct {
					Label  string `json:"label"`
					Value  string `json:"value"`
					Parent string `json:"parent"` // 三级tab的父tab value（二级）
				} `json:"third_tabs"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			if templateSquareConfigModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Tab 配置未就绪"})
				return
			}
			// 验证二级tab必须隶属于父tab
			mainTabValues := make(map[string]bool)
			for _, t := range req.MainTabs {
				mainTabValues[t.Value] = true
			}
			for _, t := range req.SubTabs {
				if t.Parent == "" {
					c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "二级tab必须指定父tab"})
					return
				}
				if !mainTabValues[t.Parent] {
					c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "二级tab的父tab不存在: " + t.Parent})
					return
				}
			}
			subTabValues := make(map[string]bool)
			for _, t := range req.SubTabs {
				subTabValues[t.Value] = true
			}
			for _, t := range req.ThirdTabs {
				if t.Parent == "" {
					c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "三级tab必须指定父tab"})
					return
				}
				if !subTabValues[t.Parent] {
					c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "三级tab的父tab不存在: " + t.Parent})
					return
				}
			}
			mainList := make([]model.TabItem, 0, len(req.MainTabs))
			for _, t := range req.MainTabs {
				mainList = append(mainList, model.TabItem{Label: t.Label, Value: t.Value})
			}
			subList := make([]model.TabItem, 0, len(req.SubTabs))
			for _, t := range req.SubTabs {
				subList = append(subList, model.TabItem{Label: t.Label, Value: t.Value, Parent: t.Parent})
			}
			thirdList := make([]model.TabItem, 0, len(req.ThirdTabs))
			for _, t := range req.ThirdTabs {
				thirdList = append(thirdList, model.TabItem{Label: t.Label, Value: t.Value, Parent: t.Parent})
			}
			if err := validateTemplateSquareConfig(mainList, subList, thirdList); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
				return
			}
			mainBytes, _ := json.Marshal(mainList)
			subBytes, _ := json.Marshal(subList)
			thirdBytes, _ := json.Marshal(thirdList)
			if err := templateSquareConfigModel.Set(string(mainBytes), string(subBytes), string(thirdBytes)); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "保存成功"})
		})

		// ---------- 分类管理（需在 /:id 之前注册，避免 "categories" 被当作 id）----------
		// 获取分类列表
		templates.GET("/categories", func(c *gin.Context) {
			list, err := templateCategoryModel.List()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取分类列表失败: " + err.Error(),
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{"list": list},
			})
		})

		// 新增分类
		templates.POST("/categories", func(c *gin.Context) {
			var req struct {
				ID        string `json:"id" binding:"required,max=64"`
				Name      string `json:"name" binding:"required,max=128"`
				SortOrder int    `json:"sort_order"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}
			cat := &model.TemplateCategory{ID: req.ID, Name: req.Name, SortOrder: req.SortOrder}
			if err := templateCategoryModel.Create(cat); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "创建分类失败: " + err.Error(),
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "创建成功",
				"data": cat,
			})
		})

		// 删除分类
		templates.DELETE("/categories/:id", func(c *gin.Context) {
			id := c.Param("id")
			if id == "" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "分类ID不能为空"})
				return
			}
			count, err := templateCategoryModel.CountTemplatesByCategory(id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询失败: " + err.Error()})
				return
			}
			if count > 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "该分类下还有模板，请先将模板移至其他分类或删除后再删除分类",
					"data": gin.H{"count": count},
				})
				return
			}
			if err := templateCategoryModel.Delete(id); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除分类失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
		})

		// 获取模板列表
		templates.GET("", func(c *gin.Context) {
			category := c.Query("category")
			status := c.Query("status")
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

			if page < 1 {
				page = 1
			}
			if pageSize < 1 {
				pageSize = 20
			}

			offset := (page - 1) * pageSize
			templateList, err := templateModel.List(category, status, pageSize, offset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取模板列表失败: " + err.Error(),
				})
				return
			}

			total, err := templateModel.Count(category, status)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取总数失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      templateList,
					"total":     total,
					"page":      page,
					"page_size": pageSize,
				},
			})
		})

		// 获取模板详情
		templates.GET("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的模板ID",
				})
				return
			}

			template, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "模板不存在",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": template,
			})
		})

		// 创建模板
		templates.POST("", func(c *gin.Context) {
			var req TemplateRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			// 获取当前管理员信息（从中间件）
			username := GetUsername(c)
			if username == "" {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code": 401,
					"msg":  "未授权",
				})
				return
			}

			template := &model.Template{
				Name:         req.Name,
				Category:     req.Category,
				MainTab:      req.MainTab,
				SubTab:       req.SubTab,
				ThirdTab:     req.ThirdTab,
				Description:  req.Description,
				Thumbnail:    req.Thumbnail,
				PreviewURL:   req.PreviewURL,
				Images:       req.Images,
				Price:        req.Price,
				IsFree:       req.IsFree,
				Status:       req.Status,
				PublishScope: req.PublishScope,
				RejectReason: req.RejectReason,
				SourceType:   req.SourceType,
				Creator:      username,
			}
			if err := validateTemplateTabAssignment(templateSquareConfigModel, template.MainTab, template.SubTab, template.ThirdTab); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  err.Error(),
				})
				return
			}
			if err := validateTemplatePrimaryImage(template); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  err.Error(),
				})
				return
			}
			if err := generateTemplateDerivedImages(context.Background(), template, "templates/admin"); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "生成模板衍生图失败: " + err.Error(),
				})
				return
			}

			if err := templateModel.Create(template); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "创建模板失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "创建成功",
				"data": template,
			})
		})

		// 更新模板
		templates.PUT("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的模板ID",
				})
				return
			}

			var req TemplateRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			template, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "模板不存在",
				})
				return
			}

			template.Name = req.Name
			template.Category = req.Category
			template.MainTab = req.MainTab
			template.SubTab = req.SubTab
			template.ThirdTab = req.ThirdTab
			template.Description = req.Description
			template.Thumbnail = req.Thumbnail
			template.PreviewURL = req.PreviewURL
			template.Images = req.Images
			template.Price = req.Price
			template.IsFree = req.IsFree
			template.Status = req.Status
			template.PublishScope = req.PublishScope
			template.RejectReason = req.RejectReason
			template.SourceType = req.SourceType
			if err := validateTemplateTabAssignment(templateSquareConfigModel, template.MainTab, template.SubTab, template.ThirdTab); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  err.Error(),
				})
				return
			}
			if err := validateTemplatePrimaryImage(template); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  err.Error(),
				})
				return
			}
			if err := generateTemplateDerivedImages(context.Background(), template, templateVariantNamespace(template)); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "生成模板衍生图失败: " + err.Error(),
				})
				return
			}

			if err := templateModel.Update(template); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "更新模板失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "更新成功",
				"data": template,
			})
		})

		// 移动模板到指定分类
		templates.PATCH("/:id/category", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的模板ID",
				})
				return
			}
			var req struct {
				Category string `json:"category" binding:"required"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}
			template, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "模板不存在",
				})
				return
			}
			_, err = templateCategoryModel.GetByID(req.Category)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "目标分类不存在",
				})
				return
			}
			template.Category = req.Category
			if err := templateModel.Update(template); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "移动失败: " + err.Error(),
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "移动成功",
				"data": template,
			})
		})

		// 审核：仅更新模板状态（通过/拒绝）
		templates.PATCH("/:id/status", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的模板ID",
				})
				return
			}

			var req struct {
				Status       string `json:"status" binding:"required,oneof=published draft archived rejected"`
				RejectReason string `json:"reject_reason"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			template, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "模板不存在",
				})
				return
			}

			template.Status = req.Status
			if req.Status == "published" {
				template.RejectReason = ""
			} else if req.Status == "rejected" {
				template.RejectReason = req.RejectReason
			}
			if err := templateModel.Update(template); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "更新状态失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "操作成功",
				"data": template,
			})
		})

		// 删除模板
		templates.DELETE("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的模板ID",
				})
				return
			}

			if err := templateModel.Delete(id); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "删除模板失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "删除成功",
			})
		})

		// 设置/取消精选案例
		templates.PATCH("/:id/featured", func(c *gin.Context) {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  "无效的模板ID",
				})
				return
			}

			var req struct {
				IsFeatured bool `json:"is_featured" binding:"required"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"code": 400,
					"msg":  FormatValidationError("参数错误: " + err.Error()),
				})
				return
			}

			template, err := templateModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"code": 404,
					"msg":  "模板不存在",
				})
				return
			}

			if err := templateModel.SetFeatured(id, req.IsFeatured); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "操作失败: " + err.Error(),
				})
				return
			}

			template.IsFeatured = req.IsFeatured
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "操作成功",
				"data": template,
			})
		})

		// 获取精选案例列表（管理后台）
		templates.GET("/featured", func(c *gin.Context) {
			limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
			if limit < 1 {
				limit = 20
			}
			if limit > 500 {
				limit = 500
			}

			templateList, err := templateModel.GetFeaturedTemplates(limit)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"code": 500,
					"msg":  "获取精选案例列表失败: " + err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list": templateList,
				},
			})
		})

		// ---------- 精选案例组管理 ----------
		featuredGroups := templates.Group("/featured-groups")
		{
			// 获取精选案例组列表
			featuredGroups.GET("", func(c *gin.Context) {
				page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
				pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
				if page < 1 {
					page = 1
				}
				if pageSize < 1 {
					pageSize = 20
				}
				offset := (page - 1) * pageSize

				groups, err := featuredCaseGroupModel.List(pageSize, offset)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"code": 500,
						"msg":  "获取精选案例组列表失败: " + err.Error(),
					})
					return
				}

				total, err := featuredCaseGroupModel.Count()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"code": 500,
						"msg":  "获取总数失败: " + err.Error(),
					})
					return
				}

				// 获取每个组的案例详情
				var result []gin.H
				for _, group := range groups {
					groupData := gin.H{
						"id":           group.ID,
						"name":         group.Name,
						"display_mode": group.DisplayMode,
						"case1_id":     group.Case1ID,
						"case2_id":     group.Case2ID,
						"case1_label":  group.Case1Label,
						"case2_label":  group.Case2Label,
						"sort_order":   group.SortOrder,
						"created_at":   group.CreatedAt,
						"updated_at":   group.UpdatedAt,
					}

					// 获取第一个案例
					case1, err := templateModel.GetByID(group.Case1ID)
					if err == nil && case1 != nil {
						groupData["case1"] = case1
					}

					// 获取第二个案例（如果存在）
					if group.Case2ID > 0 {
						case2, err := templateModel.GetByID(group.Case2ID)
						if err == nil && case2 != nil {
							groupData["case2"] = case2
						}
					}

					result = append(result, groupData)
				}

				c.JSON(http.StatusOK, gin.H{
					"code": 0,
					"msg":  "success",
					"data": gin.H{
						"list":      result,
						"total":     total,
						"page":      page,
						"page_size": pageSize,
					},
				})
			})

			// 获取精选案例组详情
			featuredGroups.GET("/:id", func(c *gin.Context) {
				id, err := strconv.ParseInt(c.Param("id"), 10, 64)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  "无效的组ID",
					})
					return
				}

				group, err := featuredCaseGroupModel.GetByID(id)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{
						"code": 404,
						"msg":  "精选案例组不存在",
					})
					return
				}

				groupData := gin.H{
					"id":           group.ID,
					"name":         group.Name,
					"display_mode": group.DisplayMode,
					"case1_id":     group.Case1ID,
					"case2_id":     group.Case2ID,
					"case1_label":  group.Case1Label,
					"case2_label":  group.Case2Label,
					"sort_order":   group.SortOrder,
					"created_at":   group.CreatedAt,
					"updated_at":   group.UpdatedAt,
				}

				// 获取第一个案例
				case1, err := templateModel.GetByID(group.Case1ID)
				if err == nil && case1 != nil {
					groupData["case1"] = case1
				}

				// 获取第二个案例（如果存在）
				if group.Case2ID > 0 {
					case2, err := templateModel.GetByID(group.Case2ID)
					if err == nil && case2 != nil {
						groupData["case2"] = case2
					}
				}

				c.JSON(http.StatusOK, gin.H{
					"code": 0,
					"msg":  "success",
					"data": groupData,
				})
			})

			// 创建精选案例组
			featuredGroups.POST("", func(c *gin.Context) {
				var req struct {
					Name        string `json:"name" binding:"required"`
					DisplayMode string `json:"display_mode" binding:"required,oneof=comparison side_by_side normal"`
					Case1ID     int64  `json:"case1_id" binding:"required"`
					Case2ID     int64  `json:"case2_id"`
					Case1Label  string `json:"case1_label"`
					Case2Label  string `json:"case2_label"`
					SortOrder   int    `json:"sort_order"`
				}
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  FormatValidationError("参数错误: " + err.Error()),
					})
					return
				}

				// 验证案例是否存在
				case1, err := templateModel.GetByID(req.Case1ID)
				if err != nil || case1 == nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  "第一个案例不存在",
					})
					return
				}

				if req.Case2ID > 0 {
					case2, err := templateModel.GetByID(req.Case2ID)
					if err != nil || case2 == nil {
						c.JSON(http.StatusBadRequest, gin.H{
							"code": 400,
							"msg":  "第二个案例不存在",
						})
						return
					}
				}

				// 设置默认标签
				if req.Case1Label == "" {
					req.Case1Label = "真实"
				}
				if req.Case2Label == "" {
					req.Case2Label = "AI"
				}

				group := &model.FeaturedCaseGroup{
					Name:        req.Name,
					DisplayMode: req.DisplayMode,
					Case1ID:     req.Case1ID,
					Case2ID:     req.Case2ID,
					Case1Label:  req.Case1Label,
					Case2Label:  req.Case2Label,
					SortOrder:   req.SortOrder,
				}

				if err := featuredCaseGroupModel.Create(group); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"code": 500,
						"msg":  "创建精选案例组失败: " + err.Error(),
					})
					return
				}

				c.JSON(http.StatusOK, gin.H{
					"code": 0,
					"msg":  "创建成功",
					"data": group,
				})
			})

			// 更新精选案例组
			featuredGroups.PUT("/:id", func(c *gin.Context) {
				id, err := strconv.ParseInt(c.Param("id"), 10, 64)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  "无效的组ID",
					})
					return
				}

				var req struct {
					Name        string `json:"name" binding:"required"`
					DisplayMode string `json:"display_mode" binding:"required,oneof=comparison side_by_side normal"`
					Case1ID     int64  `json:"case1_id" binding:"required"`
					Case2ID     int64  `json:"case2_id"`
					Case1Label  string `json:"case1_label"`
					Case2Label  string `json:"case2_label"`
					SortOrder   int    `json:"sort_order"`
				}
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  FormatValidationError("参数错误: " + err.Error()),
					})
					return
				}

				group, err := featuredCaseGroupModel.GetByID(id)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{
						"code": 404,
						"msg":  "精选案例组不存在",
					})
					return
				}

				// 验证案例是否存在
				case1, err := templateModel.GetByID(req.Case1ID)
				if err != nil || case1 == nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  "第一个案例不存在",
					})
					return
				}

				if req.Case2ID > 0 {
					case2, err := templateModel.GetByID(req.Case2ID)
					if err != nil || case2 == nil {
						c.JSON(http.StatusBadRequest, gin.H{
							"code": 400,
							"msg":  "第二个案例不存在",
						})
						return
					}
				}

				// 设置默认标签
				if req.Case1Label == "" {
					req.Case1Label = "真实"
				}
				if req.Case2Label == "" {
					req.Case2Label = "AI"
				}

				group.Name = req.Name
				group.DisplayMode = req.DisplayMode
				group.Case1ID = req.Case1ID
				group.Case2ID = req.Case2ID
				group.Case1Label = req.Case1Label
				group.Case2Label = req.Case2Label
				group.SortOrder = req.SortOrder

				if err := featuredCaseGroupModel.Update(group); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"code": 500,
						"msg":  "更新精选案例组失败: " + err.Error(),
					})
					return
				}

				c.JSON(http.StatusOK, gin.H{
					"code": 0,
					"msg":  "更新成功",
					"data": group,
				})
			})

			// 删除精选案例组
			featuredGroups.DELETE("/:id", func(c *gin.Context) {
				id, err := strconv.ParseInt(c.Param("id"), 10, 64)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{
						"code": 400,
						"msg":  "无效的组ID",
					})
					return
				}

				if err := featuredCaseGroupModel.Delete(id); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{
						"code": 500,
						"msg":  "删除精选案例组失败: " + err.Error(),
					})
					return
				}

				c.JSON(http.StatusOK, gin.H{
					"code": 0,
					"msg":  "删除成功",
				})
			})
		}
	}
}
