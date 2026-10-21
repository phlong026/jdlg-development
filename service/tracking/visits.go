package tracking

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"
)

// Visit is one card of the customer timeline: one session reading one
// material. Refreshes inside the session merge; a later session is a new card.
// The nested Visitor is the same projection the customer list serves.
type Visit struct {
	ID       string         `json:"visit_id"`
	Started  time.Time      `json:"started_at"`
	Visible  int64          `json:"visible_ms"`
	Visitor  Visitor        `json:"visitor"`
	Content  VisitContent   `json:"content"`
	Source   VisitSource    `json:"source"`
	Progress *VisitProgress `json:"progress,omitempty"`
	// ContentViews counts this visitor's page views of this material inside
	// the filter window, the same figure as the visitor material card.
	ContentViews int  `json:"content_page_views"`
	Engaged      bool `json:"engaged"`
}

// VisitContent is the historical title/cover projection, never a body.
type VisitContent struct {
	ID         string `json:"content_id"`
	Version    int    `json:"content_version"`
	Title      string `json:"title"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Cover      *Asset `json:"cover,omitempty"`
	Route      string `json:"route_key,omitempty"`
	Accessible bool   `json:"content_accessible"`
}

// VisitSource says how the reader arrived. Kind is share (opened a share made
// inside the mini program), code_link (a backend-minted code or URL Link, which
// has no mini-program session) or direct. Channel is the sharer's reported
// WeChat channel and stays empty when WeChat did not report one. Forwarder is
// the direct upstream sharer unless that sharer operates the material.
type VisitSource struct {
	Kind      string          `json:"kind"`
	Channel   string          `json:"channel,omitempty"`
	Forwarder *VisitForwarder `json:"forwarder,omitempty"`
}

type VisitForwarder struct {
	Key      string `json:"visitor_key"`
	Nickname string `json:"nickname"`
	State    string `json:"identity_state"`
}

// VisitProgress is video completion (deduplicated, capped at 100%) plus whole
// replays, or the deepest article scroll. It is omitted without data.
type VisitProgress struct {
	Kind    string `json:"kind"`
	Percent int    `json:"percent"`
	Plays   int    `json:"plays,omitempty"`
}

// "看得认真" is a behaviour hint beside the intent label, not an intent level.
const (
	engagedVisibleMS    = 60_000
	engagedContentViews = 3
	engagedVideoPercent = 80
	// Fewer than two whole plays reads as one viewing, not a replay.
	minReportedPlays = 2
)

// visitChannels are the accepted source filters.
var visitChannels = map[string]bool{"app_message": true, "timeline": true, "code_link": true, "direct": true}

// The timeline reads raw engagement events for every served card, so its page
// is smaller than the customer list's.
const (
	visitPageSize    = 20
	maxVisitPageSize = 50
)

type visitGroup struct {
	Visit
	session, owner, share string
	views                 []string
}

func (s *Service) Visits(ctx context.Context, scope Scope, f Filter) (List, error) {
	out := List{Items: []Visit{}}
	if e := s.authorizeView(scope); e != nil {
		return out, e
	}
	switch {
	case f.Limit <= 0:
		f.Limit = visitPageSize
	case f.Limit > maxVisitPageSize:
		f.Limit = maxVisitPageSize
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	overrideLimit, err := readOverrideCursor(f)
	if err != nil {
		return out, err
	}
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	effective := ownerScope(scope, f.Owner)
	pending := []pendingEvaluation{}
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		p, e := s.projectVisitors(ctx, r, scope, f, overrideLimit)
		if e != nil {
			return e
		}
		// Like the customer list, every projected label is audited, not only
		// the served cards: both screens share one evaluation trail.
		out.ProfileRevision, pending = p.profileRevision, p.pending
		groups := groupVisits(p.facts, p.visitors, f)
		// Only a share-based source filter needs every entry share resolved;
		// otherwise the served page is enough.
		shares := map[string]entryShare{}
		filterByShare := f.Channel == "app_message" || f.Channel == "timeline" || f.Channel == "code_link"
		if filterByShare {
			if shares, e = s.entryShares(r, scope, f, groups); e != nil {
				return e
			}
		}
		items := make([]*visitGroup, 0, len(groups))
		seen := map[string]bool{}
		for _, g := range groups {
			if filterByShare && !visitSourceMatches(sourceOf(g.share, shares), f.Channel) || f.Channel == "direct" && g.share != "" {
				continue
			}
			if !seen[g.Visitor.Key] {
				seen[g.Visitor.Key] = true
				if g.Visitor.State == "identified" {
					out.Identified++
				} else {
					out.Anonymous++
				}
			}
			items = append(items, g)
		}
		sort.Slice(items, func(i, j int) bool {
			if !items[i].Started.Equal(items[j].Started) {
				return items[i].Started.After(items[j].Started)
			}
			return items[i].ID < items[j].ID
		})
		start, end, cursor, e := paginateOverrideSnapshot(f, len(items), p.intentData.overrideRevision)
		if e != nil {
			return e
		}
		page := items[start:end]
		if !filterByShare {
			if shares, e = s.entryShares(r, scope, f, page); e != nil {
				return e
			}
		}
		if e = s.attachSources(ctx, r, f, page, shares); e != nil {
			return e
		}
		if e = s.fillVisitPage(ctx, r, scope, effective, f, page); e != nil {
			return e
		}
		visits := make([]Visit, 0, len(page))
		for _, g := range page {
			visits = append(visits, g.Visit)
		}
		out.Items = visits
		out.Cursor = withProfileRevision(cursor, p.profileRevision)
		out.FirstCursor = withProfileRevision(overrideSnapshotCursor(f, 0, p.intentData.overrideRevision), p.profileRevision)
		return nil
	})
	if err != nil {
		return out, err
	}
	// A served label is never left unaudited: failing to record fails the request.
	if err = s.recordIntentEvaluations(ctx, effective, pending); err != nil {
		return List{Items: []Visit{}}, err
	}
	return out, nil
}

// groupVisits merges counted material views of listed visitors per session and
// material version. Plain pages and views started before the window never
// open a card.
func groupVisits(facts []factView, visitors map[string]*Visitor, f Filter) []*visitGroup {
	ordered := append([]factView{}, facts...)
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].Started.Equal(ordered[j].Started) {
			return ordered[i].Started.Before(ordered[j].Started)
		}
		return ordered[i].ID < ordered[j].ID
	})
	contentViews := map[string]int{}
	by := map[string]*visitGroup{}
	groups := []*visitGroup{}
	for _, fact := range ordered {
		if fact.Content == "" || !fact.Count {
			continue
		}
		material := contentKey(fact.Content, fact.Version)
		contentViews[fact.Visitor+"\x00"+material]++
		visitor := visitors[fact.Visitor]
		if visitor == nil || !visitorListed(*visitor, f) {
			continue
		}
		id := fact.Session + ":" + material
		g := by[id]
		if g == nil {
			g = &visitGroup{Visit: Visit{ID: id, Started: fact.Started, Visitor: *visitor, Content: VisitContent{ID: fact.Content, Version: fact.Version, Route: fact.Route}}, session: fact.Session, owner: fact.Owner}
			by[id] = g
			groups = append(groups, g)
		}
		if g.share == "" {
			g.share = fact.Share
		}
		g.Visible += fact.Visible
		g.views = append(g.views, fact.ID)
	}
	for _, g := range groups {
		g.ContentViews = contentViews[g.Visitor.Key+"\x00"+contentKey(g.Content.ID, g.Content.Version)]
	}
	return groups
}

func visitSourceMatches(source VisitSource, channel string) bool {
	switch channel {
	case "", "all":
		return true
	case "app_message", "timeline":
		return source.Kind == "share" && source.Channel == channel
	default:
		return source.Kind == channel
	}
}

// entryShare is an authorized entry share with the channel its sharer reported.
type entryShare struct {
	share   Share
	channel string
}

// sourceOf classifies an entry share. A share record that is gone or belongs
// to an unauthorized owner still reads as a share with no channel.
func sourceOf(id string, shares map[string]entryShare) VisitSource {
	if id == "" {
		return VisitSource{Kind: "direct"}
	}
	es, ok := shares[id]
	if !ok {
		return VisitSource{Kind: "share"}
	}
	if es.share.Origin == "console" || es.share.Origin == "" && es.share.Session == "" {
		return VisitSource{Kind: "code_link"}
	}
	return VisitSource{Kind: "share", Channel: es.channel}
}

// entryShares resolves the given groups' entry shares inside the authorized
// owners, and each share's channel from the share_intent recorded in the
// sharer's session (event rows index the entry share, not the created one).
func (s *Service) entryShares(r Repository, scope Scope, f Filter, groups []*visitGroup) (map[string]entryShare, error) {
	out := map[string]entryShare{}
	ids := []string{}
	for _, g := range groups {
		if g.share != "" {
			ids = appendUnique(ids, g.share)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	owners := permittedOwners(scope, f.Owner)
	rows, err := scopedRecords(r, "share", RecordQuery{Owners: owners, Shares: ids})
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	sessions := []string{}
	var earliest time.Time
	for _, row := range rows {
		var sh Share
		if json.Unmarshal(row.Payload, &sh) != nil {
			return nil, ErrUnavailable
		}
		if !wanted[sh.ID] || !scope.Allows(row.Owner) {
			continue
		}
		out[sh.ID] = entryShare{share: sh}
		if sh.Session != "" {
			sessions = appendUnique(sessions, sh.Session)
			if earliest.IsZero() || row.At.Before(earliest) {
				earliest = row.At
			}
		}
	}
	if len(sessions) == 0 {
		return out, nil
	}
	// A share_intent never precedes its share; the bound skips the sharer's
	// earlier reading in the same session.
	events, err := scopedRecords(r, "event", RecordQuery{Owners: owners, Sessions: sessions, AtFrom: earliest.Add(-time.Minute), AtThrough: f.AsOf})
	if err != nil {
		return nil, err
	}
	for _, row := range events {
		var ev Event
		if json.Unmarshal(row.Payload, &ev) != nil {
			return nil, ErrUnavailable
		}
		es, ok := out[ev.Share]
		if ev.Type != "share_intent" || ev.SourceChannel == "" || !ok || es.channel != "" {
			continue
		}
		es.channel = ev.SourceChannel
		out[ev.Share] = es
	}
	return out, nil
}

// attachSources sets each served card's source and names the direct upstream
// sharer, resolved through same-session login like the reader list. Operators
// of the material (the salesperson's own send) and deleted visitors, including
// sessions mid staged deletion, are never shown as forwarders.
func (s *Service) attachSources(ctx context.Context, r Repository, f Filter, page []*visitGroup, shares map[string]entryShare) error {
	operator := map[string]bool{}
	dead := map[string]bool{}
	deleted := func(kind, key string) (bool, error) {
		cacheKey := kind + "\x00" + key
		if v, ok := dead[cacheKey]; ok {
			return v, nil
		}
		v, err := tombstoned(r, kind, key)
		dead[cacheKey] = v
		return v, err
	}
	forwarders := map[string]*VisitForwarder{}
	sharers := []factView{}
	for _, g := range page {
		g.Source = sourceOf(g.share, shares)
		es, ok := shares[g.share]
		if g.Source.Kind != "share" || !ok || es.share.Sharer == "" {
			continue
		}
		if fw, done := forwarders[g.share]; done {
			g.Source.Forwarder = fw
			continue
		}
		sh := es.share
		forwarders[g.share] = nil
		key := resolvedVisitorAt(r, Record{Visitor: sh.Sharer, Session: sh.Session}, f.AsOf)
		if account, ok := visitorAccount(key); ok && s.IsContentOperator != nil {
			opKey := key + "\x00" + sh.Owner
			isOperator, checked := operator[opKey]
			if !checked {
				var err error
				if isOperator, err = s.IsContentOperator(sourceReadContext(ctx, r), account, sh.Owner); err != nil {
					return err
				}
				operator[opKey] = isOperator
			}
			if isOperator {
				continue
			}
		}
		for _, check := range [][2]string{{"deletion", key}, {"deletion", sh.Sharer}, {"deletion_session", sh.Session}} {
			if check[1] == "" {
				continue
			}
			gone, err := deleted(check[0], check[1])
			if err != nil {
				return err
			}
			if gone {
				key = ""
				break
			}
		}
		if key == "" {
			continue
		}
		fw := &VisitForwarder{Key: key}
		forwarders[g.share] = fw
		g.Source.Forwarder = fw
		sharers = append(sharers, factView{PageView: PageView{ID: "forwarder-" + g.share}, Visitor: key})
	}
	if len(sharers) == 0 {
		return nil
	}
	// Forwarder names are presentation only; they never filter or order the
	// list, so they stay outside the cursor's profile revision.
	profiles, _, err := s.queryProfiles(ctx, r, sharers)
	if err != nil {
		return err
	}
	for _, fw := range forwarders {
		if fw != nil {
			profile := profiles[fw.Key]
			fw.Nickname, fw.State = profile.Nickname, profile.State
		}
	}
	return nil
}

// fillVisitPage resolves material and reads raw engagement events only for the
// served page, so progress costs at most one page of sessions.
func (s *Service) fillVisitPage(ctx context.Context, r Repository, scope, effective Scope, f Filter, page []*visitGroup) error {
	if len(page) == 0 {
		return nil
	}
	sessions := []string{}
	for _, g := range page {
		sessions = appendUnique(sessions, g.session)
	}
	raw, err := s.factsSelected(r, scope, f, factSelection{Sessions: sessions})
	if err != nil {
		return err
	}
	events := map[string][]Event{}
	for _, fact := range raw {
		events[fact.ID] = fact.events
	}
	materials := map[string]VisitContent{}
	for _, g := range page {
		key := g.Content.Route + "\x00" + contentKey(g.Content.ID, g.Content.Version)
		content, ok := materials[key]
		if !ok {
			resolved, err := s.visitedContent(ctx, r, effective, g.Content.ID, g.Content.Version, g.Content.Route)
			if err != nil {
				return err
			}
			content = VisitContent{ID: resolved.ID, Version: resolved.Version, Title: resolved.Title, Type: resolved.Type, Status: resolved.Status, Cover: resolved.Cover, Route: resolved.Route, Accessible: resolved.Accessible}
			materials[key] = content
		}
		g.Content = content
		viewEvents := make([][]Event, 0, len(g.views))
		for _, view := range g.views {
			viewEvents = append(viewEvents, events[view])
		}
		g.Progress = visitProgress(viewEvents)
		g.Engaged = g.Visible >= engagedVisibleMS || g.ContentViews >= engagedContentViews || g.Progress != nil && g.Progress.Kind == "video" && g.Progress.Percent >= engagedVideoPercent
	}
	return nil
}

// visitProgress mirrors the material card's video reduction on one visit:
// per media the peak cumulative unique coverage against its duration, plus
// replays from the accepted watch deltas. Without video it reports the
// deepest scroll.
func visitProgress(views [][]Event) *VisitProgress {
	peak := map[string][2]int64{}
	var watched int64
	var scroll float64
	for _, events := range views {
		for _, ev := range events {
			switch {
			case ev.Type == "scroll_depth" && ev.Scroll > scroll:
				scroll = ev.Scroll
			case (ev.Type == "video_progress" || ev.Type == "video_state") && ev.Media != "" && ev.Duration > 0:
				current := peak[ev.Media]
				if ev.Unique > current[0] {
					current[0] = ev.Unique
				}
				if ev.Duration > current[1] {
					current[1] = ev.Duration
				}
				peak[ev.Media] = current
				watched += ev.Watch
			}
		}
	}
	var unique, duration int64
	for _, p := range peak {
		unique += p[0]
		duration += p[1]
	}
	if duration > 0 {
		out := &VisitProgress{Kind: "video", Percent: int(math.Min(100, math.Round(float64(unique)*100/float64(duration))))}
		if plays := int(watched / duration); plays >= minReportedPlays {
			out.Plays = plays
		}
		return out
	}
	if scroll > 0 {
		return &VisitProgress{Kind: "article", Percent: int(math.Min(100, math.Round(scroll)))}
	}
	return nil
}
