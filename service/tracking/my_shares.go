package tracking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MyShare is one share created by the authenticated mini-program account.
// Traffic is restricted to this exact share; owner membership alone is never
// enough to expose a colleague's distribution results.
type MyShare struct {
	ID             string     `json:"share_id"`
	At             time.Time  `json:"created_at"`
	Route          string     `json:"route_key,omitempty"`
	Content        string     `json:"content_id,omitempty"`
	Version        int        `json:"content_version,omitempty"`
	Title          string     `json:"title"`
	Type           string     `json:"type,omitempty"`
	Status         string     `json:"status,omitempty"`
	Channel        string     `json:"channel,omitempty"`
	Expires        time.Time  `json:"expires_at"`
	Revoked        bool       `json:"revoked"`
	Visitors       int        `json:"visitor_count"`
	Views          int        `json:"page_views"`
	DirectoryViews int        `json:"directory_views,omitempty"`
	ContentViews   int        `json:"content_views,omitempty"`
	LastVisit      *time.Time `json:"last_visit,omitempty"`
	// AvgViewsPerVisitor 是"人均浏览"口径的参与度参考（B17）：总浏览/访客数，
	// 保留一位小数。它是比值不是事实声明——访客数受匿名与登录流失影响。
	AvgViewsPerVisitor float64 `json:"avg_views_per_visitor,omitempty"`
	// HighIntentVisitors 预留：高意向访客数与访客榜同一引擎，但列表端点的
	// 事件查询必须收敛在分享批次内（见 TestMySharesUseServerSharerAndBatchTrafficQuery），
	// 意向计算需要跨分享扫描，改由客户Tab(my-visitors)下发的聚合承担。
	HighIntentVisitors int `json:"high_intent_visitors,omitempty"`
}

// MyShareVisit is the cumulative, snapshot-pinned traffic from one visitor to
// one authorized share. It deliberately carries no company-wide history.
type MyShareVisit struct {
	Visitor        Visitor   `json:"visitor"`
	First          time.Time `json:"first_visit"`
	Last           time.Time `json:"last_visit"`
	Views          int       `json:"page_views"`
	DirectoryViews int       `json:"directory_views,omitempty"`
	ContentViews   int       `json:"content_views,omitempty"`
	Visible        int64     `json:"visible_ms"`
	Sessions       int       `json:"session_count"`
	VideoWatch     int64     `json:"video_watch_ms,omitempty"`
	Progress       int       `json:"progress_pct,omitempty"`
}

func actorVisitorKey(actor int64) string {
	if actor <= 0 {
		return ""
	}
	return "u:" + strconv.FormatInt(actor, 10)
}

func shareVisibleToActor(scope Scope, actor int64, share Share) bool {
	key := actorVisitorKey(actor)
	return key != "" && scope.Actor == actor && scope.View && scope.Allows(share.Owner) && share.Sharer == key
}

func (s *Service) authorizeMyShares(scope Scope, actor int64) error {
	if err := s.authorized(scope); err != nil {
		return err
	}
	if !scope.View || actor <= 0 || scope.Actor != actor {
		return fail(403, "无本人分享记录查看权限")
	}
	return nil
}

func decodeActorShare(row Record, scope Scope, actor int64) (Share, bool, error) {
	var share Share
	if json.Unmarshal(row.Payload, &share) != nil {
		return share, false, ErrUnavailable
	}
	if share.ID == "" || row.Share != "" && share.ID != row.Share || share.Owner != row.Owner || row.Visitor != "" && share.Sharer != row.Visitor {
		return share, false, ErrUnavailable
	}
	return share, shareVisibleToActor(scope, actor, share), nil
}

type actorShareRow struct {
	share Share
	at    time.Time
	id    string
}

