package route

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"service/notedocx"
	"service/tracking"
)

const projectNoteExportMaxBytes int64 = 32 << 20
const projectNoteExportTimeout = 20 * time.Second

var projectNoteExportSlots = make(chan struct{}, 2)

func registerProjectNoteExportHandlers(root, manage *gin.RouterGroup, svc *tracking.Service, deps projectNoteHandlerDeps) {
	manage.GET("/projects/:id/versions/:version", func(c *gin.Context) {
		version, err := svc.ProjectNoteGetVersion(c.Request.Context(), projectNoteScope(c), c.Param("id"), c.Param("version"))
		trackingResponse(c, version, err)
	})
	manage.Match([]string{http.MethodGet, http.MethodHead}, "/projects/:id/versions/:version/export.docx", func(c *gin.Context) {
		scope, projectID, versionID := projectNoteScope(c), c.Param("id"), c.Param("version")
		serveProjectNoteDOCX(c, svc, func(ctx context.Context) (tracking.ProjectNoteVersion, error) {
			return svc.ProjectNoteGetVersion(ctx, scope, projectID, versionID)
		})
	})
	root.Match([]string{http.MethodGet, http.MethodHead}, "/shared/:token/export.docx", func(c *gin.Context) {
		token, actor := c.Param("token"), deps.actor(c)
		serveProjectNoteDOCX(c, svc, func(ctx context.Context) (tracking.ProjectNoteVersion, error) {
			return svc.ProjectNoteSharedVersion(ctx, token, actor)
		})
	})
}

func buildProjectNoteDOCX(ctx context.Context, svc *tracking.Service, version tracking.ProjectNoteVersion, max int64) ([]byte, error) {
	doc := notedocx.Document{Title: version.Title, Subtitle: fmt.Sprintf("固定版本 V%d · %s", version.Revision, version.FrozenAt.UTC().Format("2006-01-02 15:04 UTC")), Body: version.Body, BodyFormat: version.BodyFormat}
	assets := append([]tracking.Asset(nil), version.Media...)
	if version.Cover != nil {
		doc.CoverID = version.Cover.ID
		if !projectNoteHasMedia(assets, version.Cover.ID) {
			assets = append(assets, *version.Cover)
		}
	}
	var total int64
	for _, asset := range assets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		media := notedocx.Media{ID: asset.ID, Kind: asset.Kind, MIME: asset.MIME, Width: asset.Width, Height: asset.Height}
		if asset.Kind == "image" {
			data, found, err := readContentMedia(ctx, svc, asset.ID, notedocx.MaxImageBytes)
			if err != nil {
				if contextErr := ctx.Err(); contextErr != nil {
					return nil, contextErr
				}
				log.Printf("[project-note-export] error operation=media_read error_type=%T", err)
				return nil, tracking.ErrUnavailable
			}
			if !found || len(data) == 0 {
				return nil, &tracking.Error{Status: 503, Message: "方案图片暂不可用，未生成不完整文件"}
			}
			total += int64(len(data))
			if total > max {
				return nil, notedocx.ErrLimit
			}
			media.Data = data
		}
		doc.Media = append(doc.Media, media)
	}
	return notedocx.Build(ctx, doc, notedocx.Limits{MaxOutputBytes: max})
}
func projectNoteHasMedia(assets []tracking.Asset, id string) bool {
	for _, asset := range assets {
		if asset.ID == id {
			return true
		}
	}
	return false
}

func serveProjectNoteDOCX(c *gin.Context, svc *tracking.Service, resolve func(context.Context) (tracking.ProjectNoteVersion, error)) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), projectNoteExportTimeout)
	defer cancel()
	version, err := resolve(ctx)
	if err != nil {
		trackingResponse(c, nil, err)
		return
	}
	select {
	case projectNoteExportSlots <- struct{}{}:
		defer func() { <-projectNoteExportSlots }()
	default:
		c.Header("Retry-After", "1")
		trackingResponse(c, nil, &tracking.Error{Status: 429, Message: "导出任务较多，请稍后重试"})
		return
	}
	data, err := buildProjectNoteDOCX(ctx, svc, version, projectNoteExportMaxBytes)
	if err != nil {
		status, message := 503, "方案导出暂不可用，请稍后重试"
		if errors.Is(err, notedocx.ErrLimit) {
			status, message = 413, "方案图片或文件超过导出上限，请拆分方案"
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status, message = 504, "方案导出超时，请稍后重试"
		}
		log.Printf("[project-note-export] error operation=build error_type=%T", err)
		trackingResponse(c, nil, &tracking.Error{Status: status, Message: message})
		return
	}
	current, err := resolve(ctx)
	if err != nil {
		trackingResponse(c, nil, err)
		return
	}
	if !reflect.DeepEqual(current, version) {
		trackingResponse(c, nil, tracking.ErrUnavailable)
		return
	}
	if err = ctx.Err(); err != nil {
		trackingResponse(c, nil, &tracking.Error{Status: 504, Message: "方案导出超时，请稍后重试"})
		return
	}
	controller := http.NewResponseController(c.Writer)
	deadline, _ := ctx.Deadline()
	if err = controller.SetWriteDeadline(deadline); err != nil {
		log.Printf("[project-note-export] error operation=write_deadline error_type=%T", err)
		trackingResponse(c, nil, tracking.ErrUnavailable)
		return
	}
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	c.Header("Content-Disposition", `attachment; filename="jiadi-project-note.docx"`)
	c.Header("Content-Length", strconv.Itoa(len(data)))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'")
	c.Header("Accept-Ranges", "none")
	c.Status(http.StatusOK)
	if c.Request.Method != http.MethodHead {
		written, writeErr := io.CopyBuffer(c.Writer, bytes.NewReader(data), make([]byte, 32<<10))
		if writeErr != nil || written != int64(len(data)) {
			log.Printf("[project-note-export] error operation=write error_type=%T bytes=%d", writeErr, written)
			return
		}
	}
	if err = controller.Flush(); err != nil {
		log.Printf("[project-note-export] error operation=flush error_type=%T", err)
		return
	}
	if err = controller.SetWriteDeadline(time.Time{}); err != nil {
		log.Printf("[project-note-export] error operation=clear_deadline error_type=%T", err)
	}
}
