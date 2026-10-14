package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeMPSource struct {
	articles []MPArticle
	err      error
}

func (f *fakeMPSource) Articles(_ context.Context, limit int) ([]MPArticle, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.articles) > limit {
		return f.articles[:limit], nil
	}
	return f.articles, nil
}

type fakeMediaFetcher struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]bool
}

func (f *fakeMediaFetcher) Fetch(_ context.Context, raw string, _ int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, raw)
	if f.fail[raw] {
		return nil, ErrUnavailable
	}
	return []byte("image:" + raw), nil
}

func mpArticle(n int, body string) MPArticle {
	return MPArticle{ID: fmt.Sprintf("pub-%d#0", n), Title: fmt.Sprintf("文章 %d", n), HTML: body, URL: fmt.Sprintf("https://mp.weixin.qq.com/s/a%d", n), UpdatedAt: time.Unix(1760000000, 0)}
}

func mpFixture(t *testing.T) (*Service, Scope, MPSyncConfig) {
	s, scope := fixture(t)
	return s, scope, MPSyncConfig{AppID: "wx-mp", Owner: "owner-a", Rights: "own"}
}

// storeAsMedia is the test stand-in for the route's validating media store.
var importedMediaCounter atomic.Int64

func storeAsMedia(s *Service, scope Scope) MediaStore {
	return func(ctx context.Context, owner string, data []byte, name string) (Asset, error) {
		// Unique across calls, as the random IDs of the real store are.
		id := fmt.Sprintf("imported-media-%06d", importedMediaCounter.Add(1))
		asset := Asset{ID: id, Kind: "image", URL: "/api/v1/miniprogram/tracking/media/" + id, Width: 640, Height: 480, MIME: "image/png", Title: name}
		return asset, s.AddMedia(ctx, scope, asset, owner)
	}
}

