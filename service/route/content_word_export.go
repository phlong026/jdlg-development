package route

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

func registerContentWordExportHandler(g *gin.RouterGroup, svc *tracking.Service, scope func(*gin.Context) tracking.Scope) {
	g.Match([]string{http.MethodGet, http.MethodHead}, "/contents/:id/versions/:version/export.docx", func(c *gin.Context) {
		version, err := strconv.Atoi(c.Param("version"))
		if err != nil || version <= 0 {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusNotFound, Message: "素材不可访问"})
			return
		}
		currentScope := scope(c)
		serveProjectNoteDOCX(c, svc, func(ctx context.Context) (tracking.ProjectNoteVersion, error) {
			if _, err := svc.OpenPublishedMaterial(ctx, currentScope, c.Param("id"), version); err != nil {
				return tracking.ProjectNoteVersion{}, err
			}
			content, err := svc.ReadContent(ctx, c.Param("id"), version)
			if err != nil {
				return tracking.ProjectNoteVersion{}, err
			}
			if content.PublishedAt == nil {
				return tracking.ProjectNoteVersion{}, tracking.ErrUnavailable
			}
			return tracking.ProjectNoteVersion{
				ID: content.ID + ":" + strconv.Itoa(content.Version), Revision: content.Version,
				Title: content.Title, Body: content.Body, BodyFormat: content.BodyFormat,
				Cover: content.Cover, Media: content.Media, FrozenAt: *content.PublishedAt,
			}, nil
		})
	})
}
