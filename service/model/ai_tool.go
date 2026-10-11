package model

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type AIToolPresetReference struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	ImageURL     string `json:"image_url"`
	PromptSuffix string `json:"prompt_suffix"`
}

type AIToolStylePreset struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	ReferenceMode          string `json:"reference_mode"`
	PreviewImageURL        string `json:"preview_image_url"`
	ModelReferenceImageURL string `json:"model_reference_image_url"`
	ImageURL               string `json:"image_url"` // 兼容旧数据：历史上同时用于前台预览和 AI 引用
	PromptSuffix           string `json:"prompt_suffix"`
}

const (
	AIToolReferenceModePreviewOnly     = "preview_only"
	AIToolReferenceModePreviewAndModel = "preview_and_model"
)

const (
	legacyAIToolUploadHint     = "请先上传 1 张原图，再选择 1 张预设参考图或上传 1 张自定义参考图。"
	defaultAIToolUploadHint    = "请先上传 1 张待处理图片，再选择一个风格方向；标记为图像参考的风格会同时传入 AI 分析。"
	legacyAIToolUsageRuleText  = "系统会自动带入该工具对应的默认提示词和参考风格规则，你只需要补充自己的要求。"
	defaultAIToolUsageRuleText = "系统会结合工具默认提示词、所选风格卡片和你的补充要求生成；标记为图像参考的风格会同时参考示例图。"
	commonAIToolUsageText      = "这是当前分类下优先推荐的常用工具。"
	extendedAIToolUsageText    = "这是当前分类下的扩展工具，适合探索更多表达方向。"
)

