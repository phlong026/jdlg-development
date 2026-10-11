package tracking

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestSourceReaderReusesTransactionThroughQueryWrappers(t *testing.T) {
	state := &scopedSQLState{t: t}
	db := sql.OpenDB(scopedSQLConnector{state: state})
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	store := &SQLStore{DB: db}
	err := store.Transaction(ctx, "tenant", func(r Repository) error {
		wrapped := boundedQueryRepository(r)
		reader := SourceReadDB(sourceReadContext(ctx, wrapped), db)
		if reader != r.(*sqlRepo).tx {
			t.Fatal("source callback borrowed a second connection")
		}
		var owner string
		if e := reader.QueryRowContext(ctx, "SELECT tenant_id FROM tracking_tenant_lock WHERE tenant_id=? FOR UPDATE", "tenant").Scan(&owner); e != nil {
			return e
		}
		if owner != "tenant" {
			t.Fatal(owner)
		}
		rows, e := reader.QueryContext(ctx, "SELECT tenant_id FROM tracking_tenant_lock WHERE tenant_id=? FOR UPDATE", "tenant")
		if e != nil {
			return e
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatal("query source unavailable")
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if !state.committed {
		t.Fatal("transaction did not commit")
	}
	if db.Stats().WaitCount != 0 {
		t.Fatal("source callback waited for another connection")
	}
}
func TestSourceReaderFallbackOnlyWithoutTransaction(t *testing.T) {
	if SourceReadDB(context.Background(), nil) != nil {
		t.Fatal("nil source should remain unavailable")
	}
	db := sql.OpenDB(scopedSQLConnector{state: &scopedSQLState{t: t}})
	defer db.Close()
	if SourceReadDB(context.Background(), db) != db {
		t.Fatal("outside transaction should use explicit DB")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sourceReadContext(ctx, &memoryQuerySnapshot{}).Err() != context.Canceled {
		t.Fatal("caller cancellation lost")
	}
}
