package tracking

import (
	"context"
	"testing"
)

func TestProjectNoteExactVersionKeepsFrozenBodyAndDesignerBoundary(t *testing.T) {
	s, designer, other := projectNoteFixture(t)
	project, note, v1, _ := seedProjectNote(t, s, designer, 101)
	if _, err := s.ProjectNoteSaveNote(context.Background(), designer, note.ID, ProjectNoteDraft{Title: "新草稿", Body: "方案二", BodyFormat: "plain"}, note.Revision); err != nil {
		t.Fatal(err)
	}
	frozen, err := s.ProjectNoteGetVersion(context.Background(), designer, project.ID, v1.ID)
	if err != nil || frozen.Body != v1.Body || frozen.ID != v1.ID {
		t.Fatalf("frozen=%#v err=%v", frozen, err)
	}
	for _, scope := range []Scope{other, {Tenant: designer.Tenant, Actor: designer.Actor, Owners: []string{"owner-b"}, Manage: true}, {Tenant: designer.Tenant, Actor: 101, Owners: designer.Owners, Manage: false}} {
		if _, err = s.ProjectNoteGetVersion(context.Background(), scope, project.ID, v1.ID); err == nil {
			t.Fatal("unrelated account read exact version")
		}
	}
	otherProject, err := s.ProjectNoteCreateProject(context.Background(), designer, ProjectNoteProjectInput{Name: "另一项目", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteGetVersion(context.Background(), designer, otherProject.ID, v1.ID); err == nil {
		t.Fatal("cross-project version accepted")
	}
	s.IsContentOperator = func(context.Context, int64, string) (bool, error) { return false, nil }
	if _, err = s.ProjectNoteGetVersion(context.Background(), designer, project.ID, v1.ID); err == nil {
		t.Fatal("revoked live designer grant accepted")
	}
}

func TestProjectNoteSharedVersionRequiresOnlyValidShare(t *testing.T) {
	s, designer, _ := projectNoteFixture(t)
	project, _, v1, share := seedProjectNote(t, s, designer, 101)
	version, err := s.ProjectNoteSharedVersion(context.Background(), share.Token, 101)
	if err != nil || version.ID != v1.ID {
		t.Fatalf("version=%#v err=%v", version, err)
	}
	// 分享链接即访问凭据：非成员登录后也可读取固定版本。
	if _, err = s.ProjectNoteSharedVersion(context.Background(), share.Token, 102); err != nil {
		t.Fatalf("logged-in non-member rejected: %v", err)
	}
	if err = s.ProjectNoteRevokeMember(context.Background(), designer, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteSharedVersion(context.Background(), share.Token, 101); err != nil {
		t.Fatalf("valid share rejected after membership revoke: %v", err)
	}
	if err = s.ProjectNoteRevokeShare(context.Background(), designer, share.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProjectNoteSharedVersion(context.Background(), share.Token, 101); err == nil {
		t.Fatal("revoked share accepted")
	}
}
