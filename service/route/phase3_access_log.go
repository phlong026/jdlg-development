package route

import (
	"github.com/gin-gonic/gin"
	"log"
	"strings"
	"time"
)

func SkipPhase3RawAccessLog(c *gin.Context) bool {
	path := c.Request.URL.Path
	for _, prefix := range []string{"/share", "/api/v1/share-web", "/api/v1/wechat/official", "/api/v1/miniprogram/workbench", "/api/v1/miniprogram/notifications", "/api/v1/miniprogram/official-account", "/api/v1/miniprogram/tracking/my-shares"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// Short codes and OAuth codes are credentials. Log route patterns and timings,
// never the original URI, headers, error strings or recovered panic payload.
func Phase3AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !SkipPhase3RawAccessLog(c) {
			c.Next()
			return
		}
		started := time.Now()
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("[phase3] error request_panic")
				c.AbortWithStatusJSON(500, gin.H{"code": 500, "msg": "服务暂不可用"})
			}
			route := c.FullPath()
			if route == "" {
				route = "unmatched"
			}
			log.Printf("[phase3] method=%s route=%s status=%d elapsed_ms=%d", c.Request.Method, route, c.Writer.Status(), time.Since(started).Milliseconds())
		}()
		c.Next()
	}
}
