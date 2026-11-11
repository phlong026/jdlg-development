package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type aiPricingCacheConnector struct {
	queries int
}

func (c *aiPricingCacheConnector) Connect(context.Context) (driver.Conn, error) { return c, nil }
func (c *aiPricingCacheConnector) Driver() driver.Driver                        { return c }
func (c *aiPricingCacheConnector) Open(string) (driver.Conn, error)             { return c, nil }
func (c *aiPricingCacheConnector) Prepare(string) (driver.Stmt, error)          { return nil, driver.ErrSkip }
func (c *aiPricingCacheConnector) Close() error                                 { return nil }
func (c *aiPricingCacheConnector) Begin() (driver.Tx, error)                    { return nil, driver.ErrSkip }
func (c *aiPricingCacheConnector) QueryContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Rows, error) {
	c.queries++
	scene := ""
	if len(args) > 0 {
		scene, _ = args[0].Value.(string)
	}
	return &aiPricingCacheRows{
		columns: []string{"id", "scene", "stones", "extra_config", "created_at", "updated_at"},
		values: [][]driver.Value{{
			int64(7), scene, int64(30), `{"duration_min":5,"duration_max":12,"provider":"private"}`,
			time.Unix(100, 0), time.Unix(200, 0),
		}},
	}, nil
}

type aiPricingCacheRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *aiPricingCacheRows) Columns() []string { return r.columns }
func (r *aiPricingCacheRows) Close() error      { return nil }
func (r *aiPricingCacheRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func TestAIPricingCachePreservesExtraConfigAcrossColdAndHotReads(t *testing.T) {
	connector := &aiPricingCacheConnector{}
	db := sql.OpenDB(connector)
	defer db.Close()
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer redisClient.Close()
	pricing := NewAIPricingModel(db, redisClient)

	cold, err := pricing.GetByScene("ai_video_1")
	if err != nil {
		t.Fatal(err)
	}
	hot, err := pricing.GetByScene("ai_video_1")
	if err != nil {
		t.Fatal(err)
	}
	if connector.queries != 1 {
		t.Fatalf("database queries = %d, want 1 cold read followed by cache hit", connector.queries)
	}
	for label, item := range map[string]*AIPricing{"cold": cold, "hot": hot} {
		if got := item.GetExtraConfig(); got != `{"duration_min":5,"duration_max":12,"provider":"private"}` {
			t.Fatalf("%s extra config = %q", label, got)
		}
	}
}

func TestAIPricingLegacyCacheWithoutExtraConfigSelfHealsFromDatabase(t *testing.T) {
	connector := &aiPricingCacheConnector{}
	db := sql.OpenDB(connector)
	defer db.Close()
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer redisClient.Close()
	pricing := NewAIPricingModel(db, redisClient)
	if err := redisClient.Set(context.Background(), "ai_pricing:scene:ai_video_1", `{"id":7,"scene":"ai_video_1","stones":30}`, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}

	item, err := pricing.GetByScene("ai_video_1")
	if err != nil {
		t.Fatal(err)
	}
	if connector.queries != 1 || item.GetExtraConfig() == "" {
		t.Fatalf("legacy cache was not healed queries=%d extra=%q", connector.queries, item.GetExtraConfig())
	}
	second, err := pricing.GetByScene("ai_video_1")
	if err != nil {
		t.Fatal(err)
	}
	if connector.queries != 1 || second.GetExtraConfig() == "" {
		t.Fatalf("healed cache missed queries=%d extra=%q", connector.queries, second.GetExtraConfig())
	}
}
