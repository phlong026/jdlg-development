package route

import (
	"context"
	"database/sql"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

// loadMiniTrackingMyScope turns current mini-account grants into the owner
// boundary for self-only share feedback. Content creation and visitor traffic
// are separate grants: can_manage alone must never disclose visitor history.
func loadMiniTrackingMyScope(ctx context.Context, db *sql.DB, tenant string, actor int64) (tracking.Scope, error) {
	scope := tracking.Scope{Tenant: tenant, Actor: actor, Owners: []string{}}
	if actor <= 0 {
		return scope, &tracking.Error{Status: 401, Message: "请先登录"}
	}
	if db == nil || tenant == "" {
		return scope, tracking.ErrUnavailable
	}
	rows, err := tracking.SourceReadDB(ctx, db).QueryContext(ctx, `SELECT t.owner_id FROM tracking_access t JOIN users u ON u.id=t.user_id WHERE t.user_id=? AND t.tenant_id=? AND t.enabled=TRUE AND t.can_view=TRUE AND u.user_type='miniprogram' AND COALESCE(u.status,'active')='active'`, actor, tenant)
	if err != nil {
		return scope, tracking.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var owner string
		if rows.Scan(&owner) != nil {
			return scope, tracking.ErrUnavailable
		}
		if owner != "" {
			scope.Owners = append(scope.Owners, owner)
		}
	}
	if rows.Err() != nil {
		return scope, tracking.ErrUnavailable
	}
	if len(scope.Owners) == 0 {
		return scope, &tracking.Error{Status: 403, Message: "无本人分享记录查看权限"}
	}
	scope.View = true
	return scope, nil
}

// registerTrackingMyRoutes is intentionally separate from the shared tracking
// route registration. The leader can attach it after constructing svc:
// registerTrackingMyRoutes(pub, svc, users, codes).
func registerTrackingMyRoutes(pub *gin.RouterGroup, svc *tracking.Service, users *model.UserModel, codes *model.CodeSessionRedisModel) {
	my := pub.Group("/my-shares", TokenAuthRequired(codes), MiniprogramBannedBlock(users), func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		scope, err := loadMiniTrackingMyScope(c.Request.Context(), users.DB, svc.Config.Tenant, GetTokenUserID(c))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		c.Set("tracking.my.scope", scope)
		c.Next()
	})
	registerTrackingMyHandlers(my, svc)
}

func registerTrackingMyHandlers(my *gin.RouterGroup, svc *tracking.Service) {
	scope := func(c *gin.Context) tracking.Scope {
		return c.MustGet("tracking.my.scope").(tracking.Scope)
	}
	my.GET("", func(c *gin.Context) {
		filter, err := trackingFilter(c)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		current := scope(c)
		out, err := svc.MyShares(c.Request.Context(), current, current.Actor, filter)
		trackingResponse(c, out, err)
	})
	my.GET("/:id/visits", func(c *gin.Context) {
		filter, err := trackingFilter(c)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		current := scope(c)
		out, err := svc.MyShareVisits(c.Request.Context(), current, current.Actor, c.Param("id"), filter)
		trackingResponse(c, out, err)
	})
}
