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

// Mini-program users cannot log in as management users. Explicit mini-account
// grants are deliberately separate; management_access never implies this grant.
func loadMiniTrackingMaterialScope(ctx context.Context, db *sql.DB, tenant string, actor int64) (tracking.Scope, error) {
	scope := tracking.Scope{Tenant: tenant, Actor: actor, Owners: []string{}}
	if actor <= 0 {
		return scope, &tracking.Error{Status: 401, Message: "请先登录"}
	}
	if db == nil || tenant == "" {
		return scope, tracking.ErrUnavailable
	}
	rows, err := tracking.SourceReadDB(ctx, db).QueryContext(ctx, `SELECT t.owner_id FROM tracking_access t JOIN users u ON u.id=t.user_id WHERE t.user_id=? AND t.tenant_id=? AND t.enabled=TRUE AND t.can_manage=TRUE AND u.user_type='miniprogram' AND COALESCE(u.status,'active')='active'`, actor, tenant)
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
		return scope, &tracking.Error{Status: 403, Message: "无素材分享权限"}
	}
	scope.Manage = true
	return scope, nil
}
func registerTrackingMaterialRoutes(pub *gin.RouterGroup, svc *tracking.Service, users *model.UserModel, codes *model.CodeSessionRedisModel) {
	svc.IsContentOperator = func(ctx context.Context, account int64, owner string) (bool, error) {
		scope, err := loadMiniTrackingMaterialScope(ctx, users.DB, svc.Config.Tenant, account)
		var denied *tracking.Error
		if errors.As(err, &denied) && (denied.Status == 401 || denied.Status == 403) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return owner != "" && scope.Manage && scope.Allows(owner), nil
	}
	materials := pub.Group("/materials", TokenAuthRequired(codes), MiniprogramBannedBlock(users), func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		scope, err := loadMiniTrackingMaterialScope(c.Request.Context(), users.DB, svc.Config.Tenant, GetTokenUserID(c))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		c.Set("tracking.material.scope", scope)
		c.Next()
	})
	materials.GET("", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		out, err := svc.PublishedMaterials(c.Request.Context(), c.MustGet("tracking.material.scope").(tracking.Scope), c.Query("cursor"), limit)
		trackingResponse(c, out, err)
	})
	materials.GET("/:id/versions/:version", func(c *gin.Context) {
		version, _ := strconv.Atoi(c.Param("version"))
		out, err := svc.OpenPublishedMaterial(c.Request.Context(), c.MustGet("tracking.material.scope").(tracking.Scope), c.Param("id"), version)
		trackingResponse(c, out, err)
	})
}
