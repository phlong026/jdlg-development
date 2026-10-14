package tracking

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// Older entries, shares and identity links did not populate the indexed
// visitor/session columns. Keep their authoritative JSON semantics in sync
// with the scoped repository predicates rather than silently omitting them.
func privacyRecord(row Record) (Record, error) {
	switch row.Kind {
	case "entry", "session":
		var entry Entry
		if json.Unmarshal(row.Payload, &entry) != nil {
			return row, ErrUnavailable
		}
		row.Visitor, row.Session = entry.Visitor, entry.Session
	case "share":
		var share Share
		if json.Unmarshal(row.Payload, &share) != nil {
			return row, ErrUnavailable
		}
		row.Visitor, row.Share, row.Session = share.Sharer, share.ID, share.Session
	case "identity":
		var link map[string]string
		if json.Unmarshal(row.Payload, &link) != nil || !strings.HasPrefix(link["visitor_key"], "u:") || link["original_visitor_key"] == "" {
			return row, ErrUnavailable
		}
		row.Visitor, row.Session = link["visitor_key"], row.ID
	}
	return row, nil
}

// Privacy writes cannot treat an unavailable/corrupt identity lookup as an
// anonymous identity. Doing so could select an owner or delete a partial graph.
func privacyVisitor(r Repository, row Record) (string, error) {
	row, err := privacyRecord(row)
	if err != nil {
		return "", err
	}
	if row.Session == "" || row.Kind == "identity" {
		return row.Visitor, nil
	}
	linkRow, err := r.Get("identity", row.Session)
	if errors.Is(err, ErrNotFound) {
		return row.Visitor, nil
	}
	if err != nil {
		return "", err
	}
	var link map[string]string
	if json.Unmarshal(linkRow.Payload, &link) != nil {
		return "", ErrUnavailable
	}
	if link["original_visitor_key"] == row.Visitor && strings.HasPrefix(link["visitor_key"], "u:") {
		return link["visitor_key"], nil
	}
	return row.Visitor, nil
}

type visitorDeletion struct {
	rows                                       map[string]Record
	sessions, events, views, sequences, shares map[string]bool
}

func deletionKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func (p *visitorDeletion) add(scope Scope, row Record) error {
	var err error
	row, err = privacyRecord(row)
	if err != nil {
		return err
	}
	if !scope.Allows(row.Owner) {
		return fail(403, "删除需覆盖该账号全部授权范围")
	}
	if row.Session != "" {
		p.sessions[row.Session] = true
	}
	switch row.Kind {
	case "event":
		var event Event
		if json.Unmarshal(row.Payload, &event) != nil {
			return ErrUnavailable
		}
		p.events[row.ID] = true
		if event.View != "" {
			p.views[event.View] = true
		}
		p.sequences[hash(event.View+":"+event.Segment+":"+event.Player+":"+strconv.Itoa(event.Seq))] = true
	case "daily_summary":
		var summary DailySummary
		if json.Unmarshal(row.Payload, &summary) != nil {
			return ErrUnavailable
		}
		if summary.View.ID != "" {
			p.views[summary.View.ID] = true
		}
	case "share":
		if row.Share != "" {
			p.shares[row.Share] = true
		}
	}
	p.rows[row.Kind+"/"+row.ID] = row
	return nil
}

