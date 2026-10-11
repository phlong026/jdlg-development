package tracking

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

var ErrNotFound = errors.New("not found")

type SQLStore struct{ DB *sql.DB }
type sqlRepo struct {
	ctx    context.Context
	tx     *sql.Tx
	tenant string
}

func (s *SQLStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
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
	if e = fn(&sqlRepo{ctx, tx, tenant}); e != nil {
		return e
	}
	return tx.Commit()
}
func (r *sqlRepo) Get(kind, id string) (Record, error) {
	var v Record
	var b []byte
	var at string
	e := r.tx.QueryRowContext(r.ctx, `SELECT kind,record_id,owner_id,visitor_key,session_id,content_id,share_id,DATE_FORMAT(occurred_at,'%Y-%m-%d %H:%i:%s.%f'),payload FROM tracking_records WHERE tenant_id=? AND kind=? AND record_id=?`, r.tenant, kind, id).Scan(&v.Kind, &v.ID, &v.Owner, &v.Visitor, &v.Session, &v.Content, &v.Share, &at, &b)
	if e == sql.ErrNoRows {
		return v, ErrNotFound
	}
	v.At, _ = time.ParseInLocation("2006-01-02 15:04:05.999999", at, time.UTC)
	v.Tenant = r.tenant
	v.Payload = b
	return v, e
}
func (r *sqlRepo) List(kind string) ([]Record, error) {
	rows, e := r.tx.QueryContext(r.ctx, `SELECT kind,record_id,owner_id,visitor_key,session_id,content_id,share_id,DATE_FORMAT(occurred_at,'%Y-%m-%d %H:%i:%s.%f'),payload FROM tracking_records WHERE tenant_id=? AND kind=? ORDER BY occurred_at,record_id`, r.tenant, kind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var v Record
		var b []byte
		var at string
		if e = rows.Scan(&v.Kind, &v.ID, &v.Owner, &v.Visitor, &v.Session, &v.Content, &v.Share, &at, &b); e != nil {
			return nil, e
		}
		v.At, _ = time.ParseInLocation("2006-01-02 15:04:05.999999", at, time.UTC)
		v.Tenant = r.tenant
		v.Payload = b
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *sqlRepo) write(v Record, upsert bool) error {
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
