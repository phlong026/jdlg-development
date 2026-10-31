package route

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/shareweb"
	"service/tracking"
)

const shareWebCollectionRoute = "pages/collectionreader/collectionreader"

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

func shareWebHandoffTarget(path, rawQuery string) (tracking.EntryInput, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil || values.Get("st") == "" {
		return tracking.EntryInput{}, tracking.ErrUnavailable
	}
	target := tracking.EntryInput{Route: path, ShareToken: values.Get("st")}
	positive := func(key string) (int, error) {
		value, parseErr := strconv.Atoi(values.Get(key))
		if parseErr != nil || value < 1 {
			return 0, tracking.ErrUnavailable
		}
		return value, nil
	}
	switch path {
	case trackingReaderRoute:
		target.Content = values.Get("content_id")
		target.Version, err = positive("content_version")
		if err != nil || target.Content == "" {
			return tracking.EntryInput{}, tracking.ErrUnavailable
		}
		if values.Get("collection_id") != "" || values.Get("collection_version") != "" {
			target.CollectionID = values.Get("collection_id")
			target.CollectionVersion, err = positive("collection_version")
			if err != nil || target.CollectionID == "" {
				return tracking.EntryInput{}, tracking.ErrUnavailable
			}
		}
	case shareWebCollectionRoute:
		target.Content = values.Get("collection_id")
		target.Version, err = positive("collection_version")
		if err != nil || target.Content == "" {
			return tracking.EntryInput{}, tracking.ErrUnavailable
		}
	}
	return target, nil
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
	trackingReady := svc.Config.Enabled && ((share.Route != trackingReaderRoute && share.Route != shareWebCollectionRoute) || svc.Config.ContentEnabled)
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
		// 引导页沿用既有样式，不把分享正文或媒体写入 HTML。
		c.Header("Content-Security-Policy", "default-src 'self'; img-src 'self'; media-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
		switch share.Route {
		case shareWebCollectionRoute:
			if _, err = svc.ReadPublishedProjectCollection(c.Request.Context(), share.Content, share.Version); err != nil {
				c.Status(http.StatusNotFound)
				return
			}
		case trackingReaderRoute:
			if _, err = svc.ReadContent(c.Request.Context(), share.Content, share.Version); err != nil {
				c.Status(http.StatusNotFound)
				return
			}
		default:
			if !tracking.LegacyRoute(share.Route) {
				c.Status(http.StatusNotFound)
				return
			}
		}
		if err = shareweb.RenderLoginLanding(c.Writer, code, nil); err != nil {
			c.Status(http.StatusNotFound)
		}
	})
	root.GET("/share/s/:code/items/:position", func(c *gin.Context) {
		if _, ok := shareWebOrigin(); !ok {
			c.Status(http.StatusNotFound)
			return
		}
		position, err := strconv.Atoi(c.Param("position"))
		if err != nil || position < 0 {
			c.Status(http.StatusNotFound)
			return
		}
		code := c.Param("code")
		share, err := svc.ResolveShareRecord(c.Request.Context(), code)
		if err != nil || share.Route != shareWebCollectionRoute {
			c.Status(http.StatusNotFound)
			return
		}
		_, err = svc.ReadPublishedProjectCollectionItemContent(c.Request.Context(), share.Content, share.Version, position)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "private, no-store")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'self'; img-src 'self'; media-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
		if err = shareweb.RenderLoginLanding(c.Writer, code, &position); err != nil {
			c.Status(http.StatusNotFound)
		}
	})

	web := v1.Group("/share-web")
	mutationRateLimit := trackingEntryRateLimit()
	mutation := func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		c.Header("Cache-Control", "private, no-store")
		if !shareWebMutationAllowed(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "网页请求来源无效"})
			return
		}
		c.Next()
	}
	// 网页没有经过小程序登录核验，只提供原分享目标的跳转，不再暴露正文、媒体或记录匿名浏览。
	requireNativeLogin := func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "请在小程序中登录并补齐头像、昵称后查看"})
	}
	web.POST("/entry", requireNativeLogin)
	web.POST("/events", requireNativeLogin)
	web.Match([]string{http.MethodGet, http.MethodHead}, "/media/:code/:id", requireNativeLogin)
	web.Match([]string{http.MethodGet, http.MethodHead}, "/collection-media/:code/:position/:id", requireNativeLogin)
	web.POST("/open-miniprogram", mutation, mutationRateLimit, func(c *gin.Context) {
		var input struct {
			Code             string `json:"code"`
			ItemPosition     *int   `json:"item_position"`
			SourceEntryToken string `json:"source_entry_token"`
			SourceEventID    string `json:"source_page_view_event_id"`
			HandoffToken     string `json:"handoff_token"`
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
		if input.ItemPosition != nil {
			if share.Route != shareWebCollectionRoute || *input.ItemPosition < 0 {
				trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "分享入口参数无效"})
				return
			}
			collection, readErr := svc.ReadPublishedProjectCollection(c.Request.Context(), share.Content, share.Version)
			if readErr != nil || *input.ItemPosition >= len(collection.Items) || !collection.Items[*input.ItemPosition].Available {
				trackingResponse(c, nil, &tracking.Error{Status: 404, Message: "合集作品不可访问"})
				return
			}
			item := collection.Items[*input.ItemPosition]
			values := url.Values{
				"st": {input.Code}, "content_id": {item.ContentID}, "content_version": {strconv.Itoa(item.ContentVersion)},
				"collection_id": {share.Content}, "collection_version": {strconv.Itoa(share.Version)}, "item_position": {strconv.Itoa(*input.ItemPosition)},
			}
			path, query = item.RouteKey, values.Encode()
		}
		target, err := shareWebHandoffTarget(path, query)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		handoff, err := svc.CreateShareHandoffForTarget(c.Request.Context(), input.Code, input.SourceEntryToken, input.SourceEventID, input.HandoffToken, target)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		cacheKey := share.ID
		if input.ItemPosition != nil {
			cacheKey += ":item:" + strconv.Itoa(*input.ItemPosition)
		}
		if handoff.Ready {
			values, parseErr := url.ParseQuery(query)
			if parseErr != nil {
				trackingResponse(c, nil, tracking.ErrUnavailable)
				return
			}
			values.Set("ht", input.HandoffToken)
			query = values.Encode()
			cacheKey += ":" + input.HandoffToken
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		link, err := urlLinks.load(ctx, cacheKey, svc.Now(), func() (tracking.URLLinkResult, error) {
			if !urlLinkLimiter.allow(share.ID) {
				return tracking.URLLinkResult{}, &tracking.Error{Status: 429, Message: "生成URL Link过于频繁，请稍后再试"}
			}
			return tracking.WechatURLLinkUntil(ctx, api, credentials.AppID, credentials.Secret, path, query, trackingWechatEnv(), svc.Now(), share.Expires)
		})
		if err != nil {
			log.Printf("[ShareWeb] error operation=url_link share_id=%s error_type=%T", share.ID, err)
		}
		out := gin.H{"url_link": link.URL, "expires_at": link.Expires, "handoff_ready": handoff.Ready}
		if handoff.Ready {
			out["handoff_expires_at"] = handoff.Expires
		}
		trackingResponse(c, out, err)
	})
}
