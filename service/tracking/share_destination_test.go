package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
)

func TestOwnShareKeepsAuthenticatedMiniProgramSharer(t *testing.T) {
	s, _, scope, _ := reviewFixture()
	s.Config.ConsentVersion = "v1"
	s.Config.DefaultOwner = "one"
	scope.Actor, scope.Manage = 42, true
	entry, err := s.Enter(context.Background(), EntryInput{Route: "page", Client: "client-own-share", Consent: "v1"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateShare(context.Background(), ShareInput{Route: "page", Key: "own-share-destination"}, entry.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	token := created["share_token"].(string)
	share, err := s.OwnShare(context.Background(), Scope{Tenant: scope.Tenant, Actor: 42}, 42, token)
	if err != nil || share.Sharer != "u:42" || share.Origin != "miniprogram" {
		t.Fatalf("own share = %+v err=%v", share, err)
	}
	if _, err = s.OwnShare(context.Background(), Scope{Tenant: scope.Tenant, Actor: 43, Owners: scope.Owners, Manage: true}, 43, token); err == nil {
		t.Fatal("another account claimed the share destination")
	} else {
		var denied *Error
		if !errors.As(err, &denied) || denied.Status != 404 {
			t.Fatalf("foreign share error = %v", err)
		}
	}
}

func TestShareNativeDestinationUsesSameToken(t *testing.T) {
	share := Share{Route: publishedReaderRoute, Content: "article-one", Version: 3}
	path, rawQuery, err := ShareNativeDestination(share, "same-share-token")
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatal(err)
	}
	if path != publishedReaderRoute || values.Get("content_id") != "article-one" || values.Get("content_version") != "3" || values.Get("st") != "same-share-token" {
		t.Fatalf("destination path=%q query=%v", path, values)
	}
}

func TestResolveShareRecordRejectsRevokedShare(t *testing.T) {
	s, rows, _, _ := reviewFixture()
	s.Config.ConsentVersion = "v1"
	s.Config.DefaultOwner = "one"
	entry, err := s.Enter(context.Background(), EntryInput{Route: "page", Client: "client-revoked-share", Consent: "v1"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateShare(context.Background(), ShareInput{Route: "page", Key: "revoked-share-destination"}, entry.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	token := created["share_token"].(string)
	row, err := rows.Get("share", hash(token))
	if err != nil {
		t.Fatal(err)
	}
	var share Share
	if err = json.Unmarshal(row.Payload, &share); err != nil {
		t.Fatal(err)
	}
	share.Revoked = true
	row.Payload = marshal(share)
	if err = rows.Put(row); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveShareRecord(context.Background(), token); err == nil {
		t.Fatal("revoked share remained readable")
	}
}
