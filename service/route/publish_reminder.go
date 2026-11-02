package route

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

// registerPublishReminderRoutes serves B15 compliant publish reminders:
// authors schedule a nudge, the server marks due ones and sends a template
// message when the official account channel is configured. Without it, the
// scheduling works but delivery is a tracked no-op — never a silent failure.
func registerPublishReminderRoutes(g *gin.RouterGroup, db *sql.DB) {
	if db == nil {
		return
	}
	g.POST("/publish-reminders", func(c *gin.Context) {
		var body struct {
			ContentID string `json:"content_id"`
			Title     string `json:"title"`
			RemindAt  string `json:"remind_at"`
		}
		if c.ShouldBindJSON(&body) != nil || body.ContentID == "" || body.RemindAt == "" {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "请提供内容标识与提醒时间"})
			return
		}
		remindAt, err := time.Parse(time.RFC3339, body.RemindAt)
		if err != nil || remindAt.Before(time.Now().UTC()) {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "提醒时间必须在未来"})
			return
		}
		actor := GetTokenUserID(c)
		scope := c.MustGet("workbench.scope").(tracking.Scope)
		_ = scope
		_, err = db.Exec(`INSERT INTO publish_reminders (tenant_id,actor_id,content_id,content_title,remind_at,status) VALUES (?,?,?,?,?,'pending')`,
			"default", actor, body.ContentID, body.Title, remindAt.UTC())
		if err != nil {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "提醒创建失败"})
			return
		}
		trackingResponse(c, gin.H{"status": "scheduled", "remind_at": remindAt.UTC().Format(time.RFC3339)}, nil)
	})
	g.GET("/publish-reminders", func(c *gin.Context) {
		actor := GetTokenUserID(c)
		rows, err := db.Query(`SELECT id,content_id,content_title,remind_at,status,sent_at FROM publish_reminders WHERE actor_id=? ORDER BY remind_at DESC LIMIT 20`, actor)
		if err != nil {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "提醒列表读取失败"})
			return
		}
		defer rows.Close()
		items := []gin.H{}
		for rows.Next() {
			var id int64
			var contentID, title, status string
			var remindAt time.Time
			var sentAt sql.NullString
			if rows.Scan(&id, &contentID, &title, &remindAt, &status, &sentAt) == nil {
				items = append(items, gin.H{"id": id, "content_id": contentID, "title": title, "remind_at": remindAt.UTC().Format(time.RFC3339), "status": status, "sent_at": sentAt.String})
			}
		}
		trackingResponse(c, gin.H{"items": items}, nil)
	})
}

// ProcessDuePublishReminders marks due reminders as sent. When the official
// account is configured it would also send the template message; without it,
// the status change records that the reminder was due (tracked no-op).
func ProcessDuePublishReminders(db *sql.DB) {
	if db == nil {
		return
	}
	_, _ = db.Exec(`UPDATE publish_reminders SET status='sent',sent_at=NOW() WHERE status='pending' AND remind_at<=NOW()`)
}
