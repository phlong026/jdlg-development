package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	VerifyBusiness func(context.Context, BusinessProof) (bool, error)
	// Server-side, current account grant lookup. Never derived from client fields.
	IsContentOperator    func(context.Context, int64, string) (bool, error)
	ResolveLegacy        func(string, int64) (LegacyMetadata, error)
	ResolveLegacyContext func(context.Context, string, int64) (LegacyMetadata, error)
	VerifyCRM            func(context.Context, Scope, string, string, string) error
	Store                Store
	Config               Config
	Now                  func() time.Time
}

func New(store Store, cfg Config) *Service {
	return &Service{Store: store, Config: cfg, Now: func() time.Time { return time.Now().UTC() }}
}
func (s *Service) transaction(ctx context.Context, fn func(Repository) error) error {
	if s.Store == nil || s.Config.Tenant == "" {
		return ErrUnavailable
	}
	return s.Store.Transaction(ctx, s.Config.Tenant, fn)
}
func (s *Service) authorized(scope Scope) error {
	if scope.Tenant != s.Config.Tenant || scope.Actor <= 0 || len(scope.Owners) == 0 {
		return fail(403, "未授权访问记录范围")
	}
	return nil
}
func (s *Service) entry(r Repository, token string) (Entry, error) {
	var en Entry
	if !validID(token) {
		return en, fail(401, "采集凭据无效")
	}
	en, e := decode[Entry](r, "entry", hash(token))
	if e != nil || s.Now().After(en.Expires) {
		return en, fail(401, "采集凭据已失效")
	}
	if _, e = r.Get("deletion_session", en.Session); e == nil {
		return en, fail(410, "访问记录已删除")
	}
	if _, e = r.Get("deletion", en.Visitor); e == nil {
		return en, fail(410, "访问记录已删除")
	}
	return en, nil
}
func (s *Service) available(ctx context.Context, r Repository, route, content string, version int) error {
	if e := s.Config.validateRoute(route); e != nil {
		return e
	}
	if route != "pages/contentreader/contentreader" {
		if LegacyRoute(route) {
			_, err := s.resolveLegacy(sourceReadContext(ctx, r), route, content)
			if err != nil {
				return err
			}
			if version <= 0 {
				return fail(404, "原内容版本无效")
			}
			return nil
		}
		if content != "" {
			return fail(400, "此页面不接受内容标识")
		}
		return nil
	}
	if !s.Config.ContentEnabled {
		return fail(404, "内容不可访问")
	}
	if _, e := r.Get("content_revoked", content); e == nil {
		return fail(404, "内容不可访问")
	}
	c, e := decode[Content](r, "version", contentKey(content, version))
	if e != nil || c.Status != "published" {
		return fail(404, "内容不可访问")
	}
	master, e := decode[Content](r, "content", content)
	if e != nil || master.Status == "revoked" {
		return fail(404, "内容不可访问")
	}
	return nil
}
func audit(r Repository, owner, action string, actor int64, target string, now time.Time, detail any) error {
	return r.Insert(record("audit", opaque(), owner, now, map[string]any{"action": action, "actor": actor, "target": target, "detail": detail, "at": now}))
}
func (s *Service) Enter(ctx context.Context, in EntryInput, account int64) (Entry, error) {
	var out Entry
	if !s.Config.Enabled {
		return out, fail(503, "采集未启用")
	}
	if in.Consent != s.Config.ConsentVersion || in.Consent == "" {
		return out, fail(403, "未同意访问记录告知")
	}
	if !validID(in.Client) {
		return out, fail(400, "客户端随机标识无效")
	}
	e := s.transaction(ctx, func(r Repository) error {
		var legacy Content
		if LegacyRoute(in.Route) {
			var err error
			legacy, err = s.resolveLegacy(sourceReadContext(ctx, r), in.Route, in.Content)
			if err != nil {
				return err
			}
			in.Version = legacy.Version
		}
		if e := s.available(ctx, r, in.Route, in.Content, in.Version); e != nil {
			return e
		}
		now := s.Now()
		owner := s.Config.DefaultOwner
		share := ""
		self := false
		if in.Route == publishedReaderRoute {
			content, err := decode[Content](r, "version", contentKey(in.Content, in.Version))
			if err != nil {
				return err
			}
			owner = content.Owner
			if account > 0 && s.IsContentOperator != nil {
				self, err = s.IsContentOperator(sourceReadContext(ctx, r), account, owner)
				if err != nil {
					return err
				}
			}
		}
		if in.ShareToken != "" {
			sh, e := decode[Share](r, "share", hash(in.ShareToken))
			if e == nil && sh.Route == in.Route && sh.Content == in.Content && sh.Version == in.Version && !sh.Revoked && now.Before(sh.Expires) {
				share = sh.ID
				owner = sh.Owner
				self = self || account > 0 && sh.Sharer == "u:"+strconv.FormatInt(account, 10)
			}
		}
		if owner == "" {
			return fail(503, "访问归属未配置")
		}
		anon := in.Anonymous
		if !validID(anon) {
			anon = opaque()
		}
		visitor := "a:" + anon
		if account > 0 {
			visitor = "u:" + strconv.FormatInt(account, 10)
		}
		if _, e := r.Get("deletion", visitor); e == nil {
			return fail(410, "访问记录已删除")
		}
		out = Entry{Route: in.Route, Content: in.Content, Version: in.Version, Session: opaque(), Visitor: visitor, Anonymous: anon, Account: account, Client: in.Client, Owner: owner, Share: share, Started: now, LastActive: now, Expires: now.Add(72 * time.Hour), Accessible: true, Self: self}
		if in.Previous != "" && in.ShareToken == "" {
			old, err := s.entry(r, in.Previous)
			linked := false
			if err == nil {
				existing, lookup := decode[map[string]string](r, "identity", old.Session)
				if lookup == nil {
					linked = true
					if existing["visitor_key"] != visitor {
						err = fail(409, "访问会话已关联其他身份")
					}
				} else if !errors.Is(lookup, ErrNotFound) {
					return lookup
				}
			}
			if err == nil && old.Client == in.Client && now.Sub(old.LastActive) < 30*time.Minute && old.Account == account && (in.Route != publishedReaderRoute || old.Owner == owner) {
				out.Session = old.Session
				out.Started = old.Started
				out.Share = old.Share
				out.Owner = old.Owner
				out.Self = out.Self || old.Self
			} else if err == nil && old.Client == in.Client && now.Sub(old.LastActive) < 30*time.Minute && old.Account == 0 && account > 0 && (in.Route != publishedReaderRoute || old.Owner == owner) {
				out.Session = old.Session
				out.Started = old.Started
				out.Share = old.Share
				out.Owner = old.Owner
				out.OriginalVisitor = old.Visitor
				if !linked {
					if e := r.Put(record("identity", old.Session, old.Owner, now, map[string]string{"visitor_key": visitor, "original_visitor_key": old.Visitor})); e != nil {
						return e
					}
					if e := audit(r, old.Owner, "identity_link", account, old.Session, now, map[string]string{"source": "verified_same_client_session"}); e != nil {
						return e
					}
				}
			}
		}
		if legacy.ID != "" {
			if err := saveLegacy(r, legacy, out.Owner, now); err != nil {
				return err
			}
		}
		token := opaque()
		if e := r.Insert(record("entry", hash(token), out.Owner, now, out)); e != nil {
			return e
		}
		session := record("session", out.Session, out.Owner, out.Started, out)
		session.Visitor = out.Visitor
		session.Session = out.Session
		if e := r.Put(session); e != nil {
			return e
		}
		if out.OriginalVisitor != "" {
			if e := s.RebuildSessionSummaries(r, out.Session, now); e != nil {
				return e
			}
		}
		out.Token = token
		return nil
	})
	return out, e
}

