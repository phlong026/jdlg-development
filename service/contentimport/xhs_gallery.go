package contentimport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

const xhsInitialStatePrefix = "window.__INITIAL_STATE__="

type XHSPageFetcher interface {
	FetchXHSPage(context.Context, string) ([]byte, error)
}

// XHSResolver reads the public note page first. Only an explicitly identified
// video note may consume one configured provider request.
type XHSResolver struct {
	Fetcher XHSPageFetcher
	Video   Resolver
}

func (resolver XHSResolver) Resolve(ctx context.Context, raw string) (Source, error) {
	link, err := parseXHSPageLink(raw)
	if err != nil {
		return Source{}, err
	}
	if link.Platform != Xiaohongshu {
		return Source{}, fail("unsupported_platform")
	}
	if resolver.Fetcher == nil {
		return Source{}, fail("not_configured")
	}
	page, err := resolver.Fetcher.FetchXHSPage(ctx, link.URL)
	if err != nil {
		return Source{}, err
	}
	source, err := ParseXiaohongshuGallery(link.URL, page)
	if resolveErrorCode(err) != "xhs_video_source" {
		return source, err
	}
	if resolver.Video == nil {
		return Source{}, fail("provider_not_configured")
	}
	return resolver.Video.Resolve(ctx, link.URL)
}

type xhsInitialState struct {
	Note struct {
		NoteDetailMap map[string]struct {
			Note xhsPublicNote `json:"note"`
		} `json:"noteDetailMap"`
	} `json:"note"`
}

type xhsPublicNote struct {
	NoteID    string           `json:"noteId"`
	Type      string           `json:"type"`
	Title     string           `json:"title"`
	Desc      string           `json:"desc"`
	User      xhsPublicUser    `json:"user"`
	ImageList []xhsPublicImage `json:"imageList"`
}

type xhsPublicUser struct {
	Nickname string `json:"nickname"`
}

type xhsPublicImage struct {
	URLDefault string         `json:"urlDefault"`
	Width      int            `json:"width"`
	Height     int            `json:"height"`
	InfoList   []xhsImageInfo `json:"infoList"`
}

type xhsImageInfo struct {
	ImageScene string `json:"imageScene"`
	URL        string `json:"url"`
}

func ParseXiaohongshuGallery(sourceURL string, page []byte) (Source, error) {
	link, err := parseXHSPageLink(sourceURL)
	if err != nil || link.Platform != Xiaohongshu {
		return Source{}, fail("invalid_link")
	}
	if len(page) == 0 || len(page) > maxXHSPageBytes {
		return Source{}, fail("invalid_xhs_response")
	}
	stateJSON, err := extractXHSInitialState(page)
	if err != nil {
		return Source{}, err
	}
	var state xhsInitialState
	if json.Unmarshal(stateJSON, &state) != nil || len(state.Note.NoteDetailMap) != 1 {
		return Source{}, fail("invalid_xhs_response")
	}

	var mapID string
	var note xhsPublicNote
	for id, detail := range state.Note.NoteDetailMap {
		mapID, note = validXiaohongshuID(id), detail.Note
	}
	noteID := validXiaohongshuID(note.NoteID)
	requestedID := xhsNoteIDFromURL(link.URL)
	if mapID == "" || noteID == "" || mapID != noteID || requestedID != "" && requestedID != noteID {
		return Source{}, fail("invalid_native_id")
	}

	switch strings.ToLower(strings.TrimSpace(note.Type)) {
	case "video":
		return Source{}, fail("xhs_video_source")
	case "normal":
		// Continue below. A video cover is therefore never mistaken for a gallery.
	default:
		return Source{}, fail("xhs_unknown_source")
	}
	if len(note.ImageList) == 0 {
		return Source{}, fail("xhs_gallery_incomplete")
	}
	if len(note.ImageList) > 50 {
		return Source{}, fail("article_too_large")
	}

	media := make([]RemoteMedia, 0, len(note.ImageList))
	for index, image := range note.ImageList {
		imageURL := xhsFullImageURL(image)
		if imageURL == "" || image.Width <= 0 || image.Height <= 0 {
			return Source{}, fail("xhs_gallery_incomplete")
		}
		media = append(media, RemoteMedia{
			ID:     fmt.Sprintf("%s-image-%02d", noteID, index+1),
			Kind:   "image",
			URL:    imageURL,
			Width:  image.Width,
			Height: image.Height,
		})
	}

	description := strings.TrimSpace(note.Desc)
	title := strings.TrimSpace(note.Title)
	if title == "" {
		title = truncateRunes(description, 40)
	}
	if title == "" {
		title = "小红书图文"
	}
	return Source{
		Platform:  Xiaohongshu,
		ID:        noteID,
		SourceURL: link.URL,
		Title:     truncateRunes(title, 200),
		Author:    truncateRunes(strings.TrimSpace(note.User.Nickname), 120),
		Text:      description,
		Media:     media,
		CoverURL:  media[0].URL,
		// Public-page image URLs do not prove pixel-level watermark removal.
		WatermarkExtraction: false,
	}, nil
}

