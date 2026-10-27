package route

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func workbenchEnabled() bool { return os.Getenv("CONTENT_WORKBENCH_ENABLED") == "true" }

// Readiness must not create a directory during a GET or publish its server path.
func workbenchMediaConfigured() bool {
	dir := os.Getenv("TRACKING_MEDIA_DIR")
	if !filepath.IsAbs(dir) {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir() && info.Mode().Perm()&0077 == 0
}

func registerContentWorkbenchRoutes(v1 *gin.RouterGroup, svc *tracking.Service, users *model.UserModel, codes *model.CodeSessionRedisModel) {
	g := v1.Group("/miniprogram/workbench", func(c *gin.Context) {
		// Authentication reads the body for signature verification. Bound it first,
		// including multipart uploads, so an unauthenticated request cannot grow it.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 51<<20)
		c.Next()
	}, TokenAuthRequired(codes), MiniprogramBannedBlock(users), func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		scope, err := loadMiniTrackingMaterialScope(c.Request.Context(), users.DB, svc.Config.Tenant, GetTokenUserID(c))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		c.Set("workbench.scope", scope)
		c.Next()
	})
	registerContentWorkbenchHandlers(g, svc)
}

func registerContentWorkbenchHandlers(g *gin.RouterGroup, svc *tracking.Service) {
	registerNativeReferenceHandlers(g, svc)
	scope := func(c *gin.Context) tracking.Scope { return c.MustGet("workbench.scope").(tracking.Scope) }
	g.GET("/context", func(c *gin.Context) {
		media := contentMediaConfigured(svc)
		trackingResponse(c, gin.H{
			"enabled": workbenchEnabled(), "can_manage": true, "owners": scope(c).Owners,
			"publish_enabled":             svc.Config.ContentEnabled,
			"native_tracking_enabled":     svc.Config.Enabled,
			"media_upload_configured":     media,
			"url_link_configured":         media && trackingURLLinkEnabled() && miniprogramCredentials().AppID != "" && miniprogramCredentials().Secret != "",
			"official_account_configured": os.Getenv("OFFICIAL_ACCOUNT_APP_ID") != "" && os.Getenv("OFFICIAL_ACCOUNT_APP_SECRET") != "",
			"import_capabilities_path":    "/api/v1/miniprogram/workbench/imports/capabilities",
		}, nil)
	})
	// Keep draft listing separate from visit analytics: a content-management grant
	// never becomes permission to query all visitors belonging to the same owner.
	g.GET("/contents", func(c *gin.Context) {
		filter, err := trackingFilter(c)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		out, err := svc.Contents(c.Request.Context(), scope(c), filter, "")
		trackingResponse(c, out, err)
	})
	g.GET("/contents/:id", func(c *gin.Context) {
		filter, err := trackingFilter(c)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		out, err := svc.Contents(c.Request.Context(), scope(c), filter, c.Param("id"))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		items := out.Items.([]tracking.Content)
		if len(items) == 0 {
			trackingResponse(c, nil, &tracking.Error{Status: 404, Message: "素材不可访问"})
			return
		}
		trackingResponse(c, items[0], nil)
	})
	writes := g.Group("", func(c *gin.Context) {
		if !workbenchEnabled() {
			trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "手机内容制作暂未开通"})
			return
		}
		c.Next()
	})
	save := func(c *gin.Context) {
		var in tracking.Draft
		if !trackingBind(c, &in) {
			return
		}
		out, err := svc.SaveContent(c.Request.Context(), scope(c), c.Param("id"), in)
		trackingResponse(c, out, err)
	}
	writes.POST("/contents", save)
	writes.PATCH("/contents/:id", save)
	writes.POST("/contents/:id/preview", func(c *gin.Context) {
		var in struct {
			Revision int `json:"expected_revision"`
		}
		if !trackingBind(c, &in) {
			return
		}
		out, err := svc.Preview(c.Request.Context(), scope(c), c.Param("id"), in.Revision)
		trackingResponse(c, out, err)
	})
	writes.POST("/contents/:id/publish", func(c *gin.Context) {
		var in struct {
			Revision int    `json:"expected_revision"`
			Preview  string `json:"preview_token"`
			Key      string `json:"idempotency_key"`
		}
		if !trackingBind(c, &in) {
			return
		}
		out, err := svc.Publish(c.Request.Context(), scope(c), c.Param("id"), in.Revision, in.Preview, in.Key)
		trackingResponse(c, out, err)
	})
	writes.POST("/contents/:id/revoke", func(c *gin.Context) {
		var in struct {
			Revision int    `json:"expected_revision"`
			Reason   string `json:"reason"`
			Key      string `json:"idempotency_key"`
		}
		if !trackingBind(c, &in) {
			return
		}
		out, err := svc.Revoke(c.Request.Context(), scope(c), c.Param("id"), in.Revision, in.Reason, in.Key)
		trackingResponse(c, out, err)
	})
	writes.POST("/share-entries", func(c *gin.Context) {
		var in struct {
			Token string `json:"share_token"`
		}
		if !trackingBind(c, &in) {
			return
		}
		out, err := shareEntryDestination(c.Request.Context(), svc, scope(c), in.Token)
		trackingResponse(c, out, err)
	})
	g.GET("/media", func(c *gin.Context) {
		out, err := svc.ListMedia(c.Request.Context(), scope(c))
		trackingResponse(c, out, err)
	})
	g.Match([]string{http.MethodGet, http.MethodHead}, "/media/:id", func(c *gin.Context) {
		current := scope(c)
		serveTrackingMedia(c, svc, &current)
	})
	writes.POST("/media", func(c *gin.Context) {
		if !contentMediaConfigured(svc) {
			trackingResponse(c, nil, &tracking.Error{Status: 503, Message: "素材存储尚未配置，请联系管理员"})
			return
		}
		uploadTrackingMedia(c, svc, scope(c))
	})
}
