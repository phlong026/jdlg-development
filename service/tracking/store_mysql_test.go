package tracking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// This test is opt-in because it needs a disposable, migrated MySQL database.
// It refuses non-loopback DSNs so a developer cannot point it at production.
func TestSQLStoreSerializesConcurrentTenantTransactionsMySQL(t *testing.T) {
	dsn := os.Getenv("JDLG_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set JDLG_TEST_MYSQL_DSN to a disposable loopback MySQL database")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse JDLG_TEST_MYSQL_DSN: %v", err)
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil || cfg.Net != "tcp" || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		t.Fatalf("JDLG_TEST_MYSQL_DSN must use loopback TCP, got network=%q", cfg.Net)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(32)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		t.Fatalf("ping disposable MySQL: %v", err)
	}
	cleanup := func(query string, args ...any) {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
		defer cleanupCancel()
		if _, cleanupErr := db.ExecContext(cleanupCtx, query, args...); cleanupErr != nil {
			t.Errorf("clean up tenant lock fixture: %v", cleanupErr)
		}
	}

	tenant := fmt.Sprintf("local-lock-test-%d", time.Now().UnixNano())
	defer cleanup("DELETE FROM tracking_tenant_lock WHERE tenant_id=?", tenant)
	store := NewSQLStore(db, 0, 0)

	const writers = 24
	start := make(chan struct{})
	errs := make(chan error, writers)
	var active atomic.Int32
	var overlapped atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- store.Transaction(ctx, tenant, func(Repository) error {
				if active.Add(1) != 1 {
					overlapped.Store(true)
				}
				defer active.Add(-1)
				time.Sleep(2 * time.Millisecond)
				return nil
			})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent tenant transaction failed: %v", err)
		}
	}
	if overlapped.Load() {
		t.Fatal("same-tenant callbacks were not serialized")
	}
	var lockRows int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tracking_tenant_lock WHERE tenant_id=?", tenant).Scan(&lockRows); err != nil || lockRows != 1 {
		t.Fatalf("first writer did not create exactly one tenant lock row: rows=%d err=%v", lockRows, err)
	}

	rollbackTenant := tenant + "-rollback"
	defer cleanup("DELETE FROM tracking_tenant_lock WHERE tenant_id=?", rollbackTenant)
	wantRollback := errors.New("force rollback")
	if err = store.Transaction(ctx, rollbackTenant, func(Repository) error { return wantRollback }); !errors.Is(err, wantRollback) {
		t.Fatalf("forced rollback returned %v", err)
	}
	if err = store.Transaction(ctx, rollbackTenant, func(Repository) error { return nil }); err != nil {
		t.Fatalf("rollback did not release tenant lock: %v", err)
	}

	tenantA, tenantB := tenant+"-a", tenant+"-b"
	defer cleanup("DELETE FROM tracking_tenant_lock WHERE tenant_id IN (?,?)", tenantA, tenantB)
	enteredA := make(chan struct{})
	releaseA := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseA) }) }
	defer release()
	errA := make(chan error, 1)
	go func() {
		errA <- store.Transaction(ctx, tenantA, func(Repository) error {
			close(enteredA)
			<-releaseA
			return nil
		})
	}()
	select {
	case <-enteredA:
	case err = <-errA:
		t.Fatalf("held tenant transaction failed before callback: %v", err)
	case <-ctx.Done():
		t.Fatalf("held tenant transaction did not enter callback: %v", ctx.Err())
	}
	errB := make(chan error, 1)
	go func() { errB <- store.Transaction(ctx, tenantB, func(Repository) error { return nil }) }()
	select {
	case err = <-errB:
		if err != nil {
			t.Fatalf("different-tenant transaction failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("different tenant was blocked by the held tenant lock")
	}
	release()
	if err = <-errA; err != nil {
		t.Fatalf("held tenant transaction failed: %v", err)
	}
}
