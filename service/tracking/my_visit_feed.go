package tracking

import (
	"context"
	"sort"
	"strings"
)

// MyVisitFeedItem is one real page view inside the current designer's own
// share graph. Heartbeats enrich the card but never create another card.
type MyVisitFeedItem struct {
	MyVisitEntry
	Visitor           Visitor     `json:"visitor"`
	Cover             *Asset      `json:"cover,omitempty"`
	Type              string      `json:"type,omitempty"`
	Status            string      `json:"status,omitempty"`
	Source            VisitSource `json:"source"`
	ContentPageViews  int         `json:"content_page_views"`
	ProgressAvailable bool        `json:"progress_available"`
}

func myVisitFeedContentKey(entry *MyVisitEntry, shares map[string]Share) string {
	if key, ok := myEventContentKey(Event{Route: entry.Route, Content: entry.Content, Version: entry.Version, Share: entry.ShareID}, shares); ok {
		return key
	}
	return entry.Route + "\x00" + contentKey(entry.Content, entry.Version)
}

func myVisitFeedSourceMatches(source VisitSource, channel string) bool {
	return visitSourceMatches(source, channel)
}

func myVisitFeedMaterialRows(shares []Share) []actorShareRow {
	rows := make([]actorShareRow, 0, len(shares))
	seen := map[struct {
		owner, route, content string
		version               int
	}]bool{}
	for _, share := range shares {
		key := struct {
			owner, route, content string
			version               int
		}{share.Owner, share.Route, share.Content, share.Version}
		if seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, actorShareRow{share: share})
	}
	return rows
}

// MyVisitFeed returns the "客户查看" stream. It deliberately starts from
// myVisitorAggregates so actor/sharer isolation, identity resolution, deletion
// tombstones and the bounded share window stay identical to MyVisitors.
func (s *Service) MyVisitFeed(ctx context.Context, scope Scope, actor int64, f Filter) (List, error) {
	out := List{Items: []MyVisitFeedItem{}}
	if err := s.authorizeMyShares(scope, actor); err != nil {
		return out, err
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	// This endpoint's time window always applies to visits, never share creation.
	f.CreatedWindow = false
	if f.Channel != "" && f.Channel != "all" && !visitChannels[f.Channel] {
		return out, fail(400, "访问来源筛选无效")
	}
	out.Snapshot = Snapshot{AsOf: f.AsOf, Metric: MetricVersion}
	effective := ownerScope(scope, f.Owner)
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		rule, _, ruleErr := activeRule(r, effective, f.AsOf)
		if ruleErr != nil {
			return ruleErr
		}
		ruleFilter := Filter{Start: f.AsOf.AddDate(0, 0, -rule.WindowDays), End: f.AsOf.Add(1), AsOf: f.AsOf, Owner: f.Owner, ShareCursor: f.ShareCursor}
		scanFilter := f
		if ruleFilter.Start.Before(scanFilter.Start) {
			scanFilter.Start = ruleFilter.Start
		}
		if ruleFilter.End.After(scanFilter.End) {
			scanFilter.End = ruleFilter.End
		}
		events, shares, window, err := s.myShareEventScan(ctx, r, scope, actor, scanFilter)
		if err != nil {
			return err
		}
		display := foldMyVisitorAggregates(events, shares, window, f)
		rules := foldMyVisitorAggregates(events, shares, window, ruleFilter)
		out.ShareWindow = window

		facts := make([]factView, 0, len(display.visitors))
		for key := range display.visitors {
			facts = append(facts, factView{PageView: PageView{ID: "my-visit-feed-" + key}, Visitor: key})
		}
		profiles, revision, err := s.queryProfiles(ctx, r, facts)
		if err != nil {
			return err
		}
		out.ProfileRevision = revision
		intentFacts, err := s.myIntentFacts(ctx, r, scope, ruleFilter, rules.visitors, f.AsOf)
		if err != nil {
			return err
		}
		shareIndex := make(map[string]Share, len(shares))
		groups := make([]*visitGroup, 0, len(shares))
		for _, share := range shares {
			shareIndex[share.ID] = share
			groups = append(groups, &visitGroup{share: share.ID})
		}
		materials, err := s.myShareMaterials(ctx, r, effective, myVisitFeedMaterialRows(shares))
		if err != nil {
			return err
		}
		entrySources, err := s.entryShares(r, effective, f, groups)
		if err != nil {
			return err
		}

		counts := map[string]int{}
		for visitor, aggregate := range display.visitors {
			for _, entry := range aggregate.entries {
				if !myVisitFeedSourceMatches(sourceOf(entry.ShareID, entrySources), f.Channel) {
					continue
				}
				counts[visitor+"\x00"+myVisitFeedContentKey(entry, shareIndex)]++
			}
		}
		items := make([]MyVisitFeedItem, 0)
		for key, aggregate := range display.visitors {
			visitor := profiles[key]
			visitor.Last, visitor.Views, visitor.Visible = aggregate.last, aggregate.views, aggregate.visible
			visitor.Sessions, visitor.Shares = len(aggregate.sessions), len(aggregate.shares)
			ruleAggregate := rules.visitors[key]
			if ruleAggregate == nil {
				ruleAggregate = &myVisitorAccumulator{sessions: map[string]bool{}, shares: map[string]bool{}, contents: map[string]bool{}, entries: map[string]*MyVisitEntry{}, entryCovered: map[string][]interval{}}
			}
			visitor.Intent = s.myIntent(effective, key, ruleAggregate, intentFacts, f.AsOf)
			applyBSideDowngrade(profiles, key, &visitor.Intent)
			if !visitorListed(visitor, f) {
				continue
			}
			for _, raw := range aggregate.entries {
				entry := *raw
				source := sourceOf(entry.ShareID, entrySources)
				if !myVisitFeedSourceMatches(source, f.Channel) {
					continue
				}
				item := MyVisitFeedItem{MyVisitEntry: entry, Visitor: visitor, Source: source}
				item.ProgressAvailable = raw.coverageAvailable
				if raw.coverageAvailable {
					item.Progress = raw.coverageProgress
				}
				item.ContentPageViews = counts[key+"\x00"+myVisitFeedContentKey(raw, shareIndex)]
				materialKey, ok := myEventContentKey(Event{Route: entry.Route, Content: entry.Content, Version: entry.Version, Share: entry.ShareID}, shareIndex)
				if ok {
					if material, exists := materials[materialKey]; exists {
						item.Title, item.Cover, item.Type, item.Status = material.Title, material.Cover, material.Type, material.Status
					}
				}
				if strings.TrimSpace(item.Title) == "" || item.Title == item.Route || strings.HasPrefix(item.Title, "pages/") {
					item.Title = s.myRouteTitle(item.Route)
				}
				items = append(items, item)
			}
		}
		sort.Slice(items, func(i, j int) bool {
			if !items[i].Occurred.Equal(items[j].Occurred) {
				return items[i].Occurred.After(items[j].Occurred)
			}
			return items[i].View < items[j].View
		})
		start, end, cursor, first, err := paginateDetail(f, len(items), "my-visit-feed-v1", actorVisitorKey(actor), scope)
		if err != nil {
			return err
		}
		out.Items, out.Cursor, out.FirstCursor = items[start:end], cursor, first
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}
