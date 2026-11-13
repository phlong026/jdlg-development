package tracking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 我的客户视图（按访客聚合的分享链路数据）。数据源与分享记录同表
// （tracking_records），差别只在透镜：从"分享日志"换成"谁最热、看了多久"。
// 严格限定在登录账号自己创建的分享集合内；同事与全租户数据永不出现在这里。
//
// 意向口径：链路内自包含计算——事实只来自本人分享的访问与业务动作，规则
// 阈值/人工修正语义与管理端一致（同一 activeRule + loadIntentOverrides），
// 但事实集合限定本人 session，因此同事链路的 session/view 证据不会混入。
// 链路推导标签不写入管理端意向审计（recordIntentEvaluations 是 owner 级
// 全量评估的回放凭证，链路推导标签写入会造成两个口径互相污染）。

const (
	// myVisitorShareCap bounds one aggregation to the newest 400 shares of the
	// actor. 超出部分本期不参与榜单，避免单请求扫描失去预算上限。
	myVisitorShareCap = 400
	// myVisitorSharePage is one actorShares page while collecting the share set.
	myVisitorSharePage = 50
)

// MyVisitEntry is one counted page view of one visitor: 时间、来源、时长与
// 视频进度。它是逐次明细的最小单位（仿真稿④的时间线）。
type MyVisitEntry struct {
	View       string    `json:"view_id"`
	Session    string    `json:"session_id"`
	Occurred   time.Time `json:"occurred_at"`
	Route      string    `json:"route_key"`
	Content    string    `json:"content_id,omitempty"`
	Version    int       `json:"content_version,omitempty"`
	Title      string    `json:"title,omitempty"`
	ShareID    string    `json:"share_id,omitempty"`
	Visible    int64     `json:"visible_ms"`
	VideoWatch int64     `json:"video_watch_ms"`
	Progress   int       `json:"progress_pct"`
}

// MyVisitor is one customer aggregated across every share of the actor.
// Visitor 裸嵌入：字段提升到顶层（visitor_key/nickname/page_views/visible_ms/
// session_count/last_visit/intent），小程序按顶层字段消费。
type MyVisitor struct {
	ProjectActivityCount int `json:"project_activity_count"`
	Visitor
	First        time.Time `json:"first_visit"`
	ContentCount int       `json:"content_count"`
	VideoWatch   int64     `json:"video_watch_ms"`
	Progress     int       `json:"progress_pct"`
}

// MyContentStat is one material aggregated across the actor's shares:
// 仿真稿的素材热度榜（按访客去重）。
type MyContentStat struct {
	ContentID string     `json:"content_id"`
	Version   int        `json:"content_version"`
	Route     string     `json:"route_key"`
	Title     string     `json:"title"`
	Type      string     `json:"type,omitempty"`
	Status    string     `json:"status,omitempty"`
	Cover     *Asset     `json:"cover,omitempty"`
	Visitors  int        `json:"visitor_count"`
	Views     int        `json:"page_views"`
	Forwards  int        `json:"forward_count"`
	Last      *time.Time `json:"last_visit,omitempty"`
}

// MyVisitorDetail is the drill-down payload of one visitor: 页头（身份+意向+累计）
// 与逐次明细一次返回，小程序访问详情页一次拉取即可渲染。
type MyVisitorDetail struct {
	ProjectActivities          []MyProjectCustomerActivity `json:"project_activities"`
	ProjectActivitiesTruncated bool                        `json:"project_activities_truncated,omitempty"`
	ShareWindow                *MyShareWindow              `json:"share_window,omitempty"`
	Visitor
	CanConvertLead bool           `json:"can_convert_lead"`
	Intent         Intent         `json:"intent"`
	First          time.Time      `json:"first_visit"`
	Last           time.Time      `json:"last_visit"`
	Views          int            `json:"page_views"`
	Visible        int64          `json:"visible_ms"`
	Sessions       int            `json:"session_count"`
	ShareCount     int            `json:"share_count"`
	ContentCount   int            `json:"content_count"`
	VideoWatch     int64          `json:"video_watch_ms"`
	Progress       int            `json:"progress_pct"`
	Items          []MyVisitEntry `json:"items"`
	Cursor         string         `json:"next_cursor,omitempty"`
	FirstCursor    string         `json:"first_cursor,omitempty"`
	Snapshot
}

