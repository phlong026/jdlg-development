package route

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sync/atomic"
	"time"
)

// B1 授权补偿：远端配置失败或进程中断后，pending 意图由自动补偿 worker 周期重放，
// 直到转 active（成功）或 attempts 用尽（转人工介入）。
const (
	crmAccessCompensateInterval = 60 * time.Second
	crmAccessCompensateBatch    = 5
	crmAccessCompensateMaxTries = 5
)

// crmCompensationDegraded 记录未迁移环境（intent 列缺失）的降级日志只打一次。
var crmCompensationDegraded atomic.Bool

type crmReplayResult int

const (
	crmReplayActive crmReplayResult = iota
	crmReplayRemoteFailed
	crmReplaySuperseded
	crmReplayInternal
)

// crmReplayIntent 按持久化意图重放远端配置并执行终局事务，由手动重试端点（automatic=false）
// 与自动补偿 worker（automatic=true）共享。
// wireUID 用于远端签名（Java 拒绝 uid<=0，自动重放沿用原操作者 uid）；自动重放的 Go 侧审计
// 记为 operator_id=0 并附 replay 标记，手动重试计入 attempts，自动重放由 worker 抢占时计数。
func crmReplayIntent(ctx context.Context, db *sql.DB, intent crmAccessIntent, revision, wireUID int64, automatic bool) (crmReplayResult, string) {
	body, _ := json.Marshal(map[string]any{"userId": intent.UserID, "revision": revision, "displayName": intent.DisplayName, "employeeId": intent.EmployeeID, "role": intent.Role, "enabled": intent.CRMEnabled && intent.Enabled})
	status, data, _, callErr := crmRemote(ctx, "POST", "/provision/accounts", "@platform", wireUID, nil, body)
	ok := callErr == nil && status == 200
	message := ""
	if !ok {
		message = "客户账号配置失败，请核对员工与服务后重试"
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &e) == nil && e.Message != "" {
			message = e.Message
		}
		if status == 409 {
			message += "，请刷新后核对最新状态"
		}
	}
	finalTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return crmReplayInternal, "授权保持待处理，请稍后再试"
	}
	defer finalTx.Rollback()
	// 用户新操作永远覆盖重放：版本被越过时放弃本次重放，由新操作自行终局。
	var current int64
	if finalTx.QueryRowContext(ctx, `SELECT version FROM management_access WHERE user_id=? FOR UPDATE`, intent.UserID).Scan(&current) != nil || current != revision {
		return crmReplaySuperseded, "授权已被新操作更新，请刷新查看"
	}
	before, _ := json.Marshal(map[string]any{"state": "pending", "version": revision})
	result := "failed"
	if ok {
		result = "active"
		if err = crmFinalizeLink(ctx, finalTx, "active", intent.CRMEnabled && intent.Enabled, intent.UserID, revision); err != nil {
			return crmReplayInternal, "授权保存失败"
		}
		if _, err = finalTx.ExecContext(ctx, `UPDATE management_access SET scope=?,enabled=?,updated_at=NOW(6) WHERE user_id=? AND version=?`, intent.Scope, intent.Enabled, intent.UserID, revision); err != nil {
			return crmReplayInternal, "平台权限保存失败"
		}
	} else if !automatic {
		if _, err = finalTx.ExecContext(ctx, `UPDATE management_crm_link SET attempts=attempts+1,updated_at=NOW(6) WHERE user_id=? AND version=? AND state='pending'`, intent.UserID, revision); err != nil {
			return crmReplayInternal, "授权保存失败"
		}
	}
	// 自动重放失败只记日志等待下一轮；成功与手动失败都写审计。
	if ok || !automatic {
		auditOperator := wireUID
		if automatic {
			auditOperator = 0
		}
		if _, err = finalTx.ExecContext(ctx, `INSERT INTO management_access_audit(operator_id,user_id,action,before_value,after_value,result,request_id,created_at) VALUES (?,?,'save_access',?,?,?,?,NOW(6))`, auditOperator, intent.UserID, string(before), crmIntentAudit(intent, automatic), result, crmRandom()); err != nil {
			return crmReplayInternal, "授权审计写入失败"
		}
	}
	if finalTx.Commit() != nil {
		return crmReplayInternal, "授权保存失败"
	}
	if ok {
		return crmReplayActive, ""
	}
	return crmReplayRemoteFailed, message
}

