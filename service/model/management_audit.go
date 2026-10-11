package model

import (
	"database/sql"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// ManagementOperationAudit 一条管理端写操作审计记录。
// 请求体刻意不落库（可能含密码/密钥明文），只记"谁、何时、对什么、做了什么方法、结果如何"。
type ManagementOperationAudit struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	Username   string    `json:"username"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Status     int       `json:"status"`
	DurationMs int       `json:"duration_ms"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
}

type ManagementAuditModel struct {
	DB *sql.DB
}

func NewManagementAuditModel(db *sql.DB) *ManagementAuditModel {
	return &ManagementAuditModel{DB: db}
}

// InitTable 幂等建表（与启动迁移同一份 DDL，供单测与独立部署场景使用）。
func (m *ManagementAuditModel) InitTable() error {
	_, err := m.DB.Exec(`
CREATE TABLE IF NOT EXISTS management_operation_audit (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  username VARCHAR(64) NOT NULL DEFAULT '',
  method VARCHAR(8) NOT NULL DEFAULT '',
  path VARCHAR(255) NOT NULL DEFAULT '',
  target_type VARCHAR(64) NOT NULL DEFAULT '',
  target_id VARCHAR(64) NOT NULL DEFAULT '',
  status INT NOT NULL DEFAULT 0,
  duration_ms INT NOT NULL DEFAULT 0,
  ip VARCHAR(45) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_audit_user (user_id, created_at),
  INDEX idx_audit_target (target_type, target_id),
  INDEX idx_audit_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`)
	return err
}

func (m *ManagementAuditModel) Insert(item *ManagementOperationAudit) error {
	res, err := m.DB.Exec(`
INSERT INTO management_operation_audit (user_id, username, method, path, target_type, target_id, status, duration_ms, ip)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.UserID, item.Username, item.Method, item.Path, item.TargetType, item.TargetID, item.Status, item.DurationMs, item.IP)
	if err != nil {
		return err
	}
	item.ID, _ = res.LastInsertId()
	return nil
}

// AuditListFilter 查询条件；零值表示不过滤。
type AuditListFilter struct {
	Username   string
	TargetType string
	Method     string
	Page       int
	PageSize   int
}

func (m *ManagementAuditModel) List(filter AuditListFilter) ([]*ManagementOperationAudit, int64, error) {
	where := " WHERE 1=1"
	args := []interface{}{}
	if filter.Username != "" {
		where += " AND username = ?"
		args = append(args, filter.Username)
	}
	if filter.TargetType != "" {
		where += " AND target_type = ?"
		args = append(args, filter.TargetType)
	}
	if filter.Method != "" {
		where += " AND method = ?"
		args = append(args, filter.Method)
	}

	var total int64
	if err := m.DB.QueryRow("SELECT COUNT(*) FROM management_operation_audit"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, pageSize := filter.Page, filter.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	args = append(args, pageSize, (page-1)*pageSize) // LIMIT ? OFFSET ? 依次是 取行数、跳过行数

	rows, err := m.DB.Query(`
SELECT id, user_id, username, method, path, target_type, target_id, status, duration_ms, ip, created_at
FROM management_operation_audit`+where+`
ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]*ManagementOperationAudit, 0)
	for rows.Next() {
		item := &ManagementOperationAudit{}
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.Method, &item.Path,
			&item.TargetType, &item.TargetID, &item.Status, &item.DurationMs, &item.IP, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, item)
	}
	return list, total, rows.Err()
}

// AuditWriter 带缓冲的异步审计写入器。
//
// 审计绝不能拖慢甚至阻塞业务请求：写库走带缓冲 channel + 单消费 goroutine；
// 缓冲耗尽时丢弃该条并计数告警（宁可缺一条审计，不可挂掉管理端）。
type AuditWriter struct {
	model    *ManagementAuditModel
	ch       chan *ManagementOperationAudit
	stopCh   chan struct{}
	wg       sync.WaitGroup
	dropped  atomic.Int64
	stopOnce sync.Once
}

func NewAuditWriter(m *ManagementAuditModel, bufferSize int) *AuditWriter {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	w := &AuditWriter{
		model:  m,
		ch:     make(chan *ManagementOperationAudit, bufferSize),
		stopCh: make(chan struct{}),
	}
	w.wg.Add(1)
	go w.consume()
	return w
}

func (w *AuditWriter) consume() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopCh:
			return
		case item := <-w.ch:
			if err := w.model.Insert(item); err != nil {
				log.Printf("[审计] 写入失败（该条丢失）: user=%s %s %s err=%v", item.Username, item.Method, item.Path, err)
			}
		}
	}
}

// Offer 非阻塞提交一条审计记录；缓冲满时丢弃并周期性告警。
func (w *AuditWriter) Offer(item *ManagementOperationAudit) {
	if w == nil {
		return
	}
	select {
	case w.ch <- item:
	default:
		n := w.dropped.Add(1)
		if n%100 == 1 {
			log.Printf("[审计][告警] 缓冲已满，累计丢弃 %d 条审计记录", n)
		}
	}
}

func (w *AuditWriter) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() { close(w.stopCh) })
	w.wg.Wait()
}
