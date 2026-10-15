package tracking

import (
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// DailySummary contains derived metrics, never raw action or video payloads.
// Disjoint UTC visibility ranges permit exact clipping across business days.
type DailySummary struct {
	Metric          string     `json:"metric_version"`
	Day             string     `json:"business_day"`
	Timezone        string     `json:"business_timezone"`
	View            PageView   `json:"page_view"`
	Visitor         string     `json:"visitor_key"`
	OriginalVisitor string     `json:"original_visitor_key"`
	Owner           string     `json:"owner_id"`
	Session         string     `json:"session_id"`
	Count           bool       `json:"counts_page_view"`
	Last            time.Time  `json:"last_observed_at"`
	Ranges          [][2]int64 `json:"visible_ranges_utc_ms"`
	BuiltAt         time.Time  `json:"built_at"`
	// A segment with fresh visibility in this day ended after the day's end.
	// Raw reduction caps such a segment's last observation at a day-aligned
	// window end, which the compacted ranges alone cannot reproduce.
	Spill bool `json:"segment_continues_next_day,omitempty"`
}
type summaryState struct {
	Metric     string `json:"metric_version"`
	RawExpired bool   `json:"raw_events_expired"`
	// Complete marks states written by a projector that records every field
	// summaryUsable needs; older states only serve expired sessions.
	Complete bool      `json:"complete,omitempty"`
	BuiltAt  time.Time `json:"built_at,omitempty"`
	// DeferredFrom is the earliest accepted event left out because it occurred
	// after the build time (a device clock slightly ahead). Until a rebuild, the
	// projection is exact only for snapshots before that instant.
	DeferredFrom *time.Time `json:"deferred_from,omitempty"`
}

var businessTimezone = time.FixedZone("Asia/Shanghai", 28800)

// SessionRecordLister optionally pushes session filtering into the database.
// All implementations enforce a fail-closed read budget.
type SessionRecordLister interface {
	ListSession(kind, session string) ([]Record, error)
}

func sessionRecords(r Repository, kind, session string) ([]Record, error) {
	if indexed, ok := r.(SessionRecordLister); ok {
		return indexed.ListSession(kind, session)
	}
	return scopedRecords(r, kind, RecordQuery{Sessions: []string{session}})
}
func tombstoned(r Repository, kind, id string) (bool, error) {
	_, e := r.Get(kind, id)
	if errors.Is(e, ErrNotFound) {
		return false, nil
	}
	return e == nil, e
}
func businessDay(t time.Time) time.Time {
	x := t.In(businessTimezone)
	return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, businessTimezone)
}
func summaryID(x DailySummary) string {
	return hash(string(marshal([]any{x.Metric, x.Day, x.Owner, x.OriginalVisitor, x.Session, x.View.ID, x.View.Content, x.View.Version})))
}

