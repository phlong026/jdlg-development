package route

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"service/model"
	"service/tracking"
)

func TestPresentContentAuthorsUsesServerCreatorAndFiltersAvatar(t *testing.T) {
	contents := []tracking.Content{
		{ID: "new-team-content", Owner: "company", CreatorID: 9},
		{ID: "legacy-personal", Owner: "designer-7"},
		{ID: "legacy-team", Owner: "company"},
		{ID: "malformed-personal", Owner: "designer-007"},
	}
	resolved, err := presentContentAuthors(context.Background(), contents, func(_ context.Context, ids []int64) (map[int64]model.UserProfilePresentation, error) {
		if len(ids) != 2 || ids[0] != 9 || ids[1] != 7 {
			t.Fatalf("profile ids = %#v", ids)
		}
		return map[int64]model.UserProfilePresentation{
			9: {UserID: 9, Nickname: " 李设计师 ", Avatar: "https://127.0.0.1/private-avatar.png"},
			7: {UserID: 7, Nickname: "陈设计师", Avatar: "https://cdn.example.com/avatar.png"},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Author == nil || resolved[0].Author.DisplayName != "李设计师" || resolved[0].Author.Avatar != "" {
		t.Fatalf("new author = %#v", resolved[0].Author)
	}
	if resolved[1].Author == nil || resolved[1].Author.DisplayName != "陈设计师" || resolved[1].Author.Avatar != "https://cdn.example.com/avatar.png" {
		t.Fatalf("legacy author = %#v", resolved[1].Author)
	}
	if resolved[2].Author != nil || resolved[3].Author != nil {
		t.Fatalf("untrusted historical owners gained authors: %#v %#v", resolved[2].Author, resolved[3].Author)
	}
}

func TestPresentContentAuthorsOmitsUnknownProfile(t *testing.T) {
	contents, err := presentContentAuthors(context.Background(), []tracking.Content{{ID: "content", Owner: "company", CreatorID: 9}}, func(context.Context, []int64) (map[int64]model.UserProfilePresentation, error) {
		return map[int64]model.UserProfilePresentation{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if contents[0].Author != nil {
		t.Fatalf("missing profile became author: %#v", contents[0].Author)
	}
}

func TestPresentContentAuthorsDegradesWhenOptionalResolverFails(t *testing.T) {
	input := []tracking.Content{{ID: "content", Owner: "company", CreatorID: 9}}
	contents, err := presentContentAuthors(context.Background(), input, func(context.Context, []int64) (map[int64]model.UserProfilePresentation, error) {
		return nil, errors.New("profile directory unavailable")
	})
	if err != nil {
		t.Fatalf("optional author resolver blocked content: %v", err)
	}
	if len(contents) != 1 || contents[0].ID != input[0].ID || contents[0].Author != nil {
		t.Fatalf("degraded content = %#v", contents)
	}
}

func TestPublicContentReadDegradesWhenOptionalAuthorResolverFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := tracking.New(&governanceTestStore{records: map[string]tracking.Record{}}, tracking.Config{
		Tenant: "public-author", Enabled: true, ContentEnabled: true, ConsentVersion: "v1",
		Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}},
	})
	scope := tracking.Scope{Tenant: "public-author", Actor: 9, Owners: []string{"company"}, Manage: true}
	draft, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "公开内容", Body: "正文", BodyFormat: "plain", Rights: "own", Owner: "company"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(context.Background(), scope, draft.ID, draft.Revision, preview["preview_token"].(string), "public-author-publish")
	if err != nil {
		t.Fatal(err)
	}
	version := published["content"].(tracking.Content).Version
	router := gin.New()
	group := router.Group("/tracking")
	registerPublicContentReadHandler(group, svc, func(context.Context, []int64) (map[int64]model.UserProfilePresentation, error) {
		return nil, errors.New("profile directory unavailable")
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/tracking/contents/"+draft.ID+"/versions/"+strconv.Itoa(version), nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "公开内容") || strings.Contains(recorder.Body.String(), `"author"`) {
		t.Fatalf("public degraded response = %d %s (version=%d)", recorder.Code, recorder.Body.String(), version)
	}
}
