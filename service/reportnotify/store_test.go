package reportnotify

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// The scripted driver checks SQL contracts without claiming real MySQL locking
// behavior. Deployment must separately exercise these migrations and races.
type sqlStep struct {
	kind     string
	contains []string
	args     func([]driver.NamedValue) error
	columns  []string
	rows     [][]driver.Value
	affected int64
	err      error
}
type sqlScript struct {
	mu      sync.Mutex
	steps   []sqlStep
	commits int
}

func (s *sqlScript) next(kind, query string, args []driver.NamedValue) (sqlStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.steps) == 0 {
		return sqlStep{}, fmt.Errorf("unexpected SQL operation %s", kind)
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if step.kind != kind {
		return step, fmt.Errorf("expected %s, got %s", step.kind, kind)
	}
	for _, part := range step.contains {
		if !strings.Contains(query, part) {
			return step, fmt.Errorf("SQL missing required clause %s", part)
		}
	}
	if step.args != nil {
		if err := step.args(args); err != nil {
			return step, err
		}
	}
	return step, step.err
}

type testConnector struct{ script *sqlScript }

func (c testConnector) Connect(context.Context) (driver.Conn, error) {
	return &testConnection{script: c.script}, nil
}
func (c testConnector) Driver() driver.Driver { return testDriver{} }

type testDriver struct{}

func (testDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector required") }

type testConnection struct{ script *sqlScript }

func (c *testConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements unused")
}
func (c *testConnection) Close() error              { return nil }
func (c *testConnection) Begin() (driver.Tx, error) { return &testTransaction{script: c.script}, nil }
func (c *testConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *testConnection) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	step, err := c.script.next("exec", query, args)
	return driver.RowsAffected(step.affected), err
}
func (c *testConnection) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	step, err := c.script.next("query", query, args)
	return &testRows{columns: step.columns, rows: step.rows}, err
}

type testTransaction struct{ script *sqlScript }

func (tx *testTransaction) Commit() error   { tx.script.commits++; return nil }
func (tx *testTransaction) Rollback() error { return nil }

type testRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *testRows) Columns() []string { return r.columns }
func (r *testRows) Close() error      { return nil }
func (r *testRows) Next(values []driver.Value) error {
	if r.index == len(r.rows) {
		return io.EOF
	}
	copy(values, r.rows[r.index])
	r.index++
	return nil
}
func scriptedStore(t *testing.T, steps ...sqlStep) (*SQLStore, *sqlScript) {
	t.Helper()
	script := &sqlScript{steps: steps}
	db := sql.OpenDB(testConnector{script: script})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		db.Close()
		if len(script.steps) != 0 {
			t.Errorf("%d expected SQL calls remain", len(script.steps))
		}
	})
	store, err := NewSQLStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return store, script
}

func TestSQLCatchUpIsDurableUniqueAndUnconfiguredIsManual(t *testing.T) {
	store, _ := scriptedStore(t, sqlStep{kind: "exec", affected: 1,
		contains: []string{"INSERT INTO report_notifications", "SHA2(CONCAT('report_ready:'", "r.status='ready'", "r.paid=1", "r.protocol_version='report-house-v1'", "NOT EXISTS", "LIMIT ?", "ON DUPLICATE KEY UPDATE"},
		args: func(args []driver.NamedValue) error {
			if args[0].Value != StateManualRequired || args[1].Value != "gateway_unconfigured" || args[2].Value != int64(BatchSize) {
				return errors.New("manual outbox contract changed")
			}
			return nil
		}}, sqlStep{kind: "exec", contains: []string{"status='pending'", "status='manual_required'", "LIMIT ?"}})
	count, err := store.CatchUp(context.Background(), false, BatchSize)
	if err != nil || count != 1 {
		t.Fatalf("catchup count=%d err=%v", count, err)
	}
}

func TestSQLConfiguredCatchupReactivatesOnlyUnconfiguredManualEvents(t *testing.T) {
	store, _ := scriptedStore(t,
		sqlStep{kind: "exec", affected: 0, contains: []string{"INSERT INTO report_notifications"}},
		sqlStep{kind: "exec", affected: 1, contains: []string{"status='manual_required' AND error_code='gateway_unconfigured'", "LIMIT ?"}})
	if count, err := store.CatchUp(context.Background(), true, BatchSize); err != nil || count != 1 {
		t.Fatalf("reactivation=%d %v", count, err)
	}
}

