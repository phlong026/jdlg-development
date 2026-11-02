package route

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// registerContentPropagationManagement serves the admin propagation ranking
// (B9). Data comes from tracking_records kind='native_ref': CreatorActor is
// the referencing party, Owner is the content owner. No payload changes needed.
func registerContentPropagationManagement(group *gin.RouterGroup, db *sql.DB) {
	group.GET("/content/propagation", func(c *gin.Context) {
		if db == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "数据库不可用"})
			return
		}
		limit := intQueryDefault(c, "limit", 20)
		offset := intQueryDefault(c, "offset", 0)
		if limit < 1 || limit > 100 {
			limit = 20
		}

		// Aggregate native_ref records by target content, counting references
		// and distinct referencing actors. JSON payload carries target info.
		rows, err := db.Query(`
			SELECT
				JSON_UNQUOTE(JSON_EXTRACT(r.payload, '$.target.content_id')) AS content_id,
				MAX(JSON_UNQUOTE(JSON_EXTRACT(r.payload, '$.target.title'))) AS title,
				COUNT(*) AS reference_count,
				COUNT(DISTINCT JSON_EXTRACT(r.payload, '$.creator_actor')) AS active_referencers,
				MIN(r.occurred_at) AS first_referenced_at,
				MAX(r.occurred_at) AS last_referenced_at
			FROM tracking_records r
			WHERE r.kind = 'native_ref'
				AND JSON_EXTRACT(r.payload, '$.deleted') IS NULL
			GROUP BY content_id
			ORDER BY reference_count DESC, last_referenced_at DESC
			LIMIT ? OFFSET ?`, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "查询传播数据失败"})
			return
		}
		defer rows.Close()

		type PropagationItem struct {
			ContentID         string `json:"content_id"`
			Title             string `json:"title"`
			ReferenceCount    int    `json:"reference_count"`
			ActiveReferencers int    `json:"active_referencers"`
			FirstReferencedAt string `json:"first_referenced_at"`
			LastReferencedAt  string `json:"last_referenced_at"`
		}
		items := make([]PropagationItem, 0, limit)
		for rows.Next() {
			var item PropagationItem
			var firstRef, lastRef sql.NullString
			if err := rows.Scan(&item.ContentID, &item.Title, &item.ReferenceCount, &item.ActiveReferencers, &firstRef, &lastRef); err != nil {
				continue
			}
			item.FirstReferencedAt = firstRef.String
			item.LastReferencedAt = lastRef.String
			items = append(items, item)
		}

		// Total count for pagination
		var total int64
		db.QueryRow(`SELECT COUNT(DISTINCT JSON_EXTRACT(r.payload, '$.target.content_id')) FROM tracking_records r WHERE r.kind='native_ref' AND JSON_EXTRACT(r.payload,'$.deleted') IS NULL`).Scan(&total)

		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": items, "total": total, "limit": limit, "offset": offset}})
	})
}

func intQueryDefault(c *gin.Context, key string, fallback int) int {
	if raw := c.Query(key); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			return v
		}
	}
	return fallback
}
