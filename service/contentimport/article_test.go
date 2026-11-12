package contentimport

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestParseArticleHTMLKeepsTextLazyImagesAndEmbeddedVideosInOrder(t *testing.T) {
	raw := `<!doctype html><html><head>
<meta property="og:title" content="春季案例"><meta name="author" content="华匠邦建科">
<meta name="gh_id" content="gh_12345678"><script>alert(1)</script></head><body>
<div class="rich_media_wrp"><div id="js_content" class="js_inner">
<h2 style="text-align:center">章节一</h2><p>第一段</p>
<img src="data:image/gif;base64,AAAA" data-src="https://mmbiz.qpic.cn/a/one.jpg">
<iframe class="video_iframe rich_pages" data-mpvid="wxv_1234567890" data-src="https://mp.weixin.qq.com/mp/readtemplate?t=pages/video_player_tmpl&amp;action=mpvideo&amp;vid=wxv_1234567890"></iframe>
<blockquote>重点引用</blockquote><ol><li>甲</li><li>乙</li></ol>
<video data-vid="local_video_123" src="https://mpvideo.qpic.cn/direct.mp4?vid=local_video_123"></video>
</div><div id="js_common_share_desc">不应重复抓取</div></div></body></html>`

	doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/abc", raw)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Source.Title != "春季案例" || doc.Source.Author != "华匠邦建科" || doc.SourceAccountID != "gh_12345678" || doc.SourceAccountName != "华匠邦建科" {
		t.Fatalf("metadata mismatch: %#v", doc)
	}
	wantKinds := []string{"heading", "paragraph", "image", "video", "quote", "list", "video"}
	var kinds []string
	for _, block := range doc.Body.Blocks {
		kinds = append(kinds, block.Kind)
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("block order %v, want %v; blocks=%#v", kinds, wantKinds, doc.Body.Blocks)
	}
	if doc.Body.Blocks[0].Level != 2 || doc.Body.Blocks[0].Style.Align != "center" {
		t.Fatalf("heading style not preserved: %#v", doc.Body.Blocks[0])
	}
	if len(doc.Source.Media) != 3 || doc.Source.Media[0].URL != "https://mmbiz.qpic.cn/a/one.jpg" || doc.Source.Media[1].ID != "wxv_1234567890" || doc.Source.Media[1].URL != "" {
		t.Fatalf("media mismatch: %#v", doc.Source.Media)
	}
	for i, index := range []int{2, 3, 6} {
		want := "remote-000" + string(rune('0'+i))
		if doc.Body.Blocks[index].MediaID != want {
			t.Fatalf("block %d media ref %q, want %q", index, doc.Body.Blocks[index].MediaID, want)
		}
	}
	if doc.ArchiveHTML != "" {
		t.Fatalf("unsafe archive emitted: %q", doc.ArchiveHTML)
	}
	if strings.Contains(strings.Join(doc.Warnings, " "), "script") {
		t.Fatalf("script should be silently discarded: %v", doc.Warnings)
	}
}

func TestParseArticleHTMLRejectsShareDescriptionWithoutBody(t *testing.T) {
	for name, body := range map[string]string{
		"standalone":         `<div id="js_common_share_desc">这只是分享摘要</div>`,
		"wrapper":            `<div class="rich_media_wrp"><div id="js_common_share_desc">这只是分享摘要</div></div>`,
		"empty body":         `<div id="js_content"></div><div id="js_common_share_desc">这只是分享摘要</div>`,
		"summary root":       `<div id="js_common_share_desc" class="js_inner">这只是分享摘要</div>`,
		"summary descendant": `<div id="js_common_share_desc"><div class="js_inner">这只是分享摘要</div></div>`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseArticleHTML("https://mp.weixin.qq.com/s/summary", `<meta property="og:title" content="文章标题">`+body)
			var resolved *ResolveError
			if !errors.As(err, &resolved) || resolved.Code != "article_body_missing" {
				t.Fatalf("share description must not count as article body: %v", err)
			}
		})
	}
}

func TestParseArticleHTMLFallbackExcludesShareDescription(t *testing.T) {
	doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/fallback", `<div class="rich_media_wrp"><p>完整正文</p><div id="js_common_share_desc">这只是分享摘要</div></div>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Body.Blocks) != 1 || doc.Body.Blocks[0].Text != "完整正文" || strings.Contains(doc.Body.LayoutHTML, "分享摘要") {
		t.Fatalf("share description leaked into body: %#v", doc.Body)
	}
}