type AITool struct {
	ID                    int64     `json:"id"`
	Code                  string    `json:"code"`
	Name                  string    `json:"name"`
	Category              string    `json:"category"`
	ShortDescription      string    `json:"short_description"`
	DetailDescription     string    `json:"detail_description"`
	ListCoverImage        string    `json:"list_cover_image"`
	DetailBeforeImage     string    `json:"detail_before_image"`
	DetailAfterImage      string    `json:"detail_after_image"`
	PromptPlaceholder     string    `json:"prompt_placeholder"`
	DefaultPrompt         string    `json:"default_prompt"`
	UploadHint            string    `json:"upload_hint"`
	ShowUsageTips         bool      `json:"show_usage_tips"`
	UsageTipsTitle        string    `json:"usage_tips_title"`
	UsageTipsContent      string    `json:"usage_tips_content"`
	SortOrder             int       `json:"sort_order"`
	IsPublished           bool      `json:"is_published"`
	IsCommon              bool      `json:"is_common"`
	UsageCount            int64     `json:"usage_count"`
	PresetReferenceImages string    `json:"-"`
	StylePresetsRaw       string    `json:"-"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type AIToolModel struct {
	DB *sql.DB
}

type aiToolExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

type aiToolSeed struct {
	Code              string
	Name              string
	Category          string
	ShortDescription  string
	DetailDescription string
	Common            bool
	SortOrder         int
}

var defaultAIToolPresetReferenceMap = map[string][]AIToolPresetReference{
	"architecture": {
		{ID: "arch-ref-1", Name: "新中式外观参考", Description: "偏温润材质与层次立面表达", PromptSuffix: "新中式建筑表达，层次清晰，材质真实，暖色光影"},
		{ID: "arch-ref-2", Name: "现代建筑参考", Description: "偏简洁比例与干净立面气质", PromptSuffix: "现代建筑气质，比例利落，立面干净，空间秩序明确"},
	},
	"interior": {
		{ID: "interior-ref-1", Name: "奶油室内参考", Description: "偏柔和材质与温暖氛围", PromptSuffix: "室内空间柔和奶油风，材质细腻，氛围舒适"},
		{ID: "interior-ref-2", Name: "现代简约参考", Description: "偏干净收口与清爽布局", PromptSuffix: "现代简约室内，动线清晰，材质统一，光影自然"},
	},
	"landscape": {
		{ID: "landscape-ref-1", Name: "庭院景观参考", Description: "偏植物层次与步道关系", PromptSuffix: "庭院景观表达，植物层次丰富，铺装清晰，空间宜人"},
		{ID: "landscape-ref-2", Name: "夜景氛围参考", Description: "偏灯光与景观层次氛围", PromptSuffix: "夜景景观氛围，灯光柔和，层次分明，空间具有引导感"},
	},
	"planning": {
		{ID: "planning-ref-1", Name: "总平分析参考", Description: "偏总图结构与逻辑表达", PromptSuffix: "总平逻辑清晰，分析结构明确，图面整洁专业"},
		{ID: "planning-ref-2", Name: "规划彩平参考", Description: "偏色彩分区与功能识别", PromptSuffix: "规划彩平表达，分区明确，色彩统一，专业清晰"},
	},
}

var defaultAIToolStylePresetMap = map[string][]AIToolStylePreset{
	"architecture": {
		{ID: "arch-style-1", Name: "专业方案", PromptSuffix: "专业方案表现，适合汇报展示"},
		{ID: "arch-style-2", Name: "写实效果", PromptSuffix: "真实材质与自然光影，贴近落地效果"},
		{ID: "arch-style-3", Name: "竞赛表达", PromptSuffix: "视觉冲击强，概念表达鲜明"},
	},
	"interior": {
		{ID: "interior-style-1", Name: "温暖写实", PromptSuffix: "空间温暖自然，材质柔和真实"},
		{ID: "interior-style-2", Name: "极简清爽", PromptSuffix: "空间克制简洁，构图清晰干净"},
	},
	"landscape": {
		{ID: "landscape-style-1", Name: "日景表现", PromptSuffix: "日景明亮自然，植物层次清楚"},
		{ID: "landscape-style-2", Name: "夜景氛围", PromptSuffix: "夜景灯光氛围明显，空间更具故事感"},
	},
	"planning": {
		{ID: "planning-style-1", Name: "分析表达", PromptSuffix: "分析逻辑突出，结构清楚，利于汇报"},
		{ID: "planning-style-2", Name: "彩平汇报", PromptSuffix: "彩平清晰专业，配色统一，版面利落"},
	},
}

var defaultAIToolSeeds = []aiToolSeed{
	{Code: "new_minnan_tool", Name: "新闽派工具", Category: "architecture", ShortDescription: "用文字快速生成新闽派建筑风格画面", DetailDescription: "面向新闽派建筑风格的专项工具，当前支持文生图，后续预留图生图能力。", Common: true, SortOrder: 5},
	{Code: "masterplan-coloring", Name: "总平填色", Category: "architecture", ShortDescription: "把总平底图快速整理成专业汇报图面", DetailDescription: "适合把总平线稿或草图整理为更清晰、更有层次的表达图。", SortOrder: 10},
	{Code: "plan-coloring", Name: "户型填色", Category: "architecture", ShortDescription: "让平面图更像正式方案汇报效果", DetailDescription: "适合平面功能区表达、色彩区分和快速汇报。", Common: true, SortOrder: 20},
	{Code: "view-conversion", Name: "视角转换", Category: "architecture", ShortDescription: "让同一方案切换不同观察角度", DetailDescription: "适合把已有视角切换成更适合展示的观察方式。", Common: true, SortOrder: 30},
	{Code: "style-transfer", Name: "风格迁移", Category: "architecture", ShortDescription: "保留结构基础上快速切换气质", DetailDescription: "适合探索同一方案在不同风格下的视觉可能性。", Common: true, SortOrder: 40},
	{Code: "multifloor-plan-to-render", Name: "多层平面图转外观效果图", Category: "architecture", ShortDescription: "从平面逻辑快速补成外观方向感", DetailDescription: "适合已有平面逻辑但外观表达还没有展开时使用。", Common: true, SortOrder: 50},
	{Code: "partial-edit", Name: "局部修改", Category: "architecture", ShortDescription: "只改门头、材质、窗型等局部内容", DetailDescription: "适合在不重做整图的情况下做定向细修。", Common: true, SortOrder: 60},
	{Code: "night-scene", Name: "一键夜景图", Category: "architecture", ShortDescription: "快速切换为夜景氛围效果", DetailDescription: "适合方案汇报时补充夜景版本。", SortOrder: 70},
	{Code: "plan-to-view", Name: "平面图转视图", Category: "architecture", ShortDescription: "从平面关系补出更直观的视角图", DetailDescription: "适合前期概念沟通与方向确认。", SortOrder: 80},
	{Code: "perspective-to-elevation", Name: "透视图转立面图", Category: "architecture", ShortDescription: "从透视信息反推更规整的立面表达", DetailDescription: "适合补充立面研究与报规前期表达。", SortOrder: 90},
	{Code: "interior-plan-coloring", Name: "户型填色", Category: "interior", ShortDescription: "让室内平面图更清晰、更利于讲解", DetailDescription: "适合做室内功能分区、动线与氛围表达。", Common: true, SortOrder: 110},
	{Code: "interior-style-transfer", Name: "风格迁移", Category: "interior", ShortDescription: "快速切换室内风格方向", DetailDescription: "适合在同一空间结构基础上尝试多种气质。", Common: true, SortOrder: 120},
	{Code: "plan-to-interior-render", Name: "平面图转效果图", Category: "interior", ShortDescription: "把平面思路快速转成空间感觉", DetailDescription: "适合室内方案初期的方向讨论与体验预览。", Common: true, SortOrder: 130},
	{Code: "interior-night-scene", Name: "夜景效果图", Category: "interior", ShortDescription: "让室内方案快速拥有夜景氛围", DetailDescription: "适合补充灯光方案表达和氛围展示。", SortOrder: 140},
	{Code: "furniture-replace", Name: "物品替换", Category: "interior", ShortDescription: "替换家具、软装和局部摆设", DetailDescription: "适合快速试不同软装搭配与风格方案。", Common: true, SortOrder: 150},
	{Code: "render-to-elevation", Name: "效果图转立面图", Category: "interior", ShortDescription: "从空间透视图整理成立面表达", DetailDescription: "适合补充施工前的表达整理。", SortOrder: 160},
	{Code: "landscape-coloring", Name: "彩平填色", Category: "landscape", ShortDescription: "让景观彩平更统一、更专业", DetailDescription: "适合植物、铺装、水景等信息的图面整理。", Common: true, SortOrder: 210},
	{Code: "landscape-view-conversion", Name: "景观视角转换", Category: "landscape", ShortDescription: "切换不同步行或鸟瞰观察方式", DetailDescription: "适合快速生成更适合展示的景观视角。", Common: true, SortOrder: 220},
	{Code: "landscape-style-transfer", Name: "景观风格迁移", Category: "landscape", ShortDescription: "保留布局基础切换不同景观风格", DetailDescription: "适合在方案稳定后做风格探索。", SortOrder: 230},
	{Code: "landscape-plan-to-render", Name: "景观平面图转效果图", Category: "landscape", ShortDescription: "把平面关系快速变成空间场景", DetailDescription: "适合庭院、公园、景观节点等前期表达。", Common: true, SortOrder: 240},
	{Code: "landscape-partial-edit", Name: "景观局部修改", Category: "landscape", ShortDescription: "只调整节点、植物或铺装局部", DetailDescription: "适合不推翻整体布局的精细化修改。", Common: true, SortOrder: 250},
	{Code: "landscape-night-scene", Name: "景观一键夜景图", Category: "landscape", ShortDescription: "快速补充景观夜景版本", DetailDescription: "适合提升汇报完整度与沉浸氛围。", SortOrder: 260},
	{Code: "landscape-perspective-to-elevation", Name: "景观透视图转立面图", Category: "landscape", ShortDescription: "辅助从透视角度整理节点立面关系", DetailDescription: "适合节点深化前的表达辅助。", SortOrder: 270},
	{Code: "planning-masterplan-coloring", Name: "规划总平面填色", Category: "planning", ShortDescription: "让规划总图更清楚地表达功能分区", DetailDescription: "适合汇报版总平、鸟瞰总图和彩平整理。", Common: true, SortOrder: 310},
	{Code: "planning-analysis", Name: "规划总图分析图", Category: "planning", ShortDescription: "快速生成更易讲解的分析表达", DetailDescription: "适合做结构、动线、功能和策略分析。", Common: true, SortOrder: 320},
	{Code: "planning-view-conversion", Name: "规划视角转换", Category: "planning", ShortDescription: "切换更适合展示规划策略的角度", DetailDescription: "适合从总图延伸出更直观的观察方式。", Common: true, SortOrder: 330},
	{Code: "planning-plan-to-render", Name: "规划平面图转效果图", Category: "planning", ShortDescription: "从平面逻辑转成场景化效果表达", DetailDescription: "适合总体规划、片区概念和前期汇报。", Common: true, SortOrder: 340},
	{Code: "planning-partial-edit", Name: "规划局部修改", Category: "planning", ShortDescription: "只微调局部节点和画面重点", DetailDescription: "适合方向已经明确后的精修调整。", SortOrder: 350},
}

func buildDefaultUsageTipsContent(uploadHint string, isCommon bool) string {
	lines := make([]string, 0, 3)
	trimmedUploadHint := strings.TrimSpace(uploadHint)
	if trimmedUploadHint != "" {
		lines = append(lines, trimmedUploadHint)
	}
	lines = append(lines, defaultAIToolUsageRuleText)
	if isCommon {
		lines = append(lines, commonAIToolUsageText)
	} else {
		lines = append(lines, extendedAIToolUsageText)
	}
	return strings.Join(lines, "\n")
}

func buildLegacyUsageTipsContent(isCommon bool) string {
	lines := []string{legacyAIToolUploadHint, legacyAIToolUsageRuleText}
	if isCommon {
		lines = append(lines, commonAIToolUsageText)
	} else {
		lines = append(lines, extendedAIToolUsageText)
	}
	return strings.Join(lines, "\n")
}

func NewAIToolModel(db *sql.DB) *AIToolModel {
	return &AIToolModel{DB: db}
}

func (m *AIToolModel) InitTable() error {
	schema := `
CREATE TABLE IF NOT EXISTS ai_tools (
	id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	code VARCHAR(128) NOT NULL,
	name VARCHAR(255) NOT NULL,
	category VARCHAR(64) NOT NULL,
	short_description VARCHAR(255) NOT NULL,
	detail_description TEXT,
	list_cover_image VARCHAR(512) DEFAULT '',
	detail_before_image VARCHAR(512) DEFAULT '',
	detail_after_image VARCHAR(512) DEFAULT '',
	preset_reference_images LONGTEXT,
	style_presets LONGTEXT,
	prompt_placeholder VARCHAR(255) DEFAULT '',
	default_prompt TEXT,
	upload_hint VARCHAR(255) DEFAULT '',
	show_usage_tips TINYINT(1) NOT NULL DEFAULT 1,
	usage_tips_title VARCHAR(128) NOT NULL DEFAULT '使用提示',
	usage_tips_content TEXT,
	sort_order INT NOT NULL DEFAULT 0,
	is_published TINYINT(1) NOT NULL DEFAULT 1,
	is_common TINYINT(1) NOT NULL DEFAULT 0,
	usage_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
	UNIQUE KEY uk_code (code),
	INDEX idx_category (category),
	INDEX idx_sort_order (sort_order),
	INDEX idx_is_published (is_published)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`
	if _, err := m.DB.Exec(schema); err != nil {
		return err
	}
	_, _ = m.DB.Exec(`ALTER TABLE ai_tools ADD COLUMN show_usage_tips TINYINT(1) NOT NULL DEFAULT 1`)
	_, _ = m.DB.Exec(`ALTER TABLE ai_tools ADD COLUMN usage_tips_title VARCHAR(128) NOT NULL DEFAULT '使用提示'`)
	_, _ = m.DB.Exec(`ALTER TABLE ai_tools ADD COLUMN usage_tips_content TEXT`)
	_, _ = m.DB.Exec(`UPDATE ai_tools SET usage_tips_title = '使用提示' WHERE COALESCE(TRIM(usage_tips_title), '') = ''`)
	if err := m.migrateAIToolCopywriting(); err != nil {
		return err
	}
	_, _ = m.DB.Exec(`UPDATE ai_tools
		SET usage_tips_content = CONCAT_WS('\n', NULLIF(TRIM(upload_hint), ''), ?, CASE WHEN is_common = 1 THEN ? ELSE ? END)
		WHERE COALESCE(TRIM(usage_tips_content), '') = ''`, defaultAIToolUsageRuleText, commonAIToolUsageText, extendedAIToolUsageText)
	if err := m.seedDefaultsIfEmpty(); err != nil {
		return err
	}
	return m.ensureDefaultSeeds()
}

func (m *AIToolModel) migrateAIToolCopywriting() error {
	if _, err := m.DB.Exec(`UPDATE ai_tools SET upload_hint = ? WHERE TRIM(COALESCE(upload_hint, '')) = ?`, defaultAIToolUploadHint, legacyAIToolUploadHint); err != nil {
		return err
	}
	if _, err := m.DB.Exec(`UPDATE ai_tools SET usage_tips_content = ? WHERE TRIM(COALESCE(usage_tips_content, '')) = ?`, buildDefaultUsageTipsContent(defaultAIToolUploadHint, true), buildLegacyUsageTipsContent(true)); err != nil {
		return err
	}
	if _, err := m.DB.Exec(`UPDATE ai_tools SET usage_tips_content = ? WHERE TRIM(COALESCE(usage_tips_content, '')) = ?`, buildDefaultUsageTipsContent(defaultAIToolUploadHint, false), buildLegacyUsageTipsContent(false)); err != nil {
		return err
	}
	return nil
}

func (m *AIToolModel) seedDefaultsIfEmpty() error {
	var count int64
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM ai_tools`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, seed := range defaultAIToolSeeds {
		tool := buildDefaultAITool(seed)
		if err := m.Create(tool); err != nil {
			return err
		}
	}
	return nil
}

func buildDefaultAITool(seed aiToolSeed) *AITool {
	presetReferences, _ := json.Marshal(defaultAIToolPresetReferenceMap[seed.Category])
	stylePresets, _ := json.Marshal(defaultAIToolStylePresetMap[seed.Category])
	tool := &AITool{
		Code:                  seed.Code,
		Name:                  seed.Name,
		Category:              seed.Category,
		ShortDescription:      seed.ShortDescription,
		DetailDescription:     seed.DetailDescription,
		PromptPlaceholder:     fmt.Sprintf("补充一下你希望 %s 重点强化的画面要求", seed.Name),
		DefaultPrompt:         fmt.Sprintf("%s，请保留原图核心结构与空间逻辑，提升图面表达质量与专业感。", seed.Name),
		UploadHint:            defaultAIToolUploadHint,
		ShowUsageTips:         true,
		UsageTipsTitle:        "使用提示",
		UsageTipsContent:      buildDefaultUsageTipsContent(defaultAIToolUploadHint, seed.Common),
		SortOrder:             seed.SortOrder,
		IsPublished:           true,
		IsCommon:              seed.Common,
		PresetReferenceImages: string(presetReferences),
		StylePresetsRaw:       string(stylePresets),
	}
	if seed.Code == "new_minnan_tool" {
		tool.PromptPlaceholder = "描述你想生成的新闽派建筑、场景、视角、材质和氛围"
		tool.DefaultPrompt = "MNMIX，新闽派建筑风格，结合闽南红砖、燕尾脊、石雕砖雕、骑楼或地域街巷尺度，建筑比例真实，材质细节清晰，光影自然，画面具有专业建筑摄影质感。"
		tool.UploadHint = "当前支持文生图，无需上传图片；图生图能力将在接口参数确认后开放。"
		tool.UsageTipsContent = buildDefaultUsageTipsContent(tool.UploadHint, seed.Common)
		tool.PresetReferenceImages = "[]"
		tool.StylePresetsRaw = "[]"
	}
	return tool
}

func (t *AITool) GetPresetReferences() []AIToolPresetReference {
	list := make([]AIToolPresetReference, 0)
	if strings.TrimSpace(t.PresetReferenceImages) == "" {
		return list
	}
	_ = json.Unmarshal([]byte(t.PresetReferenceImages), &list)
	return list
}

func (t *AITool) GetStylePresets() []AIToolStylePreset {
	list := make([]AIToolStylePreset, 0)
	if strings.TrimSpace(t.StylePresetsRaw) == "" {
		return list
	}
	_ = json.Unmarshal([]byte(t.StylePresetsRaw), &list)
	return list
}

func (t *AITool) FindPresetReferenceByID(id string) *AIToolPresetReference {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil
	}
	for _, item := range t.GetPresetReferences() {
		if item.ID == trimmedID {
			current := item
			return &current
		}
	}
	return nil
}

