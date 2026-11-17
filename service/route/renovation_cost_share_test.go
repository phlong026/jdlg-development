package route

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"service/model"

	"github.com/gin-gonic/gin"
)

func renovationFloat(value float64) *float64 { return &value }
func renovationInt(value int) *int           { return &value }
func renovationBool(value bool) *bool        { return &value }

func validRenovationShareResult() model.CostEstimateShareResult {
	result := model.CostEstimateShareResult{
		Version: 5, QuoteType: "renovation", EstimatedTotalCost: "0.23万元", EstimatedUnitCost: "23.48元/㎡",
		TotalLabel: "装修参考总价", UnitLabel: "参考单价", DynamicNotes: []string{},
		Renovation: &model.RenovationCostEstimate{PricingVersion: "renovation-reference-v1", TotalAmount: renovationFloat(2348)},
	}
	values := []string{"福建 / 泉州", "100㎡", "新房", "1层", "3m", "10m", "10m", "3室2厅1厨2卫1阳台", "现代简约", "标准"}
	for index, label := range renovationCostBasicInfoLabels {
		result.BasicInfo = append(result.BasicInfo, model.CostEstimateBasicInfo{Label: label, Value: values[index], FullWidth: index == 0})
	}
	amounts := []float64{1000, 294, 706, 200, 80, 68}
	for index, name := range renovationCostBreakdownNames {
		result.CostBreakdown = append(result.CostBreakdown, model.CostEstimateBreakdown{
			Name: name, Amount: amounts[index], AmountText: fmt.Sprintf("%.2f元", amounts[index]), Percent: amounts[index] / 2348 * 100, Note: "参考测算", Estimable: true,
		})
	}
	for _, definition := range renovationCostSpaces {
		space := model.RenovationCostSpace{ID: definition.ID, Name: definition.Name, Count: renovationInt(0), Amount: renovationFloat(0), Groups: []model.RenovationCostGroup{}}
		groupIDs := make([]string, 0, len(definition.Groups))
		for groupID := range definition.Groups {
			groupIDs = append(groupIDs, groupID)
		}
		sort.Strings(groupIDs)
		for _, groupID := range groupIDs {
			space.Groups = append(space.Groups, model.RenovationCostGroup{ID: groupID, Name: definition.Groups[groupID], Amount: renovationFloat(0), Items: []model.RenovationCostItem{}})
		}
		result.Renovation.Spaces = append(result.Renovation.Spaces, space)
	}
	result.Renovation.Spaces[0].Count = renovationInt(1)
	result.Renovation.Spaces[0].Amount = renovationFloat(2000)
	for index := range result.Renovation.Spaces[0].Groups {
		if result.Renovation.Spaces[0].Groups[index].ID != "ceiling" {
			continue
		}
		result.Renovation.Spaces[0].Groups[index] = model.RenovationCostGroup{
			ID: "ceiling", Name: "天花板", Amount: renovationFloat(2000), Items: []model.RenovationCostItem{{
				ID: "bedroom.ceiling.fixture", Name: "顶面工程", Unit: "项", Quantity: renovationFloat(1), MaterialUnitPrice: renovationFloat(1000), LaborAuxUnitPrice: renovationFloat(1000), Amount: renovationFloat(2000), Included: renovationBool(true),
			}},
		}
	}
	return result
}

func renovationShareRequest(router *gin.Engine, body, user string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/miniprogram/ai/renovation-cost/shares", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("test-user", user)
	}
	router.ServeHTTP(w, req)
	return w
}

