package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func publishMaterial(t *testing.T, s *Service, scope Scope, title string) Content {
	t.Helper()
	ctx := context.Background()
	draft, err := s.SaveContent(ctx, scope, "", Draft{Title: title, Body: "published body", Rights: "own", Owner: scope.Owners[0]})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), "publish-material-"+draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	return result["content"].(Content)
}
func TestPublishedMaterialsScopeSnapshotAndRevocation(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	published := publishMaterial(t, s, scope, "published title")
	draft, err := s.SaveContent(ctx, scope, published.ID, Draft{Title: "SECRET draft title", Body: "SECRET draft body", Rights: "own", Owner: scope.Owners[0], Revision: published.Revision})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.SaveContent(ctx, scope, "", Draft{Title: "UNPUBLISHED title", Body: "UNPUBLISHED body", Rights: "own", Owner: scope.Owners[0]})
	if err != nil {
		t.Fatal(err)
	}
	otherScope := scope
	otherScope.Owners = []string{"owner-b"}
	other := publishMaterial(t, s, otherScope, "OTHER owner title")
	s.Config.Enabled = false
	list, err := s.PublishedMaterials(ctx, scope, "", 20)
	if err != nil || len(list.Items) != 1 {
		t.Fatal(list, err)
	}
	if list.Items[0].Title != "published title" || list.Items[0].Version != 1 || list.Items[0].Status != "published" {
		t.Fatal(list)
	}
	encoded, _ := json.Marshal(list)
	for _, secret := range []string{"SECRET", "UNPUBLISHED", "OTHER", "body", "owner_id", "revision", "page_views"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("leak %q: %s", secret, encoded)
		}
	}
	for _, id := range []string{fresh.ID, other.ID, "missing-material"} {
		if _, err := s.OpenPublishedMaterial(ctx, scope, id, 1); err == nil {
			t.Fatal("unauthorized open", id)
		}
	}
	if _, err := s.OpenPublishedMaterial(ctx, scope, published.ID, 2); err == nil {
		t.Fatal("unpublished version open")
	}
	if _, err := s.OpenPublishedMaterial(ctx, scope, published.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revoke(ctx, scope, published.ID, draft.Revision, "撤回", "revoke-material-one"); err != nil {
		t.Fatal(err)
	}
	list, err = s.PublishedMaterials(ctx, scope, "", 20)
	if err != nil || len(list.Items) != 0 {
		t.Fatal(list, err)
	}
	if _, err := s.OpenPublishedMaterial(ctx, scope, published.ID, 1); err == nil {
		t.Fatal("revoked entry open")
	}
	if _, err := s.ReadContent(ctx, published.ID, 1); err == nil {
		t.Fatal("revoked public reader open")
	}
}
func TestPublishedMaterialsDenyMissingGrantAndBoundPagination(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		publishMaterial(t, s, scope, "public")
	}
	for _, bad := range []Scope{{Tenant: scope.Tenant, Actor: scope.Actor, Owners: scope.Owners, View: true}, {Tenant: "other", Actor: scope.Actor, Owners: scope.Owners, Manage: true}, {Tenant: scope.Tenant, Actor: 0, Owners: scope.Owners, Manage: true}, {Tenant: scope.Tenant, Actor: scope.Actor, Manage: true}} {
		if _, err := s.PublishedMaterials(ctx, bad, "", 20); err == nil {
			t.Fatalf("scope accepted %+v", bad)
		}
	}
	first, err := s.PublishedMaterials(ctx, scope, "", 2)
	if err != nil || len(first.Items) != 2 || first.Cursor == "" {
		t.Fatal(first, err)
	}
	second, err := s.PublishedMaterials(ctx, scope, first.Cursor, 2)
	if err != nil || len(second.Items) != 1 || second.Cursor != "" || second.Items[0].ID <= first.Cursor {
		t.Fatal(second, err)
	}
	s.Config.ContentEnabled = false
	if _, err := s.PublishedMaterials(ctx, scope, "", 20); err == nil {
		t.Fatal("content switch ignored")
	}
}
func TestPublishedOperatorSelfVisitIsServerVerified(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	content := publishMaterial(t, s, scope, "public")
	s.Config.DefaultOwner = "different-default"
	granted := true
	calls := 0
	s.IsContentOperator = func(_ context.Context, account int64, owner string) (bool, error) {
		calls++
		return granted && account == scope.Actor && owner == scope.Owners[0], nil
	}
	input := EntryInput{Route: publishedReaderRoute, Content: content.ID, Version: 1, Client: "material-client", Anonymous: "material-anonymous", Consent: "v1"}
	author, err := s.Enter(ctx, input, scope.Actor)
	if err != nil || !author.Self || author.Owner != scope.Owners[0] {
		t.Fatal(author, err)
	}
	if result := accept(t, s, author, ev(s, author, "material-author-pv", "page_view", s.Now())); result[0].Status != "accepted" {
		t.Fatal(result)
	}
	stats, err := s.Visitors(ctx, scope, Filter{})
	if err != nil || stats.Identified != 0 || stats.Anonymous != 0 {
		t.Fatal("operator polluted stats", stats, err)
	}
	returned, err := s.Enter(ctx, input, scope.Actor)
	if err != nil || !returned.Self || calls != 2 {
		t.Fatal(returned, calls, err)
	}
	other, err := s.Enter(ctx, input, 99)
	if err != nil || other.Self {
		t.Fatal(other, err)
	}
	spoof := ev(s, other, "material-recipient-pv", "page_view", s.Now())
	spoof.View = "material-recipient-view"
	spoof.Flags = []string{"self", "preview"}
	accept(t, s, other, spoof)
	stats, err = s.Visitors(ctx, scope, Filter{})
	if err != nil || stats.Identified != 1 {
		t.Fatal("client forged self", stats, err)
	}
	granted = false
	returned, err = s.Enter(ctx, input, scope.Actor)
	if err != nil || returned.Self {
		t.Fatal("cached old grant", returned, err)
	}
	s.IsContentOperator = func(context.Context, int64, string) (bool, error) { return false, errors.New("db unavailable") }
	if _, err := s.Enter(ctx, input, scope.Actor); err == nil {
		t.Fatal("permission lookup failed open")
	}
}

