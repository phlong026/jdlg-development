package route

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
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
	actor              func(*gin.Context) int64
	verifyMember       func(context.Context, int64) error
	profiles           projectNoteProfileResolver
	verifyCustomerLink func(context.Context, tracking.Scope, string) error
}

// P4（2026-11-10 用户定稿）：私有笔记素材上限图片 20 MiB、视频 500 MiB。
// 请求体上限按视频上限 + 1 MiB 计；超时与流式落盘由 T5.1 负责。
const (
	projectNoteImageMaxBytes      = 20 << 20
	projectNoteVideoMaxBytes      = 500 << 20
	projectNoteUploadBodyMaxBytes = 501 << 20
)

func projectNotesEnabled() bool { return os.Getenv("PROJECT_NOTES_ENABLED") == "true" }

func projectNotePrivateMiddlewares(users *model.UserModel, codes *model.CodeSessionRedisModel) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		trackingEntryRateLimit(),
		func(c *gin.Context) {
			// Authentication verifies multipart requests before the handler. Bound
			// the stream first so an unauthenticated upload cannot grow unchecked.
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, projectNoteUploadBodyMaxBytes)
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
	}
}

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

func verifyProjectPlaybackActor(ctx context.Context, db *sql.DB, account int64, _ sharedVideoKind) error {
	if err := verifyActiveMiniProgramAccount(ctx, db, account); err != nil {
		return err
	}
	profile, err := miniprogramProfileLoaderFromModel(model.NewUserProfileModel(db))(ctx, account)
	if err != nil {
		return tracking.ErrUnavailable
	}
	if missing := miniprogramProfileMissingFields(profile); len(missing) > 0 {
		return &tracking.Error{Status: http.StatusPreconditionRequired, Message: "请先补充头像和昵称"}
	}
	return nil
}