// RebuildSessionSummaries runs inside the acceptance/identity transaction.
// Once raw rows expire, retained projections are frozen; reads resolve identity.
func (s *Service) RebuildSessionSummaries(r Repository, session string, now time.Time) error {
	old, e := sessionRecords(r, "daily_summary", session)
	if e != nil {
		return e
	}
	dead, e := tombstoned(r, "deletion_session", session)
	if e != nil {
		return e
	}
	if dead {
		for _, row := range old {
			if e = r.Remove(row.Kind, row.ID); e != nil {
				return e
			}
		}
		return r.Remove("summary_state", session)
	}
	state, e := decode[summaryState](r, "summary_state", session)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return e
	}
	if state.RawExpired {
		return nil
	}
	rows, e := sessionRecords(r, "event", session)
	if e != nil {
		return e
	}
	type accepted struct {
		event Event
		row   Record
	}
	events := []accepted{}
	var deferred *time.Time
	for _, row := range rows {
		visitor := resolvedVisitor(r, row)
		dead, e := tombstoned(r, "deletion", visitor)
		if e != nil {
			return e
		}
		if dead {
			continue
		}
		var ev Event
		if json.Unmarshal(row.Payload, &ev) != nil {
			return ErrUnavailable
		}
		if hasFlag(ev, "self") || hasFlag(ev, "preview") || hasFlag(ev, "test") || hasFlag(ev, "abnormal") || hasFlag(ev, "duplicate") {
			continue
		}
		if ev.Received.After(now) || ev.Occurred.After(now) {
			// Raw reduction admits an event once both times are within as_of.
			at := ev.Occurred
			if ev.Received.After(at) {
				at = ev.Received
			}
			if deferred == nil || at.Before(*deferred) {
				deferred = &at
			}
			continue
		}
		events = append(events, accepted{ev, row})
	}
	sort.Slice(events, func(i, j int) bool {
		a, b := events[i].event, events[j].event
		if a.Occurred.Equal(b.Occurred) {
			return a.ID < b.ID
		}
		return a.Occurred.Before(b.Occurred)
	})
	views := map[string]accepted{}
	for _, a := range events {
		if a.event.Type == "page_view" {
			if _, ok := views[a.event.View]; !ok {
				views[a.event.View] = a
			}
		}
	}
	summaries := map[string]*DailySummary{}
	get := func(p accepted, at time.Time) *DailySummary {
		day := businessDay(at)
		key := p.event.View + "\x00" + day.Format("2006-01-02")
		if x := summaries[key]; x != nil {
			return x
		}
		title := s.Config.Routes[p.event.Route].Title
		if p.event.Content != "" {
			if c, e := decode[Content](r, "version", contentKey(p.event.Content, p.event.Version)); e == nil {
				title = c.Title
			}
		}
		x := &DailySummary{Metric: MetricVersion, Day: day.Format("2006-01-02"), Timezone: "Asia/Shanghai", View: PageView{ID: p.event.View, Route: p.event.Route, Title: title, Content: p.event.Content, Version: p.event.Version, Previous: resolvedPrevious(r, session, p.event.Previous), Share: p.event.Share, Started: p.event.Occurred, Flags: []string{"incomplete"}}, Visitor: resolvedVisitor(r, p.row), OriginalVisitor: p.row.Visitor, Owner: p.row.Owner, Session: session, Last: at, BuiltAt: now, Ranges: [][2]int64{}}
		summaries[key] = x
		return x
	}
	for _, p := range views {
		get(p, p.event.Occurred).Count = true
	}
	covered := []interval{}
	endings := map[string]time.Time{}
	for _, a := range events {
		ev := a.event
		p, ok := views[ev.View]
		if !ok || p.row.Owner != a.row.Owner || resolvedVisitor(r, p.row) != resolvedVisitor(r, a.row) || ev.Occurred.Before(p.event.Occurred) {
			continue
		}
		if ev.Type == "page_end" {
			endings[ev.View] = ev.Occurred
		}
		if ev.Type == "visible_segment" && ev.Delta > 0 {
			lo, hi := ev.Occurred.Add(-time.Duration(ev.Delta)*time.Millisecond).UnixMilli(), ev.Occurred.UnixMilli()
			if lo < p.event.Occurred.UnixMilli() {
				lo = p.event.Occurred.UnixMilli()
			}
			fresh := []interval{{lo, hi}}
			for _, c := range covered {
				next := []interval{}
				for _, f := range fresh {
					if c.hi <= f.lo || c.lo >= f.hi {
						next = append(next, f)
						continue
					}
					if f.lo < c.lo {
						next = append(next, interval{f.lo, c.lo})
					}
					if c.hi < f.hi {
						next = append(next, interval{c.hi, f.hi})
					}
				}
				fresh = next
			}
			covered, _ = addInterval(covered, interval{lo, hi})
			for _, part := range fresh {
				for part.lo < part.hi {
					at := time.UnixMilli(part.lo)
					end := businessDay(at).AddDate(0, 0, 1).UnixMilli()
					if end > part.hi {
						end = part.hi
					}
					x := get(p, at)
					x.Ranges = append(x.Ranges, [2]int64{part.lo, end})
					x.View.Visible += end - part.lo
					if ev.Occurred.UnixMilli() >= businessDay(at).AddDate(0, 0, 1).UnixMilli() {
						x.Spill = true
					}
					last := time.UnixMilli(end)
					if last.After(x.Last) {
						x.Last = last
					}
					part.lo = end
				}
			}
		}
		// Every attached event's day has a bucket, even without visibility, so a
		// window's last observation (e.g. a page_end after midnight) is retained.
		if x := get(p, ev.Occurred); x != nil {
			if ev.Occurred.After(x.Last) {
				x.Last = ev.Occurred
			}
			if hasFlag(ev, "late") {
				x.View.Flags = appendUnique(x.View.Flags, "late")
			}
			if ev.Type == "page_end" {
				at := ev.Occurred
				x.View.Ended = &at
				x.View.Flags = []string{}
			}
		}
	}
	// Collapse adjacent intervals so regular heartbeats become one derived
	// visibility range, rather than retaining one payload per source event.
	for _, x := range summaries {
		if ended, ok := endings[x.View.ID]; ok {
			x.View.Ended = &ended
			flags := []string{}
			for _, flag := range x.View.Flags {
				if flag != "incomplete" {
					flags = append(flags, flag)
				}
			}
			x.View.Flags = flags
		}
		merged := []interval{}
		for _, span := range x.Ranges {
			merged, _ = addInterval(merged, interval{span[0], span[1]})
		}
		x.Ranges = make([][2]int64, 0, len(merged))
		for _, span := range merged {
			x.Ranges = append(x.Ranges, [2]int64{span.lo, span.hi})
		}
	}
	for _, row := range old {
		if e = r.Remove(row.Kind, row.ID); e != nil {
			return e
		}
	}
	var stateRow Record
	for _, x := range summaries {
		day, _ := time.ParseInLocation("2006-01-02", x.Day, businessTimezone)
		// Expire whole daily buckets only after their end, never prematurely
		// remove observations later than a non-midnight maintenance cutoff.
		rec := record("daily_summary", summaryID(*x), x.Owner, day.AddDate(0, 0, 1).Add(-time.Nanosecond), x)
		rec.Session = session
		rec.Visitor = x.Visitor
		rec.Content = x.View.Content
		rec.Share = x.View.Share
		if e = r.Put(rec); e != nil {
			return e
		}
		if stateRow.At.IsZero() || rec.At.After(stateRow.At) {
			stateRow = rec
		}
	}
	state = summaryState{Metric: MetricVersion, Complete: true, BuiltAt: now, DeferredFrom: deferred}
	if len(summaries) == 0 {
		// Record that the session was projected and has nothing to show, so list
		// queries need not rescan its raw events. Owner and visitor keep the state
		// discoverable by owner scope and personal-data deletion.
		entry, e := decode[Entry](r, "session", session)
		if errors.Is(e, ErrNotFound) {
			return r.Remove("summary_state", session)
		}
		if e != nil {
			return e
		}
		stateRow = record("summary_state", session, entry.Owner, now, nil)
		stateRow.Visitor, stateRow.Session = entry.Visitor, session
	}
	stateRow.Kind = "summary_state"
	stateRow.ID = session
	stateRow.Payload = marshal(state)
	return r.Put(stateRow)
}

