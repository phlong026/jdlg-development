package route

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
	"service/model"
)

const wecomLoginPath = "/api/v1/management/wecom"

var memberIDPattern = regexp.MustCompile(`^[A-Za-z0-9_@.\-]{1,128}$`)
var oauthRandomPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)

type wecomLoginConfig struct{ Corp, Agent, Secret, Callback, Frontend string }

func loadWecomLoginConfig() (wecomLoginConfig, error) {
	c := wecomLoginConfig{os.Getenv("WECOM_LOGIN_CORP_ID"), os.Getenv("WECOM_LOGIN_AGENT_ID"), os.Getenv("WECOM_LOGIN_SECRET"), os.Getenv("WECOM_LOGIN_CALLBACK_URL"), os.Getenv("WECOM_LOGIN_FRONTEND_ORIGIN")}
	if os.Getenv("WECOM_LOGIN_ENABLED") != "true" || !memberIDPattern.MatchString(c.Corp) || c.Secret == "" {
		return c, fmt.Errorf("企微登录尚未配置")
	}
	n, e := strconv.ParseInt(c.Agent, 10, 64)
	if e != nil || n <= 0 {
		return c, fmt.Errorf("企微应用配置无效")
	}
	for _, raw := range []string{c.Callback, c.Frontend} {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return c, fmt.Errorf("企微登录地址必须为固定HTTPS地址")
		}
	}
	cb, _ := url.Parse(c.Callback)
	front, _ := url.Parse(c.Frontend)
	if cb.Host != front.Host {
		return c, fmt.Errorf("企微登录回调必须通过后台同源HTTPS代理")
	}
	if cb.Path != wecomLoginPath+"/callback" || cb.RawPath != "" || front.Path != "" {
		return c, fmt.Errorf("企微登录回调或后台源地址无效")
	}
	return c, nil
}
func identityHash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }

// One provider per registered route group; credentials are never sent to the browser.
type wecomIdentityProvider struct {
	client     *http.Client
	mu         sync.Mutex
	token, key string
	until      time.Time
}

func newWecomIdentityProvider() *wecomIdentityProvider {
	return &wecomIdentityProvider{client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (p *wecomIdentityProvider) call(ctx context.Context, path string, q url.Values, out any) error {
	req, e := http.NewRequestWithContext(ctx, "GET", "https://qyapi.weixin.qq.com/cgi-bin/"+path+"?"+q.Encode(), nil)
	if e != nil {
		return fmt.Errorf("企微请求配置无效")
	}
	res, e := p.client.Do(req)
	if e != nil {
		return fmt.Errorf("企微身份服务连接失败")
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 262145))
	if e != nil || len(b) > 262144 || res.StatusCode != 200 {
		return fmt.Errorf("企微身份服务响应异常")
	}
	var status struct {
		Code int `json:"errcode"`
	}
	if json.Unmarshal(b, &status) != nil {
		return fmt.Errorf("企微身份服务格式异常")
	}
	if status.Code != 0 {
		return wecomProviderError(status.Code)
	}
	if json.Unmarshal(b, out) != nil {
		return fmt.Errorf("企微身份服务格式异常")
	}
	return nil
}

type wecomProviderError int

func (e wecomProviderError) Error() string {
	return fmt.Sprintf("企微身份校验失败（错误码%d），请检查应用权限或重新登录", int(e))
}

// Refresh once if WeCom invalidates a cached token before its advertised expiry.
func (p *wecomIdentityProvider) authorizedCall(ctx context.Context, c wecomLoginConfig, path string, q url.Values, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		token, e := p.accessToken(ctx, c)
		if e != nil {
			return e
		}
		q.Set("access_token", token)
		e = p.call(ctx, path, q, out)
		var code wecomProviderError
		if attempt == 0 && errors.As(e, &code) && (code == 40014 || code == 42001) {
			p.mu.Lock()
			if p.token == token {
				p.until = time.Time{}
			}
			p.mu.Unlock()
			continue
		}
		return e
	}
	return fmt.Errorf("企微凭据刷新失败")
}

