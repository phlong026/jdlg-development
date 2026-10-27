package tracking

import (
	"errors"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const maxArticleLayoutBytes = 80000

var (
	layoutLengthPattern = regexp.MustCompile(`^(?:0|[0-9]{1,3}(?:\.[0-9]{1,2})?(?:px|em|rem|%))$`)
	layoutHexColor      = regexp.MustCompile(`^#[0-9a-f]{3,8}$`)
	layoutRGBColor      = regexp.MustCompile(`^rgba?\([0-9]{1,3},[0-9]{1,3},[0-9]{1,3}(?:,(?:0|1|0?\.[0-9]{1,2}))?\)$`)
	layoutBorderPattern = regexp.MustCompile(`^([0-9]{1,2}px) (solid|dashed|dotted) (.+)$`)
)

var articleLayoutTags = map[string]bool{
	"section": true, "div": true, "p": true, "span": true, "strong": true, "b": true,
	"em": true, "i": true, "u": true, "s": true, "br": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "blockquote": true, "ul": true,
	"ol": true, "li": true, "figure": true, "figcaption": true, "table": true,
	"thead": true, "tbody": true, "tfoot": true, "tr": true, "td": true, "th": true,
	"hr": true, "code": true, "pre": true, "img": true, "jd-video": true,
}

var articleLayoutVoidTags = map[string]bool{"br": true, "hr": true, "img": true}

// NormalizeArticleLayoutHTML converts imported markup to the durable article
// layout contract. Media are opaque IDs; network-capable attributes never
// survive this boundary.
func NormalizeArticleLayoutHTML(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	if len(input) > maxArticleLayoutBytes {
		return "", errors.New("article layout too large")
	}
	context := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := nethtml.ParseFragment(strings.NewReader(input), context)
	if err != nil {
		return "", errors.New("invalid article layout")
	}
	var out strings.Builder
	for _, node := range nodes {
		if err := writeArticleLayout(&out, node); err != nil {
			return "", err
		}
	}
	if out.Len() == 0 || out.Len() > maxArticleLayoutBytes {
		return "", errors.New("invalid article layout")
	}
	return out.String(), nil
}

func writeArticleLayout(out *strings.Builder, node *nethtml.Node) error {
	switch node.Type {
	case nethtml.TextNode:
		out.WriteString(html.EscapeString(node.Data))
		return nil
	case nethtml.ElementNode:
		tag := strings.ToLower(node.Data)
		if !articleLayoutTags[tag] {
			if tag == "script" || tag == "style" || tag == "iframe" || tag == "object" || tag == "embed" || tag == "svg" || tag == "audio" || tag == "link" || tag == "meta" {
				return nil
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if err := writeArticleLayout(out, child); err != nil {
					return err
				}
			}
			return nil
		}
		attrs, err := normalizedLayoutAttributes(tag, node.Attr)
		if err != nil {
			return err
		}
		out.WriteByte('<')
		out.WriteString(tag)
		for _, attr := range attrs {
			out.WriteByte(' ')
			out.WriteString(attr.Key)
			out.WriteString(`="`)
			out.WriteString(html.EscapeString(attr.Val))
			out.WriteByte('"')
		}
		out.WriteByte('>')
		if articleLayoutVoidTags[tag] {
			return nil
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := writeArticleLayout(out, child); err != nil {
				return err
			}
		}
		out.WriteString("</")
		out.WriteString(tag)
		out.WriteByte('>')
	}
	return nil
}

func normalizedLayoutAttributes(tag string, attrs []nethtml.Attribute) ([]nethtml.Attribute, error) {
	values := map[string]string{}
	for _, attr := range attrs {
		key := strings.ToLower(strings.TrimSpace(attr.Key))
		switch key {
		case "style":
			if style := normalizeArticleLayoutStyle(attr.Val); style != "" {
				values[key] = style
			}
		case "alt":
			if tag == "img" {
				values[key] = truncateRunesForLayout(strings.TrimSpace(attr.Val), 300)
			}
		case "colspan", "rowspan":
			if tag == "td" || tag == "th" {
				if number, err := strconv.Atoi(attr.Val); err == nil && number >= 1 && number <= 20 {
					values[key] = strconv.Itoa(number)
				}
			}
		case "data-media-id":
			if tag == "img" || tag == "jd-video" {
				values[key] = strings.TrimSpace(attr.Val)
			}
		}
	}
	if tag == "img" || tag == "jd-video" {
		if !validID(values["data-media-id"]) {
			return nil, errors.New("invalid article layout media")
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		order := map[string]int{"data-media-id": 0, "alt": 1, "colspan": 2, "rowspan": 3, "style": 4}
		return order[keys[i]] < order[keys[j]]
	})
	out := make([]nethtml.Attribute, 0, len(keys))
	for _, key := range keys {
		out = append(out, nethtml.Attribute{Key: key, Val: values[key]})
	}
	return out, nil
}

func normalizeArticleLayoutStyle(input string) string {
	allowed := make([]string, 0, 8)
	for _, declaration := range strings.Split(input, ";") {
		parts := strings.SplitN(declaration, ":", 2)
		if len(parts) != 2 {
			continue
		}
		property := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.ToLower(strings.Join(strings.Fields(parts[1]), " "))
		if safeArticleCSSValue(property, value) {
			allowed = append(allowed, property+":"+value)
		}
	}
	return strings.Join(allowed, ";")
}

func safeArticleCSSValue(property, value string) bool {
	if value == "" || strings.ContainsAny(value, `\\{}@`) || strings.Contains(value, "url") || strings.Contains(value, "expression") || strings.Contains(value, "var(") {
		return false
	}
	switch property {
	case "text-align":
		return value == "left" || value == "center" || value == "right" || value == "justify"
	case "color", "background-color":
		return safeArticleColor(value)
	case "font-size", "line-height", "letter-spacing", "margin", "margin-top", "margin-right", "margin-bottom", "margin-left", "padding", "padding-top", "padding-right", "padding-bottom", "padding-left", "border-radius", "gap", "width", "max-width", "min-height":
		return safeArticleLengthList(value, property == "margin" || property == "padding")
	case "font-weight":
		return value == "normal" || value == "bold" || value == "400" || value == "500" || value == "600" || value == "700"
	case "font-style":
		return value == "normal" || value == "italic"
	case "text-decoration":
		return value == "none" || value == "underline" || value == "line-through"
	case "display":
		return value == "block" || value == "inline" || value == "inline-block" || value == "flex"
	case "justify-content":
		return value == "flex-start" || value == "center" || value == "flex-end" || value == "space-between"
	case "align-items":
		return value == "flex-start" || value == "center" || value == "flex-end" || value == "stretch"
	case "flex-direction":
		return value == "row" || value == "column"
	case "white-space":
		return value == "normal" || value == "pre-wrap"
	case "overflow-wrap", "word-break":
		return value == "normal" || value == "break-word" || value == "break-all"
	case "box-sizing":
		return value == "border-box"
	case "border", "border-top", "border-right", "border-bottom", "border-left":
		match := layoutBorderPattern.FindStringSubmatch(value)
		return len(match) == 4 && safeArticleColor(match[3])
	default:
		return false
	}
}

func safeArticleLengthList(value string, multiple bool) bool {
	parts := strings.Fields(value)
	if len(parts) == 0 || (!multiple && len(parts) != 1) || len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		if !layoutLengthPattern.MatchString(part) {
			return false
		}
	}
	return true
}

