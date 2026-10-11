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
}
type summaryState struct {
	Metric     string `json:"metric_version"`
	RawExpired bool   `json:"raw_events_expired"`
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
		if ev.Received.After(now) || ev.Occurred.After(now) || hasFlag(ev, "self") || hasFlag(ev, "preview") || hasFlag(ev, "test") || hasFlag(ev, "abnormal") || hasFlag(ev, "duplicate") {
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
					last := time.UnixMilli(end)
					if last.After(x.Last) {
						x.Last = last
					}
					part.lo = end
				}
			}
		}
		key := ev.View + "\x00" + businessDay(ev.Occurred).Format("2006-01-02")
		if x := summaries[key]; x != nil {
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
	if len(summaries) > 0 {
		stateRow.Kind = "summary_state"
		stateRow.ID = session
		stateRow.Payload = marshal(summaryState{Metric: MetricVersion})
		return r.Put(stateRow)
	}
	return r.Remove("summary_state", session)
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
	return s.summaryFactsSelected(r, scope, f, factSelection{})
}
func (s *Service) summaryFactsSelected(r Repository, scope Scope, f Filter, selection factSelection) ([]factView, map[string]bool, error) {
	expired := map[string]bool{}
	rows, e := scopedRecords(r, "daily_summary", RecordQuery{Owners: permittedOwners(scope, f.Owner), Sessions: selection.Sessions, VisitorKeys: selection.Visitors, IdentityAsOf: f.AsOf, Content: f.Content, View: selection.View, AtFrom: businessDay(f.Start), AtBefore: businessDay(f.End.Add(-time.Nanosecond)).AddDate(0, 0, 1)})
	if e != nil {
		return nil, nil, e
	}
	checked := map[string]bool{}
	for _, row := range rows {
		if checked[row.Session] {
			continue
		}
		checked[row.Session] = true
		state, err := decode[summaryState](r, "summary_state", row.Session)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, nil, err
		}
		if state.RawExpired {
			expired[row.Session] = true
		}
	}
	by := map[string]*factView{}
	for _, row := range rows {
		if !expired[row.Session] || !scope.Allows(row.Owner) || (f.Owner != "" && row.Owner != f.Owner) {
			continue
		}
		dead, e := tombstoned(r, "deletion_session", row.Session)
		if e != nil {
			return nil, nil, e
		}
		if dead {
			continue
		}
		var x DailySummary
		if json.Unmarshal(row.Payload, &x) != nil {
			return nil, nil, ErrUnavailable
		}
		if x.Metric != MetricVersion {
			continue
		}
		row.Visitor = x.OriginalVisitor
		visitor := resolvedVisitorAt(r, row, f.AsOf)
		if !includes(selection.Visitors, visitor) {
			continue
		}
		dead, e = tombstoned(r, "deletion", visitor)
		if e != nil {
			return nil, nil, e
		}
		if dead {
			continue
		}
		if f.Content != "" && x.View.Content != f.Content {
			continue
		}
		day, e := time.ParseInLocation("2006-01-02", x.Day, businessTimezone)
		if e != nil {
			return nil, nil, ErrUnavailable
		}
		if !day.Before(f.End) || !day.AddDate(0, 0, 1).After(f.Start) {
			continue
		}
		if x.BuiltAt.After(f.AsOf) {
			return nil, nil, fail(409, "原始事件已过期，无法重建此历史收件快照")
		}
		count := x.Count && within(x.View.Started, f)
		visible := int64(0)
		hi := f.End
		if f.AsOf.Before(hi) {
			hi = f.AsOf
		}
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
		if !count && visible == 0 {
			continue
		}
		key := row.Owner + "\x00" + visitor + "\x00" + row.Session + "\x00" + x.View.ID
		v := by[key]
		if v == nil {
			v = &factView{PageView: x.View, Visitor: visitor, Owner: row.Owner, Session: row.Session, events: []Event{}}
			v.Visible = 0
			v.Flags = appendUnique(v.Flags, "raw_events_expired")
			by[key] = v
		}
		for _, flag := range x.View.Flags {
			v.Flags = appendUnique(v.Flags, flag)
		}
		v.Count = v.Count || count
		v.Visible += visible
		last := x.Last
		if !last.Before(f.End) {
			last = f.End.Add(-time.Nanosecond)
		}
		if last.After(f.AsOf) {
			last = f.AsOf
		}
		if last.After(v.Last) {
			v.Last = last
		}
	}
	out := []factView{}
	for _, v := range by {
		out = append(out, *v)
	}
	return out, expired, nil
}
