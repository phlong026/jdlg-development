package tracking

import (
	"strings"
	"testing"
)

func TestConvertMPContentKeepsStructureAndDropsActiveContent(t *testing.T) {
	raw := `<section style="color:red"><span>第一段</span></section>
<section><section><p>嵌套段落</p></section><section><strong>粗</strong>与<b>粗2</b><i>斜</i></section></section>
<h1>大标题</h1><h4>小标题</h4>
<script>alert(1)</script><style>p{}</style><iframe src="https://evil.example/"></iframe>
<mpvoice name="x">语音</mpvoice><svg><text>图形</text></svg>
<ul><li>甲</li><li>乙</li></ul><blockquote>引用</blockquote>
<p><a href="javascript:alert(1)" onclick="x()">链接文字</a><br>换行</p>`
	body, images, err := convertMPContent(raw, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<p>第一段</p>", "<p>嵌套段落</p>", "<p><strong>粗</strong>与<strong>粗2</strong><em>斜</em></p>", "<h2>大标题</h2>", "<h3>小标题</h3>", "<ul><li>甲</li><li>乙</li></ul>", "<blockquote>引用</blockquote>", "链接文字<br/>换行"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
	for _, banned := range []string{"script", "alert", "style", "iframe", "evil.example", "mpvoice", "语音", "图形", "javascript", "onclick", "href", "color"} {
		if strings.Contains(body, banned) {
			t.Errorf("%q survived in %s", banned, body)
		}
	}
	if len(images) != 0 {
		t.Fatalf("unexpected images %v", images)
	}
	// The result passes the same normaliser every saved body goes through.
	if _, format, err := normalizeBody(body, "rich_text"); err != nil || format != "rich_text" {
		t.Fatalf("normaliser rejected the converted body: %v", err)
	}
}

func TestConvertMPContentPicturesAreAllowlistedOrderedAndBounded(t *testing.T) {
	raw := `<p>文</p>
<section><img data-src="https://mmbiz.qpic.cn/a/1?wx_fmt=png" src="https://evil.example/placeholder.gif"></section>
<img src="http://mmbiz.qpic.cn/a/2">
<img data-src="https://mmbiz.qpic.cn/a/1?wx_fmt=png">
<img src="https://evil.example/x.png"><img src="http://127.0.0.1/x.png"><img src="https://mmbiz.qpic.cn.evil.example/x.png">
<img src="https://user:pw@mmbiz.qpic.cn/a/3"><img src="https://mmbiz.qpic.cn:8443/a/4"><img src="data:image/png;base64,AAAA">
<img src="//mmbiz.qlogo.cn/a/5"><img src="https://thirdwx.qlogo.cn/a/6"><img src="https://mmbiz.qpic.cn/a/7">`
	body, images, err := convertMPContent(raw, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://mmbiz.qpic.cn/a/1?wx_fmt=png", "https://mmbiz.qpic.cn/a/2", "https://mmbiz.qlogo.cn/a/5"}
	if strings.Join(images, "\n") != strings.Join(want, "\n") {
		t.Fatalf("images:\n%v\nwant\n%v", images, want)
	}
	if strings.Contains(body, "<p></p>") || strings.Contains(body, "img") || strings.Count(body, "<p>") != 1 {
		t.Fatalf("picture-only blocks must not leave empty paragraphs: %s", body)
	}
}

func TestAllowedMPMediaHost(t *testing.T) {
	for host, ok := range map[string]bool{
		"mmbiz.qpic.cn": true, "qpic.cn": true, "thirdwx.qlogo.cn": true, "MMBIZ.QPIC.CN": true, "mmbiz.qpic.cn.": true,
		"qpic.cn.evil.example": false, "evilqpic.cn": false, "notqpic.cn": false, "127.0.0.1": false, "localhost": false, "": false, "mp.weixin.qq.com": false,
	} {
		if allowedMPMediaHost(host) != ok {
			t.Errorf("%q: want %v", host, ok)
		}
	}
}

func TestMPTitleIsOneBoundedLine(t *testing.T) {
	if got := mpTitle("  春季\n\t新品 发布  "); got != "春季 新品 发布" {
		t.Fatalf("%q", got)
	}
	long := mpTitle(strings.Repeat("长", 120))
	if r := []rune(long); len(r) != 80 || r[79] != '…' {
		t.Fatalf("not bounded to 80 runes: %d", len(r))
	}
	if mpTitle("\u0000\u0007 \n") != "" {
		t.Fatal("control-only title kept")
	}
}
