package tracking

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// probeSQL records how SQLStore drives the database: transaction options and
// every statement. All queries return no rows.
type probeSQL struct {
	mu      sync.Mutex
	options []driver.TxOptions
	queries []string
	execs   []string
	gate    chan struct{} // when set, the tenant-lock query waits on it
}
type probeConnector struct{ p *probeSQL }

func (c probeConnector) Connect(context.Context) (driver.Conn, error) { return probeConn{c.p}, nil }
func (c probeConnector) Driver() driver.Driver                        { return probeDriver{} }

type probeDriver struct{}

func (probeDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type probeConn struct{ p *probeSQL }

func (c probeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected prepare") }
func (c probeConn) Close() error                        { return nil }
func (c probeConn) Begin() (driver.Tx, error)           { return probeTx{}, nil }
func (c probeConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.p.mu.Lock()
	c.p.options = append(c.p.options, opts)
	c.p.mu.Unlock()
	return probeTx{}, nil
}
func (c probeConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	c.p.mu.Lock()
	c.p.execs = append(c.p.execs, q)
	c.p.mu.Unlock()
	return driver.RowsAffected(1), nil
}
func (c probeConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.p.mu.Lock()
	c.p.queries = append(c.p.queries, q)
	gate := c.p.gate
	c.p.mu.Unlock()
	if strings.Contains(q, "tracking_tenant_lock") {
		if gate != nil {
			<-gate
		}
		return &probeRows{columns: []string{"tenant_id"}, values: [][]driver.Value{{"tenant"}}}, nil
	}
	return &probeRows{columns: []string{"kind", "record_id", "owner_id", "visitor_key", "session_id", "content_id", "share_id", "occurred_at", "payload"}}, nil
}

type probeTx struct{}

func (probeTx) Commit() error   { return nil }
func (probeTx) Rollback() error { return nil }

type probeRows struct {
	columns []string
	values  [][]driver.Value
	i       int
}

func (r *probeRows) Columns() []string { return r.columns }
func (r *probeRows) Close() error      { return nil }
func (r *probeRows) Next(out []driver.Value) error {
	if r.i >= len(r.values) {
		return io.EOF
	}
	copy(out, r.values[r.i])
	r.i++
	return nil
}

func TestSQLReadTransactionIsLockFreeSnapshotAndRejectsWrites(t *testing.T) {
	p := &probeSQL{}
	db := sql.OpenDB(probeConnector{p})
	defer db.Close()
	store := NewSQLStore(db, 0, 0)
	err := store.ReadTransaction(context.Background(), "tenant", func(r Repository) error {
		if _, e := r.Get("entry", "missing"); !errors.Is(e, ErrNotFound) {
			t.Fatalf("read failed: %v", e)
		}
		if e := r.Put(record("audit", "a", "", time.Now(), map[string]string{})); !errors.Is(e, ErrReadOnly) {
			t.Fatalf("put allowed: %v", e)
		}
		if e := r.Insert(record("audit", "b", "", time.Now(), map[string]string{})); !errors.Is(e, ErrReadOnly) {
			t.Fatalf("insert allowed: %v", e)
		}
		if e := r.Remove("audit", "a"); !errors.Is(e, ErrReadOnly) {
			t.Fatalf("remove allowed: %v", e)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.execs) != 0 {
		t.Fatalf("read transaction executed statements: %v", p.execs)
	}
	for _, q := range p.queries {
		if strings.Contains(q, "tracking_tenant_lock") {
			t.Fatalf("read transaction took the tenant lock: %s", q)
		}
	}
	if len(p.options) != 1 || !p.options[0].ReadOnly || p.options[0].Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
		t.Fatalf("want one READ ONLY REPEATABLE READ transaction, got %+v", p.options)
	}

	// The write path is unchanged: it still serializes on the tenant row.
	w := &probeSQL{}
	writeDB := sql.OpenDB(probeConnector{w})
	defer writeDB.Close()
	if err = NewSQLStore(writeDB, 0, 0).Transaction(context.Background(), "tenant", func(Repository) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(w.execs) == 0 || !strings.Contains(w.execs[0], "tracking_tenant_lock") || len(w.queries) == 0 || !strings.Contains(w.queries[0], "FOR UPDATE") {
		t.Fatalf("write transaction lost its lock: execs=%v queries=%v", w.execs, w.queries)
	}
}

func TestSQLStoreSlotsBoundPoolUseAndHonorCancellation(t *testing.T) {
	p := &probeSQL{gate: make(chan struct{})}
	db := sql.OpenDB(probeConnector{p})
	defer db.Close()
	store := NewSQLStore(db, 1, 1)
	held := make(chan error, 1)
	go func() {
		// Blocks on the tenant lock while occupying the only write slot.
		held <- store.Transaction(context.Background(), "tenant", func(Repository) error { return nil })
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		p.mu.Lock()
		waiting := len(p.queries) > 0
		p.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first writer never reached the lock")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	opened := len(p.options)
	if err := store.Transaction(ctx, "tenant", func(Repository) error { return nil }); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second writer was not bounded: %v", err)
	}
	if len(p.options) != opened {
		t.Fatal("bounded writer still opened a database transaction")
	}
	// Readers have their own slots and never wait for the tenant lock.
	if err := store.ReadTransaction(context.Background(), "tenant", func(Repository) error { return nil }); err != nil {
		t.Fatalf("reader blocked behind writer: %v", err)
	}
	close(p.gate)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
}

// txProbeStore reports whether any store transaction is open, so tests can
// assert that external calls happen outside every transaction.
type txProbeStore struct {
	*memoryStore
	active atomic.Int32
}

func (s *txProbeStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	s.active.Add(1)
	defer s.active.Add(-1)
	return s.memoryStore.Transaction(ctx, tenant, fn)
}
func (s *txProbeStore) ReadTransaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	s.active.Add(1)
	defer s.active.Add(-1)
	return s.memoryStore.ReadTransaction(ctx, tenant, fn)
}

func crmProbeFixture(t *testing.T) (*Service, *txProbeStore, Scope) {
	t.Helper()
	s, scope := fixture(t)
	probe := &txProbeStore{memoryStore: s.Store.(*memoryStore)}
	s.Store = probe
	en := enter(t, s, 1)
	accept(t, s, en, ev(s, en, "event-crm-00001", "page_view", s.Now()))
	return s, probe, scope
}

func TestLinkCRMVerifiesOutsideTransactionsAndRechecks(t *testing.T) {
	s, probe, scope := crmProbeFixture(t)
	calls := 0
	s.VerifyCRM = func(_ context.Context, got Scope, visitor, customer, number string) error {
		calls++
		if probe.active.Load() != 0 {
			t.Error("CRM bridge called while a tracking transaction was open")
		}
		if len(got.Owners) != 1 || got.Owners[0] != "owner-a" || visitor != "u:1" {
			t.Errorf("unexpected verification target %+v %s", got, visitor)
		}
		return nil
	}
	if err := s.LinkCRM(context.Background(), scope, "u:1", "customer-1", "B-1", "verified"); err != nil {
		t.Fatal(err)
	}
	target, err := s.VisitorCRMTarget(context.Background(), scope, "u:1")
	if err != nil || target.CustomerID != "customer-1" || target.Owner != "owner-a" {
		t.Fatalf("target: %+v %v", target, err)
	}
	if calls != 2 {
		t.Fatalf("want link and target verification, got %d calls", calls)
	}

	// The visitor is deleted while the bridge answers. The write phase must
	// re-resolve under the lock and refuse instead of recreating a mapping.
	s2, _, scope2 := crmProbeFixture(t)
	s2.VerifyCRM = func(context.Context, Scope, string, string, string) error {
		if err := s2.DeleteVisitor(context.Background(), scope2, "u:1", "requested"); err != nil {
			t.Errorf("concurrent deletion failed: %v", err)
		}
		return nil
	}
	if err := s2.LinkCRM(context.Background(), scope2, "u:1", "customer-1", "B-1", "verified"); err == nil {
		t.Fatal("mapping written for a visitor deleted during verification")
	}
	_ = s2.readTransaction(context.Background(), func(r Repository) error {
		if _, err := r.Get("crm", privateKey("owner-a", "u:1")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted visitor regained a CRM mapping: %v", err)
		}
		return nil
	})
}

func TestIntentEvaluationsSkipVisitorsDeletedAfterSnapshot(t *testing.T) {
	s, scope := fixture(t)
	scope.Owners = []string{"owner-a"}
	now := s.Now()
	if err := s.transaction(context.Background(), func(r Repository) error {
		return r.Put(record("deletion", "u:9", "", now, map[string]any{"deleted_at": now}))
	}); err != nil {
		t.Fatal(err)
	}
	in := Intent{Level: "low", Rule: RuleVersion, Calculated: now}
	if err := s.recordIntentEvaluations(context.Background(), scope, []pendingEvaluation{{key: "u:9", intent: in}, {key: "u:8", intent: in}}); err != nil {
		t.Fatal(err)
	}
	_ = s.readTransaction(context.Background(), func(r Repository) error {
		if _, err := r.Get("intent_evaluation", privateKey("owner-a", "u:9")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted visitor evaluation recreated: %v", err)
		}
		if _, err := r.Get("intent_evaluation", privateKey("owner-a", "u:8")); err != nil {
			t.Fatalf("live visitor evaluation missing: %v", err)
		}
		return nil
	})
}

// The CRM bridge's own client allows 20 seconds, so a bridge that hangs would
// hold the operator's request that long. Linking must give up sooner.
func TestLinkCRMStopsWaitingForAHungBridge(t *testing.T) {
	s, _, scope := crmProbeFixture(t)
	saved := crmLinkVerifyTimeout
	crmLinkVerifyTimeout = 50 * time.Millisecond
	t.Cleanup(func() { crmLinkVerifyTimeout = saved })
	sawDeadline := false
	s.VerifyCRM = func(ctx context.Context, _ Scope, _, _, _ string) error {
		_, sawDeadline = ctx.Deadline()
		<-ctx.Done()
		return ctx.Err()
	}
	started := time.Now()
	err := s.LinkCRM(context.Background(), scope, "u:1", "customer-1", "B-1", "verified")
	if err == nil {
		t.Fatal("a bridge that never answered must not produce a mapping")
	}
	if !sawDeadline || time.Since(started) > 2*time.Second {
		t.Fatalf("the verification must carry a deadline and return promptly: deadline=%v after %v", sawDeadline, time.Since(started))
	}
	_ = s.readTransaction(context.Background(), func(r Repository) error {
		if _, e := r.Get("crm", privateKey("owner-a", "u:1")); !errors.Is(e, ErrNotFound) {
			t.Fatalf("a timed-out verification left a mapping: %v", e)
		}
		return nil
	})
}
