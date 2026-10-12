package tracking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// VisitorContent is a historical visit projection, never an editor document or
// a replay of revoked content. There is no body, source URL, or media list.
type VisitorContent struct {
	ID         string    `json:"content_id"`
	Version    int       `json:"content_version"`
	Title      string    `json:"title"`
	Type       string    `json:"type"`
	Status     string    `json:"status"`
	Cover      *Asset    `json:"cover,omitempty"`
	Route      string    `json:"route_key,omitempty"`
	Accessible bool      `json:"content_accessible"`
	PageViews  int       `json:"page_views"`
	VisibleMS  int64     `json:"visible_ms"`
	LastVisit  time.Time `json:"last_visit"`
}

func (s *Service) visitedContent(ctx context.Context, r Repository, scope Scope, id string, version int, route string) (VisitorContent, error) {
	out := VisitorContent{ID: id, Version: version, Route: route, Title: "素材暂不可访问", Status: "unavailable"}
	if id == "" || version <= 0 || s.Config.validateRoute(route) != nil {
		return out, nil
	}
	// Owned articles require an authorized current master and no revocation.
	// Legacy metadata requires the original public source to remain public.
	materialOwner := ""
	if !LegacyRoute(route) {
		row, err := r.Get("content", id)
		if errors.Is(err, ErrNotFound) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		if !scope.Allows(row.Owner) {
			return out, nil
		}
		var current Content
		materialOwner = row.Owner
		if json.Unmarshal(row.Payload, &current) != nil || current.ID != id || current.Owner != row.Owner {
			return out, ErrUnavailable
		}
		revoked, err := tombstoned(r, "content_revoked", id)
		if err != nil {
			return out, err
		}
		if revoked || current.Status == "revoked" {
			out.Title, out.Status = "素材已撤回", "revoked"
			return out, nil
		}
		if !s.Config.ContentEnabled || route != publishedReaderRoute {
			return out, nil
		}
	} else {
		_, err := s.resolveLegacy(sourceReadContext(ctx, r), route, id)
		if err != nil {
			var te *Error
			if errors.As(err, &te) && (te.Status == 404 || te.Status == 410) {
				return out, nil
			}
			return out, err
		}
	}
	row, err := r.Get("version", contentKey(id, version))
	if errors.Is(err, ErrNotFound) {
		out.Title, out.Status = "素材版本已失效", "expired"
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var c Content
	if json.Unmarshal(row.Payload, &c) != nil {
		return out, ErrUnavailable
	}
	if c.ID != id || c.Version != version || c.Status != "published" {
		return out, nil
	}
	if !LegacyRoute(route) && (!scope.Allows(row.Owner) || row.Owner != materialOwner || c.Owner != materialOwner) {
		return out, nil
	}
	out.Title, out.Type, out.Status, out.Accessible = c.Title, c.Type, c.Status, true
	if c.Cover != nil && c.Cover.Kind == "image" {
		a := *c.Cover
		// Managed assets still pass the existing media endpoint authorization.
		if LegacyRoute(route) {
			if safeURL(a.URL) {
				out.Cover = &a
			}
		} else if a.URL == "/api/v1/miniprogram/tracking/media/"+a.ID {
			media, err := r.Get("media", a.ID)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return out, err
			}
			if err == nil && media.Owner == row.Owner && scope.Allows(media.Owner) {
				out.Cover = &a
			}
		}
	}
	return out, nil
}

