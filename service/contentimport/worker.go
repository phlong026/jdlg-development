package contentimport

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"service/tracking"
)

type Worker struct {
	Store         JobStore
	Resolvers     map[Platform]DocumentResolver
	Authorize     func(context.Context, Job) error
	SaveMedia     func(context.Context, Job, int, RemoteMedia) (tracking.Asset, error)
	SaveDraft     func(context.Context, Job, Document, []tracking.Asset) (string, error)
	LeaseDuration time.Duration
	PollInterval  time.Duration
	Now           func() time.Time
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	if w.Store == nil || w.Authorize == nil || w.SaveMedia == nil || w.SaveDraft == nil || w.LeaseDuration <= 0 {
		return false, ErrUnavailable
	}
	job, err := w.Store.Claim(ctx, w.now(), w.LeaseDuration)
	if errors.Is(err, ErrNoJob) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	finish := func(status, contentID, code string) error {
		return w.Store.Finish(ctx, job.ID, job.LeaseToken, status, contentID, code)
	}
	if err = w.Authorize(ctx, job); err != nil {
		return true, finish(Blocked, "", "permission_revoked")
	}
	doc := job.Document
	if doc == nil {
		resolver := w.Resolvers[job.Platform]
		if resolver == nil {
			return true, finish(Blocked, "", "provider_not_configured")
		}
		if err = w.Store.BeginResolve(ctx, job.ID, job.LeaseToken); err != nil {
			return true, err
		}
		resolveCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		result, resolveErr := resolver.ResolveDocument(resolveCtx, job.URL)
		cancel()
		if resolveErr != nil {
			status, code := failureState(resolveErr)
			return true, finish(status, "", code)
		}
		if err = validateDocument(result); err != nil {
			status, code := failureState(err)
			return true, finish(status, "", code)
		}
		// After this point restart recovery uses the stored resolution and cannot
		// issue the paid parse request a second time.
		if err = w.Store.SaveResolution(ctx, job.ID, job.LeaseToken, result); err != nil {
			return true, err
		}
		doc = &result
	} else if err = validateDocument(*doc); err != nil {
		status, code := failureState(err)
		return true, finish(status, "", code)
	}
	assets := make([]tracking.Asset, 0, len(doc.Source.Media))
	for i, media := range doc.Source.Media {
		if err = w.Store.Renew(ctx, job.ID, job.LeaseToken, w.now(), w.LeaseDuration); err != nil {
			return true, err
		}
		if err = w.Authorize(ctx, job); err != nil {
			return true, finish(Blocked, "", "permission_revoked")
		}
		asset, saveErr := w.SaveMedia(ctx, job, i, media)
		if saveErr != nil {
			status, code := failureState(saveErr)
			return true, finish(status, "", code)
		}
		if asset.ID == "" || asset.Kind != media.Kind {
			return true, finish(Failed, "", "invalid_stored_media")
		}
		assets = append(assets, asset)
	}
	if err = w.Store.Renew(ctx, job.ID, job.LeaseToken, w.now(), w.LeaseDuration); err != nil {
		return true, err
	}
	if err = w.Authorize(ctx, job); err != nil {
		return true, finish(Blocked, "", "permission_revoked")
	}
	contentID, err := w.SaveDraft(ctx, job, *doc, assets)
	if err != nil {
		status, code := failureState(err)
		return true, finish(status, "", code)
	}
	if contentID == "" {
		return true, finish(Failed, "", "draft_save_failed")
	}
	return true, finish(Ready, contentID, "")
}
func (w *Worker) Run(ctx context.Context) {
	poll := w.PollInterval
	if poll <= 0 {
		poll = 2 * time.Second
	}
	for ctx.Err() == nil {
		jobCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		worked, err := w.RunOnce(jobCtx)
		cancel()
		delay := poll
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[content-import] error worker=import error_type=%T", err)
			delay = 10 * time.Second
		} else if worked {
			delay = 30 * time.Second
		}
		// A single lane and spacing apply across platforms, including after a failed
		// parse. Do not rotate cookies or repeatedly hit a verification page.
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func failureState(err error) (string, string) {
	var contentError *tracking.Error
	if errors.As(err, &contentError) {
		if contentError.Status == 401 || contentError.Status == 403 {
			return Blocked, "permission_revoked"
		}
		if contentError.Status >= 400 && contentError.Status < 500 {
			return Failed, "invalid_draft"
		}
	}
	var known *ResolveError
	if errors.As(err, &known) {
		code := known.Code
		if code == "" || len(code) > 64 {
			return Failed, "import_failed"
		}
		for _, c := range code {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
				return Failed, "import_failed"
			}
		}
		if known.Uncertain {
			return Uncertain, code
		}
		switch code {
		case "disabled", "provider_not_configured", "browser_required", "requires_login", "verification_required", "rate_limited", "unsupported_encryption", "unsupported_embed", "unsupported_video", "risk_control", "media_source_expired", "private_storage_required", "xhs_unknown_source", "invalid_xhs_response":
			return Blocked, code
		}
		return Failed, code
	}
	return Failed, "import_failed"
}
func validateDocument(doc Document) error {
	if doc.Source.Title == "" || doc.Body.Schema != 1 || len(doc.Body.Blocks) == 0 || len(doc.Body.Blocks) > 500 || len(doc.Source.Media) > 50 {
		return fail("invalid_document")
	}
	if len(doc.Warnings) > 0 {
		return fail("unsupported_embed")
	}
	refs := map[string]string{}
	for i, m := range doc.Source.Media {
		if m.Kind != "image" && m.Kind != "video" {
			return fail("invalid_media")
		}
		if m.URL == "" {
			if m.Kind == "video" {
				return fail("missing_video")
			}
			return fail("missing_media")
		}
		refs[fmt.Sprintf("remote-%04d", i)] = m.Kind
	}
	for _, block := range doc.Body.Blocks {
		if block.Kind == "image" || block.Kind == "video" {
			if refs[block.MediaID] != block.Kind {
				return fail("missing_media")
			}
		}
	}
	return nil
}

