package route

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"log"
	"net/http"
	"service/function"
	"service/model"
	"strings"
	"unicode/utf8"
)

// crmAccessIntent is the pending authorization intent (B1): written by the prepare transaction into
// management_crm_link.intent, cleared after the final transaction confirms success, and replayed by
// the retry endpoint / compensation worker after a remote failure or a process crash.
type crmAccessIntent struct {
	UserID      int64  `json:"userId"`
	Revision    int64  `json:"revision"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
	DisplayName string `json:"displayName"`
	EmployeeID  string `json:"employeeId"`
	Role        string `json:"role"`
	CRMEnabled  bool   `json:"crmEnabled"`
	OperatorID  int64  `json:"operatorId"`
}

// crmIntentMissing reports whether the error is "unknown column" (MySQL 1054), i.e. the
// compensation migration has not been applied yet.
func crmIntentMissing(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1054
}

// crmPrepareLink writes the pending link together with the replay intent; un-migrated
// environments degrade to the legacy write so the core authorization flow keeps working.
func crmPrepareLink(ctx context.Context, tx *sql.Tx, userID int64, username string, revision int64, intent string, operator int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO management_crm_link(user_id,crm_username,state,enabled,version,intent,attempts,updated_by,updated_at) VALUES (?,?,'pending',FALSE,?,?,0,?,NOW(6)) ON DUPLICATE KEY UPDATE state='pending',enabled=FALSE,version=VALUES(version),intent=VALUES(intent),attempts=0,updated_by=VALUES(updated_by),updated_at=VALUES(updated_at)`, userID, username, revision, intent, operator)
	if !crmIntentMissing(err) {
		return err
	}
	log.Printf("[crm-access] management_crm_link.intent missing, compensation degraded (apply management_crm_link_intent_up.sql)")
	_, err = tx.ExecContext(ctx, `INSERT INTO management_crm_link(user_id,crm_username,state,enabled,version,updated_by,updated_at) VALUES (?,?,'pending',FALSE,?,?,NOW(6)) ON DUPLICATE KEY UPDATE state='pending',enabled=FALSE,version=VALUES(version),updated_by=VALUES(updated_by),updated_at=VALUES(updated_at)`, userID, username, revision, operator)
	return err
}

