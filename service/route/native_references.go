package route

import (
	"github.com/gin-gonic/gin"
	"service/tracking"
	"strconv"
)

func registerNativeReferenceHandlers(g *gin.RouterGroup, svc *tracking.Service) {
	group := g.Group("", func(c *gin.Context) {
		if !workbenchEnabled() {
			trackingResponse(c, nil, &tracking.Error{Status: 403, Message: "手机内容制作暂未开通"})
			return
		}
		c.Next()
	})
	scope := func(c *gin.Context) tracking.Scope { return c.MustGet("workbench.scope").(tracking.Scope) }
	group.GET("/native-reference-candidates", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		out, err := svc.SearchNativeReferenceCandidates(c.Request.Context(), scope(c), c.Query("cursor"), limit, c.Query("q"))
		trackingResponse(c, out, err)
	})
	group.GET("/native-references", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		out, err := svc.NativeReferences(c.Request.Context(), scope(c), c.Query("cursor"), limit)
		trackingResponse(c, out, err)
	})
	group.POST("/native-references", func(c *gin.Context) {
		var in tracking.NativeReferenceInput
		if !projectNoteBind(c, &in) {
			return
		}
		out, err := svc.CreateNativeReference(c.Request.Context(), scope(c), in)
		trackingResponse(c, out, err)
	})
	group.DELETE("/native-references/:id", func(c *gin.Context) {
		err := svc.DeleteNativeReference(c.Request.Context(), scope(c), c.Param("id"))
		trackingResponse(c, gin.H{"removed": err == nil}, err)
	})
	group.GET("/native-references/:id/target", func(c *gin.Context) {
		out, err := svc.NativeReferenceTarget(c.Request.Context(), scope(c), c.Param("id"))
		trackingResponse(c, out, err)
	})
}
