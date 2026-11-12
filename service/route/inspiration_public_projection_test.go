package route

import (
	"testing"

	"service/model"
)

func TestInspirationPublicResponseOmitsInternalPublishingFields(t *testing.T) {
	response := inspirationToResponse(&model.InspirationAsset{
		ID:            9,
		Title:         "公开灵感",
		Status:        "published",
		SortOrder:     99,
		Creator:       "公开作者",
		CreatorUserID: 42,
	})

	if response["title"] != "公开灵感" || response["creator"] != "公开作者" {
		t.Fatalf("public inspiration response = %#v", response)
	}
	for _, field := range []string{"status", "sort_order", "creator_user_id"} {
		if _, exists := response[field]; exists {
			t.Fatalf("internal field %s leaked: %#v", field, response)
		}
	}
}
