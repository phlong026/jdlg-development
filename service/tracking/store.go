package tracking

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Repository is always opened with a server-selected tenant. No caller-provided SQL or tenant.
type Repository interface {
	Get(kind, id string) (Record, error)
	List(kind string) ([]Record, error)
	Put(Record) error
	Insert(Record) error
	Remove(kind, id string) error
}
type Store interface {
	Transaction(context.Context, string, func(Repository) error) error
}

// ReadStore runs fn on one consistent snapshot without taking the tenant lock,
// so reads never queue behind (or block) fact acceptance. Repositories passed to
// fn reject every write; a path that must write uses Transaction instead.
type ReadStore interface {
	ReadTransaction(context.Context, string, func(Repository) error) error
}

var ErrNotFound = errors.New("not found")

// ErrReadOnly marks a write attempted inside a read transaction: a programming
// error, never a client condition.
var ErrReadOnly = errors.New("tracking read transaction cannot write")

// SQLStore shares the application's connection pool. The optional slot limits
// cap how many pool connections tracking may hold at once, waiting in Go (not
// on a held connection) when full. Nil slots mean no limit.
type SQLStore struct {
	DB                    *sql.DB
	writeSlots, readSlots chan struct{}
}

func NewSQLStore(db *sql.DB, maxWriters, maxReaders int) *SQLStore {
	s := &SQLStore{DB: db}
	if maxWriters > 0 {
		s.writeSlots = make(chan struct{}, maxWriters)
	}
	if maxReaders > 0 {
		s.readSlots = make(chan struct{}, maxReaders)
	}
	return s
}

func acquireSlot(ctx context.Context, slots chan struct{}) (func(), error) {
	if slots == nil {
		return func() {}, nil
	}
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ErrUnavailable
	}
}

type sqlRepo struct {
	ctx      context.Context
	tx       *sql.Tx
	tenant   string
	budget   readBudget
	readOnly bool
}

func (s *SQLStore) ReadTransaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	release, e := acquireSlot(ctx, s.readSlots)
	if e != nil {
		return e
	}
	defer release()
	// REPEATABLE READ gives every query in fn the same snapshot, which the tenant
	// lock used to provide by excluding writers. READ ONLY makes MySQL reject a
	// write even if the Go-side guard were bypassed.
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	repo := &sqlRepo{ctx: ctx, tx: tx, tenant: tenant, readOnly: true}
	if e = fn(repo); e != nil {
		return e
	}
	if repo.budget.err != nil {
		return repo.budget.err
	}
	return tx.Commit()
}

func (s *SQLStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	release, e := acquireSlot(ctx, s.writeSlots)
	if e != nil {
		return e
	}
	defer release()
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Per-tenant serialization makes fact acceptance + identity/deletion + projection atomic.
	// Load/performance is an explicit release gate; replace with partitioned row locks after profiling.
	if _, e = tx.ExecContext(ctx, "INSERT IGNORE INTO tracking_tenant_lock(tenant_id) VALUES (?)", tenant); e != nil {
		return e
	}
	var locked string
	if e = tx.QueryRowContext(ctx, "SELECT tenant_id FROM tracking_tenant_lock WHERE tenant_id=? FOR UPDATE", tenant).Scan(&locked); e != nil {
		return e
	}
	repo := &sqlRepo{ctx: ctx, tx: tx, tenant: tenant}
	if e = fn(repo); e != nil {
		return e
	}
	if repo.budget.err != nil {
		return repo.budget.err
	}
	return tx.Commit()
}
func (r *sqlRepo) Get(kind, id string) (Record, error) {
	if r.budget.err != nil {
		return Record{}, r.budget.err
	}
	var v Record
	var b []byte
	var at string
	e := r.tx.QueryRowContext(r.ctx, `SELECT kind,record_id,owner_id,visitor_key,session_id,content_id,share_id,DATE_FORMAT(occurred_at,'%Y-%m-%d %H:%i:%s.%f'),CASE WHEN OCTET_LENGTH(payload)<=? THEN payload ELSE NULL END FROM tracking_records WHERE tenant_id=? AND kind=? AND record_id=?`, maxQueryPayloadBytes, r.tenant, kind, id).Scan(&v.Kind, &v.ID, &v.Owner, &v.Visitor, &v.Session, &v.Content, &v.Share, &at, &b)
	if e == sql.ErrNoRows {
		return v, ErrNotFound
	}
	if e != nil {
		return v, e
	}
	if b == nil {
		return v, r.budget.reject()
	}
	v.At, e = time.ParseInLocation("2006-01-02 15:04:05.999999", at, time.UTC)
	if e != nil {
		return v, e
	}
	v.Tenant = r.tenant
	v.Payload = b
	return v, r.budget.consume(v)
}
func (r *sqlRepo) List(kind string) ([]Record, error) {
	return r.ListScoped(kind, RecordQuery{})
}

