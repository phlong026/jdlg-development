package tracking

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func crmTargetFixture(t *testing.T) (*Service, Scope) {
	s, scope := fixture(t)
	if e := s.transaction(context.Background(), func(r Repository) error {
		row := record("crm", privateKey("owner-a", "u:42"), "owner-a", s.Now(), map[string]string{"customer_id": "customer-42", "business_number": "C42", "source": "verified_upstream", "basis": "isolated fixture"})
		row.Visitor = "u:42"
		return r.Put(row)
	}); e != nil {
		t.Fatal(e)
	}
	s.VerifyCRM = func(context.Context, Scope, string, string, string) error { return nil }
	return s, scope
}
func TestVisitorCRMTargetRevalidatesExactPermissionedMappingWithoutWrites(t *testing.T) {
	s, sc := crmTargetFixture(t)
	before := len(s.Store.(*memoryStore).data[sc.Tenant])
	calls := 0
	s.VerifyCRM = func(ctx context.Context, scope Scope, visitor, customer, number string) error {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded source read")
		}
		if !reflect.DeepEqual(scope.Owners, []string{"owner-a"}) || scope.Actor != sc.Actor || visitor != "u:42" || customer != "customer-42" || number != "C42" {
			t.Fatalf("wrong source scope: %+v %s %s %s", scope, visitor, customer, number)
		}
		return nil
	}
	for i := 0; i < 2; i++ {
		target, e := s.VisitorCRMTarget(context.Background(), sc, "u:42")
		if e != nil || target.Kind != "customer" || target.CustomerID != "customer-42" || target.Owner != "owner-a" {
			t.Fatalf("%+v %v", target, e)
		}
	}
	if calls != 2 || len(s.Store.(*memoryStore).data[sc.Tenant]) != before {
		t.Fatal("cached authorization or unexpected write")
	}
}
func TestVisitorCRMTargetRejectsAnonymousUnprivilegedAndRevoked(t *testing.T) {
	for _, name := range []string{"anonymous", "noncanonical", "view", "crm", "owner", "missing", "upstream", "deleted", "unverified", "unsafe-target", "nil-verifier"} {
		t.Run(name, func(t *testing.T) {
			s, scope := crmTargetFixture(t)
			key := "u:42"
			calls := 0
			s.VerifyCRM = func(context.Context, Scope, string, string, string) error {
				calls++
				if name == "upstream" {
					return errors.New("access revoked")
				}
				return nil
			}
			switch name {
			case "anonymous":
				key = "a:anonymous"
			case "noncanonical":
				key = "u:042"
			case "view":
				scope.View = false
			case "crm":
				scope.CRM = false
			case "owner":
				scope.Owners = []string{"other"}
			case "missing":
				key = "u:99"
			case "nil-verifier":
				s.VerifyCRM = nil
			case "deleted":
				s.transaction(context.Background(), func(r Repository) error { return r.Put(record("deletion", key, "", s.Now(), nil)) })
			case "unverified", "unsafe-target":
				s.transaction(context.Background(), func(r Repository) error {
					payload := map[string]string{"customer_id": "customer-42", "business_number": "C42", "source": "verified_upstream"}
					if name == "unverified" {
						payload["source"] = "client"
					} else {
						payload["customer_id"] = "../other"
					}
					row := record("crm", privateKey("owner-a", key), "owner-a", s.Now(), payload)
					row.Visitor = key
					return r.Put(row)
				})
			}
			if target, e := s.VisitorCRMTarget(context.Background(), scope, key); e == nil || target.CustomerID != "" {
				t.Fatalf("disclosed target %+v %v", target, e)
			}
			if name != "upstream" && calls != 0 {
				t.Fatal("unauthorized source lookup")
			}
		})
	}
}
func TestVisitorCRMTargetRequiresExplicitOwnerWhenMappingsDiffer(t *testing.T) {
	s, sc := crmTargetFixture(t)
	sc.Owners = append(sc.Owners, "owner-b")
	s.transaction(context.Background(), func(r Repository) error {
		row := record("crm", privateKey("owner-b", "u:42"), "owner-b", s.Now(), map[string]string{"customer_id": "B", "business_number": "B", "source": "verified_upstream"})
		row.Visitor = "u:42"
		return r.Put(row)
	})
	if _, e := s.VisitorCRMTarget(context.Background(), sc, "u:42"); e == nil {
		t.Fatal("ambiguous owner accepted")
	}
	sc.Owners = []string{"owner-a"}
	if _, e := s.VisitorCRMTarget(context.Background(), sc, "u:42"); e != nil {
		t.Fatal(e)
	}
}
