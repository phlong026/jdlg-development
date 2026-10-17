package route

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func TestTrackingLeadGateIdentityMatrix(t *testing.T) {
	cases := []struct {
		name         string
		state        string
		identityType string
		wantType     string
		wantMessage  string
	}{
		{name: "anonymous visitor blocked", state: "anonymous", identityType: "", wantMessage: "匿名"},
		{name: "deleted visitor blocked", state: "deleted", identityType: "乡墅业主", wantMessage: "匿名"},
		{name: "unselected identity becomes pending sales lead", state: "identified", identityType: "", wantType: model.CustomerLeadTypeIdentityPending},
		{name: "blank identity becomes pending sales lead", state: "identified", identityType: "  ", wantType: model.CustomerLeadTypeIdentityPending},
		{name: "designer becomes partner lead", state: "identified", identityType: "设计师", wantType: model.CustomerLeadTypePartner},
		{name: "builder crew becomes partner lead", state: "identified", identityType: "施工队", wantType: model.CustomerLeadTypePartner},
		{name: "supplier becomes partner lead", state: "identified", identityType: "建材商", wantType: model.CustomerLeadTypePartner},
		{name: "institution becomes partner lead", state: "identified", identityType: "机构用户", wantType: model.CustomerLeadTypePartner},
		{name: "padded bside still partner", state: "identified", identityType: " 设计师 ", wantType: model.CustomerLeadTypePartner},
		{name: "unknown identity fails closed", state: "identified", identityType: "魔法师", wantMessage: "无法识别的访客身份"},
		{name: "homeowner becomes sales lead", state: "identified", identityType: "乡墅业主", wantType: model.CustomerLeadTypeSales},
		{name: "padded homeowner becomes sales lead", state: "identified", identityType: " 乡墅业主 ", wantType: model.CustomerLeadTypeSales},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			leadType, err := trackingLeadGate(tc.state, tc.identityType)
			if tc.wantMessage != "" {
				if err == nil {
					t.Fatalf("expected block for state=%q identity=%q, got type %q", tc.state, tc.identityType, leadType)
				}
				if err.Status != 422 || !strings.Contains(err.Message, tc.wantMessage) {
					t.Fatalf("block mismatch: status=%d message=%q want contains %q", err.Status, err.Message, tc.wantMessage)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected pass for state=%q identity=%q, got %v", tc.state, tc.identityType, err)
			}
			if leadType != tc.wantType {
				t.Fatalf("lead type for identity=%q = %q, want %q", tc.identityType, leadType, tc.wantType)
			}
		})
	}
}

// 线索分流（model.PartnerIdentityTypes）与访问记录的 B 端判定（tracking.BSideIdentities）
// 必须是同一份名单，否则同一个人在两处会被分到不同类别。
func TestPartnerIdentitiesMatchTrackingBSide(t *testing.T) {
	if len(model.PartnerIdentityTypes) != len(tracking.BSideIdentities) {
		t.Fatalf("partner identities %v differ from tracking B-side %v", model.PartnerIdentityTypes, tracking.BSideIdentities)
	}
	for _, identity := range model.PartnerIdentityTypes {
		if !tracking.BSideIdentities[identity] {
			t.Fatalf("partner identity %q missing from tracking.BSideIdentities", identity)
		}
		if model.CustomerLeadTypeOf(identity) != model.CustomerLeadTypePartner {
			t.Fatalf("%q should map to partner lead", identity)
		}
	}
	if model.CustomerLeadTypeOf(trackingCustomerIdentity) != model.CustomerLeadTypeSales {
		t.Fatalf("%s should map to sales lead", trackingCustomerIdentity)
	}
}

func TestTrackingVisitorAccountKeyParsing(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want int64
		ok   bool
	}{
		{key: "u:42", want: 42, ok: true},
		{key: "a:anonymous", ok: false},
		{key: "u:0", ok: false},
		{key: "u:-1", ok: false},
		{key: "u:abc", ok: false},
		{key: "u:01", ok: false},
		{key: "", ok: false},
	} {
		id, ok := trackingVisitorAccount(tc.key)
		if ok != tc.ok || id != tc.want {
			t.Fatalf("trackingVisitorAccount(%q) = (%d, %v), want (%d, %v)", tc.key, id, ok, tc.want, tc.ok)
		}
	}
}

func TestBuildTrackingLeadSummary(t *testing.T) {
	visitor := tracking.Visitor{
		Key: "u:7", State: "identified", IdentityType: "乡墅业主",
		Sessions: 5, Views: 23, Visible: 43 * 60 * 1000,
	}
	visitor.Intent.Level = "high"
	visitor.Intent.Reasons = []string{"30天内5次会话", "已收藏素材"}
	got := buildTrackingLeadSummary(visitor, []tracking.VisitorContent{
		{Title: "三层宅基地方案"}, {Title: "造价测算说明"}, {Title: "施工流程"},
	})
	for _, want := range []string{"到访 5 次", "浏览 23 个页面", "约 43 分钟", "《三层宅基地方案》", "等 3 份素材", "意向：high", "30天内5次会话"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q missing %q", got, want)
		}
	}
	// Sub-minute visibility still reports one readable minute, not zero.
	visitor.Visible = 1200
	short := buildTrackingLeadSummary(visitor, nil)
	if !strings.Contains(short, "约 1 分钟") || !strings.Contains(short, "暂无素材浏览记录") {
		t.Fatal(short)
	}
	// A reason is optional punctuation, never a fabricated motive.
	visitor.Intent.Reasons = nil
	if plain := buildTrackingLeadSummary(visitor, nil); strings.Contains(plain, "（）") {
		t.Fatal(plain)
	}
}

