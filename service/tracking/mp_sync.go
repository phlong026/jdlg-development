package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

// MPSyncConfig describes one official account whose published articles are
// copied into content drafts.
type MPSyncConfig struct {
	// AppID namespaces article IDs, so two accounts never collide.
	AppID string
	// Owner is the follow-up owner of every synced draft.
	Owner string
	// Rights is the declared right to use the content: "own" or "authorized".
	Rights       string
	MaxArticles  int
	ImportImages bool
	MaxImages    int   // per article
	RunImages    int   // per run, bounds the work of one sync
	ImageBytes   int64 // per picture
}

func (c MPSyncConfig) withDefaults() MPSyncConfig {
	if c.MaxArticles <= 0 {
		c.MaxArticles = 30
	}
	if c.MaxImages <= 0 {
		c.MaxImages = 20
	}
	if c.MaxImages > 50 { // the most media one content may hold
		c.MaxImages = 50
	}
	if c.RunImages <= 0 {
		c.RunImages = 200
	}
	if c.ImageBytes <= 0 {
		c.ImageBytes = 10 << 20
	}
	return c
}

// MediaStore validates and stores one downloaded picture as an authorized media
// asset of owner. It owns the file layout and the image checks.
type MediaStore func(ctx context.Context, owner string, data []byte, name string) (Asset, error)

type MPSyncCounts struct {
	Fetched        int `json:"fetched"`
	Created        int `json:"created"`
	Updated        int `json:"updated"`
	Unchanged      int `json:"unchanged"`
	Skipped        int `json:"skipped"`
	Conflicts      int `json:"conflicts"`
	Failed         int `json:"failed"`
	ImagesImported int `json:"images_imported"`
	ImageFailures  int `json:"image_failures"`
}

// MPSyncProblem names an article that was not synced. Ref is a short digest of
// the article ID, enough to correlate runs without storing article text.
type MPSyncProblem struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

type MPSyncResult struct {
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
	Counts     MPSyncCounts    `json:"counts"`
	Problems   []MPSyncProblem `json:"problems"`
	// StateSaveFailed reports that the run finished but its record (the "last run"
	// the operator sees, and its audit entry) could not be written. The drafts and
	// counts are real either way, so this is a notice, not a failed run.
	StateSaveFailed bool `json:"state_save_failed,omitempty"`
}

// MPSyncState is what the operator sees about the latest run.
type MPSyncState struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	WechatCode int    `json:"wechat_errcode,omitempty"`
	MPSyncResult
}

const maxMPProblems = 20

// Reasons an article is skipped.
const (
	mpDeletedUpstream = "deleted_upstream"
	mpTitleEmpty      = "title_empty"
	mpEmptyBody       = "empty_body"
	mpInvalid         = "invalid"
	mpRevoked         = "revoked"
	mpEditedLocally   = "edited_locally"
	mpFailed          = "failed"
)

type mpMapping struct {
	ContentID string `json:"content_id"`
	// Fingerprint identifies the upstream article as last synced.
	Fingerprint string `json:"fingerprint"`
	// Signature identifies the editable fields of the content as sync last wrote
	// them. A different current signature means a person edited it since.
	Signature string `json:"content_signature"`
	// Images records how pictures were handled when the draft was last written.
	// The fingerprint covers the article's text and picture addresses, not whether
	// the pictures actually arrived, so without this a failed download, a spent
	// per-run budget or image import enabled later would never be retried.
	Images string `json:"images,omitempty"`
}

const (
	mpImagesOff      = "off"      // import was disabled; no pictures were requested
	mpImagesComplete = "complete" // every requested picture is attached
	mpImagesPartial  = "partial"  // at least one picture failed or hit the run budget
)

func contentSignature(c Content) string {
	cover := ""
	if c.Cover != nil {
		cover = c.Cover.ID
	}
	ids := make([]string, 0, len(c.Media))
	for _, m := range c.Media {
		ids = append(ids, m.ID)
	}
	return hash(strings.Join([]string{c.Title, c.Body, c.BodyFormat, c.SourceURL, c.SourceNote, c.Rights, cover, strings.Join(ids, ",")}, "\x00"))
}

func mpRef(articleID string) string { return hash(articleID)[:12] }

type mpPrepared struct {
	draft       Draft
	images      []string
	thumb       string
	fingerprint string
}

