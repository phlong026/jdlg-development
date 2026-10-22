package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestCostEstimateShareStore(t *testing.T) (*CostEstimateShareRedisModel, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return NewCostEstimateShareRedisModel(client), mr
}

func TestCostEstimateShareChecksAbsoluteExpiryEvenWhenRedisTTLWasRestored(t *testing.T) {
	store, mr := newTestCostEstimateShareStore(t)
	created, err := store.Create(context.Background(), 42, CostEstimateShareResult{Version: 4})
	if err != nil {
		t.Fatal(err)
	}
	created.ExpiresAt = time.Now().Add(-time.Second)
	payload, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	mr.Set(costEstimateShareKey(created.ReportID), string(payload))
	if _, err := store.Get(context.Background(), created.ReportID); !errors.Is(err, ErrCostEstimateShareNotFound) {
		t.Fatalf("restored stale snapshot remained readable: %v", err)
	}
}

func TestCostEstimateShareRoundTripAndExpiry(t *testing.T) {
	store, mr := newTestCostEstimateShareStore(t)
	result := CostEstimateShareResult{Version: 4, EstimatedTotalCost: "80.00万元"}
	created, err := store.Create(context.Background(), 42, result)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.ReportID) != 32 || !created.ExpiresAt.After(time.Now().Add(29*24*time.Hour)) {
		t.Fatalf("invalid share metadata: %+v", created)
	}
	if ttl := mr.TTL(costEstimateShareKey(created.ReportID)); ttl != CostEstimateShareTTL {
		t.Fatalf("ttl = %s", ttl)
	}
	got, err := store.Get(context.Background(), created.ReportID)
	if err != nil || !reflect.DeepEqual(got.Result, result) || got.OwnerID != 42 {
		t.Fatalf("get = %+v, %v", got, err)
	}
	mr.FastForward(CostEstimateShareTTL)
	if _, err := store.Get(context.Background(), created.ReportID); !errors.Is(err, ErrCostEstimateShareNotFound) {
		t.Fatalf("expired share remained readable: %v", err)
	}
}

func TestCostEstimateShareOnlyCreatorCanRevoke(t *testing.T) {
	store, _ := newTestCostEstimateShareStore(t)
	created, err := store.Create(context.Background(), 42, CostEstimateShareResult{Version: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(context.Background(), created.ReportID, 43); !errors.Is(err, ErrCostEstimateShareNotFound) {
		t.Fatalf("foreign revoke = %v", err)
	}
	if _, err := store.Get(context.Background(), created.ReportID); err != nil {
		t.Fatalf("foreign revoke changed report: %v", err)
	}
	if err := store.Revoke(context.Background(), created.ReportID, 42); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), created.ReportID); !errors.Is(err, ErrCostEstimateShareNotFound) {
		t.Fatalf("revoked share remained readable: %v", err)
	}
}

