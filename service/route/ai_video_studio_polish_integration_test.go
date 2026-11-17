package route

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"service/model"
)

type studioPolishIntegrationFixture struct {
	db      *sql.DB
	store   *model.AIVideoStudioStore
	media   *model.AIVideoStudioMediaStore
	service *studioPolishService
	router  *gin.Engine
	userID  int64
}

func openStudioPolishIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("AI_VIDEO_STUDIO_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable AI_VIDEO_STUDIO_TEST_DSN")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil || config.Net != "tcp" || !strings.HasPrefix(config.Addr, "127.0.0.1:") || !strings.HasPrefix(config.DBName, "jdlg_studio_test") {
		t.Fatal("unsafe prompt polish fixture DSN")
	}
	adminConfig := config.Clone()
	adminConfig.DBName = ""
	admin, err := sql.Open("mysql", adminConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.Ping(); err != nil {
		admin.Close()
		t.Skipf("local prompt polish MySQL fixture unavailable: %v", err)
	}
	schema := "jdlg_studio_test_route_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	if _, err = admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	testConfig := config.Clone()
	testConfig.DBName = schema
	db, err := sql.Open("mysql", testConfig.FormatDSN())
	if err != nil {
		_, _ = admin.Exec("DROP DATABASE `" + schema + "`")
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec("DROP DATABASE `" + schema + "`")
		_ = admin.Close()
	})
	return db
}

func newStudioPolishIntegrationFixture(t *testing.T, provider http.Handler) *studioPolishIntegrationFixture {
	t.Helper()
	db := openStudioPolishIntegrationDB(t)
	store := model.NewAIVideoStudioStore(db)
	media := model.NewAIVideoStudioMediaStore(db)
	for _, init := range []func() error{store.InitTables, store.InitExecutionTables, store.InitWorkerTables, media.InitTable, model.NewStoneRecordModel(db).InitTable} {
		if err := init(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users(id BIGINT PRIMARY KEY,stones BIGINT UNSIGNED NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	userID := int64(970001)
	if _, err := db.Exec(`INSERT INTO users(id,stones) VALUES(?,100),(?,100)`, userID, userID+1); err != nil {
		t.Fatal(err)
	}
	unit := int64(7)
	prices, _ := json.Marshal([]model.AIVideoStudioPrice{{Code: studioPolishPriceCode, Label: "提示词优化", BillingUnit: "per_request", UnitStones: &unit, Enabled: true, Version: 7}})
	polish, _ := json.Marshal(model.AIVideoStudioPolishConfig{Enabled: true, Model: "fixture-model", TimeoutSeconds: 2})
	if _, err := db.Exec(`UPDATE ai_video_studio_config SET enabled=1,modes_json=?,prices_json=?,prompt_optimize_json=?,version=7 WHERE id=1`, `{"text":true,"reference":true}`, prices, polish); err != nil {
		t.Fatal(err)
	}
	providerServer := httptest.NewServer(provider)
	t.Cleanup(providerServer.Close)
	store.Ready = true
	service := &studioPolishService{
		store:  store,
		media:  media,
		client: providerServer.Client(),
		loadProvider: func(context.Context) (chatStreamProvider, error) {
			return chatStreamProvider{provider: "fixture", model: "unused-default", endpoint: providerServer.URL, apiKey: "fixture-key", completions: true}, nil
		},
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("test", cookie.NewStore([]byte("test-only-cookie-signing-key-32-bytes"))))
	client := router.Group("/ai-video-studio", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-User"), 10, 64)
		c.Set("user_id", id)
		c.Next()
	})
	registerStudioPolishRoutes(client, service)
	management := router.Group("/management/ai-video-studio", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", int64(990001))
		session.Set("username", "fixture-admin")
		_ = session.Save()
		c.Next()
	})
	registerStudioPolishManagement(management, service)
	return &studioPolishIntegrationFixture{db: db, store: store, media: media, service: service, router: router, userID: userID}
}

func (f *studioPolishIntegrationFixture) createDraft(t *testing.T, userID int64, mode string) *model.AIVideoStudioDraft {
	t.Helper()
	draft := &model.AIVideoStudioDraft{UserID: userID, Name: "庭院漫游", Mode: mode, Prompt: "庭院镜头向前", ReferencePrompt: "按图片1保持建筑，镜头向前", References: json.RawMessage(`[]`), Ratio: "16:9", Resolution: "768p", Duration: 4, Quantity: 1}
	if err := f.store.CreateDraft(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	return draft
}

func polishRequest(t *testing.T, router http.Handler, userID int64, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-User", strconv.FormatInt(userID, 10))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var envelope map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s %s response (%d): %v: %s", method, path, recorder.Code, err, recorder.Body.String())
	}
	return recorder.Code, envelope
}

