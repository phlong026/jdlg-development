package notedocx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"
)

func tinyPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func unzipDocument(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, file := range reader.File {
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		out[file.Name], err = io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func assertXML(t *testing.T, data []byte) {
	t.Helper()
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		if _, err := decoder.Token(); err == io.EOF {
			return
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildCreatesCompletePackageWithOrderedArticleAndImage(t *testing.T) {
	data, err := Build(context.Background(), Document{
		Title:      "张宅方案",
		BodyFormat: "article_v2",
		Body:       `{"schema":1,"blocks":[{"kind":"heading","text":"设计说明","level":2},{"kind":"paragraph","text":"第一段"},{"kind":"image","media_id":"image-0001","caption":"门厅"},{"kind":"video","media_id":"video-0001","caption":"演示"},{"kind":"list","ordered":true,"items":["第一项","第二项"]}]}`,
		Media: []Media{
			{ID: "image-0001", Kind: "image", MIME: "image/png", Width: 2000, Height: 1000, Data: tinyPNG(t, 2, 1)},
			{ID: "video-0001", Kind: "video", MIME: "video/mp4"},
		},
	}, Limits{MaxOutputBytes: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	parts := unzipDocument(t, data)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/_rels/document.xml.rels", "word/media/image1.png"} {
		if len(parts[name]) == 0 {
			t.Fatalf("missing package part %s", name)
		}
	}
	for name, part := range parts {
		if strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels") {
			assertXML(t, part)
		}
	}
	document := string(parts["word/document.xml"])
	positions := []int{
		strings.Index(document, "设计说明"),
		strings.Index(document, "第一段"),
		strings.Index(document, `r:embed="rIdImage1"`),
		strings.Index(document, "门厅"),
		strings.Index(document, "视频请在甲第方案中查看"),
		strings.Index(document, "1. 第一项"),
	}
	for i, position := range positions {
		if position < 0 || i > 0 && position <= positions[i-1] {
			t.Fatalf("semantic order lost: %v", positions)
		}
	}
	if strings.Contains(document, "video-0001") || strings.Contains(document, "http") && strings.Contains(document, "private") {
		t.Fatal("private media identifier or URL leaked into document")
	}
	rels := string(parts["word/_rels/document.xml.rels"])
	if !strings.Contains(rels, `Id="rIdImage1"`) || !strings.Contains(rels, `Target="media/image1.png"`) {
		t.Fatal("image relationship missing")
	}
}

func TestBuildPrefersLayoutHTMLSemanticOrder(t *testing.T) {
	body := `{"schema":1,"layout_html":"<section><p>布局第一段</p><img data-media-id=\"image-0001\"><h2>布局第二段</h2><jd-video data-media-id=\"video-0001\"></jd-video></section>","blocks":[{"kind":"paragraph","text":"块备用内容"}]}`
	data, err := Build(context.Background(), Document{
		Title:      "原排版",
		BodyFormat: "article_v2",
		Body:       body,
		Media: []Media{
			{ID: "image-0001", Kind: "image", MIME: "image/png", Data: tinyPNG(t, 1, 1)},
			{ID: "video-0001", Kind: "video", MIME: "video/mp4"},
		},
	}, Limits{MaxOutputBytes: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	document := string(unzipDocument(t, data)["word/document.xml"])
	if !strings.Contains(document, "布局第一段") || !strings.Contains(document, "布局第二段") || strings.Contains(document, "块备用内容") {
		t.Fatalf("layout_html was not preferred: %s", document)
	}
}

func TestBuildRejectsMissingImageInvalidImageLimitAndCanceledContext(t *testing.T) {
	tests := []struct {
		name string
		doc  Document
		max  int64
	}{
		{name: "missing", doc: Document{Title: "x", BodyFormat: "article_v2", Body: `{"schema":1,"blocks":[{"kind":"image","media_id":"missing-01"}]}`}, max: 1 << 20},
		{name: "invalid", doc: Document{Title: "x", BodyFormat: "article_v2", Body: `{"schema":1,"blocks":[{"kind":"image","media_id":"image-0001"}]}`, Media: []Media{{ID: "image-0001", Kind: "image", MIME: "image/png", Data: []byte("not an image")}}}, max: 1 << 20},
		{name: "output limit", doc: Document{Title: "x", BodyFormat: "plain", Body: strings.Repeat("x", 2048)}, max: 256},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Build(context.Background(), test.doc, Limits{MaxOutputBytes: test.max}); err == nil {
				t.Fatal("expected failure")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, Document{Title: "x", BodyFormat: "plain", Body: "body"}, Limits{MaxOutputBytes: 1 << 20}); err == nil {
		t.Fatal("canceled build succeeded")
	}
}

func TestBuildPreservesRichTextParagraphsListsAndEscapedCharacters(t *testing.T) {
	data, err := Build(context.Background(), Document{Title: "方案 & 版本 <一>", BodyFormat: "rich_text", Body: `<h2>设计说明</h2><p>第一行<br>第二行</p><ol><li>入口</li><li>客厅</li></ol><blockquote>保留确认后的功能</blockquote>`}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	xml := string(unzipDocument(t, data)["word/document.xml"])
	for _, text := range []string{"方案 &amp; 版本 &lt;一&gt;", "第一行", "<w:br/>", "第二行", "1. 入口", "2. 客厅", "保留确认后的功能"} {
		if !strings.Contains(xml, text) {
			t.Errorf("missing %q", text)
		}
	}
}

func TestBuildRejectsExternalHTMLAndImageMetadataMismatch(t *testing.T) {
	for _, doc := range []Document{
		{Title: "external", BodyFormat: "article_v2", Body: `{"schema":1,"layout_html":"<p>正文</p><img src=\"https://example.org/private\">","blocks":[{"kind":"paragraph","text":"正文"}]}`},
		{Title: "script", BodyFormat: "rich_text", Body: `<p>正文</p><script>secret</script>`},
		{Title: "mime", BodyFormat: "plain", Body: "正文", Media: []Media{{ID: "media-0001", Kind: "image", MIME: "image/jpeg", Data: tinyPNG(t, 1, 1)}}},
		{Title: "control", BodyFormat: "plain", Body: "正文\x00"},
	} {
		if _, err := Build(context.Background(), doc, Limits{}); err == nil {
			t.Fatalf("invalid document accepted: %s", doc.Title)
		}
	}
}

func TestBuildRejectsImagePixelBombBeforeDecoding(t *testing.T) {
	data := tinyPNG(t, 1, 1)
	binary.BigEndian.PutUint32(data[16:20], 20000)
	binary.BigEndian.PutUint32(data[20:24], 20000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	_, err := Build(context.Background(), Document{Title: "large", BodyFormat: "plain", Body: "正文", Media: []Media{{ID: "image-0001", Kind: "image", MIME: "image/png", Data: data}}}, Limits{})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("pixel limit must be a deterministic size failure, got %v", err)
	}
}

func solidWebP(t *testing.T) ([]byte, []byte) {
	t.Helper()
	// Synthetic 2048 x 2048 solid-color image; its PNG is much larger than WebP.
	data, err := base64.StdEncoding.DecodeString("UklGRtQAAABXRUJQVlA4TMgAAAAv/8f/AQdQ+MIXvv8BIUHi/+9mIvqf8Z///Oc///nPf/7zn//85z//+c9//vOf//znP//5z3/+85///Oc///nPf/7zn//85z//+c9//vOf//znP//5z3/+85///Oc///nPf/7zn//85z//+c9//vOf//znP//5z3/+85///Oc///nPf/7zn//85z//+c9//vOf//znP//5z3/+85///Oc///nPf/7zn//85z//+c9//vOf//znP//5z3/+85///Oc///nPf/7vAQ==")
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var converted bytes.Buffer
	if err = png.Encode(&converted, decoded); err != nil {
		t.Fatal(err)
	}
	if converted.Len() <= len(data)*2 {
		t.Fatal("fixture must exercise compressed-image expansion")
	}
	return data, converted.Bytes()
}

func TestBuildLimitsEmbeddedImagesAfterWebPConversion(t *testing.T) {
	webp, pngData := solidWebP(t)
	max := int64(len(pngData)*2 - 1)
	for _, first := range []Media{
		{ID: "image-0001", Kind: "image", MIME: "image/webp", Data: webp},
		{ID: "image-0001", Kind: "image", MIME: "image/png", Data: pngData},
	} {
		t.Run(first.MIME, func(t *testing.T) {
			doc := Document{Title: "expanded images", BodyFormat: "plain", Body: "正文", Media: []Media{
				first, {ID: "image-0002", Kind: "image", MIME: "image/webp", Data: webp},
			}}
			if int64(len(first.Data)+len(webp)) >= max {
				t.Fatal("raw inputs must fit the budget")
			}
			complete, err := Build(context.Background(), doc, Limits{})
			if err != nil || int64(len(complete)) >= max {
				t.Fatalf("compressed package must fit, independently of image-memory limit: bytes=%d error=%v", len(complete), err)
			}
			data, err := Build(context.Background(), doc, Limits{MaxOutputBytes: max})
			if !errors.Is(err, ErrLimit) || len(data) != 0 {
				t.Fatalf("expanded aggregate above %d must fail without output, got bytes=%d error=%v", max, len(data), err)
			}
		})
	}
}

func TestBuildChargesRepeatedImageReferencesOnce(t *testing.T) {
	webp, converted := solidWebP(t)
	doc := Document{Title: "repeated image", BodyFormat: "article_v2", Body: `{"schema":1,"blocks":[{"kind":"image","media_id":"image-0001"},{"kind":"image","media_id":"image-0001"}]}`, Media: []Media{
		{ID: "image-0001", Kind: "image", MIME: "image/webp", Data: webp},
	}}
	data, err := Build(context.Background(), doc, Limits{MaxOutputBytes: int64(len(converted))})
	if err != nil {
		t.Fatal(err)
	}
	parts := unzipDocument(t, data)
	if !bytes.Equal(parts["word/media/image1.png"], converted) || parts["word/media/image2.png"] != nil {
		t.Fatal("repeated image should occupy one embedded image")
	}
	if strings.Count(string(parts["word/document.xml"]), `r:embed="rIdImage1"`) != 2 {
		t.Fatal("both references must remain visible")
	}
}

func TestPrepareImageHonorsRemainingBudget(t *testing.T) {
	webp, converted := solidWebP(t)
	for _, asset := range []Media{
		{Kind: "image", MIME: "image/webp", Data: webp},
		{Kind: "image", MIME: "image/png", Data: converted},
	} {
		t.Run(asset.MIME, func(t *testing.T) {
			part, err := prepareImage(context.Background(), asset, 1, int64(len(converted)-1))
			if !errors.Is(err, ErrLimit) || len(part.data) != 0 {
				t.Fatalf("image must fit remaining embedded budget: bytes=%d error=%v", len(part.data), err)
			}
		})
	}
}