func (p *wecomIdentityProvider) accessToken(ctx context.Context, c wecomLoginConfig) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := identityHash(c.Corp + "\n" + c.Agent + "\n" + c.Secret)
	if p.key == key && p.until.After(time.Now()) {
		return p.token, nil
	}
	var data struct {
		Token   string `json:"access_token"`
		Expires int    `json:"expires_in"`
	}
	e := p.call(ctx, "gettoken", url.Values{"corpid": {c.Corp}, "corpsecret": {c.Secret}}, &data)
	if e != nil {
		return "", e
	}
	if data.Token == "" || data.Expires <= 60 {
		return "", fmt.Errorf("企微应用凭据无效")
	}
	p.key = key
	p.token = data.Token
	p.until = time.Now().Add(time.Duration(data.Expires-60) * time.Second)
	return p.token, nil
}

type wecomMember struct {
	ID     string `json:"userid"`
	Name   string `json:"name"`
	Status int    `json:"status"`
}

func (p *wecomIdentityProvider) member(ctx context.Context, c wecomLoginConfig, id string) (wecomMember, error) {
	var m wecomMember
	if !memberIDPattern.MatchString(id) {
		return m, fmt.Errorf("请填写准确的企微员工UserID")
	}
	e := p.authorizedCall(ctx, c, "user/get", url.Values{"userid": {id}}, &m)
	if e != nil {
		return m, e
	}
	if m.ID != id || m.Status != 1 {
		return m, fmt.Errorf("企微成员未激活、已停用或不在应用可读取范围")
	}
	return m, nil
}
func (p *wecomIdentityProvider) exchange(ctx context.Context, c wecomLoginConfig, code string) (wecomMember, error) {
	var data struct {
		ID string `json:"userid"`
	}
	if e := p.authorizedCall(ctx, c, "auth/getuserinfo", url.Values{"code": {code}}, &data); e != nil {
		return wecomMember{}, e
	}
	if !memberIDPattern.MatchString(data.ID) {
		return wecomMember{}, fmt.Errorf("仅本企业已开通的员工可以登录")
	}
	return p.member(ctx, c, data.ID)
}

type wecomBinding struct {
	UserID        int64  `json:"userId"`
	CorpID        string `json:"corpId"`
	EmployeeID    string `json:"employeeId"`
	EmployeeName  string `json:"employeeName"`
	Enabled       bool   `json:"enabled"`
	Version       int64  `json:"version"`
	AccessVersion int64  `json:"-"`
}

// wecomBindingReader 让绑定读取既可用 *sql.DB 也可用事务 *sql.Tx，
// 供授权保存流程在事务内核验身份，避免事务外读的检查窗口。
type wecomBindingReader interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

func readWecomBinding(ctx context.Context, db wecomBindingReader, id int64) (wecomBinding, error) {
	b := wecomBinding{UserID: id}
	e := db.QueryRowContext(ctx, `SELECT corp_id,employee_id,employee_name,enabled,version FROM management_wecom_identity WHERE user_id=?`, id).Scan(&b.CorpID, &b.EmployeeID, &b.EmployeeName, &b.Enabled, &b.Version)
	return b, e
}

// Sessions retain the binding revision, so rebind/disable invalidates issued OAuth sessions.
func checkWecomSession(c *gin.Context, db *sql.DB) bool {
	s := sessions.Default(c)
	if s.Get("auth_method") != "wecom" {
		return true
	}
	cfg, e := loadWecomLoginConfig()
	if e != nil {
		crmError(c, 401, "企微登录已停用，请重新登录")
		return false
	}
	b, e := readWecomBinding(c.Request.Context(), db, GetUserID(c))
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		crmError(c, 503, "企微身份授权服务不可用")
		return false
	}
	expiry, _ := s.Get("wecom_expires").(int64)
	if e != nil || !b.Enabled || b.CorpID != cfg.Corp || s.Get("wecom_corp") != b.CorpID || s.Get("wecom_employee") != b.EmployeeID || s.Get("wecom_version") != b.Version || expiry <= time.Now().Unix() {
		crmError(c, 401, "企微身份绑定已变更或登录已过期，请重新登录")
		return false
	}
	return true
}

