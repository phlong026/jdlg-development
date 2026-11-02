package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"service/tracking"
)

type adminScopeAccount struct {
	scope, userType, status, crmUser string
	enabled                          bool
	grants                           map[string][5]bool
}

type adminScopeConnector struct{ accounts map[int64]adminScopeAccount }
type adminScopeConn struct{ state *adminScopeConnector }
type adminScopeRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (s *adminScopeConnector) Connect(context.Context) (driver.Conn, error) {
	return adminScopeConn{s}, nil
}
func (*adminScopeConnector) Driver() driver.Driver { return adminScopeDriver{} }

type adminScopeDriver struct{}

func (adminScopeDriver) Open(string) (driver.Conn, error)  { return nil, errors.New("use connector") }
func (adminScopeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (adminScopeConn) Close() error                        { return nil }
func (adminScopeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c adminScopeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	query = strings.Join(strings.Fields(query), " ")
	if strings.Contains(query, "FROM management_access a JOIN users u") {
		if args[0].Value == int64(99) {
			return nil, errors.New("private database diagnostic")
		}
		if !strings.Contains(query, "COALESCE(u.status,'active')='active'") {
			return nil, errors.New("management lookup did not require active account")
		}
		account := c.state.accounts[args[0].Value.(int64)]
		rows := &adminScopeRows{columns: []string{"scope", "enabled", "version"}}
		if account.userType == "management" && account.status == "active" {
			rows.values = append(rows.values, []driver.Value{account.scope, account.enabled, int64(1)})
		}
		return rows, nil
	}
	if strings.Contains(query, "SELECT DISTINCT owner_id FROM tracking_records") {
		if strings.Contains(query, "kind IN") || strings.Contains(query, "kind=") {
			return nil, errors.New("owner catalog excluded event-only or legacy owners")
		}
		if len(args) != 2 || args[0].Value != "tenant-a" || args[1].Value != "tenant-a" {
			return nil, errors.New("owner catalog escaped tenant")
		}
		return &adminScopeRows{columns: []string{"owner_id"}, values: [][]driver.Value{{"owner-a"}, {"owner-b"}, {"event-only"}, {"legacy-only"}}}, nil
	}
	if strings.Contains(query, "SELECT t.owner_id,t.can_view,t.can_manage,t.can_crm,t.can_delete,t.can_rules") {
		account := c.state.accounts[args[0].Value.(int64)]
		rows := &adminScopeRows{columns: []string{"owner_id", "can_view", "can_manage", "can_crm", "can_delete", "can_rules"}}
		for owner, grant := range account.grants {
			rows.values = append(rows.values, []driver.Value{owner, grant[0], grant[1], grant[2], grant[3], grant[4]})
		}
		return rows, nil
	}
	if strings.Contains(query, "SELECT owner_id FROM tracking_access") {
		account := c.state.accounts[args[0].Value.(int64)]
		owner := args[2].Value.(string)
		grant, ok := account.grants[owner]
		rows := &adminScopeRows{columns: []string{"owner_id"}}
		if ok && grant[0] && grant[2] {
			rows.values = append(rows.values, []driver.Value{owner})
		}
		return rows, nil
	}
	if strings.Contains(query, "SELECT crm_username FROM management_crm_link") {
		account := c.state.accounts[args[0].Value.(int64)]
		rows := &adminScopeRows{columns: []string{"crm_username"}}
		if account.crmUser != "" {
			rows.values = append(rows.values, []driver.Value{account.crmUser})
		}
		return rows, nil
	}
	return nil, errors.New("unexpected query: " + query)
}
func (r *adminScopeRows) Columns() []string { return r.columns }
func (r *adminScopeRows) Close() error      { return nil }
func (r *adminScopeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func adminScopeDB(t *testing.T, accounts map[int64]adminScopeAccount) *sql.DB {
	t.Helper()
	db := sql.OpenDB(&adminScopeConnector{accounts: accounts})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestPlatformAdminReceivesCurrentTenantTrackingScope(t *testing.T) {
	accounts := map[int64]adminScopeAccount{
		1: {scope: "platform_admin", userType: "management", status: "active", enabled: true},
		2: {scope: "crm_only", userType: "management", status: "active", enabled: true, grants: map[string][5]bool{"owner-a": {true, false, true, false, false}}},
		3: {scope: "unknown", userType: "management", status: "active", enabled: true},
		4: {scope: "platform_admin", userType: "management", status: "banned", enabled: true},
	}
	db := adminScopeDB(t, accounts)
	admin, err := loadManagementTrackingScopeFor(context.Background(), db, "tenant-a", 1, "default-owner")
	sort.Strings(admin.Owners)
	if err != nil || !reflect.DeepEqual(admin.Owners, []string{"default-owner", "event-only", "legacy-only", "owner-a", "owner-b"}) || !admin.View || !admin.Manage || !admin.CRM || !admin.Delete || !admin.Rules {
		t.Fatalf("platform admin scope = %+v error=%v", admin, err)
	}
	crm, err := loadManagementTrackingScopeFor(context.Background(), db, "tenant-a", 2, "default-owner")
	if err != nil || !reflect.DeepEqual(crm.Owners, []string{"owner-a"}) || !crm.View || crm.Manage || !crm.CRM || crm.Delete || crm.Rules {
		t.Fatalf("crm explicit scope = %+v error=%v", crm, err)
	}
	for _, actor := range []int64{3, 4} {
		if _, err := loadManagementTrackingScopeFor(context.Background(), db, "tenant-a", actor, "default-owner"); err == nil {
			t.Fatalf("actor %d received tracking scope", actor)
		}
	}
	if _, err := loadManagementTrackingScopeFor(context.Background(), db, "tenant-a", 99, "default-owner"); !errors.Is(err, tracking.ErrUnavailable) {
		t.Fatalf("database failure did not fail closed as unavailable: %v", err)
	}
	background, err := loadTrackingScopeFor(context.Background(), db, "tenant-a", 1)
	if err != nil || len(background.Owners) != 0 || background.View || background.Manage || background.CRM || background.Delete || background.Rules {
		t.Fatalf("background actor inherited platform admin catalog: %+v error=%v", background, err)
	}
}

func TestPlatformAdminCRMStillRequiresRealBindingAndTarget(t *testing.T) {
	accounts := map[int64]adminScopeAccount{
		1: {scope: "platform_admin", userType: "management", status: "active", enabled: true, crmUser: "managed-1"},
		2: {scope: "crm_only", userType: "management", status: "active", enabled: true, crmUser: "managed-2"},
	}
	db := adminScopeDB(t, accounts)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/management/v1/customers/C1" {
			t.Fatalf("unexpected CRM path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"customer":{"id":"C1","customerNumber":"B1","mergedInto":null}}}`))
	}))
	t.Cleanup(remote.Close)
	t.Setenv("CRM_BRIDGE_URL", remote.URL)
	t.Setenv("CRM_BRIDGE_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("CRM_BRIDGE_AUDIENCE", "crm-test")
	verify := makeTrackingCRMVerifier(db)
	admin := tracking.Scope{Tenant: "tenant-a", Actor: 1, Owners: []string{"owner-b"}, View: true, CRM: true}
	if err := verify(context.Background(), admin, "u:9", "C1", "B1"); err != nil {
		t.Fatalf("platform admin CRM denied without redundant tracking grant: %v", err)
	}
	if err := verify(context.Background(), admin, "u:9", "C1", "wrong"); err == nil {
		t.Fatal("platform admin bypassed CRM target verification")
	}
	crmOnly := tracking.Scope{Tenant: "tenant-a", Actor: 2, Owners: []string{"owner-b"}, View: true, CRM: true}
	if err := verify(context.Background(), crmOnly, "u:9", "C1", "B1"); err == nil {
		t.Fatal("crm-only account bypassed explicit tracking grant")
	}
}
