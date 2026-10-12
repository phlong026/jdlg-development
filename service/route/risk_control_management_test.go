package route

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// scriptQueryResult 按查询内容返回脚本化结果：
//   - device-risk 概览的 device_risk_users 查询 → 注入失败（复现"查询失败被记成 0"）
//   - 管理审计 COUNT → 返回 0，审计列表 → 返回空行集（已配置但无记录的合法空态）
//   - 其余 COUNT 查询 → 返回固定计数值
func scriptQueryResult(query string) (driver.Rows, error) {
	if strings.Contains(query, "GROUP BY device_id, user_id") {
		return nil, errors.New("injected: device risk query failed")
	}
	if strings.Contains(query, "management_operation_audit") {
		if strings.Contains(query, "COUNT(*)") {
			return &singleCountRows{value: 0}, nil
		}
		return &emptyAuditRows{}, nil
	}
	return &singleCountRows{value: 7}, nil
}

type scriptedRiskDriver struct{}

func (scriptedRiskDriver) Open(string) (driver.Conn, error) { return scriptedRiskConn{}, nil }

type scriptedRiskConn struct{}

func (scriptedRiskConn) Prepare(query string) (driver.Stmt, error) {
	return scriptedRiskStmt{query: query}, nil
}
func (scriptedRiskConn) Close() error              { return nil }
func (scriptedRiskConn) Begin() (driver.Tx, error) { return nil, errors.New("tx not supported") }

type scriptedRiskStmt struct{ query string }

func (scriptedRiskStmt) Close() error  { return nil }
func (scriptedRiskStmt) NumInput() int { return -1 }
func (scriptedRiskStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("exec not supported")
}
func (s scriptedRiskStmt) Query([]driver.Value) (driver.Rows, error) {
	return scriptQueryResult(s.query)
}

type singleCountRows struct {
	value int64
	done  bool
}

func (r *singleCountRows) Columns() []string { return []string{"COUNT(*)"} }
func (r *singleCountRows) Close() error      { return nil }
func (r *singleCountRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.value
	return nil
}

// openScriptedRiskDB 注册一次性驱动并返回连接，避免 sql.Register 重名 panic。
func openScriptedRiskDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("scripted-risk-%d", time.Now().UnixNano())
	sql.Register(name, scriptedRiskDriver{})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("打开脚本化 DB 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// P1-01：device_risk_users 查询失败必须让概览整体失败，
// 不能把失败吞成 0 让后台显示虚假的"0 个风险用户"。
func TestQueryRiskControlOverviewDoesNotSwallowQueryFailure(t *testing.T) {
	db := openScriptedRiskDB(t)
	data, err := queryRiskControlOverview(db)
	if err == nil {
		t.Fatalf("device_risk_users 查询失败时应返回错误，实际返回了概览数据 device_risk_users=%v", data["device_risk_users"])
	}
	if data != nil {
		t.Fatalf("失败时不应返回部分数据: %v", data)
	}
}