func (t *AITool) FindStylePresetByID(id string) *AIToolStylePreset {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil
	}
	for _, item := range t.GetStylePresets() {
		if item.ID == trimmedID {
			current := item
			return &current
		}
	}
	return nil
}

func (s AIToolStylePreset) EffectiveReferenceMode() string {
	mode := strings.TrimSpace(s.ReferenceMode)
	if mode == "" {
		if strings.TrimSpace(s.ImageURL) == "" && strings.TrimSpace(s.ModelReferenceImageURL) == "" {
			return AIToolReferenceModePreviewOnly
		}
		return AIToolReferenceModePreviewAndModel
	}
	if mode == AIToolReferenceModePreviewOnly {
		return AIToolReferenceModePreviewOnly
	}
	return AIToolReferenceModePreviewAndModel
}

func (s AIToolStylePreset) EffectivePreviewImageURL() string {
	if url := strings.TrimSpace(s.PreviewImageURL); url != "" {
		return url
	}
	if url := strings.TrimSpace(s.ImageURL); url != "" {
		return url
	}
	return strings.TrimSpace(s.ModelReferenceImageURL)
}

func (s AIToolStylePreset) EffectiveModelReferenceImageURL() string {
	if s.EffectiveReferenceMode() != AIToolReferenceModePreviewAndModel {
		return ""
	}
	if url := strings.TrimSpace(s.ModelReferenceImageURL); url != "" {
		return url
	}
	return strings.TrimSpace(s.ImageURL)
}

