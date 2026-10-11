package route

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
)

type aiToolManagementRequest struct {
	Code              string                        `json:"code" binding:"required"`
	Name              string                        `json:"name" binding:"required"`
	Category          string                        `json:"category" binding:"required,oneof=architecture interior landscape planning"`
	ShortDescription  string                        `json:"short_description"`
	DetailDescription string                        `json:"detail_description"`
	ListCoverImage    string                        `json:"list_cover_image"`
	DetailBeforeImage string                        `json:"detail_before_image"`
	DetailAfterImage  string                        `json:"detail_after_image"`
	PromptPlaceholder string                        `json:"prompt_placeholder"`
	DefaultPrompt     string                        `json:"default_prompt"`
	UploadHint        string                        `json:"upload_hint"`
	ShowUsageTips     bool                          `json:"show_usage_tips"`
	UsageTipsTitle    string                        `json:"usage_tips_title"`
	UsageTipsContent  string                        `json:"usage_tips_content"`
	SortOrder         int                           `json:"sort_order"`
	IsPublished       bool                          `json:"is_published"`
	IsCommon          bool                          `json:"is_common"`
	PresetReferences  []model.AIToolPresetReference `json:"preset_references"`
	StylePresets      []model.AIToolStylePreset     `json:"style_presets"`
	// Note 草稿/发布备注（写入版本记录，不属于生效内容）
	Note              string                        `json:"note"`
}

func normalizeAIToolManagementRequest(req *aiToolManagementRequest) {
	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	req.Category = strings.TrimSpace(req.Category)
	req.ShortDescription = strings.TrimSpace(req.ShortDescription)
	req.DetailDescription = strings.TrimSpace(req.DetailDescription)
	req.ListCoverImage = strings.TrimSpace(req.ListCoverImage)
	req.DetailBeforeImage = strings.TrimSpace(req.DetailBeforeImage)
	req.DetailAfterImage = strings.TrimSpace(req.DetailAfterImage)
	req.PromptPlaceholder = strings.TrimSpace(req.PromptPlaceholder)
	req.DefaultPrompt = strings.TrimSpace(req.DefaultPrompt)
	req.UploadHint = strings.TrimSpace(req.UploadHint)
	req.UsageTipsTitle = strings.TrimSpace(req.UsageTipsTitle)
	req.UsageTipsContent = strings.TrimSpace(req.UsageTipsContent)
	if req.UsageTipsTitle == "" {
		req.UsageTipsTitle = "使用提示"
	}
	for index := range req.PresetReferences {
		req.PresetReferences[index].ID = strings.TrimSpace(req.PresetReferences[index].ID)
		req.PresetReferences[index].Name = strings.TrimSpace(req.PresetReferences[index].Name)
		req.PresetReferences[index].Description = strings.TrimSpace(req.PresetReferences[index].Description)
		req.PresetReferences[index].ImageURL = strings.TrimSpace(req.PresetReferences[index].ImageURL)
		req.PresetReferences[index].PromptSuffix = strings.TrimSpace(req.PresetReferences[index].PromptSuffix)
	}
	for index := range req.StylePresets {
		req.StylePresets[index].ID = strings.TrimSpace(req.StylePresets[index].ID)
		req.StylePresets[index].Name = strings.TrimSpace(req.StylePresets[index].Name)
		req.StylePresets[index].ReferenceMode = strings.TrimSpace(req.StylePresets[index].ReferenceMode)
		req.StylePresets[index].PreviewImageURL = strings.TrimSpace(req.StylePresets[index].PreviewImageURL)
		req.StylePresets[index].ModelReferenceImageURL = strings.TrimSpace(req.StylePresets[index].ModelReferenceImageURL)
		req.StylePresets[index].ImageURL = strings.TrimSpace(req.StylePresets[index].ImageURL)
		if req.StylePresets[index].ReferenceMode == "" {
			if req.StylePresets[index].ImageURL == "" && req.StylePresets[index].ModelReferenceImageURL == "" {
				req.StylePresets[index].ReferenceMode = model.AIToolReferenceModePreviewOnly
			} else {
				req.StylePresets[index].ReferenceMode = model.AIToolReferenceModePreviewAndModel
			}
		}
		if req.StylePresets[index].ReferenceMode != model.AIToolReferenceModePreviewOnly {
			req.StylePresets[index].ReferenceMode = model.AIToolReferenceModePreviewAndModel
		}
		if req.StylePresets[index].PreviewImageURL == "" {
			req.StylePresets[index].PreviewImageURL = req.StylePresets[index].ImageURL
		}
		if req.StylePresets[index].ReferenceMode == model.AIToolReferenceModePreviewAndModel && req.StylePresets[index].ModelReferenceImageURL == "" {
			req.StylePresets[index].ModelReferenceImageURL = req.StylePresets[index].ImageURL
		}
		if req.StylePresets[index].ImageURL == "" {
			req.StylePresets[index].ImageURL = req.StylePresets[index].PreviewImageURL
		}
		req.StylePresets[index].PromptSuffix = strings.TrimSpace(req.StylePresets[index].PromptSuffix)
	}
}