func TestBuildTrackingLeadEvidenceTruncatesAndCarriesMetrics(t *testing.T) {
	visitor := tracking.Visitor{Key: "u:9", Sessions: 2, Views: 8, Visible: 90000}
	visitor.Intent.Level = "medium"
	visitor.Intent.Reasons = []string{"近期反复到访"}
	for i := 0; i < trackingLeadEvidenceCap+7; i++ {
		visitor.Intent.Evidence = append(visitor.Intent.Evidence, tracking.IntentEvidence{Kind: "session", ID: string(rune('a'+i%26)) + string(rune('0'+i%10)) + string(rune('x'+i%5)), Session: "s"})
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	evidence := buildTrackingLeadEvidence(visitor, []tracking.VisitorContent{{Title: "方案A", Type: "image", PageViews: 4, VisibleMS: 50000}}, 77, now)
	items, ok := evidence["intent_evidence"].([]map[string]interface{})
	if !ok || len(items) != trackingLeadEvidenceCap {
		t.Fatalf("evidence cap not applied: %d", len(items))
	}
	metrics, _ := evidence["metrics"].(map[string]interface{})
	if metrics["sessions"] != 2 || metrics["page_views"] != 8 || metrics["visible_ms"] != int64(90000) {
		t.Fatal(metrics)
	}
	if evidence["visitor_key"] != "u:9" || evidence["converted_by"] != int64(77) || evidence["converted_at"] != now.Format(time.RFC3339) {
		t.Fatal(evidence)
	}
	top, _ := evidence["top_contents"].([]map[string]interface{})
	if len(top) != 1 || top[0]["title"] != "方案A" || top[0]["page_views"] != 4 {
		t.Fatal(top)
	}
}

func TestTrackingLeadDisplayName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"林间访客", "林间访客"},
		{"已识别账号", ""},
		{"  ", ""},
		{"匿名访客 a1b2c3", ""},
		{" 乡墅业主老王 ", "乡墅业主老王"},
	} {
		if got := trackingLeadDisplayName(tc.in); got != tc.want {
			t.Fatalf("trackingLeadDisplayName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Contract test: the conversion endpoint exists behind the tracking
// management group and keeps the CRM handover rule, dedupe and evidence
// snapshot wired. Follows the repository's source-contract test style.
func TestTrackingLeadConversionContract(t *testing.T) {
	source, err := os.ReadFile("share_tracking_lead.go")
	if err != nil {
		t.Fatalf("read share_tracking_lead.go: %v", err)
	}
	content := string(source)
	for _, want := range []string{
		`"/visitors/:id/lead"`,
		"!scope.CRM",
		"crmEnabled()",
		"FindLatestOpenLeadID",
		"CreateLead",
		"CreateEvent",
		trackingLeadSource,
		trackingLeadEventType,
		"buildTrackingLeadEvidence",
		"buildTrackingLeadSummary",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("lead conversion contract missing %q", want)
		}
	}
	// The wiring must actually register the route with the lead model.
	wiring, err := os.ReadFile("share_tracking.go")
	if err != nil {
		t.Fatalf("read share_tracking.go: %v", err)
	}
	if !strings.Contains(string(wiring), "registerTrackingLeadRoutes(mg, svc, customerServiceModel, users)") {
		t.Fatal("registerTrackingLeadRoutes not wired into the management group")
	}
}

// The endpoint sits inside the no-store + auth-protected management group:
// an unauthenticated POST must be rejected with 401 (not 404), proving the
// route is registered and inherits the same cache isolation as the rest.
func TestTrackingLeadEndpointRegisteredBehindAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("test", cookie.NewStore([]byte("test-only-cookie-signing-key-32-bytes"))))
	RegisterTrackingRoutes(router.Group("/api/v1"), &model.UserModel{}, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/management/tracking/visitors/u:1/lead", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("cache header %q", got)
	}
}

// Contract: the recompute hook and the opt-in auto loop stay wired, and the
// manual endpoint shares one conversion pipeline with automation.
func TestTrackingLeadAutomationContract(t *testing.T) {
	auto, err := os.ReadFile("share_tracking_lead_auto.go")
	if err != nil {
		t.Fatalf("read share_tracking_lead_auto.go: %v", err)
	}
	for _, want := range []string{
		"feedbackIntentUpgrades",
		`"intent_upgrade"`,
		"ListEventsForLead",          // daily dedupe before appending
		"TRACKING_LEAD_AUTO_ENABLED", // opt-in, default off
		`"share_tracking_auto"`,
		"convertVisitorToLead", // auto reuses the manual pipeline
		"crmEnabled()",         // CRM handover stops automation cold
	} {
		if !strings.Contains(string(auto), want) {
			t.Fatalf("automation contract missing %q", want)
		}
	}
	rules, err := os.ReadFile("share_tracking_rules.go")
	if err != nil {
		t.Fatalf("read share_tracking_rules.go: %v", err)
	}
	if !strings.Contains(string(rules), "go feedbackIntentUpgrades(") {
		t.Fatal("recompute does not trigger the upgrade feedback hook")
	}
}
