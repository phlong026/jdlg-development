package contentimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type stubXHSPageFetcher struct {
	body []byte
	err  error
	seen string
}

type stubXHSVideoResolver struct {
	source Source
	err    error
	calls  int
	seen   string
}

func (resolver *stubXHSVideoResolver) Resolve(_ context.Context, raw string) (Source, error) {
	resolver.calls++
	resolver.seen = raw
	return resolver.source, resolver.err
}

func (fetcher *stubXHSPageFetcher) FetchXHSPage(_ context.Context, raw string) ([]byte, error) {
	fetcher.seen = raw
	return fetcher.body, fetcher.err
}

func xhsImage(defaultURL, previewURL string, width, height int) map[string]any {
	return map[string]any{
		"urlDefault": defaultURL,
		"urlPre":     previewURL,
		"width":      width,
		"height":     height,
		"infoList": []map[string]any{
			{"imageScene": "WB_PRV", "url": previewURL},
			{"imageScene": "WB_DFT", "url": defaultURL},
		},
	}
}

func xhsInitialStatePage(noteID, noteType, title, description string, images []map[string]any) []byte {
	state := map[string]any{
		"note": map[string]any{
			"noteDetailMap": map[string]any{
				noteID: map[string]any{
					"note": map[string]any{
						"noteId":    noteID,
						"type":      noteType,
						"title":     title,
						"desc":      description,
						"user":      map[string]any{"nickname": "作者甲"},
						"imageList": images,
					},
				},
			},
		},
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		panic(err)
	}
	// Real pages currently contain JavaScript undefined values outside strings.
	// The parser must normalize those tokens without rewriting note text.
	script := string(encoded[:len(encoded)-1]) + `,"serverFlags":undefined}`
	return []byte(`<html><body><script>window.__INITIAL_STATE__=` + script + `;</script></body></html>`)
}

func TestParseXiaohongshuGalleryPreservesFullTextAndImageOrder(t *testing.T) {
	const noteID = "6a9206bc000000001e0160eb"
	const description = "第一段完整文案\n第二段的 undefined 字样仍需保留 #自建房[话题]#"
	first := "http://sns-webpic-qc.xhscdn.com/path/first.webp"
	second := "https://sns-webpic-qc.xhscdn.com/path/second.webp"
	page := xhsInitialStatePage(noteID, "normal", "三图案例", description, []map[string]any{
		xhsImage(first, "http://sns-webpic-qc.xhscdn.com/path/first-preview.webp", 1272, 2772),
		xhsImage(second, "https://sns-webpic-qc.xhscdn.com/path/second-preview.webp", 1080, 1440),
		xhsImage(first, "http://sns-webpic-qc.xhscdn.com/path/first-preview.webp", 1272, 2772),
	})

	source, err := ParseXiaohongshuGallery(
		"https://www.xiaohongshu.com/explore/6A9206BC000000001E0160EB?xsec_token=token%2Bvalue&xsec_source=pc_feed",
		page,
	)
	if err != nil {
		t.Fatal(err)
	}
	if source.ID != noteID || source.Platform != Xiaohongshu || source.Title != "三图案例" || source.Author != "作者甲" {
		t.Fatalf("metadata = %#v", source)
	}
	if source.SourceURL != "https://www.xiaohongshu.com/explore/6a9206bc000000001e0160eb?xsec_source=pc_feed&xsec_token=token%2Bvalue" {
		t.Fatalf("source URL = %q", source.SourceURL)
	}
	if source.Text != description {
		t.Fatalf("text = %q", source.Text)
	}
	if source.WatermarkExtraction {
		t.Fatal("photo gallery must not claim pixel-level watermark extraction")
	}
	if source.CoverURL != "https://sns-webpic-qc.xhscdn.com/path/first.webp" {
		t.Fatalf("cover = %q", source.CoverURL)
	}
	if len(source.Media) != 3 {
		t.Fatalf("media count = %d", len(source.Media))
	}
	wantURLs := []string{
		"https://sns-webpic-qc.xhscdn.com/path/first.webp",
		second,
		"https://sns-webpic-qc.xhscdn.com/path/first.webp",
	}
	for index, media := range source.Media {
		if media.Kind != "image" || media.URL != wantURLs[index] || media.Width <= 0 || media.Height <= 0 {
			t.Fatalf("media[%d] = %#v", index, media)
		}
		if media.ID != fmt.Sprintf("%s-image-%02d", noteID, index+1) {
			t.Fatalf("media[%d] id = %q", index, media.ID)
		}
	}

	fetcher := &stubXHSPageFetcher{body: page}
	video := &stubXHSVideoResolver{}
	resolved, err := (XHSResolver{Fetcher: fetcher, Video: video}).Resolve(context.Background(), source.SourceURL)
	if err != nil || len(resolved.Media) != 3 || video.calls != 0 {
		t.Fatalf("gallery resolution = %#v, %v; video calls=%d", resolved, err, video.calls)
	}
}

