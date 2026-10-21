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
	for _, cond := range []string{"leadQueueHighCond", "leadQueueUnassignedCond"} {
		// 定义 1 次 + ListLeads 用 1 次 + Overview 用 1 次
		if got := strings.Count(content, cond); got != 3 {
			t.Fatalf("%s 应当被定义一次并在列表与总览各使用一次（共 3 处），实际 %d 处", cond, got)
		}
	}
	for _, want := range []string{`json:"high_todo_count"`, `json:"unassigned_count"`, `json:"invalid_count"`} {
		if !strings.Contains(content, want) {
			t.Fatalf("overview should expose %s", want)
		}
	}
}

// Contract: the 48h grace period and NULL-safe unassigned check stay in the
// shared queue criteria used by both Overview and ListLeads; the funnel keeps
// its per-account dedupe and windowed queries.
func TestLeadQueueCriteriaContract(t *testing.T) {
	source, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	content := string(source)
	for _, want := range []string{
		"INTERVAL 48 HOUR",                            // overdue grace period
		"leadQueueOverdueCond",                        // shared constant, not inline SQL
		"COALESCE(assigned_to, '') = ''",              // NULL-safe unassigned
		"COUNT(DISTINCT user_id) FROM customer_leads", // funnel dedupes by account
		"FunnelMetrics(start, end time.Time)",         // windowed signature
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("queue criteria contract missing %q", want)
		}
	}
	if strings.Count(content, "next_followup_at IS NULL OR next_followup_at <= NOW()") != 0 {
		t.Fatal("legacy no-grace overdue SQL still present")
	}
}

// 漏斗按"人"计：进入客服 = 登录人数 + 匿名会话数（匿名无法去重，按次计）；
// 后续各层都要求登录，按账号去重；转化率都相对第一层。
func TestBuildCustomerFunnel(t *testing.T) {
	metrics := buildCustomerFunnel(customerFunnelCounts{
		EnteredUsers:      4,
		AnonymousSessions: 6,
		Steps: map[string]int64{
			"structured_requirement_collected": 2,
			"human_handoff_triggered":          1,
			"lead_submit":                      2,
		},
		Converted: 1,
	})
	want := []struct {
		key       string
		label     string
		count     int64
		anonymous int64
		rate      float64
	}{
		{"welcome_shown", "进入客服", 10, 6, 100},
		{"structured_requirement_collected", "完成需求采集", 2, 0, 20},
		{"generate_click", "点击生成方案", 0, 0, 0},
		{"human_handoff_triggered", "转人工承接", 1, 0, 10},
		{"lead_submit", "提交线索", 2, 0, 20},
		{"converted", "已成交", 1, 0, 10},
	}
	if len(metrics) != len(want) {
		t.Fatalf("got %d metrics, want %d", len(metrics), len(want))
	}
	for i, w := range want {
		got := metrics[i]
		if got.Key != w.key || got.Label != w.label || got.Count != w.count || got.AnonymousSessions != w.anonymous || got.ConversionRate != w.rate {
			t.Fatalf("metric[%d] = %+v, want %+v", i, got, w)
		}
	}
}

func TestBuildCustomerFunnelEmpty(t *testing.T) {
	for _, metric := range buildCustomerFunnel(customerFunnelCounts{}) {
		if metric.Count != 0 || metric.ConversionRate != 0 {
			t.Fatalf("empty funnel should be all zero, got %+v", metric)
		}
	}
}

// 口径契约："提交线索"数留资动作（lead_submit 事件，按账号去重），
// 同一账号再次留资只会更新旧线索，若按 customer_leads.created_at 计数会恒为 0。
// 线索列表按最近一次留资时间排序，让再次留资的老客户冒到前面。
func TestFunnelAndListCountResubmissions(t *testing.T) {
	source, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	content := string(source)
	for _, want := range []string{
		"COUNT(DISTINCT user_id) FROM customer_service_events WHERE event_type = 'lead_submit' AND user_id > 0", // 提交线索按留资动作计
		"COUNT(DISTINCT CASE WHEN user_id > 0 THEN user_id END)",                                                // 登录用户去重
		"COUNT(DISTINCT CASE WHEN user_id = 0 THEN session_no END)",                                             // 匿名按会话
		"FIELD(intent_level, 'high', 'medium', 'low'), last_submitted_at DESC",                                  // 列表冒出再次留资
		"AS last_submitted_at",
		"e.user_id = customer_leads.user_id", // 只认线索本人的留资事件
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("funnel/list contract missing %q", want)
		}
	}
	if strings.Contains(content, "COUNT(DISTINCT user_id) FROM customer_leads WHERE user_id > 0") {
		t.Fatal("提交线索不应再按 customer_leads 新建时间计数")
	}
}

// 线索列表按客户合并：同一小程序用户只取最近一条，并返回 submission_count。
// user_id=0 的匿名线索不能合并（按 id 区分）。
func TestCustomerLeadListDedupesByUser(t *testing.T) {
	source, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	content := string(source)
	for _, want := range []string{
		"Dedupe string",
		"groupByUser",
		"ROW_NUMBER() OVER",
		"COUNT(DISTINCT CASE WHEN user_id > 0 THEN CONCAT('u', user_id) ELSE CONCAT('l', id) END)",
		"AS submission_count",
		`json:"submission_count,omitempty"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("lead user dedupe contract missing %q", want)
		}
	}
	routeSource, err := os.ReadFile("../route/customer_service.go")
	if err != nil {
		t.Fatalf("read route customer_service.go: %v", err)
	}
	if !strings.Contains(string(routeSource), `c.DefaultQuery("dedupe", "all")`) {
		t.Fatal("lead list route should read the dedupe query parameter")
	}
}

// 「去客户档案跟进」需要客户 ID：线索列表/详情必须从同步交付表带出 customer_id，
// 且表缺失或线索未同步时不能阻断线索列表。
func TestCustomerLeadExposesCRMArchiveID(t *testing.T) {
	source, err := os.ReadFile("customer_service.go")
	if err != nil {
		t.Fatalf("read customer_service.go: %v", err)
	}
	content := string(source)
	for _, want := range []string{
		`json:"customer_id,omitempty"`,
		"attachLeadCustomerIDs",
		"FROM customer_lead_crm_delivery WHERE lead_id IN (",
		"AND customer_id <> ''",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("lead customer id contract missing %q", want)
		}
	}
	if strings.Count(content, "m.attachLeadCustomerIDs(") < 2 {
		t.Fatal("线索列表和详情都应补 customer_id")
	}
}
