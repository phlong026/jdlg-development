package route

import (
	"github.com/gin-gonic/gin"
	"service/tracking"
	"strconv"
)

func registerProjectMemberReader(root *gin.RouterGroup, svc *tracking.Service, deps projectNoteHandlerDeps) {
	root.GET("/member-projects/:id", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		out, err := svc.ProjectNoteMemberDirectory(c.Request.Context(), deps.actor(c), c.Param("id"), limit, c.Query("cursor"))
		trackingResponse(c, out, err)
	})
	root.GET("/member-projects/:id/versions/:version", func(c *gin.Context) {
		out, err := svc.ProjectNoteMemberVersion(c.Request.Context(), deps.actor(c), c.Param("id"), c.Param("version"))
		trackingResponse(c, out, err)
	})
	root.GET("/member-projects/:id/versions/:version/media/:media", func(c *gin.Context) {
		asset, err := svc.ProjectNoteMemberMedia(c.Request.Context(), deps.actor(c), c.Param("id"), c.Param("version"), c.Param("media"))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		serveContentMediaBytes(c, svc, asset)
	})
	root.POST("/member-projects/:id/versions/:version/media/:media/playback", func(c *gin.Context) {
		actor := deps.actor(c)
		asset, err := svc.ProjectNoteMemberMedia(c.Request.Context(), actor, c.Param("id"), c.Param("version"), c.Param("media"))
		if err == nil {
			err = requireSharedVideo(asset)
		}
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		sharedVideoPlaybackResponse(c, sharedVideoPlaybackGrant{Kind: sharedVideoMember, Actor: actor, ProjectID: c.Param("id"), VersionID: c.Param("version"), AssetID: c.Param("media")})
	})
}