type ShareInput struct {
	Route   string `json:"route_key"`
	Content string `json:"content_id"`
	Version int    `json:"content_version"`
	Parent  string `json:"parent_share_id"`
	Channel string `json:"channel"`
	Key     string `json:"idempotency_key"`
}

func (s *Service) CreateShare(ctx context.Context, in ShareInput, token string, account int64) (map[string]any, error) {
	var out map[string]any
	if !s.Config.Enabled {
		return out, fail(503, "采集未启用")
	}
	if !validID(in.Key) {
		return out, fail(400, "缺少幂等标识")
	}
	e := s.transaction(ctx, func(r Repository) error {
		var legacy Content
		if LegacyRoute(in.Route) {
			var err error
			legacy, err = s.resolveLegacy(sourceReadContext(ctx, r), in.Route, in.Content)
			if err != nil {
				return err
			}
			if in.Version != legacy.Version {
				return fail(409, "原内容已更新，请重新进入页面后分享")
			}
		}
		if e := s.available(ctx, r, in.Route, in.Content, in.Version); e != nil {
			return e
		}
		now := s.Now()
		owner := s.Config.DefaultOwner
		visitor := ""
		parent := ""
		session := ""
		if en, e := s.entry(r, token); e == nil {
			if en.Account != account {
				return fail(403, "分享账号与访问上下文不一致")
			}
			session = en.Session
			owner = en.Owner
			visitor = en.Visitor
			if in.Parent != "" && in.Parent == en.Share {
				parent = in.Parent
			}
		} else if account <= 0 {
			return fail(401, "需要有效访问上下文或登录")
		}
		if account > 0 {
			visitor = "u:" + strconv.FormatInt(account, 10)
		}
		if _, err := r.Get("deletion", visitor); err == nil {
			return fail(410, "访问记录已删除")
		}
		if owner == "" {
			return fail(503, "归属未配置")
		}
		if in.Route == "pages/contentreader/contentreader" {
			c, _ := decode[Content](r, "content", in.Content)
			owner = c.Owner
		}
		if legacy.ID != "" {
			if err := saveLegacy(r, legacy, owner, now); err != nil {
				return err
			}
		}
		key := hash(visitor + ":" + in.Key)
		finger := hash(string(marshal(in)))
		if old, e := decode[map[string]any](r, "share_request", key); e == nil {
			if old["fingerprint"] != finger {
				return fail(409, "幂等请求内容冲突")
			}
			out = old
			delete(out, "fingerprint")
			return nil
		}
		tok := opaque()
		sh := Share{ID: opaque(), Route: in.Route, Content: in.Content, Version: in.Version, Owner: owner, Sharer: visitor, Session: session, Parent: parent, Expires: now.Add(30 * 24 * time.Hour)}
		rec := record("share", hash(tok), owner, now, sh)
		rec.Share, rec.Visitor, rec.Session = sh.ID, sh.Sharer, sh.Session
		if e := r.Insert(rec); e != nil {
			return e
		}
		out = map[string]any{"share_id": sh.ID, "share_token": tok, "expires_at": sh.Expires, "fingerprint": finger}
		request := record("share_request", key, owner, now, out)
		request.Visitor, request.Session, request.Share = sh.Sharer, sh.Session, sh.ID
		if e := r.Insert(request); e != nil {
			return e
		}
		delete(out, "fingerprint")
		return nil
	})
	return out, e
}
func (s *Service) validateEvent(ctx context.Context, r Repository, en Entry, e *Event) error {
	now := s.Now()
	if e.Schema != 1 || !validID(e.ID) || (e.Type != "share_intent" && !validID(e.View)) || e.Session != en.Session {
		return fail(400, "事件版本或上下文无效")
	}
	if e.Route != en.Route || e.Content != en.Content || e.Version != en.Version {
		return fail(400, "页面内容绑定不一致")
	}
	if err := s.available(ctx, r, e.Route, e.Content, e.Version); err != nil {
		return err
	}
	if e.Occurred.IsZero() || e.Occurred.After(now.Add(2*time.Minute)) || e.Occurred.Before(now.Add(-72*time.Hour)) || e.Occurred.Before(en.Started.Add(-2*time.Minute)) {
		return fail(400, "事件时间超出允许窗口")
	}
	allowed := map[string]bool{"page_view": true, "page_end": true, "visible_segment": true, "share_intent": true, "content_click": true, "image_open": true, "outbound_click": true, "scroll_depth": true, "video_state": true, "video_progress": true}
	if !allowed[e.Type] {
		return fail(400, "不支持的客户端事件")
	}
	if e.SourceChannel != "" && e.SourceChannel != "app_message" && e.SourceChannel != "timeline" {
		return fail(400, "来源渠道无效")
	}
	if e.Monotonic < 0 || e.Seq < 0 || len(e.Component) > 80 || len(e.Action) > 80 || len(e.Target) > 128 || len(e.Media) > 128 || len(e.SDK) > 40 {
		return fail(400, "事件字段超限")
	}
	if e.Type == "page_view" && (e.AppState != "foreground" || e.PageState != "visible") {
		return fail(400, "页面未可见")
	}
	if e.Type == "visible_segment" {
		if !validID(e.Segment) || e.Seq <= 0 || e.Delta < 0 || e.Delta > 15000 || e.Monotonic < e.Delta {
			return fail(400, "可见分段无效")
		}
		if e.AppState != "foreground" || e.PageState != "visible" {
			return fail(400, "不可见事件不计时")
		}
	}
	if e.Type == "content_click" && e.Target == "" {
		return fail(400, "缺少点击目标")
	}
	if e.Scroll < 0 || e.Scroll > 100 {
		return fail(400, "滚动深度无效")
	}
	if strings.HasPrefix(e.Type, "video_") {
		if e.Seq <= 0 || len(e.Player) > 128 || e.Monotonic < e.Watch {
			return fail(400, "视频序号或计时无效")
		}
		if e.Route != "pages/contentreader/contentreader" {
			return fail(400, "视频未绑定自有阅读素材")
		}
		version, err := decode[Content](r, "version", contentKey(e.Content, e.Version))
		if err != nil {
			return fail(404, "视频内容不可访问")
		}
		found := false
		for _, m := range version.Media {
			if m.ID == e.Media && m.Kind == "video" {
				found = true
			}
		}
		if !found {
			return fail(404, "视频未属于该内容版本")
		}

		if !s.Config.VideoEnabled {
			return fail(403, "视频采集关闭")
		}
		if e.Media == "" || e.Player == "" || e.Position < 0 || e.Duration <= 0 || e.Position > e.Duration || e.Watch < 0 || e.Watch > 15000 || len(e.Ranges) > 256 || e.Rate < 0 || e.Rate > 4 {
			return fail(400, "视频事件无效")
		}
		if e.Watch > 0 && (e.PlayState != "playing" || e.AppState != "foreground" || e.PageState != "visible") {
			return fail(400, "无有效播放状态")
		}
		for _, rr := range e.Ranges {
			if rr[0] < 0 || rr[1] < rr[0] || rr[1] > e.Duration {
				return fail(400, "视频片段无效")
			}
		}
	}
	if e.Share != en.Share && e.Type != "share_intent" {
		return fail(400, "分享归因不一致")
	}
	if e.Type == "share_intent" && e.Share != "" {
		shares, err := scopedRecords(r, "share", RecordQuery{Shares: []string{e.Share}})
		if err != nil {
			return err
		}
		ok := false
		for _, sr := range shares {
			var sh Share
			_ = json.Unmarshal(sr.Payload, &sh)
			if sh.ID == e.Share && sh.Sharer == en.Visitor && (sh.Session == "" || sh.Session == en.Session) && sh.Owner == en.Owner && sh.Route == e.Route && sh.Content == e.Content && sh.Version == e.Version {
				ok = true
			}
		}
		if !ok {
			return fail(400, "分享操作无有效分享标识")
		}
	}
	// Client-supplied diagnostic labels are whitelisted and never confer trusted privileges.
	flags := []string{}
	for _, f := range e.Flags {
		if f == "sampling_gap" || f == "inferred_seek" || f == "queue_loss" {
			flags = append(flags, f)
		}
	}
	if now.Sub(e.Occurred) > time.Minute {
		flags = append(flags, "late")
	}
	if en.Self {
		flags = append(flags, "self")
	}
	if en.Preview {
		flags = append(flags, "preview")
	}
	if strings.HasPrefix(e.Type, "video_") {
		intervals := []interval{}
		for _, rr := range e.Ranges {
			intervals, _ = addInterval(intervals, interval{rr[0], rr[1]})
		}
		e.Unique = 0
		for _, rr := range intervals {
			e.Unique += rr.hi - rr.lo
		}
		if e.Unique > e.Duration {
			return fail(400, "视频覆盖无效")
		}
	}
	e.Flags = flags
	e.Received = now
	return nil
}
func (s *Service) Accept(ctx context.Context, token string, events []Event) ([]Result, error) {
	out := []Result{}
	if !s.Config.Enabled {
		return nil, fail(503, "采集关闭")
	}
	if len(events) == 0 || len(events) > 50 {
		return nil, fail(413, "批次最多50条")
	}
	err := s.transaction(ctx, func(r Repository) error {
		en, err := s.entry(r, token)
		if err != nil {
			return err
		}
		now := s.Now()
		// Transactional per-credential minute budget survives process restarts; duplicates still consume budget.
		bucket := hash(token) + ":" + now.Format("200601021504")
		n, _ := decode[int](r, "rate", bucket)
		if n+len(events) > 600 {
			return fail(429, "采集过于频繁")
		}
		if err = r.Put(record("rate", bucket, en.Owner, now, n+len(events))); err != nil {
			return err
		}
		for _, e := range events {
			res := Result{ID: e.ID, Status: "rejected"}
			original := e
			original.Received = time.Time{}
			fingerprint := hash(string(marshal(original)))
			if prior, err := decode[map[string]string](r, "event_hash", e.ID); err == nil {
				if prior["fingerprint"] == fingerprint {
					res.Status = "duplicate"
				} else {
					res.Reason = "event_id_conflict"
				}
				out = append(out, res)
				continue
			}
			if err = s.validateEvent(ctx, r, en, &e); err != nil {
				res.Reason = err.Error()
				out = append(out, res)
				continue
			}
			if e.Previous != "" {
				prev, perr := decode[map[string]string](r, "view_binding", e.Previous)
				if !validID(e.Previous) || e.Previous == e.View || (perr == nil && prev["session"] != en.Session) || (perr != nil && !errors.Is(perr, ErrNotFound)) {
					res.Reason = "previous_page_view_invalid"
					out = append(out, res)
					continue
				}
			}
			if e.Type != "share_intent" {
				binding, err := decode[map[string]string](r, "view_binding", e.View)
				want := map[string]string{"session": en.Session, "route": e.Route, "content": e.Content, "version": strconv.Itoa(e.Version)}
				if err == nil && string(marshal(binding)) != string(marshal(want)) {
					res.Reason = "page_view_binding_conflict"
					out = append(out, res)
					continue
				}
				if errors.Is(err, ErrNotFound) {
					if err = r.Insert(record("view_binding", e.View, en.Owner, e.Occurred, want)); err != nil {
						return err
					}
				} else if err != nil {
					return err
				}
			}
			if e.Type == "visible_segment" || strings.HasPrefix(e.Type, "video_") && e.Seq > 0 {
				seqKey := hash(e.View + ":" + e.Segment + ":" + e.Player + ":" + strconv.Itoa(e.Seq))
				if old, err := decode[string](r, "sequence", seqKey); err == nil {
					if old == fingerprint {
						res.Status = "duplicate"
					} else {
						res.Reason = "sequence_conflict"
					}
					out = append(out, res)
					continue
				}
				if err = r.Insert(record("sequence", seqKey, en.Owner, e.Occurred, fingerprint)); err != nil {
					return err
				}
			}
			er := record("event", e.ID, en.Owner, e.Occurred, e)
			er.Visitor = en.Visitor
			er.Session = en.Session
			er.Content = e.Content
			er.Share = en.Share
			if err = r.Insert(er); err != nil {
				return err
			}
			if err = r.Insert(record("event_hash", e.ID, en.Owner, e.Occurred, map[string]string{"fingerprint": fingerprint})); err != nil {
				return err
			}
			res.Status = "accepted"
			out = append(out, res)
		}
		if err = s.RebuildSessionSummaries(r, en.Session, now); err != nil {
			return err
		}
		en.LastActive = now
		if err = r.Put(record("entry", hash(token), en.Owner, now, en)); err != nil {
			return err
		}
		return nil
	})
	return out, err
}