func TestRenovationCostShareStrictContractAndBalance(t *testing.T) {
	valid := validRenovationShareResult()
	if !validRenovationCostEstimateShare(valid) {
		t.Fatal("valid renovation snapshot rejected")
	}
	for name, mutate := range map[string]func(*model.CostEstimateShareResult){
		"quote type":    func(r *model.CostEstimateShareResult) { r.QuoteType = "building" },
		"basic order":   func(r *model.CostEstimateShareResult) { r.BasicInfo[0].Label = "手机号" },
		"basic width":   func(r *model.CostEstimateShareResult) { r.BasicInfo[1].FullWidth = true },
		"notes":         func(r *model.CostEstimateShareResult) { r.DynamicNotes = []string{"不应公开"} },
		"unknown unit":  func(r *model.CostEstimateShareResult) { r.Renovation.Spaces[0].Groups[0].Items[0].Unit = "台" },
		"unknown group": func(r *model.CostEstimateShareResult) { r.Renovation.Spaces[0].Groups[0].ID = "private" },
		"missing group": func(r *model.CostEstimateShareResult) {
			r.Renovation.Spaces[0].Groups = r.Renovation.Spaces[0].Groups[:len(r.Renovation.Spaces[0].Groups)-1]
		},
		"negative quantity": func(r *model.CostEstimateShareResult) {
			r.Renovation.Spaces[0].Groups[0].Items[0].Quantity = renovationFloat(-1)
		},
		"nan amount": func(r *model.CostEstimateShareResult) { r.Renovation.TotalAmount = renovationFloat(math.NaN()) },
		"infinite price": func(r *model.CostEstimateShareResult) {
			r.Renovation.Spaces[0].Groups[0].Items[0].MaterialUnitPrice = renovationFloat(math.Inf(1))
		},
		"forged balanced item amount": func(r *model.CostEstimateShareResult) {
			r.Renovation.Spaces[0].Groups[0].Items[0].MaterialUnitPrice = renovationFloat(1)
			r.Renovation.Spaces[0].Groups[0].Items[0].LaborAuxUnitPrice = renovationFloat(1)
		},
		"excluded amount": func(r *model.CostEstimateShareResult) {
			r.Renovation.Spaces[0].Groups[0].Items[0].Included = renovationBool(false)
		},
		"group imbalance": func(r *model.CostEstimateShareResult) {
			r.Renovation.Spaces[0].Groups[0].Amount = renovationFloat(1999)
		},
		"space imbalance":   func(r *model.CostEstimateShareResult) { r.Renovation.Spaces[0].Amount = renovationFloat(1999) },
		"breakdown balance": func(r *model.CostEstimateShareResult) { r.CostBreakdown[0].Amount++ },
		"forged balanced breakdown distribution": func(r *model.CostEstimateShareResult) {
			r.CostBreakdown[0].Amount++
			r.CostBreakdown[1].Amount--
		},
		"fee formula":    func(r *model.CostEstimateShareResult) { r.CostBreakdown[3].Amount = 199 },
		"total balance":  func(r *model.CostEstimateShareResult) { r.Renovation.TotalAmount = renovationFloat(2347) },
		"missing number": func(r *model.CostEstimateShareResult) { r.Renovation.Spaces[0].Amount = nil },
		"duplicate item": func(r *model.CostEstimateShareResult) {
			r.Renovation.Spaces[0].Groups[0].Items = append(r.Renovation.Spaces[0].Groups[0].Items, r.Renovation.Spaces[0].Groups[0].Items[0])
		},
		"too many items": func(r *model.CostEstimateShareResult) {
			for index := 0; index < renovationCostShareMaxItems; index++ {
				r.Renovation.Spaces[0].Groups[0].Items = append(r.Renovation.Spaces[0].Groups[0].Items, model.RenovationCostItem{
					ID: fmt.Sprintf("bedroom.ceiling.extra-%03d", index), Name: "附加项", Unit: "项", Quantity: renovationFloat(0), MaterialUnitPrice: renovationFloat(0), LaborAuxUnitPrice: renovationFloat(0), Amount: renovationFloat(0), Included: renovationBool(true),
				})
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			result := validRenovationShareResult()
			mutate(&result)
			if validRenovationCostEstimateShare(result) {
				t.Fatal("invalid renovation DTO accepted")
			}
		})
	}
	zero := validRenovationShareResult()
	zero.EstimatedTotalCost = "0.00万元"
	zero.EstimatedUnitCost = "0.00元/㎡"
	zero.Renovation.TotalAmount = renovationFloat(0)
	zero.Renovation.Spaces[0].Count = renovationInt(0)
	zero.Renovation.Spaces[0].Amount = renovationFloat(0)
	zero.Renovation.Spaces[0].Groups[0].Amount = renovationFloat(0)
	zero.Renovation.Spaces[0].Groups[0].Items[0].Amount = renovationFloat(0)
	zero.Renovation.Spaces[0].Groups[0].Items[0].Included = renovationBool(false)
	for index := range zero.CostBreakdown {
		zero.CostBreakdown[index].Amount = 0
		zero.CostBreakdown[index].AmountText = "0.00元"
		zero.CostBreakdown[index].Percent = 0
	}
	if !validRenovationCostEstimateShare(zero) {
		t.Fatal("legal zero-space and zero-fee snapshot rejected")
	}
}

