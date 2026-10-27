// Package notedocx builds a bounded, self-contained Word document. It has no
// network or storage access: callers supply only already-authorized media.
package notedocx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	MaxImageBytes    = 10 << 20
	MaxImagePixels   = 16 << 20
	DefaultMaxBytes  = 32 << 20
	maxDocumentBytes = 1 << 20
	maxElements      = 10000
)

var (
	ErrInvalid = errors.New("invalid private note document")
	ErrLimit   = errors.New("private note export exceeds limit")
)

type Media struct {
	ID, Kind, MIME string
	Width, Height  int
	Data           []byte
}
type Document struct {
	Title, Subtitle, Body, BodyFormat string
	CoverID                           string
	Media                             []Media
}
type Limits struct{ MaxOutputBytes int64 }
type block struct {
	Kind    string   `json:"kind"`
	Text    string   `json:"text"`
	MediaID string   `json:"media_id"`
	Caption string   `json:"caption"`
	Level   int      `json:"level"`
	Ordered bool     `json:"ordered"`
	Items   []string `json:"items"`
}
type element struct {
	text, mediaID, kind string
	heading             bool
}
type imagePart struct {
	name, relationship, mime string
	data                     []byte
	width, height            int
}

// Build returns no bytes on failure. Images are embedded, and no external
// relationship, media identifier, share credential or video URL is emitted.
func Build(ctx context.Context, doc Document, limits Limits) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(doc.Body) > maxDocumentBytes || len(doc.Title) > 1024 || len(doc.Subtitle) > 1024 || len(doc.Media) > 51 {
		return nil, ErrLimit
	}
	max := limits.MaxOutputBytes
	if max <= 0 || max > DefaultMaxBytes {
		max = DefaultMaxBytes
	}
	elements, err := documentElements(doc)
	if err != nil {
		return nil, err
	}
	if doc.CoverID != "" {
		found := false
		for _, item := range elements {
			if item.mediaID == doc.CoverID && item.kind == "image" {
				found = true
				break
			}
		}
		if !found {
			elements = append([]element{{kind: "image", mediaID: doc.CoverID}}, elements...)
		}
	}
	if len(elements) > maxElements {
		return nil, ErrLimit
	}
	media := map[string]Media{}
	var aggregate int64
	for _, asset := range doc.Media {
		if asset.ID == "" || media[asset.ID].ID != "" {
			return nil, ErrInvalid
		}
		if asset.Kind != "image" && asset.Kind != "video" {
			return nil, ErrInvalid
		}
		aggregate += int64(len(asset.Data))
		if len(asset.Data) > MaxImageBytes || aggregate > max {
			return nil, ErrLimit
		}
		media[asset.ID] = asset
	}
	var body strings.Builder
	if err = appendParagraph(&body, doc.Title, "Title"); err != nil {
		return nil, err
	}
	if doc.Subtitle != "" {
		if err = appendParagraph(&body, doc.Subtitle, "Subtitle"); err != nil {
			return nil, err
		}
	}
	images := []imagePart{}
	imageByID := map[string]int{}
	var embeddedBytes int64
	drawingID := 0
	for _, item := range elements {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		switch item.kind {
		case "image":
			asset, ok := media[item.mediaID]
			if !ok || asset.Kind != "image" {
				return nil, ErrInvalid
			}
			index, found := imageByID[asset.ID]
			if !found {
				part, readErr := prepareImage(ctx, asset, len(images)+1, max-embeddedBytes)
				if readErr != nil {
					return nil, readErr
				}
				embeddedBytes += int64(len(part.data))
				images = append(images, part)
				index = len(images) - 1
				imageByID[asset.ID] = index
			}
			drawingID++
			appendDrawing(&body, images[index], drawingID)
		case "video":
			asset, ok := media[item.mediaID]
			if !ok || asset.Kind != "video" {
				return nil, ErrInvalid
			}
			if err = appendParagraph(&body, "视频请在甲第方案中查看", ""); err != nil {
				return nil, err
			}
		default:
			style := ""
			if item.heading {
				style = "Heading1"
			}
			if err = appendParagraph(&body, item.text, style); err != nil {
				return nil, err
			}
		}
		if body.Len() > maxDocumentBytes*8 {
			return nil, ErrLimit
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return packageDocument(ctx, body.String(), images, max)
}

func documentElements(doc Document) ([]element, error) {
	var out []element
	switch doc.BodyFormat {
	case "", "plain":
		for _, line := range strings.Split(strings.ReplaceAll(doc.Body, "\r\n", "\n"), "\n") {
			out = append(out, element{text: line})
		}
	case "rich_text":
		var err error
		out, err = htmlElements(doc.Body, false)
		if err != nil {
			return nil, err
		}
	case "article_v2":
		var article struct {
			Schema     int     `json:"schema"`
			Blocks     []block `json:"blocks"`
			LayoutHTML string  `json:"layout_html"`
		}
		if json.Unmarshal([]byte(doc.Body), &article) != nil || article.Schema != 1 || len(article.Blocks) < 1 || len(article.Blocks) > 500 {
			return nil, ErrInvalid
		}
		if article.LayoutHTML != "" {
			return htmlElements(article.LayoutHTML, true)
		}
		for _, part := range article.Blocks {
			switch part.Kind {
			case "paragraph", "quote", "heading":
				out = append(out, element{text: part.Text, heading: part.Kind == "heading"})
			case "image", "video":
				out = append(out, element{kind: part.Kind, mediaID: part.MediaID})
				if part.Caption != "" {
					out = append(out, element{text: part.Caption})
				}
			case "list":
				for i, text := range part.Items {
					prefix := "• "
					if part.Ordered {
						prefix = strconv.Itoa(i+1) + ". "
					}
					out = append(out, element{text: prefix + text})
				}
			default:
				return nil, ErrInvalid
			}
		}
	default:
		return nil, ErrInvalid
	}
	// For plain/rich notes media are separate attachments in their stored order.
	if doc.BodyFormat != "article_v2" {
		for _, asset := range doc.Media {
			out = append(out, element{kind: asset.Kind, mediaID: asset.ID})
		}
	}
	return out, nil
}

// HTML is converted into semantic paragraphs in DOM order. No style, src,
// hyperlink or arbitrary attribute is copied into the Word package.
func htmlElements(source string, layout bool) ([]element, error) {
	nodes, err := html.ParseFragment(strings.NewReader(source), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return nil, ErrInvalid
	}
	out := []element{}
	var text strings.Builder
	heading := false
	visited := 0
	type listState struct {
		ordered bool
		index   int
	}
	lists := []listState{}
	flush := func() {
		if strings.TrimSpace(text.String()) != "" {
			out = append(out, element{text: strings.TrimSpace(text.String()), heading: heading})
		}
		text.Reset()
	}
	blocks := map[string]bool{"p": true, "div": true, "section": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "blockquote": true, "li": true, "tr": true, "figcaption": true, "pre": true}
	allowed := map[string]bool{"p": true, "div": true, "section": true, "span": true, "strong": true, "b": true, "em": true, "i": true, "u": true, "s": true, "br": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "blockquote": true, "ul": true, "ol": true, "li": true, "figure": true, "figcaption": true, "table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true, "td": true, "th": true, "hr": true, "code": true, "pre": true}
	var walk func(*html.Node, int) error
	walk = func(node *html.Node, depth int) error {
		visited++
		if visited > maxElements || depth > 128 {
			return ErrLimit
		}
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
			return nil
		}
		if node.Type == html.CommentNode {
			return nil
		}
		if node.Type != html.ElementNode || node.Namespace != "" {
			return ErrInvalid
		}
		if layout && (node.Data == "img" || node.Data == "jd-video") {
			flush()
			id := ""
			for _, a := range node.Attr {
				if a.Key == "data-media-id" {
					id = a.Val
				}
				if a.Key == "src" || a.Key == "href" {
					return ErrInvalid
				}
			}
			if id == "" {
				return ErrInvalid
			}
			kind := "image"
			if node.Data == "jd-video" {
				kind = "video"
			}
			out = append(out, element{kind: kind, mediaID: id})
			return nil
		}
		if !allowed[node.Data] {
			return ErrInvalid
		}
		isBlock := blocks[node.Data]
		previousHeading := heading
		if isBlock {
			flush()
			heading = strings.HasPrefix(node.Data, "h") && len(node.Data) == 2 && node.Data != "hr"
		}
		if node.Data == "br" {
			text.WriteByte('\n')
		}
		if node.Data == "li" {
			prefix := "• "
			if len(lists) > 0 {
				list := &lists[len(lists)-1]
				list.index++
				if list.ordered {
					prefix = strconv.Itoa(list.index) + ". "
				}
			}
			text.WriteString(prefix)
		}
		if node.Data == "ul" || node.Data == "ol" {
			lists = append(lists, listState{ordered: node.Data == "ol"})
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child, depth+1); err != nil {
				return err
			}
		}
		if node.Data == "ul" || node.Data == "ol" {
			lists = lists[:len(lists)-1]
		}
		if isBlock {
			flush()
			heading = previousHeading
		}
		if node.Data == "td" || node.Data == "th" {
			text.WriteString("  ")
		}
		return nil
	}
	for _, node := range nodes {
		if err = walk(node, 0); err != nil {
			return nil, err
		}
	}
	flush()
	return out, nil
}

