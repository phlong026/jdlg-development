package route

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"service/config"
	"service/model"
)

const studioPolishPriceCode = "ai_video_studio_prompt_optimize"

type studioPolishService struct {
	store        *model.AIVideoStudioStore
	media        *model.AIVideoStudioMediaStore
	loadProvider func(context.Context) (chatStreamProvider, error)
	client       *http.Client
}

func newStudioPolishService(store *model.AIVideoStudioStore, media *model.AIVideoStudioMediaStore, configs *model.AIAPIConfigModel, cfg *config.Config) *studioPolishService {
	return &studioPolishService{store: store, media: media, client: &http.Client{}, loadProvider: func(ctx context.Context) (chatStreamProvider, error) {
		if configs == nil || cfg == nil {
			return chatStreamProvider{}, errors.New("文本服务尚未配置")
		}
		textConfig, err := configs.GetByTaskType("ai_chat")
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return chatStreamProvider{}, errors.New("文本配置暂不可读")
		}
		if textConfig != nil {
			if !textConfig.IsActive {
				return chatStreamProvider{}, errors.New("文本配置尚未启用")
			}
			if textConfig.Method != "" && !strings.EqualFold(textConfig.Method, "POST") {
				return chatStreamProvider{}, errors.New("润色需要POST文本接口")
			}
			if textConfig.APIKeyLocation != "" && textConfig.APIKeyLocation != "header_bearer" {
				return chatStreamProvider{}, errors.New("润色需要Bearer文本接口")
			}
		}
		selected, err := resolveChatStreamProvider(textConfig, cfg.AI.LaoZhangAPIKey, cfg.AI.ArkAPIKey)
		if err != nil {
			return selected, err
		}
		parsed, err := url.Parse(selected.endpoint)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.TrimSpace(selected.apiKey) == "" {
			return selected, errors.New("文本接口地址或凭据尚未配置")
		}
		path := strings.TrimRight(parsed.Path, "/")
		if selected.completions && !strings.HasSuffix(path, "/chat/completions") || !selected.completions && !strings.HasSuffix(path, "/responses") {
			return selected, errors.New("文本接口协议尚未支持")
		}
		return selected, nil
	}}
}

func studioPolishPrice(config model.AIVideoStudioConfig) (model.AIVideoStudioPrice, bool) {
	var found model.AIVideoStudioPrice
	count := 0
	for _, price := range config.Prices {
		if price.Code == studioPolishPriceCode {
			found = price
			count++
		}
	}
	return found, count == 1 && found.Enabled && found.BillingUnit == "per_request" && found.UnitStones != nil && *found.UnitStones > 0 && *found.UnitStones <= maxAIVideoStudioUnitStones
}

func (s *studioPolishService) readiness(ctx context.Context, config model.AIVideoStudioConfig) (model.AIVideoStudioPrice, chatStreamProvider, error) {
	var p chatStreamProvider
	price, ok := studioPolishPrice(config)
	if !config.Enabled || !config.PromptOptimize.Enabled {
		return price, p, errors.New("AI润色暂未开放")
	}
	if !ok {
		return price, p, errors.New("润色价格尚未配置")
	}
	if err := validateStudioPolishConfig(config.PromptOptimize); err != nil {
		return price, p, err
	}
	if s == nil || s.store == nil || !s.store.Ready || s.loadProvider == nil {
		return price, p, errors.New("润色服务暂未就绪")
	}
	p, err := s.loadProvider(ctx)
	return price, p, err
}

func validateStudioPolishConfig(c model.AIVideoStudioPolishConfig) error {
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 120 || c.Enabled && c.TimeoutSeconds < 1 {
		return errors.New("润色超时必须在1到120秒")
	}
	if !utf8.ValidString(c.Model) || utf8.RuneCountInString(c.Model) > 128 {
		return errors.New("润色模型名称不合法")
	}
	for _, r := range c.Model {
		if unicode.IsControl(r) {
			return errors.New("润色模型名称不合法")
		}
	}
	if c.Enabled && strings.TrimSpace(c.Model) == "" {
		return errors.New("请指定润色模型")
	}
	return nil
}

