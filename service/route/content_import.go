package route

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"service/config"
	"service/contentimport"
	"service/model"
	"service/tracking"
)

type importPlatformCapability struct {
	Platform      contentimport.Platform `json:"platform"`
	Available     bool                   `json:"available"`
	Reason        string                 `json:"reason,omitempty"`
	EmbeddedVideo bool                   `json:"supports_embedded_video"`
}
type importCapabilities struct {
	Enabled   bool                       `json:"enabled"`
	Platforms []importPlatformCapability `json:"platforms"`
}

func registerContentImportHandlers(g *gin.RouterGroup, store contentimport.JobStore, capabilities func(context.Context) importCapabilities) {
	g.GET("/capabilities", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		trackingResponse(c, capabilities(c.Request.Context()), nil)
	})
	jobs := g.Group("", func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		if store == nil || !capabilities(c.Request.Context()).Enabled {
			importResponse(c, nil, contentimport.ErrUnavailable)
			return
		}
		s, ok := c.Get("workbench.scope")
		scope, valid := s.(tracking.Scope)
		if !ok || !valid || !scope.Manage || scope.Actor <= 0 {
			importResponse(c, nil, &tracking.Error{Status: 403, Message: "无内容导入权限"})
			return
		}
		c.Next()
	})
	scope := func(c *gin.Context) tracking.Scope { return c.MustGet("workbench.scope").(tracking.Scope) }
	limiter := newTrackingPeerLimiter(10000, 20, time.Now)
	jobs.GET("", func(c *gin.Context) {
		s := scope(c)
		out, err := store.List(c.Request.Context(), s.Actor, s.Owners, 20)
		importResponse(c, gin.H{"items": out}, err)
	})
	jobs.POST("", func(c *gin.Context) {
		var in struct {
			URL    string `json:"source_url"`
			Owner  string `json:"owner_id"`
			Rights string `json:"rights"`
			Key    string `json:"idempotency_key"`
		}
		if !trackingBind(c, &in) {
			return
		}
		s := scope(c)
		if !s.Allows(in.Owner) {
			importResponse(c, nil, &tracking.Error{Status: 403, Message: "归属范围未授权"})
			return
		}
		link, err := contentimport.ParseSourceLink(in.URL)
		if err != nil || len(link.URL) > 2048 || (in.Rights != "own" && in.Rights != "authorized") || len(in.Key) < 8 || len(in.Key) > 128 {
			importResponse(c, nil, &tracking.Error{Status: 400, Message: "请检查链接、授权确认及请求标识"})
			return
		}
		ready := false
		for _, p := range capabilities(c.Request.Context()).Platforms {
			if p.Platform == link.Platform && p.Available {
				ready = true
			}
		}
		if !ready {
			importResponse(c, nil, &tracking.Error{Status: 503, Message: "该平台的导入服务尚未配置"})
			return
		}
		if !limiter.allow(strconv.FormatInt(s.Actor, 10)) {
			importResponse(c, nil, &tracking.Error{Status: 429, Message: "提交过于频繁，请稍后再试"})
			return
		}
		out, err := store.Submit(c.Request.Context(), contentimport.Request{Tenant: s.Tenant, Actor: s.Actor, Owner: in.Owner, URL: link.URL, Platform: link.Platform, Rights: in.Rights, Key: in.Key})
		importResponse(c, out, err)
	})
	ownJob := func(c *gin.Context) (contentimport.Job, error) {
		s := scope(c)
		out, err := store.Get(c.Request.Context(), s.Actor, c.Param("id"))
		if err == nil && !s.Allows(out.Owner) {
			return contentimport.Job{}, contentimport.ErrNotFound
		}
		return out, err
	}
	jobs.GET("/:id", func(c *gin.Context) { out, err := ownJob(c); importResponse(c, out, err) })
	jobs.POST("/:id/retry", func(c *gin.Context) {
		var in struct{}
		if !trackingBind(c, &in) {
			return
		}
		if _, err := ownJob(c); err != nil {
			importResponse(c, nil, err)
			return
		}
		if !limiter.allow(strconv.FormatInt(scope(c).Actor, 10)) {
			importResponse(c, nil, &tracking.Error{Status: 429, Message: "操作过于频繁，请稍后再试"})
			return
		}
		out, err := store.Retry(c.Request.Context(), scope(c).Actor, c.Param("id"))
		importResponse(c, out, err)
	})
}
func importResponse(c *gin.Context, v any, err error) {
	if err != nil {
		var known *tracking.Error
		if !errors.As(err, &known) {
			status, msg := 503, "内容导入服务暂不可用"
			if errors.Is(err, contentimport.ErrNotFound) {
				status, msg = 404, "导入任务不可访问"
			} else if errors.Is(err, contentimport.ErrConflict) {
				status, msg = 409, "任务状态已变化或已有相同请求，请刷新后查看"
			}
			err = &tracking.Error{Status: status, Message: msg}
		}
	}
	trackingResponse(c, v, err)
}