func TestSQLClaimFencesOwnerAndTurnsExpiredSendingIntoLookupOnly(t *testing.T) {
	for _, previous := range []string{StatePending, StateUncertain} {
		t.Run(previous, func(t *testing.T) {
			store, script := scriptedStore(t,
				sqlStep{kind: "exec", contains: []string{"status='uncertain'", "status='sending' AND lease_until<NOW(6)"}},
				sqlStep{kind: "query", contains: []string{"r.user_id=n.user_id", "r.bound=1", "COALESCE(r.external_user_id,'')<>''", "LIMIT 1 FOR UPDATE", "n.status IN ('pending','uncertain')"},
					columns: []string{"event_id", "report_id", "user_id", "external_user_id", "status", "attempt_count"},
					rows:    [][]driver.Value{{EventID("report-1"), "report-1", int64(7), "real-bound-owner", previous, int64(1)}}},
				sqlStep{kind: "exec", affected: 1, contains: []string{"lease_token=?", "lease_until=DATE_ADD", "WHERE event_id=? AND status=?"},
					args: func(args []driver.NamedValue) error {
						if args[1].Value != "real-bound-owner" {
							return errors.New("recipient not taken from owner binding")
						}
						if previous == StateUncertain && (args[0].Value != StateUncertain || args[2].Value != int64(0)) {
							return errors.New("uncertain job was prepared for resend")
						}
						return nil
					}})
			job, err := store.Claim(context.Background(), LeaseDuration)
			if err != nil || job.Reconcile != (previous == StateUncertain) || script.commits != 1 {
				t.Fatalf("claim=%+v %v commits=%d", job, err, script.commits)
			}
		})
	}
}

func TestSQLUnboundReportStaysUnclaimedForLaterBinding(t *testing.T) {
	store, script := scriptedStore(t,
		sqlStep{kind: "exec", contains: []string{"status='sending'"}},
		sqlStep{kind: "query", contains: []string{"r.bound=1", "r.user_id=n.user_id"}, columns: []string{"event_id", "report_id", "user_id", "external_user_id", "status", "attempt_count"}})
	if _, err := store.Claim(context.Background(), LeaseDuration); !errors.Is(err, ErrNoJob) || script.commits != 1 {
		t.Fatalf("unbound claim=%v commits=%d", err, script.commits)
	}
}

func TestSQLFinishRejectsExpiredOrReplacedLease(t *testing.T) {
	store, _ := scriptedStore(t, sqlStep{kind: "exec", affected: 0,
		contains: []string{"lease_token=? AND lease_until>NOW(6)", "lease_token=NULL,lease_until=NULL"}})
	err := store.Finish(context.Background(), readyJob(false), Outcome{State: StateSent, Receipt: "receipt"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale completion=%v", err)
	}
}

func TestSQLUncertainOutcomeSchedulesLookupWithoutResend(t *testing.T) {
	store, _ := scriptedStore(t, sqlStep{kind: "exec", affected: 1,
		contains: []string{"next_attempt_at=DATE_ADD"}, args: func(args []driver.NamedValue) error {
			if args[0].Value != StateUncertain || args[3].Value != LookupDelay.Microseconds() || args[5].Value != StateUncertain {
				return errors.New("lookup completion contract changed")
			}
			return nil
		}})
	if err := store.Finish(context.Background(), readyJob(true), Outcome{State: StateUncertain, ErrorCode: "delivery_unconfirmed"}); err != nil {
		t.Fatal(err)
	}
}

func TestSQLSchemaVerificationAndErrorsAreExplicit(t *testing.T) {
	store, _ := scriptedStore(t, sqlStep{kind: "query", columns: []string{"event_id"}, contains: []string{"JOIN building_reports", "LIMIT 0"}})
	if err := store.VerifySchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	broken, _ := scriptedStore(t, sqlStep{kind: "exec", err: errors.New("private database error")})
	if _, err := broken.CatchUp(context.Background(), false, BatchSize); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("DB failure=%v", err)
	}
}
