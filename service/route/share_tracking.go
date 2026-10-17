package route

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"crypto/sha256"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"path/filepath"
	"service/config"
	"service/model"
	"service/tracking"
)

func trackingResponse(c *gin.Context, v any, e error) {
	if e != nil {
		var te *tracking.Error
		if errors.As(e, &te) {
			c.AbortWithStatusJSON(te.Status, gin.H{"code": te.Status, "msg": te.Message})
		} else {
			c.AbortWithStatusJSON(503, gin.H{"code": 503, "msg": "访问记录服务暂不可用"})
		}
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": v})
}
func trackingBind(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		var max *http.MaxBytesError
		status := 400
		if errors.As(e, &max) {
			status = 413
		}
		trackingResponse(c, nil, &tracking.Error{Status: status, Message: "请求字段或体积无效"})
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "请求内容无效"})
		return false
	}
	return true
}
func trackingMutationAllowed(r *http.Request) bool {
	if r.Header.Get("X-Tracking-Request") != "1" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	for _, s := range corsAllowedOrigins() {
		if origin == s {
			return true
		}
	}
	u, e := url.Parse(origin)
	return e == nil && u.Host == r.Host && (u.Scheme == "https" || u.Scheme == "http" && r.TLS == nil)
}
func loadTrackingScope(c *gin.Context, db *sql.DB, tenant string) (tracking.Scope, error) {
	return loadTrackingScopeFor(c.Request.Context(), db, tenant, GetUserID(c))
}

// loadTrackingScopeFor reads one management account's current tracking grants.
// A background job acts through the same lookup a login session does, so it can
// never hold more than an explicit, enabled grant.
func loadTrackingScopeFor(ctx context.Context, db *sql.DB, tenant string, actor int64) (tracking.Scope, error) {
	scope := tracking.Scope{Tenant: tenant, Actor: actor, Owners: []string{}}
	if scope.Actor <= 0 {
		return scope, &tracking.Error{Status: 401, Message: "未登录"}
	}
	if db == nil || tenant == "" {
		return scope, tracking.ErrUnavailable
	}
	access, e := readManagementAccess(ctx, db, scope.Actor)
	if e != nil || !access.Enabled {
		return scope, &tracking.Error{Status: 403, Message: "管理账号未授权"}
	}
	rows, e := db.QueryContext(ctx, `SELECT t.owner_id,t.can_view,t.can_manage,t.can_crm,t.can_delete,t.can_rules FROM tracking_access t JOIN users u ON u.id=t.user_id WHERE t.user_id=? AND t.tenant_id=? AND t.enabled=TRUE AND u.user_type='management'`, scope.Actor, tenant)
	if e != nil {
		return scope, tracking.ErrUnavailable
	}
	defer rows.Close()
	first := true
	for rows.Next() {
		var owner string
		var view, manage, crm, del, rules bool
		if rows.Scan(&owner, &view, &manage, &crm, &del, &rules) != nil {
			return scope, tracking.ErrUnavailable
		}
		if !view && !manage {
			continue
		}
		scope.Owners = append(scope.Owners, owner)
		if first {
			scope.View = view
			scope.Manage = manage
			scope.CRM = crm
			scope.Delete = del
			scope.Rules = rules
			first = false
		} else {
			scope.View = scope.View && view
			scope.Manage = scope.Manage && manage
			scope.CRM = scope.CRM && crm
			scope.Delete = scope.Delete && del
			scope.Rules = scope.Rules && rules
		}
	}
	if rows.Err() != nil {
		return scope, tracking.ErrUnavailable
	}
	return scope, nil
}
func trackingFilter(c *gin.Context) (tracking.Filter, error) {
	f := tracking.Filter{Content: c.Query("content_id"), Identity: c.Query("identity"), Intent: c.Query("intent"), Search: c.Query("q"), ContentSearch: c.Query("content_q"), VisitorSearch: c.Query("visitor_q"), Owner: c.Query("owner"), Sort: c.Query("sort"), Direction: c.Query("direction"), Cursor: c.Query("cursor"), Related: c.Query("include_related") == "true", Visitor: c.Query("visitor")}
	for name, target := range map[string]*time.Time{"start": &f.Start, "end": &f.End, "as_of": &f.AsOf} {
		if raw := c.Query(name); raw != "" {
			t, e := time.Parse(time.RFC3339Nano, raw)
			if e != nil {
				return f, &tracking.Error{Status: 400, Message: "时间格式无效"}
			}
			*target = t
		}
	}
	if raw := c.Query("content_version"); raw != "" {
		value, e := strconv.Atoi(raw)
		if e != nil || value <= 0 {
			return f, &tracking.Error{Status: 400, Message: "访问版本无效"}
		}
		f.ContentVersion = value
	}
	f.Limit, _ = strconv.Atoi(c.Query("limit"))
	if mv := c.Query("metric_version"); mv != "" && mv != tracking.MetricVersion {
		return f, &tracking.Error{Status: 409, Message: "指标版本已变化，请刷新"}
	}
	return f, nil
}

