package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"service/model"
	"service/tracking"
	"strings"
	"testing"
)

type customerLinkDriver struct{}
type customerLinkConn struct{}
type customerLinkRows struct {
	value driver.Value
	done  bool
}

func (customerLinkDriver) Open(string) (driver.Conn, error)  { return customerLinkConn{}, nil }
func (customerLinkConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (customerLinkConn) Close() error                        { return nil }
func (customerLinkConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (customerLinkConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "SELECT p.user_id") {
		return &customerChoiceRows{values: []driver.Value{int64(101), "张女士", "", "乡墅业主"}}, nil
	}
	if strings.Contains(query, "SELECT u.id,COALESCE(b.accepted") {
		return &customerChoiceRows{values: []driver.Value{int64(101), true, int64(1), model.BusinessContactConsentPurpose, model.BusinessContactConsentVersion, true, "contacted", "sales", nil}}, nil
	}
	if strings.Contains(query, "SELECT t.owner_id") {
		return &customerLinkRows{value: "company"}, nil
	}
	if strings.Contains(query, "SELECT 1 FROM users") {
		return &customerLinkRows{value: int64(1)}, nil
	}
	for _, clause := range []string{"tomb.kind='deletion'", "sh.kind='share'", "e.owner_id IN", "touch.source='share_tracking_my'", "project.kind='note_project'", "deleted.kind='note_project_deletion'"} {
		if !strings.Contains(query, clause) {
			return nil, errors.New("missing trusted relationship: " + clause)
		}
	}
	if strings.Contains(query, "LIMIT 400") || strings.Contains(query, "share_cursor") {
		return nil, errors.New("relationship must not depend on analytics window")
	}
	hasCompany := false
	for _, arg := range args {
		if arg.Value == "company" {
			hasCompany = true
		}
	}
	return &customerLinkRows{value: args[4].Value == "u:7" && args[3].Value == "u:101" && hasCompany}, nil
}
func (*customerLinkRows) Columns() []string { return []string{"allowed"} }
func (*customerLinkRows) Close() error      { return nil }
func (r *customerLinkRows) Next(out []driver.Value) error {
	if r.done {
		return io.EOF
	}
	out[0] = r.value
	r.done = true
	return nil
}
func init() { sql.Register("my-customer-project-link-test", customerLinkDriver{}) }

func TestProjectCustomerVerifierAcceptsOlderOwnRelationshipAndRejectsOtherActor(t *testing.T) {
	db, _ := sql.Open("my-customer-project-link-test", "")
	defer db.Close()
	svc := tracking.New(nil, tracking.Config{Tenant: "tenant"})
	verify := makeProjectCustomerLinkVerifier(db, svc)
	own := tracking.Scope{Tenant: "tenant", Actor: 7, Owners: []string{"designer-7"}, Manage: true, View: true}
	if err := verify(context.Background(), own, "u:101"); err != nil {
		t.Fatalf("own pre-400 relationship rejected: %v", err)
	}
	other := own
	other.Actor = 8
	if err := verify(context.Background(), other, "u:101"); err == nil {
		t.Fatal("same-owner other actor acquired customer reference")
	}
	for _, ref := range []string{"u:0101", "101", "crm-101", "u:0"} {
		if err := verify(context.Background(), own, ref); err == nil {
			t.Fatalf("invalid canonical ref accepted: %s", ref)
		}
	}
	revoked := own
	revoked.Manage = false
	if err := verify(context.Background(), revoked, "u:101"); err == nil {
		t.Fatal("revoked project manager linked customer")
	}
}

func TestProjectCustomerRelationshipParametersNeverInterpolateCallerValues(t *testing.T) {
	scope := tracking.Scope{Tenant: "tenant' OR 1=1", Actor: 7, Owners: []string{"owner' OR 1=1"}}
	query, args := projectCustomerRelationshipQuery(scope, "u:101", 101)
	if strings.Contains(query, scope.Tenant) || strings.Contains(query, scope.Owners[0]) || len(args) == 0 || strings.Count(query, "?") != len(args) {
		t.Fatal("relationship query interpolates data")
	}
}

type customerHandoffFixture struct{ state model.MyCustomerHandoff }

func (f customerHandoffFixture) MyCustomerHandoff(_ context.Context, actor, customer int64) (model.MyCustomerHandoff, error) {
	if actor != 7 || customer != 101 {
		return model.MyCustomerHandoff{}, errors.New("wrong customer actor")
	}
	return f.state, nil
}
func TestMyCustomerDetailChecksRealContactReadinessAndKeepsConvertedStatus(t *testing.T) {
	detail := tracking.MyVisitorDetail{Visitor: tracking.Visitor{Key: "u:101", State: "identified", IdentityType: "乡墅业主"}}
	scope := tracking.Scope{Actor: 7}
	for _, tc := range []struct {
		state model.MyCustomerHandoff
		can   bool
	}{
		{model.MyCustomerHandoff{}, false},
		{model.MyCustomerHandoff{ContactReady: true}, true},
		{model.MyCustomerHandoff{Converted: true, Status: "contacted", Assigned: true}, false},
	} {
		out, err := presentMyCustomerHandoff(context.Background(), customerHandoffFixture{tc.state}, scope, detail)
		if err != nil || out.CanConvertLead != tc.can || out.Handoff.Converted != tc.state.Converted {
			t.Fatalf("contact gating failed: %+v %v", out, err)
		}
		if !tc.state.ContactReady && !tc.state.Converted && out.ConversionReason == "" {
			t.Fatal("missing lawful contact guidance")
		}
	}
}

type customerChoiceRows struct{ values []driver.Value }

func (r *customerChoiceRows) Columns() []string {
	columns := make([]string, len(r.values))
	for i := range columns {
		columns[i] = "field"
	}
	return columns
}
func (*customerChoiceRows) Close() error { return nil }
func (r *customerChoiceRows) Next(out []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(out, r.values)
	r.values = nil
	return nil
}
func TestProjectCustomerExactPreselectionUsesIndependentAuthorizedRelationship(t *testing.T) {
	db, _ := sql.Open("my-customer-project-link-test", "")
	defer db.Close()
	// No analytics store: a legitimate old customer is selected through the
	// indexed relationship, not through the first 400-share analytics window.
	svc := tracking.New(nil, tracking.Config{Tenant: "tenant"})
	scope := tracking.Scope{Tenant: "tenant", Actor: 7, Owners: []string{"company"}, View: true}
	page, err := myProjectCustomerChoices(context.Background(), svc, model.NewCustomerServiceModel(db), scope, tracking.Filter{Visitor: "u:101"})
	if err != nil {
		t.Fatal(err)
	}
	items := page["items"].([]myProjectCustomerChoice)
	if len(items) != 1 || items[0].CustomerRef != "u:101" || items[0].Nickname != "张女士" {
		t.Fatalf("wrong precise candidate: %+v", items)
	}
	for _, ref := range []string{"u:999", "u:0101"} {
		if _, err := myProjectCustomerChoices(context.Background(), svc, model.NewCustomerServiceModel(db), scope, tracking.Filter{Visitor: ref}); err == nil {
			t.Fatalf("unknown/untrusted customer fell back to list: %s", ref)
		}
	}
	other := scope
	other.Actor = 8
	if _, err := myProjectCustomerChoices(context.Background(), svc, model.NewCustomerServiceModel(db), other, tracking.Filter{Visitor: "u:101"}); err == nil {
		t.Fatal("another actor selected private customer")
	}
}
