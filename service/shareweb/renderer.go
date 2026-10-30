package shareweb

import (
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"regexp"
	"strings"

	"service/tracking"
)

//go:embed templates/reader.html assets/reader.css assets/reader.js
var files embed.FS

var codePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,256}$`)
var classPattern = regexp.MustCompile(`^[a-z]+$`)

type Page struct {
	Code      string
	Content   tracking.Content
	Trackable bool
}

type viewBlock struct {
	Kind, Text, Caption, MediaID, MediaURL, Class string
	Level                                         int
	Items                                         []string
	Ordered                                       bool
	SpacingBefore, SpacingAfter                   int
}

type viewPage struct {
	Code, Title, ContentID string
	Version                int
	Blocks                 []viewBlock
	Trackable              bool
	RichHTML               template.HTML
	LayoutHTML             template.HTML
}

var readerTemplate = template.Must(template.ParseFS(files, "templates/reader.html"))

func Render(w io.Writer, page Page) error {
	if !codePattern.MatchString(page.Code) || page.Content.Status != "published" || page.Content.ID == "" || page.Content.Version <= 0 {
		return errors.New("content unavailable")
	}
	blocks, richHTML, layoutHTML, err := renderBlocks(page)
	if err != nil {
		return err
	}
	return readerTemplate.ExecuteTemplate(w, "reader.html", viewPage{
		Code: page.Code, Title: page.Content.Title, ContentID: page.Content.ID,
		Version: page.Content.Version, Blocks: blocks, Trackable: page.Trackable, RichHTML: richHTML, LayoutHTML: layoutHTML,
	})
}

// RenderLanding exposes no source body. It is the safe browser handoff for
// legacy public routes whose content model cannot be rendered by this service.
func RenderLanding(w io.Writer, code, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "甲第灵光内容"
	}
	return Render(w, Page{Code: code, Content: tracking.Content{
		ID: "legacy-landing", Version: 1, Title: title, Status: "published",
		BodyFormat: "plain", Body: "请在甲第灵光小程序中查看完整内容。",
	}})
}

func renderBlocks(page Page) ([]viewBlock, template.HTML, template.HTML, error) {
	switch page.Content.BodyFormat {
	case "article_v2":
		var document tracking.ArticleDocument
		if err := json.Unmarshal([]byte(page.Content.Body), &document); err != nil || document.Schema != 1 || len(document.Blocks) == 0 {
			return nil, "", "", errors.New("article unavailable")
		}
		media := map[string]tracking.Asset{}
		for _, asset := range page.Content.Media {
			media[asset.ID] = asset
		}
		if document.LayoutHTML != "" {
			layout, err := renderArticleLayout(page.Code, document.LayoutHTML, media)
			if err != nil {
				return nil, "", "", err
			}
			return nil, "", template.HTML(layout), nil // layout is re-sanitized and receives only local media URLs.
		}
		out := make([]viewBlock, 0, len(document.Blocks))
		for _, block := range document.Blocks {
			item := viewBlock{Kind: block.Kind, Text: block.Text, Caption: block.Caption, Level: block.Level, Items: block.Items, Ordered: block.Ordered, Class: styleClass(block.Style), SpacingBefore: block.Style.SpacingBefore, SpacingAfter: block.Style.SpacingAfter}
			if block.Kind == "image" || block.Kind == "video" {
				asset, ok := media[block.MediaID]
				if !ok || asset.Kind != block.Kind {
					return nil, "", "", errors.New("article media unavailable")
				}
				item.MediaURL = "/api/v1/share-web/media/" + page.Code + "/" + asset.ID
				item.MediaID = asset.ID
			}
			out = append(out, item)
		}
		return out, "", "", nil
	case "plain", "rich_text":
		body := strings.TrimSpace(page.Content.Body)
		if body == "" {
			return nil, "", "", errors.New("content unavailable")
		}
		out := []viewBlock{}
		var rich template.HTML
		if page.Content.BodyFormat == "rich_text" {
			clean, err := tracking.SanitizePublishedRichText(body)
			if err != nil {
				return nil, "", "", errors.New("content unavailable")
			}
			rich = template.HTML(clean) // clean is produced by tracking's element-only whitelist.
		} else {
			out = append(out, viewBlock{Kind: "paragraph", Text: body})
		}
		for _, asset := range page.Content.Media {
			if (asset.Kind != "image" && asset.Kind != "video") || !codePattern.MatchString(asset.ID) {
				return nil, "", "", errors.New("content media unavailable")
			}
			out = append(out, viewBlock{Kind: asset.Kind, MediaID: asset.ID, MediaURL: "/api/v1/share-web/media/" + page.Code + "/" + asset.ID})
		}
		return out, rich, "", nil
	default:
		return nil, "", "", errors.New("content unavailable")
	}
}

func renderArticleLayout(code, input string, media map[string]tracking.Asset) (string, error) {
	layout, err := tracking.NormalizeArticleLayoutHTML(input)
	if err != nil {
		return "", errors.New("article unavailable")
	}
	refs, err := tracking.ArticleLayoutMediaReferences(layout)
	if err != nil {
		return "", errors.New("article unavailable")
	}
	for id, kind := range refs {
		asset, ok := media[id]
		if !ok || asset.Kind != kind || !codePattern.MatchString(asset.ID) {
			return "", errors.New("article media unavailable")
		}
		url := "/api/v1/share-web/media/" + code + "/" + asset.ID
		if kind == "image" {
			layout = strings.ReplaceAll(layout, `<img data-media-id="`+id+`"`, `<img src="`+url+`"`)
		} else {
			layout = strings.ReplaceAll(layout, `<jd-video data-media-id="`+id+`"`, `<video data-media-id="`+id+`" src="`+url+`" controls playsinline preload="metadata"`)
		}
	}
	layout = strings.ReplaceAll(layout, "</jd-video>", "</video>")
	if strings.Contains(layout, `<img data-media-id=`) || strings.Contains(layout, "jd-video") {
		return "", errors.New("article media unavailable")
	}
	return layout, nil
}

func styleClass(style tracking.ArticleStyle) string {
	parts := []string{"block"}
	for _, value := range []string{style.Align, style.FontSize, style.Border} {
		if classPattern.MatchString(value) {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
}

func Asset(name string) ([]byte, error) {
	if name != "reader.css" && name != "reader.js" {
		return nil, errors.New("asset unavailable")
	}
	return files.ReadFile("assets/" + name)
}
