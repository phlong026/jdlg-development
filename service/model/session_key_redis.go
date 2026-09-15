package model

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionKeyRedisModel 微信小程序 session_key 的 Redis 缓存。
//
// 设计取舍：
//   - 不落 MySQL：session_key 是敏感凭证，且生命周期短（微信侧 30 分钟），仅在 Redis 内保存可降低泄露面。
//   - 与 UserRedisModel 解耦：session_key 不属于用户业务字段，独立维护便于审计与回滚。
//   - 仅暴露 Set/Get：删除依赖 TTL 自动过期；显式删除会引入"半路过期"的复杂分支。
type SessionKeyRedisModel struct {
	Redis *redis.Client
	ctx   context.Context
}

// NewSessionKeyRedisModel 创建 session_key Redis 模型
func NewSessionKeyRedisModel(rdb *redis.Client) *SessionKeyRedisModel {
	return &SessionKeyRedisModel{Redis: rdb, ctx: context.Background()}
}

// ErrSessionKeyNotFound session_key 在 Redis 中不存在或已过期。
//
// 调用方应让用户重新发起 wx.login 拿新的 code 再换 session_key。
var ErrSessionKeyNotFound = errors.New("session_key 不存在或已过期")

// sessionKeyRedisKey 统一前缀，便于运维清理。
func sessionKeyRedisKey(openID string) string {
	return "wx:session_key:" + openID
}

// SetSessionKey 写入 session_key，TTL 由调用方决定。
//
// 推荐 TTL：25 分钟（微信文档默认 30 分钟，留 5 分钟余量供后端调用 /xpay/* 使用）。
func (m *SessionKeyRedisModel) SetSessionKey(openID, sessionKey string, ttl time.Duration) error {
	if openID == "" || sessionKey == "" {
		return errors.New("openID 或 sessionKey 不能为空")
	}
	return m.Redis.Set(m.ctx, sessionKeyRedisKey(openID), sessionKey, ttl).Err()
}

// GetSessionKey 取出 session_key；不存在时返回 ErrSessionKeyNotFound。
func (m *SessionKeyRedisModel) GetSessionKey(openID string) (string, error) {
	if openID == "" {
		return "", errors.New("openID 不能为空")
	}
	val, err := m.Redis.Get(m.ctx, sessionKeyRedisKey(openID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrSessionKeyNotFound
	}
	if err != nil {
		return "", err
	}
	return val, nil
}
