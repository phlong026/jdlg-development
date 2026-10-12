package route

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

// Exercise the production route handlers and service with an injected verified
// scope. Authentication middleware is not replaced in production.
type governanceTestStore struct{ records map[string]tracking.Record }
type governanceTestRepo struct{ records map[string]tracking.Record }

func (s *governanceTestStore) Transaction(_ context.Context, tenant string, fn func(tracking.Repository) error) error {
	next := map[string]tracking.Record{}
	for key, row := range s.records {
		if row.Tenant != tenant {
			return errors.New("unexpected tenant")
		}
		next[key] = row
	}
	r := &governanceTestRepo{records: next}
	if err := fn(r); err != nil {
		return err
	}
	for key, row := range next {
		row.Tenant = tenant
		next[key] = row
	}
	s.records = next
	return nil
}
func (r *governanceTestRepo) Get(kind, id string) (tracking.Record, error) {
	row, ok := r.records[kind+":"+id]
	if !ok {
		return row, tracking.ErrNotFound
	}
	return row, nil
}
func (r *governanceTestRepo) List(kind string) ([]tracking.Record, error) {
	out := []tracking.Record{}
	for _, row := range r.records {
		if row.Kind == kind {
			out = append(out, row)
		}
	}
	return out, nil
}
func (r *governanceTestRepo) Put(row tracking.Record) error {
	r.records[row.Kind+":"+row.ID] = row
	return nil
}
func (r *governanceTestRepo) Insert(row tracking.Record) error {
	if _, ok := r.records[row.Kind+":"+row.ID]; ok {
		return errors.New("duplicate")
	}
	return r.Put(row)
}
func (r *governanceTestRepo) Remove(kind, id string) error {
	delete(r.records, kind+":"+id)
	return nil
}

func governanceRouteFixture(t *testing.T, owners []string) (*gin.Engine, *governanceTestStore, *tracking.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	registered := gin.New()
	svc := RegisterTrackingRoutes(registered.Group("/api/v1"), &model.UserModel{}, nil)
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	store := &governanceTestStore{records: map[string]tracking.Record{}}
	svc.Config.Tenant = "governance-tenant"
	svc.Store = store
	svc.Now = func() time.Time { return now }
	svc.VerifyCRM = func(context.Context, tracking.Scope, string, string, string) error { return nil }
	scope := tracking.Scope{Tenant: svc.Config.Tenant, Actor: 7, Owners: owners, View: true, CRM: true, Rules: true, Delete: true}
	for _, owner := range []string{"owner-a", "owner-b"} {
		row := tracking.Record{Tenant: scope.Tenant, Kind: "event", ID: "event-" + owner, Owner: owner, Visitor: "u:42", At: now, Payload: json.RawMessage(`{}`)}
		store.records[row.Kind+":"+row.ID] = row
		seedScope := scope
		seedScope.Owners = []string{owner}
		if err := svc.LinkCRM(context.Background(), seedScope, "u:42", "crm-"+owner, "number-"+owner, "verified seed"); err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	for _, route := range registered.Routes() {
		if strings.Contains(route.Path, "/identity-links") || strings.HasSuffix(route.Path, "/intent-overrides") || strings.HasSuffix(route.Path, "/deletions") {
			router.Handle(route.Method, route.Path, func(c *gin.Context) { c.Set("tracking.scope", scope); c.Next() }, route.HandlerFunc)
		}
	}
	return router, store, svc
}

var governanceMutations = []struct {
	name, method, path, body, kind string
}{
	{"link", "POST", "/identity-links", `{"visitor_key":"u:42","customer_id":"new-customer","business_number":"new-number","basis":"verified"}`, "crm"},
	{"unlink", "DELETE", "/identity-links/u:42", "", "crm"},
	{"override", "POST", "/intent-overrides", `{"visitor_key":"u:42","level":"high","reason":"verified","expires_at":"2026-10-12T12:00:00Z"}`, "intent_override"},
}

func TestTrackingGovernanceMutationOwnerScope(t *testing.T) {
	for _, mutation := range governanceMutations {
		for _, query := range []struct {
			name, value string
			status      int
		}{{"unauthorized", "?owner=outside", 403}, {"missing_mixed", "", 409}, {"empty", "?owner=", 400}, {"duplicate", "?owner=owner-a&owner=owner-b", 400}, {"selected", "?owner=owner-b", 200}} {
			t.Run(mutation.name+"/"+query.name, func(t *testing.T) {
				router, store, svc := governanceRouteFixture(t, []string{"owner-a", "owner-b"})
				before := store.records
				verified := []tracking.Scope{}
				svc.VerifyCRM = func(_ context.Context, scope tracking.Scope, _, _, _ string) error {
					verified = append(verified, scope)
					return nil
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(mutation.method, "/api/v1/management/tracking"+mutation.path+query.value, strings.NewReader(mutation.body)))
				if response.Code != query.status {
					t.Fatalf("status %d, want %d: %s", response.Code, query.status, response.Body.String())
				}
				if query.status != 200 {
					if !reflect.DeepEqual(before, store.records) || len(verified) != 0 {
						t.Fatal("rejected scope changed data or reached CRM verifier")
					}
					return
				}
				for key, row := range before {
					if row.Owner != "owner-b" && !reflect.DeepEqual(row, store.records[key]) {
						t.Fatal("changed another owner's record", row.Kind, row.Owner)
					}
				}
				selectedCount := 0
				for _, row := range store.records {
					if row.Kind == mutation.kind && row.Owner == "owner-b" {
						selectedCount++
						if mutation.name == "link" && !strings.Contains(string(row.Payload), "new-customer") {
							t.Fatal("selected owner's mapping was not updated")
						}
					}
				}
				wantCount := 1
				if mutation.name == "unlink" {
					wantCount = 0
				}
				if selectedCount != wantCount {
					t.Fatalf("selected owner records %d, want %d", selectedCount, wantCount)
				}
				if mutation.name == "link" && (len(verified) != 1 || !reflect.DeepEqual(verified[0].Owners, []string{"owner-b"})) {
					t.Fatal("CRM verifier did not receive the narrowed owner scope", verified)
				}
			})
		}
	}
}

func TestTrackingGovernanceSingleOwnerCanOmitSelection(t *testing.T) {
	for _, mutation := range governanceMutations {
		t.Run(mutation.name, func(t *testing.T) {
			router, _, _ := governanceRouteFixture(t, []string{"owner-a"})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(mutation.method, "/api/v1/management/tracking"+mutation.path, strings.NewReader(mutation.body)))
			if response.Code != 200 {
				t.Fatal(response.Code, response.Body.String())
			}
		})
	}
}

