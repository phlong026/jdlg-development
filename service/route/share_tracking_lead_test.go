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
		name          string
		state         string
		identityType  string
		customerShare float64
		wantBlocked   bool
		wantMessage   string
	}{
		{name: "anonymous visitor blocked", state: "anonymous", identityType: "", wantBlocked: true, wantMessage: "匿名"},
		{name: "deleted visitor blocked", state: "deleted", identityType: "乡墅业主", wantBlocked: true, wantMessage: "匿名"},
		{name: "unselected identity below gray share blocked", state: "identified", identityType: "", customerShare: 0.69, wantBlocked: true, wantMessage: "尚未确认身份"},
		{name: "unselected identity at gray share passes", state: "identified", identityType: "", customerShare: 0.7},
		{name: "unselected identity with no signal blocked", state: "identified", identityType: "", customerShare: 0, wantBlocked: true, wantMessage: "尚未确认身份"},
		{name: "designer excluded regardless of share", state: "identified", identityType: "设计师", customerShare: 0.95, wantBlocked: true, wantMessage: "B端用户"},
		{name: "builder crew excluded", state: "identified", identityType: "施工队", wantBlocked: true, wantMessage: "B端用户"},
		{name: "supplier excluded", state: "identified", identityType: "建材商", wantBlocked: true, wantMessage: "B端用户"},
		{name: "institution excluded", state: "identified", identityType: "机构用户", wantBlocked: true, wantMessage: "B端用户"},
		{name: "padded bside still excluded", state: "identified", identityType: " 设计师 ", wantBlocked: true, wantMessage: "B端用户"},
		{name: "unknown identity fails closed", state: "identified", identityType: "魔法师", wantBlocked: true, wantMessage: "无法识别的访客身份"},
		{name: "homeowner passes regardless of share", state: "identified", identityType: "乡墅业主", customerShare: 0, wantBlocked: false},
		{name: "padded homeowner passes", state: "identified", identityType: " 乡墅业主 ", wantBlocked: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := trackingLeadGate(tc.state, tc.identityType, tc.customerShare)
			if tc.wantBlocked {
				if err == nil {
					t.Fatalf("expected block for state=%q identity=%q share=%.2f", tc.state, tc.identityType, tc.customerShare)
				}
				if err.Status != 422 || !strings.Contains(err.Message, tc.wantMessage) {
					t.Fatalf("block mismatch: status=%d message=%q want contains %q", err.Status, err.Message, tc.wantMessage)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected pass for state=%q identity=%q share=%.2f, got %v", tc.state, tc.identityType, tc.customerShare, err)
			}
		})
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
