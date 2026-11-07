package route

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"service/tracking"
)

func posterErrorStatus(err error) int {
	var known *tracking.Error
	if errors.As(err, &known) {
		return known.Status
	}
	return 0
}

func TestContentPosterSceneUsesResolvableShareTokenForFixedPublishedVersion(t *testing.T) {
	w := newURLLinkWorld(t)
	scene, err := contentPosterScene(context.Background(), w.svc, w.scope, w.contentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := w.svc.ResolveShare(context.Background(), scene)
	if err != nil || resolved["route_key"] != trackingReaderRoute || resolved["content_id"] != w.contentID || resolved["content_version"] != 1 {
		t.Fatalf("poster scene does not resolve to the fixed publication: %#v err=%v", resolved, err)
	}
	if again, err := contentPosterScene(context.Background(), w.svc, w.scope, w.contentID, 1); err != nil || again != scene {
		t.Fatalf("same-day poster scene changed: %q err=%v", again, err)
	}
}

func TestContentPosterSceneRejectsMissingPermissionUnpublishedAndRevokedContent(t *testing.T) {
	w := newURLLinkWorld(t)
	viewOnly := w.scope
	viewOnly.Manage = false
	if _, err := contentPosterScene(context.Background(), w.svc, viewOnly, w.contentID, 1); posterErrorStatus(err) != 403 {
		t.Fatalf("view-only account created poster scene: %v", err)
	}
	foreign := w.scope
	foreign.Owners = []string{"owner-b"}
	if _, err := contentPosterScene(context.Background(), w.svc, foreign, w.contentID, 1); posterErrorStatus(err) != 404 {
		t.Fatalf("foreign owner created poster scene: %v", err)
	}
	if _, err := contentPosterScene(context.Background(), w.svc, w.scope, w.contentID, 2); err == nil {
		t.Fatal("unpublished version created poster scene")
	}
	cards, err := w.svc.Contents(context.Background(), w.scope, tracking.Filter{}, w.contentID)
	if err != nil || len(cards.Items.([]tracking.Content)) != 1 {
		t.Fatalf("content lookup: %#v %v", cards.Items, err)
	}
	current := cards.Items.([]tracking.Content)[0]
	if _, err = w.svc.Revoke(context.Background(), w.scope, w.contentID, current.Revision, "停止公开", "poster-revoke-content"); err != nil {
		t.Fatal(err)
	}
	if _, err = contentPosterScene(context.Background(), w.svc, w.scope, w.contentID, 1); err == nil {
		t.Fatal("revoked content created poster scene")
	}
}

func TestExistingSharePosterSceneRequiresActualSharerOrAuthorizedOwner(t *testing.T) {
	w := newURLLinkWorld(t)
	token, err := contentPosterScene(context.Background(), w.svc, w.scope, w.contentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	actualSharer := tracking.Scope{Tenant: w.scope.Tenant, Actor: w.scope.Actor}
	if scene, err := existingSharePosterScene(context.Background(), w.svc, actualSharer, token); err != nil || scene != token {
		t.Fatalf("actual sharer denied: %q %v", scene, err)
	}
	authorized := w.scope
	authorized.Actor = 99
	if scene, err := existingSharePosterScene(context.Background(), w.svc, authorized, token); err != nil || scene != token {
		t.Fatalf("authorized owner denied: %q %v", scene, err)
	}
	foreign := tracking.Scope{Tenant: w.scope.Tenant, Actor: 99, Owners: []string{"owner-b"}, Manage: true}
	if _, err := existingSharePosterScene(context.Background(), w.svc, foreign, token); posterErrorStatus(err) != 404 {
		t.Fatalf("foreign account used opaque token as permission: %v", err)
	}
	if _, err := existingSharePosterScene(context.Background(), w.svc, actualSharer, "not-a-real-share-token"); posterErrorStatus(err) != 400 {
		t.Fatalf("invalid token status = %d err=%v", posterErrorStatus(err), err)
	}
	store := w.svc.Store.(*governanceTestStore)
	for key, row := range store.records {
		if row.Kind != "share" {
			continue
		}
		var share tracking.Share
		if json.Unmarshal(row.Payload, &share) != nil || share.Content != w.contentID {
			continue
		}
		share.Revoked = true
		row.Payload, _ = json.Marshal(share)
		store.records[key] = row
	}
	if _, err := existingSharePosterScene(context.Background(), w.svc, actualSharer, token); posterErrorStatus(err) != 404 {
		t.Fatalf("revoked share created a poster: %v", err)
	}
}
