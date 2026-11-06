package tracking

import (
	"encoding/json"
	"io"
	"strings"
)

const maxArticleBlocks = 500

func normalizeArticleDocument(body string) (string, error) {
	var input ArticleDocument
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Schema != 1 {
		return "", fail(400, "原排版正文格式无效")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", fail(400, "原排版正文格式无效")
	}
	if len(input.Blocks) == 0 || len(input.Blocks) > maxArticleBlocks {
		return "", fail(400, "原排版正文块数无效")
	}
	if input.Presentation != "" && input.Presentation != "article" && input.Presentation != "gallery" && input.Presentation != "video" {
		return "", fail(400, "内容呈现类型无效")
	}
	if input.Presentation != "" && input.Presentation != "article" && articlePresentation(input) != input.Presentation {
		return "", fail(400, "内容与所选呈现类型不符，请保留文章排版")
	}
	out := ArticleDocument{Schema: 1, Presentation: input.Presentation, Blocks: make([]ArticleBlock, 0, len(input.Blocks))}
	if input.LayoutHTML != "" {
		layout, err := NormalizeArticleLayoutHTML(input.LayoutHTML)
		if err != nil {
			return "", fail(400, "原排版正文布局无效")
		}
		out.LayoutHTML = layout
	}
	for _, block := range input.Blocks {
		normalized, err := normalizeArticleBlock(block)
		if err != nil {
			return "", err
		}
		out.Blocks = append(out.Blocks, normalized)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", ErrUnavailable
	}
	return string(encoded), nil
}

// ContentPresentation reads display intent without rewriting an immutable body.
func ContentPresentation(content Content) string {
	if content.Type == "document" || content.Type == "note" || content.Type == "project_note" {
		return ""
	}
	if content.BodyFormat != "article_v2" {
		if content.Type == "video" {
			return "video"
		}
		return "article"
	}
	var document ArticleDocument
	if json.Unmarshal([]byte(content.Body), &document) != nil {
		return "article"
	}
	if document.Presentation == "article" {
		return "article"
	}
	return articlePresentation(document)
}

func articlePresentation(document ArticleDocument) string {
	if document.LayoutHTML != "" {
		return "article"
	}
	images, videos, firstImage, lastImage := 0, 0, -1, -1
	for index, block := range document.Blocks {
		if block.Style != (ArticleStyle{}) {
			return "article"
		}
		switch block.Kind {
		case "paragraph":
		case "image":
			images++
			if firstImage < 0 {
				firstImage = index
			}
			lastImage = index
		case "video":
			videos++
		default:
			return "article"
		}
	}
	if videos == 1 && images == 0 {
		return "video"
	}
	if images > 0 && videos == 0 {
		for index := firstImage; index <= lastImage; index++ {
			if document.Blocks[index].Kind != "image" {
				return "article"
			}
		}
		return "gallery"
	}
	return "article"
}

func normalizeArticleBlock(block ArticleBlock) (ArticleBlock, error) {
	block.Kind = strings.TrimSpace(block.Kind)
	block.Text = strings.TrimSpace(block.Text)
	block.Caption = strings.TrimSpace(block.Caption)
	style, err := normalizeArticleStyle(block.Style)
	if err != nil {
		return ArticleBlock{}, err
	}
	out := ArticleBlock{Kind: block.Kind, Style: style}
	switch block.Kind {
	case "paragraph":
		if block.Text == "" || len(block.Text) > 10000 {
			return ArticleBlock{}, fail(400, "段落内容无效")
		}
		out.Text = block.Text
	case "heading":
		if block.Text == "" || len(block.Text) > 500 || (block.Level != 2 && block.Level != 3) {
			return ArticleBlock{}, fail(400, "标题内容或层级无效")
		}
		out.Text, out.Level = block.Text, block.Level
	case "image", "video":
		if !validID(block.MediaID) || len(block.Caption) > 500 {
			return ArticleBlock{}, fail(400, "媒体引用无效")
		}
		out.MediaID, out.Caption = block.MediaID, block.Caption
	case "list":
		if len(block.Items) == 0 || len(block.Items) > 100 {
			return ArticleBlock{}, fail(400, "列表内容无效")
		}
		out.Ordered = block.Ordered
		out.Items = make([]string, 0, len(block.Items))
		for _, item := range block.Items {
			item = strings.TrimSpace(item)
			if item == "" || len(item) > 2000 {
				return ArticleBlock{}, fail(400, "列表项内容无效")
			}
			out.Items = append(out.Items, item)
		}
	case "quote":
		if block.Text == "" || len(block.Text) > 10000 {
			return ArticleBlock{}, fail(400, "引用内容无效")
		}
		out.Text = block.Text
	default:
		return ArticleBlock{}, fail(400, "原排版正文含不支持的内容块")
	}
	return out, nil
}

func normalizeArticleStyle(style ArticleStyle) (ArticleStyle, error) {
	if style.Align != "" && style.Align != "left" && style.Align != "center" && style.Align != "right" {
		return ArticleStyle{}, fail(400, "正文对齐样式无效")
	}
	if style.FontSize != "" && style.FontSize != "small" && style.FontSize != "normal" && style.FontSize != "large" {
		return ArticleStyle{}, fail(400, "正文字号样式无效")
	}
	if style.Border != "" && style.Border != "none" && style.Border != "left" && style.Border != "box" {
		return ArticleStyle{}, fail(400, "正文边框样式无效")
	}
	if style.SpacingBefore < 0 || style.SpacingBefore > 48 || style.SpacingAfter < 0 || style.SpacingAfter > 48 {
		return ArticleStyle{}, fail(400, "正文段距样式无效")
	}
	return ArticleStyle{Align: style.Align, FontSize: style.FontSize, SpacingBefore: style.SpacingBefore, SpacingAfter: style.SpacingAfter, Border: style.Border}, nil
}

func articleMediaReferences(body string) (map[string]string, error) {
	var doc ArticleDocument
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return nil, fail(400, "原排版正文格式无效")
	}
	refs := map[string]string{}
	for _, block := range doc.Blocks {
		if block.Kind != "image" && block.Kind != "video" {
			continue
		}
		if old := refs[block.MediaID]; old != "" && old != block.Kind {
			return nil, fail(400, "同一媒体不能同时作为图片和视频")
		}
		refs[block.MediaID] = block.Kind
	}
	if doc.LayoutHTML != "" {
		layoutRefs, err := ArticleLayoutMediaReferences(doc.LayoutHTML)
		if err != nil {
			return nil, fail(400, "原排版正文布局无效")
		}
		for id, kind := range layoutRefs {
			if old := refs[id]; old != "" && old != kind {
				return nil, fail(400, "同一媒体不能同时作为图片和视频")
			}
			refs[id] = kind
		}
	}
	return refs, nil
}
