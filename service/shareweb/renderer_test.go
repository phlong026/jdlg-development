package shareweb

import (
	"bytes"
	"strings"
	"testing"

	"service/tracking"
)

func TestShareWebRendererEscapesArticleTextAndBindsMediaToCode(t *testing.T) {
	content := tracking.Content{
		ID: "article-one", Version: 1, Title: "<script>title()</script>", Status: "published", BodyFormat: "article_v2",
		Body:  `{"schema":1,"blocks":[{"kind":"paragraph","text":"<img src=x onerror=alert(1)>"},{"kind":"image","media_id":"media-image","caption":"<b>caption</b>"},{"kind":"video","media_id":"media-video"}]}`,
		Media: []tracking.Asset{{ID: "media-image", Kind: "image"}, {ID: "media-video", Kind: "video"}},
	}
	var output bytes.Buffer
	if err := Render(&output, Page{Code: "opaque-code-123456", Content: content, Trackable: true}); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, unsafe := range []string{"<script>title()", "<img src=x onerror=alert(1)>", "<b>caption</b>"} {
		if strings.Contains(html, unsafe) {
			t.Fatalf("unescaped content %q in %s", unsafe, html)
		}
	}
	if !strings.Contains(html, "/api/v1/share-web/media/opaque-code-123456/media-image") || !strings.Contains(html, "/api/v1/share-web/media/opaque-code-123456/media-video") {
		t.Fatalf("media was not rebound to the validated share code: %s", html)
	}
	if !strings.Contains(html, `data-media-id="media-video"`) {
		t.Fatalf("video is missing its tracking identity: %s", html)
	}
}

func TestShareWebRendererRejectsPrivateOrDraftContent(t *testing.T) {
	for _, content := range []tracking.Content{
		{ID: "draft", Version: 1, Status: "draft", BodyFormat: "text", Body: "secret"},
		{ID: "private", Version: 1, Status: "published", BodyFormat: "private_note", Body: "secret"},
	} {
		if err := Render(&bytes.Buffer{}, Page{Code: "opaque-code-123456", Content: content}); err == nil {
			t.Fatalf("rendered inaccessible content %+v", content)
		}
	}
}

func TestShareWebRendererKeepsSanitizedRichTextAndAppendsLegacyMedia(t *testing.T) {
	content := tracking.Content{
		ID: "legacy-rich-article", Version: 2, Title: "旧格式文章", Status: "published", BodyFormat: "rich_text",
		Body:  `<h2>标题</h2><p><strong>重点</strong><script>alert(1)</script></p>`,
		Media: []tracking.Asset{{ID: "legacy-image-0001", Kind: "image"}, {ID: "legacy-video-0001", Kind: "video"}},
	}
	var output bytes.Buffer
	if err := Render(&output, Page{Code: "opaque-code-123456", Content: content}); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, wanted := range []string{"<h2>标题</h2>", "<strong>重点</strong>", "/legacy-image-0001", "/legacy-video-0001"} {
		if !strings.Contains(html, wanted) {
			t.Fatalf("missing %q in %s", wanted, html)
		}
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "alert(1)") {
		t.Fatalf("unsafe rich text in %s", html)
	}
}

func TestShareWebRendererAppliesArticleSpacingToMediaAndCaption(t *testing.T) {
	content := tracking.Content{
		ID: "spaced-article", Version: 1, Title: "排版文章", Status: "published", BodyFormat: "article_v2",
		Body:  `{"schema":1,"blocks":[{"kind":"image","media_id":"media-image-0001","caption":"平面图","style":{"align":"center","font_size":"small","spacing_before":12,"spacing_after":18,"border":"box"}}]}`,
		Media: []tracking.Asset{{ID: "media-image-0001", Kind: "image"}},
	}
	var output bytes.Buffer
	if err := Render(&output, Page{Code: "opaque-code-123456", Content: content}); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if !strings.Contains(html, `class="block center small box"`) || !strings.Contains(html, `margin-top:12px;margin-bottom:18px`) || !strings.Contains(html, `<figcaption>平面图</figcaption>`) {
		t.Fatalf("spacing or caption style missing: %s", html)
	}
}

func TestShareWebRendererUsesSanitizedLayoutAndLocalMediaURLs(t *testing.T) {
	content := tracking.Content{
		ID: "layout-article", Version: 1, Title: "原排版", Status: "published", BodyFormat: "article_v2",
		Body:  `{"schema":1,"layout_html":"<section style=\"padding:12px;background:url(https://evil.test/x)\"><p>前文</p><img data-media-id=\"layout-image-one\"><blockquote>引用<jd-video data-media-id=\"layout-video-one\"></jd-video></blockquote><script>alert(1)</script></section>","blocks":[{"kind":"paragraph","text":"兼容正文"}]}`,
		Media: []tracking.Asset{{ID: "layout-image-one", Kind: "image"}, {ID: "layout-video-one", Kind: "video"}},
	}
	var output bytes.Buffer
	if err := Render(&output, Page{Code: "opaque-code-123456", Content: content}); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, wanted := range []string{`class="original-layout"`, `<p>前文</p>`, `/api/v1/share-web/media/opaque-code-123456/layout-image-one`, `/api/v1/share-web/media/opaque-code-123456/layout-video-one`, `<video `} {
		if !strings.Contains(html, wanted) {
			t.Fatalf("missing %q in %s", wanted, html)
		}
	}
	if !strings.Contains(html, `data-media-id="layout-video-one"`) {
		t.Fatalf("layout video is missing its tracking identity: %s", html)
	}
	for _, unsafe := range []string{"evil.test", "alert(1)", "jd-video", `data-media-id="layout-image-one"`} {
		if strings.Contains(html, unsafe) {
			t.Fatalf("unsafe placeholder %q survived: %s", unsafe, html)
		}
	}
}

func TestShareWebRendererRejectsMissingLayoutMedia(t *testing.T) {
	content := tracking.Content{
		ID: "layout-missing", Version: 1, Title: "原排版", Status: "published", BodyFormat: "article_v2",
		Body: `{"schema":1,"layout_html":"<img data-media-id=\"layout-image-one\">","blocks":[{"kind":"paragraph","text":"兼容正文"}]}`,
	}
	if err := Render(&bytes.Buffer{}, Page{Code: "opaque-code-123456", Content: content}); err == nil {
		t.Fatal("rendered layout with missing media")
	}
}
