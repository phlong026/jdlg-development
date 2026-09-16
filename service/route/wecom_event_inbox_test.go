package route

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"service/component/virtualpay"
	"service/reportwecom"
)

func TestForwardWecomEventResignsAndDelivers(t *testing.T) {
	const token = "forward-test-token"
	const encrypted = "ENC-BODY"
	var gotSig, gotTs, gotNonce, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.URL.Query().Get("msg_signature")
		gotTs = r.URL.Query().Get("timestamp")
		gotNonce = r.URL.Query().Get("nonce")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
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
	if gotBody != encrypted {
		t.Fatal("encrypted body not forwarded verbatim")
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
