package route

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/shareweb"
	"service/tracking"
)

type shareURLLinkCache struct {
	mu       sync.Mutex
	capacity int
	values   map[string]tracking.URLLinkResult
	inflight map[string]*shareURLLinkCall
}

type shareURLLinkCall struct {
	done  chan struct{}
	value tracking.URLLinkResult
	err   error
}

func newShareURLLinkCache(capacity int) *shareURLLinkCache {
	return &shareURLLinkCache{capacity: capacity, values: map[string]tracking.URLLinkResult{}, inflight: map[string]*shareURLLinkCall{}}
}

func (c *shareURLLinkCache) load(ctx context.Context, key string, now time.Time, generate func() (tracking.URLLinkResult, error)) (tracking.URLLinkResult, error) {
	c.mu.Lock()
	value, ok := c.values[key]
	if ok && now.Before(value.Expires) {
		c.mu.Unlock()
		return value, nil
	}
	delete(c.values, key)
	if call := c.inflight[key]; call != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return tracking.URLLinkResult{}, ctx.Err()
		case <-call.done:
			return call.value, call.err
		}
	}
	call := &shareURLLinkCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	call.value, call.err = generate()
	c.mu.Lock()
	if call.err == nil {
		if len(c.values) >= c.capacity {
			for old := range c.values {
				delete(c.values, old)
				break
			}
		}
		c.values[key] = call.value
	}
	delete(c.inflight, key)
	close(call.done)
	c.mu.Unlock()
	return call.value, call.err
}

func shareWebOrigin() (string, bool) {
	if os.Getenv("SHARE_WEB_ENABLED") != "true" {
		return "", false
	}
	raw := strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_SHARE_ORIGIN")), "/")
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return "", false
	}
	return raw, true
}

func shareWebMutationAllowed(c *gin.Context) bool {
	origin, ok := shareWebOrigin()
	return ok && c.GetHeader("Origin") == origin && c.GetHeader("X-Share-Web") == "1"
}

func shareEntryDestination(ctx context.Context, svc *tracking.Service, scope tracking.Scope, token string) (gin.H, error) {
	share, err := svc.OwnShare(ctx, scope, scope.Actor, token)
	if err != nil {
		return nil, err
	}
	path, query, err := tracking.ShareNativeDestination(share, token)
	if err != nil {
		return nil, err
	}
	origin, ok := shareWebOrigin()
	if !ok {
		return nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "分享网页尚未配置"}
	}
	credentials := miniprogramCredentials()
	trackingReady := svc.Config.Enabled && (share.Route != trackingReaderRoute || svc.Config.ContentEnabled)
	return gin.H{
		"share_id": share.ID, "native_path": path, "native_query": query,
		"web_url": origin + "/share/s/" + url.PathEscape(token), "expires_at": share.Expires,
		"tracking_ready": trackingReady, "url_link_ready": trackingURLLinkEnabled() && credentials.AppID != "" && credentials.Secret != "",
		"handoff_ready": false,
	}, nil
}

func registerMiniProgramShareEntryRoutes(v1 *gin.RouterGroup, svc *tracking.Service, users *model.UserModel, codes *model.CodeSessionRedisModel) {
	g := v1.Group("/miniprogram/tracking", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		c.Next()
	}, TokenAuthRequired(codes), MiniprogramBannedBlock(users))
	g.POST("/share-entries", func(c *gin.Context) {
		var in struct {
			Token string `json:"share_token"`
		}
		if !trackingBind(c, &in) {
			return
		}
		actor := GetTokenUserID(c)
		out, err := shareEntryDestination(c.Request.Context(), svc, tracking.Scope{Tenant: svc.Config.Tenant, Actor: actor}, in.Token)
		trackingResponse(c, out, err)
	})
}

func registerShareWebRoutes(root *gin.Engine, v1 *gin.RouterGroup, svc *tracking.Service, api *tracking.WechatAPI, users *model.UserModel, codes *model.CodeSessionRedisModel) {
	registerShareWebHandlers(root, v1, svc, api)
	registerMiniProgramShareEntryRoutes(v1, svc, users, codes)
}

