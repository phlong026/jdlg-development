package route

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"service/contentimport"
	"service/contentstorage"
	"service/tracking"
)

type importMediaFetch func(context.Context, contentimport.Platform, string, int64) ([]byte, string, error)
type importMediaFileFetch func(context.Context, contentimport.Platform, string, int64) (contentimport.MediaFile, error)

func saveImportedMedia(ctx context.Context, svc *tracking.Service, scope tracking.Scope, job contentimport.Job, index int, source contentimport.RemoteMedia, fetch importMediaFetch) (tracking.Asset, error) {
	var out tracking.Asset
	if scope.Actor != job.Actor || scope.Tenant != job.Tenant || !scope.Manage || !scope.Allows(job.Owner) || !contentMediaConfigured(svc) {
		return out, tracking.ErrUnavailable
	}
	scope.Owners = []string{job.Owner}
	id, err := importedMediaID(job, index, source)
	if err != nil {
		return out, err
	}
	max := int64(10 << 20)
	if source.Kind == "video" {
		max = 50 << 20
	} else if source.Kind != "image" {
		return out, &contentimport.ResolveError{Code: "invalid_media"}
	}
	return persistImportedMedia(ctx, svc, scope, persistImportedMediaInput{
		ID: id, Kind: source.Kind, URL: source.URL, Caption: source.Caption,
		DefaultTitle: "链接导入素材", Max: max, Fetch: fetch,
		PlatformForFetch: job.Platform, Origin: "import:" + string(job.Platform), Owner: job.Owner,
	})
}

func saveImportedMediaFile(ctx context.Context, svc *tracking.Service, scope tracking.Scope, job contentimport.Job, index int, source contentimport.RemoteMedia, fetch importMediaFileFetch) (tracking.Asset, error) {
	var out tracking.Asset
	if scope.Actor != job.Actor || scope.Tenant != job.Tenant || !scope.Manage || !scope.Allows(job.Owner) || !contentMediaConfigured(svc) {
		return out, tracking.ErrUnavailable
	}
	scope.Owners = []string{job.Owner}
	id, err := importedMediaID(job, index, source)
	if err != nil {
		return out, err
	}
	max := int64(10 << 20)
	if source.Kind == "video" {
		max = 500 << 20
	} else if source.Kind != "image" {
		return out, &contentimport.ResolveError{Code: "invalid_media"}
	}
	return persistImportedMediaFile(ctx, svc, scope, persistImportedMediaFileInput{
		ID: id, Kind: source.Kind, URL: source.URL, Caption: source.Caption,
		DefaultTitle: "链接导入素材", Max: max, Fetch: fetch,
		PlatformForFetch: job.Platform, Origin: "import:" + string(job.Platform), Owner: job.Owner,
	})
}

func importedMediaID(job contentimport.Job, index int, source contentimport.RemoteMedia) (string, error) {
	sourceKey := source.ID
	if sourceKey == "" {
		sourceKey = job.ID + ":" + fmt.Sprint(index)
	}
	if job.Platform == contentimport.Xiaohongshu && source.Kind == "image" {
		parsed, err := url.Parse(source.URL)
		if err != nil {
			return "", &contentimport.ResolveError{Code: "invalid_media"}
		}
		query := parsed.Query()
		for _, key := range []string{"sign", "signature", "expires", "token", "auth_key", "wsSecret", "wsTime"} {
			query.Del(key)
		}
		sourceKey += ":" + parsed.Hostname() + parsed.EscapedPath() + "?" + query.Encode()
	}
	identity, _ := json.Marshal([]string{job.Tenant, job.Owner, string(job.Platform), source.Kind, sourceKey})
	sum := sha256.Sum256(identity)
	return base64.RawURLEncoding.EncodeToString(sum[:24]), nil
}

type persistImportedMediaInput struct {
	ID               string
	Kind             string // image | video
	URL              string
	Caption          string
	DefaultTitle     string
	Max              int64
	Fetch            importMediaFetch
	PlatformForFetch contentimport.Platform
	Origin           string // media_objects.origin 审计标记
	Owner            string
}

