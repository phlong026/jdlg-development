package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAIVideoStudioPolishAtomicLifecycle(t *testing.T) {
	db := executionTestDB(t)
	ctx := context.Background()
	store := NewAIVideoStudioStore(db)
	for _, init := range []func() error{store.InitTables, store.InitExecutionTables, NewStoneRecordModel(db).InitTable} {
		if err := init(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users(id BIGINT PRIMARY KEY,stones BIGINT UNSIGNED NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	uid := int64(930001)
	if _, err := db.Exec(`REPLACE INTO users(id,stones) VALUES(?,100)`, uid); err != nil {
		t.Fatal(err)
	}
	unit := int64(7)
	prices, _ := json.Marshal([]AIVideoStudioPrice{{Code: "ai_video_studio_prompt_optimize", BillingUnit: "per_request", UnitStones: &unit, Enabled: true, Version: 9}})
	polish, _ := json.Marshal(AIVideoStudioPolishConfig{Enabled: true, Model: "fixture-model", TimeoutSeconds: 45})
	if _, err := db.Exec(`UPDATE ai_video_studio_config SET enabled=1,modes_json='{"text":true}',prices_json=?,prompt_optimize_json=?,version=9 WHERE id=1`, prices, polish); err != nil {
		t.Fatal(err)
	}

	draft := AIVideoStudioDraft{UserID: uid, Name: "润色", Mode: "reference", ReferencePrompt: "参考图片1的庭院布局", References: json.RawMessage(`[{"purpose":"庭院布局","position":1,"media_type":"image","media_id":"fixture-image"}]`), Ratio: "16:9", Resolution: "768p", Duration: 4, Quantity: 1}
	if err := store.CreateDraft(ctx, &draft); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := json.Marshal(draft)
	hash, _ := AIVideoStudioSnapshotHash(snapshot)
	newQuote := func() string {
		id := uuid.NewString()
		q := AIVideoStudioQuote{ID: id, UserID: uid, DraftID: draft.ID, DraftRevision: draft.Revision, QuoteVersion: 9, InputHash: hash, PriceCode: "ai_video_studio_prompt_optimize", BillingUnit: "per_request", UnitStones: unit, BillableUnits: 1, TotalStones: unit, InputSnapshot: snapshot, ExpiresAt: time.Now().Add(time.Hour)}
		if err := store.CreateQuote(ctx, q); err != nil {
			t.Fatal(err)
		}
		return id
	}

	rollbackQuote := newQuote()
	if _, err := db.Exec(`CREATE TRIGGER polish_fail_reserve BEFORE INSERT ON stone_records FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture reserve failure'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreatePolishTask(ctx, uid, rollbackQuote, "rollback"); err == nil {
		t.Fatal("ledger failure committed polish task")
	}
	if _, err := db.Exec(`DROP TRIGGER polish_fail_reserve`); err != nil {
		t.Fatal(err)
	}
	if balance, _ := NewUserRedisModel(db, nil).GetStones(uid); balance != 100 {
		t.Fatalf("rollback balance=%d", balance)
	}
	var rollbackTasks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_prompt_tasks WHERE quote_id=?`, rollbackQuote).Scan(&rollbackTasks); err != nil || rollbackTasks != 0 {
		t.Fatalf("rollback tasks=%d err=%v", rollbackTasks, err)
	}

	quoteID := newQuote()
	var creators atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, replay, err := store.CreatePolishTask(ctx, uid, quoteID, "same-request")
			if err != nil {
				t.Errorf("create: %v", err)
				return
			}
			if task.TaskID == "" {
				t.Error("missing task id")
			}
			if !replay {
				creators.Add(1)
			}
		}()
	}
	wg.Wait()
	if creators.Load() != 1 {
		t.Fatalf("created %d tasks", creators.Load())
	}
	if balance, _ := NewUserRedisModel(db, nil).GetStones(uid); balance != 93 {
		t.Fatalf("balance=%d", balance)
	}
	task, err := store.GetPolishSubmission(ctx, uid, "same-request")
	if err != nil || task.Status != "queued" || task.FeeStatus != "reserved" {
		t.Fatalf("task=%+v err=%v", task, err)
	}
	if adminTask, adminErr := store.GetPolishTask(ctx, 0, task.TaskID); adminErr != nil || adminTask.UserID != uid {
		t.Fatalf("admin task=%+v err=%v", adminTask, adminErr)
	}
	if _, crossErr := store.GetPolishTask(ctx, uid+1, task.TaskID); !errors.Is(crossErr, sql.ErrNoRows) {
		t.Fatalf("cross account task: %v", crossErr)
	}
	if _, invalidErr := store.GetPolishTask(ctx, -1, task.TaskID); !errors.Is(invalidErr, ErrAIVideoStudioConflict) {
		t.Fatalf("negative account task: %v", invalidErr)
	}
	if _, _, err = store.CreatePolishTask(ctx, uid, newQuote(), "same-request"); !errors.Is(err, ErrAIVideoStudioConflict) {
		t.Fatalf("changed quote: %v", err)
	}
	if _, _, err = store.CreatePolishTask(ctx, uid, quoteID, "changed-request"); !errors.Is(err, ErrAIVideoStudioConflict) {
		t.Fatalf("same quote reused: %v", err)
	}

	claimed, err := store.ClaimPolishTask(ctx)
	if err != nil || claimed == nil || claimed.TaskID != task.TaskID || claimed.Status != "submitting" {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	result := "庭院镜头平稳向前推进，保持结构稳定。"
	if _, err = db.Exec(`CREATE TRIGGER polish_fail_settle BEFORE INSERT ON ai_video_studio_prompt_fees FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture settlement failure'`); err != nil {
		t.Fatal(err)
	}
	if err = store.SavePolishResult(ctx, task.TaskID, claimed.LeaseOwner, result); err == nil {
		t.Fatal("settlement failure ignored")
	}
	if _, err = db.Exec(`DROP TRIGGER polish_fail_settle`); err != nil {
		t.Fatal(err)
	}
	var durable string
	if err = db.QueryRow(`SELECT optimized_prompt FROM ai_video_studio_prompt_tasks WHERE id=?`, task.TaskID).Scan(&durable); err != nil || durable != result {
		t.Fatalf("durable=%q err=%v", durable, err)
	}
	if err = store.RecoverPolishTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if err = store.SavePolishResult(ctx, task.TaskID, claimed.LeaseOwner, result); err != nil {
		t.Fatal(err)
	}
	task, err = store.GetPolishTask(ctx, uid, task.TaskID)
	if err != nil || task.Status != "completed" || task.FeeStatus != "settled" || task.OptimizedPrompt == "" {
		t.Fatalf("completed=%+v err=%v", task, err)
	}
	if balance, _ := NewUserRedisModel(db, nil).GetStones(uid); balance != 93 {
		t.Fatalf("settled charged twice: %d", balance)
	}

	cancelQuote := newQuote()
	cancelTask, _, err := store.CreatePolishTask(ctx, uid, cancelQuote, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CancelPolishTask(ctx, uid, cancelTask.TaskID); err != nil {
		t.Fatal(err)
	}
	if err = store.CancelPolishTask(ctx, uid, cancelTask.TaskID); err != nil {
		t.Fatal(err)
	}
	if balance, _ := NewUserRedisModel(db, nil).GetStones(uid); balance != 93 {
		t.Fatalf("cancel refund=%d", balance)
	}
}

func TestAIVideoStudioPolishSharesGlobalConcurrencyWithVideo(t *testing.T) {
	db := executionTestDB(t)
	ctx := context.Background()
	store := NewAIVideoStudioStore(db)
	for _, init := range []func() error{store.InitTables, store.InitExecutionTables, NewStoneRecordModel(db).InitTable} {
		if err := init(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users(id BIGINT PRIMARY KEY,stones BIGINT UNSIGNED NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	uid := int64(930003)
	if _, err := db.Exec(`REPLACE INTO users(id,stones) VALUES(?,20)`, uid); err != nil {
		t.Fatal(err)
	}
	unit := int64(2)
	prices, _ := json.Marshal([]AIVideoStudioPrice{{Code: polishPriceCode, BillingUnit: "per_request", UnitStones: &unit, Enabled: true}})
	polish, _ := json.Marshal(AIVideoStudioPolishConfig{Enabled: true, Model: "fixture", TimeoutSeconds: 45})
	if _, err := db.Exec(`UPDATE ai_video_studio_config SET enabled=1,modes_json='{"text":true}',limits_json='{"max_concurrency":1,"timeout_seconds":900}',prices_json=?,prompt_optimize_json=?,version=4 WHERE id=1`, prices, polish); err != nil {
		t.Fatal(err)
	}
	draft := AIVideoStudioDraft{UserID: uid, Name: "x", Mode: "text", Prompt: "x", References: json.RawMessage(`[]`), Ratio: "16:9", Resolution: "768p", Duration: 4, Quantity: 1}
	if err := store.CreateDraft(ctx, &draft); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := json.Marshal(draft)
	hash, _ := AIVideoStudioSnapshotHash(snapshot)
	q := AIVideoStudioQuote{ID: uuid.NewString(), UserID: uid, DraftID: draft.ID, DraftRevision: draft.Revision, QuoteVersion: 4, InputHash: hash, PriceCode: polishPriceCode, BillingUnit: "per_request", UnitStones: 2, BillableUnits: 1, TotalStones: 2, InputSnapshot: snapshot, ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.CreateQuote(ctx, q); err != nil {
		t.Fatal(err)
	}
	task, _, err := store.CreatePolishTask(ctx, uid, q.ID, "shared-capacity")
	if err != nil {
		t.Fatal(err)
	}
	batchID, videoTaskID := uuid.NewString(), uuid.NewString()
	if _, err = db.Exec(`INSERT INTO ai_video_studio_batches(id,user_id,draft_id,quote_id,idempotency_key,mode,status,input_snapshot,fee_status,total_stones) VALUES(?,?,?,?,?,'text','running',?,'reserved',1)`, batchID, uid, draft.ID, uuid.NewString(), uuid.NewString(), snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO ai_video_studio_tasks(id,batch_id,user_id,item_index,status) VALUES(?,?,?,0,'running')`, videoTaskID, batchID, uid); err != nil {
		t.Fatal(err)
	}
	if claimed, err := store.ClaimPolishTask(ctx); err != nil || claimed != nil {
		t.Fatalf("polish exceeded video capacity: %+v %v", claimed, err)
	}
	if _, err = db.Exec(`UPDATE ai_video_studio_tasks SET status='completed' WHERE id=?`, videoTaskID); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimPolishTask(ctx)
	if err != nil || claimed == nil || claimed.TaskID != task.TaskID {
		t.Fatalf("polish not admitted: %+v %v", claimed, err)
	}

	// The reverse direction uses the same config lock and must also see polish activity.
	if _, err = db.Exec(`UPDATE ai_video_studio_tasks SET status='queued' WHERE id=?`, videoTaskID); err != nil {
		t.Fatal(err)
	}
	video, err := store.claimStudioTask(ctx)
	if err != nil || video != nil {
		t.Fatalf("video exceeded polish capacity: %+v %v", video, err)
	}
}

func TestAIVideoStudioPolishInterruptedSubmissionStaysUncertain(t *testing.T) {
	db := executionTestDB(t)
	ctx := context.Background()
	store := NewAIVideoStudioStore(db)
	for _, init := range []func() error{store.InitTables, store.InitExecutionTables, NewStoneRecordModel(db).InitTable} {
		if err := init(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users(id BIGINT PRIMARY KEY,stones BIGINT UNSIGNED NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	uid := int64(930002)
	if _, err := db.Exec(`REPLACE INTO users(id,stones) VALUES(?,20)`, uid); err != nil {
		t.Fatal(err)
	}
	unit := int64(2)
	prices, _ := json.Marshal([]AIVideoStudioPrice{{Code: "ai_video_studio_prompt_optimize", BillingUnit: "per_request", UnitStones: &unit, Enabled: true}})
	polish, _ := json.Marshal(AIVideoStudioPolishConfig{Enabled: true, Model: "fixture", TimeoutSeconds: 45})
	if _, err := db.Exec(`UPDATE ai_video_studio_config SET enabled=1,modes_json='{"text":true}',limits_json='{"max_concurrency":3}',prices_json=?,prompt_optimize_json=?,version=3 WHERE id=1`, prices, polish); err != nil {
		t.Fatal(err)
	}
	draft := AIVideoStudioDraft{UserID: uid, Name: "x", Mode: "text", Prompt: "x", References: json.RawMessage(`[]`), Ratio: "16:9", Resolution: "768p", Duration: 4, Quantity: 1}
	if err := store.CreateDraft(ctx, &draft); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := json.Marshal(draft)
	hash, _ := AIVideoStudioSnapshotHash(snapshot)
	q := AIVideoStudioQuote{ID: uuid.NewString(), UserID: uid, DraftID: draft.ID, DraftRevision: draft.Revision, QuoteVersion: 3, InputHash: hash, PriceCode: "ai_video_studio_prompt_optimize", BillingUnit: "per_request", UnitStones: 2, BillableUnits: 1, TotalStones: 2, InputSnapshot: snapshot, ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.CreateQuote(ctx, q); err != nil {
		t.Fatal(err)
	}
	task, _, err := store.CreatePolishTask(ctx, uid, q.ID, "unknown")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimPolishTask(ctx)
	if err != nil || claimed == nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE ai_video_studio_prompt_tasks SET lease_until=DATE_SUB(NOW(),INTERVAL 1 SECOND) WHERE id=?`, task.TaskID); err != nil {
		t.Fatal(err)
	}
	if err = store.RecoverPolishTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if next, err := store.ClaimPolishTask(ctx); err != nil || next != nil {
		t.Fatalf("uncertain resubmitted: %+v %v", next, err)
	}
	task, _ = store.GetPolishTask(ctx, uid, task.TaskID)
	if task.Status != "uncertain" || task.FeeStatus != "reserved" {
		t.Fatalf("recovered=%+v", task)
	}
	if _, err = db.Exec(`CREATE TRIGGER polish_fail_reconcile BEFORE INSERT ON stone_records FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture reconcile failure'`); err != nil {
		t.Fatal(err)
	}
	if err = store.ReconcilePolishTask(ctx, task.TaskID, "confirmed_rejected", "供应商确认未受理", "", 99, "admin"); err == nil {
		t.Fatal("reconciliation ignored ledger failure")
	}
	var auditCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_prompt_reconciliations WHERE task_id=?`, task.TaskID).Scan(&auditCount); err != nil || auditCount != 0 {
		t.Fatalf("partial reconciliation audit=%d err=%v", auditCount, err)
	}
	task, _ = store.GetPolishTask(ctx, uid, task.TaskID)
	if task.Status != "uncertain" || task.FeeStatus != "reserved" {
		t.Fatalf("partial reconciliation task=%+v", task)
	}
	if balance, _ := NewUserRedisModel(db, nil).GetStones(uid); balance != 18 {
		t.Fatalf("partial reconciliation balance=%d", balance)
	}
	if _, err = db.Exec(`DROP TRIGGER polish_fail_reconcile`); err != nil {
		t.Fatal(err)
	}
	sameDecisionErrors := make(chan error, 2)
	var sameDecisions sync.WaitGroup
	sameDecisions.Add(2)
	go func() {
		defer sameDecisions.Done()
		sameDecisionErrors <- store.ReconcilePolishTask(ctx, task.TaskID, "confirmed_rejected", "供应商确认未受理", "", 99, "admin")
	}()
	go func() {
		defer sameDecisions.Done()
		sameDecisionErrors <- store.ReconcilePolishTask(ctx, task.TaskID, "confirmed_rejected", "重复核对", "", 100, "admin2")
	}()
	sameDecisions.Wait()
	close(sameDecisionErrors)
	for sameDecisionErr := range sameDecisionErrors {
		if sameDecisionErr != nil {
			t.Fatalf("same decision replay: %v", sameDecisionErr)
		}
	}
	if balance, _ := NewUserRedisModel(db, nil).GetStones(uid); balance != 20 {
		t.Fatalf("refund=%d", balance)
	}

	// Two opposite manual decisions serialize on the same wallet/task locks. Only
	// one decision and one financial outcome can commit.
	q.ID = uuid.NewString()
	if err = store.CreateQuote(ctx, q); err != nil {
		t.Fatal(err)
	}
	competing, _, err := store.CreatePolishTask(ctx, uid, q.ID, "competing-decision")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err = store.ClaimPolishTask(ctx)
	if err != nil || claimed == nil || claimed.TaskID != competing.TaskID {
		t.Fatalf("claim competing=%+v err=%v", claimed, err)
	}
	if err = store.MarkPolishUncertain(ctx, competing.TaskID, claimed.LeaseOwner, "结果待人工核对"); err != nil {
		t.Fatal(err)
	}
	decisionResult := "镜头平稳推进，保持庭院建筑和材质稳定。"
	errorsByAction := make(chan error, 2)
	var decisions sync.WaitGroup
	decisions.Add(2)
	go func() {
		defer decisions.Done()
		errorsByAction <- store.ReconcilePolishTask(ctx, competing.TaskID, "confirmed_rejected", "供应商确认未受理", "", 101, "reject-admin")
	}()
	go func() {
		defer decisions.Done()
		errorsByAction <- store.ReconcilePolishTask(ctx, competing.TaskID, "confirmed_result", "供应商返回结果截图", decisionResult, 102, "result-admin")
	}()
	decisions.Wait()
	close(errorsByAction)
	var successes, conflicts int
	for decisionErr := range errorsByAction {
		if decisionErr == nil {
			successes++
		} else if errors.Is(decisionErr, ErrAIVideoStudioConflict) {
			conflicts++
		} else {
			t.Fatalf("decision error: %v", decisionErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("decision outcomes success=%d conflict=%d", successes, conflicts)
	}
	competing, err = store.GetPolishTask(ctx, uid, competing.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_prompt_reconciliations WHERE task_id=?`, competing.TaskID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("decision audits=%d err=%v", auditCount, err)
	}
	if competing.Status == "completed" {
		if competing.OptimizedPrompt != decisionResult || competing.FeeStatus != "settled" {
			t.Fatalf("result decision=%+v", competing)
		}
		if err = store.ReconcilePolishTask(ctx, competing.TaskID, "confirmed_result", "重复结果核对", decisionResult, 103, "admin"); err != nil {
			t.Fatal(err)
		}
		if err = store.ReconcilePolishTask(ctx, competing.TaskID, "confirmed_result", "不同结果", decisionResult+"不同", 103, "admin"); !errors.Is(err, ErrAIVideoStudioConflict) {
			t.Fatalf("different result replay=%v", err)
		}
		if err = store.ReconcilePolishTask(ctx, competing.TaskID, "confirmed_rejected", "相反决策", "", 103, "admin"); !errors.Is(err, ErrAIVideoStudioConflict) {
			t.Fatalf("opposite rejection=%v", err)
		}
		if balance, _ := NewUserRedisModel(db, nil).GetStones(uid); balance != 18 {
			t.Fatalf("result balance=%d", balance)
		}
	} else if competing.Status == "failed" {
		if competing.FeeStatus != "released" {
			t.Fatalf("rejected decision=%+v", competing)
		}
		if err = store.ReconcilePolishTask(ctx, competing.TaskID, "confirmed_rejected", "重复拒绝核对", "", 103, "admin"); err != nil {
			t.Fatal(err)
		}
		if err = store.ReconcilePolishTask(ctx, competing.TaskID, "confirmed_result", "相反决策", decisionResult, 103, "admin"); !errors.Is(err, ErrAIVideoStudioConflict) {
			t.Fatalf("opposite result=%v", err)
		}
		if balance, _ := NewUserRedisModel(db, nil).GetStones(uid); balance != 20 {
			t.Fatalf("rejected balance=%d", balance)
		}
	} else {
		t.Fatalf("unexpected competing status=%+v", competing)
	}
}
