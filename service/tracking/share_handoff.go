package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const shareHandoffLifetime = 5 * time.Minute

type ShareHandoffResult struct {
	Ready   bool      `json:"handoff_ready"`
	Expires time.Time `json:"handoff_expires_at,omitempty"`
}
type shareHandoff struct {
	ShareID       string    `json:"share_id"`
	Route         string    `json:"route_key"`
	Content       string    `json:"content_id"`
	Version       int       `json:"content_version"`
	SourceEvent   string    `json:"source_event_id"`
	SourceVisitor string    `json:"source_visitor"`
	SourceSession string    `json:"source_session"`
	Expires       time.Time `json:"expires_at"`
	Redeemed      bool      `json:"redeemed"`
}
type handoffRedeem struct {
	Key             string `json:"handoff_key"`
	Visitor         string `json:"visitor_key"`
	Client          string `json:"client_instance"`
	FirstPageViewID string `json:"first_page_view_id,omitempty"`
}

// Internal evidence is explicitly persisted but never serialized in API Entry
// responses. An old entry without these fields cannot authorize a web handoff.
func persistedEntry(entry Entry) any {
	return struct {
		Entry
		HandoffKey     string `json:"_handoff_key,omitempty"`
		WebSource      bool   `json:"_web_source,omitempty"`
		ConsentVersion string `json:"_consent_version,omitempty"`
	}{entry, entry.HandoffKey, entry.WebSource, entry.ConsentVersion}
}
func (e *Entry) UnmarshalJSON(raw []byte) error {
	type publicEntry Entry
	var stored struct {
		publicEntry
		HandoffKey     string `json:"_handoff_key"`
		WebSource      bool   `json:"_web_source"`
		ConsentVersion string `json:"_consent_version"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return err
	}
	*e = Entry(stored.publicEntry)
	e.HandoffKey, e.WebSource, e.ConsentVersion = stored.HandoffKey, stored.WebSource, stored.ConsentVersion
	return nil
}
func validHandoffToken(token string) bool {
	return len(token) >= 32 && len(token) <= 128 && validID(token)
}
func handoffEvidenceAbsent(err error) bool {
	if errors.Is(err, ErrNotFound) {
		return true
	}
	var known *Error
	return errors.As(err, &known) && (known.Status == 401 || known.Status == 403 || known.Status == 404 || known.Status == 410)
}

func (s *Service) handoffContentAvailable(r Repository, share Share) (bool, error) {
	if share.Route != publishedReaderRoute || !s.Config.ContentEnabled {
		return false, nil
	}
	if err := s.Config.validateRoute(share.Route); err != nil {
		return false, nil
	}
	if _, err := r.Get("content_revoked", share.Content); err == nil {
		return false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	master, err := decode[Content](r, "content", share.Content)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	version, err := decode[Content](r, "version", contentKey(share.Content, share.Version))
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return master.Status != "revoked" && version.Status == "published" && version.Owner == share.Owner && master.Owner == share.Owner && version.ID == share.Content && version.Version == share.Version, nil
}

// CreateShareHandoff proves one explicit browser-to-mini-program transition.
// It never links identities, creates a Share, or counts a URL Link as a read.
func (s *Service) CreateShareHandoff(ctx context.Context, code, sourceToken, eventID, token string) (ShareHandoffResult, error) {
	var out ShareHandoffResult
	if !s.Config.Enabled || !validID(code) || !validID(sourceToken) || !validID(eventID) || !validHandoffToken(token) {
		return out, nil
	}
	err := s.transaction(ctx, func(r Repository) error {
		share, err := decode[Share](r, "share", hash(code))
		if handoffEvidenceAbsent(err) {
			return nil
		}
		if err != nil {
			return err
		}
		now := s.Now().UTC()
		if share.Revoked || !now.Before(share.Expires) || share.Route != publishedReaderRoute {
			return nil
		}
		available, err := s.handoffContentAvailable(r, share)
		if err != nil {
			return err
		}
		if !available {
			return nil
		}
		source, err := s.entry(r, sourceToken)
		if handoffEvidenceAbsent(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !source.WebSource || source.ConsentVersion != s.Config.ConsentVersion || source.ConsentVersion == "" || source.Account != 0 || !strings.HasPrefix(source.Visitor, "a:") || source.Attribution != "share" || source.Share != share.ID || source.Owner != share.Owner || source.Route != share.Route || source.Content != share.Content || source.Version != share.Version {
			return nil
		}
		row, err := r.Get("event", eventID)
		if handoffEvidenceAbsent(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var event Event
		if json.Unmarshal(row.Payload, &event) != nil {
			return ErrUnavailable
		}
		if event.Type != "page_view" || event.ID != eventID || event.Received.IsZero() || event.Received.After(now) || now.Sub(event.Received) >= shareHandoffLifetime || row.Owner != source.Owner || row.Visitor != source.Visitor || row.Session != source.Session || row.Share != source.Share || event.Session != source.Session || event.Route != source.Route || event.Content != source.Content || event.Version != source.Version || event.Share != source.Share {
			return nil
		}
		key := hash(token)
		if old, err := decode[shareHandoff](r, "share_handoff", key); err == nil {
			if old.ShareID != share.ID || old.SourceEvent != eventID || old.SourceVisitor != source.Visitor || old.SourceSession != source.Session {
				return fail(409, "交接标识已用于其他来源")
			}
			if now.Before(old.Expires) {
				out = ShareHandoffResult{Ready: true, Expires: old.Expires}
			}
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		expires := now.Add(shareHandoffLifetime)
		if share.Expires.Before(expires) {
			expires = share.Expires
		}
		proof := shareHandoff{ShareID: share.ID, Route: share.Route, Content: share.Content, Version: share.Version, SourceEvent: eventID, SourceVisitor: source.Visitor, SourceSession: source.Session, Expires: expires}
		rec := record("share_handoff", key, source.Owner, now, proof)
		rec.Visitor, rec.Session, rec.Share, rec.Content = source.Visitor, source.Session, share.ID, share.Content
		if err = r.Insert(rec); err != nil {
			return err
		}
		out = ShareHandoffResult{Ready: true, Expires: expires}
		return nil
	})
	return out, err
}

func (s *Service) redeemShareHandoff(r Repository, entry *Entry, token string) error {
	if token == "" || !validHandoffToken(token) || entry.WebSource || entry.Attribution != "share" || entry.Share == "" {
		return nil
	}
	key := hash(token)
	row, err := r.Get("share_handoff", key)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var proof shareHandoff
	if json.Unmarshal(row.Payload, &proof) != nil {
		return ErrUnavailable
	}
	if row.Owner != entry.Owner || proof.ShareID != entry.Share || proof.Route != entry.Route || proof.Content != entry.Content || proof.Version != entry.Version || !s.Now().Before(proof.Expires) {
		return nil
	}
	if _, err = r.Get("deletion", proof.SourceVisitor); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if _, err = r.Get("deletion_session", proof.SourceSession); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if old, err := decode[handoffRedeem](r, "handoff_redeem", key); err == nil {
		if !proof.Redeemed {
			return ErrUnavailable
		}
		if old.Key == key && old.Visitor == entry.Visitor && old.Client == entry.Client {
			entry.HandoffKey = key
		}
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	// The source-side consumed bit survives a target-side privacy deletion.
	if proof.Redeemed {
		return nil
	}
	proof.Redeemed = true
	row.Payload = marshal(proof)
	if err = r.Put(row); err != nil {
		return err
	}
	target := record("handoff_redeem", key, entry.Owner, s.Now(), handoffRedeem{Key: key, Visitor: entry.Visitor, Client: entry.Client})
	target.Visitor, target.Session = entry.Visitor, entry.Session
	if err = r.Insert(target); err != nil {
		return err
	}
	entry.HandoffKey = key
	return nil
}

func (s *Service) claimHandoffNotice(r Repository, entry Entry, event Event, wouldNotify bool) (bool, error) {
	if entry.HandoffKey == "" {
		return wouldNotify, nil
	}
	proof, err := decode[shareHandoff](r, "share_handoff", entry.HandoffKey)
	if errors.Is(err, ErrNotFound) {
		return wouldNotify, nil
	}
	if err != nil {
		return false, err
	}
	if !proof.Redeemed || proof.ShareID != entry.Share || proof.Route != entry.Route || proof.Content != entry.Content || proof.Version != entry.Version {
		return false, ErrUnavailable
	}
	if !s.Now().Before(proof.Expires) {
		return wouldNotify, nil
	}
	targetRow, err := r.Get("handoff_redeem", entry.HandoffKey)
	if errors.Is(err, ErrNotFound) {
		return wouldNotify, nil
	}
	if err != nil {
		return false, err
	}
	var target handoffRedeem
	if json.Unmarshal(targetRow.Payload, &target) != nil || target.Key != entry.HandoffKey || target.Visitor != entry.Visitor || target.Client != entry.Client {
		return false, ErrUnavailable
	}
	if target.FirstPageViewID != "" && target.FirstPageViewID != event.View {
		return wouldNotify, nil
	}
	if target.FirstPageViewID == "" {
		target.FirstPageViewID = event.View
		targetRow.Payload = marshal(target)
		if err = r.Put(targetRow); err != nil {
			return false, err
		}
	}
	// Reading occurs even when notifications are disabled. Remember the first
	// view now so enabling notices later cannot suppress an independent reading.
	if !wouldNotify {
		return false, nil
	}
	if _, err = r.Get("deletion", proof.SourceVisitor); err == nil {
		return wouldNotify, nil
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if _, err = r.Get("notice_event", proof.SourceEvent); err == nil {
		return false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	claimID := hash(proof.SourceEvent)
	if _, err = r.Get("handoff_notice", claimID); err == nil {
		return false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	// This is a source-side consumed fact, not an identity bridge. Retaining it
	// after target deletion prevents another pre-issued token from re-notifying.
	claim := record("handoff_notice", claimID, entry.Owner, s.Now(), map[string]bool{"claimed": true})
	claim.Visitor, claim.Session = proof.SourceVisitor, proof.SourceSession
	if err = r.Insert(claim); err != nil {
		return false, err
	}
	return true, nil
}