func buildAIToolFromManagementRequest(req *aiToolManagementRequest) (*model.AITool, error) {
	presetReferences, err := json.Marshal(req.PresetReferences)
	if err != nil {
		return nil, err
	}
	stylePresets, err := json.Marshal(req.StylePresets)
	if err != nil {
		return nil, err
	}
	return &model.AITool{
		Code:                  req.Code,
		Name:                  req.Name,
		Category:              req.Category,
		ShortDescription:      req.ShortDescription,
		DetailDescription:     req.DetailDescription,
		ListCoverImage:        req.ListCoverImage,
		DetailBeforeImage:     req.DetailBeforeImage,
		DetailAfterImage:      req.DetailAfterImage,
		PromptPlaceholder:     req.PromptPlaceholder,
		DefaultPrompt:         req.DefaultPrompt,
		UploadHint:            req.UploadHint,
		ShowUsageTips:         req.ShowUsageTips,
		UsageTipsTitle:        req.UsageTipsTitle,
		UsageTipsContent:      req.UsageTipsContent,
		SortOrder:             req.SortOrder,
		IsPublished:           req.IsPublished,
		IsCommon:              req.IsCommon,
		PresetReferenceImages: string(presetReferences),
		StylePresetsRaw:       string(stylePresets),
	}, nil
}

func validateImageURL(imageURL string, fieldName string) string {
	if imageURL == "" {
		return ""
	}
	parsedURL, err := url.Parse(imageURL)
	if err != nil {
		return fieldName + "不是有效的URL"
	}
	if parsedURL.Scheme != "https" {
		return fieldName + "必须使用HTTPS协议"
	}
	return ""
}

func validateAIToolManagementRequest(req *aiToolManagementRequest) string {
	if req.Code == "" {
		return "工具编码不能为空"
	}
	if req.Name == "" {
		return "工具名称不能为空"
	}
	if len(req.Name) > 100 {
		return "工具名称不能超过100个字符"
	}
	if len(req.ShortDescription) > 200 {
		return "工具短描述不能超过200个字符"
	}
	if len(req.DetailDescription) > 2000 {
		return "工具详细描述不能超过2000个字符"
	}
	if len(req.DefaultPrompt) > 4000 {
		return "默认提示词不能超过4000个字符"
	}
	if len(req.UsageTipsTitle) > 128 {
		return "使用提示标题不能超过128个字符"
	}
	if len(req.UsageTipsContent) > 4000 {
		return "使用提示内容不能超过4000个字符"
	}
	// 校验图片URL必须使用HTTPS
	if errMsg := validateImageURL(req.ListCoverImage, "列表封面图"); errMsg != "" {
		return errMsg
	}
	if errMsg := validateImageURL(req.DetailBeforeImage, "详情前图"); errMsg != "" {
		return errMsg
	}
	if errMsg := validateImageURL(req.DetailAfterImage, "详情后图"); errMsg != "" {
		return errMsg
	}
	presetIDs := make(map[string]struct{})
	for _, item := range req.PresetReferences {
		if item.ID == "" || item.Name == "" {
			return "预设参考图的ID和名称不能为空"
		}
		if errMsg := validateImageURL(item.ImageURL, "预设参考图["+item.Name+"]的图片URL"); errMsg != "" {
			return errMsg
		}
		if _, exists := presetIDs[item.ID]; exists {
			return "预设参考图ID不能重复"
		}
		presetIDs[item.ID] = struct{}{}
	}
	styleIDs := make(map[string]struct{})
	for _, item := range req.StylePresets {
		if item.ID == "" || item.Name == "" {
			return "风格项的ID和名称不能为空"
		}
		if item.ReferenceMode != model.AIToolReferenceModePreviewOnly && item.ReferenceMode != model.AIToolReferenceModePreviewAndModel {
			return "风格项[" + item.Name + "]的模式无效"
		}
		if errMsg := validateImageURL(item.PreviewImageURL, "风格项["+item.Name+"]的用户预览图URL"); errMsg != "" {
			return errMsg
		}
		if errMsg := validateImageURL(item.ModelReferenceImageURL, "风格项["+item.Name+"]的AI引用图URL"); errMsg != "" {
			return errMsg
		}
		if errMsg := validateImageURL(item.ImageURL, "风格项["+item.Name+"]的旧版图片URL"); errMsg != "" {
			return errMsg
		}
		if item.ReferenceMode == model.AIToolReferenceModePreviewAndModel && strings.TrimSpace(item.ModelReferenceImageURL) == "" {
			return "风格项[" + item.Name + "]选择“用户预览 + AI引用”时必须配置AI引用图"
		}
		if _, exists := styleIDs[item.ID]; exists {
			return "风格项ID不能重复"
		}
		styleIDs[item.ID] = struct{}{}
	}
	return ""
}

