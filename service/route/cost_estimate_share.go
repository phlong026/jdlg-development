package route

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"service/model"

	"github.com/gin-gonic/gin"
)

const costEstimateShareMaxBodySize = 16 << 10

// Authentication verifies signatures by reading the body, so apply this before
// TokenAuthRequired rather than waiting until the endpoint decodes its DTO.
func costEstimateShareBodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		body := http.MaxBytesReader(c.Writer, c.Request.Body, costEstimateShareMaxBodySize)
		defer body.Close()
		payload, err := io.ReadAll(body)
		if err != nil {
			var max *http.MaxBytesError
			status := http.StatusBadRequest
			if errors.As(err, &max) {
				status = http.StatusRequestEntityTooLarge
			}
			c.AbortWithStatusJSON(status, gin.H{"code": status, "msg": "测算报告请求体无效或过大"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(payload))
		c.Next()
	}
}

var costEstimateBasicInfoLabels = []string{
	"所在地区", "建筑占地面积", "总建筑面积", "楼层数", "首层高度", "其他楼层高度", "地下室形式",
	"建筑风格", "结构形式", "屋顶形式", "外立面样式", "建筑基础", "装修标准", "门窗标准",
}

var costEstimateBreakdownNames = []string{
	"地基工程", "主体工程", "屋顶工程", "门窗工程", "外墙装饰", "水电及其它费用", "地下室工程",
}

func validCostShareText(text string, max int) bool {
	if strings.TrimSpace(text) != text || text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > max {
		return false
	}
	for _, char := range text {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func validCostShareAmount(text, unit string) bool {
	if !validCostShareText(text, 40) || !strings.HasSuffix(text, unit) {
		return false
	}
	number, err := strconv.ParseFloat(strings.TrimSuffix(text, unit), 64)
	return err == nil && number > 0 && validCostShareNumber(number, 1e12)
}

func validCostShareNumber(number, max float64) bool {
	return !math.IsNaN(number) && !math.IsInf(number, 0) && number >= 0 && number <= max
}

func validCostEstimateShare(result model.CostEstimateShareResult) bool {
	if result.Version != 4 || !validCostShareAmount(result.EstimatedTotalCost, "万元") || !validCostShareAmount(result.EstimatedUnitCost, "元/㎡") {
		return false
	}
	if !(result.TotalLabel == "参考总造价" && result.UnitLabel == "参考单价" || result.TotalLabel == "已测算部分合计" && result.UnitLabel == "已测算部分单价") {
		return false
	}
	if len(result.BasicInfo) != len(costEstimateBasicInfoLabels) || len(result.CostBreakdown) < 6 || len(result.CostBreakdown) > 7 || result.DynamicNotes == nil || len(result.DynamicNotes) > 10 {
		return false
	}
	for index, item := range result.BasicInfo {
		if item.Label != costEstimateBasicInfoLabels[index] || !validCostShareText(item.Value, 100) {
			return false
		}
	}
	for index, item := range result.CostBreakdown {
		if item.Name != costEstimateBreakdownNames[index] || !validCostShareText(item.AmountText, 40) || !validCostShareText(item.Note, 300) {
			return false
		}
		if !validCostShareNumber(item.Amount, 1e12) || !validCostShareNumber(item.Percent, 100) {
			return false
		}
	}
	for _, note := range result.DynamicNotes {
		if !validCostShareText(note, 300) {
			return false
		}
	}
	return true
}

func bindCostEstimateShare(c *gin.Context) (model.CostEstimateShareResult, bool) {
	var result model.CostEstimateShareResult
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, costEstimateShareMaxBodySize)
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
		c.JSON(status, gin.H{"code": status, "msg": "测算报告字段或体积无效"})
		return result, false
	}
	if !validCostEstimateShare(result) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "测算报告内容无效，请重新测算"})
		return result, false
	}
	return result, true
}

func costEstimateShareFailure(c *gin.Context, operation string, err error) {
	if errors.Is(err, model.ErrCostEstimateShareRateLimited) {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "分享过于频繁，请稍后再试"})
		return
	}
	if errors.Is(err, model.ErrCostEstimateShareQuotaExceeded) {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": 429, "msg": "有效分享报告已达上限，请撤销旧报告或等待过期"})
		return
	}
	if errors.Is(err, model.ErrCostEstimateShareNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "分享报告已失效或不存在"})
		return
	}
	log.Printf("[cost-share] error operation=%s reason=%v", operation, err)
	c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "分享报告暂不可用，请稍后重试"})
}

// The public group can only read a deliberately shared, immutable snapshot;
// it never reads private AI tasks, paid reports or a visitor's local estimate.
func RegisterCostEstimateShareRoutes(public, auth *gin.RouterGroup, store *model.CostEstimateShareRedisModel) {
	auth.POST("/ai/cost/shares", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		session := GetTokenCodeSession(c)
		if session == nil || session.UserID <= 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "请先登录后分享报告"})
			return
		}
		result, ok := bindCostEstimateShare(c)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		snapshot, err := store.Create(ctx, session.UserID, result)
		if err != nil {
			costEstimateShareFailure(c, "create", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"report_id": snapshot.ReportID, "expires_at": snapshot.ExpiresAt}})
	})
	public.GET("/ai/cost/shares/:id", trackingEntryRateLimit(), func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		snapshot, err := store.Get(ctx, c.Param("id"))
		if err != nil {
			costEstimateShareFailure(c, "read", err)
			return
		}
		if !validCostEstimateShare(snapshot.Result) {
			costEstimateShareFailure(c, "read", errors.New("stored report schema invalid"))
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"report_id": snapshot.ReportID, "expires_at": snapshot.ExpiresAt, "result": snapshot.Result}})
	})
	auth.DELETE("/ai/cost/shares/:id", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		session := GetTokenCodeSession(c)
		if session == nil || session.UserID <= 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "请先登录"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		if err := store.Revoke(ctx, c.Param("id"), session.UserID); err != nil {
			costEstimateShareFailure(c, "revoke", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"revoked": true}})
	})
}
