package route

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"service/component/virtualpay"
	"service/reportwecom"
)

// 企微事件统一收口：报告回调在应答企微前把事件落入 wecom_event_inbox，
// 之后由本文件的后台任务补偿两类下游：
//  1. 报告业务（HandleEvent）内联失败时的重试；
//  2. 把原始密文转发给 Java CRM 的 /iycallback/handle，解决报告回调与
//     CRM 建档共用同一企微事件入口的共存问题（评审报告 P0-2）。
// 转发以新时间戳重新计算企微签名，要求两端回调 Token/AESKey 配置一致。
//
// 状态值：0=完成 1=待处理 2=跳过（不属于该下游） 3=永久失败。
// attempts 是两个下游共享的补偿处理轮次，每轮开始前持久化加一。

const wecomEventCRMDisabled = "未配置CRM桥接或转发已关闭"

type wecomEventInbox struct {
	db      *sql.DB
	config  reportwecom.Config
	service *reportwecom.Service
}

func newWecomEventInbox(db *sql.DB, cfg reportwecom.Config, service *reportwecom.Service) (*wecomEventInbox, error) {
	if db == nil {
		return nil, errors.New("wecom event inbox: no database")
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS wecom_event_inbox (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 dedup_key VARCHAR(64) NOT NULL UNIQUE,
 event_type VARCHAR(64) NOT NULL DEFAULT '',
 change_type VARCHAR(64) NOT NULL DEFAULT '',
 encrypted_body MEDIUMTEXT NOT NULL,
 plain_body MEDIUMTEXT NOT NULL,
 report_status TINYINT NOT NULL DEFAULT 1,
 crm_status TINYINT NOT NULL DEFAULT 1,
 attempts INT NOT NULL DEFAULT 0,
 last_error VARCHAR(255) NOT NULL DEFAULT '',
 next_attempt_at DATETIME NULL,
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
 INDEX idx_wecom_event_dispatch (report_status, crm_status, next_attempt_at),
 INDEX idx_wecom_event_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return nil, err
	}
	return &wecomEventInbox{db: db, config: cfg, service: service}, nil
}

func wecomEventDedupKey(plain []byte, e reportwecom.Event) string {
	if e.MsgId != "" {
		return e.MsgId
	}
	sum := sha256.Sum256(plain)
	return hex.EncodeToString(sum[:])
}

// wecomEventRelevantToCRM 与 Java 端 WecomController 感兴趣的事件保持一致，
// 其余事件不转发，避免无意义调用。
func wecomEventRelevantToCRM(e reportwecom.Event) bool {
	if e.Event != "change_external_contact" {
		return false
	}
	switch e.ChangeType {
	case "add_external_contact", "edit_external_contact", "del_follow_user", "del_external_contact":
		return true
	}
	return false
}

func (s *wecomEventInbox) Record(ctx context.Context, plain []byte, encrypted string, e reportwecom.Event) (string, bool, error) {
	key := wecomEventDedupKey(plain, e)
	crmStatus := int8(1)
	if !wecomEventRelevantToCRM(e) {
		crmStatus = 2
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO wecom_event_inbox
 (dedup_key,event_type,change_type,encrypted_body,plain_body,report_status,crm_status)
 VALUES (?,?,?,?,?,1,?) ON DUPLICATE KEY UPDATE dedup_key=dedup_key`,
		key, e.Event, e.ChangeType, encrypted, string(plain), crmStatus)
	if err != nil {
		return key, false, err
	}
	duplicate, err := resultPinnedInsert(result)
	if err != nil {
		return key, false, err
	}
	return key, duplicate, nil
}

func resultPinnedInsert(result sql.Result) (bool, error) {
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 0, nil
}

func (s *wecomEventInbox) Settled(dedupKey string, handleErr error, permanent bool) {
	var status int8
	msg := ""
	switch {
	case handleErr == nil:
		status = 0
	case permanent:
		status = 2
		msg = handleErr.Error()
	default:
		status = 1
		msg = handleErr.Error()
	}
	if len(msg) > 250 {
		msg = msg[:250]
	}
	_, err := s.db.Exec(`UPDATE wecom_event_inbox
 SET report_status=?, last_error=?,
     next_attempt_at=CASE WHEN report_status=1 THEN DATE_ADD(NOW(), INTERVAL 1 MINUTE) ELSE next_attempt_at END
 WHERE dedup_key=?`, status, msg, dedupKey)
	if err != nil {
		log.Printf("[wecom-inbox] settle report status failed: %v", err)
	}
}

func (s *wecomEventInbox) markCRM(dedupKey string, status int8, msg string, retry bool, attempts int) {
	if len(msg) > 250 {
		msg = msg[:250]
	}
	next := "next_attempt_at"
	if retry {
		wait := attempts * 5
		if wait > 60 {
			wait = 60
		}
		next = fmt.Sprintf("DATE_ADD(NOW(), INTERVAL %d MINUTE)", wait)
	}
	_, err := s.db.Exec(`UPDATE wecom_event_inbox
 SET crm_status=?, last_error=?, next_attempt_at=`+next+` WHERE dedup_key=?`, status, msg, dedupKey)
	if err != nil {
		log.Printf("[wecom-inbox] mark crm status failed: %v", err)
	}
}

// forwardWecomEvent 把原始密文转发给 Java CRM 回调。对端按企微规则
// 校验签名与时间窗，转发时刻晚于原始推送，必须重算签名。
func (s *wecomEventInbox) forwardWecomEvent(ctx context.Context, encryptedBody string) (int, error) {
	base := strings.TrimRight(os.Getenv("CRM_BRIDGE_URL"), "/")
	if base == "" {
		return 0, errors.New("CRM bridge not configured")
	}
	body, err := xml.Marshal(struct {
		XMLName xml.Name `xml:"xml"`
		Encrypt string   `xml:"Encrypt"`
	}{Encrypt: encryptedBody})
	if err != nil {
		return 0, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := crmRandom()
	q := url.Values{}
	q.Set("msg_signature", virtualpay.SignPushSignature(s.config.Token, ts, nonce, encryptedBody))
	q.Set("timestamp", ts)
	q.Set("nonce", nonce)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/iycallback/handle?"+q.Encode(), strings.NewReader(string(body)))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/xml; charset=UTF-8")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, nil
}

var wecomEventDispatchLock sync.Mutex

func (s *wecomEventInbox) dispatchPending() {
	if !wecomEventDispatchLock.TryLock() {
		return
	}
	defer wecomEventDispatchLock.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT dedup_key,event_type,change_type,encrypted_body,plain_body,report_status,crm_status,attempts
 FROM wecom_event_inbox
 WHERE ((report_status=1) OR (crm_status=1)) AND (next_attempt_at IS NULL OR next_attempt_at<=NOW())
 ORDER BY id LIMIT 20`)
	if err != nil {
		log.Printf("[wecom-inbox] dispatch query failed: %v", err)
		return
	}
	type pending struct {
		key, eventType, changeType, encrypted, plain string
		reportStatus, crmStatus                      int8
		attempts                                     int
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.key, &p.eventType, &p.changeType, &p.encrypted, &p.plain, &p.reportStatus, &p.crmStatus, &p.attempts); err != nil {
			rows.Close()
			log.Printf("[wecom-inbox] dispatch scan failed: %v", err)
			return
		}
		batch = append(batch, p)
	}
	rows.Close()

	forwardEnabled := os.Getenv("CRM_EVENT_FORWARD_ENABLED") != "false"
	for _, p := range batch {
		if p.attempts >= 100 {
			if _, err := s.db.Exec(`UPDATE wecom_event_inbox
 SET report_status=CASE WHEN report_status=1 THEN 3 ELSE report_status END,
     crm_status=CASE WHEN crm_status=1 THEN 3 ELSE crm_status END,
     last_error='重试次数用尽', next_attempt_at=NULL
 WHERE dedup_key=? AND (report_status=1 OR crm_status=1)`, p.key); err != nil {
				log.Printf("[wecom-inbox] mark attempts exhausted failed: %v", err)
			}
			continue
		}
		result, err := s.db.Exec(`UPDATE wecom_event_inbox SET attempts=attempts+1 WHERE dedup_key=? AND attempts=?`, p.key, p.attempts)
		if err != nil {
			log.Printf("[wecom-inbox] persist dispatch attempt failed: %v", err)
			continue
		}
		affected, err := result.RowsAffected()
		if err != nil || affected == 0 {
			if err != nil {
				log.Printf("[wecom-inbox] read dispatch attempt result failed: %v", err)
			}
			continue
		}
		p.attempts++
		if p.crmStatus == 1 {
			switch {
			case !crmEnabled() || !forwardEnabled:
				s.markCRM(p.key, 2, wecomEventCRMDisabled, false, p.attempts)
			default:
				status, err := s.forwardWecomEvent(ctx, p.encrypted)
				switch {
				case err != nil:
					s.markCRM(p.key, 1, "转发失败: "+err.Error(), true, p.attempts)
				case status == http.StatusOK:
					s.markCRM(p.key, 0, "", false, p.attempts)
				case status == http.StatusForbidden || status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge:
					// 配置不一致或报文被拒：重试无意义，标记失败暴露给运维
					s.markCRM(p.key, 3, fmt.Sprintf("回调被拒绝 HTTP %d，请核对两端回调 Token/AESKey 配置", status), false, p.attempts)
				default:
					// 503=Java 尚未配置回调等暂时态，退避重试
					s.markCRM(p.key, 1, fmt.Sprintf("下游未就绪 HTTP %d", status), true, p.attempts)
				}
			}
		}
		if p.reportStatus == 1 && s.service != nil {
			var event reportwecom.Event
			if xml.Unmarshal([]byte(p.plain), &event) != nil {
				s.db.Exec(`UPDATE wecom_event_inbox SET report_status=3, last_error='事件解析失败' WHERE dedup_key=?`, p.key)
				continue
			}
			retryCtx, retryCancel := context.WithTimeout(ctx, 10*time.Second)
			handleErr := s.service.HandleEvent(retryCtx, event)
			retryCancel()
			s.Settled(p.key, handleErr, handleErr != nil && reportwecom.PermanentEventError(handleErr))
		}
	}
}

func (s *wecomEventInbox) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM wecom_event_inbox
 WHERE report_status IN (0,2) AND crm_status IN (0,2)
   AND updated_at < DATE_SUB(NOW(), INTERVAL 14 DAY)
 LIMIT 500`); err != nil {
		log.Printf("[wecom-inbox] cleanup failed: %v", err)
	}
}

// startWecomEventDispatch 启动补偿 worker；单实例部署按进程内锁防重入，
// 多实例部署时依赖 dedup_key 唯一约束与下游幂等兜底。
func startWecomEventDispatch(store *wecomEventInbox) {
	if store == nil {
		return
	}
	go func() {
		loops := 0
		for {
			store.dispatchPending()
			loops++
			if loops%120 == 0 {
				store.cleanup()
			}
			time.Sleep(30 * time.Second)
		}
	}()
}
