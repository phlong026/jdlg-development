package route

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"service/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func validCostEstimateShareResult() model.CostEstimateShareResult {
	result := model.CostEstimateShareResult{
		Version: 4, EstimatedTotalCost: "80.00万元", EstimatedUnitCost: "2500.00元/㎡",
		TotalLabel: "参考总造价", UnitLabel: "参考单价",
		DynamicNotes: []string{"不同地基因方案不同，价格不同。"},
	}
	values := []string{"福建 / 泉州", "100㎡", "320㎡", "3层", "3.6m", "3.3m", "无地下室", "新闽派", "框架结构", "坡屋顶", "真石漆", "独立基础", "标准型", "标准型"}
	for i, label := range costEstimateBasicInfoLabels {
		result.BasicInfo = append(result.BasicInfo, model.CostEstimateBasicInfo{Label: label, Value: values[i], FullWidth: i == 0})
	}
	for i, name := range costEstimateBreakdownNames[:6] {
		amount := 100000.0
		if i == 0 {
			amount = 300000
		}
		result.CostBreakdown = append(result.CostBreakdown, model.CostEstimateBreakdown{Name: name, Amount: amount, AmountText: "10.00万元", Percent: 12, Note: "参考测算", Estimable: true})
	}
	return result
}

func newCostEstimateShareRouter(t *testing.T) (*gin.Engine, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return costEstimateShareRouterForStore(t, model.NewCostEstimateShareRedisModel(client)), mr
}

func costEstimateShareRouterForStore(t *testing.T, store *model.CostEstimateShareRedisModel) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	public := router.Group("/api/v1/miniprogram")
	auth := public.Group("")
	// Production uses TokenAuthRequired and MiniprogramBannedBlock on this group.
	auth.Use(func(c *gin.Context) {
		switch c.GetHeader("test-user") {
		case "42":
			c.Set("code_session", &model.CodeSession{UserID: 42})
		case "43":
			c.Set("code_session", &model.CodeSession{UserID: 43})
		}
	})
	RegisterCostEstimateShareRoutes(public, auth, store)
	RegisterRenovationCostShareRoutes(auth, store)
	return router
}

func costShareRequest(router *gin.Engine, method, path, body, user string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/api/v1/miniprogram/ai/cost/shares"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("test-user", user)
	}
	router.ServeHTTP(w, req)
	return w
}

