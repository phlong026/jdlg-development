package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type handoffDriver struct{}
type handoffConn struct{}
type handoffRows struct{ values []driver.Value }

func (handoffDriver) Open(string) (driver.Conn, error)  { return handoffConn{}, nil }
func (handoffConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (handoffConn) Close() error                        { return nil }
func (handoffConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (handoffConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	for _, required := range []string{"JSON_EXTRACT(e.payload", "e.user_id=u.id", "e.source='share_tracking_my'", "COALESCE(u.status,'active')='active'"} {
		if !strings.Contains(query, required) {
			return nil, errors.New("missing trusted handoff relationship: " + required)
		}
	}
	actor := args[0].Value.(int64)
	status := ""
	assigned := ""
	if actor == 11 {
		status = "contacted"
		assigned = "sales"
	}
	return &handoffRows{[]driver.Value{int64(77), true, int64(4), BusinessContactConsentPurpose, BusinessContactConsentVersion, true, status, assigned, nil}}, nil
}
func (*handoffRows) Columns() []string {
	return []string{"id", "accepted", "revision", "purpose", "version", "phone_exists", "status", "assigned", "next"}
}
func (*handoffRows) Close() error { return nil }
func (r *handoffRows) Next(out []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(out, r.values)
	r.values = nil
	return nil
}
func init() { sql.Register("my-customer-handoff-test", handoffDriver{}) }
func TestMyCustomerHandoffOnlyExposesActorsOwnTouchAndNoPhoneOrLeadID(t *testing.T) {
	db, _ := sql.Open("my-customer-handoff-test", "")
	defer db.Close()
	m := NewCustomerServiceModel(db)
	mine, err := m.MyCustomerHandoff(context.Background(), 11, 77)
	if err != nil || !mine.Converted || mine.Status != "contacted" || !mine.ContactReady {
		t.Fatalf("mine=%+v err=%v", mine, err)
	}
	other, err := m.MyCustomerHandoff(context.Background(), 22, 77)
	if err != nil || other.Converted || other.Assigned {
		t.Fatalf("other actor observed handoff: %+v %v", other, err)
	}
}

type touchFixture struct {
	seen   map[int64]bool
	writes int
}
type touchDriver struct{ f *touchFixture }
type touchConnector struct{ f *touchFixture }

func (c touchConnector) Connect(context.Context) (driver.Conn, error) { return touchConn{c.f}, nil }
func (c touchConnector) Driver() driver.Driver                        { return touchDriver{c.f} }

type touchConn struct{ f *touchFixture }
type touchTx struct{}

func (d touchDriver) Open(string) (driver.Conn, error)  { return touchConn{d.f}, nil }
func (c touchConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c touchConn) Close() error                        { return nil }
func (c touchConn) Begin() (driver.Tx, error)           { return touchTx{}, nil }
func (touchTx) Commit() error                           { return nil }
func (touchTx) Rollback() error                         { return nil }
func (c touchConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(q, "JSON_EXTRACT(payload,'$.converted_by')") || len(args) != 5 {
		return nil, errors.New("touch dedupe missing actor")
	}
	return &touchBoolRows{value: c.f.seen[args[4].Value.(int64)]}, nil
}
func (c touchConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(q, "INSERT INTO customer_service_events") {
		return nil, errors.New("unexpected touch mutation")
	}
	var payload struct {
		Actor int64 `json:"converted_by"`
	}
	raw, ok := args[8].Value.(string)
	if !ok {
		raw = string(args[8].Value.([]byte))
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	c.f.seen[payload.Actor] = true
	c.f.writes++
	return touchResult{}, nil
}

type touchResult struct{}

func (touchResult) LastInsertId() (int64, error) { return 1, nil }
func (touchResult) RowsAffected() (int64, error) { return 1, nil }

type touchBoolRows struct {
	value bool
	done  bool
}

func (*touchBoolRows) Columns() []string { return []string{"exists"} }
func (*touchBoolRows) Close() error      { return nil }
func (r *touchBoolRows) Next(v []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	v[0] = r.value
	return nil
}
func TestMyTrackingTouchDedupeKeepsAnotherDesignerFirstHandoff(t *testing.T) {
	f := &touchFixture{seen: map[int64]bool{}}
	db := sql.OpenDB(touchConnector{f})
	defer db.Close()
	m := NewCustomerServiceModel(db)
	for _, actor := range []int64{7, 7, 8, 8} {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		event := &CustomerServiceEventCreateInput{UserID: 101, LeadID: 12, EventType: "share_intent_convert", Source: "share_tracking_my", Payload: map[string]interface{}{"converted_by": actor}}
		if err = m.appendMyTrackingTouchOn(tx, event); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if f.writes != 2 || !f.seen[7] || !f.seen[8] {
		t.Fatalf("touches lost another actor or duplicated retries: %+v", f)
	}
}
