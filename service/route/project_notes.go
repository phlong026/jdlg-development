package route

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
	"service/model"
	"service/tracking"
)

const projectNoteScopeKey = "project-notes.scope"

type projectNoteHandlerDeps struct {
	actor        func(*gin.Context) int64
	verifyMember func(context.Context, int64) error
}

func projectNotesEnabled() bool { return os.Getenv("PROJECT_NOTES_ENABLED") == "true" }

func verifyActiveMiniProgramAccount(ctx context.Context, db *sql.DB, account int64) error {
	if db == nil || account <= 0 {
		return &tracking.Error{Status: http.StatusBadRequest, Message: "受邀账号不存在或已停用"}
	}
	var found int
	err := tracking.SourceReadDB(ctx, db).QueryRowContext(ctx, `SELECT 1 FROM users WHERE id=? AND user_type='miniprogram' AND COALESCE(status,'active')='active'`, account).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return &tracking.Error{Status: http.StatusBadRequest, Message: "受邀账号不存在或已停用"}
	}
	if err != nil {
		return tracking.ErrUnavailable
	}
	return nil
}

func registerProjectNoteRoutes(v1 *gin.RouterGroup, svc *tracking.Service, users *model.UserModel, codes *model.CodeSessionRedisModel) {
	root := v1.Group("/miniprogram/project-notes",
		trackingEntryRateLimit(),
		func(c *gin.Context) {
			// Authentication verifies multipart requests before the handler. Bound
			// the stream first so an unauthenticated upload cannot grow unchecked.
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 51<<20)
			c.Next()
		},
		TokenAuthRequired(codes),
		MiniprogramBannedBlock(users),
		func(c *gin.Context) {
			c.Header("Cache-Control", "private, no-store")
			if !projectNotesEnabled() {
				trackingResponse(c, nil, &tracking.Error{Status: http.StatusForbidden, Message: "私人项目笔记暂未开通"})
				return
			}
			c.Next()
		},
	)
	manage := root.Group("", func(c *gin.Context) {
		scope, err := loadMiniTrackingMaterialScope(c.Request.Context(), users.DB, svc.Config.Tenant, GetTokenUserID(c))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		c.Set(projectNoteScopeKey, scope)
		c.Next()
	})
	registerProjectNoteHandlers(root, manage, svc, projectNoteHandlerDeps{
		actor: func(c *gin.Context) int64 { return GetTokenUserID(c) },
		verifyMember: func(ctx context.Context, account int64) error {
			return verifyActiveMiniProgramAccount(ctx, users.DB, account)
		},
	})
}

func projectNoteBind(c *gin.Context, value any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		var tooLarge *http.MaxBytesError
		status := http.StatusBadRequest
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		trackingResponse(c, nil, &tracking.Error{Status: status, Message: "请求字段或体积无效"})
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "请求内容无效"})
		return false
	}
	return true
}

func projectNoteScope(c *gin.Context) tracking.Scope {
	return c.MustGet(projectNoteScopeKey).(tracking.Scope)
}

func projectNoteAccount(c *gin.Context, raw string) (int64, bool) {
	account, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || account <= 0 {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "账号标识无效"})
		return 0, false
	}
	return account, true
}

