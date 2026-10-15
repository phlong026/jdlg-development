package tracking

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

// Catalog metadata is read inside the same bounded transaction as the facts.
// Legacy source rows are owner-scoped, then deduplicated by material ID.
func queryCatalog(r Repository, scope Scope, f Filter, id string) ([]Content, error) {
	rows, err := scopedRecords(r, "content", RecordQuery{Owners: permittedOwners(scope, f.Owner), Content: id, CatalogAsOf: f.AsOf})
	if err != nil {
		return nil, err
	}
	legacy, err := scopedRecords(r, "legacy_catalog", RecordQuery{Owners: permittedOwners(scope, f.Owner), Content: id, CatalogAsOf: f.AsOf})
	if err != nil {
		return nil, err
	}
	latest := map[string]Record{}
	for _, row := range legacy {
		if !scope.Allows(row.Owner) || f.Owner != "" && row.Owner != f.Owner {
			continue
		}
		var c Content
		if json.Unmarshal(row.Payload, &c) != nil {
			return nil, ErrUnavailable
		}
		if catalogCreatedAt(c, row.At).After(f.AsOf) || id != "" && c.ID != id {
			continue
		}
		old, ok := latest[c.ID]
		if !ok || row.At.After(old.At) || row.At.Equal(old.At) && row.ID < old.ID {
			latest[c.ID] = row
		}
	}
	for _, row := range latest {
		rows = append(rows, row)
	}
	out := []Content{}
	for _, row := range rows {
		if !scope.Allows(row.Owner) || f.Owner != "" && row.Owner != f.Owner {
			continue
		}
		var c Content
		if json.Unmarshal(row.Payload, &c) != nil {
			return nil, ErrUnavailable
		}
		if catalogCreatedAt(c, row.At).After(f.AsOf) || id != "" && c.ID != id {
			continue
		}
		if c.Owner != row.Owner {
			return nil, ErrUnavailable
		}
		c.PageViews, c.VisibleMS, c.LastVisit, c.Identified, c.Anonymous = 0, 0, nil, 0, 0
		out = append(out, c)
	}
	return out, nil
}
func matchingCatalog(items []Content, search string) (map[string]bool, string) {
	selected := map[string]bool{}
	signature := []string{}
	for _, c := range items {
		if search == "" || strings.Contains(strings.ToLower(c.Title), strings.ToLower(search)) {
			selected[c.ID] = true
		}
		if search != "" {
			signature = append(signature, c.ID+"\x00"+c.Title)
		}
	}
	if search == "" {
		return selected, ""
	}
	sort.Strings(signature)
	return selected, hash(string(marshal(signature)))
}
func joinedQueryRevision(a, b string) string {
	if b == "" {
		return a
	}
	if a == "" {
		return b
	}
	return hash(a + ":" + b)
}
func filterFactTitles(r Repository, scope Scope, f Filter, facts []factView) ([]factView, string, error) {
	if f.ContentSearch == "" {
		return facts, "", nil
	}
	catalog, err := queryCatalog(r, scope, f, f.Content)
	if err != nil {
		return nil, "", err
	}
	selected, revision := matchingCatalog(catalog, f.ContentSearch)
	out := []factView{}
	for _, fact := range facts {
		if selected[fact.Content] {
			out = append(out, fact)
		}
	}
	return out, revision, nil
}

// Used by lazy detail endpoints so their metrics cannot escape the active
// title/nickname/identity selection. No new interpretation of intent is made.
func (s *Service) filterQueryFacts(ctx context.Context, r Repository, scope Scope, f Filter, facts []factView) ([]factView, string, error) {
	facts, titleRevision, err := filterFactTitles(r, scope, f, facts)
	if err != nil {
		return nil, "", err
	}
	if f.Search == "" && f.VisitorSearch == "" && !hasVisitorFilter(f) {
		return facts, titleRevision, nil
	}
	by := map[string]Visitor{}
	revision := ""
	if f.Search != "" || f.VisitorSearch != "" {
		by, revision, err = s.queryProfiles(ctx, r, facts)
		if err != nil {
			return nil, "", err
		}
	} else {
		for _, fact := range facts {
			by[fact.Visitor] = visitorPresentation(fact.Visitor)
		}
	}
	effective := ownerScope(scope, f.Owner)
	if f.Search != "" && effective.CRM && len(effective.Owners) == 1 {
		keys := make([]string, 0, len(by))
		for key := range by {
			keys = append(keys, key)
		}
		rows, err := scopedRecords(r, "crm", RecordQuery{Owners: effective.Owners, VisitorKeys: keys, AtThrough: f.AsOf})
		if err != nil {
			return nil, "", err
		}
		for _, row := range rows {
			if row.Owner != effective.Owners[0] || row.ID != privateKey(row.Owner, row.Visitor) {
				continue
			}
			v, ok := by[row.Visitor]
			if !ok {
				continue
			}
			var link map[string]string
			if json.Unmarshal(row.Payload, &link) != nil {
				return nil, "", ErrUnavailable
			}
			v.CRMLabel = link["business_number"]
			by[row.Visitor] = v
		}
	}
	out := []factView{}
	for _, fact := range facts {
		if visitorMatches(by[fact.Visitor], f) {
			out = append(out, fact)
		}
	}
	return out, joinedQueryRevision(revision, titleRevision), nil
}