// prepareMPArticle maps an article to a validated draft without touching the
// network or the store. reason is non-empty when the article cannot be synced.
func (s *Service) prepareMPArticle(scope Scope, cfg MPSyncConfig, article MPArticle) (prepared mpPrepared, reason string) {
	title := mpTitle(article.Title)
	if title == "" {
		return prepared, mpTitleEmpty
	}
	body, images, err := convertMPContent(article.HTML, cfg.MaxImages)
	if err != nil {
		return prepared, mpInvalid
	}
	if strings.TrimSpace(nodeTextOf(body)) == "" {
		return prepared, mpEmptyBody
	}
	source := ""
	if u := strings.TrimSpace(article.URL); safeURL(u) && strings.HasPrefix(u, "https://mp.weixin.qq.com/") {
		source = u
	}
	draft, err := s.validateDraft(scope, Draft{Title: title, Body: body, BodyFormat: "rich_text", SourceURL: source, SourceNote: "公众号文章自动同步生成的草稿，发布前请人工核对正文与配图。", Rights: cfg.Rights, Owner: cfg.Owner})
	if err != nil {
		var known *Error
		if errors.As(err, &known) && strings.Contains(known.Message, "正文不能为空") {
			return prepared, mpEmptyBody
		}
		return prepared, mpInvalid
	}
	thumb := cleanMPMediaURL(article.ThumbURL)
	prepared = mpPrepared{draft: draft, images: images, thumb: thumb}
	prepared.fingerprint = hash(strings.Join([]string{draft.Title, draft.Body, draft.SourceURL, draft.Rights, thumb, strings.Join(images, ",")}, "\x00"))
	return prepared, ""
}

// SyncOfficialAccount copies the account's published articles into drafts. It
// never publishes, never revokes, and never overwrites a draft a person has
// edited. The acting scope is checked exactly as for a manual save.
func (s *Service) SyncOfficialAccount(ctx context.Context, scope Scope, cfg MPSyncConfig, src MPSource, fetch MediaFetcher, store MediaStore) (MPSyncResult, error) {
	cfg = cfg.withDefaults()
	res := MPSyncResult{StartedAt: s.Now(), Problems: []MPSyncProblem{}}
	if err := s.authorized(scope); err != nil {
		return res, err
	}
	if !scope.Manage || cfg.Owner == "" || !scope.Allows(cfg.Owner) || cfg.AppID == "" || (cfg.Rights != "own" && cfg.Rights != "authorized") {
		return res, fail(403, "同步账号无该归属的素材管理权限或配置不完整")
	}
	articles, err := src.Articles(ctx, cfg.MaxArticles)
	if err != nil {
		res.FinishedAt = s.Now()
		s.recordMPSync(ctx, scope, cfg, res, err)
		return res, err
	}
	res.Counts.Fetched = len(articles)
	problem := func(article MPArticle, reason string) {
		if len(res.Problems) < maxMPProblems {
			res.Problems = append(res.Problems, MPSyncProblem{Ref: mpRef(article.ID), Reason: reason})
		}
	}
	runImages := 0
	for _, article := range articles {
		if err = ctx.Err(); err != nil {
			break
		}
		outcome, reason := s.syncMPArticle(ctx, scope, cfg, article, fetch, store, &runImages, &res.Counts)
		switch outcome {
		case "created":
			res.Counts.Created++
		case "updated":
			res.Counts.Updated++
		case "unchanged":
			res.Counts.Unchanged++
		case "conflict":
			res.Counts.Conflicts++
			problem(article, reason)
		case "skipped":
			res.Counts.Skipped++
			problem(article, reason)
		default:
			res.Counts.Failed++
			problem(article, reason)
		}
	}
	res.FinishedAt = s.Now()
	// The drafts are already written. A failure to record the run must not turn a
	// finished sync into an error that hides its counts, so it is reported on the
	// result instead.
	if recordErr := s.recordMPSync(ctx, scope, cfg, res, err); recordErr != nil {
		res.StateSaveFailed = true
	}
	return res, err
}