const recordColumns = `r.kind,r.record_id,r.owner_id,r.visitor_key,r.session_id,r.content_id,r.share_id,DATE_FORMAT(r.occurred_at,'%Y-%m-%d %H:%i:%s.%f'),CASE WHEN OCTET_LENGTH(r.payload)<=? THEN r.payload ELSE NULL END`

// Legacy kinds keep their authoritative relationship fields in JSON. These
// expressions are fixed server strings, shared by filtering and identity joins.
func recordSessionColumn(kind string) string {
	switch kind {
	case "entry", "session", "share":
		return "JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.session_id'))"
	case "identity":
		return "r.record_id"
	case "view_binding":
		return "JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.session'))"
	default:
		return "r.session_id"
	}
}
func recordVisitorColumn(kind string) string {
	switch kind {
	case "entry", "session", "identity":
		return "JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.visitor_key'))"
	case "share":
		return "JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.sharer_id'))"
	case "daily_summary":
		return "JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.original_visitor_key'))"
	default:
		return "r.visitor_key"
	}
}
func recordShareColumn(kind string) string {
	switch kind {
	case "share", "share_request":
		return "JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.share_id'))"
	default:
		return "r.share_id"
	}
}

// buildScopedQuery only interpolates fixed column names. Every caller value,
// including limits, is bound. Identity aliases remain tenant/session scoped.
func buildScopedQuery(tenant, kind string, q RecordQuery, limit int) (string, []any) {
	where := []string{"r.tenant_id=?", "r.kind=?"}
	args := []any{maxQueryPayloadBytes, tenant, kind}
	addIn := func(column string, values []string) {
		if values == nil {
			return
		}
		if len(values) == 0 {
			where = append(where, "1=0")
			return
		}
		where = append(where, column+" IN ("+strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")+")")
		for _, value := range values {
			args = append(args, value)
		}
	}
	addIn("r.owner_id", q.Owners)
	addIn("r.record_id", q.IDs)
	addIn(recordSessionColumn(kind), q.Sessions)
	addIn(recordShareColumn(kind), q.Shares)
	if q.Content != "" {
		column := "r.content_id"
		if kind == "content" || kind == "legacy_catalog" {
			column = "JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.content_id'))"
		}
		where = append(where, column+"=?")
		args = append(args, q.Content)
	}
	if q.View != "" {
		path := "$.page_view_id"
		if kind == "daily_summary" {
			path = "$.page_view.page_view_id"
		}
		where = append(where, "JSON_UNQUOTE(JSON_EXTRACT(r.payload,'"+path+"'))=?")
		args = append(args, q.View)
	}
	stamp := func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05.999999") }
	if !q.AtFrom.IsZero() {
		where = append(where, "r.occurred_at>=?")
		args = append(args, stamp(q.AtFrom.Truncate(time.Microsecond)))
	}
	if !q.AtBefore.IsZero() {
		where = append(where, "r.occurred_at<?")
		args = append(args, stamp(ceilMicrosecond(q.AtBefore)))
	}
	if !q.AtThrough.IsZero() {
		where = append(where, "r.occurred_at<=?")
		args = append(args, stamp(q.AtThrough))
	}
	if !q.CatalogAsOf.IsZero() && (kind == "content" || kind == "legacy_catalog") {
		where = append(where, catalogCreatedAtSQL+"<=?")
		args = append(args, stamp(q.CatalogAsOf))
	}
	if q.VisitorKeys != nil {
		if len(q.VisitorKeys) == 0 {
			where = append(where, "1=0")
		} else {
			original := recordVisitorColumn(kind)
			slots := strings.TrimSuffix(strings.Repeat("?,", len(q.VisitorKeys)), ",")
			direct := original + " IN (" + slots + ")"
			copies := 2
			if kind == "daily_summary" {
				// Persisted projected identity is an additional candidate for
				// deletion; snapshot reads still resolve OriginalVisitor.
				direct += " OR r.visitor_key IN (" + slots + ")"
				copies++
			}
			clause := "(" + direct + " OR EXISTS (SELECT 1 FROM tracking_records identity_link WHERE identity_link.tenant_id=r.tenant_id AND identity_link.kind='identity' AND identity_link.record_id=" + recordSessionColumn(kind) + " AND JSON_UNQUOTE(JSON_EXTRACT(identity_link.payload,'$.original_visitor_key'))=" + original + " AND JSON_UNQUOTE(JSON_EXTRACT(identity_link.payload,'$.visitor_key')) LIKE 'u:%' AND JSON_UNQUOTE(JSON_EXTRACT(identity_link.payload,'$.visitor_key')) IN (" + slots + ")"
			for i := 0; i < copies; i++ {
				for _, key := range q.VisitorKeys {
					args = append(args, key)
				}
			}
			if !q.IdentityAsOf.IsZero() {
				clause += " AND identity_link.occurred_at<=?"
				args = append(args, stamp(q.IdentityAsOf))
			}
			where = append(where, clause+"))")
		}
	}
	if q.hasCursor() {
		// Rows are ordered by (occurred_at, record_id); resume strictly after the cursor.
		where = append(where, "(r.occurred_at>? OR (r.occurred_at=? AND r.record_id>?))")
		args = append(args, stamp(q.AfterAt), stamp(q.AfterAt), q.AfterID)
	}
	if q.Limit > 0 && q.Limit < limit {
		limit = q.Limit
	}
	if q.WithoutExactSummary {
		where = append(where, "NOT EXISTS (SELECT 1 FROM tracking_records summary_state WHERE summary_state.tenant_id=r.tenant_id AND summary_state.kind='summary_state' AND summary_state.record_id="+recordSessionColumn(kind)+" AND JSON_UNQUOTE(JSON_EXTRACT(summary_state.payload,'$.complete'))='true' AND JSON_EXTRACT(summary_state.payload,'$.deferred_from') IS NULL)")
	}
	args = append(args, limit)
	return "SELECT " + recordColumns + " FROM tracking_records r WHERE " + strings.Join(where, " AND ") + " ORDER BY r.occurred_at,r.record_id LIMIT ?", args
}
func (r *sqlRepo) ListScoped(kind string, q RecordQuery) ([]Record, error) {
	if queryHasNoRows(q) {
		return []Record{}, nil
	}
	if r.budget.err != nil {
		return nil, r.budget.err
	}
	query, args := buildScopedQuery(r.tenant, kind, q, maxQueryRecords-r.budget.rows+1)
	rows, err := r.tx.QueryContext(r.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var row Record
		var at string
		var payload []byte
		if err = rows.Scan(&row.Kind, &row.ID, &row.Owner, &row.Visitor, &row.Session, &row.Content, &row.Share, &at, &payload); err != nil {
			return nil, err
		}
		row.Payload = payload
		// Oversized JSON is never returned by the database into Go memory.
		if row.Payload == nil {
			return nil, r.budget.reject()
		}
		row.At, err = time.ParseInLocation("2006-01-02 15:04:05.999999", at, time.UTC)
		if err != nil {
			return nil, err
		}
		row.Tenant = r.tenant
		if err = r.budget.consume(row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
func (r *sqlRepo) write(v Record, upsert bool) error {
	if r.readOnly {
		return ErrReadOnly
	}
	if v.At.IsZero() {
		v.At = time.Now().UTC()
	}
	q := `INSERT INTO tracking_records(tenant_id,kind,record_id,owner_id,visitor_key,session_id,content_id,share_id,occurred_at,payload) VALUES (?,?,?,?,?,?,?,?,?,?)`
	if upsert {
		q += ` ON DUPLICATE KEY UPDATE owner_id=VALUES(owner_id),visitor_key=VALUES(visitor_key),session_id=VALUES(session_id),content_id=VALUES(content_id),share_id=VALUES(share_id),occurred_at=VALUES(occurred_at),payload=VALUES(payload)`
	}
	_, e := r.tx.ExecContext(r.ctx, q, r.tenant, v.Kind, v.ID, v.Owner, v.Visitor, v.Session, v.Content, v.Share, v.At.UTC().Format("2006-01-02 15:04:05.999999"), string(v.Payload))
	return e
}
func (r *sqlRepo) Put(v Record) error    { return r.write(v, true) }
func (r *sqlRepo) Insert(v Record) error { return r.write(v, false) }
func (r *sqlRepo) Remove(kind, id string) error {
	if r.readOnly {
		return ErrReadOnly
	}
	_, e := r.tx.ExecContext(r.ctx, "DELETE FROM tracking_records WHERE tenant_id=? AND kind=? AND record_id=?", r.tenant, kind, id)
	return e
}
func decode[T any](r Repository, kind, id string) (T, error) {
	var v T
	x, e := r.Get(kind, id)
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(x.Payload, &v)
	return v, e
}
func record(kind, id, owner string, at time.Time, v any) Record {
	return Record{Kind: kind, ID: id, Owner: owner, At: at, Payload: marshal(v)}
}

// ListSession uses the tenant/session index and the same fail-closed budget.
func (r *sqlRepo) ListSession(kind, session string) ([]Record, error) {
	return r.ListScoped(kind, RecordQuery{Sessions: []string{session}})
}