// ConfirmBusiness is server-only. Call only after the underlying business record commits.
func (s *Service) ConfirmBusiness(ctx context.Context, session, action, businessID string) error {
	if !validID(businessID) || (action != "consultation" && action != "appointment" && action != "quote") {
		return fail(400, "业务结果无效")
	}
	return s.transaction(ctx, func(r Repository) error {
		en, e := decode[Entry](r, "session", session)
		if e != nil {
			return fail(404, "会话不可访问")
		}
		if _, e = r.Get("deletion_session", en.Session); e == nil {
			return fail(410, "访问记录已删除")
		}
		if _, e = r.Get("deletion", en.Visitor); e == nil {
			return fail(410, "访问记录已删除")
		}
		id := hash(action + ":" + businessID)
		if _, e = r.Get("business", id); e == nil {
			return nil
		}
		rec := record("business", id, en.Owner, s.Now(), map[string]string{"action": action, "session": session})
		rec.Visitor = en.Visitor
		rec.Session = session
		return r.Insert(rec)
	})
}
func sortEvents(events []Event) {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Occurred.Equal(events[j].Occurred) {
			if events[i].Seq == events[j].Seq {
				return events[i].ID < events[j].ID
			}
			return events[i].Seq < events[j].Seq
		}
		return events[i].Occurred.Before(events[j].Occurred)
	})
}
func reasonError(err error) error {
	if err == nil {
		return nil
	}
	var known *Error
	if errors.As(err, &known) {
		return err
	}
	return fmt.Errorf("%w", ErrUnavailable)
}

