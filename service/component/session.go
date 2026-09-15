package component

import (
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/redis"
	"net/http"
	"os"
	"service/config"
)

var SessionStore sessions.Store

// InitSessionStore 初始化基于Redis的session存储
func InitSessionStore(cfg *config.Config) (sessions.Store, error) {
	if os.Getenv("CRM_BRIDGE_URL") != "" && len(cfg.Server.SessionSecret) < 32 {
		return nil, fmt.Errorf("客户管理接入需配置至少 32 字符的独立 SESSION_SECRET")
	}
	store, err := redis.NewStore(
		10,                               // 池大小
		"tcp",                            // 网络类型
		cfg.Redis.Addr,                   // 地址
		"",                               // username (Redis 6.0+)
		cfg.Redis.Password,               // 密码
		[]byte(cfg.Server.SessionSecret), // 加密key
	)
	if err != nil {
		return nil, err
	}

	if os.Getenv("CRM_BRIDGE_URL") != "" {
		store.Options(sessions.Options{Path: "/", MaxAge: 28800, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	}
	SessionStore = store
	return store, nil
}

// GetSessionStore 获取session存储
func GetSessionStore() sessions.Store {
	return SessionStore
}