// Cookie files belong to the server operator. Never read a browser profile or
// accept a Cookie in an end-user request, and do not include values in logs.
func importCookieFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		return "", contentimport.ErrUnavailable
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 || info.Size() == 0 {
		return "", contentimport.ErrUnavailable
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", contentimport.ErrUnavailable
	}
	value := strings.TrimSpace(string(data))
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", contentimport.ErrUnavailable
	}
	return value, nil
}

func registerContentImports(parent context.Context, v1 *gin.RouterGroup, svc *tracking.Service, users *model.UserModel, codes *model.CodeSessionRedisModel, configs ...*config.Config) func() {
	var appConfig *config.Config
	if len(configs) > 0 {
		appConfig = configs[0]
	}
	configureContentMedia(svc, appConfig)
	ctx, cancel := context.WithCancel(parent)
	var jobs sync.WaitGroup
	enabled := os.Getenv("CONTENT_IMPORT_ENABLED") == "true"
	var store contentimport.JobStore
	if enabled && workbenchEnabled() && contentMediaConfigured(svc) {
		sqlStore, err := contentimport.NewSQLStore(users.DB, svc.Config.Tenant)
		if err == nil {
			verifyCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			err = sqlStore.VerifySchema(verifyCtx)
			stop()
		}
		if err == nil {
			store = sqlStore
		} else {
			log.Printf("[content-import] error disabled reason=schema_unavailable error_type=%T", err)
		}
	}
	fetcher := contentimport.NewHTTPFetcher()
	capture := contentimport.NewCaptureWorker(contentimport.CaptureWorkerConfig{
		Endpoint:       os.Getenv("CONTENT_IMPORT_CAPTURE_URL"),
		HealthEndpoint: os.Getenv("CONTENT_IMPORT_CAPTURE_HEALTH_URL"),
		DialAddress:    os.Getenv("CONTENT_IMPORT_CAPTURE_DIAL_ADDRESS"),
		Secret:         os.Getenv("CONTENT_IMPORT_CAPTURE_SECRET"),
	})
	resolvers := map[contentimport.Platform]contentimport.DocumentResolver{contentimport.WechatArticle: &contentimport.ArticleResolver{Fetcher: fetcher, VideoCapture: capture}}
	appID, secret := os.Getenv("CONTENT_IMPORT_DOUYIDOU_APP_ID"), os.Getenv("CONTENT_IMPORT_DOUYIDOU_APP_SECRET")
	var video contentimport.Resolver
	if strings.TrimSpace(appID) != "" && strings.TrimSpace(secret) != "" {
		video = contentimport.NewDouyidouResolver(contentimport.DouyidouConfig{AppID: appID, AppSecret: secret})
		resolvers[contentimport.Douyin] = contentimport.VideoDocumentResolver{Resolver: video}
	}
	resolvers[contentimport.Xiaohongshu] = contentimport.VideoDocumentResolver{Resolver: contentimport.XHSResolver{Fetcher: fetcher, Video: video}}
	if enabled && os.Getenv("CONTENT_IMPORT_YUANBAO_ENABLED") == "true" {
		cookie, err := importCookieFile(os.Getenv("CONTENT_IMPORT_YUANBAO_COOKIE_FILE"))
		if err != nil {
			log.Printf("[content-import] error channels_disabled reason=cookie_file_unavailable")
		} else if cookie != "" {
			resolvers[contentimport.Channels] = contentimport.VideoDocumentResolver{Resolver: contentimport.NewYuanbaoResolver(contentimport.YuanbaoConfig{Enabled: true, Cookie: cookie})}
		}
	}
	capabilities := func(ctx context.Context) importCapabilities {
		ready := store != nil && enabled && workbenchEnabled() && contentMediaConfigured(svc)
		out := importCapabilities{Enabled: ready, Platforms: []importPlatformCapability{}}
		for _, platform := range []contentimport.Platform{contentimport.WechatArticle, contentimport.Douyin, contentimport.Xiaohongshu, contentimport.Kuaishou, contentimport.Channels} {
			p := importPlatformCapability{Platform: platform, Available: ready && resolvers[platform] != nil}
			if !p.Available {
				p.Reason = "该平台导入服务尚未开通"
			}
			if platform == contentimport.WechatArticle && p.Available {
				p.EmbeddedVideo = capture.Available(ctx)
				if !p.EmbeddedVideo {
					p.Reason = "图文可导入；文内视频采集服务未就绪"
				}
			}
			if platform == contentimport.Xiaohongshu && p.Available && video == nil {
				p.Reason = "需公开页可读取的完整分享链接；视频需配置解析服务"
			}
			if platform == contentimport.Channels && p.Available {
				p.Reason = "实验接入，受登录状态和视频格式限制"
			}
			out.Platforms = append(out.Platforms, p)
		}
		return out
	}
	g := v1.Group("/miniprogram/workbench/imports", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
		c.Header("Cache-Control", "private, no-store")
		c.Next()
	}, TokenAuthRequired(codes), MiniprogramBannedBlock(users), func(c *gin.Context) {
		scope, err := loadMiniTrackingMaterialScope(c.Request.Context(), users.DB, svc.Config.Tenant, GetTokenUserID(c))
		if err != nil {
			importResponse(c, nil, err)
			return
		}
		c.Set("workbench.scope", scope)
		c.Next()
	})
	registerContentImportHandlers(g, store, capabilities)
	current := func(ctx context.Context, job contentimport.Job) (tracking.Scope, error) {
		scope, err := loadMiniTrackingMaterialScope(ctx, users.DB, svc.Config.Tenant, job.Actor)
		if err == nil && (job.Tenant != scope.Tenant || !scope.Allows(job.Owner) || !capabilities(ctx).Enabled) {
			err = contentimport.ErrUnavailable
		}
		return scope, err
	}
	if store != nil {
		worker := &contentimport.Worker{Store: store, Resolvers: resolvers, LeaseDuration: 3 * time.Minute, Authorize: func(ctx context.Context, job contentimport.Job) error { _, err := current(ctx, job); return err }, SaveMedia: func(ctx context.Context, job contentimport.Job, index int, media contentimport.RemoteMedia) (tracking.Asset, error) {
			scope, err := current(ctx, job)
			if err != nil {
				return tracking.Asset{}, err
			}
			fetch := func(ctx context.Context, platform contentimport.Platform, url string, max int64) ([]byte, string, error) {
				data, mime, err := fetcher.FetchMedia(ctx, platform, url, max)
				if err == nil {
					_, err = current(ctx, job)
				}
				return data, mime, err
			}
			return saveImportedMedia(ctx, svc, scope, job, index, media, fetch)
		}, SaveDraft: func(ctx context.Context, job contentimport.Job, doc contentimport.Document, assets []tracking.Asset) (string, error) {
			scope, err := current(ctx, job)
			if err != nil {
				return "", err
			}
			draft, err := importDraft(job, doc, assets)
			if err != nil {
				return "", err
			}
			provenance := map[string]string{"platform": string(doc.Source.Platform), "source_id": doc.Source.ID, "source_account_id": doc.SourceAccountID, "source_account_name": doc.SourceAccountName, "source_author": doc.Source.Author}
			out, err := svc.SaveImportedDraft(ctx, scope, job.ID, draft, provenance)
			return out.ID, err
		}}
		jobs.Add(1)
		go func() { defer jobs.Done(); worker.Run(ctx) }()
	}
	return func() { cancel(); jobs.Wait() }
}
