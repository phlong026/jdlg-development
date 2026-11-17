package route

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"service/model"

	"github.com/gin-gonic/gin"
)

func TestRenovationDynamicJavaScriptSnapshots(t *testing.T) {
	// The Mini Program test recalculates these fixtures with the real geometry,
	// pricing and snapshot adapter, so this checks both sides of the same contract.
	payload, err := os.ReadFile(filepath.Join("..", "..", "miniprogram", "tests", "fixtures", "renovation-dynamic-v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string                        `json:"name"`
		Report model.CostEstimateShareResult `json:"report"`
	}
	if err := json.Unmarshal(payload, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected measured and estimated snapshots, got %d", len(cases))
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			if fixture.Report.Renovation.PricingVersion != "renovation-dynamic-v2" || !validStoredCostEstimateShare(fixture.Report) {
				t.Fatal("real JavaScript dynamic snapshot rejected")
			}
			data, err := json.Marshal(fixture.Report)
			if err != nil || len(data) >= renovationCostShareMaxBodySize {
				t.Fatal("dynamic snapshot cannot be sent within the existing request limit")
			}
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest("POST", "/api/v1/miniprogram/ai/renovation-cost/shares", strings.NewReader(string(data)))
			_, ok := bindRenovationCostEstimateShare(context)
			if !ok {
				t.Fatalf("valid dynamic snapshot body rejected: %s", recorder.Body.String())
			}
		})
	}
}
