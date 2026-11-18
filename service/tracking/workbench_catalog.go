package tracking

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sort"
	"time"
)

const (
	workbenchCreatedAt  = "created_at"
	workbenchPageViews  = "workbench_page_views"
	workbenchCollection = "collection"
)

// WorkbenchContents exposes content metadata and, when requested, aggregate
// view totals for the authorized catalog. Tracking analytics retain their
// separate visitor permission.
func (s *Service) WorkbenchContents(ctx context.Context, scope Scope, f Filter) (List, error) {
	out := List{Items: []Content{}}
	if err := s.authorized(scope); err != nil {
		return out, err
	}
	if !scope.Manage || hasVisitorFilter(f) || f.Visitor != "" {
		return out, fail(403, "无内容工作台查询权限")
	}
	order := f.Sort
	switch order {
	case "", workbenchCreatedAt:
		order = workbenchCreatedAt
	case "page_views":
		order = workbenchPageViews
		f.Start, f.End = time.Time{}, time.Time{}
	case workbenchCollection:
		if err := s.projectCollectionAccess(scope); err != nil {
			return out, err
		}
	case "last_visit", "shares", "visible_ms":
		scope.View = false
		return s.Contents(ctx, scope, f, "")
	default:
		return out, fail(400, "排序无效")
	}
	f.Sort, f.Search, f.Intent = "", "", ""
	f, err := s.Normalize(f)
	if err != nil {
		return out, err
	}
	f.Sort = order
	out.Snapshot = Snapshot{f.AsOf, MetricVersion}
	err = s.readQueryTransaction(ctx, func(r Repository) error {
		catalog, err := queryCatalog(r, scope, f, f.Content)
		if err != nil {
			return err
		}
		selected, revision := matchingCatalog(catalog, f.ContentSearch)
		items := make([]Content, 0, len(selected))
		for _, item := range catalog {
			if selected[item.ID] {
				items = append(items, item)
			}
		}
		switch order {
		case workbenchPageViews:
			err = s.addWorkbenchPageViews(r, scope, f, items)
		case workbenchCollection:
			var collectionRevision string
			collectionRevision, err = groupWorkbenchContents(r, scope, f, items)
			revision = joinedQueryRevision(revision, collectionRevision)
		}
		if err != nil {
			return err
		}
		if err = checkProfileRevision(f, revision); err != nil {
			return err
		}
		sort.Slice(items, func(i, j int) bool {
			return compareContentOrder(f, contentOrder(items[i], order), contentOrder(items[j], order)) < 0
		})
		page, cursor, err := paginateContents(f, items)
		if err != nil {
			return err
		}
		out.Items, out.Cursor = page, withProfileRevision(cursor, revision)
		out.ProfileRevision = revision
		out.FirstCursor = withProfileRevision(firstContentCursor(f), revision)
		return nil
	})
	return out, err
}

type workbenchContentKey struct{ owner, id string }

func (s *Service) addWorkbenchPageViews(r Repository, scope Scope, f Filter, items []Content) error {
	if len(items) == 0 {
		return nil
	}
	counts := make(map[workbenchContentKey]int, len(items))
	owners := make(map[string]bool)
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		counts[workbenchContentKey{item.Owner, item.ID}] = 0
		owners[item.Owner] = true
		ids[item.ID] = true
	}
	// Retained projections and raw events share the same validity rules as the
	// existing tracking totals, including deleted sessions and test events.
	selectedOwners := make([]string, 0, len(owners))
	for _, owner := range scope.Owners {
		if owners[owner] {
			selectedOwners = append(selectedOwners, owner)
		}
	}
	scope.Owners = selectedOwners
	facts, err := s.readFacts(r, scope, f, factSelection{Summaries: true, ContentIDs: slices.Sorted(maps.Keys(ids))})
	if err != nil {
		return err
	}
	for _, fact := range facts {
		key := workbenchContentKey{fact.Owner, fact.Content}
		if _, present := counts[key]; present && fact.Count {
			counts[key]++
		}
	}
	for i, item := range items {
		items[i].PageViews = counts[workbenchContentKey{item.Owner, item.ID}]
	}
	return nil
}

func groupWorkbenchContents(r Repository, scope Scope, f Filter, items []Content) (string, error) {
	byContent := make(map[workbenchContentKey]ProjectCollection, len(items))
	for _, item := range items {
		byContent[workbenchContentKey{item.Owner, item.ID}] = ProjectCollection{}
	}
	if len(items) > 0 {
		rows, err := scopedRecords(r, projectCollectionKind, RecordQuery{
			Owners: permittedOwners(scope, f.Owner), VisitorKeys: []string{projectCollectionActorKey(scope.Actor)}, ExactVisitors: true,
		})
		if err != nil {
			return "", err
		}
		for _, row := range rows {
			var stored projectCollectionStored
			if json.Unmarshal(row.Payload, &stored) != nil || stored.CreatorActor != scope.Actor || stored.Owner != row.Owner || stored.ID != row.ID {
				return "", ErrUnavailable
			}
			if stored.Status == "deleted" || !scope.Allows(row.Owner) || f.Owner != "" && f.Owner != row.Owner || stored.CreatedAt.After(f.AsOf) {
				continue
			}
			for _, member := range stored.Items {
				key := workbenchContentKey{row.Owner, member.ContentID}
				current, present := byContent[key]
				if present && (current.ID == "" || stored.CreatedAt.After(current.CreatedAt) || stored.CreatedAt.Equal(current.CreatedAt) && stored.ID < current.ID) {
					byContent[key] = stored.ProjectCollection
				}
			}
		}
	}
	// The mutable collection membership cannot be reconstructed at an old
	// snapshot. Reject a continued cursor when its visible grouping changed.
	groups := make([]string, 0, len(items))
	for i, item := range items {
		collection := byContent[workbenchContentKey{item.Owner, item.ID}]
		items[i].CollectionID, items[i].CollectionTitle = collection.ID, collection.Title
		if collection.ID != "" {
			groups = append(groups, item.Owner+"\x00"+item.ID+"\x00"+collection.ID+"\x00"+collection.Title)
		}
	}
	sort.Strings(groups)
	return hash(string(marshal(groups))), nil
}