func (s *Service) queryContents(ctx context.Context, scope Scope, f Filter, id string) (List, error) {
	out := List{Items: []Content{}}
	if err := s.authorized(scope); err != nil {
		return out, err
	}
	if !scope.View && !scope.Manage {
		return out, fail(403, "无素材访问权限")
	}
	// Legacy q/intent remain visitor-list-only. Material search has explicit fields.
	f.Search, f.Intent = "", ""
	if !scope.View && hasVisitorFilter(f) {
		return out, fail(403, "无访客筛选权限")
	}
	var err error
	f, err = s.Normalize(f)
	if err != nil {
		return out, err
	}
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		selectedID := id
		if selectedID == "" {
			selectedID = f.Content
		}
		catalog, e := queryCatalog(r, scope, f, selectedID)
		if e != nil {
			return e
		}
		selected, titleRevision := matchingCatalog(catalog, f.ContentSearch)
		facts := []factView{}
		profileRevision := ""
		if scope.View {
			metrics := f
			if id != "" {
				metrics.Content = id
			}
			// Catalog title selection was already evaluated. Preserve that same set.
			metrics.ContentSearch = ""
			facts, e = s.facts(r, scope, metrics)
			if e != nil {
				return e
			}
			selectedFacts := []factView{}
			for _, fact := range facts {
				if selected[fact.Content] {
					selectedFacts = append(selectedFacts, fact)
				}
			}
			facts, profileRevision, e = s.filterQueryFacts(ctx, r, scope, metrics, selectedFacts)
			if e != nil {
				return e
			}
		}
		revision := joinedQueryRevision(profileRevision, titleRevision)
		if e = checkProfileRevision(f, revision); e != nil {
			return e
		}
		out.ProfileRevision = revision
		summaries := map[string]Content{}
		visitors := map[string]map[string]bool{}
		totalVisitors := map[string]bool{}
		for _, fact := range facts {
			if fact.Content == "" {
				continue
			}
			c := summaries[fact.Content]
			if fact.Count {
				c.PageViews++
			}
			c.VisibleMS += fact.Visible
			if c.LastVisit == nil || fact.Last.After(*c.LastVisit) {
				at := fact.Last
				c.LastVisit = &at
			}
			if visitors[fact.Content] == nil {
				visitors[fact.Content] = map[string]bool{}
			}
			if !visitors[fact.Content][fact.Visitor] {
				if visitorPresentation(fact.Visitor).State == "anonymous" {
					c.Anonymous++
				} else {
					c.Identified++
				}
				visitors[fact.Content][fact.Visitor] = true
			}
			totalVisitors[fact.Visitor] = true
			summaries[fact.Content] = c
		}
		items := []Content{}
		for _, c := range catalog {
			if !selected[c.ID] || hasVisitorFilter(f) && len(visitors[c.ID]) == 0 {
				continue
			}
			summary := summaries[c.ID]
			c.PageViews, c.VisibleMS, c.LastVisit = summary.PageViews, summary.VisibleMS, summary.LastVisit
			c.Identified, c.Anonymous = summary.Identified, summary.Anonymous
			items = append(items, c)
		}
		for key := range totalVisitors {
			if visitorPresentation(key).State == "anonymous" {
				out.Anonymous++
			} else {
				out.Identified++
			}
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
		out.Items, out.Cursor = page, withProfileRevision(cursor, revision)
		out.FirstCursor = withProfileRevision(firstContentCursor(f), revision)
		return nil
	})
	return out, err
}
