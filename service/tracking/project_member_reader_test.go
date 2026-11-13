package tracking

import (
	"context"
	"testing"
)

func TestMemberDirectoryReadsFrozenVersionsWithoutDraftOrManagementAccess(t *testing.T) {
	ctx := context.Background()
	s, designer, other := projectNoteFixture(t)
	project, note, version, _ := seedProjectNote(t, s, designer, 101)
	directory, err := s.ProjectNoteMemberDirectory(ctx, 101, project.ID, 10, "")
	if err != nil || len(directory.Versions) != 1 || directory.Versions[0].ID != version.ID {
		t.Fatalf("member directory: %+v %v", directory, err)
	}
	view, err := s.ProjectNoteMemberVersion(ctx, 101, project.ID, version.ID)
	if err != nil || view.Version.Body != note.Body {
		t.Fatalf("member version: %+v %v", view, err)
	}
	if _, err = s.ProjectNoteGetProjectDetail(ctx, other, project.ID); err == nil {
		t.Fatal("member reader must not broaden designer management")
	}
	if _, err = s.ProjectNoteMemberDirectory(ctx, 102, project.ID, 10, ""); err == nil {
		t.Fatal("nonmember read directory")
	}
	if err = s.ProjectNoteRevokeMember(ctx, designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteMemberVersion(ctx, 101, project.ID, version.ID); err == nil {
		t.Fatal("revoked member read version")
	}
}