// annotateVisitorRiskTags 给已识别访客（visitor_key = "u:<id>"）注入风控风险标签，
// 让「分享访问记录」与「风控台」在同一个列表里互相印证。匿名访客不查：
// 没有 user_id，且按隐私边界不做跨身份关联。标签查询失败只降级，不打断列表。
func annotateVisitorRiskTags(users *model.UserModel, list tracking.List) (tracking.List, error) {
	if users == nil || users.DB == nil {
		return list, nil
	}
	raw, e := json.Marshal(list.Items)
	if e != nil {
		return list, nil
	}
	var visitors []map[string]any
	if json.Unmarshal(raw, &visitors) != nil {
		return list, nil
	}
	ids := make([]int64, 0, len(visitors))
	for _, item := range visitors {
		if state, _ := item["identity_state"].(string); state != "identified" {
			continue
		}
		key, _ := item["visitor_key"].(string)
		if !strings.HasPrefix(key, "u:") {
			continue
		}
		if id, err := strconv.ParseInt(strings.TrimPrefix(key, "u:"), 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return list, nil
	}
	tags, err := riskTagsByUserIDs(users.DB, ids)
	if err != nil {
		log.Printf("[Tracking] 访客风险标签查询失败: %v", err)
		return list, nil
	}
	for _, item := range visitors {
		key, _ := item["visitor_key"].(string)
		if !strings.HasPrefix(key, "u:") {
			continue
		}
		if id, err := strconv.ParseInt(strings.TrimPrefix(key, "u:"), 10, 64); err == nil {
			if t, ok := tags[id]; ok {
				item["risk_tags"] = t
			}
		}
	}
	list.Items = visitors
	return list, nil
}

// Private annotations can target one authorized owner without changing the
// authenticated scope. Identity deletion must continue to use the complete scope.
func trackingPrivateMutationScope(c *gin.Context, scope tracking.Scope) (tracking.Scope, error) {
	owners, supplied := c.Request.URL.Query()["owner"]
	if !supplied {
		return scope, nil
	}
	if len(owners) != 1 || owners[0] == "" {
		return scope, &tracking.Error{Status: 400, Message: "请选择单一归属范围"}
	}
	if !scope.Allows(owners[0]) {
		return scope, &tracking.Error{Status: 403, Message: "归属范围未授权"}
	}
	scope.Owners = []string{owners[0]}
	return scope, nil
}
func RegisterTrackingRoutes(v1 *gin.RouterGroup, users *model.UserModel, codes *model.CodeSessionRedisModel, customerServiceModel *model.CustomerServiceModel) *tracking.Service {
	cfg := tracking.LoadConfig()
	if warning := trackingProxyPolicyWarning(cfg.Enabled, os.Getenv("TRACKING_TRUSTED_PROXY_CIDRS")); warning != "" {
		log.Printf("[Tracking] %s", warning)
	}
	// Tracking shares the application pool (20 connections). Cap what it may hold
	// so queued tracking work waits in Go instead of starving core business.
	wechatAPI := newTrackingWechatAPI(miniprogramCredentials())
	svc := tracking.New(tracking.NewSQLStore(users.DB, trackingSlotLimit("TRACKING_MAX_WRITE_TX", 4), trackingSlotLimit("TRACKING_MAX_READ_TX", 6)), cfg)
	svc.VerifyCRM = makeTrackingCRMVerifier(users.DB)
	svc.VerifyBusiness = makeTrackingBusinessVerifier(users.DB)
	svc.ResolveLegacyContext = makeTrackingLegacyResolver(users.DB)
	svc.ResolveVisitorProfiles = makeTrackingProfileResolver(users.DB)
	// This is a separate capability group, not an implicit platform-admin data grant.
	mpSettings := loadMPSyncSettings(os.Getenv)
	for _, problem := range mpSettings.Problems {
		log.Printf("[Tracking] 公众号同步未启用：%s", problem)
	}
	var mpRunner *mpSyncRunner
	if mpSettings.Enabled {
		mpRunner = &mpSyncRunner{
			svc: svc, settings: mpSettings,
			source: &tracking.OfficialAccountSource{API: wechatAPI, AppID: mpSettings.Config.AppID, Secret: mpSettings.Secret},
			scopeFor: func(ctx context.Context) (tracking.Scope, error) {
				return loadTrackingScopeFor(ctx, users.DB, cfg.Tenant, mpSettings.Actor)
			},
			mediaStore: func(scope tracking.Scope) tracking.MediaStore { return trackingMPMediaStore(svc, scope) },
		}
		if mpSettings.Config.ImportImages {
			mpRunner.fetcher = tracking.NewSafeMediaFetcher()
		}
		go mpRunner.Loop(context.Background())
	}
	mg := v1.Group("/management/tracking")
	mg.Use(func(c *gin.Context) {
		// Capability and private visit responses must never survive a permission
		// recheck through HTTP caches, including authentication failures.
		c.Header("Cache-Control", "private, no-store")
		c.Next()
	}, AuthRequired, func(c *gin.Context) {
		scope, e := loadTrackingScope(c, users.DB, cfg.Tenant)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		if !checkWecomSession(c, users.DB) {
			return
		}
		if c.Request.Method != "GET" && !trackingMutationAllowed(c.Request) {
			trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "请求来源校验失败"})
			return
		}
		c.Set("tracking.scope", scope)
		c.Next()
	})
	registerTrackingRuleRoutes(mg, svc)
	registerTrackingCRMTargetRoutes(mg, svc)
	registerTrackingLeadRoutes(mg, svc, customerServiceModel, users)
	sc := func(c *gin.Context) tracking.Scope { return c.MustGet("tracking.scope").(tracking.Scope) }
	view := func(c *gin.Context) bool {
		if !sc(c).View {
			trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "无客户轨迹查看权限"})
			return false
		}
		return true
	}
	mg.GET("/contents/:id/code", func(c *gin.Context) {
		if os.Getenv("TRACKING_WECHAT_CODE_ENABLED") != "true" || !sc(c).Manage {
			trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "微信码权限未接通"})
			return
		}
		version, _ := strconv.Atoi(c.Query("version"))
		contents, e := svc.Contents(c.Request.Context(), sc(c), tracking.Filter{}, c.Param("id"))
		if e != nil || len(contents.Items.([]tracking.Content)) == 0 {
			trackingResponse(c, nil, &tracking.Error{Status: 404, Message: "素材不可访问"})
			return
		}
		if _, e = svc.ReadContent(c.Request.Context(), c.Param("id"), version); e != nil {
			trackingResponse(c, nil, e)
			return
		}
		root, e := trackingMediaRoot()
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		key := trackingCodeCacheKey(cfg.Tenant, sc(c).Actor, c.Param("id"), version, os.Getenv("TRACKING_WECHAT_ENV"), svc.Now())
		name := filepath.Join(root, "code-"+key)
		share, e := svc.CreateShare(c.Request.Context(), tracking.ShareInput{Route: "pages/contentreader/contentreader", Content: c.Param("id"), Version: version, Key: "qr-" + key}, "", sc(c).Actor)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		if bytes, e := os.ReadFile(name); e == nil {
			c.Header("Cache-Control", "private, no-store")
			c.Data(200, http.DetectContentType(bytes), bytes)
			return
		}
		wx := config.Get().Wechat
		environment := os.Getenv("TRACKING_WECHAT_ENV")
		if environment == "" {
			environment = "release"
		}
		bytes, e := tracking.WechatCode(c.Request.Context(), wx.AppID, wx.AppSecret, share["share_token"].(string), "pages/contentreader/contentreader", environment)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		if e = os.WriteFile(name, bytes, 0600); e != nil {
			trackingResponse(c, nil, e)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.Data(200, http.DetectContentType(bytes), bytes)
	})
	// Generating a link creates a share and can spend WeChat quota, so it is a
	// mutation: POST puts it behind the management group's origin check, and each
	// account is held to a modest rate so a loop over contents cannot drain quota.
	urlLinkLimiter := newTrackingPeerLimiter(1000, trackingURLLinkPerMinute, time.Now)
	mg.POST("/contents/:id/url-link", func(c *gin.Context) {
		if !urlLinkLimiter.allow(strconv.FormatInt(sc(c).Actor, 10)) {
			trackingResponse(c, nil, &tracking.Error{Status: 429, Message: "生成URL Link过于频繁，请稍后再试"})
			return
		}
		version, _ := strconv.Atoi(c.Query("version"))
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		link, e := trackingURLLink(ctx, svc, wechatAPI, miniprogramCredentials(), sc(c), c.Param("id"), version)
		if e != nil {
			var wechat *tracking.WechatError
			if errors.As(e, &wechat) {
				log.Printf("[Tracking] url link: wechat errcode %d", wechat.Code)
				e = &tracking.Error{Status: 502, Message: trackingURLLinkProblem(e)}
			}
			trackingResponse(c, nil, e)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		trackingResponse(c, link, nil)
	})
	registerMPSyncRoutes(mg, svc, mpRunner, mpSettings, sc)
	mg.GET("/context", func(c *gin.Context) {
		scope := sc(c)
		owners := []gin.H{}
		for _, o := range scope.Owners {
			owners = append(owners, gin.H{"id": o, "name": o})
		}
		trackingResponse(c, gin.H{"can_view": scope.View, "can_manage": scope.Manage, "can_crm": scope.CRM, "can_delete": scope.Delete, "can_rules": scope.Rules, "owners": owners, "share_methods": []string{"native_miniprogram"}, "mp_sync": mpSyncVisible(mpSettings, scope)}, nil)
	})
	mg.GET("/diagnostics/rejections", func(c *gin.Context) {
		days := 7
		if raw := c.Query("days"); raw != "" {
			value, e := strconv.Atoi(raw)
			if e != nil {
				trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "统计天数无效"})
				return
			}
			days = value
		}
		v, e := svc.RejectionStats(c.Request.Context(), sc(c), days)
		trackingResponse(c, gin.H{"items": v}, e)
	})
	mg.GET("/visitors", func(c *gin.Context) {
		if !view(c) {
			return
		}
		f, e := trackingFilter(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		v, e := svc.Visitors(c.Request.Context(), sc(c), f)
		if e == nil {
			// 风控联动：已识别访客注入 risk_tags（查询失败只降级不打断列表）
			v, e = annotateVisitorRiskTags(users, v)
		}
		trackingResponse(c, v, e)
	})
	mg.GET("/visitors/:id/contents", func(c *gin.Context) {
		if !view(c) {
			return
		}
		f, e := trackingFilter(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		v, e := svc.VisitorContents(c.Request.Context(), sc(c), c.Param("id"), f)
		trackingResponse(c, v, e)
	})
	mg.GET("/visitors/:id/sessions", func(c *gin.Context) {
		if !view(c) {
			return
		}
		f, e := trackingFilter(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		v, e := svc.Sessions(c.Request.Context(), sc(c), c.Param("id"), f)
		trackingResponse(c, v, e)
	})
	mg.GET("/page-views/:id/events", func(c *gin.Context) {
		if !view(c) {
			return
		}
		f, e := trackingFilter(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		v, e := svc.Events(c.Request.Context(), sc(c), c.Param("id"), f)
		trackingResponse(c, v, e)
	})
	mg.GET("/contents", func(c *gin.Context) {
		f, e := trackingFilter(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		v, e := svc.Contents(c.Request.Context(), sc(c), f, "")
		trackingResponse(c, v, e)
	})
	mg.GET("/contents/:id", func(c *gin.Context) {
		f, e := trackingFilter(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		v, e := svc.Contents(c.Request.Context(), sc(c), f, c.Param("id"))
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		items := v.Items.([]tracking.Content)
		if len(items) == 0 {
			trackingResponse(c, nil, &tracking.Error{Status: 404, Message: "素材不可访问"})
			return
		}
		trackingResponse(c, struct {
			tracking.Content
			tracking.Snapshot
		}{items[0], v.Snapshot}, nil)
	})
	mg.GET("/contents/:id/visitors", func(c *gin.Context) {
		if !view(c) {
			return
		}
		f, e := trackingFilter(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		f.Content = c.Param("id")
		v, e := svc.Visitors(c.Request.Context(), sc(c), f)
		trackingResponse(c, v, e)
	})
	save := func(c *gin.Context) {
		var in tracking.Draft
		if !trackingBind(c, &in) {
			return
		}
		v, e := svc.SaveContent(c.Request.Context(), sc(c), c.Param("id"), in)
		trackingResponse(c, v, e)
	}
	mg.POST("/contents", save)
	mg.PATCH("/contents/:id", save)
	mg.POST("/contents/:id/preview", func(c *gin.Context) {
		var in struct {
			Revision int `json:"expected_revision"`
		}
		if !trackingBind(c, &in) {
			return
		}
		v, e := svc.Preview(c.Request.Context(), sc(c), c.Param("id"), in.Revision)
		trackingResponse(c, v, e)
	})
	mg.POST("/contents/:id/publish", func(c *gin.Context) {
		var in struct {
			Revision int    `json:"expected_revision"`
			Preview  string `json:"preview_token"`
			Key      string `json:"idempotency_key"`
		}
		if !trackingBind(c, &in) {
			return
		}
		v, e := svc.Publish(c.Request.Context(), sc(c), c.Param("id"), in.Revision, in.Preview, in.Key)
		if e == nil {
			addPublishShareMethods(c.Request.Context(), svc, wechatAPI, sc(c), v)
		}
		trackingResponse(c, v, e)
	})
	mg.POST("/contents/:id/revoke", func(c *gin.Context) {
		var in struct {
			Revision int    `json:"expected_revision"`
			Reason   string `json:"reason"`
			Key      string `json:"idempotency_key"`
		}
		if !trackingBind(c, &in) {
			return
		}
		v, e := svc.Revoke(c.Request.Context(), sc(c), c.Param("id"), in.Revision, in.Reason, in.Key)
		trackingResponse(c, v, e)
	})
	mg.GET("/media", func(c *gin.Context) { v, e := svc.ListMedia(c.Request.Context(), sc(c)); trackingResponse(c, v, e) })
	mg.POST("/media", func(c *gin.Context) { uploadTrackingMedia(c, svc, sc(c)) })
	mg.POST("/identity-links", func(c *gin.Context) {
		var in struct {
			Visitor  string `json:"visitor_key"`
			Customer string `json:"customer_id"`
			Number   string `json:"business_number"`
			Basis    string `json:"basis"`
		}
		if !trackingBind(c, &in) {
			return
		}
		scope, e := trackingPrivateMutationScope(c, sc(c))
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		e = svc.LinkCRM(c.Request.Context(), scope, in.Visitor, in.Customer, in.Number, in.Basis)
		trackingResponse(c, gin.H{"linked": e == nil}, e)
	})
	mg.DELETE("/identity-links/:id", func(c *gin.Context) {
		scope, e := trackingPrivateMutationScope(c, sc(c))
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		e = svc.UnlinkCRM(c.Request.Context(), scope, c.Param("id"))
		trackingResponse(c, gin.H{"unlinked": e == nil}, e)
	})
	mg.POST("/intent-overrides", func(c *gin.Context) {
		var in struct {
			Visitor string    `json:"visitor_key"`
			Level   string    `json:"level"`
			Reason  string    `json:"reason"`
			Expires time.Time `json:"expires_at"`
		}
		if !trackingBind(c, &in) {
			return
		}
		scope, e := trackingPrivateMutationScope(c, sc(c))
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		e = svc.OverrideIntent(c.Request.Context(), scope, in.Visitor, in.Level, in.Reason, in.Expires)
		trackingResponse(c, gin.H{"updated": e == nil}, e)
	})
	mg.POST("/deletions", func(c *gin.Context) {
		var in struct {
			Visitor string `json:"visitor_key"`
			Reason  string `json:"reason"`
		}
		if !trackingBind(c, &in) {
			return
		}
		// A deletion too large for one transaction is staged. The data is already
		// unreadable once it starts; this request does a bounded share of the
		// removal and a repeated identical request continues it.
		done, e := svc.DeleteVisitorProgress(c.Request.Context(), sc(c), in.Visitor, in.Reason, trackingDeletionPagesPerRequest)
		status := "in_progress"
		if done {
			status = "completed"
		}
		trackingResponse(c, gin.H{"status": status}, e)
	})
	pub := v1.Group("/miniprogram/tracking")
	registerTrackingMaterialRoutes(pub, svc, users, codes)
	pub.POST("/share-entry", func(c *gin.Context) {
		var in struct {
			Scene string `json:"scene"`
		}
		if !trackingBind(c, &in) {
			return
		}
		v, e := svc.ResolveShare(c.Request.Context(), in.Scene)
		trackingResponse(c, v, e)
	})
	pub.GET("/config", func(c *gin.Context) {
		trackingResponse(c, gin.H{"enabled": cfg.Enabled, "content_enabled": cfg.ContentEnabled, "video_enabled": cfg.VideoEnabled, "intent_enabled": cfg.IntentEnabled, "consent_version": cfg.ConsentVersion, "privacy_notice": cfg.PrivacyNotice, "sample_ms": 10000, "max_queue": 500, "ttl_hours": 72}, nil)
	})
	pub.GET("/contents/:id/versions/:version", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		version, _ := strconv.Atoi(c.Param("version"))
		v, e := svc.ReadContent(c.Request.Context(), c.Param("id"), version)
		trackingResponse(c, v, e)
	})
	pub.GET("/media/:id", func(c *gin.Context) {
		scope, e := loadTrackingScope(c, users.DB, cfg.Tenant)
		var selected *tracking.Scope
		if e == nil {
			if !checkWecomSession(c, users.DB) {
				return
			}
			selected = &scope
		}
		serveTrackingMedia(c, svc, selected)
	})
	pub.POST("/events/batch", func(c *gin.Context) {
		var in struct {
			Batch  string           `json:"batch_id"`
			Events []tracking.Event `json:"events"`
		}
		if !trackingBind(c, &in) {
			return
		}
		results, e := svc.Accept(c.Request.Context(), c.GetHeader("X-Tracking-Token"), in.Events)
		trackingResponse(c, gin.H{"results": results, "metric_version": tracking.MetricVersion}, e)
	})
	optionalAuth := func(c *gin.Context) {
		if c.GetHeader("token") == "" {
			c.Next()
			return
		}
		TokenAuthRequired(codes)(c)
	}
	pub.POST("/entry", trackingEntryRateLimit(), optionalAuth, MiniprogramBannedBlock(users), func(c *gin.Context) {
		var in tracking.EntryInput
		if !trackingBind(c, &in) {
			return
		}
		v, e := svc.Enter(c.Request.Context(), in, GetTokenUserID(c))
		trackingResponse(c, v, e)
	})
	pub.POST("/shares", trackingEntryRateLimit(), optionalAuth, func(c *gin.Context) {
		var in tracking.ShareInput
		if !trackingBind(c, &in) {
			return
		}
		v, e := svc.CreateShare(c.Request.Context(), in, c.GetHeader("X-Tracking-Token"), GetTokenUserID(c))
		trackingResponse(c, v, e)
	})
	return svc
}

// Each staged page is one short transaction of up to 500 rows per query, so this
// bounds one HTTP request to a few tens of thousands of rows.
const trackingDeletionPagesPerRequest = 60

// publishedContent reads the content out of a publish result. A first publish
// carries the typed value, but an idempotent replay returns the stored
// publication, whose content is decoded JSON, so both shapes are accepted.
func publishedContent(result map[string]any) (tracking.Content, bool) {
	var content tracking.Content
	switch value := result["content"].(type) {
	case tracking.Content:
		content = value
	case map[string]any:
		raw, err := json.Marshal(value)
		if err != nil || json.Unmarshal(raw, &content) != nil {
			return tracking.Content{}, false
		}
	default:
		return tracking.Content{}, false
	}
	return content, content.ID != "" && content.Version > 0
}

// addPublishShareMethods adds the operator's share methods to a publish result.
// It runs for a replay too: when the first response was lost or timed out after
// the publication committed, the retry must still be able to return the link.
func addPublishShareMethods(ctx context.Context, svc *tracking.Service, api *tracking.WechatAPI, scope tracking.Scope, result map[string]any) {
	content, ok := publishedContent(result)
	if !ok {
		return
	}
	existing, _ := result["share_methods"].(map[string]any)
	result["share_methods"] = trackingShareMethods(ctx, svc, api, scope, content, existing)
}

// trackingPublishLinkTimeout bounds the WeChat call made after a publish has
// already committed. The operator is waiting on this response, and a retry of the
// publish returns the same link, so a slow WeChat is cut off early rather than
// left to hold the request for the full interactive limit.
var trackingPublishLinkTimeout = 10 * time.Second // a variable only so a test can shorten it

// trackingShareMethods lists what the operator can use right after a publish.
// The publish already succeeded, so a failure here is reported as a message and
// never fails the request.
func trackingShareMethods(ctx context.Context, svc *tracking.Service, api *tracking.WechatAPI, scope tracking.Scope, content tracking.Content, existing map[string]any) gin.H {
	methods := gin.H{"message": "请在小程序自有素材页打开后使用原生分享。微信码与URL Link须完成服务端权限核验后开放。"}
	for key, value := range existing {
		methods[key] = value
	}
	if os.Getenv("TRACKING_WECHAT_CODE_ENABLED") == "true" {
		methods["miniprogram_code_url"] = "/api/v1/management/tracking/contents/" + content.ID + "/code?version=" + strconv.Itoa(content.Version)
		methods["message"] = "真实微信码由服务端按需请求，加载失败表示权限或配置尚未通过"
	}
	if trackingURLLinkEnabled() {
		linkCtx, cancel := context.WithTimeout(ctx, trackingPublishLinkTimeout)
		defer cancel()
		link, err := trackingURLLink(linkCtx, svc, api, miniprogramCredentials(), scope, content.ID, content.Version)
		if err != nil {
			var wechat *tracking.WechatError
			if errors.As(err, &wechat) {
				log.Printf("[Tracking] url link: wechat errcode %d", wechat.Code)
			}
			methods["message"] = trackingURLLinkProblem(err)
		} else {
			methods["url_link"] = link.URL
			methods["expires_at"] = link.Expires
		}
	}
	return methods
}

// trackingSlotLimit reads a positive connection cap, falling back to the default
// for empty or invalid values. It never disables the cap.
func trackingSlotLimit(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 64 {
		log.Printf("[Tracking] %s=%q 无效，使用默认值 %d", name, raw, fallback)
		return fallback
	}
	return value
}

// Daily generations expire well before the associated 30-day share token.
// Actor/tenant/environment isolation prevents cached codes borrowing attribution.
func trackingCodeCacheKey(tenant string, actor int64, content string, version int, environment string, now time.Time) string {
	sum := sha256.Sum256([]byte(tenant + ":" + strconv.FormatInt(actor, 10) + ":" + content + ":" + strconv.Itoa(version) + ":" + environment + ":" + now.UTC().Format("2006-01-02")))
	return hex.EncodeToString(sum[:])
}
