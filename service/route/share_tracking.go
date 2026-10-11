package route

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
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
	scope := tracking.Scope{Tenant: tenant, Actor: GetUserID(c), Owners: []string{}}
	if scope.Actor <= 0 {
		return scope, &tracking.Error{Status: 401, Message: "未登录"}
	}
	if db == nil || tenant == "" {
		return scope, tracking.ErrUnavailable
	}
	access, e := readManagementAccess(c.Request.Context(), db, scope.Actor)
	if e != nil || !access.Enabled {
		return scope, &tracking.Error{Status: 403, Message: "管理账号未授权"}
	}
	rows, e := db.QueryContext(c.Request.Context(), `SELECT t.owner_id,t.can_view,t.can_manage,t.can_crm,t.can_delete,t.can_rules FROM tracking_access t JOIN users u ON u.id=t.user_id WHERE t.user_id=? AND t.tenant_id=? AND t.enabled=TRUE AND u.user_type='management'`, scope.Actor, tenant)
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
	f := tracking.Filter{Content: c.Query("content_id"), Identity: c.Query("identity"), Intent: c.Query("intent"), Search: c.Query("q"), Owner: c.Query("owner"), Sort: c.Query("sort"), Direction: c.Query("direction"), Cursor: c.Query("cursor"), Related: c.Query("include_related") == "true"}
	for name, target := range map[string]*time.Time{"start": &f.Start, "end": &f.End, "as_of": &f.AsOf} {
		if raw := c.Query(name); raw != "" {
			t, e := time.Parse(time.RFC3339Nano, raw)
			if e != nil {
				return f, &tracking.Error{Status: 400, Message: "时间格式无效"}
			}
			*target = t
		}
	}
	f.Limit, _ = strconv.Atoi(c.Query("limit"))
	if mv := c.Query("metric_version"); mv != "" && mv != tracking.MetricVersion {
		return f, &tracking.Error{Status: 409, Message: "指标版本已变化，请刷新"}
	}
	return f, nil
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
func RegisterTrackingRoutes(v1 *gin.RouterGroup, users *model.UserModel, codes *model.CodeSessionRedisModel) *tracking.Service {
	cfg := tracking.LoadConfig()
	svc := tracking.New(&tracking.SQLStore{DB: users.DB}, cfg)
	svc.VerifyCRM = makeTrackingCRMVerifier(users.DB)
	svc.ResolveLegacy = makeTrackingLegacyResolver(users.DB)
	// This is a separate capability group, not an implicit platform-admin data grant.
	mg := v1.Group("/management/tracking")
	mg.Use(AuthRequired, func(c *gin.Context) {
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
	mg.GET("/context", func(c *gin.Context) {
		scope := sc(c)
		owners := []gin.H{}
		for _, o := range scope.Owners {
			owners = append(owners, gin.H{"id": o, "name": o})
		}
		trackingResponse(c, gin.H{"can_view": scope.View, "can_manage": scope.Manage, "can_crm": scope.CRM, "can_delete": scope.Delete, "can_rules": scope.Rules, "owners": owners, "share_methods": []string{"native_miniprogram"}}, nil)
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
		if e == nil && os.Getenv("TRACKING_WECHAT_CODE_ENABLED") == "true" {
			if content, ok := v["content"].(tracking.Content); ok {
				v["share_methods"] = gin.H{"miniprogram_code_url": "/api/v1/management/tracking/contents/" + content.ID + "/code?version=" + strconv.Itoa(content.Version), "message": "真实微信码由服务端按需请求，加载失败表示权限或配置尚未通过；URL Link未开通"}
			}
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
		e := svc.DeleteVisitor(c.Request.Context(), sc(c), in.Visitor, in.Reason)
		trackingResponse(c, gin.H{"status": "completed"}, e)
	})
	pub := v1.Group("/miniprogram/tracking")
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
func trackingEntryRateLimit() gin.HandlerFunc {
	var mu sync.Mutex
	type slot struct {
		at time.Time
		n  int
	}
	buckets := map[string]slot{}
	return func(c *gin.Context) {
		mu.Lock()
		now := time.Now()
		key := c.ClientIP()
		x := buckets[key]
		if now.Sub(x.at) > time.Minute {
			x = slot{at: now}
		}
		x.n++
		buckets[key] = x
		if len(buckets) > 10000 {
			for k, v := range buckets {
				if now.Sub(v.at) > time.Minute {
					delete(buckets, k)
				}
			}
		}
		allowed := x.n <= 60 && len(buckets) <= 10000
		mu.Unlock()
		if !allowed {
			trackingResponse(c, nil, &tracking.Error{Status: 429, Message: "请求过于频繁"})
			return
		}
		c.Next()
	}
}

// Daily generations expire well before the associated 30-day share token.
// Actor/tenant/environment isolation prevents cached codes borrowing attribution.
func trackingCodeCacheKey(tenant string, actor int64, content string, version int, environment string, now time.Time) string {
	sum := sha256.Sum256([]byte(tenant + ":" + strconv.FormatInt(actor, 10) + ":" + content + ":" + strconv.Itoa(version) + ":" + environment + ":" + now.UTC().Format("2006-01-02")))
	return hex.EncodeToString(sum[:])
}
