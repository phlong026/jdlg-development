package route

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

type contentInteractionHandler struct {
	service  *tracking.Service
	profiles projectNoteProfileResolver
}

type contentCommentAuthor struct {
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
}

type contentCommentPresentation struct {
	ID        string               `json:"comment_id"`
	Text      string               `json:"text"`
	CreatedAt time.Time            `json:"created_at"`
	Mine      bool                 `json:"mine"`
	Author    contentCommentAuthor `json:"author"`
}

type contentInteractionPresentation struct {
	LikeCount     int                          `json:"like_count"`
	FavoriteCount int                          `json:"favorite_count"`
	CommentCount  int                          `json:"comment_count"`
	Liked         bool                         `json:"liked"`
	Favorited     bool                         `json:"favorited"`
	Comments      []contentCommentPresentation `json:"comments"`
	NextCursor    string                       `json:"next_cursor,omitempty"`
}

func parseContentInteractionTarget(c *gin.Context) (string, int, error) {
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil || version <= 0 {
		return "", 0, &tracking.Error{Status: http.StatusNotFound, Message: "内容不可访问"}
	}
	return c.Param("id"), version, nil
}

func (h contentInteractionHandler) presentComments(ctx context.Context, comments []tracking.ContentComment) []contentCommentPresentation {
	ids := make([]int64, 0, len(comments))
	seen := map[int64]bool{}
	for _, comment := range comments {
		if comment.ActorID > 0 && !seen[comment.ActorID] {
			seen[comment.ActorID] = true
			ids = append(ids, comment.ActorID)
		}
	}
	profiles := map[int64]model.UserProfilePresentation{}
	if h.profiles != nil && len(ids) > 0 {
		var err error
		profiles, err = h.profiles(ctx, ids)
		if err != nil {
			log.Printf("[content-interactions] error operation=profile_projection error_type=%T", err)
			profiles = map[int64]model.UserProfilePresentation{}
		}
	}
	out := make([]contentCommentPresentation, 0, len(comments))
	for _, comment := range comments {
		profile := profiles[comment.ActorID]
		nickname := strings.TrimSpace(profile.Nickname)
		if nickname == "" {
			nickname = "微信用户"
		}
		out = append(out, contentCommentPresentation{
			ID: comment.ID, Text: comment.Text, CreatedAt: comment.CreatedAt, Mine: comment.Mine,
			Author: contentCommentAuthor{Nickname: nickname, Avatar: profile.Avatar},
		})
	}
	return out
}

func (h contentInteractionHandler) get(c *gin.Context) {
	id, version, err := parseContentInteractionTarget(c)
	if err != nil {
		trackingResponse(c, nil, err)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	interactions, err := h.service.ContentInteractions(c.Request.Context(), id, version, GetTokenUserID(c), c.Query("cursor"), limit)
	if err != nil {
		trackingResponse(c, nil, err)
		return
	}
	comments := h.presentComments(c.Request.Context(), interactions.Comments)
	trackingResponse(c, contentInteractionPresentation{
		LikeCount: interactions.LikeCount, FavoriteCount: interactions.FavoriteCount, CommentCount: interactions.CommentCount,
		Liked: interactions.Liked, Favorited: interactions.Favorited, Comments: comments, NextCursor: interactions.NextCursor,
	}, nil)
}

func (h contentInteractionHandler) addComment(c *gin.Context) {
	id, version, err := parseContentInteractionTarget(c)
	if err != nil {
		trackingResponse(c, nil, err)
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if !trackingBind(c, &input) {
		return
	}
	comment, err := h.service.AddContentComment(c.Request.Context(), id, version, GetTokenUserID(c), input.Text)
	if err != nil {
		trackingResponse(c, nil, err)
		return
	}
	presented := h.presentComments(c.Request.Context(), []tracking.ContentComment{comment})
	trackingResponse(c, presented[0], nil)
}

func bindContentReaction(c *gin.Context) (bool, bool) {
	var input struct {
		Active *bool `json:"active"`
	}
	if !trackingBind(c, &input) {
		return false, false
	}
	if input.Active == nil {
		trackingResponse(c, nil, &tracking.Error{Status: http.StatusBadRequest, Message: "active字段不能为空"})
		return false, false
	}
	return *input.Active, true
}

func (h contentInteractionHandler) setLike(c *gin.Context) {
	h.setReaction(c, h.service.SetContentLike)
}

func (h contentInteractionHandler) setFavorite(c *gin.Context) {
	h.setReaction(c, h.service.SetContentFavorite)
}

func (h contentInteractionHandler) setReaction(c *gin.Context, set func(context.Context, string, int, int64, bool) (int, error)) {
	id, version, err := parseContentInteractionTarget(c)
	if err != nil {
		trackingResponse(c, nil, err)
		return
	}
	active, ok := bindContentReaction(c)
	if !ok {
		return
	}
	count, err := set(c.Request.Context(), id, version, GetTokenUserID(c), active)
	trackingResponse(c, gin.H{"active": active, "count": count}, err)
}

func (h contentInteractionHandler) deleteComment(c *gin.Context) {
	id, version, err := parseContentInteractionTarget(c)
	if err == nil {
		err = h.service.DeleteContentComment(c.Request.Context(), id, version, GetTokenUserID(c), c.Param("comment_id"))
	}
	trackingResponse(c, gin.H{"deleted": err == nil}, err)
}

func registerContentInteractionRoutes(pub *gin.RouterGroup, service *tracking.Service, users *model.UserModel, codes *model.CodeSessionRedisModel, profiles projectNoteProfileResolver) {
	h := contentInteractionHandler{service: service, profiles: profiles}
	base := pub.Group("/contents/:id/versions/:version/interactions", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Header("Cache-Control", "private, no-store")
		c.Next()
		if c.Writer.Status() >= 500 {
			log.Printf("[content-interactions] error route=%s status=%d", c.FullPath(), c.Writer.Status())
		}
	})
	optionalAuth := func(c *gin.Context) {
		if c.GetHeader("token") == "" {
			c.Next()
			return
		}
		TokenAuthRequired(codes)(c)
	}
	base.GET("", optionalAuth, MiniprogramBannedBlock(users), h.get)
	authed := base.Group("", TokenAuthRequired(codes), MiniprogramBannedBlock(users))
	authed.POST("/comments", h.addComment)
	authed.POST("/like", h.setLike)
	authed.POST("/favorite", h.setFavorite)
	authed.DELETE("/comments/:comment_id", h.deleteComment)
}
