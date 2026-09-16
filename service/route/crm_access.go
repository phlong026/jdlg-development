package route

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"service/function"
	"service/model"
	"strings"
	"unicode/utf8"
)

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
		_, err = tx.ExecContext(ctx, `INSERT INTO management_crm_link(user_id,crm_username,state,enabled,version,updated_by,updated_at) VALUES (?,?,'pending',FALSE,?,?,NOW(6)) ON DUPLICATE KEY UPDATE state='pending',enabled=FALSE,version=VALUES(version),updated_by=VALUES(updated_by),updated_at=VALUES(updated_at)`, in.UserID, username, revision, GetUserID(c))
		if err != nil || tx.Commit() != nil {
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
		_, err = finalTx.ExecContext(ctx, `UPDATE management_crm_link SET state=?,enabled=?,updated_at=NOW(6) WHERE user_id=? AND version=?`, result, result == "active" && in.CRMEnabled && in.Enabled, in.UserID, revision)
		if err != nil {
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
}