func (s *Service) VisitorContents(ctx context.Context, scope Scope, key string, f Filter) (List, error) {
	out := List{Items: []VisitorContent{}}
	if err := s.authorizeView(scope); err != nil {
		return out, err
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	err = s.queryTransaction(ctx, func(r Repository) error {
		facts, e := s.factsSelected(r, scope, f, factSelection{Visitors: []string{key}})
		if e != nil {
			return e
		}
		if len(facts) == 0 {
			return fail(404, "访客记录不可访问")
		}
		facts, revision, e := s.filterQueryFacts(ctx, r, scope, f, facts)
		if e != nil {
			return e
		}
		if e = checkProfileRevision(f, revision); e != nil {
			return e
		}
		out.ProfileRevision = revision
		by := map[string]VisitorContent{}
		effective := ownerScope(scope, f.Owner)
		omitted := map[string]bool{}
		for _, fact := range facts {
			if fact.Visitor != key {
				continue
			}
			if fact.Content == "" {
				omitted[fact.ID] = true
				continue
			}
			k := contentKey(fact.Content, fact.Version)
			c, ok := by[k]
			if !ok {
				c, e = s.visitedContent(ctx, r, effective, fact.Content, fact.Version, fact.Route)
				if e != nil {
					return e
				}
			}
			if fact.Count {
				c.PageViews++
			}
			c.VisibleMS += fact.Visible
			if fact.Last.After(c.LastVisit) {
				c.LastVisit = fact.Last
			}
			by[k] = c
		}
		out.OmittedPageCount = len(omitted)
		items := make([]VisitorContent, 0, len(by))
		for _, c := range by {
			items = append(items, c)
		}
		sort.Slice(items, func(i, j int) bool {
			a, b := items[i], items[j]
			av, bv := visitorContentSortValue(a, f.Sort), visitorContentSortValue(b, f.Sort)
			if av == bv {
				if a.ID == b.ID {
					return a.Version < b.Version
				}
				return a.ID < b.ID
			}
			if f.Direction == "asc" {
				return av < bv
			}
			return av > bv
		})
		page, next, first, e := paginateVisitorContents(f, scope, key, items)
		if e != nil {
			return e
		}
		out.Items, out.Cursor, out.FirstCursor = page, withProfileRevision(next, revision), withProfileRevision(first, revision)
		return nil
	})
	return out, err
}
func visitorContentSortValue(c VisitorContent, order string) int64 {
	switch order {
	case "page_views":
		return int64(c.PageViews)
	case "visible_ms":
		return c.VisibleMS
	default:
		return c.LastVisit.UnixNano()
	}
}

type visitorContentCursor struct {
	Kind      string `json:"kind"`
	Signature string `json:"signature"`
	ID        string `json:"id,omitempty"`
	Version   int    `json:"version,omitempty"`
	Value     int64  `json:"value,omitempty"`
	First     bool   `json:"first,omitempty"`
}

func paginateVisitorContents(f Filter, scope Scope, key string, items []VisitorContent) ([]VisitorContent, string, string, error) {
	canonical := f
	canonical.Cursor = ""
	owners := append([]string{}, scope.Owners...)
	sort.Strings(owners)
	signature := hash(string(marshal([]any{canonical, scope.Tenant, scope.Actor, owners, key})))
	start := 0
	if f.Cursor != "" {
		var c visitorContentCursor
		b, e := base64.RawURLEncoding.DecodeString(f.Cursor)
		if e != nil || json.Unmarshal(b, &c) != nil || c.Kind != "visitor-content-v1" || c.Signature != signature || !c.First && (!validID(c.ID) || c.Version < 0) {
			return nil, "", "", fail(400, "游标与筛选不一致")
		}
		if !c.First {
			start = len(items)
			for i, item := range items {
				value := visitorContentSortValue(item, f.Sort)
				after := value == c.Value && (item.ID > c.ID || item.ID == c.ID && item.Version > c.Version)
				if value != c.Value {
					if f.Direction == "asc" {
						after = value > c.Value
					} else {
						after = value < c.Value
					}
				}
				if after {
					start = i
					break
				}
			}
		}
	}
	end := start + f.Limit
	if end > len(items) {
		end = len(items)
	}
	next := ""
	if end < len(items) && end > start {
		last := items[end-1]
		next = base64.RawURLEncoding.EncodeToString(marshal(visitorContentCursor{Kind: "visitor-content-v1", Signature: signature, ID: last.ID, Version: last.Version, Value: visitorContentSortValue(last, f.Sort)}))
	}
	first := base64.RawURLEncoding.EncodeToString(marshal(visitorContentCursor{Kind: "visitor-content-v1", Signature: signature, First: true}))
	return items[start:end], next, first, nil
}
