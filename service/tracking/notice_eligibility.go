package tracking

import (
	"context"
	"encoding/json"
	"errors"
)

// NoticeEligible rechecks current facts immediately before materialization and
// delivery. A durable event is evidence of a visit, never permanent permission.
func (s *Service) NoticeEligible(ctx context.Context, eventID string, recipient int64, owners ...string) (bool, error) {
	if !validID(eventID) || recipient <= 0 {
		return false, nil
	}
	eligible := false
	err := s.readTransaction(ctx, func(r Repository) error {
		notice, err := decode[NoticeEvent](r, "notice_event", eventID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		allowedOwner := len(owners) == 0
		for _, owner := range owners {
			if owner == notice.Owner {
				allowedOwner = true
			}
		}
		if !allowedOwner || notice.ID != eventID || notice.RecipientID != recipient {
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
		if json.Unmarshal(row.Payload, &event) != nil || event.ID != eventID {
			return ErrUnavailable
		}
		entry, err := decode[Entry](r, "session", row.Session)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
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
		var share Share
		if json.Unmarshal(shares[0].Payload, &share) != nil {
			return ErrUnavailable
		}
		if _, ok, err := BuildBrowseNotice(entry, event, share, NoticeRules{BrowseEnabled: true}); err != nil || !ok {
			return err
		}
		if !s.Now().Before(share.Expires) {
			return nil
		}
		visitor, err := privacyVisitor(r, row)
		if err != nil {
			return err
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
		if err = s.available(ctx, r, share.Route, share.Content, share.Version); err != nil {
			var known *Error
			if errors.As(err, &known) && (known.Status == 404 || known.Status == 410) {
				return nil
			}
			return err
		}
		if entry.CollectionID != "" {
			if _, err = s.projectCollectionChild(ctx, r, entry.CollectionID, entry.CollectionVersion, entry.Route, entry.Content, entry.Version); err != nil {
				var known *Error
				if errors.As(err, &known) && (known.Status == 404 || known.Status == 410) {
					return nil
				}
				return err
			}
		}
		eligible = true
		return nil
	})
	return eligible, err
}
