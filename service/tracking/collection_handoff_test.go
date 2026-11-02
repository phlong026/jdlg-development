package tracking

import (
	"context"
	"strings"
	"testing"
)

type collectionHandoffFixture struct {
	service    *Service
	scope      Scope
	shareToken string
	collection PublishedProjectCollection
	child      Content
	otherChild Content
	source     Entry
	event      Event
}

func newCollectionHandoffFixture(t *testing.T, childSource bool) collectionHandoffFixture {
	t.Helper()
	s, scope := fixture(t)
	s.NoticeRules.BrowseEnabled = true
	s.Config.Routes[projectCollectionReaderRoute] = Route{Title: "项目合集", Type: "collection", Public: true, Audience: RouteAudienceCustomer}
	scope.Actor = 42
	child := publishNativeReferenceContent(t, s, scope, "合集交接作品", "collection-handoff-child")
	otherChild := publishNativeReferenceContent(t, s, scope, "合集其他作品", "collection-handoff-other-child")
	draft, err := s.SaveProjectCollection(context.Background(), scope, "", collectionInput(scope.Owners[0], "collection-handoff-create", child, otherChild))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewProjectCollection(context.Background(), scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	collection, err := s.PublishProjectCollection(context.Background(), scope, draft.ID, draft.Revision, preview.PreviewToken, "collection-handoff-publish")
	if err != nil {
		t.Fatal(err)
	}
	author, err := s.Enter(context.Background(), EntryInput{Route: projectCollectionReaderRoute, Content: collection.ID, Version: collection.Version, Client: "collection-handoff-author", Consent: "v1"}, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateShare(context.Background(), ShareInput{Route: author.Route, Content: author.Content, Version: author.Version, Key: "collection-handoff-share"}, author.Token, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	shareToken := created["share_token"].(string)
	input := EntryInput{Route: projectCollectionReaderRoute, Content: collection.ID, Version: collection.Version, Client: "collection-handoff-web", Consent: "v1", ShareToken: shareToken, WebSource: true}
	if childSource {
		input.Route, input.Content, input.Version = publishedReaderRoute, child.ID, child.Version
		input.CollectionID, input.CollectionVersion = collection.ID, collection.Version
	}
	source, err := s.Enter(context.Background(), input, 0)
	if err != nil {
		t.Fatal(err)
	}
	event := ev(s, source, "collection-handoff-source-event", "page_view", s.Now())
	event.View = "collection-handoff-source-view"
	if got := accept(t, s, source, event); got[0].Status != "accepted" {
		t.Fatal(got)
	}
	return collectionHandoffFixture{
		service: s, scope: scope, shareToken: shareToken, collection: collection,
		child: child, otherChild: otherChild, source: source, event: event,
	}
}

func TestCollectionShareHandoffRedeemsOnlyExactDirectoryOrFixedChild(t *testing.T) {
	for _, childSource := range []bool{false, true} {
		name := "directory"
		if childSource {
			name = "child"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newCollectionHandoffFixture(t, childSource)
			token := strings.Repeat(map[bool]string{false: "d", true: "c"}[childSource], 43)
			proof, err := fixture.service.CreateShareHandoff(context.Background(), fixture.shareToken, fixture.source.Token, fixture.event.ID, token)
			if err != nil || !proof.Ready {
				t.Fatalf("proof=%+v err=%v", proof, err)
			}
			exact := EntryInput{Route: fixture.source.Route, Content: fixture.source.Content, Version: fixture.source.Version, CollectionID: fixture.source.CollectionID, CollectionVersion: fixture.source.CollectionVersion, Client: "collection-handoff-mini", Consent: "v1", ShareToken: fixture.shareToken, HandoffToken: token}
			mini, err := fixture.service.Enter(context.Background(), exact, 101)
			if err != nil || mini.HandoffKey != hash(token) || mini.Share != fixture.source.Share {
				t.Fatalf("mini=%+v err=%v", mini, err)
			}
			miniEvent := ev(fixture.service, mini, "collection-handoff-mini-event-"+name, "page_view", fixture.service.Now())
			miniEvent.View = "collection-handoff-mini-view-" + name
			if result := accept(t, fixture.service, mini, miniEvent); result[0].Status != "accepted" {
				t.Fatalf("mini event=%+v", result)
			}
			rows := fixture.service.Store.(*memoryStore).data[fixture.service.Config.Tenant]
			if _, ok := rows["notice_event:"+miniEvent.ID]; ok {
				t.Fatal("collection handoff repeated the acknowledged web notice")
			}
			if _, ok := rows["notice_event:"+fixture.event.ID]; !ok {
				t.Fatal("collection web notice missing")
			}

			otherToken := strings.Repeat(map[bool]string{false: "e", true: "f"}[childSource], 43)
			proof, err = fixture.service.CreateShareHandoff(context.Background(), fixture.shareToken, fixture.source.Token, fixture.event.ID, otherToken)
			if err != nil || !proof.Ready {
				t.Fatalf("other proof=%+v err=%v", proof, err)
			}
			wrong := EntryInput{Route: publishedReaderRoute, Content: fixture.child.ID, Version: fixture.child.Version, CollectionID: fixture.collection.ID, CollectionVersion: fixture.collection.Version, Client: "collection-handoff-wrong", Consent: "v1", ShareToken: fixture.shareToken, HandoffToken: otherToken}
			if childSource {
				wrong.Content, wrong.Version = fixture.otherChild.ID, fixture.otherChild.Version
			}
			mismatch, err := fixture.service.Enter(context.Background(), wrong, 102)
			if err != nil || mismatch.HandoffKey != "" || mismatch.Share != fixture.source.Share {
				t.Fatalf("cross-target handoff mismatch=%+v err=%v", mismatch, err)
			}
		})
	}
}

func TestCollectionShareHandoffRequiresCurrentCollectionAndChild(t *testing.T) {
	for _, revoke := range []string{"collection", "child"} {
		t.Run(revoke, func(t *testing.T) {
			fixture := newCollectionHandoffFixture(t, true)
			if revoke == "collection" {
				managed, err := fixture.service.ProjectCollection(context.Background(), fixture.scope, fixture.collection.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = fixture.service.RevokeProjectCollection(context.Background(), fixture.scope, fixture.collection.ID, managed.Revision, "撤回合集", "collection-handoff-revoke"); err != nil {
					t.Fatal(err)
				}
			} else if _, err := fixture.service.Revoke(context.Background(), fixture.scope, fixture.child.ID, fixture.child.Revision, "撤回作品", "collection-handoff-child-revoke"); err != nil {
				t.Fatal(err)
			}
			proof, err := fixture.service.CreateShareHandoff(context.Background(), fixture.shareToken, fixture.source.Token, fixture.event.ID, strings.Repeat("r", 43))
			if err != nil || proof.Ready {
				t.Fatalf("revoked %s proof=%+v err=%v", revoke, proof, err)
			}
		})
	}
}

func TestCollectionShareHandoffTargetMustMatchTheWebSource(t *testing.T) {
	directory := newCollectionHandoffFixture(t, false)
	directoryToken := strings.Repeat("t", 43)
	childTarget := EntryInput{Route: publishedReaderRoute, Content: directory.child.ID, Version: directory.child.Version, CollectionID: directory.collection.ID, CollectionVersion: directory.collection.Version, ShareToken: directory.shareToken}
	if proof, err := directory.service.CreateShareHandoffForTarget(context.Background(), directory.shareToken, directory.source.Token, directory.event.ID, directoryToken, childTarget); err != nil || proof.Ready {
		t.Fatalf("directory source produced child handoff: %+v %v", proof, err)
	}
	directoryTarget := EntryInput{Route: directory.source.Route, Content: directory.source.Content, Version: directory.source.Version, ShareToken: directory.shareToken}
	if proof, err := directory.service.CreateShareHandoffForTarget(context.Background(), directory.shareToken, directory.source.Token, directory.event.ID, directoryToken, directoryTarget); err != nil || !proof.Ready {
		t.Fatalf("exact directory target = %+v %v", proof, err)
	}

	child := newCollectionHandoffFixture(t, true)
	childToken := strings.Repeat("u", 43)
	exactChild := EntryInput{Route: child.source.Route, Content: child.source.Content, Version: child.source.Version, CollectionID: child.source.CollectionID, CollectionVersion: child.source.CollectionVersion, ShareToken: child.shareToken}
	if proof, err := child.service.CreateShareHandoffForTarget(context.Background(), child.shareToken, child.source.Token, child.event.ID, childToken, exactChild); err != nil || !proof.Ready {
		t.Fatalf("exact child target = %+v %v", proof, err)
	}
}
