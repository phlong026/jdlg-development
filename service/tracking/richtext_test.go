package tracking

import (
	"context"
	"testing"
)

func TestRichTextAllowlist(t *testing.T) {
	cases := []struct{ name, input, want string }{
		{"formatting", `<h2>标题</h2><p><strong>粗体</strong><br><em>斜体</em></p><h3>小标题</h3><ul><li>一</li></ul><ol><li>二</li></ol><blockquote>引用</blockquote>`, `<h2>标题</h2><p><strong>粗体</strong><br/><em>斜体</em></p><h3>小标题</h3><ul><li>一</li></ul><ol><li>二</li></ol><blockquote>引用</blockquote>`},
		{"script and style contents", `<script>alert('secret')</script><style>body{display:none}</style><p>保留</p>`, `<p>保留</p>`},
		{"event attributes", `<p onclick="alert(1)" style="background:url(https://tracker.test)" id="x"><strong onmouseover="evil()">安全</strong></p>`, `<p><strong>安全</strong></p>`},
		{"dangerous URLs and external embeds", `<a href="javascript:alert(1)">文字</a><img src="https://tracker.test/x" onerror="evil()"><iframe src="https://evil.test">隐藏</iframe><object data="data:text/html,x">隐藏</object><p>末尾</p>`, `文字<p>末尾</p>`},
		{"foreign namespaces", `<svg><a href="javascript:evil()"><text>危险</text></a></svg><math><mtext>危险</mtext></math><p>安全</p>`, `<p>安全</p>`},
		{"escaped text stays text", `<p>&lt;img src=x onerror=evil()&gt; &amp; 内容</p>`, `<p>&lt;img src=x onerror=evil()&gt; &amp; 内容</p>`},
		{"malformed nesting", `<p><strong>保留</p>文字`, `<p><strong>保留</strong></p><strong>文字</strong>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, format, err := normalizeBody(tc.input, "rich_text")
			if err != nil || format != "rich_text" || got != tc.want {
				t.Fatalf("got %q %q %v; want %q", got, format, err, tc.want)
			}
			again, _, err := normalizeBody(got, "rich_text")
			if err != nil || again != got {
				t.Fatalf("not idempotent: %q %v", again, err)
			}
		})
	}
	for _, body := range []string{`<script>x</script>`, `<img src=x>`, `<p> </p>`} {
		if _, _, err := normalizeBody(body, "rich_text"); err == nil {
			t.Fatalf("accepted empty sanitized body: %q", body)
		}
	}
	if _, _, err := normalizeBody("hello", "html"); err == nil {
		t.Fatal("accepted unknown format")
	}
	for _, format := range []string{"", "plain"} {
		got, f, err := normalizeBody("<strong>literal</strong>\n", format)
		if err != nil || f != "plain" || got != "<strong>literal</strong>\n" {
			t.Fatal("plain compatibility lost")
		}
	}
}

func TestRichTextSavePreviewPublishRead(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	c, err := s.SaveContent(ctx, scope, "", Draft{Title: "文章", Body: `<p onload="evil()"><strong>正文</strong><script>secret</script></p>`, BodyFormat: "rich_text", Rights: "own", Owner: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Body != `<p><strong>正文</strong></p>` || c.BodyFormat != "rich_text" {
		t.Fatalf("unsafe saved content: %+v", c)
	}
	preview, err := s.Preview(ctx, scope, c.ID, c.Revision)
	if err != nil {
		t.Fatal(err)
	}
	pc := preview["content"].(Content)
	if pc.Body != c.Body {
		t.Fatal("preview differs from sanitized save")
	}
	_, err = s.Publish(ctx, scope, c.ID, c.Revision, preview["preview_token"].(string), "publish-rich-0001")
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadContent(ctx, c.ID, 1)
	if err != nil || read.Body != c.Body || read.BodyFormat != "rich_text" {
		t.Fatalf("read not sanitized: %+v %v", read, err)
	}
	plain, err := s.SaveContent(ctx, scope, "", Draft{Title: "旧格式", Body: "<strong>literal</strong>", Rights: "own", Owner: "owner-a"})
	if err != nil || plain.BodyFormat != "plain" || plain.Body != "<strong>literal</strong>" {
		t.Fatalf("legacy default changed: %+v %v", plain, err)
	}
}
