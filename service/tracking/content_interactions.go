package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

const (
	contentCommentKind  = "content_comment"
	contentLikeKind     = "content_like"
	contentFavoriteKind = "content_favorite"
)

type ContentComment struct {
	ID        string    `json:"comment_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	Mine      bool      `json:"mine"`
	ActorID   int64     `json:"-"`
}

type ContentInteractions struct {
	LikeCount     int              `json:"like_count"`
	FavoriteCount int              `json:"favorite_count"`
	CommentCount  int              `json:"comment_count"`
	Liked         bool             `json:"liked"`
	Favorited     bool             `json:"favorited"`
	Comments      []ContentComment `json:"comments"`
	NextCursor    string           `json:"next_cursor,omitempty"`
}

type contentReaction struct {
	ActorID int64  `json:"actor_id"`
	Target  string `json:"target"`
}

type storedContentComment struct {
	ActorID   int64      `json:"actor_id"`
	Target    string     `json:"target"`
	Text      string     `json:"text"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

func contentInteractionTarget(id string, version int) (string, error) {
	if !validID(id) || version <= 0 {
		return "", fail(404, "内容不可访问")
	}
	return hash(contentKey(id, version)), nil
}

func (s *Service) interactionContent(ctx context.Context, r Repository, id string, version int) (Content, string, error) {
	var empty Content
	target, err := contentInteractionTarget(id, version)
	if err != nil {
		return empty, "", err
	}
	if _, err = r.Get("content_revoked", id); err == nil {
		return empty, "", fail(404, "内容不可访问")
	} else if !errors.Is(err, ErrNotFound) {
		return empty, "", err
	}
	if err = s.available(ctx, r, publishedReaderRoute, id, version); err != nil {
		return empty, "", err
	}
	master, err := decode[Content](r, "content", id)
	if err != nil {
		return empty, "", err
	}
	snapshot, err := decode[Content](r, "version", contentKey(id, version))
	if err != nil {
		return empty, "", err
	}
	if master.ID != id || snapshot.ID != id || snapshot.Version != version || master.Owner == "" || snapshot.Owner != master.Owner {
		return empty, "", fail(404, "内容不可访问")
	}
	return snapshot, target, nil
}

func reactionID(kind, target string, actor int64) string {
	return hash(fmt.Sprintf("%s:%s:%d", kind, target, actor))
}

func contentInteractionRecord(kind, id, owner, target string, actor int64, at time.Time, payload any) Record {
	row := record(kind, id, owner, at, payload)
	row.Content = target
	row.Visitor = "u:" + strconv.FormatInt(actor, 10)
	return row
}

func duplicateContentReaction(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func validateContentReaction(r Repository, kind, key, owner, target string, actor int64) error {
	row, err := r.Get(kind, key)
	if err != nil {
		return err
	}
	_, selected, err := validateReactionRows([]Record{row}, kind, owner, target, actor)
	if err != nil {
		return err
	}
	if !selected {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) setContentReaction(ctx context.Context, id string, version int, actor int64, kind string, active bool) (int, error) {
	if actor <= 0 {
		return 0, fail(401, "未登录")
	}
	count := 0
	err := s.transaction(ctx, func(r Repository) error {
		content, target, err := s.interactionContent(ctx, r, id, version)
		if err != nil {
			return err
		}
		key := reactionID(kind, target, actor)
		row, err := r.Get(kind, key)
		switch {
		case active && errors.Is(err, ErrNotFound):
			reaction := contentReaction{ActorID: actor, Target: target}
			if err = r.Insert(contentInteractionRecord(kind, key, content.Owner, target, actor, s.Now(), reaction)); err != nil {
				// The deterministic key makes repeated writes idempotent. A concurrent
				// request can win after Get and before Insert, so accept only a MySQL
				// duplicate whose committed row still matches this actor and version.
				if !duplicateContentReaction(err) {
					return err
				}
				if err = validateContentReaction(r, kind, key, content.Owner, target, actor); err != nil {
					return err
				}
			}
		case active && err != nil:
			return err
		case active:
			if err = validateContentReaction(r, kind, key, content.Owner, target, actor); err != nil {
				return err
			}
		case !active && errors.Is(err, ErrNotFound):
			// Explicit false is idempotent.
		case !active && err != nil:
			return err
		default:
			if _, selected, validateErr := validateReactionRows([]Record{row}, kind, content.Owner, target, actor); validateErr != nil || !selected {
				if validateErr != nil {
					return validateErr
				}
				return ErrUnavailable
			}
			if err = r.Remove(kind, key); err != nil {
				return err
			}
		}
		rows, err := scopedRecords(r, kind, RecordQuery{Content: target})
		if err != nil {
			return err
		}
		count, _, err = validateReactionRows(rows, kind, content.Owner, target, actor)
		return err
	})
	return count, err
}

func (s *Service) SetContentLike(ctx context.Context, id string, version int, actor int64, active bool) (int, error) {
	return s.setContentReaction(ctx, id, version, actor, contentLikeKind, active)
}

func (s *Service) SetContentFavorite(ctx context.Context, id string, version int, actor int64, active bool) (int, error) {
	return s.setContentReaction(ctx, id, version, actor, contentFavoriteKind, active)
}

func (s *Service) AddContentComment(ctx context.Context, id string, version int, actor int64, text string) (ContentComment, error) {
	var out ContentComment
	if actor <= 0 {
		return out, fail(401, "未登录")
	}
	text = strings.TrimSpace(text)
	if count := utf8.RuneCountInString(text); count < 1 || count > 500 {
		return out, fail(400, "评论需为1至500个字")
	}
	err := s.transaction(ctx, func(r Repository) error {
		content, target, err := s.interactionContent(ctx, r, id, version)
		if err != nil {
			return err
		}
		now := s.Now()
		out = ContentComment{ID: opaque(), Text: text, CreatedAt: now, Mine: true, ActorID: actor}
		stored := storedContentComment{ActorID: actor, Target: target, Text: text}
		return r.Insert(contentInteractionRecord(contentCommentKind, out.ID, content.Owner, target, actor, now, stored))
	})
	return out, err
}

func (s *Service) DeleteContentComment(ctx context.Context, id string, version int, actor int64, commentID string) error {
	if actor <= 0 {
		return fail(401, "未登录")
	}
	if !validID(commentID) {
		return fail(404, "评论不存在")
	}
	return s.transaction(ctx, func(r Repository) error {
		content, target, err := s.interactionContent(ctx, r, id, version)
		if err != nil {
			return err
		}
		row, err := r.Get(contentCommentKind, commentID)
		if errors.Is(err, ErrNotFound) {
			return fail(404, "评论不存在")
		}
		if err != nil {
			return err
		}
		var comment storedContentComment
		if json.Unmarshal(row.Payload, &comment) != nil || !validStoredContentComment(row, comment, content.Owner, target) {
			return fail(404, "评论不存在")
		}
		if comment.ActorID != actor {
			return fail(403, "只能删除自己的评论")
		}
		if comment.DeletedAt != nil {
			return nil
		}
		now := s.Now()
		comment.DeletedAt = &now
		row.Payload = marshal(comment)
		return r.Put(row)
	})
}

func validateReactionRows(rows []Record, kind, owner, target string, actor int64) (count int, selected bool, err error) {
	for _, row := range rows {
		var reaction contentReaction
		if json.Unmarshal(row.Payload, &reaction) != nil || reaction.ActorID <= 0 || reaction.Target != target || row.Kind != kind || row.Content != target || row.Owner != owner || row.Visitor != "u:"+strconv.FormatInt(reaction.ActorID, 10) || row.ID != reactionID(kind, target, reaction.ActorID) {
			return 0, false, ErrUnavailable
		}
		count++
		if actor > 0 && reaction.ActorID == actor {
			selected = true
		}
	}
	return count, selected, nil
}

func (s *Service) ContentInteractions(ctx context.Context, id string, version int, actor int64, cursor string, limit int) (ContentInteractions, error) {
	out := ContentInteractions{Comments: []ContentComment{}}
	if cursor != "" && !validID(cursor) {
		return out, fail(400, "分页标识无效")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		content, target, err := s.interactionContent(ctx, r, id, version)
		if err != nil {
			return err
		}
		likes, err := scopedRecords(r, contentLikeKind, RecordQuery{Content: target})
		if err != nil {
			return err
		}
		out.LikeCount, out.Liked, err = validateReactionRows(likes, contentLikeKind, content.Owner, target, actor)
		if err != nil {
			return err
		}
		favorites, err := scopedRecords(r, contentFavoriteKind, RecordQuery{Content: target})
		if err != nil {
			return err
		}
		out.FavoriteCount, out.Favorited, err = validateReactionRows(favorites, contentFavoriteKind, content.Owner, target, actor)
		if err != nil {
			return err
		}
		rows, err := scopedRecords(r, contentCommentKind, RecordQuery{Content: target})
		if err != nil {
			return err
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].At.Equal(rows[j].At) {
				return rows[i].ID > rows[j].ID
			}
			return rows[i].At.After(rows[j].At)
		})
		start := 0
		if cursor != "" {
			start = -1
			for i := range rows {
				if rows[i].ID == cursor {
					start = i + 1
					break
				}
			}
			if start < 0 {
				return fail(400, "分页标识无效")
			}
		}
		visible := make([]ContentComment, 0, len(rows))
		for _, row := range rows {
			var stored storedContentComment
			if json.Unmarshal(row.Payload, &stored) != nil || !validStoredContentComment(row, stored, content.Owner, target) {
				return ErrUnavailable
			}
			if stored.DeletedAt != nil {
				continue
			}
			visible = append(visible, ContentComment{ID: row.ID, Text: stored.Text, CreatedAt: row.At, Mine: actor > 0 && stored.ActorID == actor, ActorID: stored.ActorID})
		}
		out.CommentCount = len(visible)
		if start > len(rows) {
			start = len(rows)
		}
		page := make([]ContentComment, 0, limit)
		for _, row := range rows[start:] {
			var stored storedContentComment
			if json.Unmarshal(row.Payload, &stored) != nil || !validStoredContentComment(row, stored, content.Owner, target) {
				return ErrUnavailable
			}
			if stored.DeletedAt != nil {
				continue
			}
			if len(page) == limit {
				out.NextCursor = page[len(page)-1].ID
				break
			}
			page = append(page, ContentComment{ID: row.ID, Text: stored.Text, CreatedAt: row.At, Mine: actor > 0 && stored.ActorID == actor, ActorID: stored.ActorID})
		}
		out.Comments = page
		return nil
	})
	return out, err
}

func validStoredContentComment(row Record, stored storedContentComment, owner, target string) bool {
	count := utf8.RuneCountInString(stored.Text)
	return stored.ActorID > 0 && stored.Target == target && count >= 1 && count <= 500 && stored.Text == strings.TrimSpace(stored.Text) &&
		row.Kind == contentCommentKind && row.Content == target && row.Owner == owner && row.Visitor == "u:"+strconv.FormatInt(stored.ActorID, 10) && validID(row.ID)
}