// PrepareSummaryRetention freezes projections before deleting any raw events,
// including sessions straddling the cutoff. All steps must share a transaction.
func (s *Service) PrepareSummaryRetention(r Repository, removed []Record, now time.Time, summaryBefore ...time.Time) error {
	sessions := map[string]bool{}
	for _, row := range removed {
		if row.Kind == "event" {
			sessions[row.Session] = true
		}
	}
	for session := range sessions {
		if e := s.RebuildSessionSummaries(r, session, now); e != nil {
			return e
		}
		// Legacy raw sessions may have no projection in the reviewed plan.
		// Do not leave newly reconstructed buckets beyond summary retention.
		if len(summaryBefore) > 0 && !summaryBefore[0].IsZero() {
			for _, kind := range []string{"daily_summary", "summary_state"} {
				rows, err := sessionRecords(r, kind, session)
				if err != nil {
					return err
				}
				for _, row := range rows {
					if row.At.Before(summaryBefore[0]) {
						if err = r.Remove(kind, row.ID); err != nil {
							return err
						}
					}
				}
			}
		}
		rec, e := r.Get("summary_state", session)
		if errors.Is(e, ErrNotFound) {
			continue
		}
		if e != nil {
			return e
		}
		var state summaryState
		if json.Unmarshal(rec.Payload, &state) != nil {
			return ErrUnavailable
		}
		state.RawExpired = true
		rec.Payload = marshal(state)
		if e = r.Put(rec); e != nil {
			return e
		}
	}
	return nil
}