func xmlText(text string) (string, error) {
	if !utf8.ValidString(text) {
		return "", ErrInvalid
	}
	for _, r := range text {
		if !(r == 9 || r == 10 || r == 13 || r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000 && r <= 0x10FFFF) {
			return "", ErrInvalid
		}
	}
	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, []byte(text)); err != nil {
		return "", err
	}
	return escaped.String(), nil
}
func appendParagraph(out *strings.Builder, text string, style string) error {
	escaped, err := xmlText(text)
	if err != nil {
		return err
	}
	parts := strings.Split(escaped, "&#xA;")
	properties := ""
	if style != "" {
		properties = `<w:pStyle w:val="` + style + `"/>`
	}
	out.WriteString(`<w:p><w:pPr>` + properties + `<w:spacing w:after="160"/></w:pPr><w:r>`)
	for i, part := range parts {
		if i > 0 {
			out.WriteString(`<w:br/>`)
		}
		out.WriteString(`<w:t xml:space="preserve">` + part + `</w:t>`)
	}
	out.WriteString(`</w:r></w:p>`)
	return nil
}

func prepareImage(ctx context.Context, asset Media, index int, remainingBytes int64) (imagePart, error) {
	part := imagePart{}
	if err := ctx.Err(); err != nil {
		return part, err
	}
	if len(asset.Data) == 0 || len(asset.Data) > MaxImageBytes || remainingBytes <= 0 {
		return part, ErrLimit
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(asset.Data))
	if err != nil || config.Width < 1 || config.Height < 1 {
		return part, ErrInvalid
	}
	if config.Width > 20000 || config.Height > 20000 || int64(config.Width)*int64(config.Height) > MaxImagePixels {
		return part, ErrLimit
	}
	mime, extension := "", ""
	switch format {
	case "png":
		mime, extension = "image/png", "png"
	case "jpeg":
		mime, extension = "image/jpeg", "jpg"
	case "gif":
		mime, extension = "image/gif", "gif"
	case "webp":
		mime, extension = "image/webp", "png"
	default:
		return part, ErrInvalid
	}
	if asset.MIME != mime {
		return part, ErrInvalid
	}
	if format != "webp" && int64(len(asset.Data)) > remainingBytes {
		return part, ErrLimit
	}
	decoded, _, err := image.Decode(bytes.NewReader(asset.Data))
	if err != nil {
		return part, ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return part, err
	}
	data := asset.Data
	if format == "webp" {
		var converted bytes.Buffer
		writer := &boundedWriter{ctx: ctx, target: &converted, max: min(int64(MaxImageBytes), remainingBytes)}
		if err = png.Encode(writer, decoded); err != nil {
			return part, err
		}
		data = converted.Bytes()
		mime = "image/png"
	}
	return imagePart{name: fmt.Sprintf("image%d.%s", index, extension), relationship: fmt.Sprintf("rIdImage%d", index), mime: mime, data: data, width: config.Width, height: config.Height}, nil
}