func polishData(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing response data: %#v", envelope)
	}
	return data
}

func (f *studioPolishIntegrationFixture) quoteAndCreate(t *testing.T, draft *model.AIVideoStudioDraft, key string) (map[string]any, map[string]any) {
	t.Helper()
	status, response := polishRequest(t, f.router, draft.UserID, http.MethodPost, "/ai-video-studio/prompt-polish/quotes", map[string]any{"draft_id": draft.ID, "expected_revision": draft.Revision})
	if status != http.StatusOK {
		t.Fatalf("quote status=%d body=%#v", status, response)
	}
	quote := polishData(t, response)
	status, response = polishRequest(t, f.router, draft.UserID, http.MethodPost, "/ai-video-studio/prompt-polish/tasks", map[string]any{"quote_id": quote["quote_id"], "idempotency_key": key})
	if status != http.StatusOK {
		t.Fatalf("task status=%d body=%#v", status, response)
	}
	return quote, polishData(t, response)
}

func TestStudioPolishHTTPCompletesOnceAndReplaysSameTask(t *testing.T) {
	var calls atomic.Int32
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"保持庭院建筑稳定，镜头平稳向前推进。"}}]}`)
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	quote, task := fixture.quoteAndCreate(t, draft, "success-once")
	if got := task["original_prompt"]; got != draft.Prompt {
		t.Fatalf("original_prompt=%v", got)
	}
	if err := fixture.service.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	taskID := task["task_id"].(string)
	status, response := polishRequest(t, fixture.router, fixture.userID, http.MethodGet, "/ai-video-studio/prompt-polish/tasks/"+taskID, nil)
	completed := polishData(t, response)
	if status != http.StatusOK || completed["status"] != "completed" || completed["fee_status"] != "settled" || completed["optimized_prompt"] == "" {
		t.Fatalf("completed response=%d %#v", status, completed)
	}
	status, response = polishRequest(t, fixture.router, fixture.userID, http.MethodPost, "/ai-video-studio/prompt-polish/tasks", map[string]any{"quote_id": quote["quote_id"], "idempotency_key": "success-once"})
	if status != http.StatusOK || polishData(t, response)["task_id"] != taskID || calls.Load() != 1 {
		t.Fatalf("replay status=%d body=%#v calls=%d", status, response, calls.Load())
	}
	var balance, reserves, settles int
	if err := fixture.db.QueryRow(`SELECT stones FROM users WHERE id=?`, fixture.userID).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_prompt_fees WHERE task_id=? AND kind='reserve'`, taskID).Scan(&reserves); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_prompt_fees WHERE task_id=? AND kind='settle'`, taskID).Scan(&settles); err != nil {
		t.Fatal(err)
	}
	if balance != 93 || reserves != 1 || settles != 1 {
		t.Fatalf("balance=%d reserve=%d settle=%d", balance, reserves, settles)
	}
}

func TestStudioPolishHTTPKeepsSupplierBindingInsideDatabase(t *testing.T) {
	f := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("read-only checks must never call supplier") }))
	d := f.createDraft(t, f.userID, "text")
	quote, task := f.quoteAndCreate(t, d, "private-binding")
	taskID := task["task_id"].(string)
	var endpoint string
	if err := f.db.QueryRow(`SELECT JSON_UNQUOTE(JSON_EXTRACT(input_snapshot,'$.polish_endpoint')) FROM ai_video_studio_prompt_tasks WHERE id=?`, taskID).Scan(&endpoint); err != nil || endpoint == "" {
		t.Fatal("missing internal supplier binding", err)
	}
	responses := []map[string]any{quote, task}
	for _, path := range []string{"/ai-video-studio/prompt-polish/tasks/" + taskID, "/management/ai-video-studio/prompt-polish/tasks/" + taskID, "/management/ai-video-studio/prompt-polish/tasks"} {
		status, response := polishRequest(t, f.router, f.userID, http.MethodGet, path, nil)
		if status != http.StatusOK {
			t.Fatal(status, response)
		}
		responses = append(responses, response)
	}
	for _, response := range responses {
		data, _ := json.Marshal(response)
		for _, private := range []string{"polish_endpoint", "polish_provider", "polish_model", endpoint} {
			if bytes.Contains(data, []byte(private)) {
				t.Fatalf("response exposed supplier binding %s", private)
			}
		}
	}
}

func TestStudioPolishHTTPRejectsCrossAccountTaskRead(t *testing.T) {
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"庭院平稳推进"}}]}`)
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	_, task := fixture.quoteAndCreate(t, draft, "private-task")
	status, _ := polishRequest(t, fixture.router, fixture.userID+1, http.MethodGet, "/ai-video-studio/prompt-polish/tasks/"+task["task_id"].(string), nil)
	if status != http.StatusNotFound {
		t.Fatalf("cross-account task read status=%d", status)
	}
}

