package tracking

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// normalizeBody makes rich HTML safe at the persistence boundary. Media and source
// links are separate permission-checked records, never inline HTML attributes.
func normalizeBody(body, format string) (string, string, error) {
	if format == "" || format == "plain" {
		return body, "plain", nil
	}
	if format != "rich_text" {
		return "", "", fail(400, "不支持的正文格式")
	}
	nodes, err := html.ParseFragment(strings.NewReader(body), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "", "", fail(400, "富文本格式无效")
	}
	allowed := map[string]bool{"p": true, "br": true, "strong": true, "em": true, "h2": true, "h3": true, "ul": true, "ol": true, "li": true, "blockquote": true}
	drop := map[string]bool{"script": true, "style": true, "iframe": true, "object": true, "embed": true, "svg": true, "math": true, "template": true, "noscript": true, "textarea": true, "title": true, "video": true, "audio": true}
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	var visible strings.Builder
	var copySafe func(*html.Node, *html.Node, int)
	copySafe = func(node, parent *html.Node, depth int) {
		if depth > 256 {
			return
		}
		if node.Type == html.TextNode {
			parent.AppendChild(&html.Node{Type: html.TextNode, Data: node.Data})
			visible.WriteString(node.Data)
			return
		}
		if node.Type != html.ElementNode || node.Namespace != "" || drop[node.Data] {
			return
		}
		target := parent
		if allowed[node.Data] {
			target = &html.Node{Type: html.ElementNode, Data: node.Data, DataAtom: node.DataAtom}
			parent.AppendChild(target)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			copySafe(child, target, depth+1)
		}
	}
	for _, node := range nodes {
		copySafe(node, root, 0)
	}
	if strings.TrimSpace(visible.String()) == "" {
		return "", "", fail(400, "清理后的正文不能为空")
	}
	var out bytes.Buffer
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		if err := html.Render(&out, node); err != nil {
			return "", "", ErrUnavailable
		}
	}
	return out.String(), "rich_text", nil
}