func saveWecomBinding(ctx context.Context, db *sql.DB, in wecomBinding, operator int64) (wecomBinding, error) {
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return in, e
	}
	defer tx.Rollback()
	var kind string
	if e = tx.QueryRowContext(ctx, `SELECT user_type FROM users WHERE id=? FOR UPDATE`, in.UserID).Scan(&kind); e != nil {
		return in, e
	}
	if kind != "management" {
		return in, fmt.Errorf("请选择后台管理账号")
	}
	if in.Enabled {
		var version int64
		var pending int
		if e = tx.QueryRowContext(ctx, `SELECT version FROM management_access WHERE user_id=? FOR UPDATE`, in.UserID).Scan(&version); e != nil {
			return in, e
		}
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM management_crm_link WHERE user_id=? AND state<>'active'`, in.UserID).Scan(&pending); e != nil {
			return in, e
		}
		if version != in.AccessVersion || pending > 0 {
			return in, fmt.Errorf("角色授权正在变更或尚未完成，请完成授权后重新绑定")
		}
	}
	old := wecomBinding{UserID: in.UserID}
	e = tx.QueryRowContext(ctx, `SELECT corp_id,employee_id,employee_name,enabled,version FROM management_wecom_identity WHERE user_id=? FOR UPDATE`, in.UserID).Scan(&old.CorpID, &old.EmployeeID, &old.EmployeeName, &old.Enabled, &old.Version)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return in, e
	}
	if old.Version != in.Version {
		return in, fmt.Errorf("绑定已更新，请重新选择账号后再操作")
	}
	if !in.Enabled {
		if old.Version == 0 {
			return in, fmt.Errorf("账号尚未绑定")
		}
		in = old
		in.Enabled = false
	}
	in.Version = old.Version + 1
	if old.Version == 0 {
		_, e = tx.ExecContext(ctx, `INSERT INTO management_wecom_identity(user_id,corp_id,employee_id,employee_name,enabled,version,updated_by,updated_at) VALUES (?,?,?,?,?,?,?,NOW(6))`, in.UserID, in.CorpID, in.EmployeeID, in.EmployeeName, in.Enabled, in.Version, operator)
	} else {
		_, e = tx.ExecContext(ctx, `UPDATE management_wecom_identity SET corp_id=?,employee_id=?,employee_name=?,enabled=?,version=?,updated_by=?,updated_at=NOW(6) WHERE user_id=?`, in.CorpID, in.EmployeeID, in.EmployeeName, in.Enabled, in.Version, operator, in.UserID)
	}
	if e != nil {
		var duplicate *mysql.MySQLError
		if errors.As(e, &duplicate) && duplicate.Number == 1062 {
			return in, fmt.Errorf("该企微员工已绑定其他后台账号（停用绑定仍保留占用），请先核对原账号")
		}
		return in, e
	}
	before, _ := json.Marshal(old)
	after, _ := json.Marshal(in)
	_, e = tx.ExecContext(ctx, `INSERT INTO management_access_audit(operator_id,user_id,action,before_value,after_value,result,request_id,created_at) VALUES (?,?,'wecom_identity',?,?,'success',?,NOW(6))`, operator, in.UserID, string(before), string(after), crmRandom())
	if e != nil {
		return in, e
	}
	return in, tx.Commit()
}

func validateStaffIdentity(ctx context.Context, db wecomBindingReader, id int64, employee string) error {
	b, e := readWecomBinding(ctx, db, id)
	var absent *mysql.MySQLError
	if errors.Is(e, sql.ErrNoRows) || (errors.As(e, &absent) && absent.Number == 1146) {
		return nil
	}
	if e != nil {
		return fmt.Errorf("无法核验企微身份绑定")
	}
	if b.Enabled && b.EmployeeID != employee {
		return fmt.Errorf("顾问的业务员工必须与已绑定的企微登录身份一致，请先停用旧绑定后调整")
	}
	return nil
}
func validateBindingCRM(ctx context.Context, in wecomBinding, corp string, operator int64) error {
	status, data, _, e := crmRemote(ctx, "GET", "/provision/identity", "@platform", operator, nil, nil)
	var info struct {
		Corp     string `json:"corpId"`
		Demo     bool   `json:"demo"`
		Accounts []struct {
			UserID   int64  `json:"managementUserId"`
			Employee string `json:"employeeId"`
			Role     string `json:"role"`
			Enabled  bool   `json:"enabled"`
		} `json:"accounts"`
	}
	if e != nil || status != 200 || json.Unmarshal(data, &info) != nil {
		return fmt.Errorf("无法核验客户服务企业与角色，请先更新并启动客户服务")
	}
	if info.Demo || info.Corp != corp {
		return fmt.Errorf("企微登录企业与正式客户库不一致，禁止绑定")
	}
	for _, a := range info.Accounts {
		if a.UserID == in.UserID && a.Enabled && a.Role == "staff" && a.Employee != in.EmployeeID {
			return fmt.Errorf("登录员工与该顾问的业务员工不一致，请先核对账号授权")
		}
	}
	return nil
}
func registerWecomBinding(g *gin.RouterGroup, users *model.UserModel) {
	registerWecomBindingProvider(g, users, newWecomIdentityProvider())
}
func registerWecomBindingProvider(g *gin.RouterGroup, users *model.UserModel, p *wecomIdentityProvider) {
	admin := func(c *gin.Context) {
		a, ok := c.Get("management.access")
		if !ok || a.(managementAccess).Scope != "platform_admin" {
			crmError(c, 403, "仅平台管理员可绑定身份")
			return
		}
		if c.Request.Method != "GET" && !crmMutationAllowed(c.Request) {
			crmError(c, 403, "请求来源校验失败")
			return
		}
		c.Next()
	}
	g.GET("/access/wecom/:id", admin, func(c *gin.Context) {
		id, e := strconv.ParseInt(c.Param("id"), 10, 64)
		if e != nil || id <= 0 {
			crmError(c, 400, "账号无效")
			return
		}
		b, e := readWecomBinding(c.Request.Context(), users.DB, id)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			crmError(c, 503, "请先执行企微身份数据库升级")
			return
		}
		cfg, ce := loadWecomLoginConfig()
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"binding": b, "configured": ce == nil, "corpId": cfg.Corp}})
	})
	g.POST("/access/wecom", admin, func(c *gin.Context) {
		var in wecomBinding
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		if c.ShouldBindJSON(&in) != nil || in.UserID <= 0 || in.Version < 0 {
			crmError(c, 400, "绑定参数无效")
			return
		}
		// Revocation is local and still works when WeCom is unavailable or disabled.
		if in.Enabled {
			cfg, e := loadWecomLoginConfig()
			if e != nil {
				crmError(c, 503, e.Error())
				return
			}
			ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
			defer cancel()
			access, e := readManagementAccess(ctx, users.DB, in.UserID)
			if e != nil {
				crmError(c, 400, "请先完成该后台账号的角色授权")
				return
			}
			in.AccessVersion = access.Version
			m, e := p.member(ctx, cfg, in.EmployeeID)
			if e != nil {
				crmError(c, 400, e.Error())
				return
			}
			in.CorpID = cfg.Corp
			in.EmployeeName = m.Name
			if e = validateBindingCRM(ctx, in, cfg.Corp, GetUserID(c)); e != nil {
				crmError(c, 409, e.Error())
				return
			}
		}
		b, e := saveWecomBinding(c.Request.Context(), users.DB, in, GetUserID(c))
		if e != nil {
			var sqlError *mysql.MySQLError
			if errors.As(e, &sqlError) || errors.Is(e, sql.ErrNoRows) {
				crmError(c, 503, "身份保存失败，请检查账号及数据库升级")
			} else {
				crmError(c, 409, e.Error())
			}
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": b})
	})
}

func registerWecomLogin(r *gin.RouterGroup, users *model.UserModel) {
	registerWecomLoginProvider(r, users, newWecomIdentityProvider())
}
func registerWecomLoginProvider(r *gin.RouterGroup, users *model.UserModel, p *wecomIdentityProvider) {
	g := r.Group("/wecom")
	g.GET("/config", func(c *gin.Context) {
		_, e := loadWecomLoginConfig()
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"enabled": e == nil}})
	})
	g.GET("/start", func(c *gin.Context) {
		cfg, e := loadWecomLoginConfig()
		if e != nil {
			crmError(c, 503, e.Error())
			return
		}
		state, browser := crmRandom(), crmRandom()
		ctx := c.Request.Context()
		_, e = users.DB.ExecContext(ctx, `DELETE FROM management_wecom_oauth_state WHERE expires_at < NOW(6)`)
		if e == nil {
			_, e = users.DB.ExecContext(ctx, `INSERT INTO management_wecom_oauth_state(state_hash,browser_hash,corp_id,agent_id,expires_at) VALUES (?,?,?,?,DATE_ADD(NOW(6),INTERVAL 5 MINUTE))`, identityHash(state), identityHash(browser), cfg.Corp, cfg.Agent)
		}
		if e != nil {
			crmError(c, 503, "企微登录结构未就绪，请联系管理员")
			return
		}
		http.SetCookie(c.Writer, &http.Cookie{Name: "wecom_login_browser", Value: browser, Path: wecomLoginPath, MaxAge: 300, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		q := url.Values{"appid": {cfg.Corp}, "agentid": {cfg.Agent}, "redirect_uri": {cfg.Callback}, "state": {state}}
		target := "https://open.work.weixin.qq.com/wwopen/sso/qrConnect"
		if strings.Contains(strings.ToLower(c.Request.UserAgent()), "wxwork") {
			target = "https://open.weixin.qq.com/connect/oauth2/authorize"
			q.Set("response_type", "code")
			q.Set("scope", "snsapi_base")
		}
		target += "?" + q.Encode()
		if strings.Contains(target, "oauth2/authorize") {
			target += "#wechat_redirect"
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		c.Redirect(http.StatusFound, target)
	})
	g.GET("/callback", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		cfg, e := loadWecomLoginConfig()
		if e != nil {
			crmError(c, 503, "企微登录未启用")
			return
		}
		fail := func(reason string) { c.Redirect(http.StatusSeeOther, cfg.Frontend+"/admin/login?wecom_error="+reason) }
		q := c.Request.URL.Query()
		state, code := q.Get("state"), q.Get("code")
		cookie, _ := c.Cookie("wecom_login_browser")
		if len(q["state"]) != 1 || len(q["code"]) != 1 || !oauthRandomPattern.MatchString(state) || !oauthRandomPattern.MatchString(cookie) || code == "" || len(code) > 512 {
			fail("expired")
			return
		}
		result, e := users.DB.ExecContext(c.Request.Context(), `DELETE FROM management_wecom_oauth_state WHERE state_hash=? AND browser_hash=? AND corp_id=? AND agent_id=? AND expires_at>NOW(6)`, identityHash(state), identityHash(cookie), cfg.Corp, cfg.Agent)
		if e != nil {
			fail("unavailable")
			return
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			fail("expired")
			return
		}
		http.SetCookie(c.Writer, &http.Cookie{Name: "wecom_login_browser", Value: "", Path: wecomLoginPath, MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		m, e := p.exchange(ctx, cfg, code)
		if e != nil {
			fail("identity")
			return
		}
		var id, revision int64
		var username string
		e = users.DB.QueryRowContext(ctx, `SELECT u.id,u.username,i.version FROM management_wecom_identity i JOIN users u ON u.id=i.user_id JOIN management_access a ON a.user_id=u.id WHERE i.corp_id=? AND i.employee_id=? AND i.enabled=TRUE AND u.user_type='management' AND a.enabled=TRUE AND a.scope IN ('platform_admin','crm_only')`, cfg.Corp, m.ID).Scan(&id, &username, &revision)
		if errors.Is(e, sql.ErrNoRows) {
			fail("not_enabled")
			return
		}
		if e != nil {
			fail("unavailable")
			return
		}
		session := sessions.Default(c)
		session.Clear()
		session.Options(sessions.Options{Path: "/", MaxAge: 28800, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		session.Set("user_id", id)
		session.Set("username", username)
		session.Set("user_type", "management")
		session.Set("auth_method", "wecom")
		session.Set("wecom_corp", cfg.Corp)
		session.Set("wecom_employee", m.ID)
		session.Set("wecom_version", revision)
		session.Set("wecom_expires", time.Now().Add(8*time.Hour).Unix())
		if session.Save() != nil {
			fail("unavailable")
			return
		}
		c.Redirect(http.StatusSeeOther, cfg.Frontend+"/admin/customer-management")
	})
}
