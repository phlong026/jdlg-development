package route

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"service/function"
	"service/model"
)

// AuthRequired 登录校验中间件
func AuthRequired(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "未登录",
		})
		c.Abort()
		return
	}
	c.Next()
}

// GetUserID 从session获取用户ID
func GetUserID(c *gin.Context) int64 {
	session := sessions.Default(c)
	userID, _ := session.Get("user_id").(int64)
	return userID
}

// GetUsername 从session获取用户名
func GetUsername(c *gin.Context) string {
	session := sessions.Default(c)
	username, _ := session.Get("username").(string)
	return username
}

// SetUserSession 设置用户session
func SetUserSession(c *gin.Context, userID int64, username string, userType string) error {
	session := sessions.Default(c)
	session.Clear()
	session.Set("user_id", userID)
	session.Set("username", username)
	session.Set("user_type", userType)
	// 管理端会话安全属性必须显式设置：redistore 默认 30 天有效期且不带
	// HttpOnly/Secure/SameSite，一旦后台出现 XSS 会话即可被脚本读取外传。
	// 与企微登录（wecom_identity.go）及 CRM 模式的 8 小时会话保持一致。
	session.Options(sessions.Options{
		Path:     "/",
		MaxAge:   28800,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return session.Save()
}

// ClearUserSession 清除用户session
func ClearUserSession(c *gin.Context) error {
	session := sessions.Default(c)
	session.Clear()
	return session.Save()
}

// TokenAuthRequired token验证中间件
// 验证流程：token → 解密还原sessionId → 查询sessionId状态 → 鉴权 → 执行业务逻辑
// 请求头需要包含：token, token-signature (HMAC), sin, md5-signature
func TokenAuthRequired(codeSessionModel *model.CodeSessionRedisModel) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 从请求头读取token、token签名、sin、md5签名、pass、tm
		token := c.GetHeader("token")
		tokenSignature := c.GetHeader("token-signature")
		sin := c.GetHeader("sin")
		md5Signature := c.GetHeader("md5-signature")
		pass := c.GetHeader("pass")
		tm := c.GetHeader("tm")

		// 检查必需的请求头
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("缺少token"),
			})
			c.Abort()
			return
		}
		if tokenSignature == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("缺少token签名"),
			})
			c.Abort()
			return
		}
		if sin == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("缺少sin参数", "Sin"),
			})
			c.Abort()
			return
		}
		if md5Signature == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("缺少md5签名"),
			})
			c.Abort()
			return
		}
		if pass == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("缺少pass参数"),
			})
			c.Abort()
			return
		}
		if tm == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("缺少tm参数"),
			})
			c.Abort()
			return
		}

		// 2. 验证token的HMAC签名
		if !function.VerifyTokenSignature(token, tokenSignature) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("token签名验证失败"),
			})
			c.Abort()
			return
		}

		// 3. 解密token获取sessionId
		sessionID, err := function.DecryptToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("token解密失败: " + err.Error()),
			})
			c.Abort()
			return
		}

		// 4. 查询sessionId状态
		codeSession, err := codeSessionModel.GetBySessionID(sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  "无效的session_id",
			})
			c.Abort()
			return
		}

		// 5. 检查是否被封禁
		if codeSession.IsBanned {
			c.JSON(http.StatusForbidden, gin.H{
				"code": 403,
				"msg":  "账号已被封禁，无法使用",
			})
			c.Abort()
			return
		}

		// 6. 读取请求体并验证sin
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatParamError("读取请求体失败: " + err.Error()),
			})
			c.Abort()
			return
		}
		// 恢复请求体，以便后续处理函数使用
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// multipart 上传时前端用 "{}" 参与签名，此处用 "{}" 校验 sin
		bodyForSin := bodyBytes
		if strings.Contains(c.GetHeader("Content-Type"), "multipart/form-data") {
			bodyForSin = []byte("{}")
		}
		// 验证sin
		sinValid, err := function.VerifySin(bodyForSin, sin)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatParamError("sin验证失败: "+err.Error(), "Sin"),
			})
			c.Abort()
			return
		}
		if !sinValid {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("sin验证失败", "Sin"),
			})
			c.Abort()
			return
		}

		// 7. 验证md5签名
		// 获取接口地址（包含路径和查询参数）
		apiPath := c.Request.URL.Path
		if c.Request.URL.RawQuery != "" {
			apiPath += "?" + c.Request.URL.RawQuery
		}

		md5Valid, err := function.VerifyMD5Signature(sin, tokenSignature, apiPath, md5Signature)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatParamError("md5签名验证失败: " + err.Error()),
			})
			c.Abort()
			return
		}
		if !md5Valid {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("md5签名验证失败"),
			})
			c.Abort()
			return
		}

		// 8. 验证tm参数（时间戳和接口地址）
		tmValid, timestamp, err := function.VerifyTm(tm, apiPath, 300) // 5分钟有效期
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatParamError("tm验证失败: " + err.Error()),
			})
			c.Abort()
			return
		}
		if !tmValid {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("tm验证失败"),
			})
			c.Abort()
			return
		}

		// 9. 验证pass参数（验证时间戳是否与tm一致）
		deviceID := codeSession.DeviceID
		passValid, err := function.VerifyPass(pass, sin, md5Signature, deviceID, timestamp)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 400,
				"msg":  FormatParamError("pass验证失败: " + err.Error()),
			})
			c.Abort()
			return
		}
		if !passValid {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code": 401,
				"msg":  FormatParamError("pass验证失败"),
			})
			c.Abort()
			return
		}

		// 10. 鉴权通过，将session信息存储到context中，供后续处理函数使用
		c.Set("session_id", sessionID)
		c.Set("user_id", codeSession.UserID)
		c.Set("code_session", codeSession)

		c.Next()
	}
}

// GetTokenUserID 从context获取用户ID（token验证后）
func GetTokenUserID(c *gin.Context) int64 {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0
	}
	id, ok := userID.(int64)
	if !ok {
		return 0
	}
	return id
}

// GetTokenSessionID 从context获取sessionID（token验证后）
func GetTokenSessionID(c *gin.Context) string {
	sessionID, exists := c.Get("session_id")
	if !exists {
		return ""
	}
	id, ok := sessionID.(string)
	if !ok {
		return ""
	}
	return id
}

// GetTokenCodeSession 从context获取CodeSession（token验证后）
func GetTokenCodeSession(c *gin.Context) *model.CodeSession {
	codeSession, exists := c.Get("code_session")
	if !exists {
		return nil
	}
	cs, ok := codeSession.(*model.CodeSession)
	if !ok {
		return nil
	}
	return cs
}
