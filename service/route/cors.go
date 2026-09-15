package route

import (
	"net/http"
	"service/config"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware CORS中间件
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		cfg := config.Get()

		// 开发环境允许所有来源，生产环境只允许配置的来源
		allowedOrigin := origin
		if cfg.IsDevelopment() {
			// 开发环境：如果没有Origin头，允许所有来源
			if origin == "" {
				allowedOrigin = "*"
			}
		} else {
			// 生产环境：只允许指定的来源，如果没有Origin头则不允许
			if origin == "" {
				allowedOrigin = ""
			}
		}

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
