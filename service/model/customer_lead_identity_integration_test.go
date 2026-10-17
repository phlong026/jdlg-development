package model

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

// 线索按身份分流的真实 SQL 验证，仅在显式提供的空一次性测试库上运行：
//
//	CREATE DATABASE jd_customer_lead_test; CUSTOMER_LEAD_TEST_DSN='user:pass@tcp(127.0.0.1:13306)/jd_customer_lead_test?parseTime=true' go test ./model -run TestCustomerLeadIdentityRouting
func TestCustomerLeadIdentityRoutingMySQL(t *testing.T) {
	dsn := os.Getenv("CUSTOMER_LEAD_TEST_DSN")
	if dsn == "" {
		t.Skip("set CUSTOMER_LEAD_TEST_DSN to an isolated jd_customer_lead_test database")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(parsed.DBName, "jd_customer_lead_test") {
		t.Fatal("refusing non-test database")
	}
	parsed.ParseTime = true
	db, err := sql.Open("mysql", parsed.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables int
	if err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("test database must be empty: %d %v", tables, err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}

	profiles := NewUserProfileModel(db)
	if err := profiles.InitTable(); err != nil {
		t.Fatal(err)
	}
	// 旧库形态：先建不含 identity_type 的线索表，验证增量列与回填。
	exec(`CREATE TABLE customer_leads (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
		name VARCHAR(64) NOT NULL DEFAULT '', phone VARCHAR(32) NOT NULL DEFAULT '', wechat VARCHAR(128) NOT NULL DEFAULT '',
		enterprise_wechat_contact VARCHAR(128) NOT NULL DEFAULT '', demand_summary TEXT,
		house_floors VARCHAR(64) NOT NULL DEFAULT '', house_style VARCHAR(64) NOT NULL DEFAULT '',
		land_width VARCHAR(64) NOT NULL DEFAULT '', land_depth VARCHAR(64) NOT NULL DEFAULT '',
		room_requirement VARCHAR(255) NOT NULL DEFAULT '', source VARCHAR(64) NOT NULL DEFAULT '',
		source_task_no VARCHAR(128) NOT NULL DEFAULT '', intent_level VARCHAR(32) NOT NULL DEFAULT 'medium',
		status VARCHAR(32) NOT NULL DEFAULT 'new', assigned_to VARCHAR(64) NOT NULL DEFAULT '', remark TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	exec(`INSERT INTO customer_leads (user_id, demand_summary, status, updated_at) VALUES (1, '历史业主线索', 'contacted', '2026-01-01 08:00:00')`)
	exec(`INSERT INTO user_profiles (user_id, identity_type) VALUES (1, '乡墅业主'), (2, '设计师'), (4, '')`)

	leads := NewCustomerServiceModel(db)
	if err := leads.InitTable(); err != nil {
		t.Fatal(err)
	}
	legacy, err := leads.GetLead(1)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.IdentityType != "乡墅业主" || legacy.LeadType != CustomerLeadTypeSales {
		t.Fatalf("backfill should label the legacy lead: %+v", legacy)
	}
	if legacy.UpdatedAt.Format("2006-01-02 15:04:05") != "2026-01-01 08:00:00" {
		t.Fatalf("backfill must keep updated_at (overdue clock), got %v", legacy.UpdatedAt)
	}

	create := func(userID int64) int64 {
		t.Helper()
		id, err := leads.CreateLead(&CustomerLeadCreateInput{UserID: userID, DemandSummary: "三层新中式", Phone: "13800000000", Source: "customer_service", SessionNo: "s" + string(rune('0'+userID))})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	partnerID := create(2) // 设计师
	noProfileID := create(3)
	emptyID := create(4)
	for id, want := range map[int64]string{partnerID: CustomerLeadTypePartner, noProfileID: CustomerLeadTypeIdentityPending, emptyID: CustomerLeadTypeIdentityPending} {
		lead, err := leads.GetLead(id)
		if err != nil {
			t.Fatal(err)
		}
		if lead.LeadType != want {
			t.Fatalf("lead %d type = %q (identity %q), want %q", id, lead.LeadType, lead.IdentityType, want)
		}
	}

	overview, err := leads.Overview()
	if err != nil {
		t.Fatal(err)
	}
	if overview.TotalCount != 4 || overview.TodoCount != 3 || overview.PartnerTodoCount != 1 || overview.PartnerCount != 1 || overview.IdentityPendingCount != 2 || overview.NewCount != 2 || overview.ContactedCount != 1 {
		t.Fatalf("overview should keep partner leads out of sales counts: %+v", overview)
	}
	count := func(params CustomerLeadListParams) int64 {
		t.Helper()
		_, total, err := leads.ListLeads(params)
		if err != nil {
			t.Fatal(err)
		}
		return total
	}
	if got := count(CustomerLeadListParams{Queue: "todo"}); got != overview.TodoCount {
		t.Fatalf("todo queue = %d, badge %d", got, overview.TodoCount)
	}
	if got := count(CustomerLeadListParams{Queue: "partner"}); got != overview.PartnerTodoCount {
		t.Fatalf("partner queue = %d, badge %d", got, overview.PartnerTodoCount)
	}
	if got := count(CustomerLeadListParams{Queue: "identity_pending"}); got != overview.IdentityPendingCount {
		t.Fatalf("identity pending queue = %d, badge %d", got, overview.IdentityPendingCount)
	}
	if got := count(CustomerLeadListParams{Queue: "unassigned"}); got != overview.UnassignedCount || got != 3 {
		t.Fatalf("unassigned queue = %d, badge %d", got, overview.UnassignedCount)
	}
	if got := count(CustomerLeadListParams{Queue: "all"}); got != overview.TotalCount {
		t.Fatalf("all queue = %d, total %d", got, overview.TotalCount)
	}
	if got := count(CustomerLeadListParams{LeadType: CustomerLeadTypeSales}); got != 3 {
		t.Fatalf("sales lead type filter = %d, want 3", got)
	}
	if got := count(CustomerLeadListParams{LeadType: CustomerLeadTypePartner}); got != 1 {
		t.Fatalf("partner lead type filter = %d, want 1", got)
	}

	exec(`UPDATE customer_leads SET status = 'converted' WHERE id = ?`, partnerID)
	metrics, err := leads.FunnelMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if converted := metrics[len(metrics)-1]; converted.Key != "converted" || converted.Count != 0 {
		t.Fatalf("a converted partner lead must not count as a sale: %+v", converted)
	}

	// 账号补选身份：同步到线索，且不刷新 updated_at。
	exec(`UPDATE customer_leads SET updated_at = '2026-02-01 09:00:00' WHERE id = ?`, emptyID)
	if err := profiles.UpdateIdentityType(4, "施工队"); err != nil {
		t.Fatal(err)
	}
	moved, err := leads.GetLead(emptyID)
	if err != nil {
		t.Fatal(err)
	}
	if moved.LeadType != CustomerLeadTypePartner || moved.UpdatedAt.Format("2006-01-02 15:04:05") != "2026-02-01 09:00:00" {
		t.Fatalf("identity change should move the lead to partner without touching updated_at: %+v", moved)
	}

	// 已有线索再次留资：空身份不覆盖已记录身份。
	exec(`UPDATE user_profiles SET identity_type = '' WHERE user_id = 1`)
	if _, err := leads.CreateLead(&CustomerLeadCreateInput{UserID: 1, DemandSummary: "再次咨询"}); err != nil {
		t.Fatal(err)
	}
	again, err := leads.GetLead(1)
	if err != nil {
		t.Fatal(err)
	}
	if again.IdentityType != "乡墅业主" {
		t.Fatalf("an empty identity read must not erase the recorded identity: %+v", again)
	}
}