func extractXHSInitialState(page []byte) ([]byte, error) {
	root, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil, fail("invalid_xhs_response")
	}
	var state string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if state != "" {
			return
		}
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "script") && node.FirstChild != nil {
			candidate := strings.TrimSpace(node.FirstChild.Data)
			if strings.HasPrefix(candidate, xhsInitialStatePrefix) {
				state = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(candidate, xhsInitialStatePrefix)), ";"))
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if state == "" || len(state) > maxXHSPageBytes {
		return nil, fail("invalid_xhs_response")
	}
	return normalizeXHSUndefined([]byte(state)), nil
}

func normalizeXHSUndefined(raw []byte) []byte {
	const token = "undefined"
	out := make([]byte, 0, len(raw))
	inString, escaped := false, false
	for index := 0; index < len(raw); {
		char := raw[index]
		if inString {
			out = append(out, char)
			index++
			if escaped {
				escaped = false
			} else if char == '\\' {
				escaped = true
			} else if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
			out = append(out, char)
			index++
			continue
		}
		if len(raw)-index >= len(token) && string(raw[index:index+len(token)]) == token && tokenBoundary(raw, index-1) && tokenBoundary(raw, index+len(token)) {
			out = append(out, "null"...)
			index += len(token)
			continue
		}
		out = append(out, char)
		index++
	}
	return out
}

func tokenBoundary(raw []byte, index int) bool {
	if index < 0 || index >= len(raw) {
		return true
	}
	char := raw[index]
	return !(char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char == '_' || char == '$')
}

func xhsNoteIDFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return validXiaohongshuID(firstMatch(xiaohongshuPathID, parsed.Path))
}

func parseXHSPageLink(raw string) (ParsedLink, error) {
	link, err := ParseLink(raw)
	if err != nil || link.Platform != Xiaohongshu {
		return link, err
	}
	source := xhsSourceQuery(raw)
	if source == "" {
		return link, nil
	}
	parsed, err := url.Parse(link.URL)
	if err != nil {
		return ParsedLink{}, fail("invalid_link")
	}
	query := parsed.Query()
	query.Set("xsec_source", source)
	parsed.RawQuery = query.Encode()
	link.URL = parsed.String()
	return link, nil
}

func xhsSourceQuery(raw string) string {
	match := linkPattern.FindString(strings.TrimSpace(raw))
	match = strings.TrimRight(match, ",.!?;:)]}\uff0c\u3002\uff01\uff1f\uff1b\uff1a\u3001\uff09\u3011\u300b\u300d\u201d")
	if match == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(match), "http://") && !strings.HasPrefix(strings.ToLower(match), "https://") {
		match = "https://" + match
	}
	parsed, err := url.Parse(match)
	if err != nil {
		return ""
	}
	source := strings.TrimSpace(parsed.Query().Get("xsec_source"))
	if len(source) == 0 || len(source) > 64 {
		return ""
	}
	for _, char := range source {
		if !validXHSShortCodeChar(char) {
			return ""
		}
	}
	return source
}

func xhsFullImageURL(image xhsPublicImage) string {
	for _, info := range image.InfoList {
		if strings.EqualFold(strings.TrimSpace(info.ImageScene), "WB_DFT") {
			return validXHSImageURL(info.URL)
		}
	}
	return validXHSImageURL(image.URLDefault)
}

func validXHSImageURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Port() != "" && parsed.Port() != "443" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "xhscdn.com" && !strings.HasSuffix(host, ".xhscdn.com") {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	parsed.Scheme = "https"
	return validRemoteURL(parsed.String())
}

var _ Resolver = XHSResolver{}