func (m *AIToolModel) Create(tool *AITool) error {
	query := `INSERT INTO ai_tools (code, name, category, short_description, detail_description, list_cover_image, detail_before_image, detail_after_image, preset_reference_images, style_presets, prompt_placeholder, default_prompt, upload_hint, show_usage_tips, usage_tips_title, usage_tips_content, sort_order, is_published, is_common)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	showUsageTipsInt := 0
	if tool.ShowUsageTips {
		showUsageTipsInt = 1
	}
	isPublishedInt := 0
	if tool.IsPublished {
		isPublishedInt = 1
	}
	isCommonInt := 0
	if tool.IsCommon {
		isCommonInt = 1
	}
	result, err := m.DB.Exec(query, tool.Code, tool.Name, tool.Category, tool.ShortDescription, tool.DetailDescription, tool.ListCoverImage, tool.DetailBeforeImage, tool.DetailAfterImage, tool.PresetReferenceImages, tool.StylePresetsRaw, tool.PromptPlaceholder, tool.DefaultPrompt, tool.UploadHint, showUsageTipsInt, tool.UsageTipsTitle, tool.UsageTipsContent, tool.SortOrder, isPublishedInt, isCommonInt)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	tool.ID = id
	return nil
}

func (m *AIToolModel) createIfMissing(tool *AITool) error {
	query := `INSERT IGNORE INTO ai_tools (code, name, category, short_description, detail_description, list_cover_image, detail_before_image, detail_after_image, preset_reference_images, style_presets, prompt_placeholder, default_prompt, upload_hint, show_usage_tips, usage_tips_title, usage_tips_content, sort_order, is_published, is_common)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	showUsageTipsInt := 0
	if tool.ShowUsageTips {
		showUsageTipsInt = 1
	}
	isPublishedInt := 0
	if tool.IsPublished {
		isPublishedInt = 1
	}
	isCommonInt := 0
	if tool.IsCommon {
		isCommonInt = 1
	}
	_, err := m.DB.Exec(query, tool.Code, tool.Name, tool.Category, tool.ShortDescription, tool.DetailDescription, tool.ListCoverImage, tool.DetailBeforeImage, tool.DetailAfterImage, tool.PresetReferenceImages, tool.StylePresetsRaw, tool.PromptPlaceholder, tool.DefaultPrompt, tool.UploadHint, showUsageTipsInt, tool.UsageTipsTitle, tool.UsageTipsContent, tool.SortOrder, isPublishedInt, isCommonInt)
	return err
}

