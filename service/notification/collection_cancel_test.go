package notification

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"
)

type collectionCancelConnector struct{ state *collectionCancelState }
type collectionCancelState struct {
	queries []string
	args    [][]driver.NamedValue
}

func (c collectionCancelConnector) Connect(context.Context) (driver.Conn, error) {
	return collectionCancelConn{state: c.state}, nil
}
func (collectionCancelConnector) Driver() driver.Driver { return collectionCancelDriver{} }

type collectionCancelDriver struct{}

func (collectionCancelDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type collectionCancelConn struct{ state *collectionCancelState }

func (collectionCancelConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (collectionCancelConn) Close() error { return nil }
func (c collectionCancelConn) Begin() (driver.Tx, error) {
	return collectionCancelTx{}, nil
}
func (c collectionCancelConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c collectionCancelConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.queries = append(c.state.queries, query)
	c.state.args = append(c.state.args, append([]driver.NamedValue(nil), args...))
	return driver.RowsAffected(1), nil
}

type collectionCancelTx struct{}

func (collectionCancelTx) Commit() error   { return nil }
func (collectionCancelTx) Rollback() error { return nil }

func TestCancelCollectionPendingTxUsesCollectionEventContext(t *testing.T) {
	state := &collectionCancelState{}
	db := sql.OpenDB(collectionCancelConnector{state: state})
	defer db.Close()
	store, err := NewSQLStore(db, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if err = store.CancelCollectionPendingTx(context.Background(), tx, "collection-a", time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if len(state.queries) != 3 {
		t.Fatalf("queries=%d want=3", len(state.queries))
	}
	for i, query := range state.queries {
		if !strings.Contains(query, "JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.collection_id'))=?") {
			t.Fatalf("query %d does not scope collection event: %s", i, query)
		}
		if strings.Contains(query, "n.content_id=?") || strings.Contains(query, "content_id=?") {
			t.Fatalf("query %d would cancel source content outside this collection: %s", i, query)
		}
		args := state.args[i]
		if len(args) == 0 || args[len(args)-1].Value != "collection-a" {
			t.Fatalf("query %d args=%#v", i, args)
		}
	}
	if !strings.Contains(state.queries[0], "mini_notification_event_jobs j JOIN tracking_records r") {
		t.Fatalf("job query=%s", state.queries[0])
	}
	if !strings.Contains(state.queries[1], "mini_notifications n JOIN tracking_records r") {
		t.Fatalf("inbox query=%s", state.queries[1])
	}
	if !strings.Contains(state.queries[2], "mini_notification_deliveries d JOIN mini_notifications n") || !strings.Contains(state.queries[2], "JOIN tracking_records r") {
		t.Fatalf("delivery query=%s", state.queries[2])
	}
}

func TestCancelCollectionPendingTxRejectsInvalidInput(t *testing.T) {
	store := &SQLStore{tenant: "tenant-a"}
	if err := store.CancelCollectionPendingTx(context.Background(), nil, "collection-a", time.Now()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil tx err=%v", err)
	}
}