type myVisitorAccumulator struct {
	first, last  time.Time
	views        int
	sessions     map[string]bool
	shares       map[string]bool
	contents     map[string]bool
	visible      int64
	watch        int64
	progress     int
	entries      map[string]*MyVisitEntry
	entryCovered map[string][]interval
}

func (a *myVisitorAccumulator) entry(view string) *MyVisitEntry {
	item := a.entries[view]
	if item == nil {
		item = &MyVisitEntry{View: view}
		a.entries[view] = item
	}
	return item
}

// myActorShares collects the actor's newest shares up to the aggregation cap.
type MyShareWindow struct {
	Limit      int    `json:"limit"`
	Truncated  bool   `json:"truncated"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type myShareWindowCursor struct {
	At        time.Time `json:"at"`
	ID        string    `json:"id"`
	Signature string    `json:"signature"`
}

func myShareWindowSignature(scope Scope, actor int64, f Filter) string {
	return detailSignature(Filter{AsOf: f.AsOf, Owner: f.Owner}, "my-share-window-v1", actorVisitorKey(actor), scope)
}

func (s *Service) myActorShares(r Repository, scope Scope, actor int64, f Filter) ([]Share, *MyShareWindow, error) {
	out := []Share{}
	window := &MyShareWindow{Limit: myVisitorShareCap}
	beforeAt, beforeID := time.Time{}, ""
	if f.ShareCursor != "" {
		var cursor myShareWindowCursor
		raw, err := base64.RawURLEncoding.DecodeString(f.ShareCursor)
		if len(f.ShareCursor) > 4096 || err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.At.IsZero() || cursor.ID == "" || cursor.Signature != myShareWindowSignature(scope, actor, f) {
			return nil, nil, fail(400, "分享统计窗口已变化，请返回最新分享")
		}
		beforeAt, beforeID = cursor.At, cursor.ID
	}
	var last actorShareRow
	for len(out) <= myVisitorShareCap {
		limit := myVisitorSharePage
		if remaining := myVisitorShareCap + 1 - len(out); remaining < limit {
			limit = remaining
		}
		rows, err := s.actorShares(r, scope, actor, f, "", beforeAt, beforeID, limit)
		if err != nil {
			return nil, nil, err
		}
		for _, row := range rows {
			if len(out) == myVisitorShareCap {
				window.Truncated = true
				window.NextCursor = base64.RawURLEncoding.EncodeToString(marshal(myShareWindowCursor{last.at, last.id, myShareWindowSignature(scope, actor, f)}))
				return out, window, nil
			}
			out = append(out, row.share)
			last = row
		}
		if len(rows) < limit {
			break
		}
		beforeAt, beforeID = last.at, last.id
	}
	return out, window, nil
}

type myShareScanEvent struct {
	row     Record
	ev      Event
	visitor string
}

// myShareEventScan gathers the accepted events of the actor's shares with the
// visitor already resolved (identity links + deletion tombstones applied).
// 删除标记走分块批量查询（复用 deletedMyShareVisitors 的模式，避免 N+1）。
func (s *Service) myShareEventScan(ctx context.Context, r Repository, scope Scope, actor int64, f Filter) ([]myShareScanEvent, []Share, *MyShareWindow, error) {
	actorKey := actorVisitorKey(actor)
	shares, window, err := s.myActorShares(r, scope, actor, f)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(shares) == 0 {
		return nil, shares, window, nil
	}
	shareIDs := make([]string, 0, len(shares))
	byID := make(map[string]Share, len(shares))
	for _, share := range shares {
		shareIDs = append(shareIDs, share.ID)
		byID[share.ID] = share
	}
	rows, err := scopedRecords(r, "event", RecordQuery{Owners: permittedOwners(scope, f.Owner), Shares: shareIDs, AtFrom: f.Start, AtBefore: f.End.Add(15 * time.Second), AtThrough: f.AsOf})
	if err != nil {
		return nil, nil, nil, err
	}
	sessions := make([]string, 0, len(rows))
	accepted := make([]myShareScanEvent, 0, len(rows))
	for _, row := range rows {
		var ev Event
		if json.Unmarshal(row.Payload, &ev) != nil {
			return nil, nil, nil, ErrUnavailable
		}
		if _, known := byID[row.Share]; !known {
			continue
		}
		if ev.Share != "" && ev.Share != row.Share {
			continue
		}
		if ev.Received.After(f.AsOf) || ev.Occurred.After(f.AsOf) || row.Visitor == actorKey || excludedMyShareEvent(ev) {
			continue
		}
		accepted = append(accepted, myShareScanEvent{row: row, ev: ev})
		sessions = append(sessions, row.Session)
	}
	meta, err := loadSessionMeta(r, sessions, true, true, false)
	if err != nil {
		return nil, nil, nil, err
	}
	resolve := func(row Record) string { return resolvedMyShareVisitor(meta, row, f.AsOf) }
	keys := make([]string, 0, len(accepted))
	for i := range accepted {
		accepted[i].visitor = resolve(accepted[i].row)
		keys = append(keys, accepted[i].visitor)
	}
	deleted := map[string]bool{}
	for len(keys) > 0 {
		n := sessionMetaChunk
		if n > len(keys) {
			n = len(keys)
		}
		tombstones, err := scopedRecords(r, "deletion", RecordQuery{IDs: keys[:n]})
		if err != nil {
			return nil, nil, nil, err
		}
		for _, row := range tombstones {
			deleted[row.ID] = true
		}
		keys = keys[n:]
	}
	kept := accepted[:0]
	for i := range accepted {
		if meta.deleted[accepted[i].row.Session] || accepted[i].visitor == actorKey || deleted[accepted[i].visitor] {
			continue
		}
		kept = append(kept, accepted[i])
	}
	return kept, shares, window, nil
}

// myEventContentKey 归组键：合集路由的所有事件（目录与作品）都记到合集名下，
// 合集追踪以合集为热度单位；其余按素材版本归组。
func myEventContentKey(ev Event, sharesByID map[string]Share) (string, bool) {
	if ev.Route == projectCollectionReaderRoute {
		share, ok := sharesByID[ev.Share]
		if ok && share.Content != "" {
			return projectCollectionReaderRoute + "\x00" + contentKey(share.Content, share.Version), true
		}
		return "", false
	}
	if ev.Content == "" {
		return "", false
	}
	return ev.Route + "\x00" + contentKey(ev.Content, ev.Version), true
}

func videoProgressPct(ev Event) int {
	if ev.Duration <= 0 || ev.Position <= 0 {
		return 0
	}
	pct := int(ev.Position * 100 / ev.Duration)
	if pct > 100 {
		pct = 100
	}
	return pct
}

func sharesByID(shares []Share) map[string]Share {
	out := make(map[string]Share, len(shares))
	for _, share := range shares {
		out[share.ID] = share
	}
	return out
}

// myVisitorAggregates folds the actor's share events into per-visitor and
// per-view accumulators. within() 门禁在任何状态写入之前：窗口外 page_view
// 不建条目、不计 session/素材/分享，heartbeats 只属于窗口内已建立的访问。
func (s *Service) myVisitorAggregates(ctx context.Context, r Repository, scope Scope, actor int64, f Filter) (myVisitorAggregates, []Share, error) {
	out := myVisitorAggregates{visitors: map[string]*myVisitorAccumulator{}}
	events, shares, window, err := s.myShareEventScan(ctx, r, scope, actor, f)
	if err != nil {
		return out, nil, err
	}
	out.window = window
	byID := sharesByID(shares)
	sort.Slice(events, func(i, j int) bool {
		if events[i].ev.Occurred.Equal(events[j].ev.Occurred) {
			return events[i].ev.ID < events[j].ev.ID
		}
		return events[i].ev.Occurred.Before(events[j].ev.Occurred)
	})
	viewVisitor := map[string]string{}
	for _, item := range events {
		visitor := item.visitor
		a := out.visitors[visitor]
		if a == nil {
			a = &myVisitorAccumulator{sessions: map[string]bool{}, shares: map[string]bool{}, contents: map[string]bool{}, entries: map[string]*MyVisitEntry{}, entryCovered: map[string][]interval{}}
			out.visitors[visitor] = a
		}
		ev := item.ev
		if ev.Type == "page_view" {
			if !within(ev.Occurred, f) {
				continue
			}
			viewVisitor[ev.View] = visitor
			a.shares[ev.Share] = true
			a.sessions[item.row.Session] = true
			if key, ok := myEventContentKey(ev, byID); ok {
				a.contents[key] = true
			}
			entry := a.entry(ev.View)
			entry.Session = item.row.Session
			entry.Route = ev.Route
			entry.Content = ev.Content
			entry.Version = ev.Version
			entry.ShareID = ev.Share
			if entry.Occurred.IsZero() || ev.Occurred.Before(entry.Occurred) {
				entry.Occurred = ev.Occurred
			}
			a.views++
			if a.first.IsZero() || ev.Occurred.Before(a.first) {
				a.first = ev.Occurred
			}
			if ev.Occurred.After(a.last) {
				a.last = ev.Occurred
			}
			continue
		}
		visitor = viewVisitor[ev.View]
		if visitor == "" {
			continue
		}
		target := out.visitors[visitor]
		if target == nil {
			continue
		}
		entry := target.entry(ev.View)
		switch ev.Type {
		case "visible_segment":
			if ev.Delta <= 0 {
				continue
			}
			lo := ev.Occurred.Add(-time.Duration(ev.Delta) * time.Millisecond)
			hi := ev.Occurred
			if lo.Before(f.Start) {
				lo = f.Start
			}
			if hi.After(f.End) {
				hi = f.End
			}
			var added int64
			target.entryCovered[ev.View], added = addInterval(target.entryCovered[ev.View], interval{lo.UnixMilli(), hi.UnixMilli()})
			entry.Visible += added
			target.visible += added
			if added > 0 && hi.After(target.last) {
				target.last = hi
			}
		case "video_progress":
			if ev.Watch <= 0 || !within(ev.Occurred, f) {
				continue
			}
			target.watch += ev.Watch
			entry.VideoWatch += ev.Watch
			pct := videoProgressPct(ev)
			if pct > target.progress {
				target.progress = pct
			}
			if pct > entry.Progress {
				entry.Progress = pct
			}
		}
	}
	return out, shares, nil
}

// myVisitorAggregates 是一次窗口聚合的结果：按访客的累计器 + 本次涉及的分享。
type myVisitorAggregates struct {
	window   *MyShareWindow
	visitors map[string]*myVisitorAccumulator
	shares   []Share
}

// myIntentFacts 是链路内意向计算的输入。规则按 f.Owner 收敛后的归属解析；
// 业务动作事实按规则窗口收集且 session 必须属于本人链路聚合结果。
type myIntentFacts struct {
	rule       RuleConfig
	mixedRules bool
	business   map[string][]Record
	overrides  map[string]Record
}

func (s *Service) myIntentFacts(ctx context.Context, r Repository, scope Scope, f Filter, ruleAgg map[string]*myVisitorAccumulator, now time.Time) (myIntentFacts, error) {
	out := myIntentFacts{business: map[string][]Record{}, overrides: map[string]Record{}}
	effective := ownerScope(scope, f.Owner)
	rule, mixed, err := activeRule(r, effective, now)
	if err != nil {
		return out, err
	}
	out.rule, out.mixedRules = rule, mixed
	if mixed || !s.Config.IntentEnabled {
		// 多归属规则不一致：与管理端一致返回 ScopeRequired 语义，不取事实。
		return out, nil
	}
	keys := make([]string, 0, len(ruleAgg))
	for key := range ruleAgg {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows, err := scopedRecords(r, "business", RecordQuery{Owners: permittedOwners(effective, f.Owner), VisitorKeys: keys, AtFrom: now.AddDate(0, 0, -rule.WindowDays), AtThrough: now, IdentityAsOf: now})
	if err != nil {
		return out, err
	}
	verifiedSources := map[string]bool{}
	verifyContext, cancelVerify := context.WithTimeout(sourceReadContext(ctx, r), 2*time.Second)
	defer cancelVerify()
	for _, b := range rows {
		if !effective.Allows(b.Owner) || b.At.Before(now.AddDate(0, 0, -rule.WindowDays)) || b.At.After(now) {
			continue
		}
		visitor := resolvedVisitorAt(r, b, now)
		a := ruleAgg[visitor]
		if a == nil || !a.sessions[b.Session] {
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
					return out, ErrUnavailable
				}
				verifiedSources[cacheKey] = valid
			}
			if !valid {
				continue
			}
		}
		out.business[visitor] = append(out.business[visitor], b)
	}
	if len(effective.Owners) == 1 {
		var limit *int64
		out.overrides, _, err = loadIntentOverrides(r, effective, now, keys, limit)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// myIntent 与管理端 intent() 同一阈值/人工修正/mixed 语义，事实为本人链路。
// 与管理端的两处差异是设计决定：① 事实集合限定本人分享链路（不泄漏同事
// session/view 证据）；② 链路推导标签不写 recordIntentEvaluations——那批
// 审计记录是管理端 owner 级全量评估的回放凭证，两个口径混写会互相污染。
func (s *Service) myIntent(scope Scope, key string, a *myVisitorAccumulator, facts myIntentFacts, now time.Time) Intent {
	rule := facts.rule
	in := Intent{Level: "unknown", Reasons: []string{"证据不足，待判断"}, Rule: rule.Version, Start: now.AddDate(0, 0, -rule.WindowDays), End: now, Calculated: now, Thresholds: rule.RuleParameters, Exclusions: rule.Exclusions, ParticipatingMetrics: rule.Metrics, Timezone: rule.Timezone, OverrideState: "none", Evidence: []IntentEvidence{}}
	if facts.mixedRules {
		in.ScopeRequired = true
		in.Rule = "mixed"
		in.Reasons = []string{"所选归属使用不同规则，请选择单一归属查看意向参考"}
		return in
	}
	if !s.Config.IntentEnabled {
		return in
	}
	starts := map[string]time.Time{}
	for _, entry := range a.entries {
		if s0, ok := starts[entry.Session]; !ok || entry.Occurred.Before(s0) {
			starts[entry.Session] = entry.Occurred
		}
	}
	evidence := []IntentEvidence{}
	var customerMS, audienceMS int64
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	sessionDays := map[string]int{}
	type cappedSession struct {
		session string
		at      time.Time
	}
	capped := []cappedSession{}
	for session, start := range starts {
		at := start
		if at.Before(in.Start) {
			at = in.Start
		}
		day := at.In(zone).Format("2006-01-02")
		if sessionDays[day] >= rule.DailySessionCap {
			continue
		}
		sessionDays[day]++
		in.Metrics.Sessions++
		capped = append(capped, cappedSession{session, at})
	}
	sort.Slice(capped, func(i, j int) bool {
		if capped[i].at.Equal(capped[j].at) {
			return capped[i].session < capped[j].session
		}
		return capped[i].at.Before(capped[j].at)
	})
	for _, item := range capped {
		evidence = append(evidence, IntentEvidence{Kind: "page_view", Session: item.session, At: item.at})
	}
	for _, entry := range a.entries {
		if audience := s.routeAudience(entry.Route); audience != RouteAudienceNeutral {
			audienceMS += entry.Visible
			if audience == RouteAudienceCustomer {
				customerMS += entry.Visible
			}
		}
	}
	business := append([]Record{}, facts.business[key]...)
	sort.Slice(business, func(i, j int) bool {
		if business[i].At.Equal(business[j].At) {
			return business[i].ID < business[j].ID
		}
		return business[i].At.Before(business[j].At)
	})
	actionDays := map[string]int{}
	seenBusiness := map[string]bool{}
	for _, b := range business {
		if !scope.Allows(b.Owner) || b.At.Before(in.Start) || b.At.After(now) || seenBusiness[b.ID] || !a.sessions[b.Session] {
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
	if audienceMS > 0 {
		in.CustomerShare = float64(customerMS) / float64(audienceMS)
	}
	if len(evidence) > 100 {
		in.Evidence = append([]IntentEvidence{}, evidence[:100]...)
		in.EvidenceTruncated = true
	}
	if len(a.sessions) > 0 && !strings.HasPrefix(key, "a:") {
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
	if len(scope.Owners) == 1 {
		rec, ok := facts.overrides[privateKey(scope.Owners[0], key)]
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

// MyVisitors lists the actor's share-reachable visitors aggregated per person.
// 意向按规则窗口独立聚合（ruleFilter），榜单统计按展示窗口（f）——两窗口解耦。
func (s *Service) MyVisitors(ctx context.Context, scope Scope, actor int64, f Filter) (List, error) {
	out := List{Items: []MyVisitor{}}
	if err := s.authorizeMyShares(scope, actor); err != nil {
		return out, err
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	effective := ownerScope(scope, f.Owner)
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		rule, _, ruleErr := activeRule(r, effective, f.AsOf)
		if ruleErr != nil {
			return ruleErr
		}
		ruleFilter := Filter{Start: f.AsOf.AddDate(0, 0, -rule.WindowDays), End: f.AsOf.Add(time.Nanosecond), AsOf: f.AsOf, Owner: f.Owner, ShareCursor: f.ShareCursor}
		dispAgg, _, err := s.myVisitorAggregates(ctx, r, scope, actor, f)
		if err != nil {
			return err
		}
		projectActivities, projectTruncated, err := s.myProjectCustomerActivities(ctx, r, scope, actor, f)
		if err != nil {
			return err
		}
		includeProjectCustomers(dispAgg.visitors, projectActivities)
		out.ShareWindow = dispAgg.window
		ruleAgg, _, err := s.myVisitorAggregates(ctx, r, scope, actor, ruleFilter)
		if err != nil {
			return err
		}
		out.ProjectActivitiesTruncated = projectTruncated
		keys := make([]string, 0, len(dispAgg.visitors))
		facts := make([]factView, 0, len(dispAgg.visitors))
		for key := range dispAgg.visitors {
			keys = append(keys, key)
			facts = append(facts, factView{PageView: PageView{ID: "my-visitor-" + key}, Visitor: key})
		}
		sort.Strings(keys)
		profiles, _, err := s.queryProfiles(ctx, r, facts)
		if err != nil {
			return err
		}
		factsData, err := s.myIntentFacts(ctx, r, scope, ruleFilter, ruleAgg.visitors, f.AsOf)
		if err != nil {
			return err
		}
		items := make([]MyVisitor, 0, len(dispAgg.visitors))
		for key, a := range dispAgg.visitors {
			if f.Visitor != "" && key != f.Visitor {
				continue
			}
			visitor := profiles[key]
			visitor.Last = a.last
			visitor.Views = a.views
			visitor.Visible = a.visible
			visitor.Sessions = len(a.sessions)
			visitor.Shares = len(a.shares)
			// 访客在规则窗口内无本人链路事实时用空聚合：意向=unknown（证据不足）。
			ruleAcc := ruleAgg.visitors[key]
			if ruleAcc == nil {
				ruleAcc = &myVisitorAccumulator{sessions: map[string]bool{}, shares: map[string]bool{}, contents: map[string]bool{}, entries: map[string]*MyVisitEntry{}, entryCovered: map[string][]interval{}}
			}
			visitor.Intent = s.myIntent(effective, key, ruleAcc, factsData, f.AsOf)
			applyBSideDowngrade(profiles, key, &visitor.Intent)
			if !visitorListed(visitor, f) {
				continue
			}
			if visitor.State == "identified" {
				out.Identified++
			} else {
				out.Anonymous++
			}
			items = append(items, MyVisitor{ProjectActivityCount: len(projectActivities[key]), Visitor: visitor, First: a.first, ContentCount: len(a.contents), VideoWatch: a.watch, Progress: a.progress})
		}
		sort.Slice(items, func(i, j int) bool {
			a, b := items[i].Visitor, items[j].Visitor
			cmp := 0
			switch f.Sort {
			case "page_views":
				cmp = a.Views - b.Views
			case "shares":
				cmp = a.Shares - b.Shares
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
		start, end, cursor, first, err := paginateDetail(f, len(items), "my-visitors-v1", actorVisitorKey(actor), scope)
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

// MyVisitorEntries returns the per-view timeline of one visitor restricted to
// the actor's own shares. 访问详情页的逐次记录数据源。
func (s *Service) MyVisitorEntries(ctx context.Context, scope Scope, actor int64, f Filter) (MyVisitorDetail, error) {
	out := MyVisitorDetail{Items: []MyVisitEntry{}}
	if err := s.authorizeMyShares(scope, actor); err != nil {
		return out, err
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	if f.Visitor == "" {
		return out, fail(400, "请指定访客")
	}
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	effective := ownerScope(scope, f.Owner)
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		rule, _, ruleErr := activeRule(r, effective, f.AsOf)
		if ruleErr != nil {
			return ruleErr
		}
		ruleFilter := Filter{Start: f.AsOf.AddDate(0, 0, -rule.WindowDays), End: f.AsOf.Add(time.Nanosecond), AsOf: f.AsOf, Owner: f.Owner, ShareCursor: f.ShareCursor}
		dispAgg, shares, err := s.myVisitorAggregates(ctx, r, scope, actor, f)
		if err != nil {
			return err
		}
		projectActivities, projectTruncated, err := s.myProjectCustomerActivities(ctx, r, scope, actor, f)
		if err != nil {
			return err
		}
		includeProjectCustomers(dispAgg.visitors, projectActivities)
		out.ShareWindow = dispAgg.window
		ruleAgg, _, err := s.myVisitorAggregates(ctx, r, scope, actor, ruleFilter)
		if err != nil {
			return err
		}
		out.ProjectActivities = projectActivities[f.Visitor]
		out.ProjectActivitiesTruncated = projectTruncated
		a, ok := dispAgg.visitors[f.Visitor]
		if !ok {
			return fail(404, "访客记录不可访问")
		}
		entries := make([]MyVisitEntry, 0, len(a.entries))
		for _, entry := range a.entries {
			entries = append(entries, *entry)
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Occurred.Equal(entries[j].Occurred) {
				return entries[i].View < entries[j].View
			}
			return entries[i].Occurred.After(entries[j].Occurred)
		})
		rows := make([]actorShareRow, 0, len(shares))
		for _, share := range shares {
			rows = append(rows, actorShareRow{share: share})
		}
		materials, err := s.myShareMaterials(ctx, r, effective, rows)
		if err != nil {
			return err
		}
		shareIndex := sharesByID(shares)
		for i := range entries {
			entry := &entries[i]
			key, ok := myEventContentKey(Event{Route: entry.Route, Content: entry.Content, Version: entry.Version, Share: entry.ShareID}, shareIndex)
			if ok {
				if material, exists := materials[key]; exists && strings.TrimSpace(material.Title) != "" {
					entry.Title = material.Title
				}
			}
			if strings.TrimSpace(entry.Title) == "" || entry.Title == entry.Route || strings.HasPrefix(entry.Title, "pages/") {
				entry.Title = s.myRouteTitle(entry.Route)
			}
		}
		profiles, _, err := s.queryProfiles(ctx, r, []factView{{PageView: PageView{ID: "my-visitor-" + f.Visitor}, Visitor: f.Visitor}})
		if err != nil {
			return err
		}
		factsData, err := s.myIntentFacts(ctx, r, scope, ruleFilter, ruleAgg.visitors, f.AsOf)
		if err != nil {
			return err
		}
		visitor := profiles[f.Visitor]
		ruleAcc := ruleAgg.visitors[f.Visitor]
		if ruleAcc == nil {
			ruleAcc = &myVisitorAccumulator{sessions: map[string]bool{}, shares: map[string]bool{}, contents: map[string]bool{}, entries: map[string]*MyVisitEntry{}, entryCovered: map[string][]interval{}}
		}
		visitor.Last = a.last
		visitor.Views = a.views
		visitor.Visible = a.visible
		visitor.Sessions = len(a.sessions)
		visitor.Shares = len(a.shares)
		visitor.Intent = s.myIntent(effective, f.Visitor, ruleAcc, factsData, f.AsOf)
		applyBSideDowngrade(profiles, f.Visitor, &visitor.Intent)
		out.Visitor, out.Intent = visitor, visitor.Intent
		out.First, out.Last, out.Views = a.first, a.last, a.views
		out.Visible, out.Sessions, out.ShareCount = a.visible, len(a.sessions), len(a.shares)
		out.ContentCount, out.VideoWatch, out.Progress = len(a.contents), a.watch, a.progress
		start, end, cursor, first, err := paginateDetail(f, len(entries), "my-visit-entries-v1", f.Visitor, scope)
		if err != nil {
			return err
		}
		out.Items, out.Cursor, out.FirstCursor = entries[start:end], cursor, first
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

// applyBSideDowngrade mirrors the management visitor list: B 端账号只作渠道观察。
func applyBSideDowngrade(profiles map[string]Visitor, key string, intent *Intent) {
	if profile, ok := profiles[key]; ok && profile.State == "identified" && BSideIdentities[profile.IdentityType] {
		intent.Level = "low"
		intent.Exclusions = append(intent.Exclusions, "bside_identity")
		intent.Reasons = append([]string{"B端账号（" + profile.IdentityType + "）仅作渠道观察，不参与客户意向"}, intent.Reasons...)
	}
}

// MyContentStats aggregates the actor's share traffic per material:
// 素材热度榜（查看人数按访客去重；被转发=窗口内客户的分享意图事件）。
func (s *Service) MyContentStats(ctx context.Context, scope Scope, actor int64, f Filter) (List, error) {
	out := List{Items: []MyContentStat{}}
	if err := s.authorizeMyShares(scope, actor); err != nil {
		return out, err
	}
	contentSort := f.Sort
	if contentSort == "visitor_count" {
		// visitor_count is specific to this projection; keep global Filter
		// normalization unchanged for every other tracking endpoint.
		f.Sort = ""
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		events, shares, window, err := s.myShareEventScan(ctx, r, scope, actor, f)
		if err != nil {
			return err
		}
		out.ShareWindow = window
		if len(events) == 0 {
			return nil
		}
		byID := sharesByID(shares)
		type contentAcc struct {
			visitors map[string]bool
			views    int
			forwards int
			last     *time.Time
		}
		acc := map[string]*contentAcc{}
		for _, item := range events {
			key, ok := myEventContentKey(item.ev, byID)
			if !ok {
				continue
			}
			// 先校验事件类型与窗口，再创建/更新统计（窗口外转发不计入）。
			isForward := item.ev.Type == "share_intent"
			isView := item.ev.Type == "page_view"
			if !isForward && !isView {
				continue
			}
			if !within(item.ev.Occurred, f) {
				continue
			}
			stat := acc[key]
			if stat == nil {
				stat = &contentAcc{visitors: map[string]bool{}}
				acc[key] = stat
			}
			at := item.ev.Occurred
			if stat.last == nil || at.After(*stat.last) {
				stat.last = &at
			}
			if isForward {
				stat.forwards++
				continue
			}
			stat.visitors[item.visitor] = true
			stat.views++
		}
		if len(acc) == 0 {
			return nil
		}
		rows := make([]actorShareRow, 0, len(shares))
		for _, share := range shares {
			rows = append(rows, actorShareRow{share: share})
		}
		materials, err := s.myShareMaterials(ctx, r, ownerScope(scope, f.Owner), rows)
		if err != nil {
			return err
		}
		items := make([]MyContentStat, 0, len(acc))
		for key, stat := range acc {
			item := MyContentStat{Visitors: len(stat.visitors), Views: stat.views, Forwards: stat.forwards, Last: stat.last}
			item.Route = key
			if idx := strings.Index(key, "\x00"); idx >= 0 {
				item.Route = key[:idx]
				ck := key[idx+1:]
				if cidx := strings.Index(ck, ":"); cidx >= 0 {
					item.ContentID = ck[:cidx]
					if v, perr := strconv.Atoi(ck[cidx+1:]); perr == nil {
						item.Version = v
					}
				} else {
					item.ContentID = ck
				}
			}
			if material, ok := materials[key]; ok {
				item.Version, item.Title, item.Type, item.Status, item.Cover = material.Version, material.Title, material.Type, material.Status, material.Cover
			}
			if strings.TrimSpace(item.Title) == "" || item.Title == item.Route || strings.HasPrefix(item.Title, "pages/") {
				item.Title = s.myRouteTitle(item.Route)
			}
			if f.Search != "" && !strings.Contains(strings.ToLower(item.Title), strings.ToLower(f.Search)) {
				continue
			}
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool {
			if contentSort == "visitor_count" && items[i].Visitors != items[j].Visitors {
				if f.Direction == "asc" {
					return items[i].Visitors < items[j].Visitors
				}
				return items[i].Visitors > items[j].Visitors
			}
			if items[i].Views != items[j].Views {
				return items[i].Views > items[j].Views
			}
			if items[i].Visitors != items[j].Visitors {
				return items[i].Visitors > items[j].Visitors
			}
			return items[i].Route+items[i].ContentID < items[j].Route+items[j].ContentID
		})
		start, end, cursor, first, err := paginateDetail(f, len(items), "my-content-stats-v1", actorVisitorKey(actor), scope)
		if err != nil {
			return err
		}
		out.Items, out.Cursor, out.FirstCursor = items[start:end], cursor, first
		return nil
	})
	return out, err
}

// myRouteTitle is used only when the immutable material snapshot has no usable
// title. Route keys are implementation details and must not become UI copy.
func (s *Service) myRouteTitle(route string) string {
	if configured, ok := s.Config.Routes[route]; ok {
		title := strings.TrimSpace(configured.Title)
		if title != "" && title != route && !strings.HasPrefix(title, "pages/") {
			return title
		}
		switch configured.Type {
		case "home":
			return "首页"
		case "list", "catalog":
			return "内容列表"
		case "form":
			return "服务工具"
		case "collection":
			return "项目合集"
		case "owned-content":
			return "内容作品"
		case "landing":
			return "方案介绍"
		case "detail":
			return "内容详情"
		}
	}
	return "内容页面"
}
