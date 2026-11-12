package tracking

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestVideoCopyMetadataRoundTrip(t *testing.T) {
	body := `{"schema":1,"presentation":"video","video_layout":"douyin","share_copy":"转发说明","moments_copy":"朋友圈说明","blocks":[{"kind":"video","media_id":"asset-video"}]}`
	normalized, err := normalizeArticleDocument(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"video_layout":"douyin"`, `"share_copy":"转发说明"`, `"moments_copy":"朋友圈说明"`} {
		if !strings.Contains(normalized, value) {
			t.Fatalf("lost %s", value)
		}
	}
	for _, invalid := range []string{strings.Replace(body, `"douyin"`, `"unknown"`, 1), strings.Replace(body, "转发说明", strings.Repeat("中", 2001), 1)} {
		if _, err := normalizeArticleDocument(invalid); err == nil {
			t.Fatal("invalid video metadata accepted")
		}
	}
}

func TestIncompleteVideoDraftIsPrivateAndCannotPublish(t *testing.T) {
	s, scope := fixture(t)
	ctx := context.Background()
	input := Draft{BodyFormat: "article_v2", Body: `{"schema":1,"presentation":"video","draft_only":true,"blocks":[]}`, Owner: "owner-a", Rights: "own"}
	saved, err := s.SaveContent(ctx, scope, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Preview(ctx, scope, saved.ID, saved.Revision); err == nil {
		t.Fatal("incomplete draft previewed")
	}
	if _, err = s.Publish(ctx, scope, saved.ID, saved.Revision, "", "publish-incomplete-video"); err == nil {
		t.Fatal("incomplete draft published")
	}
	if _, err = s.ReadContent(ctx, saved.ID, 1); err == nil {
		t.Fatal("incomplete draft public")
	}
	input.Owner = "not-authorized"
	if _, err = s.SaveContent(ctx, scope, "", input); err == nil {
		t.Fatal("draft bypassed owner authorization")
	}
	input.Owner = "owner-a"
	input.Title = "完整视频"
	input.Revision = saved.Revision
	asset := Asset{ID: "asset-video", Kind: "video", URL: "/api/v1/miniprogram/tracking/media/asset-video", MIME: "video/mp4", Width: 1080, Height: 1920}
	if err = s.AddMedia(ctx, scope, asset, input.Owner); err != nil {
		t.Fatal(err)
	}
	input.MediaIDs = []string{asset.ID}
	input.Body = `{"schema":1,"presentation":"video","video_layout":"channels","share_copy":"分享给你","blocks":[{"kind":"video","media_id":"asset-video"}]}`
	saved, err = s.SaveContent(ctx, scope, saved.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(ctx, scope, saved.ID, saved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(ctx, scope, saved.ID, saved.Revision, preview["preview_token"].(string), "publish-complete-video"); err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadContent(ctx, saved.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal([]byte(read.Body), &doc); err != nil || doc["share_copy"] != "分享给你" {
		t.Fatalf("metadata lost %s %v", read.Body, err)
	}
}
