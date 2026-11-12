package route

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

func registerProjectCollectionHandlers(group *gin.RouterGroup, svc *tracking.Service) {
	collections := group.Group("/project-collections", func(c *gin.Context) {
		if !workbenchEnabled() {
			trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "手机内容制作暂未开通"})
			return
		}
		c.Next()
	})
	scope := func(c *gin.Context) tracking.Scope {
		return c.MustGet("workbench.scope").(tracking.Scope)
	}

	collections.GET("/candidates", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		out, err := svc.ProjectCollectionCandidates(c.Request.Context(), scope(c), c.Query("cursor"), limit)
		trackingResponse(c, out, err)
	})
	collections.GET("", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		out, err := svc.ProjectCollections(c.Request.Context(), scope(c), c.Query("cursor"), limit)
		trackingResponse(c, out, err)
	})
	collections.POST("", func(c *gin.Context) {
		var input tracking.ProjectCollectionDraft
		if !trackingBind(c, &input) {
			return
		}
		out, err := svc.SaveProjectCollection(c.Request.Context(), scope(c), "", input)
		trackingResponse(c, out, err)
	})
	collections.GET("/:id", func(c *gin.Context) {
		out, err := svc.ProjectCollection(c.Request.Context(), scope(c), c.Param("id"))
		trackingResponse(c, out, err)
	})
	collections.PATCH("/:id", func(c *gin.Context) {
		var input tracking.ProjectCollectionDraft
		if !trackingBind(c, &input) {
			return
		}
		out, err := svc.SaveProjectCollection(c.Request.Context(), scope(c), c.Param("id"), input)
		trackingResponse(c, out, err)
	})
	collections.DELETE("/:id", func(c *gin.Context) {
		var input struct {
			Revision int `json:"expected_revision"`
		}
		if !trackingBind(c, &input) {
			return
		}
		out, err := svc.DeleteProjectCollectionDraft(c.Request.Context(), scope(c), c.Param("id"), input.Revision)
		trackingResponse(c, out, err)
	})
	collections.POST("/:id/delete", func(c *gin.Context) {
		var input struct {
			Revision int `json:"expected_revision"`
		}
		if !trackingBind(c, &input) {
			return
		}
		out, err := svc.DeleteProjectCollection(c.Request.Context(), scope(c), c.Param("id"), input.Revision)
		trackingResponse(c, out, err)
	})
	collections.POST("/:id/preview", func(c *gin.Context) {
		var input struct {
			Revision int `json:"expected_revision"`
		}
		if !trackingBind(c, &input) {
			return
		}
		out, err := svc.PreviewProjectCollection(c.Request.Context(), scope(c), c.Param("id"), input.Revision)
		trackingResponse(c, out, err)
	})
	collections.POST("/:id/publish", func(c *gin.Context) {
		var input struct {
			Revision     int    `json:"expected_revision"`
			PreviewToken string `json:"preview_token"`
			Key          string `json:"idempotency_key"`
		}
		if !trackingBind(c, &input) {
			return
		}
		out, err := svc.PublishProjectCollection(c.Request.Context(), scope(c), c.Param("id"), input.Revision, input.PreviewToken, input.Key)
		trackingResponse(c, out, err)
	})
	collections.POST("/:id/revoke", func(c *gin.Context) {
		var input struct {
			Revision int    `json:"expected_revision"`
			Reason   string `json:"reason"`
			Key      string `json:"idempotency_key"`
		}
		if !trackingBind(c, &input) {
			return
		}
		out, err := svc.RevokeProjectCollection(c.Request.Context(), scope(c), c.Param("id"), input.Revision, input.Reason, input.Key)
		trackingResponse(c, out, err)
	})
}

// Public collection reads follow the same rule as published content reads:
// the immutable published version is readable without a management login, and
// current collection/source revocation is still enforced on every request.
func registerPublicProjectCollectionHandlers(group *gin.RouterGroup, svc *tracking.Service) {
	collections := group.Group("/project-collections", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	collections.GET("/:id/versions/:version", func(c *gin.Context) {
		version, _ := strconv.Atoi(c.Param("version"))
		out, err := svc.ReadPublishedProjectCollection(c.Request.Context(), c.Param("id"), version)
		trackingResponse(c, out, err)
	})
	collections.GET("/:id/versions/:version/items/:position", func(c *gin.Context) {
		version, position, ok := projectCollectionPosition(c)
		if !ok {
			return
		}
		out, err := svc.OpenPublishedProjectCollectionItem(c.Request.Context(), c.Param("id"), version, position)
		trackingResponse(c, out, err)
	})
	collections.GET("/:id/versions/:version/items/:position/content", func(c *gin.Context) {
		version, position, ok := projectCollectionPosition(c)
		if !ok {
			return
		}
		out, err := svc.ReadPublishedProjectCollectionItemContent(c.Request.Context(), c.Param("id"), version, position)
		trackingResponse(c, out, err)
	})
	serveItemMedia := func(c *gin.Context) {
		version, position, ok := projectCollectionPosition(c)
		if !ok {
			return
		}
		asset, err := svc.ReadPublishedProjectCollectionItemMedia(c.Request.Context(), c.Param("id"), version, position, c.Param("media"))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		serveContentMediaBytes(c, svc, asset)
	}
	collections.GET("/:id/versions/:version/items/:position/media/:media", serveItemMedia)
	collections.HEAD("/:id/versions/:version/items/:position/media/:media", serveItemMedia)
}

func projectCollectionPosition(c *gin.Context) (int, int, bool) {
	version, versionErr := strconv.Atoi(c.Param("version"))
	position, positionErr := strconv.Atoi(c.Param("position"))
	if versionErr != nil || positionErr != nil || version < 1 || position < 0 {
		trackingResponse(c, nil, &tracking.Error{Status: 404, Message: "合集作品不可访问"})
		return 0, 0, false
	}
	return version, position, true
}