func TestPublishedOperatorShareRecipientAndCrossOwnerPrevious(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	content := publishMaterial(t, s, scope, "public")
	s.IsContentOperator = func(_ context.Context, actor int64, owner string) (bool, error) {
		return actor == scope.Actor && scope.Allows(owner), nil
	}
	s.Config.DefaultOwner = "unrelated-owner"
	previous := enter(t, s, scope.Actor)
	input := EntryInput{Route: publishedReaderRoute, Content: content.ID, Version: 1, Client: previous.Client, Consent: "v1", Previous: previous.Token}
	author, err := s.Enter(ctx, input, scope.Actor)
	if err != nil || !author.Self || author.Owner != content.Owner || author.Session == previous.Session {
		t.Fatal("previous owner contaminated reader", author, err)
	}
	share, err := s.CreateShare(ctx, ShareInput{Route: publishedReaderRoute, Content: content.ID, Version: 1, Channel: "app_message", Key: "native-material-share"}, author.Token, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	input.Previous = ""
	input.ShareToken = share["share_token"].(string)
	input.Client = "recipient-client"
	recipient, err := s.Enter(ctx, input, 99)
	if err != nil || recipient.Self || recipient.Share != share["share_id"] || recipient.Owner != content.Owner {
		t.Fatal(recipient, err)
	}
	pv := ev(s, recipient, "native-recipient-pv", "page_view", s.Now())
	pv.View = "native-recipient-view"
	if results := accept(t, s, recipient, pv); results[0].Status != "accepted" {
		t.Fatal(results)
	}
	stats, err := s.Visitors(ctx, scope, Filter{})
	if err != nil || stats.Identified != 1 || stats.Items.([]Visitor)[0].Views != 1 {
		t.Fatal(stats, err)
	}
}