func registerStudioPolishRoutes(g *gin.RouterGroup, s *studioPolishService) {
	p := g.Group("/prompt-polish")
	p.GET("/capabilities", func(c *gin.Context) {
		cfg, err := s.store.Config(c, false)
		if err != nil {
			studioError(c, 503, "润色配置暂不可读")
			return
		}
		price, _, readyErr := s.readiness(c, cfg)
		reason := ""
		if readyErr != nil {
			reason = readyErr.Error()
		}
		var amount int64
		if price.UnitStones != nil {
			amount = *price.UnitStones
		}
		studioOK(c, gin.H{"enabled": readyErr == nil, "disabled_reason": reason, "model": cfg.PromptOptimize.Model, "total_stones": amount, "config_version": cfg.Version})
	})
	p.POST("/quotes", s.quote)
	p.POST("/tasks", func(c *gin.Context) {
		var in struct {
			QuoteID string `json:"quote_id"`
			Key     string `json:"idempotency_key"`
		}
		if c.ShouldBindJSON(&in) != nil || in.QuoteID == "" || strings.TrimSpace(in.Key) == "" || len(in.Key) > 128 {
			studioError(c, 400, "报价或请求标识不合法")
			return
		}
		// Replays stay readable even after the operator disables new work.
		previous, err := s.store.GetPolishSubmission(c, GetTokenUserID(c), in.Key)
		if err == nil {
			if previous.QuoteID != in.QuoteID {
				studioError(c, 409, "请求标识已经用于另一报价")
				return
			}
			studioOK(c, studioPolishTaskView(previous, false))
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			studioPolishError(c, err, "读取提交")
			return
		}
		cfg, err := s.store.Config(c, false)
		if err != nil {
			studioPolishError(c, err, "读取配置")
			return
		}
		if _, _, err = s.readiness(c, cfg); err != nil {
			studioError(c, 503, err.Error())
			return
		}
		task, _, err := s.store.CreatePolishTask(c, GetTokenUserID(c), in.QuoteID, in.Key)
		if err != nil {
			studioPolishError(c, err, "创建任务")
			return
		}
		studioOK(c, studioPolishTaskView(task, false))
	})
	p.GET("/submissions/:key", func(c *gin.Context) {
		if len(c.Param("key")) > 128 {
			studioError(c, 400, "请求标识不合法")
			return
		}
		task, err := s.store.GetPolishSubmission(c, GetTokenUserID(c), c.Param("key"))
		if err != nil {
			studioPolishError(c, err, "查询提交")
			return
		}
		studioOK(c, studioPolishTaskView(task, false))
	})
	p.GET("/tasks/:id", func(c *gin.Context) {
		task, err := s.store.GetPolishTask(c, GetTokenUserID(c), c.Param("id"))
		if err != nil {
			studioPolishError(c, err, "读取任务")
			return
		}
		studioOK(c, studioPolishTaskView(task, false))
	})
	p.POST("/tasks/:id/cancel", func(c *gin.Context) {
		if err := s.store.CancelPolishTask(c, GetTokenUserID(c), c.Param("id")); err != nil {
			studioPolishError(c, err, "取消任务")
			return
		}
		task, err := s.store.GetPolishTask(c, GetTokenUserID(c), c.Param("id"))
		if err != nil {
			studioPolishError(c, err, "读取取消结果")
			return
		}
		studioOK(c, studioPolishTaskView(task, false))
	})
}

func (s *studioPolishService) quote(c *gin.Context) {
	var in struct {
		DraftID  int64 `json:"draft_id"`
		Revision int64 `json:"expected_revision"`
	}
	if c.ShouldBindJSON(&in) != nil || in.DraftID < 1 || in.Revision < 1 {
		studioError(c, 400, "草稿参数不合法")
		return
	}
	cfg, err := s.store.Config(c, false)
	if err != nil {
		studioPolishError(c, err, "读取报价配置")
		return
	}
	price, provider, err := s.readiness(c, cfg)
	if err != nil {
		studioError(c, 503, err.Error())
		return
	}
	draft, err := s.store.GetDraft(c, GetTokenUserID(c), in.DraftID)
	if err != nil {
		studioPolishError(c, err, "读取草稿")
		return
	}
	if draft.Revision != in.Revision {
		studioError(c, 409, "草稿已更新，请重新润色")
		return
	}
	if !cfg.Modes[draft.Mode] {
		studioError(c, 403, "当前生成方式尚未开放")
		return
	}
	input, err := studioPolishDraftInput(*draft)
	if err != nil {
		studioError(c, 400, err.Error())
		return
	}
	if err = validateAIVideoStudioDraftSave(input, studioLimitsFromConfig(cfg)); err != nil {
		studioError(c, 400, err.Error())
		return
	}
	if err = validateAIVideoStudioMedia(c, s.media, GetTokenUserID(c), &input); err != nil {
		studioError(c, 400, err.Error())
		return
	}
	if _, _, err = studioPolishInstructions(*draft); err != nil {
		studioError(c, 400, err.Error())
		return
	}
	snapshot, err := json.Marshal(studioPolishSnapshot{AIVideoStudioDraft: *draft, Model: cfg.PromptOptimize.Model, Provider: provider.provider, Endpoint: provider.endpoint, Completions: provider.completions})
	if err != nil {
		studioPolishError(c, err, "构建报价")
		return
	}
	hash, err := model.AIVideoStudioSnapshotHash(snapshot)
	if err != nil {
		studioPolishError(c, err, "构建报价指纹")
		return
	}
	q := model.AIVideoStudioQuote{ID: uuid.NewString(), UserID: GetTokenUserID(c), DraftID: draft.ID, DraftRevision: draft.Revision, QuoteVersion: cfg.Version, InputHash: hash, PriceCode: studioPolishPriceCode, UnitStones: *price.UnitStones, BillingUnit: "per_request", BillableUnits: 1, TotalStones: *price.UnitStones, InputSnapshot: snapshot, ExpiresAt: time.Now().Add(10 * time.Minute)}
	if err = s.store.CreateQuote(c, q); err != nil {
		studioPolishError(c, err, "保存报价")
		return
	}
	// Supplier binding remains server-side; clients only need the draft and opaque hash.
	q.InputSnapshot, _ = json.Marshal(draft)
	studioOK(c, q)
}