func TestCostEstimateShareRejectsInvalidIdentityAndUnavailableStore(t *testing.T) {
	store, _ := newTestCostEstimateShareStore(t)
	for _, id := range []string{"", "1", "../../user", "1234567890123456789012345678901/"} {
		if _, err := store.Get(context.Background(), id); !errors.Is(err, ErrCostEstimateShareNotFound) {
			t.Fatalf("id %q = %v", id, err)
		}
	}
	if _, err := store.Create(context.Background(), 0, CostEstimateShareResult{}); err == nil {
		t.Fatal("anonymous creator accepted")
	}
	if _, err := NewCostEstimateShareRedisModel(nil).Create(context.Background(), 42, CostEstimateShareResult{}); err == nil {
		t.Fatal("unavailable Redis accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Create(ctx, 42, CostEstimateShareResult{}); err == nil {
		t.Fatal("cancelled create succeeded")
	}
}

func TestCostEstimateShareDoesNotReuseOrModifyOtherSnapshot(t *testing.T) {
	store, _ := newTestCostEstimateShareStore(t)
	first, err := store.Create(context.Background(), 42, CostEstimateShareResult{Version: 4, EstimatedTotalCost: "80.00万元"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(context.Background(), 42, CostEstimateShareResult{Version: 4, EstimatedTotalCost: "90.00万元"})
	if err != nil || first.ReportID == second.ReportID {
		t.Fatalf("independent share failed: %+v, %v", second, err)
	}
	got, err := store.Get(context.Background(), first.ReportID)
	if err != nil || got.Result.EstimatedTotalCost != "80.00万元" {
		t.Fatalf("old snapshot changed: %+v, %v", got, err)
	}
}

func TestCostEstimateShareCreationLimitIsSharedAcrossInstances(t *testing.T) {
	first, mr := newTestCostEstimateShareStore(t)
	second := NewCostEstimateShareRedisModel(first.Redis)
	for i := 0; i < 10; i++ {
		store := []*CostEstimateShareRedisModel{first, second}[i%2]
		if _, err := store.Create(context.Background(), 42, CostEstimateShareResult{Version: 4}); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := second.Create(context.Background(), 42, CostEstimateShareResult{Version: 4}); !errors.Is(err, ErrCostEstimateShareRateLimited) {
		t.Fatal("a new application instance bypassed the creation limit", err)
	}
	if _, err := second.Create(context.Background(), 43, CostEstimateShareResult{Version: 4}); err != nil {
		t.Fatal("unrelated owner was blocked", err)
	}
	mr.FastForward(time.Minute)
	if _, err := first.Create(context.Background(), 42, CostEstimateShareResult{Version: 4}); err != nil {
		t.Fatal("creation window did not expire", err)
	}
}

func TestCostEstimateShareActiveQuotaAndRevokeReleaseAreShared(t *testing.T) {
	first, mr := newTestCostEstimateShareStore(t)
	second := NewCostEstimateShareRedisModel(first.Redis)
	var created *CostEstimateShareSnapshot
	for i := 0; i < 100; i++ {
		if i%10 == 0 {
			mr.FastForward(time.Minute)
		}
		var err error
		created, err = first.Create(context.Background(), 42, CostEstimateShareResult{Version: 4})
		if err != nil {
			t.Fatal(i, err)
		}
	}
	mr.FastForward(time.Minute)
	if _, err := second.Create(context.Background(), 42, CostEstimateShareResult{Version: 4}); !errors.Is(err, ErrCostEstimateShareQuotaExceeded) {
		t.Fatal("active reports can grow without an owner quota", err)
	}
	if err := second.Revoke(context.Background(), created.ReportID, 43); !errors.Is(err, ErrCostEstimateShareNotFound) {
		t.Fatal("foreign owner revoked a snapshot", err)
	}
	if _, err := second.Create(context.Background(), 42, CostEstimateShareResult{Version: 4}); err == nil {
		t.Fatal("foreign revoke released the quota")
	}
	if err := second.Revoke(context.Background(), created.ReportID, 42); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Create(context.Background(), 42, CostEstimateShareResult{Version: 4}); err != nil {
		t.Fatal("owner revoke did not release the quota", err)
	}
	snapshots := 0
	for _, key := range mr.Keys() {
		if validCostEstimateShareID(strings.TrimPrefix(key, "cost_estimate:share:")) {
			snapshots++
		}
	}
	if snapshots != 100 {
		t.Fatalf("stored snapshots = %d, want 100", snapshots)
	}
	mr.FastForward(CostEstimateShareTTL)
	if _, err := second.Create(context.Background(), 42, CostEstimateShareResult{Version: 4}); err != nil {
		t.Fatal("expired snapshots did not release the quota", err)
	}
}

func TestCostEstimateSharePrunesExpiredOwnerEntries(t *testing.T) {
	store, _ := newTestCostEstimateShareStore(t)
	ctx := context.Background()
	for i := 0; i < CostEstimateShareMaxActive; i++ {
		if err := store.Redis.ZAdd(ctx, costEstimateShareOwnerKey(42), redis.Z{Score: 1, Member: fmt.Sprintf("expired-%d", i)}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Create(ctx, 42, CostEstimateShareResult{Version: 4}); err != nil {
		t.Fatal("expired index members blocked a fresh report", err)
	}
	if count, err := store.Redis.ZCard(ctx, costEstimateShareOwnerKey(42)).Result(); err != nil || count != 1 {
		t.Fatalf("active index count = %d, %v", count, err)
	}
}

func TestCostEstimateShareConcurrentQuotaCannotCreateUnindexedReports(t *testing.T) {
	store, mr := newTestCostEstimateShareStore(t)
	ctx := context.Background()
	for i := 0; i < CostEstimateShareMaxActive-1; i++ {
		if i%10 == 0 {
			mr.FastForward(time.Minute)
		}
		if _, err := store.Create(ctx, 42, CostEstimateShareResult{Version: 4}); err != nil {
			t.Fatal(err)
		}
	}
	mr.FastForward(time.Minute)
	results := make(chan error, 10)
	var work sync.WaitGroup
	for i := 0; i < 10; i++ {
		work.Add(1)
		go func() {
			defer work.Done()
			_, err := NewCostEstimateShareRedisModel(store.Redis).Create(ctx, 42, CostEstimateShareResult{Version: 4})
			results <- err
		}()
	}
	work.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, ErrCostEstimateShareQuotaExceeded) {
			t.Fatal(err)
		}
	}
	if created != 1 {
		t.Fatalf("created %d reports for one remaining slot", created)
	}
	snapshots := 0
	for _, key := range mr.Keys() {
		if validCostEstimateShareID(strings.TrimPrefix(key, "cost_estimate:share:")) {
			snapshots++
		}
	}
	if count, err := store.Redis.ZCard(ctx, costEstimateShareOwnerKey(42)).Result(); err != nil || count != 100 || snapshots != 100 {
		t.Fatalf("owner index=%d, stored snapshots=%d, err=%v", count, snapshots, err)
	}
}

func TestCostEstimateShareConcurrentInstancesDoNotExceedCreationLimit(t *testing.T) {
	first, _ := newTestCostEstimateShareStore(t)
	second := NewCostEstimateShareRedisModel(first.Redis)
	results := make(chan error, 30)
	var work sync.WaitGroup
	for i := 0; i < 30; i++ {
		work.Add(1)
		go func(index int) {
			defer work.Done()
			_, err := []*CostEstimateShareRedisModel{first, second}[index%2].Create(context.Background(), 42, CostEstimateShareResult{Version: 4})
			results <- err
		}(i)
	}
	work.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		}
	}
	if created != 10 {
		t.Fatalf("concurrent creations = %d, want 10", created)
	}
}
