package tracking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Query reads retain the existing tenant transaction/lock and fail closed even
// if a legacy optional metadata lookup ignores a budget error.
func (s *Service) queryTransaction(ctx context.Context, fn func(Repository) error) error {
	return s.transaction(ctx, func(r Repository) error {
		bounded := boundedQueryRepository(r).(*queryRepository)
		err := fn(bounded)
		if bounded.budget.err != nil {
			return bounded.budget.err
		}
		return err
	})
}

type factSelection struct {
	Visitors, Sessions []string
	View               string
}

type factView struct {
	PageView
	Visitor, Owner, Session string
	Count                   bool
	Last                    time.Time
	events                  []Event
}

func (s *Service) Normalize(f Filter) (Filter, error) {
	now := s.Now()
	if f.AsOf.IsZero() {
		f.AsOf = now
	}
	if f.AsOf.After(now) {
		return f, fail(400, "快照时间无效")
	}
	if f.End.IsZero() {
		f.End = f.AsOf.Add(time.Nanosecond)
	}
	days := s.Config.RetentionDays
	if days <= 0 {
		days = 180
	}
	earliest := f.AsOf.AddDate(0, 0, -days)
	if f.Start.IsZero() || f.Start.Before(earliest) {
		f.Start = earliest
	}
	if !f.Start.Before(f.End) {
		return f, fail(400, "时间范围无效")
	}
	if f.Limit <= 0 {
		f.Limit = 30
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	if f.Sort == "" {
		f.Sort = "last_visit"
	}
	if f.Sort != "last_visit" && f.Sort != "page_views" && f.Sort != "visible_ms" {
		return f, fail(400, "排序无效")
	}
	if f.Direction == "" {
		f.Direction = "desc"
	}
	if f.Direction != "asc" && f.Direction != "desc" {
		return f, fail(400, "排序方向无效")
	}
	if f.Identity != "" && f.Identity != "all" && f.Identity != "identified" && f.Identity != "anonymous" {
		return f, fail(400, "身份筛选无效")
	}
	if f.Intent != "" && f.Intent != "all" && f.Intent != "high" && f.Intent != "medium" && f.Intent != "low" && f.Intent != "unknown" {
		return f, fail(400, "意向筛选无效")
	}
	if len(f.Search) > 100 {
		return f, fail(400, "搜索超限")
	}
	return f, nil
}
func resolvedVisitor(r Repository, row Record) string {
	return resolvedVisitorAt(r, row, time.Time{})
}
func resolvedVisitorAt(r Repository, row Record, asOf time.Time) string {
	rec, e := r.Get("identity", row.Session)
	if e != nil || (!asOf.IsZero() && rec.At.After(asOf)) {
		return row.Visitor
	}
	var link map[string]string
	if json.Unmarshal(rec.Payload, &link) == nil && link["original_visitor_key"] == row.Visitor && strings.HasPrefix(link["visitor_key"], "u:") {
		return link["visitor_key"]
	}
	return row.Visitor
}
func (s *Service) authorizeView(scope Scope) error {
	if e := s.authorized(scope); e != nil {
		return e
	}
	if !scope.View {
		return fail(403, "无访问记录查看权限")
	}
	return nil
}
func ownerScope(scope Scope, owner string) Scope {
	if owner != "" {
		if scope.Allows(owner) {
			scope.Owners = []string{owner}
		} else {
			scope.Owners = nil
		}
	}
	return scope
}
func privateKey(owner, key string) string { return hash(owner + "\x00" + key) }

type interval struct{ lo, hi int64 }

func addInterval(all []interval, next interval) ([]interval, int64) {
	if next.hi <= next.lo {
		return all, 0
	}
	before := int64(0)
	for _, i := range all {
		before += i.hi - i.lo
	}
	all = append(all, next)
	sort.Slice(all, func(i, j int) bool { return all[i].lo < all[j].lo })
	out := []interval{}
	for _, v := range all {
		if len(out) == 0 || v.lo > out[len(out)-1].hi {
			out = append(out, v)
		} else if v.hi > out[len(out)-1].hi {
			out[len(out)-1].hi = v.hi
		}
	}
	after := int64(0)
	for _, i := range out {
		after += i.hi - i.lo
	}
	return out, after - before
}
func hasFlag(e Event, f string) bool {
	for _, x := range e.Flags {
		if x == f {
			return true
		}
	}
	return false
}
func (s *Service) facts(r Repository, scope Scope, f Filter) ([]factView, error) {
	return s.factsSelected(r, scope, f, factSelection{})
}
func (s *Service) factsSelected(r Repository, scope Scope, f Filter, selection factSelection) ([]factView, error) {
	if !scope.View {
		return nil, fail(403, "无访问记录查看权限")
	}
	retained, expired, e := s.summaryFactsSelected(r, scope, f, selection)
	if e != nil {
		return nil, e
	}
	owners := permittedOwners(scope, f.Owner)
	// All accepted visible segments are <= 15 seconds. Include segments ending
	// after End that overlap the requested window; never discard the earlier PV.
	candidates, e := scopedRecords(r, "event", RecordQuery{Owners: owners, Sessions: selection.Sessions, VisitorKeys: selection.Visitors, IdentityAsOf: f.AsOf, Content: f.Content, View: selection.View, AtFrom: f.Start, AtBefore: f.End.Add(15 * time.Second), AtThrough: f.AsOf})
	if e != nil {
		return nil, e
	}
	sessions := []string{}
	seenSessions := map[string]bool{}
	for _, row := range candidates {
		if !seenSessions[row.Session] {
			sessions = append(sessions, row.Session)
			seenSessions[row.Session] = true
		}
	}
	// Read the complete authorized session, not just the candidate material or
	// view. Session-wide overlap deduplication depends on the other page views.
	rows, e := scopedRecords(r, "event", RecordQuery{Owners: owners, Sessions: sessions, AtThrough: f.AsOf})
	if e != nil {
		return nil, e
	}
	identities, deleted, deletedSessions := map[string]Record{}, map[string]bool{}, map[string]bool{}
	for _, session := range sessions {
		rec, err := r.Get("identity", session)
		if err == nil {
			identities[session] = rec
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		dead, err := tombstoned(r, "deletion_session", session)
		if err != nil {
			return nil, err
		}
		deletedSessions[session] = dead
		state, err := decode[summaryState](r, "summary_state", session)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if state.RawExpired {
			expired[session] = true
		}
	}
	versions := map[string]string{}
	views := map[string]*factView{}
	events := []struct {
		Event
		Record
	}{}
	for _, row := range rows {
		if expired[row.Session] {
			continue
		}
		if !scope.Allows(row.Owner) || (f.Owner != "" && row.Owner != f.Owner) {
			continue
		}
		visitor := row.Visitor
		if link, ok := identities[row.Session]; ok && !link.At.After(f.AsOf) {
			var value map[string]string
			if json.Unmarshal(link.Payload, &value) != nil {
				return nil, ErrUnavailable
			}
			if value["original_visitor_key"] == row.Visitor && strings.HasPrefix(value["visitor_key"], "u:") {
				visitor = value["visitor_key"]
			}
		}

		dead, checked := deleted[visitor]
		if !checked {
			var err error
			dead, err = tombstoned(r, "deletion", visitor)
			if err != nil {
				return nil, err
			}
			deleted[visitor] = dead
		}
		if deletedSessions[row.Session] || dead {
			continue
		}

		var ev Event
		if json.Unmarshal(row.Payload, &ev) != nil {
			return nil, ErrUnavailable
		}
		if ev.Received.After(f.AsOf) || ev.Occurred.After(f.AsOf) || hasFlag(ev, "self") || hasFlag(ev, "preview") || hasFlag(ev, "test") || hasFlag(ev, "abnormal") || hasFlag(ev, "duplicate") {
			continue
		}
		row.Visitor = visitor
		events = append(events, struct {
			Event
			Record
		}{ev, row})
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Occurred.Equal(events[j].Occurred) {
			return events[i].Event.ID < events[j].Event.ID
		}
		return events[i].Occurred.Before(events[j].Occurred)
	})
	// First construct established PVs; out-of-order arrival never creates a view from a heartbeat.
	for _, ev := range events {
		if ev.Type != "page_view" {
			continue
		}
		if _, ok := views[ev.View]; ok {
			continue
		}
		title := s.Config.Routes[ev.Route].Title
		if ev.Event.Content != "" {
			key := contentKey(ev.Event.Content, ev.Version)
			if cached, ok := versions[key]; ok {
				title = cached
			} else {
				if c, e := decode[Content](r, "version", key); e == nil {
					title = c.Title
				}
				versions[key] = title
			}
		}
		views[ev.View] = &factView{PageView: PageView{ID: ev.View, Route: ev.Route, Title: title, Content: ev.Event.Content, Version: ev.Version, Previous: resolvedPrevious(r, ev.Record.Session, ev.Previous), Share: ev.Event.Share, Started: ev.Occurred, Flags: []string{"incomplete"}}, Visitor: ev.Record.Visitor, Owner: ev.Owner, Session: ev.Record.Session, Count: within(ev.Occurred, f), Last: ev.Occurred, events: []Event{}}
	}
	covered := map[string][]interval{}
	for _, x := range events {
		ev := x.Event
		ev.Previous = resolvedPrevious(r, x.Record.Session, ev.Previous)
		v := views[ev.View]
		if v == nil || v.Session != x.Record.Session || v.Visitor != x.Record.Visitor || v.Owner != x.Record.Owner {
			continue
		}
		if ev.Type == "page_end" {
			at := ev.Occurred
			if !at.Before(v.Started) {
				v.Ended = &at
				v.Flags = []string{}
			}
		}
		if ev.Type == "visible_segment" && ev.Delta > 0 {
			lo := ev.Occurred.Add(-time.Duration(ev.Delta) * time.Millisecond)
			hi := ev.Occurred
			if lo.Before(f.Start) {
				lo = f.Start
			}
			if lo.Before(v.Started) {
				lo = v.Started
			}
			if hi.After(f.End) {
				hi = f.End
			}
			var added int64
			covered[v.Session], added = addInterval(covered[v.Session], interval{lo.UnixMilli(), hi.UnixMilli()})
			v.Visible += added
			if added > 0 {
				last := hi
				if !last.Before(f.End) {
					last = f.End.Add(-time.Nanosecond)
				}
				if last.After(v.Last) {
					v.Last = last
				}
			}
		}
		if within(ev.Occurred, f) {
			if ev.Occurred.After(v.Last) {
				v.Last = ev.Occurred
			}
			v.events = append(v.events, ev)
			if hasFlag(ev, "late") {
				v.Flags = appendUnique(v.Flags, "late")
			}
		}
	}
	out := append([]factView{}, retained...)
	for _, v := range views {
		if (v.Count || v.Visible > 0) && (f.Content == "" || v.Content == f.Content) && includes(selection.Visitors, v.Visitor) && (selection.View == "" || v.ID == selection.View) {
			out = append(out, *v)
		}
	}
	return out, nil
}
func appendUnique(a []string, s string) []string {
	for _, v := range a {
		if v == s {
			return a
		}
	}
	return append(a, s)
}

type intentFacts struct {
	rule             RuleConfig
	mixedRules       bool
	views            map[string][]factView
	business         map[string][]Record
	overrides        map[string]Record
	overrideRevision int64
	crm              map[string]Record
}

func (s *Service) loadIntentFacts(ctx context.Context, r Repository, scope Scope, now time.Time, visitors []string, overrideLimit ...*int64) (intentFacts, error) {
	data := intentFacts{views: map[string][]factView{}, business: map[string][]Record{}, overrides: map[string]Record{}, crm: map[string]Record{}}
	var ruleErr error
	data.rule, data.mixedRules, ruleErr = activeRule(r, scope, now)
	if ruleErr != nil {
		return data, ruleErr
	}
	if s.Config.IntentEnabled && !data.mixedRules {
		facts, e := s.factsSelected(r, scope, Filter{Start: now.AddDate(0, 0, -data.rule.WindowDays), End: now.Add(time.Nanosecond), AsOf: now}, factSelection{Visitors: visitors})
		if e != nil {
			return data, e
		}
		for _, v := range facts {
			data.views[v.Visitor] = append(data.views[v.Visitor], v)
		}
		business, e := scopedRecords(r, "business", RecordQuery{Owners: permittedOwners(scope, ""), AtFrom: now.AddDate(0, 0, -data.rule.WindowDays), AtThrough: now, VisitorKeys: visitors, IdentityAsOf: now})
		if e != nil {
			return data, e
		}
		verifiedSources := map[string]bool{}
		verifyContext, cancelVerify := context.WithTimeout(sourceReadContext(ctx, r), 2*time.Second)
		defer cancelVerify()
		for _, b := range business {
			if !scope.Allows(b.Owner) || b.At.Before(now.AddDate(0, 0, -data.rule.WindowDays)) || b.At.After(now) {
				continue
			}
			if s.VerifyBusiness != nil {
				proof, ok := sourceBusinessProof(b)
				if !ok {
					continue
				}
				cacheKey := string(marshal(proof))
				valid, checked := verifiedSources[cacheKey]
				if !checked {
					var verifyErr error
					valid, verifyErr = s.VerifyBusiness(verifyContext, proof)
					if verifyErr != nil {
						return data, ErrUnavailable
					}
					verifiedSources[cacheKey] = valid
				}
				if !valid {
					continue
				}
			}
			key := resolvedVisitorAt(r, b, now)
			data.business[key] = append(data.business[key], b)
		}
		var limit *int64
		if len(overrideLimit) != 0 {
			limit = overrideLimit[0]
		}
		data.overrides, data.overrideRevision, e = loadIntentOverrides(r, scope, now, visitors, limit)
		if e != nil {
			return data, e
		}
	}
	if scope.CRM {
		rows, e := scopedRecords(r, "crm", RecordQuery{Owners: permittedOwners(scope, ""), AtThrough: now, VisitorKeys: visitors, IdentityAsOf: now})
		if e != nil {
			return data, e
		}
		for _, rec := range rows {
			if scope.Allows(rec.Owner) && !rec.At.After(now) {
				data.crm[rec.ID] = rec
			}
		}
	}
	return data, nil
}
func (s *Service) intent(scope Scope, key string, now time.Time, data intentFacts) Intent {
	rule := data.rule
	in := Intent{overrideRevision: data.overrideRevision, Level: "unknown", Reasons: []string{"证据不足，待判断"}, Rule: rule.Version, Start: now.AddDate(0, 0, -rule.WindowDays), End: now, Calculated: now, Thresholds: rule.RuleParameters, Exclusions: rule.Exclusions, ParticipatingMetrics: rule.Metrics, Timezone: rule.Timezone, OverrideState: "none", Evidence: []IntentEvidence{}}
	if data.mixedRules {
		in.ScopeRequired = true
		in.Rule = "mixed"
		in.Reasons = []string{"所选归属使用不同规则，请选择单一归属查看意向参考"}
		return in
	}
	if !s.Config.IntentEnabled {
		return in
	}
	facts := append([]factView{}, data.views[key]...)
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Started.Equal(facts[j].Started) {
			return facts[i].ID < facts[j].ID
		}
		return facts[i].Started.Before(facts[j].Started)
	})
	sessions := map[string]bool{}
	sessionDays := map[string]int{}
	validSessions := map[string]bool{}
	evidence := []IntentEvidence{}
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	for _, v := range facts {
		if v.Visitor != key {
			continue
		}
		validSessions[v.Session] = true
		if sessions[v.Session] {
			continue
		}
		sessions[v.Session] = true
		at := v.Started
		if at.Before(in.Start) {
			at = in.Start
		}
		day := at.In(zone).Format("2006-01-02")
		if sessionDays[day] >= rule.DailySessionCap {
			continue
		}
		sessionDays[day]++
		in.Metrics.Sessions++
		evidence = append(evidence, IntentEvidence{Kind: "page_view", ID: v.ID, View: v.ID, Session: v.Session, At: v.Started, Content: v.Content, Version: v.Version})
	}
	business := append([]Record{}, data.business[key]...)
	sort.Slice(business, func(i, j int) bool {
		if business[i].At.Equal(business[j].At) {
			return business[i].ID < business[j].ID
		}
		return business[i].At.Before(business[j].At)
	})
	actionDays := map[string]int{}
	seenBusiness := map[string]bool{}
	for _, b := range business {
		if !scope.Allows(b.Owner) || b.At.Before(in.Start) || b.At.After(now) || seenBusiness[b.ID] || !validSessions[b.Session] {
			continue
		}
		seenBusiness[b.ID] = true
		var detail map[string]string
		if json.Unmarshal(b.Payload, &detail) != nil {
			continue
		}
		action := detail["action"]
		if action != "favorite" && action != "consultation" && action != "appointment" && action != "quote" {
			continue
		}
		if detail["quality"] == "abnormal" || detail["quality"] == "test" || detail["quality"] == "preview" || detail["quality"] == "self" {
			continue
		}
		bucket := action + ":" + b.At.In(zone).Format("2006-01-02")
		if actionDays[bucket] >= rule.DailyActionCap {
			continue
		}
		actionDays[bucket]++
		if action == "favorite" {
			in.Metrics.Favorites++
		} else {
			in.Metrics.BusinessResults++
		}
		evidence = append(evidence, IntentEvidence{Kind: "business", ID: b.ID, Session: b.Session, Action: action, At: b.At})
	}
	in.sourceFingerprint = hash(string(marshal(evidence)))
	in.Evidence = evidence
	if len(evidence) > 100 {
		in.Evidence = append([]IntentEvidence{}, evidence[:100]...)
		in.EvidenceTruncated = true
	}
	if len(validSessions) > 0 && !strings.HasPrefix(key, "a:") {
		in.Level = "low"
		in.Reasons = []string{"当前可观测行为较少；不表示没有需求"}
		if in.Metrics.Sessions > 1 {
			in.Reasons = append(in.Reasons, "窗口内有效独立会话 "+strconv.Itoa(in.Metrics.Sessions)+" 次；复访本身不证明成交意愿")
		}
		if in.Metrics.BusinessResults >= rule.HighBusinessResults {
			in.Level = "high"
			in.Reasons = []string{"近" + strconv.Itoa(rule.WindowDays) + "天内服务端确认业务请求 " + strconv.Itoa(in.Metrics.BusinessResults) + " 次；具体行为见证据，需人工核实"}
		} else if in.Metrics.Sessions >= rule.MediumSessions && in.Metrics.Favorites >= rule.MediumFavorites {
			in.Level = "medium"
			in.Reasons = []string{"近" + strconv.Itoa(rule.WindowDays) + "天跨 " + strconv.Itoa(in.Metrics.Sessions) + " 个有效会话访问，并有 " + strconv.Itoa(in.Metrics.Favorites) + " 次服务端确认收藏；仅供跟进参考"}
		}
	}
	in.OriginalRule = &IntentRule{Level: in.Level, Reasons: append([]string{}, in.Reasons...), Rule: in.Rule, Start: in.Start, End: in.End}
	// An override remains private to its owner, and cannot make anonymous data a real identity.
	if len(scope.Owners) == 1 {
		rec, ok := data.overrides[privateKey(scope.Owners[0], key)]
		if ok && rec.Owner == scope.Owners[0] && !rec.At.After(now) {
			var manual IntentOverride
			if json.Unmarshal(rec.Payload, &manual) == nil {
				if manual.Created.IsZero() {
					manual.Created = rec.At
				}
				if manual.Expires.After(now) && !strings.HasPrefix(key, "a:") {
					in.Override = &manual
					in.Note = manual.Reason
					in.Level = manual.Level
					in.OverrideState = "active"
				} else {
					in.OverrideState = "expired"
				}
			}
		}
	}
	return in
}
func (s *Service) Visitors(ctx context.Context, scope Scope, f Filter) (List, error) {
	out := List{Items: []Visitor{}}
	if e := s.authorizeView(scope); e != nil {
		return out, e
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
	err = s.queryTransaction(ctx, func(r Repository) error {
		facts, e := s.facts(r, scope, f)
		if e != nil {
			return e
		}
		by := map[string]*Visitor{}
		ss := map[string]map[string]bool{}
		for _, v := range facts {
			item := by[v.Visitor]
			if item == nil {
				state := "identified"
				name := "已识别账号"
				if strings.HasPrefix(v.Visitor, "a:") {
					state = "anonymous"
					suffix := v.Visitor
					if len(suffix) > 6 {
						suffix = suffix[len(suffix)-6:]
					}
					name = "匿名访客 " + suffix
				}
				item = &Visitor{Key: v.Visitor, Nickname: name, State: state}
				by[v.Visitor] = item
				ss[v.Visitor] = map[string]bool{}
			}
			if v.Count {
				item.Views++
			}
			item.Visible += v.Visible
			if v.Last.After(item.Last) {
				item.Last = v.Last
			}
			ss[v.Visitor][v.Session] = true
		}
		effective := ownerScope(scope, f.Owner)
		intentData, e := s.loadIntentFacts(ctx, r, effective, f.AsOf, visitorKeys(by), overrideLimit)
		if e != nil {
			return e
		}
		items := []Visitor{}
		for key, v := range by {
			v.Sessions = len(ss[key])
			v.Intent = s.intent(effective, key, f.AsOf, intentData)
			if _, e = s.recordIntentEvaluation(r, effective, key, v.Intent); e != nil {
				return e
			}
			if scope.CRM && len(effective.Owners) == 1 {
				rec, ok := intentData.crm[privateKey(effective.Owners[0], key)]
				if ok && rec.Owner == effective.Owners[0] && !rec.At.After(f.AsOf) {
					var link map[string]string
					if json.Unmarshal(rec.Payload, &link) == nil {
						v.CRMLabel = link["business_number"]
					}
				}
			}

			if f.Identity != "" && f.Identity != "all" && v.State != f.Identity {
				continue
			}
			if f.Intent != "" && f.Intent != "all" && v.Intent.Level != f.Intent {
				continue
			}
			if f.Search != "" && !strings.Contains(strings.ToLower(v.Nickname+" "+v.CRMLabel), strings.ToLower(f.Search)) {
				continue
			}
			if v.State == "identified" {
				out.Identified++
			} else {
				out.Anonymous++
			}
			items = append(items, *v)
		}
		sort.Slice(items, func(i, j int) bool {
			a, b := items[i], items[j]
			cmp := 0
			switch f.Sort {
			case "page_views":
				cmp = a.Views - b.Views
			case "visible_ms":
				if a.Visible < b.Visible {
					cmp = -1
				} else if a.Visible > b.Visible {
					cmp = 1
				}
			default:
				if a.Last.Before(b.Last) {
					cmp = -1
				} else if a.Last.After(b.Last) {
					cmp = 1
				}
			}
			if cmp == 0 {
				return a.Key < b.Key
			}
			if f.Direction == "asc" {
				return cmp < 0
			}
			return cmp > 0
		})
		start, end, cursor, e := paginateOverrideSnapshot(f, len(items), intentData.overrideRevision)
		if e != nil {
			return e
		}
		out.Items = items[start:end]
		out.Cursor = cursor
		out.FirstCursor = overrideSnapshotCursor(f, 0, intentData.overrideRevision)
		return nil
	})
	return out, err
}
func paginate(f Filter, total int) (int, int, string, error) {
	signatureFilter := f
	signatureFilter.Cursor = ""
	sig := hash(string(marshal(signatureFilter)))
	start := 0
	if f.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(f.Cursor)
		if e != nil {
			return 0, 0, "", fail(400, "游标无效")
		}
		var c struct {
			Offset    int    `json:"offset"`
			Signature string `json:"signature"`
		}
		if json.Unmarshal(b, &c) != nil || c.Offset < 0 || c.Offset > 1000000 || c.Signature != sig {
			return 0, 0, "", fail(400, "游标与筛选不一致")
		}
		start = c.Offset
	}
	if start > total {
		start = total
	}
	end := start + f.Limit
	if end > total {
		end = total
	}
	cursor := ""
	if end < total {
		cursor = base64.RawURLEncoding.EncodeToString(marshal(map[string]any{"offset": end, "signature": sig}))
	}
	return start, end, cursor, nil
}
func (s *Service) Contents(ctx context.Context, scope Scope, f Filter, id string) (List, error) {
	out := List{Items: []Content{}}
	if e := s.authorized(scope); e != nil {
		return out, e
	}
	if !scope.View && !scope.Manage {
		return out, fail(403, "无素材访问权限")
	}
	// Visitor-only filters must not silently alter the content view.
	f.Search, f.Identity, f.Intent = "", "", ""
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	err = s.queryTransaction(ctx, func(r Repository) error {
		rows, e := scopedRecords(r, "content", RecordQuery{Owners: permittedOwners(scope, f.Owner), Content: id, CatalogAsOf: f.AsOf})
		if e != nil {
			return e
		}
		legacyRows, err := scopedRecords(r, "legacy_catalog", RecordQuery{Owners: permittedOwners(scope, f.Owner), Content: id, CatalogAsOf: f.AsOf})
		if err != nil {
			return err
		}
		// A source may be shared into several authorized owner scopes. Show one
		// material card with scoped aggregate facts, not duplicate totals.
		latestLegacy := map[string]Record{}
		for _, row := range legacyRows {
			if !scope.Allows(row.Owner) || (f.Owner != "" && row.Owner != f.Owner) {
				continue
			}
			var c Content
			if json.Unmarshal(row.Payload, &c) != nil {
				return ErrUnavailable
			}
			if catalogCreatedAt(c, row.At).After(f.AsOf) {
				continue
			}
			old, ok := latestLegacy[c.ID]
			if !ok || row.At.After(old.At) || (row.At.Equal(old.At) && row.ID < old.ID) {
				latestLegacy[c.ID] = row
			}
		}
		for _, row := range latestLegacy {
			rows = append(rows, row)
		}
		facts := []factView{}
		if scope.View {
			metricsFilter := f
			if id != "" && metricsFilter.Content == "" {
				metricsFilter.Content = id
			}
			facts, e = s.facts(r, scope, metricsFilter)
			if e != nil {
				return e
			}
		}
		summaries := map[string]Content{}
		for _, v := range facts {
			c := summaries[v.Content]
			if v.Count {
				c.PageViews++
			}
			c.VisibleMS += v.Visible
			if c.LastVisit == nil || v.Last.After(*c.LastVisit) {
				at := v.Last
				c.LastVisit = &at
			}
			summaries[v.Content] = c
		}
		items := []Content{}
		for _, row := range rows {
			if !scope.Allows(row.Owner) || (f.Owner != "" && row.Owner != f.Owner) || (id != "" && row.ID != id && row.Kind != "legacy_catalog") {
				continue
			}
			var c Content
			if json.Unmarshal(row.Payload, &c) != nil {
				return ErrUnavailable
			}
			if catalogCreatedAt(c, row.At).After(f.AsOf) {
				continue
			}
			if id != "" && c.ID != id {
				continue
			}
			c.PageViews, c.VisibleMS, c.LastVisit = 0, 0, nil
			summary := summaries[c.ID]
			c.PageViews, c.VisibleMS, c.LastVisit = summary.PageViews, summary.VisibleMS, summary.LastVisit

			items = append(items, c)
		}
		sort.Slice(items, func(i, j int) bool {
			a, b := items[i], items[j]
			av, bv := contentSortValue(a, f.Sort), contentSortValue(b, f.Sort)
			if av == bv {
				return a.ID < b.ID
			}
			if f.Direction == "asc" {
				return av < bv
			}
			return av > bv
		})
		page, cursor, e := paginateContents(f, items)
		if e != nil {
			return e
		}
		out.Items = page
		out.Cursor = cursor
		return nil
	})
	return out, err
}
func (s *Service) Sessions(ctx context.Context, scope Scope, key string, f Filter) (List, error) {
	out := List{Items: []Session{}}
	if e := s.authorizeView(scope); e != nil {
		return out, e
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	err = s.queryTransaction(ctx, func(r Repository) error {
		all, e := s.factsSelected(r, scope, f, factSelection{Visitors: []string{key}})
		if e != nil {
			return e
		}
		selected := map[string]bool{}
		sessions := []string{}
		for _, v := range all {
			if v.Visitor == key && !selected[v.Session] {
				selected[v.Session] = true
				sessions = append(sessions, v.Session)
			}
		}
		if len(selected) == 0 {
			return fail(404, "访客记录不可访问")
		}
		if f.Related && f.Content != "" {
			base := f
			base.Content = ""
			all, e = s.factsSelected(r, scope, base, factSelection{Visitors: []string{key}, Sessions: sessions})
			if e != nil {
				return e
			}
		}
		by := map[string]*Session{}
		for _, v := range all {
			if v.Visitor != key || !selected[v.Session] || (!f.Related && f.Content != "" && v.Content != f.Content) {
				continue
			}
			ss := by[v.Session]
			if ss == nil {
				ss = &Session{ID: v.Session, Started: v.Started, Last: v.Started, Share: v.Share, Source: "direct", Views: []PageView{}}
				if v.Share != "" {
					ss.Source = "share"
				}
				by[v.Session] = ss
			}
			ss.Views = append(ss.Views, v.PageView)
			if v.Started.Before(ss.Started) {
				ss.Started = v.Started
			}
			if v.Last.After(ss.Last) {
				ss.Last = v.Last
			}
		}
		items := []Session{}
		for _, ss := range by {
			sort.Slice(ss.Views, func(i, j int) bool {
				if ss.Views[i].Started.Equal(ss.Views[j].Started) {
					return ss.Views[i].ID < ss.Views[j].ID
				}
				return ss.Views[i].Started.Before(ss.Views[j].Started)
			})
			items = append(items, *ss)
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].Started.Equal(items[j].Started) {
				return items[i].ID < items[j].ID
			}
			return items[i].Started.After(items[j].Started)
		})
		start, end, cursor, e := paginate(f, len(items))
		if e != nil {
			return e
		}
		out.Items = items[start:end]
		out.Cursor = cursor
		return nil
	})
	return out, err
}
func (s *Service) Events(ctx context.Context, scope Scope, id string, f Filter) (List, error) {
	out := List{Items: []Event{}}
	if e := s.authorizeView(scope); e != nil {
		return out, e
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	err = s.queryTransaction(ctx, func(r Repository) error {
		facts, e := s.factsSelected(r, scope, f, factSelection{View: id})
		if e != nil {
			return e
		}
		for _, v := range facts {
			if v.ID != id {
				continue
			}
			for _, flag := range v.Flags {
				if flag == "raw_events_expired" {
					return fail(410, "原始事件已按保留期清除，仅保留汇总")
				}
			}
			sortEvents(v.events)
			start, end, cursor, e := paginate(f, len(v.events))
			if e != nil {
				return e
			}
			out.Items = v.events[start:end]
			out.Cursor = cursor
			return nil
		}
		return fail(404, "页面访问不可访问")
	})
	return out, err
}

func visitorKeys(by map[string]*Visitor) []string {
	keys := make([]string, 0, len(by))
	for key := range by {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
