package route

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"service/model"
	"service/tracking"
)

type aiVideoStudioLimits struct {
	ImageMax           int
	VideoMax           int
	AudioMax           int
	TotalMax           int
	MediaDurationMin   float64
	MediaDurationMax   float64
	VideoTotalDuration float64
	AudioTotalDuration float64
	QuantityMax        int
	MaxDuration768p    int
	MaxDuration2k      int
}

func defaultAIVideoStudioLimits() aiVideoStudioLimits {
	return aiVideoStudioLimits{8, 3, 3, 12, 2, 15, 15, 15, 1, 15, 15}
}

type aiVideoStudioReferenceInput struct {
	MediaID         string  `json:"media_id"`
	MediaType       string  `json:"media_type"`
	Purpose         string  `json:"purpose"`
	Position        int     `json:"position"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type aiVideoStudioDraftInput struct {
	Name              string                        `json:"name"`
	Mode              string                        `json:"mode"`
	Prompt            string                        `json:"prompt"`
	ReferencePrompt   string                        `json:"reference_prompt"`
	FirstFrameMediaID string                        `json:"first_frame_media_id"`
	LastFrameMediaID  string                        `json:"last_frame_media_id"`
	References        []aiVideoStudioReferenceInput `json:"references"`
	Ratio             string                        `json:"ratio"`
	Resolution        string                        `json:"resolution"`
	Duration          int                           `json:"duration"`
	Quantity          int                           `json:"quantity"`
	ExpectedRevision  int64                         `json:"expected_revision"`
}

func validateAIVideoStudioDraft(in aiVideoStudioDraftInput, limits aiVideoStudioLimits) error {
	if err := validateAIVideoStudioDraftSave(in, limits); err != nil {
		return err
	}
	if in.Mode == "reference" {
		if strings.TrimSpace(in.ReferencePrompt) == "" {
			return errors.New("参考提示词不能为空")
		}
	} else if strings.TrimSpace(in.Prompt) == "" {
		return errors.New("画面描述不能为空")
	}
	switch in.Mode {
	case "first_frame":
		if in.FirstFrameMediaID == "" {
			return errors.New("首帧生成需要一张首帧")
		}
	case "first_last_frame":
		if in.FirstFrameMediaID == "" || in.LastFrameMediaID == "" {
			return errors.New("首尾帧生成必须同时提供首帧和尾帧")
		}
	case "reference":
		if len(in.References) == 0 {
			return errors.New("参考生成必须使用参考素材")
		}
	}
	return nil
}

func validateAIVideoStudioDraftSave(in aiVideoStudioDraftInput, limits aiVideoStudioLimits) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Mode = strings.TrimSpace(in.Mode)
	in.Prompt = strings.TrimSpace(in.Prompt)
	if len([]rune(in.Name)) > 120 {
		return errors.New("创作名称不能超过120字")
	}
	if len([]rune(in.Prompt)) > 7000 || len([]rune(in.ReferencePrompt)) > 7000 {
		return errors.New("提示词不能超过7000字")
	}
	if in.Duration < 4 {
		return errors.New("时长不能少于4秒")
	}
	if in.Quantity < 1 || in.Quantity > limits.QuantityMax {
		return errors.New("生成数量超出当前能力限制")
	}
	if in.Resolution != "768p" && in.Resolution != "2k" {
		return errors.New("清晰度不受支持")
	}
	if in.Resolution == "2k" && in.Duration > limits.MaxDuration2k {
		return errors.New("2K视频时长超出当前能力限制")
	}
	if in.Resolution == "768p" && in.Duration > limits.MaxDuration768p {
		return errors.New("视频时长超出当前能力限制")
	}
	if in.Ratio != "16:9" && in.Ratio != "9:16" && in.Ratio != "1:1" && in.Ratio != "adaptive" {
		return errors.New("画面比例不受支持")
	}
	switch in.Mode {
	case "text":
		if in.Ratio == "adaptive" {
			return errors.New("文生视频不能使用自动比例")
		}
		if in.FirstFrameMediaID != "" || in.LastFrameMediaID != "" || len(in.References) > 0 {
			return errors.New("文生视频不能携带参考素材")
		}
	case "first_frame":
		if in.LastFrameMediaID != "" || len(in.References) > 0 {
			return errors.New("首帧生成只能使用一张首帧")
		}
		if in.Ratio != "adaptive" {
			return errors.New("首帧生成比例由首帧自适应")
		}
	case "first_last_frame":
		if len(in.References) > 0 || (in.LastFrameMediaID != "" && in.FirstFrameMediaID == "") {
			return errors.New("首尾帧生成不能携带参考素材或仅提供尾帧")
		}
		if in.FirstFrameMediaID != "" && in.FirstFrameMediaID == in.LastFrameMediaID {
			return errors.New("首帧和尾帧不能相同")
		}
		if in.Ratio != "adaptive" {
			return errors.New("首尾帧生成比例由首帧自适应")
		}
	case "reference":
		if in.FirstFrameMediaID != "" || in.LastFrameMediaID != "" {
			return errors.New("参考生成不能夹带首尾帧")
		}
		if len(in.References) > 0 {
			if err := validateAIVideoStudioReferences(in.References, limits); err != nil {
				return err
			}
		}
	default:
		return errors.New("生成模式不受支持")
	}
	return nil
}

func validateAIVideoStudioReferences(refs []aiVideoStudioReferenceInput, limits aiVideoStudioLimits) error {
	if len(refs) > limits.TotalMax {
		return errors.New("参考素材总数超出限制")
	}
	seen := map[string]bool{}
	counts := map[string]int{}
	totals := map[string]float64{}
	for i, ref := range refs {
		ref.MediaID = strings.TrimSpace(ref.MediaID)
		if ref.MediaID == "" || seen[ref.MediaID] {
			return errors.New("参考素材为空或重复")
		}
		seen[ref.MediaID] = true
		if ref.Position != i+1 {
			return errors.New("参考素材编号必须连续且与选择顺序一致")
		}
		if ref.MediaType != "image" && ref.MediaType != "video" && ref.MediaType != "audio" {
			return errors.New("参考素材类型不受支持")
		}
		counts[ref.MediaType]++
		if ref.MediaType != "image" {
			if ref.DurationSeconds < limits.MediaDurationMin || ref.DurationSeconds > limits.MediaDurationMax {
				return errors.New("视频或音频单条时长超出限制")
			}
			totals[ref.MediaType] += ref.DurationSeconds
		}
	}
	if counts["image"] > limits.ImageMax || counts["video"] > limits.VideoMax || counts["audio"] > limits.AudioMax {
		return errors.New("单类参考素材数量超出限制")
	}
	if totals["video"] > limits.VideoTotalDuration || totals["audio"] > limits.AudioTotalDuration {
		return errors.New("视频或音频累计时长超出限制")
	}
	return nil
}

func studioLimitsFromConfig(config model.AIVideoStudioConfig) aiVideoStudioLimits {
	l := defaultAIVideoStudioLimits()
	set := func(key string, target *int) {
		if value := config.Limits[key]; value > 0 {
			*target = value
		}
	}
	set("max_quantity", &l.QuantityMax)
	set("max_duration_768p", &l.MaxDuration768p)
	set("max_duration_2k", &l.MaxDuration2k)
	set("max_reference_images", &l.ImageMax)
	set("max_reference_videos", &l.VideoMax)
	set("max_reference_audios", &l.AudioMax)
	set("max_reference_total", &l.TotalMax)
	return l
}

func studioError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"code": status, "msg": message})
}
func studioOK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": data})
}
func studioPage(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}
func studioProviderConfigured() bool {
	return strings.TrimSpace(os.Getenv("AI_VIDEO_STUDIO_METASO_API_KEY")) != ""
}

// Execution and accounting stay hard-disabled until one durable transaction can
// reserve stones and persist the batch/fee fact without a Redis/MySQL split-brain.
func studioExecutionReady() bool  { return false }
func studioAccountingReady() bool { return false }

func RegisterAIVideoStudioRoutes(group *gin.RouterGroup, store *model.AIVideoStudioStore, media *model.AIVideoStudioMediaStore, trackingService *tracking.Service, users *model.UserModel) {
	g := group.Group("/ai-video-studio")
	g.Use(func(c *gin.Context) {
		userID := GetTokenUserID(c)
		var approved int
		err := store.DB.QueryRowContext(c, `SELECT COUNT(*) FROM certification_applications WHERE user_id=? AND type='designer' AND status='approved' AND TRIM(identity_type)='设计师'`, userID).Scan(&approved)
		if err != nil {
			studioError(c, 503, "设计师认证状态暂时无法核验")
			return
		}
		if approved == 0 {
			studioError(c, 403, "当前账号尚未通过设计师认证")
			return
		}
		c.Next()
	})
	g.GET("/capabilities", func(c *gin.Context) {
		config, err := store.Config(c, studioProviderConfigured())
		if err != nil {
			studioError(c, 503, "AI视频能力暂时不可用")
			return
		}
		billingReady := false
		if validateAIVideoStudioConfig(config) == nil {
			for _, price := range config.Prices {
				if price.Enabled && price.Code != "ai_video_studio_prompt_optimize" && price.UnitStones != nil && *price.UnitStones > 0 {
					billingReady = true
				}
			}
		}
		_, ffprobeErr := exec.LookPath("ffprobe")
		mediaSigningReady := len(strings.TrimSpace(os.Getenv("TRACKING_MEDIA_SIGN_KEY"))) >= 32
		imageMediaReady := contentMediaConfigured(trackingService) && mediaSigningReady
		mixedMediaReady := imageMediaReady && ffprobeErr == nil
		executionReady, accountingReady := studioExecutionReady(), studioAccountingReady()
		enabled := config.Enabled && config.ProviderConfigured && billingReady && executionReady && accountingReady
		reason := ""
		if !executionReady || !accountingReady {
			reason = "生成服务尚未开放"
		} else if !config.Enabled {
			reason = "运营暂未开放"
		} else if !config.ProviderConfigured {
			reason = "视频生成服务尚未配置"
		} else if !billingReady {
			reason = "视频生成价格尚未配置"
		}
		modes := make([]gin.H, 0, 4)
		for _, code := range []string{"text", "first_frame", "first_last_frame", "reference"} {
			open := enabled && config.Modes[code]
			if (code == "first_frame" || code == "first_last_frame") && !imageMediaReady {
				open = false
			}
			if code == "reference" && !mixedMediaReady {
				open = false
			}
			modeReason := ""
			if !open {
				modeReason = reason
				if modeReason == "" {
					modeReason = "该生成方式暂未开放"
				}
			}
			modes = append(modes, gin.H{"code": code, "enabled": open, "disabled_reason": modeReason})
		}
		limits := studioLimitsFromConfig(config)
		studioOK(c, gin.H{"enabled": enabled, "billing_ready": billingReady, "execution_ready": executionReady, "accounting_ready": accountingReady, "history_readable": true, "disabled_reason": reason, "capability_version": config.Version, "quote_version": config.Version, "modes": modes, "ratios": []string{"16:9", "9:16", "1:1", "adaptive"}, "resolutions": []string{"768p", "2k"}, "durations_by_resolution": gin.H{"768p": durationRange(limits.MaxDuration768p), "2k": durationRange(limits.MaxDuration2k)}, "quantity_max": limits.QuantityMax, "image_upload_enabled": imageMediaReady, "media_upload_enabled": mixedMediaReady, "media_disabled_reason": map[bool]string{true: "", false: "素材存储、安全签名或时长探测尚未配置"}[mixedMediaReady], "reference_limits": gin.H{"image_max": limits.ImageMax, "video_max": limits.VideoMax, "audio_max": limits.AudioMax, "total_max": limits.TotalMax, "media_duration_min": limits.MediaDurationMin, "media_duration_max": limits.MediaDurationMax, "video_total_duration_max": limits.VideoTotalDuration, "audio_total_duration_max": limits.AudioTotalDuration}})
	})
	g.GET("/drafts", func(c *gin.Context) {
		page, size := studioPage(c)
		items, total, err := store.ListDrafts(c, GetTokenUserID(c), c.Query("status"), c.Query("mode"), size, (page-1)*size)
		if err != nil {
			studioError(c, 500, "草稿读取失败")
			return
		}
		studioOK(c, gin.H{"items": items, "total": total, "page": page, "page_size": size, "has_more": int64(page*size) < total})
	})
	g.POST("/drafts", func(c *gin.Context) {
		var in aiVideoStudioDraftInput
		if err := c.ShouldBindJSON(&in); err != nil {
			studioError(c, 400, "草稿参数不合法")
			return
		}
		config, err := store.Config(c, studioProviderConfigured())
		if err != nil {
			studioError(c, 503, "能力配置读取失败")
			return
		}
		if err := validateAIVideoStudioDraftSave(in, studioLimitsFromConfig(config)); err != nil {
			studioError(c, 400, err.Error())
			return
		}
		if strings.TrimSpace(in.Name) == "" {
			in.Name = "未命名创作"
		}
		draft := studioDraftModel(GetTokenUserID(c), 0, in)
		if err := store.CreateDraft(c, &draft); err != nil {
			studioError(c, 500, "草稿保存失败")
			return
		}
		studioOK(c, draft)
	})
	g.GET("/drafts/:id", func(c *gin.Context) {
		id, ok := studioInt64Param(c, "id")
		if !ok {
			return
		}
		draft, err := store.GetDraft(c, GetTokenUserID(c), id)
		if errors.Is(err, sql.ErrNoRows) {
			studioError(c, 404, "草稿不存在")
			return
		}
		if err != nil {
			studioError(c, 500, "草稿读取失败")
			return
		}
		studioOK(c, draft)
	})
	g.PATCH("/drafts/:id", func(c *gin.Context) {
		id, ok := studioInt64Param(c, "id")
		if !ok {
			return
		}
		var in aiVideoStudioDraftInput
		if err := c.ShouldBindJSON(&in); err != nil || in.ExpectedRevision < 1 {
			studioError(c, 400, "草稿参数或修订号不合法")
			return
		}
		config, err := store.Config(c, studioProviderConfigured())
		if err != nil {
			studioError(c, 503, "能力配置读取失败")
			return
		}
		if err := validateAIVideoStudioDraftSave(in, studioLimitsFromConfig(config)); err != nil {
			studioError(c, 400, err.Error())
			return
		}
		if strings.TrimSpace(in.Name) == "" {
			in.Name = "未命名创作"
		}
		draft := studioDraftModel(GetTokenUserID(c), id, in)
		if err := store.UpdateDraft(c, &draft, in.ExpectedRevision); errors.Is(err, model.ErrAIVideoStudioConflict) {
			studioError(c, 409, "草稿已更新，请刷新后重试")
			return
		} else if err != nil {
			studioError(c, 500, "草稿更新失败")
			return
		}
		studioOK(c, draft)
	})
	g.DELETE("/drafts/:id", func(c *gin.Context) {
		id, ok := studioInt64Param(c, "id")
		if !ok {
			return
		}
		var in struct {
			ExpectedRevision int64 `json:"expected_revision"`
		}
		if c.ShouldBindJSON(&in) != nil || in.ExpectedRevision < 1 {
			studioError(c, 400, "修订号不合法")
			return
		}
		if err := store.DeleteDraft(c, GetTokenUserID(c), id, in.ExpectedRevision); errors.Is(err, model.ErrAIVideoStudioConflict) {
			studioError(c, 409, "草稿已更新，请刷新后重试")
			return
		} else if err != nil {
			studioError(c, 500, "草稿删除失败")
			return
		}
		studioOK(c, gin.H{"deleted": true})
	})
	registerAIVideoStudioPromptRoutes(g, store)
	registerAIVideoStudioMediaRoutes(g, store, media, trackingService, users)
	registerAIVideoStudioAdoptionRoutes(g, media, trackingService, users)
	g.POST("/quotes", func(c *gin.Context) { handleAIVideoStudioQuote(c, store, media) })
	g.POST("/batches", func(c *gin.Context) { studioError(c, 503, "生成执行服务尚未完成价格与任务恢复配置") })
	g.GET("/batches", func(c *gin.Context) {
		page, size := studioPage(c)
		items, total, err := store.ListBatches(c, GetTokenUserID(c), size, (page-1)*size)
		if err != nil {
			studioError(c, 500, "创作记录读取失败")
			return
		}
		studioOK(c, gin.H{"items": items, "total": total, "page": page, "page_size": size, "has_more": int64(page*size) < total})
	})
	g.GET("/batches/:id", func(c *gin.Context) {
		item, err := store.GetBatch(c, GetTokenUserID(c), c.Param("id"))
		if errors.Is(err, sql.ErrNoRows) {
			studioError(c, 404, "创作记录不存在")
			return
		}
		if err != nil {
			studioError(c, 500, "创作记录读取失败")
			return
		}
		studioOK(c, item)
	})
}

func durationRange(max int) []int {
	values := make([]int, 0)
	for i := 4; i <= max; i++ {
		values = append(values, i)
	}
	return values
}
func studioInt64Param(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id < 1 {
		studioError(c, 400, "ID不合法")
		return 0, false
	}
	return id, true
}
func studioDraftModel(userID, id int64, in aiVideoStudioDraftInput) model.AIVideoStudioDraft {
	refs, _ := json.Marshal(in.References)
	return model.AIVideoStudioDraft{ID: id, UserID: userID, Name: strings.TrimSpace(in.Name), Mode: in.Mode, Prompt: strings.TrimSpace(in.Prompt), ReferencePrompt: strings.TrimSpace(in.ReferencePrompt), FirstFrameMediaID: strings.TrimSpace(in.FirstFrameMediaID), LastFrameMediaID: strings.TrimSpace(in.LastFrameMediaID), References: refs, Ratio: in.Ratio, Resolution: in.Resolution, Duration: in.Duration, Quantity: in.Quantity}
}

func studioDraftInput(draft *model.AIVideoStudioDraft) aiVideoStudioDraftInput {
	var references []aiVideoStudioReferenceInput
	_ = json.Unmarshal(draft.References, &references)
	return aiVideoStudioDraftInput{Name: draft.Name, Mode: draft.Mode, Prompt: draft.Prompt, ReferencePrompt: draft.ReferencePrompt, FirstFrameMediaID: draft.FirstFrameMediaID, LastFrameMediaID: draft.LastFrameMediaID, References: references, Ratio: draft.Ratio, Resolution: draft.Resolution, Duration: draft.Duration, Quantity: draft.Quantity}
}

func registerAIVideoStudioPromptRoutes(g *gin.RouterGroup, store *model.AIVideoStudioStore) {
	g.GET("/prompts", func(c *gin.Context) {
		page, size := studioPage(c)
		items, total, err := store.ListPrompts(c, GetTokenUserID(c), size, (page-1)*size)
		if err != nil {
			studioError(c, 500, "提示词读取失败")
			return
		}
		studioOK(c, gin.H{"items": items, "total": total, "page": page, "page_size": size, "has_more": int64(page*size) < total})
	})
	g.POST("/prompts", func(c *gin.Context) {
		var p model.AIVideoStudioPrompt
		if c.ShouldBindJSON(&p) != nil || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Prompt) == "" {
			studioError(c, 400, "提示词名称和内容不能为空")
			return
		}
		if len([]rune(p.Name)) > 120 || len([]rune(p.Prompt)) > 7000 {
			studioError(c, 400, "提示词名称或内容过长")
			return
		}
		if err := store.CreatePrompt(c, GetTokenUserID(c), &p); err != nil {
			studioError(c, 500, "提示词保存失败")
			return
		}
		studioOK(c, p)
	})
	g.PATCH("/prompts/:id", func(c *gin.Context) {
		id, ok := studioInt64Param(c, "id")
		if !ok {
			return
		}
		var input struct {
			Name             string `json:"name"`
			Mode             string `json:"mode"`
			Prompt           string `json:"prompt"`
			ExpectedRevision int64  `json:"expected_revision"`
		}
		if c.ShouldBindJSON(&input) != nil || input.ExpectedRevision < 1 || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Prompt) == "" {
			studioError(c, 400, "提示词参数不合法")
			return
		}
		if len([]rune(input.Name)) > 120 || len([]rune(input.Prompt)) > 7000 {
			studioError(c, 400, "提示词名称或内容过长")
			return
		}
		p := model.AIVideoStudioPrompt{ID: id, Name: input.Name, Mode: input.Mode, Prompt: input.Prompt}
		if err := store.UpdatePrompt(c, GetTokenUserID(c), &p, input.ExpectedRevision); errors.Is(err, model.ErrAIVideoStudioConflict) {
			studioError(c, 409, "提示词已更新，请刷新后重试")
			return
		} else if err != nil {
			studioError(c, 500, "提示词更新失败")
			return
		}
		studioOK(c, p)
	})
	g.DELETE("/prompts/:id", func(c *gin.Context) {
		id, ok := studioInt64Param(c, "id")
		if !ok {
			return
		}
		var input struct {
			ExpectedRevision int64 `json:"expected_revision"`
		}
		if c.ShouldBindJSON(&input) != nil || input.ExpectedRevision < 1 {
			studioError(c, 400, "修订号不合法")
			return
		}
		if err := store.DeletePrompt(c, GetTokenUserID(c), id, input.ExpectedRevision); errors.Is(err, model.ErrAIVideoStudioConflict) {
			studioError(c, 409, "提示词已更新，请刷新后重试")
			return
		} else if err != nil {
			studioError(c, 500, "提示词删除失败")
			return
		}
		studioOK(c, gin.H{"deleted": true})
	})
}

func validateAIVideoStudioMedia(ctx context.Context, media *model.AIVideoStudioMediaStore, userID int64, input *aiVideoStudioDraftInput) error {
	for _, frame := range []string{input.FirstFrameMediaID, input.LastFrameMediaID} {
		if frame == "" {
			continue
		}
		item, err := media.Get(ctx, userID, frame)
		if err != nil {
			return errors.New("首尾帧素材无效或无权使用")
		}
		if item.Kind != "image" {
			return errors.New("首尾帧必须使用图片素材")
		}
	}
	for index := range input.References {
		ref := &input.References[index]
		item, err := media.Get(ctx, userID, ref.MediaID)
		if err != nil {
			return errors.New("参考素材无效或无权使用")
		}
		if item.Kind != ref.MediaType {
			return errors.New("参考素材类型与实际文件不一致")
		}
		ref.DurationSeconds = item.DurationSeconds
	}
	return nil
}

func handleAIVideoStudioQuote(c *gin.Context, store *model.AIVideoStudioStore, media *model.AIVideoStudioMediaStore) {
	var input struct {
		DraftID          int64 `json:"draft_id"`
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if c.ShouldBindJSON(&input) != nil || input.DraftID < 1 || input.ExpectedRevision < 1 {
		studioError(c, 400, "报价参数不合法")
		return
	}
	if !studioExecutionReady() || !studioAccountingReady() {
		studioError(c, 503, "生成服务尚未开放")
		return
	}
	config, err := store.Config(c, studioProviderConfigured())
	if err != nil {
		studioError(c, 503, "能力配置读取失败")
		return
	}
	if !config.Enabled || !config.ProviderConfigured {
		studioError(c, 503, "视频生成能力尚未配置完成")
		return
	}
	if err := validateAIVideoStudioConfig(config); err != nil {
		studioError(c, 503, "视频生成配置尚未完成")
		return
	}
	draft, err := store.GetDraft(c, GetTokenUserID(c), input.DraftID)
	if errors.Is(err, sql.ErrNoRows) {
		studioError(c, 404, "草稿不存在")
		return
	}
	if err != nil {
		studioError(c, 500, "草稿读取失败")
		return
	}
	if draft.Revision != input.ExpectedRevision {
		studioError(c, 409, "草稿已更新，请重新报价")
		return
	}
	if !config.Modes[draft.Mode] {
		studioError(c, 403, "当前生成方式未开放")
		return
	}
	validatedInput := studioDraftInput(draft)
	if err := validateAIVideoStudioMedia(c, media, GetTokenUserID(c), &validatedInput); err != nil {
		studioError(c, 400, err.Error())
		return
	}
	if err := validateAIVideoStudioDraft(validatedInput, studioLimitsFromConfig(config)); err != nil {
		studioError(c, 400, err.Error())
		return
	}
	price, ok := studioPrice(config.Prices, draft.Resolution, draft.Duration)
	if !ok || price.UnitStones == nil || *price.UnitStones <= 0 {
		studioError(c, 503, "当前生成档位尚未配置价格")
		return
	}
	validatedReferences, _ := json.Marshal(validatedInput.References)
	snapshotDraft := *draft
	snapshotDraft.References = validatedReferences
	snapshot, _ := json.Marshal(snapshotDraft)
	sum := sha256.Sum256(snapshot)
	unitStones := *price.UnitStones
	units, totalStones, err := calculateAIVideoStudioQuoteTotal(unitStones, draft.Duration, draft.Quantity, price.BillingUnit)
	if err != nil {
		studioError(c, 503, "当前生成档位价格无效")
		return
	}
	quote := model.AIVideoStudioQuote{ID: uuid.NewString(), UserID: GetTokenUserID(c), DraftID: draft.ID, DraftRevision: draft.Revision, QuoteVersion: config.Version, InputHash: hex.EncodeToString(sum[:]), PriceCode: price.Code, UnitStones: unitStones, BillingUnit: price.BillingUnit, BillableUnits: units, TotalStones: totalStones, InputSnapshot: snapshot, ExpiresAt: time.Now().Add(10 * time.Minute)}
	if err := store.CreateQuote(c, quote); err != nil {
		studioError(c, 500, "报价创建失败")
		return
	}
	studioOK(c, quote)
}
func studioPrice(prices []model.AIVideoStudioPrice, resolution string, duration int) (model.AIVideoStudioPrice, bool) {
	code := "ai_video_studio_768p"
	if strings.EqualFold(resolution, "2k") {
		code = "ai_video_studio_2k"
	} else if duration > 15 {
		code = "ai_video_studio_768p_long"
	}
	var selected model.AIVideoStudioPrice
	matches := 0
	for _, price := range prices {
		if price.Enabled && price.Code == code && (price.BillingUnit == "per_request" || price.BillingUnit == "per_video" || price.BillingUnit == "per_second") {
			selected = price
			matches++
		}
	}
	return selected, matches == 1
}

const (
	maxAIVideoStudioConcurrency    = 20
	maxAIVideoStudioTimeoutSeconds = 3600
	maxAIVideoStudioQuantity       = 10
	maxAIVideoStudioUnitStones     = int64(1_000_000_000)
)

var requiredAIVideoStudioPriceCodes = map[string]bool{
	"ai_video_studio_prompt_optimize": true,
	"ai_video_studio_768p":            true,
	"ai_video_studio_2k":              true,
	"ai_video_studio_768p_long":       true,
}

func calculateAIVideoStudioQuoteTotal(unitStones int64, duration, quantity int, billingUnit string) (int64, int64, error) {
	if unitStones < 1 || unitStones > maxAIVideoStudioUnitStones || duration < 1 || duration > 30 || quantity < 1 || quantity > maxAIVideoStudioQuantity {
		return 0, 0, errors.New("报价数值超出安全范围")
	}
	units := int64(1)
	switch billingUnit {
	case "per_second":
		if int64(duration) > math.MaxInt64/int64(quantity) {
			return 0, 0, errors.New("报价计费数量溢出")
		}
		units = int64(duration) * int64(quantity)
	case "per_video":
		units = int64(quantity)
	case "per_request":
		units = 1
	default:
		return 0, 0, errors.New("报价计价方式无效")
	}
	if unitStones > math.MaxInt64/units {
		return 0, 0, errors.New("报价总额溢出")
	}
	return units, unitStones * units, nil
}

func defaultAIVideoStudioPrices() []model.AIVideoStudioPrice {
	return []model.AIVideoStudioPrice{
		{Code: "ai_video_studio_prompt_optimize", Label: "提示词优化", BillingUnit: "per_request"},
		{Code: "ai_video_studio_768p", Label: "768P 视频生成", BillingUnit: "per_second"},
		{Code: "ai_video_studio_2k", Label: "2K 视频生成", BillingUnit: "per_second"},
		{Code: "ai_video_studio_768p_long", Label: "768P 长视频生成", BillingUnit: "per_second"},
	}
}

func validateAIVideoStudioConfig(config model.AIVideoStudioConfig) error {
	limits := config.Limits
	if limits["max_concurrency"] < 1 || limits["max_concurrency"] > maxAIVideoStudioConcurrency {
		return errors.New("并发限制超出安全范围")
	}
	if limits["timeout_seconds"] < 1 || limits["timeout_seconds"] > maxAIVideoStudioTimeoutSeconds {
		return errors.New("超时时间超出安全范围")
	}
	if limits["max_quantity"] < 1 || limits["max_quantity"] > maxAIVideoStudioQuantity {
		return errors.New("生成数量超出安全范围")
	}
	if limits["max_duration_768p"] < 4 || limits["max_duration_768p"] > 30 {
		return errors.New("768P时长限制必须在4到30秒")
	}
	if limits["max_duration_2k"] < 4 || limits["max_duration_2k"] > 15 {
		return errors.New("2K时长限制必须在4到15秒")
	}
	for key, max := range map[string]int{"max_reference_images": 8, "max_reference_videos": 3, "max_reference_audios": 3, "max_reference_total": 12} {
		if limits[key] < 0 || limits[key] > max {
			return fmt.Errorf("%s 超出安全范围", key)
		}
	}
	if limits["max_reference_total"] < limits["max_reference_images"] || limits["max_reference_total"] < limits["max_reference_videos"] || limits["max_reference_total"] < limits["max_reference_audios"] {
		return errors.New("参考素材总上限不能小于单类上限")
	}
	seenPrices := make(map[string]bool, len(requiredAIVideoStudioPriceCodes))
	activeVideoPrice, longPrice := false, false
	for _, price := range config.Prices {
		if !requiredAIVideoStudioPriceCodes[price.Code] {
			return errors.New("存在未知价格代码")
		}
		if seenPrices[price.Code] {
			return errors.New("价格代码不能重复")
		}
		seenPrices[price.Code] = true
		if price.BillingUnit != "per_request" && price.BillingUnit != "per_video" && price.BillingUnit != "per_second" {
			return errors.New("价格计价方式无效")
		}
		if price.UnitStones != nil && (*price.UnitStones < 0 || *price.UnitStones > maxAIVideoStudioUnitStones) {
			return errors.New("单价超出安全范围")
		}
		if price.Enabled && price.UnitStones != nil && *price.UnitStones > 0 {
			if price.Code != "ai_video_studio_prompt_optimize" {
				activeVideoPrice = true
			}
			if price.Code == "ai_video_studio_768p_long" {
				longPrice = true
			}
		}
	}
	if len(seenPrices) != len(requiredAIVideoStudioPriceCodes) {
		return errors.New("价格目录必须包含全部四个独立价格项")
	}
	if limits["max_duration_768p"] > 15 && !longPrice {
		return errors.New("开放16至30秒前必须配置并启用长视频价格")
	}
	if config.Enabled {
		hasMode := false
		for _, enabled := range config.Modes {
			hasMode = hasMode || enabled
		}
		if !hasMode {
			return errors.New("至少开放一种生成模式")
		}
		if !activeVideoPrice {
			return errors.New("至少配置并启用一个视频价格")
		}
	}
	return nil
}

func RegisterAIVideoStudioManagementRoutes(group *gin.RouterGroup, store *model.AIVideoStudioStore) {
	g := group.Group("/ai-video-studio")
	g.GET("/config", func(c *gin.Context) {
		config, err := store.Config(c, studioProviderConfigured())
		if err != nil {
			studioError(c, 500, "配置读取失败")
			return
		}
		if len(config.Prices) == 0 {
			config.Prices = defaultAIVideoStudioPrices()
		}
		studioOK(c, gin.H{"enabled": config.Enabled, "execution_ready": studioExecutionReady(), "accounting_ready": studioAccountingReady(), "provider_code": config.ProviderCode, "provider_display_name": "秘塔 H3", "provider_configured": config.ProviderConfigured, "modes": config.Modes, "limits": config.Limits, "prices": config.Prices, "version": config.Version, "updated_at": config.UpdatedAt})
	})
	g.PUT("/config", func(c *gin.Context) {
		var input struct {
			Enabled         bool                       `json:"enabled"`
			ProviderCode    string                     `json:"provider_code"`
			Modes           map[string]bool            `json:"modes"`
			Limits          map[string]int             `json:"limits"`
			Prices          []model.AIVideoStudioPrice `json:"prices"`
			ExpectedVersion int64                      `json:"expected_version"`
		}
		if c.ShouldBindJSON(&input) != nil || input.ExpectedVersion < 1 || input.ProviderCode != "metaso_h3" {
			studioError(c, 400, "配置参数不合法")
			return
		}
		for code := range input.Modes {
			if code != "text" && code != "first_frame" && code != "first_last_frame" && code != "reference" {
				studioError(c, 400, "存在不支持的生成模式")
				return
			}
		}
		allowedPrices := map[string]bool{"ai_video_studio_prompt_optimize": true, "ai_video_studio_768p": true, "ai_video_studio_2k": true, "ai_video_studio_768p_long": true}
		for i := range input.Prices {
			price := &input.Prices[i]
			if !allowedPrices[price.Code] || (price.BillingUnit != "per_request" && price.BillingUnit != "per_video" && price.BillingUnit != "per_second") {
				studioError(c, 400, "价格目录不完整")
				return
			}
			if price.Enabled && (price.UnitStones == nil || *price.UnitStones <= 0) {
				studioError(c, 400, "启用的价格必须大于0")
				return
			}
			price.Version = input.ExpectedVersion + 1
		}
		config := model.AIVideoStudioConfig{Enabled: input.Enabled, ProviderCode: input.ProviderCode, ProviderConfigured: studioProviderConfigured(), Modes: input.Modes, Limits: input.Limits, Prices: input.Prices}
		if err := validateAIVideoStudioConfig(config); err != nil {
			studioError(c, 400, err.Error())
			return
		}
		if err := store.UpdateConfig(c, config, input.ExpectedVersion); errors.Is(err, model.ErrAIVideoStudioConflict) {
			studioError(c, 409, "配置已被其他管理员更新")
			return
		} else if err != nil {
			studioError(c, 500, "配置保存失败")
			return
		}
		updated, err := store.Config(c, studioProviderConfigured())
		if err != nil {
			studioError(c, 500, "配置保存后读取失败")
			return
		}
		if len(updated.Prices) == 0 {
			updated.Prices = defaultAIVideoStudioPrices()
		}
		studioOK(c, gin.H{"enabled": updated.Enabled, "execution_ready": studioExecutionReady(), "accounting_ready": studioAccountingReady(), "provider_code": updated.ProviderCode, "provider_display_name": "秘塔 H3", "provider_configured": updated.ProviderConfigured, "modes": updated.Modes, "limits": updated.Limits, "prices": updated.Prices, "version": updated.Version, "updated_at": updated.UpdatedAt})
	})
	g.GET("/tasks", func(c *gin.Context) {
		page, size := studioPage(c)
		status := strings.TrimSpace(c.Query("status"))
		keyword := strings.TrimSpace(c.Query("keyword"))
		allowedStatus := map[string]bool{"": true, "queued": true, "submitting": true, "submitted": true, "running": true, "succeeded": true, "partial_failed": true, "failed": true, "cancelled": true, "uncertain": true}
		if !allowedStatus[status] || len([]rune(keyword)) > 100 {
			studioError(c, 400, "任务筛选参数不合法")
			return
		}
		items, total, err := store.ListBatchesFiltered(c, 0, status, keyword, size, (page-1)*size)
		if err != nil {
			studioError(c, 500, "任务读取失败")
			return
		}
		studioOK(c, gin.H{"items": items, "total": total, "page": page, "page_size": size})
	})
	g.GET("/tasks/:id", func(c *gin.Context) {
		item, err := store.GetBatch(c, 0, c.Param("id"))
		if errors.Is(err, sql.ErrNoRows) {
			studioError(c, 404, "任务不存在")
			return
		}
		if err != nil {
			studioError(c, 500, "任务读取失败")
			return
		}
		studioOK(c, item)
	})
	g.GET("/ledger", func(c *gin.Context) {
		page, size := studioPage(c)
		status := strings.TrimSpace(c.Query("status"))
		keyword := strings.TrimSpace(c.Query("keyword"))
		allowed := map[string]bool{"": true, "reserved": true, "settled": true, "released": true, "refunded": true, "pending": true, "failed": true}
		if !allowed[status] || len([]rune(keyword)) > 100 {
			studioError(c, 400, "费用筛选参数不合法")
			return
		}
		items, total, err := store.ListFeesForManagement(c, status, keyword, size, (page-1)*size)
		if err != nil {
			studioError(c, 500, "费用流水读取失败")
			return
		}
		studioOK(c, gin.H{"items": items, "total": total, "page": page, "page_size": size, "has_more": int64(page*size) < total})
	})
}
