package route

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	registerContentWorkbenchHandlers(g, svc, makeProjectNoteProfileResolver(users.DB), func(userID int64) (int64, int64) {
		return generationQuotaSnapshot(context.Background(), users, userID)
	})
	registerWritingPromptsRoute(g, users.DB)
	registerGenerationImportRoute(g, svc, users)
	registerSharePosterRoute(g, svc)
	registerPublishReminderRoutes(g, users.DB)
}

type workbenchOwnerChoice struct {
	OwnerID     string `json:"owner_id"`
	DisplayName string `json:"display_name"`
}

func workbenchOwnerChoices(ctx context.Context, scope tracking.Scope, resolve projectNoteProfileResolver) ([]workbenchOwnerChoice, error) {
	personalOwner := "designer-" + strconv.FormatInt(scope.Actor, 10)
	profile := model.UserProfilePresentation{}
	hasPersonalOwner := false
	for _, owner := range scope.Owners {
		if owner == personalOwner {
			hasPersonalOwner = true
			break
		}
	}
	if resolve != nil && scope.Actor > 0 && hasPersonalOwner {
		profiles, err := resolve(ctx, []int64{scope.Actor})
		if err != nil {
			return nil, err
		}
		profile = profiles[scope.Actor]
	}
	choices := make([]workbenchOwnerChoice, 0, len(scope.Owners))
	teamIndex := 0
	for _, owner := range scope.Owners {
		label := "个人创作空间"
		if owner == personalOwner {
			if nickname := strings.TrimSpace(profile.Nickname); nickname != "" {
				// B19 角色位（展示层）：自选身份做前缀，销售/设计师的产出可区分；
				// identity_type 是展示标签，绝不能当授权事实使用。
				if identity := strings.TrimSpace(profile.IdentityType); identity != "" && len([]rune(identity)) <= 12 {
					label = identity + "·" + nickname
				} else {
					label = nickname
				}
			}
		} else {
			teamIndex++
			label = "团队内容空间"
			if teamIndex > 1 {
				label += " " + strconv.Itoa(teamIndex)
			}
		}
		choices = append(choices, workbenchOwnerChoice{OwnerID: owner, DisplayName: label})
	}
	return choices, nil
}

func registerContentWorkbenchHandlers(g *gin.RouterGroup, svc *tracking.Service, profiles projectNoteProfileResolver, quota func(userID int64) (stones, monthGenerations int64)) {
	registerNativeReferenceHandlers(g, svc)
	registerProjectCollectionHandlers(g, svc)
	scope := func(c *gin.Context) tracking.Scope { return c.MustGet("workbench.scope").(tracking.Scope) }
	g.GET("/context", func(c *gin.Context) {
		media := contentMediaConfigured(svc)
		currentScope := scope(c)
		ownerChoices, err := workbenchOwnerChoices(c.Request.Context(), currentScope, profiles)
		if err != nil {
			log.Printf("[content-workbench] error operation=owner_profiles actor=%d error_type=%T", currentScope.Actor, err)
			ownerChoices, _ = workbenchOwnerChoices(c.Request.Context(), currentScope, nil)
		}
		// B1 配额条：失败返回 -1，前端隐藏而不是显示误导值。
		var stones, monthGenerations int64 = -1, -1
		if quota != nil {
			stones, monthGenerations = quota(GetTokenUserID(c))
		}
		trackingResponse(c, gin.H{
			"enabled": workbenchEnabled(), "can_manage": true, "owners": currentScope.Owners, "owner_choices": ownerChoices,
			"publish_enabled":             svc.Config.ContentEnabled,
			"native_tracking_enabled":     svc.Config.Enabled,
			"media_upload_configured":     media,
			"url_link_configured":         media && trackingURLLinkEnabled() && miniprogramCredentials().AppID != "" && miniprogramCredentials().Secret != "",
			"official_account_configured": os.Getenv("OFFICIAL_ACCOUNT_APP_ID") != "" && os.Getenv("OFFICIAL_ACCOUNT_APP_SECRET") != "",
			"import_capabilities_path":    "/api/v1/miniprogram/workbench/imports/capabilities",
			"stones_balance":              stones,
			"month_generations":           monthGenerations,
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
		// 分类Tab/项目关联的服务端过滤（仿真稿⑦）：type 由前端映射（article/note/video/document）。
		filter.TypeFilter = c.Query("type")
		filter.ProjectID = c.Query("project_id")
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
	writes.DELETE("/contents/:id", func(c *gin.Context) {
		var in struct {
			Revision int `json:"expected_revision"`
		}
		if !trackingBind(c, &in) {
			return
		}
		out, err := svc.DeleteContentDraft(c.Request.Context(), scope(c), c.Param("id"), in.Revision)
		trackingResponse(c, out, err)
	})
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
		limit, _ := strconv.Atoi(c.Query("limit"))
		var kinds []string
		if raw := strings.TrimSpace(c.Query("kind")); raw != "" {
			kinds = strings.Split(raw, ",")
		}
		out, err := svc.ListMediaPage(c.Request.Context(), scope(c), c.Query("cursor"), limit, c.Query("q"), kinds)
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