func TestParseArticleHTMLFindsRealBodyAfterShareDescription(t *testing.T) {
	doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/fallback", `<div id="js_common_share_desc" class="js_inner"><div class="js_inner">这只是分享摘要</div></div><div class="js_inner"><p>完整正文</p></div>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Body.Blocks) != 1 || doc.Body.Blocks[0].Text != "完整正文" || strings.Contains(doc.Body.LayoutHTML, "分享摘要") {
		t.Fatalf("real fallback body lost or summary leaked: %#v", doc.Body)
	}
}

func TestParseArticleHTMLCompositeTextExcludesShareDescription(t *testing.T) {
	for name, body := range map[string]string{
		"heading":           `<h2>完整正文<span id="js_common_share_desc">这只是分享摘要</span></h2>`,
		"quote":             `<blockquote>完整正文<span id="js_common_share_desc">这只是分享摘要</span></blockquote>`,
		"list":              `<ul><li>完整正文<span id="js_common_share_desc">这只是分享摘要</span></li></ul>`,
		"summary list item": `<ul><li id="js_common_share_desc">这只是分享摘要</li><li>完整正文</li></ul>`,
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/composite", `<div id="js_content">`+body+`</div>`)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Body.Blocks) != 1 {
				t.Fatalf("expected one real body block: %#v", doc.Body)
			}
			block := doc.Body.Blocks[0]
			text := block.Text + strings.Join(block.Items, "")
			if text != "完整正文" || strings.Contains(doc.Body.LayoutHTML, "分享摘要") {
				t.Fatalf("summary leaked into composite body: %#v", doc.Body)
			}
		})
	}
}

func TestParseArticleHTMLUsesFallbackAndWarnsForUnknownIframe(t *testing.T) {
	doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/fallback", `<html><body><div class="rich_media_wrp"><p>备用正文</p><iframe src="https://example.com/embed"></iframe></div></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Body.Blocks) != 1 || doc.Body.Blocks[0].Text != "备用正文" {
		t.Fatalf("fallback body mismatch: %#v", doc.Body.Blocks)
	}
	if !reflect.DeepEqual(doc.Warnings, []string{"unsupported_iframe_removed"}) {
		t.Fatalf("warnings: %v", doc.Warnings)
	}
}

func TestParseArticleHTMLKeepsCustomWXVAndVideoSource(t *testing.T) {
	doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/media", `<div class="js_inner">
<mp-common-videosnap data-vid="wxv_abcdefgh1234"></mp-common-videosnap>
<video><source src="https://mpvideo.qpic.cn/direct.mp4?vid=direct_12345678"></video>
</div>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Source.Media) != 2 || doc.Source.Media[0].ID != "wxv_abcdefgh1234" || doc.Source.Media[1].ID != "url-"+stableURLHash("https://mpvideo.qpic.cn/direct.mp4?vid=direct_12345678") {
		t.Fatalf("media=%#v", doc.Source.Media)
	}
	if doc.Body.Blocks[0].Kind != "video" || doc.Body.Blocks[1].Kind != "video" {
		t.Fatalf("blocks=%#v", doc.Body.Blocks)
	}
}

func TestParseArticleHTMLDropsProfileWeappControlsAndActiveContent(t *testing.T) {
	doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/clean", `<div id="js_content">
<p>保留正文</p><mpprofile>公众号名片</mpprofile><mp-weapp>小程序卡片</mp-weapp>
<div class="video_controls">播放控件</div><script>危险脚本</script><style>.x{}</style>
</div>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Body.Blocks) != 1 || doc.Body.Blocks[0].Text != "保留正文" {
		t.Fatalf("active/noise content survived: %#v", doc.Body.Blocks)
	}
}

func TestParseArticleHTMLRejectsVideoThatCannotBeRepresented(t *testing.T) {
	_, err := ParseArticleHTML("https://mp.weixin.qq.com/s/incomplete", `<div id="js_content"><p>正文</p><video></video></div>`)
	var resolved *ResolveError
	if !errors.As(err, &resolved) || resolved.Code != "unsupported_video" {
		t.Fatalf("error=%v", err)
	}
}