// ───────────────────────── 草稿-发布分离（二期批次2） ─────────────────────────
// 管理端保存只写草稿快照（ai_tool_versions.status='draft'）；
// 发布 = 校验后把快照应用到 ai_tools 行（小程序读路径零改动）并落 published 版本。

// aiToolSnapshotFromRequest 把管理端请求序列化为草稿快照 JSON。
// Note 只进版本记录不进快照：备注变化不应造成"有未发布修改"误报。
func aiToolSnapshotFromRequest(req *aiToolManagementRequest) (string, error) {
	payload := *req
	payload.Note = ""
	if payload.PresetReferences == nil {
		payload.PresetReferences = []model.AIToolPresetReference{}
	}
	if payload.StylePresets == nil {
		payload.StylePresets = []model.AIToolStylePreset{}
	}
	data, err := json.Marshal(payload)
	return string(data), err
}

// aiToolRequestFromSnapshot 解析草稿快照回管理端请求结构
func aiToolRequestFromSnapshot(snapshot string) (*aiToolManagementRequest, error) {
	req := &aiToolManagementRequest{}
	if err := json.Unmarshal([]byte(snapshot), req); err != nil {
		return nil, err
	}
	return req, nil
}

// applyAIToolRequestToRow 校验并把请求内容应用到 ai_tools 行（发布/回滚共用的唯一写入口）
func applyAIToolRequestToRow(aiToolModel *model.AIToolModel, toolID int64, req *aiToolManagementRequest) (*model.AITool, error) {
	normalizeAIToolManagementRequest(req)
	if validateMsg := validateAIToolManagementRequest(req); validateMsg != "" {
		return nil, fmt.Errorf("%s", validateMsg)
	}
	tool, err := buildAIToolFromManagementRequest(req)
	if err != nil {
		return nil, err
	}
	storedTool, err := aiToolModel.GetByID(toolID)
	if err != nil {
		return nil, err
	}
	tool.ID = storedTool.ID
	tool.UsageCount = storedTool.UsageCount
	tool.CreatedAt = storedTool.CreatedAt
	if err := aiToolModel.Update(tool); err != nil {
		return nil, err
	}
	return tool, nil
}

// publishAIToolDraft 发布草稿：应用到行 + 落版本（发布/回滚共用；snapshot 为要发布的内容）
func publishAIToolDraft(aiToolModel *model.AIToolModel, versionModel *model.AIToolVersionModel, toolID int64, req *aiToolManagementRequest, operator, note string) (*model.AIToolVersion, error) {
	snapshot, err := aiToolSnapshotFromRequest(req)
	if err != nil {
		return nil, err
	}
	if _, err := applyAIToolRequestToRow(aiToolModel, toolID, req); err != nil {
		return nil, err
	}
	return versionModel.RecordPublish(toolID, snapshot, operator, note)
}

