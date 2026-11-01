package route

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"service/tracking"
)

// TestCertifiedDesignerOwnShareScopeMySQL is opt-in through the same private,
// disposable MySQL configuration as the phase-three lifecycle test. It checks
// the production SQL rather than a query-shape mock.
func TestCertifiedDesignerOwnShareScopeMySQL(t *testing.T) {
	db := openPhase3MySQLLiveDB(t)
	defer db.Close()
	ctx := context.Background()

	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS users (
			id BIGINT PRIMARY KEY,
			user_type VARCHAR(24) NOT NULL,
			status VARCHAR(24) NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS certification_applications (
			id BIGINT PRIMARY KEY,
			user_id BIGINT NOT NULL,
			type VARCHAR(24) NOT NULL,
			status VARCHAR(32) NOT NULL,
			identity_type VARCHAR(32) NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS management_access (
			user_id BIGINT PRIMARY KEY,
			scope VARCHAR(32) NOT NULL,
			enabled BOOLEAN NOT NULL,
			version BIGINT NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	applyPhase3MySQLMigrations(t, db)

	const tenant = "share-scope-live"
	for _, cleanup := range []string{
		`DELETE FROM tracking_access WHERE user_id BETWEEN 90501 AND 90504`,
		`DELETE FROM tracking_access WHERE user_id BETWEEN 90601 AND 90604`,
		`DELETE FROM tracking_records WHERE tenant_id IN (?, ?)`,
		`DELETE FROM certification_applications WHERE user_id BETWEEN 90501 AND 90504`,
		`DELETE FROM management_access WHERE user_id BETWEEN 90601 AND 90604`,
		`DELETE FROM users WHERE id BETWEEN 90501 AND 90504`,
		`DELETE FROM users WHERE id BETWEEN 90601 AND 90604`,
	} {
		args := []any{}
		if cleanup == `DELETE FROM tracking_records WHERE tenant_id IN (?, ?)` {
			args = []any{tenant, tenant + "-other"}
		}
		if _, err := db.ExecContext(ctx, cleanup, args...); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,user_type,status) VALUES
		(90501,'miniprogram','active'),(90502,'miniprogram','active'),
		(90503,'miniprogram','banned'),(90504,'miniprogram','active'),
		(90601,'management','active'),(90602,'management','active'),
		(90603,'management','banned'),(90604,'management','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO management_access(user_id,scope,enabled,version) VALUES
		(90601,'platform_admin',TRUE,1),(90602,'crm_only',TRUE,1),
		(90603,'platform_admin',TRUE,1),(90604,'operator',TRUE,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO certification_applications(id,user_id,type,status,identity_type) VALUES
		(99501,90501,'designer','approved','设计师'),
		(99502,90502,'designer','pending_review','设计师'),
		(99503,90503,'designer','approved','设计师')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tracking_access
		(user_id,tenant_id,owner_id,can_view,can_manage,can_crm,can_delete,can_rules,enabled,updated_by,updated_at) VALUES
		(90501,?,'organization',TRUE,FALSE,FALSE,FALSE,FALSE,TRUE,1,UTC_TIMESTAMP(6)),
		(90501,?,'company',FALSE,TRUE,FALSE,FALSE,FALSE,TRUE,1,UTC_TIMESTAMP(6)),
		(90501,?,'private-a',TRUE,TRUE,FALSE,FALSE,FALSE,FALSE,1,UTC_TIMESTAMP(6)),
		(90501,?,'legacy',FALSE,FALSE,FALSE,FALSE,FALSE,TRUE,1,UTC_TIMESTAMP(6))`, tenant, tenant, tenant, tenant+"-other"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tracking_access
		(user_id,tenant_id,owner_id,can_view,can_manage,can_crm,can_delete,can_rules,enabled,updated_by,updated_at) VALUES
		(90602,?,'crm-owner',TRUE,FALSE,TRUE,FALSE,FALSE,TRUE,1,UTC_TIMESTAMP(6)),
		(90602,?,'foreign-crm-owner',TRUE,FALSE,TRUE,FALSE,FALSE,TRUE,1,UTC_TIMESTAMP(6))`, tenant, tenant+"-other"); err != nil {
		t.Fatal(err)
	}

	insertShare := func(record, recordTenant, owner, visitor, sharer string) {
		t.Helper()
		payload := `{"share_id":"` + record + `","route_key":"page","owner_id":"` + owner + `","sharer_id":"` + sharer + `","expires_at":"2030-01-01T00:00:00Z"}`
		if _, err := db.ExecContext(ctx, `INSERT INTO tracking_records
			(tenant_id,kind,record_id,owner_id,visitor_key,occurred_at,payload)
				VALUES (?,'share',?,?,?,UTC_TIMESTAMP(6),?)`, recordTenant, record, owner, visitor, payload); err != nil {
			t.Fatal(err)
		}
	}
	insertShare("scalar-own", tenant, "company", "u:90501", "u:90501")
	insertShare("disabled-own", tenant, "private-a", "u:90501", "u:90501")
	insertShare("legacy-own", tenant, "legacy", "", "u:90501")
	insertShare("public-own", tenant, "public", "u:90501", "u:90501")
	insertShare("public-coworker", tenant, "public", "u:90777", "u:90777")
	insertShare("coworker-only", tenant, "coworker", "u:90777", "u:90777")
	insertShare("foreign-tenant", tenant+"-other", "foreign", "u:90501", "u:90501")
	insertShare("admin-current-owner", tenant, "admin-content", "u:90777", "u:90777")
	insertShare("admin-foreign-owner", tenant+"-other", "admin-foreign", "u:90777", "u:90777")

	for _, kind := range []string{"event", "legacy_catalog", "project_collection"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO tracking_records (tenant_id,kind,record_id,owner_id,occurred_at,payload) VALUES (?,?,?,?,UTC_TIMESTAMP(6),'{}')`, tenant, kind, "admin-history-"+kind, "history-"+kind); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("approved designer receives only permitted own-share owners", func(t *testing.T) {
		scope, err := loadMiniTrackingMyScope(ctx, db, tenant, 90501)
		want := []string{"designer-90501", "legacy", "organization", "public"}
		sort.Strings(scope.Owners)
		if err != nil || !scope.View || !reflect.DeepEqual(scope.Owners, want) {
			t.Fatalf("approved designer scope=%+v err=%v, want owners=%v", scope, err, want)
		}
		svc := tracking.New(tracking.NewSQLStore(db, 1, 2), tracking.Config{Tenant: tenant, Enabled: true, Routes: map[string]tracking.Route{"page": {Public: true, Title: "公共素材"}}})
		list, err := svc.MyShares(ctx, scope, 90501, tracking.Filter{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, share := range list.Items.([]tracking.MyShare) {
			ids = append(ids, share.ID)
		}
		sort.Strings(ids)
		if !reflect.DeepEqual(ids, []string{"legacy-own", "public-own"}) {
			t.Fatalf("another actor or denied owner leaked: %v", ids)
		}
		if _, err := svc.MyShareVisits(ctx, scope, 90501, "public-coworker", tracking.Filter{}); err == nil {
			t.Fatal("coworker visitors became readable")
		}
		if _, err := svc.MyShares(ctx, scope, 90502, tracking.Filter{}); err == nil {
			t.Fatal("caller-controlled actor became readable")
		}
	})

	t.Run("unapproved and banned accounts fail closed", func(t *testing.T) {
		for _, actor := range []int64{90502, 90503, 90504} {
			scope, err := loadMiniTrackingMyScope(ctx, db, tenant, actor)
			if !errors.Is(err, errMySharesViewDenied) || scope.View || len(scope.Owners) != 0 {
				t.Fatalf("actor %d unexpectedly received share scope: %+v err=%v", actor, scope, err)
			}
		}
	})

	t.Run("certification revocation removes derived owners immediately", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE certification_applications SET status='rejected' WHERE user_id=90501`); err != nil {
			t.Fatal(err)
		}
		scope, err := loadMiniTrackingMyScope(ctx, db, tenant, 90501)
		if err != nil || !scope.View || !reflect.DeepEqual(scope.Owners, []string{"organization"}) {
			t.Fatalf("revoked designer retained derived owners: %+v err=%v", scope, err)
		}
	})

	t.Run("platform admin receives every current-tenant owner and capability", func(t *testing.T) {
		scope, err := loadManagementTrackingScopeFor(ctx, db, tenant, 90601, "default-owner")
		want := []string{"admin-content", "company", "coworker", "crm-owner", "default-owner", "history-event", "history-legacy_catalog", "history-project_collection", "legacy", "organization", "private-a", "public"}
		if err != nil || !reflect.DeepEqual(scope.Owners, want) || !scope.View || !scope.Manage || !scope.CRM || !scope.Delete || !scope.Rules {
			t.Fatalf("platform admin scope=%+v err=%v, want owners=%v", scope, err, want)
		}
	})

	t.Run("crm-only account retains explicit current-tenant scope", func(t *testing.T) {
		scope, err := loadManagementTrackingScopeFor(ctx, db, tenant, 90602, "default-owner")
		if err != nil || !reflect.DeepEqual(scope.Owners, []string{"crm-owner"}) || !scope.View || scope.Manage || !scope.CRM || scope.Delete || scope.Rules {
			t.Fatalf("crm-only scope=%+v err=%v", scope, err)
		}
	})

	t.Run("banned and unknown management roles fail closed", func(t *testing.T) {
		for _, actor := range []int64{90603, 90604} {
			scope, err := loadManagementTrackingScopeFor(ctx, db, tenant, actor, "default-owner")
			var denied *tracking.Error
			if !errors.As(err, &denied) || denied.Status != 403 || len(scope.Owners) != 0 {
				t.Fatalf("actor %d unexpectedly received admin scope: %+v err=%v", actor, scope, err)
			}
		}
	})
}
