package route

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"service/model"
)

const miniprogramAPIPrefix = "/api/v1/miniprogram"

type miniprogramProfile struct {
	Nickname string
	Avatar   string
}

type miniprogramProfileLoader func(context.Context, int64) (miniprogramProfile, error)

// These exact endpoints are the only paths that may run before profile completion.
// Endpoints that read or update a profile keep their existing route-level token check.
var miniprogramProfileBootstrap = map[string]map[string]bool{
	http.MethodGet: {
		miniprogramAPIPrefix + "/profile":         true,
		miniprogramAPIPrefix + "/tracking/config": true,
	},
	http.MethodPost: {
		miniprogramAPIPrefix + "/login":                            true,
		miniprogramAPIPrefix + "/login/password":                   true,
		miniprogramAPIPrefix + "/login/phone":                      true,
		miniprogramAPIPrefix + "/sms/send":                         true,
		miniprogramAPIPrefix + "/profile/password/recover":         true,
		miniprogramAPIPrefix + "/ai/upload-reference-image":        true,
		miniprogramAPIPrefix + "/tracking/share-entry":             true,
		miniprogramAPIPrefix + "/wecom/enterprise-wechat/callback": true,
	},
	http.MethodPut: {
		miniprogramAPIPrefix + "/profile/nickname": true,
		miniprogramAPIPrefix + "/profile/avatar":   true,
		miniprogramAPIPrefix + "/profile/identity": true,
	},
}

func isMiniprogramAPIPath(path string) bool {
	return path == miniprogramAPIPrefix || strings.HasPrefix(path, miniprogramAPIPrefix+"/")
}

func isMiniprogramProfileBootstrap(method, path string) bool {
	return method == http.MethodOptions || miniprogramProfileBootstrap[method][path]
}

// miniprogramProfileAuthentication is deliberately separate from completeness.
// TokenAuthRequired calls c.Next(), so the next middleware must own the profile
// check to ensure no business handler runs first.
func miniprogramProfileAuthentication(auth gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isMiniprogramAPIPath(c.Request.URL.Path) || isMiniprogramProfileBootstrap(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		c.Header("Cache-Control", "private,no-store")
		auth(c)
	}
}

func miniprogramProfileCompleteness(load miniprogramProfileLoader) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isMiniprogramAPIPath(c.Request.URL.Path) || isMiniprogramProfileBootstrap(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		userID := GetTokenUserID(c)
		if userID <= 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		profile, err := load(ctx, userID)
		if err != nil {
			log.Printf("[profile-access] error reason=profile_read_failed error_type=%T", err)
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "用户资料暂时无法核验，请重试"})
			return
		}
		missing := miniprogramProfileMissingFields(profile)
		if len(missing) > 0 {
			c.AbortWithStatusJSON(http.StatusPreconditionRequired, gin.H{
				"code": 428,
				"msg":  "请先补充头像和昵称",
				"data": gin.H{"profile_required": true, "missing_fields": missing},
			})
			return
		}
		c.Next()
	}
}

func miniprogramProfileLoaderFromModel(profiles *model.UserProfileModel) miniprogramProfileLoader {
	return func(ctx context.Context, userID int64) (miniprogramProfile, error) {
		if profiles == nil || profiles.DB == nil {
			return miniprogramProfile{}, errors.New("profile store unavailable")
		}
		var profile miniprogramProfile
		err := profiles.DB.QueryRowContext(ctx,
			`SELECT COALESCE(nickname, ''), COALESCE(avatar, '') FROM user_profiles WHERE user_id = ?`,
			userID,
		).Scan(&profile.Nickname, &profile.Avatar)
		if errors.Is(err, sql.ErrNoRows) {
			return miniprogramProfile{}, nil
		}
		if err != nil {
			return miniprogramProfile{}, err
		}
		return profile, nil
	}
}

func miniprogramProfileMissingFields(profile miniprogramProfile) []string {
	missing := make([]string, 0, 2)
	nickname := strings.TrimSpace(profile.Nickname)
	if nickname == "" || !utf8.ValidString(nickname) || utf8.RuneCountInString(nickname) > 32 {
		missing = append(missing, "nickname")
	}
	if !hasSavedProfileAvatar(profile.Avatar) {
		missing = append(missing, "avatar")
	}
	return missing
}

func hasSavedProfileAvatar(raw string) bool {
	avatar := strings.TrimSpace(raw)
	if avatar == "" || len(avatar) > 2048 || containsSpaceOrBackslash(avatar) || strings.Contains(avatar, "#") ||
		strings.Contains(avatar, "/assets/images/home.jpg") ||
		strings.Contains(avatar, "/assets/images/default-avatar") {
		return false
	}
	decoded, err := url.PathUnescape(avatar)
	if err != nil || !utf8.ValidString(decoded) {
		return false
	}
	parsed, err := url.ParseRequestURI(avatar)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Path == "" || parsed.Path[0] != '/' {
		return false
	}
	if parsed.Port() != "" || net.ParseIP(parsed.Hostname()) != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	labels := strings.Split(host, ".")
	if len(labels) < 2 || labels[len(labels)-1] == "local" || labels[len(labels)-1] == "localhost" || !onlyASCIIAlpha(labels[len(labels)-1]) {
		return false
	}
	for _, label := range labels {
		if !validDNSLabel(label) {
			return false
		}
	}
	return true
}

func containsSpaceOrBackslash(value string) bool {
	for _, character := range value {
		if character == '\\' || unicode.IsSpace(character) {
			return true
		}
	}
	return false
}

func onlyASCIIAlpha(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < 'a' || character > 'z' {
			return false
		}
	}
	return true
}

func validDNSLabel(label string) bool {
	if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, character := range label {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}
