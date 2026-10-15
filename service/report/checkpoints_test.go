package report

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCanonicalStagePayload(t *testing.T) {
	first, err := canonicalStagePayload([]byte(`{ "b": 2, "a": 1 }`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := canonicalStagePayload([]byte(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || string(first) != `{"a":1,"b":2}` {
		t.Fatalf("canonical payloads differ: %s / %s", first, second)
	}
	for _, invalid := range [][]byte{nil, []byte(`{"a":1} trailing`), []byte(`not-json`), make([]byte, maxStagePayloadBytes+1)} {
		if _, err := canonicalStagePayload(invalid); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("payload should be invalid, got %v", err)
		}
	}
}

func TestSaveStageInsertsCanonicalCheckpointWithActiveLease(t *testing.T) {
	state := activeCheckpointState()
	store, closeDB := newCheckpointTestStore(t, state)
	defer closeDB()

	err := store.SaveStage(context.Background(), "report-1", "lease-1", "run-1", "outline", json.RawMessage(`{ "b":2,"a":1 }`))
	if err != nil {
		t.Fatal(err)
	}
	if !state.committed || string(state.inserted) != `{"a":1,"b":2}` {
		t.Fatalf("commit=%v inserted=%s", state.committed, state.inserted)
	}
}

func TestSaveStageIsIdempotentButImmutable(t *testing.T) {
	state := activeCheckpointState()
	state.stagePayload = []byte(`{"b":2,"a":1}`)
	store, closeDB := newCheckpointTestStore(t, state)
	err := store.SaveStage(context.Background(), "report-1", "lease-1", "run-1", "outline", json.RawMessage(`{"a":1,"b":2}`))
	closeDB()
	if err != nil || !state.committed || state.inserted != nil {
		t.Fatalf("same checkpoint err=%v committed=%v inserted=%s", err, state.committed, state.inserted)
	}

	state = activeCheckpointState()
	state.stagePayload = []byte(`{"a":1}`)
	store, closeDB = newCheckpointTestStore(t, state)
	defer closeDB()
	err = store.SaveStage(context.Background(), "report-1", "lease-1", "run-1", "outline", json.RawMessage(`{"a":2}`))
	if !errors.Is(err, ErrConflict) || state.committed || state.inserted != nil {
		t.Fatalf("changed checkpoint err=%v committed=%v inserted=%s", err, state.committed, state.inserted)
	}
}

func TestSaveStageRejectsStaleLease(t *testing.T) {
	state := activeCheckpointState()
	state.leaseToken = "new-lease"
	store, closeDB := newCheckpointTestStore(t, state)
	defer closeDB()

	err := store.SaveStage(context.Background(), "report-1", "old-lease", "run-1", "outline", json.RawMessage(`{"a":1}`))
	if !errors.Is(err, ErrConflict) || state.inserted != nil || state.committed {
		t.Fatalf("stale lease err=%v committed=%v inserted=%s", err, state.committed, state.inserted)
	}
}

func TestSaveStageRejectsInactiveGeneration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*checkpointDBState)
	}{
		{name: "expired lease", mutate: func(state *checkpointDBState) { state.leaseActive = false }},
		{name: "non-generating status", mutate: func(state *checkpointDBState) { state.status = StatusReady }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := activeCheckpointState()
			test.mutate(state)
			store, closeDB := newCheckpointTestStore(t, state)
			defer closeDB()
			err := store.SaveStage(context.Background(), "report-1", "lease-1", "run-1", "outline", json.RawMessage(`{"a":1}`))
			if !errors.Is(err, ErrConflict) || state.inserted != nil || state.committed {
				t.Fatalf("inactive generation err=%v committed=%v inserted=%s", err, state.committed, state.inserted)
			}
		})
	}
}

