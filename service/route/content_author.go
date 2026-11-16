package route

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

func historicalPersonalContentCreator(owner string) int64 {
	const prefix = "designer-"
	if !strings.HasPrefix(owner, prefix) {
		return 0
	}
	raw := strings.TrimPrefix(owner, prefix)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || owner != prefix+strconv.FormatInt(id, 10) {
		return 0
	}
	return id
}

func presentContentAuthors(ctx context.Context, contents []tracking.Content, resolve projectNoteProfileResolver) ([]tracking.Content, error) {
	out := make([]tracking.Content, len(contents))
	copy(out, contents)
	ids := make([]int64, 0, len(out))
	seen := make(map[int64]bool, len(out))
	for _, content := range out {
		id := content.CreatorID
		if id == 0 {
			id = historicalPersonalContentCreator(content.Owner)
		}
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || resolve == nil {
		return out, nil
	}
	profiles, err := resolve(ctx, ids)
	if err != nil {
		log.Printf("[content-author] warn operation=resolve_profiles author_count=%d error_type=%T", len(ids), err)
		return out, nil
	}
	for i := range out {
		id := out[i].CreatorID
		if id == 0 {
			id = historicalPersonalContentCreator(out[i].Owner)
		}
		profile, ok := profiles[id]
		name := strings.TrimSpace(profile.Nickname)
		if !ok || name == "" {
			continue
		}
		out[i].Author = &tracking.ContentAuthor{DisplayName: name, Avatar: sanitizePublicImageURL(profile.Avatar)}
	}
	return out, nil
}

func registerPublicContentReadHandler(group *gin.RouterGroup, svc *tracking.Service, profiles projectNoteProfileResolver) {
	group.GET("/contents/:id/versions/:version", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		version, err := strconv.Atoi(c.Param("version"))
		if err != nil || version < 1 {
			trackingResponse(c, nil, &tracking.Error{Status: 400, Message: "素材版本无效"})
			return
		}
		content, err := svc.ReadContent(c.Request.Context(), c.Param("id"), version)
		if err == nil {
			content, err = presentContentAuthor(c.Request.Context(), content, profiles)
		}
		trackingResponse(c, content, err)
	})
}

func presentContentAuthor(ctx context.Context, content tracking.Content, resolve projectNoteProfileResolver) (tracking.Content, error) {
	contents, err := presentContentAuthors(ctx, []tracking.Content{content}, resolve)
	if err != nil {
		return tracking.Content{}, err
	}
	return contents[0], nil
}

func presentContentList(ctx context.Context, list tracking.List, resolve projectNoteProfileResolver) (tracking.List, error) {
	contents, ok := list.Items.([]tracking.Content)
	if !ok {
		return list, tracking.ErrUnavailable
	}
	presented, err := presentContentAuthors(ctx, contents, resolve)
	if err != nil {
		return list, err
	}
	list.Items = presented
	return list, nil
}