func listContents(t *testing.T, s *Service) []Content {
	t.Helper()
	out := []Content{}
	if err := s.readTransaction(context.Background(), func(r Repository) error {
		rows, err := r.List("content")
		for _, row := range rows {
			var c Content
			if e := json.Unmarshal(row.Payload, &c); e != nil {
				return e
			}
			out = append(out, c)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func syncOnce(t *testing.T, s *Service, scope Scope, cfg MPSyncConfig, src MPSource, fetch MediaFetcher) MPSyncResult {
	t.Helper()
	res, err := s.SyncOfficialAccount(context.Background(), scope, cfg, src, fetch, storeAsMedia(s, scope))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestMPSyncCreatesDraftsOnlyAndIsIdempotent(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	src := &fakeMPSource{articles: []MPArticle{mpArticle(1, "<section>第一篇</section>"), mpArticle(2, "<p>第二篇</p>")}}
	res := syncOnce(t, s, scope, cfg, src, nil)
	if res.Counts.Fetched != 2 || res.Counts.Created != 2 || res.Counts.Failed != 0 {
		t.Fatalf("counts: %+v", res.Counts)
	}
	contents := listContents(t, s)
	if len(contents) != 2 {
		t.Fatalf("%d contents", len(contents))
	}
	for _, c := range contents {
		if c.Status != "draft" || c.Version != 0 || c.PublishedAt != nil || c.Owner != "owner-a" || c.Rights != "own" || c.BodyFormat != "rich_text" || !strings.HasPrefix(c.SourceURL, "https://mp.weixin.qq.com/s/") || !strings.Contains(c.SourceNote, "人工核对") {
			t.Fatalf("a synced article must be an unpublished draft for review: %+v", c)
		}
	}
	// Nothing was published: no public version exists and nothing is readable.
	if err := s.readTransaction(context.Background(), func(r Repository) error {
		versions, _ := r.List("version")
		if len(versions) != 0 {
			t.Fatalf("sync published %d versions", len(versions))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	again := syncOnce(t, s, scope, cfg, src, nil)
	if again.Counts.Unchanged != 2 || again.Counts.Created != 0 || again.Counts.Updated != 0 || len(listContents(t, s)) != 2 {
		t.Fatalf("a second run must change nothing: %+v", again.Counts)
	}
	state, found, err := s.MPSyncStatus(context.Background(), scope, "owner-a")
	if err != nil || !found || !state.OK || state.Counts.Unchanged != 2 {
		t.Fatalf("state: %+v %v %v", state, found, err)
	}
}

func TestMPSyncUpdatesAnUnchangedDraftAndKeepsAPublishedVersionReadable(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	src := &fakeMPSource{articles: []MPArticle{mpArticle(1, "<p>旧正文</p>")}}
	syncOnce(t, s, scope, cfg, src, nil)
	id := listContents(t, s)[0].ID

	src.articles[0].HTML = "<p>新正文</p>"
	if res := syncOnce(t, s, scope, cfg, src, nil); res.Counts.Updated != 1 || res.Counts.Created != 0 {
		t.Fatalf("draft update: %+v", res.Counts)
	}
	cs := listContents(t, s)
	if len(cs) != 1 || cs[0].ID != id || !strings.Contains(cs[0].Body, "新正文") || cs[0].Revision != 2 {
		t.Fatalf("draft not updated in place: %+v", cs)
	}

	// A person reviews and publishes it. Publishing changes the revision, which
	// is not an edit.
	preview, err := s.Preview(context.Background(), scope, id, cs[0].Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(context.Background(), scope, id, cs[0].Revision, preview["preview_token"].(string), "publish-key-mp-1"); err != nil {
		t.Fatal(err)
	}
	src.articles[0].HTML = "<p>再次修改</p>"
	res := syncOnce(t, s, scope, cfg, src, nil)
	if res.Counts.Updated != 1 || res.Counts.Conflicts != 0 {
		t.Fatalf("publishing without editing must not look like a local edit: %+v", res.Counts)
	}
	after := listContents(t, s)[0]
	if after.Status != "draft" || after.Version != 1 || !strings.Contains(after.Body, "再次修改") {
		t.Fatalf("expected a new draft over the published version: %+v", after)
	}
	published, err := s.ReadContent(context.Background(), id, 1)
	if err != nil || !strings.Contains(published.Body, "新正文") {
		t.Fatalf("the published version must stay as published: %+v %v", published, err)
	}
}

func TestMPSyncNeverOverwritesAPersonsEdit(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	src := &fakeMPSource{articles: []MPArticle{mpArticle(1, "<p>同步正文</p>")}}
	syncOnce(t, s, scope, cfg, src, nil)
	c := listContents(t, s)[0]
	edited, err := s.SaveContent(context.Background(), scope, c.ID, Draft{Title: c.Title, Body: "<p>人工修改后的正文</p>", BodyFormat: "rich_text", SourceURL: c.SourceURL, SourceNote: c.SourceNote, Rights: c.Rights, Owner: c.Owner, Revision: c.Revision})
	if err != nil {
		t.Fatal(err)
	}
	src.articles[0].HTML = "<p>上游又改了</p>"
	res := syncOnce(t, s, scope, cfg, src, nil)
	if res.Counts.Conflicts != 1 || res.Counts.Updated != 0 || len(res.Problems) != 1 || res.Problems[0].Reason != mpEditedLocally {
		t.Fatalf("expected one reported conflict: %+v", res)
	}
	now := listContents(t, s)[0]
	if now.Revision != edited.Revision || !strings.Contains(now.Body, "人工修改后的正文") {
		t.Fatalf("a person's edit was overwritten: %+v", now)
	}
	if again := syncOnce(t, s, scope, cfg, src, nil); again.Counts.Conflicts != 1 {
		t.Fatalf("the conflict must stay visible until resolved: %+v", again.Counts)
	}
}

func TestMPSyncSkipsRevokedDeletedAndUnusableArticles(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	src := &fakeMPSource{articles: []MPArticle{mpArticle(1, "<p>将被撤回</p>")}}
	syncOnce(t, s, scope, cfg, src, nil)
	id := listContents(t, s)[0].ID
	c := listContents(t, s)[0]
	preview, _ := s.Preview(context.Background(), scope, id, c.Revision)
	if _, err := s.Publish(context.Background(), scope, id, c.Revision, preview["preview_token"].(string), "publish-key-mp-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Revoke(context.Background(), scope, id, c.Revision+1, "撤回原因", "revoke-key-mp-0001"); err != nil {
		t.Fatal(err)
	}
	deleted := mpArticle(2, "<p>已删除</p>")
	deleted.Deleted = true
	imageOnly := mpArticle(3, `<section><img src="https://mmbiz.qpic.cn/x"></section>`)
	noTitle := mpArticle(4, "<p>正文</p>")
	noTitle.Title = " \n "
	src.articles = []MPArticle{mpArticle(1, "<p>撤回后上游又改</p>"), deleted, imageOnly, noTitle}
	res := syncOnce(t, s, scope, cfg, src, nil)
	reasons := map[string]bool{}
	for _, p := range res.Problems {
		reasons[p.Reason] = true
	}
	if res.Counts.Skipped != 4 || res.Counts.Created != 0 || !reasons[mpRevoked] || !reasons[mpDeletedUpstream] || !reasons[mpEmptyBody] || !reasons[mpTitleEmpty] {
		t.Fatalf("expected four distinct skips: %+v", res)
	}
	if len(listContents(t, s)) != 1 {
		t.Fatal("a skipped article created content")
	}
	for _, c := range listContents(t, s) {
		if c.Status != "revoked" {
			t.Fatalf("sync changed revoked content: %+v", c)
		}
	}
	// The revoked article is remembered, so it is not re-processed each run.
	if again := syncOnce(t, s, scope, cfg, src, nil); again.Counts.Unchanged != 1 {
		t.Fatalf("revoked article reprocessed: %+v", again.Counts)
	}
}

func TestMPSyncImportsPicturesOnceAndSurvivesFailures(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	cfg.ImportImages = true
	a := mpArticle(1, `<p>正文</p><img data-src="https://mmbiz.qpic.cn/p/1"><img data-src="https://mmbiz.qpic.cn/p/2"><img data-src="https://mmbiz.qpic.cn/p/bad">`)
	a.ThumbURL = "https://mmbiz.qpic.cn/p/cover"
	src := &fakeMPSource{articles: []MPArticle{a}}
	fetch := &fakeMediaFetcher{fail: map[string]bool{"https://mmbiz.qpic.cn/p/bad": true}}
	res := syncOnce(t, s, scope, cfg, src, fetch)
	if res.Counts.Created != 1 || res.Counts.ImagesImported != 3 || res.Counts.ImageFailures != 1 {
		t.Fatalf("a failing picture must cost only itself: %+v", res.Counts)
	}
	c := listContents(t, s)[0]
	if c.Cover == nil || len(c.Media) != 3 {
		t.Fatalf("cover and pictures not attached: %+v", c)
	}
	for _, m := range c.Media {
		if m.Kind != "image" || !strings.HasPrefix(m.URL, "/api/v1/miniprogram/tracking/media/") {
			t.Fatalf("media must be a stored asset, never the remote URL: %+v", m)
		}
	}
	// Upstream text changes: the pictures already imported are reused.
	fetch.calls = nil
	src.articles[0].HTML = strings.Replace(a.HTML, "正文", "改过的正文", 1)
	fetch.fail = nil
	res = syncOnce(t, s, scope, cfg, src, fetch)
	if res.Counts.Updated != 1 {
		t.Fatalf("update: %+v", res.Counts)
	}
	if len(fetch.calls) != 1 || fetch.calls[0] != "https://mmbiz.qpic.cn/p/bad" {
		t.Fatalf("only the previously failed picture may be fetched again: %v", fetch.calls)
	}
}

// An author who only swaps a picture changes the article, even though its text
// is identical, so the draft must pick the new picture up.
func TestMPSyncNoticesAReplacedPictureWithUnchangedText(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	cfg.ImportImages = true
	src := &fakeMPSource{articles: []MPArticle{mpArticle(1, `<p>同一段正文</p><img src="https://mmbiz.qpic.cn/p/old">`)}}
	fetch := &fakeMediaFetcher{}
	syncOnce(t, s, scope, cfg, src, fetch)
	before := listContents(t, s)[0]
	src.articles[0].HTML = `<p>同一段正文</p><img src="https://mmbiz.qpic.cn/p/new">`
	res := syncOnce(t, s, scope, cfg, src, fetch)
	if res.Counts.Updated != 1 {
		t.Fatalf("a replaced picture was not noticed: %+v", res.Counts)
	}
	after := listContents(t, s)[0]
	if len(after.Media) != 1 || len(before.Media) != 1 || after.Media[0].ID == before.Media[0].ID {
		t.Fatalf("the new picture was not attached: before=%+v after=%+v", before.Media, after.Media)
	}
}

func TestMPSyncNeverFetchesWhenImportIsOffAndBoundsRunWork(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	body := `<p>正文</p><img src="https://mmbiz.qpic.cn/p/1">`
	fetch := &fakeMediaFetcher{}
	syncOnce(t, s, scope, cfg, &fakeMPSource{articles: []MPArticle{mpArticle(1, body)}}, fetch)
	if len(fetch.calls) != 0 {
		t.Fatalf("pictures were fetched with import off: %v", fetch.calls)
	}
	cfg.ImportImages, cfg.RunImages, cfg.MaxImages = true, 2, 5
	articles := []MPArticle{}
	for i := 10; i < 14; i++ {
		articles = append(articles, mpArticle(i, fmt.Sprintf(`<p>正文%d</p><img src="https://mmbiz.qpic.cn/p/%d"><img src="https://mmbiz.qpic.cn/q/%d">`, i, i, i)))
	}
	res := syncOnce(t, s, scope, cfg, &fakeMPSource{articles: articles}, fetch)
	if res.Counts.ImagesImported != 2 || res.Counts.ImageFailures < 1 || len(fetch.calls) != 2 {
		t.Fatalf("per-run picture budget not enforced: %+v fetched=%d", res.Counts, len(fetch.calls))
	}
	if res.Counts.Created != 4 {
		t.Fatalf("exhausting the picture budget must not block articles: %+v", res.Counts)
	}
}

func TestMPSyncRefusesWithoutPermissionOrCompleteConfiguration(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	src := &fakeMPSource{articles: []MPArticle{mpArticle(1, "<p>正文</p>")}}
	cases := map[string]func() (Scope, MPSyncConfig){
		"no manage permission": func() (Scope, MPSyncConfig) { v := scope; v.Manage = false; return v, cfg },
		"owner outside scope":  func() (Scope, MPSyncConfig) { c := cfg; c.Owner = "owner-b"; return scope, c },
		"no owner":             func() (Scope, MPSyncConfig) { c := cfg; c.Owner = ""; return scope, c },
		"no app id":            func() (Scope, MPSyncConfig) { c := cfg; c.AppID = ""; return scope, c },
		"invalid rights":       func() (Scope, MPSyncConfig) { c := cfg; c.Rights = "public"; return scope, c },
		"no actor":             func() (Scope, MPSyncConfig) { v := scope; v.Actor = 0; return v, cfg },
	}
	for name, build := range cases {
		sc, c := build()
		if _, err := s.SyncOfficialAccount(context.Background(), sc, c, src, nil, nil); err == nil {
			t.Errorf("%s: sync ran", name)
		}
	}
	if len(listContents(t, s)) != 0 {
		t.Fatal("a refused sync created content")
	}
	if _, _, err := s.MPSyncStatus(context.Background(), Scope{Tenant: scope.Tenant, Actor: 7, Owners: []string{"owner-a"}, View: true}, "owner-a"); err == nil {
		t.Fatal("status readable without manage permission")
	}
	if _, _, err := s.MPSyncStatus(context.Background(), scope, "owner-b"); err == nil {
		t.Fatal("status readable for another owner")
	}
}

func TestMPSyncRecordsSourceFailuresWithoutLeakingDetail(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	src := &fakeMPSource{err: &WechatError{Code: 48001}}
	if _, err := s.SyncOfficialAccount(context.Background(), scope, cfg, src, nil, nil); err == nil {
		t.Fatal("source failure hidden")
	}
	state, found, err := s.MPSyncStatus(context.Background(), scope, "owner-a")
	if err != nil || !found || state.OK || state.WechatCode != 48001 || state.Error == "" || strings.Contains(state.Error, "secret") {
		t.Fatalf("state: %+v %v %v", state, found, err)
	}
	src.err = errors.New("dial tcp 10.0.0.1:443: connection refused")
	_, _ = s.SyncOfficialAccount(context.Background(), scope, cfg, src, nil, nil)
	state, _, _ = s.MPSyncStatus(context.Background(), scope, "owner-a")
	if strings.Contains(state.Error, "10.0.0.1") || strings.Contains(state.Error, "dial") {
		t.Fatalf("internal detail reached the status: %q", state.Error)
	}
	// A later good run replaces the failure.
	src.err = nil
	src.articles = []MPArticle{mpArticle(1, "<p>正文</p>")}
	syncOnce(t, s, scope, cfg, src, nil)
	if state, _, _ = s.MPSyncStatus(context.Background(), scope, "owner-a"); !state.OK || state.Error != "" || state.WechatCode != 0 {
		t.Fatalf("stale failure kept: %+v", state)
	}
}

func TestMPSyncBoundsTitleSourceAndProblemList(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	long := mpArticle(1, "<p>正文</p>")
	long.Title = strings.Repeat("题", 200)
	long.URL = "https://evil.example/phish"
	other := mpArticle(2, "<p>正文二</p>")
	other.URL = "http://mp.weixin.qq.com/s/insecure"
	syncOnce(t, s, scope, cfg, &fakeMPSource{articles: []MPArticle{long, other}}, nil)
	for _, c := range listContents(t, s) {
		if len([]rune(c.Title)) > 80 || c.SourceURL != "" {
			t.Fatalf("title/source not bounded: %q %q", c.Title, c.SourceURL)
		}
	}
	many := []MPArticle{}
	for i := 100; i < 160; i++ {
		a := mpArticle(i, "<p></p>")
		many = append(many, a)
	}
	cfg.MaxArticles = 60
	res := syncOnce(t, s, scope, cfg, &fakeMPSource{articles: many}, nil)
	if res.Counts.Skipped != 60 || len(res.Problems) != maxMPProblems {
		t.Fatalf("problem list not bounded: skipped=%d listed=%d", res.Counts.Skipped, len(res.Problems))
	}
}

func TestMPSyncConcurrentRunsDoNotDuplicateContent(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	articles := []MPArticle{}
	for i := 0; i < 5; i++ {
		articles = append(articles, mpArticle(i, fmt.Sprintf("<p>正文 %d</p>", i)))
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.SyncOfficialAccount(context.Background(), scope, cfg, &fakeMPSource{articles: articles}, nil, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := len(listContents(t, s)); got != 5 {
		t.Fatalf("concurrent syncs left %d contents for 5 articles", got)
	}
}

func TestMPSyncArticleIDsAreNamespacedByAccount(t *testing.T) {
	s, scope, cfg := mpFixture(t)
	src := &fakeMPSource{articles: []MPArticle{mpArticle(1, "<p>正文</p>")}}
	syncOnce(t, s, scope, cfg, src, nil)
	cfg.AppID = "wx-other-account"
	syncOnce(t, s, scope, cfg, src, nil)
	if len(listContents(t, s)) != 2 {
		t.Fatal("two accounts sharing an article ID were merged")
	}
}