func (m *AIToolModel) ensureDefaultSeeds() error {
	for _, seed := range defaultAIToolSeeds {
		tool := buildDefaultAITool(seed)
		if err := m.createIfMissing(tool); err != nil {
			return err
		}
	}
	return m.ensureNewMinnanToolSeed()
}

func (m *AIToolModel) ensureNewMinnanToolSeed() error {
	tool := buildDefaultAITool(aiToolSeed{
		Code:              "new_minnan_tool",
		Name:              "新闽派工具",
		Category:          "architecture",
		ShortDescription:  "用文字快速生成新闽派建筑风格画面",
		DetailDescription: "面向新闽派建筑风格的专项工具，当前支持文生图，后续预留图生图能力。",
		Common:            true,
		SortOrder:         5,
	})
	showUsageTipsInt := 0
	if tool.ShowUsageTips {
		showUsageTipsInt = 1
	}
	query := `UPDATE ai_tools SET
		name = ?,
		category = ?,
		short_description = ?,
		detail_description = ?,
		preset_reference_images = ?,
		style_presets = ?,
		prompt_placeholder = ?,
		default_prompt = ?,
		upload_hint = ?,
		show_usage_tips = ?,
		usage_tips_title = ?,
		usage_tips_content = ?,
		sort_order = ?,
		is_published = 1,
		is_common = 1,
		updated_at = NOW()
		WHERE code = ?`
	_, err := m.DB.Exec(query,
		tool.Name,
		tool.Category,
		tool.ShortDescription,
		tool.DetailDescription,
		tool.PresetReferenceImages,
		tool.StylePresetsRaw,
		tool.PromptPlaceholder,
		tool.DefaultPrompt,
		tool.UploadHint,
		showUsageTipsInt,
		tool.UsageTipsTitle,
		tool.UsageTipsContent,
		tool.SortOrder,
		tool.Code,
	)
	return err
}