func registerProjectNoteHandlers(root, manage *gin.RouterGroup, svc *tracking.Service, deps projectNoteHandlerDeps) {
	manage.GET("/projects", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		out, err := svc.ProjectNoteListProjects(c.Request.Context(), projectNoteScope(c), limit, c.Query("cursor"))
		trackingResponse(c, out, err)
	})
	manage.POST("/projects", func(c *gin.Context) {
		var input tracking.ProjectNoteProjectInput
		if !projectNoteBind(c, &input) {
			return
		}
		// The existing CRM verifier requires a trusted CRM subject and business
		// number which a mini-program designer scope does not carry. Fail closed
		// instead of treating a client-provided CRM id as proof of access.
		if strings.TrimSpace(input.CRMID) != "" {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusForbidden, Message: "小程序设计师CRM引用校验尚未开通"})
			return
		}
		out, err := svc.ProjectNoteCreateProject(c.Request.Context(), projectNoteScope(c), input)
		trackingResponse(c, out, err)
	})
	manage.GET("/projects/:id", func(c *gin.Context) {
		out, err := svc.ProjectNoteGetProjectDetail(c.Request.Context(), projectNoteScope(c), c.Param("id"))
		trackingResponse(c, out, err)
	})
	manage.POST("/projects/:id/members/:account", func(c *gin.Context) {
		account, ok := projectNoteAccount(c, c.Param("account"))
		if !ok {
			return
		}
		if err := deps.verifyMember(c.Request.Context(), account); err != nil {
			trackingResponse(c, nil, err)
			return
		}
		out, err := svc.ProjectNoteInviteMember(c.Request.Context(), projectNoteScope(c), c.Param("id"), account)
		trackingResponse(c, out, err)
	})
	manage.DELETE("/projects/:id/members/:account", func(c *gin.Context) {
		account, ok := projectNoteAccount(c, c.Param("account"))
		if !ok {
			return
		}
		err := svc.ProjectNoteRevokeMember(c.Request.Context(), projectNoteScope(c), c.Param("id"), account)
		trackingResponse(c, gin.H{"revoked": err == nil}, err)
	})
	manage.PUT("/projects/:id/confirmer", func(c *gin.Context) {
		var input struct {
			AccountID int64 `json:"account_id"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		if err := deps.verifyMember(c.Request.Context(), input.AccountID); err != nil {
			trackingResponse(c, nil, err)
			return
		}
		err := svc.ProjectNoteSetConfirmer(c.Request.Context(), projectNoteScope(c), c.Param("id"), input.AccountID)
		trackingResponse(c, gin.H{"confirmer_id": input.AccountID}, err)
	})
	manage.POST("/projects/:id/notes", func(c *gin.Context) {
		var input tracking.ProjectNoteDraft
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteCreateNote(c.Request.Context(), projectNoteScope(c), c.Param("id"), input)
		trackingResponse(c, out, err)
	})
	manage.GET("/projects/:id/media", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		out, err := svc.ProjectNoteListMedia(c.Request.Context(), projectNoteScope(c), c.Param("id"), limit, c.Query("cursor"))
		trackingResponse(c, out, err)
	})
	manage.POST("/projects/:id/media", func(c *gin.Context) {
		uploadProjectNoteMedia(c, svc, projectNoteScope(c), c.Param("id"))
	})
	manage.Match([]string{http.MethodGet, http.MethodHead}, "/projects/:id/media/:media", func(c *gin.Context) {
		media, err := svc.ProjectNoteGetMedia(c.Request.Context(), projectNoteScope(c), c.Param("id"), c.Param("media"))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		serveContentMediaBytes(c, svc, media.Asset)
	})
	manage.PATCH("/notes/:id", func(c *gin.Context) {
		var input struct {
			tracking.ProjectNoteDraft
			ExpectedRevision int `json:"expected_revision"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteSaveNote(c.Request.Context(), projectNoteScope(c), c.Param("id"), input.ProjectNoteDraft, input.ExpectedRevision)
		trackingResponse(c, out, err)
	})
	manage.POST("/notes/:id/versions", func(c *gin.Context) {
		var input struct {
			ExpectedRevision int `json:"expected_revision"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteFreezeVersion(c.Request.Context(), projectNoteScope(c), c.Param("id"), input.ExpectedRevision)
		trackingResponse(c, out, err)
	})
	manage.POST("/versions/:id/shares", func(c *gin.Context) {
		var input struct {
			ExpiresAt      time.Time `json:"expires_at"`
			IdempotencyKey string    `json:"idempotency_key"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteCreateShare(c.Request.Context(), projectNoteScope(c), c.Param("id"), input.ExpiresAt, input.IdempotencyKey)
		trackingResponse(c, out, err)
	})
	manage.POST("/shares/:id/revoke", func(c *gin.Context) {
		err := svc.ProjectNoteRevokeShare(c.Request.Context(), projectNoteScope(c), c.Param("id"))
		trackingResponse(c, gin.H{"revoked": err == nil}, err)
	})
	manage.POST("/feedback/:id/resolve", func(c *gin.Context) {
		var input struct {
			Reply          string `json:"reply"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteResolveFeedback(c.Request.Context(), projectNoteScope(c), c.Param("id"), input.Reply, input.IdempotencyKey)
		trackingResponse(c, out, err)
	})

	root.GET("/shared/:token", func(c *gin.Context) {
		out, err := svc.ProjectNoteReadShare(c.Request.Context(), c.Param("token"), deps.actor(c))
		if err == nil {
			out = projectNotePrivateMediaURLs(out, c.Param("token"))
		}
		trackingResponse(c, out, err)
	})
	root.POST("/shared/:token/open", func(c *gin.Context) {
		var input struct {
			EventID string `json:"event_id"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteOpenShare(c.Request.Context(), c.Param("token"), deps.actor(c), input.EventID)
		trackingResponse(c, out, err)
	})
	root.POST("/shared/:token/feedback", func(c *gin.Context) {
		var input struct {
			Text           string `json:"text"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteAddFeedback(c.Request.Context(), c.Param("token"), deps.actor(c), input.Text, input.IdempotencyKey)
		trackingResponse(c, out, err)
	})
	root.POST("/shared/:token/confirm", func(c *gin.Context) {
		var input struct {
			IdempotencyKey string `json:"idempotency_key"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteConfirmShare(c.Request.Context(), c.Param("token"), deps.actor(c), input.IdempotencyKey)
		trackingResponse(c, out, err)
	})
	root.Match([]string{http.MethodGet, http.MethodHead}, "/shared/:token/media/:id", func(c *gin.Context) {
		asset, err := svc.ProjectNoteReadShareMedia(c.Request.Context(), c.Param("token"), deps.actor(c), c.Param("id"))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		serveContentMediaBytes(c, svc, asset)
	})
}

func uploadProjectNoteMedia(c *gin.Context, svc *tracking.Service, scope tracking.Scope, projectID string) {
	if !contentMediaConfigured(svc) {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "私人项目媒体存储尚未配置"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 51<<20)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusRequestEntityTooLarge, Message: "上传体积超过限制"})
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	if len(c.Request.MultipartForm.Value) != 0 || len(c.Request.MultipartForm.File) != 1 || len(c.Request.MultipartForm.File["file"]) != 1 {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "上传字段无效"})
		return
	}
	source, header, err := c.Request.FormFile("file")
	if err != nil {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "缺少媒体文件"})
		return
	}
	defer source.Close()
	sniff := make([]byte, 512)
	count, _ := io.ReadFull(source, sniff)
	mime := http.DetectContentType(sniff[:count])
	kind, max := "image", int64(10<<20)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	case "video/mp4":
		kind, max = "video", 50<<20
	default:
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "仅支持经过类型校验的PNG/JPEG/GIF/WebP和MP4文件"})
		return
	}
	if header.Size <= 0 || header.Size > max {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusRequestEntityTooLarge, Message: "媒体体积超过限制"})
		return
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		trackingResponse(c, nil, err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(source, max+1))
	if err != nil || int64(len(data)) > max || len(data) == 0 {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusRequestEntityTooLarge, Message: "上传失败或超出限制"})
		return
	}
	width, height := 0, 0
	if kind == "image" {
		config, _, decodeErr := image.DecodeConfig(bytes.NewReader(data))
		if decodeErr != nil || config.Width < 1 || config.Height < 1 || config.Width > 20000 || config.Height > 20000 {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "图片尺寸无效"})
			return
		}
		width, height = config.Width, config.Height
	} else {
		width, height, err = trackingMP4Dimensions(data)
		if err != nil {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "无法识别MP4尺寸，请选择标准MP4原文件"})
			return
		}
	}
	randomID := make([]byte, 24)
	if _, err = rand.Read(randomID); err != nil {
		trackingResponse(c, nil, tracking.ErrUnavailable)
		return
	}
	mediaID := base64.RawURLEncoding.EncodeToString(randomID)
	asset := tracking.Asset{ID: mediaID, Kind: kind, URL: fmt.Sprintf("/api/v1/miniprogram/project-notes/projects/%s/media/%s", url.PathEscape(projectID), mediaID), Width: width, Height: height, MIME: mime, Title: filepath.Base(header.Filename)}
	if len(asset.Title) > 200 {
		asset.Title = "已上传媒体"
	}
	digest := sha256.Sum256(data)
	operation, already, err := svc.ProjectNotePrepareMedia(c.Request.Context(), scope, projectID, asset, fmt.Sprintf("%x", digest), "upload")
	if err != nil {
		trackingResponse(c, nil, err)
		return
	}
	if !already {
		if err = storeContentMedia(c.Request.Context(), svc, mediaID, data, mime); err != nil {
			importResponse(c, nil, err)
			return
		}
	}
	media, err := svc.ProjectNoteFinalizeMedia(c.Request.Context(), scope, projectID, mediaID, operation)
	trackingResponse(c, media, err)
}

func projectNotePrivateMediaURLs(view tracking.ProjectNoteShareView, token string) tracking.ProjectNoteShareView {
	mediaURL := func(asset tracking.Asset) tracking.Asset {
		asset.URL = fmt.Sprintf("/api/v1/miniprogram/project-notes/shared/%s/media/%s", url.PathEscape(token), url.PathEscape(asset.ID))
		return asset
	}
	if view.Version.Cover != nil {
		cover := mediaURL(*view.Version.Cover)
		view.Version.Cover = &cover
	}
	for index := range view.Version.Media {
		view.Version.Media[index] = mediaURL(view.Version.Media[index])
	}
	return view
}
