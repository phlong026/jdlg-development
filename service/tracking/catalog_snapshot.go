package tracking

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"time"
)

// Pre-upgrade catalog rows have no creation field. Freeze their last persisted
// timestamp on the first mutation, rather than moving membership with updates.
// Their original creation time cannot be reconstructed from a mutable row alone;
// strict pre-upgrade historical reconstruction needs a reviewed audit backfill.
func catalogCreatedAt(c Content, persisted time.Time) time.Time {
	if c.CreatedAt != nil {
		return *c.CreatedAt
	}
	return persisted
}
func loadCatalogContent(r Repository, kind, id string) (Content, error) {
	var c Content
	row, err := r.Get(kind, id)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(row.Payload, &c); err != nil {
		return c, err
	}
	created := catalogCreatedAt(c, row.At).UTC()
	c.CreatedAt = &created
	c.Presentation = ContentPresentation(c)
	return c, nil
}

// New timestamps retain UTC nanoseconds. SQL DATETIME(6) supplies a broad
// microsecond bound; Contents applies the exact JSON timestamp before membership
// and legacy-owner deduplication. Trim only the SQL representation to six digits.
const catalogCreatedAtSQL = `COALESCE(
 STR_TO_DATE(LEFT(REPLACE(JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.created_at')),'Z',''),26),'%Y-%m-%dT%H:%i:%s.%f'),
 STR_TO_DATE(JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.created_at')),'%Y-%m-%dT%H:%i:%sZ'),
 r.occurred_at)`

// Content pagination uses the metric at the fixed as_of plus the immutable ID,
// rather than an offset that can repeat cards if an earlier item is suppressed.
func contentSortValue(c Content, order string) int64 {
	switch order {
	case "page_views", workbenchPageViews:
		return int64(c.PageViews)
	case "shares":
		return int64(c.Shares)
	case "visible_ms":
		return c.VisibleMS
	case workbenchCreatedAt, workbenchCollection:
		return 0
	default:
		if c.LastVisit != nil {
			return c.LastVisit.UnixNano()
		}
		return 0
	}
}

type contentOrderKey struct {
	ID              string `json:"id"`
	Value           int64  `json:"value"`
	CreatedAt       int64  `json:"created_at,omitempty"`
	CollectionID    string `json:"collection_id,omitempty"`
	CollectionTitle string `json:"collection_title,omitempty"`
}

func contentOrder(c Content, order string) contentOrderKey {
	key := contentOrderKey{ID: c.ID, Value: contentSortValue(c, order)}
	switch order {
	case workbenchCreatedAt, workbenchPageViews, workbenchCollection:
		if c.CreatedAt != nil {
			key.CreatedAt = c.CreatedAt.UnixNano()
		}
	}
	if order == workbenchCollection {
		key.CollectionID, key.CollectionTitle = c.CollectionID, c.CollectionTitle
	}
	return key
}

func compareContentOrder(f Filter, a, b contentOrderKey) int {
	if f.Sort == workbenchCollection && a.CollectionID != b.CollectionID {
		if a.CollectionID == "" {
			return 1
		}
		if b.CollectionID == "" {
			return -1
		}
		if titleOrder := cmp.Compare(a.CollectionTitle, b.CollectionTitle); titleOrder != 0 {
			return titleOrder
		}
		return cmp.Compare(a.CollectionID, b.CollectionID)
	}
	if metric := cmp.Compare(a.Value, b.Value); metric != 0 {
		if f.Direction == "asc" {
			return metric
		}
		return -metric
	}
	if creation := cmp.Compare(b.CreatedAt, a.CreatedAt); creation != 0 {
		if f.Sort == workbenchCreatedAt && f.Direction == "asc" {
			return -creation
		}
		return creation
	}
	return cmp.Compare(a.ID, b.ID)
}

func paginateContents(f Filter, items []Content) ([]Content, string, error) {
	signatureFilter := f
	signatureFilter.Cursor = ""
	signature := hash(string(marshal(signatureFilter)))
	type contentCursor struct {
		contentOrderKey
		Kind      string `json:"kind"`
		Signature string `json:"signature"`
		First     bool   `json:"first,omitempty"`
	}
	start := 0
	if f.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c contentCursor
		if err != nil || json.Unmarshal(b, &c) != nil || c.Kind != "content-v1" || c.Signature != signature || (!c.First && !validID(c.ID)) {
			return nil, "", fail(400, "游标与筛选不一致")
		}
		if !c.First {
			start = len(items)
			for i, item := range items {
				if compareContentOrder(f, contentOrder(item, f.Sort), c.contentOrderKey) > 0 {
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
		next = base64.RawURLEncoding.EncodeToString(marshal(contentCursor{Kind: "content-v1", Signature: signature, contentOrderKey: contentOrder(last, f.Sort)}))
	}
	return items[start:end], next, nil
}

// First page reuses the exact catalog snapshot and its query revision.
func firstContentCursor(f Filter) string {
	f.Cursor = ""
	return base64.RawURLEncoding.EncodeToString(marshal(map[string]any{"kind": "content-v1", "signature": hash(string(marshal(f))), "first": true}))
}