func safeArticleColor(value string) bool {
	if layoutHexColor.MatchString(value) {
		return true
	}
	if !layoutRGBColor.MatchString(value) {
		return false
	}
	start := strings.IndexByte(value, '(')
	end := strings.LastIndexByte(value, ')')
	parts := strings.Split(value[start+1:end], ",")
	for _, component := range parts[:3] {
		number, err := strconv.Atoi(component)
		if err != nil || number > 255 {
			return false
		}
	}
	return true
}

func truncateRunesForLayout(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

// ArticleLayoutMediaReferences returns the complete nested media union.
func ArticleLayoutMediaReferences(layout string) (map[string]string, error) {
	normalized, err := NormalizeArticleLayoutHTML(layout)
	if err != nil {
		return nil, err
	}
	refs := map[string]string{}
	context := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := nethtml.ParseFragment(strings.NewReader(normalized), context)
	if err != nil {
		return nil, err
	}
	var walk func(*nethtml.Node) error
	walk = func(node *nethtml.Node) error {
		if node.Type == nethtml.ElementNode && (node.Data == "img" || node.Data == "jd-video") {
			kind := "image"
			if node.Data == "jd-video" {
				kind = "video"
			}
			id := ""
			for _, attr := range node.Attr {
				if attr.Key == "data-media-id" {
					id = attr.Val
				}
			}
			if old := refs[id]; old != "" && old != kind {
				return errors.New("article layout media kind conflict")
			}
			refs[id] = kind
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, node := range nodes {
		if err := walk(node); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

// RewriteArticleLayoutMediaIDs atomically replaces every media placeholder.
func RewriteArticleLayoutMediaIDs(layout string, replacements map[string]string) (string, error) {
	normalized, err := NormalizeArticleLayoutHTML(layout)
	if err != nil {
		return "", err
	}
	context := &nethtml.Node{Type: nethtml.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := nethtml.ParseFragment(strings.NewReader(normalized), context)
	if err != nil {
		return "", err
	}
	var rewrite func(*nethtml.Node) error
	rewrite = func(node *nethtml.Node) error {
		if node.Type == nethtml.ElementNode && (node.Data == "img" || node.Data == "jd-video") {
			for index := range node.Attr {
				if node.Attr[index].Key != "data-media-id" {
					continue
				}
				next, ok := replacements[node.Attr[index].Val]
				if !ok || !validID(next) {
					return errors.New("missing article layout media")
				}
				node.Attr[index].Val = next
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := rewrite(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, node := range nodes {
		if err := rewrite(node); err != nil {
			return "", err
		}
	}
	var out strings.Builder
	for _, node := range nodes {
		if err := writeArticleLayout(&out, node); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}