// registerShareWebHandlers is separate from RegisterRoutes so tests can
// exercise the public boundary without production credentials.
func registerShareWebHandlers(root *gin.Engine, v1 *gin.RouterGroup, svc *tracking.Service, api *tracking.WechatAPI) {
	urlLinks := newShareURLLinkCache(10000)
	urlLinkLimiter := newTrackingPeerLimiter(10000, trackingURLLinkPerMinute, time.Now)
	root.GET("/share/assets/:name", func(c *gin.Context) {
		data, err := shareweb.Asset(c.Param("name"))
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		contentType := "application/javascript; charset=utf-8"
		if strings.HasSuffix(c.Param("name"), ".css") {
			contentType = "text/css; charset=utf-8"
		}
		c.Header("Cache-Control", "public, max-age=3600")
		c.Data(http.StatusOK, contentType, data)
	})
	root.GET("/share/s/:code", func(c *gin.Context) {
		if _, ok := shareWebOrigin(); !ok {
			c.Status(http.StatusNotFound)
			return
		}
		code := c.Param("code")
		share, err := svc.ResolveShareRecord(c.Request.Context(), code)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "private, no-store")
		c.Header("Referrer-Policy", "no-referrer")
		// Inline spacing values are server-generated bounded integers. Stored rich
		// text is re-sanitized and cannot carry style attributes.
		c.Header("Content-Security-Policy", "default-src 'self'; img-src 'self'; media-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
		if share.Route != trackingReaderRoute {
			if !tracking.LegacyRoute(share.Route) {
				c.Status(http.StatusNotFound)
				return
			}
			title := svc.Config.Routes[share.Route].Title
			if err = shareweb.RenderLanding(c.Writer, code, title); err != nil {
				c.Status(http.StatusNotFound)
			}
			return
		}
		content, err := svc.ReadContent(c.Request.Context(), share.Content, share.Version)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		if err = shareweb.Render(c.Writer, shareweb.Page{Code: code, Content: content, Trackable: svc.Config.Enabled}); err != nil {
			c.Status(http.StatusNotFound)
		}
	})

	web := v1.Group("/share-web")
	mutationRateLimit := trackingEntryRateLimit()
	mutation := func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		if !shareWebMutationAllowed(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "网页请求来源无效"})
			return
		}
		c.Next()
	}
	web.POST("/entry", mutation, mutationRateLimit, func(c *gin.Context) {
		var input struct {
			Code    string `json:"code"`
			Client  string `json:"client_instance"`
			Consent string `json:"consent_version"`
		}
		if !trackingBind(c, &input) {
			return
		}
		share, err := svc.ResolveShareRecord(c.Request.Context(), input.Code)
		if err != nil || share.Route != trackingReaderRoute {
			trackingResponse(c, nil, &tracking.Error{Status: 404, Message: "分享入口无效"})
			return
		}
		entry, err := svc.Enter(c.Request.Context(), tracking.EntryInput{Route: share.Route, Content: share.Content, Version: share.Version, ShareToken: input.Code, Client: input.Client, Consent: input.Consent}, 0)
		trackingResponse(c, entry, err)
	})
	web.POST("/events", mutation, mutationRateLimit, func(c *gin.Context) {
		var input struct {
			Token  string           `json:"entry_token"`
			Events []tracking.Event `json:"events"`
		}
		if !trackingBind(c, &input) {
			return
		}
		out, err := svc.Accept(c.Request.Context(), input.Token, input.Events)
		trackingResponse(c, out, err)
	})
	web.Match([]string{http.MethodGet, http.MethodHead}, "/media/:code/:id", func(c *gin.Context) {
		if _, ok := shareWebOrigin(); !ok {
			c.Status(http.StatusNotFound)
			return
		}
		share, err := svc.ResolveShareRecord(c.Request.Context(), c.Param("code"))
		if err != nil || share.Route != trackingReaderRoute {
			c.Status(http.StatusNotFound)
			return
		}
		content, err := svc.ReadContent(c.Request.Context(), share.Content, share.Version)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		found := false
		for _, media := range content.Media {
			if media.ID == c.Param("id") {
				found = true
				break
			}
		}
		if !found {
			c.Status(http.StatusNotFound)
			return
		}
		asset, err := svc.Media(c.Request.Context(), nil, c.Param("id"))
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		serveContentMediaBytes(c, svc, asset)
	})
	web.POST("/open-miniprogram", mutation, mutationRateLimit, func(c *gin.Context) {
		var input struct {
			Code string `json:"code"`
		}
		if !trackingBind(c, &input) {
			return
		}
		credentials := miniprogramCredentials()
		if !trackingURLLinkEnabled() || credentials.AppID == "" || credentials.Secret == "" || api == nil {
			trackingResponse(c, nil, &tracking.Error{Status: 503, Message: "URL Link尚未配置"})
			return
		}
		share, err := svc.ResolveShareRecord(c.Request.Context(), input.Code)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		path, query, err := tracking.ShareNativeDestination(share, input.Code)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		link, err := urlLinks.load(ctx, share.ID, svc.Now(), func() (tracking.URLLinkResult, error) {
			if !urlLinkLimiter.allow(share.ID) {
				return tracking.URLLinkResult{}, &tracking.Error{Status: 429, Message: "生成URL Link过于频繁，请稍后再试"}
			}
			return tracking.WechatURLLinkUntil(ctx, api, credentials.AppID, credentials.Secret, path, query, trackingWechatEnv(), svc.Now(), share.Expires)
		})
		if err != nil {
			log.Printf("[ShareWeb] URL Link generation failed for share_id=%s: %v", share.ID, err)
		}
		trackingResponse(c, link, err)
	})
}
