package route

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

func projectNoteHistoryLimit(c *gin.Context) (int, bool) {
	raw := c.Query("limit")
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "分页数量无效"})
		return 0, false
	}
	return limit, true
}

// registerProjectNoteHistoryHandlers is kept separate so the history contract
// can be wired into the main route only after its bounded queries are accepted.
func registerProjectNoteHistoryHandlers(root, manage *gin.RouterGroup, svc *tracking.Service, deps projectNoteHandlerDeps) {
	manage.GET("/projects/:id/history/:type", func(c *gin.Context) {
		limit, ok := projectNoteHistoryLimit(c)
		if !ok {
			return
		}
		page, err := svc.ProjectNoteHistory(c.Request.Context(), projectNoteScope(c), c.Param("id"), c.Param("type"), c.Query("version_id"), limit, c.Query("cursor"))
		trackingResponse(c, page, err)
	})
	manage.GET("/projects/:id/feedback/:feedback_id", func(c *gin.Context) {
		feedback, err := svc.ProjectNoteGetFeedback(c.Request.Context(), projectNoteScope(c), c.Param("id"), c.Param("feedback_id"))
		trackingResponse(c, feedback, err)
	})
	manage.GET("/projects/:id/notes/:note_id", func(c *gin.Context) {
		note, err := svc.ProjectNoteGetNote(c.Request.Context(), projectNoteScope(c), c.Param("id"), c.Param("note_id"))
		trackingResponse(c, note, err)
	})
	root.GET("/shared/:token/history/:type", func(c *gin.Context) {
		limit, ok := projectNoteHistoryLimit(c)
		if !ok {
			return
		}
		page, err := svc.ProjectNoteSharedHistory(c.Request.Context(), c.Param("token"), deps.actor(c), c.Param("type"), limit, c.Query("cursor"))
		trackingResponse(c, page, err)
	})
}