func appendDrawing(out *strings.Builder, part imagePart, unique int) {
	const maxWidth, maxHeight = int64(5486400), int64(7772400)
	width, height := int64(part.width)*9525, int64(part.height)*9525
	if width > maxWidth {
		height = height * maxWidth / width
		width = maxWidth
	}
	if height > maxHeight {
		width = width * maxHeight / height
		height = maxHeight
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	fmt.Fprintf(out, `<w:p><w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="方案图片"/><wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="方案图片"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`, width, height, unique, part.relationship, width, height)
}

type boundedWriter struct {
	ctx          context.Context
	target       io.Writer
	written, max int64
}

func (w *boundedWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(data)) > w.max-w.written {
		return 0, ErrLimit
	}
	n, err := w.target.Write(data)
	w.written += int64(n)
	return n, err
}
func packageDocument(ctx context.Context, body string, images []imagePart, max int64) ([]byte, error) {
	var out bytes.Buffer
	writer := zip.NewWriter(&boundedWriter{ctx: ctx, target: &out, max: max})
	add := func(name string, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := writer.Create(name)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		return err
	}
	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>`
	relationships := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`
	seen := map[string]bool{}
	for _, part := range images {
		ext := strings.TrimPrefix(part.name[strings.LastIndex(part.name, "."):], ".")
		if !seen[ext] {
			contentTypes += `<Default Extension="` + ext + `" ContentType="` + part.mime + `"/>`
			seen[ext] = true
		}
		relationships += `<Relationship Id="` + part.relationship + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/` + part.name + `"/>`
		if err := add("word/media/"+part.name, part.data); err != nil {
			return nil, err
		}
	}
	contentTypes += `</Types>`
	relationships += `</Relationships>`
	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body>` + body + `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="0" w:footer="0" w:gutter="0"/></w:sectPr></w:body></w:document>`
	parts := []struct{ name, data string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdMain" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", document}, {"word/_rels/document.xml.rels", relationships},
		{"word/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:eastAsia="Microsoft YaHei"/><w:color w:val="000000"/><w:sz w:val="22"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="300" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:rPr><w:b/><w:color w:val="000000"/><w:sz w:val="36"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Subtitle"><w:name w:val="Subtitle"/><w:basedOn w:val="Normal"/><w:rPr><w:color w:val="000000"/><w:sz w:val="20"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:color w:val="000000"/><w:sz w:val="28"/></w:rPr></w:style></w:styles>`},
	}
	for _, part := range parts {
		if err := add(part.name, []byte(part.data)); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
