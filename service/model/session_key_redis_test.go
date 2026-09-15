package model

import (
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestSessionKeyModel(t *testing.T) (*SessionKeyRedisModel, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewSessionKeyRedisModel(rdb), mr
}

// TestSessionKey_RoundTripAndTTL 验证写入读出 + TTL 到期后失效。
//
// 这条测试存在的意义：
//   - 虚拟支付 prepare 接口拿到的 session_key 必须在 25 分钟内可用，否则用户支付失败。
//   - 一旦 TTL 写入失效（例如 Set 时把 ttl 写成 0），Redis 会"永久保存"，违反"敏感凭证短生命周期"原则。
//   - 用 miniredis FastForward 推进虚拟时间，能稳定复现"过期 → ErrSessionKeyNotFound"链路。
func TestSessionKey_RoundTripAndTTL(t *testing.T) {
	m, mr := newTestSessionKeyModel(t)

	if err := m.SetSessionKey("openid-1", "sk-abc", 25*time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, err := m.GetSessionKey("openid-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "sk-abc" {
		t.Fatalf("got=%q want %q", got, "sk-abc")
	}

	// 推进时间 30 分钟，触发过期
	mr.FastForward(30 * time.Minute)

	if _, err := m.GetSessionKey("openid-1"); !errors.Is(err, ErrSessionKeyNotFound) {
		t.Fatalf("expected ErrSessionKeyNotFound after expiry, got %v", err)
	}
}

// TestSessionKey_RejectsEmpty 锁定空入参直接拒绝。
//
// 这条测试存在的意义：openID 或 sessionKey 为空往往意味着上游链路出错（Code2Session 失败但被忽略）。
// 直接报错而非静默写入，能让"上游失败"在调用现场立即暴露，避免后续 GetSessionKey 拿到空串、签名出错却找不到根因。
func TestSessionKey_RejectsEmpty(t *testing.T) {
	m, _ := newTestSessionKeyModel(t)

	if err := m.SetSessionKey("", "sk", time.Minute); err == nil {
		t.Fatal("empty openID should fail")
	}
	if err := m.SetSessionKey("openid-1", "", time.Minute); err == nil {
		t.Fatal("empty sessionKey should fail")
	}
	if _, err := m.GetSessionKey(""); err == nil {
		t.Fatal("empty openID on Get should fail")
	}
}
