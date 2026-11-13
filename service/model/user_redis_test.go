package model

import (
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"testing"
)

func TestWalletNeverFallsBackToRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Set("user:stones:1", "100")
	m := NewUserRedisModel(nil, redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	if _, err := m.GetStones(1); err == nil {
		t.Fatal("missing database must fail closed")
	}
	for _, op := range []func() error{func() error { return m.AddStones(1, 10) }, func() error { return m.DeductStones(1, 10) }, func() error { return m.SetStones(1, 10) }} {
		if err := op(); err == nil {
			t.Fatal("paid write without database succeeded")
		}
	}
	if got, _ := mr.Get("user:stones:1"); got != "100" {
		t.Fatal("Redis evidence modified")
	}
}