func TestParseArticleHTMLOnlyTreatsKnownVideoElementsAsEmbeds(t *testing.T) {
	doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/spoof", `<div id="js_content">
<script data-vid="wxv_script12345">脚本文字</script><object data-mpvid="wxv_object12345">对象文字</object>
<a data-vid="wxv_anchor12345">链接正文</a><div data-mpvid="wxv_divfake12345"><p>容器正文</p></div>
<mp-common-videosnap data-vid="wxv_realvideo123"></mp-common-videosnap>
</div>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Source.Media) != 1 || doc.Source.Media[0].ID != "wxv_realvideo123" {
		t.Fatalf("spoofed video attribute captured: %#v", doc.Source.Media)
	}
	var text strings.Builder
	for _, block := range doc.Body.Blocks {
		text.WriteString(block.Text)
	}
	if got := text.String(); !strings.Contains(got, "链接正文") || !strings.Contains(got, "容器正文") || strings.Contains(got, "脚本文字") || strings.Contains(got, "对象文字") {
		t.Fatalf("body text=%q blocks=%#v", got, doc.Body.Blocks)
	}
}

func TestParseArticleHTMLRejectsRiskAndUnavailablePages(t *testing.T) {
	for name, test := range map[string][2]string{
		"captcha": {`<html><body><div id="js_content">当前环境异常，请完成验证码</div></body></html>`, "risk_control"},
		"deleted": {`<html><body><div id="js_content">该内容已被发布者删除</div></body></html>`, "source_unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseArticleHTML("https://mp.weixin.qq.com/s/x", test[0])
			var resolved *ResolveError
			if !errors.As(err, &resolved) || resolved.Code != test[1] {
				t.Fatalf("error=%v, want %s", err, test[1])
			}
		})
	}
}

type stubArticleFetcher struct {
	html string
	err  error
}

func (f stubArticleFetcher) FetchArticle(context.Context, string) ([]byte, error) {
	return []byte(f.html), f.err
}

type stubVideoCapture struct {
	videos []CaptureVideo
	err    error
	gotIDs []string
}

func (c *stubVideoCapture) Capture(_ context.Context, _ string, ids []string) ([]CaptureVideo, error) {
	c.gotIDs = append([]string(nil), ids...)
	return c.videos, c.err
}

func TestArticleResolverRequiresEveryEmbeddedVideo(t *testing.T) {
	html := `<div id="js_content"><iframe data-mpvid="wxv_11111111"></iframe><p>中间</p><iframe data-vid="wxv_22222222"></iframe></div>`
	capture := &stubVideoCapture{videos: []CaptureVideo{{ID: "wxv_11111111", URL: "https://mpvideo.qpic.cn/a.mp4?vid=wxv_11111111"}}}
	resolver := ArticleResolver{Fetcher: stubArticleFetcher{html: html}, VideoCapture: capture}
	_, err := resolver.ResolveDocument(context.Background(), "https://mp.weixin.qq.com/s/two")
	var resolved *ResolveError
	if !errors.As(err, &resolved) || resolved.Code != "missing_video" {
		t.Fatalf("error=%v", err)
	}
	if !reflect.DeepEqual(capture.gotIDs, []string{"wxv_11111111", "wxv_22222222"}) {
		t.Fatalf("capture ids=%v", capture.gotIDs)
	}
}

func TestArticleResolverReturnsBrowserRequiredWithoutCaptureWorker(t *testing.T) {
	resolver := ArticleResolver{Fetcher: stubArticleFetcher{html: `<div id="js_content"><iframe data-mpvid="wxv_11111111"></iframe></div>`}}
	_, err := resolver.ResolveDocument(context.Background(), "https://mp.weixin.qq.com/s/video")
	var resolved *ResolveError
	if !errors.As(err, &resolved) || resolved.Code != "browser_required" {
		t.Fatalf("error=%v", err)
	}
}

func TestArticleImageIdentityDoesNotTrustLocalDOMIDs(t *testing.T) {
	doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/example", `<meta property="og:title" content="图片"><div id="js_content"><p>正文</p><img data-id="image_local_123" data-src="https://mmbiz.qpic.cn/one.jpg"><img data-id="image_local_123" data-src="https://mmbiz.qpic.cn/two.jpg"></div>`)
	if err != nil || len(doc.Source.Media) != 2 || doc.Source.Media[0].ID == doc.Source.Media[1].ID {
		t.Fatalf("images merged: %+v %v", doc.Source.Media, err)
	}
}
func TestArticleMissingImageCannotSilentlyProduceCompleteDraft(t *testing.T) {
	for _, image := range []string{`<img>`, `<img src="https://external.example/a.jpg">`} {
		doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/example", `<meta property="og:title" content="图片"><div id="js_content"><p>正文</p>`+image+`</div>`)
		if err == nil && len(doc.Warnings) == 0 {
			t.Fatalf("missing image accepted: %s", image)
		}
	}
}

