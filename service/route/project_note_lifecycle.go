package route

import (
	"github.com/gin-gonic/gin"
	"service/tracking"
)

type projectNoteLifecycleInput struct {
	ExpectedRevision int `json:"expected_revision"`
}

func registerProjectNoteLifecycleHandlers(manage *gin.RouterGroup, svc *tracking.Service) {
	manage.DELETE("/projects/:id", func(c *gin.Context) {
		var input projectNoteLifecycleInput
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteDeleteProject(c.Request.Context(), projectNoteScope(c), c.Param("id"), input.ExpectedRevision)
		trackingResponse(c, out, err)
	})

	manage.POST("/notes/:id/copy", func(c *gin.Context) {
		var input projectNoteLifecycleInput
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteCopy(c.Request.Context(), projectNoteScope(c), c.Param("id"), input.ExpectedRevision)
		trackingResponse(c, out, err)
	})

	manage.DELETE("/notes/:id", func(c *gin.Context) {
		var input projectNoteLifecycleInput
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteDelete(c.Request.Context(), projectNoteScope(c), c.Param("id"), input.ExpectedRevision)
		trackingResponse(c, out, err)
	})
}
