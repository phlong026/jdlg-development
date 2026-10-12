package route

import (
	"github.com/gin-gonic/gin"
	"service/tracking"
)

// The parent group supplies verified staff scope, session checks and mutation CSRF guards.
func registerTrackingRuleRoutes(group *gin.RouterGroup, svc *tracking.Service) {
	scopeFor := func(c *gin.Context) (tracking.Scope, error) {
		scope := c.MustGet("tracking.scope").(tracking.Scope)
		return trackingPrivateMutationScope(c, scope)
	}
	group.GET("/intent-rules", func(c *gin.Context) {
		scope, e := scopeFor(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		value, e := svc.Rules(c.Request.Context(), scope)
		trackingResponse(c, value, e)
	})
	group.POST("/intent-rules", func(c *gin.Context) {
		scope, e := scopeFor(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		var input tracking.RuleUpdate
		if !trackingBind(c, &input) {
			return
		}
		value, e := svc.UpdateRules(c.Request.Context(), scope, input)
		trackingResponse(c, value, e)
	})
	group.POST("/intent-rules/recompute", func(c *gin.Context) {
		scope, e := scopeFor(c)
		if e != nil {
			trackingResponse(c, nil, e)
			return
		}
		var input tracking.RecomputeInput
		if !trackingBind(c, &input) {
			return
		}
		value, e := svc.RecomputeIntents(c.Request.Context(), scope, input)
		trackingResponse(c, value, e)
	})
}
