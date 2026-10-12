package route

import (
	"github.com/gin-gonic/gin"
	"service/tracking"
)

func registerTrackingCRMTargetRoutes(group *gin.RouterGroup, svc *tracking.Service) {
	group.GET("/visitors/:id/crm-target", func(c *gin.Context) {
		scope, e := trackingPrivateMutationScope(c, c.MustGet("tracking.scope").(tracking.Scope))
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		target, e := svc.VisitorCRMTarget(c.Request.Context(), scope, c.Param("id"))
		c.Header("Cache-Control", "private, no-store")
		trackingResponse(c, target, e)
	})
}