// persistImportedMedia 完成"远端 URL → workbench 私有媒体"的通用转存：优先命中已存
// ID 避免重复下载，类型/尺寸校验后按两阶段（优先）或本地目录落库。链接导入与
// AI 生成结果转存（B1 直连管线）共用此实现，任何一处校验规则变更两边同时生效。
func persistImportedMedia(ctx context.Context, svc *tracking.Service, scope tracking.Scope, in persistImportedMediaInput) (tracking.Asset, error) {
	var out tracking.Asset
	data, found, err := readContentMedia(ctx, svc, in.ID, in.Max)
	if err != nil {
		return out, err
	}
	if !found {
		data, _, err = in.Fetch(ctx, in.PlatformForFetch, in.URL, in.Max)
		if err != nil {
			return out, err
		}
	}
	if int64(len(data)) > in.Max || len(data) == 0 {
		return out, &contentimport.ResolveError{Code: "media_too_large"}
	}
	mime := http.DetectContentType(data)
	width, height := 0, 0
	if in.Kind == "image" {
		if mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" && mime != "image/webp" {
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
	out = tracking.Asset{ID: in.ID, Kind: in.Kind, URL: "/api/v1/miniprogram/tracking/media/" + in.ID, Width: width, Height: height, MIME: mime, Title: clipImportText(in.Caption, 200)}
	if out.Title == "" {
		out.Title = in.DefaultTitle
	}
	if svc.MediaObjects != nil {
		digest := sha256.Sum256(data)
		operation, _, err := svc.PrepareMediaObject(ctx, scope, out, in.Owner, fmt.Sprintf("%x", digest), in.Origin)
		if err != nil {
			return tracking.Asset{}, err
		}
		if err = storeContentMedia(ctx, svc, in.ID, data, mime); err != nil {
			return tracking.Asset{}, err
		}
		return svc.FinalizeMediaObject(ctx, scope, in.ID, operation)
	}
	if !found {
		if err = storeContentMedia(ctx, svc, in.ID, data, mime); err != nil {
			return tracking.Asset{}, err
		}
	}
	if old, e := svc.Media(ctx, &scope, in.ID); e == nil {
		if old.Kind != out.Kind || old.MIME != out.MIME || old.Width != out.Width || old.Height != out.Height {
			return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_stored_media"}
		}
		return old, nil
	}
	if e := svc.AddMedia(ctx, scope, out, in.Owner); e != nil {
		old, readErr := svc.Media(ctx, &scope, in.ID)
		if readErr == nil && old.Kind == out.Kind && old.Width == out.Width && old.Height == out.Height && old.MIME == out.MIME {
			return old, nil
		}
		return tracking.Asset{}, e
	}
	return out, nil
}

type persistImportedMediaFileInput struct {
	ID               string
	Kind             string
	URL              string
	Caption          string
	DefaultTitle     string
	Max              int64
	Fetch            importMediaFileFetch
	PlatformForFetch contentimport.Platform
	Origin           string
	Owner            string
}

func persistImportedMediaFile(ctx context.Context, svc *tracking.Service, scope tracking.Scope, in persistImportedMediaFileInput) (tracking.Asset, error) {
	if existing, found, err := existingImportedMediaFile(ctx, svc, scope, in.ID, in.Max); err != nil {
		return tracking.Asset{}, err
	} else if found {
		if existing.Kind != in.Kind {
			return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_stored_media"}
		}
		return existing, nil
	}
	media, err := in.Fetch(ctx, in.PlatformForFetch, in.URL, in.Max)
	if err != nil {
		return tracking.Asset{}, err
	}
	defer func() { _ = media.Remove() }()
	file, info, err := openImportedMediaFile(media, in.Max)
	if err != nil {
		return tracking.Asset{}, err
	}
	defer file.Close()
	header := make([]byte, 512)
	n, readErr := file.ReadAt(header, 0)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_media"}
	}
	mime := http.DetectContentType(header[:n])
	width, height := 0, 0
	if in.Kind == "image" {
		if mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" && mime != "image/webp" {
			return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_media"}
		}
		cfg, _, decodeErr := image.DecodeConfig(file)
		if decodeErr != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 20000 || cfg.Height > 20000 {
			return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_media"}
		}
		width, height = cfg.Width, cfg.Height
	} else if in.Kind == "video" {
		if mime != "video/mp4" {
			return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_media"}
		}
		width, height, err = trackingMP4DimensionsAtContext(ctx, file, info.Size())
		if err != nil {
			return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_media"}
		}
	} else {
		return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_media"}
	}
	digest, err := digestImportedMediaFile(file, info.Size())
	if err != nil || media.SHA256 != "" && !strings.EqualFold(media.SHA256, digest) {
		return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_media"}
	}
	out := tracking.Asset{ID: in.ID, Kind: in.Kind, URL: "/api/v1/miniprogram/tracking/media/" + in.ID, Width: width, Height: height, MIME: mime, Title: clipImportText(in.Caption, 200)}
	if out.Title == "" {
		out.Title = in.DefaultTitle
	}
	if svc.MediaObjects != nil {
		operation, _, prepareErr := svc.PrepareMediaObject(ctx, scope, out, in.Owner, digest, in.Origin)
		if prepareErr != nil {
			return tracking.Asset{}, prepareErr
		}
		if err = storeContentMediaFile(ctx, svc, in.ID, media.Path, info.Size(), mime, digest); err != nil {
			if errors.Is(err, contentstorage.ErrPublicObject) {
				return tracking.Asset{}, &contentimport.ResolveError{Code: "private_storage_required"}
			}
			return tracking.Asset{}, err
		}
		return svc.FinalizeMediaObject(ctx, scope, in.ID, operation)
	}
	if err = storeContentMediaFile(ctx, svc, in.ID, media.Path, info.Size(), mime, digest); err != nil {
		return tracking.Asset{}, err
	}
	if old, readErr := svc.Media(ctx, &scope, in.ID); readErr == nil {
		if old.Kind != out.Kind || old.MIME != out.MIME || old.Width != out.Width || old.Height != out.Height {
			return tracking.Asset{}, &contentimport.ResolveError{Code: "invalid_stored_media"}
		}
		return old, nil
	}
	if err = svc.AddMedia(ctx, scope, out, in.Owner); err != nil {
		return tracking.Asset{}, err
	}
	return out, nil
}

