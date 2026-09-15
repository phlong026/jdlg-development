package model

import (
	"errors"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestUserRedisModel 启动一个内存 miniredis，并返回预置好 Stones 的 UserRedisModel
func newTestUserRedisModel(t *testing.T, userID int64, initStones int64) (*UserRedisModel, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	m := NewUserRedisModel(nil, rdb)

	// 直接写入 Redis，避免走 DB 回源逻辑
	key := "user:stones:" + itoa(userID)
	mr.Set(key, itoa(initStones))
	return m, mr
}

func itoa(v int64) string {
	// 独立实现避免引入 strconv（简短）
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestDeductStonesSuccess(t *testing.T) {
	m, _ := newTestUserRedisModel(t, 1, 100)
	if err := m.DeductStones(1, 30); err != nil {
		t.Fatalf("expect success, got %v", err)
	}
	left, err := m.GetStones(1)
	if err != nil {
		t.Fatalf("GetStones: %v", err)
	}
	if left != 70 {
		t.Fatalf("expect 70, got %d", left)
	}
}

func TestDeductStonesInsufficient(t *testing.T) {
	m, _ := newTestUserRedisModel(t, 1, 10)
	err := m.DeductStones(1, 30)
	if err == nil {
		t.Fatal("expect ErrInsufficientStones, got nil")
	}
	if !errors.Is(err, ErrInsufficientStones) {
		t.Fatalf("expect ErrInsufficientStones, got %v", err)
	}
	// 余额不应被修改
	left, _ := m.GetStones(1)
	if left != 10 {
		t.Fatalf("balance should stay 10, got %d", left)
	}
}

// 并发场景：20 个 goroutine 各扣 10 石头，初始 100 石头 → 成功 10 次，失败 10 次，余额 0
func TestDeductStonesAtomicUnderConcurrency(t *testing.T) {
	m, _ := newTestUserRedisModel(t, 1, 100)

	const goroutines = 20
	const amount = int64(10)

	var wg sync.WaitGroup
	successes := int32(0)
	failures := int32(0)
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := m.DeductStones(1, amount)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
				return
			}
			if errors.Is(err, ErrInsufficientStones) {
				failures++
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if successes != 10 || failures != 10 {
		t.Fatalf("expect 10 success + 10 insufficient, got %d / %d", successes, failures)
	}
	left, _ := m.GetStones(1)
	if left != 0 {
		t.Fatalf("expect balance 0 after race, got %d", left)
	}
}
