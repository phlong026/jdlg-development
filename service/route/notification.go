package route

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/notification"
)

var noticeTemplateFieldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

// parseNoticeTemplateFields accepts only an explicit server-side mapping from
// verified provider fields to known notice values. Duplicate JSON keys and
// trailing content are rejected instead of silently guessing a template.
func parseNoticeTemplateFields(raw string) (map[string]string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, notification.ErrInvalidInput
	}
	fields := map[string]string{}
	allowed := map[string]bool{"title": true, "body": true, "time": true}
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok || !noticeTemplateFieldName.MatchString(key) || fields[key] != "" {
			return nil, notification.ErrInvalidInput
		}
		var value string
		if decoder.Decode(&value) != nil || !allowed[value] {
			return nil, notification.ErrInvalidInput
		}
		fields[key] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') || len(fields) == 0 || len(fields) > 20 {
		return nil, notification.ErrInvalidInput
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, notification.ErrInvalidInput
	}
	return fields, nil
}

// RegisterNotificationRoutes registers recipient-scoped mini-program inbox
// endpoints. Pass the unauthenticated /miniprogram group; this function adds
// the standard token and banned-user gates itself.
func RegisterNotificationRoutes(group *gin.RouterGroup, store notification.InboxStore, users *model.UserModel, codes *model.CodeSessionRedisModel) {
	authed := group.Group("/notifications", TokenAuthRequired(codes), MiniprogramBannedBlock(users))
	registerNotificationHandlers(authed, store)
}

func registerNotificationHandlers(group *gin.RouterGroup, store notification.InboxStore) {
	group.Use(func(c *gin.Context) {
		if store == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "msg": "消息中心暂不可用"})
			return
		}
		c.Next()
	})
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.Next()
	})
	group.GET("", func(c *gin.Context) {
		limit := 20
		if raw := c.Query("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				notificationResponse(c, nil, notification.ErrInvalidInput)
				return
			}
			limit = parsed
		}
		page, err := store.ListInbox(c.Request.Context(), GetTokenUserID(c), c.Query("cursor"), limit)
		notificationResponse(c, page, err)
	})
	group.GET("/unread-count", func(c *gin.Context) {
		count, err := store.UnreadCount(c.Request.Context(), GetTokenUserID(c))
		notificationResponse(c, gin.H{"unread_count": count}, err)
	})
	group.PATCH("/read-all", func(c *gin.Context) {
		count, err := store.MarkAllRead(c.Request.Context(), GetTokenUserID(c), time.Now().UTC())
		notificationResponse(c, gin.H{"updated": count}, err)
	})
	group.GET("/:id", func(c *gin.Context) {
		item, err := store.GetNotification(c.Request.Context(), GetTokenUserID(c), c.Param("id"))
		notificationResponse(c, item, err)
	})
	group.PATCH("/:id/read", func(c *gin.Context) {
		err := store.MarkRead(c.Request.Context(), GetTokenUserID(c), c.Param("id"), time.Now().UTC())
		notificationResponse(c, gin.H{"notification_id": c.Param("id"), "read": err == nil}, err)
	})
}

func notificationResponse(c *gin.Context, value any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": value})
		return
	}
	status, message := http.StatusServiceUnavailable, "消息中心暂不可用"
	switch {
	case errors.Is(err, notification.ErrInvalidInput):
		status, message = http.StatusBadRequest, "请求参数无效"
	case errors.Is(err, notification.ErrNotFound), errors.Is(err, notification.ErrConflict):
		status, message = http.StatusNotFound, "消息不存在"
	}
	c.AbortWithStatusJSON(status, gin.H{"code": status, "msg": message})
}
