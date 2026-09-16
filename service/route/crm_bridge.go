package route

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"service/model"
)

type managementAccess struct {
	Scope   string `json:"scope"`
	Enabled bool   `json:"enabled"`
	Version int64  `json:"version"`
}

func crmEnabled() bool { return os.Getenv("CRM_BRIDGE_URL") != "" }
func readManagementAccess(ctx context.Context, db *sql.DB, id int64) (managementAccess, error) {
	var a managementAccess
	err := db.QueryRowContext(ctx, `SELECT a.scope,a.enabled,a.version FROM management_access a JOIN users u ON u.id=a.user_id WHERE a.user_id=? AND u.user_type='management'`, id).Scan(&a.Scope, &a.Enabled, &a.Version)
	return a, err
}
func managementCRMPath(method, path string) bool {
	if method == "GET" && path == "/api/v1/management/me" {
		return true
	}
	if method == "POST" && path == "/api/v1/management/logout" {
		return true
	}
	_, ok := crmRoute(method, strings.TrimPrefix(path, "/api/v1/management/crm"))
	return strings.HasPrefix(path, "/api/v1/management/crm/") && ok
}

// Once the access table exists, its policy remains enforced even if the bridge is disabled.
func ManagementAccessRequired(users *model.UserModel) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "POST" && c.Request.URL.Path == "/api/v1/management/logout" {
			c.Next()
			return
		}
		if users == nil || users.DB == nil {
			crmError(c, 503, "管理授权服务不可用")
			return
		}
		a, err := readManagementAccess(c.Request.Context(), users.DB, GetUserID(c))
		var dbError *mysql.MySQLError
		if !crmEnabled() && errors.As(err, &dbError) && dbError.Number == 1146 && sessions.Default(c).Get("auth_method") != "wecom" {
			c.Next()
			return
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			crmError(c, 503, "管理授权服务不可用")
			return
		}
		if err != nil || !a.Enabled || !(a.Scope == "platform_admin" || a.Scope == "crm_only") {
			crmError(c, 403, "账号未授权或已停用，请联系管理员")
			return
		}
		c.Set("management.access", a)
		if !checkWecomSession(c, users.DB) {
			return
		}
		if a.Scope == "crm_only" && !managementCRMPath(c.Request.Method, c.Request.URL.Path) {
			crmError(c, 403, "此账号仅可访问客户管理模块")
			return
		}
		c.Next()
	}
}
func crmError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"code": status, "msg": message})
}
func crmRoute(method, suffix string) (string, bool) {
	if method == "POST" && (suffix == "/customers" || suffix == "/report-review") {
		return suffix, true
	}
	if method == "GET" {
		switch suffix {
		case "/context":
			return "/auth/me", true
		case "/customers", "/overview", "/dashboard", "/employees", "/sync", "/export/customers", "/export/followups", "/workbench", "/owners", "/notices", "/push-status":
			return suffix, true
		}
	}
	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) >= 2 && parts[0] == "customers" && safeCRMID(parts[1]) {
		if len(parts) == 2 && (method == "GET" || method == "PATCH") {
			return "/customers/" + url.PathEscape(parts[1]), true
		}
		if len(parts) == 3 && ((method == "POST" && (parts[2] == "followups" || parts[2] == "claim" || parts[2] == "attachments" || parts[2] == "requests" || parts[2] == "transfer" || parts[2] == "activate" || parts[2] == "merge" || parts[2] == "unlink" || parts[2] == "restore" || parts[2] == "review-followup")) || (method == "PATCH" && parts[2] == "report")) {
			return "/customers/" + url.PathEscape(parts[1]) + "/" + parts[2], true
		}
	}
	if method == "GET" && len(parts) == 4 && parts[0] == "customers" && parts[2] == "attachments" && safeCRMID(parts[1]) && safeCRMID(parts[3]) {
		return suffix, true
	}
	if method == "POST" && len(parts) == 3 && safeCRMID(parts[1]) && ((parts[0] == "cases" && parts[2] == "decision") || (parts[0] == "notices" && (parts[2] == "seen" || parts[2] == "retry-push"))) {
		return suffix, true
	}
	if method == "POST" && suffix == "/sync" {
		return suffix, true
	}
	if method == "POST" && len(parts) == 4 && parts[0] == "sync" && parts[1] == "events" && parts[3] == "retry" && safeCRMID(parts[2]) {
		return "/sync/events/" + url.PathEscape(parts[2]) + "/retry", true
	}
	return "", false
}
func safeCRMID(s string) bool {
	return len(s) > 0 && len(s) <= 128 && !strings.ContainsAny(s, "/\\%;\r\n") && s != "." && s != ".."
}
func crmMutationAllowed(r *http.Request) bool {
	if r.Header.Get("X-CRM-Request") != "1" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	for _, allowed := range strings.Split(os.Getenv("CRM_ALLOWED_ORIGINS"), ",") {
		if origin == strings.TrimSpace(allowed) && allowed != "" {
			return true
		}
	}
	return false
}
func crmSignature(key, canonical string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
// crmRandom 生成签名 nonce。crypto/rand 在正常系统上不会失败；
// 万一失败退回到时间+进程熵，保证调用方永不 panic（nonce 冲突会被
// 数据库唯一约束拒绝，请求安全失败而不是重放）。
func crmRandom() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("t%d-p%d", time.Now().UnixNano(), os.Getpid())))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func crmRemote(ctx context.Context, method, suffix, subject string, userID int64, query url.Values, body []byte) (int, []byte, http.Header, error) {
	base, err := url.Parse(os.Getenv("CRM_BRIDGE_URL"))
	key := os.Getenv("CRM_BRIDGE_KEY")
	audience := os.Getenv("CRM_BRIDGE_AUDIENCE")
	kid := os.Getenv("CRM_BRIDGE_KEY_ID")
	if kid == "" {
		kid = "v1"
	}
	if err != nil || base == nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || strings.Trim(base.Path, "/") != "" || len(key) < 32 || audience == "" {
		return 0, nil, nil, fmt.Errorf("CRM bridge configuration invalid")
	}
	for _, v := range []string{subject, kid, audience} {
		if strings.ContainsAny(v, "\r\n") {
			return 0, nil, nil, fmt.Errorf("invalid identity")
		}
	}
	path := "/internal/management/v1" + suffix
	target := strings.TrimRight(base.String(), "/") + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := crmRandom()
	uid := strconv.FormatInt(userID, 10)
	digest := sha256.Sum256(body)
	canonical := strings.Join([]string{"CRM-BRIDGE-V1", kid, audience, subject, uid, ts, nonce, method, req.URL.EscapedPath(), req.URL.RawQuery, hex.EncodeToString(digest[:])}, "\n")
	for k, v := range map[string]string{"Key-Id": kid, "Audience": audience, "Subject": subject, "User-Id": uid, "Timestamp": ts, "Nonce": nonce, "Signature": crmSignature(key, canonical)} {
		req.Header.Set("X-CRM-"+k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20+1))
	if len(data) > 32<<20 {
		return 0, nil, nil, fmt.Errorf("CRM response too large")
	}
	return res.StatusCode, data, res.Header, err
}
func crmQuery(c *gin.Context) (url.Values, error) {
	q := url.Values{}
	for k, v := range c.Request.URL.Query() {
		if len(v) != 1 || len(v[0]) > 200 {
			return nil, fmt.Errorf("筛选参数无效")
		}
		switch k {
		case "keyword", "stage", "ownerId", "intent", "category", "queue", "source":
			q.Set(k, v[0])
		case "page", "page_size":
			n, e := strconv.Atoi(v[0])
			if e != nil || n < 1 || (k == "page_size" && n > 100) {
				return nil, fmt.Errorf("分页参数无效")
			}
			if k == "page_size" {
				k = "size"
			}
			q.Set(k, v[0])
		default:
			return nil, fmt.Errorf("不支持的筛选参数")
		}
	}
	return q, nil
}
// crmExportRateLimit 限制每账号导出频率：CSV 导出是重查询，
// 单机部署下用进程内滑动窗口兜底，避免误点或脚本刷取拖垮服务。
var crmExportLimiter = struct {
	sync.Mutex
	last map[int64]time.Time
}{last: map[int64]time.Time{}}