func (m *AIToolModel) GetByID(id int64) (*AITool, error) {
	query := `SELECT id, code, name, category, short_description, detail_description, list_cover_image, detail_before_image, detail_after_image, preset_reference_images, style_presets, prompt_placeholder, default_prompt, upload_hint, show_usage_tips, usage_tips_title, usage_tips_content, sort_order, is_published, is_common, usage_count, created_at, updated_at
	          FROM ai_tools WHERE id = ?`
	tool := &AITool{}
	var showUsageTipsInt int
	var isPublishedInt int
	var isCommonInt int
	if err := m.DB.QueryRow(query, id).Scan(&tool.ID, &tool.Code, &tool.Name, &tool.Category, &tool.ShortDescription, &tool.DetailDescription, &tool.ListCoverImage, &tool.DetailBeforeImage, &tool.DetailAfterImage, &tool.PresetReferenceImages, &tool.StylePresetsRaw, &tool.PromptPlaceholder, &tool.DefaultPrompt, &tool.UploadHint, &showUsageTipsInt, &tool.UsageTipsTitle, &tool.UsageTipsContent, &tool.SortOrder, &isPublishedInt, &isCommonInt, &tool.UsageCount, &tool.CreatedAt, &tool.UpdatedAt); err != nil {
		return nil, err
	}
	tool.ShowUsageTips = showUsageTipsInt == 1
	tool.IsPublished = isPublishedInt == 1
	tool.IsCommon = isCommonInt == 1
	return tool, nil
}

