package tracking

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

// Exercise database/sql row iteration, copy ownership, LIMIT binding, and
// transaction rollback without claiming a live-MySQL execution/load result.
type scopedSQLConnector struct{ state *scopedSQLState }
type scopedSQLState struct {
	t                             *testing.T
	rows                          int
	payload                       []byte
	query                         string
	args                          []driver.NamedValue
	committed, rolledBack, closed bool
}

func (c scopedSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return scopedSQLConn{c.state}, nil
}
func (c scopedSQLConnector) Driver() driver.Driver { return scopedSQLDriver{} }

type scopedSQLDriver struct{}

func (scopedSQLDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type scopedSQLConn struct{ s *scopedSQLState }

func (c scopedSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c scopedSQLConn) Close() error              { return nil }
func (c scopedSQLConn) Begin() (driver.Tx, error) { return scopedSQLTx{c.s}, nil }
func (c scopedSQLConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c scopedSQLConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if !strings.HasPrefix(q, "INSERT IGNORE INTO tracking_tenant_lock") {
		c.s.t.Fatalf("unexpected write %s", q)
	}
	return driver.RowsAffected(1), nil
}
func (c scopedSQLConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT tenant_id FROM tracking_tenant_lock") {
		return &scopedSQLRows{s: c.s, lock: true, n: 1}, nil
	}
	c.s.query, c.s.args = q, append([]driver.NamedValue{}, args...)
	return &scopedSQLRows{s: c.s, n: c.s.rows, payload: append([]byte(nil), c.s.payload...)}, nil
}

type scopedSQLTx struct{ s *scopedSQLState }

func (t scopedSQLTx) Commit() error   { t.s.committed = true; return nil }
func (t scopedSQLTx) Rollback() error { t.s.rolledBack = true; return nil }

type scopedSQLRows struct {
	s       *scopedSQLState
	n, i    int
	lock    bool
	payload []byte
}

func (r *scopedSQLRows) Columns() []string {
	if r.lock {
		return []string{"tenant_id"}
	}
	return []string{"kind", "record_id", "owner_id", "visitor_key", "session_id", "content_id", "share_id", "occurred_at", "payload"}
}
func (r *scopedSQLRows) Close() error { r.s.closed = true; return nil }
func (r *scopedSQLRows) Next(out []driver.Value) error {
	if r.i >= r.n {
		return io.EOF
	}
	r.i++
	if r.lock {
		out[0] = "tenant"
		return nil
	}
	out[0], out[1], out[2], out[3], out[4], out[5], out[6], out[7] = "event", "id", "one", "u:1", "session", "content", "", "2026-10-11 00:00:00.000001"
	// Reuse driver-owned bytes deliberately: callers must retain independent
	// JSON blobs when scanning the next row overwrites the driver's buffer.
	if r.payload != nil {
		if len(r.payload) == 3 {
			r.payload[1] = byte('0' + r.i)
		}
		out[8] = r.payload
	} else {
		out[8] = nil
	}
	return nil
}
func sqlReadFixture(t *testing.T, s *scopedSQLState) (*sqlRepo, func()) {
	t.Helper()
	s.t = t
	db := sql.OpenDB(scopedSQLConnector{s})
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	return &sqlRepo{ctx: context.Background(), tx: tx, tenant: "tenant"}, func() { tx.Rollback(); db.Close() }
}
func TestSQLScopedReadBudgetAndDriverByteOwnership(t *testing.T) {
	t.Run("copy each payload", func(t *testing.T) {
		state := &scopedSQLState{rows: 2, payload: []byte(`"0"`)}
		r, close := sqlReadFixture(t, state)
		defer close()
		got, err := r.ListScoped("event", RecordQuery{Owners: []string{"one"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || string(got[0].Payload) != `"1"` || string(got[1].Payload) != `"2"` {
			t.Fatalf("driver buffer aliased: %+v", got)
		}
		if got[0].Tenant != "tenant" || !state.closed {
			t.Fatal("tenant or row close missing")
		}
		if state.args[len(state.args)-1].Value != int64(maxQueryRecords+1) {
			t.Fatal("missing budget-plus-one limit")
		}
	})
	t.Run("remaining row budget", func(t *testing.T) {
		state := &scopedSQLState{rows: 2, payload: []byte(`{}`)}
		r, close := sqlReadFixture(t, state)
		defer close()
		r.budget.rows = maxQueryRecords - 1
		got, err := r.ListSession("event", "session")
		requireQueryError(t, err, 422)
		if got != nil || state.args[len(state.args)-1].Value != int64(2) || !state.closed {
			t.Fatal("partial rows or unbounded LIMIT")
		}
	})
	t.Run("oversized SQL payload sentinel", func(t *testing.T) {
		state := &scopedSQLState{rows: 1, payload: nil}
		r, close := sqlReadFixture(t, state)
		defer close()
		_, err := r.Get("content", "large")
		requireQueryError(t, err, 422)
		if r.budget.err == nil || !strings.Contains(state.query, "CASE WHEN OCTET_LENGTH(payload)<=?") {
			t.Fatal("oversized read not sticky/bounded")
		}
	})
	t.Run("cumulative byte budget", func(t *testing.T) {
		state := &scopedSQLState{rows: 1, payload: []byte(`{"value":"twenty-plus bytes"}`)}
		r, close := sqlReadFixture(t, state)
		defer close()
		r.budget.bytes = maxQueryPayloadBytes - 5
		got, err := r.List("event")
		requireQueryError(t, err, 422)
		if got != nil {
			t.Fatal("partial result escaped")
		}
	})
	t.Run("empty query does not widen scope", func(t *testing.T) {
		state := &scopedSQLState{rows: 1, payload: []byte(`{}`)}
		r, close := sqlReadFixture(t, state)
		defer close()
		got, err := r.ListScoped("event", RecordQuery{Owners: []string{}})
		if err != nil || len(got) != 0 || state.query != "" {
			t.Fatal("empty owner scope reached DB")
		}
	})
}
func TestSQLBudgetFailureRollsBackEvenIfCallbackIgnoresIt(t *testing.T) {
	state := &scopedSQLState{t: t, rows: 1, payload: nil}
	db := sql.OpenDB(scopedSQLConnector{state})
	defer db.Close()
	store := SQLStore{DB: db}
	err := store.Transaction(context.Background(), "tenant", func(r Repository) error { _, _ = r.List("event"); return nil })
	requireQueryError(t, err, 422)
	if state.committed || !state.rolledBack {
		t.Fatal("failed budget transaction committed")
	}
}