const crmExportInterval = 15 * time.Second

func crmExportAllowed(userID int64, now time.Time) bool {
	crmExportLimiter.Lock()
	defer crmExportLimiter.Unlock()
	if last, ok := crmExportLimiter.last[userID]; ok && now.Sub(last) < crmExportInterval {
		return false
	}
	crmExportLimiter.last[userID] = now
	return true
}

func crmProxy(users *model.UserModel) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !crmEnabled() {
			crmError(c, 503, "客户管理接入尚未配置")
			return
		}
		suffix, ok := crmRoute(c.Request.Method, strings.TrimPrefix(c.Request.URL.Path, "/api/v1/management/crm"))
		if !ok {
			crmError(c, 404, "接口不存在")
			return
		}
		if strings.HasPrefix(suffix, "/export/") {
			if !crmExportAllowed(GetUserID(c), time.Now()) {
				c.Header("Retry-After", "15")
				crmError(c, 429, "导出过于频繁，请稍后再试")
				return
			}
		}
		if c.Request.Method != "GET" && !crmMutationAllowed(c.Request) {
			crmError(c, 403, "请求来源校验失败")
			return
		}
		var subject string
		err := users.DB.QueryRowContext(c.Request.Context(), `SELECT crm_username FROM management_crm_link WHERE user_id=? AND enabled=TRUE AND state='active'`, GetUserID(c)).Scan(&subject)
		if err != nil {
			crmError(c, 403, "尚未关联可用的客户管理账号")
			return
		}
		q, err := crmQuery(c)
		if err != nil {
			crmError(c, 400, err.Error())
			return
		}
		limit := int64(65536)
		if c.Request.Method == "POST" && strings.HasSuffix(suffix, "/attachments") {
			limit = 3 * 1024 * 1024
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, limit+1))
		if err != nil || int64(len(body)) > limit {
			crmError(c, 413, "请求过大")
			return
		}
		status, data, headers, err := crmRemote(c.Request.Context(), c.Request.Method, suffix, subject, GetUserID(c), q, body)
		c.Header("Cache-Control", "no-store")
		if err != nil || status == 401 || status >= 500 {
			crmError(c, 502, "客户服务连接失败，请稍后重试或联系管理员")
			return
		}
		if status < 200 || status >= 300 {
			var e struct {
				Message string `json:"message"`
			}
			json.Unmarshal(data, &e)
			if e.Message == "" {
				e.Message = "客户请求失败"
			}
			crmError(c, status, e.Message)
			return
		}
		if c.Request.Method == "GET" && strings.Contains(suffix, "/attachments/") {
			c.Header("Content-Disposition", headers.Get("Content-Disposition"))
			c.Header("X-Content-Type-Options", "nosniff")
			c.Data(200, headers.Get("Content-Type"), data)
			return
		}
		if strings.HasPrefix(suffix, "/export/") {
			c.Header("Content-Disposition", headers.Get("Content-Disposition"))
			c.Data(200, "text/csv;charset=UTF-8", data)
			return
		}
		var value any
		if len(data) > 0 {
			if json.Unmarshal(data, &value) != nil {
				crmError(c, 502, "客户服务返回格式错误")
				return
			}
		}
		if suffix == "/customers" && c.Request.Method == "GET" {
			if p, ok := value.(map[string]any); ok {
				page := 1
				if raw := q.Get("page"); raw != "" {
					page, _ = strconv.Atoi(raw)
				}
				value = gin.H{"list": p["content"], "total": p["totalElements"], "page": page, "page_size": p["size"]}
			}
		}
		c.JSON(200, gin.H{"code": 0, "msg": "success", "data": value})
	}
}
func RegisterCRMManagementRoutes(r *gin.RouterGroup, users *model.UserModel) {
	g := r.Group("/crm")
	proxy := crmProxy(users)
	for _, p := range []string{"/context", "/overview", "/dashboard", "/customers", "/customers/:id", "/employees", "/sync", "/export/customers", "/export/followups", "/workbench", "/owners", "/notices", "/push-status", "/customers/:id/attachments/:fileId"} {
		g.GET(p, proxy)
	}
	g.PATCH("/customers/:id", proxy)
	g.POST("/customers", proxy)
	g.POST("/report-review", proxy)
	g.POST("/customers/:id/claim", proxy)
	g.PATCH("/customers/:id/report", proxy)
	g.POST("/customers/:id/followups", proxy)
	for _, action := range []string{"attachments", "requests", "transfer", "activate", "merge", "unlink", "restore", "review-followup"} {
		g.POST("/customers/:id/"+action, proxy)
	}
	g.POST("/cases/:id/decision", proxy)
	g.POST("/notices/:id/seen", proxy)
	g.POST("/notices/:id/retry-push", proxy)
	g.POST("/sync", proxy)
	g.POST("/sync/events/:id/retry", proxy)
	registerCRMAccess(g, users)
	registerCRMSources(g, users.DB)
}
