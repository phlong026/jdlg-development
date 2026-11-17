package route

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"service/model"

	"github.com/gin-gonic/gin"
)

const renovationCostShareMaxBodySize = 64 << 10
const renovationCostShareMaxItems = 128
const renovationAuxiliaryReference = 12058
const renovationLaborReference = 28952

var renovationCostBasicInfoLabels = []string{
	"所在地区", "装修面积", "房屋状态", "楼层数", "层高", "面宽", "进深", "户型", "室内风格", "装修标准",
}

var renovationCostBreakdownNames = []string{"主材费", "辅材费", "人工费", "管理费", "设计费", "税费"}

type renovationCostSpaceDefinition struct {
	ID     string
	Name   string
	Groups map[string]string
}

var renovationCostSpaces = []renovationCostSpaceDefinition{
	{ID: "bedroom", Name: "卧室", Groups: map[string]string{"ceiling": "天花板", "wall": "墙面", "floor": "地面", "door": "室内门"}},
	{ID: "living", Name: "客餐厅", Groups: map[string]string{"ceiling": "天花板", "wall": "墙面", "floor": "地面", "threshold": "门槛石"}},
	{ID: "kitchen", Name: "厨房", Groups: map[string]string{"ceiling": "天花板", "wall": "墙面", "floor": "地面", "kitchenware": "厨具", "door": "室内门", "lighting": "灯具"}},
	{ID: "bathroom", Name: "卫生间", Groups: map[string]string{"ceiling": "天花板", "wall": "墙面", "floor": "地面", "sanitary": "卫浴用具", "door": "室内门"}},
	{ID: "balcony", Name: "阳台", Groups: map[string]string{"ceiling": "天花板", "wall": "墙面", "floor": "地面"}},
	{ID: "common", Name: "全屋公共", Groups: map[string]string{"utilities": "水电改造", "handling": "搬运清洁", "demolition": "拆改与保护"}},
}

var renovationCostUnits = map[string]bool{"㎡": true, "m": true, "项": true, "套": true, "个": true, "位": true, "块": true, "根": true}

func renovationCostShareBodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		body := http.MaxBytesReader(c.Writer, c.Request.Body, renovationCostShareMaxBodySize)
		defer body.Close()
		payload, err := io.ReadAll(body)
		if err != nil {
			var max *http.MaxBytesError
			status := http.StatusBadRequest
			if errors.As(err, &max) {
				status = http.StatusRequestEntityTooLarge
			}
			c.AbortWithStatusJSON(status, gin.H{"code": status, "msg": "装修报价请求体无效或过大"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(payload))
		c.Next()
	}
}

func renovationAmount(value *float64, max float64) (float64, bool) {
	if value == nil || !validCostShareNumber(*value, max) {
		return 0, false
	}
	return *value, true
}

func renovationAmountsEqual(left, right float64) bool {
	return math.Abs(left-right) <= 0.011
}

func renovationDisplayAmount(text, suffix string, allowZero bool) (float64, bool) {
	if !validCostShareText(text, 40) || !strings.HasSuffix(text, suffix) {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSuffix(text, suffix), 64)
	if err != nil || !validCostShareNumber(value, 1e12) || !allowZero && value == 0 {
		return 0, false
	}
	return value, true
}

