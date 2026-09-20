package route

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	cos "github.com/tencentyun/cos-go-sdk-v5"
	"service/component"
	"service/config"
	"service/report"
	"service/reportnotify"
	"service/reportwecom"
)

type ReportModule struct {
	Service           *report.Service
	Worker            *report.Worker
	Renderer          report.Renderer
	Wecom             *reportwecom.Service
	WecomConfig       reportwecom.Config
	DB                *sql.DB
	DecisionEnabled   bool
	HouseEnabled      bool
	Notifications     *reportnotify.Service
	EmbeddedWorker    bool
	WorkerConcurrency int
}
type unavailablePackCatalog struct{}

func (unavailablePackCatalog) Match(report.Input) (report.ReviewedPack, error) {
	return report.ReviewedPack{}, report.ErrPackUnavailable
}
func (unavailablePackCatalog) Get(string, string) (report.ReviewedPack, error) {
	return report.ReviewedPack{}, report.ErrPackUnavailable
}

func NewReportModule(ctx context.Context, db *sql.DB, appConfig *config.Config) (*ReportModule, error) {
	cfg, err := report.LoadConfig()
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	store, err := report.NewMySQLStore(db)
	if err != nil {
		return nil, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := store.VerifySchema(checkCtx); err != nil {
		return nil, err
	}
	if cfg.MultiAgentEnabled {
		if err := store.VerifyAgentSchema(checkCtx); err != nil {
			return nil, err
		}
	}
	if cfg.DecisionEnabled {
		if err := store.VerifyDecisionSchema(checkCtx); err != nil {
			return nil, err
		}
	}
	pdfConfig := component.ReportPDFConfig{PythonPath: os.Getenv("REPORT_PDF_PYTHON"), ScriptPath: os.Getenv("REPORT_PDF_SCRIPT"), FontPath: os.Getenv("REPORT_PDF_FONT"), AssetRoot: os.Getenv("REPORT_ASSET_ROOT"), Timeout: 90 * time.Second}
	if cfg.HouseEnabled {
		if !filepath.IsAbs(pdfConfig.AssetRoot) || pdfConfig.PythonPath == "" {
			return nil, report.ErrNotReady
		}
		info, err := os.Stat(filepath.Join(filepath.Dir(pdfConfig.ScriptPath), "house_v1.py"))
		if err != nil || !info.Mode().IsRegular() {
			return nil, report.ErrNotReady
		}
	}
	var renderer report.Renderer
	var storage report.PrivateStorage
	if pdfConfig.PythonPath != "" {
		for _, path := range []string{pdfConfig.PythonPath, pdfConfig.ScriptPath, pdfConfig.FontPath} {
			if !filepath.IsAbs(path) {
				return nil, report.ErrNotReady
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() {
				return nil, report.ErrNotReady
			}
		}
		renderer, err = component.NewReportPDFRenderer(pdfConfig)
		if err != nil {
			return nil, err
		}
		if cfg.MultiAgentEnabled {
			version, err := reportArtifactVersion(pdfConfig)
			if err != nil {
				return nil, err
			}
			renderer, err = report.NewCachedRenderer(renderer, os.Getenv("REPORT_ARTIFACT_DIR"), version)
			if err != nil {
				return nil, err
			}
		}
	}
	if os.Getenv("REPORT_COS_BUCKET") != "" {
		storage, err = privateReportStorage(appConfig)
		if err != nil {
			return nil, err
		}
	}
	if renderer == nil || storage == nil {
		cfg.PaymentEnabled = false
		renderer, storage = nil, nil
	}
	var catalog report.PackCatalog = unavailablePackCatalog{}
	if cfg.ManifestPath != "" {
		packs, err := report.NewFilePackCatalog(cfg.ManifestPath)
		if err != nil {
			return nil, err
		}
		if err := packs.VerifyAssetRoot(pdfConfig.AssetRoot); err != nil {
			return nil, err
		}
		catalog = packs
	}
	wecomConfig, wecomErr := reportwecom.LoadConfig()
	cfg.LegacyPaymentBlocked = wecomErr != nil || !wecomConfig.Enabled || wecomConfig.AppID != appConfig.Wechat.AppID
	if !appConfig.VirtualPay.Enabled || !cfg.DecisionEnabled && (wecomErr != nil || !wecomConfig.Enabled || wecomConfig.AppID != appConfig.Wechat.AppID) {
		cfg.PaymentEnabled = false
		log.Printf("[report] warn checkout=disabled reason=delivery_or_payment_configuration")
	}
	var wecomStore *reportwecom.SQLStore
	var wecomProvider *reportwecom.HTTPProvider
	if wecomErr == nil && wecomConfig.Enabled && wecomConfig.AppID == appConfig.Wechat.AppID {
		wecomStore = &reportwecom.SQLStore{DB: db}
		if err := wecomStore.VerifySchema(checkCtx); err != nil {
			if !cfg.DecisionEnabled {
				return nil, err
			}
			cfg.LegacyPaymentBlocked = true
			wecomStore = nil
			log.Printf("[report] warn delivery=unavailable reason=wecom_schema_not_ready")
		} else {
			wecomProvider = reportwecom.NewHTTPProvider(wecomConfig)
			// Legacy checkout still requires a usable welcome delivery path.
			if err := wecomProvider.Warmup(checkCtx); err != nil {
				cfg.LegacyPaymentBlocked = true
				if !cfg.DecisionEnabled {
					cfg.PaymentEnabled = false
				}
				log.Printf("[report] warn delivery=unavailable reason=wecom_not_ready")
			}
		}
	}
	service, err := report.NewService(cfg, store, catalog, storage)
	if err != nil {
		return nil, err
	}
	var textClient report.TextClient
	if !cfg.MultiAgentEnabled && os.Getenv("REPORT_TEXT_ENDPOINT") != "" {
		if !strings.HasPrefix(os.Getenv("REPORT_TEXT_ENDPOINT"), "https://") {
			return nil, report.ErrInvalidInput
		}
		textClient, err = report.NewHTTPTextClient(os.Getenv("REPORT_TEXT_ENDPOINT"), os.Getenv("REPORT_TEXT_API_KEY"), os.Getenv("REPORT_TEXT_MODEL"), 20*time.Second, &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
		if err != nil {
			return nil, err
		}
	}
	var workerOptions []report.WorkerOption
	if cfg.MultiAgentEnabled {
		agentClient, err := configuredReportAgentClient()
		if err != nil {
			return nil, err
		}
		agents, err := report.NewMultiAgentRunner(agentClient, store)
		if err != nil {
			return nil, err
		}
		var runner report.AgentRunner = agents
		if cfg.DecisionEnabled {
			decision, err := report.NewReportOrchestrator(agentClient, store)
			if err != nil {
				return nil, err
			}
			dispatcher := report.ProtocolDispatcher{Legacy: agents, Decision: decision}
			if cfg.HouseEnabled {
				house, err := configuredHouseOrchestrator(ctx, db, appConfig, agentClient, store, pdfConfig.AssetRoot)
				if err != nil {
					return nil, err
				}
				dispatcher.House = house
			}
			runner = dispatcher
		}
		workerOptions = append(workerOptions, report.WithAgentRunner(runner))
	}
	worker, err := report.NewWorker(cfg, store, report.NewGenerator(textClient), catalog, renderer, storage, workerOptions...)
	if err != nil {
		return nil, err
	}
	module := &ReportModule{Service: service, Worker: worker, Renderer: renderer, DB: db, WecomConfig: wecomConfig, DecisionEnabled: cfg.DecisionEnabled, HouseEnabled: cfg.HouseEnabled, EmbeddedWorker: cfg.EmbeddedWorker, WorkerConcurrency: cfg.WorkerConcurrency}
	if cfg.HouseEnabled {
		module.Notifications, err = configuredReportNotifications(ctx, db)
		if err != nil {
			return nil, err
		}
	}
	if wecomProvider != nil {
		module.Wecom = &reportwecom.Service{Provider: wecomProvider, Store: wecomStore, Identity: module, Binder: service, EmployeeID: wecomConfig.EmployeeID}
	}
	return module, nil
}

func configuredReportAgentClient() (*report.HTTPAgentClient, error) {
	endpoint := strings.TrimSpace(os.Getenv("REPORT_TEXT_ENDPOINT"))
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, report.ErrInvalidInput
	}
	model := strings.TrimSpace(os.Getenv("REPORT_TEXT_MODEL"))
	roleModel := func(name string) string {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
		return model
	}
	version := strings.TrimSpace(os.Getenv("REPORT_AGENT_CONFIG_VERSION"))
	if version == "" {
		version = "v1"
	}
	return report.NewHTTPAgentClient(report.MultiAgentHTTPConfig{
		Endpoint: endpoint, APIKey: os.Getenv("REPORT_TEXT_API_KEY"), ConfigVersion: version,
		PlannerModel: roleModel("REPORT_AGENT_PLANNER_MODEL"), WriterModel: roleModel("REPORT_AGENT_WRITER_MODEL"),
		VisualModel: roleModel("REPORT_AGENT_VISUAL_MODEL"), ReviewerModel: roleModel("REPORT_AGENT_REVIEWER_MODEL"),
		DiagnosisModel: roleModel("REPORT_AGENT_DIAGNOSIS_MODEL"), DecisionModel: roleModel("REPORT_AGENT_DECISION_MODEL"),
		BudgetModel: roleModel("REPORT_AGENT_BUDGET_MODEL"), EditorModel: roleModel("REPORT_AGENT_EDITOR_MODEL"),
		SpatialModel: roleModel("REPORT_AGENT_SPATIAL_MODEL"),
		Timeout:      45 * time.Second, MaxResponseBytes: 256 << 10,
	}, &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}

func reportArtifactVersion(config component.ReportPDFConfig) (string, error) {
	digest := sha256.New()
	sources := []string{config.ScriptPath, filepath.Join(filepath.Dir(config.ScriptPath), "decision_v5.py"), config.FontPath}
	houseScript := filepath.Join(filepath.Dir(config.ScriptPath), "house_v1.py")
	if _, err := os.Stat(houseScript); err == nil {
		sources = append(sources, houseScript)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", report.ErrNotReady
	}
	for _, source := range sources {
		file, err := os.Open(source)
		if err != nil {
			log.Printf("[report] error artifact_version=open_failed")
			return "", report.ErrNotReady
		}
		written, readErr := io.Copy(digest, io.LimitReader(file, 128<<20+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || written > 128<<20 {
			log.Printf("[report] error artifact_version=read_failed")
			return "", report.ErrNotReady
		}
		_, _ = digest.Write([]byte{0})
	}
	_, _ = digest.Write([]byte(os.Getenv("REPORT_AGENT_CONFIG_VERSION")))
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func privateReportStorage(cfg *config.Config) (report.PrivateStorage, error) {
	bucket, region := os.Getenv("REPORT_COS_BUCKET"), os.Getenv("REPORT_COS_REGION")
	// A separate, operator-verified private bucket is required; never reuse the public asset URL builder.
	if !hostPart(bucket) || !hostPart(region) || bucket == cfg.COS.Bucket || os.Getenv("REPORT_COS_PRIVATE_CONFIRMED") != "true" {
		return nil, report.ErrNotReady
	}
	endpoint, err := url.Parse("https://" + bucket + ".cos." + region + ".myqcloud.com")
	if err != nil {
		return nil, report.ErrInvalidInput
	}
	id, key := os.Getenv("REPORT_COS_SECRET_ID"), os.Getenv("REPORT_COS_SECRET_KEY")
	if id == "" {
		id = cfg.COS.SecretID
	}
	if key == "" {
		key = cfg.COS.SecretKey
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: endpoint}, &http.Client{Timeout: 20 * time.Second, Transport: &cos.AuthorizationTransport{SecretID: id, SecretKey: key}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
	return report.NewCOSPrivateStorage(client, id, key, "private-reports", 20*time.Second)
}
func hostPart(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// Run uses database leases for coordination across both embedded and standalone workers.
func (m *ReportModule) Run(ctx context.Context) {
	if m == nil || m.Worker == nil {
		return
	}
	concurrency := m.WorkerConcurrency
	if concurrency < 1 || concurrency > 8 {
		concurrency = 1
	}
	var wg sync.WaitGroup
	if m.Notifications != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.runNotifications(ctx)
		}()
	}
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				worked, err := m.Worker.RunOnce(ctx)
				if err != nil && ctx.Err() == nil {
					log.Printf("[report] warn worker_cycle=failed")
				}
				if worked && err == nil {
					continue
				}
				timer := time.NewTimer(2 * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
	}
	wg.Wait()
}
func (m *ReportModule) OwnerUnionID(ctx context.Context, userID int64, id string) (string, error) {
	view, err := m.Service.Get(ctx, userID, id)
	if err != nil {
		return "", err
	}
	if !view.Paid {
		return "", report.ErrPaymentRequired
	}
	if err := m.requireActivePayment(ctx, userID, id); err != nil {
		return "", err
	}
	var union string
	if err := m.DB.QueryRowContext(ctx, "SELECT COALESCE(unionid,'') FROM users WHERE id=?", userID).Scan(&union); err != nil {
		log.Printf("[report] error operation=read_owner_identity")
		return "", report.ErrNotReady
	}
	if union == "" {
		return "", reportwecom.ErrUnionIDRequired
	}
	return union, nil
}
func (m *ReportModule) requireActivePayment(ctx context.Context, userID int64, id string) error {
	var status string
	err := m.DB.QueryRowContext(ctx, `SELECT o.status FROM building_reports r JOIN user_orders o ON o.order_no=r.payment_ref AND o.user_id=r.user_id WHERE r.report_id=? AND r.user_id=? AND o.type='report'`, id, userID).Scan(&status)
	if err != nil || status != "success" {
		return report.ErrPaymentRequired
	}
	return nil
}

func RegisterReportRoutes(public, auth, v1 *gin.RouterGroup, module *ReportModule) {
	registerDecisionQuestions(public, module)
	registerHouseQuestions(public, module)
	public.GET("/reports/config", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"enabled": module != nil, "decision_enabled": module != nil && module.DecisionEnabled, "house_enabled": module != nil && module.HouseEnabled}})
	})
	group := auth.Group("/reports")
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		if module == nil {
			reportError(c, report.ErrDisabled)
			c.Abort()
			return
		}
		if GetTokenCodeSession(c) == nil {
			c.AbortWithStatusJSON(401, gin.H{"code": 401, "msg": "请先登录"})
		}
	})
	registerDecisionReportRoutes(group, module)
	registerHouseRoutes(group, module)
	group.POST("", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var input report.Input
		if decoder.Decode(&input) != nil {
			reportError(c, report.ErrInvalidInput)
			return
		}
		var tail any
		if decoder.Decode(&tail) != io.EOF {
			reportError(c, report.ErrInvalidInput)
			return
		}
		result, err := module.Service.CreatePreview(c.Request.Context(), GetTokenCodeSession(c).UserID, input)
		if err != nil {
			reportError(c, err)
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": result})
	})
	group.GET("/recent", func(c *gin.Context) {
		result, err := module.Service.Latest(c.Request.Context(), GetTokenCodeSession(c).UserID)
		if err != nil {
			reportError(c, err)
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": result})
	})
	group.GET("/:id", func(c *gin.Context) {
		ctx, user := c.Request.Context(), GetTokenCodeSession(c).UserID
		result, err := module.Service.Get(ctx, user, c.Param("id"))
		if err != nil {
			reportError(c, err)
			return
		}
		quote, err := module.Service.GetPaymentQuote(ctx, user, c.Param("id"))
		if err != nil {
			reportError(c, err)
			return
		}
		if result.CanPay && !report.IsDecisionProtocol(result.ProtocolVersion) {
			var union string
			if module.DB.QueryRowContext(ctx, "SELECT COALESCE(unionid,'') FROM users WHERE id=?", user).Scan(&union) != nil || union == "" {
				result.CanPay = false
				quote.CanPay = false
				result.PaymentBlockReason = "请先使用当前微信账号完成身份关联，再购买报告。"
			}
		}
		if result.PaymentBlockReason != "" && !strings.Contains(result.PaymentBlockReason, "请先") {
			result.PaymentBlockReason = reportBlockMessage(result.PaymentBlockReason)
		}
		c.JSON(200, gin.H{"code": 0, "data": struct {
			report.SafeView
			Quote               report.PaymentQuote `json:"quote"`
			PreviewPDFAvailable bool                `json:"preview_pdf_available"`
		}{result, quote, module.Renderer != nil && !report.IsDecisionProtocol(result.ProtocolVersion)}})
	})
	group.POST("/:id/retry", func(c *gin.Context) {
		result, err := module.Service.Retry(c.Request.Context(), GetTokenCodeSession(c).UserID, c.Param("id"))
		if err != nil {
			reportError(c, err)
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": result})
	})
	group.GET("/:id/document", func(c *gin.Context) {
		ctx, user, id, mode := c.Request.Context(), GetTokenCodeSession(c).UserID, c.Param("id"), c.Query("mode")
		if mode == report.ModeFull {
			if err := module.requireActivePayment(ctx, user, id); err != nil {
				reportError(c, err)
				return
			}
		}
		if mode == report.ModePreview {
			if err := module.Service.EnsurePreviewPDF(ctx, user, id, module.Renderer); err != nil {
				reportError(c, err)
				return
			}
		}
		link, err := module.Service.GetDownloadURL(ctx, user, id, mode)
		if err != nil {
			reportError(c, err)
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"document_url": link}})
	})
	group.POST("/:id/wecom", func(c *gin.Context) {
		if module.Wecom == nil {
			reportError(c, reportwecom.ErrNotConfigured)
			return
		}
		result, err := module.Wecom.EnsureContact(c.Request.Context(), c.Param("id"), GetTokenCodeSession(c).UserID)
		if err != nil {
			reportError(c, err)
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": result})
	})
	if module != nil && module.Wecom != nil {
		// 事件统一收口：先落库再应答企微，报告重试与 CRM 转发由 inbox 补偿（P0-2）
		inbox, inboxErr := newWecomEventInbox(module.DB, module.WecomConfig, module.Wecom)
		if inboxErr != nil {
			log.Printf("[wecom-inbox] 事件收口表不可用，回调退回内联模式: %v", inboxErr)
		}
		handler := gin.WrapH(reportwecom.Callback(module.WecomConfig, module.Wecom))
		if inbox != nil {
			handler = gin.WrapH(reportwecom.Callback(module.WecomConfig, module.Wecom, inbox))
			startWecomEventDispatch(inbox)
		}
		v1.GET("/wecom/reports/callback", handler)
		v1.POST("/wecom/reports/callback", handler)
	}
}
func reportBlockMessage(code string) string {
	switch code {
	case "already_paid":
		return "这份报告已购买，无需重复支付。"
	case "preview_not_ready":
		return "先读完您的建房方向，再决定是否深入。"
	case "confirmation_required":
		return "请先确认诊断方向和关注重点，再购买完整报告。"
	case "invalid_confirmation", "invalid_preview", "invalid_frozen_input":
		return "报告信息版本不一致，请重新填写并生成预览。"
	case "reviewed_pack_unavailable":
		return "暂未匹配到符合条件的审核方案，当前不收取报告费用。"
	default:
		return "完整报告服务尚未开放购买，请稍后查看。"
	}
}
func reportError(c *gin.Context, err error) {
	status, msg := 503, "报告服务暂时不可用，请稍后重试。"
	switch {
	case errors.Is(err, report.ErrInvalidInput):
		status, msg = 400, "信息不完整或超出支持范围，请返回问卷核对。"
	case errors.Is(err, report.ErrNotFound), errors.Is(err, report.ErrForbidden):
		status, msg = 404, "没有找到属于您的报告，请核对登录账号。"
	case errors.Is(err, report.ErrPaymentRequired):
		status, msg = 403, "付款尚未确认或订单已取消，请刷新订单状态。"
	case errors.Is(err, report.ErrBindingRequired):
		status, msg = 403, "请先添加企业微信并完成领取身份校验。"
	case errors.Is(err, reportwecom.ErrUnionIDRequired):
		status, msg = 409, "当前微信身份尚未关联，请联系顾问核实领取身份。"
	case errors.Is(err, reportwecom.ErrNotOwner):
		status, msg = 403, "添加企业微信的账号与报告账号不一致，请核对。"
	case errors.Is(err, report.ErrConflict):
		status, msg = 409, "报告正在处理中，或已达到重试上限，请刷新或联系顾问。"
	case errors.Is(err, report.ErrPackUnavailable):
		status, msg = 409, "暂未匹配到经过审核的完整方案，不会收取报告费用。"
	case errors.Is(err, report.ErrNotReady):
		status, msg = 409, "报告尚未准备好，请稍后刷新。"
	}
	c.JSON(status, gin.H{"code": status, "msg": msg})
}
