package route

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"log"
	"strconv"
	"sync"
	"time"
)

// The durable lead row is the outbox. A delivery checkpoint advances only after
// CRM acknowledges its idempotent source ID; crashes and missing checkpoints replay.
var crmSourceWorker sync.Mutex

func syncCRMLeads(db *sql.DB) error {
	if !crmSourceWorker.TryLock() {
		return nil
	}
	defer crmSourceWorker.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT l.id,l.name,l.phone,l.wechat,COALESCE(l.demand_summary,''),l.user_id,l.source,l.created_at,
 SHA2(CONCAT_WS('|',l.name,l.phone,l.wechat,COALESCE(l.demand_summary,'')),256),COALESCE(d.history_cursor,0)
 FROM customer_leads l LEFT JOIN customer_lead_crm_delivery d ON d.lead_id=l.id
 WHERE (EXISTS(SELECT 1 FROM customer_lead_followups f WHERE f.lead_id=l.id AND f.id>COALESCE(d.history_cursor,0)) OR d.lead_id IS NULL OR d.source_hash<>SHA2(CONCAT_WS('|',l.name,l.phone,l.wechat,COALESCE(l.demand_summary,'')),256))
 AND (d.next_attempt_at IS NULL OR d.next_attempt_at<=NOW()) ORDER BY l.id LIMIT 20`)
	if err != nil {
		return err
	}
	type lead struct {
		id                                 int64
		name, phone, wechat, summary, hash string
		userID                             int64
		channel                            string
		createdAt                          time.Time
		historyCursor                      int64
	}
	var batch []lead
	for rows.Next() {
		var l lead
		if err = rows.Scan(&l.id, &l.name, &l.phone, &l.wechat, &l.summary, &l.userID, &l.channel, &l.createdAt, &l.hash, &l.historyCursor); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, l := range batch {
		history, cursor, historyErr := crmLeadHistory(ctx, db, l.id, l.historyCursor)
		if historyErr != nil {
			return historyErr
		}
		body, _ := json.Marshal(map[string]any{"history": history, "submittedBy": strconv.FormatInt(l.userID, 10), "channel": crmSourceText(l.channel, 64), "createdAt": l.createdAt.UTC().Format(time.RFC3339), "externalId": strconv.FormatInt(l.id, 10), "name": l.name, "phone": l.phone, "wechat": crmSourceText(l.wechat, 80), "summary": crmSourceText(l.summary, 2000)})
		status, data, _, remoteErr := crmRemote(ctx, "POST", "/ingest/miniprogram", "@source", 1, nil, body)
		var ack struct {
			CustomerID string `json:"customerId"`
		}
		decodeErr := json.Unmarshal(data, &ack)
		if remoteErr == nil && status == 200 && decodeErr == nil && ack.CustomerID != "" {
			_, err = db.ExecContext(ctx, `INSERT INTO customer_lead_crm_delivery(lead_id,customer_id,source_hash,history_cursor,attempts,last_error,next_attempt_at,synced_at) VALUES (?,?,?,?,0,'',NULL,NOW()) ON DUPLICATE KEY UPDATE customer_id=VALUES(customer_id),source_hash=VALUES(source_hash),history_cursor=GREATEST(history_cursor,VALUES(history_cursor)),last_error='',next_attempt_at=NULL,synced_at=NOW()`, l.id, ack.CustomerID, l.hash, cursor)
		} else {
			// Store a safe diagnostic, never response bodies containing customer data.
			_, err = db.ExecContext(ctx, `INSERT INTO customer_lead_crm_delivery(lead_id,attempts,last_error,next_attempt_at) VALUES (?,1,?,DATE_ADD(NOW(),INTERVAL 5 MINUTE)) ON DUPLICATE KEY UPDATE attempts=attempts+1,last_error=VALUES(last_error),next_attempt_at=VALUES(next_attempt_at)`, l.id, "客户服务接入失败，HTTP "+strconv.Itoa(status))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func registerCRMSources(g *gin.RouterGroup, db *sql.DB) {
	if db == nil {
		return
	}
	_, schemaErr := db.Exec(`CREATE TABLE IF NOT EXISTS customer_lead_crm_delivery (
 lead_id BIGINT UNSIGNED PRIMARY KEY,customer_id VARCHAR(128) NOT NULL DEFAULT '',source_hash VARCHAR(64) NOT NULL DEFAULT '',attempts INT NOT NULL DEFAULT 0,last_error VARCHAR(255) NOT NULL DEFAULT '',next_attempt_at DATETIME NULL,synced_at DATETIME NULL,history_cursor BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	if schemaErr == nil {
		var columnCount int
		schemaErr = db.QueryRow("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='customer_lead_crm_delivery' AND column_name='history_cursor'").Scan(&columnCount)
		if schemaErr == nil && columnCount == 0 {
			_, schemaErr = db.Exec("ALTER TABLE customer_lead_crm_delivery ADD COLUMN history_cursor BIGINT NOT NULL DEFAULT 0")
		}
	}
	g.GET("/source-sync", func(c *gin.Context) {
		access, ok := c.Get("management.access")
		if !ok || access.(managementAccess).Scope != "platform_admin" {
			crmError(c, 403, "仅平台管理员可查看来源接入")
			return
		}
		if schemaErr != nil {
			crmError(c, 503, "来源接入表未就绪")
			return
		}
		var total, done, failed int64
		if err := db.QueryRowContext(c.Request.Context(), `SELECT COUNT(*),COALESCE(SUM(d.customer_id<>''),0),COALESCE(SUM(d.last_error<>''),0) FROM customer_leads l LEFT JOIN customer_lead_crm_delivery d ON d.lead_id=l.id`).Scan(&total, &done, &failed); err != nil {
			crmError(c, 503, "来源状态读取失败")
			return
		}
		var historyPending int64
		if err := db.QueryRowContext(c.Request.Context(), "SELECT COUNT(*) FROM customer_lead_followups f LEFT JOIN customer_lead_crm_delivery d ON d.lead_id=f.lead_id WHERE f.id>COALESCE(d.history_cursor,0)").Scan(&historyPending); err != nil {
			crmError(c, 503, "历史跟进同步状态读取失败")
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"historyPending": historyPending, "total": total, "delivered": done, "failed": failed, "pending": total - done, "enabled": crmEnabled()}})
	})
	if schemaErr != nil || !crmEnabled() {
		return
	}
	go func() {
		for {
			if err := syncCRMLeads(db); err != nil {
				log.Print("CRM source checkpoint unavailable; will retry")
			}
			time.Sleep(time.Minute)
		}
	}()
}

