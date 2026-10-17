package tracking

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

// Deterministic SQL boundary fixture: the production SQLStore performs locking,
// builds owner/creation predicates and enforces the real row/payload budgets.
// The fixture validates the emitted SQL and models its selected rows; it does
// not claim to execute MySQL's JSON/date functions or benchmark database scans.
type catalogSQLState struct {
	t                                *testing.T
	kind                             string
	asOf                             time.Time
	payloadRows, payloadBytes, calls int
}
type catalogSQLConnector struct{ state *catalogSQLState }

func (c catalogSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return catalogSQLConn{c.state}, nil
}
func (catalogSQLConnector) Driver() driver.Driver { return scopedSQLDriver{} }

type catalogSQLConn struct{ state *catalogSQLState }

func (catalogSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (catalogSQLConn) Close() error              { return nil }
func (catalogSQLConn) Begin() (driver.Tx, error) { return mediaReproTx{}, nil }
func (c catalogSQLConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (catalogSQLConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (c catalogSQLConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT tenant_id") {
		return &mediaReproRows{lock: true, n: 1}, nil
	}
	c.state.calls++
	if len(args) > 2 && args[2].Value == "share" {
		// Share-metric tally bound to the same tenant/owner/window predicates.
		// The fixture models no share rows, so the page resolves empty.
		return &catalogSQLRows{s: c.state, n: 0}, nil
	}
	if !strings.Contains(q, "r.tenant_id=? AND r.kind=?") || !strings.Contains(q, "r.owner_id IN (?)") || !strings.Contains(q, catalogCreatedAtSQL+"<=?") {
		c.state.t.Fatalf("missing tenant/owner/immutable catalog boundary: %s", q)
	}
	if len(args) != 6 || args[1].Value != "org-a" || args[3].Value != "owner-a" || args[4].Value != c.state.asOf.UTC().Format("2006-01-02 15:04:05.999999") {
		c.state.t.Fatalf("unbound catalog boundary: %+v", args)
	}
	if n, ok := args[5].Value.(int64); !ok || n > maxQueryRecords+1 {
		c.state.t.Fatal("unbounded catalog read", args)
	}
	rows := &catalogSQLRows{s: c.state}
	if args[2].Value == c.state.kind {
		rows.n = 2
	}
	// There are 20,001 further catalog records with 60,000-byte bodies, created
	// after as_of. The declared SQL boundary excludes them before row transfer.
	return rows, nil
}

type catalogSQLRows struct {
	s    *catalogSQLState
	n, i int
}

func (*catalogSQLRows) Columns() []string {
	return []string{"kind", "record_id", "owner_id", "visitor_key", "session_id", "content_id", "share_id", "occurred_at", "payload"}
}
func (*catalogSQLRows) Close() error { return nil }
func (r *catalogSQLRows) Next(v []driver.Value) error {
	if r.i >= r.n {
		return io.EOF
	}
	r.i++
	id := "content-b"
	if r.i == 2 {
		id = "content-c"
	}
	created := r.s.asOf.Add(-time.Hour)
	updated := r.s.asOf.Add(time.Hour)
	c := Content{ID: id, Owner: "owner-a", Status: "draft", CreatedAt: &created, Title: "edited after snapshot"}
	payload := marshal(c)
	r.s.payloadRows++
	r.s.payloadBytes += len(payload)
	v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8] = r.s.kind, id, "owner-a", "", "", "", "", updated.Format("2006-01-02 15:04:05.999999"), payload
	return nil
}
func TestCatalogSQLSnapshotMembershipIsBoundBeforeTransfer(t *testing.T) {
	for _, kind := range []string{"content", "legacy_catalog"} {
		t.Run(kind, func(t *testing.T) {
			at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
			state := &catalogSQLState{t: t, kind: kind, asOf: at}
			db := sql.OpenDB(catalogSQLConnector{state})
			defer db.Close()
			s := New(&SQLStore{DB: db}, Config{Tenant: "org-a"})
			s.Now = func() time.Time { return at.Add(time.Hour) }
			scope := Scope{Tenant: "org-a", Actor: 1, Owners: []string{"owner-a"}, Manage: true}
			f := Filter{AsOf: at, Limit: 1}
			first, err := s.Contents(context.Background(), scope, f, "")
			if err != nil {
				t.Fatal(err)
			}
			f.Cursor = first.Cursor
			second, err := s.Contents(context.Background(), scope, f, "")
			if err != nil {
				t.Fatal(err)
			}
			if first.Items.([]Content)[0].ID != "content-b" || second.Items.([]Content)[0].ID != "content-c" || second.Cursor != "" {
				t.Fatal(first, second)
			}
			if state.calls != 6 || state.payloadRows != 4 || state.payloadBytes > 4096 {
				t.Fatalf("unrelated catalog entered budget: %+v", state)
			}
			t.Logf("20,001 post-snapshot 60KB rows excluded; transferred rows=%d bytes=%d across two pages", state.payloadRows, state.payloadBytes)
		})
	}
}