func (m *AIToolModel) GetByCode(code string) (*AITool, error) {
	query := `SELECT id, code, name, category, short_description, detail_description, list_cover_image, detail_before_image, detail_after_image, preset_reference_images, style_presets, prompt_placeholder, default_prompt, upload_hint, show_usage_tips, usage_tips_title, usage_tips_content, sort_order, is_published, is_common, usage_count, created_at, updated_at
	          FROM ai_tools WHERE code = ?`
	tool := &AITool{}
	var showUsageTipsInt int
	var isPublishedInt int
	var isCommonInt int
	if err := m.DB.QueryRow(query, strings.TrimSpace(code)).Scan(&tool.ID, &tool.Code, &tool.Name, &tool.Category, &tool.ShortDescription, &tool.DetailDescription, &tool.ListCoverImage, &tool.DetailBeforeImage, &tool.DetailAfterImage, &tool.PresetReferenceImages, &tool.StylePresetsRaw, &tool.PromptPlaceholder, &tool.DefaultPrompt, &tool.UploadHint, &showUsageTipsInt, &tool.UsageTipsTitle, &tool.UsageTipsContent, &tool.SortOrder, &isPublishedInt, &isCommonInt, &tool.UsageCount, &tool.CreatedAt, &tool.UpdatedAt); err != nil {
		return nil, err
	}
	tool.ShowUsageTips = showUsageTipsInt == 1
	tool.IsPublished = isPublishedInt == 1
	tool.IsCommon = isCommonInt == 1
	return tool, nil
}

