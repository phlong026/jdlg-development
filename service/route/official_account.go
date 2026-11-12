package route

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/officialaccount"
)

const officialOAuthCallbackPath = "/api/v1/wechat/official/oauth/callback"

type OfficialAccountDeps struct {
	Config   officialaccount.Config
	Store    officialaccount.BindingStore
	Provider officialaccount.OAuthProvider
	Receipts officialaccount.ReceiptStore
	Now      func() time.Time
}

// RegisterOfficialAccountRoutes adds authenticated binding endpoints and the
// public WeChat callbacks. The caller passes the existing /api/v1 and
// /api/v1/miniprogram groups; this function applies mini-program auth itself.
func RegisterOfficialAccountRoutes(public, miniprogram *gin.RouterGroup, deps OfficialAccountDeps, users *model.UserModel, codes *model.CodeSessionRedisModel) {
	authed := miniprogram.Group("/official-account", TokenAuthRequired(codes), MiniprogramBannedBlock(users))
	registerOfficialAccountHandlers(public, authed, deps)
}

func registerOfficialAccountHandlers(public, authed *gin.RouterGroup, deps OfficialAccountDeps) {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	authed.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.Next()
	})
	authed.GET("/binding", func(c *gin.Context) {
		status := deps.Config.Status()
		result := gin.H{"capability": status, "bound": false, "subscribed": false}
		if deps.Config.Enabled && deps.Config.AppID != "" {
			if deps.Store == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "msg": "服务号服务暂不可用"})
				return
			}
			binding, err := deps.Store.ResolveBinding(c.Request.Context(), GetTokenUserID(c), deps.Config.AppID)
			if err == nil {
				result["bound"], result["subscribed"], result["binding_version"] = true, true, binding.Version
			} else if errors.Is(err, officialaccount.ErrNotSubscribed) {
				result["bound"] = true
			} else if !errors.Is(err, officialaccount.ErrNotBound) {
				officialAccountResponse(c, nil, err)
				return
			}
		}
		officialAccountResponse(c, result, nil)
	})
	authed.POST("/binding", func(c *gin.Context) {
		if deps.Store == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "msg": "服务号服务暂不可用"})
			return
		}
		if !deps.Config.Status().BindingAvailable {
			officialAccountResponse(c, nil, officialaccount.ErrNotConfigured)
			return
		}
		intent, err := deps.Store.CreateIntent(c.Request.Context(), GetTokenUserID(c), deps.Now(), officialaccount.DefaultIntentTTL)
		if err != nil {
			officialAccountResponse(c, nil, err)
			return
		}
		callback := deps.Config.PublicOrigin + officialOAuthCallbackPath
		query := url.Values{
			"appid": {deps.Config.AppID}, "redirect_uri": {callback}, "response_type": {"code"},
			"scope": {"snsapi_base"}, "state": {intent.State},
		}
		officialAccountResponse(c, gin.H{
			"authorization_url": "https://open.weixin.qq.com/connect/oauth2/authorize?" + query.Encode() + "#wechat_redirect",
			"expires_at":        intent.ExpiresAt,
		}, nil)
	})
	authed.DELETE("/binding", func(c *gin.Context) {
		if deps.Store == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "msg": "服务号服务暂不可用"})
			return
		}
		if !deps.Config.Enabled || deps.Config.AppID == "" {
			officialAccountResponse(c, nil, officialaccount.ErrNotConfigured)
			return
		}
		err := deps.Store.Unbind(c.Request.Context(), GetTokenUserID(c), deps.Config.AppID, deps.Now())
		if errors.Is(err, officialaccount.ErrNotBound) {
			err = nil
		}
		officialAccountResponse(c, gin.H{"unbound": err == nil}, err)
	})

	callback := public.Group("/wechat/official")
	callback.GET("/oauth/callback", func(c *gin.Context) {
		if !deps.Config.Status().BindingAvailable || deps.Store == nil || deps.Provider == nil {
			c.String(http.StatusServiceUnavailable, "unavailable")
			return
		}
		code, state := c.Query("code"), c.Query("state")
		if code == "" || state == "" || len(code) > 256 || len(state) > 128 {
			c.String(http.StatusBadRequest, "invalid callback")
			return
		}
		identity, err := deps.Provider.ExchangeCode(c.Request.Context(), code)
		if err != nil {
			c.String(http.StatusBadGateway, "provider unavailable")
			return
		}
		if _, err = deps.Store.ConsumeAndBind(c.Request.Context(), state, deps.Config.AppID, identity.OpenID, identity.UnionID, identity.Subscribed, deps.Now()); err != nil {
			if errors.Is(err, officialaccount.ErrInvalidState) || errors.Is(err, officialaccount.ErrExpiredState) || errors.Is(err, officialaccount.ErrConsumedState) {
				c.String(http.StatusBadRequest, "invalid state")
				return
			}
			c.String(http.StatusServiceUnavailable, "binding unavailable")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte("<!doctype html><meta charset=utf-8><title>绑定完成</title><p>服务号身份绑定完成，请返回甲第灵光小程序。</p>"))
	})
	callback.GET("/callback", func(c *gin.Context) {
		if !deps.Config.Status().BindingAvailable || deps.Store == nil || !freshWechatTimestamp(c.Query("timestamp"), deps.Now()) {
			c.String(http.StatusServiceUnavailable, "unavailable")
			return
		}
		encrypted, signature := c.Query("echostr"), c.Query("msg_signature")
		if encrypted == "" || signature == "" {
			c.String(http.StatusForbidden, "invalid signature")
			return
		}
		plain, err := officialaccount.DecryptEcho(deps.Config, c.Query("timestamp"), c.Query("nonce"), signature, encrypted)
		if err != nil {
			c.String(http.StatusForbidden, "invalid signature")
			return
		}
		c.String(http.StatusOK, string(plain))
	})
	callback.POST("/callback", func(c *gin.Context) {
		if !deps.Config.Status().BindingAvailable || deps.Store == nil || !freshWechatTimestamp(c.Query("timestamp"), deps.Now()) {
			c.String(http.StatusServiceUnavailable, "unavailable")
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, (128<<10)+1))
		if err != nil || len(body) > 128<<10 {
			c.String(http.StatusBadRequest, "invalid message")
			return
		}
		event, err := officialaccount.ParseEncryptedCallback(deps.Config, c.Query("timestamp"), c.Query("nonce"), c.Query("msg_signature"), body)
		if errors.Is(err, officialaccount.ErrUnsupportedEvent) {
			c.String(http.StatusOK, "success")
			return
		}
		if err != nil {
			c.String(http.StatusForbidden, "invalid message")
			return
		}
		if event.Type == officialaccount.EventTemplateSendFinished {
			if deps.Receipts == nil {
				c.String(http.StatusServiceUnavailable, "receipt unavailable")
				return
			}
			if _, err = deps.Receipts.RecordProviderReceipt(c.Request.Context(), event.ProviderMessageID, event.ProviderStatus, deps.Now()); err != nil {
				c.String(http.StatusServiceUnavailable, "receipt unavailable")
				return
			}
			c.String(http.StatusOK, "success")
			return
		}
		_, err = deps.Store.ApplyEvent(c.Request.Context(), event, deps.Now())
		if errors.Is(err, officialaccount.ErrInvalidState) || errors.Is(err, officialaccount.ErrExpiredState) || errors.Is(err, officialaccount.ErrConsumedState) {
			// A stale QR state is permanent. Acknowledge it so WeChat does not retry.
			c.String(http.StatusOK, "success")
			return
		}
		if err != nil {
			c.String(http.StatusServiceUnavailable, "event unavailable")
			return
		}
		c.String(http.StatusOK, "success")
	})
}

func freshWechatTimestamp(raw string, now time.Time) bool {
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || strings.TrimSpace(raw) == "" {
		return false
	}
	delta := now.Unix() - seconds
	return delta >= -300 && delta <= 300
}

func officialAccountResponse(c *gin.Context, value any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": value})
		return
	}
	status, message := http.StatusServiceUnavailable, "服务号服务暂不可用"
	if errors.Is(err, officialaccount.ErrNotConfigured) {
		status, message = http.StatusConflict, "服务号能力未启用"
	} else if errors.Is(err, officialaccount.ErrInvalidInput) {
		status, message = http.StatusBadRequest, "请求参数无效"
	}
	c.AbortWithStatusJSON(status, gin.H{"code": status, "msg": message})
}