type studioPolishSnapshot struct {
	model.AIVideoStudioDraft
	Model       string `json:"polish_model"`
	Provider    string `json:"polish_provider"`
	Endpoint    string `json:"polish_endpoint"`
	Completions bool   `json:"polish_completions"`
}

func studioPolishDraftInput(d model.AIVideoStudioDraft) (aiVideoStudioDraftInput, error) {
	var refs []aiVideoStudioReferenceInput
	err := json.Unmarshal(d.References, &refs)
	return aiVideoStudioDraftInput{Name: d.Name, Mode: d.Mode, Prompt: d.Prompt, ReferencePrompt: d.ReferencePrompt, FirstFrameMediaID: d.FirstFrameMediaID, LastFrameMediaID: d.LastFrameMediaID, References: refs, Ratio: d.Ratio, Resolution: d.Resolution, Duration: d.Duration, Quantity: d.Quantity}, err
}

func studioPolishTaskView(task *model.AIVideoStudioPolishTask, admin bool) gin.H {
	data, _ := json.Marshal(task)
	view := gin.H{}
	_ = json.Unmarshal(data, &view)
	var snapshot model.AIVideoStudioDraft
	if json.Unmarshal(task.InputSnapshot, &snapshot) == nil {
		original := snapshot.Prompt
		if snapshot.Mode == "reference" {
			original = snapshot.ReferencePrompt
		}
		view["original_prompt"] = original
		view["input_snapshot"] = snapshot
	}
	if admin {
		view["user_id"] = task.UserID
	}
	return view
}

func studioPolishError(c *gin.Context, err error, operation string) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		studioError(c, 404, "润色记录不存在")
	case errors.Is(err, model.ErrInsufficientStones):
		studioError(c, 402, "灵石余额不足")
	case errors.Is(err, model.ErrAIVideoStudioConflict):
		studioError(c, 409, "状态或报价已变化，请查询原任务或重新报价")
	default:
		log.Printf("[AI视频润色] operation=%s err_type=%T", operation, err)
		studioError(c, 503, "润色服务暂不可用，请稍后查询")
	}
}

