package route

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"service/component/virtualpay"
	"service/reportwecom"
)

func testWecomEventInboxDB(t *testing.T) (*sql.DB, *wecomEventInbox) {
	t.Helper()
	dsn := os.Getenv("WECOM_EVENT_INBOX_TEST_DSN")
	if dsn == "" {
		t.Skip("set WECOM_EVENT_INBOX_TEST_DSN to run the MySQL integration tests")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping MySQL: %v", err)
	}
	var database string
	if err := db.QueryRow(`SELECT DATABASE()`).Scan(&database); err != nil || database != "wecom_inbox_test" {
		db.Close()
		t.Fatalf("refusing to reset non-test database %q: %v", database, err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS wecom_event_inbox`); err != nil {
		db.Close()
		t.Fatalf("reset inbox table: %v", err)
	}
	inbox, err := newWecomEventInbox(db, reportwecom.Config{Token: "integration-token"}, nil)
	if err != nil {
		db.Close()
		t.Fatalf("create inbox: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS wecom_event_inbox`)
		_ = db.Close()
	})
	return db, inbox
}

func TestForwardWecomEventResignsAndDelivers(t *testing.T) {
	const token = "forward-test-token"
	const encrypted = "ENC-BODY"
	var gotSig, gotTs, gotNonce, gotBody, gotRoot, gotContentType string
	var decodeErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.URL.Query().Get("msg_signature")
		gotTs = r.URL.Query().Get("timestamp")
		gotNonce = r.URL.Query().Get("nonce")
		gotContentType = r.Header.Get("Content-Type")
		var envelope struct {
			XMLName xml.Name `xml:"xml"`
			Encrypt string   `xml:"Encrypt"`
		}
		decodeErr = xml.NewDecoder(r.Body).Decode(&envelope)
		gotRoot = envelope.XMLName.Local
		gotBody = envelope.Encrypt
		w.WriteHeader(200)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()
	t.Setenv("CRM_BRIDGE_URL", server.URL)

	inbox := &wecomEventInbox{config: reportwecom.Config{Token: token}}
	now := time.Now().Unix()
	status, err := inbox.forwardWecomEvent(context.Background(), encrypted)
	if err != nil || status != 200 {
		t.Fatalf("forward failed: status=%d err=%v", status, err)
	}
	if decodeErr != nil || gotRoot != "xml" || gotBody != encrypted {
		t.Fatalf("encrypted XML envelope = root:%q body:%q, decode error: %v", gotRoot, gotBody, decodeErr)
	}
	if gotContentType != "application/xml; charset=UTF-8" {
		t.Fatalf("content type = %q, want application/xml", gotContentType)
	}
	if gotSig == "" || gotNonce == "" {
		t.Fatal("missing fresh signature query")
	}
	ts, err := strconv.ParseInt(gotTs, 10, 64)
	if err != nil {
		t.Fatalf("bad timestamp %q: %v", gotTs, err)
	}
	if now-ts > 300 || ts-now > 300 {
		t.Fatal("timestamp not fresh, Java would reject")
	}
	if !virtualpay.VerifyPushSignature(token, gotTs, gotNonce, encrypted, gotSig) {
		t.Fatal("fresh signature does not verify against shared token")
	}
}

func TestWecomEventClassification(t *testing.T) {
	if !reportwecom.PermanentEventError(reportwecom.ErrNotOwner) || !reportwecom.PermanentEventError(reportwecom.ErrExpired) {
		t.Fatal("permanent errors misclassified")
	}
	if reportwecom.PermanentEventError(errors.New("db down")) {
		t.Fatal("transient error misclassified as permanent")
	}
	relevant := reportwecom.Event{Event: "change_external_contact", ChangeType: "add_external_contact"}
	if !wecomEventRelevantToCRM(relevant) {
		t.Fatal("add event should forward to CRM")
	}
	if wecomEventRelevantToCRM(reportwecom.Event{Event: "change_external_contact", ChangeType: "edit_party"}) {
		t.Fatal("irrelevant event should not forward")
	}
	if wecomEventDedupKey([]byte("x"), reportwecom.Event{}) == wecomEventDedupKey([]byte("y"), reportwecom.Event{}) {
		t.Fatal("content dedup key collision")
	}
	if wecomEventDedupKey([]byte("x"), reportwecom.Event{MsgId: "m1"}) != "m1" {
		t.Fatal("MsgId should win as dedup key")
	}
}

