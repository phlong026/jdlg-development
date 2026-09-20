package report

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompletionAndFailureWritesRequireActiveDatabaseLease(t *testing.T) {
	state := &fencingDBState{}
	store, closeDB := newFencingTestStore(t, state)
	defer closeDB()
	ctx := context.Background()
	if err := store.CompletePreview(ctx, "report-1", "lease-1", []byte(`{}`), GenerationRulesOnly, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteFull(ctx, "report-1", "lease-1", []byte(`{}`), "reports/full.pdf"); err != nil {
		t.Fatal(err)
	}
	if err := store.Fail(ctx, "report-1", "lease-1", ModeFull, "failed", false); err != nil {
		t.Fatal(err)
	}
	if len(state.execs) != 3 {
		t.Fatalf("exec count = %d", len(state.execs))
	}
	for _, call := range state.execs {
		if !strings.Contains(call.query, "lease_token=? AND lease_until>NOW(6)") {
			t.Fatalf("write is not fenced by active database lease: %s", call.query)
		}
	}
}

func TestClaimUsesDatabaseClockForExpiryAndNewLease(t *testing.T) {
	state := &fencingDBState{claimReport: true}
	store, closeDB := newFencingTestStore(t, state)
	defer closeDB()
	job, err := store.Claim(context.Background(), time.Unix(1, 0), 90*time.Second, 3)
	if err != nil || job == nil {
		t.Fatalf("claim = %#v, %v", job, err)
	}
	if len(state.execs) != 2 {
		t.Fatalf("claim exec count = %d", len(state.execs))
	}
	if !strings.Contains(state.execs[0].query, "lease_until<NOW(6)") {
		t.Fatalf("expiry used application clock: %s", state.execs[0].query)
	}
	if !strings.Contains(state.selectQuery, "lease_until<NOW(6)") {
		t.Fatalf("claim selection used application clock: %s", state.selectQuery)
	}
	if !strings.Contains(state.execs[1].query, "lease_until=DATE_ADD(NOW(6),INTERVAL ? MICROSECOND)") {
		t.Fatalf("new lease used application timestamp: %s", state.execs[1].query)
	}
	for _, call := range state.execs {
		for _, arg := range call.args {
			if _, ok := arg.(time.Time); ok {
				t.Fatalf("claim passed application time to SQL: %#v", call.args)
			}
		}
	}
}

type fencingExec struct {
	query string
	args  []any
}

type fencingDBState struct {
	claimReport bool
	selectQuery string
	execs       []fencingExec
	committed   bool
}

var fencingDriverID atomic.Uint64

func newFencingTestStore(t *testing.T, state *fencingDBState) (*MySQLStore, func()) {
	t.Helper()
	name := fmt.Sprintf("report-fencing-%d", fencingDriverID.Add(1))
	sql.Register(name, fencingDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMySQLStore(db)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	return store, func() { _ = db.Close() }
}

type fencingDriver struct{ state *fencingDBState }

func (d fencingDriver) Open(string) (driver.Conn, error) { return &fencingConn{state: d.state}, nil }

type fencingConn struct{ state *fencingDBState }

func (c *fencingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (c *fencingConn) Close() error              { return nil }
func (c *fencingConn) Begin() (driver.Tx, error) { return &fencingTx{state: c.state}, nil }
func (c *fencingConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

func (c *fencingConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	values := make([]any, len(args))
	for index, arg := range args {
		values[index] = arg.Value
	}
	c.state.execs = append(c.state.execs, fencingExec{query: query, args: values})
	return driver.RowsAffected(1), nil
}

func (c *fencingConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.state.selectQuery = query
	rows := &fencingRows{columns: []string{"report"}}
	if c.state.claimReport {
		now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
		rows.columns = make([]string, 29)
		for index := range rows.columns {
			rows.columns[index] = fmt.Sprintf("column_%d", index)
		}
		rows.values = [][]driver.Value{{
			"report-1", int64(11), strings.Repeat("a", 64), ProtocolDecisionV5, []byte(`{}`),
			nil, nil, StatusPreviewQueued, GenerationRulesOnly, int64(0), int64(0),
			nil, nil, nil, nil, nil, nil, nil, nil, nil,
			nil, nil, int64(0), nil, nil, nil, nil, now, now,
		}}
	}
	return rows, nil
}

type fencingTx struct{ state *fencingDBState }

func (tx *fencingTx) Commit() error   { tx.state.committed = true; return nil }
func (tx *fencingTx) Rollback() error { return nil }

type fencingRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *fencingRows) Columns() []string { return r.columns }
func (r *fencingRows) Close() error      { return nil }
func (r *fencingRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
