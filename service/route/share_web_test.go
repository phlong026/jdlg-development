package route

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"service/tracking"
)

const shareWebVideoID = "video-share-web-0000000000000001"

func shareWebFixture(t *testing.T) (*gin.Engine, *tracking.Service, *governanceTestStore, string, tracking.Content) {
	t.Helper()
	t.Setenv("SHARE_WEB_ENABLED", "true")
	t.Setenv("PUBLIC_SHARE_ORIGIN", "https://www.jiadilingguang.com")
	store := &governanceTestStore{records: map[string]tracking.Record{}}
	svc := tracking.New(store, tracking.Config{
		Enabled: true, ContentEnabled: true, VideoEnabled: true, Tenant: "share-web", DefaultOwner: "owner-a", ConsentVersion: "v1",
		Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}},
	})
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }
	scope := tracking.Scope{Tenant: "share-web", Actor: 42, Owners: []string{"owner-a"}, Manage: true}
	if err := svc.AddMedia(context.Background(), scope, tracking.Asset{ID: shareWebVideoID, Kind: "video", MIME: "video/mp4", Width: 1280, Height: 720, URL: "/api/v1/miniprogram/tracking/media/" + shareWebVideoID}, "owner-a"); err != nil {
		t.Fatal(err)
	}
	content, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "安全文章", Body: "安全正文<script>alert(1)</script>", BodyFormat: "rich_text", Rights: "own", Owner: "owner-a", MediaIDs: []string{shareWebVideoID}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), scope, content.ID, content.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(context.Background(), scope, content.ID, content.Revision, preview["preview_token"].(string), "share-web-publish")
	if err != nil {
		t.Fatal(err)
	}
	publishedContent := published["content"].(tracking.Content)
	version := publishedContent.Version
	entry, err := svc.Enter(context.Background(), tracking.EntryInput{Route: "pages/contentreader/contentreader", Content: content.ID, Version: version, Client: "share-web-author-client", Consent: "v1"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateShare(context.Background(), tracking.ShareInput{Route: entry.Route, Content: entry.Content, Version: entry.Version, Channel: "app_message", Key: "share-web-entry"}, entry.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerShareWebHandlers(router, router.Group("/api/v1"), svc, tracking.NewWechatAPI())
	return router, svc, store, created["share_token"].(string), publishedContent
}

func TestShareWebGETEscapesBodyAndCreatesNoTrackingRecord(t *testing.T) {
	router, _, store, code, _ := shareWebFixture(t)
	before := len(store.records)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/share/s/"+code, nil))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "<script>alert(1)</script>") {
		t.Fatalf("GET %d %s", w.Code, w.Body.String())
	}
	if len(store.records) != before {
		t.Fatalf("GET created records: before=%d after=%d", before, len(store.records))
	}
}

func shareWebCollectionFixture(t *testing.T) (*gin.Engine, *tracking.Service, *governanceTestStore, string, tracking.PublishedProjectCollection, tracking.Content) {
	t.Helper()
	t.Setenv("SHARE_WEB_ENABLED", "true")
	t.Setenv("PUBLIC_SHARE_ORIGIN", "https://www.jiadilingguang.com")
	store := &governanceTestStore{records: map[string]tracking.Record{}}
	svc := tracking.New(store, tracking.Config{
		Enabled: true, ContentEnabled: true, Tenant: "share-web-collection", DefaultOwner: "owner-a", ConsentVersion: "v1",
		Routes: map[string]tracking.Route{
			"pages/contentreader/contentreader":       {Public: true},
			"pages/collectionreader/collectionreader": {Public: true},
		},
	})
	now := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }
	scope := tracking.Scope{Tenant: "share-web-collection", Actor: 42, Owners: []string{"owner-a"}, Manage: true}
	content, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "庭院案例", Body: "合集作品正文", BodyFormat: "plain", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), scope, content.ID, content.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(context.Background(), scope, content.ID, content.Revision, preview["preview_token"].(string), "share-web-collection-content")
	if err != nil {
		t.Fatal(err)
	}
	publishedContent := published["content"].(tracking.Content)
	collection, err := svc.SaveProjectCollection(context.Background(), scope, "", tracking.ProjectCollectionDraft{
		Title: "新中式项目合集", Description: "三个项目的代表方案", Owner: "owner-a",
		Items:          []tracking.ProjectCollectionItemInput{{ContentID: publishedContent.ID, ContentVersion: publishedContent.Version}},
		IdempotencyKey: "share-web-collection-create",
	})
	if err != nil {
		t.Fatal(err)
	}
	collectionPreview, err := svc.PreviewProjectCollection(context.Background(), scope, collection.ID, collection.Revision)
	if err != nil {
		t.Fatal(err)
	}
	publishedCollection, err := svc.PublishProjectCollection(context.Background(), scope, collection.ID, collection.Revision, collectionPreview.PreviewToken, "share-web-collection-publish")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := svc.Enter(context.Background(), tracking.EntryInput{Route: "pages/collectionreader/collectionreader", Content: publishedCollection.ID, Version: publishedCollection.Version, Client: "share-web-collection-author", Consent: "v1"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateShare(context.Background(), tracking.ShareInput{Route: entry.Route, Content: entry.Content, Version: entry.Version, Channel: "app_message", Key: "share-web-collection-share"}, entry.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerShareWebHandlers(router, router.Group("/api/v1"), svc, tracking.NewWechatAPI())
	return router, svc, store, created["share_token"].(string), publishedCollection, publishedContent
}

func TestShareWebCollectionDirectoryAndItemGETCreateNoTrackingRecords(t *testing.T) {
	router, _, store, code, collection, content := shareWebCollectionFixture(t)
	before := len(store.records)
	directory := httptest.NewRecorder()
	router.ServeHTTP(directory, httptest.NewRequest(http.MethodGet, "/share/s/"+code, nil))
	if directory.Code != http.StatusOK || !strings.Contains(directory.Body.String(), collection.Title) || !strings.Contains(directory.Body.String(), "/share/s/"+code+"/items/0") {
		t.Fatalf("directory = %d %s", directory.Code, directory.Body.String())
	}
	item := httptest.NewRecorder()
	router.ServeHTTP(item, httptest.NewRequest(http.MethodGet, "/share/s/"+code+"/items/0", nil))
	if item.Code != http.StatusOK || !strings.Contains(item.Body.String(), content.Title) || !strings.Contains(item.Body.String(), "合集作品正文") {
		t.Fatalf("item = %d %s", item.Code, item.Body.String())
	}
	if len(store.records) != before {
		t.Fatalf("collection GET created records: before=%d after=%d", before, len(store.records))
	}
}

func TestShareWebCollectionItemEntryKeepsOriginalShareAttribution(t *testing.T) {
	router, _, store, code, collection, content := shareWebCollectionFixture(t)
	shareCount := 0
	for _, row := range store.records {
		if row.Kind == "share" {
			shareCount++
		}
	}
	body, _ := json.Marshal(map[string]any{"code": code, "item_position": 0, "client_instance": "browser-collection-reader", "consent_version": "v1"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/share-web/entry", bytes.NewReader(body))
	req.Header.Set("Origin", "https://www.jiadilingguang.com")
	req.Header.Set("X-Share-Web", "1")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("entry = %d %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data tracking.Entry `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	entry := envelope.Data
	if entry.Route != "pages/contentreader/contentreader" || entry.Content != content.ID || entry.Version != content.Version || entry.CollectionID != collection.ID || entry.CollectionVersion != collection.Version || entry.Share == "" || entry.Attribution != "share" {
		t.Fatalf("collection child entry = %+v", entry)
	}
	afterShares := 0
	for _, row := range store.records {
		if row.Kind == "share" {
			afterShares++
		}
	}
	if afterShares != shareCount {
		t.Fatalf("collection navigation created a fake child share: before=%d after=%d", shareCount, afterShares)
	}
}

func TestShareWebCollectionPagesStopAfterCollectionRevoke(t *testing.T) {
	router, svc, _, code, collection, _ := shareWebCollectionFixture(t)
	scope := tracking.Scope{Tenant: "share-web-collection", Actor: 42, Owners: []string{"owner-a"}, Manage: true}
	if _, err := svc.RevokeProjectCollection(context.Background(), scope, collection.ID, collection.Revision, "停止公开", "share-web-collection-revoke"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/share/s/" + code, "/share/s/" + code + "/items/0"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("revoked %s = %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestShareWebCollectionItemOpensExactNativeChildWithOriginalShare(t *testing.T) {
	t.Setenv("TRACKING_WECHAT_URLLINK_ENABLED", "true")
	t.Setenv("TRACKING_WECHAT_ENV", "release")
	withMiniprogramCredentials(t)
	_, svc, _, code, collection, content := shareWebCollectionFixture(t)
	var generated map[string]any
	wechat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			_, _ = io.WriteString(w, `{"access_token":"collection-share-web-token"}`)
		case "/wxa/generate_urllink":
			if err := json.NewDecoder(r.Body).Decode(&generated); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{"errcode":0,"url_link":"https://wxaurl.cn/collection-child"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer wechat.Close()
	api := tracking.NewWechatAPI()
	api.Base = wechat.URL
	router := gin.New()
	registerShareWebHandlers(router, router.Group("/api/v1"), svc, api)
	body, _ := json.Marshal(map[string]any{"code": code, "item_position": 0})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/share-web/open-miniprogram", bytes.NewReader(body))
	req.Header.Set("Origin", "https://www.jiadilingguang.com")
	req.Header.Set("X-Share-Web", "1")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("open child = %d %s", w.Code, w.Body.String())
	}
	query, _ := generated["query"].(string)
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	if generated["path"] != "pages/contentreader/contentreader" || values.Get("st") != code || values.Get("content_id") != content.ID || values.Get("content_version") != strconv.Itoa(content.Version) || values.Get("collection_id") != collection.ID || values.Get("collection_version") != strconv.Itoa(collection.Version) || values.Get("item_position") != "0" {
		t.Fatalf("native child target = %#v query=%s", generated, query)
	}
}

func TestShareWebCollectionDirectorySourceCannotClaimChildHandoffReady(t *testing.T) {
	t.Setenv("TRACKING_WECHAT_URLLINK_ENABLED", "true")
	withMiniprogramCredentials(t)
	_, svc, _, code, collection, _ := shareWebCollectionFixture(t)
	source, err := svc.Enter(context.Background(), tracking.EntryInput{Route: "pages/collectionreader/collectionreader", Content: collection.ID, Version: collection.Version, Client: "collection-directory-handoff", Consent: "v1", ShareToken: code, WebSource: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	event := tracking.Event{Schema: 1, ID: "collection-directory-handoff-view", Type: "page_view", Session: source.Session, View: "collection-directory-handoff-page", Route: source.Route, Content: source.Content, Version: source.Version, Share: source.Share, Occurred: svc.Now(), AppState: "foreground", PageState: "visible"}
	if results, acceptErr := svc.Accept(context.Background(), source.Token, []tracking.Event{event}); acceptErr != nil || len(results) != 1 || results[0].Status != "accepted" {
		t.Fatal(results, acceptErr)
	}
	var generated map[string]any
	wechat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			_, _ = io.WriteString(w, `{"access_token":"directory-target-test"}`)
		case "/wxa/generate_urllink":
			if decodeErr := json.NewDecoder(r.Body).Decode(&generated); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			_, _ = io.WriteString(w, `{"errcode":0,"url_link":"https://wxaurl.cn/directory-target-test"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer wechat.Close()
	api := tracking.NewWechatAPI()
	api.Base = wechat.URL
	router := gin.New()
	registerShareWebHandlers(router, router.Group("/api/v1"), svc, api)
	token := strings.Repeat("z", 43)
	body, _ := json.Marshal(map[string]any{"code": code, "item_position": 0, "source_entry_token": source.Token, "source_page_view_event_id": event.ID, "handoff_token": token})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/share-web/open-miniprogram", bytes.NewReader(body))
	req.Header.Set("Origin", "https://www.jiadilingguang.com")
	req.Header.Set("X-Share-Web", "1")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			Ready bool `json:"handoff_ready"`
		} `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Data.Ready {
		t.Fatalf("directory source claimed child handoff: %s", w.Body.String())
	}
	query, _ := generated["query"].(string)
	values, parseErr := url.ParseQuery(query)
	if parseErr != nil || values.Get("ht") != "" {
		t.Fatalf("mismatched handoff leaked into child URL: %q %v", query, parseErr)
	}
}

func TestShareWebEntryRequiresFixedOrigin(t *testing.T) {
	router, _, store, code, _ := shareWebFixture(t)
	body, _ := json.Marshal(map[string]any{"code": code, "client_instance": "browser-client-1234", "consent_version": "v1"})
	before := len(store.records)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/share-web/entry", strings.NewReader(string(body)))
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("X-Share-Web", "1")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || len(store.records) != before {
		t.Fatalf("cross-origin entry %d %s records=%d/%d", w.Code, w.Body.String(), len(store.records), before)
	}
}

func TestShareWebVideoEventsReachAggregationAndStopAfterContentRevoke(t *testing.T) {
	router, svc, store, code, content := shareWebFixture(t)
	post := func(path string, payload any, output any) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Origin", "https://www.jiadilingguang.com")
		req.Header.Set("X-Share-Web", "1")
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if output != nil && w.Code == http.StatusOK {
			var envelope struct {
				Code int             `json:"code"`
				Data json.RawMessage `json:"data"`
			}
			if err = json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || json.Unmarshal(envelope.Data, output) != nil {
				t.Fatalf("decode %s: status=%d body=%s err=%v", path, w.Code, w.Body.String(), err)
			}
		}
		return w
	}

	var entry tracking.Entry
	if w := post("/api/v1/share-web/entry", map[string]string{"code": code, "client_instance": "browser-video-client-0001", "consent_version": "v1"}, &entry); w.Code != http.StatusOK {
		t.Fatalf("entry = %d %s", w.Code, w.Body.String())
	}
	viewID := "share-web-video-view-0001"
	now := svc.Now()
	events := []tracking.Event{
		{ID: "share-web-page-view-0001", Type: "page_view", Schema: 1, SDK: "share-web-v1", Session: entry.Session, View: viewID, Route: entry.Route, Content: entry.Content, Version: entry.Version, Share: entry.Share, Occurred: now, AppState: "foreground", PageState: "visible", Flags: []string{}},
		{ID: "share-web-video-state-0001", Type: "video_state", Schema: 1, SDK: "share-web-v1", Session: entry.Session, View: viewID, Seq: 1, Route: entry.Route, Content: entry.Content, Version: entry.Version, Share: entry.Share, Media: shareWebVideoID, Player: "share-video-1", Duration: 10000, Rate: 1, PlayState: "playing", Occurred: now, AppState: "foreground", PageState: "visible", Flags: []string{}},
		{ID: "share-web-video-progress-0001", Type: "video_progress", Schema: 1, SDK: "share-web-v1", Session: entry.Session, View: viewID, Seq: 2, Route: entry.Route, Content: entry.Content, Version: entry.Version, Share: entry.Share, Media: shareWebVideoID, Player: "share-video-1", Position: 2000, Duration: 10000, Watch: 2000, Rate: 1, PlayState: "playing", Ranges: [][2]int64{{0, 2000}}, Monotonic: 2000, Occurred: now, AppState: "foreground", PageState: "visible", Flags: []string{}},
	}
	var results []tracking.Result
	if w := post("/api/v1/share-web/events", map[string]any{"entry_token": entry.Token, "events": events}, &results); w.Code != http.StatusOK {
		t.Fatalf("events = %d %s", w.Code, w.Body.String())
	}
	if len(results) != 3 || results[0].Status != "accepted" || results[1].Status != "accepted" || results[2].Status != "accepted" {
		t.Fatalf("video results = %+v", results)
	}
	eventCount := 0
	for _, row := range store.records {
		if row.Kind == "event" {
			eventCount++
		}
	}
	if eventCount != 3 {
		t.Fatalf("stored events = %d, want 3", eventCount)
	}
	visits, err := svc.Visits(context.Background(), tracking.Scope{Tenant: "share-web", Actor: 42, Owners: []string{"owner-a"}, View: true, Manage: true}, tracking.Filter{Owner: "owner-a", Content: entry.Content})
	if err != nil {
		t.Fatal(err)
	}
	items := visits.Items.([]tracking.Visit)
	if len(items) != 1 || items[0].Progress == nil || items[0].Progress.Kind != "video" || items[0].Progress.Percent != 20 {
		t.Fatalf("video aggregation = %+v", items)
	}
	if _, err = svc.Revoke(context.Background(), tracking.Scope{Tenant: "share-web", Actor: 42, Owners: []string{"owner-a"}, Manage: true}, content.ID, content.Revision, "撤回联调素材", "share-web-revoke-0001"); err != nil {
		t.Fatal(err)
	}
	late := events[2]
	late.ID = "share-web-video-progress-late"
	late.Seq = 3
	late.Position = 3000
	late.Monotonic = 3000
	var lateResults []tracking.Result
	if w := post("/api/v1/share-web/events", map[string]any{"entry_token": entry.Token, "events": []tracking.Event{late}}, &lateResults); w.Code != http.StatusOK {
		t.Fatalf("late events = %d %s", w.Code, w.Body.String())
	}
	if len(lateResults) != 1 || lateResults[0].Status != "rejected" {
		t.Fatalf("revoked event result = %+v", lateResults)
	}
	after := 0
	for _, row := range store.records {
		if row.Kind == "event" {
			after++
		}
	}
	if after != eventCount {
		t.Fatalf("revoked content added events: before=%d after=%d", eventCount, after)
	}
}

func TestShareWebMediaRequiresTheSharedPublishedVersion(t *testing.T) {
	t.Setenv("SHARE_WEB_ENABLED", "true")
	t.Setenv("PUBLIC_SHARE_ORIGIN", "https://www.jiadilingguang.com")
	dir := t.TempDir()
	t.Setenv("TRACKING_MEDIA_DIR", dir)
	store := &governanceTestStore{records: map[string]tracking.Record{}}
	svc := tracking.New(store, tracking.Config{
		Enabled: true, ContentEnabled: true, VideoEnabled: true, Tenant: "share-media", DefaultOwner: "owner-a", ConsentVersion: "v1",
		Routes: map[string]tracking.Route{"pages/contentreader/contentreader": {Public: true}},
	})
	scope := tracking.Scope{Tenant: "share-media", Actor: 42, Owners: []string{"owner-a"}, Manage: true}
	publicID, draftID := strings.Repeat("p", 32), strings.Repeat("d", 32)
	for _, id := range []string{publicID, draftID} {
		if err := svc.AddMedia(context.Background(), scope, tracking.Asset{ID: id, Kind: "image", MIME: "image/png", Width: 2, Height: 2, URL: "/api/v1/miniprogram/tracking/media/" + id}, "owner-a"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id), []byte(id+"-bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"schema":1,"blocks":[{"kind":"image","media_id":"` + publicID + `"}]}`
	content, err := svc.SaveContent(context.Background(), scope, "", tracking.Draft{Title: "媒体文章", Body: body, BodyFormat: "article_v2", Rights: "own", Owner: "owner-a", MediaIDs: []string{publicID}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(context.Background(), scope, content.ID, content.Revision)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.Publish(context.Background(), scope, content.ID, content.Revision, preview["preview_token"].(string), "share-media-publish")
	if err != nil {
		t.Fatal(err)
	}
	version := published["content"].(tracking.Content).Version
	entry, err := svc.Enter(context.Background(), tracking.EntryInput{Route: trackingReaderRoute, Content: content.ID, Version: version, Client: "share-media-client", Consent: "v1"}, 42)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateShare(context.Background(), tracking.ShareInput{Route: entry.Route, Content: entry.Content, Version: entry.Version, Channel: "app_message", Key: "share-media-create"}, entry.Token, 42)
	if err != nil {
		t.Fatal(err)
	}
	code := created["share_token"].(string)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerShareWebHandlers(router, router.Group("/api/v1"), svc, tracking.NewWechatAPI())

	valid := httptest.NewRecorder()
	router.ServeHTTP(valid, httptest.NewRequest(http.MethodGet, "/api/v1/share-web/media/"+code+"/"+publicID, nil))
	if valid.Code != http.StatusOK || valid.Body.String() != publicID+"-bytes" {
		t.Fatalf("published media = %d %q", valid.Code, valid.Body.String())
	}
	t.Setenv("SHARE_WEB_ENABLED", "false")
	disabled := httptest.NewRecorder()
	router.ServeHTTP(disabled, httptest.NewRequest(http.MethodGet, "/api/v1/share-web/media/"+code+"/"+publicID, nil))
	if disabled.Code != http.StatusNotFound {
		t.Fatalf("disabled share media = %d %q", disabled.Code, disabled.Body.String())
	}
	t.Setenv("SHARE_WEB_ENABLED", "true")
	denied := httptest.NewRecorder()
	router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/api/v1/share-web/media/"+code+"/"+draftID, nil))
	if denied.Code != http.StatusNotFound || strings.Contains(denied.Body.String(), draftID+"-bytes") {
		t.Fatalf("draft media leaked = %d %q", denied.Code, denied.Body.String())
	}
}

func TestShareWebURLLinkUsesTheSameShareAndCachesWechatResult(t *testing.T) {
	t.Setenv("TRACKING_WECHAT_URLLINK_ENABLED", "true")
	t.Setenv("TRACKING_WECHAT_ENV", "release")
	withMiniprogramCredentials(t)
	_, svc, _, code, _ := shareWebFixture(t)
	linkCalls := 0
	var generated map[string]any
	var callsMu sync.Mutex
	wechat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			_, _ = io.WriteString(w, `{"access_token":"share-web-token"}`)
		case "/wxa/generate_urllink":
			time.Sleep(50 * time.Millisecond)
			callsMu.Lock()
			linkCalls++
			_ = json.NewDecoder(r.Body).Decode(&generated)
			callsMu.Unlock()
			_, _ = io.WriteString(w, `{"errcode":0,"url_link":"https://wxaurl.cn/share-web"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer wechat.Close()
	api := tracking.NewWechatAPI()
	api.Base = wechat.URL
	router := gin.New()
	registerShareWebHandlers(router, router.Group("/api/v1"), svc, api)
	body, _ := json.Marshal(map[string]string{"code": code})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/share-web/open-miniprogram", strings.NewReader(string(body)))
			req.Header.Set("Origin", "https://www.jiadilingguang.com")
			req.Header.Set("X-Share-Web", "1")
			router.ServeHTTP(w, req)
			responses <- w
		}()
	}
	for i := 0; i < 2; i++ {
		w := <-responses
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "https://wxaurl.cn/share-web") {
			t.Fatalf("request %d = %d %s", i, w.Code, w.Body.String())
		}
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	query, err := url.ParseQuery(generated["query"].(string))
	if err != nil || generated["path"] != trackingReaderRoute || query.Get("st") != code || linkCalls != 1 {
		t.Fatalf("generated=%v query=%v err=%v calls=%d", generated, query, err, linkCalls)
	}
}

func TestShareURLLinkSingleflightDoesNotBlockDifferentShares(t *testing.T) {
	cache := newShareURLLinkCache(10)
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})
	go func() {
		_, _ = cache.load(context.Background(), "share-a", now, func() (tracking.URLLinkResult, error) {
			close(started)
			<-release
			return tracking.URLLinkResult{URL: "https://wxaurl.cn/a", Expires: now.Add(time.Hour)}, nil
		})
		close(firstDone)
	}()
	<-started
	secondDone := make(chan struct{})
	go func() {
		_, _ = cache.load(context.Background(), "share-b", now, func() (tracking.URLLinkResult, error) {
			return tracking.URLLinkResult{URL: "https://wxaurl.cn/b", Expires: now.Add(time.Hour)}, nil
		})
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("an in-flight share blocked an unrelated share")
	}
	close(release)
	<-firstDone
}

func TestShareWebHandoffsCarryOnlyTheirOwnTokenAndReuseOnlyTheirOwnURLLink(t *testing.T) {
	t.Setenv("TRACKING_WECHAT_URLLINK_ENABLED", "true")
	withMiniprogramCredentials(t)
	_, svc, _, code, content := shareWebFixture(t)
	source, err := svc.Enter(context.Background(), tracking.EntryInput{Route: trackingReaderRoute, Content: content.ID, Version: content.Version, Client: "web-handoff-http-client", Consent: "v1", ShareToken: code, WebSource: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	event := tracking.Event{Schema: 1, ID: "web-handoff-http-source-event", Type: "page_view", Session: source.Session, View: "web-handoff-http-source-view", Route: source.Route, Content: source.Content, Version: source.Version, Share: source.Share, Occurred: svc.Now(), AppState: "foreground", PageState: "visible", Monotonic: 1}
	if results, err := svc.Accept(context.Background(), source.Token, []tracking.Event{event}); err != nil || len(results) != 1 || results[0].Status != "accepted" {
		t.Fatal(results, err)
	}
	queries := []url.Values{}
	wechat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			_, _ = io.WriteString(w, `{"access_token":"handoff-test-token"}`)
		case "/wxa/generate_urllink":
			var payload map[string]any
			if json.NewDecoder(r.Body).Decode(&payload) != nil {
				t.Error("bad request")
			}
			q, err := url.ParseQuery(payload["query"].(string))
			if err != nil {
				t.Error(err)
			}
			queries = append(queries, q)
			_, _ = io.WriteString(w, `{"errcode":0,"url_link":"https://wxaurl.cn/handoff-test"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer wechat.Close()
	api := tracking.NewWechatAPI()
	api.Base = wechat.URL
	router := gin.New()
	registerShareWebHandlers(router, router.Group("/api/v1"), svc, api)
	tokens := []string{strings.Repeat("a", 43), strings.Repeat("b", 43), strings.Repeat("a", 43), ""}
	for _, token := range tokens {
		payload := map[string]string{"code": code, "source_entry_token": source.Token, "source_page_view_event_id": event.ID, "handoff_token": token}
		body, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/share-web/open-miniprogram", bytes.NewReader(body))
		req.Header.Set("Origin", "https://www.jiadilingguang.com")
		req.Header.Set("X-Share-Web", "1")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d %s", w.Code, w.Body.String())
		}
		var result struct {
			Data struct {
				Ready   bool      `json:"handoff_ready"`
				Expires time.Time `json:"handoff_expires_at"`
			} `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.Ready != (token != "") {
			t.Fatal(w.Body.String())
		}
		if result.Data.Ready && result.Data.Expires.IsZero() {
			t.Fatal("ready without expiry")
		}
	}
	if len(queries) != 3 || queries[0].Get("ht") != tokens[0] || queries[1].Get("ht") != tokens[1] || queries[2].Get("ht") != "" {
		t.Fatalf("handoff URL cache leaked: %v", queries)
	}
	for _, q := range queries {
		if q.Get("st") != code || q.Get("content_id") != content.ID {
			t.Fatal(q)
		}
	}
}
