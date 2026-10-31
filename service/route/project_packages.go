package route

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

type projectPackageDraftRequest struct {
	PackageID        string                                 `json:"package_id,omitempty"`
	ProjectID        string                                 `json:"project_id,omitempty"`
	ExpectedRevision int                                    `json:"expected_revision,omitempty"`
	VersionIDs       []string                               `json:"version_ids"`
	NoteSelections   []tracking.ProjectPackageNoteSelection `json:"note_selections,omitempty"`
	RecipientIDs     []int64                                `json:"recipient_ids"`
	IdempotencyKey   string                                 `json:"idempotency_key"`
}

type projectPackagePublishRequest struct {
	PackageID        string    `json:"package_id,omitempty"`
	ExpectedRevision int       `json:"expected_revision"`
	PreviewSignature string    `json:"preview_signature"`
	ExpiresAt        time.Time `json:"expires_at"`
	IdempotencyKey   string    `json:"idempotency_key"`
}

func projectPackagePathIdentity(c *gin.Context, body, path, label string) bool {
	if strings.TrimSpace(body) == "" || strings.TrimSpace(body) == path {
		return true
	}
	trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: label + "与访问路径不一致"})
	return false
}

// registerProjectPackageHandlers intentionally receives the already protected
// manager and invited groups. Authentication, feature gating and designer scope
// remain the responsibility of the shared route registration.
func registerProjectPackageHandlers(manage, shared *gin.RouterGroup, svc *tracking.Service, actor func(*gin.Context) int64) {
	manage.PUT("/projects/:id/packages/draft", func(c *gin.Context) {
		var body projectPackageDraftRequest
		if !projectNoteBind(c, &body) || !projectPackagePathIdentity(c, body.ProjectID, c.Param("id"), "项目") {
			return
		}
		out, err := svc.ProjectPackageSaveDraft(c.Request.Context(), projectNoteScope(c), tracking.ProjectPackageDraftInput{
			PackageID: body.PackageID, ProjectID: c.Param("id"), ExpectedRevision: body.ExpectedRevision,
			VersionIDs: body.VersionIDs, NoteSelections: body.NoteSelections, RecipientIDs: body.RecipientIDs, IdempotencyKey: body.IdempotencyKey,
		})
		trackingResponse(c, out, err)
	})
	manage.GET("/packages/:id/preview", func(c *gin.Context) {
		out, err := svc.ProjectPackagePreview(c.Request.Context(), projectNoteScope(c), c.Param("id"))
		trackingResponse(c, out, err)
	})
	manage.POST("/packages/:id/publish", func(c *gin.Context) {
		var body projectPackagePublishRequest
		if !projectNoteBind(c, &body) || !projectPackagePathIdentity(c, body.PackageID, c.Param("id"), "分享资料") {
			return
		}
		out, err := svc.ProjectPackagePublish(c.Request.Context(), projectNoteScope(c), tracking.ProjectPackagePublishInput{
			PackageID: c.Param("id"), ExpectedRevision: body.ExpectedRevision, PreviewSignature: body.PreviewSignature,
			ExpiresAt: body.ExpiresAt, IdempotencyKey: body.IdempotencyKey,
		})
		trackingResponse(c, out, err)
	})
	manage.POST("/package-deliveries/:id/revoke", func(c *gin.Context) {
		err := svc.ProjectPackageRevoke(c.Request.Context(), projectNoteScope(c), c.Param("id"))
		trackingResponse(c, gin.H{"revoked": err == nil}, err)
	})
	manage.GET("/projects/:id/package-history", func(c *gin.Context) {
		out, err := svc.ProjectPackageGetHistory(c.Request.Context(), projectNoteScope(c), c.Param("id"))
		trackingResponse(c, out, err)
	})

	shared.GET("/shared/:token", func(c *gin.Context) {
		out, err := svc.ProjectPackageRead(c.Request.Context(), c.Param("token"), actor(c))
		trackingResponse(c, out, err)
	})
	shared.GET("/shared/:token/versions/:version", func(c *gin.Context) {
		out, err := svc.ProjectPackageReadVersion(c.Request.Context(), c.Param("token"), actor(c), c.Param("version"))
		trackingResponse(c, out, err)
	})
	shared.Match([]string{http.MethodGet, http.MethodHead}, "/shared/:token/versions/:version/media/:media", func(c *gin.Context) {
		asset, err := svc.ProjectPackageReadVersionMedia(c.Request.Context(), c.Param("token"), actor(c), c.Param("version"), c.Param("media"))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		serveContentMediaBytes(c, svc, asset)
	})
	shared.POST("/shared/:token/versions/:version/feedback", func(c *gin.Context) {
		var body struct {
			Text           string `json:"text"`
			Satisfaction   string `json:"satisfaction"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if !projectNoteBind(c, &body) {
			return
		}
		out, err := svc.ProjectPackageAddFeedback(c.Request.Context(), c.Param("token"), actor(c), c.Param("version"), body.Text, body.Satisfaction, body.IdempotencyKey)
		trackingResponse(c, out, err)
	})
	shared.POST("/shared/:token/versions/:version/confirm", func(c *gin.Context) {
		var body struct {
			IdempotencyKey string `json:"idempotency_key"`
		}
		if !projectNoteBind(c, &body) {
			return
		}
		out, err := svc.ProjectPackageConfirmVersion(c.Request.Context(), c.Param("token"), actor(c), c.Param("version"), body.IdempotencyKey)
		trackingResponse(c, out, err)
	})
	shared.POST("/shared/:token/receipts", func(c *gin.Context) {
		var body struct {
			VersionID    string `json:"version_id,omitempty"`
			EventID      string `json:"event_id"`
			Acknowledged bool   `json:"acknowledged"`
		}
		if !projectNoteBind(c, &body) {
			return
		}
		if !body.Acknowledged {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "请由用户明确确认阅读回执"})
			return
		}
		out, err := svc.ProjectPackageAcknowledge(c.Request.Context(), c.Param("token"), actor(c), body.VersionID, body.EventID)
		trackingResponse(c, out, err)
	})
}
