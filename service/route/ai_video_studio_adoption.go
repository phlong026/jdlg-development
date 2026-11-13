package route

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

const studioAdoptionMaxBytes = int64(50 << 20)

type studioContentAdoptionRequest struct {
	DraftInstanceID string `json:"draft_instance_id" binding:"required"`
	OwnerID         string `json:"owner_id" binding:"required"`
	Title           string `json:"title" binding:"required"`
}

type studioProjectNoteAdoptionRequest struct {
	DraftInstanceID  string `json:"draft_instance_id" binding:"required"`
	ProjectID        string `json:"project_id" binding:"required"`
	NoteID           string `json:"note_id" binding:"required"`
	ExpectedRevision int    `json:"expected_revision" binding:"required"`
	InsertAt         *int   `json:"insert_at" binding:"required"`
}

func studioAdoptionMediaID(channel string, actor int64, draftInstanceID, sourceMediaID string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("ai-video-studio-adoption:%s:%d:%s:%s", channel, actor, draftInstanceID, sourceMediaID)))
	return hex.EncodeToString(digest[:16])
}

func validStudioAdoptionIdentity(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == ':') {
			return false
		}
	}
	return true
}

func scopeAllowsOwner(scope tracking.Scope, owner string) bool {
	for _, allowed := range scope.Owners {
		if owner == allowed {
			return true
		}
	}
	return false
}

func loadStudioAdoptionMedia(ctx context.Context, svc *tracking.Service, source *model.AIVideoStudioMedia, destinationID, destinationURL string) (tracking.Asset, []byte, error) {
	if source == nil || source.Kind != "video" || source.MIME != "video/mp4" || source.Width < 1 || source.Height < 1 || source.SizeBytes < 1 || source.SizeBytes > studioAdoptionMaxBytes || !validStoredMediaID(source.StorageKey) || !validStoredMediaID(destinationID) {
		return tracking.Asset{}, nil, errors.New("只能使用已留存的MP4视频")
	}
	asset := tracking.Asset{ID: destinationID, Kind: "video", MIME: "video/mp4", URL: destinationURL, Width: source.Width, Height: source.Height, Title: strings.TrimSpace(source.Name)}
	data, found, err := readContentMedia(ctx, svc, source.StorageKey, studioAdoptionMaxBytes)
	if err != nil || !found {
		return tracking.Asset{}, nil, errors.New("AI视频原文件不可用")
	}
	if int64(len(data)) != source.SizeBytes {
		return tracking.Asset{}, nil, errors.New("AI视频原文件完整性校验失败")
	}
	digest := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), source.SHA256) {
		return tracking.Asset{}, nil, errors.New("AI视频原文件完整性校验失败")
	}
	if len([]rune(asset.Title)) > 200 {
		asset.Title = "AI视频"
	}
	return asset, data, nil
}

func copyStudioAdoptionMedia(ctx context.Context, svc *tracking.Service, source *model.AIVideoStudioMedia, destinationID, destinationURL string) (tracking.Asset, error) {
	asset, data, err := loadStudioAdoptionMedia(ctx, svc, source, destinationID, destinationURL)
	if err != nil {
		return tracking.Asset{}, err
	}
	if err = storeAndVerifyStudioAdoptionMedia(ctx, svc, destinationID, data, asset.MIME); err != nil {
		return tracking.Asset{}, err
	}
	return asset, nil
}

func storeAndVerifyStudioAdoptionMedia(ctx context.Context, svc *tracking.Service, id string, data []byte, mime string) error {
	if err := storeContentMedia(ctx, svc, id, data, mime); err != nil {
		return err
	}
	stored, found, err := readContentMedia(ctx, svc, id, studioAdoptionMaxBytes)
	if err != nil || !found || len(stored) != len(data) || sha256.Sum256(stored) != sha256.Sum256(data) {
		return errors.New("回填视频复制完整性校验失败")
	}
	return nil
}