func (m *AIToolModel) List(category, keyword string, isPublished *bool, limit, offset int) ([]*AITool, error) {
	query := `SELECT id, code, name, category, short_description, detail_description, list_cover_image, detail_before_image, detail_after_image, preset_reference_images, style_presets, prompt_placeholder, default_prompt, upload_hint, show_usage_tips, usage_tips_title, usage_tips_content, sort_order, is_published, is_common, usage_count, created_at, updated_at FROM ai_tools WHERE 1=1`
	args := make([]interface{}, 0)
	if trimmedCategory := strings.TrimSpace(category); trimmedCategory != "" {
		query += ` AND category = ?`
		args = append(args, trimmedCategory)
	}
	if trimmedKeyword := strings.TrimSpace(keyword); trimmedKeyword != "" {
		likeKeyword := "%" + trimmedKeyword + "%"
		query += ` AND (name LIKE ? OR code LIKE ? OR short_description LIKE ? OR detail_description LIKE ?)`
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}
	if isPublished != nil {
		if *isPublished {
			query += ` AND is_published = 1`
		} else {
			query += ` AND is_published = 0`
		}
	}
	query += ` ORDER BY sort_order ASC, id ASC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]*AITool, 0)
	for rows.Next() {
		tool := &AITool{}
		var showUsageTipsInt int
		var isPublishedInt int
		var isCommonInt int
		if err := rows.Scan(&tool.ID, &tool.Code, &tool.Name, &tool.Category, &tool.ShortDescription, &tool.DetailDescription, &tool.ListCoverImage, &tool.DetailBeforeImage, &tool.DetailAfterImage, &tool.PresetReferenceImages, &tool.StylePresetsRaw, &tool.PromptPlaceholder, &tool.DefaultPrompt, &tool.UploadHint, &showUsageTipsInt, &tool.UsageTipsTitle, &tool.UsageTipsContent, &tool.SortOrder, &isPublishedInt, &isCommonInt, &tool.UsageCount, &tool.CreatedAt, &tool.UpdatedAt); err != nil {
			return nil, err
		}
		tool.ShowUsageTips = showUsageTipsInt == 1
		tool.IsPublished = isPublishedInt == 1
		tool.IsCommon = isCommonInt == 1
		list = append(list, tool)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func (m *AIToolModel) Count(category, keyword string, isPublished *bool) (int64, error) {
	query := `SELECT COUNT(*) FROM ai_tools WHERE 1=1`
	args := make([]interface{}, 0)
	if trimmedCategory := strings.TrimSpace(category); trimmedCategory != "" {
		query += ` AND category = ?`
		args = append(args, trimmedCategory)
	}
	if trimmedKeyword := strings.TrimSpace(keyword); trimmedKeyword != "" {
		likeKeyword := "%" + trimmedKeyword + "%"
		query += ` AND (name LIKE ? OR code LIKE ? OR short_description LIKE ? OR detail_description LIKE ?)`
		args = append(args, likeKeyword, likeKeyword, likeKeyword, likeKeyword)
	}
	if isPublished != nil {
		if *isPublished {
			query += ` AND is_published = 1`
		} else {
			query += ` AND is_published = 0`
		}
	}
	var count int64
	if err := m.DB.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (m *AIToolModel) Update(tool *AITool) error {
	query := `UPDATE ai_tools SET code = ?, name = ?, category = ?, short_description = ?, detail_description = ?, list_cover_image = ?, detail_before_image = ?, detail_after_image = ?, preset_reference_images = ?, style_presets = ?, prompt_placeholder = ?, default_prompt = ?, upload_hint = ?, show_usage_tips = ?, usage_tips_title = ?, usage_tips_content = ?, sort_order = ?, is_published = ?, is_common = ? WHERE id = ?`
	showUsageTipsInt := 0
	if tool.ShowUsageTips {
		showUsageTipsInt = 1
	}
	isPublishedInt := 0
	if tool.IsPublished {
		isPublishedInt = 1
	}
	isCommonInt := 0
	if tool.IsCommon {
		isCommonInt = 1
	}
	_, err := m.DB.Exec(query, tool.Code, tool.Name, tool.Category, tool.ShortDescription, tool.DetailDescription, tool.ListCoverImage, tool.DetailBeforeImage, tool.DetailAfterImage, tool.PresetReferenceImages, tool.StylePresetsRaw, tool.PromptPlaceholder, tool.DefaultPrompt, tool.UploadHint, showUsageTipsInt, tool.UsageTipsTitle, tool.UsageTipsContent, tool.SortOrder, isPublishedInt, isCommonInt, tool.ID)
	return err
}

func (m *AIToolModel) Delete(id int64) error {
	_, err := m.DB.Exec(`DELETE FROM ai_tools WHERE id = ?`, id)
	return err
}

func (m *AIToolModel) incrementUsageCountWithExecutor(executor aiToolExecutor, id int64) error {
	if id <= 0 {
		return nil
	}
	_, err := executor.Exec(`UPDATE ai_tools SET usage_count = usage_count + 1 WHERE id = ?`, id)
	return err
}

func (m *AIToolModel) IncrementUsageCount(id int64) error {
	return m.incrementUsageCountWithExecutor(m.DB, id)
}

func (m *AIToolModel) IncrementUsageCountWithTx(tx *sql.Tx, id int64) error {
	return m.incrementUsageCountWithExecutor(tx, id)
}
