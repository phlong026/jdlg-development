package tracking

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// Client-initiated shares carry the entry session the sharer was in. Rows
// without one were minted by the management console (QR codes and URL Links);
// they are distribution inputs, not visitor forwarding behavior, and never
// count toward anyone's share metrics.
type shareFact struct {
	id, sharer, route, content string
	version                    int
	at                         time.Time
}

func clientShares(r Repository, scope Scope, f Filter) ([]shareFact, error) {
	rows, err := scopedRecords(r, "share", RecordQuery{Owners: permittedOwners(scope, f.Owner), AtFrom: f.Start, AtBefore: f.End, AtThrough: f.AsOf})
	if err != nil {
		return nil, err
	}
	out := []shareFact{}
	for _, row := range rows {
		var sh Share
		if json.Unmarshal(row.Payload, &sh) != nil {
			continue
		}
		if sh.Session == "" || sh.Sharer == "" || sh.Sharer != row.Visitor {
			continue
		}
		out = append(out, shareFact{id: sh.ID, sharer: sh.Sharer, route: sh.Route, content: sh.Content, version: sh.Version, at: row.At})
	}
	return out, nil
}

// shareCounts tallies client-initiated forwards per visitor inside the filter
// window. The recent tally covers the trailing seven days only when the window
// reaches them; otherwise recent is nil and the caller reports "not covered".
// WeChat never confirms delivery, so this counts initiations, matching the
// client-facing wording.
func shareCounts(r Repository, scope Scope, f Filter) (totals, recent map[string]int, recentCovered bool, err error) {
	shares, err := clientShares(r, scope, f)
	if err != nil {
		return nil, nil, false, err
	}
	cutoff := f.AsOf.Add(-7 * 24 * time.Hour)
	recentCovered = !f.Start.After(cutoff)
	totals, recent = map[string]int{}, map[string]int{}
	for _, sh := range shares {
		totals[sh.sharer]++
		if recentCovered && !sh.at.Before(cutoff) {
			recent[sh.sharer]++
		}
	}
	return totals, recent, recentCovered, nil
}

// VisitorShare is one forwarding action a visitor initiated: what was shared,
// through which channel, and what traffic it brought back. Downstream counts
// come from page views attributed to the share, with the same exclusions as
// every other metric (self visits, previews, tests, abnormal flows).
type VisitorShare struct {
	ID       string    `json:"share_id"`
	At       time.Time `json:"occurred_at"`
	Route    string    `json:"route_key,omitempty"`
	Content  string    `json:"content_id,omitempty"`
	Version  int       `json:"content_version,omitempty"`
	Title    string    `json:"title"`
	Type     string    `json:"type,omitempty"`
	Status   string    `json:"status,omitempty"`
	Channel  string    `json:"channel,omitempty"`
	Visitors int       `json:"brought_visitors"`
	Views    int       `json:"brought_views"`
}

// VisitorShares lists one visitor's forwarding actions inside the filter
// window, newest first. Material metadata follows the same privacy rules as
// sessions: revoked or inaccessible content shows its state, never its body.
func (s *Service) VisitorShares(ctx context.Context, scope Scope, key string, f Filter) (List, error) {
	out := List{Items: []VisitorShare{}}
	if err := s.authorizeView(scope); err != nil {
		return out, err
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		if deleted, e := tombstoned(r, "deletion", key); e != nil {
			return e
		} else if deleted {
			return fail(404, "访客记录不可访问")
		}
		shares, e := clientShares(r, scope, f)
		if e != nil {
			return e
		}
		filtered := shares[:0]
		ids := []string{}
		for _, sh := range shares {
			if sh.sharer != key {
				continue
			}
			filtered = append(filtered, sh)
			ids = append(ids, sh.id)
		}
		if len(filtered) == 0 {
			out.Items = []VisitorShare{}
			return nil
		}
		owners := permittedOwners(scope, f.Owner)
		channels, e := shareChannels(r, owners, key, f)
		if e != nil {
			return e
		}
		broughtVisitors, broughtViews, e := shareDownstream(r, owners, ids, f)
		if e != nil {
			return e
		}
		effective := ownerScope(scope, f.Owner)
		materials := map[string]VisitorContent{}
		items := make([]VisitorShare, 0, len(filtered))
		for _, sh := range filtered {
			item := VisitorShare{ID: sh.id, At: sh.at, Route: sh.route, Content: sh.content, Version: sh.version, Channel: channels[sh.id], Visitors: len(broughtVisitors[sh.id]), Views: broughtViews[sh.id]}
			if sh.content != "" && sh.version > 0 {
				cacheKey := contentKey(sh.content, sh.version) + ":" + sh.route
				material, cached := materials[cacheKey]
				if !cached {
					material, e = s.visitedContent(ctx, r, effective, sh.content, sh.version, sh.route)
					if e != nil {
						return e
					}
					materials[cacheKey] = material
				}
				item.Title, item.Type, item.Status = material.Title, material.Type, material.Status
			} else {
				item.Title = s.Config.Routes[sh.route].Title
			}
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].At.Equal(items[j].At) {
				return items[i].ID < items[j].ID
			}
			return items[i].At.After(items[j].At)
		})
		start, end, cursor, first, e := paginateDetail(f, len(items), "visitor-shares-v1", key, scope)
		if e != nil {
			return e
		}
		out.Items = items[start:end]
		out.Cursor, out.FirstCursor = cursor, first
		return nil
	})
	return out, err
}

// shareChannels maps created share IDs to the channel the visitor shared
// through, read from their share_intent events. Timeline shares reusing a
// cached intent keep their channel; a share without a matching event (first
// timeline tap, lost event) stays empty and the client says "未记录".
func shareChannels(r Repository, owners []string, key string, f Filter) (map[string]string, error) {
	rows, err := scopedRecords(r, "event", RecordQuery{Owners: owners, VisitorKeys: []string{key}, AtFrom: f.Start, AtBefore: f.End})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, row := range rows {
		var ev Event
		if json.Unmarshal(row.Payload, &ev) != nil {
			continue
		}
		if ev.Type != "share_intent" || ev.Share == "" || ev.SourceChannel == "" {
			continue
		}
		if _, ok := out[ev.Share]; !ok {
			out[ev.Share] = ev.SourceChannel
		}
	}
	return out, nil
}

func shareDownstream(r Repository, owners []string, ids []string, f Filter) (visitors map[string]map[string]bool, views map[string]int, err error) {
	rows, err := scopedRecords(r, "event", RecordQuery{Owners: owners, Shares: ids, AtFrom: f.Start, AtBefore: f.End})
	if err != nil {
		return nil, nil, err
	}
	visitors, views = map[string]map[string]bool{}, map[string]int{}
	for _, row := range rows {
		var ev Event
		if json.Unmarshal(row.Payload, &ev) != nil {
			continue
		}
		if ev.Type != "page_view" || ev.Share == "" {
			continue
		}
		if hasFlag(ev, "self") || hasFlag(ev, "preview") || hasFlag(ev, "test") || hasFlag(ev, "abnormal") || hasFlag(ev, "duplicate") {
			continue
		}
		if visitors[ev.Share] == nil {
			visitors[ev.Share] = map[string]bool{}
		}
		visitors[ev.Share][row.Visitor] = true
		views[ev.Share]++
	}
	return visitors, views, nil
}