func RegisterAIToolManagementRoutes(r *gin.RouterGroup, aiToolModel *model.AIToolModel, taskModel *model.AITaskModel) {
	// 工具版本表（草稿-发布分离）：建表 fail-soft，失败则保存行为回退为旧的"直接更新行"
	versionModel := model.NewAIToolVersionModel(aiToolModel.DB)
	if err := versionModel.InitTable(); err != nil {
		versionModel = nil
		fmt.Printf("[WARN] AI工具版本表初始化失败（保存将直接更新行，不走草稿）: %v\n", err)
	}
	tools := r.Group("/ai-tools")
	{
		tools.GET("", func(c *gin.Context) {
			category := strings.TrimSpace(c.Query("category"))
			keyword := strings.TrimSpace(c.Query("keyword"))
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			if page < 1 {
				page = 1
			}
			if pageSize < 1 {
				pageSize = 20
			}
			if pageSize > 100 {
				pageSize = 100
			}
			offset := (page - 1) * pageSize

			var isPublished *bool
			if publishedStr := strings.TrimSpace(c.Query("is_published")); publishedStr != "" {
				published := publishedStr == "1" || strings.EqualFold(publishedStr, "true")
				isPublished = &published
			}

			list, err := aiToolModel.List(category, keyword, isPublished, pageSize, offset)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取 AI 工具列表失败: " + err.Error()})
				return
			}
			total, err := aiToolModel.Count(category, keyword, isPublished)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取 AI 工具总数失败: " + err.Error()})
				return
			}
			// 运营数据：近7日调用 / 成功率 / 贡献灵石 / 逐日趋势（任务表聚合，仅成功任务计收入）
			usageStats := make(map[int64]*model.AIToolUsageStat)
			if taskModel != nil {
				stats, statsErr := taskModel.ToolUsageStats(time.Now().AddDate(0, 0, -7), 7)
				if statsErr == nil {
					usageStats = stats
				}
			}
			// 草稿状态徽章：has_draft=有草稿；draft_differs=草稿与线上不同（待发布）
			hasDraftMap, differsMap := map[int64]bool{}, map[int64]bool{}
			if versionModel != nil {
				if h, d, draftErr := versionModel.DraftStateMap(); draftErr == nil {
					hasDraftMap, differsMap = h, d
				}
			}
			responseList := make([]gin.H, 0, len(list))
			for _, item := range list {
				entry := buildAIToolManagementResponse(item)
				if stat, ok := usageStats[item.ID]; ok {
					entry["usage_7d"] = stat.Total
					entry["success_7d"] = stat.Success
					entry["failed_rate_7d"] = stat.FailedRate
					entry["stones_7d"] = stat.Stones
					entry["daily_series_7d"] = stat.DailySeries
				} else {
					entry["usage_7d"] = int64(0)
					entry["success_7d"] = int64(0)
					entry["failed_rate_7d"] = float64(0)
					entry["stones_7d"] = int64(0)
					entry["daily_series_7d"] = []int64{}
				}
				entry["has_draft"] = hasDraftMap[item.ID]
				entry["draft_differs"] = differsMap[item.ID]
				responseList = append(responseList, entry)
			}
			c.JSON(http.StatusOK, gin.H{
				"code": 0,
				"msg":  "success",
				"data": gin.H{
					"list":      responseList,
					"total":     total,
					"page":      page,
					"page_size": pageSize,
				},
			})
		})

		tools.GET("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
				return
			}
			tool, err := aiToolModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "工具不存在"})
				return
			}
			response := buildAIToolManagementResponse(tool)
			// 编辑用详情：有草稿则展示草稿态（线上行不受影响），并标注来源
			if versionModel != nil {
				if draft, draftErr := versionModel.GetDraft(id); draftErr == nil && draft != nil {
					if draftReq, parseErr := aiToolRequestFromSnapshot(draft.Snapshot); parseErr == nil {
						if draftTool, buildErr := buildAIToolFromManagementRequest(draftReq); buildErr == nil {
							draftTool.ID = tool.ID
							draftTool.UsageCount = tool.UsageCount
							draftTool.CreatedAt = tool.CreatedAt
							draftTool.UpdatedAt = tool.UpdatedAt
							response = buildAIToolManagementResponse(draftTool)
						}
					}
					response["is_draft"] = true
					response["draft_note"] = draft.Note
				} else {
					response["is_draft"] = false
				}
				latest, latestErr := versionModel.GetLatestPublished(id)
				if latestErr == nil && latest != nil {
					response["latest_version_no"] = latest.VersionNo
				}
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": response})
		})

		tools.POST("", func(c *gin.Context) {
			var req aiToolManagementRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			normalizeAIToolManagementRequest(&req)
			if validateMsg := validateAIToolManagementRequest(&req); validateMsg != "" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": validateMsg})
				return
			}
			tool, err := buildAIToolFromManagementRequest(&req)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "工具配置序列化失败: " + err.Error()})
				return
			}
			if err := aiToolModel.Create(tool); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "创建失败: " + err.Error()})
				return
			}
			operator := GetUsername(c)
			// 新建工具：初始内容落草稿；勾选"发布"则立即发布 v1（对齐旧的即时可见语义）
			if versionModel != nil {
				snapshot, snapErr := aiToolSnapshotFromRequest(&req)
				if snapErr == nil {
					_ = versionModel.SaveDraft(tool.ID, snapshot, operator, "初始创建")
					if req.IsPublished {
						if _, pubErr := publishAIToolDraft(aiToolModel, versionModel, tool.ID, &req, operator, "初始创建并发布"); pubErr != nil {
							fmt.Printf("[WARN] AI工具初始发布失败（工具保留为草稿态）: id=%d, err=%v\n", tool.ID, pubErr)
						}
					}
				}
			}
			createdTool, err := aiToolModel.GetByID(tool.ID)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": buildAIToolManagementResponse(tool)})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": buildAIToolManagementResponse(createdTool)})
		})

		tools.PUT("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
				return
			}
			storedTool, err := aiToolModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "工具不存在"})
				return
			}
			var req aiToolManagementRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			normalizeAIToolManagementRequest(&req)
			if validateMsg := validateAIToolManagementRequest(&req); validateMsg != "" {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": validateMsg})
				return
			}
			// 二期批次2：保存只写草稿，线上行不受影响；发布走 POST /:id/publish。
			// 版本表不可用时回退旧行为（直接更新行），保证降级可用。
			if versionModel != nil {
				snapshot, snapErr := aiToolSnapshotFromRequest(&req)
				if snapErr != nil {
					c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "工具配置序列化失败: " + snapErr.Error()})
					return
				}
				if err := versionModel.SaveDraft(id, snapshot, GetUsername(c), req.Note); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "保存草稿失败: " + err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已保存为草稿，线上配置未变化；点击「发布」后生效", "data": gin.H{"id": id, "saved_as": "draft"}})
				return
			}
			tool, err := buildAIToolFromManagementRequest(&req)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "工具配置序列化失败: " + err.Error()})
				return
			}
			tool.ID = storedTool.ID
			tool.UsageCount = storedTool.UsageCount
			tool.CreatedAt = storedTool.CreatedAt
			tool.UpdatedAt = storedTool.UpdatedAt
			if err := aiToolModel.Update(tool); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "更新失败: " + err.Error()})
				return
			}
			updatedTool, err := aiToolModel.GetByID(id)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": buildAIToolManagementResponse(tool)})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": buildAIToolManagementResponse(updatedTool)})
		})

		// 发布草稿：把草稿快照应用到 ai_tools 行（小程序即时生效）并落 published 版本
		tools.POST("/:id/publish", func(c *gin.Context) {
			if versionModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "版本服务不可用"})
				return
			}
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
				return
			}
			if _, err := aiToolModel.GetByID(id); err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "工具不存在"})
				return
			}
			var req struct {
				Note string `json:"note"`
			}
			_ = c.ShouldBindJSON(&req)
			draft, draftErr := versionModel.GetDraft(id)
			if draftErr != nil || draft == nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "该工具没有待发布的草稿，请先保存修改"})
				return
			}
			draftReq, parseErr := aiToolRequestFromSnapshot(draft.Snapshot)
			if parseErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "草稿快照解析失败: " + parseErr.Error()})
				return
			}
			version, pubErr := publishAIToolDraft(aiToolModel, versionModel, id, draftReq, GetUsername(c), req.Note)
			if pubErr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "发布失败: " + pubErr.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "发布成功，线上已生效", "data": gin.H{
				"version_no": version.VersionNo, "published_at": version.PublishedAt,
			}})
		})

		// 草稿与线上的差异（发布前确认弹窗）
		tools.GET("/:id/diff", func(c *gin.Context) {
			if versionModel == nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"items": []gin.H{}, "has_draft": false}})
				return
			}
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
				return
			}
			draft, draftErr := versionModel.GetDraft(id)
			if draftErr != nil || draft == nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{"items": []gin.H{}, "has_draft": false}})
				return
			}
			var draftMap map[string]interface{}
			_ = json.Unmarshal([]byte(draft.Snapshot), &draftMap)
			var publishedMap map[string]interface{}
			latest, latestErr := versionModel.GetLatestPublished(id)
			if latestErr == nil && latest != nil {
				_ = json.Unmarshal([]byte(latest.Snapshot), &publishedMap)
			}
			items := model.DiffAIToolSnapshots(publishedMap, draftMap)
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": gin.H{
				"has_draft": true, "items": items, "draft_note": draft.Note,
				"published_version_no": latestVersionNo(latest),
			}})
		})

		// 版本列表 + 效果对比（每个版本发布后 7 日内 vs 发布前 7 日）
		tools.GET("/:id/versions", func(c *gin.Context) {
			if versionModel == nil {
				c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": []gin.H{}})
				return
			}
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
				return
			}
			limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
			versions, listErr := versionModel.ListVersions(id, limit)
			if listErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "获取版本列表失败: " + listErr.Error()})
				return
			}
			result := make([]gin.H, 0, len(versions))
			for index, version := range versions {
				entry := gin.H{
					"id": version.ID, "version_no": version.VersionNo, "status": version.Status,
					"note": version.Note, "created_by": version.CreatedBy,
					"created_at": version.CreatedAt, "published_at": version.PublishedAt,
				}
				// 效果对比：[本版发布, 下版发布) 为 after 窗口；[发布-7d, 发布) 为 before 窗口
				if taskModel != nil && version.PublishedAt != nil {
					publishedAt := *version.PublishedAt
					windowEnd := time.Now()
					if index > 0 && versions[index-1].PublishedAt != nil {
						windowEnd = *versions[index-1].PublishedAt
					}
					after, afterErr := taskModel.ToolWindowStats(id, publishedAt, windowEnd)
					before, beforeErr := taskModel.ToolWindowStats(id, publishedAt.AddDate(0, 0, -7), publishedAt)
					if afterErr == nil && beforeErr == nil {
						entry["stats"] = gin.H{
							"after":  gin.H{"total": after.Total, "success_rate": successRateOf(after)},
							"before": gin.H{"total": before.Total, "success_rate": successRateOf(before)},
						}
					}
				}
				result = append(result, entry)
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success", "data": result})
		})

		// 回滚：用历史版本快照再发布一次（同样留痕）
		tools.POST("/:id/rollback", func(c *gin.Context) {
			if versionModel == nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "版本服务不可用"})
				return
			}
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
				return
			}
			var req struct {
				VersionID int64 `json:"version_id" binding:"required"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": FormatValidationError("参数错误: " + err.Error())})
				return
			}
			version, versionErr := versionModel.GetVersion(req.VersionID)
			if versionErr != nil || version == nil || version.ToolID != id {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "版本不存在"})
				return
			}
			versionReq, parseErr := aiToolRequestFromSnapshot(version.Snapshot)
			if parseErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "版本快照解析失败: " + parseErr.Error()})
				return
			}
			note := fmt.Sprintf("回滚到 v%d（%s）", version.VersionNo, firstNonEmpty(version.Note, "历史版本"))
			newVersion, pubErr := publishAIToolDraft(aiToolModel, versionModel, id, versionReq, GetUsername(c), note)
			if pubErr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "回滚失败: " + pubErr.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "回滚成功，线上已恢复为 v" + strconv.FormatInt(version.VersionNo, 10) + " 的配置", "data": gin.H{
				"version_no": newVersion.VersionNo, "rolled_back_to": version.VersionNo,
			}})
		})

		tools.DELETE("/:id", func(c *gin.Context) {
			id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
			if err != nil || id <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "无效的ID"})
				return
			}
			if taskModel != nil {
				relatedTaskCount, countErr := taskModel.CountByToolID(id)
				if countErr != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "检查工具关联任务失败: " + countErr.Error()})
					return
				}
				if relatedTaskCount > 0 {
					c.JSON(http.StatusConflict, gin.H{"code": 409, "msg": "该工具已有关联生成记录，不能直接删除，请先下线"})
					return
				}
			}
			if err := aiToolModel.Delete(id); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "删除失败: " + err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
		})
	}
}

// successRateOf 窗口成功率（无调用返回 -1 表示无样本）
func successRateOf(stat model.ToolWindowStat) float64 {
	if stat.Total <= 0 {
		return -1
	}
	return float64(stat.Success) / float64(stat.Total)
}

// firstNonEmpty 已在 favorite.go 声明，此处直接复用

func latestVersionNo(version *model.AIToolVersion) int64 {
	if version == nil {
		return 0
	}
	return version.VersionNo
}
