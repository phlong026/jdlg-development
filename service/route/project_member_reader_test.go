package route

import (
	"context"
	"net/http"
	"testing"
)

func TestMemberVideoPlaybackRechecksMembershipAfterTicketIssue(t *testing.T) {
	svc, scope, actor, router, project, version, _, asset, _ := createSharedVideoFixture(t)
	*actor = 101
	issued := projectNoteRequest(t, router, http.MethodPost, "/api/v1/miniprogram/project-notes/member-projects/"+project.ID+"/versions/"+version.ID+"/media/"+asset.ID+"/playback", nil)
	if issued.Code != http.StatusOK {
		t.Fatal(issued.Code, issued.Body.String())
	}
	playback := playbackURL(t, issued.Body.Bytes())
	if err := svc.ProjectNoteRevokeMember(context.Background(), scope, project.ID, 101); err != nil {
		t.Fatal(err)
	}
	denied := projectNoteRequest(t, router, http.MethodGet, playback, nil)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("revoked member streaming: %d %s", denied.Code, denied.Body.String())
	}
}