func (s *Service) actorShares(r Repository, scope Scope, actor int64, f Filter, shareID string, beforeAt time.Time, beforeID string, limit int) ([]actorShareRow, error) {
	query := RecordQuery{
		Owners:        permittedOwners(scope, f.Owner),
		VisitorKeys:   []string{actorVisitorKey(actor)},
		ExactVisitors: true,
		AtThrough:     f.AsOf,
		Descending:    true,
		BeforeAt:      beforeAt,
		BeforeID:      beforeID,
		Limit:         limit,
	}
	// CreatedWindow 把时间窗应用到分享创建时间（近3天/7天/30天 chips）。
	// 默认关闭：窗口语义历史上属于访问而非分享，只在显式请求时收窄。
	if f.CreatedWindow {
		query.AtFrom = f.Start
		query.AtBefore = f.End
	}
	if shareID != "" {
		query.Shares = []string{shareID}
	}
	rows, err := pageRecords(r, "share", query)
	if err != nil {
		return nil, err
	}
	out := make([]actorShareRow, 0, len(rows))
	for _, row := range rows {
		share, visible, err := decodeActorShare(row, scope, actor)
		if err != nil {
			return nil, err
		}
		if visible && (shareID == "" || share.ID == shareID) {
			out = append(out, actorShareRow{share: share, at: row.At, id: row.ID})
		}
	}
	return out, nil
}

type mySharesCursor struct {
	Kind      string    `json:"kind"`
	BeforeAt  time.Time `json:"before_at"`
	BeforeID  string    `json:"before_id"`
	Signature string    `json:"signature"`
}

func mySharesCursorSignature(f Filter, scope Scope, actor int64) string {
	return detailSignature(f, "my-shares-keyset-v2", actorVisitorKey(actor), scope)
}

