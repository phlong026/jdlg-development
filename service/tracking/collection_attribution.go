package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const projectCollectionReaderRoute = "pages/collectionreader/collectionreader"

// projectCollectionVersion validates the immutable directory against its
// current master. A published snapshot never outlives a withdrawn collection.
func (s *Service) projectCollectionVersion(r Repository, id string, version int) (publishedProjectCollectionStored, error) {
	var out publishedProjectCollectionStored
	if !s.Config.ContentEnabled || !validID(id) || version < 1 {
		return out, fail(404, "合集不可访问")
	}
	master, err := decode[projectCollectionStored](r, projectCollectionKind, id)
	if errors.Is(err, ErrNotFound) {
		return out, fail(404, "合集不可访问")
	}
	if err != nil {
		return out, err
	}
	if master.ID != id || master.Status == "revoked" || master.Status == "deleted" || master.Version < version {
		return out, fail(404, "合集不可访问")
	}
	out, err = decode[publishedProjectCollectionStored](r, projectCollectionVersionKind, projectCollectionVersionKey(id, version))
	if errors.Is(err, ErrNotFound) {
		return publishedProjectCollectionStored{}, fail(404, "合集不可访问")
	}
	if err != nil {
		return publishedProjectCollectionStored{}, err
	}
	if out.ID != id || out.Version != version || out.Owner != master.Owner || out.Status != "published" {
		return publishedProjectCollectionStored{}, fail(404, "合集不可访问")
	}
	return out, nil
}

func (s *Service) projectCollectionChild(ctx context.Context, r Repository, id string, version int, route, content string, contentVersion int) (publishedProjectCollectionStored, error) {
	collection, err := s.projectCollectionVersion(r, id, version)
	if err != nil {
		return collection, err
	}
	listed := false
	for _, item := range collection.Items {
		if item.RouteKey == route && item.ContentID == content && item.ContentVersion == contentVersion {
			listed = true
			break
		}
	}
	if !listed {
		return publishedProjectCollectionStored{}, fail(404, "合集作品不可访问")
	}
	if err = s.available(ctx, r, route, content, contentVersion); err != nil {
		var known *Error
		if errors.As(err, &known) && (known.Status == 404 || known.Status == 410) {
			return publishedProjectCollectionStored{}, fail(404, "合集作品不可访问")
		}
		return publishedProjectCollectionStored{}, err
	}
	return collection, nil
}

func (s *Service) validateProjectCollectionEntryContext(ctx context.Context, r Repository, in EntryInput) (string, error) {
	if in.CollectionID == "" && in.CollectionVersion == 0 {
		return "", nil
	}
	if !validID(in.CollectionID) || in.CollectionVersion < 1 || in.Route != publishedReaderRoute {
		return "", fail(400, "合集阅读上下文无效")
	}
	collection, err := s.projectCollectionChild(ctx, r, in.CollectionID, in.CollectionVersion, in.Route, in.Content, in.Version)
	if err != nil {
		return "", err
	}
	return collection.Owner, nil
}

func collectionShareMatchesEntry(share Share, in EntryInput) bool {
	if share.Route == in.Route && share.Content == in.Content && share.Version == in.Version {
		return true
	}
	return in.Route == publishedReaderRoute && in.CollectionID != "" &&
		share.Route == projectCollectionReaderRoute && share.Content == in.CollectionID && share.Version == in.CollectionVersion
}

func canContinueProjectCollectionShare(r Repository, old Entry, in EntryInput, now time.Time) (bool, error) {
	if in.Route != projectCollectionReaderRoute {
		return true, nil
	}
	if old.Route != publishedReaderRoute || old.CollectionID != in.Content || old.CollectionVersion != in.Version || old.Attribution != "share" || old.Share == "" || old.Owner == "" {
		return false, nil
	}
	rows, err := scopedRecords(r, "share", RecordQuery{Owners: []string{old.Owner}, Shares: []string{old.Share}})
	if err != nil {
		return false, err
	}
	if len(rows) != 1 {
		return false, nil
	}
	var share Share
	if json.Unmarshal(rows[0].Payload, &share) != nil {
		return false, ErrUnavailable
	}
	return share.ID == old.Share && share.Owner == old.Owner && !share.Revoked && now.Before(share.Expires) &&
		share.Route == projectCollectionReaderRoute && share.Content == in.Content && share.Version == in.Version, nil
}

func (s *Service) projectCollectionShareMaterial(r Repository, share Share) (VisitorContent, error) {
	fallback := VisitorContent{ID: share.Content, Version: share.Version, Route: share.Route, Title: "合集暂不可访问", Type: "项目合集", Status: "已失效"}
	published, err := decode[publishedProjectCollectionStored](r, projectCollectionVersionKind, projectCollectionVersionKey(share.Content, share.Version))
	if errors.Is(err, ErrNotFound) {
		return fallback, nil
	}
	if err != nil {
		return VisitorContent{}, err
	}
	if published.ID != share.Content || published.Version != share.Version || published.Owner != share.Owner {
		return VisitorContent{}, ErrUnavailable
	}
	fallback.Title = published.Title
	master, err := decode[projectCollectionStored](r, projectCollectionKind, share.Content)
	if errors.Is(err, ErrNotFound) {
		return fallback, nil
	}
	if err != nil {
		return VisitorContent{}, err
	}
	if master.ID != share.Content || master.Owner != share.Owner {
		return VisitorContent{}, ErrUnavailable
	}
	if master.Status == "revoked" || master.Status == "deleted" {
		fallback.Status = "已撤回"
		return fallback, nil
	}
	fallback.Status = "已发布"
	fallback.Accessible = true
	if published.Cover != nil && published.Cover.Kind == "image" && safeURL(published.Cover.URL) {
		cover := *published.Cover
		fallback.Cover = &cover
	}
	return fallback, nil
}
