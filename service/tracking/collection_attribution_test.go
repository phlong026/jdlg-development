package tracking

import (
	"context"
	"strings"
	"testing"
)

func publishedCollectionFixture(t *testing.T) (*Service, Scope, Content, Content, ProjectCollection, PublishedProjectCollection) {
	t.Helper()
	s, scope := fixture(t)
	s.Config.Routes[projectCollectionReaderRoute] = Route{Title: "项目合集", Type: "detail", Public: true, Audience: RouteAudienceCustomer}
	scope.Actor = 42
	first := publishNativeReferenceContent(t, s, scope, "合集作品", "collection-attribution-first")
	other := publishNativeReferenceContent(t, s, scope, "非合集作品", "collection-attribution-other")
	draft, err := s.SaveProjectCollection(context.Background(), scope, "", collectionInput(scope.Owners[0], "collection-attribution-create", first))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewProjectCollection(context.Background(), scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.PublishProjectCollection(context.Background(), scope, draft.ID, draft.Revision, preview.PreviewToken, "collection-attribution-publish")
	if err != nil {
		t.Fatal(err)
	}
	return s, scope, first, other, draft, published
}

func TestProjectCollectionShareAttributesDirectoryAndFixedChildToActualSharer(t *testing.T) {
	s, scope, child, _, _, published := publishedCollectionFixture(t)
	s.NoticeRules.BrowseEnabled = true
	ctx := context.Background()

	sharerEntry, err := s.Enter(ctx, EntryInput{
		Route: projectCollectionReaderRoute, Content: published.ID, Version: published.Version,
		Client: "collection-sharer-client", Consent: s.Config.ConsentVersion,
	}, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateShare(ctx, ShareInput{
		Route: projectCollectionReaderRoute, Content: published.ID, Version: published.Version,
		Channel: "app_message", Key: "collection-share-create",
	}, sharerEntry.Token, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	shareID, shareToken := created["share_id"].(string), created["share_token"].(string)

	directory, err := s.Enter(ctx, EntryInput{
		Route: projectCollectionReaderRoute, Content: published.ID, Version: published.Version,
		ShareToken: shareToken, Client: "collection-reader-directory", Consent: s.Config.ConsentVersion,
	}, 101)
	if err != nil || directory.Share != shareID || directory.Attribution != "share" {
		t.Fatalf("directory=%+v err=%v", directory, err)
	}
	childEntry, err := s.Enter(ctx, EntryInput{
		Route: publishedReaderRoute, Content: child.ID, Version: child.Version,
		CollectionID: published.ID, CollectionVersion: published.Version,
		ShareToken: shareToken, Client: "collection-reader-child", Consent: s.Config.ConsentVersion,
	}, 101)
	if err != nil || childEntry.Share != shareID || childEntry.Attribution != "share" || childEntry.CollectionID != published.ID {
		t.Fatalf("child entry=%+v err=%v", childEntry, err)
	}
	returnedDirectory, err := s.Enter(ctx, EntryInput{
		Route: projectCollectionReaderRoute, Content: published.ID, Version: published.Version,
		Previous: childEntry.Token, Client: childEntry.Client, Consent: s.Config.ConsentVersion,
	}, 101)
	if err != nil || returnedDirectory.Share != shareID || returnedDirectory.Attribution != "share" || returnedDirectory.Session != childEntry.Session {
		t.Fatalf("returned directory=%+v err=%v", returnedDirectory, err)
	}
	otherDraft, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-other-create", child))
	if err != nil {
		t.Fatal(err)
	}
	otherPreview, err := s.PreviewProjectCollection(ctx, scope, otherDraft.ID, otherDraft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	otherCollection, err := s.PublishProjectCollection(ctx, scope, otherDraft.ID, otherDraft.Revision, otherPreview.PreviewToken, "collection-other-publish")
	if err != nil {
		t.Fatal(err)
	}
	wrongDirectory, err := s.Enter(ctx, EntryInput{
		Route: projectCollectionReaderRoute, Content: otherCollection.ID, Version: otherCollection.Version,
		Previous: childEntry.Token, Client: childEntry.Client, Consent: s.Config.ConsentVersion,
	}, 101)
	if err != nil {
		t.Fatal(err)
	}
	if wrongDirectory.Share != "" || wrongDirectory.Attribution != "unattributed" || wrongDirectory.Session == childEntry.Session {
		t.Fatalf("previous attribution crossed collections: %+v", wrongDirectory)
	}

	directoryView := ev(s, directory, "collection-directory-view", "page_view", s.Now())
	childView := ev(s, childEntry, "collection-child-view", "page_view", s.Now())
	directoryView.View = "collection-directory-page-view"
	childView.View = "collection-child-page-view"
	if got := accept(t, s, directory, directoryView); got[0].Status != "accepted" {
		t.Fatalf("directory event=%+v", got)
	}
	if got := accept(t, s, childEntry, childView); got[0].Status != "accepted" {
		t.Fatalf("child event=%+v", got)
	}

	list, err := s.MyShares(ctx, scope, scope.Actor, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := list.Items.([]MyShare)
	if !ok {
		t.Fatalf("share items type=%T", list.Items)
	}
	if len(items) != 1 || items[0].Title != published.Title || items[0].Status != "已发布" || items[0].Views != 2 || items[0].DirectoryViews != 1 || items[0].ContentViews != 1 {
		t.Fatalf("collection shares=%+v", items)
	}
	visits, err := s.MyShareVisits(ctx, scope, scope.Actor, shareID, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	visitItems, ok := visits.Items.([]MyShareVisit)
	if !ok {
		t.Fatalf("visit items type=%T", visits.Items)
	}
	if len(visitItems) != 1 || visitItems[0].Views != 2 || visitItems[0].DirectoryViews != 1 || visitItems[0].ContentViews != 1 {
		t.Fatalf("collection visits=%+v", visitItems)
	}

	rows := s.Store.(*memoryStore).data[s.Config.Tenant]
	for eventID, wantKind := range map[string]string{
		directoryView.ID: "collection_directory_browse",
		childView.ID:     "collection_content_browse",
	} {
		notice, err := decode[NoticeEvent](&memoryRepo{data: rows}, "notice_event", eventID)
		if err != nil || notice.RecipientID != scope.Actor || notice.ShareID != shareID || notice.Kind != wantKind {
			t.Fatalf("notice %s=%+v err=%v", eventID, notice, err)
		}
	}
}

func TestProjectCollectionContextRejectsUnlistedAndRevokedContinuation(t *testing.T) {
	s, scope, child, unlisted, draft, published := publishedCollectionFixture(t)
	s.NoticeRules.BrowseEnabled = true
	ctx := context.Background()
	sharerEntry, err := s.Enter(ctx, EntryInput{
		Route: projectCollectionReaderRoute, Content: published.ID, Version: published.Version,
		Client: "collection-revoke-sharer", Consent: s.Config.ConsentVersion,
	}, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateShare(ctx, ShareInput{
		Route: projectCollectionReaderRoute, Content: published.ID, Version: published.Version,
		Key: "collection-revoke-share",
	}, sharerEntry.Token, scope.Actor)
	if err != nil {
		t.Fatal(err)
	}
	shareToken := created["share_token"].(string)

	_, err = s.Enter(ctx, EntryInput{
		Route: publishedReaderRoute, Content: unlisted.ID, Version: unlisted.Version,
		CollectionID: published.ID, CollectionVersion: published.Version,
		ShareToken: shareToken, Client: "collection-unlisted-child", Consent: s.Config.ConsentVersion,
	}, 101)
	if err == nil || !strings.Contains(err.Error(), "合集") {
		t.Fatalf("unlisted collection context err=%v", err)
	}

	childEntry, err := s.Enter(ctx, EntryInput{
		Route: publishedReaderRoute, Content: child.ID, Version: child.Version,
		CollectionID: published.ID, CollectionVersion: published.Version,
		ShareToken: shareToken, Client: "collection-revoked-child", Consent: s.Config.ConsentVersion,
	}, 101)
	if err != nil {
		t.Fatal(err)
	}
	beforeRevoke := ev(s, childEntry, "collection-notice-before-revoke", "page_view", s.Now())
	beforeRevoke.View = "collection-before-revoke-view"
	if result := accept(t, s, childEntry, beforeRevoke); result[0].Status != "accepted" {
		t.Fatalf("event before revoke=%+v", result)
	}
	if eligible, err := s.NoticeEligible(ctx, beforeRevoke.ID, scope.Actor, scope.Owners[0]); err != nil || !eligible {
		t.Fatalf("notice before revoke eligible=%v err=%v", eligible, err)
	}
	managed, err := s.ProjectCollection(ctx, scope, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RevokeProjectCollection(ctx, scope, draft.ID, managed.Revision, "停止公开", "collection-attribution-revoke"); err != nil {
		t.Fatal(err)
	}
	if eligible, err := s.NoticeEligible(ctx, beforeRevoke.ID, scope.Actor, scope.Owners[0]); err != nil || eligible {
		t.Fatalf("notice after revoke eligible=%v err=%v", eligible, err)
	}
	afterRevoke := ev(s, childEntry, "collection-event-after-revoke", "page_view", s.Now())
	afterRevoke.View = "collection-after-revoke-view"
	result := accept(t, s, childEntry, afterRevoke)
	if result[0].Status != "rejected" || !strings.Contains(result[0].Reason, "合集") {
		t.Fatalf("event after revoke=%+v", result)
	}
	_, err = s.Enter(ctx, EntryInput{
		Route: publishedReaderRoute, Content: child.ID, Version: child.Version,
		CollectionID: published.ID, CollectionVersion: published.Version,
		ShareToken: shareToken, Client: "collection-entry-after-revoke", Consent: s.Config.ConsentVersion,
	}, 102)
	if err == nil || !strings.Contains(err.Error(), "合集") {
		t.Fatalf("entry after revoke err=%v", err)
	}
}

func TestProjectCollectionContentAndMediaReadsRevalidateCollectionContext(t *testing.T) {
	s, scope := fixture(t)
	scope.Actor = 42
	ctx := context.Background()
	asset := Asset{ID: "1234567890abcdef1234567890abcdef", Kind: "image", URL: "/api/v1/miniprogram/tracking/media/1234567890abcdef1234567890abcdef", Width: 800, Height: 600, MIME: "image/jpeg"}
	if err := s.AddMedia(ctx, scope, asset, scope.Owners[0]); err != nil {
		t.Fatal(err)
	}
	draft, err := s.SaveContent(ctx, scope, "", Draft{Title: "带图作品", Body: "正文", BodyFormat: "plain", Rights: "own", Owner: scope.Owners[0], CoverID: asset.ID, MediaIDs: []string{asset.ID}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(ctx, scope, draft.ID, draft.Revision)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Publish(ctx, scope, draft.ID, draft.Revision, preview["preview_token"].(string), "collection-media-source-publish")
	if err != nil {
		t.Fatal(err)
	}
	content := result["content"].(Content)
	collection, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "collection-media-create", content))
	if err != nil {
		t.Fatal(err)
	}
	collectionPreview, err := s.PreviewProjectCollection(ctx, scope, collection.ID, collection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.PublishProjectCollection(ctx, scope, collection.ID, collection.Revision, collectionPreview.PreviewToken, "collection-media-publish")
	if err != nil {
		t.Fatal(err)
	}

	read, err := s.ReadPublishedProjectCollectionItemContent(ctx, collection.ID, published.Version, 0)
	if err != nil || read.Cover == nil || !strings.Contains(read.Cover.URL, "/project-collections/"+collection.ID+"/") || len(read.Media) != 1 || read.Media[0].URL != read.Cover.URL {
		t.Fatalf("content=%+v err=%v", read, err)
	}
	if got, err := s.ReadPublishedProjectCollectionItemMedia(ctx, collection.ID, published.Version, 0, asset.ID); err != nil || got != asset {
		t.Fatalf("media=%+v err=%v", got, err)
	}
	managed, err := s.ProjectCollection(ctx, scope, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RevokeProjectCollection(ctx, scope, collection.ID, managed.Revision, "停止公开", "collection-media-revoke"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadPublishedProjectCollectionItemContent(ctx, collection.ID, published.Version, 0); err == nil {
		t.Fatal("revoked collection still served content")
	}
	if _, err = s.ReadPublishedProjectCollectionItemMedia(ctx, collection.ID, published.Version, 0, asset.ID); err == nil {
		t.Fatal("revoked collection still served media")
	}
}