func planVisitorDeletion(r Repository, scope Scope, key string, resuming bool) (*visitorDeletion, error) {
	p := &visitorDeletion{rows: map[string]Record{}, sessions: map[string]bool{}, events: map[string]bool{}, views: map[string]bool{}, sequences: map[string]bool{}, shares: map[string]bool{}}
	// Deliberately do not restrict Owners: full-identity deletion must detect
	// any inaccessible owner, including one represented only by an old entry.
	for _, kind := range []string{"event", "entry", "session", "identity", "business", "share", "daily_summary", "summary_state", "crm", "intent_override", "intent_override_history", "intent_override_boundary", "intent_evaluation"} {
		rows, err := scopedRecords(r, kind, RecordQuery{VisitorKeys: []string{key}})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			visitor, err := privacyVisitor(r, row)
			if err != nil {
				return nil, err
			}
			if visitor != key {
				continue
			}
			if err = p.add(scope, row); err != nil {
				return nil, err
			}
		}
	}
	// Pre-owner-scoped private annotations used the identity itself as ID.
	for _, kind := range []string{"crm", "intent_override", "intent_override_history", "intent_override_boundary", "intent_evaluation"} {
		row, err := r.Get(kind, key)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err = p.add(scope, row); err != nil {
			return nil, err
		}
	}
	if len(p.rows) == 0 && !resuming {
		return nil, fail(404, "访客不可访问")
	}
	// Only verified target sessions expand the graph. Never turn a device's
	// anonymous visitor key into a global alias for the identified account.
	sessions := deletionKeys(p.sessions)
	for _, kind := range []string{"event", "entry", "session", "identity", "business", "share", "daily_summary", "summary_state", "crm", "intent_override", "intent_override_history", "intent_override_boundary", "intent_evaluation", "view_binding"} {
		rows, err := scopedRecords(r, kind, RecordQuery{Sessions: sessions})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if err = p.add(scope, row); err != nil {
				return nil, err
			}
		}
	}
	// Historical shares have no session column. Recover only an unambiguous
	// accepted share_intent relationship; a device-wide anonymous key is not
	// identity proof. Missing or conflicting evidence leaves the old link alone.
	if err := p.addLegacyShares(r, scope); err != nil {
		return nil, err
	}
	for _, related := range []struct {
		kind string
		q    RecordQuery
	}{
		{"event_hash", RecordQuery{IDs: deletionKeys(p.events)}},
		{"sequence", RecordQuery{IDs: deletionKeys(p.sequences)}},
		{"view_binding", RecordQuery{IDs: deletionKeys(p.views)}},
		{"share_request", RecordQuery{Shares: deletionKeys(p.shares)}},
	} {
		rows, err := scopedRecords(r, related.kind, related.q)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if err = p.add(scope, row); err != nil {
				return nil, err
			}
		}
	}
	return p, nil
}

func (p *visitorDeletion) addLegacyShares(r Repository, scope Scope) error {
	candidates := map[string]bool{}
	for _, row := range p.rows {
		if row.Kind != "event" || !p.sessions[row.Session] {
			continue
		}
		var event Event
		if json.Unmarshal(row.Payload, &event) != nil {
			return ErrUnavailable
		}
		if event.Type == "share_intent" && event.Share != "" {
			candidates[event.Share] = true
		}
	}
	shares, err := scopedRecords(r, "share", RecordQuery{Shares: deletionKeys(candidates)})
	if err != nil {
		return err
	}
	eventsByVisitor := map[string][]Record{}
	for _, row := range shares {
		var share Share
		if json.Unmarshal(row.Payload, &share) != nil {
			return ErrUnavailable
		}
		if share.Session != "" || !strings.HasPrefix(share.Sharer, "a:") {
			continue
		}
		// Also inspect this anonymous key's other sessions for conflicting
		// share-intent evidence. Do not resolve the key as an account-wide alias.
		events, loaded := eventsByVisitor[share.Sharer]
		if !loaded {
			events, err = scopedRecords(r, "event", RecordQuery{VisitorKeys: []string{share.Sharer}})
			if err != nil {
				return err
			}
			eventsByVisitor[share.Sharer] = events
		}
		verified, ambiguous := false, false
		for _, eventRow := range events {
			var event Event
			if json.Unmarshal(eventRow.Payload, &event) != nil {
				return ErrUnavailable
			}
			if event.Type != "share_intent" || event.Share != share.ID {
				continue
			}
			if eventRow.Visitor != share.Sharer || eventRow.Owner != row.Owner || eventRow.Session == "" || event.Session != eventRow.Session || event.Route != share.Route || event.Content != share.Content || event.Version != share.Version || event.Received.IsZero() || event.Received.Before(row.At) || !p.sessions[eventRow.Session] {
				ambiguous = true
				continue
			}
			verified = true
		}
		if verified && !ambiguous {
			if err = p.add(scope, row); err != nil {
				return err
			}
		}
	}
	return nil
}