func (s *Service) syncMPArticle(ctx context.Context, scope Scope, cfg MPSyncConfig, article MPArticle, fetch MediaFetcher, store MediaStore, runImages *int, counts *MPSyncCounts) (string, string) {
	if article.Deleted {
		// A removal upstream never revokes content here: revoking is a decision
		// for a person, because it stops every existing share.
		return "skipped", mpDeletedUpstream
	}
	prepared, reason := s.prepareMPArticle(scope, cfg, article)
	if reason != "" {
		return "skipped", reason
	}
	key := hash("mp:" + cfg.AppID + ":" + article.ID)
	var current mpMapping
	var known bool
	if err := s.readTransaction(ctx, func(r Repository) error {
		var e error
		current, e = decode[mpMapping](r, "mp_article", key)
		known = e == nil
		if errors.Is(e, ErrNotFound) {
			return nil
		}
		return e
	}); err != nil {
		return "failed", mpFailed
	}
	importImages := cfg.ImportImages && fetch != nil && store != nil
	// retrying marks a pass whose only purpose is to attach missing pictures; the
	// cover and pictures already on the draft are kept to tell whether it did.
	var retrying bool
	var attachedCover string
	var attachedMedia []string
	if known && current.Fingerprint == prepared.fingerprint {
		if !importImages || current.Images == mpImagesComplete {
			return "unchanged", ""
		}
		// Pictures are wanted but were not all attached last time. Retry only a
		// draft nobody has touched: an edited or revoked one is left exactly as
		// it was, as for any other unchanged article, and costs no downloads.
		var retry bool
		if err := s.readTransaction(ctx, func(r Repository) error {
			existing, e := loadCatalogContent(r, "content", current.ContentID)
			if errors.Is(e, ErrNotFound) {
				return nil
			}
			if e != nil {
				return e
			}
			retry = existing.Status != "revoked" && contentSignature(existing) == current.Signature
			if existing.Cover != nil {
				attachedCover = existing.Cover.ID
			}
			for _, m := range existing.Media {
				attachedMedia = append(attachedMedia, m.ID)
			}
			return nil
		}); err != nil {
			return "failed", mpFailed
		}
		if !retry {
			return "unchanged", ""
		}
		retrying = true
	}
	draft := prepared.draft
	imageState := mpImagesOff
	if importImages {
		failuresBefore := counts.ImageFailures
		if prepared.thumb != "" {
			if id, ok := s.importMPMedia(ctx, scope, cfg, fetch, store, prepared.thumb, runImages, counts); ok {
				draft.CoverID = id
			}
		}
		for _, raw := range prepared.images {
			if len(draft.MediaIDs) >= cfg.MaxImages {
				break
			}
			if id, ok := s.importMPMedia(ctx, scope, cfg, fetch, store, raw, runImages, counts); ok {
				draft.MediaIDs = append(draft.MediaIDs, id)
			}
		}
		imageState = mpImagesComplete
		if counts.ImageFailures > failuresBefore {
			imageState = mpImagesPartial
		}
		// Nothing new arrived: leave the draft, and its revision, exactly as it is.
		// A picture that never loads must not rewrite the draft on every run under
		// a person who is reviewing it. If every picture turns out to be attached
		// already (a mapping from before pictures were tracked), only the mapping
		// is updated, so the article settles without touching the content.
		if retrying && draft.CoverID == attachedCover && slices.Equal(draft.MediaIDs, attachedMedia) {
			if imageState == mpImagesComplete {
				current.Images = mpImagesComplete
				if err := s.transaction(ctx, func(r Repository) error {
					return r.Put(record("mp_article", key, cfg.Owner, s.Now(), current))
				}); err != nil {
					return "failed", mpFailed
				}
			}
			return "unchanged", ""
		}
	}
	outcome, outcomeReason := "", ""
	err := s.transaction(ctx, func(r Repository) error {
		now := s.Now()
		mapping, e := decode[mpMapping](r, "mp_article", key)
		exists := e == nil
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		id := ""
		if exists {
			existing, e := loadCatalogContent(r, "content", mapping.ContentID)
			switch {
			case e == nil:
				id = existing.ID
				if existing.Status == "revoked" {
					// Record the article as handled so it is not retried every run.
					outcome, outcomeReason = "skipped", mpRevoked
					mapping.Fingerprint = prepared.fingerprint
					return r.Put(record("mp_article", key, cfg.Owner, now, mapping))
				}
				if contentSignature(existing) != mapping.Signature {
					outcome, outcomeReason = "conflict", mpEditedLocally
					return nil
				}
				draft.Revision = existing.Revision
			case errors.Is(e, ErrNotFound):
				// The mapped content is gone; create a fresh draft below.
			default:
				return e
			}
		}
		saved, e := s.saveContentIn(r, scope, id, draft, now)
		if e != nil {
			return e
		}
		mapping = mpMapping{ContentID: saved.ID, Fingerprint: prepared.fingerprint, Signature: contentSignature(saved), Images: imageState}
		if e = r.Put(record("mp_article", key, cfg.Owner, now, mapping)); e != nil {
			return e
		}
		outcome = "created"
		if id != "" {
			outcome = "updated"
		}
		return nil
	})
	if err != nil {
		var known *Error
		if errors.As(err, &known) && known.Status >= 400 && known.Status < 500 {
			return "skipped", mpInvalid
		}
		return "failed", mpFailed
	}
	return outcome, outcomeReason
}

