package component

import (
	"fmt"
	"log"
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
	if !cfg.IsDevelopment() && len(cfg.Server.SessionSecret) < 32 {
		return nil, fmt.Errorf("非开发环境必须配置至少 32 字符的独立 SESSION_SECRET（会话凭据不能用内置默认值签发）")
	}
	if cfg.IsDevelopment() && len(cfg.Server.SessionSecret) < 32 {
		log.Print("警告: SESSION_SECRET 使用内置默认值，仅供本地开发；部署时必须配置独立强密钥")
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

	// 统一会话 Cookie 安全属性：有效期 8 小时、HttpOnly、Secure、SameSite=Lax。
	// 此前仅在启用 CRM 桥接时设置，密码登录路径会落回 redistore 默认值
	// （30 天有效期、无 HttpOnly/Secure），三条登录路径行为不一致。
	store.Options(sessions.Options{Path: "/", MaxAge: 28800, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	SessionStore = store
	return store, nil
}

// GetSessionStore 获取session存储
func GetSessionStore() sessions.Store {
	return SessionStore
}
