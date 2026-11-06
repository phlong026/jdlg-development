package tracking

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestArticleDocumentKeepsMediaOrderAndNormalizesStyles(t *testing.T) {
	input := `{"schema":1,"blocks":[{"kind":"paragraph","text":"第一段","style":{"align":"center","font_size":"large","spacing_before":12,"spacing_after":18,"border":"box"}},{"kind":"image","media_id":"asset-0001","caption":"平面图"},{"kind":"paragraph","text":"第二段"}]}`
	got, format, err := normalizeBody(input, "article_v2")
	if err != nil || format != "article_v2" {
		t.Fatalf("format=%s error=%v", format, err)
	}
	var doc ArticleDocument
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Blocks) != 3 || doc.Blocks[0].Kind != "paragraph" || doc.Blocks[1].Kind != "image" || doc.Blocks[2].Text != "第二段" {
		t.Fatalf("图文顺序丢失: %+v", doc.Blocks)
	}
	if doc.Blocks[0].Style.Align != "center" || doc.Blocks[0].Style.FontSize != "large" || doc.Blocks[0].Style.SpacingBefore != 12 || doc.Blocks[0].Style.SpacingAfter != 18 || doc.Blocks[0].Style.Border != "box" {
		t.Fatalf("白名单样式丢失: %+v", doc.Blocks[0].Style)
	}
	again, _, err := normalizeBody(got, "article_v2")
	if err != nil || again != got {
		t.Fatalf("归一化非幂等: %q %v", again, err)
	}
}

func TestArticlePresentationRoundTripAndShape(t *testing.T) {
	for _, presentation := range []string{"article", "gallery", "video"} {
		kind := "image"
		if presentation == "video" {
			kind = "video"
		}
		input := `{"schema":1,"presentation":"` + presentation + `","blocks":[{"kind":"paragraph","text":"独立完整文案"},{"kind":"` + kind + `","media_id":"asset-0001"}]}`
		got, _, err := normalizeBody(input, "article_v2")
		if err != nil || !strings.Contains(got, `"presentation":"`+presentation+`"`) {
			t.Fatalf("呈现类型未保留: %s %v", got, err)
		}
	}
	for _, input := range []string{
		`{"schema":1,"presentation":"unknown","blocks":[{"kind":"paragraph","text":"正文"}]}`,
		`{"schema":1,"presentation":"gallery","blocks":[{"kind":"video","media_id":"asset-0001"}]}`,
		`{"schema":1,"presentation":"video","blocks":[{"kind":"image","media_id":"asset-0001"}]}`,
		`{"schema":1,"presentation":"gallery","blocks":[{"kind":"image","media_id":"asset-0001"},{"kind":"paragraph","text":"图间说明"},{"kind":"image","media_id":"asset-0002"}]}`,
	} {
		if _, _, err := normalizeBody(input, "article_v2"); err == nil {
			t.Fatalf("接受了冲突的呈现类型: %s", input)
		}
	}
}

