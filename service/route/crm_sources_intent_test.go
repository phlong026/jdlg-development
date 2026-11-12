package route

import (
	"os"
	"strings"
	"testing"
)

func TestCRMIntentOnlyPassesKnownLevels(t *testing.T) {
	for in, want := range map[string]string{
		"high": "high", " Medium ": "medium", "LOW": "low",
		"": "", "unknown": "", "random": "",
	} {
		if got := crmIntent(in); got != want {
			t.Fatalf("crmIntent(%q) = %q, want %q", in, got, want)
		}
	}
}

// 契约：投递载荷必须带上线索的意向等级，否则 CRM 只能把所有新客户当"中意向"。
func TestCRMSourcePayloadCarriesIntent(t *testing.T) {
	source, err := os.ReadFile("crm_sources.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(source)
	for _, want := range []string{"l.intent_level", `"intent": crmIntent(`, "unionid", `"unionid":`} {
		if !strings.Contains(content, want) {
			t.Fatalf("crm_sources.go missing %q", want)
		}
	}
}

func TestCRMSourceUsesConsentFilteredPhoneForPayloadAndCheckpoint(t *testing.T) {
	source, err := os.ReadFile("crm_sources.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(source)
	if !strings.Contains(content, `CustomerLeadExportablePhoneSQL("l")`) {
		t.Fatal("CRM source must export only user-provided phone numbers")
	}
	if strings.Contains(content, "CONCAT_WS('|',l.name,l.phone") {
		t.Fatal("CRM checkpoint hash must not use the raw lead phone")
	}
}

func TestCRMSourceIdentityChangesReplayWithoutBreakingCheckpointContracts(t *testing.T) {
	source, err := os.ReadFile("crm_sources.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(source)
	for _, want := range []string{"COALESCE(l.identity_type,'')", `"identityType":`, "externalId", "history_cursor", "CustomerLeadExportablePhoneSQL"} {
		if !strings.Contains(content, want) {
			t.Fatalf("CRM identity delivery contract missing %q", want)
		}
	}
	if strings.Count(content, "COALESCE(l.identity_type,'')") < 3 {
		t.Fatal("identity must be selected and included in both source-hash expressions")
	}
}
