package model

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const CostEstimateShareTTL = 30 * 24 * time.Hour
const CostEstimateShareMaxActive = 100
const costEstimateShareCreateLimit = 10

var ErrCostEstimateShareNotFound = errors.New("cost estimate share not found")
var ErrCostEstimateShareRateLimited = errors.New("cost estimate share creation rate exceeded")
var ErrCostEstimateShareQuotaExceeded = errors.New("cost estimate share active quota exceeded")

// Both limits live with the snapshots so another process cannot reset them.
// Every owner index is bounded, and its TTL follows the newest snapshot.
const createCostEstimateShareScript = `
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
if redis.call('ZCARD', KEYS[2]) >= tonumber(ARGV[4]) then return -1 end
local count = tonumber(redis.call('GET', KEYS[3]) or '0')
if count >= tonumber(ARGV[5]) then return -2 end
if not redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2], 'NX') then return 0 end
redis.call('ZADD', KEYS[2], now + tonumber(ARGV[2]) * 1000, ARGV[3])
redis.call('EXPIRE', KEYS[2], ARGV[2])
redis.call('INCR', KEYS[3])
if count == 0 then redis.call('EXPIRE', KEYS[3], 60) end
return 1
`

const revokeCostEstimateShareScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
redis.call('ZREM', KEYS[2], ARGV[1])
return redis.call('DEL', KEYS[1])
`

type CostEstimateBasicInfo struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	FullWidth bool   `json:"fullWidth,omitempty"`
}

type CostEstimateBreakdown struct {
	Name       string  `json:"name"`
	Amount     float64 `json:"amount"`
	AmountText string  `json:"amountText"`
	Percent    float64 `json:"percent"`
	Note       string  `json:"note"`
	Estimable  bool    `json:"estimable"`
}

type RenovationCostItem struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Unit              string   `json:"unit"`
	Quantity          *float64 `json:"quantity"`
	MaterialUnitPrice *float64 `json:"materialUnitPrice"`
	LaborAuxUnitPrice *float64 `json:"laborAuxUnitPrice"`
	Amount            *float64 `json:"amount"`
	Included          *bool    `json:"included"`
}

type RenovationCostGroup struct {
	ID     string               `json:"id"`
	Name   string               `json:"name"`
	Amount *float64             `json:"amount"`
	Items  []RenovationCostItem `json:"items"`
}

type RenovationCostSpace struct {
	ID     string                `json:"id"`
	Name   string                `json:"name"`
	Count  *int                  `json:"count"`
	Amount *float64              `json:"amount"`
	Groups []RenovationCostGroup `json:"groups"`
}

type RenovationCostEstimate struct {
	PricingVersion string                `json:"pricingVersion"`
	QuantityBasis  string                `json:"quantityBasis,omitempty"`
	TotalAmount    *float64              `json:"totalAmount"`
	Spaces         []RenovationCostSpace `json:"spaces"`
}

// Only the reference estimate's display fields can enter a public snapshot.
type CostEstimateShareResult struct {
	Version            int                     `json:"version"`
	QuoteType          string                  `json:"quoteType,omitempty"`
	EstimatedTotalCost string                  `json:"estimatedTotalCost"`
	EstimatedUnitCost  string                  `json:"estimatedUnitCost"`
	TotalLabel         string                  `json:"totalLabel"`
	UnitLabel          string                  `json:"unitLabel"`
	BasicInfo          []CostEstimateBasicInfo `json:"basicInfo"`
	CostBreakdown      []CostEstimateBreakdown `json:"costBreakdown"`
	DynamicNotes       []string                `json:"dynamicNotes"`
	Renovation         *RenovationCostEstimate `json:"renovation,omitempty"`
}

type CostEstimateShareSnapshot struct {
	ReportID  string                  `json:"report_id"`
	OwnerID   int64                   `json:"owner_id"`
	ExpiresAt time.Time               `json:"expires_at"`
	Result    CostEstimateShareResult `json:"result"`
}

type CostEstimateShareRedisModel struct {
	Redis *redis.Client
}

func NewCostEstimateShareRedisModel(client *redis.Client) *CostEstimateShareRedisModel {
	return &CostEstimateShareRedisModel{Redis: client}
}

func costEstimateShareKey(id string) string { return "cost_estimate:share:" + id }

func costEstimateShareOwnerKey(ownerID int64) string {
	return fmt.Sprintf("cost_estimate:share:owner:%d", ownerID)
}

func validCostEstimateShareID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, char := range id {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func (m *CostEstimateShareRedisModel) Create(ctx context.Context, ownerID int64, result CostEstimateShareResult) (*CostEstimateShareSnapshot, error) {
	if ownerID <= 0 {
		return nil, errors.New("cost estimate share creator is required")
	}
	if m == nil || m.Redis == nil {
		return nil, errors.New("cost estimate share store is unavailable")
	}
	for attempt := 0; attempt < 3; attempt++ {
		var random [24]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, fmt.Errorf("create cost estimate share id: %w", err)
		}
		snapshot := &CostEstimateShareSnapshot{
			ReportID: base64.RawURLEncoding.EncodeToString(random[:]), OwnerID: ownerID,
			ExpiresAt: time.Now().UTC().Add(CostEstimateShareTTL), Result: result,
		}
		payload, err := json.Marshal(snapshot)
		if err != nil {
			return nil, fmt.Errorf("encode cost estimate share: %w", err)
		}
		ownerKey := costEstimateShareOwnerKey(ownerID)
		created, err := m.Redis.Eval(ctx, createCostEstimateShareScript,
			[]string{costEstimateShareKey(snapshot.ReportID), ownerKey, ownerKey + ":rate"},
			payload, int64(CostEstimateShareTTL/time.Second), snapshot.ReportID,
			CostEstimateShareMaxActive, costEstimateShareCreateLimit).Int64()
		if err != nil {
			return nil, fmt.Errorf("save cost estimate share: %w", err)
		}
		switch created {
		case -1:
			return nil, ErrCostEstimateShareQuotaExceeded
		case -2:
			return nil, ErrCostEstimateShareRateLimited
		case 1:
			return snapshot, nil
		}
	}
	return nil, errors.New("cost estimate share id collision")
}

func (m *CostEstimateShareRedisModel) Get(ctx context.Context, id string) (*CostEstimateShareSnapshot, error) {
	if !validCostEstimateShareID(id) {
		return nil, ErrCostEstimateShareNotFound
	}
	if m == nil || m.Redis == nil {
		return nil, errors.New("cost estimate share store is unavailable")
	}
	payload, err := m.Redis.Get(ctx, costEstimateShareKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCostEstimateShareNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read cost estimate share: %w", err)
	}
	var snapshot CostEstimateShareSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil, fmt.Errorf("decode cost estimate share: %w", err)
	}
	if snapshot.ReportID != id || snapshot.OwnerID <= 0 || !time.Now().Before(snapshot.ExpiresAt) {
		return nil, ErrCostEstimateShareNotFound
	}
	return &snapshot, nil
}

func (m *CostEstimateShareRedisModel) Revoke(ctx context.Context, id string, ownerID int64) error {
	snapshot, err := m.Get(ctx, id)
	if err != nil {
		return err
	}
	if ownerID <= 0 || snapshot.OwnerID != ownerID {
		return ErrCostEstimateShareNotFound
	}
	// IDs are never overwritten or reused: a concurrent expiry/revoke cannot
	// cause this owner check to delete somebody else's report.
	deleted, err := m.Redis.Eval(ctx, revokeCostEstimateShareScript,
		[]string{costEstimateShareKey(id), costEstimateShareOwnerKey(ownerID)}, id).Int64()
	if err != nil {
		return fmt.Errorf("revoke cost estimate share: %w", err)
	}
	if deleted == 0 {
		return ErrCostEstimateShareNotFound
	}
	return nil
}