func TestCostEstimateShareAnonymousReadsExactSnapshotWithoutOwner(t *testing.T) {
	router, _ := newCostEstimateShareRouter(t)
	result := validCostEstimateShareResult()
	body, _ := json.Marshal(result)
	if strings.Contains(string(body), "quoteType") || strings.Contains(string(body), "renovation") {
		t.Fatal("v4 payload shape changed", string(body))
	}
	created := costShareRequest(router, http.MethodPost, "", string(body), "42")
	if created.Code != 200 {
		t.Fatal(created.Code, created.Body.String())
	}
	var createResponse struct {
		Data struct {
			ReportID  string `json:"report_id"`
			ExpiresAt string `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createResponse); err != nil {
		t.Fatal(err)
	}
	id := createResponse.Data.ReportID
	if id == "" || createResponse.Data.ExpiresAt == "" {
		t.Fatal("missing share metadata")
	}
	for _, user := range []string{"", "43"} {
		read := costShareRequest(router, http.MethodGet, "/"+id+"?user_id=999", "", user)
		if read.Code != 200 || read.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(read.Code, read.Body.String())
		}
		var response struct {
			Data struct {
				Result model.CostEstimateShareResult `json:"result"`
			} `json:"data"`
		}
		if err := json.Unmarshal(read.Body.Bytes(), &response); err != nil || !reflect.DeepEqual(response.Data.Result, result) {
			t.Fatalf("snapshot mismatch: %+v %v", response, err)
		}
		if strings.Contains(read.Body.String(), "owner") || strings.Contains(read.Body.String(), "user_id") || strings.Contains(read.Body.String(), "token") {
			t.Fatal("private metadata leaked", read.Body.String())
		}
	}
	if got := costShareRequest(router, http.MethodDelete, "/"+id, "", "43"); got.Code != 404 {
		t.Fatal("foreign user revoked report", got.Code)
	}
	if got := costShareRequest(router, http.MethodDelete, "/"+id, "", "42"); got.Code != 200 {
		t.Fatal("owner cannot revoke", got.Code)
	}
	if got := costShareRequest(router, http.MethodGet, "/"+id, "", ""); got.Code != 404 {
		t.Fatal("revoked share remained public", got.Code)
	}
}

func TestCostEstimateShareRejectsAnonymousMutationAndStrictBody(t *testing.T) {
	router, _ := newCostEstimateShareRouter(t)
	body, _ := json.Marshal(validCostEstimateShareResult())
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		path := ""
		if method == http.MethodDelete {
			path = "/12345678901234567890123456789012"
		}
		if got := costShareRequest(router, method, path, string(body), ""); got.Code != 401 {
			t.Fatal("anonymous mutation accepted", got.Code)
		}
	}
	unknown := strings.TrimSuffix(string(body), "}") + `,"user_id":42}`
	for _, invalid := range []string{unknown, string(body) + " {}", `{"version":4}`, "null"} {
		if got := costShareRequest(router, http.MethodPost, "", invalid, "42"); got.Code != 400 {
			t.Fatal("invalid body accepted", got.Code, got.Body.String())
		}
	}
	if got := costShareRequest(router, http.MethodPost, "", `{"dynamicNotes":["`+strings.Repeat("x", 17000)+`"]}`, "42"); got.Code != 413 {
		t.Fatal("oversized body accepted", got.Code)
	}
}

func TestCostEstimateShareRejectsInvalidDTO(t *testing.T) {
	for name, mutate := range map[string]func(*model.CostEstimateShareResult){
		"version":           func(r *model.CostEstimateShareResult) { r.Version = 3 },
		"unknown label":     func(r *model.CostEstimateShareResult) { r.BasicInfo[0].Label = "手机号" },
		"duplicate info":    func(r *model.CostEstimateShareResult) { r.BasicInfo[1].Label = r.BasicInfo[0].Label },
		"negative amount":   func(r *model.CostEstimateShareResult) { r.CostBreakdown[0].Amount = -1 },
		"percent":           func(r *model.CostEstimateShareResult) { r.CostBreakdown[0].Percent = 101 },
		"unknown breakdown": func(r *model.CostEstimateShareResult) { r.CostBreakdown[0].Name = "私人任务" },
		"huge note":         func(r *model.CostEstimateShareResult) { r.DynamicNotes = []string{strings.Repeat("字", 301)} },
		"too many notes":    func(r *model.CostEstimateShareResult) { r.DynamicNotes = make([]string, 11) },
		"null notes":        func(r *model.CostEstimateShareResult) { r.DynamicNotes = nil },
		"invalid total":     func(r *model.CostEstimateShareResult) { r.EstimatedTotalCost = "NaN万元" },
		"invalid unit":      func(r *model.CostEstimateShareResult) { r.EstimatedUnitCost = "-1元/㎡" },
		"too many basic":    func(r *model.CostEstimateShareResult) { r.BasicInfo = append(r.BasicInfo, r.BasicInfo[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			result := validCostEstimateShareResult()
			mutate(&result)
			if validCostEstimateShare(result) {
				t.Fatal("invalid DTO accepted")
			}
		})
	}
	if !validCostEstimateShare(validCostEstimateShareResult()) {
		t.Fatal("valid DTO rejected")
	}
}

func TestCostEstimateShareRejectsOversizedTrailingJSON(t *testing.T) {
	router, _ := newCostEstimateShareRouter(t)
	body, _ := json.Marshal(validCostEstimateShareResult())
	request := string(body) + strings.Repeat(" ", 17000)
	if got := costShareRequest(router, http.MethodPost, "", request, "42"); got.Code != 413 {
		t.Fatal("oversized trailing content did not return 413", got.Code)
	}
}

func TestCostEstimateShareBoundsBodyBeforeRealAuthenticationMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	public := router.Group("/api/v1/miniprogram")
	auth := public.Group("")
	auth.Use(costEstimateShareBodyLimit(), TokenAuthRequired(nil))
	RegisterCostEstimateShareRoutes(public, auth, model.NewCostEstimateShareRedisModel(nil))
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		path := ""
		if method == http.MethodDelete {
			path = "/12345678901234567890123456789012"
		}
		got := costShareRequest(router, method, path, strings.Repeat("x", 17000), "")
		if got.Code != 413 {
			t.Fatal("oversized input reached authentication or was misreported", method, got.Code, got.Body.String())
		}
	}
	if got := costShareRequest(router, http.MethodPost, "", `{}`, ""); got.Code != 401 {
		t.Fatal("bounded request bypassed real authentication", got.Code)
	}
}

func TestCostEstimateShareRateLimitExpiryAndUnavailableStore(t *testing.T) {
	router, mr := newCostEstimateShareRouter(t)
	body, _ := json.Marshal(validCostEstimateShareResult())
	for i := 0; i < 10; i++ {
		if got := costShareRequest(router, http.MethodPost, "", string(body), "42"); got.Code != 200 {
			t.Fatal(i, got.Code)
		}
	}
	if got := costShareRequest(router, http.MethodPost, "", string(body), "42"); got.Code != 429 {
		t.Fatal("missing creation rate limit", got.Code)
	}
	if got := costShareRequest(router, http.MethodPost, "", string(body), "43"); got.Code != 200 {
		t.Fatal("unrelated account rate limited", got.Code)
	}
	var id string
	for _, key := range mr.Keys() {
		candidate := strings.TrimPrefix(key, "cost_estimate:share:")
		if len(candidate) == 32 && !strings.Contains(candidate, ":") {
			id = candidate
			break
		}
	}
	if id == "" {
		t.Fatal("no report snapshot was created")
	}
	mr.FastForward(model.CostEstimateShareTTL)
	if got := costShareRequest(router, http.MethodGet, "/"+id, "", ""); got.Code != 404 {
		t.Fatal("expired share readable", got.Code)
	}
	mr.SetError("redis unavailable with private infrastructure details")
	if got := costShareRequest(router, http.MethodGet, "/12345678901234567890123456789012", "", ""); got.Code != 503 || strings.Contains(got.Body.String(), "infrastructure") {
		t.Fatal("unavailable store error exposed or hidden", got.Code, got.Body.String())
	}
}

func TestCostEstimateShareRouterRestartCannotResetRateLimit(t *testing.T) {
	router, mr := newCostEstimateShareRouter(t)
	body, _ := json.Marshal(validCostEstimateShareResult())
	for i := 0; i < 10; i++ {
		if got := costShareRequest(router, http.MethodPost, "", string(body), "42"); got.Code != 200 {
			t.Fatal(i, got.Code, got.Body.String())
		}
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	restarted := costEstimateShareRouterForStore(t, model.NewCostEstimateShareRedisModel(client))
	if got := costShareRequest(restarted, http.MethodPost, "", string(body), "42"); got.Code != 429 {
		t.Fatal("restarting the application reset the rate limit", got.Code)
	}
	mr.FastForward(time.Minute)
	if got := costShareRequest(restarted, http.MethodPost, "", string(body), "42"); got.Code != 200 {
		t.Fatal("expired creation window is still blocked", got.Code)
	}
}

func TestCostEstimateShareActiveQuotaReturns429AndOwnerRevokeReleasesIt(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	store := model.NewCostEstimateShareRedisModel(client)
	result := validCostEstimateShareResult()
	var last *model.CostEstimateShareSnapshot
	for i := 0; i < model.CostEstimateShareMaxActive; i++ {
		if i%10 == 0 {
			mr.FastForward(time.Minute)
		}
		var err error
		last, err = store.Create(context.Background(), 42, result)
		if err != nil {
			t.Fatal(i, err)
		}
	}
	mr.FastForward(time.Minute)
	router := costEstimateShareRouterForStore(t, model.NewCostEstimateShareRedisModel(client))
	body, _ := json.Marshal(result)
	if got := costShareRequest(router, http.MethodPost, "", string(body), "42"); got.Code != 429 || !strings.Contains(got.Body.String(), "已达上限") {
		t.Fatal("active quota was not reported", got.Code, got.Body.String())
	}
	if got := costShareRequest(router, http.MethodDelete, "/"+last.ReportID, "", "42"); got.Code != 200 {
		t.Fatal("owner revoke failed", got.Code, got.Body.String())
	}
	if got := costShareRequest(router, http.MethodPost, "", string(body), "42"); got.Code != 200 {
		t.Fatal("owner revoke did not release the HTTP quota", got.Code, got.Body.String())
	}
}
