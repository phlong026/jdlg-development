package main

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

type scopeAuthDriver struct{}

type scopeAuthConn struct{ role string }

type scopeAuthRows struct {
	owners []string
	index  int
}

func (scopeAuthDriver) Open(role string) (driver.Conn, error) { return scopeAuthConn{role: role}, nil }
func (scopeAuthConn) Prepare(string) (driver.Stmt, error)     { return nil, errors.New("not supported") }
func (scopeAuthConn) Close() error                            { return nil }
func (scopeAuthConn) Begin() (driver.Tx, error)               { return nil, errors.New("not supported") }
func (c scopeAuthConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) != 2 || args[0].Value != int64(7) || args[1].Value != "org-a" {
		return nil, errors.New("authorization query lost actor or tenant")
	}
	platformOnly := strings.Contains(query, "a.scope='platform_admin'") && !strings.Contains(query, "crm_only")
	switch c.role {
	case "platform_admin":
		if !platformOnly {
			return nil, errors.New("authorization query does not require platform_admin")
		}
		return &scopeAuthRows{owners: []string{"owner-a"}}, nil
	case "crm_only":
		if strings.Contains(query, "crm_only") {
			return &scopeAuthRows{owners: []string{"owner-a"}}, nil
		}
		return &scopeAuthRows{}, nil
	default:
		return nil, errors.New("unknown test role")
	}
}
func (r *scopeAuthRows) Columns() []string { return []string{"owner_id"} }
func (r *scopeAuthRows) Close() error      { return nil }
func (r *scopeAuthRows) Next(values []driver.Value) error {
	if r.index >= len(r.owners) {
		return io.EOF
	}
	values[0] = r.owners[r.index]
	r.index++
	return nil
}

func init() { sql.Register("reconcile-summary-scope-auth", scopeAuthDriver{}) }

func TestParseOptionsDefaultsToDryRunAndAcceptsResumeCursor(t *testing.T) {
	options, err := parseOptions([]string{
		"--tenant", "org-a",
		"--actor", "7",
		"--owners", "owner-a, owner-b,owner-a",
		"--as-of", "2026-10-22T10:00:00+08:00",
		"--after-at", "2026-10-22T09:00:00.123456Z",
		"--after-id", "session-0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.reconcile.Apply || options.reconcile.Limit != 50 || len(options.owners) != 2 {
		t.Fatalf("options = %+v", options)
	}
	if want := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC); !options.reconcile.AsOf.Equal(want) {
		t.Fatalf("as_of = %s, want %s", options.reconcile.AsOf, want)
	}
	if options.reconcile.AfterID != "session-0001" || options.reconcile.AfterAt.IsZero() {
		t.Fatalf("cursor = %s %s", options.reconcile.AfterAt, options.reconcile.AfterID)
	}
}

func TestParseOptionsRejectsImplicitScopeAndInvalidTime(t *testing.T) {
	for _, args := range [][]string{
		{"--tenant", "org-a", "--actor", "7"},
		{"--tenant", "org-a", "--actor", "7", "--owners", "owner-a", "--as-of", "yesterday"},
		{"--tenant", "org-a", "--actor", "0", "--owners", "owner-a"},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
}

func TestLoadAuthorizedScopeRequiresPlatformAdmin(t *testing.T) {
	for _, test := range []struct {
		role    string
		wantErr bool
	}{
		{role: "platform_admin"},
		{role: "crm_only", wantErr: true},
	} {
		t.Run(test.role, func(t *testing.T) {
			db, err := sql.Open("reconcile-summary-scope-auth", test.role)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			scope, err := loadAuthorizedScope(context.Background(), db, "org-a", 7, []string{"owner-a"})
			if test.wantErr {
				if err == nil {
					t.Fatalf("%s received maintenance scope: %+v", test.role, scope)
				}
				return
			}
			if err != nil || !scope.Manage || !scope.Rules || !scope.Allows("owner-a") {
				t.Fatalf("platform scope=%+v err=%v", scope, err)
			}
		})
	}
}
