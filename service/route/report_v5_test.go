package route

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/component"
	"service/function"
	"service/model"
	"service/report"
)

func TestDecisionPaymentIdentityRetainsOwnerCheckWithoutUnionIDGate(t *testing.T) {
	user := &model.User{ID: 7, OpenID: "owner"}
	for _, protocol := range []string{report.ProtocolDecisionV5, "report-service-v1"} {
		err := validateReportProtocolIdentity(user, &function.WechatCode2SessionResponse{OpenID: "owner"}, protocol)
		if (err == nil) != (protocol == report.ProtocolDecisionV5) {
			t.Fatalf("protocol=%s err=%v", protocol, err)
		}
		if validateReportProtocolIdentity(user, &function.WechatCode2SessionResponse{OpenID: "other"}, protocol) == nil {
			t.Fatal("wrong WeChat owner accepted")
		}
		if validateReportProtocolIdentity(nil, nil, protocol) == nil {
			t.Fatal("missing identity accepted")
		}
	}
}

func TestArtifactVersionTracksDecisionRenderer(t *testing.T) {
	dir := t.TempDir()
	cfg := component.ReportPDFConfig{ScriptPath: filepath.Join(dir, "render.py"), FontPath: filepath.Join(dir, "font.ttf")}
	for _, name := range []string{"render.py", "font.ttf", "decision_v5.py"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := reportArtifactVersion(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "decision_v5.py"), []byte("new layout"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := reportArtifactVersion(cfg)
	if err != nil || before == after {
		t.Fatal("changed decision layout reused an old PDF cache")
	}
}

type decisionRouteStore struct {
	report.Store
	report.DecisionContextStore
	created *report.Report
}

func (s *decisionRouteStore) CreateOrGet(_ context.Context, record *report.Report) (*report.Report, bool, error) {
	s.created = record
	return record, true, nil
}
func (*decisionRouteStore) GetDecisionContext(context.Context, int64, string) (report.DecisionContext, error) {
	return report.DecisionContext{}, report.ErrNotFound
}

func TestDecisionRouteReturnsQueuedWithoutRunningModel(t *testing.T) {
	s := &decisionRouteStore{}
	svc, err := report.NewService(report.Config{Enabled: true, DecisionEnabled: true}, s, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("code_session", &model.CodeSession{UserID: 7}) })
	v1 := r.Group("/api/v1")
	RegisterReportRoutes(v1.Group("/public"), v1.Group("/private"), v1, &ReportModule{Service: svc, DecisionEnabled: true})
	body := `{"primary_goal":"space","permanent_count":2,"returning_count":4,"buildable_scope":"unknown","return_frequency":"holidays","flexible_use":"no","priorities":["老人方便"]}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/private/reports/v5", strings.NewReader(body)))
	if w.Code != 202 || s.created == nil || s.created.ProtocolVersion != report.ProtocolDecisionV5 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	for _, bad := range []string{body + ` {}`, strings.Replace(body, `"primary_goal"`, `"extra":1,"primary_goal"`, 1)} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/private/reports/v5", strings.NewReader(bad)))
		if w.Code != 400 {
			t.Fatalf("malformed body accepted: %d", w.Code)
		}
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/public/reports/v5/questions?primary_goal=family", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "family_perspectives") {
		t.Fatalf("missing family questions: %d %s", w.Code, w.Body.String())
	}
}

func TestDecisionQuestionsAvailableBeforeLoginButSubmissionIsPrivate(t *testing.T) {
	r := reportTestRouter(&ReportModule{DecisionEnabled: true})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/public/reports/v5/questions?primary_goal=space", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "budget_scope") {
		t.Fatalf("public questionnaire unavailable: %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/private/reports/v5", strings.NewReader(`{}`)))
	if w.Code != 401 {
		t.Fatalf("anonymous submission accepted: %d", w.Code)
	}
}
