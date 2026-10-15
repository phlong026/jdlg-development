package tracking

import (
	"bytes"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// allowedMPMediaHost is the allowlist for pictures imported from an official
// account article: WeChat's own image CDN and nothing else. The article HTML is
// untrusted input, so a picture URL is never fetched merely because it appears
// in it.
func allowedMPMediaHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, domain := range []string{"qpic.cn", "qlogo.cn"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// cleanMPMediaURL returns a fetchable https URL for an allowlisted host, or "".
// Older articles carry http URLs for the same CDN; they are upgraded.
func cleanMPMediaURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || !allowedMPMediaHost(u.Hostname()) || len(raw) > 2048 {
		return ""
	}
	if u.Scheme == "http" {
		u.Scheme = "https"
	}
	if u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") {
		return ""
	}
	return u.String()
}

var mpBlocks = map[string]bool{"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "ul": true, "ol": true, "li": true, "blockquote": true, "section": true, "div": true, "table": true, "pre": true}
var mpDropped = map[string]bool{"script": true, "style": true, "iframe": true, "object": true, "embed": true, "svg": true, "math": true, "template": true, "noscript": true, "textarea": true, "title": true, "video": true, "audio": true, "head": true, "link": true, "meta": true,
	// WeChat's own embeds (voice, music, profile cards, product cards) carry their
	// data in attributes; any text inside is placeholder copy, not article text.
	"mpvoice": true, "qqmusic": true, "mpcps": true, "mpprofile": true, "mpgood": true, "mpshop": true}

func hasBlockDescendant(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if mpBlocks[c.Data] || hasBlockDescendant(c) {
			return true
		}
	}
	return false
}

// convertMPContent turns article HTML into the structure normalizeBody accepts
// and lists up to maxImages picture URLs in reading order. Article markup is
// mostly styled <section> and <span>; a leaf <section> becomes a paragraph so
// text does not run together, headings are reduced to the two levels allowed,
// and everything else is flattened. Normalisation still runs afterwards as the
// security boundary; this only decides structure.
func convertMPContent(raw string, maxImages int) (body string, images []string, err error) {
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "", nil, fail(400, "文章内容无法解析")
	}
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	seen := map[string]bool{}
	var walk func(n, parent *html.Node, depth int)
	add := func(parent *html.Node, tag string, children *html.Node, depth int) {
		out := &html.Node{Type: html.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag))}
		parent.AppendChild(out)
		for c := children.FirstChild; c != nil; c = c.NextSibling {
			walk(c, out, depth+1)
		}
	}
	walk = func(n, parent *html.Node, depth int) {
		if depth > 128 {
			return
		}
		switch n.Type {
		case html.TextNode:
			parent.AppendChild(&html.Node{Type: html.TextNode, Data: n.Data})
			return
		case html.ElementNode:
		default:
			return
		}
		if n.Namespace != "" || mpDropped[n.Data] || strings.Contains(n.Data, "-") {
			return
		}
		switch n.Data {
		case "img":
			for _, key := range []string{"data-src", "src"} {
				for _, a := range n.Attr {
					if a.Key != key {
						continue
					}
					if u := cleanMPMediaURL(a.Val); u != "" && !seen[u] && len(images) < maxImages {
						seen[u] = true
						images = append(images, u)
					}
					return
				}
			}
		case "br":
			parent.AppendChild(&html.Node{Type: html.ElementNode, Data: "br", DataAtom: atom.Br})
		case "b", "strong":
			add(parent, "strong", n, depth)
		case "i", "em":
			add(parent, "em", n, depth)
		case "h1", "h2":
			add(parent, "h2", n, depth)
		case "h3", "h4", "h5", "h6":
			add(parent, "h3", n, depth)
		case "p", "ul", "ol", "li", "blockquote":
			add(parent, n.Data, n, depth)
		case "section", "div":
			if hasBlockDescendant(n) {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, parent, depth+1)
				}
			} else {
				add(parent, "p", n, depth)
			}
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, parent, depth+1)
			}
		}
	}
	for _, n := range nodes {
		walk(n, root, 0)
	}
	pruneEmpty(root)
	var out bytes.Buffer
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&out, c); err != nil {
			return "", nil, ErrUnavailable
		}
	}
	return out.String(), images, nil
}

// mpTitle makes one line of at most 80 runes with no control characters.
func mpTitle(raw string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteRune(' ')
		}
		space = false
		b.WriteRune(r)
	}
	runes := []rune(b.String())
	if len(runes) > 80 {
		return string(runes[:79]) + "…"
	}
	return string(runes)
}

func nodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(nodeText(c))
	}
	return b.String()
}

// pruneEmpty drops blocks and emphasis that hold no text, such as the empty
// paragraph left by a section that contained only a picture.
func pruneEmpty(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		pruneEmpty(c)
		if c.Type == html.ElementNode && c.Data != "br" && strings.TrimSpace(strings.ReplaceAll(nodeText(c), "\u00a0", " ")) == "" {
			n.RemoveChild(c)
		}
		c = next
	}
}

// nodeTextOf is the visible text of an HTML fragment.
func nodeTextOf(fragment string) string {
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(nodeText(n))
	}
	return strings.ReplaceAll(b.String(), "\u00a0", " ")
}