// importMPMedia returns the stored media ID for one picture URL, downloading it
// only if this owner has not already imported it. Failures cost the picture, not
// the article.
func (s *Service) importMPMedia(ctx context.Context, scope Scope, cfg MPSyncConfig, fetch MediaFetcher, store MediaStore, raw string, runImages *int, counts *MPSyncCounts) (string, bool) {
	key := hash("mp-media:" + cfg.Owner + ":" + raw)
	known := ""
	_ = s.readTransaction(ctx, func(r Repository) error {
		row, err := r.Get("mp_media", key)
		if err != nil {
			return nil
		}
		var mapped struct {
			MediaID string `json:"media_id"`
		}
		if json.Unmarshal(row.Payload, &mapped) == nil && mapped.MediaID != "" {
			if _, err = r.Get("media", mapped.MediaID); err == nil {
				known = mapped.MediaID
			}
		}
		return nil
	})
	if known != "" {
		return known, true
	}
	if *runImages >= cfg.RunImages {
		counts.ImageFailures++
		return "", false
	}
	*runImages++
	data, err := fetch.Fetch(ctx, raw, cfg.ImageBytes)
	if err != nil {
		counts.ImageFailures++
		return "", false
	}
	asset, err := store(ctx, cfg.Owner, data, "公众号配图")
	if err != nil {
		counts.ImageFailures++
		return "", false
	}
	if err = s.transaction(ctx, func(r Repository) error {
		return r.Put(record("mp_media", key, cfg.Owner, s.Now(), map[string]string{"media_id": asset.ID}))
	}); err != nil {
		counts.ImageFailures++
		return "", false
	}
	counts.ImagesImported++
	return asset.ID, true
}

func (s *Service) recordMPSync(ctx context.Context, scope Scope, cfg MPSyncConfig, res MPSyncResult, runErr error) error {
	state := MPSyncState{OK: runErr == nil, MPSyncResult: res}
	if runErr != nil {
		state.Error = "同步失败，请稍后重试"
		var wechat *WechatError
		var known *Error
		switch {
		case errors.As(runErr, &wechat):
			state.WechatCode = wechat.Code
			state.Error = "公众号接口返回错误，请核对账号是否已认证并具备发布接口权限"
		case errors.Is(runErr, context.Canceled), errors.Is(runErr, context.DeadlineExceeded):
			state.Error = "同步被中断，未处理的文章将在下次同步继续"
		case errors.As(runErr, &known):
			state.Error = known.Message
		}
	}
	// A run that was cancelled still records itself, on a context that is not.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return s.transaction(writeCtx, func(r Repository) error {
		now := s.Now()
		if e := r.Put(record("mp_sync_state", cfg.Owner, cfg.Owner, now, state)); e != nil {
			return e
		}
		return audit(r, cfg.Owner, "mp_sync_run", scope.Actor, cfg.Owner, now, map[string]any{"ok": state.OK, "counts": res.Counts})
	})
}

// MPSyncStatus returns the latest recorded run for the owner, if any.
func (s *Service) MPSyncStatus(ctx context.Context, scope Scope, owner string) (MPSyncState, bool, error) {
	var out MPSyncState
	if err := s.authorized(scope); err != nil {
		return out, false, err
	}
	if !scope.Manage || !scope.Allows(owner) {
		return out, false, fail(403, "无素材管理权限")
	}
	found := false
	err := s.readTransaction(ctx, func(r Repository) error {
		state, e := decode[MPSyncState](r, "mp_sync_state", owner)
		if errors.Is(e, ErrNotFound) {
			return nil
		}
		out, found = state, e == nil
		return e
	})
	return out, found, err
}
