package route

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func TestWritingPromptsRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{Tenant: "workbench"})
	registerContentWorkbenchRoutes(r.Group("/api/v1"), svc, &model.UserModel{}, nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/miniprogram/workbench/writing-prompts?user_id=1", nil)
	req.Header.Set("token", "client-asserted-token")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated writing-prompts: %d %s", w.Code, w.Body.String())
	}
}

func TestWritingPromptsServeCodeDefaultsWithoutTable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetWritingPromptCache()
	defer resetWritingPromptCache()
	r, _ := newWorkbenchHandlerFixture(t, &tracking.Scope{Actor: 7, Manage: true, Owners: []string{"owner-a"}})
	// writing-prompts 挂在带认证的 routes 版本；fixture 走无认证 handlers 组，这里补挂同前缀组。
	registerWritingPromptsRoute(r.Group("/workbench"), nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/workbench/writing-prompts", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("writing-prompts: %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Data struct {
			Prompts []writingPrompt `json:"prompts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	prompts := payload.Data.Prompts
	if len(prompts) != 3 {
		t.Fatalf("expected three default prompts, got %d", len(prompts))
	}
	for _, prompt := range prompts {
		if prompt.Key == "" || prompt.Heading == "" || prompt.Instruction == "" || prompt.Example == "" {
			t.Fatalf("incomplete default prompt: %+v", prompt)
		}
	}
	// 防臆造条款只存在于客户端常量，注册表下发内容不得包含它。
	if strings.Contains(w.Body.String(), "不得编造") {
		t.Fatalf("anti-fabrication clause must stay client-side, not served by the registry")
	}
}

type introPresetConnector struct {
	query string
	actor string
}

func (c *introPresetConnector) Connect(context.Context) (driver.Conn, error) {
	return introPresetConn{c}, nil
}
func (*introPresetConnector) Driver() driver.Driver { return materialScopeDriver{} }

type introPresetConn struct{ state *introPresetConnector }

func (introPresetConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (introPresetConn) Close() error                        { return nil }
func (introPresetConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c introPresetConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.query = query
	c.state.actor = args[0].Value.(string)
	return &introPresetRows{}, nil
}

type introPresetRows struct{ read bool }

func (*introPresetRows) Columns() []string { return []string{"id", "owner_kind", "name", "content"} }
func (*introPresetRows) Close() error      { return nil }
func (r *introPresetRows) Next(values []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	copy(values, []driver.Value{int64(1), "personal", "我的介绍", "介绍正文"})
	return nil
}
func TestIntroPresetsEnabledAppliesToPersonalAndTenant(t *testing.T) {
	connector := &introPresetConnector{}
	db := sql.OpenDB(connector)
	defer db.Close()
	presets := loadIntroPresets(context.Background(), db, 7)
	if len(presets) != 1 || presets[0].Content != "介绍正文" || connector.actor != "7" {
		t.Fatalf("unexpected presets or actor: %+v %+v", presets, connector)
	}
	required := "WHERE enabled=1 AND ((owner_kind='personal' AND owner_id=?) OR (owner_kind='tenant' AND owner_id='default'))"
	if !strings.Contains(connector.query, required) {
		t.Fatalf("disabled tenant presets can escape enabled filter: %s", connector.query)
	}
}
