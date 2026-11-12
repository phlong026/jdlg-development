package contentimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stdhtml "html"
	"net/url"
	"regexp"
	"service/tracking"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

const maxArticleHTMLBytes = 8 << 20

var (
	wxvIDPattern         = regexp.MustCompile(`^wxv_[A-Za-z0-9_-]{8,120}$`)
	mediaSourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9:_-]{8,128}$`)
)

// PageFetcher owns network policy (DNS/IP validation, response limits and
// redirects). The resolver deliberately receives bytes rather than an HTTP
// client so untrusted article URLs cannot bypass that policy.
type PageFetcher interface {
	FetchArticle(context.Context, string) ([]byte, error)
}

type ArticleResolver struct {
	Fetcher      PageFetcher
	VideoCapture VideoCapture
}

func (resolver ArticleResolver) ResolveDocument(ctx context.Context, articleURL string) (Document, error) {
	if resolver.Fetcher == nil {
		return Document{}, fail("not_configured")
	}
	if !validArticleURL(articleURL) {
		return Document{}, fail("invalid_source_url")
	}
	body, err := resolver.Fetcher.FetchArticle(ctx, articleURL)
	if err != nil {
		return Document{}, err
	}
	if len(body) == 0 || len(body) > maxArticleHTMLBytes {
		return Document{}, fail("invalid_article_response")
	}
	document, err := ParseArticleHTML(articleURL, string(body))
	if err != nil {
		return Document{}, err
	}

	missing := make([]string, 0)
	for _, media := range document.Source.Media {
		if media.Kind == "video" && media.URL == "" && wxvIDPattern.MatchString(media.ID) {
			missing = append(missing, media.ID)
		}
	}
	if len(missing) == 0 {
		return document, nil
	}
	if resolver.VideoCapture == nil {
		return Document{}, fail("browser_required")
	}
	captured, err := resolver.VideoCapture.Capture(ctx, articleURL, missing)
	if err != nil {
		return Document{}, err
	}
	byID, err := validatedCapturedVideos(missing, captured)
	if err != nil {
		return Document{}, err
	}
	for index := range document.Source.Media {
		media := &document.Source.Media[index]
		if media.Kind == "video" && media.URL == "" {
			video, ok := byID[media.ID]
			if !ok {
				return Document{}, fail("missing_video")
			}
			media.URL = video.URL
		}
	}
	return document, nil
}

func ParseArticleHTML(articleURL, raw string) (Document, error) {
	if !validArticleURL(articleURL) {
		return Document{}, fail("invalid_source_url")
	}
	if len(raw) == 0 || len(raw) > maxArticleHTMLBytes {
		return Document{}, fail("invalid_article_response")
	}
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return Document{}, fail("invalid_article_response")
	}
	pageText := normalizedText(visibleNodeText(root))
	pageTextLower := strings.ToLower(pageText)
	if containsAny(pageTextLower, "该内容已被发布者删除", "内容已删除", "内容因违规无法查看", "article has been deleted") {
		return Document{}, fail("source_unavailable")
	}
	if containsAny(pageTextLower, "环境异常", "请完成验证码", "访问过于频繁", "captcha", "verify you are human") {
		return Document{}, fail("risk_control")
	}

	bodyRoot := selectArticleRoot(root)
	if bodyRoot == nil {
		return Document{}, fail("article_body_missing")
	}
	builder := articleBuilder{
		document:   tracking.ArticleDocument{Schema: 1},
		mediaByKey: make(map[string]int),
		warningSet: make(map[string]bool),
	}
	layout, err := builder.buildLayout(bodyRoot)
	if err != nil {
		return Document{}, err
	}
	builder.document.LayoutHTML = layout
	builder.flowChildren(bodyRoot, tracking.ArticleStyle{})
	builder.flush()
	if builder.err != nil {
		return Document{}, builder.err
	}
	builder.ensureFallbackMediaBlocks()
	if len(builder.document.Blocks) == 0 {
		return Document{}, fail("article_body_missing")
	}
	if err := validateArticleDocumentLimits(builder.document, len(builder.media)); err != nil {
		return Document{}, err
	}

	title := firstNonEmpty(
		metaContent(root, "property", "og:title"),
		metaContent(root, "name", "title"),
		textByID(root, "activity-name"),
		textByClass(root, "rich_media_title"),
		firstTagText(root, "title"),
	)
	author := firstNonEmpty(metaContent(root, "name", "author"), textByID(root, "js_name"))
	accountName := firstNonEmpty(
		metaContent(root, "name", "account_name"),
		metaContent(root, "property", "og:site_name"),
		textByID(root, "js_name"),
		author,
	)
	accountID := firstNonEmpty(
		metaContent(root, "name", "gh_id"),
		metaContent(root, "name", "user_name"),
		attributeFromFirst(root, "data-gh-id"),
		attributeFromFirst(root, "data-user-name"),
	)

	return Document{
		Source: Source{
			Platform:  WechatArticle,
			ID:        "article-" + stableURLHash(articleURL),
			SourceURL: articleURL,
			Title:     truncateRunes(normalizedText(title), 200),
			Author:    truncateRunes(normalizedText(author), 120),
			Media:     builder.media,
		},
		Body:              builder.document,
		SourceAccountID:   truncateRunes(normalizedText(accountID), 128),
		SourceAccountName: truncateRunes(normalizedText(accountName), 120),
		// A safe archive needs a separate renderer and resource rewriting pass.
		// Empty is honest: structured blocks are the supported fidelity contract.
		ArchiveHTML: "",
		Warnings:    builder.warnings,
	}, nil
}

type articleBuilder struct {
	document   tracking.ArticleDocument
	media      []RemoteMedia
	mediaByKey map[string]int
	warnings   []string
	warningSet map[string]bool
	text       strings.Builder
	textStyle  tracking.ArticleStyle
	err        error
}

func (builder *articleBuilder) flowChildren(node *html.Node, inherited tracking.ArticleStyle) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		builder.flow(child, inherited)
	}
}

func (builder *articleBuilder) flow(node *html.Node, inherited tracking.ArticleStyle) {
	if builder.err != nil {
		return
	}
	if node.Type == html.TextNode {
		builder.appendText(node.Data, inherited)
		return
	}
	if node.Type != html.ElementNode {
		return
	}
	tag := strings.ToLower(node.Data)
	if isProhibitedArticleTag(tag) || isDroppedArticleNode(node) {
		return
	}
	style := mergeArticleStyle(inherited, styleOf(node))
	switch tag {
	case "mp-common-videosnap", "mpvideo", "mp-video":
		builder.flush()
		id := embeddedWXVAttribute(node)
		if id == "" {
			builder.err = fail("unsupported_video")
			return
		}
		mediaURL := cleanCapturedVideoURL(firstAttribute(node, "data-video-url", "data-url"), id)
		builder.addMedia(RemoteMedia{ID: id, Kind: "video", URL: mediaURL})
	case "br":
		builder.appendText("\n", style)
	case "img":
		builder.flush()
		if source := firstAttribute(node, "data-src", "src"); source != "" {
			if cleaned := cleanArticleMediaURL(source); cleaned != "" {
				// DOM image IDs are local to an article and may be repeated.
				id := safeMediaSourceID("", cleaned)
				builder.addMedia(RemoteMedia{ID: id, Kind: "image", URL: cleaned, Caption: normalizedText(firstAttribute(node, "alt"))})
				return
			}
		}
		builder.warn("missing_image")
	case "iframe":
		builder.flush()
		id := embeddedWXVID(node)
		if id == "" {
			builder.warn("unsupported_iframe_removed")
			return
		}
		mediaURL := cleanCapturedVideoURL(firstAttribute(node, "data-video-url", "data-url"), id)
		builder.addMedia(RemoteMedia{ID: id, Kind: "video", URL: mediaURL})
	case "video":
		builder.flush()
		id := firstAttribute(node, "data-mpvid", "data-vid", "data-id")
		source := firstAttribute(node, "src", "data-src")
		if source == "" {
			source = firstDescendantAttribute(node, "source", "src")
		}
		cleaned := cleanArticleMediaURL(source)
		if cleaned != "" {
			id = safeMediaSourceID(id, cleaned)
		}
		if id != "" && (cleaned != "" || wxvIDPattern.MatchString(id)) {
			builder.addMedia(RemoteMedia{ID: id, Kind: "video", URL: cleaned})
			return
		}
		builder.err = fail("unsupported_video")
	case "h1", "h2", "h3", "h4", "h5", "h6":
		builder.flush()
		level := 3
		if node.Data == "h1" || node.Data == "h2" {
			level = 2
		}
		if text := normalizedText(visibleNodeText(node)); text != "" {
			builder.document.Blocks = append(builder.document.Blocks, tracking.ArticleBlock{Kind: "heading", Text: text, Level: level, Style: style})
		}
	case "ul", "ol":
		builder.flush()
		items := directListItems(node)
		if len(items) > 0 {
			builder.document.Blocks = append(builder.document.Blocks, tracking.ArticleBlock{Kind: "list", Items: items, Ordered: node.Data == "ol", Style: style})
		}
	case "blockquote":
		builder.flush()
		if text := normalizedText(visibleNodeText(node)); text != "" {
			builder.document.Blocks = append(builder.document.Blocks, tracking.ArticleBlock{Kind: "quote", Text: text, Style: style})
		}
	case "p":
		builder.flush()
		builder.flowChildren(node, style)
		builder.flush()
	case "section", "div", "article", "main":
		builder.flowChildren(node, style)
		builder.flush()
	default:
		builder.flowChildren(node, style)
	}
}

func isProhibitedArticleTag(tag string) bool {
	switch tag {
	case "script", "style", "noscript", "template", "object", "embed", "svg", "audio", "link", "meta", "head":
		return true
	default:
		return false
	}
}

func (builder *articleBuilder) appendText(value string, style tracking.ArticleStyle) {
	if strings.TrimSpace(value) == "" && !strings.Contains(value, "\n") {
		if builder.text.Len() > 0 {
			builder.text.WriteByte(' ')
		}
		return
	}
	if builder.text.Len() == 0 {
		builder.textStyle = style
	}
	builder.text.WriteString(value)
}

func (builder *articleBuilder) flush() {
	text := normalizedText(builder.text.String())
	builder.text.Reset()
	if text == "" {
		return
	}
	builder.document.Blocks = append(builder.document.Blocks, tracking.ArticleBlock{Kind: "paragraph", Text: text, Style: builder.textStyle})
	builder.textStyle = tracking.ArticleStyle{}
}

func (builder *articleBuilder) addMedia(media RemoteMedia) {
	ref := builder.registerMedia(media)
	if ref == "" {
		return
	}
	builder.document.Blocks = append(builder.document.Blocks, tracking.ArticleBlock{
		Kind: media.Kind, MediaID: ref, Caption: media.Caption,
	})
}

func (builder *articleBuilder) registerMedia(media RemoteMedia) string {
	key := media.Kind + "\x00" + firstNonEmpty(media.ID, media.URL)
	index, exists := builder.mediaByKey[key]
	if !exists {
		index = len(builder.media)
		builder.mediaByKey[key] = index
		builder.media = append(builder.media, media)
	}
	return remoteMediaID(index)
}

func (builder *articleBuilder) ensureFallbackMediaBlocks() {
	referenced := map[string]bool{}
	for _, block := range builder.document.Blocks {
		if block.Kind == "image" || block.Kind == "video" {
			referenced[block.MediaID] = true
		}
	}
	for index, media := range builder.media {
		ref := remoteMediaID(index)
		if !referenced[ref] {
			builder.document.Blocks = append(builder.document.Blocks, tracking.ArticleBlock{Kind: media.Kind, MediaID: ref, Caption: media.Caption})
		}
	}
}

func (builder *articleBuilder) buildLayout(root *html.Node) (string, error) {
	var candidate strings.Builder
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if err := builder.writeLayoutNode(&candidate, child); err != nil {
			return "", err
		}
	}
	if builder.err != nil {
		return "", builder.err
	}
	layout, err := tracking.NormalizeArticleLayoutHTML(candidate.String())
	if err != nil {
		return "", fail("article_too_large")
	}
	return layout, nil
}

func (builder *articleBuilder) writeLayoutNode(out *strings.Builder, node *html.Node) error {
	if node.Type == html.TextNode {
		out.WriteString(stdhtml.EscapeString(node.Data))
		return nil
	}
	if node.Type != html.ElementNode {
		return nil
	}
	tag := strings.ToLower(node.Data)
	if isProhibitedArticleTag(tag) || isDroppedArticleNode(node) {
		return nil
	}
	if tag == "img" {
		source := firstAttribute(node, "data-src", "src")
		cleaned := cleanArticleMediaURL(source)
		if cleaned == "" {
			builder.warn("missing_image")
			return nil
		}
		ref := builder.registerMedia(RemoteMedia{ID: safeMediaSourceID("", cleaned), Kind: "image", URL: cleaned, Caption: normalizedText(firstAttribute(node, "alt"))})
		out.WriteString(`<img data-media-id="` + stdhtml.EscapeString(ref) + `"`)
		if alt := normalizedText(firstAttribute(node, "alt")); alt != "" {
			out.WriteString(` alt="` + stdhtml.EscapeString(alt) + `"`)
		}
		out.WriteByte('>')
		return nil
	}
	if tag == "iframe" || tag == "video" || tag == "mp-common-videosnap" || tag == "mpvideo" || tag == "mp-video" {
		media, ok := layoutVideoMedia(node, tag)
		if !ok {
			if tag == "iframe" {
				builder.warn("unsupported_iframe_removed")
				return nil
			}
			return fail("unsupported_video")
		}
		ref := builder.registerMedia(media)
		out.WriteString(`<jd-video data-media-id="` + stdhtml.EscapeString(ref) + `"></jd-video>`)
		return nil
	}
	if !isArticleLayoutTag(tag) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := builder.writeLayoutNode(out, child); err != nil {
				return err
			}
		}
		return nil
	}
	out.WriteByte('<')
	out.WriteString(tag)
	if style := firstAttribute(node, "style"); style != "" {
		out.WriteString(` style="` + stdhtml.EscapeString(style) + `"`)
	}
	if (tag == "td" || tag == "th") && firstAttribute(node, "colspan") != "" {
		out.WriteString(` colspan="` + stdhtml.EscapeString(firstAttribute(node, "colspan")) + `"`)
	}
	if (tag == "td" || tag == "th") && firstAttribute(node, "rowspan") != "" {
		out.WriteString(` rowspan="` + stdhtml.EscapeString(firstAttribute(node, "rowspan")) + `"`)
	}
	out.WriteByte('>')
	if tag == "br" || tag == "hr" {
		return nil
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := builder.writeLayoutNode(out, child); err != nil {
			return err
		}
	}
	out.WriteString("</")
	out.WriteString(tag)
	out.WriteByte('>')
	return nil
}

func layoutVideoMedia(node *html.Node, tag string) (RemoteMedia, bool) {
	if tag == "iframe" || tag == "mp-common-videosnap" || tag == "mpvideo" || tag == "mp-video" {
		id := embeddedWXVID(node)
		if tag != "iframe" {
			id = embeddedWXVAttribute(node)
		}
		if id == "" {
			return RemoteMedia{}, false
		}
		return RemoteMedia{ID: id, Kind: "video", URL: cleanCapturedVideoURL(firstAttribute(node, "data-video-url", "data-url"), id)}, true
	}
	id := firstAttribute(node, "data-mpvid", "data-vid", "data-id")
	source := firstAttribute(node, "src", "data-src")
	if source == "" {
		source = firstDescendantAttribute(node, "source", "src")
	}
	cleaned := cleanArticleMediaURL(source)
	if cleaned != "" {
		id = safeMediaSourceID(id, cleaned)
	}
	if id == "" || (cleaned == "" && !wxvIDPattern.MatchString(id)) {
		return RemoteMedia{}, false
	}
	return RemoteMedia{ID: id, Kind: "video", URL: cleaned}, true
}

func isArticleLayoutTag(tag string) bool {
	switch tag {
	case "section", "div", "p", "span", "strong", "b", "em", "i", "u", "s", "br", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "ul", "ol", "li", "figure", "figcaption", "table", "thead", "tbody", "tfoot", "tr", "td", "th", "hr", "code", "pre":
		return true
	default:
		return false
	}
}

func validateArticleDocumentLimits(document tracking.ArticleDocument, mediaCount int) error {
	if len(document.Blocks) > 500 || mediaCount > 50 {
		return fail("article_too_large")
	}
	for _, block := range document.Blocks {
		switch block.Kind {
		case "paragraph", "quote":
			if len(block.Text) > 10000 {
				return fail("article_too_large")
			}
		case "heading":
			if len(block.Text) > 500 {
				return fail("article_too_large")
			}
		case "list":
			if len(block.Items) > 100 {
				return fail("article_too_large")
			}
			for _, item := range block.Items {
				if len(item) > 2000 {
					return fail("article_too_large")
				}
			}
		case "image", "video":
			if len(block.Caption) > 500 {
				return fail("article_too_large")
			}
		}
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return fail("invalid_article_response")
	}
	if len(encoded) > 100000 {
		return fail("article_too_large")
	}
	return nil
}

func (builder *articleBuilder) warn(code string) {
	if !builder.warningSet[code] {
		builder.warningSet[code] = true
		builder.warnings = append(builder.warnings, code)
	}
}

func remoteMediaID(index int) string {
	const digits = "0123456789"
	if index < 0 || index > 9999 {
		return "remote-overflow"
	}
	return "remote-" + string([]byte{digits[index/1000%10], digits[index/100%10], digits[index/10%10], digits[index%10]})
}

func selectArticleRoot(root *html.Node) *html.Node {
	for _, selector := range []struct{ key, value string }{
		{"id", "js_content"},
		{"class", "js_inner"},
		{"class", "rich_media_wrp"},
	} {
		if found := findNode(root, func(node *html.Node) bool {
			matches := attribute(node, selector.key) == selector.value
			if selector.key == "class" {
				matches = hasClass(node, selector.value)
			}
			if !matches {
				return false
			}
			// Traversal starts at the root's children, so exclude summary roots and descendants here.
			for ancestor := node; ancestor != nil; ancestor = ancestor.Parent {
				if attribute(ancestor, "id") == "js_common_share_desc" {
					return false
				}
			}
			return true
		}); found != nil {
			return found
		}
	}
	return nil
}

func isDroppedArticleNode(node *html.Node) bool {
	// The share description is a preview, even when it sits inside the article wrapper.
	if attribute(node, "id") == "js_common_share_desc" {
		return true
	}
	tag := strings.ToLower(node.Data)
	if tag == "mpprofile" || tag == "mp-weixin-profile" || tag == "mp-weapp" || tag == "mp-miniprogram" {
		return true
	}
	tokens := strings.ToLower(attribute(node, "id") + " " + attribute(node, "class"))
	for _, blocked := range []string{"profile", "weapp", "mini_program", "miniprogram", "js_pc_qr", "qr_code", "video_controls", "video_opr", "rich_media_tool", "reward_area"} {
		if strings.Contains(tokens, blocked) {
			return true
		}
	}
	return false
}

func styleOf(node *html.Node) tracking.ArticleStyle {
	raw := strings.ToLower(attribute(node, "style"))
	style := tracking.ArticleStyle{}
	switch {
	case strings.Contains(raw, "text-align:center"), strings.Contains(raw, "text-align: center"):
		style.Align = "center"
	case strings.Contains(raw, "text-align:right"), strings.Contains(raw, "text-align: right"):
		style.Align = "right"
	case strings.Contains(raw, "text-align:left"), strings.Contains(raw, "text-align: left"):
		style.Align = "left"
	}
	if strings.Contains(raw, "border-left") {
		style.Border = "left"
	}
	return style
}

func mergeArticleStyle(parent, child tracking.ArticleStyle) tracking.ArticleStyle {
	if child.Align == "" {
		child.Align = parent.Align
	}
	if child.FontSize == "" {
		child.FontSize = parent.FontSize
	}
	if child.Border == "" {
		child.Border = parent.Border
	}
	return child
}

func embeddedWXVID(node *html.Node) string {
	for _, candidate := range []string{embeddedWXVAttribute(node), videoIDFromURL(firstAttribute(node, "data-src", "src"))} {
		if wxvIDPattern.MatchString(candidate) {
			return candidate
		}
	}
	return ""
}

func embeddedWXVAttribute(node *html.Node) string {
	id := firstAttribute(node, "data-mpvid", "data-vid")
	if wxvIDPattern.MatchString(id) {
		return id
	}
	return ""
}

func safeMediaSourceID(candidate, sourceURL string) string {
	candidate = strings.TrimSpace(candidate)
	if mediaSourceIDPattern.MatchString(candidate) {
		return candidate
	}
	return "url-" + stableURLHash(sourceURL)
}

func videoIDFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return parsed.Query().Get("vid")
}

func cleanArticleMediaURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || (parsed.Port() != "" && parsed.Port() != "443") {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if !(host == "qpic.cn" || strings.HasSuffix(host, ".qpic.cn") || host == "qlogo.cn" || strings.HasSuffix(host, ".qlogo.cn")) {
		return ""
	}
	return parsed.String()
}

func validArticleURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Path == "/s" || strings.HasPrefix(parsed.Path, "/s/")) && parsed.Scheme == "https" && strings.EqualFold(strings.TrimSuffix(parsed.Hostname(), "."), "mp.weixin.qq.com") && parsed.User == nil && parsed.Fragment == "" && (parsed.Port() == "" || parsed.Port() == "443")
}

func stableURLHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

func normalizedText(raw string) string {
	var result strings.Builder
	space := false
	for _, value := range strings.TrimSpace(strings.ReplaceAll(raw, "\u00a0", " ")) {
		if unicode.IsSpace(value) || unicode.IsControl(value) {
			space = result.Len() > 0
			continue
		}
		if space {
			result.WriteByte(' ')
			space = false
		}
		result.WriteRune(value)
	}
	return result.String()
}

func visibleNodeText(node *html.Node) string {
	if node.Type == html.ElementNode {
		switch strings.ToLower(node.Data) {
		case "script", "style", "noscript", "template", "svg", "head":
			return ""
		}
	}
	if node.Type == html.TextNode {
		return node.Data
	}
	var result strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		result.WriteString(visibleNodeText(child))
		result.WriteByte(' ')
	}
	return result.String()
}

func directListItems(node *html.Node) []string {
	items := make([]string, 0)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "li" {
			if text := normalizedText(visibleNodeText(child)); text != "" {
				items = append(items, text)
			}
		}
	}
	return items
}

func findNode(node *html.Node, predicate func(*html.Node) bool) *html.Node {
	if node.Type == html.ElementNode && predicate(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findNode(child, predicate); found != nil {
			return found
		}
	}
	return nil
}

func attribute(node *html.Node, key string) string {
	for _, item := range node.Attr {
		if strings.EqualFold(item.Key, key) {
			return strings.TrimSpace(item.Val)
		}
	}
	return ""
}

func firstAttribute(node *html.Node, keys ...string) string {
	for _, key := range keys {
		if value := attribute(node, key); value != "" {
			return value
		}
	}
	return ""
}

func hasClass(node *html.Node, class string) bool {
	for _, candidate := range strings.Fields(attribute(node, "class")) {
		if candidate == class {
			return true
		}
	}
	return false
}

func metaContent(root *html.Node, key, value string) string {
	node := findNode(root, func(node *html.Node) bool {
		return node.Data == "meta" && strings.EqualFold(attribute(node, key), value)
	})
	if node == nil {
		return ""
	}
	return attribute(node, "content")
}

func textByID(root *html.Node, id string) string {
	node := findNode(root, func(node *html.Node) bool { return attribute(node, "id") == id })
	if node == nil {
		return ""
	}
	return normalizedText(visibleNodeText(node))
}

func textByClass(root *html.Node, class string) string {
	node := findNode(root, func(node *html.Node) bool { return hasClass(node, class) })
	if node == nil {
		return ""
	}
	return normalizedText(visibleNodeText(node))
}

func firstTagText(root *html.Node, tag string) string {
	node := findNode(root, func(node *html.Node) bool { return node.Data == tag })
	if node == nil {
		return ""
	}
	return normalizedText(visibleNodeText(node))
}

func attributeFromFirst(root *html.Node, key string) string {
	node := findNode(root, func(node *html.Node) bool { return attribute(node, key) != "" })
	if node == nil {
		return ""
	}
	return attribute(node, key)
}

func firstDescendantAttribute(root *html.Node, tag, key string) string {
	node := findNode(root, func(node *html.Node) bool { return node.Data == tag && attribute(node, key) != "" })
	if node == nil {
		return ""
	}
	return attribute(node, key)
}