func TestStudioPolishHTTPRejectsStaleDraftRevision(t *testing.T) {
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"庭院平稳推进"}}]}`)
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	draft.Prompt = "已经改过的描述"
	if err := fixture.store.UpdateDraft(context.Background(), draft, draft.Revision); err != nil {
		t.Fatal(err)
	}
	status, _ := polishRequest(t, fixture.router, fixture.userID, http.MethodPost, "/ai-video-studio/prompt-polish/quotes", map[string]any{"draft_id": draft.ID, "expected_revision": 1})
	if status != http.StatusConflict {
		t.Fatalf("stale revision status=%d", status)
	}
}

func TestStudioPolishHTTPKeepsUnknownResultAndNeverCallsProviderAgain(t *testing.T) {
	var calls atomic.Int32
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	_, task := fixture.quoteAndCreate(t, draft, "unknown-once")
	if err := fixture.service.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	taskID := task["task_id"].(string)
	status, response := polishRequest(t, fixture.router, fixture.userID, http.MethodGet, "/ai-video-studio/prompt-polish/submissions/unknown-once", nil)
	if status != http.StatusOK || polishData(t, response)["status"] != "uncertain" || calls.Load() != 1 {
		t.Fatalf("unknown status=%d body=%#v calls=%d", status, response, calls.Load())
	}
	status, response = polishRequest(t, fixture.router, 0, http.MethodPost, "/management/ai-video-studio/prompt-polish/tasks/"+taskID+"/reconcile", map[string]any{"action": "confirmed_rejected", "evidence": "供应商工单已明确确认该请求未被受理"})
	if status != http.StatusOK || polishData(t, response)["status"] != "failed" {
		t.Fatalf("reconcile status=%d body=%#v", status, response)
	}
	status, _ = polishRequest(t, fixture.router, 0, http.MethodPost, "/management/ai-video-studio/prompt-polish/tasks/"+taskID+"/reconcile", map[string]any{"action": "confirmed_rejected", "evidence": "重复确认不应再次退款且应保持幂等"})
	if status != http.StatusOK {
		t.Fatalf("idempotent reconcile status=%d", status)
	}
	var balance, releases, audits int
	_ = fixture.db.QueryRow(`SELECT stones FROM users WHERE id=?`, fixture.userID).Scan(&balance)
	_ = fixture.db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_prompt_fees WHERE task_id=? AND kind='release'`, taskID).Scan(&releases)
	_ = fixture.db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_prompt_reconciliations WHERE task_id=?`, taskID).Scan(&audits)
	if balance != 100 || releases != 1 || audits != 1 || calls.Load() != 1 {
		t.Fatalf("balance=%d releases=%d audits=%d calls=%d", balance, releases, audits, calls.Load())
	}
}

func TestStudioPolishHTTPReleasesFeeForInvalidReferenceOutput(t *testing.T) {
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"图片2控制建筑，镜头向前推进。"}}]}`)
	}))
	draft := fixture.createDraft(t, fixture.userID, "reference")
	mediaID := strings.Repeat("a", 32)
	draft.References = json.RawMessage(fmt.Sprintf(`[{"media_id":%q,"media_type":"image","purpose":"保持建筑","position":1,"duration_seconds":0}]`, mediaID))
	if err := fixture.store.UpdateDraft(context.Background(), draft, draft.Revision); err != nil {
		t.Fatal(err)
	}
	asset := model.AIVideoStudioMedia{ID: mediaID, UserID: fixture.userID, OwnerID: "designer", Kind: "image", MIME: "image/jpeg", SizeBytes: 100, StorageKey: "fixture/image.jpg", Status: "ready", SHA256: strings.Repeat("b", 64), Name: "庭院.jpg"}
	if err := fixture.media.Insert(context.Background(), &asset); err != nil {
		t.Fatal(err)
	}
	_, task := fixture.quoteAndCreate(t, draft, "invalid-reference")
	if err := fixture.service.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := fixture.store.GetPolishTask(context.Background(), fixture.userID, task["task_id"].(string))
	if err != nil || stored.Status != "failed" || stored.FeeStatus != "released" {
		t.Fatalf("task=%+v err=%v", stored, err)
	}
	var balance int
	_ = fixture.db.QueryRow(`SELECT stones FROM users WHERE id=?`, fixture.userID).Scan(&balance)
	if balance != 100 {
		t.Fatalf("balance=%d", balance)
	}
}