func crmSourceText(s string, limit int) string {
	// Java validates UTF-16 lengths; non-BMP characters consume two units.
	units := 0
	for pos, r := range s {
		step := 1
		if r > 0xffff {
			step = 2
		}
		if units+step > limit {
			return s[:pos]
		}
		units += step
	}
	return s
}
func crmLeadHistory(ctx context.Context, db *sql.DB, id, cursor int64) ([]map[string]any, int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,operator_id,operator_name,COALESCE(remark,''),status,created_at FROM customer_lead_followups WHERE lead_id=? AND id>? ORDER BY id LIMIT 5`, id, cursor)
	if err != nil {
		return nil, cursor, err
	}
	defer rows.Close()
	history := []map[string]any{}
	for rows.Next() {
		var rid, operator int64
		var name, remark, status string
		var created time.Time
		if err = rows.Scan(&rid, &operator, &name, &remark, &status, &created); err != nil {
			return nil, cursor, err
		}
		history = append(history, map[string]any{"externalId": strconv.FormatInt(rid, 10), "operator": "legacy-" + strconv.FormatInt(operator, 10), "content": crmSourceText("原小程序跟进（"+name+"，原状态 "+status+"）："+remark, 2000), "createdAt": created.UTC().Format(time.RFC3339)})
		cursor = rid
	}
	return history, cursor, rows.Err()
}
