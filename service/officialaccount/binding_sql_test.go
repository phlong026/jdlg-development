package officialaccount

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type bindingSQLFixture struct {
	mu         sync.Mutex
	stateHash  string
	userID     int64
	expiresAt  time.Time
	consumedAt *time.Time
	binding    Binding
}

type bindingSQLConnector struct{ fixture *bindingSQLFixture }
type bindingSQLDriver struct{}
type bindingSQLConn struct{ fixture *bindingSQLFixture }
type bindingSQLTx struct{}
type bindingSQLRows struct {
	columns []string
	values  [][]driver.Value
}

func (c bindingSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &bindingSQLConn{fixture: c.fixture}, nil
}
func (c bindingSQLConnector) Driver() driver.Driver       { return bindingSQLDriver{} }
func (bindingSQLDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (c *bindingSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (c *bindingSQLConn) Close() error              { return nil }
func (c *bindingSQLConn) Begin() (driver.Tx, error) { return bindingSQLTx{}, nil }
func (c *bindingSQLConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return bindingSQLTx{}, nil
}
func (bindingSQLTx) Commit() error          { return nil }
func (bindingSQLTx) Rollback() error        { return nil }
func (r *bindingSQLRows) Columns() []string { return r.columns }
func (r *bindingSQLRows) Close() error      { return nil }
func (r *bindingSQLRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}

func values(args []driver.NamedValue) []driver.Value {
	out := make([]driver.Value, len(args))
	for i := range args {
		out[i] = args[i].Value
	}
	return out
}

func (c *bindingSQLConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.fixture.mu.Lock()
	defer c.fixture.mu.Unlock()
	v := values(args)
	switch {
	case strings.HasPrefix(query, "INSERT INTO official_account_binding_intents"):
		c.fixture.stateHash, c.fixture.userID = v[0].(string), v[1].(int64)
		c.fixture.expiresAt = v[2].(time.Time)
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(query, "UPDATE official_account_binding_intents"):
		at := v[0].(time.Time)
		c.fixture.consumedAt = &at
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(query, "UPDATE official_account_bindings SET active=FALSE"):
		if c.fixture.binding.AppID != v[1].(string) || c.fixture.binding.UserID != v[2].(int64) || !c.fixture.binding.Active {
			return driver.RowsAffected(0), nil
		}
		c.fixture.binding.Active = false
		c.fixture.binding.Subscribed = false
		c.fixture.binding.Version++
		c.fixture.binding.UpdatedAt = v[0].(time.Time)
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(query, "DELETE FROM official_account_bindings"):
		c.fixture.binding = Binding{}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(query, "INSERT INTO official_account_bindings"):
		c.fixture.binding = Binding{AppID: v[0].(string), UserID: v[1].(int64), OpenID: v[2].(string), UnionID: v[3].(string), Version: v[4].(int64), Active: true, Subscribed: v[5].(bool), UpdatedAt: v[6].(time.Time)}
		return driver.RowsAffected(1), nil
	default:
		return nil, errors.New("unexpected exec: " + query)
	}
}

func (c *bindingSQLConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.fixture.mu.Lock()
	defer c.fixture.mu.Unlock()
	v := values(args)
	switch {
	case strings.HasPrefix(query, "SELECT user_id,expires_at,consumed_at"):
		if c.fixture.stateHash == "" || c.fixture.stateHash != v[0].(string) {
			return &bindingSQLRows{columns: []string{"user_id", "expires_at", "consumed_at"}}, nil
		}
		var consumed driver.Value
		if c.fixture.consumedAt != nil {
			consumed = *c.fixture.consumedAt
		}
		return &bindingSQLRows{columns: []string{"user_id", "expires_at", "consumed_at"}, values: [][]driver.Value{{c.fixture.userID, c.fixture.expiresAt, consumed}}}, nil
	case strings.HasPrefix(query, "SELECT MAX(version)"):
		var version driver.Value
		if c.fixture.binding.Version > 0 {
			version = c.fixture.binding.Version
		}
		return &bindingSQLRows{columns: []string{"MAX(version)"}, values: [][]driver.Value{{version}}}, nil
	case strings.HasPrefix(query, "SELECT app_id,user_id,open_id"):
		b := c.fixture.binding
		if b.AppID == "" || b.AppID != v[0].(string) || b.UserID != v[1].(int64) {
			return &bindingSQLRows{columns: []string{"app_id", "user_id", "open_id", "union_id", "version", "active", "subscribed", "updated_at"}}, nil
		}
		return &bindingSQLRows{columns: []string{"app_id", "user_id", "open_id", "union_id", "version", "active", "subscribed", "updated_at"}, values: [][]driver.Value{{b.AppID, b.UserID, b.OpenID, b.UnionID, b.Version, b.Active, b.Subscribed, b.UpdatedAt}}}, nil
	default:
		return nil, errors.New("unexpected query: " + query)
	}
}

func TestSQLStoreHashesOneUseStateAndPersistsBinding(t *testing.T) {
	fixture := &bindingSQLFixture{}
	db := sql.OpenDB(bindingSQLConnector{fixture: fixture})
	defer db.Close()
	store, err := NewSQLStore(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	intent, err := store.CreateIntent(context.Background(), 42, now, time.Minute)
	if err != nil || intent.State == "" || fixture.stateHash == intent.State || fixture.stateHash != stateHash(intent.State) {
		t.Fatalf("intent=%+v hash=%q err=%v", intent, fixture.stateHash, err)
	}
	binding, err := store.ConsumeAndBind(context.Background(), intent.State, "wx-official", "official-openid", "", true, now)
	if err != nil || binding.UserID != 42 || binding.OpenID != "official-openid" || binding.Version != 1 {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
	resolved, err := store.ResolveVersion(context.Background(), 42, "wx-official", 1)
	if err != nil || resolved.OpenID != "official-openid" {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	if _, err := store.ConsumeAndBind(context.Background(), intent.State, "wx-official", "other", "", true, now); !errors.Is(err, ErrConsumedState) {
		t.Fatalf("replay error=%v", err)
	}
	if err := store.Unbind(context.Background(), 42, "wx-official", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveBinding(context.Background(), 42, "wx-official"); !errors.Is(err, ErrNotBound) {
		t.Fatalf("unbound binding resolved: %v", err)
	}
}