func TestStudioPolishHTTPReleasesFeeForOverlongOutput(t *testing.T) {
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": strings.Repeat("景", 7001)}}}})
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	_, task := fixture.quoteAndCreate(t, draft, "overlong-result")
	if err := fixture.service.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := fixture.store.GetPolishTask(context.Background(), fixture.userID, task["task_id"].(string))
	if err != nil || stored.Status != "failed" || stored.FeeStatus != "released" || stored.OptimizedPrompt != "" {
		t.Fatalf("task=%+v err=%v", stored, err)
	}
}

func TestStudioPolishHTTPReadsExistingTaskAfterFeatureDisabled(t *testing.T) {
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"庭院平稳推进"}}]}`)
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	_, task := fixture.quoteAndCreate(t, draft, "read-after-disable")
	if _, err := fixture.db.Exec(`UPDATE ai_video_studio_config SET enabled=0,version=version+1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	status, response := polishRequest(t, fixture.router, fixture.userID, http.MethodGet, "/ai-video-studio/prompt-polish/tasks/"+task["task_id"].(string), nil)
	if status != http.StatusOK || polishData(t, response)["status"] != "queued" {
		t.Fatalf("read status=%d body=%#v", status, response)
	}
	status, response = polishRequest(t, fixture.router, fixture.userID, http.MethodGet, "/ai-video-studio/prompt-polish/submissions/read-after-disable", nil)
	if status != http.StatusOK || polishData(t, response)["task_id"] != task["task_id"] {
		t.Fatalf("submission status=%d body=%#v", status, response)
	}
}

func TestStudioPolishHTTPRejectsQuoteAfterPriceVersionChanges(t *testing.T) {
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"庭院平稳推进"}}]}`)
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	status, response := polishRequest(t, fixture.router, fixture.userID, http.MethodPost, "/ai-video-studio/prompt-polish/quotes", map[string]any{"draft_id": draft.ID, "expected_revision": draft.Revision})
	if status != http.StatusOK {
		t.Fatalf("quote status=%d body=%#v", status, response)
	}
	quote := polishData(t, response)
	if _, err := fixture.db.Exec(`UPDATE ai_video_studio_config SET version=version+1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	status, _ = polishRequest(t, fixture.router, fixture.userID, http.MethodPost, "/ai-video-studio/prompt-polish/tasks", map[string]any{"quote_id": quote["quote_id"], "idempotency_key": "changed-price"})
	if status != http.StatusConflict {
		t.Fatalf("changed price version status=%d", status)
	}
}

