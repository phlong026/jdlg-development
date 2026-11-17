package model

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"service/aivideostudio"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStudioWorkerErrorRedactsCause(t *testing.T) {
	err := &StudioWorkerError{
		Operation: "process",
		TaskID:    "task-123",
		Cause:     errors.New("provider response contains a secret token"),
	}
	message := err.Error()
	if !strings.Contains(message, "operation=process") || !strings.Contains(message, "task_id=task-123") || !strings.Contains(message, "category=database_or_dependency") {
		t.Fatalf("safe context missing from worker error: %q", message)
	}
	if strings.Contains(message, "secret") || strings.Contains(message, "provider response") {
		t.Fatalf("worker error exposed dependency details: %q", message)
	}
	if !errors.Is(err, err.Cause) {
		t.Fatal("worker error no longer unwraps its cause")
	}
}

type studioWorkerFake struct {
	submits   atomic.Int32
	queries   atomic.Int32
	uncertain bool
	queryErr  bool
}

func (f *studioWorkerFake) Code() string { return "metaso_h3" }
func (f *studioWorkerFake) Capabilities(context.Context) (aivideostudio.ProviderCapabilities, error) {
	return aivideostudio.ProviderCapabilities{}, nil
}
func (f *studioWorkerFake) Cancel(context.Context, string) error { return errors.New("unsupported") }
func (f *studioWorkerFake) Submit(context.Context, aivideostudio.GenerationRequest) (aivideostudio.GenerationReceipt, error) {
	f.submits.Add(1)
	if f.uncertain {
		return aivideostudio.GenerationReceipt{}, aivideostudio.ErrSubmissionUncertain
	}
	return aivideostudio.GenerationReceipt{ExternalID: uuid.NewString()}, nil
}
func (f *studioWorkerFake) Query(context.Context, string) (aivideostudio.GenerationStatus, error) {
	f.queries.Add(1)
	if f.queryErr {
		return aivideostudio.GenerationStatus{}, errors.New("unknown provider state")
	}
	return aivideostudio.GenerationStatus{Status: "succeeded", ResultURL: "https://example.com/test.mp4"}, nil
}
func TestStudioWorkerDurabilityAndReconciliation(t *testing.T) {
	db := executionTestDB(t)
	s := NewAIVideoStudioStore(db)
	ctx := context.Background()
	for _, fn := range []func() error{s.InitTables, s.InitExecutionTables, s.InitWorkerTables, NewStoneRecordModel(db).InitTable} {
		if e := fn(); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := db.Exec("CREATE TABLE users(id BIGINT PRIMARY KEY,stones BIGINT UNSIGNED NOT NULL)"); e != nil {
		t.Fatal(e)
	}
	uid := int64(830002)
	if _, e := db.Exec(`REPLACE INTO users(id,stones) VALUES(?,100)`, uid); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"ai_video_studio_tasks", "ai_video_studio_fees", "ai_video_studio_batches"} {
		if _, e := db.Exec("DELETE FROM "+table+" WHERE user_id=?", uid); e != nil {
			t.Fatal(e)
		}
	}
	db.Exec(`UPDATE ai_video_studio_config SET limits_json='{"max_concurrency":1,"timeout_seconds":900}' WHERE id=1`)
	makeTask := func() string {
		id, b := uuid.NewString(), uuid.NewString()
		snap, _ := json.Marshal(AIVideoStudioDraft{Mode: "text", Prompt: "hello", Resolution: "768p", Duration: 4, References: json.RawMessage(`[]`)})
		if _, e := db.Exec(`INSERT INTO ai_video_studio_batches(id,user_id,draft_id,quote_id,idempotency_key,mode,status,input_snapshot,total_stones) VALUES(?,?,1,?,?,'text','queued',?,1)`, b, uid, uuid.NewString(), b, string(snap)); e != nil {
			t.Fatal(e)
		}
		if _, e := db.Exec(`INSERT INTO ai_video_studio_tasks(id,batch_id,user_id,item_index,status) VALUES(?,?,?,0,'queued')`, id, b, uid); e != nil {
			t.Fatal(e)
		}
		if _, e := db.Exec(`INSERT INTO ai_video_studio_fees(batch_id,task_id,user_id,quote_id,kind,amount,status,idempotency_key) VALUES(?,?,?,?,'reserve',1,'reserved',?)`, b, id, uid, uuid.NewString(), id+":reserve"); e != nil {
			t.Fatal(e)
		}
		return id
	}
	state := func(id string) string {
		var v string
		if e := db.QueryRow(`SELECT status FROM ai_video_studio_tasks WHERE id=?`, id).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	id := makeTask()
	p := &studioWorkerFake{}
	build := func(context.Context, StudioWorkerTask) (aivideostudio.GenerationRequest, error) {
		return aivideostudio.GenerationRequest{}, nil
	}
	w := StudioWorker{Store: s, Provider: p, Build: build}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- w.Step(ctx) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if p.submits.Load() != 1 {
		t.Fatal("two workers submitted more than once")
	}
	db.Exec(`UPDATE ai_video_studio_tasks SET next_poll_at=NULL WHERE id=?`, id)
	if e := w.Step(ctx); e != nil {
		t.Fatal(e)
	}
	if state(id) != "completed" || p.submits.Load() != 1 {
		t.Fatal("restart failed")
	}
	var batch string
	db.QueryRow(`SELECT batch_id FROM ai_video_studio_tasks WHERE id=?`, id).Scan(&batch)
	if b, e := s.GetBatch(ctx, uid, batch); e != nil || b["can_cancel"] != false {
		t.Fatal(b, e)
	}
	if _, e := s.GetBatch(ctx, uid+1, batch); e == nil {
		t.Fatal("cross owner read")
	}
	var key string
	db.QueryRow(`SELECT idempotency_key FROM ai_video_studio_batches WHERE id=?`, batch).Scan(&key)
	var before, after int
	db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_batches WHERE user_id=?`, uid).Scan(&before)
	if _, e := s.FindStudioSubmission(ctx, uid, key); e != nil {
		t.Fatal("submission recovery", e)
	}
	if _, e := s.FindStudioSubmission(ctx, uid+1, key); e == nil {
		t.Fatal("cross owner submission")
	}
	if _, e := s.FindStudioSubmission(ctx, uid, "missing-request"); e == nil {
		t.Fatal("missing request created")
	}
	db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_batches WHERE user_id=?`, uid).Scan(&after)
	if before != after {
		t.Fatal("lookup created task")
	}

	id = makeTask()
	db.Exec(`UPDATE ai_video_studio_tasks SET status='submitting',result_url='https://example.com/durable.mp4',output_seconds=7.5 WHERE id=?`, id)
	if e := w.Step(ctx); e != nil {
		t.Fatal(e)
	}
	var seconds float64
	db.QueryRow(`SELECT output_seconds FROM ai_video_studio_tasks WHERE id=?`, id).Scan(&seconds)
	if seconds != 7.5 || state(id) != "completed" {
		t.Fatal("recovery lost output duration", seconds)
	}
	id = makeTask()
	claimed, e := s.claimStudioTask(ctx)
	if e != nil || claimed == nil {
		t.Fatal("claim", e)
	}
	beforeSubmit := p.submits.Load()
	claimed.LeaseUntil = time.Now().Add(-time.Second)
	if e = w.processStudioTask(ctx, claimed); e == nil {
		t.Fatal("expired dispatch not rejected")
	}
	if p.submits.Load() != beforeSubmit {
		t.Fatal("expired lease sent paid request")
	}
	db.Exec(`UPDATE ai_video_studio_tasks SET lease_until=NULL WHERE id=?`, id)
	if e = w.Step(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.ReconcileStudioTask(ctx, id, "refund_confirmed_rejected", "", "供应商后台确认该请求并未受理", 1, "admin"); e != nil {
		t.Fatal(e)
	}
	id = makeTask()
	p.uncertain = true
	if e := w.Step(ctx); e != nil {
		t.Fatal(e)
	}
	if e := w.Step(ctx); e != nil {
		t.Fatal(e)
	}
	if p.submits.Load() != 2 || state(id) != "uncertain" {
		t.Fatal("uncertain resubmitted")
	}
	db.Exec(`UPDATE ai_video_studio_tasks SET status='submitting',lease_until=?,lease_owner='dead' WHERE id=?`, time.Now().Add(-time.Minute), id)
	if e := w.Step(ctx); e != nil {
		t.Fatal(e)
	}
	if state(id) != "uncertain" || p.submits.Load() != 2 {
		t.Fatal("expired submit repeated")
	}
	ev := "已通过供应商账单与后台确认本任务未受理"
	if e := s.ReconcileStudioTask(ctx, id, "refund_confirmed_rejected", "", ev, 1, "admin"); e != nil {
		t.Fatal(e)
	}
	if e := s.ReconcileStudioTask(ctx, id, "refund_confirmed_rejected", "", ev, 1, "admin"); e != nil {
		t.Fatal(e)
	}
	var refunds int
	db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_fees WHERE task_id=? AND kind='refund'`, id).Scan(&refunds)
	if refunds != 1 {
		t.Fatal("duplicate refund")
	}
	id = makeTask()
	db.Exec(`UPDATE ai_video_studio_tasks SET status='uncertain' WHERE id=?`, id)
	if _, e := db.Exec(`CREATE TRIGGER studio_worker_refund_fault BEFORE INSERT ON stone_records FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture refund failure'`); e != nil {
		t.Fatal(e)
	}
	e = s.ReconcileStudioTask(ctx, id, "refund_confirmed_rejected", "", ev, 1, "admin")
	db.Exec(`DROP TRIGGER studio_worker_refund_fault`)
	if e == nil {
		t.Fatal("refund fault not observed")
	}
	db.Exec(`UPDATE ai_video_studio_tasks SET lease_until=NULL WHERE id=?`, id)
	if e = s.ReconcileStudioTask(ctx, id, "refund_confirmed_rejected", "", ev, 1, "admin"); e != nil {
		t.Fatal("audited refund not recoverable", e)
	}
	if state(id) != "failed" {
		t.Fatal("refund recovery status")
	}
	id = makeTask()
	db.Exec(`UPDATE ai_video_studio_tasks SET status='uncertain' WHERE id=?`, id)
	if e := s.ReconcileStudioTask(ctx, id, "attach_external_id", "bound-id", ev, 1, "admin"); e != nil {
		t.Fatal(e)
	}
	if e := s.ReconcileStudioTask(ctx, id, "refund_confirmed_rejected", "", ev, 1, "admin"); e == nil {
		t.Fatal("known external ID refunded")
	}
	other := makeTask()
	db.Exec(`UPDATE ai_video_studio_tasks SET status='uncertain' WHERE id=?`, other)
	if e := s.ReconcileStudioTask(ctx, other, "attach_external_id", "bound-id", ev, 1, "admin"); e == nil {
		t.Fatal("external identity reused")
	}
	db.Exec(`UPDATE ai_video_studio_tasks SET status='failed' WHERE id IN (?,?)`, id, other)
	// A known provider identity continues conservative queries after timeout, even
	// when operations have closed new generation. No automatic refund is allowed.
	p.uncertain = false
	id = makeTask()
	if e := w.Step(ctx); e != nil {
		t.Fatal(e)
	}
	db.Exec(`UPDATE ai_video_studio_config SET enabled=0 WHERE id=1`)
	db.Exec(`UPDATE ai_video_studio_tasks SET created_at=?,next_poll_at=NULL WHERE id=?`, time.Now().Add(-2*time.Hour), id)
	p.queryErr = true
	if e := w.Step(ctx); e != nil {
		t.Fatal(e)
	}
	if state(id) != "uncertain" {
		t.Fatal("timeout lost conservative hold")
	}
	db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_fees WHERE task_id=? AND kind='refund'`, id).Scan(&refunds)
	if refunds != 0 {
		t.Fatal("timeout refunded")
	}
	p.queryErr = false
	db.Exec(`UPDATE ai_video_studio_tasks SET next_poll_at=NULL WHERE id=?`, id)
	if e := w.Step(ctx); e != nil {
		t.Fatal(e)
	}
	if state(id) != "completed" {
		t.Fatal("timeout task not recoverable")
	}
	// Cancellation racing admission refunds only if the durable queue was won.
	id = makeTask()
	db.QueryRow(`SELECT batch_id FROM ai_video_studio_tasks WHERE id=?`, id).Scan(&batch)
	wg.Add(2)
	go func() { defer wg.Done(); _ = s.CancelQueued(ctx, uid, batch) }()
	go func() { defer wg.Done(); _ = w.Step(ctx) }()
	wg.Wait()
	if st := state(id); st != "cancelled" && st != "submitted" {
		t.Fatal("cancel race", st)
	}

}
