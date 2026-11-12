package route

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
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

// Bootstrap endpoints keep their existing route-level token checks where required.
// Public preview and authenticated AI operations are classified separately below.
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
		miniprogramAPIPrefix + "/tracking/share-entry":             true,
		miniprogramAPIPrefix + "/wecom/enterprise-wechat/callback": true,
	},
	http.MethodPut: {
		miniprogramAPIPrefix + "/profile/nickname": true,
		miniprogramAPIPrefix + "/profile/avatar":   true,
		miniprogramAPIPrefix + "/profile/identity": true,
	},
}

// These routes carry only public browse data or tracking credentials. The
// tracking handlers still validate any supplied login or entry credential.
// Keep this list exact: detail pages and all business mutations remain gated.
var miniprogramProfilePublicTransport = map[string]map[string]bool{
	http.MethodGet: {
		miniprogramAPIPrefix + "/ai/pricing":           true,
		miniprogramAPIPrefix + "/ai-tools":             true,
		miniprogramAPIPrefix + "/templates/featured":   true,
		miniprogramAPIPrefix + "/templates/tab-config": true,
		miniprogramAPIPrefix + "/templates":            true,
		miniprogramAPIPrefix + "/templates/search":     true,
		miniprogramAPIPrefix + "/inspirations":         true,
		miniprogramAPIPrefix + "/designers":            true,
	},
	http.MethodPost: {
		miniprogramAPIPrefix + "/public-share-links":    true,
		miniprogramAPIPrefix + "/tracking/entry":        true,
		miniprogramAPIPrefix + "/tracking/events/batch": true,
		miniprogramAPIPrefix + "/tracking/shares":       true,
	},
}

// AI browsing is public, while creating tasks and reading account-owned AI data
// only need an authenticated account. Avatar and nickname are presentation data
// and must not be prerequisites for using these account-scoped AI operations.
// Keep every static path explicit; dynamic paths are matched by their route shape.
var miniprogramProfileAuthOnly = map[string]map[string]bool{
	http.MethodGet: {
		miniprogramAPIPrefix + "/building-code-documents":              true,
		miniprogramAPIPrefix + "/user/stones":                          true,
		miniprogramAPIPrefix + "/user/stones/summary":                  true,
		miniprogramAPIPrefix + "/user/tasks":                           true,
		miniprogramAPIPrefix + "/user/ai/download-config":              true,
		miniprogramAPIPrefix + "/user/ai/task/download-original-file":  true,
		miniprogramAPIPrefix + "/user/ai/video/download-original-file": true,
	},
	http.MethodPost: {
		miniprogramAPIPrefix + "/ai/draw":                         true,
		miniprogramAPIPrefix + "/ai/chat":                         true,
		miniprogramAPIPrefix + "/ai/generate-prompt":              true,
		miniprogramAPIPrefix + "/ai/upload-reference-image":       true,
		miniprogramAPIPrefix + "/ai/rural-villa-design":           true,
		miniprogramAPIPrefix + "/ai/allround-design":              true,
		miniprogramAPIPrefix + "/ai/building-replacement":         true,
		miniprogramAPIPrefix + "/ai/parent-child-design":          true,
		miniprogramAPIPrefix + "/ai/cost/calculate":               true,
		miniprogramAPIPrefix + "/ai/cost/doc":                     true,
		miniprogramAPIPrefix + "/ai/chat/stream":                  true,
		miniprogramAPIPrefix + "/ai/intent/stream":                true,
		miniprogramAPIPrefix + "/ai/prompt/optimize":              true,
		miniprogramAPIPrefix + "/ai/building-code/chat":           true,
		miniprogramAPIPrefix + "/ai/task/status":                  true,
		miniprogramAPIPrefix + "/ai/video/create":                 true,
		miniprogramAPIPrefix + "/ai/video/generate-prompts":       true,
		miniprogramAPIPrefix + "/building-code-documents/upload":  true,
		miniprogramAPIPrefix + "/user/stones/details":             true,
		miniprogramAPIPrefix + "/user/ai/wecom/verify-phone":      true,
		miniprogramAPIPrefix + "/user/ai/task/download-original":  true,
		miniprogramAPIPrefix + "/user/ai/video/download-original": true,
	},
}

func isMiniprogramAPIPath(path string) bool {
	return path == miniprogramAPIPrefix || strings.HasPrefix(path, miniprogramAPIPrefix+"/")
}

// miniprogramProfileGateEnabled 控制“登录+完整资料”门禁是否生效。
// 配套小程序（7.2.18+）正式发版前，旧版客户端无法通过门禁，可临时设
// MINIPROGRAM_PROFILE_GATE=off 放开止血；未设置或任何其他值均视为开启。
func miniprogramProfileGateEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MINIPROGRAM_PROFILE_GATE"))) {
	case "off", "0", "false":
		return false
	default:
		return true
	}
}

// miniprogramProfileGateMiddlewares 返回需要挂载的门禁中间件；开关关闭时为空，
// 使小程序业务路由回到门禁启用前的既有行为。
func miniprogramProfileGateMiddlewares(auth gin.HandlerFunc, load miniprogramProfileLoader) []gin.HandlerFunc {
	if !miniprogramProfileGateEnabled() {
		return nil
	}
	return []gin.HandlerFunc{
		miniprogramProfileAuthentication(auth),
		miniprogramProfileCompleteness(load),
	}
}