// ResolveShare reveals only a public destination. It does not establish an analytics session.
func (s *Service) ResolveShare(ctx context.Context, token string) (map[string]any, error) {
	var out map[string]any
	if !validID(token) {
		return out, fail(404, "分享入口无效")
	}
	err := s.transaction(ctx, func(r Repository) error {
		sh, e := decode[Share](r, "share", hash(token))
		if e != nil || sh.Revoked || s.Now().After(sh.Expires) {
			return fail(404, "分享入口无效")
		}
		if e = s.available(ctx, r, sh.Route, sh.Content, sh.Version); e != nil {
			return e
		}
		if LegacyRoute(sh.Route) {
			current, err := s.resolveLegacy(sourceReadContext(ctx, r), sh.Route, sh.Content)
			if err != nil {
				return err
			}
			if current.Version != sh.Version {
				return fail(404, "原内容已更新，分享归因已失效")
			}
		}
		out = map[string]any{"route_key": sh.Route, "content_id": sh.Content, "content_version": sh.Version, "share_token": token}
		return nil
	})
	return out, err
}

// An out-of-order predecessor remains a hint until its same-session binding arrives.
func resolvedPrevious(r Repository, session, previous string) string {
	if previous == "" {
		return ""
	}
	binding, err := decode[map[string]string](r, "view_binding", previous)
	if err != nil || binding["session"] != session {
		return ""
	}
	return previous
}
