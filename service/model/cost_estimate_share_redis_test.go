package model

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