func isMiniprogramProfileBootstrap(method, path string) bool {
	return method == http.MethodOptions || miniprogramProfileBootstrap[method][path]
}

func pathSingleSegmentBetween(path, prefix, suffix string) string {
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return ""
	}
	segment := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if segment == "" || strings.Contains(segment, "/") {
		return ""
	}
	return segment
}

func positiveIDSegment(path, prefix, suffix string) bool {
	segment := pathSingleSegmentBetween(path, prefix, suffix)
	id, err := strconv.ParseInt(segment, 10, 64)
	return err == nil && id > 0
}

func publicTrackingReadPath(method, path string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	prefix := miniprogramAPIPrefix + "/tracking/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	positive := func(value string) bool {
		n, err := strconv.Atoi(value)
		return err == nil && n >= 0
	}
	validID := func(value string, min, max int) bool {
		if len(value) < min || len(value) > max {
			return false
		}
		for _, char := range value {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
				return false
			}
		}
		return true
	}
	if len(parts) == 2 && parts[0] == "media" && validID(parts[1], 32, 32) {
		return true
	}
	if method == http.MethodHead {
		if len(parts) != 8 || parts[0] != "project-collections" || !validID(parts[1], 8, 128) || parts[2] != "versions" || parts[4] != "items" || !positive(parts[5]) || parts[6] != "media" || !validID(parts[7], 8, 128) {
			return false
		}
		version, err := strconv.Atoi(parts[3])
		return err == nil && version > 0
	}
	if method != http.MethodGet {
		return false
	}
	if len(parts) == 4 && parts[0] == "contents" && validID(parts[1], 8, 128) && parts[2] == "versions" {
		version, err := strconv.Atoi(parts[3])
		return err == nil && version > 0
	}
	if len(parts) < 4 || parts[0] != "project-collections" || !validID(parts[1], 8, 128) || parts[2] != "versions" {
		return false
	}
	version, err := strconv.Atoi(parts[3])
	if err != nil || version < 1 {
		return false
	}
	if len(parts) == 4 {
		return true
	}
	if len(parts) < 6 || parts[4] != "items" || !positive(parts[5]) {
		return false
	}
	if len(parts) == 6 || len(parts) == 7 && (parts[6] == "content") {
		return true
	}
	return len(parts) == 8 && parts[6] == "media" && validID(parts[7], 8, 128)
}

func isMiniprogramProfilePublicTransport(method, path string) bool {
	if miniprogramProfilePublicTransport[method][path] {
		return true
	}
	if publicTrackingReadPath(method, path) {
		return true
	}
	if method != http.MethodGet {
		return false
	}
	return positiveIDSegment(path, miniprogramAPIPrefix+"/templates/", "") ||
		positiveIDSegment(path, miniprogramAPIPrefix+"/designers/", "/homepage") ||
		positiveIDSegment(path, miniprogramAPIPrefix+"/inspirations/", "") ||
		positiveIDSegment(path, miniprogramAPIPrefix+"/ai-tools/", "") ||
		pathSingleSegmentBetween(path, miniprogramAPIPrefix+"/ai-tools/code/", "") != ""
}

func isMiniprogramProfileAuthOnly(method, path string) bool {
	if miniprogramProfileAuthOnly[method][path] {
		return true
	}
	switch method {
	case http.MethodGet:
		return positiveIDSegment(path, miniprogramAPIPrefix+"/ai/task/", "/info") ||
			pathSingleSegmentBetween(path, miniprogramAPIPrefix+"/ai/video/poll/", "") != "" ||
			pathSingleSegmentBetween(path, miniprogramAPIPrefix+"/ai/video/query/", "") != ""
	case http.MethodDelete:
		return positiveIDSegment(path, miniprogramAPIPrefix+"/building-code-documents/", "") ||
			pathSingleSegmentBetween(path, miniprogramAPIPrefix+"/user/tasks/", "") != ""
	}
	return false
}

func isMiniprogramProfileGateExempt(method, path string) bool {
	return isMiniprogramProfileBootstrap(method, path) || isMiniprogramProfilePublicTransport(method, path)
}

// miniprogramProfileAuthentication is deliberately separate from completeness.
// TokenAuthRequired calls c.Next(), so the next middleware must own the profile
// check to ensure no business handler runs first.
func miniprogramProfileAuthentication(auth gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isMiniprogramAPIPath(c.Request.URL.Path) || isMiniprogramProfileGateExempt(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		c.Header("Cache-Control", "private,no-store")
		auth(c)
	}
}

func miniprogramProfileCompleteness(load miniprogramProfileLoader) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isMiniprogramAPIPath(c.Request.URL.Path) || isMiniprogramProfileGateExempt(c.Request.Method, c.Request.URL.Path) || isMiniprogramProfileAuthOnly(c.Request.Method, c.Request.URL.Path) {
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
	if miniprogramProfileNicknameMissing(profile) {
		missing = append(missing, "nickname")
	}
	if !hasSavedProfileAvatar(profile.Avatar) {
		missing = append(missing, "avatar")
	}
	return missing
}

func miniprogramProfileNicknameMissing(profile miniprogramProfile) bool {
	nickname := strings.TrimSpace(profile.Nickname)
	return nickname == "" || !utf8.ValidString(nickname) || utf8.RuneCountInString(nickname) > 32
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
