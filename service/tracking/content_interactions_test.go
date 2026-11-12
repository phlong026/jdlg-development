package tracking

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type failingCommentGetStore struct {
	Store
	err error
}

type failingCommentGetRepo struct {
	Repository
	err error
}

func (s failingCommentGetStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	return s.Store.Transaction(ctx, tenant, func(r Repository) error {
		return fn(failingCommentGetRepo{Repository: r, err: s.err})
	})
}

func (r failingCommentGetRepo) Get(kind, id string) (Record, error) {
	if kind == contentCommentKind {
		return Record{}, r.err
	}
	return r.Repository.Get(kind, id)
}

func publishedInteractionFixture(t *testing.T) (*Service, Scope, Content) {
	t.Helper()
	s, scope := fixture(t)
	ctx := context.Background()
	draft, err := s.SaveContent(ctx, scope, "", Draft{Title: "互动笔记", Body: "正文", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), "interaction-publish")
	if err != nil {
		t.Fatal(err)
	}
	return s, scope, published["content"].(Content)
}

func TestContentInteractionsAreIdempotentAndActorScoped(t *testing.T) {
	s, _, content := publishedInteractionFixture(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := s.SetContentLike(ctx, content.ID, content.Version, 11, true); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetContentFavorite(ctx, content.ID, content.Version, 11, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SetContentLike(ctx, content.ID, content.Version, 12, true); err != nil {
		t.Fatal(err)
	}

	got, err := s.ContentInteractions(ctx, content.ID, content.Version, 11, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if got.LikeCount != 2 || got.FavoriteCount != 1 || !got.Liked || !got.Favorited {
		t.Fatalf("unexpected interactions: %#v", got)
	}
	anonymous, err := s.ContentInteractions(ctx, content.ID, content.Version, 0, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if anonymous.Liked || anonymous.Favorited {
		t.Fatalf("anonymous viewer inherited account state: %#v", anonymous)
	}
	if _, err := s.SetContentLike(ctx, content.ID, content.Version, 11, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetContentLike(ctx, content.ID, content.Version, 11, false); err != nil {
		t.Fatal(err)
	}
	got, err = s.ContentInteractions(ctx, content.ID, content.Version, 11, "", 20)
	if err != nil || got.LikeCount != 1 || got.Liked {
		t.Fatalf("idempotent removal failed: %#v %v", got, err)
	}
}

func TestContentCommentsValidatePaginateAndSoftDelete(t *testing.T) {
	s, _, content := publishedInteractionFixture(t)
	ctx := context.Background()
	now := s.Now()
	s.Now = func() time.Time { return now }

	if _, err := s.AddContentComment(ctx, content.ID, content.Version, 11, " \n "); err == nil {
		t.Fatal("blank comment accepted")
	}
	tooLong := make([]rune, 501)
	for i := range tooLong {
		tooLong[i] = '好'
	}
	if _, err := s.AddContentComment(ctx, content.ID, content.Version, 11, string(tooLong)); err == nil {
		t.Fatal("overlong comment accepted")
	}

	comments := make([]ContentComment, 0, 3)
	for i, text := range []string{"第一条", "第二条", "第三条"} {
		now = now.Add(time.Second)
		comment, err := s.AddContentComment(ctx, content.ID, content.Version, int64(11+i%2), text)
		if err != nil {
			t.Fatal(err)
		}
		comments = append(comments, comment)
	}
	first, err := s.ContentInteractions(ctx, content.ID, content.Version, 11, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.CommentCount != 3 || len(first.Comments) != 2 || first.Comments[0].Text != "第三条" || first.NextCursor == "" {
		t.Fatalf("first page = %#v", first)
	}
	second, err := s.ContentInteractions(ctx, content.ID, content.Version, 11, first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Comments) != 1 || second.Comments[0].Text != "第一条" || second.NextCursor != "" {
		t.Fatalf("second page = %#v", second)
	}

	if err := s.DeleteContentComment(ctx, content.ID, content.Version, 12, comments[0].ID); err == nil {
		t.Fatal("another account deleted comment")
	}
	if err := s.DeleteContentComment(ctx, content.ID, content.Version, 11, comments[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteContentComment(ctx, content.ID, content.Version, 11, comments[0].ID); err != nil {
		t.Fatal("repeated delete must be idempotent:", err)
	}
	got, err := s.ContentInteractions(ctx, content.ID, content.Version, 11, "", 20)
	if err != nil || got.CommentCount != 2 || len(got.Comments) != 2 {
		t.Fatalf("deleted comment remained visible: %#v %v", got, err)
	}
}

func TestContentInteractionsRejectUnpublishedForeignVersionAndRevokedContent(t *testing.T) {
	s, scope, content := publishedInteractionFixture(t)
	ctx := context.Background()

	if _, err := s.SetContentLike(ctx, content.ID, content.Version+1, 11, true); err == nil {
		t.Fatal("nonexistent version accepted")
	}
	if _, err := s.SetContentFavorite(ctx, content.ID, content.Version, 0, true); err == nil {
		t.Fatal("anonymous write accepted")
	}
	if _, err := s.AddContentComment(ctx, content.ID, content.Version, 0, "匿名评论"); err == nil {
		t.Fatal("anonymous comment accepted")
	}
	if _, err := s.Revoke(ctx, scope, content.ID, content.Revision, "撤回互动测试", "interaction-revoke"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ContentInteractions(ctx, content.ID, content.Version, 11, "", 20); err == nil {
		t.Fatal("revoked content interactions remained readable")
	}
	if _, err := s.SetContentLike(ctx, content.ID, content.Version, 11, true); err == nil {
		t.Fatal("revoked content accepted a write")
	}
}

func TestContentInteractionIndexFitsSchemaForMaximumContentID(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	id := strings.Repeat("a", 128)
	now := s.Now()
	content := Content{ID: id, Version: 1, Revision: 1, Title: "最长内容标识", Status: "published", Owner: "owner-a", PublishedAt: &now}
	if err := s.Store.Transaction(ctx, s.Config.Tenant, func(r Repository) error {
		if err := r.Put(record("content", id, content.Owner, now, content)); err != nil {
			return err
		}
		return r.Put(record("version", contentKey(id, 1), content.Owner, now, content))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetContentLike(ctx, id, 1, 11, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Transaction(ctx, s.Config.Tenant, func(r Repository) error {
		rows, err := scopedRecords(r, contentLikeKind, RecordQuery{})
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Content != hash(contentKey(id, 1)) || len(rows[0].Content) != 64 {
			t.Fatalf("interaction index must be a fixed hash: %#v", rows)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteContentCommentPropagatesStorageFailure(t *testing.T) {
	s, _, content := publishedInteractionFixture(t)
	ctx := context.Background()
	comment, err := s.AddContentComment(ctx, content.ID, content.Version, 11, "保留真实存储错误")
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("comment storage unavailable")
	s.Store = failingCommentGetStore{Store: s.Store, err: want}
	if err = s.DeleteContentComment(ctx, content.ID, content.Version, 11, comment.ID); !errors.Is(err, want) {
		t.Fatalf("delete error = %v, want storage failure", err)
	}
}

func TestContentInteractionsAreIsolatedByPublishedVersion(t *testing.T) {
	s, scope, first := publishedInteractionFixture(t)
	ctx := context.Background()
	revised, err := s.SaveContent(ctx, scope, first.ID, Draft{Title: "互动笔记 V2", Body: "第二版", Rights: "own", Owner: "owner-a", Revision: first.Revision})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(ctx, scope, revised.ID, revised.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(ctx, scope, revised.ID, revised.Revision, preview["preview_token"].(string), "interaction-publish-v2")
	if err != nil {
		t.Fatal(err)
	}
	second := published["content"].(Content)
	if second.Version == first.Version {
		t.Fatal("second publish did not create a fixed version")
	}
	if _, err = s.SetContentLike(ctx, first.ID, first.Version, 11, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetContentFavorite(ctx, second.ID, second.Version, 12, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddContentComment(ctx, first.ID, first.Version, 11, "第一版评论"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddContentComment(ctx, second.ID, second.Version, 12, "第二版评论"); err != nil {
		t.Fatal(err)
	}
	v1, err := s.ContentInteractions(ctx, first.ID, first.Version, 11, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.ContentInteractions(ctx, second.ID, second.Version, 12, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if v1.LikeCount != 1 || v1.FavoriteCount != 0 || len(v1.Comments) != 1 || v1.Comments[0].Text != "第一版评论" {
		t.Fatalf("v1 interactions mixed: %#v", v1)
	}
	if v2.LikeCount != 0 || v2.FavoriteCount != 1 || len(v2.Comments) != 1 || v2.Comments[0].Text != "第二版评论" {
		t.Fatalf("v2 interactions mixed: %#v", v2)
	}
}