func TestSaveStageValidatesBeforeDatabaseAccess(t *testing.T) {
	store := &MySQLStore{}
	for _, test := range []struct {
		reportID string
		lease    string
		runKey   string
		stage    string
		payload  json.RawMessage
	}{
		{reportID: "", lease: "lease-1", runKey: "run-1", stage: "outline", payload: json.RawMessage(`{}`)},
		{reportID: "report-1", lease: "", runKey: "run-1", stage: "outline", payload: json.RawMessage(`{}`)},
		{reportID: "report-1", lease: "lease-1", runKey: "bad key", stage: "outline", payload: json.RawMessage(`{}`)},
		{reportID: "report-1", lease: "lease-1", runKey: "run-1", stage: "outline", payload: json.RawMessage(`invalid`)},
	} {
		if err := store.SaveStage(context.Background(), test.reportID, test.lease, test.runKey, test.stage, test.payload); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid stage input err=%v", err)
		}
	}
}

func TestLoadStageReturnsNotFoundAndCanonicalJSON(t *testing.T) {
	state := activeCheckpointState()
	store, closeDB := newCheckpointTestStore(t, state)
	if _, err := store.LoadStage(context.Background(), "report-1", "run-1", "outline"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing stage err=%v", err)
	}
	closeDB()

	state = activeCheckpointState()
	state.stagePayload = []byte(`{ "b":2, "a":1 }`)
	store, closeDB = newCheckpointTestStore(t, state)
	defer closeDB()
	payload, err := store.LoadStage(context.Background(), "report-1", "run-1", "outline")
	if err != nil || string(payload) != `{"a":1,"b":2}` {
		t.Fatalf("payload=%s err=%v", payload, err)
	}
}

type checkpointDBState struct {
	reportExists bool
	status       string
	leaseToken   string
	leaseUntil   time.Time
	leaseActive  bool
	stagePayload []byte
	inserted     []byte
	committed    bool
}

func activeCheckpointState() *checkpointDBState {
	return &checkpointDBState{
		reportExists: true,
		status:       StatusFullGenerating,
		leaseToken:   "lease-1",
		leaseUntil:   time.Now().Add(time.Minute),
		leaseActive:  true,
	}
}

var checkpointDriverID atomic.Uint64

func newCheckpointTestStore(t *testing.T, state *checkpointDBState) (*MySQLStore, func()) {
	t.Helper()
	name := fmt.Sprintf("report-checkpoint-%d", checkpointDriverID.Add(1))
	sql.Register(name, checkpointDriver{state: state})
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

type checkpointDriver struct{ state *checkpointDBState }

func (d checkpointDriver) Open(string) (driver.Conn, error) {
	return &checkpointConn{state: d.state}, nil
}

type checkpointConn struct{ state *checkpointDBState }

func (c *checkpointConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (c *checkpointConn) Close() error              { return nil }
func (c *checkpointConn) Begin() (driver.Tx, error) { return &checkpointTx{state: c.state}, nil }
func (c *checkpointConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

func (c *checkpointConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(query, "FROM building_reports"):
		if !c.state.reportExists {
			return &checkpointRows{columns: []string{"status", "lease_token", "lease_until", "lease_active"}}, nil
		}
		return &checkpointRows{
			columns: []string{"status", "lease_token", "lease_until", "lease_active"},
			values:  [][]driver.Value{{c.state.status, c.state.leaseToken, c.state.leaseUntil, c.state.leaseActive}},
		}, nil
	case strings.Contains(query, "FROM report_agent_stages"):
		rows := &checkpointRows{columns: []string{"payload_json"}}
		if c.state.stagePayload != nil {
			rows.values = [][]driver.Value{{append([]byte(nil), c.state.stagePayload...)}}
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func (c *checkpointConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(query, "INSERT INTO report_agent_stages") || len(args) != 4 {
		return nil, fmt.Errorf("unexpected exec: %s", query)
	}
	payload, ok := args[3].Value.([]byte)
	if !ok {
		return nil, fmt.Errorf("payload type %T", args[3].Value)
	}
	c.state.inserted = append([]byte(nil), payload...)
	return driver.RowsAffected(1), nil
}

type checkpointTx struct{ state *checkpointDBState }

func (tx *checkpointTx) Commit() error {
	tx.state.committed = true
	return nil
}
func (tx *checkpointTx) Rollback() error { return nil }

type checkpointRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *checkpointRows) Columns() []string { return r.columns }
func (r *checkpointRows) Close() error      { return nil }
func (r *checkpointRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