// VideoDocumentResolver only normalizes provider data. Storage and draft writes
// remain in Worker so no temporary CDN link can masquerade as an imported file.
type VideoDocumentResolver struct{ Resolver Resolver }

func (v VideoDocumentResolver) ResolveDocument(ctx context.Context, link string) (Document, error) {
	if v.Resolver == nil {
		return Document{}, fail("provider_not_configured")
	}
	source, err := v.Resolver.Resolve(ctx, link)
	if err != nil {
		return Document{}, err
	}
	doc := Document{Source: source, Body: tracking.ArticleDocument{Schema: 1}}
	for _, text := range strings.Split(strings.ReplaceAll(source.Text, "\r\n", "\n"), "\n") {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if len(text) > 10000 {
			return Document{}, fail("article_too_large")
		}
		doc.Body.Blocks = append(doc.Body.Blocks, tracking.ArticleBlock{Kind: "paragraph", Text: text})
	}
	for i, m := range source.Media {
		doc.Body.Blocks = append(doc.Body.Blocks, tracking.ArticleBlock{Kind: m.Kind, MediaID: fmt.Sprintf("remote-%04d", i), Caption: m.Caption})
	}
	if source.CoverURL != "" {
		for i, media := range doc.Source.Media {
			if media.Kind == "image" && media.URL == source.CoverURL {
				doc.CoverRef = fmt.Sprintf("remote-%04d", i)
				break
			}
		}
		if doc.CoverRef == "" {
			index := len(doc.Source.Media)
			doc.Source.Media = append(doc.Source.Media, RemoteMedia{ID: source.ID + "-cover", Kind: "image", URL: source.CoverURL})
			doc.CoverRef = fmt.Sprintf("remote-%04d", index)
		}
	}
	return doc, validateDocument(doc)
}
