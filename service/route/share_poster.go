package route

import (
	"net/http"
	"path"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"service/component"
	"service/config"
	"service/function"
	"service/tracking"
)

// shareIDPattern matches the opaque() output: base64url, exactly 32 chars.
var shareIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)

// registerSharePosterRoute serves wxacode poster generation (B14).
// scene = share_id (32 chars from opaque(), fits the 32-char scene limit exactly).
// Ownership is enforced by the workbench scope middleware; share_id opacity
// makes cross-user guessing impractical.
func registerSharePosterRoute(g *gin.RouterGroup, _ *tracking.Service) {
	g.GET("/shares/:id/poster", func(c *gin.Context) {
		shareID := c.Param("id")
		if !shareIDPattern.MatchString(shareID) {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "无效的分享标识"})
			return
		}
		cfg := config.Get()
		if cfg.Wechat.AppID == "" || cfg.Wechat.AppSecret == "" {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "微信小程序未配置"})
			return
		}
		accessToken, err := function.GetAccessToken(cfg.Wechat.AppID, cfg.Wechat.AppSecret)
		if err != nil {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "获取微信凭证失败"})
			return
		}
		// scene = share_id; 扫码后 contentreader onLoad 从 scene 参数解析 share 归因。
		page := c.DefaultQuery("page", "pages/contentreader/contentreader")
		imageData, err := function.GetWxacodeUnlimit(accessToken, shareID, page, 430)
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
		objectKey := path.Join("share_posters", now.Format("2006/01/02"), shareID+".png")
		url, err := function.UploadBytes(c.Request.Context(), cosClient, cfg, objectKey, imageData, "image/png")
		if err != nil {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "海报码上传失败"})
			return
		}
		trackingResponse(c, gin.H{"poster_url": url, "share_id": shareID}, nil)
	})
}
