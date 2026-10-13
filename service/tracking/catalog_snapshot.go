package tracking

import (
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
	case "page_views":
		return int64(c.PageViews)
	case "visible_ms":
		return c.VisibleMS
	default:
		if c.LastVisit != nil {
			return c.LastVisit.UnixNano()
		}
		return 0
	}
}
func paginateContents(f Filter, items []Content) ([]Content, string, error) {
	signatureFilter := f
	signatureFilter.Cursor = ""
	signature := hash(string(marshal(signatureFilter)))
	type contentCursor struct {
		Kind      string `json:"kind"`
		Signature string `json:"signature"`
		ID        string `json:"id"`
		Value     int64  `json:"value"`
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
				value := contentSortValue(item, f.Sort)
				after := value == c.Value && item.ID > c.ID
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
		next = base64.RawURLEncoding.EncodeToString(marshal(contentCursor{Kind: "content-v1", Signature: signature, ID: last.ID, Value: contentSortValue(last, f.Sort)}))
	}
	return items[start:end], next, nil
}

// First page reuses the exact catalog snapshot and its query revision.
func firstContentCursor(f Filter) string {
	f.Cursor = ""
	return base64.RawURLEncoding.EncodeToString(marshal(map[string]any{"kind": "content-v1", "signature": hash(string(marshal(f))), "first": true}))
}
