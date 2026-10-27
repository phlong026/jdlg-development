package notification

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// These statements encode the cross-transaction lock order. Keep this small
// source-level guard because the CI suite has no MySQL lock-wait harness.
func TestSQLLockOrderContracts(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	source, err := os.ReadFile(strings.TrimSuffix(file, "store_lock_order_test.go") + "store.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	assertBefore := func(first, second string) {
		t.Helper()
		a, b := strings.Index(text, first), strings.Index(text, second)
		if a < 0 || b < 0 || a >= b {
			t.Fatalf("expected %q before %q", first, second)
		}
	}
	assertBefore("SELECT event_id FROM mini_notification_event_jobs", "SELECT payload FROM tracking_records")
	assertBefore("cancel_jobs", "cancel_inbox")
	assertBefore("delete_event_lock", "delete_event_deliveries")
	assertBefore("provider_receipt_store", "provider_receipt_result")
	assertBefore("SELECT delivery_id,notification_id,recipient_id", "SELECT event_id,recipient_id,kind,title,body")

	receiptStart := strings.Index(text, "func (s *SQLStore) RecordProviderReceipt")
	receiptEnd := strings.Index(text[receiptStart:], "func (s *SQLStore) ListInbox") + receiptStart
	if receiptStart < 0 || receiptEnd < receiptStart || strings.Contains(text[receiptStart:receiptEnd], "BeginTx") {
		t.Fatal("receipt persistence and delivery reconciliation must not hold both row locks in one transaction")
	}
	payloadStart := strings.Index(text, "SELECT payload FROM tracking_records")
	payloadEnd := strings.Index(text[payloadStart:], ").Scan(&payload)") + payloadStart
	if payloadStart < 0 || payloadEnd < payloadStart || strings.Contains(text[payloadStart:payloadEnd], "FOR UPDATE") {
		t.Fatal("raw notice payload read must not lock tracking_records after the job row is locked")
	}
	deliveryStart := strings.Index(text, "SELECT delivery_id,notification_id,recipient_id")
	deliveryEnd := strings.Index(text[deliveryStart:], "LIMIT 1 FOR UPDATE") + deliveryStart
	if deliveryStart < 0 || deliveryEnd < deliveryStart || strings.Contains(text[deliveryStart:deliveryEnd], "JOIN mini_notifications") {
		t.Fatal("delivery claim must lock only the delivery row")
	}
	noticeStart := strings.Index(text, "SELECT event_id,recipient_id,kind,title,body")
	noticeEnd := strings.Index(text[noticeStart:], ").Scan(") + noticeStart
	if noticeStart < 0 || noticeEnd < noticeStart || strings.Contains(text[noticeStart:noticeEnd], "FOR UPDATE") {
		t.Fatal("delivery claim must read the inbox without locking it")
	}
}