// summaryFacts returns expired sessions in the authorized, relevant buckets.
func (s *Service) summaryFacts(r Repository, scope Scope, f Filter) ([]factView, map[string]bool, error) {
	facts, summarized, _, err := s.summaryFactsSelected(r, scope, f, factSelection{}, false)
	return facts, summarized, err
}

// summaryWindowExact reports whether projections reproduce raw reduction for
// the window: bucket days are wholly inside or outside it, or nothing observed
// can lie at or beyond its end. Arbitrary historical windows use raw events.
func summaryWindowExact(f Filter) bool {
	aligned := func(t time.Time) bool { return t.Equal(businessDay(t)) }
	return f.End.After(f.AsOf) || aligned(f.Start) && aligned(f.End)
}

// summaryUsable reports whether a current session's projection equals raw
// reduction at asOf: every accepted event is in it (no acceptance or identity
// change after BuiltAt, no deferred event admitted by asOf) and nothing newer is.
func summaryUsable(state summaryState, asOf time.Time) bool {
	return state.Complete && state.Metric == MetricVersion && !state.BuiltAt.After(asOf) && (state.DeferredFrom == nil || asOf.Before(*state.DeferredFrom))
}

// summaryFactsSelected reduces projections for expired sessions and, when
// current is set, for current sessions whose projection is exact at f.AsOf.
// It returns the facts, the sessions they fully represent, and sessions found
// in projections that still need raw reduction.
func (s *Service) summaryFactsSelected(r Repository, scope Scope, f Filter, selection factSelection, current bool) ([]factView, map[string]bool, []string, error) {
	summarized := map[string]bool{}
	expired := map[string]bool{}
	raw := []string{}
	rows, e := scopedRecords(r, "daily_summary", RecordQuery{Owners: permittedOwners(scope, f.Owner), Sessions: selection.Sessions, VisitorKeys: selection.Visitors, IdentityAsOf: f.AsOf, Content: f.Content, View: selection.View, AtFrom: businessDay(f.Start), AtBefore: businessDay(f.End.Add(-time.Nanosecond)).AddDate(0, 0, 1)})
	if e != nil {
		return nil, nil, nil, e
	}
	rowSessions := make([]string, 0, len(rows))
	for _, row := range rows {
		rowSessions = append(rowSessions, row.Session)
	}
	meta, err := loadSessionMeta(r, rowSessions, false, true, true)
	if err != nil {
		return nil, nil, nil, err
	}
	checked := map[string]bool{}
	for _, row := range rows {
		if checked[row.Session] {
			continue
		}
		checked[row.Session] = true
		state, found := meta.state[row.Session]
		switch {
		case state.RawExpired:
			expired[row.Session] = true
			summarized[row.Session] = true
		case current && found && summaryUsable(state, f.AsOf):
			summarized[row.Session] = true
		case current:
			raw = append(raw, row.Session)
		}
	}
	exact := summaryWindowExact(f)
	hi := f.End
	if f.AsOf.Before(hi) {
		hi = f.AsOf
	}
	// One lookup per session, identity and visitor rather than per bucket row.
	deadSessions, deadVisitors, resolved := meta.deleted, map[string]bool{}, map[string]string{}
	tombstone := func(kind, id string, cache map[string]bool) (bool, error) {
		dead, ok := cache[id]
		if !ok {
			var err error
			if dead, err = tombstoned(r, kind, id); err != nil {
				return false, err
			}
			cache[id] = dead
		}
		return dead, nil
	}
	by := map[string]*factView{}
	for _, row := range rows {
		if !summarized[row.Session] || !scope.Allows(row.Owner) || (f.Owner != "" && row.Owner != f.Owner) {
			continue
		}
		if deadSessions[row.Session] {
			continue
		}
		var x DailySummary
		if json.Unmarshal(row.Payload, &x) != nil {
			return nil, nil, nil, ErrUnavailable
		}
		if x.Metric != MetricVersion {
			continue
		}
		row.Visitor = x.OriginalVisitor
		identityKey := row.Session + "\x00" + row.Visitor
		visitor, ok := resolved[identityKey]
		if !ok {
			visitor = resolvedVisitorAt(r, row, f.AsOf)
			resolved[identityKey] = visitor
		}
		if !includes(selection.Visitors, visitor) {
			continue
		}
		dead, e := tombstone("deletion", visitor, deadVisitors)
		if e != nil {
			return nil, nil, nil, e
		}
		if dead {
			continue
		}
		if f.Content != "" && x.View.Content != f.Content || selection.View != "" && x.View.ID != selection.View {
			continue
		}
		day, e := time.ParseInLocation("2006-01-02", x.Day, businessTimezone)
		if e != nil {
			return nil, nil, nil, ErrUnavailable
		}
		if !day.Before(f.End) || !day.AddDate(0, 0, 1).After(f.Start) {
			continue
		}
		if expired[row.Session] && x.BuiltAt.After(f.AsOf) {
			return nil, nil, nil, fail(409, "原始事件已过期，无法重建此历史收件快照")
		}
		count := x.Count && within(x.View.Started, f)
		visible := int64(0)
		for _, span := range x.Ranges {
			a, b := span[0], span[1]
			if a < f.Start.UnixMilli() {
				a = f.Start.UnixMilli()
			}
			if b > hi.UnixMilli() {
				b = hi.UnixMilli()
			}
			if b > a {
				visible += b - a
			}
		}
		key := row.Owner + "\x00" + visitor + "\x00" + row.Session + "\x00" + x.View.ID
		v := by[key]
		if v == nil {
			// Raw reduction starts a PV's last observation at its start time.
			v = &factView{PageView: x.View, Visitor: visitor, Owner: row.Owner, Session: row.Session, Last: x.View.Started, events: []Event{}}
			v.Visible = 0
			if expired[row.Session] {
				v.Flags = appendUnique(v.Flags, "raw_events_expired")
			}
			by[key] = v
		}
		for _, flag := range x.View.Flags {
			v.Flags = appendUnique(v.Flags, flag)
		}
		v.Count = v.Count || count
		v.Visible += visible
		// A bucket's last observation is its latest event or visible end that day.
		// Raw reduction keeps in-window observations, and caps a fresh segment that
		// continues past the window end at that end.
		last := time.Time{}
		if !x.Last.Before(f.Start) && x.Last.Before(f.End) && !x.Last.After(f.AsOf) {
			last = x.Last
		}
		if visible > 0 && (x.Spill && day.AddDate(0, 0, 1).Equal(f.End) || !exact && !x.Last.Before(f.End)) {
			last = f.End.Add(-time.Nanosecond)
			if last.After(f.AsOf) {
				last = f.AsOf
			}
		}
		if last.After(v.Last) {
			v.Last = last
		}
	}
	out := []factView{}
	for _, v := range by {
		if v.Count || v.Visible > 0 {
			out = append(out, *v)
		}
	}
	return out, summarized, raw, nil
}
