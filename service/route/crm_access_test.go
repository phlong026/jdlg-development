package route

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

// B1: intent is the replay contract shared by the retry endpoint and the compensation worker;
// field changes here are compatibility events.
func TestCRMAccessIntentPayloadContract(t *testing.T) {
	payload, err := json.Marshal(crmAccessIntent{UserID: 7, Revision: 4, Scope: "crm_only", Enabled: true, DisplayName: "顾问甲", EmployeeID: "e1", Role: "staff", CRMEnabled: true, OperatorID: 9})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"userId":7,"revision":4,"scope":"crm_only","enabled":true,"displayName":"顾问甲","employeeId":"e1","role":"staff","crmEnabled":true,"operatorId":9}`
	if string(payload) != want {
		t.Fatalf("intent payload changed: %s", payload)
	}
}

// Only 1054 (unknown column) degrades to the legacy write; other errors must not be swallowed.
func TestCRMAccessIntentMissingColumnDetection(t *testing.T) {
	if !crmIntentMissing(&mysql.MySQLError{Number: 1054, Message: "Unknown column 'intent' in 'field list'"}) {
		t.Fatal("1054 must degrade to legacy write")
	}
	for _, err := range []error{&mysql.MySQLError{Number: 1146, Message: "Table doesn't exist"}, errors.New("connection reset"), nil} {
		if crmIntentMissing(err) {
			t.Fatalf("must not degrade on %v", err)
		}
	}
}

// Automatic replays must carry the replay marker in the audit payload; manual retries must not.
func TestCRMAccessIntentAuditMarksAutomaticReplay(t *testing.T) {
	intent := crmAccessIntent{UserID: 7, Revision: 4, Scope: "crm_only", Enabled: true, CRMEnabled: true, OperatorID: 9}
	if got := crmIntentAudit(intent, false); strings.Contains(got, "replay") {
		t.Fatalf("manual audit must not carry replay marker: %s", got)
	}
	got := crmIntentAudit(intent, true)
	if !strings.Contains(got, `"replay":true`) || !strings.Contains(got, `"userId":7`) {
		t.Fatalf("automatic audit lost intent fields: %s", got)
	}
}
