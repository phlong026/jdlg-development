package shareweb

import (
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"regexp"
	"strconv"
	"strings"

	"service/tracking"
)

//go:embed templates/reader.html templates/collection.html assets/reader.css assets/reader.js
var files embed.FS

var codePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,256}$`)
var classPattern = regexp.MustCompile(`^[a-z]+$`)

type Page struct {
	Code         string
	Content      tracking.Content
	Trackable    bool
	MediaBase    string
	ItemPosition *int
}

type CollectionPage struct {
	Code       string
	Collection tracking.PublishedProjectCollection
	Trackable  bool
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
	CollectionItem         bool
	ItemPosition           int
}

type collectionItemView struct {
	Position  int
	Title     string
	URL       string
	CoverURL  string
	Available bool
}

type collectionViewPage struct {
	Code, Title, Description string
	Version                  int
	Items                    []collectionItemView
	Trackable                bool
}

var readerTemplate = template.Must(template.ParseFS(files, "templates/reader.html"))
var collectionTemplate = template.Must(template.ParseFS(files, "templates/collection.html"))

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
		CollectionItem: page.ItemPosition != nil, ItemPosition: intValue(page.ItemPosition),
	})
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func RenderCollection(w io.Writer, page CollectionPage) error {
	collection := page.Collection
	if !codePattern.MatchString(page.Code) || collection.Status != "published" || collection.ID == "" || collection.Version <= 0 {
		return errors.New("collection unavailable")
	}
	items := make([]collectionItemView, 0, len(collection.Items))
	for position, item := range collection.Items {
		view := collectionItemView{Position: position, Available: item.Available}
		if item.Available {
			view.Title = item.Title
			view.URL = "/share/s/" + page.Code + "/items/" + strconv.Itoa(position)
			if item.Cover != nil && item.Cover.Kind == "image" && codePattern.MatchString(item.Cover.ID) {
				view.CoverURL = "/api/v1/share-web/collection-media/" + page.Code + "/" + strconv.Itoa(position) + "/" + item.Cover.ID
			}
		} else {
			view.Title = "内容暂不可用"
		}
		items = append(items, view)
	}
	return collectionTemplate.ExecuteTemplate(w, "collection.html", collectionViewPage{
		Code: page.Code, Title: collection.Title, Description: collection.Description,
		Version: collection.Version, Items: items, Trackable: page.Trackable,
	})
}

// RenderLoginLanding keeps the original share destination without rendering its content.
func RenderLoginLanding(w io.Writer, code string, itemPosition *int) error {
	if itemPosition != nil && *itemPosition < 0 {
		return errors.New("content unavailable")
	}
	return Render(w, Page{Code: code, ItemPosition: itemPosition, Content: tracking.Content{
		ID: "login-landing", Version: 1, Title: "登录后查看完整内容", Status: "published",
		BodyFormat: "plain", Body: "请在小程序中登录并补齐头像、昵称。",
	}})
}

func renderBlocks(page Page) ([]viewBlock, template.HTML, template.HTML, error) {
	mediaURL := func(id string) string {
		if page.MediaBase != "" {
			return strings.TrimRight(page.MediaBase, "/") + "/" + id
		}
		return "/api/v1/share-web/media/" + page.Code + "/" + id
	}
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
			layout, err := renderArticleLayout(document.LayoutHTML, media, mediaURL)
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
				item.MediaURL = mediaURL(asset.ID)
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
			out = append(out, viewBlock{Kind: asset.Kind, MediaID: asset.ID, MediaURL: mediaURL(asset.ID)})
		}
		return out, rich, "", nil
	default:
		return nil, "", "", errors.New("content unavailable")
	}
}

func renderArticleLayout(input string, media map[string]tracking.Asset, mediaURL func(string) string) (string, error) {
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
		url := mediaURL(asset.ID)
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