func TestDispatchPendingPersistsAttemptAndPreservesSettledDownstream(t *testing.T) {
	db, inbox := testWecomEventInboxDB(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("CRM_BRIDGE_URL", server.URL)
	t.Setenv("CRM_EVENT_FORWARD_ENABLED", "true")

	_, err := db.Exec(`INSERT INTO wecom_event_inbox
 (dedup_key,encrypted_body,plain_body,report_status,crm_status,attempts)
 VALUES ('crm-retry','encrypted','<xml/>',0,1,0),
        ('crm-exhausted','encrypted','<xml/>',0,1,100),
        ('report-exhausted','encrypted','<xml/>',1,0,100)`)
	if err != nil {
		t.Fatalf("seed inbox: %v", err)
	}

	inbox.dispatchPending()

	var reportStatus, crmStatus, attempts int
	var nextAttempt sql.NullTime
	if err := db.QueryRow(`SELECT report_status,crm_status,attempts,next_attempt_at
 FROM wecom_event_inbox WHERE dedup_key='crm-retry'`).Scan(&reportStatus, &crmStatus, &attempts, &nextAttempt); err != nil {
		t.Fatalf("read retry row: %v", err)
	}
	if reportStatus != 0 || crmStatus != 1 || attempts != 1 || !nextAttempt.Valid {
		t.Fatalf("retry row = report:%d crm:%d attempts:%d next:%v", reportStatus, crmStatus, attempts, nextAttempt.Valid)
	}
	if requests != 1 {
		t.Fatalf("forward requests = %d, want 1", requests)
	}

	for _, tc := range []struct {
		key                 string
		wantReport, wantCRM int
	}{
		{key: "crm-exhausted", wantReport: 0, wantCRM: 3},
		{key: "report-exhausted", wantReport: 3, wantCRM: 0},
	} {
		if err := db.QueryRow(`SELECT report_status,crm_status FROM wecom_event_inbox WHERE dedup_key=?`, tc.key).Scan(&reportStatus, &crmStatus); err != nil {
			t.Fatalf("read %s: %v", tc.key, err)
		}
		if reportStatus != tc.wantReport || crmStatus != tc.wantCRM {
			t.Fatalf("%s = report:%d crm:%d, want report:%d crm:%d", tc.key, reportStatus, crmStatus, tc.wantReport, tc.wantCRM)
		}
	}
}

func TestCleanupRetainsPendingAndFailedEvents(t *testing.T) {
	db, inbox := testWecomEventInboxDB(t)
	_, err := db.Exec(`INSERT INTO wecom_event_inbox
 (dedup_key,encrypted_body,plain_body,report_status,crm_status,created_at,updated_at)
 VALUES ('old-complete','x','x',0,0,NOW()-INTERVAL 20 DAY,NOW()-INTERVAL 20 DAY),
        ('old-skipped','x','x',2,2,NOW()-INTERVAL 20 DAY,NOW()-INTERVAL 20 DAY),
        ('old-report-pending','x','x',1,0,NOW()-INTERVAL 20 DAY,NOW()-INTERVAL 20 DAY),
        ('old-crm-pending','x','x',0,1,NOW()-INTERVAL 20 DAY,NOW()-INTERVAL 20 DAY),
        ('old-report-failed','x','x',3,0,NOW()-INTERVAL 20 DAY,NOW()-INTERVAL 20 DAY),
        ('old-crm-failed','x','x',0,3,NOW()-INTERVAL 20 DAY,NOW()-INTERVAL 20 DAY),
        ('recent-complete','x','x',0,0,NOW()-INTERVAL 2 DAY,NOW()-INTERVAL 2 DAY)`)
	if err != nil {
		t.Fatalf("seed cleanup rows: %v", err)
	}

	inbox.cleanup()

	rows, err := db.Query(`SELECT dedup_key FROM wecom_event_inbox ORDER BY dedup_key`)
	if err != nil {
		t.Fatalf("list retained rows: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan retained row: %v", err)
		}
		got = append(got, key)
	}
	want := []string{"old-crm-failed", "old-crm-pending", "old-report-failed", "old-report-pending", "recent-complete"}
	if len(got) != len(want) {
		t.Fatalf("retained keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("retained keys = %v, want %v", got, want)
		}
	}
}
