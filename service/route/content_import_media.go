package route

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"service/contentimport"
	"service/tracking"
)

type importMediaFetch func(context.Context, contentimport.Platform, string, int64) ([]byte, string, error)

func saveImportedMedia(ctx context.Context, svc *tracking.Service, scope tracking.Scope, job contentimport.Job, index int, source contentimport.RemoteMedia, fetch importMediaFetch) (tracking.Asset, error) {
	var out tracking.Asset
	if scope.Actor != job.Actor || scope.Tenant != job.Tenant || !scope.Manage || !scope.Allows(job.Owner) || !workbenchMediaConfigured() {
		return out, tracking.ErrUnavailable
	}
	scope.Owners = []string{job.Owner}
	sourceKey := source.ID
	if sourceKey == "" {
		sourceKey = job.ID + ":" + fmt.Sprint(index)
	}
	sum := sha256.Sum256([]byte(job.Tenant + ":" + job.Owner + ":" + string(job.Platform) + ":" + source.Kind + ":" + sourceKey))
	id := base64.RawURLEncoding.EncodeToString(sum[:24])
	root := os.Getenv("TRACKING_MEDIA_DIR")
	path := filepath.Join(root, id)
	max := int64(10 << 20)
	if source.Kind == "video" {
		max = 50 << 20
	} else if source.Kind != "image" {
		return out, &contentimport.ResolveError{Code: "invalid_media"}
	}
	var data []byte
	info, err := os.Lstat(path)
	fileMissing := errors.Is(err, os.ErrNotExist)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > max {
			return out, &contentimport.ResolveError{Code: "invalid_stored_media"}
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			return out, openErr
		}
		data, err = io.ReadAll(io.LimitReader(file, max+1))
		closeErr := file.Close()
		if err != nil {
			return out, err
		}
		if closeErr != nil {
			return out, closeErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return out, err
	} else {
		data, _, err = fetch(ctx, job.Platform, source.URL, max)
		if err != nil {
			return out, err
		}
	}
	if int64(len(data)) > max || len(data) == 0 {
		return out, &contentimport.ResolveError{Code: "media_too_large"}
	}
	mime := http.DetectContentType(data)
	width, height := 0, 0
	if source.Kind == "image" {
		if mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" {
			return out, &contentimport.ResolveError{Code: "invalid_media"}
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
		if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 20000 || cfg.Height > 20000 {
			return out, &contentimport.ResolveError{Code: "invalid_media"}
		}
		width, height = cfg.Width, cfg.Height
	} else {
		if mime != "video/mp4" {
			return out, &contentimport.ResolveError{Code: "invalid_media"}
		}
		width, height, err = trackingMP4Dimensions(data)
		if err != nil {
			return out, &contentimport.ResolveError{Code: "invalid_media"}
		}
	}
	out = tracking.Asset{ID: id, Kind: source.Kind, URL: "/api/v1/miniprogram/tracking/media/" + id, Width: width, Height: height, MIME: mime, Title: clipImportText(source.Caption, 200)}
	if out.Title == "" {
		out.Title = "链接导入素材"
	}
	if old, e := svc.Media(ctx, &scope, id); e == nil {
		if old.Kind != out.Kind || old.MIME != out.MIME || old.Width != out.Width || old.Height != out.Height {
			return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_stored_media"}
		}
		if !fileMissing {
			return old, nil
		}
	}
	if fileMissing {
		temp, e := os.CreateTemp(root, ".import-")
		if e != nil {
			return out, e
		}
		name := temp.Name()
		defer os.Remove(name)
		if _, e = temp.Write(data); e != nil {
			temp.Close()
			return out, e
		}
		if e = temp.Close(); e != nil {
			return out, e
		}
		if e = os.Link(name, path); e != nil && !errors.Is(e, os.ErrExist) {
			return out, e
		}
	}
	if e := svc.AddMedia(ctx, scope, out, job.Owner); e != nil {
		old, readErr := svc.Media(ctx, &scope, id)
		if readErr == nil && old.Kind == out.Kind && old.Width == out.Width && old.Height == out.Height && old.MIME == out.MIME {
			return old, nil
		}
		return tracking.Asset{}, e
	}
	return out, nil
}
func importDraft(job contentimport.Job, doc contentimport.Document, assets []tracking.Asset) (tracking.Draft, error) {
	var draft tracking.Draft
	if len(assets) != len(doc.Source.Media) {
		return draft, &contentimport.ResolveError{Code: "missing_media"}
	}
	refs := map[string]tracking.Asset{}
	for i, a := range assets {
		if a.ID == "" || a.Kind != doc.Source.Media[i].Kind {
			return draft, &contentimport.ResolveError{Code: "invalid_stored_media"}
		}
		refs[fmt.Sprintf("remote-%04d", i)] = a
	}
	body := tracking.ArticleDocument{Schema: doc.Body.Schema, LayoutHTML: doc.Body.LayoutHTML, Blocks: append([]tracking.ArticleBlock{}, doc.Body.Blocks...)}
	replacements := map[string]string{}
	for i, block := range body.Blocks {
		if block.Kind == "image" || block.Kind == "video" {
			asset, ok := refs[block.MediaID]
			if !ok || asset.Kind != block.Kind {
				return draft, &contentimport.ResolveError{Code: "missing_media"}
			}
			body.Blocks[i].MediaID = asset.ID
		}
	}
	for remoteID, asset := range refs {
		replacements[remoteID] = asset.ID
	}
	if body.LayoutHTML != "" {
		rewritten, rewriteErr := tracking.RewriteArticleLayoutMediaIDs(body.LayoutHTML, replacements)
		if rewriteErr != nil {
			return draft, &contentimport.ResolveError{Code: "missing_media"}
		}
		body.LayoutHTML = rewritten
	}
	articleRefs := map[string]string{}
	for _, block := range body.Blocks {
		if block.Kind == "image" || block.Kind == "video" {
			articleRefs[block.MediaID] = block.Kind
		}
	}
	if body.LayoutHTML != "" {
		layoutRefs, refErr := tracking.ArticleLayoutMediaReferences(body.LayoutHTML)
		if refErr != nil {
			return draft, &contentimport.ResolveError{Code: "missing_media"}
		}
		for id, kind := range layoutRefs {
			articleRefs[id] = kind
		}
	}
	ids := make([]string, 0, len(articleRefs))
	seen := map[string]bool{}
	for _, asset := range assets {
		if articleRefs[asset.ID] != "" && !seen[asset.ID] {
			ids = append(ids, asset.ID)
			seen[asset.ID] = true
		}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return draft, err
	}
	coverID := ""
	if doc.CoverRef != "" {
		a, ok := refs[doc.CoverRef]
		if !ok || a.Kind != "image" {
			return draft, &contentimport.ResolveError{Code: "missing_media"}
		}
		coverID = a.ID
	}
	if coverID == "" {
		for _, a := range assets {
			if a.Kind == "image" {
				coverID = a.ID
				break
			}
		}
	}
	note := "链接导入的可编辑草稿，请在发布前核对正文、排版和媒体。"
	if doc.SourceAccountName != "" {
		note += " 来源公众号：" + clipImportText(doc.SourceAccountName, 120) + "。"
	}
	draft = tracking.Draft{Title: clipImportText(doc.Source.Title, 80), Body: string(encoded), BodyFormat: "article_v2", SourceURL: doc.Source.SourceURL, SourceNote: note, Rights: job.Rights, Owner: job.Owner, CoverID: coverID, MediaIDs: ids}
	return draft, nil
}
func clipImportText(value string, limit int) string {
	r := []rune(strings.TrimSpace(value))
	if len(r) > limit {
		r = r[:limit]
	}
	return string(r)
}