func readMySharesCursor(f Filter, scope Scope, actor int64) (time.Time, string, error) {
	if f.Cursor == "" {
		return time.Time{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
	var cursor mySharesCursor
	if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Kind != "my-shares-keyset-v2" || cursor.BeforeAt.IsZero() || cursor.BeforeID == "" || cursor.Signature != mySharesCursorSignature(f, scope, actor) {
		return time.Time{}, "", fail(400, "游标与本人分享范围不一致，请刷新")
	}
	return cursor.BeforeAt, cursor.BeforeID, nil
}

func nextMySharesCursor(f Filter, scope Scope, actor int64, row actorShareRow) string {
	return base64.RawURLEncoding.EncodeToString(marshal(mySharesCursor{Kind: "my-shares-keyset-v2", BeforeAt: row.at, BeforeID: row.id, Signature: mySharesCursorSignature(f, scope, actor)}))
}

// MyShares lists shares for the authenticated actor. The sharer predicate is
// passed to RecordQuery, so SQL narrows by the server-built u:<actor> key before
// any payload is decoded.
func (s *Service) MyShares(ctx context.Context, scope Scope, actor int64, f Filter) (List, error) {
	out := List{Items: []MyShare{}}
	if err := s.authorizeMyShares(scope, actor); err != nil {
		return out, err
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	out.Snapshot = Snapshot{AsOf: f.AsOf, Metric: MetricVersion}
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		beforeAt, beforeID, err := readMySharesCursor(f, scope, actor)
		if err != nil {
			return err
		}
		if f.Search == "" && f.Content == "" {
			// 无搜索：保持既有单次取页的键集语义（游标=页内最后一条收录项）。
			shares, err := s.actorShares(r, scope, actor, f, "", beforeAt, beforeID, f.Limit+1)
			if err != nil {
				return err
			}
			hasMore := len(shares) > f.Limit
			if hasMore {
				shares = shares[:f.Limit]
			}
			ids := make([]string, 0, len(shares))
			for _, item := range shares {
				ids = append(ids, item.share.ID)
			}
			traffic, err := myShareTraffic(r, permittedOwners(scope, f.Owner), ids, f, actorVisitorKey(actor))
			if err != nil {
				return err
			}
			effective := ownerScope(scope, f.Owner)
			materials, err := s.myShareMaterials(ctx, r, effective, shares)
			if err != nil {
				return err
			}
			items := make([]MyShare, 0, len(shares))
			for _, row := range shares {
				share := row.share
				item := MyShare{ID: share.ID, At: row.at, Route: share.Route, Content: share.Content, Version: share.Version, Channel: share.Channel, Expires: share.Expires, Revoked: share.Revoked}
				stats := traffic[share.ID]
				item.Visitors, item.Views, item.DirectoryViews, item.ContentViews, item.LastVisit = len(stats.visitors), stats.views, stats.directoryViews, stats.contentViews, stats.last
				if item.Visitors > 0 && item.Views > 0 {
					item.AvgViewsPerVisitor = math.Round(float64(item.Views)/float64(item.Visitors)*10) / 10
				}
				if share.Content != "" && share.Version > 0 {
					key := share.Route + "\x00" + contentKey(share.Content, share.Version)
					material := materials[key]
					item.Title, item.Type, item.Status = material.Title, material.Type, material.Status
				} else {
					item.Title = s.Config.Routes[share.Route].Title
				}
				items = append(items, item)
			}
			out.Items = items
			if hasMore && len(shares) > 0 {
				out.Cursor = nextMySharesCursor(f, scope, actor, shares[len(shares)-1])
			}
			return nil
		}
		// 标题/素材过滤先于分页，扫描前进直到凑满 limit+1 或扫完；
		// 游标推进到最后一条已扫描位置（页面可能稀疏，但不跳过任何分享）。
		collected := make([]MyShare, 0, f.Limit+1)
		var returned actorShareRow
		hasMore := false
		exhausted := false
		for !exhausted && !hasMore && len(collected) <= f.Limit {
			shares, err := s.actorShares(r, scope, actor, f, "", beforeAt, beforeID, myVisitorSharePage)
			if err != nil {
				return err
			}
			if len(shares) == 0 {
				exhausted = true
				break
			}
			candidates := make([]actorShareRow, 0, len(shares))
			for _, item := range shares {
				share := item.share
				if f.Content != "" && (share.Content != f.Content || f.ContentVersion > 0 && share.Version != f.ContentVersion) {
					continue
				}
				candidates = append(candidates, item)
			}
			ids := make([]string, 0, len(candidates))
			for _, item := range candidates {
				ids = append(ids, item.share.ID)
			}
			traffic, err := myShareTraffic(r, permittedOwners(scope, f.Owner), ids, f, actorVisitorKey(actor))
			if err != nil {
				return err
			}
			effective := ownerScope(scope, f.Owner)
			materials, err := s.myShareMaterials(ctx, r, effective, candidates)
			if err != nil {
				return err
			}
			for _, row := range candidates {
				share := row.share
				item := MyShare{ID: share.ID, At: row.at, Route: share.Route, Content: share.Content, Version: share.Version, Channel: share.Channel, Expires: share.Expires, Revoked: share.Revoked}
				stats := traffic[share.ID]
				item.Visitors, item.Views, item.DirectoryViews, item.ContentViews, item.LastVisit = len(stats.visitors), stats.views, stats.directoryViews, stats.contentViews, stats.last
				if item.Visitors > 0 && item.Views > 0 {
					item.AvgViewsPerVisitor = math.Round(float64(item.Views)/float64(item.Visitors)*10) / 10
				}
				if share.Content != "" && share.Version > 0 {
					key := share.Route + "\x00" + contentKey(share.Content, share.Version)
					material := materials[key]
					item.Title, item.Type, item.Status = material.Title, material.Type, material.Status
				} else {
					item.Title = s.Config.Routes[share.Route].Title
				}
				if f.Search != "" && !strings.Contains(strings.ToLower(item.Title), strings.ToLower(f.Search)) {
					continue
				}
				// 已凑满 limit：本条命中证明还有下一页——游标绑定最后一条
				// 实际返回项，本条留给下一页（不越过未返回的命中）。
				if len(collected) >= f.Limit {
					hasMore = true
					break
				}
				collected = append(collected, item)
				returned = row
			}
			if len(shares) < myVisitorSharePage {
				exhausted = true
				break
			}
			last := shares[len(shares)-1]
			beforeAt, beforeID = last.at, last.id
		}
		if hasMore {
			collected = collected[:f.Limit]
		}
		out.Items = collected
		if hasMore && returned.id != "" {
			out.Cursor = nextMySharesCursor(f, scope, actor, returned)
		}
		return nil
	})
	return out, err
}

func (s *Service) myShareMaterials(ctx context.Context, r Repository, scope Scope, shares []actorShareRow) (map[string]VisitorContent, error) {
	out := map[string]VisitorContent{}
	contentIDs, versionIDs := []string{}, []string{}
	legacyByRoute := map[string][]string{}
	for _, row := range shares {
		share := row.share
		if share.Content == "" || share.Version <= 0 {
			continue
		}
		key := share.Route + "\x00" + contentKey(share.Content, share.Version)
		if share.Route == projectCollectionReaderRoute {
			material, err := s.projectCollectionShareMaterial(r, share)
			if err != nil {
				return nil, err
			}
			out[key] = material
			continue
		}
		out[key] = VisitorContent{ID: share.Content, Version: share.Version, Route: share.Route, Title: "素材暂不可访问", Status: "unavailable"}
		if LegacyRoute(share.Route) {
			legacyByRoute[share.Route] = appendUnique(legacyByRoute[share.Route], share.Content)
			versionIDs = appendUnique(versionIDs, contentKey(share.Content, share.Version))
			continue
		}
		contentIDs = appendUnique(contentIDs, share.Content)
		versionIDs = appendUnique(versionIDs, contentKey(share.Content, share.Version))
	}
	legacyAvailable := map[string]bool{}
	for route, contents := range legacyByRoute {
		if route != CostSnapshotRoute && s.ResolveLegacyBatchContext != nil {
			resolved, err := s.ResolveLegacyBatchContext(sourceReadContext(ctx, r), route, contents)
			if err != nil {
				return nil, err
			}
			for _, content := range contents {
				metadata, ok := resolved[content]
				legacyAvailable[route+"\x00"+content] = ok && strings.TrimSpace(metadata.Title) != "" && metadata.VersionSeed != ""
			}
			continue
		}
		for _, content := range contents {
			if _, err := s.resolveLegacy(sourceReadContext(ctx, r), route, content); err != nil {
				var unavailable *Error
				if errors.As(err, &unavailable) && (unavailable.Status == 404 || unavailable.Status == 410) {
					continue
				}
				return nil, err
			}
			legacyAvailable[route+"\x00"+content] = true
		}
	}
	masters := []Record{}
	revokedRows := []Record{}
	if len(contentIDs) > 0 {
		var err error
		masters, err = scopedRecords(r, "content", RecordQuery{Owners: scope.Owners, IDs: contentIDs})
		if err != nil {
			return nil, err
		}
		revokedRows, err = scopedRecords(r, "content_revoked", RecordQuery{IDs: contentIDs})
		if err != nil {
			return nil, err
		}
	}
	versions := []Record{}
	if len(versionIDs) > 0 {
		var err error
		versions, err = scopedRecords(r, "version", RecordQuery{IDs: versionIDs})
		if err != nil {
			return nil, err
		}
	}
	revoked := map[string]bool{}
	for _, row := range revokedRows {
		revoked[row.ID] = true
	}
	masterByID := map[string]struct {
		content Content
		owner   string
	}{}
	for _, row := range masters {
		var content Content
		if json.Unmarshal(row.Payload, &content) != nil {
			return nil, ErrUnavailable
		}
		if content.ID == row.ID && content.Owner == row.Owner && scope.Allows(row.Owner) {
			masterByID[row.ID] = struct {
				content Content
				owner   string
			}{content, row.Owner}
		}
	}
	versionByID := map[string]Content{}
	for _, row := range versions {
		var content Content
		if json.Unmarshal(row.Payload, &content) != nil {
			return nil, ErrUnavailable
		}
		if contentKey(content.ID, content.Version) != row.ID {
			return nil, ErrUnavailable
		}
		if LegacyRoute(content.Route) {
			versionByID[row.ID] = content
			continue
		}
		master, ok := masterByID[content.ID]
		if ok && row.Owner == master.owner && content.Owner == master.owner && contentKey(content.ID, content.Version) == row.ID {
			versionByID[row.ID] = content
		}
	}
	mediaIDs := []string{}
	for _, row := range shares {
		share := row.share
		if share.Content == "" || share.Version <= 0 {
			continue
		}
		key := share.Route + "\x00" + contentKey(share.Content, share.Version)
		if share.Route == projectCollectionReaderRoute {
			continue
		}
		version, versionOK := versionByID[contentKey(share.Content, share.Version)]
		if LegacyRoute(share.Route) {
			if !legacyAvailable[share.Route+"\x00"+share.Content] {
				continue
			}
			if !versionOK {
				out[key] = VisitorContent{ID: share.Content, Version: share.Version, Route: share.Route, Title: "素材版本已失效", Status: "expired"}
				continue
			}
			if version.Status != "published" {
				continue
			}
			material := VisitorContent{ID: share.Content, Version: share.Version, Route: share.Route, Title: version.Title, Type: version.Type, Status: version.Status, Accessible: true}
			if version.Cover != nil && version.Cover.Kind == "image" && safeURL(version.Cover.URL) {
				cover := *version.Cover
				material.Cover = &cover
			}
			out[key] = material
			continue
		}
		master, masterOK := masterByID[share.Content]
		if revoked[share.Content] || masterOK && master.content.Status == "revoked" {
			out[key] = VisitorContent{ID: share.Content, Version: share.Version, Route: share.Route, Title: "素材已撤回", Status: "revoked"}
			continue
		}
		if masterOK && !versionOK {
			out[key] = VisitorContent{ID: share.Content, Version: share.Version, Route: share.Route, Title: "素材版本已失效", Status: "expired"}
			continue
		}
		if !masterOK || version.Status != "published" || !s.Config.ContentEnabled || share.Route != publishedReaderRoute {
			continue
		}
		material := VisitorContent{ID: share.Content, Version: share.Version, Route: share.Route, Title: version.Title, Type: version.Type, Status: version.Status, Accessible: true}
		if version.Cover != nil && version.Cover.Kind == "image" && version.Cover.URL == "/api/v1/miniprogram/tracking/media/"+version.Cover.ID {
			cover := *version.Cover
			material.Cover = &cover
			mediaIDs = appendUnique(mediaIDs, cover.ID)
		}
		out[key] = material
	}
	if len(mediaIDs) == 0 {
		return out, nil
	}
	mediaRows, err := scopedRecords(r, "media", RecordQuery{Owners: scope.Owners, IDs: mediaIDs})
	if err != nil {
		return nil, err
	}
	mediaOwners := map[string]string{}
	for _, row := range mediaRows {
		mediaOwners[row.ID] = row.Owner
	}
	for key, material := range out {
		if material.Cover != nil && mediaOwners[material.Cover.ID] != masterByID[material.ID].owner {
			material.Cover = nil
			out[key] = material
		}
	}
	return out, nil
}

type myShareTrafficStats struct {
	visitors       map[string]bool
	views          int
	directoryViews int
	contentViews   int
	last           *time.Time
}

func myShareTraffic(r Repository, owners, shares []string, f Filter, actor string) (map[string]myShareTrafficStats, error) {
	out := map[string]myShareTrafficStats{}
	if len(shares) == 0 {
		return out, nil
	}
	rows, err := scopedRecords(r, "event", RecordQuery{Owners: owners, Shares: shares, AtFrom: f.Start, AtBefore: f.End, AtThrough: f.AsOf})
	if err != nil {
		return nil, err
	}
	events := make([]myShareAcceptedEvent, 0, len(rows))
	sessions := make([]string, 0, len(rows))
	for _, row := range rows {
		var event Event
		if json.Unmarshal(row.Payload, &event) != nil {
			return nil, ErrUnavailable
		}
		if event.Type != "page_view" || event.Share == "" || event.Share != row.Share || event.Received.After(f.AsOf) || event.Occurred.After(f.AsOf) || excludedMyShareEvent(event) {
			continue
		}
		events = append(events, myShareAcceptedEvent{row, event})
		sessions = append(sessions, row.Session)
	}
	meta, err := loadSessionMeta(r, sessions, true, true, false)
	if err != nil {
		return nil, err
	}
	resolve := func(row Record) string { return resolvedMyShareVisitor(meta, row, f.AsOf) }
	deleted, err := deletedMyShareVisitors(r, events, resolve)
	if err != nil {
		return nil, err
	}
	for _, item := range events {
		visitor := resolve(item.row)
		if visitor == actor || meta.deleted[item.row.Session] || deleted[visitor] {
			continue
		}
		event := item.event
		stats := out[event.Share]
		if stats.visitors == nil {
			stats.visitors = map[string]bool{}
		}
		stats.visitors[visitor] = true
		stats.views++
		if event.Route == projectCollectionReaderRoute {
			stats.directoryViews++
		} else if event.Route == publishedReaderRoute && event.Content != "" {
			stats.contentViews++
		}
		at := event.Occurred
		if stats.last == nil || at.After(*stats.last) {
			stats.last = &at
		}
		out[event.Share] = stats
	}
	return out, nil
}

func excludedMyShareEvent(event Event) bool {
	return hasFlag(event, "self") || hasFlag(event, "preview") || hasFlag(event, "test") || hasFlag(event, "abnormal") || hasFlag(event, "duplicate")
}

// MyShareVisits returns only traffic attributed to one share that is still
// visible to the authenticated actor under the current owner grants.
func (s *Service) MyShareVisits(ctx context.Context, scope Scope, actor int64, shareID string, f Filter) (List, error) {
	out := List{Items: []MyShareVisit{}}
	if err := s.authorizeMyShares(scope, actor); err != nil {
		return out, err
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	// The requested window applies to visits, not to when the share was
	// created. An older share may still receive authorized traffic today.
	f.CreatedWindow = false
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		shares, err := s.actorShares(r, scope, actor, f, shareID, time.Time{}, "", 2)
		if err != nil {
			return err
		}
		if len(shares) != 1 {
			return fail(404, "分享访问记录不可访问")
		}
		var items []MyShareVisit
		var identified, anonymous int
		items, identified, anonymous, err = s.myShareVisitItems(ctx, r, scope, shares[0].share, f, actorVisitorKey(actor))
		if err != nil {
			return err
		}
		start, end, next, first, err := paginateDetail(f, len(items), "my-share-visits-v1", shareID, scope)
		if err != nil {
			return err
		}
		out.Items, out.Cursor, out.FirstCursor = items[start:end], next, first
		out.Identified, out.Anonymous = identified, anonymous
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

type myVisitAccumulator struct {
	first, last    time.Time
	views          int
	directoryViews int
	contentViews   int
	sessions       map[string]bool
	visible        int64
	watch          int64
	progress       int
}

type myShareAcceptedEvent struct {
	row   Record
	event Event
}

func resolvedMyShareVisitor(meta sessionMeta, row Record, asOf time.Time) string {
	link, ok := meta.identity[row.Session]
	if !ok || link.At.After(asOf) {
		return row.Visitor
	}
	var value map[string]string
	if json.Unmarshal(link.Payload, &value) == nil && value["original_visitor_key"] == row.Visitor && strings.HasPrefix(value["visitor_key"], "u:") {
		return value["visitor_key"]
	}
	return row.Visitor
}

func (s *Service) myShareVisitItems(ctx context.Context, r Repository, scope Scope, share Share, f Filter, actor string) ([]MyShareVisit, int, int, error) {
	rows, err := scopedRecords(r, "event", RecordQuery{Owners: []string{share.Owner}, Shares: []string{share.ID}, AtFrom: f.Start, AtBefore: f.End.Add(15 * time.Second), AtThrough: f.AsOf})
	if err != nil {
		return nil, 0, 0, err
	}
	events := make([]myShareAcceptedEvent, 0, len(rows))
	sessions := make([]string, 0, len(rows))
	for _, row := range rows {
		var event Event
		if json.Unmarshal(row.Payload, &event) != nil {
			return nil, 0, 0, ErrUnavailable
		}
		if event.Share != share.ID || row.Share != share.ID || event.Received.After(f.AsOf) || event.Occurred.After(f.AsOf) || row.Visitor == actor || excludedMyShareEvent(event) {
			continue
		}
		events = append(events, myShareAcceptedEvent{row, event})
		sessions = append(sessions, row.Session)
	}
	meta, err := loadSessionMeta(r, sessions, true, true, false)
	if err != nil {
		return nil, 0, 0, err
	}
	resolve := func(row Record) string { return resolvedMyShareVisitor(meta, row, f.AsOf) }
	deleted, err := deletedMyShareVisitors(r, events, resolve)
	if err != nil {
		return nil, 0, 0, err
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].event.Occurred.Equal(events[j].event.Occurred) {
			return events[i].event.ID < events[j].event.ID
		}
		return events[i].event.Occurred.Before(events[j].event.Occurred)
	})
	visits := map[string]*myVisitAccumulator{}
	viewVisitor := map[string]string{}
	covered := map[string][]interval{}
	for _, item := range events {
		if meta.deleted[item.row.Session] {
			continue
		}
		visitor := resolve(item.row)
		if visitor == actor || deleted[visitor] {
			continue
		}
		event := item.event
		if event.Type == "page_view" && within(event.Occurred, f) {
			acc := visits[visitor]
			if acc == nil {
				acc = &myVisitAccumulator{sessions: map[string]bool{}}
				visits[visitor] = acc
			}
			if acc.first.IsZero() || event.Occurred.Before(acc.first) {
				acc.first = event.Occurred
			}
			if event.Occurred.After(acc.last) {
				acc.last = event.Occurred
			}
			acc.views++
			if event.Route == projectCollectionReaderRoute {
				acc.directoryViews++
			} else if event.Route == publishedReaderRoute && event.Content != "" {
				acc.contentViews++
			}
			acc.sessions[item.row.Session] = true
			viewVisitor[event.View] = visitor
			continue
		}
		if event.Type == "video_progress" {
			visitor = viewVisitor[event.View]
			acc := visits[visitor]
			if visitor == "" || acc == nil || event.Watch <= 0 || !within(event.Occurred, f) {
				continue
			}
			acc.watch += event.Watch
			if pct := videoProgressPct(event); pct > acc.progress {
				acc.progress = pct
			}
			continue
		}
		visitor = viewVisitor[event.View]
		acc := visits[visitor]
		if visitor == "" || acc == nil || event.Type != "visible_segment" || event.Delta <= 0 {
			continue
		}
		lo, hi := event.Occurred.Add(-time.Duration(event.Delta)*time.Millisecond), event.Occurred
		if lo.Before(f.Start) {
			lo = f.Start
		}
		if hi.After(f.End) {
			hi = f.End
		}
		var added int64
		covered[item.row.Session], added = addInterval(covered[item.row.Session], interval{lo.UnixMilli(), hi.UnixMilli()})
		acc.visible += added
		if added > 0 && hi.After(acc.last) {
			acc.last = hi
		}
	}
	facts := make([]factView, 0, len(visits))
	for visitor := range visits {
		facts = append(facts, factView{PageView: PageView{ID: "my-share-" + visitor}, Visitor: visitor})
	}
	profiles, _, err := s.queryProfiles(ctx, r, facts)
	if err != nil {
		return nil, 0, 0, err
	}
	items := make([]MyShareVisit, 0, len(visits))
	identified, anonymous := 0, 0
	for key, acc := range visits {
		visitor := profiles[key]
		visitor.Last, visitor.Views, visitor.Visible, visitor.Sessions = acc.last, acc.views, acc.visible, len(acc.sessions)
		if visitor.State == "identified" {
			identified++
		} else {
			anonymous++
		}
		items = append(items, MyShareVisit{Visitor: visitor, First: acc.first, Last: acc.last, Views: acc.views, DirectoryViews: acc.directoryViews, ContentViews: acc.contentViews, Visible: acc.visible, Sessions: len(acc.sessions), VideoWatch: acc.watch, Progress: acc.progress})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Last.Equal(items[j].Last) {
			return items[i].Visitor.Key < items[j].Visitor.Key
		}
		return items[i].Last.After(items[j].Last)
	})
	return items, identified, anonymous, nil
}

func deletedMyShareVisitors(r Repository, events []myShareAcceptedEvent, resolve func(Record) string) (map[string]bool, error) {
	keys := make([]string, 0, len(events))
	for _, item := range events {
		keys = append(keys, resolve(item.row))
	}
	keys = uniqueSorted(keys)
	deleted := map[string]bool{}
	for len(keys) > 0 {
		n := sessionMetaChunk
		if n > len(keys) {
			n = len(keys)
		}
		rows, err := scopedRecords(r, "deletion", RecordQuery{IDs: keys[:n]})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			deleted[row.ID] = true
		}
		keys = keys[n:]
	}
	return deleted, nil
}
