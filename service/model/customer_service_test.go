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

// 用户 360 → "查看关联线索"：列表接口必须能按 user_id 过滤，且 user_id<=0 时不加条件（保持原有"全部线索"行为）
func TestCustomerLeadListSupportsUserIDFilter(t *testing.T) {
	modelSource, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	routeSource, err := os.ReadFile("../route/customer_service.go")
	if err != nil {
		t.Fatalf("read route customer_service.go: %v", err)
	}
	for _, want := range []string{
		"UserID      int64",
		"if params.UserID > 0 {",
		"where = append(where, `user_id = ?`)",
	} {
		if !strings.Contains(string(modelSource), want) {
			t.Fatalf("ListLeads should filter by user id, missing %q", want)
		}
	}
	if !strings.Contains(string(routeSource), `c.Query("user_id")`) {
		t.Fatal("lead list route should read the user_id query parameter")
	}
}

// 队列数量（Overview）与队列列表（ListLeads）必须共用同一段 SQL 条件，否则 Tab 角标会和列表条数对不上
func TestCustomerLeadOverviewSharesQueueConditions(t *testing.T) {
	source, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	content := string(source)
	for _, cond := range []string{"leadQueueHighCond", "leadQueueUnassignedCond", "leadQueuePartnerCond", "leadQueueIdentityPendCond", "leadQueueOverdueCond"} {
		// 定义 1 次 + ListLeads 用 1 次 + Overview 用 1 次
		if got := strings.Count(content, cond+" ") + strings.Count(content, cond+")"); got != 3 {
			t.Fatalf("%s 应当被定义一次并在列表与总览各使用一次（共 3 处），实际 %d 处", cond, got)
		}
	}
	for _, want := range []string{`json:"high_todo_count"`, `json:"unassigned_count"`, `json:"invalid_count"`, `json:"partner_todo_count"`, `json:"identity_pending_count"`} {
		if !strings.Contains(content, want) {
			t.Fatalf("overview should expose %s", want)
		}
	}
}

func TestCustomerLeadTypeOf(t *testing.T) {
	cases := map[string]string{
		"乡墅业主":   CustomerLeadTypeSales,
		" 乡墅业主 ": CustomerLeadTypeSales,
		"":       CustomerLeadTypeIdentityPending,
		"   ":    CustomerLeadTypeIdentityPending,
		"设计师":    CustomerLeadTypePartner,
		"施工队":    CustomerLeadTypePartner,
		"建材商":    CustomerLeadTypePartner,
		"机构用户":   CustomerLeadTypePartner,
		"其他身份":   CustomerLeadTypeSales,
	}
	for identity, want := range cases {
		if got := CustomerLeadTypeOf(identity); got != want {
			t.Fatalf("CustomerLeadTypeOf(%q) = %q, want %q", identity, got, want)
		}
	}
}

// 队列 SQL 里写死的 B 端名单必须与 PartnerIdentityTypes 一致；
// 销售队列与转化统计必须排除合作线索。
func TestCustomerLeadQueuesSeparatePartnerLeads(t *testing.T) {
	list := "'" + strings.Join(PartnerIdentityTypes, "', '") + "'"
	if leadSalesCond != "identity_type NOT IN ("+list+")" || leadPartnerCond != "identity_type IN ("+list+")" {
		t.Fatalf("queue identity lists drifted from PartnerIdentityTypes: %q / %q", leadSalesCond, leadPartnerCond)
	}
	for name, cond := range map[string]string{
		"todo": leadQueueTodoCond, "today": leadQueueTodayCond, "high": leadQueueHighCond,
		"overdue": leadQueueOverdueCond, "unassigned": leadQueueUnassignedCond, "identity_pending": leadQueueIdentityPendCond,
	} {
		if !strings.HasPrefix(cond, leadSalesCond) {
			t.Fatalf("sales queue %s must exclude partner leads: %q", name, cond)
		}
	}
	if !strings.HasPrefix(leadQueuePartnerCond, leadPartnerCond) {
		t.Fatalf("partner queue must select partner leads: %q", leadQueuePartnerCond)
	}
	source, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	content := string(source)
	for _, want := range []string{
		"status = 'converted' AND ` + leadSalesCond",
		"ADD COLUMN identity_type VARCHAR(32)",
		"l.updated_at = l.updated_at",
		"identity_type = IF(? = '', identity_type, ?)",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("customer_service.go missing %q", want)
		}
	}
}
