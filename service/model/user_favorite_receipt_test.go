package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// Executes the real model's SQL/transaction code with deterministic source rows;
// this does not substitute for live MySQL concurrency acceptance.
type favoriteReceiptState struct {
	current              *UserFavorite
	next                 int64
	now                  time.Time
	failRead, failCommit bool
	steps                []string
}
type favoriteReceiptConnector struct{ state *favoriteReceiptState }

func (c favoriteReceiptConnector) Connect(context.Context) (driver.Conn, error) {
	return &favoriteReceiptConn{state: c.state}, nil
}
func (favoriteReceiptConnector) Driver() driver.Driver { return favoriteReceiptDriver{} }

type favoriteReceiptDriver struct{}

func (favoriteReceiptDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type favoriteReceiptConn struct {
	state   *favoriteReceiptState
	pending *UserFavorite
	inTx    bool
}

func (c *favoriteReceiptConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *favoriteReceiptConn) Close() error                        { return nil }
func (c *favoriteReceiptConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *favoriteReceiptConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.inTx = true
	c.pending = c.state.current
	c.state.steps = append(c.state.steps, "begin")
	return favoriteReceiptTx{c}, nil
}
func (c *favoriteReceiptConn) ExecContext(_ context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "DELETE FROM user_favorites") {
		c.state.current = nil
		return driver.RowsAffected(1), nil
	}
	if !c.inTx || !strings.HasPrefix(q, "INSERT IGNORE INTO user_favorites") || len(a) != 3 {
		return nil, errors.New("unexpected source write")
	}
	c.state.steps = append(c.state.steps, "insert")
	if c.pending != nil {
		return driver.RowsAffected(0), nil
	}
	c.state.next++
	c.pending = &UserFavorite{ID: c.state.next, UserID: a[0].Value.(int64), TargetType: a[1].Value.(string), TargetID: a[2].Value.(int64), CreatedAt: c.state.now}
	return driver.RowsAffected(1), nil
}
func (c *favoriteReceiptConn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	if !c.inTx || !strings.HasSuffix(q, "LOCK IN SHARE MODE") || len(a) != 3 {
		return nil, errors.New("receipt not transaction locked")
	}
	c.state.steps = append(c.state.steps, "read-locked")
	if c.state.failRead {
		return nil, errors.New("read failed")
	}
	return &favoriteReceiptRows{row: c.pending}, nil
}

type favoriteReceiptTx struct{ c *favoriteReceiptConn }

func (tx favoriteReceiptTx) Commit() error {
	tx.c.state.steps = append(tx.c.state.steps, "commit")
	tx.c.inTx = false
	if tx.c.state.failCommit {
		return errors.New("commit failed")
	}
	tx.c.state.current = tx.c.pending
	return nil
}
func (tx favoriteReceiptTx) Rollback() error {
	tx.c.inTx = false
	tx.c.pending = nil
	tx.c.state.steps = append(tx.c.state.steps, "rollback")
	return nil
}

type favoriteReceiptRows struct {
	row  *UserFavorite
	done bool
}

func (*favoriteReceiptRows) Columns() []string {
	return []string{"id", "user_id", "target_type", "target_id", "created_at"}
}
func (*favoriteReceiptRows) Close() error { return nil }
func (r *favoriteReceiptRows) Next(out []driver.Value) error {
	if r.done || r.row == nil {
		return io.EOF
	}
	r.done = true
	out[0] = r.row.ID
	out[1] = r.row.UserID
	out[2] = r.row.TargetType
	out[3] = r.row.TargetID
	out[4] = r.row.CreatedAt
	return nil
}
func TestFavoriteCommittedReceiptSurvivesIgnoreRetryAndChangesAfterRemove(t *testing.T) {
	state := &favoriteReceiptState{now: time.Date(2026, 10, 10, 23, 59, 0, 0, time.UTC)}
	db := sql.OpenDB(favoriteReceiptConnector{state})
	defer db.Close()
	m := NewUserFavoriteModel(db)
	ctx := context.Background()
	first, e := m.AddWithReceipt(ctx, 7, "template", 12)
	if e != nil {
		t.Fatal(e)
	}
	state.now = state.now.Add(48 * time.Hour)
	again, e := m.AddWithReceipt(ctx, 7, "template", 12)
	if e != nil || *first != *again {
		t.Fatalf("INSERT IGNORE replaced durable receipt: %+v %+v %v", first, again, e)
	}
	if got := strings.Join(state.steps, ","); got != "begin,insert,read-locked,commit,begin,insert,read-locked,commit" {
		t.Fatal(got)
	}
	if e = m.Remove(7, "template", 12); e != nil {
		t.Fatal(e)
	}
	added, e := m.AddWithReceipt(ctx, 7, "template", 12)
	if e != nil || added.ID == first.ID || !added.CreatedAt.Equal(state.now) {
		t.Fatalf("re-add reused deleted success: %+v %v", added, e)
	}
}
func TestFavoriteReceiptNeverEscapesReadOrCommitFailure(t *testing.T) {
	for _, stage := range []string{"read", "commit"} {
		t.Run(stage, func(t *testing.T) {
			state := &favoriteReceiptState{now: time.Now(), failRead: stage == "read", failCommit: stage == "commit"}
			db := sql.OpenDB(favoriteReceiptConnector{state})
			defer db.Close()
			row, e := NewUserFavoriteModel(db).AddWithReceipt(context.Background(), 7, "template", 12)
			if e == nil || row != nil || state.current != nil {
				t.Fatalf("uncommitted receipt returned: %+v %v", row, e)
			}
		})
	}
}