func validRenovationCostEstimateShare(result model.CostEstimateShareResult) bool {
	if result.Version != 5 || result.QuoteType != "renovation" || result.TotalLabel != "装修参考总价" || result.UnitLabel != "参考单价" || result.Renovation == nil {
		return false
	}
	if len(result.BasicInfo) != len(renovationCostBasicInfoLabels) || len(result.CostBreakdown) != len(renovationCostBreakdownNames) || result.DynamicNotes == nil || len(result.DynamicNotes) != 0 {
		return false
	}
	for index, item := range result.BasicInfo {
		if item.Label != renovationCostBasicInfoLabels[index] || !validCostShareText(item.Value, 100) || item.FullWidth != (index == 0) {
			return false
		}
	}
	areaText := strings.TrimSuffix(result.BasicInfo[1].Value, "㎡")
	area, err := strconv.ParseFloat(areaText, 64)
	if err != nil || area <= 0 || !validCostShareNumber(area, 100000) {
		return false
	}

	breakdownTotal := 0.0
	for index, item := range result.CostBreakdown {
		if item.Name != renovationCostBreakdownNames[index] || !validCostShareNumber(item.Amount, 1e12) || !validCostShareNumber(item.Percent, 100) || !validCostShareText(item.AmountText, 40) || !validCostShareText(item.Note, 300) {
			return false
		}
		breakdownTotal += item.Amount
	}

	renovation := result.Renovation
	totalAmount, ok := renovationAmount(renovation.TotalAmount, 1e12)
	if !ok || (renovation.PricingVersion != "renovation-reference-v1" && renovation.PricingVersion != "renovation-dynamic-v2") || len(renovation.Spaces) != len(renovationCostSpaces) {
		return false
	}
	if renovation.PricingVersion == "renovation-dynamic-v2" {
		if renovation.QuantityBasis != "estimated" && renovation.QuantityBasis != "measured" && renovation.QuantityBasis != "mixed" {
			return false
		}
	} else if renovation.QuantityBasis != "" {
		return false
	}
	engineeringAmount := 0.0
	materialAmount := 0.0
	itemCount := 0
	itemIDs := make(map[string]bool)
	for spaceIndex, space := range renovation.Spaces {
		definition := renovationCostSpaces[spaceIndex]
		spaceAmount, valid := renovationAmount(space.Amount, 1e12)
		if !valid || space.Count == nil || *space.Count < 0 || *space.Count > 50 || space.ID != definition.ID || space.Name != definition.Name || space.Groups == nil || len(space.Groups) != len(definition.Groups) {
			return false
		}
		groupIDs := make(map[string]bool, len(space.Groups))
		groupTotal := 0.0
		for _, group := range space.Groups {
			expectedName, exists := definition.Groups[group.ID]
			groupAmount, amountValid := renovationAmount(group.Amount, 1e12)
			if !exists || expectedName != group.Name || groupIDs[group.ID] || !amountValid || group.Items == nil {
				return false
			}
			groupIDs[group.ID] = true
			itemTotal := 0.0
			for _, item := range group.Items {
				itemCount++
				quantity, quantityOK := renovationAmount(item.Quantity, 1e6)
				materialPrice, materialOK := renovationAmount(item.MaterialUnitPrice, 1e8)
				laborPrice, laborOK := renovationAmount(item.LaborAuxUnitPrice, 1e8)
				itemAmount, itemAmountOK := renovationAmount(item.Amount, 1e12)
				if itemCount > renovationCostShareMaxItems || !validCostShareText(item.ID, 100) || !validCostShareText(item.Name, 100) || !renovationCostUnits[item.Unit] || itemIDs[item.ID] || !quantityOK || !materialOK || !laborOK || !itemAmountOK || item.Included == nil {
					return false
				}
				expectedAmount := 0.0
				expectedMaterialAmount := 0.0
				if *item.Included {
					expectedAmount = math.Round(quantity * (materialPrice + laborPrice))
					expectedMaterialAmount = math.Round(quantity * materialPrice)
				}
				if !renovationAmountsEqual(itemAmount, expectedAmount) {
					return false
				}
				itemIDs[item.ID] = true
				itemTotal += itemAmount
				materialAmount += expectedMaterialAmount
			}
			if !renovationAmountsEqual(itemTotal, groupAmount) {
				return false
			}
			groupTotal += groupAmount
		}
		if !renovationAmountsEqual(groupTotal, spaceAmount) {
			return false
		}
		engineeringAmount += spaceAmount
	}
	nonMaterialAmount := engineeringAmount - materialAmount
	auxiliaryAmount := math.Round(nonMaterialAmount * renovationAuxiliaryReference / (renovationAuxiliaryReference + renovationLaborReference))
	laborAmount := nonMaterialAmount - auxiliaryAmount
	if !renovationAmountsEqual(result.CostBreakdown[0].Amount, materialAmount) || !renovationAmountsEqual(result.CostBreakdown[1].Amount, auxiliaryAmount) || !renovationAmountsEqual(result.CostBreakdown[2].Amount, laborAmount) {
		return false
	}
	for index, rate := range []float64{0.10, 0.04, 0.034} {
		amount := result.CostBreakdown[index+3].Amount
		expected := math.Round(engineeringAmount * rate)
		if amount != 0 && !renovationAmountsEqual(amount, expected) {
			return false
		}
	}
	if !renovationAmountsEqual(breakdownTotal, totalAmount) {
		return false
	}
	displayTotal, totalDisplayOK := renovationDisplayAmount(result.EstimatedTotalCost, "万元", true)
	displayUnit, unitDisplayOK := renovationDisplayAmount(result.EstimatedUnitCost, "元/㎡", true)
	if !totalDisplayOK || !unitDisplayOK || math.Abs(displayTotal*10000-totalAmount) > 50.011 || math.Abs(displayUnit-totalAmount/area) > 0.011 {
		return false
	}
	return true
}

func bindRenovationCostEstimateShare(c *gin.Context) (model.CostEstimateShareResult, bool) {
	var result model.CostEstimateShareResult
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, renovationCostShareMaxBodySize)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&result)
	if err == nil {
		var extra any
		err = decoder.Decode(&extra)
		if err == io.EOF {
			err = nil
		} else if err == nil {
			err = errors.New("multiple JSON values")
		}
	}
	if err != nil {
		var max *http.MaxBytesError
		status := http.StatusBadRequest
		if errors.As(err, &max) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"code": status, "msg": "装修报价字段或体积无效"})
		return result, false
	}
	if !validRenovationCostEstimateShare(result) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "装修报价内容无效，请重新测算"})
		return result, false
	}
	return result, true
}

func RegisterRenovationCostShareRoutes(auth *gin.RouterGroup, store *model.CostEstimateShareRedisModel) {
	auth.POST("/ai/renovation-cost/shares", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		session := GetTokenCodeSession(c)
		if session == nil || session.UserID <= 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "请先登录后分享报告"})
			return
		}
		result, ok := bindRenovationCostEstimateShare(c)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		snapshot, err := store.Create(ctx, session.UserID, result)
		if err != nil {
			costEstimateShareFailure(c, "create-renovation", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"report_id": snapshot.ReportID, "expires_at": snapshot.ExpiresAt}})
	})
}
