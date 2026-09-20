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
	"service/model"
	"service/report"
)

func TestHouseRouteQueuesNewProtocolAndRejectsUnknownFields(t *testing.T) {
	store := &decisionRouteStore{}
	svc, err := report.NewService(report.Config{Enabled: true, DecisionEnabled: true, HouseEnabled: true}, store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("code_session", &model.CodeSession{UserID: 7}) })
	v1 := r.Group("/api/v1")
	RegisterReportRoutes(v1.Group("/public"), v1.Group("/private"), v1, &ReportModule{Service: svc, DecisionEnabled: true, HouseEnabled: true})
	body := `{"primary_goal":"space","permanent_count":2,"returning_count":4,"buildable_scope":"unknown","return_frequency":"holidays","floors":2,"bedrooms":4,"flexible_use":"no","priorities":["老人方便"],"house":{"style":"新闽派","courtyard":"yes","parking":"unknown"}}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/private/reports/house", strings.NewReader(body)))
	if w.Code != 202 || store.created == nil || store.created.ProtocolVersion != report.ProtocolHouseV1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, bad := range []string{body + ` {}`, strings.Replace(body, `"style"`, `"secret":true,"style"`, 1)} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/private/reports/house", strings.NewReader(bad)))
		if w.Code != 400 {
			t.Fatalf("invalid body: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestHouseQuestionnaireIsPublicAndCreationRequiresLogin(t *testing.T) {
	r := reportTestRouter(&ReportModule{HouseEnabled: true})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/public/reports/house/questions", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"full_pages":null`) || !strings.Contains(w.Body.String(), `house.rooms`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/private/reports/house", strings.NewReader(`{}`)))
	if w.Code != 401 {
		t.Fatalf("anonymous accepted: %d", w.Code)
	}
	r = reportTestRouter(&ReportModule{})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/public/reports/house/questions", nil))
	if w.Code == 200 {
		t.Fatal("disabled house questionnaire exposed")
	}
}

func TestArtifactVersionTracksHouseLayout(t *testing.T) {
	dir := t.TempDir()
	cfg := component.ReportPDFConfig{ScriptPath: filepath.Join(dir, "render.py"), FontPath: filepath.Join(dir, "font.ttf")}
	for _, name := range []string{"render.py", "font.ttf", "decision_v5.py", "house_v1.py"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := reportArtifactVersion(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "house_v1.py"), []byte("changed layout"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := reportArtifactVersion(cfg)
	if err != nil || first == second {
		t.Fatal("house layout did not invalidate artifact")
	}
}

func TestHouseProviderMissingConfigurationDoesNotStartProcessors(t *testing.T) {
	if _, err := configuredHouseOrchestrator(context.Background(), nil, nil, nil, nil, t.TempDir()); err == nil {
		t.Fatal("missing configuration accepted")
	}
}
