package route

import (
	"context"
	"reflect"
	"testing"

	"service/model"
	"service/tracking"
)

func TestProjectNotePresentationUsesOneAuthorizedProfileBatch(t *testing.T) {
	var requested []int64
	detail := tracking.ProjectNoteProjectDetail{
		Project: tracking.ProjectNoteProject{DesignerID: 100},
		Members: []tracking.ProjectNoteMember{
			{ActorID: 101, Active: true},
			{ActorID: 102, Active: true},
			{ActorID: 104, Active: false},
		},
		Confirmations: []tracking.ProjectNoteConfirmation{{ConfirmerID: 101}},
	}
	got, err := presentProjectNoteProjectDetail(context.Background(), detail, func(_ context.Context, ids []int64) (map[int64]model.UserProfilePresentation, error) {
		requested = append([]int64(nil), ids...)
		return map[int64]model.UserProfilePresentation{
			100: {UserID: 100, Nickname: "  李设计师  "},
			101: {UserID: 101, Nickname: "  张女士  ", Avatar: "https://public.example/avatar.png"},
			102: {UserID: 102, Nickname: "", Avatar: "wxfile://private-avatar"},
			104: {UserID: 104, Nickname: "已移除成员", Avatar: "https://public.example/revoked.png"},
			999: {UserID: 999, Nickname: "项目外账号", Avatar: "https://public.example/other.png"},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(requested, []int64{100, 101, 102}) {
		t.Fatalf("profile batch = %#v", requested)
	}
	if got.Project.CreatorDisplayName != "李设计师" {
		t.Fatalf("creator display name = %q", got.Project.CreatorDisplayName)
	}
	if len(got.Members) != 3 || got.Members[0].ActorID != 101 || got.Members[0].DisplayName != "张女士" || got.Members[0].Avatar != "https://public.example/avatar.png" {
		t.Fatalf("first member = %#v", got.Members)
	}
	if got.Members[1].DisplayName != "项目成员" || got.Members[1].Avatar != "" {
		t.Fatalf("fallback member = %#v", got.Members[1])
	}
	if got.Members[2].DisplayName != "项目成员" || got.Members[2].Avatar != "" {
		t.Fatalf("revoked member was enriched = %#v", got.Members[2])
	}
	if len(got.Confirmations) != 1 || got.Confirmations[0].ConfirmerID != 101 || got.Confirmations[0].DisplayName != "张女士" {
		t.Fatalf("confirmation = %#v", got.Confirmations)
	}
}

func TestProjectNoteProjectPresentationUsesNeutralCreatorFallback(t *testing.T) {
	projects := []tracking.ProjectNoteProject{{ID: "project-one", DesignerID: 7}}
	got, err := presentProjectNoteProjects(context.Background(), projects, func(_ context.Context, ids []int64) (map[int64]model.UserProfilePresentation, error) {
		if !reflect.DeepEqual(ids, []int64{7}) {
			t.Fatalf("profile batch = %#v", ids)
		}
		return map[int64]model.UserProfilePresentation{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CreatorDisplayName != "项目创建人" {
		t.Fatalf("projects = %#v", got)
	}
}

func TestProjectNoteProjectPresentationPreservesEmptyArray(t *testing.T) {
	got, err := presentProjectNoteProjects(context.Background(), []tracking.ProjectNoteProject{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("projects = %#v", got)
	}
}

func TestProjectNotePresentationUsesConfirmationFallbackWithoutInventingAName(t *testing.T) {
	detail := tracking.ProjectNoteProjectDetail{Confirmations: []tracking.ProjectNoteConfirmation{{ConfirmerID: 103}}}
	got, err := presentProjectNoteProjectDetail(context.Background(), detail, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Confirmations) != 1 || got.Confirmations[0].DisplayName != "方案确认人" || got.Confirmations[0].Avatar != "" {
		t.Fatalf("fallback confirmation = %#v", got.Confirmations)
	}
}
