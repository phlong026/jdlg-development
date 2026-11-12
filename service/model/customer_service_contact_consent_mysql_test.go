package model

import (
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// This integration test uses connection-local temporary tables that shadow the
// real schema, so it can exercise MySQL row/update semantics without mutating
// production data. Set JDLG_TEST_MYSQL_DSN to run it against any disposable or
// prepared MySQL schema.
func TestMySQLConsentUpgradesLegacyLeadOnlyInsideAuthorizedTransaction(t *testing.T) {
	dsn := os.Getenv("JDLG_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JDLG_TEST_MYSQL_DSN not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	for name, ddl := range map[string]string{
		"customer_leads": `CREATE TEMPORARY TABLE customer_leads (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
			name VARCHAR(64) NOT NULL DEFAULT '', phone VARCHAR(32) NOT NULL DEFAULT '', phone_source VARCHAR(32) NOT NULL DEFAULT 'legacy_unknown',
			phone_consent_revision BIGINT UNSIGNED NOT NULL DEFAULT 0, wechat VARCHAR(128) NOT NULL DEFAULT '', enterprise_wechat_contact VARCHAR(128) NOT NULL DEFAULT '',
			demand_summary TEXT, house_floors VARCHAR(64) NOT NULL DEFAULT '', house_style VARCHAR(64) NOT NULL DEFAULT '', land_width VARCHAR(64) NOT NULL DEFAULT '',
			land_depth VARCHAR(64) NOT NULL DEFAULT '', room_requirement VARCHAR(255) NOT NULL DEFAULT '', source VARCHAR(64) NOT NULL DEFAULT '', session_no VARCHAR(128) NOT NULL DEFAULT '',
			source_task_no VARCHAR(128) NOT NULL DEFAULT '', intent_level VARCHAR(32) NOT NULL DEFAULT 'medium', status VARCHAR(32) NOT NULL DEFAULT 'new',
			assigned_to VARCHAR(64) NOT NULL DEFAULT '', next_followup_at DATETIME NULL, remark TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		"customer_service_events": `CREATE TEMPORARY TABLE customer_service_events (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, user_id BIGINT UNSIGNED NOT NULL DEFAULT 0, lead_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
			session_no VARCHAR(128) NOT NULL DEFAULT '', event_type VARCHAR(64) NOT NULL DEFAULT '', source VARCHAR(64) NOT NULL DEFAULT '', source_task_no VARCHAR(128) NOT NULL DEFAULT '',
			intent_level VARCHAR(32) NOT NULL DEFAULT 'medium', demand_summary TEXT, payload JSON NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		"business_contact_consents": `CREATE TEMPORARY TABLE business_contact_consents (
			user_id BIGINT UNSIGNED NOT NULL PRIMARY KEY, accepted TINYINT(1) NOT NULL DEFAULT 0, revision BIGINT UNSIGNED NOT NULL DEFAULT 0,
			purpose VARCHAR(64) NOT NULL DEFAULT 'use_bound_phone_for_business_contact', consent_version VARCHAR(32) NOT NULL DEFAULT 'phone_contact_v1',
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		"user_identities": `CREATE TEMPORARY TABLE user_identities (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, user_id BIGINT UNSIGNED NOT NULL, identity_type VARCHAR(32) NOT NULL,
			identity_key VARCHAR(255) NOT NULL, verified_at DATETIME NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("temporary %s: %v", name, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO user_identities (user_id,identity_type,identity_key,verified_at) VALUES (41,'phone','13800000000',NOW())`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO customer_leads (user_id,name,phone,phone_source,phone_consent_revision,demand_summary,status) VALUES (41,'legacy','13900000000','legacy_unknown',0,'legacy','new')`); err != nil {
		t.Fatal(err)
	}
	m := NewCustomerServiceModel(db)
	if err := m.SetBusinessContactConsent(41, true, "mysql_test"); err != nil {
		t.Fatal(err)
	}
	// Repeating the same choice is idempotent. In particular, it must not
	// advance the authorization revision and make an already authorized phone
	// disappear from reads that require the current revision.
	if err := m.SetBusinessContactConsent(41, true, "mysql_test_repeat"); err != nil {
		t.Fatal(err)
	}
	var consentRevision int64
	if err := db.QueryRow(`SELECT revision FROM business_contact_consents WHERE user_id=41`).Scan(&consentRevision); err != nil {
		t.Fatal(err)
	}
	if consentRevision != 1 {
		t.Fatalf("repeated grant revision=%d, want 1", consentRevision)
	}
	conversion, err := m.ConvertTrackingLead(
		&CustomerLeadCreateInput{UserID: 41, DemandSummary: "authorized refresh"},
		&CustomerServiceEventCreateInput{UserID: 41, EventType: "tracking_lead_created", DemandSummary: "authorized refresh"},
		false,
	)
	if err != nil || !conversion.Existed {
		t.Fatalf("conversion=%+v err=%v", conversion, err)
	}
	leadID := conversion.LeadID
	var phone, source string
	var revision int64
	if err := db.QueryRow(`SELECT phone,phone_source,phone_consent_revision FROM customer_leads WHERE id=?`, leadID).Scan(&phone, &source, &revision); err != nil {
		t.Fatal(err)
	}
	if phone != "13800000000" || source != CustomerLeadPhoneSourceAccountBound || revision != 1 {
		t.Fatalf("phone=%q source=%q revision=%d", phone, source, revision)
	}
	if err := m.SetBusinessContactConsent(41, false, "mysql_test"); err != nil {
		t.Fatal(err)
	}
	lead, err := m.GetLead(leadID)
	if err != nil {
		t.Fatal(err)
	}
	if lead.Phone != "" {
		t.Fatalf("withdrawn account phone remained visible: %q", lead.Phone)
	}
	if err := m.SetBusinessContactConsent(41, true, "mysql_test_regrant"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT revision FROM business_contact_consents WHERE user_id=41`).Scan(&consentRevision); err != nil {
		t.Fatal(err)
	}
	if consentRevision != 3 {
		t.Fatalf("withdraw and regrant revision=%d, want 3", consentRevision)
	}

	// Consent alone is not enough to create a contact lead: the account must
	// actually have a bound phone available for the authorized purpose.
	if err := m.SetBusinessContactConsent(42, true, "mysql_test_no_phone"); err != nil {
		t.Fatal(err)
	}
	_, err = m.ConvertTrackingLead(
		&CustomerLeadCreateInput{UserID: 42, DemandSummary: "no bound phone"},
		&CustomerServiceEventCreateInput{UserID: 42, EventType: "tracking_lead_created", DemandSummary: "no bound phone"},
		false,
	)
	if !errors.Is(err, ErrBusinessContactPhoneUnavailable) {
		t.Fatalf("no-phone conversion err=%v, want ErrBusinessContactPhoneUnavailable", err)
	}
	var leadCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM customer_leads WHERE user_id=42`).Scan(&leadCount); err != nil {
		t.Fatal(err)
	}
	if leadCount != 0 {
		t.Fatalf("no-phone conversion created %d leads, want 0", leadCount)
	}
}

// Set JDLG_TEST_MYSQL_INIT_DSN only to a disposable schema. This proves the
// runtime schema owner can follow the reviewed SQL migration and can run again
// on every restart without duplicate-column failures.
func TestMySQLContactConsentInitTableIsIdempotent(t *testing.T) {
	dsn := os.Getenv("JDLG_TEST_MYSQL_INIT_DSN")
	if dsn == "" {
		t.Skip("JDLG_TEST_MYSQL_INIT_DSN not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := NewCustomerServiceModel(db)
	for attempt := 1; attempt <= 2; attempt++ {
		if err := m.InitTable(); err != nil {
			t.Fatalf("InitTable attempt %d: %v", attempt, err)
		}
	}
	for _, column := range []string{"phone_source", "phone_consent_revision"} {
		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'customer_leads' AND COLUMN_NAME = ?
		`, column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("column %s count=%d, want 1", column, count)
		}
	}
}
