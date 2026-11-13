package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func executionTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("AI_VIDEO_STUDIO_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable AI_VIDEO_STUDIO_TEST_DSN")
	}
	c, e := mysql.ParseDSN(dsn)
	if e != nil || c.Net != "tcp" || !strings.HasPrefix(c.Addr, "127.0.0.1:") || !strings.HasPrefix(c.DBName, "jdlg_studio_test") {
		t.Fatal("unsafe fixture DSN")
	}
	db, e := sql.Open("mysql", dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func TestStudioPaidAtomicLifecycle(t *testing.T) {
	db := executionTestDB(t)
	ctx := context.Background()
	s := NewAIVideoStudioStore(db)
	for _, f := range []func() error{s.InitTables, s.InitExecutionTables, NewAIVideoStudioMediaStore(db).InitTable, NewStoneRecordModel(db).InitTable} {
		if e := f(); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := db.Exec("CREATE TABLE IF NOT EXISTS users(id BIGINT PRIMARY KEY,stones BIGINT UNSIGNED NOT NULL)"); e != nil {
		t.Fatal(e)
	}
	uid := int64(820001)
	db.Exec("DELETE FROM stone_records WHERE user_id=?", uid)
	for _, table := range []string{"ai_video_studio_tasks", "ai_video_studio_fees", "ai_video_studio_quote_consumptions", "ai_video_studio_batches", "ai_video_studio_quotes", "ai_video_studio_drafts"} {
		if _, e := db.Exec("DELETE FROM "+table+" WHERE user_id=?", uid); e != nil {
			t.Fatal(e)
		}
	}
	db.Exec("REPLACE INTO users(id,stones) VALUES (?,100)", uid)
	price := int64(1)
	prices, _ := json.Marshal([]AIVideoStudioPrice{{Code: "ai_video_studio_768p", BillingUnit: "per_request", Enabled: true, UnitStones: &price}})
	if _, e := db.Exec("UPDATE ai_video_studio_config SET enabled=1,modes_json=?,prices_json=?,version=99 WHERE id=1", `{"text":true}`, string(prices)); e != nil {
		t.Fatal(e)
	}
	defer db.Exec("UPDATE ai_video_studio_config SET enabled=0,prices_json='[]',version=1 WHERE id=1")
	draft := AIVideoStudioDraft{UserID: uid, Name: "paid", Mode: "text", Prompt: "courtyard", References: json.RawMessage(`[]`), Ratio: "16:9", Resolution: "768p", Duration: 4, Quantity: 3}
	if e := s.CreateDraft(ctx, &draft); e != nil {
		t.Fatal(e)
	}
	quote := func() string {
		snapshot, _ := json.Marshal(draft)
		q := AIVideoStudioQuote{ID: uuid.NewString(), UserID: uid, DraftID: draft.ID, DraftRevision: draft.Revision, QuoteVersion: 99, PriceCode: "ai_video_studio_768p", BillingUnit: "per_request", UnitStones: 1, BillableUnits: 1, TotalStones: 1, InputSnapshot: snapshot, ExpiresAt: nowPlusHour()}
		if e := s.CreateQuote(ctx, q); e != nil {
			t.Fatal(e)
		}
		return q.ID
	}
	q := quote()
	// Existing unexpired quotes keep their billing unit and price across config edits.
	if _, e := db.Exec(`UPDATE ai_video_studio_config SET prices_json=?,version=100 WHERE id=1`, `[{"code":"ai_video_studio_768p","enabled":true,"unit_stones":999,"billing_unit":"per_second"}]`); e != nil {
		t.Fatal(e)
	}
	id, replay, e := s.CreatePaidBatch(ctx, uid, q, "first")
	if e != nil || replay {
		t.Fatalf("create %s %v %v", id, replay, e)
	}
	if _, replay, e = s.CreatePaidBatch(ctx, uid, q, "first"); e != nil || !replay {
		t.Fatalf("replay %v %v", replay, e)
	}
	if _, _, e = s.CreatePaidBatch(ctx, uid, q, "changed"); e == nil {
		t.Fatal("same quote charged twice")
	}
	if _, _, e = s.CreatePaidBatch(ctx, uid, quote(), "first"); e == nil {
		t.Fatal("same key accepted different quote")
	}
	rows, e := db.Query("SELECT id FROM ai_video_studio_tasks WHERE batch_id=? ORDER BY item_index", id)
	if e != nil {
		t.Fatal(e)
	}
	var ids []string
	for rows.Next() {
		var v string
		rows.Scan(&v)
		ids = append(ids, v)
	}
	rows.Close()
	if len(ids) != 3 {
		t.Fatal(ids)
	}
	if e = s.FinalizeTask(ctx, ids[0], StudioTaskOutcome{Status: "failed"}); e != nil {
		t.Fatal(e)
	}
	if e = s.FinalizeTask(ctx, ids[0], StudioTaskOutcome{Status: "failed"}); e != nil {
		t.Fatal(e)
	}
	if e = s.FinalizeTask(ctx, ids[1], StudioTaskOutcome{Status: "completed", ResultURL: "https://example.com/result.mp4", DeliveryType: "DIRECT"}); e != nil {
		t.Fatal(e)
	}
	if e = s.CancelQueued(ctx, uid, id); e != nil {
		t.Fatal(e)
	}
	m := NewUserRedisModel(db, nil)
	if n, _ := m.GetStones(uid); n != 100 {
		t.Fatalf("expected full single-stone refund: %d", n)
	}

	// A failed insert rolls back wallet, quote consumption, batch and tasks together.
	rollbackQuote := quote()
	db.Exec("DROP TRIGGER IF EXISTS studio_fail_reserve")
	if _, e = db.Exec("CREATE TRIGGER studio_fail_reserve BEFORE INSERT ON stone_records FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture reserve failure'"); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.CreatePaidBatch(ctx, uid, rollbackQuote, "rollback"); e == nil {
		t.Fatal("failed ledger committed batch")
	}
	db.Exec("DROP TRIGGER IF EXISTS studio_fail_reserve")
	if n, _ := m.GetStones(uid); n != 100 {
		t.Fatalf("rollback deducted wallet %d", n)
	}
	var claims int
	if e = db.QueryRow("SELECT COUNT(*) FROM ai_video_studio_quote_consumptions WHERE quote_id=?", rollbackQuote).Scan(&claims); e != nil || claims != 0 {
		t.Fatalf("rollback consumed quote %d %v", claims, e)
	}
	recoveryID, _, e := s.CreatePaidBatch(ctx, uid, rollbackQuote, "rollback")
	if e != nil {
		t.Fatal(e)
	}
	var recoveryTask string
	if e = db.QueryRow("SELECT id FROM ai_video_studio_tasks WHERE batch_id=? ORDER BY item_index LIMIT 1", recoveryID).Scan(&recoveryTask); e != nil {
		t.Fatal(e)
	}
	db.Exec("DROP TRIGGER IF EXISTS studio_fail_settle")
	if _, e = db.Exec("CREATE TRIGGER studio_fail_settle BEFORE UPDATE ON ai_video_studio_fees FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture settlement failure'"); e != nil {
		t.Fatal(e)
	}
	result := StudioTaskOutcome{Status: "completed", ResultURL: "https://example.com/durable.mp4", DeliveryType: "DIRECT"}
	if e = s.FinalizeTask(ctx, recoveryTask, result); e == nil {
		t.Fatal("settlement fixture failure ignored")
	}
	db.Exec("DROP TRIGGER IF EXISTS studio_fail_settle")
	var durable string
	if e = db.QueryRow("SELECT result_url FROM ai_video_studio_tasks WHERE id=?", recoveryTask).Scan(&durable); e != nil || durable != result.ResultURL {
		t.Fatalf("lost direct result %q %v", durable, e)
	}
	restarted := NewAIVideoStudioStore(db)
	if e = restarted.FinalizeTask(ctx, recoveryTask, result); e != nil {
		t.Fatal(e)
	}
	if e = restarted.FinalizeTask(ctx, recoveryTask, result); e != nil {
		t.Fatal(e)
	}
	if e = restarted.CancelQueued(ctx, uid, recoveryID); e != nil {
		t.Fatal(e)
	}
	if n, _ := m.GetStones(uid); n != 99 {
		t.Fatalf("settlement deducted twice %d", n)
	}
	// Concurrent wallet consumers and same-key create requests share a single SQL authority.
	concurrentQuote := quote()
	var creators atomic.Int32
	var createWG sync.WaitGroup
	for i := 0; i < 10; i++ {
		createWG.Add(1)
		go func() {
			defer createWG.Done()
			_, replay, err := s.CreatePaidBatch(ctx, uid, concurrentQuote, "concurrent")
			if err != nil {
				t.Errorf("concurrent create: %v", err)
			} else if !replay {
				creators.Add(1)
			}
		}()
	}
	for i := 0; i < 10; i++ {
		createWG.Add(1)
		go func() {
			defer createWG.Done()
			if err := m.DeductStones(uid, 1); err != nil {
				t.Errorf("legacy wallet: %v", err)
			}
		}()
	}
	createWG.Wait()
	if creators.Load() != 1 {
		t.Fatalf("created %d batches", creators.Load())
	}
	if n, _ := m.GetStones(uid); n != 88 {
		t.Fatalf("concurrent balance %d", n)
	}
	db.Exec("UPDATE users SET stones=0 WHERE id=?", uid)
	insufficientQuote := quote()
	if _, _, e = s.CreatePaidBatch(ctx, uid, insufficientQuote, "insufficient"); !errors.Is(e, ErrInsufficientStones) {
		t.Fatalf("insufficient %v", e)
	}
	if e = db.QueryRow("SELECT COUNT(*) FROM ai_video_studio_quote_consumptions WHERE quote_id=?", insufficientQuote).Scan(&claims); e != nil || claims != 0 {
		t.Fatalf("insufficient consumed quote %d %v", claims, e)
	}

	db.Exec("UPDATE users SET stones=1 WHERE id=?", uid)
	independentQuotes := make([]string, 10)
	for i := range independentQuotes {
		independentQuotes[i] = quote()
	}
	var funded atomic.Int32
	var insufficient atomic.Int32
	for _, qid := range independentQuotes {
		createWG.Add(1)
		go func(qid string) {
			defer createWG.Done()
			_, _, err := s.CreatePaidBatch(ctx, uid, qid, qid)
			if err == nil {
				funded.Add(1)
			} else if errors.Is(err, ErrInsufficientStones) {
				insufficient.Add(1)
			} else {
				t.Errorf("independent create: %v", err)
			}
		}(qid)
	}
	createWG.Wait()
	if funded.Load() != 1 || insufficient.Load() != 9 {
		t.Fatalf("overfunded concurrent requests %d/%d", funded.Load(), insufficient.Load())
	}
	disabledQuote := quote()
	if _, e = db.Exec(`UPDATE ai_video_studio_config SET prices_json=? WHERE id=1`, `[{"code":"ai_video_studio_768p","enabled":false,"unit_stones":999,"billing_unit":"per_second"}]`); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.CreatePaidBatch(ctx, uid, disabledQuote, "disabled"); e == nil {
		t.Fatal("disabled price accepted")
	}
	if _, replay, e = s.CreatePaidBatch(ctx, uid, q, "first"); e != nil || !replay {
		t.Fatalf("replay rejected after disable %v %v", replay, e)
	}
	db.Exec("UPDATE users SET stones=10 WHERE id=?", uid)
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m.DeductStones(uid, 1) == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 10 {
		t.Fatal(success.Load())
	}
	if e = m.AddStones(uid, math.MaxInt64); e != nil {
		t.Fatal(e)
	}
	if e = m.AddStones(uid, 1); e == nil {
		t.Fatal("overflow accepted")
	}
	if e = m.DeductStones(uid, -1); e == nil {
		t.Fatal("negative deduction")
	}
	if e = m.SetStones(uid, -1); e == nil {
		t.Fatal("negative set")
	}
}

func nowPlusHour() time.Time { return time.Now().Add(time.Hour) }

func TestWalletAdminSetLedgerRollback(t *testing.T) {
	db := executionTestDB(t)
	m := NewUserRedisModel(db, nil)
	uid := int64(820009)
	if _, e := db.Exec("CREATE TABLE IF NOT EXISTS users(id BIGINT PRIMARY KEY,stones BIGINT UNSIGNED NOT NULL)"); e != nil {
		t.Fatal(e)
	}
	if e := NewStoneRecordModel(db).InitTable(); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec("REPLACE INTO users(id,stones) VALUES(?,100)", uid); e != nil {
		t.Fatal(e)
	}
	db.Exec("DROP TRIGGER IF EXISTS studio_wallet_fail_record")
	if _, e := db.Exec("CREATE TRIGGER studio_wallet_fail_record BEFORE INSERT ON stone_records FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture ledger failure'"); e != nil {
		t.Fatal(e)
	}
	defer db.Exec("DROP TRIGGER IF EXISTS studio_wallet_fail_record")

	if _, _, e := m.AdjustStonesWithRecord(uid, 50, "fixture"); e == nil {
		t.Fatal("adjust ignored ledger failure")
	}
	if _, e := m.SetStonesWithRecord(uid, 200, "fixture"); e == nil {
		t.Fatal("ledger failure ignored")
	}
	if e := m.DeductStones(uid, 30); e != nil {
		t.Fatal(e)
	}
	if got, e := m.GetStones(uid); e != nil || got != 70 {
		t.Fatalf("failed Set or compensation overwrote valid deduction: %d %v", got, e)
	}
	db.Exec("DROP TRIGGER IF EXISTS studio_wallet_fail_record")

	if e := m.DeductStones(uid, 0); e != nil {
		t.Fatal(e)
	}
	if e := m.AddStones(uid, 0); e != nil {
		t.Fatal(e)
	}
	if e := m.AddStones(-1, 0); e == nil {
		t.Fatal("free operation accepted nonexistent wallet")
	}
	old, e := m.SetStonesWithRecord(uid, 200, "fixture")
	if e != nil || old != 70 {
		t.Fatalf("actual serialized delta: %d %v", old, e)
	}
	var amount int64
	if e = db.QueryRow("SELECT amount FROM stone_records WHERE user_id=? ORDER BY id DESC LIMIT 1", uid).Scan(&amount); e != nil || amount != 130 {
		t.Fatalf("ledger delta %d %v", amount, e)
	}
}