func openImportedMediaFile(media contentimport.MediaFile, max int64) (*os.File, os.FileInfo, error) {
	if media.Path == "" || media.Size <= 0 || media.Size > max {
		return nil, nil, &contentimport.ResolveError{Code: "media_too_large"}
	}
	info, err := os.Lstat(media.Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != media.Size {
		return nil, nil, &contentimport.ResolveError{Code: "invalid_media"}
	}
	file, err := os.Open(media.Path)
	if err != nil {
		return nil, nil, err
	}
	return file, info, nil
}

func digestImportedMediaFile(file *os.File, size int64) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	hasher := sha256.New()
	written, err := io.CopyBuffer(hasher, io.LimitReader(file, size+1), make([]byte, 32<<10))
	if err != nil || written != size {
		return "", contentstorage.ErrIntegrity
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func existingImportedMediaFile(ctx context.Context, svc *tracking.Service, scope tracking.Scope, id string, max int64) (tracking.Asset, bool, error) {
	old, err := svc.Media(ctx, &scope, id)
	if err != nil {
		return tracking.Asset{}, false, nil
	}
	validMIME := old.Kind == "video" && old.MIME == "video/mp4" || old.Kind == "image" && (old.MIME == "image/jpeg" || old.MIME == "image/png" || old.MIME == "image/gif" || old.MIME == "image/webp")
	if !validMIME || old.Width <= 0 || old.Height <= 0 || old.Width > 20000 || old.Height > 20000 {
		return tracking.Asset{}, false, &contentimport.ResolveError{Code: "invalid_stored_media"}
	}
	if svc.MediaObjects != nil {
		header, ok := svc.MediaObjects.(interface {
			Head(context.Context, string, string) (contentstorage.Metadata, error)
		})
		if !ok {
			return tracking.Asset{}, false, nil
		}
		meta, headErr := header.Head(ctx, svc.Config.Tenant, id)
		if errors.Is(headErr, contentstorage.ErrNotFound) {
			return tracking.Asset{}, false, nil
		}
		if headErr != nil {
			return tracking.Asset{}, false, headErr
		}
		if meta.MIME != old.MIME || meta.Size <= 0 || meta.Size > max {
			return tracking.Asset{}, false, &contentimport.ResolveError{Code: "invalid_stored_media"}
		}
		return old, true, nil
	}
	root := os.Getenv("TRACKING_MEDIA_DIR")
	if !filepath.IsAbs(root) {
		return tracking.Asset{}, false, contentstorage.ErrUnavailable
	}
	info, statErr := os.Lstat(filepath.Join(root, id))
	if errors.Is(statErr, os.ErrNotExist) {
		return tracking.Asset{}, false, nil
	}
	if statErr != nil {
		return tracking.Asset{}, false, statErr
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > max {
		return tracking.Asset{}, false, &contentimport.ResolveError{Code: "invalid_stored_media"}
	}
	file, openErr := os.Open(filepath.Join(root, id))
	if openErr != nil {
		return tracking.Asset{}, false, openErr
	}
	defer file.Close()
	var sniff [512]byte
	n, readErr := file.Read(sniff[:])
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return tracking.Asset{}, false, readErr
	}
	if http.DetectContentType(sniff[:n]) != old.MIME {
		return tracking.Asset{}, false, &contentimport.ResolveError{Code: "invalid_stored_media"}
	}
	return old, true, nil
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