// startCRMAccessCompensation 启动授权补偿 worker：启动后立即扫描一轮（重启即补偿中断的重放），
// 之后每 60 秒扫描一次。单实例部署按行锁串行化；多实例部署依赖 Java 侧同 revision 幂等契约兜底。
func startCRMAccessCompensation(db *sql.DB) {
	if db == nil {
		return
	}
	go func() {
		for {
			replayPendingCRMAccess(db)
			time.Sleep(crmAccessCompensateInterval)
		}
	}()
}

// replayPendingCRMAccess 扫描一轮可补偿的 pending 授权并逐条重放：
// 先抢占式计数（递增 attempts），受影响行=1 才继续，避免与手动重试/并发实例重复执行。
func replayPendingCRMAccess(db *sql.DB) {
	scanCtx, scanCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scanCancel()
	rows, err := db.QueryContext(scanCtx, `SELECT user_id,version,intent,attempts FROM management_crm_link WHERE state='pending' AND intent IS NOT NULL AND attempts<? AND updated_at<NOW(6)-INTERVAL 60 SECOND LIMIT ?`, crmAccessCompensateMaxTries, crmAccessCompensateBatch)
	if crmIntentMissing(err) {
		if !crmCompensationDegraded.Swap(true) {
			log.Printf("[crm-access] compensation worker idle: management_crm_link.intent missing (apply management_crm_link_intent_up.sql)")
		}
		return
	}
	if err != nil {
		log.Printf("[crm-access] compensation scan failed: %v", err)
		return
	}
	crmCompensationDegraded.Store(false)
	type candidate struct {
		userID   int64
		version  int64
		payload  string
		attempts int64
	}
	var list []candidate
	for rows.Next() {
		var cand candidate
		if rows.Scan(&cand.userID, &cand.version, &cand.payload, &cand.attempts) != nil {
			rows.Close()
			log.Printf("[crm-access] compensation scan read failed")
			return
		}
		list = append(list, cand)
	}
	if rows.Err() != nil {
		log.Printf("[crm-access] compensation scan read failed: %v", rows.Err())
	}
	rows.Close()
	for _, cand := range list {
		var intent crmAccessIntent
		if json.Unmarshal([]byte(cand.payload), &intent) != nil || intent.UserID != cand.userID || intent.Revision != cand.version || intent.OperatorID <= 0 {
			log.Printf("[crm-access] compensation skipped corrupt intent user=%d version=%d", cand.userID, cand.version)
			continue
		}
		itemCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		res, err := db.ExecContext(itemCtx, `UPDATE management_crm_link SET attempts=attempts+1,updated_at=NOW(6) WHERE user_id=? AND version=? AND state='pending'`, cand.userID, cand.version)
		if err != nil {
			cancel()
			log.Printf("[crm-access] compensation claim failed user=%d version=%d: %v", cand.userID, cand.version, err)
			continue
		}
		if affected, _ := res.RowsAffected(); affected != 1 {
			cancel()
			continue
		}
		outcome, message := crmReplayIntent(itemCtx, db, intent, cand.version, intent.OperatorID, true)
		cancel()
		switch outcome {
		case crmReplayActive:
			log.Printf("[crm-access] compensation replay active user=%d version=%d", cand.userID, cand.version)
		case crmReplaySuperseded:
			log.Printf("[crm-access] compensation superseded by newer save user=%d version=%d", cand.userID, cand.version)
		case crmReplayRemoteFailed:
			// 本轮是第 attempts+1 次自动重放；达到上限后停自动重放，保留手动重试入口。
			if cand.attempts+1 >= crmAccessCompensateMaxTries {
				log.Printf("[crm-access] compensation gave up after %d attempts user=%d version=%d, manual retry required: %s", cand.attempts+1, cand.userID, cand.version, message)
			} else {
				log.Printf("[crm-access] compensation replay failed user=%d version=%d: %s", cand.userID, cand.version, message)
			}
		default:
			log.Printf("[crm-access] compensation error user=%d version=%d: %s", cand.userID, cand.version, message)
		}
	}
}
