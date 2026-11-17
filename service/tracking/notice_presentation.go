package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
)

// BrowseNoticePresentation contains display data, never identity or access proof.
type BrowseNoticePresentation struct {
	VisitorName  string
	ContentTitle string
}

// BrowseNoticePresentation resolves exactly one authorized immutable visit. A
// historical notice does not grant access to a withdrawn share or deleted visit.
func (s *Service) BrowseNoticePresentation(ctx context.Context, scope Scope, actor int64, eventID string) (BrowseNoticePresentation, bool, error) {
	var out BrowseNoticePresentation
	if err := s.authorizeMyShares(scope, actor); err != nil {
		return out, false, err
	}
	if !validID(eventID) {
		return out, false, nil
	}
	eligible := false
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		noticeRow, err := r.Get("notice_event", eventID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var notice NoticeEvent
		if json.Unmarshal(noticeRow.Payload, &notice) != nil {
			return ErrUnavailable
		}
		if notice.ID != eventID || notice.RecipientID != actor || !scope.Allows(notice.Owner) || noticeRow.Owner != notice.Owner {
			return nil
		}
		if notice.Kind != "browse" && notice.Kind != "collection_directory_browse" && notice.Kind != "collection_content_browse" {
			return nil
		}
		row, err := r.Get("event", eventID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var event Event
		if json.Unmarshal(row.Payload, &event) != nil {
			return ErrUnavailable
		}
		if event.ID != eventID || row.Owner != notice.Owner || row.Session == "" || event.Session != row.Session || row.Share != notice.ShareID || row.Content != event.Content {
			return nil
		}
		entry, err := decode[Entry](r, "session", row.Session)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Session != row.Session || entry.Owner != row.Owner || entry.Share != row.Share || event.Share != row.Share {
			return nil
		}
		shares, err := scopedRecords(r, "share", RecordQuery{Owners: []string{notice.Owner}, Shares: []string{notice.ShareID}})
		if err != nil {
			return err
		}
		if len(shares) == 0 {
			return nil
		}
		if len(shares) != 1 {
			return ErrUnavailable
		}
		share, visible, err := decodeActorShare(shares[0], scope, actor)
		if err != nil {
			return err
		}
		if !visible || share.ID != notice.ShareID {
			return nil
		}
		// Session rows advance during navigation. Reconstruct page context from the
		// immutable event, and collection membership from the authoritative share.
		entry.Route, entry.Content, entry.Version = event.Route, event.Content, event.Version
		entry.Visitor = row.Visitor
		entry.CollectionID, entry.CollectionVersion = "", 0
		if share.Route == projectCollectionReaderRoute && event.Route == publishedReaderRoute {
			entry.CollectionID, entry.CollectionVersion = share.Content, share.Version
		} else if event.Route != share.Route || event.Content != share.Content || event.Version != share.Version {
			return nil
		}
		rebuilt, ok, err := BuildBrowseNotice(entry, event, share, NoticeRules{BrowseEnabled: true})
		if err != nil {
			return err
		}
		// All handoff fields must agree; no raw notice field is independently trusted.
		if !ok || !sameBrowseNotice(notice, rebuilt) || noticeRow.Session != row.Session || noticeRow.Visitor != rebuilt.VisitorKey || noticeRow.Content != rebuilt.ContentID || noticeRow.Share != rebuilt.ShareID || !s.Now().Before(share.Expires) {
			return nil
		}
		visitor, err := privacyVisitor(r, row)
		if err != nil {
			return err
		}
		if _, identified := visitorAccount(visitor); !identified && (!strings.HasPrefix(visitor, "a:") || len(visitor) <= 2) {
			return ErrUnavailable
		}
		if visitor == share.Sharer {
			return nil
		}
		for _, item := range [][2]string{{"deletion", visitor}, {"deletion", row.Visitor}, {"deletion_session", row.Session}} {
			_, err := r.Get(item[0], item[1])
			if err == nil {
				return nil
			}
			if !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		if err = s.browseNoticeAvailable(ctx, r, share.Owner, share.Route, share.Content, share.Version); err != nil {
			return noticeAvailabilityError(err)
		}
		if entry.CollectionID != "" {
			if err = s.browseNoticeAvailable(ctx, r, share.Owner, entry.Route, entry.Content, entry.Version); err != nil {
				return noticeAvailabilityError(err)
			}
			if _, err = s.projectCollectionChild(ctx, r, entry.CollectionID, entry.CollectionVersion, entry.Route, entry.Content, entry.Version); err != nil {
				return noticeAvailabilityError(err)
			}
		}
		// Missing presentation may degrade, but failed reads remain retryable.
		presentation := BrowseNoticePresentation{VisitorName: "访客", ContentTitle: s.Config.Routes[event.Route].Title}
		profiles, _, err := s.queryProfiles(ctx, r, []factView{{Visitor: visitor, Owner: row.Owner, Session: row.Session}})
		if err != nil {
			log.Printf("[tracking] browse_notice presentation operation=profile event_id=%s error_type=%T", eventID, err)
			return err
		} else if strings.HasPrefix(visitor, "a:") {
			presentation.VisitorName = "匿名访客"
		} else if _, identified := visitorAccount(visitor); identified {
			presentation.VisitorName = strings.TrimSpace(profiles[visitor].Nickname)
			if presentation.VisitorName == "" {
				presentation.VisitorName = "已登录访客"
			}
		}
		materialShare := share
		materialShare.Route, materialShare.Content, materialShare.Version = entry.Route, entry.Content, entry.Version
		materialScope := scope
		materialScope.Owners = []string{share.Owner}
		materials, err := s.myShareMaterials(ctx, r, materialScope, []actorShareRow{{share: materialShare}})
		if err != nil {
			log.Printf("[tracking] browse_notice presentation operation=material event_id=%s error_type=%T", eventID, err)
			return err
		} else {
			material := materials[entry.Route+"\x00"+contentKey(entry.Content, entry.Version)]
			if material.Accessible && strings.TrimSpace(material.Title) != "" {
				presentation.ContentTitle = material.Title
			}
		}
		if presentation.ContentTitle == "" {
			presentation.ContentTitle = "分享内容"
		}
		out, eligible = presentation, true
		return nil
	})
	if err != nil {
		return BrowseNoticePresentation{}, false, err
	}
	return out, eligible, nil
}

func sameBrowseNotice(a, b NoticeEvent) bool {
	return a.ID == b.ID && a.Kind == b.Kind && a.Owner == b.Owner && a.RecipientID == b.RecipientID && a.VisitorKey == b.VisitorKey && a.ShareID == b.ShareID && a.ContentID == b.ContentID && a.CollectionID == b.CollectionID && a.CollectionVersion == b.CollectionVersion && a.ProjectID == b.ProjectID && a.Route == b.Route && a.OccurredAt.Equal(b.OccurredAt)
}

func noticeAvailabilityError(err error) error {
	var known *Error
	if errors.As(err, &known) && (known.Status == 404 || known.Status == 410) {
		return nil
	}
	return err
}

// available predates retryable notice delivery and maps some content read
// failures to 404. Preserve its callers, but do not hide storage failures here.
func (s *Service) browseNoticeAvailable(ctx context.Context, r Repository, owner, route, content string, version int) error {
	if route == projectCollectionReaderRoute {
		collection, err := s.projectCollectionVersion(r, content, version)
		if err != nil {
			return err
		}
		if collection.Owner != owner {
			return fail(404, "合集不可访问")
		}
		return s.Config.validateRoute(route)
	}
	if route != publishedReaderRoute {
		return s.available(ctx, r, route, content, version)
	}
	if !s.Config.ContentEnabled {
		return fail(404, "内容不可访问")
	}
	if _, err := r.Get("content_revoked", content); err == nil {
		return fail(404, "内容不可访问")
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	for _, item := range [][2]string{{"content", content}, {"version", contentKey(content, version)}} {
		row, err := r.Get(item[0], item[1])
		if errors.Is(err, ErrNotFound) {
			return fail(404, "内容不可访问")
		}
		if err != nil {
			return err
		}
		var value Content
		if json.Unmarshal(row.Payload, &value) != nil {
			return ErrUnavailable
		}
		if value.ID != content || value.Owner != owner || row.Owner != owner || value.Status == "revoked" || item[0] == "version" && (value.Version != version || value.Status != "published") {
			return fail(404, "内容不可访问")
		}
	}
	return s.Config.validateRoute(route)
}
