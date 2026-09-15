package model

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestNormalizeCustomerLeadStatus(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty defaults to new", input: "", want: CustomerLeadStatusNew},
		{name: "known contacted", input: "contacted", want: CustomerLeadStatusContacted},
		{name: "known converted", input: "converted", want: CustomerLeadStatusConverted},
		{name: "unknown falls back to new", input: "archived", want: CustomerLeadStatusNew},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeCustomerLeadStatus(tt.input); got != tt.want {
				t.Fatalf("NormalizeCustomerLeadStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeCustomerIntentLevel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: CustomerIntentLevelMedium},
		{input: "high", want: CustomerIntentLevelHigh},
		{input: "low", want: CustomerIntentLevelLow},
		{input: "unknown", want: CustomerIntentLevelMedium},
	}

	for _, tt := range tests {
		if got := NormalizeCustomerIntentLevel(tt.input); got != tt.want {
			t.Fatalf("NormalizeCustomerIntentLevel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCustomerLeadCreateUsesSessionAndTaskDedupe(t *testing.T) {
	source, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	content := string(source)
	for _, want := range []string{
		"SessionNo",
		"session_no VARCHAR(128)",
		"findExistingLeadID",
		"findLatestOpenLeadID",
		"updateExistingLead",
		"source_task_no = ?",
		"session_no = ?",
		"status IN ('new', 'contacted')",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("customer service lead model should contain %q", want)
		}
	}
}

func TestCustomerLeadDedupeFallsBackToOpenLead(t *testing.T) {
	source, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	content := string(source)
	for _, want := range []string{
		"return m.findLatestOpenLeadID(userID)",
		"ORDER BY updated_at DESC, id DESC",
		"status IN ('new', 'contacted')",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("customer service lead dedupe fallback should contain %q", want)
		}
	}
}

func TestCustomerLeadUpdateProtectsContactedStatus(t *testing.T) {
	source, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	content := string(source)
	if !strings.Contains(content, "status IN ('contacted', 'converted', 'invalid')") {
		t.Fatal("updateExistingLead should not downgrade contacted leads")
	}
}

func TestCustomerLeadDetailUsesEventsTimeline(t *testing.T) {
	modelSource, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	routeSource, err := os.ReadFile("../route/customer_service.go")
	if err != nil {
		t.Fatalf("read route customer_service.go: %v", err)
	}
	content := string(modelSource) + "\n" + string(routeSource)
	for _, want := range []string{
		"type CustomerServiceEvent struct",
		"func (m *CustomerServiceModel) GetLead",
		"func (m *CustomerServiceModel) ListEventsForLead",
		"customer_service_events",
		`leads.GET("/:id"`,
		`"lead": lead`,
		`"events": events`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("customer service detail should contain %q", want)
		}
	}
}

func TestCustomerLeadEventsTimelineUsesLeadIDOnly(t *testing.T) {
	source, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	content := string(source)
	if !strings.Contains(content, "WHERE lead_id = ?") {
		t.Fatal("ListEventsForLead should query by lead_id")
	}
	if strings.Contains(content, `strings.Join(where, " OR ")`) {
		t.Fatal("ListEventsForLead should not merge events by session/task fallback")
	}
	if !strings.Contains(content, "func (m *CustomerServiceModel) backfillLeadIDForEvents") {
		t.Fatal("CreateLead should backfill lead_id for pre-lead events")
	}
}

func TestCustomerLeadV3OperationalFeatures(t *testing.T) {
	modelSource, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	routeSource, err := os.ReadFile("../route/customer_service.go")
	if err != nil {
		t.Fatalf("read route customer_service.go: %v", err)
	}
	content := string(modelSource) + "\n" + string(routeSource)
	for _, want := range []string{
		"customer_lead_followups",
		"type CustomerFunnelMetric struct",
		"func (m *CustomerServiceModel) FunnelMetrics",
		"func (m *CustomerServiceModel) CreateFollowup",
		"func (m *CustomerServiceModel) BuildLeadSummary",
		"func (m *CustomerServiceModel) AnalyzeLeadIntentFromInput",
		`leads.GET("/funnel"`,
		`leads.POST("/:id/followups"`,
		`"intent_analysis": analysis`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("customer service v3 should contain %q", want)
		}
	}
}

func TestCreateFollowupRejectsInvalidLeadIDAsBadRequestInput(t *testing.T) {
	m := &CustomerServiceModel{}
	_, err := m.CreateFollowup(&CustomerLeadFollowupCreateInput{LeadID: 0, Status: CustomerLeadStatusNew})
	if !errors.Is(err, ErrInvalidCustomerLeadID) {
		t.Fatalf("expected ErrInvalidCustomerLeadID, got %v", err)
	}
}

func TestCustomerLeadIntentAnalysisRules(t *testing.T) {
	m := &CustomerServiceModel{}
	analysis := m.AnalyzeLeadIntentFromInput(&CustomerLeadCreateInput{
		Phone:         "13800000000",
		Wechat:        "wx-user",
		DemandSummary: "想了解造价和施工落地",
		SourceTaskNo:  "task-1",
		HouseFloors:   "三层",
		LandWidth:     "12米",
		LandDepth:     "10米",
		Remark:        "handoff_human",
	})
	if analysis.Level != CustomerIntentLevelHigh {
		t.Fatalf("expected high intent, got %q with score %d", analysis.Level, analysis.Score)
	}
	if analysis.Score < 60 {
		t.Fatalf("expected score >= 60, got %d", analysis.Score)
	}
}

func TestCustomerLeadIntentAnalysisDoesNotTreatDesignerQuestionAsHigh(t *testing.T) {
	m := &CustomerServiceModel{}
	analysis := m.AnalyzeLeadIntentFromInput(&CustomerLeadCreateInput{
		DemandSummary: "你们这边设计师咋样",
		IntentLevel:   CustomerIntentLevelLow,
	})
	if analysis.Level == CustomerIntentLevelHigh {
		t.Fatalf("designer question should not be high intent, got score %d", analysis.Score)
	}
}
