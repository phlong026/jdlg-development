package tracking

import (
	"encoding/base64"
	"testing"
)

func TestDetailPaginationBindsEndpointTargetAndCurrentScope(t *testing.T) {
	s, scope := fixture(t)
	f, e := s.Normalize(Filter{Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	_, _, next, first, e := paginateDetail(f, 3, "sessions-v1", "u:1", scope)
	if e != nil || next == "" || first == "" {
		t.Fatal(next, first, e)
	}
	f.Cursor = next
	start, _, _, _, e := paginateDetail(f, 3, "sessions-v1", "u:1", scope)
	if e != nil || start != 1 {
		t.Fatal(start, e)
	}
	for _, name := range []string{"kind", "target", "actor", "tenant", "owners", "permission", "filter"} {
		t.Run(name, func(t *testing.T) {
			sc := scope
			ff := f
			kind, key := "sessions-v1", "u:1"
			switch name {
			case "kind":
				kind = "events-v1"
			case "target":
				key = "u:2"
			case "actor":
				sc.Actor++
			case "tenant":
				sc.Tenant = "other"
			case "owners":
				sc.Owners = []string{"owner-b"}
			case "permission":
				sc.CRM = false
			case "filter":
				ff.Identity = "anonymous"
			}
			if _, _, _, _, e := paginateDetail(ff, 3, kind, key, sc); e == nil {
				t.Fatal("foreign detail cursor accepted")
			}
		})
	}
	f.Cursor = first
	start, _, _, _, e = paginateDetail(f, 3, "sessions-v1", "u:1", scope)
	if e != nil || start != 0 {
		t.Fatal(start, e)
	}
}
func TestDetailPaginationOldFormatRequiresRefreshAndOwnerOrderIsStable(t *testing.T) {
	s, scope := fixture(t)
	f, _ := s.Normalize(Filter{Limit: 1})
	scope.Owners = []string{"owner-b", "owner-a"}
	_, _, next, _, e := paginateDetail(f, 2, "events-v1", "view-1", scope)
	if e != nil {
		t.Fatal(e)
	}
	scope.Owners = []string{"owner-a", "owner-b"}
	f.Cursor = next
	if _, _, _, _, e = paginateDetail(f, 2, "events-v1", "view-1", scope); e != nil {
		t.Fatal(e)
	}
	f.Cursor = base64.RawURLEncoding.EncodeToString(marshal(map[string]any{"offset": 1, "signature": "old"}))
	_, _, _, _, e = paginateDetail(f, 2, "events-v1", "view-1", scope)
	if te, ok := e.(*Error); !ok || te.Status != 409 {
		t.Fatal(e)
	}
}