func TestStudioPolishHTTPRejectsExpiredQuote(t *testing.T) {
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"庭院平稳推进"}}]}`)
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	status, response := polishRequest(t, fixture.router, fixture.userID, http.MethodPost, "/ai-video-studio/prompt-polish/quotes", map[string]any{"draft_id": draft.ID, "expected_revision": draft.Revision})
	if status != http.StatusOK {
		t.Fatalf("quote status=%d body=%#v", status, response)
	}
	quote := polishData(t, response)
	if _, err := fixture.db.Exec(`UPDATE ai_video_studio_quotes SET expires_at=DATE_SUB(NOW(),INTERVAL 1 SECOND) WHERE id=?`, quote["quote_id"]); err != nil {
		t.Fatal(err)
	}
	status, _ = polishRequest(t, fixture.router, fixture.userID, http.MethodPost, "/ai-video-studio/prompt-polish/tasks", map[string]any{"quote_id": quote["quote_id"], "idempotency_key": "expired-quote"})
	if status != http.StatusConflict {
		t.Fatalf("expired quote status=%d", status)
	}
}

func TestStudioPolishHTTPReleasesQueuedTaskWhenTextModelChanges(t *testing.T) {
	var calls atomic.Int32
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"庭院平稳推进"}}]}`)
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	_, task := fixture.quoteAndCreate(t, draft, "changed-model")
	if _, err := fixture.db.Exec(`UPDATE ai_video_studio_config SET prompt_optimize_json=? WHERE id=1`, `{"enabled":true,"model":"another-model","timeout_seconds":2}`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := fixture.store.GetPolishTask(context.Background(), fixture.userID, task["task_id"].(string))
	if err != nil || stored.Status != "failed" || stored.FeeStatus != "released" || calls.Load() != 0 {
		t.Fatalf("task=%+v err=%v calls=%d", stored, err, calls.Load())
	}
	var balance int
	_ = fixture.db.QueryRow(`SELECT stones FROM users WHERE id=?`, fixture.userID).Scan(&balance)
	if balance != 100 {
		t.Fatalf("balance=%d", balance)
	}
}

func TestStudioPolishHTTPCancelReleasesQueuedTaskOnce(t *testing.T) {
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"庭院平稳推进"}}]}`)
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	_, task := fixture.quoteAndCreate(t, draft, "cancel-once")
	path := "/ai-video-studio/prompt-polish/tasks/" + task["task_id"].(string) + "/cancel"
	status, response := polishRequest(t, fixture.router, fixture.userID, http.MethodPost, path, map[string]any{})
	if status != http.StatusOK || polishData(t, response)["status"] != "cancelled" {
		t.Fatalf("cancel status=%d body=%#v", status, response)
	}
	status, _ = polishRequest(t, fixture.router, fixture.userID, http.MethodPost, path, map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("repeated cancel status=%d", status)
	}
	var balance, releases int
	_ = fixture.db.QueryRow(`SELECT stones FROM users WHERE id=?`, fixture.userID).Scan(&balance)
	_ = fixture.db.QueryRow(`SELECT COUNT(*) FROM ai_video_studio_prompt_fees WHERE task_id=? AND kind='release'`, task["task_id"]).Scan(&releases)
	if balance != 100 || releases != 1 {
		t.Fatalf("balance=%d releases=%d", balance, releases)
	}
}

func TestStudioPolishHTTPManagementListIncludesUserAndOriginalPrompt(t *testing.T) {
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"庭院平稳推进"}}]}`)
	}))
	draft := fixture.createDraft(t, fixture.userID, "text")
	fixture.quoteAndCreate(t, draft, "management-list")
	status, response := polishRequest(t, fixture.router, 0, http.MethodGet, "/management/ai-video-studio/prompt-polish/tasks?page=1&page_size=20", nil)
	data := polishData(t, response)
	items, _ := data["items"].([]any)
	if status != http.StatusOK || len(items) != 1 {
		t.Fatalf("list status=%d body=%#v", status, response)
	}
	item := items[0].(map[string]any)
	if item["user_id"] != float64(fixture.userID) || item["original_prompt"] != draft.Prompt {
		t.Fatalf("item=%#v", item)
	}
}

func TestStudioPolishHTTPProviderTimeoutBecomesUncertain(t *testing.T) {
	fixture := newStudioPolishIntegrationFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	if _, err := fixture.db.Exec(`UPDATE ai_video_studio_config SET prompt_optimize_json=? WHERE id=1`, `{"enabled":true,"model":"fixture-model","timeout_seconds":1}`); err != nil {
		t.Fatal(err)
	}
	draft := fixture.createDraft(t, fixture.userID, "text")
	_, task := fixture.quoteAndCreate(t, draft, "provider-timeout")
	if err := fixture.service.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := fixture.store.GetPolishTask(context.Background(), fixture.userID, task["task_id"].(string))
	if err != nil || stored.Status != "uncertain" || stored.FeeStatus != "reserved" {
		t.Fatalf("task=%+v err=%v", stored, err)
	}
}
