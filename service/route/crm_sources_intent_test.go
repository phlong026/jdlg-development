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