func registerProjectNoteRoutes(v1 *gin.RouterGroup, svc *tracking.Service, users *model.UserModel, codes *model.CodeSessionRedisModel) {
	registerSharedVideoPlaybackRoutes(v1, svc, func(ctx context.Context, actor int64, kind sharedVideoKind) error {
		return verifyProjectPlaybackActor(ctx, users.DB, actor, kind)
	})
	root := v1.Group("/miniprogram/project-notes", projectNotePrivateMiddlewares(users, codes)...)
	packageRoot := v1.Group("/miniprogram/project-packages", projectNotePrivateMiddlewares(users, codes)...)
	manage := root.Group("", func(c *gin.Context) {
		scope, err := loadMiniTrackingMaterialScope(c.Request.Context(), users.DB, svc.Config.Tenant, GetTokenUserID(c))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		c.Set(projectNoteScopeKey, scope)
		c.Next()
	})
	actor := func(c *gin.Context) int64 { return GetTokenUserID(c) }
	registerProjectNoteHandlers(root, manage, svc, projectNoteHandlerDeps{
		actor:              actor,
		profiles:           makeProjectNoteProfileResolver(users.DB),
		verifyCustomerLink: makeProjectCustomerLinkVerifier(users.DB, svc),
		verifyMember: func(ctx context.Context, account int64) error {
			return verifyActiveMiniProgramAccount(ctx, users.DB, account)
		},
	})
	registerProjectPackageHandlers(manage, packageRoot, svc, actor)
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
	registerProjectNoteExportHandlers(root, manage, svc, deps)
	registerProjectNoteLifecycleHandlers(manage, svc)
	registerProjectNoteHistoryHandlers(root, manage, svc, deps)
	registerProjectInvitationHandlers(root, manage, svc, deps)
	manage.GET("/projects", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		out, err := svc.ProjectNoteListProjects(c.Request.Context(), projectNoteScope(c), limit, c.Query("cursor"), c.Query("status"))
		if err == nil {
			presented, presentationErr := presentProjectNoteProjects(c.Request.Context(), out.Items, deps.profiles)
			if presentationErr != nil {
				log.Printf("[project-notes] error operation=list_creator_profiles error_type=%T", presentationErr)
				out.Items, _ = presentProjectNoteProjects(c.Request.Context(), out.Items, nil)
			} else {
				out.Items = presented
			}
		}
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
		if input.CustomerRef != "" {
			if deps.verifyCustomerLink == nil {
				trackingResponse(c, nil, &tracking.Error{Status: http.StatusForbidden, Message: "客户关联验证暂不可用"})
				return
			}
			if err := deps.verifyCustomerLink(c.Request.Context(), projectNoteScope(c), input.CustomerRef); err != nil {
				trackingResponse(c, nil, err)
				return
			}
		}
		out, err := svc.ProjectNoteCreateProject(c.Request.Context(), projectNoteScope(c), input)
		if err == nil {
			presented, presentationErr := presentProjectNoteProjects(c.Request.Context(), []tracking.ProjectNoteProject{out}, deps.profiles)
			if presentationErr != nil {
				log.Printf("[project-notes] error operation=create_creator_profile project_id=%s error_type=%T", out.ID, presentationErr)
				out.CreatorDisplayName = "项目创建人"
			} else {
				out = presented[0]
			}
		}
		trackingResponse(c, out, err)
	})
	manage.GET("/projects/:id", func(c *gin.Context) {
		detail, err := svc.ProjectNoteGetProjectDetail(c.Request.Context(), projectNoteScope(c), c.Param("id"))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		out, err := presentProjectNoteProjectDetail(c.Request.Context(), detail, deps.profiles)
		if err != nil {
			log.Printf("[project-notes] error operation=detail_profiles project_id=%s error_type=%T", detail.Project.ID, err)
			err = tracking.ErrUnavailable
		}
		trackingResponse(c, out, err)
	})
	manage.PATCH("/projects/:id", func(c *gin.Context) {
		var input tracking.ProjectNoteProjectUpdate
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteUpdateProject(c.Request.Context(), projectNoteScope(c), c.Param("id"), input)
		if err == nil {
			presented, presentationErr := presentProjectNoteProjects(c.Request.Context(), []tracking.ProjectNoteProject{out}, deps.profiles)
			if presentationErr != nil {
				log.Printf("[project-notes] error operation=update_creator_profile project_id=%s error_type=%T", out.ID, presentationErr)
				out.CreatorDisplayName = "项目创建人"
			} else {
				out = presented[0]
			}
		}
		trackingResponse(c, out, err)
	})
	manage.POST("/projects/:id/members/:account", func(c *gin.Context) {
		account, ok := projectNoteAccount(c, c.Param("account"))
		if !ok {
			return
		}
		if deps.verifyMember == nil {
			trackingResponse(c, nil, tracking.ErrUnavailable)
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
			EventID      string `json:"event_id"`
			Acknowledged bool   `json:"acknowledged"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		if !input.Acknowledged {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "请主动确认后再告知设计师"})
			return
		}
		out, err := svc.ProjectNoteOpenShare(c.Request.Context(), c.Param("token"), deps.actor(c), input.EventID)
		trackingResponse(c, out, err)
	})
	root.POST("/shared/:token/feedback", func(c *gin.Context) {
		var input struct {
			Text           string `json:"text"`
			Satisfaction   string `json:"satisfaction"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteAddFeedbackWithSatisfaction(c.Request.Context(), c.Param("token"), deps.actor(c), input.Text, input.Satisfaction, input.IdempotencyKey)
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
	root.POST("/shared/:token/media/:id/playback", func(c *gin.Context) {
		asset, err := svc.ProjectNoteReadShareMedia(c.Request.Context(), c.Param("token"), deps.actor(c), c.Param("id"))
		if err == nil {
			err = requireSharedVideo(asset)
		}
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		sharedVideoPlaybackResponse(c, sharedVideoPlaybackGrant{Kind: sharedVideoNote, ShareToken: c.Param("token"), Actor: deps.actor(c), AssetID: asset.ID})
	})
}

func uploadProjectNoteMedia(c *gin.Context, svc *tracking.Service, scope tracking.Scope, projectID string) {
	if !contentMediaConfigured(svc) {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusServiceUnavailable, Message: "私人项目媒体存储尚未配置"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, projectNoteUploadBodyMaxBytes)
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
	kind, max := "image", int64(projectNoteImageMaxBytes)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	case "video/mp4":
		kind, max = "video", projectNoteVideoMaxBytes
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
	var data []byte
	var digestHex string
	var videoPath string
	var videoSize int64
	width, height := 0, 0
	if kind == "video" {
		// D4/D13：500 MiB 视频流式落盘 + 边写边算摘要，避免整文件驻留内存。
		tmp, createErr := os.CreateTemp("", "project-note-video-*")
		if createErr != nil {
			trackingResponse(c, nil, tracking.ErrUnavailable)
			return
		}
		videoPath = tmp.Name()
		defer os.Remove(videoPath)
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(source, max+1))
		closeErr := tmp.Close()
		if copyErr != nil || closeErr != nil || written <= 0 || written > max {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusRequestEntityTooLarge, Message: "上传失败或超出限制"})
			return
		}
		videoSize = written
		file, openErr := os.Open(videoPath)
		if openErr != nil {
			trackingResponse(c, nil, tracking.ErrUnavailable)
			return
		}
		width, height, err = trackingMP4DimensionsAt(file, written)
		file.Close()
		if err != nil {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "无法识别MP4尺寸，请选择标准MP4原文件"})
			return
		}
		digestHex = hex.EncodeToString(hash.Sum(nil))
	} else {
		data, err = io.ReadAll(io.LimitReader(source, max+1))
		if err != nil || int64(len(data)) > max || len(data) == 0 {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusRequestEntityTooLarge, Message: "上传失败或超出限制"})
			return
		}
		config, _, decodeErr := image.DecodeConfig(bytes.NewReader(data))
		if decodeErr != nil || config.Width < 1 || config.Height < 1 || config.Width > 20000 || config.Height > 20000 {
			trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "图片尺寸无效"})
			return
		}
		width, height = config.Width, config.Height
		sum := sha256.Sum256(data)
		digestHex = hex.EncodeToString(sum[:])
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
	operation, already, err := svc.ProjectNotePrepareMedia(c.Request.Context(), scope, projectID, asset, digestHex, "upload")
	if err != nil {
		trackingResponse(c, nil, err)
		return
	}
	if !already {
		if kind == "video" {
			err = storeContentMediaFile(c.Request.Context(), svc, mediaID, videoPath, videoSize, mime, digestHex)
		} else {
			err = storeContentMedia(c.Request.Context(), svc, mediaID, data, mime)
		}
		if err != nil {
			// D26：写存储失败时保留 pending journal 作为可重试证据（同一摘要重试会复用 operation）。
			importResponse(c, nil, err)
			return
		}
	}
	media, err := svc.ProjectNoteFinalizeMedia(c.Request.Context(), scope, projectID, mediaID, operation)
	if err != nil {
		// D26：完成失败同样保留 pending；用同一摘要重试上传会复用 operation 并完成收尾。
	}
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
