package route

import (
	"context"
	"strings"
	"time"

	"service/model"
	"service/tracking"

	"github.com/gin-gonic/gin"
)

type projectJoinRequestPresentation struct {
	tracking.ProjectNoteJoinRequest
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar,omitempty"`
}

func presentProjectJoinRequests(ctx context.Context, requests []tracking.ProjectNoteJoinRequest, resolve projectNoteProfileResolver) ([]projectJoinRequestPresentation, error) {
	ids := make([]int64, 0, len(requests))
	seen := map[int64]bool{}
	for _, request := range requests {
		if request.ActorID > 0 && !seen[request.ActorID] {
			seen[request.ActorID] = true
			ids = append(ids, request.ActorID)
		}
	}
	profiles := map[int64]model.UserProfilePresentation{}
	if resolve != nil && len(ids) > 0 {
		var err error
		profiles, err = resolve(ctx, ids)
		if err != nil {
			return nil, err
		}
	}
	out := make([]projectJoinRequestPresentation, 0, len(requests))
	for _, request := range requests {
		profile := profiles[request.ActorID]
		name := strings.TrimSpace(profile.Nickname)
		if name == "" {
			name = "项目申请人"
		}
		out = append(out, projectJoinRequestPresentation{
			ProjectNoteJoinRequest: request,
			DisplayName:            name,
			Avatar:                 sanitizePublicImageURL(profile.Avatar),
		})
	}
	return out, nil
}

func registerProjectInvitationHandlers(root, manage *gin.RouterGroup, svc *tracking.Service, deps projectNoteHandlerDeps) {
	registerProjectMemberReader(root, svc, deps)
	manage.GET("/projects/:id/member-invitations", func(c *gin.Context) {
		items, err := svc.ProjectNoteListInvitations(c.Request.Context(), projectNoteScope(c), c.Param("id"))
		trackingResponse(c, gin.H{"items": items}, err)
	})
	manage.POST("/projects/:id/member-invitations", func(c *gin.Context) {
		var input struct {
			ExpiresAt      time.Time `json:"expires_at"`
			IdempotencyKey string    `json:"idempotency_key"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteCreateInvitation(c.Request.Context(), projectNoteScope(c), c.Param("id"), input.ExpiresAt, input.IdempotencyKey)
		trackingResponse(c, out, err)
	})
	manage.DELETE("/projects/:id/member-invitations/:invitation", func(c *gin.Context) {
		err := svc.ProjectNoteRevokeInvitation(c.Request.Context(), projectNoteScope(c), c.Param("id"), c.Param("invitation"))
		trackingResponse(c, gin.H{"revoked": err == nil}, err)
	})
	root.GET("/member-invitations/:token", func(c *gin.Context) {
		out, err := svc.ProjectNoteReadInvitation(c.Request.Context(), c.Param("token"), deps.actor(c))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		inviterName := ""
		if deps.profiles != nil && out.DesignerID > 0 {
			profiles, resolveErr := deps.profiles(c.Request.Context(), []int64{out.DesignerID})
			if resolveErr != nil {
				trackingResponse(c, nil, resolveErr)
				return
			}
			inviterName = projectNoteCreatorDisplayName(profiles[out.DesignerID])
		}
		preview := gin.H{
			"project_name":   out.ProjectName,
			"expires_at":     out.ExpiresAt,
			"request_status": out.Status,
			"inviter_name":   inviterName,
		}
		if out.Status == tracking.ProjectNoteJoinApproved {
			preview["project_id"] = out.ProjectID
		}
		trackingResponse(c, preview, nil)
	})
	root.POST("/member-invitations/:token/apply", func(c *gin.Context) {
		var input struct {
			IdempotencyKey string `json:"idempotency_key"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		out, err := svc.ProjectNoteApplyInvitation(c.Request.Context(), c.Param("token"), deps.actor(c), input.IdempotencyKey)
		trackingResponse(c, out, err)
	})
	manage.GET("/projects/:id/member-requests", func(c *gin.Context) {
		requests, err := svc.ProjectNoteListJoinRequests(c.Request.Context(), projectNoteScope(c), c.Param("id"))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		out, err := presentProjectJoinRequests(c.Request.Context(), requests, deps.profiles)
		trackingResponse(c, gin.H{"items": out}, err)
	})
	manage.POST("/projects/:id/member-requests/:request/approve", func(c *gin.Context) {
		var input struct {
			IdempotencyKey string `json:"idempotency_key"`
		}
		if !projectNoteBind(c, &input) {
			return
		}
		request, err := svc.ProjectNoteGetJoinRequest(c.Request.Context(), projectNoteScope(c), c.Param("id"), c.Param("request"))
		if err != nil {
			trackingResponse(c, nil, err)
			return
		}
		if deps.verifyMember == nil {
			trackingResponse(c, nil, tracking.ErrUnavailable)
			return
		}
		if err = deps.verifyMember(c.Request.Context(), request.ActorID); err != nil {
			trackingResponse(c, nil, err)
			return
		}
		out, err := svc.ProjectNoteApproveJoinRequest(c.Request.Context(), projectNoteScope(c), c.Param("id"), c.Param("request"), input.IdempotencyKey)
		trackingResponse(c, out, err)
	})
}