func registerAIVideoStudioAdoptionRoutes(g *gin.RouterGroup, media *model.AIVideoStudioMediaStore, svc *tracking.Service, users *model.UserModel) {
	g.POST("/media/:id/adopt/content", func(c *gin.Context) {
		var input studioContentAdoptionRequest
		if c.ShouldBindJSON(&input) != nil {
			studioError(c, http.StatusBadRequest, "内容草稿回填参数无效")
			return
		}
		userID := GetTokenUserID(c)
		source, err := media.Get(c.Request.Context(), userID, c.Param("id"))
		if err != nil {
			studioError(c, http.StatusNotFound, "已留存视频不可访问")
			return
		}
		scope, err := loadMiniTrackingMaterialScope(c.Request.Context(), users.DB, svc.Config.Tenant, userID)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		if !validStudioAdoptionIdentity(input.DraftInstanceID) || !scopeAllowsOwner(scope, input.OwnerID) || strings.TrimSpace(input.Title) == "" || len([]rune(input.Title)) > 80 {
			studioError(c, http.StatusBadRequest, "内容草稿身份、标题或归属无效")
			return
		}
		mediaID := studioAdoptionMediaID("content", userID, input.DraftInstanceID, source.ID)
		asset, err := copyStudioAdoptionMedia(c.Request.Context(), svc, source, mediaID, "/api/v1/miniprogram/tracking/media/"+mediaID)
		if err != nil {
			studioError(c, http.StatusConflict, err.Error())
			return
		}
		out, err := svc.AdoptAIVideoStudioContent(c.Request.Context(), scope, tracking.AIVideoStudioContentAdoption{
			DraftInstanceID: input.DraftInstanceID, SourceMediaID: source.ID, SourceTaskID: source.SourceTaskID,
			Owner: input.OwnerID, Title: input.Title, Asset: asset,
		})
		trackingResponse(c, out, err)
	})

	g.POST("/media/:id/adopt/project-note", func(c *gin.Context) {
		var input studioProjectNoteAdoptionRequest
		if c.ShouldBindJSON(&input) != nil || input.InsertAt == nil {
			studioError(c, http.StatusBadRequest, "项目笔记回填参数无效")
			return
		}
		userID := GetTokenUserID(c)
		source, err := media.Get(c.Request.Context(), userID, c.Param("id"))
		if err != nil {
			studioError(c, http.StatusNotFound, "已留存视频不可访问")
			return
		}
		scope, err := loadMiniTrackingMaterialScope(c.Request.Context(), users.DB, svc.Config.Tenant, userID)
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		if !validStudioAdoptionIdentity(input.DraftInstanceID) || !validStudioAdoptionIdentity(input.ProjectID) || !validStudioAdoptionIdentity(input.NoteID) || input.ExpectedRevision < 1 || *input.InsertAt < 0 {
			studioError(c, http.StatusBadRequest, "项目笔记身份、修订或插入位置无效")
			return
		}
		mediaID := studioAdoptionMediaID("project:"+input.ProjectID, userID, input.DraftInstanceID, source.ID)
		mediaURL := fmt.Sprintf("/api/v1/miniprogram/project-notes/projects/%s/media/%s", input.ProjectID, mediaID)
		asset, data, err := loadStudioAdoptionMedia(c.Request.Context(), svc, source, mediaID, mediaURL)
		if err != nil {
			studioError(c, http.StatusConflict, err.Error())
			return
		}
		operation, already, err := svc.ProjectNotePrepareMedia(c.Request.Context(), scope, input.ProjectID, asset, source.SHA256, "ai_video_studio")
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		if !already {
			if err = storeAndVerifyStudioAdoptionMedia(c.Request.Context(), svc, mediaID, data, asset.MIME); err != nil {
				studioError(c, http.StatusServiceUnavailable, "项目视频复制失败，可稍后重试")
				return
			}
		}
		if _, err = svc.ProjectNoteFinalizeMedia(c.Request.Context(), scope, input.ProjectID, mediaID, operation); err != nil {
			trackingResponse(c, nil, err)
			return
		}
		out, err := svc.AdoptAIVideoStudioProjectNote(c.Request.Context(), scope, tracking.AIVideoStudioProjectNoteAdoption{
			DraftInstanceID: input.DraftInstanceID, SourceMediaID: source.ID, SourceTaskID: source.SourceTaskID,
			ProjectID: input.ProjectID, NoteID: input.NoteID, ExpectedRevision: input.ExpectedRevision,
			InsertAt: *input.InsertAt, MediaID: mediaID,
		})
		trackingResponse(c, out, err)
	})
}
