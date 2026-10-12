package route

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"service/model"
)

// emptyAuditRows 空结果行集，列结构对齐 management_operation_audit 列表查询。
type emptyAuditRows struct{}

func (*emptyAuditRows) Columns() []string {
	return []string{"id", "user_id", "username", "method", "path", "target_type", "target_id", "status", "duration_ms", "ip", "created_at"}
}
func (*emptyAuditRows) Close() error              { return nil }
func (*emptyAuditRows) Next([]driver.Value) error { return io.EOF }

// openScriptedAuditDB 复用脚本化驱动，审计 COUNT 返回 0、列表返回空行集，
// 用于验证"已配置但无审计记录"是合法的空成功。
func openScriptedAuditDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("scripted-audit-empty-%d", time.Now().UnixNano())
	sql.Register(name, scriptedAuditDriver{})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("打开脚本化审计 DB 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type scriptedAuditDriver struct{}

func (scriptedAuditDriver) Open(string) (driver.Conn, error) { return scriptedRiskConn{}, nil }

// P1-02：审计模块未配置（auditModel 为 nil）时必须明确报不可用，
// 不能返回 200 + 空列表让后台误以为"已配置且无审计记录"。
func TestManagementAuditUnconfiguredMustNotPretendSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterManagementAuditRoutes(r.Group("/api/v1/management"), nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/management/audit", nil))

	if w.Code == http.StatusOK {
		t.Fatalf("审计模块未配置时应返回错误而不是伪成功，实际 HTTP %d: %s", w.Code, w.Body.String())
	}
}

// P1-02 反向边界：模块已配置且查询有效但无记录时，仍必须是 200 空列表成功。
func TestManagementAuditEmptyResultStillSucceeds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterManagementAuditRoutes(r.Group("/api/v1/management"), model.NewManagementAuditModel(openScriptedAuditDB(t)))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/management/audit?page=1&page_size=20", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("已配置但无审计记录应是合法空成功，实际 HTTP %d: %s", w.Code, w.Body.String())
	}
}