func TestContentPresentationKeepsMixedArticleAndSeparatesGallery(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"schema":1,"blocks":[{"kind":"paragraph","text":"视频文案"},{"kind":"video","media_id":"asset-0001"}]}`, "video"},
		{`{"schema":1,"blocks":[{"kind":"paragraph","text":"图集文案"},{"kind":"image","media_id":"asset-0001"},{"kind":"image","media_id":"asset-0002"}]}`, "gallery"},
		{`{"schema":1,"blocks":[{"kind":"heading","level":2,"text":"空间介绍"},{"kind":"video","media_id":"asset-0001"}]}`, "article"},
		{`{"schema":1,"presentation":"article","blocks":[{"kind":"image","media_id":"asset-0001"}]}`, "article"},
	} {
		c := Content{Type: "article", BodyFormat: "article_v2", Body: tc.body, Media: []Asset{{Kind: "video"}}}
		if got := ContentPresentation(c); got != tc.want {
			t.Fatalf("got=%s want=%s", got, tc.want)
		}
		category := tc.want
		if got := contentWorkbenchCategory(c); got != category {
			t.Fatalf("category=%s want=%s", got, category)
		}
	}
}

func TestPresentationSurvivesDraftPreviewAndPublishedVersion(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	asset := Asset{ID: "asset-layout", Kind: "image", URL: "/api/v1/miniprogram/tracking/media/asset-layout", Width: 1200, Height: 1600, MIME: "image/jpeg"}
	if err := s.AddMedia(ctx, scope, asset, "owner-a"); err != nil {
		t.Fatal(err)
	}
	body := `{"schema":1,"presentation":"gallery","blocks":[{"kind":"image","media_id":"asset-layout"},{"kind":"paragraph","text":"整条图集文案"}]}`
	saved, err := s.SaveContent(ctx, scope, "", Draft{Title: "图集方案", Body: body, BodyFormat: "article_v2", Rights: "own", Owner: "owner-a", CoverID: asset.ID, MediaIDs: []string{asset.ID}})
	if err != nil || saved.Presentation != "gallery" {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	preview, err := s.Preview(ctx, scope, saved.ID, saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(ctx, scope, saved.ID, saved.Revision, preview["preview_token"].(string), "publish-gallery-layout"); err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadContent(ctx, saved.ID, 1)
	if err != nil || read.Presentation != "gallery" || !strings.Contains(read.Body, `"presentation":"gallery"`) {
		t.Fatalf("read=%+v err=%v", read, err)
	}
	candidates, err := s.NativeReferenceCandidates(ctx, scope, "", 20)
	if err != nil || len(candidates.Items) != 1 || candidates.Items[0].Presentation != "gallery" {
		t.Fatalf("候选作品未保留呈现类型: %+v err=%v", candidates.Items, err)
	}
	collection, err := s.SaveProjectCollection(ctx, scope, "", collectionInput(scope.Owners[0], "gallery-collection", read))
	if err != nil || len(collection.Items) != 1 || collection.Items[0].Presentation != "gallery" {
		t.Fatalf("合集目录未保留呈现类型: %+v err=%v", collection.Items, err)
	}
}

func TestReadContentBackfillsPresentationForLegacyVersion(t *testing.T) {
	s, rows, _, now := reviewFixture()
	legacy := Content{
		ID: "legacy-gallery", Owner: "one", Status: "published", Version: 1, Type: "article",
		BodyFormat: "article_v2", Body: `{"schema":1,"blocks":[{"kind":"image","media_id":"asset-0001"},{"kind":"image","media_id":"asset-0002"},{"kind":"paragraph","text":"完整图集文案"}]}`,
	}
	if err := rows.Put(record("content", legacy.ID, legacy.Owner, now, legacy)); err != nil {
		t.Fatal(err)
	}
	if err := rows.Put(record("version", contentKey(legacy.ID, 1), legacy.Owner, now, legacy)); err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadContent(context.Background(), legacy.ID, 1)
	if err != nil || read.Presentation != "gallery" {
		t.Fatalf("legacy presentation=%q err=%v", read.Presentation, err)
	}
	var stored Content
	stored, err = decode[Content](rows, "version", contentKey(legacy.ID, 1))
	if err != nil || stored.Presentation != "" {
		t.Fatalf("read-time backfill must not rewrite the immutable version: %+v err=%v", stored, err)
	}
}

func TestArticleDocumentRejectsInvalidShapeAndLimits(t *testing.T) {
	invalid := []string{
		`{"schema":1,"script":"evil()","blocks":[{"kind":"paragraph","text":"正文"}]}`,
		`{"schema":1,"blocks":[{"kind":"paragraph","text":"正文","onclick":"evil()"}]}`,
		`{"schema":1,"blocks":[{"kind":"paragraph","text":"正文","style":{"color":"red"}}]}`,
		`{"schema":2,"blocks":[{"kind":"paragraph","text":"正文"}]}`,
		`{"schema":1,"blocks":[]}`,
		`{"schema":1,"blocks":[{"kind":"paragraph","text":" "}]}`,
		`{"schema":1,"blocks":[{"kind":"embed","url":"https://evil.test/frame"}]}`,
		`{"schema":1,"blocks":[{"kind":"heading","level":1,"text":"越界标题"}]}`,
		`{"schema":1,"blocks":[{"kind":"image","media_id":"bad/id"}]}`,
		`{"schema":1,"blocks":[{"kind":"paragraph","text":"正文","style":{"align":"absolute","spacing_after":999}}]}`,
	}
	for _, body := range invalid {
		if _, _, err := normalizeBody(body, "article_v2"); err == nil {
			t.Fatalf("接受了无效文档: %s", body)
		}
	}
	blocks := make([]ArticleBlock, 501)
	for i := range blocks {
		blocks[i] = ArticleBlock{Kind: "paragraph", Text: "正文"}
	}
	body, _ := json.Marshal(ArticleDocument{Schema: 1, Blocks: blocks})
	if _, _, err := normalizeBody(string(body), "article_v2"); err == nil {
		t.Fatal("接受了超长文档")
	}
}

func TestArticleDocumentRequiresAuthorizedMediaAndPreservesRepeatedReferences(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	for _, asset := range []Asset{
		{ID: "asset-0001", Kind: "image", URL: "/api/v1/miniprogram/tracking/media/asset-0001", Width: 1200, Height: 800, MIME: "image/jpeg"},
		{ID: "asset-0002", Kind: "video", URL: "/api/v1/miniprogram/tracking/media/asset-0002", Width: 1280, Height: 720, MIME: "video/mp4"},
		{ID: "asset-extra", Kind: "image", URL: "/api/v1/miniprogram/tracking/media/asset-extra", Width: 640, Height: 480, MIME: "image/jpeg"},
	} {
		if err := s.AddMedia(ctx, scope, asset, "owner-a"); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"schema":1,"blocks":[{"kind":"image","media_id":"asset-0001"},{"kind":"paragraph","text":"中间"},{"kind":"image","media_id":"asset-0001"},{"kind":"video","media_id":"asset-0002"}]}`
	saved, err := s.SaveContent(ctx, scope, "", Draft{Title: "原排版", Body: body, BodyFormat: "article_v2", Rights: "own", Owner: "owner-a", MediaIDs: []string{"asset-0001", "asset-0002", "asset-extra"}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.BodyFormat != "article_v2" || strings.Count(saved.Body, `"media_id":"asset-0001"`) != 2 || len(saved.Media) != 2 {
		t.Fatalf("重复引用或媒体归一化错误: %+v", saved)
	}
	preview, err := s.Preview(ctx, scope, saved.ID, saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(ctx, scope, saved.ID, saved.Revision, preview["preview_token"].(string), "publish-article-v2"); err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadContent(ctx, saved.ID, 1)
	if err != nil || read.Body != saved.Body || read.BodyFormat != "article_v2" {
		t.Fatalf("发布版本未保留原排版文档: %+v %v", read, err)
	}
	if len(read.Media) != 2 {
		t.Fatalf("未引用媒体被写入公开版本: %+v", read.Media)
	}
	_, err = s.Media(ctx, nil, "asset-extra")
	requireQueryError(t, err, 404)
	for name, draft := range map[string]Draft{
		"missing authorization": {Title: "缺失授权", Body: `{"schema":1,"blocks":[{"kind":"image","media_id":"asset-0001"}]}`, BodyFormat: "article_v2", Rights: "own", Owner: "owner-a"},
		"wrong media kind":      {Title: "类型不符", Body: `{"schema":1,"blocks":[{"kind":"video","media_id":"asset-0001"}]}`, BodyFormat: "article_v2", Rights: "own", Owner: "owner-a", MediaIDs: []string{"asset-0001"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.SaveContent(ctx, scope, "", draft); err == nil {
				t.Fatal("应拒绝不完整媒体文档")
			}
		})
	}
}

func TestArticleDocumentKeepsLegacyFormats(t *testing.T) {
	for _, tc := range []struct{ body, format, want string }{
		{"<b>literal</b>", "plain", "<b>literal</b>"},
		{"<p><strong>正文</strong></p>", "rich_text", "<p><strong>正文</strong></p>"},
	} {
		got, format, err := normalizeBody(tc.body, tc.format)
		if err != nil || got != tc.want || format != tc.format {
			t.Fatalf("旧格式兼容破坏: %q %q %v", got, format, err)
		}
	}
}

func TestArticleDocumentNormalizesSafeLayoutAndRejectsExternalCapabilities(t *testing.T) {
	body := `{"schema":1,"layout_html":"<section style=\"text-align:center;color:#123;background-image:url(https://evil.test/a.png)\" onclick=\"evil()\"><p>正文</p><img data-media-id=\"asset-0001\" src=\"https://evil.test/a.png\"><script>alert(1)</script></section>","blocks":[{"kind":"paragraph","text":"正文"}]}`
	got, _, err := normalizeBody(body, "article_v2")
	if err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{"onclick", "evil.test", "background-image", "script", "alert(1)", " src="} {
		if strings.Contains(got, unsafe) {
			t.Fatalf("unsafe layout survived: %s", got)
		}
	}
	var document ArticleDocument
	if err := json.Unmarshal([]byte(got), &document); err != nil || document.LayoutHTML != `<section style="text-align:center;color:#123"><p>正文</p><img data-media-id="asset-0001"></section>` {
		t.Fatalf("safe layout lost: %s err=%v", got, err)
	}
	again, _, err := normalizeBody(got, "article_v2")
	if err != nil || again != got {
		t.Fatalf("layout round trip changed: %s err=%v", again, err)
	}
	refs, err := articleMediaReferences(got)
	if err != nil || refs["asset-0001"] != "image" {
		t.Fatalf("layout media refs=%v err=%v", refs, err)
	}
}

func TestArticleDocumentLayoutMediaMustBeAuthorizedAndTyped(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	asset := Asset{ID: "asset-0001", Kind: "image", URL: "/api/v1/miniprogram/tracking/media/asset-0001", Width: 1200, Height: 800, MIME: "image/jpeg"}
	if err := s.AddMedia(ctx, scope, asset, "owner-a"); err != nil {
		t.Fatal(err)
	}
	base := Draft{Title: "原排版", BodyFormat: "article_v2", Rights: "own", Owner: "owner-a"}
	for name, layout := range map[string]string{
		"missing authorization": `<img data-media-id="asset-0001">`,
		"wrong kind":            `<jd-video data-media-id="asset-0001"></jd-video>`,
		"missing resource":      `<img data-media-id="asset-missing">`,
	} {
		t.Run(name, func(t *testing.T) {
			draft := base
			draft.Body = `{"schema":1,"layout_html":` + strconv.Quote(layout) + `,"blocks":[{"kind":"paragraph","text":"正文"}]}`
			if name != "missing authorization" {
				draft.MediaIDs = []string{"asset-0001"}
			}
			if _, err := s.SaveContent(ctx, scope, "", draft); err == nil {
				t.Fatal("应拒绝未授权、错误类型或缺失的 layout 媒体")
			}
		})
	}
}
