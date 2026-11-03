package route

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

var errMySharesViewDenied = &tracking.Error{Status: 403, Message: "无本人分享记录查看权限"}

// loadMiniTrackingMyScope turns current mini-account grants into the owner
// boundary for self-only share feedback. Certified designers may view their own
// shares, including forwarded public materials; can_manage alone is not enough.
func loadMiniTrackingMyScope(ctx context.Context, db *sql.DB, tenant string, actor int64) (tracking.Scope, error) {
	scope := tracking.Scope{Tenant: tenant, Actor: actor, Owners: []string{}}
	if actor <= 0 {
		return scope, &tracking.Error{Status: 401, Message: "请先登录"}
	}
	if db == nil || tenant == "" {
		return scope, tracking.ErrUnavailable
	}
	// The owner belongs to the material, not necessarily its sharer. Discover
	// only this actor's share owners; the service still enforces exact Sharer.
	sharer := "u:" + strconv.FormatInt(actor, 10)
	rows, err := tracking.SourceReadDB(ctx, db).QueryContext(ctx, `SELECT t.owner_id FROM tracking_access t JOIN users u ON u.id=t.user_id
        WHERE t.user_id=? AND t.tenant_id=? AND t.enabled=TRUE AND t.can_view=TRUE AND u.user_type='miniprogram' AND COALESCE(u.status,'active')='active'
        UNION
        SELECT candidate.owner_id FROM users u CROSS JOIN (
            SELECT CONCAT('designer-', ?) AS owner_id
            UNION
            SELECT r.owner_id FROM tracking_records r WHERE r.tenant_id=? AND r.kind='share'
                AND (r.visitor_key=? OR (r.visitor_key='' AND JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.sharer_id'))=?))
        ) candidate
        WHERE u.id=? AND u.user_type='miniprogram' AND COALESCE(u.status,'active')='active'
            AND EXISTS (SELECT 1 FROM certification_applications ca WHERE ca.user_id=u.id
                AND ca.type='designer' AND ca.status='approved' AND TRIM(ca.identity_type)='设计师')
            AND NOT EXISTS (SELECT 1 FROM tracking_access denied WHERE denied.user_id=u.id
                AND denied.tenant_id=? AND denied.owner_id=candidate.owner_id
                AND (denied.enabled=FALSE OR denied.can_view=FALSE))`, actor, tenant, actor, tenant, sharer, sharer, actor, tenant)
	if err != nil {
		return scope, tracking.ErrUnavailable
	}
	defer rows.Close()
	seen := make(map[string]bool)
	for rows.Next() {
		var owner string
		if rows.Scan(&owner) != nil {
			return scope, tracking.ErrUnavailable
		}
		if owner != "" && !seen[owner] {
			seen[owner] = true
			scope.Owners = append(scope.Owners, owner)
		}
	}
	if rows.Err() != nil {
		return scope, tracking.ErrUnavailable
	}
	if len(scope.Owners) == 0 {
		return scope, errMySharesViewDenied
	}
	scope.View = true
	return scope, nil
}

// registerTrackingMyRoutes is intentionally separate from the shared tracking
// route registration. The leader can attach it after constructing svc:
// registerTrackingMyRoutes(pub, svc, users, codes).
func registerTrackingMyRoutes(pub *gin.RouterGroup, svc *tracking.Service, users *model.UserModel, codes *model.CodeSessionRedisModel) {
	my := pub.Group("/my-shares", TokenAuthRequired(codes), MiniprogramBannedBlock(users), miniTrackingMyScopeMiddleware(users.DB, svc.Config.Tenant))
	registerTrackingMyHandlers(my, svc)
	// 我的客户视图：与分享记录同数据源（tracking_records），按访客聚合。
	visitors := pub.Group("/my-visitors", TokenAuthRequired(codes), MiniprogramBannedBlock(users), miniTrackingMyScopeMiddleware(users.DB, svc.Config.Tenant))
	visitors.GET("", func(c *gin.Context) {
		filter, err := trackingFilter(c)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		current := c.MustGet("tracking.my.scope").(tracking.Scope)
		out, err := svc.MyVisitors(c.Request.Context(), current, current.Actor, filter)
		trackingResponse(c, out, err)
	})
	visitors.GET("/entries", func(c *gin.Context) {
		filter, err := trackingFilter(c)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		current := c.MustGet("tracking.my.scope").(tracking.Scope)
		out, err := svc.MyVisitorEntries(c.Request.Context(), current, current.Actor, filter)
		trackingResponse(c, out, err)
	})
	stats := pub.Group("/my-content-stats", TokenAuthRequired(codes), MiniprogramBannedBlock(users), miniTrackingMyScopeMiddleware(users.DB, svc.Config.Tenant))
	stats.GET("", func(c *gin.Context) {
		filter, err := trackingFilter(c)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		current := c.MustGet("tracking.my.scope").(tracking.Scope)
		out, err := svc.MyContentStats(c.Request.Context(), current, current.Actor, filter)
		trackingResponse(c, out, err)
	})
}

func miniTrackingMyScopeMiddleware(db *sql.DB, tenant string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		scope, err := loadMiniTrackingMyScope(c.Request.Context(), db, tenant, GetTokenUserID(c))
		// Capability discovery is a normal page load, not a visitor-data read.
		// Only an absent grant becomes false; authentication and storage errors remain errors.
		if errors.Is(err, errMySharesViewDenied) && c.Request.Method == "GET" && c.FullPath() == "/api/v1/miniprogram/tracking/my-shares/context" {
			trackingResponse(c, gin.H{"can_view": false, "owners": []string{}}, nil)
			c.Abort()
			return
		}
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		c.Set("tracking.my.scope", scope)
		c.Next()
	}
}

func registerTrackingMyHandlers(my *gin.RouterGroup, svc *tracking.Service) {
	scope := func(c *gin.Context) tracking.Scope {
		return c.MustGet("tracking.my.scope").(tracking.Scope)
	}
	my.GET("/context", func(c *gin.Context) {
		trackingResponse(c, gin.H{"can_view": true, "owners": scope(c).Owners}, nil)
	})
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
