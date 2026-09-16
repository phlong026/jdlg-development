package route

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"service/config"
)

// corsAllowedOrigins 返回允许跨源携带凭据访问管理接口的来源白名单。
// ADMIN_ALLOWED_ORIGINS 是全后台 CORS 白名单；CRM_ALLOWED_ORIGINS 原本
// 只用于 CRM 写操作来源校验，这里合并进来避免同一来源配两遍。
func corsAllowedOrigins() []string {
	raw := os.Getenv("ADMIN_ALLOWED_ORIGINS") + "," + os.Getenv("CRM_ALLOWED_ORIGINS")
	var allowed []string
	for _, origin := range strings.Split(raw, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed = append(allowed, origin)
		}
	}
	return allowed
}

// CORSMiddleware CORS中间件
// 生产环境（或显式配置了白名单时）只回显白名单内的 Origin；
// 同源部署的请求不携带 Origin，不受影响。绝不能把任意 Origin
// 原样回显并附带 Allow-Credentials，否则已登录管理员的会话可被
// 任意第三方站点跨源读取。
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		cfg := config.Get()

		var allowedOrigin string
		allowed := corsAllowedOrigins()
		switch {
		case len(allowed) > 0:
			for _, candidate := range allowed {
				if origin == candidate {
					allowedOrigin = origin
					break
				}
			}
		case cfg.IsDevelopment():
			// 开发环境：没有配置白名单时保持宽松，方便本地调试
			if origin == "" {
				allowedOrigin = "*"
			} else {
				allowedOrigin = origin
			}
		}
		// 生产环境且未配置白名单：不下发任何 CORS 头，仅放行同源

		if allowedOrigin != "" {
			// 设置CORS响应头
			c.Writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			if allowedOrigin != "*" {
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, X-CRM-Request, Authorization, accept, origin, Cache-Control, X-Requested-With, token, token-signature, sin, md5-signature, pass, tm")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
			c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length")
			c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		}

		// 处理预检请求
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