func TestParseXiaohongshuGalleryRoutesVideoWithoutUsingItsCover(t *testing.T) {
	const noteID = "6a77dfe10000000029032ba0"
	page := xhsInitialStatePage(noteID, "video", "视频笔记", "视频说明", []map[string]any{
		xhsImage("https://sns-webpic-qc.xhscdn.com/video/cover.webp", "https://sns-webpic-qc.xhscdn.com/video/preview.webp", 1080, 1440),
	})
	_, err := ParseXiaohongshuGallery("https://www.xiaohongshu.com/explore/"+noteID, page)
	if errorCode(err) != "xhs_video_source" {
		t.Fatalf("error = %#v", err)
	}
	_, err = (XHSResolver{Fetcher: &stubXHSPageFetcher{body: page}}).Resolve(context.Background(), "https://www.xiaohongshu.com/explore/"+noteID)
	if errorCode(err) != "provider_not_configured" {
		t.Fatalf("missing video provider error = %#v", err)
	}

	fetcher := &stubXHSPageFetcher{body: page}
	video := &stubXHSVideoResolver{source: Source{Platform: Xiaohongshu, ID: noteID}}
	resolver := XHSResolver{Fetcher: fetcher, Video: video}
	source, err := resolver.Resolve(context.Background(), "https://www.xiaohongshu.com/explore/"+noteID)
	if err != nil || source.ID != noteID {
		t.Fatalf("Resolve() = %#v, %v", source, err)
	}
	if video.calls != 1 || video.seen != "https://www.xiaohongshu.com/explore/"+noteID {
		t.Fatalf("video fallback calls=%d seen=%q", video.calls, video.seen)
	}

	video = &stubXHSVideoResolver{err: uncertainResult()}
	resolver.Video = video
	_, err = resolver.Resolve(context.Background(), "https://www.xiaohongshu.com/explore/"+noteID)
	if resolved, ok := err.(*ResolveError); !ok || resolved.Code != "submission_uncertain" || !resolved.Uncertain || video.calls != 1 {
		t.Fatalf("uncertain paid result = %#v, calls=%d", err, video.calls)
	}
}

func TestParseXiaohongshuGalleryFailsClosedForIncompleteOrOversizedGallery(t *testing.T) {
	const noteID = "6a9206bc000000001e0160eb"
	tests := []struct {
		name string
		page []byte
		code string
	}{
		{
			name: "one invalid image rejects the complete gallery",
			page: xhsInitialStatePage(noteID, "normal", "", "正文", []map[string]any{
				xhsImage("https://sns-webpic-qc.xhscdn.com/one.webp", "https://sns-webpic-qc.xhscdn.com/one-preview.webp", 1080, 1440),
				xhsImage("http://127.0.0.1/private.webp", "", 1080, 1440),
			}),
			code: "xhs_gallery_incomplete",
		},
		{
			name: "unknown note type does not trigger paid video fallback",
			page: xhsInitialStatePage(noteID, "live", "", "正文", nil),
			code: "xhs_unknown_source",
		},
		{
			name: "missing initial state",
			page: []byte(`<html><body>risk page</body></html>`),
			code: "invalid_xhs_response",
		},
		{
			name: "malformed initial state",
			page: []byte(`<html><script>window.__INITIAL_STATE__={;</script></html>`),
			code: "invalid_xhs_response",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseXiaohongshuGallery("https://www.xiaohongshu.com/explore/"+noteID, test.page)
			if errorCode(err) != test.code {
				t.Fatalf("error = %#v", err)
			}
		})
	}

	images := make([]map[string]any, 51)
	for index := range images {
		images[index] = xhsImage(
			fmt.Sprintf("https://sns-webpic-qc.xhscdn.com/%02d.webp", index),
			fmt.Sprintf("https://sns-webpic-qc.xhscdn.com/%02d-preview.webp", index),
			1080,
			1440,
		)
	}
	_, err := ParseXiaohongshuGallery(
		"https://www.xiaohongshu.com/explore/"+noteID,
		xhsInitialStatePage(noteID, "normal", "", "正文", images),
	)
	if errorCode(err) != "article_too_large" {
		t.Fatalf("oversized gallery error = %#v", err)
	}
}

func TestParseXiaohongshuGalleryRequiresMatchingNativeIdentity(t *testing.T) {
	const requested = "6a9206bc000000001e0160eb"
	const returned = "6abf037c000000001a02903d"
	_, err := ParseXiaohongshuGallery(
		"https://www.xiaohongshu.com/explore/"+requested,
		xhsInitialStatePage(returned, "normal", "", "正文", []map[string]any{
			xhsImage("https://sns-webpic-qc.xhscdn.com/one.webp", "https://sns-webpic-qc.xhscdn.com/one-preview.webp", 1080, 1440),
		}),
	)
	if errorCode(err) != "invalid_native_id" {
		t.Fatalf("error = %#v", err)
	}
}

func TestXiaohongshuGalleryResolverPreservesPageErrorsAndCanonicalToken(t *testing.T) {
	const noteID = "6a9206bc000000001e0160eb"
	fetcher := &stubXHSPageFetcher{err: fail("risk_control")}
	video := &stubXHSVideoResolver{}
	resolver := XHSResolver{Fetcher: fetcher, Video: video}
	raw := "笔记 https://www.xiaohongshu.com/discovery/item/" + strings.ToUpper(noteID) + "?xsec_token=token%2Bvalue&xsec_source=pc_feed&ignored=1"
	_, err := resolver.Resolve(context.Background(), raw)
	if errorCode(err) != "risk_control" {
		t.Fatalf("error = %#v", err)
	}
	if fetcher.seen != "https://www.xiaohongshu.com/explore/"+noteID+"?xsec_source=pc_feed&xsec_token=token%2Bvalue" {
		t.Fatalf("fetched URL = %q", fetcher.seen)
	}
	if video.calls != 0 {
		t.Fatalf("paid video fallback called after page risk error: %d", video.calls)
	}

	fetcher.err = errors.New("unexpected")
	_, err = resolver.Resolve(context.Background(), raw)
	if !errors.Is(err, fetcher.err) {
		t.Fatalf("non-provider error changed: %#v", err)
	}
	if video.calls != 0 {
		t.Fatalf("paid video fallback called after fetch error: %d", video.calls)
	}
}
