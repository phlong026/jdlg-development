package route

import (
	"context"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"service/component"
	"service/config"
	"service/function"
	"service/tracking"
)

// shareIDPattern matches the opaque() output: base64url, exactly 32 chars.
var shareIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)

// registerSharePosterRoute serves wxacode poster generation (B14). Every scene
// is a resolvable share token; an opaque ID is never treated as authorization.
func registerSharePosterRoute(g *gin.RouterGroup, svc *tracking.Service) {
	g.GET("/shares/:id/poster", func(c *gin.Context) {
		scene, err := existingSharePosterScene(c.Request.Context(), svc, c.MustGet("workbench.scope").(tracking.Scope), c.Param("id"))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		generatePoster(c, scene)
	})
	g.GET("/content-poster", func(c *gin.Context) {
		version, err := strconv.Atoi(c.Query("content_version"))
		if err != nil || version < 1 {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "无效的内容版本"})
			return
		}
		scene, err := contentPosterScene(c.Request.Context(), svc, c.MustGet("workbench.scope").(tracking.Scope), c.Query("content_id"), version)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		generatePoster(c, scene)
	})
}

func contentPosterScene(ctx context.Context, svc *tracking.Service, scope tracking.Scope, contentID string, version int) (string, error) {
	if scope.Tenant != svc.Config.Tenant || scope.Actor <= 0 || !scope.Manage {
		return "", &tracking.Error{Status: http.StatusForbidden, Message: "无素材分享权限"}
	}
	content, err := svc.ReadContent(ctx, contentID, version)
	if err != nil {
		return "", err
	}
	if !scope.Allows(content.Owner) {
		return "", &tracking.Error{Status: http.StatusNotFound, Message: "素材不可访问"}
	}
	key := trackingCodeCacheKey(svc.Config.Tenant, scope.Actor, contentID, version, trackingWechatEnv()+"/poster", svc.Now())
	share, err := svc.CreateConsoleShare(ctx, tracking.ShareInput{Route: trackingReaderRoute, Content: contentID, Version: version, Key: "poster-" + key}, scope.Actor)
	if err != nil {
		return "", err
	}
	scene, ok := share["share_token"].(string)
	if !ok || !shareIDPattern.MatchString(scene) {
		return "", tracking.ErrUnavailable
	}
	return scene, nil
}

func existingSharePosterScene(ctx context.Context, svc *tracking.Service, scope tracking.Scope, token string) (string, error) {
	if !shareIDPattern.MatchString(token) {
		return "", &tracking.Error{Status: http.StatusBadRequest, Message: "无效的分享标识"}
	}
	share, err := svc.ResolveShareRecord(ctx, token)
	if err != nil || share.Route != trackingReaderRoute || share.Content == "" || share.Version < 1 {
		return "", &tracking.Error{Status: http.StatusNotFound, Message: "分享入口不可访问"}
	}
	actualSharer := scope.Tenant == svc.Config.Tenant && scope.Actor > 0 && share.Sharer == "u:"+strconv.FormatInt(scope.Actor, 10)
	authorizedOwner := scope.Tenant == svc.Config.Tenant && scope.Manage && scope.Allows(share.Owner)
	if !actualSharer && !authorizedOwner {
		return "", &tracking.Error{Status: http.StatusNotFound, Message: "分享入口不可访问"}
	}
	return token, nil
}

func generatePoster(c *gin.Context, sceneID string) {
	cfg := config.Get()
	if cfg.Wechat.AppID == "" || cfg.Wechat.AppSecret == "" {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "微信小程序未配置"})
		return
	}
	// The page is fixed and the environment accepts only release/trial/develop.
	// Scanning resolves sceneID through /share-entry before opening this version.
	imageData, err := tracking.WechatCode(c.Request.Context(), cfg.Wechat.AppID, cfg.Wechat.AppSecret, sceneID, trackingReaderRoute, trackingWechatEnv())
	if err != nil {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "生成小程序码失败"})
		return
	}
	cosClient := component.GetCOSClient()
	if cosClient == nil {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "存储未初始化"})
		return
	}
	now := time.Now().UTC()
	objectKey := path.Join("share_posters", now.Format("2006/01/02"), sceneID+".png")
	url, err := function.UploadBytes(c.Request.Context(), cosClient, cfg, objectKey, imageData, "image/png")
	if err != nil {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "海报码上传失败"})
		return
	}
	trackingResponse(c, gin.H{"poster_url": url, "scene_id": sceneID}, nil)
}