func TestTrackingDeletionIgnoresAnnotationSelectionAndRequiresFullScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		owners []string
		status int
	}{{"full_scope", []string{"owner-a", "owner-b"}, 200}, {"partial_scope", []string{"owner-a"}, 403}} {
		t.Run(tc.name, func(t *testing.T) {
			router, store, _ := governanceRouteFixture(t, tc.owners)
			before := store.records
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/management/tracking/deletions?owner=owner-a", strings.NewReader(`{"visitor_key":"u:42","reason":"verified deletion request"}`)))
			if response.Code != tc.status {
				t.Fatal(response.Code, response.Body.String())
			}
			if tc.status != 200 {
				if !reflect.DeepEqual(before, store.records) {
					t.Fatal("partial-scope deletion changed records")
				}
				return
			}
			for _, row := range store.records {
				if row.Kind == "event" || row.Kind == "crm" {
					t.Fatal("selection narrowed full identity deletion", row.Kind, row.Owner)
				}
			}
		})
	}
}

func TestTrackingPrivateScopePreservesOriginalPermissions(t *testing.T) {
	original := tracking.Scope{Tenant: "org", Actor: 7, Owners: []string{"owner-a", "owner-b"}, View: true, Delete: true}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/?owner=owner-b", nil)
	narrowed, err := trackingPrivateMutationScope(c, original)
	if err != nil || narrowed.Tenant != original.Tenant || narrowed.Actor != original.Actor || narrowed.CRM || narrowed.Rules || narrowed.Manage || !narrowed.View || !narrowed.Delete || !reflect.DeepEqual(original.Owners, []string{"owner-a", "owner-b"}) || !reflect.DeepEqual(narrowed.Owners, []string{"owner-b"}) {
		t.Fatalf("scope/permissions changed: original=%+v narrowed=%+v error=%v", original, narrowed, err)
	}
}
