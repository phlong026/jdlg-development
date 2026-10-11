package route

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 这组测试守住"零值陷阱"：Gin 的 binding:"required" 会把 false / 0 当作缺失，
// 对 bool、数字字段直接加 required 会让"取消精选""余额设为 0"这类合法请求必然 400。
// 修复方式是改用指针：required 只检查指针是否为 nil。

func bindManagementJSON(t *testing.T, body string, dst any) error {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c.ShouldBindJSON(dst)
}

func TestSetFeaturedRequestAcceptsFalse(t *testing.T) {
	var req setFeaturedRequest
	if err := bindManagementJSON(t, `{"is_featured": false}`, &req); err != nil {
		t.Fatalf("取消精选（is_featured=false）应当合法，实际报错: %v", err)
	}
	if req.IsFeatured == nil || *req.IsFeatured {
		t.Fatalf("期望 is_featured 解析为 false，实际: %v", req.IsFeatured)
	}
}

func TestSetFeaturedRequestAcceptsTrue(t *testing.T) {
	var req setFeaturedRequest
	if err := bindManagementJSON(t, `{"is_featured": true}`, &req); err != nil {
		t.Fatalf("设为精选应当合法，实际报错: %v", err)
	}
	if req.IsFeatured == nil || !*req.IsFeatured {
		t.Fatalf("期望 is_featured 解析为 true，实际: %v", req.IsFeatured)
	}
}

func TestSetFeaturedRequestRejectsMissingField(t *testing.T) {
	var req setFeaturedRequest
	if err := bindManagementJSON(t, `{}`, &req); err == nil {
		t.Fatal("缺少 is_featured 字段必须被拒绝，不能静默当成 false")
	}
}

func TestSetStonesRequestAcceptsZero(t *testing.T) {
	var req setStonesRequest
	if err := bindManagementJSON(t, `{"stones": 0}`, &req); err != nil {
		t.Fatalf("把余额直接设为 0 应当合法，实际报错: %v", err)
	}
	if req.Stones == nil || *req.Stones != 0 {
		t.Fatalf("期望 stones 解析为 0，实际: %v", req.Stones)
	}
}

func TestSetStonesRequestAcceptsPositive(t *testing.T) {
	var req setStonesRequest
	if err := bindManagementJSON(t, `{"stones": 120, "remark": "补偿"}`, &req); err != nil {
		t.Fatalf("正数余额应当合法，实际报错: %v", err)
	}
	if req.Stones == nil || *req.Stones != 120 || req.Remark != "补偿" {
		t.Fatalf("解析结果不符: %+v", req)
	}
}

func TestSetStonesRequestRejectsNegative(t *testing.T) {
	var req setStonesRequest
	if err := bindManagementJSON(t, `{"stones": -1}`, &req); err == nil {
		t.Fatal("负数余额必须被拒绝")
	}
}

func TestSetStonesRequestRejectsMissingField(t *testing.T) {
	var req setStonesRequest
	if err := bindManagementJSON(t, `{"remark": "x"}`, &req); err == nil {
		t.Fatal("缺少 stones 字段必须被拒绝，不能静默当成 0 清空用户余额")
	}
}