// crmFinalizeLink writes the terminal link state and, on success, clears the replay intent;
// un-migrated environments degrade to the legacy write.
func crmFinalizeLink(ctx context.Context, tx *sql.Tx, result string, enabled bool, userID, revision int64) error {
	if result == "active" {
		if _, err := tx.ExecContext(ctx, `UPDATE management_crm_link SET state='active',enabled=?,intent=NULL,updated_at=NOW(6) WHERE user_id=? AND version=?`, enabled, userID, revision); !crmIntentMissing(err) {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE management_crm_link SET state=?,enabled=?,updated_at=NOW(6) WHERE user_id=? AND version=?`, result, enabled, userID, revision)
	return err
}

// crmIntentAudit renders the audit payload of a replay; automatic replays carry a replay marker.
func crmIntentAudit(intent crmAccessIntent, automatic bool) string {
	if !automatic {
		payload, _ := json.Marshal(intent)
		return string(payload)
	}
	payload, _ := json.Marshal(struct {
		crmAccessIntent
		Replay bool `json:"replay"`
	}{intent, true})
	return string(payload)
}

type crmAccessInput struct {
	UserID      int64  `json:"userId"`
	Version     int64  `json:"version"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
	DisplayName string `json:"displayName"`
	EmployeeID  string `json:"employeeId"`
	Role        string `json:"role"`
	CRMEnabled  bool   `json:"crmEnabled"`
}

func registerCRMAccess(g *gin.RouterGroup, users *model.UserModel) {
	registerWecomBinding(g, users)
	admin := func(c *gin.Context) {
		a, ok := c.Get("management.access")
		if !crmEnabled() || !ok || a.(managementAccess).Scope != "platform_admin" {
			crmError(c, 403, "仅系统管理员可以管理授权")
			return
		}
		if c.Request.Method != "GET" && !crmMutationAllowed(c.Request) {
			crmError(c, 403, "请求来源校验失败")
			return
		}
		c.Next()
	}
	g.GET("/access", admin, func(c *gin.Context) {
		rows, err := users.DB.QueryContext(c.Request.Context(), `SELECT u.id,u.username,COALESCE(a.scope,'crm_only'),COALESCE(a.enabled,FALSE),COALESCE(a.version,0),COALESCE(l.crm_username,''),COALESCE(l.state,''),COALESCE(l.enabled,FALSE) FROM users u LEFT JOIN management_access a ON a.user_id=u.id LEFT JOIN management_crm_link l ON l.user_id=u.id WHERE u.user_type='management' ORDER BY u.id LIMIT 500`)
		if err != nil {
			crmError(c, 503, "授权结构未就绪")
			return
		}
		defer rows.Close()
		list := []gin.H{}
		for rows.Next() {
			var id, version int64
			var username, scope, crm, state string
			var enabled, linked bool
			if rows.Scan(&id, &username, &scope, &enabled, &version, &crm, &state, &linked) != nil {
				crmError(c, 503, "读取授权失败")
				return
			}
			list = append(list, gin.H{"userId": id, "username": username, "scope": scope, "enabled": enabled, "version": version, "crmUsername": crm, "state": state, "crmEnabled": linked})
		}
		if rows.Err() != nil {
			crmError(c, 503, "读取授权失败")
			return
		}
		status, data, _, err := crmRemote(c.Request.Context(), "GET", "/provision/accounts", "@platform", GetUserID(c), nil, nil)
		var accounts any = []any{}
		connected := err == nil && status == 200 && json.Unmarshal(data, &accounts) == nil
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"users": list, "accounts": accounts, "serviceAvailable": connected}})
	})
	g.GET("/access/employees", admin, func(c *gin.Context) {
		status, data, _, err := crmRemote(c.Request.Context(), "GET", "/provision/employees", "@platform", GetUserID(c), nil, nil)
		if err != nil || status != 200 {
			crmError(c, 502, "无法读取企微员工，请检查客户服务及同步状态")
			return
		}
		var value any
		if json.Unmarshal(data, &value) != nil {
			crmError(c, 502, "员工响应无效")
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": value})
	})
	g.POST("/access/users", admin, func(c *gin.Context) {
		var in struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		if c.ShouldBindJSON(&in) != nil || len(in.Username) < 3 || len(in.Username) > 64 || strings.ContainsAny(in.Username, " \r\n\t") || utf8.RuneCountInString(in.Password) < 12 || len(in.Password) > 72 {
			crmError(c, 400, "请填写账号与至少12字符的密码")
			return
		}
		hash, hashErr := function.HashManagementPassword(in.Password)
		if hashErr != nil {
			crmError(c, 400, "密码长度无效")
			return
		}
		tx, err := users.DB.BeginTx(c.Request.Context(), nil)
		if err != nil {
			crmError(c, 503, "账号服务不可用")
			return
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(c.Request.Context(), `INSERT INTO users(username,password,openid,unionid,user_type,can_withdraw,created_at,updated_at) VALUES (?,?,NULL,NULL,'management',FALSE,NOW(),NOW())`, in.Username, hash)
		if err != nil {
			crmError(c, 409, "账号创建失败，请检查账号是否已存在")
			return
		}
		id, _ := result.LastInsertId()
		_, err = tx.ExecContext(c.Request.Context(), `INSERT INTO management_access(user_id,scope,enabled,version,updated_by,updated_at) VALUES (?,'crm_only',FALSE,0,?,NOW(6))`, id, GetUserID(c))
		if err == nil {
			_, err = tx.ExecContext(c.Request.Context(), `INSERT INTO management_access_audit(operator_id,user_id,action,after_value,result,request_id,created_at) VALUES (?,?,'create_user',?,'success',?,NOW(6))`, GetUserID(c), id, `{"scope":"crm_only","enabled":false}`, crmRandom())
		}
		if err != nil || tx.Commit() != nil {
			crmError(c, 503, "账号保存失败")
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"userId": id, "username": in.Username}})
	})
	g.POST("/access", admin, func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
		var in crmAccessInput
		if c.ShouldBindJSON(&in) != nil || in.UserID <= 0 || in.Version < 0 || (in.Scope != "platform_admin" && in.Scope != "crm_only") || (in.Role != "admin" && in.Role != "staff") || strings.TrimSpace(in.DisplayName) == "" || utf8.RuneCountInString(in.DisplayName) > 100 {
			crmError(c, 400, "授权信息无效")
			return
		}
		if in.UserID == GetUserID(c) && (!in.Enabled || in.Scope != "platform_admin") {
			crmError(c, 400, "不能停用或降级当前系统管理员")
			return
		}
		ctx := c.Request.Context()
		tx, err := users.DB.BeginTx(ctx, nil)
		if err != nil {
			crmError(c, 503, "授权服务不可用")
			return
		}
		defer tx.Rollback()
		var kind string
		if tx.QueryRowContext(ctx, `SELECT user_type FROM users WHERE id=? FOR UPDATE`, in.UserID).Scan(&kind) != nil || kind != "management" {
			crmError(c, 400, "请选择管理后台账号")
			return
		}
		var old managementAccess
		if in.Enabled && in.CRMEnabled && in.Role == "staff" {
			if err := validateStaffIdentity(ctx, tx, in.UserID, in.EmployeeID); err != nil {
				crmError(c, 409, err.Error())
				return
			}
		}
		err = tx.QueryRowContext(ctx, `SELECT scope,enabled,version FROM management_access WHERE user_id=? FOR UPDATE`, in.UserID).Scan(&old.Scope, &old.Enabled, &old.Version)
		if err != nil && err != sql.ErrNoRows {
			crmError(c, 503, "授权读取失败")
			return
		}
		if old.Version != in.Version {
			crmError(c, 409, "授权已更新，请刷新后重试")
			return
		}
		revision := in.Version + 1
		// Disable the link before remote provisioning; failures can never leave a half-enabled identity.
		previousScope := old.Scope
		if previousScope == "" {
			previousScope = "crm_only"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO management_access(user_id,scope,enabled,version,updated_by,updated_at) VALUES (?,?,?,?,?,NOW(6)) ON DUPLICATE KEY UPDATE scope=VALUES(scope),enabled=VALUES(enabled),version=VALUES(version),updated_by=VALUES(updated_by),updated_at=VALUES(updated_at)`, in.UserID, previousScope, old.Enabled && in.Enabled, revision, GetUserID(c))
		if err != nil {
			crmError(c, 503, "授权准备失败")
			return
		}
		username := fmt.Sprintf("managed-%d", in.UserID)
		// Persist the replay intent before the remote call; it is cleared only after the final transaction confirms success.
		intent, _ := json.Marshal(crmAccessIntent{UserID: in.UserID, Revision: revision, Scope: in.Scope, Enabled: in.Enabled, DisplayName: in.DisplayName, EmployeeID: in.EmployeeID, Role: in.Role, CRMEnabled: in.CRMEnabled, OperatorID: GetUserID(c)})
		if err := crmPrepareLink(ctx, tx, in.UserID, username, revision, string(intent), GetUserID(c)); err != nil || tx.Commit() != nil {
			crmError(c, 503, "授权准备失败")
			return
		}
		before, _ := json.Marshal(old)
		after, _ := json.Marshal(in)
		requestID := crmRandom()
		body, _ := json.Marshal(map[string]any{"userId": in.UserID, "revision": revision, "displayName": in.DisplayName, "employeeId": in.EmployeeID, "role": in.Role, "enabled": in.CRMEnabled && in.Enabled})
		status, _, _, callErr := crmRemote(ctx, "POST", "/provision/accounts", "@platform", GetUserID(c), nil, body)
		result := "failed"
		if callErr == nil && status == 200 {
			result = "active"
		}
		finalTx, err := users.DB.BeginTx(ctx, nil)
		if err != nil {
			crmError(c, 503, "授权保持待处理，请刷新后重试")
			return
		}
		defer finalTx.Rollback()
		var current int64
		if finalTx.QueryRowContext(ctx, `SELECT version FROM management_access WHERE user_id=? FOR UPDATE`, in.UserID).Scan(&current) != nil || current != revision {
			crmError(c, 409, "授权被另一操作更新，请刷新核对")
			return
		}
		if err := crmFinalizeLink(ctx, finalTx, result, result == "active" && in.CRMEnabled && in.Enabled, in.UserID, revision); err != nil {
			crmError(c, 503, "授权保存失败")
			return
		}
		if result == "active" {
			_, err = finalTx.ExecContext(ctx, `UPDATE management_access SET scope=?,enabled=?,updated_at=NOW(6) WHERE user_id=? AND version=?`, in.Scope, in.Enabled, in.UserID, revision)
			if err != nil {
				crmError(c, 503, "平台权限保存失败")
				return
			}
		}
		_, err = finalTx.ExecContext(ctx, `INSERT INTO management_access_audit(operator_id,user_id,action,before_value,after_value,result,request_id,created_at) VALUES (?,?,'save_access',?,?,?,?,NOW(6))`, GetUserID(c), in.UserID, string(before), string(after), result, requestID)
		if err != nil || finalTx.Commit() != nil {
			crmError(c, 503, "授权保存失败")
			return
		}
		if result != "active" {
			crmError(c, 502, "客户账号配置失败，关联保持禁用；请核对员工与服务后刷新重试")
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"version": revision}})
	})
	// B1：平台管理员手动重试 pending 的授权意图（自动补偿 worker 之外的兜底入口）。
	g.POST("/access/retry", admin, func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		var in struct {
			UserID int64 `json:"userId"`
		}
		if c.ShouldBindJSON(&in) != nil || in.UserID <= 0 {
			crmError(c, 400, "请选择要重试的账号")
			return
		}
		ctx := c.Request.Context()
		tx, err := users.DB.BeginTx(ctx, nil)
		if err != nil {
			crmError(c, 503, "授权服务不可用")
			return
		}
		var version int64
		var state string
		var payload sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT version,state,intent FROM management_crm_link WHERE user_id=? FOR UPDATE`, in.UserID).Scan(&version, &state, &payload)
		if crmIntentMissing(err) {
			tx.Rollback()
			crmError(c, 503, "补偿结构未就绪，请先应用数据库变更")
			return
		}
		if err != nil || state != "pending" || !payload.Valid || payload.String == "" {
			tx.Rollback()
			crmError(c, 409, "该账号没有待重试的配置")
			return
		}
		var intent crmAccessIntent
		if json.Unmarshal([]byte(payload.String), &intent) != nil || intent.UserID != in.UserID || intent.Revision != version {
			tx.Rollback()
			crmError(c, 500, "补偿意图已损坏，请联系平台管理员")
			return
		}
		if tx.Commit() != nil {
			crmError(c, 503, "授权服务不可用")
			return
		}
		switch outcome, message := crmReplayIntent(ctx, users.DB, intent, version, GetUserID(c), false); outcome {
		case crmReplayActive:
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"version": version}})
		case crmReplaySuperseded:
			crmError(c, 409, "授权已被新操作更新，请刷新查看")
		case crmReplayRemoteFailed:
			crmError(c, 502, message)
		default:
			crmError(c, 503, message)
		}
	})
	if crmEnabled() && users != nil {
		startCRMAccessCompensation(users.DB)
	}
}