func TestParseArticleHTMLRejectsOversizedTextWithoutTruncating(t *testing.T) {
	tests := map[string]string{
		"paragraph": `<p>` + strings.Repeat("p", 10001) + `</p>`,
		"heading":   `<h2>` + strings.Repeat("h", 501) + `</h2>`,
		"quote":     `<blockquote>` + strings.Repeat("q", 10001) + `</blockquote>`,
		"list item": `<ul><li>` + strings.Repeat("i", 2001) + `</li></ul>`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseArticleHTML("https://mp.weixin.qq.com/s/oversized", `<div id="js_content">`+body+`</div>`)
			var resolved *ResolveError
			if !errors.As(err, &resolved) || resolved.Code != "article_too_large" {
				t.Fatalf("error=%v, want article_too_large", err)
			}
		})
	}
}

func TestParseArticleHTMLPreservesNestedMediaInSanitizedLayout(t *testing.T) {
	tests := map[string]string{
		"heading image": `<h2>标题<img data-src="https://mmbiz.qpic.cn/a.jpg"></h2>`,
		"quote video":   `<blockquote>引用<video src="https://mpvideo.qpic.cn/a.mp4?vid=direct_12345678"></video></blockquote>`,
		"list iframe":   `<ul><li>列表<iframe data-mpvid="wxv_1234567890"></iframe></li></ul>`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/nested", `<div id="js_content"><section style="padding:12px;border:1px solid #ddd">`+body+`</section></div>`)
			if err != nil {
				t.Fatal(err)
			}
			if doc.Body.LayoutHTML == "" || len(doc.Source.Media) != 1 || !strings.Contains(doc.Body.LayoutHTML, `data-media-id="remote-0000"`) {
				t.Fatalf("nested layout/media missing: %#v", doc)
			}
			if strings.Contains(doc.Body.LayoutHTML, "mp.weixin.qq.com") || strings.Contains(doc.Body.LayoutHTML, "mpvideo.qpic.cn") {
				t.Fatalf("external resource survived: %s", doc.Body.LayoutHTML)
			}
			if !strings.Contains(doc.Body.LayoutHTML, `style="padding:12px;border:1px solid #ddd"`) {
				t.Fatalf("safe box style lost: %s", doc.Body.LayoutHTML)
			}
		})
	}
}

func TestParseArticleHTMLSanitizesEventsScriptsAndUnsafeCSS(t *testing.T) {
	doc, err := ParseArticleHTML("https://mp.weixin.qq.com/s/safe", `<div id="js_content"><section onclick="evil()" style="color:#123; background:url(https://evil.test/a); position:fixed"><p>正文<script>alert(1)</script></p><img data-src="https://mmbiz.qpic.cn/a.jpg" onerror="evil()"></section></div>`)
	if err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{"onclick", "onerror", "evil.test", "position", "script", "alert(1)", "data-src", " src="} {
		if strings.Contains(doc.Body.LayoutHTML, unsafe) {
			t.Fatalf("unsafe value %q survived: %s", unsafe, doc.Body.LayoutHTML)
		}
	}
	if !strings.Contains(doc.Body.LayoutHTML, `style="color:#123"`) || !strings.Contains(doc.Body.LayoutHTML, `<img data-media-id="remote-0000">`) {
		t.Fatalf("safe layout lost: %s", doc.Body.LayoutHTML)
	}
}

func TestParseArticleHTMLRejectsAggregateDocumentLimits(t *testing.T) {
	var tooManyListItems strings.Builder
	for range 101 {
		tooManyListItems.WriteString(`<li>项</li>`)
	}
	var tooManyMedia strings.Builder
	for index := range 51 {
		tooManyMedia.WriteString(`<img data-src="https://mmbiz.qpic.cn/` + strconv.Itoa(index) + `.jpg">`)
	}
	tests := map[string]string{
		"list items": `<ul>` + tooManyListItems.String() + `</ul>`,
		"media":      tooManyMedia.String(),
		"blocks":     strings.Repeat(`<p>正文</p>`, 501),
		"body json":  strings.Repeat(`<p>`+strings.Repeat("x", 9500)+`</p>`, 11),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseArticleHTML("https://mp.weixin.qq.com/s/aggregate", `<div id="js_content">`+body+`</div>`)
			var resolved *ResolveError
			if !errors.As(err, &resolved) || resolved.Code != "article_too_large" {
				t.Fatalf("error=%v, want article_too_large", err)
			}
		})
	}
}