func TestRenovationPricingVersionsRemainReadable(t *testing.T) {
	for _, version := range []string{"renovation-reference-v1", "renovation-dynamic-v2"} {
		snapshot := validRenovationShareResult()
		snapshot.Renovation.PricingVersion = version
		if version == "renovation-dynamic-v2" {
			snapshot.Renovation.QuantityBasis = "mixed"
		}
		if !validRenovationCostEstimateShare(snapshot) || !validStoredCostEstimateShare(snapshot) {
			t.Fatalf("valid immutable snapshot rejected: %s", version)
		}
	}
	snapshot := validRenovationShareResult()
	snapshot.Renovation.PricingVersion = "unknown-dynamic-v99"
	if validRenovationCostEstimateShare(snapshot) {
		t.Fatal("unknown pricing version accepted")
	}
}

func TestRenovationCostShareUsesIndependentBoundedAuthenticatedCreate(t *testing.T) {
	router, _ := newCostEstimateShareRouter(t)
	result := validRenovationShareResult()
	for index := 0; index < 110; index++ {
		result.Renovation.Spaces[0].Groups[0].Items = append(result.Renovation.Spaces[0].Groups[0].Items, model.RenovationCostItem{
			ID: fmt.Sprintf("bedroom.ceiling.zero-%03d", index), Name: strings.Repeat("项", 40), Unit: "项", Quantity: renovationFloat(0), MaterialUnitPrice: renovationFloat(0), LaborAuxUnitPrice: renovationFloat(0), Amount: renovationFloat(0), Included: renovationBool(true),
		})
	}
	body, _ := json.Marshal(result)
	if len(body) <= costEstimateShareMaxBodySize || len(body) >= renovationCostShareMaxBodySize {
		t.Fatalf("fixture must prove independent bounds, got %d bytes", len(body))
	}
	created := renovationShareRequest(router, string(body), "42")
	if created.Code != http.StatusOK {
		t.Fatal(created.Code, created.Body.String())
	}
	var response struct {
		Data struct {
			ReportID string `json:"report_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil || response.Data.ReportID == "" {
		t.Fatal("missing identity", err)
	}
	read := costShareRequest(router, http.MethodGet, "/"+response.Data.ReportID, "", "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"quoteType":"renovation"`) {
		t.Fatal("v5 snapshot was not publicly readable", read.Code, read.Body.String())
	}
}

func TestRenovationCostShareBoundsBeforeAuthenticationAndRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	public := router.Group("/api/v1/miniprogram")
	auth := public.Group("")
	auth.Use(renovationCostShareBodyLimit(), TokenAuthRequired(nil))
	RegisterRenovationCostShareRoutes(auth, model.NewCostEstimateShareRedisModel(nil))
	if got := renovationShareRequest(router, strings.Repeat("x", renovationCostShareMaxBodySize+1), ""); got.Code != http.StatusRequestEntityTooLarge {
		t.Fatal("oversized anonymous body reached authentication", got.Code)
	}
	if got := renovationShareRequest(router, `{}`, ""); got.Code != http.StatusUnauthorized {
		t.Fatal("bounded anonymous body bypassed authentication", got.Code)
	}

	fullRouter, _ := newCostEstimateShareRouter(t)
	valid, _ := json.Marshal(validRenovationShareResult())
	unknown := strings.TrimSuffix(string(valid), "}") + `,"owner_id":42}`
	if got := renovationShareRequest(fullRouter, unknown, "42"); got.Code != http.StatusBadRequest {
		t.Fatal("unknown field accepted", got.Code, got.Body.String())
	}
	nestedUnknown := strings.Replace(string(valid), `"included":true`, `"included":true,"laborPrice":100`, 1)
	for _, invalid := range []string{nestedUnknown, string(valid) + ` {}`} {
		if got := renovationShareRequest(fullRouter, invalid, "42"); got.Code != http.StatusBadRequest {
			t.Fatal("nested unknown field or trailing JSON accepted", got.Code, got.Body.String())
		}
	}
}
