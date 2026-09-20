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
)

type houseImageSQLState struct {
	active  bool
	raw     []byte
	queries []string
	writes  int
}
type houseImageSQLDriver struct{ state *houseImageSQLState }
type houseImageSQLConn struct{ state *houseImageSQLState }
type houseImageSQLTx struct{}
type houseImageSQLRows struct {
	value []byte
	done  bool
}

var houseImageSQLID atomic.Uint64

func (d houseImageSQLDriver) Open(string) (driver.Conn, error) {
	return &houseImageSQLConn{state: d.state}, nil
}
func (c *houseImageSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *houseImageSQLConn) Close() error              { return nil }
func (c *houseImageSQLConn) Begin() (driver.Tx, error) { return houseImageSQLTx{}, nil }
func (houseImageSQLTx) Commit() error                  { return nil }
func (houseImageSQLTx) Rollback() error                { return nil }
func (r *houseImageSQLRows) Columns() []string         { return []string{"value"} }
func (r *houseImageSQLRows) Close() error              { return nil }
func (r *houseImageSQLRows) Next(dest []driver.Value) error {
	if r.done || r.value == nil {
		return io.EOF
	}
	dest[0] = r.value
	r.done = true
	return nil
}
func (c *houseImageSQLConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.queries = append(c.state.queries, q)
	if strings.HasPrefix(q, "SELECT r.report_id") {
		if !strings.Contains(q, "r.user_id=?") || !strings.Contains(q, "r.input_hash=?") || !strings.Contains(q, "r.protocol_version=?") || !strings.Contains(q, "r.paid=1") || !strings.Contains(q, "r.status=?") || !strings.Contains(q, "r.lease_token=?") || !strings.Contains(q, "r.lease_until>NOW(6)") || !strings.Contains(q, "d.confirmation_hash=? FOR UPDATE") {
			return nil, errors.New("missing write fence")
		}
		if len(args) != 7 || args[1].Value != int64(7) || args[5].Value != "lease-1" || !c.state.active {
			return &houseImageSQLRows{}, nil
		}
		return &houseImageSQLRows{value: []byte("house-report")}, nil
	}
	if !strings.Contains(q, "user_id=?") {
		return nil, errors.New("missing owner filter")
	}
	return &houseImageSQLRows{value: c.state.raw}, nil
}
func (c *houseImageSQLConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.state.queries = append(c.state.queries, q)
	c.state.writes++
	if strings.HasPrefix(q, "INSERT") {
		if !strings.Contains(q, "lease_token=? AND lease_until>NOW(6)") || !strings.Contains(q, "user_id=?") {
			return nil, errors.New("missing final reservation fence")
		}
		c.state.raw = append([]byte(nil), args[5].Value.([]byte)...)
	} else {
		if !strings.Contains(q, "r.lease_token=? AND r.lease_until>NOW(6)") || !strings.Contains(q, "i.user_id=?") {
			return nil, errors.New("missing completion fence")
		}
		c.state.raw = append([]byte(nil), args[0].Value.([]byte)...)
	}
	return driver.RowsAffected(1), nil
}
func newHouseImageSQLStore(t *testing.T, state *houseImageSQLState) *MySQLHouseImageStore {
	t.Helper()
	name := fmt.Sprintf("house-image-sql-%d", houseImageSQLID.Add(1))
	sql.Register(name, houseImageSQLDriver{state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewMySQLHouseImageStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestHouseImageSQLReservationAndCompletionRequireDatabaseLease(t *testing.T) {
	r, task, p := imageFixture(t)
	state := &houseImageSQLState{active: false}
	s := newHouseImageSQLStore(t, state)
	row := HouseImageRecord{ReportID: r.ID, UserID: r.UserID, InputHash: r.InputHash, ConfirmationHash: r.ConfirmationHash, RequestHash: strings.Repeat("c", 64), Identity: p.identity, Task: task, Status: "submitting"}
	if _, _, err := s.Reserve(context.Background(), r, row); !errors.Is(err, ErrConflict) {
		t.Fatal("expired reservation", err)
	}
	if state.writes != 0 {
		t.Fatal("expired lease wrote")
	}
	state.active = true
	first, claimed, err := s.Reserve(context.Background(), r, row)
	if err != nil || !claimed {
		t.Fatal("reserve", err)
	}
	_, claimed, err = s.Reserve(context.Background(), r, row)
	if err != nil || claimed || state.writes != 1 {
		t.Fatal("reservation was repeated", err)
	}
	first.Status = "complete"
	first.Asset = HouseImageAsset{ID: task.ID, SHA256: strings.Repeat("d", 64)}
	state.active = false
	if err := s.Complete(context.Background(), r, first); !errors.Is(err, ErrConflict) {
		t.Fatal("expired completion", err)
	}
	state.active = true
	if err := s.Complete(context.Background(), r, first); err != nil {
		t.Fatal(err)
	}
	var done HouseImageRecord
	if err := json.Unmarshal(state.raw, &done); err != nil || done.Status != "complete" {
		t.Fatal("completion not persisted", err)
	}
	if err := s.Complete(context.Background(), r, first); err != nil || state.writes != 2 {
		t.Fatal("idempotent completion", err)
	}
	other := *r
	other.UserID = 8
	if _, _, err := s.Reserve(context.Background(), &other, row); err == nil {
		t.Fatal("cross owner")
	}
	stale := *r
	stale.LeaseToken = "stale"
	if err := s.Complete(context.Background(), &stale, first); !errors.Is(err, ErrConflict) {
		t.Fatal("stale completion", err)
	}
}
func TestHouseImageSQLReadCarriesOwnerAndFrozenVersion(t *testing.T) {
	r, task, p := imageFixture(t)
	row := HouseImageRecord{ReportID: r.ID, UserID: r.UserID, InputHash: r.InputHash, ConfirmationHash: r.ConfirmationHash, RequestHash: strings.Repeat("c", 64), Identity: p.identity, Task: task, Status: "submitting"}
	raw, _ := json.Marshal(row)
	state := &houseImageSQLState{raw: raw}
	s := newHouseImageSQLStore(t, state)
	if _, err := s.Get(context.Background(), r, task.ID); err != nil {
		t.Fatal(err)
	}
	q := state.queries[0]
	for _, condition := range []string{"i.report_id=?", "i.user_id=?", "i.image_id=?", "r.input_hash=?", "r.protocol_version=?", "d.confirmation_hash=?"} {
		if !strings.Contains(q, condition) {
			t.Fatalf("missing %s", condition)
		}
	}
	other := *r
	other.InputHash = strings.Repeat("f", 64)
	if _, err := s.Get(context.Background(), &other, task.ID); err == nil {
		t.Fatal("different input read accepted")
	}
}