func startStudioPolishWorker(ctx context.Context, s *studioPolishService) func() {
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				if err := s.step(workerCtx); err != nil && workerCtx.Err() == nil {
					log.Printf("[AI视频润色] operation=worker_step err_type=%T", err)
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}

func (s *studioPolishService) step(ctx context.Context) error {
	if err := s.store.RecoverPolishTasks(ctx); err != nil {
		return err
	}
	task, err := s.store.ClaimPolishTask(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil || task == nil {
		return err
	}
	// Settlement is independent of the caller's HTTP connection and only a
	// claimed queued task may enter this single supplier call.
	cfg, err := s.store.Config(ctx, false)
	if err != nil {
		return s.store.FinalizePolishTask(ctx, task.TaskID, task.LeaseOwner, "failed", "config_unavailable", "配置读取失败，原文已保留")
	}
	_, provider, err := s.readiness(ctx, cfg)
	if err != nil {
		return s.store.FinalizePolishTask(ctx, task.TaskID, task.LeaseOwner, "failed", "configuration_disabled", "配置已变化，费用已释放")
	}
	var snapshot studioPolishSnapshot
	if json.Unmarshal(task.InputSnapshot, &snapshot) != nil || snapshot.Model != cfg.PromptOptimize.Model || snapshot.Provider != provider.provider || snapshot.Endpoint != provider.endpoint || snapshot.Completions != provider.completions {
		return s.store.FinalizePolishTask(ctx, task.TaskID, task.LeaseOwner, "failed", "configuration_changed", "文本配置已变化，请重新润色")
	}
	system, input, err := studioPolishInstructions(snapshot.AIVideoStudioDraft)
	if err != nil {
		return s.store.FinalizePolishTask(ctx, task.TaskID, task.LeaseOwner, "failed", "invalid_input", err.Error())
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.PromptOptimize.TimeoutSeconds)*time.Second)
	result, uncertain, callErr := callStudioPolish(callCtx, s.client, provider, snapshot.Model, task.TaskID, system, input)
	cancel()
	// The parent can be shutting down after a paid POST. Record its outcome
	// using a fresh bounded context; never reset the task to queued.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer finishCancel()
	if callErr != nil {
		log.Printf("[AI视频润色] operation=supplier_call task=%s uncertain=%t err_type=%T", task.TaskID, uncertain, callErr)
		if uncertain {
			return s.store.MarkPolishUncertain(finishCtx, task.TaskID, task.LeaseOwner, "润色结果待核对，请稍后查询")
		}
		return s.store.FinalizePolishTask(finishCtx, task.TaskID, task.LeaseOwner, "failed", "supplier_rejected", "润色未成功，原文已保留")
	}
	result, err = validateStudioPolishOutput(snapshot.AIVideoStudioDraft, result)
	if err != nil {
		return s.store.FinalizePolishTask(finishCtx, task.TaskID, task.LeaseOwner, "failed", "invalid_output", "润色结果未通过校验，原文已保留")
	}
	return s.store.SavePolishResult(finishCtx, task.TaskID, task.LeaseOwner, result)
}

func registerStudioPolishManagement(g *gin.RouterGroup, s *studioPolishService) {
	p := g.Group("/prompt-polish")
	p.GET("/tasks", func(c *gin.Context) {
		page, size := studioPage(c)
		status := strings.TrimSpace(c.Query("status"))
		keyword := strings.TrimSpace(c.Query("keyword"))
		if !studioPolishStatus(status) || utf8.RuneCountInString(keyword) > 100 {
			studioError(c, 400, "筛选条件不合法")
			return
		}
		items, total, err := s.store.ListPolishTasks(c, status, keyword, size, (page-1)*size)
		if err != nil {
			studioPolishError(c, err, "读取管理列表")
			return
		}
		views := make([]gin.H, 0, len(items))
		for i := range items {
			views = append(views, studioPolishTaskView(&items[i], true))
		}
		studioOK(c, gin.H{"items": views, "total": total, "page": page, "page_size": size})
	})
	p.GET("/tasks/:id", func(c *gin.Context) {
		task, err := s.store.GetPolishTask(c, 0, c.Param("id"))
		if err != nil {
			studioPolishError(c, err, "读取管理任务")
			return
		}
		studioOK(c, studioPolishTaskView(task, true))
	})
	p.POST("/tasks/:id/reconcile", func(c *gin.Context) {
		var in struct {
			Action   string `json:"action"`
			Evidence string `json:"evidence"`
			Result   string `json:"result"`
		}
		if c.ShouldBindJSON(&in) != nil || utf8.RuneCountInString(strings.TrimSpace(in.Evidence)) < 10 || utf8.RuneCountInString(in.Evidence) > 500 || in.Action != "confirmed_rejected" && in.Action != "confirmed_result" {
			studioError(c, 400, "请填写明确的核对动作和10到500字依据")
			return
		}
		task, err := s.store.GetPolishTask(c, 0, c.Param("id"))
		if err != nil {
			studioPolishError(c, err, "读取核对任务")
			return
		}
		if in.Action == "confirmed_result" {
			var draft model.AIVideoStudioDraft
			if json.Unmarshal(task.InputSnapshot, &draft) != nil {
				studioError(c, 409, "任务快照无法读取")
				return
			}
			in.Result, err = validateStudioPolishOutput(draft, in.Result)
			if err != nil {
				studioError(c, 400, err.Error())
				return
			}
		}
		if err = s.store.ReconcilePolishTask(c, task.TaskID, in.Action, strings.TrimSpace(in.Evidence), in.Result, int64(GetUserID(c)), GetUsername(c)); err != nil {
			studioPolishError(c, err, "人工核对")
			return
		}
		updated, err := s.store.GetPolishTask(c, 0, task.TaskID)
		if err != nil {
			studioPolishError(c, err, "读取核对结果")
			return
		}
		studioOK(c, studioPolishTaskView(updated, true))
	})
}

func studioPolishStatus(status string) bool {
	switch status {
	case "", "queued", "submitting", "uncertain", "completed", "failed", "cancelled":
		return true
	}
	return false
}
